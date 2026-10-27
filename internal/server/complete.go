// complete.go 处理 textDocument/completion（P6）。
//
// 候选来源（按优先级）：
//  1. 光标处的表单字段（复用 P3b 的作用域跟踪，FormFieldsAt）；
//  2. 当前文件符号（函数/变量/记录/类型）；
//  3. 跨文件符号（已打开文档里的函数/报表）；
//  4. 内置符号（P7-C：STATUS/SQLCA/ARG_VAL/fgl_*/ui.* 等）；
//  5. 关键字（来自 grammar 源码生成的清单）。
//
// `.` 之后只给字段与"可能是成员"的符号，不给关键字——那时用户要的是成员名。
//
// 不做类型推导：Genero 弱类型 + 大量运行时绑定，推导收益低于复杂度。
// 结构体成员用"表单字段 + 同文件变量/字段符号"尽力而为；这类近似只会
// 多给候选，不会漏掉正确的那个。
package server

import (
	"context"
	"encoding/json"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/internal/completion"
	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/form"
	"github.com/darcyjoven/bdl-lsp/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// handleCompletion 处理"代码补全"。
func (s *Server) handleCompletion(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.CompletionParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, err)
	}

	doc := s.docs.Get(string(params.TextDocument.URI))
	if doc == nil {
		return reply(ctx, nil, nil)
	}

	text, tree, _ := doc.Snapshot()
	if tree == nil || len(text) == 0 {
		return reply(ctx, nil, nil)
	}

	line := int(params.Position.Line)
	char := int(params.Position.Character)
	idx := doc.Index()

	offset := idx.Offset(line, char)
	if offset < 0 || offset > len(text) {
		return reply(ctx, nil, nil)
	}

	lineText := linePrefixAt(text, offset)
	cctx := completion.Context{Lang: string(doc.Lang), Line: lineText}
	cctx.Word, cctx.AfterDot = completion.SplitWord(lineText)
	if params.Context != nil {
		cctx.TriggerChar = string(params.Context.TriggerCharacter)
	}
	cctx.InComment, cctx.InString = contextAt(tree, uint(offset))

	// P6：`. `左侧是 `RECORD LIKE t.*` 时，把数据库列作为精确候选。
	fi, _ := s.indexFor(doc)
	if cctx.AfterDot {
		cols, detail := s.schemaMembersForCompletion(fi, lineText)
		cctx.MemberDetail = detail
		if len(cols) > 0 {
			cctx.MemberColumns = make([]completion.MemberColumn, 0, len(cols))
			for _, c := range cols {
				cctx.MemberColumns = append(cctx.MemberColumns, completion.MemberColumn{
					Name: c.Name, Type: c.Display(), Desc: c.Desc, Table: detail,
				})
			}
		}
	}

	// 注释/字符串里不补全：关键字会干扰写作。
	if cctx.InComment || cctx.InString {
		return reply(ctx, protocol.CompletionList{Items: []protocol.CompletionItem{}}, nil)
	}

	symbols := s.crossFileSymbols(doc.URI)
	fields := s.formFieldsAt(doc, tree, text, uint(offset))

	res := completion.Provide(cctx, fi, symbols, fields, completion.Options{
		Keywords: true,
		// 与格式化共用同一个设置：设成 lower 时补全的小写字形与格式化结果一致。
		KeywordCase: s.settings().Format.Keywords,
		Fields:      true,
		Builtins:    s.builtinCandidates(cctx.Word),
	})

	items := make([]protocol.CompletionItem, 0, len(res.Items))
	for _, it := range res.Items {
		item := protocol.CompletionItem{
			Label:    it.Label,
			Kind:     protocol.CompletionItemKind(it.Kind),
			Detail:   it.Detail,
			SortText: it.SortText,
		}
		if it.InsertText != "" {
			item.InsertText = it.InsertText
		}
		if it.Documentation != "" {
			item.Documentation = it.Documentation
		}
		items = append(items, item)
	}
	return reply(ctx, protocol.CompletionList{
		IsIncomplete: res.IsIncomplete,
		Items:        items,
	}, nil)
}

// handleCompletionResolve 在用户确认候选时补全文档。
//
// 当前没有额外信息可补（Detail 已在初次返回里给全），原样返回。
// 保留该能力是因为部分客户端在服务器不支持 resolve 时不显示文档字段。
func (s *Server) handleCompletionResolve(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var item protocol.CompletionItem
	if err := json.Unmarshal(req.Params(), &item); err != nil {
		return reply(ctx, nil, err)
	}
	return reply(ctx, item, nil)
}

// formFieldsAt 返回光标处的可用表单字段；不可用时返回 nil。
//
// 复用 P3b 的 Resolver：与字段校验走同一套作用域跟踪，因此补全给出的
// 字段与校验认的字段永远一致（不会"补全给了但校验报不存在"）。
func (s *Server) formFieldsAt(doc *document.Document, tree *ts.Tree, text []byte, offset uint) []string {
	if s.formRes == nil || doc == nil || doc.Lang != parser.LangBDL {
		return nil
	}
	if s.formQs == nil {
		s.formQs = form.CompileQueries(s.pool)
	}
	if s.formQs == nil {
		return nil
	}
	fields, _ := s.formRes.FormFieldsAt(tree, text, form.PathOfURI(doc.URI), s.formQs, offset)
	return fields
}

// crossFileSymbols 返回其它已打开文档里可跨文件引用的符号。
//
// 只看已打开文档（没有工作区级索引）：这是刻意的取舍——全工作区索引在
// 15k 文件上需要后台任务与持久化缓存（PLAN §7.6），而补全里多几个"项目
// 常用函数"的收益有限。GLOBALS 变量已由 P3a 的跨文件索引覆盖。
func (s *Server) crossFileSymbols(excludeURI string) []*index.Symbol {
	var out []*index.Symbol
	for _, other := range s.docs.All() {
		if other.URI == excludeURI {
			continue
		}
		fi, _ := s.indexFor(other)
		if fi == nil {
			continue
		}
		for _, sym := range fi.Symbols {
			switch sym.Kind {
			case index.KindFunction, index.KindReport, index.KindMain:
				out = append(out, sym)
			}
		}
	}
	return out
}

// linePrefixAt 返回给定偏移所在行、该偏移之前的那段文本。
func linePrefixAt(text []byte, offset int) string {
	if offset < 0 {
		return ""
	}
	if offset > len(text) {
		offset = len(text)
	}
	start := offset
	for start > 0 && text[start-1] != '\n' && text[start-1] != '\r' {
		start--
	}
	return string(text[start:offset])
}

// contextAt 判断偏移是否落在注释或字符串内。
//
// 用语法树判断而不是手写词法：字符串里的 `--`、注释里的引号都能正确处理。
// DescendantForByteRange 给的是"包含该偏移的最小节点"，光标在字符串中间
// 时常落到更小的子节点上，因此必须沿祖先链向上找。
func contextAt(tree *ts.Tree, offset uint) (inComment, inString bool) {
	if tree == nil {
		return false, false
	}
	root := tree.RootNode()
	if root == nil {
		return false, false
	}
	node := root.DescendantForByteRange(offset, offset)
	for n := node; n != nil; n = n.Parent() {
		switch n.Kind() {
		case "comment":
			return true, false
		case "string":
			return false, true
		}
	}
	return false, false
}
