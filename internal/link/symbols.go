package link

import (
	"os"
	"sort"
	"strings"
	"sync"
)

// ParseFunc 从一个 .4gl 文件里提取"可被调用的函数名"（返回小写）。
//
// 为什么由外部注入：本包不想依赖 tree-sitter（纯数据逻辑不该带 cgo），
// 而且测试需要能一键造假。真实实现由 server / CLI 提供。
type ParseFunc func(path string) ([]string, error)

// maxSyncDefault 是同步预热的文件数上限。
//
// 依据实测：链大小 p50=1、p90=3，典型链解析只要 2–10 ms，同步做完全无感；
// 但最大的链有 364 个文件（约 3 s），文件被 300+ 组共享的 `aws_xml`
// 并集有 953 个（约 8 s）——这类必须走后台，否则 didOpen 会卡住。
const maxSyncDefault = 20

// SymbolIndex 缓存每个文件的函数名，并提供并集查询。
//
// 两级缓存：
//   - 文件级：path → 函数名（实测 8.7 ms/文件，重用一次就回本）；
//   - 全局模组级：sub/lib/qry 的函数并集，后台一次性预热（约 983 文件）。
type SymbolIndex struct {
	parse ParseFunc

	mu      sync.Mutex
	cache   map[string][]string // path → 函数名（小写）
	errs    map[string]string   // path → 解析失败原因
	global  map[string]bool     // 全局模组函数并集
	ready   bool                // 全局模组预热是否完成
	warming map[string]bool     // 正在后台解析的路径（防重复排队）
	onReady func()              // 预热完成回调（server 用它补发一轮诊断）
}

// NewSymbolIndex 构造索引。
func NewSymbolIndex(parse ParseFunc) *SymbolIndex {
	return &SymbolIndex{
		parse:   parse,
		cache:   map[string][]string{},
		errs:    map[string]string{},
		global:  map[string]bool{},
		warming: map[string]bool{},
	}
}

// SetOnReady 设置"全局模组预热完成"的回调（必须在 StartGlobal 之前调用）。
func (s *SymbolIndex) SetOnReady(fn func()) {
	s.mu.Lock()
	s.onReady = fn
	s.mu.Unlock()
}

// Ready 报告全局模组是否已索引完。
//
// 未就绪时**不能**报 function-not-found：可见集不完整，报告就是误报。
func (s *SymbolIndex) Ready() bool {
	if s == nil {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.ready
}

// GlobalFunctions 返回全局模组的函数并集（未就绪时为空）。
func (s *SymbolIndex) GlobalFunctions() map[string]bool {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.ready {
		return nil
	}
	out := make(map[string]bool, len(s.global))
	for k := range s.global {
		out[k] = true
	}
	return out
}

// StartGlobal 后台索引全局模组的全部文件（幂等）。
//
// 983 个文件 × 8.7 ms ≈ 8.6 s（并行后约 2 s）。这段时间里
// function-not-found 不启用，就绪后回调触发一轮补发诊断。
func (s *SymbolIndex) StartGlobal(paths []string) {
	if s == nil || len(paths) == 0 {
		return
	}
	s.mu.Lock()
	if s.ready || s.warming["<global>"] {
		s.mu.Unlock()
		return
	}
	s.warming["<global>"] = true
	s.mu.Unlock()

	go func() {
		names := s.parseAll(paths, maxSyncDefault)
		s.mu.Lock()
		for _, n := range names {
			s.global[n] = true
		}
		s.ready = true
		delete(s.warming, "<global>")
		cb := s.onReady
		s.mu.Unlock()
		if cb != nil {
			cb()
		}
	}()
}

// Functions 返回这些文件声明的函数并集。
//
// complete=false 表示"还有文件没解析完"：此时可见集不完整，调用方
// **必须**放弃判定（宁可漏报不可误报）。未解析的部分已排进后台。
func (s *SymbolIndex) Functions(paths []string, maxSync int) (set map[string]bool, complete bool) {
	set = map[string]bool{}
	if s == nil {
		return set, true
	}
	if maxSync <= 0 {
		maxSync = maxSyncDefault
	}

	s.mu.Lock()
	var pending []string
	for _, p := range paths {
		if _, ok := s.cache[p]; ok {
			continue
		}
		if _, bad := s.errs[p]; bad {
			continue
		}
		if s.warming[p] {
			pending = append(pending, p) // 后台正在解析 → 必然不完整
			continue
		}
		pending = append(pending, p)
	}
	// 已在缓存里的先并进来，这样即使返回不完整，调用方也能记录已有信息。
	for _, p := range paths {
		for _, n := range s.cache[p] {
			set[n] = true
		}
	}
	s.mu.Unlock()

	if len(pending) == 0 {
		return set, true
	}

	if len(pending) > maxSync {
		// 太多：同步会卡住编辑器，全部扔后台。
		s.warmAsync(pending)
		return set, false
	}
	names := s.parseAll(pending, maxSync)
	for _, n := range names {
		set[n] = true
	}
	return set, true
}

// warmAsync 后台解析一批文件；完成后回调一次（触发补发诊断）。
func (s *SymbolIndex) warmAsync(paths []string) {
	s.mu.Lock()
	var todo []string
	for _, p := range paths {
		if s.warming[p] {
			continue
		}
		s.warming[p] = true
		todo = append(todo, p)
	}
	s.mu.Unlock()
	if len(todo) == 0 {
		return
	}
	go func() {
		_ = s.parseAll(todo, len(todo))
		s.mu.Lock()
		for _, p := range todo {
			delete(s.warming, p)
		}
		cb := s.onReady
		s.mu.Unlock()
		if cb != nil {
			cb()
		}
	}()
}

// parseAll 并行解析一批文件并填入缓存，返回合并后的函数名。
func (s *SymbolIndex) parseAll(paths []string, workers int) []string {
	if workers <= 0 {
		workers = 1
	}
	if workers > len(paths) {
		workers = len(paths)
	}
	type result struct {
		path  string
		names []string
		err   error
	}
	ch := make(chan string)
	out := make(chan result, len(paths))
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for p := range ch {
				names, err := s.parse(p)
				out <- result{path: p, names: names, err: err}
			}
		}()
	}
	go func() {
		for _, p := range paths {
			ch <- p
		}
		close(ch)
		wg.Wait()
		close(out)
	}()

	seen := map[string]bool{}
	var merged []string
	s.mu.Lock()
	for r := range out {
		if r.err != nil {
			// 读不到/解析失败：记下来，视为"这个文件不提供函数"。
			// 不标 incomplete —— 文件没了确实不提供函数，继续判定才不会漏。
			s.errs[r.path] = r.err.Error()
			s.cache[r.path] = nil
			continue
		}
		s.cache[r.path] = r.names
		for _, n := range r.names {
			if !seen[n] {
				seen[n] = true
				merged = append(merged, n)
			}
		}
	}
	s.mu.Unlock()
	return merged
}

// Errors 返回解析失败的文件（供 CLI/日志展示）。
func (s *SymbolIndex) Errors() map[string]string {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]string, len(s.errs))
	for k, v := range s.errs {
		out[k] = v
	}
	return out
}

// CachedFiles 返回已缓存的文件数（供日志/统计）。
func (s *SymbolIndex) CachedFiles() int {
	if s == nil {
		return 0
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.cache)
}

// SortedKeys 返回 map 的排序键（仅测试与日志用）。
func SortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ReadSource 是 ParseFunc 实现里常用的读文件助手（带大小保护）。
func ReadSource(path string) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > 4*1024*1024 {
		return nil, os.ErrInvalid
	}
	return os.ReadFile(path)
}

// NormalizeName 把名字规范成小写并去掉首尾空白（Genero 大小写不敏感）。
func NormalizeName(name string) string {
	return strings.ToLower(strings.TrimSpace(name))
}
