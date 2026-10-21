package server

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/globals"
	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	ts "github.com/tree-sitter/go-tree-sitter"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// occurrence 是一次词出现的位置，用于引用查找。
type occurrence struct {
	start, end uint
}

// handleDefinition 处理“转到定义”。
//
// 解析顺序（Genero 作用域规则）：
//  1. `rec.field` 形式：字段定义在 rec 的记录结构里；
//  2. 当前函数内的局部 DEFINE；
//  3. 当前函数的参数；
//  4. 模块级 DEFINE / CONSTANT / TYPE / FUNCTION / REPORT / MAIN。
//
// 同名多命中时全部返回，交给客户端让用户选择（比猜测更可靠）。
func (s *Server) handleDefinition(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.DefinitionParams
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

	offset := idx.Offset(int(params.Position.Line), int(params.Position.Character))
	word := wordAt(doc.Text(), offset)
	if word == "" {
		return reply(ctx, nil, nil)
	}

	locs := s.resolveDefinition(doc, fi, idx, word, offset)
	if len(locs) == 0 {
		return reply(ctx, nil, nil)
	}
	return reply(ctx, locs, nil)
}

// scopedSymbols 按 Genero 的作用域规则把同名符号分成若干组，优先级从高到低：
//
//	locals   —— 当前函数内的局部 DEFINE（Scope=func 且 Container 匹配）
//	params   —— 当前函数的参数
//	globals  —— 模块级（Scope=global）的变量/常量/类型/函数
//	globalsB —— GLOBALS...END GLOBALS 块内（Scope=globals-block）
//
// container 是光标所在的函数名（enclosingContainer 的结果；空表示不在函数内）。
//
// 这个函数被跳转与悬停共用，避免两处各写一套规则导致行为不一致
// （P7-B 的 bug 就是悬停漏了作用域过滤，把别的函数的局部变量也列了出来）。
//
// 裸字段名（Kind==field）不参与：`rec.field` 的字段由 resolveRecordField 处理，
// 裸字段名推断归属容易误跳。
func scopedSymbols(fi *index.FileIndex, word, container string) [][]*index.Symbol {
	return scopedSymbolsAt(fi, word, container, 0)
}

// scopedSymbolsAt 是 scopedSymbols 的带位置版本。
//
// offset 是光标在文件里的字节偏移；用来判断"光标是否落在某个记录
// 定义的内部"。记录字段的 Container 是**记录名**而不是函数名
// （见 index.collectRecordFields），所以不能用函数作用域过滤，
// 只能按"光标在不在这个记录的声明区间内"来判断。
//
// offset=0 表示不做位置判断（调用方拿不到位置时用）。
func scopedSymbolsAt(fi *index.FileIndex, word, container string, offset int) [][]*index.Symbol {
	var locals, params, globals, globalsB, fields []*index.Symbol
	if fi == nil {
		return nil
	}
	lower := strings.ToLower(word)
	for _, sym := range fi.Symbols {
		if sym.Lower != lower {
			continue
		}
		switch {
		case sym.Kind == index.KindField:
			// 记录字段：只有当光标落在**声明该字段的那个记录**内部时才算命中。
			// 否则同名裸字段名会到处命中（例如 rec1.x 与 rec2.x 都叫 x）。
			//
			// 注意：不能简单按函数过滤——字段的 Container 是记录名不是函数名。
			if offset > 0 && inRecordDeclaration(fi, sym, offset) {
				fields = append(fields, sym)
			}
			continue
		case sym.Scope == index.ScopeFunc && container != "" && sym.Container == container:
			locals = append(locals, sym)
		case sym.Scope == index.ScopeParam && container != "" && sym.Container == container:
			params = append(params, sym)
		case sym.Scope == index.ScopeGlobal:
			globals = append(globals, sym)
		case sym.Scope == index.ScopeGlobals:
			// GLOBALS 块内的声明：与模块级变量同名时块内优先
			// （块内那个才是"最近"的定义，且它可能来自 .global 文件）。
			globalsB = append(globalsB, sym)
		}
	}
	return [][]*index.Symbol{locals, params, globalsB, globals, fields}
}

// inRecordDeclaration 判断光标偏移是否落在"声明了 sym 这个字段的记录"
// 的声明区间内。
//
// 实现：找到与 sym.Parent 同名的记录符号，检查其 DeclStart..DeclEnd 是否
// 覆盖 offset。记录符号既有顶层 TYPE..END RECORD 形式，也有内联
// DEFINE x RECORD ... END RECORD 形式，两者的 Decl 区间都覆盖整个定义。
func inRecordDeclaration(fi *index.FileIndex, sym *index.Symbol, offset int) bool {
	if sym.Parent == "" {
		return false
	}
	u := uint(offset)
	for _, cand := range fi.TopLevel {
		if cand.Kind != index.KindRecord {
			continue
		}
		if !strings.EqualFold(cand.Name, sym.Parent) {
			continue
		}
		if cand.DeclStart <= u && u <= cand.DeclEnd {
			return true
		}
	}
	return false
}

// resolveDefinition 按作用域优先级解析定义位置。
//
// P3a 起为四级（Genero 作用域规则）：
//  1. `rec.field` 形式：字段定义在 rec 的记录结构里；
//  2. 当前函数内的局部 DEFINE；
//  3. 当前函数的参数；
//  4. 模块级 DEFINE / CONSTANT / TYPE / FUNCTION / REPORT / MAIN；
//  5. GLOBALS 引入（模组全局变量，定义在引用的 .global 文件里）。
//
// 同名多命中时全部返回，交给客户端让用户选择（比猜测更可靠）。
func (s *Server) resolveDefinition(doc *document.Document, fi *index.FileIndex, idx *textpos.Index, word string, offset int) []protocol.Location {
	// 1) rec.field 形式
	if rec, field, ok := qualifierAt(doc.Text(), offset, word); ok {
		if locs := resolveRecordField(doc.URI, fi, idx, rec, field); len(locs) > 0 {
			return locs
		}
	}

	groups := scopedSymbols(fi, word, enclosingContainer(fi, offset))
	for _, group := range groups {
		if len(group) > 0 {
			return toLocations(doc.URI, idx, group)
		}
	}

	// 5) GLOBALS 引入（跨文件：Location 的 URI 指向 .global 文件）
	if locs, _ := s.globalsDefinitions(word); len(locs) > 0 {
		return locs
	}

	// 6) 工作区符号索引（P6）：本文件没有、但别的模组有这个函数/报表。
	// 只在"可达模组"（本模组 + sub/lib/qry）里找，且要求唯一命中——
	// 猜错比跳不过去更糟。
	if locs := s.wsDefinition(globals.PathOfURI(doc.URI), word); len(locs) > 0 {
		return locs
	}
	return nil
}

// qualifierAt 判断光标处的词是否属于 `rec.field` 形式。
//
// 返回限定符（rec）、词本身（field）以及是否命中。
// 注意：offset 可能落在词首、词中或词尾，因此需要先求出词的起始位置。
func qualifierAt(text []byte, offset int, word string) (rec, field string, ok bool) {
	wordStart := wordStartAt(text, offset, word)
	if wordStart < 0 {
		return "", "", false
	}
	// 词前必须是 '.'（允许空白）
	i := wordStart - 1
	for i >= 0 && (text[i] == ' ' || text[i] == '\t') {
		i--
	}
	if i < 0 || text[i] != '.' {
		return "", "", false
	}
	// '.' 前必须是标识符
	j := i - 1
	for j >= 0 && (text[j] == ' ' || text[j] == '\t') {
		j--
	}
	end := j + 1
	for j >= 0 && isIdentByte(text[j]) {
		j--
	}
	start := j + 1
	if start >= end {
		return "", "", false
	}
	return string(text[start:end]), word, true
}

// wordStartAt 求出 word 在 text 中的起始位置。
//
// offset 可能位于词的任意位置（词首/词中/词尾），因此先向前回退到词首，
// 再校验该处确实是 word。
func wordStartAt(text []byte, offset int, word string) int {
	if word == "" || offset < 0 || offset > len(text) {
		return -1
	}
	// 向前回退到词首
	start := offset
	for start > 0 && isIdentByte(text[start-1]) {
		start--
	}
	// 向后延伸确认完整词
	end := start
	for end < len(text) && isIdentByte(text[end]) {
		end++
	}
	if end-start != len(word) {
		return -1
	}
	for i := 0; i < len(word); i++ {
		c := text[start+i]
		w := word[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if w >= 'A' && w <= 'Z' {
			w += 'a' - 'A'
		}
		if c != w {
			return -1
		}
	}
	return start
}

// isIdentByte 判断单字节是否为 ASCII 标识符字符。
func isIdentByte(b byte) bool {
	return b == '_' || (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// resolveRecordField 解析 `rec.field` 的跳转目标。
func resolveRecordField(uri string, fi *index.FileIndex, idx *textpos.Index, rec, field string) []protocol.Location {
	recLower := strings.ToLower(rec)
	fieldLower := strings.ToLower(field)

	var out []protocol.Location
	for _, sym := range fi.Symbols {
		if sym.Lower != recLower || len(sym.Children) == 0 {
			continue
		}
		for _, child := range sym.Children {
			if child.Lower == fieldLower {
				out = append(out, protocol.Location{
					URI:   protocol.DocumentURI(uri),
					Range: byteRange(idx, child.StartByte, child.EndByte),
				})
			}
		}
	}
	return out
}

// enclosingContainer 返回偏移所在的顶层容器（函数/报表/main）名字。
//
// 只考虑顶层容器：Genero 没有嵌套函数，因此偏移最多落在一个容器内。
func enclosingContainer(fi *index.FileIndex, offset int) string {
	if fi == nil {
		return ""
	}
	u := uint(offset)
	for _, sym := range fi.TopLevel {
		switch sym.Kind {
		case index.KindMain, index.KindFunction, index.KindReport:
			if sym.DeclStart <= u && u <= sym.DeclEnd {
				return sym.Name
			}
		}
	}
	return ""
}

// toLocations 把符号列表转成 LSP Location 列表。
func toLocations(uri string, idx *textpos.Index, syms []*index.Symbol) []protocol.Location {
	out := make([]protocol.Location, 0, len(syms))
	for _, sym := range syms {
		out = append(out, protocol.Location{
			URI:   protocol.DocumentURI(uri),
			Range: byteRange(idx, sym.StartByte, sym.EndByte),
		})
	}
	return out
}

// handleReferences 处理“查找引用”。
func (s *Server) handleReferences(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.ReferenceParams
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

	offset := idx.Offset(int(params.Position.Line), int(params.Position.Character))
	word := wordAt(doc.Text(), offset)
	if word == "" {
		return reply(ctx, nil, nil)
	}

	decls := s.resolveDefinition(doc, fi, idx, word, offset)
	includeDecl := params.Context.IncludeDeclaration

	occurrences := s.findOccurrences(doc, word)
	var out []protocol.Location
	for _, occ := range occurrences {
		rng := byteRange(idx, occ.start, occ.end)
		if !includeDecl && inDeclarations(rng, decls) {
			continue
		}
		out = append(out, protocol.Location{
			URI:   protocol.DocumentURI(doc.URI),
			Range: rng,
		})
	}
	// P6：工作区索引补齐跨文件调用点（同模组 + sub/lib/qry）。
	// 只对可按名字调用的符号（函数/报表）生效；局部变量与记录字段不跨文件。
	if s.referencesCrossFile(word) {
		out = append(out, s.wsReferences(globals.PathOfURI(doc.URI), word)...)
	}
	if len(out) == 0 {
		return reply(ctx, nil, nil)
	}
	return reply(ctx, out, nil)
}

// referencesCrossFile 报告某个词是否值得跨文件找引用。
//
// 变量/参数/记录字段的可见性由 GLOBALS 与记录结构决定，按名字跨文件匹配会
// 大面积误报；函数与报表则是按名字调用的，跨文件找才有意义。
func (s *Server) referencesCrossFile(word string) bool {
	ix := s.workspaceIndex()
	if ix == nil {
		return false
	}
	for _, d := range ix.Lookup(word) {
		switch d.Kind {
		case "function", "report", "prepare", "cursor":
			return true
		}
	}
	return false
}

// findOccurrences 在文本中查找词的全部出现，排除注释与字符串。
func (s *Server) findOccurrences(doc *document.Document, word string) []occurrence {
	text, tree, _ := doc.Snapshot()
	if tree == nil || len(text) == 0 {
		return nil
	}

	var out []occurrence
	seen := make(map[uint]bool)
	lower := strings.ToLower(word)

	for i := 0; i+len(lower) <= len(text); {
		pos := indexWordFold(text, lower, i)
		if pos < 0 {
			break
		}
		end := pos + len(lower)
		i = pos + 1

		// ASCII 词边界（非 ASCII 字符自然不匹配 isIdentByte）
		if pos > 0 && isIdentByte(text[pos-1]) {
			continue
		}
		if end < len(text) && isIdentByte(text[end]) {
			continue
		}
		if seen[uint(pos)] {
			continue
		}
		if offsetInTrivia(tree, uint(pos)) {
			continue
		}
		seen[uint(pos)] = true
		out = append(out, occurrence{start: uint(pos), end: uint(end)})
	}
	return out
}

// indexWordFold 在 text 中从 from 开始查找不区分大小写的 lower。
func indexWordFold(text []byte, lower string, from int) int {
	if from < 0 || from >= len(text) || lower == "" {
		return -1
	}
	needle := []byte(lower)
	n := len(needle)
	for i := from; i+n <= len(text); i++ {
		match := true
		for j := 0; j < n; j++ {
			c := text[i+j]
			if c >= 'A' && c <= 'Z' {
				c += 'a' - 'A'
			}
			if c != needle[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

// offsetInTrivia 判断偏移是否落在注释、字符串或字面量内。
//
// 引用查找必须排除这些位置，否则注释里提到变量名也会被算作引用。
func offsetInTrivia(tree *ts.Tree, offset uint) bool {
	root := tree.RootNode()
	if root == nil {
		return false
	}
	node := root.NamedDescendantForByteRange(offset, offset)
	for node != nil {
		switch node.Kind() {
		case "comment", "string", "literal", "preproc_directive":
			return true
		case "source_file":
			return false
		}
		node = node.Parent()
	}
	return false
}

// inDeclarations 判断给定范围是否与某个定义位置重合。
func inDeclarations(rng protocol.Range, decls []protocol.Location) bool {
	for _, d := range decls {
		if d.Range == rng {
			return true
		}
	}
	return false
}
