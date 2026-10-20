// format.go 处理 textDocument/formatting（P4）。
//
// 用户决定（2026-10-20）：
//   - **.per 不做任何格式化**——formatting 请求直接返回空（nil），
//     不注册范围格式化，也不做 format-on-save 的任何配合；
//   - .4gl：关键字大小写 + 缩进（tab/空格、个数均可配置）。
//
// 缩进解析顺序：配置显式指定 > 编辑器请求的 tabSize/insertSpaces
// （LSP FormattingOptions）> 默认 4 空格。配置里 indentStyle=auto /
// indentSize=0 表示"跟随编辑器"——VSCode/Zed 的 editor.tabSize 与
// insertSpaces 会随请求传进来，无需重复配置（无感默认）。
package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/config"
	"github.com/darcyjoven/bdl-lsp/internal/format"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// handleFormatting 处理"格式化文档"。
//
// 返回单个整文替换编辑（formatters 的常规做法，避免多编辑合并的
// 边界 bug）。语法树有 ERROR 时返回 nil（拒绝格式化，防雪上加霜）。
func (s *Server) handleFormatting(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DocumentFormattingParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, err)
	}

	doc := s.docs.Get(string(params.TextDocument.URI))
	if doc == nil {
		return reply(ctx, nil, nil)
	}

	// 用户决定：.per 不做任何格式化
	if doc.Lang != parser.LangBDL {
		return reply(ctx, nil, nil)
	}

	text, tree, _ := doc.Snapshot()
	if tree == nil {
		return reply(ctx, nil, nil)
	}

	opts := format.Options{
		KeywordCase: s.settings().Format.Keywords,
		IndentUnit:  resolveIndentUnit(s.settings().Format, params.Options),
	}

	out, ok := format.Format(tree, text, opts)
	if !ok {
		// 语法错误：拒绝格式化。日志说明（不弹诊断——语法诊断本身会报）。
		s.logger.Info("拒绝格式化：文件含语法错误", "uri", doc.URI)
		return reply(ctx, nil, nil)
	}
	if out == string(text) {
		return reply(ctx, []protocol.TextEdit{}, nil) // 无变化：空编辑列表
	}

	// 单个整文替换：range 覆盖全文（end 用极大值由客户端钳制，
	// 这是 formatter 的通行写法，避免行/列换算误差）。
	return reply(ctx, []protocol.TextEdit{{
		Range: protocol.Range{
			Start: protocol.Position{Line: 0, Character: 0},
			End:   protocol.Position{Line: ^uint32(0), Character: ^uint32(0)},
		},
		NewText: out,
	}}, nil)
}

// resolveIndentUnit 解析缩进单元字符串。
//
// 优先级：配置显式（tab/space + size）> 编辑器请求（insertSpaces/tabSize）
// > 默认 4 空格。
func resolveIndentUnit(cfg config.FormatSettings, req protocol.FormattingOptions) string {
	style := cfg.IndentStyle
	size := cfg.IndentSize

	if style == "" || style == "auto" {
		if req.InsertSpaces {
			style = "space"
		} else {
			style = "tab"
		}
	}
	if size <= 0 {
		if req.TabSize > 0 && req.TabSize <= 16 {
			size = int(req.TabSize)
		} else {
			size = 4
		}
	}

	if style == "tab" {
		return "\t"
	}
	return strings.Repeat(" ", size)
}
