// Package globals 解析 `GLOBALS "file"` 引用（P3a）。
//
// 职责边界：
//   - index 包负责从语法树提取 GlobalsImport（字面量与位置）；
//   - 本包负责把字面量变成绝对路径、读文件、解析并缓存符号表；
//   - server 包负责把结果接进诊断与跳转。
//
// 路径解析规则（按序）：
//
//	R1  文件名是变量        → Unresolvable（编译期才知值，静默降级）
//	R2  相对引用文件所在目录  → 存在即 Resolved
//	R3  依次尝试 searchPaths → 第一个存在的为 Resolved
//	R4  都不存在            → NotFound（触发 file-not-found 诊断）
//
// 禁止按 basename 在工作区隐式搜索：语料里有两个内容不同的同名
// top.global（tiptop/config 与 topcust/config），隐式匹配会解析到错误文件。
package globals

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	"github.com/darcyjoven/bdl-lsp/parser"
)

// maxDepth 是 GLOBALS 链的递归解析深度上限（防环；语料实测为 0 层链）。
const maxDepth = 8

// Status 是一条导入的解析状态。
type Status int

const (
	// StatusResolved 找到文件且成功解析。
	StatusResolved Status = iota
	// StatusNotFound 目标文件不存在（触发 file-not-found 诊断）。
	StatusNotFound
	// StatusUnresolvable 文件名是变量，静态无法解析（静默降级）。
	StatusUnresolvable
)

// String 返回状态的短名，用于日志与测试输出。
func (s Status) String() string {
	switch s {
	case StatusResolved:
		return "resolved"
	case StatusNotFound:
		return "not-found"
	case StatusUnresolvable:
		return "unresolvable"
	default:
		return "unknown"
	}
}

// Entry 是一条导入的解析结果。
type Entry struct {
	// Import 是原始导入信息（字面量、位置、是否变量形式）。
	Import index.GlobalsImport
	// Path 是解析出的绝对路径（非 Resolved 时为空）。
	Path string
	// URI 是 Path 对应的 file:// URI（非 Resolved 时为空）。
	URI string
	// Status 是解析状态。
	Status Status
	// Index 是 Resolved 时目标文件的符号表（含 ScopeGlobals 符号与
	// 它自己的 GlobalsImports，供递归与后续功能使用）。其他状态为 nil。
	Index *index.FileIndex
	// Lines 是 Resolved 时目标文件的行索引（字节偏移 → 行列换算），
	// 供跳转/悬停构造跨文件 Location。其他状态为 nil。
	Lines *textpos.Index
}

// Literal 便捷返回导入字面量。
func (e Entry) Literal() string { return e.Import.Literal }

// DocSource 返回已打开文档的文本。
//
// server 接线时传入 document.Store 的适配器：编辑器里打开的 .global
// 优先用内存版本（磁盘版可能过期）。nil 表示总是读磁盘。
type DocSource func(uri string) ([]byte, bool)

// Resolver 解析 GLOBALS 导入并缓存目标文件的符号表。
//
// 缓存策略：按规范化路径缓存 FileIndex，mtime+size 未变时复用；
// 已打开文档（DocSource 命中）的条目跳过 mtime 检查（内存版始终最新），
// 由调用方在文档变更时调用 Refresh 使其失效。
type Resolver struct {
	pool *parser.Pool
	// root 是工作区根的本地路径，可为空（空时 searchPaths 不生效）。
	root string
	// searchPaths 是附加解析根（相对 root），按序尝试。
	searchPaths []string
	// openDoc 返回已打开文档文本。
	openDoc DocSource
	// parseTimeout 是单次解析超时。
	parseTimeout time.Duration

	mu        sync.Mutex
	cache     map[string]*cacheEntry
	importers map[string]map[string]bool // path -> 引用它的 docURI 集合
	docEdges  map[string]map[string]bool // docURI -> 它引用的 path 集合
}

// cacheEntry 是一个目标文件的缓存条目。
type cacheEntry struct {
	fi      *index.FileIndex
	lines   *textpos.Index
	uri     string
	mtime   time.Time
	size    int64
	fromDoc bool // 来自已打开文档（跳过 mtime 检查）
}

// NewResolver 构造解析器。pool 由调用方创建并负责生命周期（Close）。
func NewResolver(pool *parser.Pool, root string, searchPaths []string, openDoc DocSource) *Resolver {
	return &Resolver{
		pool:         pool,
		root:         root,
		searchPaths:  searchPaths,
		openDoc:      openDoc,
		parseTimeout: 2 * time.Second,
		cache:        map[string]*cacheEntry{},
		importers:    map[string]map[string]bool{},
		docEdges:     map[string]map[string]bool{},
	}
}

// Resolve 解析一个文档的全部导入，并更新反向依赖表。
//
// 同一文档重复调用安全（旧的引用关系会被替换）。
// 目标文件自身的导入会递归解析（visiting 集合防环，深度上限 maxDepth）。
func (r *Resolver) Resolve(ctx context.Context, docURI string, imports []index.GlobalsImport) []Entry {
	r.mu.Lock()
	// 清掉该文档的旧引用边（导入列表可能已变）
	for path := range r.docEdges[docURI] {
		if set := r.importers[path]; set != nil {
			delete(set, docURI)
			if len(set) == 0 {
				delete(r.importers, path)
			}
		}
	}
	delete(r.docEdges, docURI)
	r.mu.Unlock()

	return r.resolveImports(ctx, docURI, imports, map[string]bool{})
}

// resolveImports 是 Resolve 的核心，visiting 是当前解析链上的路径集合。
//
// 环防护在这里做（不能放 loadIndex）：
//   - loadIndex 返回缓存命中的 fi 后，调用方仍会看到 fi.GlobalsImports
//     非空而继续递归；缓存只让每层变快，挡不住递归本身；
//   - visiting 的生命周期必须覆盖整个递归子树：进入某个 target 前置位、
//     处理完它的全部子导入后才回退。曾在 loadIndex 里用 defer 回退，
//     导致集合永远为空、环检查形同虚设（实测栈溢出）。
func (r *Resolver) resolveImports(ctx context.Context, docURI string, imports []index.GlobalsImport, visiting map[string]bool) []Entry {
	docPath := PathOfURI(docURI)

	entries := make([]Entry, 0, len(imports))

	addEdge := func(path string) {
		r.mu.Lock()
		if r.importers[path] == nil {
			r.importers[path] = map[string]bool{}
		}
		r.importers[path][docURI] = true
		if r.docEdges[docURI] == nil {
			r.docEdges[docURI] = map[string]bool{}
		}
		r.docEdges[docURI][path] = true
		r.mu.Unlock()
	}

	for _, imp := range imports {
		var e Entry
		e.Import = imp

		switch {
		case imp.IsVariable: // R1
			e.Status = StatusUnresolvable
		default:
			path, exists := r.candidate(docPath, imp.Literal)
			if !exists {
				e.Status = StatusNotFound // R4
				// 反向依赖边仍然要建（登记全部候选位置）：
				// 文件后来在任何候选位置出现（创建/打开）时，
				// refreshImporters 才能找到本文档并清掉警告。
				for _, p := range r.allCandidates(docPath, imp.Literal) {
					addEdge(p)
				}
				break
			}

			// 环或超深：不再深入子导入。
			// 当前 entry 仍解析（缓存已在首次访问时写入），
			// 但不递归它的 GlobalsImports，否则 a→b→a 会无限循环。
			onChain := visiting[path] || len(visiting) >= maxDepth

			fi, lines, uri := r.loadIndex(ctx, path)
			if fi == nil {
				// 文件存在但读/解析失败，按未找到降级
				e.Status = StatusNotFound
				addEdge(path)
				break
			}
			e.Status = StatusResolved
			e.Path = path
			e.URI = uri
			e.Index = fi
			e.Lines = lines
			addEdge(path)

			if onChain {
				break
			}

			// 递归解析目标文件自己的导入（语料为 0 层链，语法允许）。
			// visiting 含 path 的整个子树处理期间不回退，环在此被挡住。
			visiting[path] = true
			if len(fi.GlobalsImports) > 0 {
				r.resolveImports(ctx, uri, fi.GlobalsImports, visiting)
			}
			delete(visiting, path)
		}
		entries = append(entries, e)
	}
	return entries
}

// candidate 按规则 R2/R3 找到第一个存在的候选路径。
//
// 返回值：path 是**首选预期路径**（R2 的结果；literal 为绝对路径时就是它本身），
// exists 表示该路径是否存在。path 始终非空（literal 非空时）——
// 即使不存在也要返回预期位置，供调用方建立反向依赖边：
// 文件后来被创建/打开时，引用方的诊断才能被刷新
// （否则 NotFound 的导入永远收不到"文件出现了"的通知）。
func (r *Resolver) candidate(docPath, literal string) (path string, exists bool) {
	if literal == "" {
		return "", false
	}

	// 绝对路径：直接判断存在性
	if filepath.IsAbs(literal) {
		p := r.norm(literal)
		return p, fileExists(p)
	}

	// R2：相对引用文件所在目录（预期位置）
	fallback := ""
	if docPath != "" {
		fallback = r.norm(filepath.Join(filepath.Dir(docPath), literal))
		if fileExists(fallback) {
			return fallback, true
		}
	}

	// R3：依次尝试 searchPaths（相对工作区根）
	if r.root != "" {
		for _, sp := range r.searchPaths {
			p := r.norm(filepath.Join(r.root, sp, literal))
			if fileExists(p) {
				return p, true
			}
		}
	}
	return fallback, false
}

// allCandidates 返回全部候选路径（R2 + 各 searchPath），按尝试顺序。
//
// 用于 NotFound 时把每个可能的位置都登记进反向依赖表，
// 文件在任何候选位置出现都能触发引用方刷新。
func (r *Resolver) allCandidates(docPath, literal string) []string {
	if literal == "" {
		return nil
	}
	var out []string
	add := func(p string) {
		if p != "" {
			out = append(out, r.norm(p))
		}
	}
	if filepath.IsAbs(literal) {
		add(literal)
		return out
	}
	if docPath != "" {
		add(filepath.Join(filepath.Dir(docPath), literal))
	}
	if r.root != "" {
		for _, sp := range r.searchPaths {
			add(filepath.Join(r.root, sp, literal))
		}
	}
	return out
}

// loadIndex 加载并解析目标文件（带缓存），返回符号表与 URI。
//
// 返回 nil 表示文件读不到或解析失败（调用方按 NotFound 降级）。
// 环防护不在本方法：见 resolveImports 的注释。
func (r *Resolver) loadIndex(ctx context.Context, path string) (*index.FileIndex, *textpos.Index, string) {
	uri := URIOfPath(path)

	// 已打开文档：优先用内存版本（磁盘可能过期）
	if r.openDoc != nil {
		if text, ok := r.openDoc(uri); ok {
			if fi := r.parseAndCache(path, uri, text, true); fi != nil {
				return fi, r.linesOf(path), uri
			}
			return nil, nil, uri
		}
	}

	// 缓存命中：mtime+size 未变则复用
	if st, err := os.Stat(path); err == nil {
		r.mu.Lock()
		cached := r.cache[r.key(path)]
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

// linesOf 从缓存取行索引（parseAndCache 刚写入，必然存在）。
func (r *Resolver) linesOf(path string) *textpos.Index {
	r.mu.Lock()
	cached := r.cache[r.key(path)]
	r.mu.Unlock()
	if cached == nil {
		return nil
	}
	return cached.lines
}

// parseAndCache 解析文本并写入缓存。
func (r *Resolver) parseAndCache(path, uri string, text []byte, fromDoc bool) *index.FileIndex {
	// .per 文件也可能被引用（语法上无限制）；按扩展名选语言，
	// 不支持的语言读不出符号，返回空索引而不是失败（避免误报 NotFound）。
	lang, ok := parser.LangForPath(path)
	if !ok {
		lang = parser.LangBDL // 未知扩展名按 bdl 尝试（语料有 .inc 等目标）
	}

	ctx, cancel := context.WithTimeout(context.Background(), r.parseTimeout)
	defer cancel()
	tree, err := r.pool.ParseWith(ctx, lang, text, nil, parser.ParseOptions{
		Timeout: r.parseTimeout,
	})
	if err != nil {
		// 解析失败（含超时）：返回 nil，调用方按读失败处理。
		// 语法错误不算失败——HasError 的树仍能提取符号（与 server 现行为一致）。
		return nil
	}
	defer tree.Close()

	fi := index.Build(lang, tree, text, 0)
	fi.URI = uri

	// fromDoc 时没有对应的磁盘 stat（内存版始终最新，mtime 检查被跳过）；
	// 注意 os.FileInfo 为 nil 接口时调 ModTime 会 panic，这里必须判空。
	var mtime time.Time
	var size int64
	if !fromDoc {
		if info, err := os.Stat(path); err == nil {
			mtime = info.ModTime()
			size = info.Size()
		}
	}

	r.mu.Lock()
	r.cache[r.key(path)] = &cacheEntry{
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

// Refresh 使一个目标文件的缓存失效（下次 Resolve 时重读）。
//
// server 在 .global 文档 didChange/didOpen 后调用，
// 保证已打开文档的内存版本变化能传播到缓存。
func (r *Resolver) Refresh(path string) {
	r.mu.Lock()
	delete(r.cache, r.key(path))
	r.mu.Unlock()
}

// RefreshURI 是 Refresh 的 URI 版本。
func (r *Resolver) RefreshURI(uri string) { r.Refresh(PathOfURI(uri)) }

// Importers 返回引用了指定路径的文档 URI 列表（排序稳定，便于测试）。
//
// 当一个 .global 文件变更时，用它的引用者集合决定要刷新哪些文档的诊断。
func (r *Resolver) Importers(path string) []string {
	r.mu.Lock()
	set := r.importers[r.norm(path)]
	out := make([]string, 0, len(set))
	for uri := range set {
		out = append(out, uri)
	}
	r.mu.Unlock()
	sort.Strings(out)
	return out
}

// LinesFor 返回目标文件的行索引（缓存里取；未缓存返回 nil）。
//
// 跳转/悬停构造跨文件 Location 时使用；调用方应先 Resolve 过该文档。
func (r *Resolver) LinesFor(path string) *textpos.Index {
	r.mu.Lock()
	cached := r.cache[r.key(r.norm(path))]
	r.mu.Unlock()
	if cached == nil {
		return nil
	}
	return cached.lines
}

// Stats 返回缓存条目数（测试与日志用）。
func (r *Resolver) Stats() (cached int) {
	r.mu.Lock()
	cached = len(r.cache)
	r.mu.Unlock()
	return
}

// key 是缓存键：Windows 大小写不敏感文件系统统一小写。
func (r *Resolver) key(path string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(path)
	}
	return path
}

// norm 规范化路径分隔符（字面量里可能写 /，Windows 上转 \）。
func (r *Resolver) norm(path string) string {
	return filepath.Clean(path)
}

func fileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}

// ---------------------------------------------------------------------------
// URI ↔ 路径转换（与 internal/config 的私有实现同语义，这里导出共用）
// ---------------------------------------------------------------------------

// PathOfURI 把 file:// URI 转为本地路径；非 file URI 原样返回。
//
// 实现委托给 internal/fileuri（P7-A-2）：旧实现在这里手写，
// 只剥 `file://` 与 `%20`，遇到 VSCode 在 Windows 上编码的 `%3A`
// （`file:///c%3A/...`）会解出 `/c%3A/...` 非法路径，导致 GLOBALS
// 路径全部失效。fileuri 做完整百分号解码 + 盘符/UNC 归一。
//
// 保留本函数名是为了不改动全部调用点；逻辑只有一份。
func PathOfURI(uri string) string {
	p, err := fileuri.ToPath(uri)
	if err != nil {
		// 非 file URI（untitled:/git: 等）：原样返回，保持旧行为。
		return uri
	}
	return p
}

// URIOfPath 把本地路径转为 file:// URI（委托 fileuri）。
func URIOfPath(path string) string { return fileuri.FromPath(path) }
