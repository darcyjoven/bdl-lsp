// Package index 从语法树提取符号，供符号列表、跳转与悬停使用。
//
// 设计原则：
//   - 只做“能从语法树确定的”信息，不做类型推导；
//   - 符号位置统一用字节偏移，行号/列由上层按需换算；
//   - 同名符号可能在不同作用域重复，用 Scope 区分。
package index

import (
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
)

// Kind 是符号种类。
type Kind string

const (
	KindMain      Kind = "main"
	KindFunction  Kind = "function"
	KindReport    Kind = "report"
	KindDefine    Kind = "define"
	KindConstant  Kind = "constant"
	KindType      Kind = "type"
	KindRecord    Kind = "record"
	KindField     Kind = "field"
	KindParameter Kind = "parameter"
	KindFormItem  Kind = "form_item"
	KindScreenRec Kind = "screen_record"
)

// Scope 是符号的可见范围。
type Scope string

const (
	// ScopeGlobal 模块级：主函数之前、任何函数之外（仅本模块可见）。
	ScopeGlobal Scope = "global"
	// ScopeGlobals GLOBALS ... END GLOBALS 块内的声明：
	// 模组全局变量，所有引入同一 global 文件的模块共享（P3a）。
	// 与 ScopeGlobal 的区别只在跨文件可见性，查找顺序也更靠后。
	ScopeGlobals Scope = "globals-block"
	// ScopeFunc 函数/报表/main 体内。
	ScopeFunc Scope = "func"
	// ScopeParam 函数参数。
	ScopeParam Scope = "param"
)

// GlobalsImport 是一条 `GLOBALS "file"` 引用（引用侧）。
//
// 只记录字面量与位置，路径解析由 internal/globals 负责。
type GlobalsImport struct {
	// Literal 是去引号后的路径字面量（变量形式时为空）。
	Literal string
	// IsVariable 表示文件名是变量而非字符串字面量，静态无法解析。
	IsVariable bool
	// Name 是变量形式的变量名（IsVariable 时使用）。
	Name string
	// StartByte / EndByte 是 filename 节点（含引号/变量名）的区间。
	StartByte uint
	EndByte   uint
}

// Symbol 是一个可跳转的命名实体。
type Symbol struct {
	Name string
	// Lower 是 Name 的小写形式，用于 Genero 的大小写不敏感匹配。
	Lower string
	Kind  Kind
	Scope Scope
	// Parent 是所属容器（函数名 / 记录名），顶层为空。
	Parent string
	// Container 是声明所在的函数/报表/main 名字，便于按作用域查找。
	Container string

	// StartByte / EndByte 是符号名本身的区间（不是整个声明）。
	StartByte uint
	EndByte   uint

	// DeclStart / DeclEnd 是整个声明的区间，用于代码折叠/大纲范围。
	DeclStart uint
	DeclEnd   uint

	// TypeText 是声明的类型文本（如 "LIKE type_file.num5"、"INTEGER"），可能为空。
	TypeText string
	// Signature 是函数/报表的签名文本，仅当 Kind 为 function/report 时填充。
	Signature string
	// Children 是嵌套符号（记录字段、函数参数）。
	Children []*Symbol
}

// FileIndex 是一个文件的符号集合。
type FileIndex struct {
	// URI 由调用方填充。
	URI string
	// Lang 是 bdl 或 per。
	Lang parser.Lang
	// Version 是建立索引时的文档版本。
	Version int32
	// Symbols 按出现顺序保存全部符号（含嵌套的扁平副本）。
	Symbols []*Symbol
	// TopLevel 只包含顶层符号，嵌套结构通过 Children 表达。
	TopLevel []*Symbol
	// GlobalsImports 是文件里的 GLOBALS "..." 引用列表（按出现顺序）。
	// 只有 bdl 语言会有内容。
	GlobalsImports []GlobalsImport
	// HasGlobalsBlock 表示本文件含 GLOBALS ... END GLOBALS 块定义
	// （通常是 .global 文件）。块内符号已进 Symbols（Scope 为 ScopeGlobals），
	// 单独记一个标志便于快速判断。
	HasGlobalsBlock bool
	// LayoutRefs 是 .per LAYOUT 里的 item tag 引用（[f01] 形式）。
	// 定义在 ATTRIBUTES，这里只是引用——供“布局引用了未定义 item”
	// 的内部校验用（P3b）。仅 per 语言有内容。
	LayoutRefs []LayoutRef
}

// LayoutRef 是 LAYOUT 里的一个 item tag 引用。
type LayoutRef struct {
	// Tag 是方括号里的名字（已去括号）。
	Tag string
	// Lower 是 Tag 的小写形式。
	Lower     string
	StartByte uint
	EndByte   uint
}

// Lookup 按小写名查找符号，返回所有匹配（不同作用域可能重名）。
func (fi *FileIndex) Lookup(lower string) []*Symbol {
	if fi == nil {
		return nil
	}
	var out []*Symbol
	for _, sym := range fi.Symbols {
		if sym.Lower == lower {
			out = append(out, sym)
		}
	}
	return out
}

// Build 从语法树提取符号。
//
// container 参数用于区分模块级与函数内，调用方传空字符串。
func Build(lang parser.Lang, tree *ts.Tree, src []byte, version int32) *FileIndex {
	fi := &FileIndex{Lang: lang, Version: version}
	if tree == nil {
		return fi
	}
	root := tree.RootNode()
	if root == nil {
		return fi
	}

	b := &builder{src: src, fi: fi, cursor: tree.Walk()}
	defer b.cursor.Close()

	count := root.NamedChildCount()
	for i := uint(0); i < count; i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}
		b.visitTop(child, "")
	}
	return fi
}

type builder struct {
	src    []byte
	fi     *FileIndex
	cursor *ts.TreeCursor
}

// fieldChildren 是 ChildrenByFieldName 的安全封装（需要 cursor，且返回 Node 值）。
func (b *builder) fieldChildren(node *ts.Node, field string) []*ts.Node {
	values := node.ChildrenByFieldName(field, b.cursor)
	out := make([]*ts.Node, 0, len(values))
	for i := range values {
		v := values[i]
		out = append(out, &v)
	}
	return out
}

// visitTop 处理模块级结构（函数/报表/main/全局声明）。
func (b *builder) visitTop(node *ts.Node, container string) {
	switch node.Kind() {
	case "main":
		b.addContainer(node, "main", KindMain, container, "")
	case "function":
		name := b.fieldText(node, "name")
		sig := b.functionSignature(node, "FUNCTION")
		b.addContainer(node, name, KindFunction, container, sig)
	case "report":
		name := b.fieldText(node, "name")
		sig := b.functionSignature(node, "REPORT")
		b.addContainer(node, name, KindReport, container, sig)
	case "define":
		b.addDefines(node, ScopeGlobal, container)
	case "constant":
		b.addNamed(node, "name", KindConstant, ScopeGlobal, container, "")
	case "type":
		b.addTypeDecl(node, ScopeGlobal, container)
	case "globals":
		b.visitGlobals(node)
	case "schema", "import", "options", "preproc":
		// 其余顶层结构暂不建符号
	}
}

// visitGlobals 处理 GLOBALS 的两种形式（P3a）：
//
//   - 块定义：GLOBALS ... END GLOBALS，内部 constant/type/define 以
//     ScopeGlobals 收集（模组全局变量，跨文件共享）；
//   - 文件引用：GLOBALS "path" / GLOBALS var，记录字面量与位置，
//     路径解析交给 internal/globals。
//
// 提取必须走语法树的 filename 字段而不是行正则：
// 行尾可能带注释（GLOBALS #FUN-AC0060 ...，语料实测存在），
// 且被注释掉的 #GLOBALS 行 grammar 不会解析（实测确认）。
func (b *builder) visitGlobals(node *ts.Node) {
	// 文件引用形式：有 filename 字段
	if fn := node.ChildByFieldName("filename"); fn != nil {
		imp := GlobalsImport{
			StartByte: fn.StartByte(),
			EndByte:   fn.EndByte(),
		}
		switch fn.Kind() {
		case "string":
			// 剥掉两侧引号（可能带转义，这里只处理普通引号）
			imp.Literal = unquoteString(b.text(fn))
		case "variable":
			imp.IsVariable = true
			// 变量名取首标识符（variable 可能是 a.b 形式，文件名取整体文本即可）
			imp.Name = b.text(fn)
		default:
			// 未知形式，记变量按不可解析降级
			imp.IsVariable = true
			imp.Name = b.text(fn)
		}
		b.fi.GlobalsImports = append(b.fi.GlobalsImports, imp)
		return
	}

	// 块定义形式：遍历 constant/type/define 子节点
	b.fi.HasGlobalsBlock = true
	count := node.NamedChildCount()
	for i := uint(0); i < count; i++ {
		child := node.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "define":
			b.addDefines(child, ScopeGlobals, "")
		case "constant":
			b.addNamed(child, "name", KindConstant, ScopeGlobals, "", "")
		case "type":
			b.addTypeDecl(child, ScopeGlobals, "")
		}
	}
}

// unquoteString 去掉字符串字面量两侧的引号。
//
// tree-sitter 的 string 节点文本含引号本身；转义序列（\"）不展开——
// 文件路径里出现引号转义在语料中不存在，保持原样足够。
func unquoteString(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// addContainer 为 main/function/report 建符号，并递归收集其内部声明。
func (b *builder) addContainer(node *ts.Node, name string, kind Kind, parent, signature string) {
	displayName := name
	if displayName == "" {
		displayName = strings.ToLower(string(kind))
	}

	sym := &Symbol{
		Name:      displayName,
		Lower:     strings.ToLower(displayName),
		Kind:      kind,
		Scope:     ScopeGlobal,
		Parent:    parent,
		DeclStart: node.StartByte(),
		DeclEnd:   node.EndByte(),
		Signature: signature,
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		Container: displayName,
	}
	// 有 name 字段时把符号区间收缩到名字本身，便于精确高亮/跳转。
	if nameNode := node.ChildByFieldName("name"); nameNode != nil {
		sym.StartByte = nameNode.StartByte()
		sym.EndByte = nameNode.EndByte()
	}

	// 参数
	if params := b.fieldChildren(node, "params"); len(params) > 0 {
		for _, p := range params {
			param := &Symbol{
				Name:      b.text(p),
				Kind:      KindParameter,
				Scope:     ScopeParam,
				Parent:    displayName,
				StartByte: p.StartByte(),
				EndByte:   p.EndByte(),
				DeclStart: p.StartByte(),
				DeclEnd:   p.EndByte(),
				Container: displayName,
			}
			if param.Name == "" {
				continue
			}
			param.Lower = strings.ToLower(param.Name)
			sym.Children = append(sym.Children, param)
			b.fi.Symbols = append(b.fi.Symbols, param)
		}
	}

	// 函数体内声明
	count := node.NamedChildCount()
	for i := uint(0); i < count; i++ {
		child := node.NamedChild(i)
		if child == nil || child.Id() == node.Id() {
			continue
		}
		switch child.Kind() {
		case "define":
			sym.Children = append(sym.Children, b.collectDefines(child, ScopeFunc, displayName)...)
		case "constant":
			if s := b.makeNamed(child, "name", KindConstant, ScopeFunc, displayName, ""); s != nil {
				sym.Children = append(sym.Children, s)
			}
		case "type":
			if s := b.makeTypeDecl(child, ScopeFunc, displayName); s != nil {
				sym.Children = append(sym.Children, s)
			}
		}
	}

	b.fi.Symbols = append(b.fi.Symbols, sym)
	b.fi.TopLevel = append(b.fi.TopLevel, sym)
}

// addDefines 为模块级 DEFINE 建符号。
//
// collectDefines 已把符号追加到 fi.Symbols，这里只补 TopLevel。
func (b *builder) addDefines(node *ts.Node, scope Scope, container string) {
	syms := b.collectDefines(node, scope, container)
	b.fi.TopLevel = append(b.fi.TopLevel, syms...)
}

// collectDefines 解析一条 DEFINE 语句（可声明多个变量）。
func (b *builder) collectDefines(node *ts.Node, scope Scope, container string) []*Symbol {
	var out []*Symbol

	names := b.fieldChildren(node, "name")
	types := b.fieldChildren(node, "types")
	// 注意：**不能**用 b.typeText(node) 取整条 DEFINE 的类型文本。
	//
	// 一条 DEFINE 用未命名子节点混排多个"名字 + 类型"，例如
	//   DEFINE
	//       rvu  RECORD LIKE rvu_file.*,
	//       rvv  RECORD LIKE rvv_file.*,
	//       sr   RECORD ... END RECORD
	// 此时 node 的第 1 个 types 子节点是 rvu 的，若把它当整条语句的类型，
	// 三个变量都会拿到 "RECORD LIKE rvu_file.*"，且 names[i]↔types[i]
	// 的配对会因两个切片长度不等而错位（sr 的字段挂到 rvu 上）。
	// 实测 aapr105.4gl / s_aapt110_gl.4gl 因此产生大量误报。
	//
	// 单个名字后只有一种类型时，types 长度与 names 一致，逐项配对即可；
	// 长度不一致说明语法结构与预期不同，此时宁可不给类型（让上层跳过
	// 语义校验），也不要给出错误的类型。

	for i, nameNode := range names {
		name := b.text(nameNode)
		if name == "" {
			continue
		}
		sym := &Symbol{
			Name:      name,
			Lower:     strings.ToLower(name),
			Kind:      KindDefine,
			Scope:     scope,
			Parent:    container,
			Container: container,
			StartByte: nameNode.StartByte(),
			EndByte:   nameNode.EndByte(),
			DeclStart: node.StartByte(),
			DeclEnd:   node.EndByte(),
		}

		// 逐项配对类型节点。长度不一致时不做配对（见上方注释）。
		if len(types) == len(names) && i < len(types) {
			if rec := findChild(types[i], "record"); rec != nil {
				sym.Kind = KindRecord
				sym.Children = b.collectRecordFields(rec, name)
				sym.TypeText = b.typeText(types[i])
			} else {
				sym.TypeText = b.typeText(types[i])
			}
		}

		out = append(out, sym)
		b.fi.Symbols = append(b.fi.Symbols, sym)
	}
	return out
}

// collectRecordFields 收集 RECORD 内的字段。
func (b *builder) collectRecordFields(rec *ts.Node, parent string) []*Symbol {
	var out []*Symbol

	names := b.fieldChildren(rec, "name")
	types := b.fieldChildren(rec, "types")

	for i, nameNode := range names {
		name := b.text(nameNode)
		if name == "" {
			continue
		}
		sym := &Symbol{
			Name:      name,
			Lower:     strings.ToLower(name),
			Kind:      KindField,
			Scope:     ScopeFunc,
			Parent:    parent,
			Container: parent,
			StartByte: nameNode.StartByte(),
			EndByte:   nameNode.EndByte(),
			DeclStart: nameNode.StartByte(),
			DeclEnd:   nameNode.EndByte(),
		}
		if i < len(types) {
			sym.TypeText = b.typeText(types[i])
			if nested := findChild(types[i], "record"); nested != nil {
				sym.Kind = KindRecord
				sym.Children = b.collectRecordFields(nested, name)
			}
		}
		out = append(out, sym)
		b.fi.Symbols = append(b.fi.Symbols, sym)
	}
	return out
}

// addTypeDecl 为 TYPE 声明建符号；内部是 RECORD 时识别为记录并收集字段。
func (b *builder) addTypeDecl(node *ts.Node, scope Scope, container string) {
	if s := b.makeTypeDecl(node, scope, container); s != nil {
		b.fi.Symbols = append(b.fi.Symbols, s)
		b.fi.TopLevel = append(b.fi.TopLevel, s)
	}
}

func (b *builder) makeTypeDecl(node *ts.Node, scope Scope, container string) *Symbol {
	sym := b.makeNamed(node, "name", KindType, scope, container, b.typeText(node))
	if sym == nil {
		return nil
	}
	if rec := b.recordOf(node); rec != nil {
		sym.Kind = KindRecord
		sym.Children = b.collectRecordFields(rec, sym.Name)
	}
	return sym
}

// recordOf 返回声明里的 RECORD 节点（若有）。
func (b *builder) recordOf(node *ts.Node) *ts.Node {
	types := node.ChildByFieldName("types")
	if types == nil {
		return nil
	}
	return findChild(types, "record")
}

// addNamed 为单个具名节点建符号。
func (b *builder) addNamed(node *ts.Node, field string, kind Kind, scope Scope, container, typeText string) {
	if s := b.makeNamed(node, field, kind, scope, container, typeText); s != nil {
		b.fi.Symbols = append(b.fi.Symbols, s)
		b.fi.TopLevel = append(b.fi.TopLevel, s)
	}
}

func (b *builder) makeNamed(node *ts.Node, field string, kind Kind, scope Scope, container, typeText string) *Symbol {
	nameNode := node.ChildByFieldName(field)
	if nameNode == nil {
		return nil
	}
	name := b.text(nameNode)
	if name == "" {
		return nil
	}
	return &Symbol{
		Name:      name,
		Lower:     strings.ToLower(name),
		Kind:      kind,
		Scope:     scope,
		Parent:    container,
		Container: container,
		StartByte: nameNode.StartByte(),
		EndByte:   nameNode.EndByte(),
		DeclStart: node.StartByte(),
		DeclEnd:   node.EndByte(),
		TypeText:  typeText,
	}
}

// functionSignature 生成 "FUNCTION name(a, b)" 形式的签名。
func (b *builder) functionSignature(node *ts.Node, keyword string) string {
	name := b.fieldText(node, "name")
	if name == "" {
		return ""
	}
	var params []string
	for _, p := range b.fieldChildren(node, "params") {
		if t := b.text(p); t != "" {
			params = append(params, t)
		}
	}
	return keyword + " " + name + "(" + strings.Join(params, ", ") + ")"
}

// typeText 提取声明的类型文本。
//
// node 可以是 declare/define/type/record 等“含 types 字段”的节点，
// 也可以直接是 types 节点本身（调用方已定位到 types）。
func (b *builder) typeText(node *ts.Node) string {
	if node == nil {
		return ""
	}
	if node.Kind() == "types" {
		return normalizeSpace(b.text(node))
	}
	if child := node.ChildByFieldName("types"); child != nil {
		return normalizeSpace(b.text(child))
	}
	if child := findChild(node, "types"); child != nil {
		return normalizeSpace(b.text(child))
	}
	return ""
}

func (b *builder) fieldText(node *ts.Node, field string) string {
	return b.text(node.ChildByFieldName(field))
}

func (b *builder) text(node *ts.Node) string {
	return parser.TextOf(node, b.src)
}

// findChild 在直接子节点中查找指定类型。
func findChild(node *ts.Node, kind string) *ts.Node {
	if node == nil {
		return nil
	}
	count := node.NamedChildCount()
	for i := uint(0); i < count; i++ {
		child := node.NamedChild(i)
		if child != nil && child.Kind() == kind {
			return child
		}
	}
	return nil
}

// normalizeSpace 把连续空白压成单个空格，并把换行视为空格。
func normalizeSpace(s string) string {
	var sb strings.Builder
	sb.Grow(len(s))
	space := false
	for _, r := range s {
		if r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == '\u3000' {
			space = true
			continue
		}
		if space && sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		space = false
		sb.WriteRune(r)
	}
	return sb.String()
}
