// `bdl-lsp link` 子命令：导入与查看程序链接资料（P7-D）。
//
// 为什么做成子命令而不是编辑器里的按钮：导入要读几千个文件、跑十几秒，
// 属于"一次性/低频"动作，放进编辑器 UI 只会卡界面。用户在终端跑一次，
// 编辑器侧靠 mtime 自动热加载。
package linkcli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/link"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

// Run 执行 `bdl-lsp link <子命令> [参数]`。
//
// 退出码：0 成功；1 业务失败（没有资料/导入失败）；2 用法错误。
func Run(args []string, version string) int {
	sub := "status"
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		sub = args[0]
		args = args[1:]
	}
	switch sub {
	case "import":
		return runImport(args, version)
	case "status":
		return runStatus(args)
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "bdl-lsp link: 未知子命令 %q\n", sub)
		usage()
		return 2
	}
}

func usage() {
	fmt.Print(`用法：
  bdl-lsp link import -csv <导出.csv> [-root <项目根>] [-out <资料目录>]
  bdl-lsp link status [-root <项目根>] [-out <资料目录>]

import 把 DS.GAL_FILE 的导出转成 <项目根>/.link/links.jsonl。
       需要读源码判断哪一条是 MAIN，语料量级下大约 10–20 秒。
status 查看已有资料的统计（不解析源码，秒回）。

选项：
  -root       项目根目录（默认：当前目录）
  -csv        导出 CSV 路径（import 必填）
  -out        资料目录（默认 <root>/.link）
  -roots      模组根，逗号分隔（默认自动探测）
  -global     全局可见模组，逗号分隔（默认 sub,lib,qry，仅 status 显示用）
  -replace    覆盖已有资料（默认就是这样；保留此开关是为了将来做"合并"）
`)
}

func runImport(args []string, version string) int {
	fs := flag.NewFlagSet("link import", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	csvPath := fs.String("csv", "", "DS.GAL_FILE 导出的 CSV（必填）")
	root := fs.String("root", "", "项目根目录（默认当前目录）")
	out := fs.String("out", "", "资料目录（默认 <root>/.link）")
	roots := fs.String("roots", "", "模组根，逗号分隔")
	fs.Bool("replace", true, "覆盖已有资料")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *csvPath == "" {
		fmt.Fprintln(os.Stderr, "bdl-lsp link import: 必须指定 -csv")
		return 2
	}
	projectRoot := *root
	if projectRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "bdl-lsp link import: 取当前目录失败: %v\n", err)
			return 1
		}
		projectRoot = cwd
	}

	pool, err := parser.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "bdl-lsp link import: 初始化解析器失败: %v\n", err)
		return 1
	}
	defer pool.Close()

	res, err := link.RunImport(link.ImportRequest{
		CSVPath:      *csvPath,
		ProjectRoot:  projectRoot,
		DataDir:      *out,
		ModuleRoots:  splitList(*roots),
		Pool:         pool,
		Source:       "file",
		SourceDetail: *csvPath,
		ToolVersion:  version,
		Write:        true,
		Now:          time.Now(),
	})
	if err != nil {
		fmt.Fprintf(os.Stderr, "bdl-lsp link import: %v\n", err)
		return 1
	}
	var sb strings.Builder
	res.Report(&sb)
	fmt.Print(sb.String())
	fmt.Printf("\n已写入 %s\n", filepath.Join(res.DataDir, link.LinksFileName))
	fmt.Println("编辑器里无需重启：打开文件时会自动发现资料变更并重载。")
	return 0
}

func runStatus(args []string) int {
	fs := flag.NewFlagSet("link status", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	root := fs.String("root", "", "项目根目录（默认当前目录）")
	out := fs.String("out", "", "资料目录（默认 <root>/.link）")
	global := fs.String("global", "", "全局可见模组，逗号分隔（默认 sub,lib,qry）")
	maxAge := fs.Int("max-age", 7, "超过多少天算过期（0 = 不判断）")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	projectRoot := *root
	if projectRoot == "" {
		cwd, err := os.Getwd()
		if err != nil {
			fmt.Fprintf(os.Stderr, "bdl-lsp link status: 取当前目录失败: %v\n", err)
			return 1
		}
		projectRoot = cwd
	}
	dir := *out
	if dir == "" {
		dir = filepath.Join(projectRoot, link.DefaultDataDir)
	}
	modules := splitList(*global)
	if len(modules) == 0 {
		modules = link.DefaultGlobalModules()
	}

	st := link.LoadStatus(dir, *maxAge, modules)
	fmt.Printf("== 链接资料 ==\n")
	fmt.Printf("目录        %s\n", st.DataDir)
	if !st.Present {
		if st.Error != "" {
			fmt.Printf("状态        读取失败：%s\n", st.Error)
			return 1
		}
		fmt.Printf("状态        没有资料\n")
		fmt.Println("\n先运行：bdl-lsp link import -csv <导出.csv>")
		return 1
	}
	fmt.Printf("链          %d\n", st.Stats.Chains)
	fmt.Printf("orphan      %d\n", st.Stats.Orphans)
	fmt.Printf("ambiguous   %d\n", st.Stats.Ambiguous)
	fmt.Printf("不同文件    %d\n", st.Stats.Files)
	if m := st.Meta; m != nil {
		fmt.Printf("生成时间    %s（%d 天前）\n", m.GeneratedAt, int(st.Age.Hours()/24))
		fmt.Printf("来源        %s %s\n", m.Source, m.SourceDetail)
		fmt.Printf("缺失文件    %d（链接里有、磁盘上没有，完全忽略）\n", m.MissingFiles)
		if len(m.ResolvedFrom) > 0 {
			fmt.Printf("模组根      %s\n", strings.Join(m.ResolvedFrom, ", "))
		}
		fmt.Printf("导入统计    行 %d / 过滤 %d / 有效 %d\n", m.RowsTotal, m.RowsFilteredN, m.RowsY)
		switch {
		case m.ExternalsSkipped:
			fmt.Printf("外部函数    候选 %d 个，**超过上限，未启用**（资料可能不完整）\n", m.ExternalsTotal)
		case m.ExternalsTotal > 0:
			fmt.Printf("外部函数    %d 个（被 ≥%d 个文件或 ≥%d 次调用、全语料无定义，视为存在）\n",
				m.ExternalsTotal, m.ExternalsMinFiles, m.ExternalsMinCalls)
			show := m.Externals
			if len(show) > 5 {
				show = show[:5]
			}
			parts := make([]string, 0, len(show))
			for _, e := range show {
				parts = append(parts, fmt.Sprintf("%s(%d)", e.Name, e.Files))
			}
			fmt.Printf("            %s%s\n", strings.Join(parts, " "),
				map[bool]string{true: " …", false: ""}[m.ExternalsTotal > len(show)])
		}
	} else {
		fmt.Printf("生成时间    未知（缺 %s）\n", link.MetaFileName)
	}
	fmt.Printf("全局可见    %s\n", strings.Join(modules, ", "))
	if st.Stale {
		fmt.Printf("\n⚠ 资料已超过 %d 天未更新，function-not-found 可能误报；建议重新导入。\n",
			st.MaxAgeDays)
	}
	return 0
}

func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if v := strings.TrimSpace(p); v != "" {
			out = append(out, v)
		}
	}
	return out
}
