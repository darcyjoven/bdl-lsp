// Package server 实现 LSP 的传输、分发与生命周期。
//
// 设计：
//   - 使用 go.lsp.dev/jsonrpc2（stdio，Content-Length 帧）与 protocol 的类型定义；
//   - 不实现 protocol.Server 全套接口，而是按 method 自定义分发，便于控制错误码与未知方法；
//   - 日志只写 stderr，绝不污染 stdout（stdout 是 LSP 协议通道）。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/builtin"
	"github.com/darcyjoven/bdl-lsp/internal/config"
	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/form"
	"github.com/darcyjoven/bdl-lsp/internal/globals"
	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// Server 是语言服务器实例。
type Server struct {
	pool    *parser.Pool
	logger  *slog.Logger
	version string
	docs    *document.Store
	indexes *indexCache
	cfg     *config.Manager
	// globalsRes 解析 GLOBALS 引入（P3a）；initialize 之后非 nil。
	globalsRes *globals.Resolver
	// formRes 解析表单引用（P3b）；initialize 之后非 nil。
	formRes *form.Resolver
	// builtins 是内置函数/变量符号表（P7-C）；initialize 之后非 nil。
	// 支持覆盖文件热更新（Loader.MaybeReload）。
	builtins *builtin.Loader
	// links 是程序链接资料（P7-D）的运行时状态，始终非 nil（内部可能没有解析器）。
	links *linkState
	// ws 是工作区符号索引（P6）的运行时状态，始终非 nil。
	ws *wsIndexState
	// formQs 是字段校验的复用 query（P3b；惰性编译）。
	formQs *formQueriesAlias
	// rootPath 是工作区根的本地路径（initialize 时记录，供热更新重建解析器）。
	rootPath string
	// lastSearchPaths 是上次构建解析器用的 searchPaths（检测变化）。
	lastSearchPaths []string
	// lastPerRoots 是上次构建表单解析器用的 perRoots（检测变化）。
	lastPerRoots []string
	// globalsEntries 缓存每个文档最近一次的 GLOBALS 解析结果，
	// 供跳转/悬停查询（gMu 保护）。
	globalsMu      sync.RWMutex
	globalsEntries map[string][]globals.Entry
	// varQueries 是变量使用检查的复用 query（T5；惰性编译）。
	varQueries     *variableQueries
	varQueriesOnce sync.Once
	// linkSyncStop 停掉后台采集（P7-D-3；未启用时为 nil）。
	linkSyncStop func()
	// fnCallCollector 是函数调用收集的复用 collector（function-not-found；惰性创建）。
	fnCallOnce      sync.Once
	fnCallCollector *index.CallCollector
	// sourceDiags 缓存每文档的 T5（变量来源）诊断（debounceMu 保护）。
	// didChange 时清空并防抖重算：来源检查要全树 query（大文件 ~27ms），
	// 不能挂在每次按键上；编辑停顿 400ms 后恢复完整诊断。
	sourceDiags   map[string][]protocol.Diagnostic
	fieldDiags    map[string][]protocol.Diagnostic
	recheckTimers map[string]*time.Timer
	debounceMu    sync.Mutex

	mu        sync.Mutex
	conn      jsonrpc2.Conn
	shutdown  bool
	exitCode  int
	exitOnce  sync.Once
	exited    chan struct{}
	initParms *protocol.InitializeParams
}

// New 创建服务器。Serve 之前不会读写任何协议数据。
func New(pool *parser.Pool, logger *slog.Logger, version string) *Server {
	return &Server{
		pool:           pool,
		logger:         logger,
		version:        version,
		docs:           document.NewStore(),
		indexes:        newIndexCache(),
		ws:             &wsIndexState{},
		cfg:            config.NewManager(""),
		globalsEntries: map[string][]globals.Entry{},
		sourceDiags:    map[string][]protocol.Diagnostic{},
		fieldDiags:     map[string][]protocol.Diagnostic{},
		recheckTimers:  map[string]*time.Timer{},
		exited:         make(chan struct{}),
	}
}

// settings 返回当前配置快照。
func (s *Server) settings() config.Settings {
	if s.cfg == nil {
		return config.Default()
	}
	return s.cfg.Get()
}

// Serve 以 stdio 运行 LSP，直到客户端发送 exit 或连接关闭。
func (s *Server) Serve(ctx context.Context) error {
	return s.ServeStream(ctx, stdioStream{})
}

// ServeStream 在给定的读写流上运行 LSP。测试可传入管道，生产环境用 stdio。
func (s *Server) ServeStream(ctx context.Context, rwc io.ReadWriteCloser) error {
	stream := jsonrpc2.NewStream(rwc)
	conn := jsonrpc2.NewConn(stream)

	s.mu.Lock()
	s.conn = conn
	s.mu.Unlock()

	conn.Go(ctx, s.handle)

	// exit 与 ctx 取消都要能让服务返回：
	// 标准输入是 os.Stdin，Close 不会真正关闭它，因此不能只依赖 conn.Done()。
	select {
	case <-conn.Done():
	case <-s.exited:
	case <-ctx.Done():
		_ = conn.Close()
	}

	s.docs.CloseAll()
	s.indexes.dropAll()

	// exit 通知后等待读循环退出，避免 stdout 被提前关闭导致响应丢失。
	select {
	case <-conn.Done():
	case <-time.After(200 * time.Millisecond):
		_ = conn.Close()
	}

	// LSP 规范：未经 shutdown 就收到 exit，服务器应以非零码退出。
	s.mu.Lock()
	code := s.exitCode
	shutdown := s.shutdown
	s.mu.Unlock()
	if code != 0 {
		return exitError{code: code}
	}
	if shutdown {
		// 正常流程：shutdown 后 exit，读取端的 EOF 是预期结果，不算错误。
		return nil
	}
	return conn.Err()
}

// stdioStream 把 os.Stdin/os.Stdout 包装成 ReadWriteCloser。
// Close 不关闭标准流，避免影响进程退出。
type stdioStream struct{}

func (stdioStream) Read(p []byte) (int, error)  { return os.Stdin.Read(p) }
func (stdioStream) Write(p []byte) (int, error) { return os.Stdout.Write(p) }
func (stdioStream) Close() error                { return nil }

// handle 是 jsonrpc2 的主分发函数。
func (s *Server) handle(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	switch req.Method() {
	case protocol.MethodInitialize:
		return s.handleInitialize(ctx, reply, req)
	case protocol.MethodInitialized:
		s.logger.Info("客户端已完成初始化")
		// P6：工作区符号索引在后台建（不阻塞编辑；建好前功能降级为"只看已打开文档"）。
		s.startWorkspaceIndex(ctx)
		return nil
	case protocol.MethodShutdown:
		return s.handleShutdown(ctx, reply)
	case protocol.MethodExit:
		s.handleExit()
		return nil
	case protocol.MethodTextDocumentDidOpen:
		return s.handleDidOpen(ctx, req)
	case protocol.MethodTextDocumentDidChange:
		return s.handleDidChange(ctx, req)
	case protocol.MethodTextDocumentDidClose:
		return s.handleDidClose(ctx, req)
	case protocol.MethodTextDocumentDocumentSymbol:
		return s.handleDocumentSymbol(ctx, reply, req)
	case protocol.MethodWorkspaceSymbol:
		return s.handleWorkspaceSymbol(ctx, reply, req)
	case protocol.MethodTextDocumentHover:
		return s.handleHover(ctx, reply, req)
	case protocol.MethodWorkspaceDidChangeConfiguration:
		return s.handleDidChangeConfiguration(ctx, req)
	case protocol.MethodTextDocumentDefinition:
		return s.handleDefinition(ctx, reply, req)
	case protocol.MethodTextDocumentReferences:
		return s.handleReferences(ctx, reply, req)
	case protocol.MethodTextDocumentFormatting:
		return s.handleFormatting(ctx, reply, req)
	case protocol.MethodTextDocumentCompletion:
		return s.handleCompletion(ctx, reply, req)
	case protocol.MethodCompletionItemResolve:
		return s.handleCompletionResolve(ctx, reply, req)
	case protocol.MethodTextDocumentPrepareRename:
		return s.handlePrepareRename(ctx, reply, req)
	case protocol.MethodTextDocumentRename:
		return s.handleRename(ctx, reply, req)
	case protocol.MethodTextDocumentCodeAction:
		return s.handleCodeAction(ctx, reply, req)
	case protocol.MethodSemanticTokensFull:
		return s.handleSemanticTokensFull(ctx, reply, req)
	case MethodLinkTree:
		return s.handleLinkTree(ctx, reply, req)
	case MethodLinkReferences:
		return s.handleLinkReferences(ctx, reply, req)
	default:
		return s.handleUnknown(ctx, reply, req)
	}
}

// workspaceRootURI 选出工作区根 URI，按可靠性排序：
//
//  1. rootUri             —— 单根工作区的标准字段（LSP 3.0+）
//  2. workspaceFolders[0] —— 多根工作区；rootUri 常为 null
//  3. rootPath            —— 已废弃的字段，个别老客户端只发这个
//
// 三级都空时返回空串（无工作区，一切依赖绝对路径的功能降级但不崩溃）。
//
// 为什么需要回退（P7-A-2）：VSCode 在"打开单个文件"或配置了多根时
// 会让 rootUri 为 null，旧实现只读 RootURI，导致 rootPath 为空，
// 于是 globals.searchPaths / perRoots / .bdl-lsp/config.json 全都不生效。
func workspaceRootURI(params *protocol.InitializeParams) string {
	if u := string(params.RootURI); u != "" {
		return u
	}
	if len(params.WorkspaceFolders) > 0 {
		if u := string(params.WorkspaceFolders[0].URI); u != "" {
			return u
		}
	}
	if params.RootPath != "" {
		return params.RootPath
	}
	return ""
}

// handleUnknown 对未实现的方法：请求回 MethodNotFound，通知仅记录。
func (s *Server) handleUnknown(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	if _, isCall := req.(*jsonrpc2.Call); isCall {
		return reply(ctx, nil, jsonrpc2.ErrMethodNotFound)
	}
	s.logger.Debug("忽略未知通知", "method", req.Method())
	return nil
}

func (s *Server) handleInitialize(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.InitializeParams
	if raw := req.Params(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return reply(ctx, nil, fmt.Errorf("解析 initialize 参数: %w", err))
		}
	}

	s.mu.Lock()
	s.initParms = &params
	s.mu.Unlock()

	// 工作区根：rootUri 优先，缺失时回退 workspaceFolders / rootPath。
	//
	// 多根工作区、或"只打开单个文件"时 rootUri 会是 null，
	// 旧实现直接把它当空串 → rootPath 为空 → searchPaths、perRoots、
	// .bdl-lsp/config.json 全部失效（且不报错，极难排查）。
	rootURI := workspaceRootURI(&params)

	// 用工作区根定位工作区配置并校验
	s.cfg = config.NewManager(rootURI)
	if problems := s.cfg.Get().Validate(); len(problems) > 0 {
		for _, p := range problems {
			s.logger.Warn("配置问题", "detail", p)
		}
	}

	// P3a：GLOBALS 解析器（searchPaths 来自配置，热更新时按变化重建）
	s.rootPath = globals.PathOfURI(rootURI)
	s.lastSearchPaths = append([]string(nil), s.settings().Globals.SearchPaths...)
	s.newGlobalsResolver(s.rootPath)

	// P3b：表单解析器（perRoots 来自配置）
	s.lastPerRoots = append([]string(nil), s.settings().PerRoots...)
	s.newFormResolver(s.rootPath)

	// P7-C：内置符号表（内置数据 + 项目覆盖文件）
	s.newBuiltinLoader()

	// P7-D：链接资料（.link/links.jsonl）；没有资料时静默跳过。
	// 注意：不在这里 publishLinkStatus —— initialize 期间发通知会和
	// initialize 响应抢同一帧，客户端（和测试）都得处理乱序。
	// 改成第一次 didOpen 时发，那时用户已经真的在看文件了。
	s.newLinkResolver()
	// P7-D-3：采集（默认全关）。成功后会自己重载资料并补发诊断。
	s.startLinkSync(ctx)
	if n := len(s.settings().Globals.SearchPaths); n > 0 {
		s.logger.Info("globals searchPaths 生效", "count", n)
	}
	if params.InitializationOptions != nil {
		if raw, err := json.Marshal(params.InitializationOptions); err == nil {
			if err := s.cfg.Apply(raw); err != nil {
				s.logger.Warn("初始化选项解析失败", "error", err)
			}
		}
	}

	result := protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync:           protocol.TextDocumentSyncKindIncremental,
			HoverProvider:              true,
			DefinitionProvider:         true,
			ReferencesProvider:         true,
			DocumentSymbolProvider:     true,
			DocumentFormattingProvider: true,
			// 语义高亮只发用户自定义符号（关键字/字符串交给 TextMate），
			// 见 semtok.go 顶部说明。
			//
			// 用 map 而不是 protocol.SemanticTokensOptions：后者在这个版本里
			// 是空壳（只有 WorkDoneProgressOptions），没有 legend/full 字段。
			SemanticTokensProvider: map[string]any{
				"legend": map[string]any{
					"tokenTypes":     semanticTokenTypes(),
					"tokenModifiers": semanticTokenModifiers(),
				},
				"full": true,
			},
			CodeActionProvider: &protocol.CodeActionOptions{
				// 只发 quickfix：refactor 类动作（提取函数/内联）当前没有实现。
				CodeActionKinds: []protocol.CodeActionKind{protocol.QuickFix},
			},
			RenameProvider: &protocol.RenameOptions{
				// prepareProvider：先校验能否改名，不能改就不弹输入框。
				PrepareProvider: true,
			},
			CompletionProvider: &protocol.CompletionOptions{
				// `.` 之后给记录成员/表单字段；手工触发（Ctrl+Space）也支持。
				TriggerCharacters: []string{"."},
			},
		},
		ServerInfo: &protocol.ServerInfo{
			Name:    "bdl-lsp",
			Version: s.version,
		},
	}

	s.logger.Info("initialize", "client", clientName(params.ClientInfo), "rootUri", params.RootURI)
	return reply(ctx, result, nil)
}

func (s *Server) handleShutdown(ctx context.Context, reply jsonrpc2.Replier) error {
	s.mu.Lock()
	s.shutdown = true
	s.mu.Unlock()
	s.stopLinkSync()
	s.stopWorkspaceIndex()
	s.logger.Info("收到 shutdown")
	return reply(ctx, nil, nil)
}

func (s *Server) handleExit() {
	s.mu.Lock()
	shutdown := s.shutdown
	conn := s.conn
	if !shutdown {
		// LSP 规范：未经 shutdown 就收到 exit，应以非零码退出。
		s.exitCode = 1
		s.logger.Warn("未收到 shutdown 就收到 exit，将以退出码 1 结束")
	}
	s.mu.Unlock()

	s.exitOnce.Do(func() { close(s.exited) })
	_ = conn // 由 ServeStream 统一关闭，避免与读循环竞争
}

// exitError 表示服务器按 LSP 规范以特定退出码结束。
type exitError struct{ code int }

func (e exitError) Error() string {
	return fmt.Sprintf("LSP 以退出码 %d 结束（未先收到 shutdown）", e.code)
}

// ExitCode 返回该错误对应的进程退出码。
func (e exitError) ExitCode() int { return e.code }

func clientName(info *protocol.ClientInfo) string {
	if info == nil {
		return ""
	}
	return info.Name
}
