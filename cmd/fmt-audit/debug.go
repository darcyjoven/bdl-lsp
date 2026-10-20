package main

// debugOne 单文件调试：输出格式化后差异区域
import (
	"fmt"
	"os"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/format"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
)

func debugOne(path string) {
	pool, err := parser.New()
	if err != nil {
		panic(err)
	}
	defer pool.Close()
	src, _ := os.ReadFile(path)
	tree, _ := pool.Parse(parser.LangBDL, src, nil)
	once, ok := format.Format(tree, src, format.Options{KeywordCase: "upper", IndentUnit: "   "})
	tree.Close()
	if !ok {
		fmt.Println("拒绝格式化")
		return
	}

	// 找第一个差异行
	osLines := splitLines(string(src))
	newLines := splitLines(once)
	// FMT_DEBUG_FULL=1 时直接打印全文，便于手写用例断言。
	if os.Getenv("FMT_DEBUG_FULL") != "" {
		fmt.Printf("===== 格式化结果（%d 行）=====\n%s", len(newLines), once)
		if !strings.HasSuffix(once, "\n") {
			fmt.Println()
		}
		fmt.Println("===== 结束 =====")
	}
	for i := 0; i < len(osLines) && i < len(newLines); i++ {
		if osLines[i] != newLines[i] {
			fmt.Printf("首个差异在行 %d:\n--- 原 ---\n%q\n--- 新 ---\n%q\n", i+1, osLines[i], newLines[i])
			// 打印前后 2 行
			for j := max(0, i-2); j < min(len(osLines), i+3); j++ {
				fmt.Printf("原[%d]: %q\n", j+1, osLines[j])
			}
			for j := max(0, i-2); j < min(len(newLines), i+3); j++ {
				fmt.Printf("新[%d]: %q\n", j+1, newLines[j])
			}
			break
		}
	}

	// 新树的 ERROR 位置
	tree2, _ := pool.Parse(parser.LangBDL, []byte(once), nil)
	var cnt int
	reportErrors(tree2.RootNode(), []byte(once), &cnt)
	tree2.Close()
}

func reportErrors(n *ts.Node, src []byte, cnt *int) {
	if n == nil || *cnt > 3 {
		return
	}
	if n.IsError() {
		*cnt++
		line := 1
		for i := 0; i < int(n.StartByte()) && i < len(src); i++ {
			if src[i] == '\n' {
				line++
			}
		}
		end := int(n.EndByte())
		if end > len(src) {
			end = len(src)
		}
		fmt.Printf("ERROR #%d @行%d: %q\n", *cnt, line, string(src[n.StartByte():end]))
	}
	c := n.ChildCount()
	for i := uint(0); i < c; i++ {
		reportErrors(n.Child(i), src, cnt)
	}
}

func splitLines(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
