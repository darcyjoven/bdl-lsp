// diagnostics-check 校验"诊断码"在注册表、文档、VSCode 设置三处保持一致。
//
// 用法：
//
//	go run ./cmd/diagnostics-check            # 校验（CI/发布前）
//	go run ./cmd/diagnostics-check -write     # 重新生成 docs/DIAGNOSTICS.md
//	go run ./cmd/diagnostics-check -doc docs/DIAGNOSTICS.md -vscode ../vscode-extension/package.json
//
// 退出码：0 一致；1 发现不一致（逐条打印）；2 参数/文件错误。
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/darcyjoven/bdl-lsp/internal/diag"
	"github.com/darcyjoven/bdl-lsp/internal/diagcheck"
)

func main() {
	docPath := flag.String("doc", "docs/DIAGNOSTICS.md", "诊断码文档路径")
	vscodePath := flag.String("vscode", "../vscode-extension/package.json", "VSCode 扩展 package.json 路径（空=跳过）")
	write := flag.Bool("write", false, "重新生成文档而不是只校验")
	flag.Parse()

	specs := diag.Specs()

	if *write {
		if err := os.WriteFile(*docPath, []byte(diagcheck.Doc(specs)), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "diagnostics-check: 写 %s: %v\n", *docPath, err)
			os.Exit(2)
		}
		fmt.Printf("已生成 %s（%d 个诊断码）\n", *docPath, len(specs))
	}

	probs := diagcheck.CheckRegistry(specs)
	probs = append(probs, diagcheck.CheckDoc(*docPath, specs)...)
	if *vscodePath != "" {
		if _, err := os.Stat(*vscodePath); err == nil {
			vp, verr := diagcheck.CheckVSCode(*vscodePath, specs)
			if verr != nil {
				fmt.Fprintf(os.Stderr, "diagnostics-check: %v\n", verr)
				os.Exit(2)
			}
			probs = append(probs, vp...)
		} else {
			fmt.Fprintf(os.Stderr, "diagnostics-check: 跳过 %s（不存在）\n", *vscodePath)
		}
	}

	if len(probs) > 0 {
		for _, p := range probs {
			fmt.Println("✗ " + p.String())
		}
		fmt.Fprintf(os.Stderr, "diagnostics-check: %d 处不一致\n", len(probs))
		os.Exit(1)
	}
	fmt.Printf("诊断码一致：%d 个码，注册表 / %s / %s 三处对得上\n",
		len(specs), *docPath, *vscodePath)
}
