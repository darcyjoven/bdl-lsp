// recordname.go 校验 `BY NAME rec.member` 里的记录成员是否存在（P6 最后一项）。
//
// 语料实测（tiptop-gp，7699 个 .4gl）：
//
//	BY NAME 总计 61025 处，其中
//	  DISPLAY BY NAME   51267   ← 绝大多数是显式成员列表
//	  INPUT BY NAME      5474
//	  CONSTRUCT BY NAME  4456
//	  BY NAME rec.*        507   ← 只有 0.8%
//
// 两种形态的结局完全不同：
//
//  1. `rec.a, rec.b`（显式成员）：语法树里每个成员是
//     `(object object: (variable) property: (identifier))`，可逐个校验。
//
//  2. `rec.*`（通配）：**语法树里没有 property 节点**——`*` 被 grammar
//     当作通配吞掉。这恰好说明它不需要逐个校验：它本来就"全都要"。
//
// **默认关闭**（`bdl.diagnostics.record-member-unknown`，默认 off）。
// 记录成员静态上只能靠 TYPE..END RECORD 的字段列表，而语料里
// `DEFINE r LIKE table.*`（按数据库列展开）很常见，硬校验会大面积误报。
// 按 PLAN §7.2 与风险表的决定：先默认关闭，由用户开启并观察。
//
// 作用域（两条实测踩坑，都是 P3b 同一个陷阱的变体）：
//
//   - **按函数隔离**：同名变量在不同函数里可能是不同类型。
//     实测 aapg111.4gl：`FUNCTION g111()` 里有内联 `DEFINE sr1 RECORD`
//     （含 npq02/npp02），而 `FUNCTION aapg111_grdata()` 里有
//     `DEFINE sr1 sr1_t`（不含 npq02）。文件级平坦映射会把 g111 里的
//     `sr1.npp02` 判成"sr1_t 没有 npp02"——纯误报。
//   - **内联 RECORD 是它自己的类型**：`DEFINE x RECORD ... END RECORD`
//     的成员就挂在符号的 Children 上，不该再去按名字找 TYPE 声明。
package server

import (
	"fmt"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/diag"
	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/index"
	ts "github.com/tree-sitter/go-tree-sitter"
	"go.lsp.dev/protocol"
)

// recordMemberIssue 是一条记录成员问题。
type recordMemberIssue struct {
	// Member 是出问题的成员名。
	Member string
	// Record 是记录变量名。
	Record string
	// RecordType 是记录类型描述（TYPE 名或 "RECORD"）。
	RecordType string
	// Start / End 是成员名的字节区间。
	Start, End uint
}

// recordScope 是"某个函数作用域内"的记录变量视图。
type recordScope struct {
	// vars 是变量名 → 成员集合。不在 map 里表示"类型未知，不校验"。
	vars map[string]map[string]bool
	// types 是变量名 → 类型描述（仅用于诊断消息）。
	types map[string]string
}

// checkRecordMembers 校验 `BY NAME rec.member` 的成员是否存在。
//
// 只报告能确定记录类型、且类型是 RECORD 声明（TYPE 或内联）的场合。
// 类型未知（LIKE table.*、函数返回、动态）时静默跳过——宁可不报不误报。
func (s *Server) checkRecordMembers(doc *document.Document) []recordMemberIssue {
	text, tree, _ := doc.Snapshot()
	if tree == nil || len(text) == 0 {
		return nil
	}
	fi, _ := s.indexFor(doc)
	if fi == nil {
		return nil
	}

	scopes := s.buildRecordScopes(fi)
	containerOf := containerLookup(fi)

	var issues []recordMemberIssue
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		if n == nil {
			return
		}
		if n.Kind() == "object" {
			if iss := recordMemberAt(n, text, scopes, containerOf); iss != nil {
				issues = append(issues, *iss)
			}
		}
		c := n.ChildCount()
		for i := uint(0); i < c; i++ {
			walk(n.Child(i))
		}
	}
	walk(tree.RootNode())
	return issues
}

// membersOf 从记录符号的 Children 提取"可直接校验"的成员集合。
//
// 第二个返回值 hasOpaque 表示存在**静态看不到内部字段**的子记录组
// （`apa RECORD LIKE apa_file.*`）。这种情况下整条记录都不能校验：
// 用户写的 `sr.apb.apb26` 里的 `apb` 是子记录，其成员在数据库 schema 里，
// 索引里只有 `apa`/`type` 这些第一层名字。硬校验会把所有
// `sr.<子记录>.<字段>` 用法误报成"没有成员 apb"——实测占全部发现的
// 绝大多数（aapg111.4gl 的 sr.apb 反复误报）。
func (s *Server) membersOf(sym *index.Symbol) (members map[string]bool, hasOpaque bool) {
	members = map[string]bool{}
	// `DEFINE r RECORD LIKE table.*`：字段全部来自数据库 schema。
	// 索引里这种记录 Children 为空、TypeText 以 "RECORD LIKE" 开头。
	// 实测 aapr105.4gl 的 `rvu RECORD LIKE rvu_file.*`：不识别会把它当成
	// "零成员的记录"，于是所有 rvu.xxx 用法全报错——纯误报。
	//
	// P6：schema 索引建好后，这类记录可以**真正校验**（语料里 21144 处
	// `LIKE t.*` 声明，是记录成员用得最多的地方）。表找不到时退回不校验。
	if len(sym.Children) == 0 {
		if cols := s.recordMembersOfType(sym.TypeText); len(cols) > 0 {
			return cols, false
		}
		return members, true
	}
	// 防御性校验：内联 RECORD 的 TypeText 必然以 "RECORD" 开头。
	// 若不是，说明 index 在"一条 DEFINE 声明多个变量、类型各异"时
	// 把名字与类型节点错配了（实测 aapr105.4gl:
	//   rvu RECORD LIKE rvu_file.*,
	//   rvv RECORD LIKE rvv_file.*,
	//   sr  RECORD ... END RECORD
	// 中 rvu 拿到了 sr 的字段，TypeText 却是另一个变量的 "LIKE type_file.chr20"）。
	// 这种符号可信度不足，按不可校验处理——否则会拿别人的字段去校验，
	// 产生成片误报。
	if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sym.TypeText)), "RECORD") {
		return members, true
	}
	for _, c := range sym.Children {
		switch c.Kind {
		case index.KindField:
			if c.Lower != "" {
				members[c.Lower] = true
			}
		case index.KindRecord:
			// 子记录组：组名本身是合法成员，但组内部字段要看它自己的类型。
			// 目前 3 层访问（`rec.group.field`）在 recordMemberAt 里本来就
			// 解析不到变量（`lastIdentRun` 取到的是组名），因此不校验；
			// 这里保持"组名可见 + 整条记录不校验"的旧行为。
			if c.Lower != "" {
				members[c.Lower] = true
			}
			if len(s.recordMembersOfType(c.TypeText)) == 0 {
				hasOpaque = true
			}
		}
	}
	return members, hasOpaque
}

// buildRecordScopes 按函数容器建立"变量 → 成员集合"的映射。
//
// 容器名用符号的 Container 字段（顶层为空串）——这是 index 已经维护好的
// 归属信息，不需要重新遍历语法树。
func (s *Server) buildRecordScopes(fi *index.FileIndex) map[string]*recordScope {
	// scopeKey 给成员表加容器前缀。
	//
	// 只用符号名做 key 会让不同作用域的同名记录互相覆盖——
	// 实测 s_aapt110_gl.4gl 里 t110_g_gl_1 的 `l_api RECORD LIKE api_file.*`
	// （不可校验）被模块级同名 `l_api`（20 个可见成员）覆盖，
	// 于是 61 处 l_api.api041 全被误报。
	scopeKey := func(container, name string) string { return container + "\x00" + name }

	// 先收具名类型（TYPE x RECORD）的成员。
	typeMembers := map[string]map[string]bool{}
	for _, sym := range fi.Symbols {
		if sym.Kind != index.KindRecord || len(sym.Children) == 0 {
			continue
		}
		members, opaque := s.membersOf(sym)
		if opaque {
			// 含子记录组 → 静态无法完整校验，整条跳过（见 membersOf 注释）
			continue
		}
		// TYPE 声明与内联 DEFINE 都进这张表：内联 DEFINE 的符号名是变量名，
		// 不会被别的 DEFINE 的 TypeText 引用，因此不会互相干扰。
		typeMembers[scopeKey(sym.Container, sym.Lower)] = members
	}

	scopes := map[string]*recordScope{}
	get := func(container string) *recordScope {
		sc := scopes[container]
		if sc == nil {
			sc = &recordScope{
				vars:  map[string]map[string]bool{},
				types: map[string]string{},
			}
			scopes[container] = sc
		}
		return sc
	}

	for _, sym := range fi.Symbols {
		switch sym.Kind {
		case index.KindRecord:
			// ARRAY / DYNAMIC ARRAY 上的 `.clear()` / `.deleteElement()` /
			// `.appendElement()` / `.getLength()` 是**数组方法**，不是记录成员。
			// 实测 abxt800.4gl：`g_bxj DYNAMIC ARRAY OF RECORD ...` 被
			// `CALL g_bxj.clear()` 调用，若当作记录成员校验会误报。
			// 数组元素的成员访问在 Genero 里写作 `g_bxj[i].field`，
			// 语法树上是下标表达式而非 object，本函数不会碰到。
			// 注意：不能靠 TypeText 判断数组——`DYNAMIC ARRAY OF RECORD`
			// 的 TypeText 是 "RECORD ..."（ARRAY 关键字不在其中）。
			// 索引对数组不保留该信息，因此这里用一个保守规则：
			// 数组方法名（clear/appendElement/...）本身就说明是数组用法，
			// 在 recordMemberAt 里按"成员名是内置数组方法"排除。

			// 内联 `DEFINE x RECORD ... END RECORD`：成员就是 Children，
			// 不再按名字去查 TYPE 声明。
			members, opaque := s.membersOf(sym)
			sc := get(sym.Container)
			if opaque || len(members) == 0 {
				// 记录存在但静态不可校验。必须**记下这个 key**（nil），
				// 否则作用域里没有它 → 查找回退到模块级 → 可能撞上
				// 别的同名变量，拿错误的成员集合去校验。
				sc.vars[sym.Lower] = nil
				sc.types[sym.Lower] = "RECORD（成员静态不可见）"
			} else {
				sc.vars[sym.Lower] = members
				sc.types[sym.Lower] = "RECORD"
			}

		case index.KindParameter:
			// 参数会**遮蔽**外层同名变量，且自身类型静态不可知
			// （成员取决于调用方传入的记录）。
			//
			// 必须**无条件**覆盖：符号表按出现顺序遍历，函数体内的
			// `DEFINE rvu RECORD LIKE rvu_file.*` 可能先于参数被加入，
			// 若只在"不存在时"写入，参数就挡不住它。
			// 实测 aapr105.4gl：REPORT 参数 rvu 与 FUNCTION 里的内联 rvu
			// 同名，混用会误报。
			//
			// nil 表示"已知但不可校验"（区别于"未知"）。
			if sym.Lower == "" {
				continue
			}
			sc := get(sym.Container)
			sc.vars[sym.Lower] = nil
			sc.types[sym.Lower] = "参数"

		case index.KindDefine:
			typ := strings.ToLower(strings.TrimSpace(sym.TypeText))
			if typ == "" {
				continue
			}
			sc := get(sym.Container)
			sc.types[sym.Lower] = sym.TypeText
			// 类型可能声明在同一容器（TYPE 通常模块级）或模块级。
			if members, ok := typeMembers[scopeKey(sym.Container, typ)]; ok {
				sc.vars[sym.Lower] = members
			} else if members, ok := typeMembers[scopeKey("", typ)]; ok {
				sc.vars[sym.Lower] = members
			}
			// 类型不是本地 RECORD 声明（LIKE table.*、外部类型等）：
			// 不写 vars，校验时自然跳过。
		}
	}
	return scopes
}

// containerLookup 返回"字节偏移 → 容器名"的查找函数。
//
// 容器来自 index 里的 function/main/report 符号（DeclStart..DeclEnd）。
// 取**最内层**（区间最小）的容器：report 里可能嵌套 report 小节。
func containerLookup(fi *index.FileIndex) func(pos uint) string {
	type span struct {
		start, end uint
		name       string
	}
	var spans []span
	for _, sym := range fi.Symbols {
		switch sym.Kind {
		case index.KindFunction, index.KindMain, index.KindReport:
			if sym.DeclEnd > sym.DeclStart {
				spans = append(spans, span{sym.DeclStart, sym.DeclEnd, sym.Name})
			}
		}
	}
	return func(pos uint) string {
		best := ""
		bestLen := ^uint(0)
		for _, sp := range spans {
			if pos >= sp.start && pos < sp.end {
				if l := sp.end - sp.start; l < bestLen {
					bestLen = l
					best = sp.name
				}
			}
		}
		return best
	}
}

// recordMemberAt 检查单个 `object` 节点（形如 `rec.member`）。
func recordMemberAt(n *ts.Node, src []byte, scopes map[string]*recordScope,
	containerOf func(uint) string) *recordMemberIssue {

	obj := n.ChildByFieldName("object")
	prop := n.ChildByFieldName("property")
	if obj == nil || prop == nil {
		// `rec.*`：property 为空，通配形态无需逐个校验。
		return nil
	}
	recName, ok := recordObjectName(string(src[obj.StartByte():clampEnd(obj.EndByte(), len(src))]))
	if !ok {
		// 多级限定（`a.b.c`）：左侧不是简单变量，成员归属无从谈起。
		// 实测 cbmi100.4gl 的 `com.darcy.Hello.newHello(...)`：左侧是外部类
		// 限定名，最后一段 `darcy` 恰好与一个记录变量同名，按旧逻辑（取
		// 最后一段标识符）会把 `Hello` 判成 darcy 的成员——纯误报。
		return nil
	}
	memberName := strings.TrimSpace(string(src[prop.StartByte():clampEnd(prop.EndByte(), len(src))]))
	if recName == "" || memberName == "" {
		return nil
	}

	// 作用域查找：先看使用处所在函数，再看模块级（顶层声明对函数内可见）。
	//
	// 为什么需要两级：模块级 `DEFINE tm t_rec` 的 Container 是 ""，
	// 而它在 MAIN 里可用。只看函数作用域会漏掉绝大多数真实用法。
	// 函数内同名声明优先——这正是"同名变量在不同函数是不同类型"
	// 那个坑的修法。
	container := containerOf(n.StartByte())
	// 数组/集合的内置方法不是记录成员。`DYNAMIC ARRAY OF RECORD` 的
	// TypeText 里看不到 ARRAY（索引只留 "RECORD ..."），只能靠方法名识别。
	// 实测 abxt800.4gl：`CALL g_bxj.clear()` / `.deleteElement()`。
	if isBuiltinMemberMethod(memberName) {
		return nil
	}

	members, known := lookupRecord(scopes, container, recName)
	if !known || members == nil {
		// 未知或已知但不可校验（参数、RECORD LIKE table.* 等）→ 不校验
		return nil
	}
	if members[strings.ToLower(memberName)] {
		return nil
	}
	return &recordMemberIssue{
		Member:     memberName,
		Record:     recName,
		RecordType: lookupRecordType(scopes, container, recName),
		Start:      prop.StartByte(),
		End:        prop.EndByte(),
	}
}

// recordObjectName 从 `object` 节点的源码文本里取**简单变量名**。
//
// 接受：`rec`、`g_bmb[l_ac]`（数组元素）、`rec [1]`。
// 拒绝：任何含 `.` 的形态（`sr.apb`、`com.darcy.Hello`）——多级访问里的
// 最后一段不是变量名，硬按变量名查只会误报（见调用处的注释）。
func recordObjectName(text string) (string, bool) {
	// 去掉所有成对的下标：`g_bmb[l_ac]` → `g_bmb`
	var b strings.Builder
	depth := 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '[':
			depth++
		case ']':
			if depth > 0 {
				depth--
			}
		default:
			if depth == 0 {
				b.WriteByte(text[i])
			}
		}
	}
	name := strings.TrimSpace(b.String())
	if name == "" {
		return "", false
	}
	for i := 0; i < len(name); i++ {
		if !isWordByte(name[i]) {
			return "", false // `.`、空格、调用括号等都不算简单变量
		}
	}
	return strings.ToLower(name), true
}

// lookupRecordType 返回记录变量的类型描述（用于诊断消息）。
func lookupRecordType(scopes map[string]*recordScope, container, varName string) string {
	if sc := scopes[container]; sc != nil {
		if t, ok := sc.types[varName]; ok {
			return t
		}
	}
	if sc := scopes[""]; sc != nil {
		return sc.types[varName]
	}
	return ""
}

// lookupRecord 按"函数作用域 → 模块级"的顺序查找记录变量的成员集合。
func lookupRecord(scopes map[string]*recordScope, container, varName string) (map[string]bool, bool) {
	if sc := scopes[container]; sc != nil {
		if m, ok := sc.vars[varName]; ok {
			return m, true
		}
	}
	if container != "" {
		if sc := scopes[""]; sc != nil {
			if m, ok := sc.vars[varName]; ok {
				return m, true
			}
		}
	}
	return nil, false
}

// isBuiltinMemberMethod 判断成员名是否是 Genero 的内置集合/数组方法。
//
// 这些名字出现在 `rec.xxx` 形态里时不是记录字段，而是对数组/字典/字符串
// 等值的方法调用。校验时一律放过。
func isBuiltinMemberMethod(name string) bool {
	switch strings.ToLower(name) {
	case "clear", "appendelement", "insertelement", "deleteelement",
		"getlength", "getelement", "setelement", "getindex",
		"setkey", "getkey", "haskey", "keys", "values", "remove",
		"add", "length", "substring", "toupper", "tolower", "trim":
		return true
	}
	return false
}

// clampEnd 把节点结束偏移夹到文本长度内。
func clampEnd(end uint, n int) uint {
	if int(end) > n {
		return uint(n)
	}
	return end
}

// lastIdentRun 取字符串里最后一段标识符字符（用于从 `(variable (identifier))`
// 这类 S-expr 片段里抠出名字）。
func lastIdentRun(s string) string {
	end := -1
	start := -1
	for i := len(s) - 1; i >= 0; i-- {
		c := s[i]
		if isIdentByte(c) {
			if end < 0 {
				end = i + 1
			}
			start = i
			continue
		}
		if end >= 0 {
			break
		}
	}
	if start < 0 || end < 0 {
		return ""
	}
	return s[start:end]
}

// recordMemberChecks 把记录成员问题变成诊断（受配置开关控制，默认 off）。
func (s *Server) recordMemberChecks(doc *document.Document, issues *[]protocol.Diagnostic) {
	if doc == nil || doc.Lang != "bdl" {
		return
	}
	settings := s.settings()
	severity, enabled := diag.SeverityFor(diag.CodeRecordMemberUnknown, settings)
	if !enabled {
		return
	}
	idx := doc.Index()
	for _, iss := range s.checkRecordMembers(doc) {
		*issues = append(*issues, protocol.Diagnostic{
			Range:    byteRange(idx, iss.Start, iss.End),
			Severity: severity,
			Source:   "bdl-lsp",
			Code:     string(diag.CodeRecordMemberUnknown),
			Message: fmt.Sprintf("记录 %q（%s）没有成员 %q（已检查 RECORD 声明的字段）",
				iss.Record, iss.RecordType, iss.Member),
		})
	}
}
