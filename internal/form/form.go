// Package form 解析 4GL 的表单引用并校验字段存在性（P3b）。
//
// 职责边界：
//   - index 包提供 .per 的符号表（item/PHANTOM/SCREEN RECORD）与 LayoutRefs；
//   - 本包负责把 `WITH FORM "path"` 的路径解析成 .per 文件、
//     从 4GL 语法树提取表单绑定与字段引用、做作用域跟踪并产出诊断；
//   - server 包负责接进 didOpen/didChange 与配置。
//
// 路径解析规则（语料实测 8209 处 WITH FORM 的分布，见 PLAN §7.2）：
//
//	R1a  <4gl 所在目录>/../per/<basename>.per     —— 94%（tiptop 物理布局）
//	R1b  <4gl 所在目录>/<字面量>.per               —— 字面量即相对路径时
//	R2   工作区 basename 索引唯一命中               —— 4%
//	R3   basename 多命中时用字面量首段（模块名）消歧 —— 0.1%（但消歧全部成功）
//	都失败 → 表单未找到（不报字段错，只报 form-not-found，级别可配置）
//
// 禁止在有歧义时猜测字段：Q3 决策“只在表单唯一解析成功时报告”。
package form

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
)

// maxPerFiles 是工作区 .per 索引的文件数上限（防误把超大目录全扫）。
const maxPerFiles = 100000

// Resolver 解析表单路径并缓存 .per 符号表。
type Resolver struct {
	pool *parser.Pool
	// root 是工作区根的本地路径（可为空：R2/R3 失效，只剩 R1）。
	root string
	// perRoots 是附加解析根（相对 root），按序尝试。
	perRoots []string
	// openDoc 返回已打开文档文本（优先磁盘）。
	openDoc DocSource

	mu        sync.Mutex
	cache     map[string]*cacheEntry // path -> 条目
	indexOnce sync.Once
	perIndex  map[string][]string // basename(lower) -> 绝对路径列表
	importers map[string]map[string]bool
	docEdges  map[string]map[string]bool
}

// DocSource 返回已打开文档的文本（与 globals 包同语义）。
type DocSource func(uri string) ([]byte, bool)

// cacheEntry 是一个 .per 文件的缓存条目。
type cacheEntry struct {
	fi      *index.FileIndex
	lines   *textpos.Index
	uri     string
	mtime   time.Time
	size    int64
	fromDoc bool
}

// NewResolver 构造解析器。
func NewResolver(pool *parser.Pool, root string, perRoots []string, openDoc DocSource) *Resolver {
	return &Resolver{
		pool:      pool,
		root:      root,
		perRoots:  perRoots,
		openDoc:   openDoc,
		cache:     map[string]*cacheEntry{},
		importers: map[string]map[string]bool{},
		docEdges:  map[string]map[string]bool{},
	}
}

// Resolved 是一次表单解析的结果。
type Resolved struct {
	// Path 是 .per 的绝对路径。
	Path string
	// URI 是 file:// URI。
	URI string
	// Index 是 .per 的符号表。
	Index *index.FileIndex
	// Lines 是 .per 的行索引。
	Lines *textpos.Index
	// Literal 是原始字面量（诊断消息用）。
	Literal string
}

// Resolve 把 WITH FORM 的字面量解析为 .per 文件。
//
// 返回 nil 表示未找到（调用方报 form-not-found，不报字段错）。
func (r *Resolver) Resolve(literal, refPath string) *Resolved {
	literal = strings.TrimSpace(literal)
	if literal == "" {
		return nil
	}
	if p, ok := r.candidate(literal, refPath); ok {
		if fi, lines, uri := r.loadIndex(p); fi != nil {
			return &Resolved{Path: p, URI: uri, Index: fi, Lines: lines, Literal: literal}
		}
	}
	return nil
}

// candidate 按规则找 .per 候选路径。
func (r *Resolver) candidate(literal, refPath string) (string, bool) {
	base := basenameNoExt(literal)
	if base == "" {
		return "", false
	}

	// R1a：../per/<base>.per（tiptop 物理布局：模块/4gl 与 模块/per 并列）
	if refPath != "" {
		p := norm(filepath.Join(filepath.Dir(refPath), "..", "per", base+".per"))
		if fileExists(p) {
			return p, true
		}
	}

	// R1b：字面量按引用文件目录拼（含扩展名或不带）
	if refPath != "" {
		p := norm(filepath.Join(filepath.Dir(refPath), literal+".per"))
		if fileExists(p) {
			return p, true
		}
		if !strings.HasSuffix(strings.ToLower(literal), ".per") {
			p2 := norm(filepath.Join(filepath.Dir(refPath), literal))
			if fileExists(p2) {
				return p2, true
			}
		}
	}

	// R1c：perRoots 配置根
	if r.root != "" {
		for _, pr := range r.perRoots {
			p := norm(filepath.Join(r.root, pr, base+".per"))
			if fileExists(p) {
				return p, true
			}
			p2 := norm(filepath.Join(r.root, pr, literal+".per"))
			if fileExists(p2) {
				return p2, true
			}
		}
	}

	// R2/R3：工作区 basename 索引（唯一命中，或模块段消歧）
	hits := r.indexLookup(base)
	if len(hits) == 1 {
		return hits[0], true
	}
	if len(hits) > 1 {
		// R3：字面量首段（模块名，如 amm/42f/x 的 amm）匹配路径中的目录段
		seg := firstSegment(literal)
		if seg != "" {
			var better []string
			for _, h := range hits {
				if pathHasSegment(h, seg) {
					better = append(better, h)
				}
			}
			if len(better) == 1 {
				return better[0], true
			}
		}
		// 歧义：宁可不校验，不猜（Q3 决策）
		return "", false
	}
	return "", false
}

// indexLookup 查工作区 basename 索引（惰性构建）。
func (r *Resolver) indexLookup(base string) []string {
	if r.root == "" {
		return nil
	}
	r.indexOnce.Do(func() {
		r.perIndex = buildPerIndex(r.root)
	})
	if r.perIndex == nil {
		return nil
	}
	return r.perIndex[strings.ToLower(base)]
}

// buildPerIndex 遍历工作区建 basename → 路径列表 索引。
//
// 只收集 .per 文件名，不读内容——7000+ 文件的目录遍历是毫秒级。
// 超过 maxPerFiles 视为异常目录（root 配错），放弃索引。
func buildPerIndex(root string) map[string][]string {
	idx := map[string][]string{}
	count := 0
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if depth > 12 || count > maxPerFiles {
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() {
				// 跳过常见的重目录（.git 一定跳；node_modules/target 防意外）
				switch name {
				case ".git", "node_modules", "target", "dist":
					continue
				}
				walk(filepath.Join(dir, name), depth+1)
				continue
			}
			if strings.EqualFold(filepath.Ext(name), ".per") {
				count++
				base := strings.ToLower(strings.TrimSuffix(name, filepath.Ext(name)))
				idx[base] = append(idx[base], filepath.Join(dir, name))
			}
		}
	}
	walk(root, 0)
	if count > maxPerFiles {
		return nil
	}
	return idx
}

// loadIndex 加载并解析 .per（缓存 + mtime 校验），与 globals 包同套路。
func (r *Resolver) loadIndex(path string) (*index.FileIndex, *textpos.Index, string) {
	uri := URIOfPath(path)

	if r.openDoc != nil {
		if text, ok := r.openDoc(uri); ok {
			if fi := r.parseAndCache(path, uri, text, true); fi != nil {
				return fi, r.linesOf(path), uri
			}
			return nil, nil, uri
		}
	}

	if st, err := os.Stat(path); err == nil {
		r.mu.Lock()
		cached := r.cache[key(path)]
		r.mu.Unlock()
		if cached != nil && !cached.fromDoc &&
			cached.mtime.Equal(st.ModTime()) && cached.size == st.Size() {
			return cached.fi, cached.lines, cached.uri
		}
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, uri
	}
	fi := r.parseAndCache(path, uri, data, false)
	return fi, r.linesOf(path), uri
}

func (r *Resolver) parseAndCache(path, uri string, text []byte, fromDoc bool) *index.FileIndex {
	tree, err := r.pool.Parse(parser.LangPer, text, nil)
	if err != nil {
		return nil
	}
	defer tree.Close()

	fi := index.BuildPer(tree, text, 0)
	fi.URI = uri

	var mtime time.Time
	var size int64
	if !fromDoc {
		if info, err := os.Stat(path); err == nil {
			mtime = info.ModTime()
			size = info.Size()
		}
	}

	r.mu.Lock()
	r.cache[key(path)] = &cacheEntry{
		fi:      fi,
		lines:   textpos.New(text),
		uri:     uri,
		mtime:   mtime,
		size:    size,
		fromDoc: fromDoc,
	}
	r.mu.Unlock()
	return fi
}

func (r *Resolver) linesOf(path string) *textpos.Index {
	r.mu.Lock()
	cached := r.cache[key(path)]
	r.mu.Unlock()
	if cached == nil {
		return nil
	}
	return cached.lines
}

// AddEdge 登记引用关系（4gl 文档 → .per 路径），供刷新诊断用。
func (r *Resolver) AddEdge(docURI, perPath string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.importers[perPath] == nil {
		r.importers[perPath] = map[string]bool{}
	}
	r.importers[perPath][docURI] = true
	if r.docEdges[docURI] == nil {
		r.docEdges[docURI] = map[string]bool{}
	}
	r.docEdges[docURI][perPath] = true
}

// ClearEdges 清掉文档的全部引用边（导入可能已变）。
func (r *Resolver) ClearEdges(docURI string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for path := range r.docEdges[docURI] {
		if set := r.importers[path]; set != nil {
			delete(set, docURI)
			if len(set) == 0 {
				delete(r.importers, path)
			}
		}
	}
	delete(r.docEdges, docURI)
}

// Importers 返回引用了某 .per 的文档 URI 列表（排序稳定）。
func (r *Resolver) Importers(path string) []string {
	r.mu.Lock()
	set := r.importers[norm(path)]
	out := make([]string, 0, len(set))
	for uri := range set {
		out = append(out, uri)
	}
	r.mu.Unlock()
	sort.Strings(out)
	return out
}

// Refresh 使 .per 缓存失效。
func (r *Resolver) Refresh(path string) {
	r.mu.Lock()
	delete(r.cache, key(path))
	r.mu.Unlock()
}

// RefreshURI 是 Refresh 的 URI 版本。
func (r *Resolver) RefreshURI(uri string) { r.Refresh(PathOfURI(uri)) }

// Stats 返回缓存条目数（测试用）。
func (r *Resolver) Stats() int {
	r.mu.Lock()
	n := len(r.cache)
	r.mu.Unlock()
	return n
}

func basenameNoExt(literal string) string {
	base := filepath.Base(strings.ReplaceAll(literal, "\\", "/"))
	return strings.TrimSuffix(base, filepath.Ext(base))
}

func firstSegment(literal string) string {
	parts := strings.Split(strings.ReplaceAll(literal, "\\", "/"), "/")
	if len(parts) > 1 {
		return strings.ToLower(parts[0])
	}
	return ""
}

func pathHasSegment(path, seg string) bool {
	for _, p := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.ToLower(p) == seg {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

func norm(path string) string { return filepath.Clean(path) }

func key(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// ---------------------------------------------------------------------------
// URI ↔ 路径（与 globals 包同实现；两包各自持有一份避免相互依赖）
// ---------------------------------------------------------------------------

// PathOfURI 把 file:// URI 转为本地路径。
func PathOfURI(uri string) string {
	const prefix = "file://"
	if !strings.HasPrefix(uri, prefix) {
		return uri
	}
	p := strings.TrimPrefix(uri, prefix)
	if len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	return strings.ReplaceAll(p, "%20", " ")
}

// URIOfPath 把本地路径转为 file:// URI。
func URIOfPath(path string) string {
	p := strings.ReplaceAll(path, "\\", "/")
	p = strings.ReplaceAll(p, " ", "%20")
	if len(p) > 1 && p[1] == ':' {
		return "file:///" + p
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + p
}
