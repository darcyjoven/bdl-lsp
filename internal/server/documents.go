package server

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// handleDidOpen 处理文档打开：解析并发布诊断。
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
	s.reparse(ctx, doc, nil)
	return nil
}

// rawChange 保留 range 字段的“是否存在”信息。
//
// go.lsp.dev/protocol v0.12.0 把 Range 定义为值类型，无法区分
// “省略 range（整文替换）”与“range 全零（在文件开头插入）”，
// 因此这里直接用 json.RawMessage 判断 key 是否存在。
type rawChange struct {
	Range       *rawRange `json:"range"`
	RangeLength uint32    `json:"rangeLength,omitempty"`
	Text        string    `json:"text"`
}

type rawRange struct {
	Start rawPosition `json:"start"`
	End   rawPosition `json:"end"`
}

type rawPosition struct {
	Line      uint32 `json:"line"`
	Character uint32 `json:"character"`
}

// handleDidChange 处理增量编辑：把 LSP 变更转为 document.Change 后重解析。
func (s *Server) handleDidChange(ctx context.Context, req jsonrpc2.Request) error {
	var params struct {
		TextDocument struct {
			URI     protocol.DocumentURI `json:"uri"`
			Version int32                `json:"version"`
		} `json:"textDocument"`
		ContentChanges []json.RawMessage `json:"contentChanges"`
	}
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
	if len(params.ContentChanges) == 0 {
		return nil
	}

	changes, err := decodeChanges(params.ContentChanges)
	if err != nil {
		s.logger.Warn("didChange 变更内容解析失败", "uri", uri, "error", err)
		return nil
	}

	oldTree, err := doc.ApplyChanges(params.TextDocument.Version, changes)
	if err != nil {
		// 编辑器与服务器状态不一致（少见）：回退到“整文替换最后一段”。
		// 用最后一段文本作为全文只对“客户端发全文”的场景正确，因此
		// 仅在能确认最后一段就是全文时回退，否则放弃本次更新并记日志。
		s.logger.Warn("增量编辑失败", "uri", uri, "error", err)
		return nil
	}

	s.reparse(ctx, doc, oldTree)
	return nil
}

// decodeChanges 把 JSON 变更数组解码为内部 Change 列表。
func decodeChanges(raws []json.RawMessage) ([]document.Change, error) {
	out := make([]document.Change, 0, len(raws))
	for i, raw := range raws {
		var rc rawChange
		if err := json.Unmarshal(raw, &rc); err != nil {
			return nil, err
		}
		if rc.Range == nil {
			// 省略 range = 整文替换
			out = append(out, document.Change{Full: true, Text: rc.Text})
			continue
		}
		if rc.Range.Start.Line > rc.Range.End.Line ||
			(rc.Range.Start.Line == rc.Range.End.Line && rc.Range.Start.Character > rc.Range.End.Character) {
			return nil, errors.New("变更区间起点晚于终点")
		}
		out = append(out, document.Change{
			StartLine: int(rc.Range.Start.Line),
			StartCol:  int(rc.Range.Start.Character),
			EndLine:   int(rc.Range.End.Line),
			EndCol:    int(rc.Range.End.Character),
			Text:      rc.Text,
		})
		_ = i
	}
	return out, nil
}

// handleDidClose 处理文档关闭：释放资源并清空诊断。
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
