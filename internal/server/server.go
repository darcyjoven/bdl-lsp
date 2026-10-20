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

	"github.com/darcyjoven/bdl-lsp/internal/document"
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

	mu        sync.Mutex
	conn      jsonrpc2.Conn
	shutdown  bool
	exitOnce  sync.Once
	exited    chan struct{}
	initParms *protocol.InitializeParams
}

// New 创建服务器。Serve 之前不会读写任何协议数据。
func New(pool *parser.Pool, logger *slog.Logger, version string) *Server {
	return &Server{
		pool:    pool,
		logger:  logger,
		version: version,
		docs:    document.NewStore(),
		exited:  make(chan struct{}),
	}
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

	select {
	case <-conn.Done():
	case <-ctx.Done():
		_ = conn.Close()
	}

	s.docs.CloseAll()
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
	default:
		return s.handleUnknown(ctx, reply, req)
	}
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

	result := protocol.InitializeResult{
		Capabilities: protocol.ServerCapabilities{
			TextDocumentSync:       protocol.TextDocumentSyncKindIncremental,
			HoverProvider:          true,
			DefinitionProvider:     true,
			ReferencesProvider:     true,
			DocumentSymbolProvider: true,
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
	s.logger.Info("收到 shutdown")
	return reply(ctx, nil, nil)
}

func (s *Server) handleExit() {
	s.mu.Lock()
	shutdown := s.shutdown
	conn := s.conn
	s.mu.Unlock()

	if !shutdown {
		s.logger.Warn("未收到 shutdown 就收到 exit，按 LSP 协议应以失败码退出")
	}
	s.exitOnce.Do(func() { close(s.exited) })
	if conn != nil {
		_ = conn.Close()
	}
}

func (s *Server) handleDidOpen(ctx context.Context, req jsonrpc2.Request) error {
	var params protocol.DidOpenTextDocumentParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		s.logger.Warn("didOpen 参数解析失败", "error", err)
		return nil
	}

	uri := string(params.TextDocument.URI)
	lang, ok := parser.LangForPath(uri)
	if !ok {
		return nil // 只处理 bdl / per，其他文件静默忽略
	}

	doc := s.docs.Open(uri, lang, params.TextDocument.Version, params.TextDocument.Text)
	s.logger.Debug("didOpen", "uri", uri, "lang", lang, "version", doc.Version)
	s.reparse(ctx, doc)
	return nil
}

func (s *Server) handleDidChange(ctx context.Context, req jsonrpc2.Request) error {
	var params protocol.DidChangeTextDocumentParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		s.logger.Warn("didChange 参数解析失败", "error", err)
		return nil
	}

	uri := string(params.TextDocument.URI)
	doc := s.docs.Get(uri)
	if doc == nil {
		s.logger.Debug("didChange 找不到文档，忽略", "uri", uri)
		return nil
	}

	// Block 1：整文替换（取最后一次 change 的 text 作为最新全文）。
	// 增量编辑与增量重解析在 Block 2 实现。
	if n := len(params.ContentChanges); n > 0 {
		doc.Update(params.TextDocument.Version, params.ContentChanges[n-1].Text)
	}
	s.reparse(ctx, doc)
	return nil
}

func (s *Server) handleDidClose(ctx context.Context, req jsonrpc2.Request) error {
	var params protocol.DidCloseTextDocumentParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		s.logger.Warn("didClose 参数解析失败", "error", err)
		return nil
	}

	uri := string(params.TextDocument.URI)
	s.docs.Close(uri)
	s.logger.Debug("didClose", "uri", uri)
	// 关闭时清空诊断，避免编辑器残留。
	s.publish(ctx, uri, 0, nil)
	return nil
}

func clientName(info *protocol.ClientInfo) string {
	if info == nil {
		return ""
	}
	return info.Name
}
