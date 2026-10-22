// grammar-audit 统计语料里 grammar 解析失败（ERROR / MISSING）的分布，
// 产出"语法缺口清单"（P7-C-2）。
//
// # 为什么需要它
//
// 语法树错了会连带高亮、大纲、格式化、缩进全错，插件侧无法补救，只能改
// grammar。但"哪些语句解析不了"不能靠猜：语料 15108 个文件，手工抽样一定
// 漏。这个工具把 ERROR 节点的上下文按频次排出来，高频且语句级的先补，
// 低频的留档（见 docs/PLAN-P7.md §10.10）。
//
// # 用法
//
//	go run ./cmd/grammar-audit                       # 全量语料，前 30 个缺口
//	go run ./cmd/grammar-audit -top 50 -examples 3   # 每个缺口 3 个例子
//	go run ./cmd/grammar-audit -json out.json        # 机器可读（改 grammar 前后对比）
//	go run ./cmd/grammar-audit -limit 2000           # 抽样（快速迭代用）
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/darcyjoven/bdl-lsp/internal/gramaudit"
	"github.com/darcyjoven/bdl-lsp/parser"
)

func main() {
	root := flag.String("root", "/code/workspace/tiptop-gp", "语料根目录")
	top := flag.Int("top", 30, "展示前 N 个缺口")
	examples := flag.Int("examples", 2, "每个缺口最多几个示例（0=不给）")
	limit := flag.Int("limit", 0, "最多处理文件数（0=全部）")
	workers := flag.Int("workers", 0, "并行度（0=自动）")
	jsonOut := flag.String("json", "", "把完整结果写成 JSON（用于改 grammar 前后对比）")
	flag.Parse()

	pool, err := parser.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "grammar-audit: 初始化解析器: %v\n", err)
		os.Exit(2)
	}
	defer pool.Close()

	// 全量缺口只算一遍（全语料约 24 秒），-top 只影响终端输出，
	// JSON 里始终是完整清单——改 grammar 前后要逐文件 diff。
	rep := gramaudit.Run(pool, *root, *limit, *workers, 0)
	if rep.Files == 0 {
		fmt.Fprintf(os.Stderr, "grammar-audit: %s 下没有 .4gl / .per\n", *root)
		os.Exit(2)
	}
	if *jsonOut != "" {
		if err := gramaudit.WriteJSON(*jsonOut, rep); err != nil {
			fmt.Fprintf(os.Stderr, "grammar-audit: 写 %s: %v\n", *jsonOut, err)
		}
	}
	shown := rep
	if *top > 0 && len(shown.Gaps) > *top {
		shown.Gaps = shown.Gaps[:*top]
	}
	gramaudit.Print(os.Stdout, shown, *examples)
}
