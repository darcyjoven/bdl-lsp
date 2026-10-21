package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// handleDocumentSymbol 返回文档大纲。
func (s *Server) handleDocumentSymbol(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DocumentSymbolParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, err)
	}

	doc := s.docs.Get(string(params.TextDocument.URI))
	if doc == nil {
		return reply(ctx, []protocol.DocumentSymbol{}, nil)
	}

	fi, idx := s.indexFor(doc)
	if fi == nil || idx == nil {
		return reply(ctx, []protocol.DocumentSymbol{}, nil)
	}

	symbols := make([]protocol.DocumentSymbol, 0, len(fi.TopLevel))
	for _, sym := range fi.TopLevel {
		symbols = append(symbols, toDocumentSymbol(sym, idx))
	}
	return reply(ctx, symbols, nil)
}

// toDocumentSymbol 把内部符号转换为 LSP 的 DocumentSymbol（含嵌套）。
func toDocumentSymbol(sym *index.Symbol, idx *textpos.Index) protocol.DocumentSymbol {
	sym.StartByte, sym.EndByte = clampRange(sym.StartByte, sym.EndByte, idx.Size())
	sym.DeclStart, sym.DeclEnd = clampRange(sym.DeclStart, sym.DeclEnd, idx.Size())

	selection := byteRange(idx, sym.StartByte, sym.EndByte)
	decl := byteRange(idx, sym.DeclStart, sym.DeclEnd)
	// 声明区间必须包含选择区间，否则编辑器可能报错
	if decl.Start.Line > selection.Start.Line ||
		(decl.Start.Line == selection.Start.Line && decl.Start.Character > selection.Start.Character) {
		decl = selection
	}

	out := protocol.DocumentSymbol{
		Name:           sym.Name,
		Detail:         symbolDetail(sym),
		Kind:           toSymbolKind(sym.Kind),
		Range:          decl,
		SelectionRange: selection,
	}

	for _, child := range sym.Children {
		out.Children = append(out.Children, toDocumentSymbol(child, idx))
	}
	return out
}

// symbolDetail 生成大纲里显示的补充信息。
//
// 原则：一行以内、信息密度高。RECORD 列出字段数而不是整段类型文本
// （真实文件里 RECORD 的类型文本常夹带大量注释，直接展示会撑爆大纲）。
func symbolDetail(sym *index.Symbol) string {
	switch sym.Kind {
	case index.KindMain:
		return "MAIN"
	case index.KindRecord:
		if len(sym.Children) > 0 {
			return fmt.Sprintf("RECORD (%d 字段)", len(sym.Children))
		}
		return "RECORD"
	case index.KindScreenRec:
		if len(sym.Children) > 0 {
			return fmt.Sprintf("SCREEN RECORD (%d 字段)", len(sym.Children))
		}
		return "SCREEN RECORD"
	case index.KindFunction, index.KindReport:
		if sym.Signature != "" {
			return sym.Signature
		}
		return string(sym.Kind)
	default:
		if sym.TypeText != "" {
			return truncate(sym.TypeText, 80)
		}
		return string(sym.Kind)
	}
}

// truncate 截断过长文本，避免大纲被单条信息撑爆。
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	// 按 rune 截断，避免切坏 UTF-8
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	return string(runes[:max]) + "…"
}

// toSymbolKind 把内部种类映射到 LSP SymbolKind。
func toSymbolKind(kind index.Kind) protocol.SymbolKind {
	switch kind {
	case index.KindMain, index.KindFunction:
		return protocol.SymbolKindFunction
	case index.KindReport:
		return protocol.SymbolKindFunction
	case index.KindDefine:
		return protocol.SymbolKindVariable
	case index.KindConstant:
		return protocol.SymbolKindConstant
	case index.KindType:
		return protocol.SymbolKindClass
	case index.KindRecord:
		return protocol.SymbolKindStruct
	case index.KindField:
		return protocol.SymbolKindField
	case index.KindParameter:
		return protocol.SymbolKindVariable
	case index.KindFormItem:
		return protocol.SymbolKindProperty
	case index.KindScreenRec:
		return protocol.SymbolKindStruct
	default:
		return protocol.SymbolKindVariable
	}
}

// handleWorkspaceSymbol 在所有已索引文档中搜索符号。
func (s *Server) handleWorkspaceSymbol(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.WorkspaceSymbolParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, err)
	}

	query := strings.ToLower(strings.TrimSpace(params.Query))
	const maxResults = 200

	var out []protocol.SymbolInformation
	for _, doc := range s.docs.All() {
		fi, idx := s.indexFor(doc)
		if fi == nil || idx == nil {
			continue
		}
		for _, sym := range fi.Symbols {
			if query != "" && !strings.Contains(sym.Lower, query) {
				continue
			}
			// 只返回可跨文件引用的符号：局部变量与记录字段会大量噪声
			if sym.Scope == index.ScopeFunc || sym.Scope == index.ScopeParam {
				continue
			}
			out = append(out, protocol.SymbolInformation{
				Name: sym.Name,
				Kind: toSymbolKind(sym.Kind),
				Location: protocol.Location{
					URI:   protocol.DocumentURI(doc.URI),
					Range: byteRange(idx, sym.StartByte, sym.EndByte),
				},
				ContainerName: sym.Container,
			})
			if len(out) >= maxResults {
				break
			}
		}
		if len(out) >= maxResults {
			break
		}
	}

	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return reply(ctx, out, nil)
}

// handleHover 返回光标处符号的签名与位置。
func (s *Server) handleHover(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.HoverParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, err)
	}

	doc := s.docs.Get(string(params.TextDocument.URI))
	if doc == nil {
		return reply(ctx, nil, nil)
	}

	fi, idx := s.indexFor(doc)
	if fi == nil || idx == nil {
		return reply(ctx, nil, nil)
	}

	word := s.identAt(doc, idx, int(params.Position.Line), int(params.Position.Character))
	if word == "" {
		return reply(ctx, nil, nil)
	}

	// 按作用域优先级取候选（与跳转共用同一套规则）。
	//
	// 旧实现直接 fi.Lookup(小写名)，会把**全文件**同名符号都列出来：
	// 两个函数各有一个局部变量 i 时，悬停任一 i 都会同时显示两个 i
	// 及其所属函数（用户实测报出的 bug）。
	//
	// 现在的顺序与 Genero 一致：
	//   当前函数的局部变量 → 当前函数的参数 → 模块级
	// 全部落空才去 GLOBALS 引入（模组全局变量）。
	offset := idx.Offset(int(params.Position.Line), int(params.Position.Character))
	container := enclosingContainer(fi, offset)
	var matches []*index.Symbol
	for _, group := range scopedSymbolsAt(fi, word, container, offset) {
		if len(group) > 0 {
			matches = group
			break
		}
	}
	if len(matches) == 0 {
		// P3a（F2）：本文档没有定义时，查 GLOBALS 引入的模组全局变量。
		// 悬停来源标注是用户明确要求的能力。
		if h := s.hoverGlobalsImported(doc, idx, word); h != nil {
			return reply(ctx, *h, nil)
		}
		return reply(ctx, nil, nil)
	}

	// 同一作用域内仍可能多命中（GLOBALS 块里重复定义等），
	// 列出全部让用户自己判断，比猜测可靠。
	var sb strings.Builder
	for i, sym := range matches {
		if i > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(hoverText(sym))
	}

	// P3a（F2）：定义方文档（.global 自己打开）里悬停块内变量时，
	// 标注它是模组全局变量（跨模块共享）。
	if len(matches) == 1 && matches[0].Scope == index.ScopeGlobals {
		sb.WriteString("\n\n---\n模组全局变量（GLOBALS 块内定义，跨模块共享）")
	}

	return reply(ctx, protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: sb.String(),
		},
		Range: ptrRange(byteRange(idx, matches[0].StartByte, matches[0].EndByte)),
	}, nil)
}

// hoverGlobalsImported 生成"GLOBALS 引入变量"的悬停内容（F2）。
//
// 位置 Range 用引用方文档的（光标处的词），内容列出全部来源：
// 变量名 + 类型 + 来源文件:行号。多来源时全部列出（与跳转策略一致）。
func (s *Server) hoverGlobalsImported(doc *document.Document, idx *textpos.Index, word string) *protocol.Hover {
	if s.globalsRes == nil {
		return nil
	}
	locs, sources := s.globalsDefinitions(word)
	if len(locs) == 0 {
		return nil
	}

	// 从对应 entry 取类型文本：重新查一遍带类型（sources 与 locs 对齐）
	typeLines := s.globalsTypeLines(word)

	var sb strings.Builder
	sb.WriteString("```bdl\n")
	sb.WriteString(word)
	if len(typeLines) == 1 && typeLines[0] != "" {
		sb.WriteString(" : ")
		sb.WriteString(typeLines[0])
	}
	sb.WriteString("\n```")
	sb.WriteString("\n\n来源（GLOBALS 引入）：")
	for i, src := range sources {
		if i > 0 {
			sb.WriteString("\n")
		}
		sb.WriteString("- ")
		sb.WriteString(src)
	}
	if len(typeLines) > 1 {
		sb.WriteString("\n\n多个来源的类型：")
		for _, tt := range typeLines {
			if tt == "" {
				continue
			}
			sb.WriteString("\n- ")
			sb.WriteString(tt)
		}
	}

	// Range 落在光标处的词上（本文档坐标）
	rng := protocol.Range{}
	if off, ok := wordOffset(doc, idx, word); ok {
		rng = byteRange(idx, uint(off), uint(off+len(word)))
	}
	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: sb.String(),
		},
		Range: &rng,
	}
}

// hoverText 生成单个符号的 markdown 描述。
func hoverText(sym *index.Symbol) string {
	var sb strings.Builder

	switch sym.Kind {
	case index.KindFunction, index.KindReport, index.KindMain:
		if sym.Signature != "" {
			sb.WriteString("```bdl\n")
			sb.WriteString(sym.Signature)
			sb.WriteString("\n```")
		} else {
			sb.WriteString("```bdl\n")
			sb.WriteString(strings.ToUpper(string(sym.Kind)))
			sb.WriteString(" ")
			sb.WriteString(sym.Name)
			sb.WriteString("\n```")
		}
	case index.KindFormItem:
		sb.WriteString("```per\n")
		sb.WriteString(sym.TypeText)
		sb.WriteString(" ")
		sb.WriteString(sym.Name)
		sb.WriteString("\n```")
	case index.KindScreenRec:
		sb.WriteString("```per\nSCREEN RECORD ")
		sb.WriteString(sym.Name)
		sb.WriteString("\n```")
	default:
		// 优先用保留换行的形态：多行 RECORD 挤成一行后完全没法读
		// （实测 20+ 字段的 RECORD 会变成几百字符的单一长行）。
		// TypeText 是规范化过的（换行→空格），只用于语义判断。
		decl := sym.TypeTextRaw
		if decl == "" {
			decl = sym.TypeText
		}
		sb.WriteString("```bdl\n")
		if decl != "" {
			sb.WriteString(sym.Name)
			sb.WriteString(" : ")
			sb.WriteString(decl)
		} else {
			sb.WriteString(sym.Name)
		}
		sb.WriteString("\n```")
	}

	if sym.Container != "" && sym.Container != sym.Name {
		sb.WriteString("\n\n所属: `")
		sb.WriteString(sym.Container)
		sb.WriteString("`")
	}
	if sym.Scope != "" {
		sb.WriteString("\n\n作用域: ")
		sb.WriteString(string(sym.Scope))
	}
	return sb.String()
}

// ptrRange 返回 Range 的指针（Hover.Range 是 *Range）。
func ptrRange(r protocol.Range) *protocol.Range { return &r }

// byteRange 把字节区间换算为 LSP Range。
func byteRange(idx *textpos.Index, start, end uint) protocol.Range {
	if idx == nil {
		// 行索引缺失（如跨文件目标未缓存）：退化为单点，
		// 不 panic——诊断/跳转的健壮性优先于精确位置。
		return protocol.Range{}
	}
	sl, sc := idx.Position(int(start))
	el, ec := idx.Position(int(end))
	return protocol.Range{
		Start: protocol.Position{Line: uint32(sl), Character: uint32(sc)},
		End:   protocol.Position{Line: uint32(el), Character: uint32(ec)},
	}
}

// clampRange 保证区间不越界且 start <= end。
func clampRange(start, end uint, size int) (uint, uint) {
	if size <= 0 {
		return 0, 0
	}
	if start > uint(size) {
		start = uint(size)
	}
	if end > uint(size) {
		end = uint(size)
	}
	if start > end {
		start = end
	}
	return start, end
}
