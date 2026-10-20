package parser

import (
	"fmt"
	"sort"
	"strings"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// Issue 是解析阶段发现的一个问题（语法错误或缺失节点）。
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

// CollectIssues 遍历语法树，收集 ERROR / MISSING 节点。
//
// 限制最多 maxIssues 条，避免畸形文件产生海量诊断拖垮编辑器。
func CollectIssues(tree *ts.Tree, maxIssues int) []Issue {
	if tree == nil {
		return nil
	}
	root := tree.RootNode()
	if root == nil || !root.HasError() {
		return nil
	}

	var issues []Issue
	cursor := tree.Walk()
	defer cursor.Close()

	walkErrors(root, cursor, maxIssues, &issues)
	sort.SliceStable(issues, func(i, j int) bool { return issues[i].StartByte < issues[j].StartByte })
	return issues
}

func walkErrors(node *ts.Node, cursor *ts.TreeCursor, maxIssues int, out *[]Issue) {
	if len(*out) >= maxIssues {
		return
	}
	if node == nil {
		return
	}

	switch {
	case node.IsError():
		*out = append(*out, newIssue(node, false, "语法错误"))
		// ERROR 节点内部仍可能有更精确的 MISSING，继续下探。
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
		walkErrors(child, cursor, maxIssues, out)
		if len(*out) >= maxIssues {
			return
		}
	}
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
