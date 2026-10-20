// semtok.go 处理 textDocument/semanticTokens/full（P6）。
//
// 设计原则：**只标注 TextMate grammar 做不到的部分。**
//
// 关键字、字符串、注释、数字这些语法层面能确定的，TextMate 已经做得很准
// （而且和 grammar 同源）。语义高亮再报一遍只会：
//   - 与 TextMate 抢同一批 token（客户端合并规则不一致）；
//   - 白白增加数据量（7000 行文件光关键字就几千个 token）。
//
// 所以这里只发**用户自定义符号**：
//   - 函数 / 报表 / MAIN 名 → function（定义处带 declaration 修饰）
//   - 变量 / 常量 → variable（常量带 readonly）
//   - 记录 / TYPE / SCREEN RECORD → struct
//   - 字段 / 表单 item → property
//   - 参数 → parameter
//   - GLOBALS 块内声明 → variable + static
//
// 效果：TextMate 给"这个词是语法结构"，语义高亮给"这个词是你项目里的
// 某个函数/变量"。两者叠加后，用户定义的名字终于能和关键字区分开。
package server

import (
	"context"
	"encoding/json"
	"sort"

	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// 语义 token 类型索引。顺序必须与 semanticTokenTypes() 返回的切片一致——
// 客户端用这个下标解释 data 里的数字，顺序错了颜色就全乱。
const (
	tokTypeVariable = iota
	tokTypeParameter
	tokTypeFunction
	tokTypeStruct
	tokTypeProperty
	tokTypeKeyword
)

// 修饰位（bitmask，最多 32 个）。
const (
	modDeclaration = 1 << iota // 这里是定义处
	modReadonly                // 常量
	modStatic                  // GLOBALS 块内（跨模块共享）
)

// semanticTokenTypes 返回类型表（顺序与上面的常量一致）。
func semanticTokenTypes() []protocol.SemanticTokenTypes {
	return []protocol.SemanticTokenTypes{
		protocol.SemanticTokenVariable,
		protocol.SemanticTokenParameter,
		protocol.SemanticTokenFunction,
		protocol.SemanticTokenStruct,
		protocol.SemanticTokenProperty,
		protocol.SemanticTokenKeyword,
	}
}

// semanticTokenModifiers 返回修饰表（顺序与上面的 bit 一致）。
func semanticTokenModifiers() []protocol.SemanticTokenModifiers {
	return []protocol.SemanticTokenModifiers{
		protocol.SemanticTokenModifierDeclaration,
		protocol.SemanticTokenModifierReadonly,
		protocol.SemanticTokenModifierStatic,
	}
}

// handleSemanticTokensFull 处理整文档语义 token 请求。
func (s *Server) handleSemanticTokensFull(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.SemanticTokensParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, err)
	}

	doc := s.docs.Get(string(params.TextDocument.URI))
	if doc == nil {
		return reply(ctx, nil, nil)
	}

	data := s.semanticTokensFor(doc)
	return reply(ctx, protocol.SemanticTokens{Data: data}, nil)
}

// semanticTokensFor 计算文档的语义 token（已按 LSP 要求编码）。
func (s *Server) semanticTokensFor(doc *document.Document) []uint32 {
	fi, idx := s.indexFor(doc)
	if fi == nil || idx == nil {
		return nil
	}

	type token struct {
		line, char, length int
		typ, mods          int
	}
	var toks []token
	seen := map[uint]bool{}

	for _, sym := range fi.Symbols {
		typ, mods, ok := classifySymbol(sym)
		if !ok {
			continue
		}
		start := sym.StartByte
		end := sym.EndByte
		if start >= end {
			continue
		}
		// 同一位置只发一个 token（同名符号可能重复出现在父子层级里）。
		if seen[start] {
			continue
		}
		seen[start] = true

		line, char := idx.Position(int(start))
		// token 长度用"字符数"而不是字节数：LSP 里 length 是 UTF-16 码元，
		// 中文标识符（虽然罕见）会算错。按行内文本量一次。
		length := tokenLength(doc, idx, start, end)
		if length <= 0 {
			continue
		}
		toks = append(toks, token{line: line, char: char, length: length, typ: typ, mods: mods})
	}

	if len(toks) == 0 {
		return nil
	}
	// LSP 要求按行、列升序，且用相对编码。
	sort.SliceStable(toks, func(i, j int) bool {
		if toks[i].line != toks[j].line {
			return toks[i].line < toks[j].line
		}
		return toks[i].char < toks[j].char
	})

	data := make([]uint32, 0, len(toks)*5)
	prevLine, prevChar := 0, 0
	for _, t := range toks {
		deltaLine := t.line - prevLine
		deltaChar := t.char
		if deltaLine == 0 {
			deltaChar = t.char - prevChar
		}
		if deltaLine < 0 || deltaChar < 0 {
			// 排序保证了不会发生；跳过坏数据好过发错数据。
			continue
		}
		data = append(data,
			uint32(deltaLine),
			uint32(deltaChar),
			uint32(t.length),
			uint32(t.typ),
			uint32(t.mods),
		)
		prevLine = t.line
		prevChar = t.char
	}
	return data
}

// classifySymbol 把索引符号映射成 (类型, 修饰位)；ok=false 表示不标注。
func classifySymbol(sym *index.Symbol) (typ, mods int, ok bool) {
	if sym == nil || sym.Name == "" {
		return 0, 0, false
	}
	switch sym.Kind {
	case index.KindFunction:
		return tokTypeFunction, modDeclaration, true
	case index.KindMain:
		return tokTypeFunction, modDeclaration, true
	case index.KindReport:
		return tokTypeFunction, modDeclaration, true
	case index.KindDefine:
		return tokTypeVariable, withStatic(sym, modDeclaration), true
	case index.KindConstant:
		return tokTypeVariable, withStatic(sym, modDeclaration|modReadonly), true
	case index.KindType:
		return tokTypeStruct, modDeclaration, true
	case index.KindRecord:
		return tokTypeStruct, modDeclaration, true
	case index.KindScreenRec:
		return tokTypeStruct, modDeclaration, true
	case index.KindField:
		return tokTypeProperty, modDeclaration, true
	case index.KindFormItem:
		return tokTypeProperty, modDeclaration, true
	case index.KindParameter:
		return tokTypeParameter, modDeclaration, true
	default:
		return 0, 0, false
	}
}

// withStatic 给 GLOBALS 块内的符号加上 static 修饰（跨模块共享）。
func withStatic(sym *index.Symbol, mods int) int {
	if sym.Scope == index.ScopeGlobals {
		return mods | modStatic
	}
	return mods
}

// tokenLength 返回 token 的 UTF-16 长度（LSP 规范要求）。
//
// 只量符号名本身（StartByte..EndByte），不跨行：标识符不可能跨行。
func tokenLength(doc *document.Document, idx *textpos.Index, start, end uint) int {
	text, _, _ := doc.Snapshot()
	if int(end) > len(text) || start >= end {
		return 0
	}
	raw := text[start:end]
	// 去掉可能的引号/装饰（.per 的 item 名有时带后缀），只算标识符部分。
	length := 0
	for _, r := range string(raw) {
		if r == '\n' || r == '\r' {
			break
		}
		// UTF-16：BMP 之外的字符占 2 个码元。
		if r > 0xFFFF {
			length += 2
		} else {
			length++
		}
	}
	_ = idx
	return length
}
