// Package format 实现 4GL 的保守格式化（P4）。
//
// 用户要求（2026-10-20）：
//   - `.per` **不做任何格式化**（server 层直接拒绝，本包只处理 BDL）；
//   - `.4gl` 关键字大小写可配置：大写 / 小写 / 不变；
//   - 缩进可选 tab 或空格，且可选个数。
//
// 设计原则（沿 PLAN §7.3 的"保守"约定）：
//   - 只做两件事：关键字大小写 + 行首缩进。不增删空格、不合并/拆分行、
//     不动逗号/运算符周围的空白——那些会制造大面积 diff；
//   - 字符串、注释、预处理指令的内容绝不改写（关键字改写只作用于
//     语法树里的**匿名 token**，字符串/注释/标识符都是命名节点，
//     天然不在改写范围）；
//   - 缩进按块结构（IF/WHILE/FUNCTION/…的祖先链）计算，
//     多行块注释内部的行不改缩进；
//   - 语法树有 ERROR 时拒绝格式化（返回原文），避免在坏文件上雪上加霜；
//   - 幂等：format(format(x)) == format(x)。
package format

import (
	"bytes"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// Options 是格式化选项（已解析为最终值，auto 在 server 层消解）。
type Options struct {
	// KeywordCase："" / "unchanged" 不改；"upper" 大写；"lower" 小写。
	KeywordCase string
	// IndentUnit 是一个缩进级的字符串（"\t" 或 N 个空格）。
	IndentUnit string
}

// blockNodes 是"内部内容整体缩进一级"的节点类型。
//
// 推导自 tree-sitter-bdl 的 node-types.json 与 Zed indents.scm 的经验；
// 逐类核对过真实语料的排版（CASE/WHEN 的 WHEN 相对 CASE 缩进一级、
// ELSE 与 IF 对齐等，见 format_test.go 的用例）。
//
// 注意两种"对齐"行为的差异（容易搞错）：
//   - when/otherwise 是块节点：WHEN 行在 CASE 内缩进一级，其语句体再一级；
//   - else/catch **不是**块节点但是"回退行"：ELSE 行回到 IF 的层级，
//     其语句体与 IF 的语句体同级。
var blockNodes = map[string]bool{
	// 流程控制
	"if": true, "while": true, "for": true, "foreach": true,
	"try": true, "case": true, "when": true, "otherwise": true,
	// 容器
	"function": true, "main": true, "report": true,
	// 交互块
	"input": true, "display": true, "menu": true, "construct": true,
	"dialog": true, "prompt": true,
	// 声明块与事务
	"record": true, "globals": true, "begin": true,
	// define：单行时 startLine==本行不计（无影响）；跨行时变量
	// 列表行缩进一级（tiptop 风格：DEFINE 后变量行比 DEFINE 多一层）
	"define": true,
	// 交互块内的事件分支（BEFORE/AFTER FIELD、ON ACTION…）
	"branch": true,
	// 报表小节（OUTPUT/FORMAT/ORDER 内部再缩进一级）
	"report_output_section": true, "report_format_section": true,
	"report_order_section": true, "report_control_block": true,
}

// closerKeywords 是"行首命中时回退一级"的关键字。
//
// END 系（END IF/END MENU/END RECORD/END GLOBALS…全部以 END 开头）、
// ELSE、CATCH 回到所属块的层级。WHEN/OTHERWISE 不在此列：
// 它们相对 CASE 缩进一级（见 blockNodes 注释）。
var closerKeywords = map[string]bool{
	"END": true, "ELSE": true, "CATCH": true,
}

// Format 格式化 4GL 源码，返回新文本。
//
// ok=false 表示拒绝格式化（语法树有 ERROR），返回原文。
// 幂等：对已格式化的文本再次调用结果不变。
func Format(tree *ts.Tree, src []byte, opts Options) (string, bool) {
	if tree == nil {
		return string(src), false
	}
	root := tree.RootNode()
	if root == nil || root.HasError() {
		return string(src), false
	}

	edits := keywordEdits(root, src, opts.KeywordCase)
	edits = append(edits, indentEdits(root, src, opts.IndentUnit)...)
	if len(edits) == 0 {
		return string(src), true
	}

	sort.Slice(edits, func(i, j int) bool {
		if edits[i].start != edits[j].start {
			return edits[i].start < edits[j].start
		}
		return edits[i].end < edits[j].end
	})
	return apply(src, edits), true
}

// edit 是一处文本替换（字节区间，含头不含尾）。
type edit struct {
	start, end uint
	text       string
}

// apply 按序应用编辑（互不重叠）。
func apply(src []byte, edits []edit) string {
	var out bytes.Buffer
	out.Grow(len(src))
	var pos uint
	for _, e := range edits {
		if e.start < pos {
			continue // 防御：重叠编辑跳过（理论上不会出现）
		}
		out.Write(src[pos:e.start])
		out.WriteString(e.text)
		pos = e.end
	}
	out.Write(src[pos:])
	return out.String()
}

// ---------------------------------------------------------------------------
// 关键字大小写
// ---------------------------------------------------------------------------

// keywordEdits 收集关键字大小写改写。
//
// 只改写**匿名 token**（node.IsNamed()==false）：tree-sitter 里关键字与
// 标点是匿名 token，而标识符/字符串/注释/数字都是命名节点——所以这条
// 规则天然不会碰用户标识符与字面量内容。匿名 token 里含字母的即是
// 关键字（TRUE/FALSE 这类字面量关键字在 BDL grammar 里是命名节点
// boolean，同样不会被动）。
func keywordEdits(root *ts.Node, src []byte, keywordCase string) []edit {
	if keywordCase != "upper" && keywordCase != "lower" {
		return nil
	}

	var edits []edit
	var visit func(n *ts.Node)
	visit = func(n *ts.Node) {
		if n == nil {
			return
		}
		if !n.IsNamed() {
			text := src[n.StartByte():n.EndByte()]
			// 预处理指令（&include/&define…）大小写敏感：grammar 的
			// external scanner 只认小写——实测 &include 大写后解析
			// 直接报 4 个 ERROR（语料 aapi002.4gl 踩过）。绝不动它们。
			if len(text) > 0 && text[0] == '&' {
				return
			}
			if hasLetter(text) {
				var want string
				if keywordCase == "upper" {
					want = strings.ToUpper(string(text))
				} else {
					want = strings.ToLower(string(text))
				}
				if want != string(text) {
					edits = append(edits, edit{start: n.StartByte(), end: n.EndByte(), text: want})
				}
			}
			return // 匿名 token 是叶子
		}
		// 遍历全部子节点（含匿名；NamedChildren 只给命名的）
		count := n.ChildCount()
		for i := uint(0); i < count; i++ {
			visit(n.Child(i))
		}
	}
	visit(root)
	return edits
}

// hasLetter 报告字节里是否含 ASCII 字母（用于区分关键字与纯标点）。
func hasLetter(b []byte) bool {
	for _, c := range b {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			return true
		}
	}
	return false
}

// ---------------------------------------------------------------------------
// 缩进
// ---------------------------------------------------------------------------

// indentEdits 收集行首缩进改写。
//
// 每行的缩进 = 该行首 token 的"块祖先链"深度（祖先起始行严格早于本行
// 的 blockNodes 计数）；行首 token 是回退关键字（END/ELSE/CATCH）时
// 再减一。空行不改；多行块注释内部的行不改（防止改坏注释排版）。
func indentEdits(root *ts.Node, src []byte, indentUnit string) []edit {
	if indentUnit == "" {
		return nil
	}

	lineStarts := lineStartOffsets(src)
	var edits []edit

	for li := 0; li < len(lineStarts); li++ {
		line := li
		var lineEnd uint
		if li+1 < len(lineStarts) {
			lineEnd = lineStarts[li+1]
		} else {
			lineEnd = uint(len(src))
		}
		// 本行内容范围（不含换行符）
		contentEnd := lineEnd
		if contentEnd > 0 && src[contentEnd-1] == '\n' {
			contentEnd--
		}
		if contentEnd > 0 && src[contentEnd-1] == '\r' {
			contentEnd--
		}

		// 行首第一个非空白字符
		off := lineStarts[li]
		for off < contentEnd && (src[off] == ' ' || src[off] == '\t') {
			off++
		}
		if off >= contentEnd {
			continue // 空行（或只有空白）：不动
		}

		// 多行块注释内部：行首落在 comment 节点里且该注释起始行更早
		node := root.DescendantForByteRange(off, off)
		if node == nil {
			continue
		}
		if node.Kind() == "comment" && lineOfByte(lineStarts, node.StartByte()) < line {
			continue
		}

		// 块深度：祖先链上 startLine < 本行 的 blockNodes 计数。
		//
		// 同一行可以同时开启多个块（如 `DEFINE l_rec RECORD`：define 与
		// record 两个块都起于本行）。此时它们对本行的缩进贡献只有一级，
		// 否则记录体每行会多缩进一级。故按 startLine 去重：每个起始行
		// 最多贡献一级。
		depth := 0
		lastStartLine := -1
		for a := node; a != nil; a = a.Parent() {
			startLine := lineOfByte(lineStarts, a.StartByte())
			if !blockNodes[a.Kind()] || startLine >= line {
				continue
			}
			if startLine == lastStartLine {
				continue // 同一行已计入一级
			}
			depth++
			lastStartLine = startLine
		}
		// 回退关键字（行首 token 是匿名 END/ELSE/CATCH）
		if !node.IsNamed() {
			if closerKeywords[strings.ToUpper(string(src[node.StartByte():node.EndByte()]))] {
				depth--
			}
		}

		if depth < 0 {
			depth = 0
		}
		want := strings.Repeat(indentUnit, depth)
		cur := string(src[lineStarts[li]:off])
		if cur != want {
			edits = append(edits, edit{start: lineStarts[li], end: off, text: want})
		}
	}
	return edits
}

// lineStartOffsets 返回每行起始字节偏移（首行为 0）。
func lineStartOffsets(src []byte) []uint {
	starts := make([]uint, 0, 64)
	starts = append(starts, 0)
	for i, c := range src {
		if c == '\n' {
			starts = append(starts, uint(i+1))
		}
	}
	return starts
}

// lineOfByte 用二分查找返回字节偏移所在的行号（0 基）。
func lineOfByte(starts []uint, off uint) int {
	lo, hi := 0, len(starts)
	for lo < hi {
		mid := (lo + hi) / 2
		if starts[mid] <= off {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	return lo - 1
}
