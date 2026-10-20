// check.go 从 4GL 语法树提取表单绑定与字段引用，做作用域跟踪并产出诊断。
//
// 提取用单个 tree-sitter query（多 pattern 一次扫描；match 按文档序返回，
// 实测验证过）——这是 P3a 的教训：TreeCursor 逐节点 DFS 在 7138 行文件上
// 要 74ms，query 只要 13ms。
//
// 作用域规则（PLAN §7.2）：以函数为作用域、按语句顺序维护“当前表单”；
// 无法确定绑定时不校验（宁可不报不误报）。
package form

import (
	"fmt"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
)

// queryBindingsRefs 一个 query 同时捕获表单绑定与字段引用。
//
// 绑定（改变“当前表单”）：
//
//	(window form: (expression (literal (string) @formpath)))
//	  —— OPEN WINDOW w WITH FORM "path"（语料 8209 处，主要形式）
//	(form name: (identifier) @formname form: (expression (literal (string) @formpath)))
//	  —— OPEN FORM x FROM "path"（登记名字绑定，不立即成为当前表单）
//	(display name: (identifier) @displayform)
//	  —— DISPLAY FORM <名字>（把名字对应的表单设为当前）
//
// 字段引用（校验对象）：
//
//	(branch field: (identifier) @field)
//	  —— BEFORE/AFTER FIELD f、ON CHANGE f（交互块内的字段钩子）
//	(hook field: (variable (identifier) @field))
//	  —— NEXT FIELD f
//	(input field: (variable (identifier) @field))
//	  —— INPUT ... FROM f1, f2
//	(construct field: (variable (identifier) @field))
//	  —— CONSTRUCT ... ON f1, f2
//	(display field: (variable (identifier) @field))
//	  —— DISPLAY ... TO f
//	(infield_function params: (expression (variable (identifier) @field)))
//	  —— INFIELD(f)
const queryForms = `
(window form: (expression (literal (string) @formpath)))
(form name: (identifier) @formname form: (expression (literal (string) @formpath)))
(display name: (identifier) @displayform)
(branch field: (identifier) @field)
(hook field: (variable (identifier) @field))
(input field: (variable (identifier) @field))
(construct field: (variable (identifier) @field))
(display field: (variable (identifier) @field))
(infield_function params: (expression (variable (identifier) @field)))
(function) @container
(main) @container
(report) @container
`

// queries 是惰性编译并复用的 query（同 P3a variableQueries 套路）。
type Queries struct {
	query *ts.Query
	idx   map[string]uint // capture 名 → 索引
}

// CompileQueries 编译 query（失败返回 nil，功能静默禁用）。
//
// typed-nil 教训（P3a 实测踩过）：qerr 必须与 error 接口分开声明。
func CompileQueries(pool *parser.Pool) *Queries {
	lang, err := pool.Language(parser.LangBDL)
	if err != nil {
		return nil
	}
	q, qerr := ts.NewQuery(lang, queryForms)
	if qerr != nil {
		return nil
	}
	names := []string{
		"formpath", "formname", "displayform", "field", "container",
	}
	idx := map[string]uint{}
	for _, n := range names {
		i, ok := q.CaptureIndexForName(n)
		if !ok {
			q.Close()
			return nil
		}
		idx[n] = i
	}
	return &Queries{query: q, idx: idx}
}

// Close 释放 query。
func (qs *Queries) Close() {
	if qs == nil || qs.query == nil {
		return
	}
	qs.query.Close()
}

// event 是 query 捕获流里的一条（已按文档序）。
type event struct {
	kind string // formpath / formname+path / displayform / field
	// 绑定类
	path     string // WITH FORM / OPEN FORM 的字符串字面量（去引号）
	formName string // OPEN FORM x 的名字 / DISPLAY FORM x 的名字
	// 引用类
	word       string // 字段名
	start, end uint
}

// FieldIssue 是一个字段校验问题。
type FieldIssue struct {
	// Field 是字段名。
	Field string
	// Start/End 是字段引用在 4gl 里的区间。
	Start, End uint
	// FormPath 是解析到的表单路径（RelatedLocation 用）。
	FormPath string
	// FormURI 是表单 URI。
	FormURI string
	// FormLine 是表单里“最近似位置”（提示用，不精确到字段——
	// 字段本来就不存在）。
	FormLine int
	// Literal 是 WITH FORM 的字面量。
	Literal string
	// Kind 是问题类型：field-not-in-form / form-not-found / form-ambiguous。
	Kind string
	// Message 是完整消息。
	Message string
}

// Check 在 4gl 文档上执行字段校验。
//
// 返回问题列表与本次解析成功的表单路径集合（后者供调用方登记引用边——
// 无问题的场合也要登记，.per 变更时才能刷新本文档）。
// qs 为 nil 时返回 (nil, nil)（功能禁用）。
// 树有 ERROR 节点时仍然执行——语法错误的文件里字段引用大多仍可提取
// （tree-sitter 的错误恢复保留了子树）。
func (r *Resolver) Check(tree *ts.Tree, src []byte, refPath string, qs *Queries) ([]FieldIssue, []string) {
	if tree == nil || qs == nil {
		return nil, nil
	}
	root := tree.RootNode()
	if root == nil {
		return nil, nil
	}

	// 1) 收集事件流（query match 按文档序；再按 start 排序保证确定性）
	all, containers := collectEvents(qs, root, src)
	if len(all) == 0 {
		return nil, nil
	}

	containerOf := makeContainerOf(containers)

	sortByStart(all, func(c captured) uint { return c.node.StartByte() })

	// 2) 作用域跟踪：按文档序扫描事件，维护当前表单与命名绑定
	currentForm := map[int]*Resolved{}           // 容器索引 → 当前表单
	namedForms := map[int]map[string]*Resolved{} // 容器 → OPEN FORM 名字绑定

	var issues []FieldIssue
	usedForms := map[string]bool{} // 解析成功的表单路径（登记引用边用）

	// 表单解析结果缓存（同字面量只解析一次）
	resolvedCache := map[string]*Resolved{}

	resolveForm := func(literal string) *Resolved {
		if lit, ok := resolvedCache[literal]; ok {
			return lit
		}
		res := r.Resolve(literal, refPath)
		resolvedCache[literal] = res
		return res
	}

	for _, c := range all {
		ci := containerOf(c.node.StartByte())
		switch c.name {
		case "formpath":
			literal := unquote(parser.TextOf(&c.node, src))
			if literal == "" {
				continue
			}
			// OPEN WINDOW ... WITH FORM "p"：成为**所在函数**的当前表单。
			// 作用域按函数隔离（PLAN §7.2）：整文件线性跟踪会跨函数泄漏——
			// 实测 saxmt400_slk.4gl（15 个 WITH FORM）里函数 A 的表单
			// 泄漏到函数 B 的字段上，全量语料 2.8 万条误报；隔离后归零。
			res := resolveForm(literal)
			if res != nil {
				currentForm[ci] = res
				usedForms[res.Path] = true
			}
		case "formname":
			// OPEN FORM x FROM "p" 的名字：与最近的 formpath 配对
			// （两个 capture 同属一个 match，formpath 先出现已更新状态）
			name := parser.TextOf(&c.node, src)
			if name != "" && currentForm[ci] != nil {
				if namedForms[ci] == nil {
					namedForms[ci] = map[string]*Resolved{}
				}
				namedForms[ci][strings.ToLower(name)] = currentForm[ci]
			}
		case "displayform":
			name := strings.ToLower(parser.TextOf(&c.node, src))
			var res *Resolved
			if nf := namedForms[ci]; nf != nil {
				res = nf[name]
			}
			if res == nil {
				// DISPLAY FORM <名字>：名字本身常是表单 basename
				res = resolveForm(name)
			}
			if res != nil {
				currentForm[ci] = res
				usedForms[res.Path] = true
			}
		case "field":
			word := parser.TextOf(&c.node, src)
			// 模块级（ci=-1）或函数内无表单绑定 → 不校验（无法确定）
			if word == "" || ci < 0 {
				continue
			}
			current := currentForm[ci]
			if current == nil {
				continue
			}
			if iss := checkField(current, word, c.node.StartByte(), c.node.EndByte()); iss != nil {
				issues = append(issues, *iss)
			}
		}
	}

	// 路径集合（排序稳定，便于登记与测试断言）
	paths := make([]string, 0, len(usedForms))
	for p := range usedForms {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	return issues, paths
}

// checkField 校验一个字段是否存在于表单。
func checkField(form *Resolved, word string, start, end uint) *FieldIssue {
	lower := strings.ToLower(word)
	for _, sym := range form.Index.Symbols {
		if sym.Kind == index.KindFormItem && sym.Lower == lower {
			return nil // 存在（PHANTOM 也是 KindFormItem，天然算存在）
		}
		// SCREEN RECORD 的字段（table.column 的 column 部分）也算：
		// 4gl 的 ON CHANGE 等可以引用 screen record 字段
		if sym.Kind == index.KindField && sym.Lower == lower {
			return nil
		}
	}
	return &FieldIssue{
		Field:    word,
		Start:    start,
		End:      end,
		FormPath: form.Path,
		FormURI:  form.URI,
		Literal:  form.Literal,
		Kind:     "field-not-in-form",
		Message: fmt.Sprintf("字段 %q 不在表单 %q 中（已检查 ATTRIBUTES 与 SCREEN RECORD）",
			word, form.Literal),
	}
}

// captureName 由索引查 capture 名（query.CaptureNames 每次调用有分配，缓存到 map）。
func captureName(qs *Queries, i uint32) string {
	for name, idx := range qs.idx {
		if idx == uint(i) {
			return name
		}
	}
	return ""
}

// sortByStart 把捕获按起始字节排序（稳定排序保持同位置的原顺序）。
func sortByStart[T any](all []T, start func(T) uint) {
	// 插入排序足够（事件流基本有序，只有同 pattern 内部是序的）
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && start(all[j]) < start(all[j-1]); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
}

// unquote 去掉字符串字面量的引号。
func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}

// captured 是 query 捕获流里的一条（已带 capture 名）。
type captured struct {
	name string
	node ts.Node
}

// collectEvents 一次 query 扫描收集全部捕获与函数容器（按文档序）。
func collectEvents(qs *Queries, root *ts.Node, src []byte) ([]captured, []ts.Node) {
	var all []captured
	var containers []ts.Node // function/main/report（按文档序）
	qc := ts.NewQueryCursor()
	matches := qc.Matches(qs.query, root, src)
	for m := matches.Next(); m != nil; m = matches.Next() {
		for _, c := range m.Captures {
			all = append(all, captured{name: captureName(qs, c.Index), node: c.Node})
			switch c.Node.Kind() {
			case "function", "main", "report":
				containers = append(containers, c.Node)
			}
		}
	}
	qc.Close()
	return all, containers
}

// makeContainerOf 返回"字节偏移 → 容器索引"的查找函数（-1 = 模块级）。
//
// 二分查找：容器的字节区间互不重叠且按文档序，因此可直接对 EndByte 二分。
// 作用域按函数隔离（PLAN §7.2）：整文件线性跟踪会跨函数泄漏——实测
// saxmt400_slk.4gl（15 个 WITH FORM）里函数 A 的表单泄漏到函数 B 的字段上，
// 全量语料 2.8 万条误报；按函数隔离后归零。
func makeContainerOf(containers []ts.Node) func(pos uint) int {
	return func(pos uint) int {
		lo, hi := 0, len(containers)
		for lo < hi {
			mid := (lo + hi) / 2
			if containers[mid].EndByte() <= pos {
				lo = mid + 1
			} else {
				hi = mid
			}
		}
		if lo < len(containers) && containers[lo].StartByte() <= pos && pos < containers[lo].EndByte() {
			return lo
		}
		return -1
	}
}

// FormFieldsAt 返回指定字节位置上可用的表单字段名（供补全用）。
//
// 与 Check 走完全相同的绑定跟踪逻辑，只是遇到 pos 之后的绑定就停止——
// 补全要看的是"光标处"的表单，而不是整文件跑完的最终状态。
//
// 返回字段名（按字母序去重）与表单字面量；无绑定时返回 nil。
func (r *Resolver) FormFieldsAt(tree *ts.Tree, src []byte, refPath string,
	qs *Queries, pos uint) ([]string, string) {

	if tree == nil || qs == nil {
		return nil, ""
	}
	root := tree.RootNode()
	if root == nil {
		return nil, ""
	}
	all, containers := collectEvents(qs, root, src)
	if len(all) == 0 {
		return nil, ""
	}
	containerOf := makeContainerOf(containers)
	sortByStart(all, func(c captured) uint { return c.node.StartByte() })

	currentForm := map[int]*Resolved{}
	namedForms := map[int]map[string]*Resolved{}
	var at *Resolved

	resolveCache := map[string]*Resolved{}
	resolveForm := func(literal string) *Resolved {
		if res, ok := resolveCache[literal]; ok {
			return res
		}
		res := r.Resolve(literal, refPath)
		resolveCache[literal] = res
		return res
	}

	for _, c := range all {
		// 只看光标之前的绑定：之后的 DISPLAY FORM 不能影响当前位置。
		if c.node.StartByte() > pos {
			break
		}
		ci := containerOf(c.node.StartByte())
		switch c.name {
		case "formpath":
			if literal := unquote(parser.TextOf(&c.node, src)); literal != "" {
				if res := resolveForm(literal); res != nil {
					currentForm[ci] = res
				}
			}
		case "formname":
			name := parser.TextOf(&c.node, src)
			if name != "" && currentForm[ci] != nil {
				if namedForms[ci] == nil {
					namedForms[ci] = map[string]*Resolved{}
				}
				namedForms[ci][strings.ToLower(name)] = currentForm[ci]
			}
		case "displayform":
			name := strings.ToLower(parser.TextOf(&c.node, src))
			var res *Resolved
			if nf := namedForms[ci]; nf != nil {
				res = nf[name]
			}
			if res == nil {
				res = resolveForm(name)
			}
			if res != nil {
				currentForm[ci] = res
			}
		}
	}

	// 光标所在容器决定用哪个表单。
	at = currentForm[containerOf(pos)]
	if at == nil || at.Index == nil {
		return nil, ""
	}

	seen := map[string]bool{}
	var fields []string
	for _, sym := range at.Index.Symbols {
		switch sym.Kind {
		case index.KindFormItem, index.KindField:
			if sym.Name != "" && !seen[sym.Lower] {
				seen[sym.Lower] = true
				fields = append(fields, sym.Name)
			}
		}
	}
	sort.Slice(fields, func(i, j int) bool {
		return strings.ToLower(fields[i]) < strings.ToLower(fields[j])
	})
	return fields, at.Literal
}
