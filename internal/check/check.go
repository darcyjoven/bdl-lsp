// Package check 提供 `bdl-lsp check` 子命令：解析文件并打印问题。
//
// 它不依赖编辑器，可直接用于 CI 冒烟、语料回归和手工排查。
package check

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

// Result 是单个文件的检查结果。
type Result struct {
	File   string         `json:"file"`
	Lang   string         `json:"lang,omitempty"`
	Issues []parser.Issue `json:"issues"`
	// Error 表示读取/解析阶段的失败（与语法问题区分）。
	Error string `json:"error,omitempty"`
}

// Run 执行 check 子命令，返回进程退出码。
//
// 退出码：0 = 全部通过；1 = 有语法问题；2 = 参数/环境错误。
func Run(args []string, version string) int {
	fs := flag.NewFlagSet("bdl-lsp check", flag.ContinueOnError)
	asJSON := fs.Bool("json", false, "以 JSON 输出")
	maxIssues := fs.Int("max-issues", 50, "每个文件最多报告的问题数")
	quiet := fs.Bool("q", false, "只输出有问题的文件")
	showVersion := fs.Bool("version", false, "打印版本")

	if err := fs.Parse(args); err != nil {
		fmt.Fprintf(os.Stderr, "bdl-lsp check: %v\n", err)
		return 2
	}
	if *showVersion {
		fmt.Printf("bdl-lsp %s\n", version)
		return 0
	}
	files := fs.Args()
	if len(files) == 0 {
		fmt.Fprintln(os.Stderr, "用法: bdl-lsp check [flags] <file.4gl|file.per>...")
		return 2
	}

	pool, err := parser.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bdl-lsp check: %v\n", err)
		return 2
	}
	defer pool.Close()

	results := make([]Result, 0, len(files))
	failed := 0
	for _, file := range files {
		res := checkFile(pool, file, *maxIssues)
		if res.Error != "" || len(res.Issues) > 0 {
			failed++
		}
		results = append(results, res)
	}

	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(results); err != nil {
			fmt.Fprintf(os.Stderr, "bdl-lsp check: 输出 JSON: %v\n", err)
			return 2
		}
	} else {
		printResults(os.Stdout, results, *quiet)
	}

	if failed > 0 {
		return 1
	}
	return 0
}

func checkFile(pool *parser.Pool, path string, maxIssues int) Result {
	res := Result{File: path}

	lang, ok := parser.LangForPath(path)
	if !ok {
		res.Error = "不支持的文件类型（期望 .4gl/.global/.per）"
		return res
	}
	res.Lang = string(lang)

	src, err := os.ReadFile(path)
	if err != nil {
		res.Error = err.Error()
		return res
	}

	tree, err := pool.Parse(lang, src, nil)
	if err != nil {
		res.Error = err.Error()
		return res
	}
	defer tree.Close()

	res.Issues = parser.CollectIssues(tree, maxIssues)
	if res.Issues == nil {
		res.Issues = []parser.Issue{}
	}
	return res
}

func printResults(w io.Writer, results []Result, quiet bool) {
	withIssues := 0
	for _, res := range results {
		if res.Error != "" {
			fmt.Fprintf(w, "%s: ERROR %s\n", res.File, res.Error)
			continue
		}
		if len(res.Issues) == 0 {
			if !quiet {
				fmt.Fprintf(w, "%s: OK\n", res.File)
			}
			continue
		}
		withIssues++
		fmt.Fprintf(w, "%s: %d 个问题\n", res.File, len(res.Issues))
		for _, iss := range res.Issues {
			// tree-sitter 行号 0 基，报告时用 1 基，符合编辑器习惯。
			fmt.Fprintf(w, "  %d:%d  %s\n", iss.StartRow+1, iss.StartCol+1, iss.Message)
		}
	}

	total := len(results)
	fmt.Fprintf(w, "\n共 %d 个文件，%d 个有问题\n", total, withIssues)
	for _, res := range results {
		if res.Error != "" {
			fmt.Fprintf(w, "读取/解析失败: %s (%s)\n", filepath.Clean(res.File), res.Error)
		}
	}
}
