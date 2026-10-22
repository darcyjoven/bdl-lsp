package server

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/diag"
	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
	"github.com/darcyjoven/bdl-lsp/link"
	"go.lsp.dev/protocol"
)

// linkState 保存链接资料（P7-D）的运行时状态。
//
// 为什么单独一个结构：链接资料有三态（关闭 / 没有资料 / 已加载），
// 外加"索引预热中"这个过渡态，散在 Server 字段里会让判断逻辑到处漏。
type linkState struct {
	mu       sync.RWMutex
	resolver *link.Resolver
	// dataDir 是资料目录（绝对路径），用于热重载与诊断。
	dataDir string
	// loaded 表示资料已成功加载（resolver 非 nil）。
	loaded bool
	// err 是加载失败原因（会作为 link-data-invalid 报出来）。
	err error
	// stamp 是 links.jsonl 的 mtime/size，用于发现"用户重新导入了"。
	stamp fileStampLite
	last  time.Time
	// statusPublished 记录"资料自身的问题"是否已发过一次（首个 didOpen 发）。
	statusPublished bool
}

type fileStampLite struct {
	mod  time.Time
	size int64
}

// linkReloadInterval 与内置符号表保持同一节奏（2s 节流）。
const linkReloadInterval = 2 * time.Second

// newLinkResolver 按当前配置加载链接资料。
//
// 三种结局都要能正常工作：
//   - 功能关闭（bdl.link.enable=false）→ resolver=nil，function-not-found 不启用；
//   - 没有资料（`.link/links.jsonl` 不存在）→ resolver=nil，不报错（新项目常态）；
//   - 资料坏了 → resolver=nil，发 link-data-invalid 让用户知道为什么诊断没出现。
func (s *Server) newLinkResolver() {
	if s.links == nil {
		s.links = &linkState{}
	}
	settings := s.settings()

	s.links.mu.Lock()
	defer s.links.mu.Unlock()

	if !settings.LinkEnabled() || s.rootPath == "" {
		s.links.resolver = nil
		s.links.loaded = false
		return
	}

	dataDir := settings.LinkDataDir()
	if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(s.rootPath, dataDir)
	}
	s.links.dataDir = dataDir

	idx, err := link.Load(dataDir)
	if err != nil {
		s.links.resolver = nil
		s.links.loaded = false
		s.links.err = err
		if err == link.ErrNoData {
			// 没有资料是常态（不是错误），只记 debug：新项目第一次打开
			// 不应该看到任何红字。
			s.logger.Debug("没有链接资料，function-not-found 不启用", "dir", dataDir)
			return
		}
		s.logger.Warn("链接资料加载失败", "dir", dataDir, "error", err)
		s.links.err = err
		return
	}

	layout := link.NewLayout(s.rootPath, settings.Link.ModuleRoots)
	res := link.NewResolver(idx, layout, settings.Link.GlobalModules, link.NewBDLParseFunc(s.pool))
	// 索引预热完成后补发一轮诊断：首轮因为"可见集不完整"被跳过，
	// 这里把结果补上（用户不该为了看诊断去动一下文件）。
	res.SetOnReady(func() { s.rediagnoseAll() })

	s.links.resolver = res
	s.links.loaded = true
	s.links.err = nil
	s.links.stamp = stampOfFile(filepath.Join(dataDir, link.LinksFileName))
	s.links.last = time.Now()

	st := idx.Stats()
	s.logger.Info("链接资料已加载",
		"dir", dataDir, "chains", st.Chains, "orphans", st.Orphans,
		"ambiguous", st.Ambiguous, "files", st.Files,
		"globalModules", res.GlobalModules())

	// 后台预热全局模组索引（sub/lib/qry 约 983 文件，并行约 2–3 s）。
	res.Start()
}

// linkResolver 返回已就绪的链接解析器；不可用时返回 nil。
func (s *Server) linkResolver() *link.Resolver {
	if s == nil || s.links == nil {
		return nil
	}
	s.links.mu.RLock()
	defer s.links.mu.RUnlock()
	if !s.settings().LinkEnabled() {
		return nil
	}
	return s.links.resolver
}

// visibleLinkFunctions 返回该文件"通过链接可见的函数名"（小写）。
//
// complete=false 表示可见集不完整（索引预热中、或链文件太多已排队后台）：
// 调用方必须放弃判定。宁可漏报不可误报——P7-B 用 3660 条误报换来的教训。
func (s *Server) visibleLinkFunctions(path string, hasMain bool) (set map[string]bool, complete bool) {
	res := s.linkResolver()
	if res == nil {
		return nil, false
	}
	return res.VisibleFunctions(path, hasMain)
}

// maybeReloadLinks 在 links.jsonl 变化后重载（2s 节流），返回是否重载。
//
// 场景：用户跑完 `tiptop link import` 就切回编辑器，期望马上生效，
// 而不是重启窗口。
func (s *Server) maybeReloadLinks() bool {
	if s == nil || s.links == nil || !s.settings().LinkEnabled() {
		return false
	}
	s.links.mu.Lock()
	dataDir := s.links.dataDir
	last := s.links.last
	old := s.links.stamp
	loaded := s.links.loaded
	s.links.mu.Unlock()

	if dataDir == "" {
		return false
	}
	if !last.IsZero() && time.Since(last) < linkReloadInterval {
		return false
	}
	s.links.mu.Lock()
	s.links.last = time.Now()
	s.links.mu.Unlock()

	// 没有资料时也要检查：用户可能刚导入了一份。
	path := filepath.Join(dataDir, link.LinksFileName)
	now := stampOfFile(path)
	if now == old && (loaded || now.mod.IsZero()) {
		return false
	}
	s.newLinkResolver()
	s.logger.Info("链接资料已变更，重新加载", "dir", dataDir)
	return true
}

// rediagnoseAll 重新诊断所有已打开文档（链接索引就绪/资料重载后调用）。
func (s *Server) rediagnoseAll() {
	ctx := context.Background()
	for _, doc := range s.docs.All() {
		s.reparse(ctx, doc, nil)
	}
}

// stampOfFile 取文件 mtime+size；不存在返回零值。
func stampOfFile(path string) fileStampLite {
	st, err := os.Stat(path)
	if err != nil {
		return fileStampLite{}
	}
	return fileStampLite{mod: st.ModTime(), size: st.Size()}
}

// maybePublishLinkStatus 在首个 didOpen 时发布一次（幂等）。
func (s *Server) maybePublishLinkStatus(ctx context.Context) {
	if s == nil || s.links == nil {
		return
	}
	s.links.mu.Lock()
	if s.links.statusPublished {
		s.links.mu.Unlock()
		return
	}
	s.links.statusPublished = true
	s.links.mu.Unlock()
	s.publishLinkStatus(ctx)
}

// publishLinkStatus 发布链接资料自身的诊断（格式坏了 / 过期了）。
//
// 落在资料文件上（links.meta.json / links.jsonl）而不是随便一份源码：
// 用户点开就知道该重新导入了，而不是困惑"为什么这个文件有提示"。
func (s *Server) publishLinkStatus(ctx context.Context) {
	if s == nil || s.links == nil || s.rootPath == "" {
		return
	}
	settings := s.settings()
	if !settings.LinkEnabled() {
		return
	}
	s.links.mu.RLock()
	dataDir := s.links.dataDir
	loadErr := s.links.err
	idx := (*link.Index)(nil)
	if s.links.resolver != nil {
		idx = s.links.resolver.Index()
	}
	s.links.mu.RUnlock()
	if dataDir == "" {
		return
	}

	// 1) 资料坏了：报在 links.jsonl 上（那个文件存在且就是坏的那份）。
	if loadErr != nil && loadErr != link.ErrNoData {
		uri := fileURI(filepath.Join(dataDir, link.LinksFileName))
		s.publishExtra(ctx, uri, []protocol.Diagnostic{{
			Range:    protocol.Range{},
			Severity: protocol.DiagnosticSeverityWarning,
			Source:   "bdl-lsp",
			Message:  fmt.Sprintf("链接资料无法加载：%v；function-not-found 已停用。请重新运行 `tiptop link import`", loadErr),
			Code:     string(diag.CodeLinkDataInvalid),
		}})
		return
	}

	// 2) 过期：报在 links.meta.json 上（能看出是什么时候导入的）。
	if idx == nil || idx.Meta() == nil {
		return
	}
	maxAge := settings.Link.MaxAgeDays
	if maxAge <= 0 {
		maxAge = 7
	}
	meta := idx.Meta()
	age := meta.Age()
	if meta.GeneratedTime().IsZero() || age < time.Duration(maxAge)*24*time.Hour {
		return
	}
	severity, enabled := diag.SeverityFor(diag.CodeLinkDataStale, settings)
	if !enabled {
		return
	}
	uri := fileURI(filepath.Join(dataDir, link.MetaFileName))
	s.publishExtra(ctx, uri, []protocol.Diagnostic{{
		Range:    protocol.Range{},
		Severity: severity,
		Source:   "bdl-lsp",
		Message: fmt.Sprintf("链接资料已 %d 天未更新（生成于 %s）。过期资料会让 function-not-found 误报，建议重新运行 `tiptop link import`",
			int(age.Hours()/24), meta.GeneratedAt),
		Code: string(diag.CodeLinkDataStale),
	}})
}

// publishExtra 发布"不属于任何已打开文档"的诊断。
//
// 这类诊断指向资料文件（.link/links.jsonl），它们通常没被打开，版本号用 0：
// LSP 的 publishDiagnostics 允许对任意 URI 发布，VSCode 会收进 Problems 面板。
func (s *Server) publishExtra(ctx context.Context, uri string, diags []protocol.Diagnostic) {
	version := int32(0)
	if doc := s.docs.Get(uri); doc != nil {
		version = doc.Version
	}
	params := &protocol.PublishDiagnosticsParams{
		URI:         protocol.DocumentURI(uri),
		Diagnostics: diags,
	}
	if version > 0 {
		params.Version = uint32(version)
	}
	if err := s.notify(ctx, protocol.MethodTextDocumentPublishDiagnostics, params); err != nil {
		s.logger.Debug("发布链接资料诊断失败", "uri", uri, "error", err)
	}
}

// filePathOf 把文档 URI 转成本地路径（失败返回空串）。
//
// 走 internal/fileuri：VSCode 在 Windows 上把 `C:` 编码成 `%3A`，
// 手写实现会在那里出错（P7-A 踩过）。
func filePathOf(uri string) string {
	p, err := fileuri.ToPath(uri)
	if err != nil {
		return ""
	}
	return p
}

// fileURI 把绝对路径转成 file:// URI。
func fileURI(path string) string {
	return "file://" + filepath.ToSlash(path)
}
