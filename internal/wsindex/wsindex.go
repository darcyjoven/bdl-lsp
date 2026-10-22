// Package wsindex 建立**工作区级**符号索引（P6：全工作区符号索引）。
//
// 为什么需要：此前 `workspace/symbol` / 跳转 / 引用只覆盖"已打开的文档"，
// 而真实项目是 7000+ 个 `.4gl`（语料实测 7600）。没有索引时，跳不到没打开的
// 文件、也搜不到没打开过的函数——用户看到的是"有时候能跳，有时候不能"。
//
// # 只收"能跨文件引用"的东西
//
// 索引存声明与调用点两类记录，且只收**模组级**符号：局部变量/参数/记录字段
// 在同一文件内已经有 per-document 索引，放进工作区索引只会把 `workspace/symbol`
// 变成噪声（实测局部符号数量是顶层符号的 5~6 倍）。
//
// # 持久化
//
// 7600 个文件全量解析约 20-25 s（8 核）。每次都重扫是不可接受的，所以：
//   - 快照按文件记录 (mtime,size)，未变的文件直接复用上次结果；
//   - 快照 gob+gzip 落盘（实测 ~3-5 MB），冷启动只做 stat（15k 次 stat < 1 s）；
//   - 解析器版本（`Grammar`）变化时整份作废——符号提取逻辑跟着代码走，
//     版本号不带会让"升级后还用旧快照"的 bug 极难发现。
package wsindex

import (
	"compress/gzip"
	"context"
	"encoding/gob"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/darcyjoven/bdl-lsp/internal/glob"
	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/link"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

// Version 是快照格式版本。字段含义变化（不只是加字段）时必须 +1。
const Version = 1

// Decl 是一条声明记录。
type Decl struct {
	// Name 是源码写法（保留大小写），Lower 是小写（Genero 大小写不敏感）。
	Name  string
	Lower string
	// Kind 取自 index.Kind（program/function/report/variable/record/...）。
	Kind string
	// Module 是模组名（tiptop 布局里的目录名，如 aap）；无法判断时为空。
	Module string
	// File 是绝对路径。
	File string
	// Line / Col 是符号名起始位置（0-based）。
	Line int
	Col  int
	// Detail 是签名或类型文本（可空），供 `workspace/symbol` 的容器/详情展示。
	Detail string
}

// Call 是一条调用点记录。
type Call struct {
	// Lower 是小写函数名；Name 保留源码写法。
	Name  string
	Lower string
	File  string
	Line  int
	Col   int
	// Len 是名字长度（按字节），用于算 Range 的结束位置。
	Len int
}

// File 是单个文件的索引结果（含复用所需的元信息）。
type File struct {
	Path  string
	MTime int64
	Size  int64
	// Stale 表示上次解析失败或文件读不到（保留条目以免每次重试）。
	Stale bool
	Decls []Decl
	Calls []Call
}

// Index 是一份工作区符号快照。
type Index struct {
	Version int
	// Grammar 是解析器/提取逻辑的指纹（见包注释）。
	Grammar string
	Root    string
	Files   []File
}

// Options 是 Build 的参数。
type Options struct {
	// Root 是工作区根（绝对路径）。
	Root string
	// ModuleRoots 是模组根（相对 Root）。空 = 自动探测（tiptop/topcust）。
	ModuleRoots []string
	// Grammar 是解析器指纹，与快照里的不一致时不做复用。
	Grammar string
	// Workers 是并行度，<=0 时取 GOMAXPROCS。
	Workers int
	// Limit 限制文件数（调试/测试用），0=全部。
	Limit int
	// Exclude 是排除的文件 glob（相对 Root，`**` 跨目录）。
	// 来自 `bdl.ignore.files`：构建产物、历史遗留目录不进索引。
	Exclude []string
	// Progress 每处理约 200 个文件回调一次（可在任意 goroutine 调用）。
	Progress func(done, total int)
	// Logf 记录非致命错误（可为 nil）。
	Logf func(format string, args ...any)
}

// Stats 是本次构建的统计（供日志与进度展示）。
type Stats struct {
	Total   int
	Reused  int
	Parsed  int
	Failed  int
	Elapsed int64 // 毫秒
}

// Bindings 由调用方提供，用于解析与调用点提取（可复用已有实例）。
type Bindings struct {
	Pool  *parser.Pool
	Calls *index.CallCollector
}

// Build 建立（或增量刷新）工作区索引。
//
// prev 非 nil 且 Grammar 一致时，未变化的文件直接复用；ctx 取消后尽快返回。
func Build(ctx context.Context, b Bindings, opt Options, prev *Index) (*Index, Stats, error) {
	if b.Pool == nil {
		return nil, Stats{}, fmt.Errorf("wsindex: 缺少 parser.Pool")
	}
	if b.Calls == nil {
		return nil, Stats{}, fmt.Errorf("wsindex: 缺少 CallCollector")
	}

	layout := link.NewLayout(opt.Root, opt.ModuleRoots)
	// 单个写错的排除模式只跳过它自己：配置校验会给出警告，但不应让
	// 整个工作区索引构建失败（用户只是多打了一个 `[`）。
	var exclude glob.Set
	for _, p := range opt.Exclude {
		g, gerr := glob.Compile(p)
		if gerr != nil {
			if opt.Logf != nil {
				opt.Logf("wsindex: 跳过非法排除 glob %q: %v", p, gerr)
			}
			continue
		}
		exclude = append(exclude, g)
	}
	files, err := enumerate(layout, opt.Limit, exclude)
	if err != nil {
		return nil, Stats{}, err
	}
	workers := opt.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}

	reuse := map[string]*File{}
	if prev != nil && prev.Grammar == opt.Grammar {
		for i := range prev.Files {
			f := &prev.Files[i]
			reuse[f.Path] = f
		}
	}

	idx := &Index{Version: Version, Grammar: opt.Grammar, Root: opt.Root}
	idx.Files = make([]File, len(files))
	stats := Stats{Total: len(files)}

	progressStep := 200
	done := 0
	report := func() {
		if opt.Progress != nil && (done%progressStep == 0 || done == len(files)) {
			opt.Progress(done, len(files))
		}
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var mu sync.Mutex
	var wg sync.WaitGroup
	work := make(chan int)
	// 每个 worker 读自己的槽位；ctx 取消时尽快退出（不再发新任务）。
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range work {
				path := files[i]
				var f File
				if old := reuse[path]; old != nil {
					if sameFile(old, path) {
						f = *old
						mu.Lock()
						stats.Reused++
						done++
						report()
						mu.Unlock()
						idx.Files[i] = f
						continue
					}
				}
				f = scanFile(b, path)
				mu.Lock()
				stats.Parsed++
				if f.Stale {
					stats.Failed++
				}
				done++
				report()
				mu.Unlock()
				idx.Files[i] = f
			}
		}()
	}

	var sendErr error
	for i := range files {
		// 先查一次：select 的两个 case 同时就绪时是随机选的，
		// 已取消的 context 可能连发好几个任务才发现（-race 下必现）。
		if err := ctx.Err(); err != nil {
			sendErr = err
			break
		}
		select {
		case <-ctx.Done():
			sendErr = ctx.Err()
		case work <- i:
		}
		if sendErr != nil {
			break
		}
	}
	close(work)
	wg.Wait()
	if sendErr != nil {
		return nil, stats, sendErr
	}
	return idx, stats, nil
}

// sameFile 判断文件自上次索引以来是否未变（mtime + size）。
//
// 只用 mtime 不够：rsync/解压可能保留 mtime 却换了内容；加上 size 能挡住
// 绝大多数情况，而哈希每个文件（7600 × 平均 200 KB）会把冷启动从 1 s 拖到 10 s+。
func sameFile(f *File, path string) bool {
	st, err := os.Stat(path)
	if err != nil {
		return false
	}
	return st.ModTime().UnixNano() == f.MTime && st.Size() == f.Size
}

// scanFile 解析单个文件并抽取声明与调用点。
func scanFile(b Bindings, path string) File {
	f := File{Path: path}
	st, err := os.Stat(path)
	if err != nil {
		f.Stale = true
		return f
	}
	f.MTime = st.ModTime().UnixNano()
	f.Size = st.Size()

	src, err := os.ReadFile(path)
	if err != nil {
		f.Stale = true
		return f
	}
	tree, err := b.Pool.Parse(parser.LangBDL, src, nil)
	if err != nil || tree == nil {
		f.Stale = true
		return f
	}
	defer tree.Close()

	fi := index.Build(parser.LangBDL, tree, src, 0)
	if fi == nil {
		f.Stale = true
		return f
	}
	idx := newLineIndex(src)
	module := moduleOf(path)
	for _, sym := range fi.Symbols {
		// 只收模组级：函数体内/参数/记录字段在同文件索引里，放进来只会是噪声。
		if sym.Scope != index.ScopeGlobal && sym.Scope != index.ScopeGlobals {
			continue
		}
		if !indexableKind(sym.Kind) {
			continue
		}
		line, col := idx.at(sym.StartByte)
		name := sym.Name
		if sym.Kind == index.KindMain {
			// `MAIN` 的符号名在语法树里是字面量 "main"（不是程序代码）。
			// 工作区索引里 7600 个 "main" 毫无用处，用户搜的是程序代码，
			// 所以用文件名兜底（tiptop 布局下文件名即程序代码）。
			name = codeOf(path)
		}
		f.Decls = append(f.Decls, Decl{
			Name:   name,
			Lower:  strings.ToLower(name),
			Kind:   string(sym.Kind),
			Module: module,
			File:   path,
			Line:   line,
			Col:    col,
			Detail: sym.Signature,
		})
	}

	for _, c := range b.Calls.Calls(tree, src) {
		line, col := idx.at(c.Start)
		f.Calls = append(f.Calls, Call{
			Name:  c.Name,
			Lower: c.Lower,
			File:  path,
			Line:  line,
			Col:   col,
			Len:   int(c.End - c.Start),
		})
	}
	return f
}

// indexableKind 报告该符号种类是否值得进工作区索引。
//
// 排除字段（记录成员）与参数：它们数量最大、跨文件无意义。
func indexableKind(k index.Kind) bool {
	switch k {
	case index.KindMain, index.KindFunction, index.KindReport, index.KindDefine,
		index.KindConstant, index.KindType, index.KindRecord, index.KindPrepare,
		index.KindCursor:
		return true
	}
	return false
}

// codeOf 取文件名（去扩展名），作为 MAIN 程序的搜索名。
func codeOf(path string) string {
	base := filepath.Base(path)
	if i := strings.LastIndexByte(base, '.'); i > 0 {
		return base[:i]
	}
	return base
}

// moduleOf 从路径取模组名：`.../<root>/<module>/4gl/x.4gl` → module。
//
// 不依赖 layout 的缓存，便于在 worker 里并发调用（纯字符串处理）。
func moduleOf(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := len(parts) - 1; i > 1; i-- {
		if parts[i] == "4gl" || parts[i] == "per" || parts[i] == "4fd" {
			return strings.ToLower(parts[i-1])
		}
	}
	return ""
}

// lineIndex 把字节偏移换算成 (行, 列)。
type lineIndex struct{ starts []int }

func newLineIndex(src []byte) *lineIndex {
	li := &lineIndex{starts: []int{0}}
	for i, c := range src {
		if c == '\n' {
			li.starts = append(li.starts, i+1)
		}
	}
	return li
}

func (li *lineIndex) at(off uint) (line, col int) {
	i := sort.Search(len(li.starts), func(i int) bool { return li.starts[i] > int(off) }) - 1
	if i < 0 {
		i = 0
	}
	return i, int(off) - li.starts[i]
}

// enumerate 列出待索引的文件（.4gl 与 .global）。
//
// 只走模组根，避免把工作区里的 node_modules / 构建产物扫进来。
func enumerate(layout *link.Layout, limit int, exclude glob.Set) ([]string, error) {
	var files []string
	for _, root := range layout.RootDirs() {
		err := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return nil // 权限/竞态：跳过，不中断整次索引
			}
			if d.IsDir() {
				name := d.Name()
				if name == "node_modules" || name == ".git" || strings.HasPrefix(name, "artifacts") {
					return filepath.SkipDir
				}
				return nil
			}
			lower := strings.ToLower(p)
			if strings.HasSuffix(lower, ".4gl") || strings.HasSuffix(lower, ".global") {
				if !exclude.Empty() {
					rel, rerr := filepath.Rel(layout.ProjectRoot, p)
					if rerr == nil {
						rel = filepath.ToSlash(rel)
						if exclude.Match(rel) || exclude.Match(filepath.Base(rel)) {
							return nil
						}
					}
				}
				files = append(files, p)
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files, nil
}

// Search 按子串搜索声明（空查询返回前 limit 条）。
//
// 结果按 (模块, 文件, 行) 排序，保证同样输入同样输出——`workspace/symbol`
// 的结果顺序直接影响用户体验，不能依赖 map 迭代。
func (ix *Index) Search(query string, limit int) []Decl {
	if ix == nil {
		return nil
	}
	q := strings.ToLower(strings.TrimSpace(query))
	var out []Decl
	for i := range ix.Files {
		for j := range ix.Files[i].Decls {
			d := &ix.Files[i].Decls[j]
			if q != "" && !strings.Contains(d.Lower, q) {
				continue
			}
			out = append(out, *d)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Module != out[j].Module {
			return out[i].Module < out[j].Module
		}
		if out[i].File != out[j].File {
			return out[i].File < out[j].File
		}
		return out[i].Line < out[j].Line
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// Lookup 精确查找同名声明（大小写不敏感），优先模组级函数类符号。
func (ix *Index) Lookup(name string) []Decl {
	if ix == nil {
		return nil
	}
	lower := strings.ToLower(name)
	var out []Decl
	for i := range ix.Files {
		for j := range ix.Files[i].Decls {
			d := &ix.Files[i].Decls[j]
			if d.Lower == lower {
				out = append(out, *d)
			}
		}
	}
	return out
}

// CallsOf 返回某个函数名在全部文件里的调用点。
func (ix *Index) CallsOf(name string) []Call {
	if ix == nil {
		return nil
	}
	lower := strings.ToLower(name)
	var out []Call
	for i := range ix.Files {
		for j := range ix.Files[i].Calls {
			c := &ix.Files[i].Calls[j]
			if c.Lower == lower {
				out = append(out, *c)
			}
		}
	}
	return out
}

// FileCount 返回索引里的文件数。
func (ix *Index) FileCount() int {
	if ix == nil {
		return 0
	}
	return len(ix.Files)
}

// Save 把快照写成 gob+gzip（先写临时文件再 rename，避免中断留下半个文件）。
func Save(path string, ix *Index) error {
	if ix == nil {
		return fmt.Errorf("wsindex: nil 快照")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	fh, err := os.Create(tmp)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	zw := gzip.NewWriter(fh)
	enc := gob.NewEncoder(zw)
	if err := enc.Encode(ix); err != nil {
		fh.Close()
		return err
	}
	if err := zw.Close(); err != nil {
		fh.Close()
		return err
	}
	if err := fh.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Load 读取快照；文件不存在或格式不认识时返回 (nil, error)。
func Load(path string) (*Index, error) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer fh.Close()
	zr, err := gzip.NewReader(fh)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	var ix Index
	if err := gob.NewDecoder(zr).Decode(&ix); err != nil {
		return nil, err
	}
	if ix.Version != Version {
		return nil, fmt.Errorf("wsindex: 快照版本 %d，期望 %d", ix.Version, Version)
	}
	return &ix, nil
}

// CachePath 返回工作区对应的快照路径。
//
// 放系统缓存目录而不是工作区里的 `.bdl-lsp/`：索引是纯缓存，用户不该在
// 自己的仓库里看到它，也不该被 git 状态打扰。缓存目录不可用时退回工作区
// 下的 `.bdl-lsp/`（至少能用）。
func CachePath(root string) string {
	if dir, err := os.UserCacheDir(); err == nil && dir != "" {
		return filepath.Join(dir, "bdl-lsp", sanitize(root)+".idx")
	}
	return filepath.Join(root, ".bdl-lsp", "symbols.idx")
}

// sanitize 把路径压成安全的文件名（缓存目录里要能区分不同工作区）。
func sanitize(root string) string {
	repl := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	name := repl.Replace(strings.Trim(root, "/\\"))
	if len(name) > 80 {
		name = name[len(name)-80:]
	}
	if name == "" {
		name = "workspace"
	}
	return name
}
