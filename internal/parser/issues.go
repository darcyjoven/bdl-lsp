package parser

import (
	"fmt"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// Issue 是解析阶段发现的一个问题。
//
// 行号/列号均为 0 基，列按 UTF-8 字节计；转换为 LSP 的 UTF-16 时由上层处理。
type Issue struct {
	// StartByte / EndByte 是源码中的字节区间。
	StartByte uint
	EndByte   uint
	// StartRow / StartCol、EndRow / EndCol 是 tree-sitter 的行列（字节列）。
	StartRow uint
	StartCol uint
	EndRow   uint
	EndCol   uint
	// Kind 是节点类型：ERROR 或 MISSING 节点的期望类型。
	Kind string
	// Missing 为 true 表示 MISSING 节点（缺少必填项）。
	Missing bool
	// Message 是面向用户的描述。
	Message string
}

// CollectOptions 控制问题收集行为。
type CollectOptions struct {
	// MaxIssues 最多返回多少条；<=0 时使用 200。
	MaxIssues int
	// MergeAdjacent 为 true 时，同一行且相邻的多个 ERROR 合并为一条。
	MergeAdjacent bool
}

// DefaultCollectOptions 返回推荐配置。
func DefaultCollectOptions() CollectOptions {
	return CollectOptions{MaxIssues: 200, MergeAdjacent: true}
}

// CollectIssues 遍历语法树，收集 ERROR / MISSING 节点。
func CollectIssues(tree *ts.Tree, maxIssues int) []Issue {
	return Collect(tree, CollectOptions{MaxIssues: maxIssues})
}

// Collect 按选项收集问题。
func Collect(tree *ts.Tree, opts CollectOptions) []Issue {
	if tree == nil {
		return nil
	}
	root := tree.RootNode()
	if root == nil || !root.HasError() {
		return nil
	}

	max := opts.MaxIssues
	if max <= 0 {
		max = 200
	}

	var issues []Issue
	walkErrors(root, max, &issues)
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].StartByte != issues[j].StartByte {
			return issues[i].StartByte < issues[j].StartByte
		}
		// 同一位置：MISSING 优先（信息更具体）
		return issues[i].Missing && !issues[j].Missing
	})

	// 同一位置、同一类型的重复问题去重（tree-sitter 有时会嵌套报告）
	issues = dedupe(issues)

	if opts.MergeAdjacent {
		issues = mergeAdjacent(issues)
	}
	return issues
}

// walkErrors 递归收集。ERROR 节点保留但继续下探，以捕获内部更精确的 MISSING。
func walkErrors(node *ts.Node, maxIssues int, out *[]Issue) {
	if node == nil || len(*out) >= maxIssues {
		return
	}

	switch {
	case node.IsError():
		*out = append(*out, newIssue(node, false, errorMessage(node)))
	case node.IsMissing():
		*out = append(*out, newIssue(node, true, fmt.Sprintf("缺少 %s", node.Kind())))
		return
	case !node.HasError():
		return
	}

	count := node.ChildCount()
	for i := uint(0); i < count; i++ {
		child := node.Child(i)
		if child == nil {
			continue
		}
		walkErrors(child, maxIssues, out)
		if len(*out) >= maxIssues {
			return
		}
	}
}

// errorMessage 生成 ERROR 节点的消息。
//
// tree-sitter 不提供具体错误原因，因此尽量给出上下文：
// 若 ERROR 区间正好是单个词，附带该词；否则给出长度提示。
func errorMessage(node *ts.Node) string {
	start, end := node.ByteRange()
	if end > start && end-start <= 40 {
		return "语法错误"
	}
	rows := node.EndPosition().Row - node.StartPosition().Row
	if rows > 0 {
		return fmt.Sprintf("语法错误（跨 %d 行）", rows+1)
	}
	return "语法错误"
}

func newIssue(node *ts.Node, missing bool, msg string) Issue {
	start := node.StartPosition()
	end := node.EndPosition()
	return Issue{
		StartByte: node.StartByte(),
		EndByte:   node.EndByte(),
		StartRow:  start.Row,
		StartCol:  start.Column,
		EndRow:    end.Row,
		EndCol:    end.Column,
		Kind:      node.Kind(),
		Missing:   missing,
		Message:   msg,
	}
}

// dedupe 去掉完全相同（起止+类型+缺失标记）的重复问题。
func dedupe(in []Issue) []Issue {
	if len(in) <= 1 {
		return in
	}
	out := in[:0]
	for _, iss := range in {
		if len(out) > 0 {
			prev := out[len(out)-1]
			if prev.StartByte == iss.StartByte && prev.EndByte == iss.EndByte &&
				prev.Kind == iss.Kind && prev.Missing == iss.Missing {
				continue
			}
		}
		out = append(out, iss)
	}
	return out
}

// mergeAdjacent 合并同一行内紧邻的多个 ERROR。
//
// 典型场景：一行里漏了逗号，tree-sitter 会在同一行连续报告多个 ERROR，
// 合并成一条更容易阅读，也避免编辑器里一排红点。
func mergeAdjacent(in []Issue) []Issue {
	if len(in) <= 1 {
		return in
	}
	out := make([]Issue, 0, len(in))
	for _, iss := range in {
		if len(out) > 0 {
			last := &out[len(out)-1]
			if canMerge(*last, iss) {
				if iss.EndByte > last.EndByte {
					last.EndByte = iss.EndByte
					last.EndRow = iss.EndRow
					last.EndCol = iss.EndCol
				}
				continue
			}
		}
		out = append(out, iss)
	}
	return out
}

// canMerge 判断两个问题是否可以合并为一条。
//
// 条件：都是 ERROR（不是 MISSING）、位于同一行、且区间相邻或重叠。
// MISSING 不参与合并，因为它指向“缺少某物”的具体位置，单独展示更有用。
func canMerge(a, b Issue) bool {
	if a.Missing || b.Missing {
		return false
	}
	if a.EndRow != b.StartRow {
		return false
	}
	// 相邻（中间最多 3 字节空白）或重叠
	return b.StartByte <= a.EndByte+3
}

// TextOf 返回节点在源码中的文本（用于日志/调试）。
func TextOf(node *ts.Node, src []byte) string {
	if node == nil {
		return ""
	}
	start, end := node.ByteRange()
	if start > end || end > uint(len(src)) {
		return ""
	}
	return strings.TrimSpace(string(src[start:end]))
}
