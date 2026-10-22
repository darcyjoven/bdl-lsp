package server

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"time"

	linkpkg "github.com/darcyjoven/bdl-lsp/internal/link"
	"github.com/darcyjoven/bdl-lsp/internal/wsindex"
	"go.lsp.dev/protocol"
)

// wsIndexState 是工作区符号索引的运行时状态（P6）。
//
// 索引在后台建：initialized 之后启动 goroutine，编辑不受影响。
// 建好之前 `workspace/symbol` 等继续用"已打开文档"的旧路径——
// 功能永远是"部分可用"，不会因为索引没建完而完全没结果。
type wsIndexState struct {
	mu       sync.RWMutex
	idx      *wsindex.Index
	building bool
	started  bool
	cancel   context.CancelFunc
	done     chan struct{}
	// stats 是最近一次构建的统计（日志/测试用）。
	stats wsindex.Stats
	err   error
}

// wsState 返回索引状态（New 里创建，始终非 nil）。
func (s *Server) wsState() *wsIndexState {
	return s.ws
}

// workspaceIndex 返回当前可用快照（可能为 nil）。
func (s *Server) workspaceIndex() *wsindex.Index {
	st := s.wsState()
	if st == nil {
		return nil
	}
	st.mu.RLock()
	defer st.mu.RUnlock()
	return st.idx
}

// startWorkspaceIndex 在 initialized 之后启动后台索引（幂等）。
//
// 为什么不放在 initialize 里：initialize 期间还不能发通知（会和响应抢帧），
// 而且那时用户可能只是打开了一个文件。
func (s *Server) startWorkspaceIndex(ctx context.Context) {
	if s.rootPath == "" {
		return
	}
	if !s.settings().IndexEnabled() {
		s.logger.Info("工作区索引已关闭（bdl.index.enable=false）")
		return
	}
	st := s.wsState()
	if st == nil {
		return
	}
	st.mu.Lock()
	if st.started {
		st.mu.Unlock()
		return
	}
	st.started = true
	st.building = true
	st.done = make(chan struct{})
	bctx, cancel := context.WithCancel(ctx)
	st.cancel = cancel
	st.mu.Unlock()

	go s.buildWorkspaceIndex(bctx, st)
}

// stopWorkspaceIndex 停止后台索引（shutdown 时调用）。
func (s *Server) stopWorkspaceIndex() {
	st := s.wsState()
	if st == nil {
		return
	}
	st.mu.Lock()
	cancel := st.cancel
	done := st.done
	st.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if done != nil {
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			// 索引 goroutine 卡在文件 IO 上：不必等它，进程退出即可。
		}
	}
}

// buildWorkspaceIndex 建索引：先加载旧快照（立即可用），再增量刷新。
func (s *Server) buildWorkspaceIndex(ctx context.Context, st *wsIndexState) {
	defer func() {
		st.mu.Lock()
		st.building = false
		st.mu.Unlock()
		close(st.done)
	}()

	grammar := fmt.Sprintf("bdl-lsp %s / wsindex %d", s.version, wsindex.Version)
	cachePath := wsindex.CachePath(s.rootPath)

	// 旧快照先挂上：冷启动时跳转/符号立刻有结果，随后被新结果替换。
	var prev *wsindex.Index
	if cached, err := wsindex.Load(cachePath); err == nil && cached.Root == s.rootPath && cached.Grammar == grammar {
		prev = cached
		st.mu.Lock()
		st.idx = cached
		st.mu.Unlock()
		s.logger.Info("工作区索引：已载入快照", "files", cached.FileCount())
	}

	settings := s.settings()
	calls := s.callCollector()
	started := time.Now()
	ix, stats, err := wsindex.Build(ctx, wsindex.Bindings{Pool: s.pool, Calls: calls}, wsindex.Options{
		Root:        s.rootPath,
		ModuleRoots: settings.Link.ModuleRoots,
		Grammar:     grammar,
		Exclude:     settings.Ignore.Files,
		Progress: func(done, total int) {
			if done == total || done%(total/10+1) == 0 {
				s.logger.Debug("工作区索引进度", "done", done, "total", total)
			}
		},
		Logf: s.logger.Warn,
	}, prev)
	stats.Elapsed = time.Since(started).Milliseconds()
	if err != nil {
		st.mu.Lock()
		st.err = err
		st.mu.Unlock()
		s.logger.Warn("工作区索引未完成", "error", err, "parsed", stats.Parsed, "reused", stats.Reused)
		return
	}

	st.mu.Lock()
	st.idx = ix
	st.stats = stats
	st.err = nil
	st.mu.Unlock()

	if ix.FileCount() == 0 {
		// 工作区里没有 BDL 代码（不是 tiptop 布局）：不用每次都记一条 info。
		s.logger.Debug("工作区索引：没有可索引的文件", "root", s.rootPath)
		st.mu.Lock()
		st.idx = ix
		st.stats = stats
		st.mu.Unlock()
		return
	}
	s.logger.Info("工作区索引就绪",
		"files", ix.FileCount(), "reused", stats.Reused, "parsed", stats.Parsed,
		"failed", stats.Failed, "ms", stats.Elapsed)

	if err := wsindex.Save(cachePath, ix); err != nil {
		// 存不了只是下次要重扫，不影响本次使用。
		s.logger.Warn("工作区索引快照保存失败", "path", cachePath, "error", err)
	}
}

// ---- 查询：workspace/symbol / 跳转 / 引用 ----

// wsSymbols 把工作区索引的条目转成 LSP 的 SymbolInformation。
func wsSymbols(ix *wsindex.Index, query string, limit int) []protocol.SymbolInformation {
	decls := ix.Search(query, limit)
	out := make([]protocol.SymbolInformation, 0, len(decls))
	for _, d := range decls {
		out = append(out, protocol.SymbolInformation{
			Name: d.Name,
			Kind: wsSymbolKind(d.Kind),
			Location: protocol.Location{
				URI:   pathURI(d.File),
				Range: wsRange(d.Line, d.Col, len(d.Name)),
			},
			ContainerName: d.Module,
		})
	}
	return out
}

// wsSymbolKind 把 index.Kind 映射到 LSP 的符号种类。
func wsSymbolKind(kind string) protocol.SymbolKind {
	switch kind {
	case "main":
		return protocol.SymbolKindModule
	case "function":
		return protocol.SymbolKindFunction
	case "report":
		return protocol.SymbolKindFunction
	case "define":
		return protocol.SymbolKindVariable
	case "constant":
		return protocol.SymbolKindConstant
	case "type":
		return protocol.SymbolKindStruct
	case "record":
		return protocol.SymbolKindStruct
	case "prepare":
		return protocol.SymbolKindVariable
	case "cursor":
		return protocol.SymbolKindVariable
	}
	return protocol.SymbolKindVariable
}

func wsRange(line, col, length int) protocol.Range {
	return protocol.Range{
		Start: protocol.Position{Line: uint32(line), Character: uint32(col)},
		End:   protocol.Position{Line: uint32(line), Character: uint32(col + length)},
	}
}

// pathURI 把本地路径转成 file:// URI（用与客户端一致的百分号编码）。
func pathURI(path string) protocol.DocumentURI {
	u := filepath.ToSlash(path)
	if !strings.HasPrefix(u, "/") {
		u = "/" + u
	}
	return protocol.DocumentURI("file://" + percentEncodePath(u))
}

// moduleOfPath 取 4gl 文件所属模组（`…/<root>/<module>/4gl/x.4gl`）。
func moduleOfPath(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	for i := len(parts) - 1; i > 1; i-- {
		if parts[i] == "4gl" || parts[i] == "per" {
			return strings.ToLower(parts[i-1])
		}
	}
	return ""
}

// reachableModules 返回当前文件能跨文件看到哪些模组。
//
// 规则与 P7-D 链接资料一致（三条铁律的第 2 条）：本模组 + 全局模组
// （sub/lib/qry）。不依赖链接资料——索引是"代码在哪"的问题，
// 链接是"能不能调用"的问题；这里只回答前者，宁可漏报不误报。
func (s *Server) reachableModules(curModule string) map[string]bool {
	mods := map[string]bool{}
	if curModule != "" {
		mods[curModule] = true
	}
	globals := s.settings().Link.GlobalModules
	if len(globals) == 0 {
		globals = linkpkg.DefaultGlobalModules()
	}
	for _, m := range globals {
		mods[strings.ToLower(m)] = true
	}
	return mods
}

// wsDefinition 用工作区索引兜底跳转：找不到本地定义时，看别的文件有没有。
//
// 只在"可达模组"里找，且要求**唯一命中**——多个同名函数时不敢猜
// （Genero 的可见性由链接决定，猜错比跳不过去更糟）。
func (s *Server) wsDefinition(curPath, word string) []protocol.Location {
	ix := s.workspaceIndex()
	if ix == nil || word == "" {
		return nil
	}
	reachable := s.reachableModules(moduleOfPath(curPath))
	var hits []wsindex.Decl
	for _, d := range ix.Lookup(word) {
		if !reachable[d.Module] {
			continue
		}
		if sameFile(d.File, curPath) {
			continue // 本文件由 per-document 索引负责
		}
		hits = append(hits, d)
	}
	if len(hits) == 0 {
		return nil
	}
	// 多个命中：优先"声明所在模组就是本模组"，仍不唯一则只认函数类唯一写法。
	prefer := make([]wsindex.Decl, 0, len(hits))
	cur := moduleOfPath(curPath)
	for _, d := range hits {
		if d.Module == cur {
			prefer = append(prefer, d)
		}
	}
	if len(prefer) > 0 {
		hits = prefer
	}
	if len(hits) > 1 {
		return nil
	}
	d := hits[0]
	return []protocol.Location{{
		URI:   pathURI(d.File),
		Range: wsRange(d.Line, d.Col, len(d.Name)),
	}}
}

// wsReferences 用工作区索引补齐跨文件引用（同模组 + 全局模组）。
//
// 只对**函数/报表**这类可按名字调用的符号有意义：变量/记录的可见性由
// GLOBALS 与记录结构决定，跨文件按名字匹配会大面积误报。
func (s *Server) wsReferences(curPath, word string) []protocol.Location {
	ix := s.workspaceIndex()
	if ix == nil || word == "" {
		return nil
	}
	reachable := s.reachableModules(moduleOfPath(curPath))
	var out []protocol.Location
	for _, c := range ix.CallsOf(word) {
		if sameFile(c.File, curPath) {
			continue // 本文件已由 findOccurrences 覆盖
		}
		if !reachable[moduleOfPath(c.File)] {
			continue
		}
		out = append(out, protocol.Location{
			URI:   pathURI(c.File),
			Range: wsRange(c.Line, c.Col, c.Len),
		})
	}
	return out
}

// sameFile 比较两个路径是否指向同一个文件（大小写不敏感，Windows 也要对）。
func sameFile(a, b string) bool {
	return strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// percentEncodePath 对路径做最小的百分号编码（保留 '/'）。
//
// 与 internal/fileuri 的编码保持一致；这里只处理 URI 里必须转义的字符
// （空格、#、?、%），避免把中文路径编成乱码。
func percentEncodePath(p string) string {
	var b strings.Builder
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch c {
		case ' ', '#', '?', '%':
			fmt.Fprintf(&b, "%%%02X", c)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}
