// Package index 从语法树提取符号，供符号列表、跳转与悬停使用。
//
// 设计原则：
//   - 只做“能从语法树确定的”信息，不做类型推导；
//   - 符号位置统一用字节偏移，行号/列由上层按需换算；
//   - 同名符号可能在不同作用域重复，用 Scope 区分。
package index

import (
	"strings"
	"sync"

	"github.com/darcyjoven/bdl-lsp/parser"
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
	// KindPrepare 是 PREPARE p FROM <sql> 声明的 **prepare statement**。
	//
	// 注意：grammar 里 prepare/execute 用的字段名也叫 `cursor`
	// （见 tree-sitter-bdl 的 sql.js `_cid`），但语义上是 prepared statement，
	// 与 DECLARE CURSOR 的游标是两个命名空间的东西。实现必须按**父节点类型**
	// 区分，不能按字段名区分。
	KindPrepare Kind = "prepare"
	// KindCursor 是 DECLARE c CURSOR FOR ... 声明的游标。
	KindCursor Kind = "cursor"
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
	//
	// **已规范化**：连续空白（含换行）压成单个空格。语义判断（前缀匹配、
	// 相等比较）用它——那些地方换行没有意义，规范化后更容易判断。
	//
	// 需要展示给用户时（悬停）请用 TypeTextRaw，否则多行 RECORD 会被挤成一行。
	TypeText string
	// TypeTextRaw 与 TypeText 内容相同但**保留源码换行与缩进**。
	//
	// 用途：悬停展示。多行 RECORD 定义挤成一行后完全无法阅读
	// （实测 20+ 字段的 RECORD 会变成几百字符的单一长行）。
	// 语义判断不要用它——换行/缩进会让前缀匹配和比较变得脆弱。
	TypeTextRaw string
	// Signature 是函数/报表的签名文本，仅当 Kind 为 function/report 时填充。
	Signature string
	// RefName 是这个符号声明时**引用的另一个符号名**，供展示与解析使用。
	//
	// 目前只有游标用到：`DECLARE c CURSOR FOR p_q` 的 RefName 是 p_q
	// （来源是 prepare statement，名字与游标处在不同命名空间）。
	// 来源是静态 SQL（FOR SELECT ...）或 SQL 字符串（FROM l_sql）时为空。
	//
	// 之所以要在这里记下来：DECLARE 的两个 `cursor:` 字段在语法树里
	// **字段名完全相同**，靠出现顺序区分（第一个是游标名，第二个才是来源）。
	// 顺序信息在这里拿最自然，散到服务器层再解析一遍语法树容易写错。
	//
	// 注意：光标落在 `FOR p_q` 上时不需要它——那时 identAt 拿到的就是 p_q
	// 本身，按普通作用域查找就能命中 prepare 符号。RefName 是用来说明
	// "这个游标绑的是哪条 prepare"。
	RefName string
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
	// FormFieldNames 是 ATTRIBUTES 里每个 item 的**字段名**（`=` 后面的
	// `formonly.more` / `alh_file.alh01` 的列部分）。SCREEN RECORD 引用的是
	// 字段名而不是 item tag，两者在 `Edit mor=formonly.more` 这种写法里
	// 并不相同——不收集这一项会把合法引用报成未定义。
	// 仅 per 语言有内容。
	FormFieldNames []LayoutRef
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

	b.forEachChild(root, func(child *ts.Node) {
		b.visitTop(child, "")
	})
	// P7-B-5：PREPARE / DECLARE CURSOR。放在 visitTop 之后统一收集，
	// 因为它们是可执行语句、能嵌在 IF/FOREACH 等任意深度（见 collectSQLStatements）。
	b.collectSQLStatements(root, tree.Language())
	return fi
}

// namedChildBulkThreshold 是"改用批量取子节点"的子节点数阈值。
//
// 两种取法各有代价，交叉点由实测决定（40 个真实 .4gl，见下表）：
//
//   - 逐个索引 NamedChild(i)：1 次 cgo 调用/孩子，但 tree-sitter 内部
//     要**从头迭代到第 i 个**（node.c 的 ts_node__child），所以取 n 个
//     孩子的 C 侧工作量是 O(n²)；
//
//   - NamedChildren(cursor)：一次成型，但每个孩子 3 次 cgo 调用
//     （Node + IsNamed + GotoNextSibling）。
//
//     cgo 调用约 230ns/次，是绝对大头；C 侧步进几乎免费。因此小 n 时
//     索引胜（cgo 少），大 n 时平方项压过 cgo 差，批量胜：
//
//     孩子数   逐个索引    批量      胜者
//     14      3.1µs     6.8µs     索引
//     54     19.4µs    25.1µs     索引
//     64     31.6µs    29.1µs     批量
//     163    172.6µs    72.5µs     批量（2.4 倍）
//     204    306.8µs    87.6µs     批量（3.5 倍）
//
// 顶层 root 在实码里常有上百个孩子（大量模块级 DEFINE + 几十个函数），
// 所以这一档最值钱。取 64：交叉点约在 55，64 已明确越过噪声。
const namedChildBulkThreshold = 64

// forEachChild 遍历 node 的具名子节点，按孩子数量自动选最优取法。
//
// fn 里可以安全调用需要 b.cursor 的辅助函数（如 fieldChildren）：
// 批量分支先把孩子取进切片，此后不再依赖游标状态。
func (b *builder) forEachChild(node *ts.Node, fn func(child *ts.Node)) {
	count := node.NamedChildCount()
	if count >= namedChildBulkThreshold {
		kids := node.NamedChildren(b.cursor)
		for i := range kids {
			fn(&kids[i])
		}
		return
	}
	for i := uint(0); i < count; i++ {
		if child := node.NamedChild(i); child != nil {
			fn(child)
		}
	}
}

// sqlStmtKinds 把种类名预编译成**符号 id**。
//
// 为什么不直接比字符串：Node.Kind() 每次调用都走 C.GoString 分配一个
// Go 字符串。定位语句时每个候选点都要问一次类型，累计开销可观。
// Node.KindId() 返回 uint16，零分配。
type sqlStmtKinds struct {
	prepare uint16
	declare uint16
}

// sqlStmtKindsOnce 缓存预编译结果。
//
// 符号 id 由编译期固定的 grammar 决定，同一个二进制里所有 Pool 的 BDL
// Language 包的都是同一份 C grammar，所以进程内缓存一份是安全的。
var (
	sqlStmtKindsOnce  sync.Once
	sqlStmtKindsCache *sqlStmtKinds
)

// sqlStmtKindsFor 预编译并缓存符号 id。
func sqlStmtKindsFor(lang *ts.Language) *sqlStmtKinds {
	sqlStmtKindsOnce.Do(func() {
		sqlStmtKindsCache = &sqlStmtKinds{
			prepare: lang.IdForNodeKind("prepare", true),
			declare: lang.IdForNodeKind("declare", true),
		}
	})
	return sqlStmtKindsCache
}

// sqlKeywordLen 是 PREPARE / DECLARE 的字面长度。
const sqlKeywordLen = 7

// collectSQLStatements 收集整个文件里的 PREPARE / DECLARE CURSOR 语句。
//
// 做法：**先在源码字节里找出关键字出现的位置，再让 tree-sitter 定位到
// 对应节点**（root.DescendantForByteRange 只沿树下降一次，O(深度)）。
//
// 为什么不是"遍历语法树找这两类节点"（前后试过两种，都因为太慢放弃）：
//   - 逐节点递归整棵树：NamedChild(i) 在 tree-sitter 里是 O(i)
//     （ts_node__child 每次都从头迭代子节点），且每条语句都要问一次类型，
//     光这一项就让 Build 翻倍；
//   - tree-sitter query：由 C 侧扫描，实测约 230ns/节点，10678 节点的
//     文件要 2.4ms；34KB 的真实文件上 Build 从 422µs 涨到 1212µs（2.9 倍）。
//
// 现在的成本只和**关键字出现次数**成正比。附带好处：不需要维护"哪些节点
// 可能含语句"的白名单，grammar 加新的语句块也不会漏（那种白名单会把
// PREPARE 写在 MENU/DIALOG 里的情况漏掉）。
//
// 误命中由类型检查兜住：字符串/注释里的 "prepare" 会被定位到 string/comment
// 节点，走父链也到不了 prepare 语句，直接丢弃。
func (b *builder) collectSQLStatements(root *ts.Node, lang *ts.Language) {
	// PER grammar 里没有 prepare/declare 节点，不用走。
	if b.fi.Lang != parser.LangBDL {
		return
	}
	kinds := sqlStmtKindsFor(lang)

	// 容器用于确定语句归属（作用域）。
	var containers []*Symbol
	for _, s := range b.fi.TopLevel {
		switch s.Kind {
		case KindMain, KindFunction, KindReport:
			containers = append(containers, s)
		}
	}

	// 同一条语句可能被命中多次（例如动态 SQL 字符串里也写着 "PREPARE"），
	// 用节点 id 去重，避免收出重复符号。
	seen := make(map[uintptr]bool)
	forEachSQLKeyword(b.src, func(off int) {
		stmt := sqlStmtNode(root.DescendantForByteRange(uint(off), uint(off+sqlKeywordLen)), kinds)
		if stmt == nil || seen[stmt.Id()] {
			return
		}
		seen[stmt.Id()] = true
		switch stmt.KindId() {
		case kinds.prepare:
			b.addSQLSymbol(stmt, KindPrepare, containers)
		case kinds.declare:
			b.addSQLSymbol(stmt, KindCursor, containers)
		}
	})
}

// sqlStmtNode 从关键字处定位到的节点回溯出 prepare/declare 语句节点。
//
// 关键字本身是匿名 token，它就是语句节点的直接子节点，所以通常只要看
// 父节点；但关键字也可能落在语句内部的字符串/表达式里（动态 SQL 文本），
// 那种情况下走父链多上几层就能回到语句节点。上限 4 层：真正的关键字
// 1~2 层必中，而字符串/注释里的误命中不必一路爬到根。
func sqlStmtNode(n *ts.Node, kinds *sqlStmtKinds) *ts.Node {
	for depth, cur := 0, n; cur != nil && depth < 4; depth, cur = depth+1, cur.Parent() {
		switch cur.KindId() {
		case kinds.prepare, kinds.declare:
			return cur
		}
	}
	return nil
}

// forEachSQLKeyword 在源码里找出所有 PREPARE / DECLARE 关键字（不分大小写），
// 按出现顺序回调其起始字节。
//
// 只做字节扫描、不解析：成本约 1 字节一次比较，比语法树遍历低一个量级。
// 前一个字节是标识符字符时跳过——否则 r100_prepare1 这类名字里的
// "prepare" 会带来大量无谓的节点定位。
func forEachSQLKeyword(src []byte, fn func(off int)) {
	for i := 0; i+sqlKeywordLen <= len(src); i++ {
		// 先用首字母快速筛掉绝大多数位置（PREPARE / DECLARE 同在 p/d 开头）。
		c := src[i] | 0x20
		if c != 'p' && c != 'd' {
			continue
		}
		if i > 0 && isIdentByte(src[i-1]) {
			continue
		}
		word := src[i : i+sqlKeywordLen]
		if matchFoldASCII(word, "prepare") || matchFoldASCII(word, "declare") {
			fn(i)
		}
	}
}

// matchFoldASCII 判断字节切片是否与 ASCII 字面量大小写不敏感相等。
//
// 只对 ASCII 字母做折叠：|0x20 只动 bit 5，能撞上 'p' 的字节只有
// 'P'(0x50) 与 'p'(0x70)，不会把标点误判成字母。非 ASCII 字节
// (>0x7F) 折叠后仍 >0x7F，也不会命中。
func matchFoldASCII(b []byte, s string) bool {
	if len(b) != len(s) {
		return false
	}
	for i := 0; i < len(b); i++ {
		if b[i]|0x20 != s[i]|0x20 {
			return false
		}
	}
	return true
}

// isIdentByte 判断字节是否可出现在 Genero 标识符里（ASCII 部分）。
//
// 只用于跳过标识符内部的关键字；非 ASCII（中文变量名）不会被误判成
// 词首，但那只会多一次节点定位，结果仍然正确。
func isIdentByte(c byte) bool {
	return c == '_' ||
		(c >= 'a' && c <= 'z') ||
		(c >= 'A' && c <= 'Z') ||
		(c >= '0' && c <= '9') ||
		c >= 0x80
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
	b.forEachChild(node, func(child *ts.Node) {
		switch child.Kind() {
		case "define":
			b.addDefines(child, ScopeGlobals, "")
		case "constant":
			b.addNamed(child, "name", KindConstant, ScopeGlobals, "", "")
		case "type":
			b.addTypeDecl(child, ScopeGlobals, "")
		}
	})
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
	b.forEachChild(node, func(child *ts.Node) {
		if child.Id() == node.Id() {
			return
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
	})
	// PREPARE / DECLARE CURSOR 不在这里收：它们是可执行语句、能嵌在
	// IF/FOREACH 等任意深度，由 Build 末尾的 collectSQLStatements 统一扫。

	b.fi.Symbols = append(b.fi.Symbols, sym)
	b.fi.TopLevel = append(b.fi.TopLevel, sym)
}

// addSQLSymbol 为一个 prepare/declare 节点建符号并挂到所属容器。
//
// containers 是全文件的 main/function/report 容器列表（见
// collectSQLStatements）；语句区间落在谁的声明区间内就归谁。
func (b *builder) addSQLSymbol(node *ts.Node, kind Kind, containers []*Symbol) {
	var sym *Symbol
	if kind == KindPrepare {
		sym = b.makePrepare(node)
	} else {
		sym = b.makeCursor(node)
	}
	if sym == nil {
		return
	}
	start, end := node.StartByte(), node.EndByte()
	for _, c := range containers {
		if c.DeclStart <= start && end <= c.DeclEnd {
			sym.Scope = ScopeFunc
			sym.Parent = c.Name
			sym.Container = c.Name
			c.Children = append(c.Children, sym)
			break
		}
	}
	b.fi.Symbols = append(b.fi.Symbols, sym)
}

// makePrepare 为 `PREPARE p FROM <sql>` 建 prepare statement 符号。
//
// 字段说明（tree-sitter-bdl 的 rule/sql.js）：
//
//	prepare: seq(kw("PREPARE"), $._cid, kw("FROM"), $._sqlstring)
//	_cid     = field("cursor", $.identifier)   ← 这里的 `cursor` 字段其实是 prepare 名
//	_sqlstring = field("sql", $.expression)
//
// 字段名叫 cursor 是 grammar 复用 `_cid` 造成的，语义上不是游标。所以这里
// **不按字段名判断种类**，而是由父节点类型（prepare）决定。
func (b *builder) makePrepare(node *ts.Node) *Symbol {
	nameNode := node.ChildByFieldName("cursor")
	if nameNode == nil {
		return nil
	}
	name := b.text(nameNode)
	if name == "" {
		return nil
	}
	sym := &Symbol{
		Name:      name,
		Lower:     strings.ToLower(name),
		Kind:      KindPrepare,
		Scope:     ScopeGlobal,
		StartByte: nameNode.StartByte(),
		EndByte:   nameNode.EndByte(),
		DeclStart: node.StartByte(),
		DeclEnd:   node.EndByte(),
	}
	// SQL 来源作为类型文本：悬停时能直接看到 PREPARE 的是哪条 SQL。
	// 保留换行（动态 SQL 常由多行字符串拼接而成）。
	if sqlNode := node.ChildByFieldName("sql"); sqlNode != nil {
		sym.TypeText = normalizeSpace(b.text(sqlNode))
		sym.TypeTextRaw = normalizeIndent(b.text(sqlNode))
	}
	return sym
}

// makeCursor 为 `DECLARE c CURSOR FOR ...` 建游标符号。
//
// 字段说明（tree-sitter-bdl 的 rule/sql.js）：
//
//	declare: seq(kw("DECLARE"), $._cid, optional(kw("SCROLL")), kw("CURSOR"), ...
//	             choice(seq(kw("FOR"), choice($._static_sql, $._cid)),
//	                    seq(kw("FOR"), "(", ... , ")"),
//	                    seq(kw("FROM"), choice($._static_sql, $._sqlstring))))
//
// **`cursor:` 字段会出现两次**：
//  1. 第一个 = 游标名（DECLARE 后面那个）；
//  2. 第二个 = 来源。只有当来源是标识符（prepare statement 名）时才有，
//     来源是静态 SQL（FOR SELECT ...）或 SQL 字符串（FROM l_sql）时没有。
//
// 两者字段名完全一样，只能靠**出现顺序**区分，这就是必须在这里处理的原因：
// 顺序信息在语法树上拿最自然，散到服务器层再解析一遍容易写错。
func (b *builder) makeCursor(node *ts.Node) *Symbol {
	names := b.fieldChildren(node, "cursor")
	if len(names) == 0 {
		return nil
	}
	nameNode := names[0]
	name := b.text(nameNode)
	if name == "" {
		return nil
	}
	sym := &Symbol{
		Name:      name,
		Lower:     strings.ToLower(name),
		Kind:      KindCursor,
		Scope:     ScopeGlobal,
		StartByte: nameNode.StartByte(),
		EndByte:   nameNode.EndByte(),
		DeclStart: node.StartByte(),
		DeclEnd:   node.EndByte(),
	}
	// 第二个 cursor 字段是来源 prepare 名（静态 SQL / 字符串来源时不存在）。
	if len(names) >= 2 {
		sym.RefName = b.text(names[1])
	}
	return sym
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
			sym.TypeText = b.typeText(types[i])
			sym.TypeTextRaw = b.typeTextRaw(types[i])
			if rec := findChild(types[i], "record"); rec != nil {
				sym.Kind = KindRecord
				sym.Children = b.collectRecordFields(rec, name)
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

// typeTextRaw 与 typeText 取材相同，但只做**缩进归一**，不压换行。
//
// 用于悬停展示：多行 RECORD 语句保留原有行结构，用户看到的就是源码的样子。
// 归一化只做两件事：
//  1. 去掉每行末尾空白（否则 markdown 代码块里会拖出长尾空格）；
//  2. 把制表符换成 4 个空格（markdown 里 tab 宽度不可控）。
//
// 不去公共缩进——代码块本身有缩进更好读，且能反映真实嵌套。
func (b *builder) typeTextRaw(node *ts.Node) string {
	if node == nil {
		return ""
	}
	target := node
	if node.Kind() != "types" {
		if child := node.ChildByFieldName("types"); child != nil {
			target = child
		} else if child := findChild(node, "types"); child != nil {
			target = child
		}
	}
	return normalizeIndent(b.text(target))
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

// normalizeIndent 保留换行，只做行尾去空白与 tab→4 空格。
//
// 与 normalizeSpace 的区别：后者会把整个多行 RECORD 压成一行。
func normalizeIndent(s string) string {
	if s == "" {
		return ""
	}
	lines := strings.Split(strings.ReplaceAll(s, "\r\n", "\n"), "\n")
	for i, line := range lines {
		line = strings.ReplaceAll(line, "\t", "    ")
		lines[i] = strings.TrimRight(line, " \t\u3000")
	}
	// 去掉首尾空行（语句首尾常有多余空行）
	for len(lines) > 0 && lines[0] == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
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
