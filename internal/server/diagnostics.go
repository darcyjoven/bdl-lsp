package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/config"
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
//
// 保护策略（Block 5）：
//   - 文件超过 limits 时不做语法诊断，只发一条提示，避免拖垮编辑器；
//   - 解析超时后放弃本次诊断但**保留**上次诊断，避免编辑时闪烁。
func (s *Server) reparse(ctx context.Context, doc *document.Document, oldTree *ts.Tree) {
	text, _, version := doc.Snapshot()
	settings := s.settings()

	if reason := tooLargeReason(text, settings.Limits); reason != "" {
		s.logger.Debug("跳过语法诊断", "uri", doc.URI, "reason", reason)
		s.publishNotice(ctx, doc.URI, version, reason)
		return
	}

	opts := parser.ParseOptions{
		Timeout: time.Duration(settings.Limits.ParseTimeoutMS) * time.Millisecond,
	}
	tree, err := s.pool.ParseWith(ctx, doc.Lang, text, oldTree, opts)
	if err != nil {
		// doc 仍持有 oldTree，保留它供下次复用，不要在此关闭（否则双重释放）。
		if errors.Is(err, parser.ErrTimeout) && ctx.Err() == nil {
			s.logger.Warn("解析超时，保留上次诊断", "uri", doc.URI, "timeout", opts.Timeout)
			return
		}
		s.logger.Error("解析失败", "uri", doc.URI, "error", err)
		s.publish(ctx, doc.URI, version, nil)
		return
	}

	issues := parser.Collect(tree, parser.CollectOptions{
		MaxIssues:     settings.Limits.MaxIssues,
		MergeAdjacent: true,
	})
	// SetTree 负责关闭 doc 中原有的树（即 oldTree），调用方不要再单独关闭。
	doc.SetTree(tree)

	if ctx.Err() != nil {
		return
	}
	s.publish(ctx, doc.URI, version, issues)
}

// tooLargeReason 返回跳过大文件诊断的原因；未超限时返回空串。
func tooLargeReason(text []byte, limits config.Limits) string {
	if limits.MaxFileBytes > 0 && len(text) > limits.MaxFileBytes {
		return fmt.Sprintf("文件 %.1f MB 超过解析上限 %.1f MB，已跳过语法检查",
			float64(len(text))/(1024*1024), float64(limits.MaxFileBytes)/(1024*1024))
	}
	if limits.MaxFileLines > 0 {
		lines := 1
		for _, b := range text {
			if b == '\n' {
				lines++
			}
		}
		if lines > limits.MaxFileLines {
			return fmt.Sprintf("文件 %d 行超过解析上限 %d 行，已跳过语法检查", lines, limits.MaxFileLines)
		}
	}
	return ""
}

// publish 把 parser.Issue 转换为 LSP 诊断并发送。
func (s *Server) publish(ctx context.Context, uri string, version int32, issues []parser.Issue) {
	var idx *textpos.Index
	if doc := s.docs.Get(uri); doc != nil {
		idx = doc.Index()
	}

	settings := s.settings()
	diags := make([]protocol.Diagnostic, 0, len(issues))
	for _, iss := range issues {
		severity, ok := severityFor(iss, settings)
		if !ok {
			continue // 该级别被配置为 off
		}
		diags = append(diags, protocol.Diagnostic{
			Range:    toProtocolRange(idx, iss),
			Severity: severity,
			Source:   "bdl-lsp",
			Message:  iss.Message,
			Code:     diagnosticCode(iss),
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

// publishNotice 发送一条提示性诊断（大文件跳过等）。
func (s *Server) publishNotice(ctx context.Context, uri string, version int32, message string) {
	params := &protocol.PublishDiagnosticsParams{
		URI: protocol.DocumentURI(uri),
		Diagnostics: []protocol.Diagnostic{{
			Range:    protocol.Range{},
			Severity: protocol.DiagnosticSeverityInformation,
			Source:   "bdl-lsp",
			Message:  message,
			Code:     "file-too-large",
		}},
	}
	if version > 0 {
		params.Version = uint32(version)
	}
	if err := s.notify(ctx, protocol.MethodTextDocumentPublishDiagnostics, params); err != nil {
		s.logger.Warn("发送提示失败", "uri", uri, "error", err)
	}
}

// severityFor 依据配置决定诊断级别；ok=false 表示该诊断被关闭。
//
// MISSING 节点通常表示“缺少某个必填结构”，与语法错误分开配置。
func severityFor(iss parser.Issue, settings config.Settings) (protocol.DiagnosticSeverity, bool) {
	level := settings.SyntaxDiagnostics
	if iss.Missing {
		level = settings.MissingDiagnostics
	}
	return toSeverity(level)
}

// toSeverity 把配置里的级别字符串转换为 LSP 枚举。
func toSeverity(level config.DiagnosticLevel) (protocol.DiagnosticSeverity, bool) {
	switch level {
	case config.LevelError:
		return protocol.DiagnosticSeverityError, true
	case config.LevelWarning:
		return protocol.DiagnosticSeverityWarning, true
	case config.LevelInfo:
		return protocol.DiagnosticSeverityInformation, true
	case config.LevelHint:
		return protocol.DiagnosticSeverityHint, true
	case config.LevelOff:
		return 0, false
	default:
		return protocol.DiagnosticSeverityError, true
	}
}

// diagnosticCode 返回诊断的机器可读代码，便于用户过滤。
func diagnosticCode(iss parser.Issue) string {
	if iss.Missing {
		return "missing-" + iss.Kind
	}
	return "syntax-error"
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
	if ix == nil {
		// 文档已关闭：退化为单点，避免发送越界位置。
		return protocol.Range{}
	}
	startLine, startChar := ix.Position(int(iss.StartByte))
	endLine, endChar := ix.Position(int(iss.EndByte))
	return protocol.Range{
		Start: protocol.Position{Line: uint32(startLine), Character: uint32(startChar)},
		End:   protocol.Position{Line: uint32(endLine), Character: uint32(endChar)},
	}
}
