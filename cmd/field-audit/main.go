// field-audit 对语料执行字段校验并输出统计（P3b 评估脚本）。
//
// 用途：
//   - 统计表单解析率与字段命中/未命中分布；
//   - 抽样输出未命中样本，供人工标注误报。
//
// 用法：
//
//	go run ./cmd/field-audit [-root 语料根] [-limit N]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/form"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

func firstSegment(literal string) string {
	parts := strings.Split(strings.ReplaceAll(literal, "\\", "/"), "/")
	if len(parts) > 1 {
		return strings.ToLower(parts[0])
	}
	return ""
}

func pathHasSegment(path, seg string) bool {
	for _, p := range strings.Split(filepath.ToSlash(path), "/") {
		if strings.ToLower(p) == seg {
			return true
		}
	}
	return false
}

func main() {
	root := flag.String("root", "/code/workspace/tiptop-gp", "语料根目录")
	limit := flag.Int("limit", 0, "最多处理文件数（0=全部）")
	sample := flag.Int("sample", 30, "未命中样本输出数")
	flag.Parse()

	pool, err := parser.New()
	if err != nil {
		panic(err)
	}
	defer pool.Close()
	qs := form.CompileQueries(pool)
	if qs == nil {
		fmt.Println("query 编译失败")
		os.Exit(1)
	}
	res := form.NewResolver(pool, *root, nil, nil)

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

	totalRefs := 0
	totalIssues := 0
	filesWithForms := 0
	issueSamples := make([]string, 0, *sample)
	issueByForm := map[string]int{}

	// 自动标注：模块段一致性（字面量 aap/42f/x 的 aap 应出现在解析路径里；
	// 不一致 = 可能解析到了错误表单 = 可疑误报）
	var segMatch, segMismatch, segNone int

	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		tree, err := pool.Parse(parser.LangBDL, src, nil)
		if err != nil {
			continue
		}
		issues, used := res.Check(tree, src, f, qs)
		tree.Close()
		if len(used) > 0 {
			filesWithForms++
		}
		// 引用数：粗略用 query 再跑一次？不——用 issues 数即可
		totalIssues += len(issues)
		for _, iss := range issues {
			issueByForm[iss.Literal]++
			// 模块段一致性标注
			if seg := firstSegment(iss.Literal); seg != "" {
				if pathHasSegment(iss.FormPath, seg) {
					segMatch++
				} else {
					segMismatch++
				}
			} else {
				segNone++
			}
			if len(issueSamples) < *sample {
				line := 1 + strings.Count(string(src[:iss.Start]), "\n")
				issueSamples = append(issueSamples,
					fmt.Sprintf("%s:%d  %s", f, line, iss.Message))
			}
		}
		_ = totalRefs
	}

	fmt.Printf("文件数            : %d\n", len(files))
	fmt.Printf("含表单引用的文件  : %d\n", filesWithForms)
	fmt.Printf("字段问题总数      : %d\n", totalIssues)
	fmt.Printf("涉及表单数        : %d\n", len(issueByForm))
	fmt.Println()
	fmt.Println("问题最多的表单（前 10）:")
	type kv struct {
		k string
		v int
	}
	var lst []kv
	for k, v := range issueByForm {
		lst = append(lst, kv{k, v})
	}
	sort.Slice(lst, func(i, j int) bool { return lst[i].v > lst[j].v })
	for i, e := range lst {
		if i >= 10 {
			break
		}
		fmt.Printf("  %4d  %s\n", e.v, e.k)
	}
	fmt.Println()
	fmt.Println("自动标注（模块段一致性：字面量 aap/42f/x 的 aap 应在解析路径中）:")
	fmt.Printf("  模块一致   : %d\n", segMatch)
	fmt.Printf("  模块不一致 : %d  ← 可疑误报（解析到别的模块的表单）\n", segMismatch)
	fmt.Printf("  无模块段   : %d\n", segNone)
	fmt.Println()
	fmt.Printf("未命中样本（%d 条，供人工标注误报）:\n", len(issueSamples))
	for _, s := range issueSamples {
		fmt.Println("  " + s)
	}
}
