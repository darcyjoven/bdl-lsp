// link-audit 用真实 CSV 复跑链接资料导入，核对 PLAN-P7 §11 的实测数字。
//
// 它同时是 `bdl-lsp link import` 的"对照实现"：两者调用同一个
// `link.RunImport`，所以数字必须一致。不一致就说明 CLI 那条路有分支没走到。
//
// 用法：
//
//	go run ./cmd/link-audit                          # 只统计，不写盘
//	go run ./cmd/link-audit -write                   # 写到 <root>/.link
//	go run ./cmd/link-audit -csv x.csv -root <proj>  # 指定输入与项目根
//	go run ./cmd/link-audit -status -root <proj>     # 只读已落盘的资料
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/link"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

func main() {
	csvPath := flag.String("csv", "artifacts/link/link.csv", "DS.GAL_FILE 导出的 CSV")
	root := flag.String("root", "/code/workspace/tiptop-gp", "项目根目录")
	dataDir := flag.String("out", "", "资料目录（默认 <root>/.link）")
	roots := flag.String("roots", "", "模组根，逗号分隔（默认自动探测）")
	toolVersion := flag.String("tool-version", "", "记进 meta 的版本号")
	write := flag.Bool("write", false, "写盘（links.jsonl + links.meta.json）")
	status := flag.Bool("status", false, "只读已落盘的资料并汇总，不导入")
	top := flag.Int("top", 8, "列出文件数最多的前 N 条链")
	showMissing := flag.Bool("missing", false, "统计缺失文件按模组分布")
	showExt := flag.Bool("externals", false, "列出全部外部函数（被调用但全资料无定义）")
	flag.Parse()

	pool, err := parser.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "link-audit: 初始化解析器: %v\n", err)
		os.Exit(2)
	}
	defer pool.Close()

	dir := *dataDir
	if dir == "" {
		dir = filepath.Join(*root, link.DefaultDataDir)
	}

	if *status {
		st := link.LoadStatus(dir, 0, link.DefaultGlobalModules())
		printStatus(st)
		return
	}

	var moduleRoots []string
	if *roots != "" {
		moduleRoots = splitList(*roots)
	}

	out, err := link.RunImport(link.ImportRequest{
		CSVPath:      *csvPath,
		ProjectRoot:  *root,
		DataDir:      dir,
		ModuleRoots:  moduleRoots,
		Pool:         pool,
		Source:       "file",
		SourceDetail: *csvPath,
		ToolVersion:  *toolVersion,
		Write:        *write,
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "link-audit: %v\n", err)
		os.Exit(2)
	}
	var sb strings.Builder
	out.Report(&sb)
	fmt.Print(sb.String())

	if *write {
		fmt.Printf("\n已写入 %s\n", out.DataDir)
	} else {
		fmt.Printf("\n（未写盘；加 -write 才会写 %s）\n", out.DataDir)
	}

	// 重新加载并展示落盘后的内容，顺带验证 Load 与 Save 往返一致。
	idx, err := link.Load(out.DataDir)
	if err == nil && idx != nil {
		fmt.Printf("\n加载校验：链 %d，orphan %d，ambiguous %d，不同文件 %d\n",
			idx.Stats().Chains, idx.Stats().Orphans, idx.Stats().Ambiguous, idx.Stats().Files)
		fmt.Printf("\n文件数最多的链：\n")
		for _, c := range link.TopChains(idx, *top) {
			fmt.Printf("  %-12s %4d 文件  main=%s%s\n",
				c.Program, len(c.Files), c.Main, ambiguousTag(c))
		}
	} else if err != nil && err != link.ErrNoData {
		fmt.Fprintf(os.Stderr, "加载失败: %v\n", err)
	}

	if *showMissing {
		printMissing(out)
	}
	if *showExt {
		printExternals(out)
	}
}

// printExternals 列出被"当成存在"的外部函数。
//
// 用户 2026-10-21 定的口径：函数名在项目里找不到定义，多半是**语料本身缺**
// （不是用户写错），只要不是太多就忽略，但必须记录下来——这个命令就是
// "记录"的出口，出问题时能一眼看到哪些名字被放过了。
func printExternals(out *link.ImportOutcome) {
	meta := out.Result.Meta
	fmt.Printf("\n== 外部函数（被调用但从无定义，视为存在）==\n")
	if meta.ExternalsSkipped {
		fmt.Printf("候选 %d 个，超过上限 %d → **未启用**\n", meta.ExternalsTotal, link.MaxExternals)
		return
	}
	if meta.ExternalsTotal == 0 {
		fmt.Printf("无\n")
		return
	}
	fmt.Printf("共 %d 个（阈值：被 ≥%d 个文件或 ≥%d 次调用）；列出前 %d 个：\n",
		meta.ExternalsTotal, meta.ExternalsMinFiles, meta.ExternalsMinCalls, len(meta.Externals))
	for _, e := range meta.Externals {
		fmt.Printf("  %-40s %5d 文件 / %6d 次\n", e.Name, e.Files, e.Calls)
	}
}

func ambiguousTag(c *link.Chain) string {
	if c.Ambiguous {
		return "  [ambiguous]"
	}
	return ""
}

func printStatus(st link.Status) {
	fmt.Printf("== 链接资料状态 ==\n")
	fmt.Printf("目录        %s\n", st.DataDir)
	if !st.Present {
		if st.Error != "" {
			fmt.Printf("状态        读取失败：%s\n", st.Error)
		} else {
			fmt.Printf("状态        没有资料（先运行 link import）\n")
		}
		return
	}
	fmt.Printf("链          %d\n", st.Stats.Chains)
	fmt.Printf("orphan      %d\n", st.Stats.Orphans)
	fmt.Printf("ambiguous   %d\n", st.Stats.Ambiguous)
	fmt.Printf("不同文件    %d\n", st.Stats.Files)
	if st.Meta != nil {
		fmt.Printf("生成时间    %s（%s 前）\n", st.Meta.GeneratedAt, st.Age.Round(1e9))
		fmt.Printf("来源        %s %s\n", st.Meta.Source, st.Meta.SourceDetail)
		fmt.Printf("缺失文件    %d\n", st.Meta.MissingFiles)
		fmt.Printf("模组根      %s\n", strings.Join(st.Meta.ResolvedFrom, ", "))
	} else {
		fmt.Printf("生成时间    未知（缺 links.meta.json）\n")
	}
}

// printMissing 统计缺失文件按模组分布，验证"集中在 W* 模组"这一事实。
func printMissing(out *link.ImportOutcome) {
	if out == nil || out.Result == nil {
		return
	}
	layout := out.Layout
	type stat struct{ total, missing int }
	stats := map[string]*stat{}
	for _, c := range out.Result.Chains {
		for _, f := range c.Files {
			key := strings.ToUpper(f.Module())
			s := stats[key]
			if s == nil {
				s = &stat{}
				stats[key] = s
			}
			s.total++
			if _, ok := layout.Path(f.Module(), f.Code()); !ok {
				s.missing++
			}
		}
	}
	type row struct {
		mod     string
		total   int
		missing int
	}
	var rows []row
	for k, v := range stats {
		if v.missing > 0 {
			rows = append(rows, row{k, v.total, v.missing})
		}
	}
	// 冒泡（行数 < 100）
	for i := 1; i < len(rows); i++ {
		for j := i; j > 0 && rows[j].missing > rows[j-1].missing; j-- {
			rows[j], rows[j-1] = rows[j-1], rows[j]
		}
	}
	fmt.Printf("\n缺失文件按模组（前 12）：\n")
	for i, r := range rows {
		if i >= 12 {
			fmt.Printf("  … 其余 %d 个模组\n", len(rows)-12)
			break
		}
		fmt.Printf("  %-8s %4d / %4d\n", r.mod, r.missing, r.total)
	}
	if len(rows) == 0 {
		fmt.Printf("  （无缺失）\n")
	}
}

func splitList(s string) []string {
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
