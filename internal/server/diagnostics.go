package server

import (
	"context"
	"errors"

	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	ts "github.com/tree-sitter/go-tree-sitter"
	"go.lsp.dev/protocol"
)

// errConnNotReady 表示连接尚未建立（初始化早期或已关闭）。
var errConnNotReady = errors.New("LSP 连接尚未建立")

// reparse 重新解析文档并把语法问题发布为诊断。
//
// oldTree 非 nil 时走增量解析（调用方已对 oldTree 执行 Tree.Edit）。
func (s *Server) reparse(ctx context.Context, doc *document.Document, oldTree *ts.Tree) {
	text, _, version := doc.Snapshot()

	tree, err := s.pool.Parse(doc.Lang, text, oldTree)
	if err != nil {
		// 解析失败：doc 仍持有 oldTree，保留它供下次复用，不要在此关闭。
		s.logger.Error("解析失败", "uri", doc.URI, "error", err)
		s.publish(ctx, doc.URI, version, nil)
		return
	}

	issues := parser.CollectIssues(tree, 200)
	// SetTree 负责关闭 doc 中原有的树（即 oldTree），调用方不要再单独关闭，
	// 否则会双重释放导致 SIGSEGV。
	doc.SetTree(tree)

	if ctx.Err() != nil {
		return
	}
	s.publish(ctx, doc.URI, version, issues)
}

// publish 把 parser.Issue 转换为 LSP 诊断并发送。
func (s *Server) publish(ctx context.Context, uri string, version int32, issues []parser.Issue) {
	var idx *textpos.Index
	if doc := s.docs.Get(uri); doc != nil {
		idx = doc.Index()
	}

	diags := make([]protocol.Diagnostic, 0, len(issues))
	for _, iss := range issues {
		diags = append(diags, protocol.Diagnostic{
			Range:    toProtocolRange(idx, iss),
			Severity: protocol.DiagnosticSeverityError,
			Source:   "bdl-lsp",
			Message:  iss.Message,
		})
	}

	params := &protocol.PublishDiagnosticsParams{
		URI:         protocol.DocumentURI(uri),
		Diagnostics: diags,
	}
	if version > 0 {
		params.Version = uint32(version)
	}

	if err := s.notify(ctx, protocol.MethodTextDocumentPublishDiagnostics, params); err != nil {
		s.logger.Warn("发送诊断失败", "uri", uri, "error", err)
	}
}

// notify 向客户端发送通知。
func (s *Server) notify(ctx context.Context, method string, params any) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return errConnNotReady
	}
	return conn.Notify(ctx, method, params)
}

// toProtocolRange 把字节偏移转换为 LSP 的 UTF-16 行列。
func toProtocolRange(ix *textpos.Index, iss parser.Issue) protocol.Range {
	var startLine, startChar, endLine, endChar int
	if ix == nil {
		// 文档已关闭：退化为单点，避免发送越界位置。
		return protocol.Range{}
	}
	startLine, startChar = ix.Position(int(iss.StartByte))
	endLine, endChar = ix.Position(int(iss.EndByte))
	return protocol.Range{
		Start: protocol.Position{Line: uint32(startLine), Character: uint32(startChar)},
		End:   protocol.Position{Line: uint32(endLine), Character: uint32(endChar)},
	}
}
