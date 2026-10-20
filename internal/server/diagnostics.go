package server

import (
	"context"
	"errors"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"go.lsp.dev/protocol"
)

// errConnNotReady 表示连接尚未建立（初始化早期或已关闭）。
var errConnNotReady = errors.New("LSP 连接尚未建立")

// reparse 重新解析文档并把语法问题发布为诊断。
func (s *Server) reparse(ctx context.Context, doc *document.Document) {
	text, _, version := doc.Snapshot()

	tree, err := s.pool.Parse(doc.Lang, text, nil)
	if err != nil {
		s.logger.Error("解析失败", "uri", doc.URI, "error", err)
		s.publish(ctx, doc.URI, version, nil)
		return
	}
	issues := parser.CollectIssues(tree, 200)
	doc.SetTree(tree)

	if ctx.Err() != nil {
		return
	}
	s.publish(ctx, doc.URI, version, issues)
}

// publish 把 parser.Issue 转换为 LSP 诊断并发送。
func (s *Server) publish(ctx context.Context, uri string, version int32, issues []parser.Issue) {
	var text []byte
	if doc := s.docs.Get(uri); doc != nil {
		text, _, _ = doc.Snapshot()
	}

	diags := make([]protocol.Diagnostic, 0, len(issues))
	for _, iss := range issues {
		diags = append(diags, protocol.Diagnostic{
			Range:    toProtocolRange(text, iss),
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
func toProtocolRange(text []byte, iss parser.Issue) protocol.Range {
	startLine, startChar := byteToUTF16Position(text, int(iss.StartByte))
	endLine, endChar := byteToUTF16Position(text, int(iss.EndByte))
	return protocol.Range{
		Start: protocol.Position{Line: uint32(startLine), Character: uint32(startChar)},
		End:   protocol.Position{Line: uint32(endLine), Character: uint32(endChar)},
	}
}

// byteToUTF16Position 把绝对字节偏移换算为 0 基行号与 UTF-16 列。
func byteToUTF16Position(text []byte, offset int) (line, char int) {
	if offset < 0 {
		return 0, 0
	}
	if offset > len(text) {
		offset = len(text)
	}

	lineStart := 0
	for i := 0; i < offset; i++ {
		if text[i] == '\n' {
			line++
			lineStart = i + 1
		}
	}

	for i := lineStart; i < offset; {
		r, size := utf8.DecodeRune(text[i:])
		if r == utf8.RuneError && size == 1 {
			char++ // 非法字节按单码元处理，保持位置单调
			i++
			continue
		}
		char += utf16Len(r)
		i += size
	}
	return line, char
}

// utf16Len 返回一个 rune 的 UTF-16 码元数。
func utf16Len(r rune) int {
	if r < 0x10000 {
		return 1
	}
	return len(utf16.Encode([]rune{r}))
}
