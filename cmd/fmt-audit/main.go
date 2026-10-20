// fmt-audit 对语料执行格式化并验证：幂等性 + 不新增语法错误（P4 评估）。
//
// 用法：go run ./cmd/fmt-audit [-root 语料根] [-limit N]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/format"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
)

func main() {
	root := flag.String("root", "/code/workspace/tiptop-gp", "语料根目录")
	limit := flag.Int("limit", 0, "最多处理文件数（0=全部）")
	keyword := flag.String("keywords", "upper", "关键字大小写（评估默认用大写，改动最多）")
	flag.Parse()

	if dbg := os.Getenv("FMT_DEBUG"); dbg != "" {
		debugOne(dbg)
		return
	}

	pool, err := parser.New()
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	var files []string
	filepath.WalkDir(*root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if !d.IsDir() && strings.HasSuffix(p, ".4gl") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	if *limit > 0 && len(files) > *limit {
		files = files[:*limit]
	}

	var total, formatted, skipped, changed int
	var nonIdempotent, newErrors []string

	countErrs := func(src []byte) int {
		tree, err := pool.Parse(parser.LangBDL, src, nil)
		if err != nil {
			return -1
		}
		n := 0
		countErrNodes(tree.RootNode(), &n)
		tree.Close()
		return n
	}

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		total++
		opts := format.Options{KeywordCase: *keyword, IndentUnit: "   "}

		tree, err := pool.Parse(parser.LangBDL, src, nil)
		if err != nil {
			skipped++
			continue
		}
		once, ok := format.Format(tree, src, opts)
		tree.Close()
		if !ok {
			skipped++ // 语法错误拒绝
			continue
		}
		formatted++
		if once != string(src) {
			changed++
		}

		// 幂等
		tree2, err := pool.Parse(parser.LangBDL, []byte(once), nil)
		if err == nil {
			twice, _ := format.Format(tree2, []byte(once), opts)
			tree2.Close()
			if once != twice {
				if len(nonIdempotent) < 5 {
					nonIdempotent = append(nonIdempotent, f)
				}
			}
		}

		// 不新增 ERROR（格式化前后 ERROR 数不增加）
		before := countErrs(src)
		after := countErrs([]byte(once))
		if before >= 0 && after > before {
			if len(newErrors) < 5 {
				newErrors = append(newErrors, fmt.Sprintf("%s: %d → %d", f, before, after))
			}
		}
	}

	fmt.Printf("文件总数        : %d\n", total)
	fmt.Printf("格式化成功      : %d\n", formatted)
	fmt.Printf("拒绝（语法错误）: %d\n", skipped)
	fmt.Printf("内容有变化      : %d\n", changed)
	fmt.Printf("幂等失败        : %d\n", len(nonIdempotent))
	for _, f := range nonIdempotent {
		fmt.Println("  幂等失败:", f)
	}
	fmt.Printf("新增 ERROR      : %d\n", len(newErrors))
	for _, f := range newErrors {
		fmt.Println("  新增:", f)
	}
}

// countErrNodes 统计树里的 ERROR 节点数。
func countErrNodes(n *ts.Node, count *int) {
	if n == nil {
		return
	}
	if n.IsError() {
		*count++
	}
	c := n.ChildCount()
	for i := uint(0); i < c; i++ {
		countErrNodes(n.Child(i), count)
	}
}
