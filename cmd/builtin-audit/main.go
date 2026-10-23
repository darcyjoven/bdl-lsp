// builtin-audit 是内置符号表（P7-C）的度量与补表工具。
//
// 解决什么问题：Genero 的内置函数/变量有几百个，靠手抄一定漏。这个工具
// 把"语料里出现、但既没定义也不在内置表里"的标识符按频次列出来，
// 人只需要判断"这是内置名还是用户自己的名字"，再回填 data/builtins.json。
//
// 与 diag-audit 的分工：diag-audit 看"每个诊断码各多少条"（降噪总账），
// builtin-audit 只看"内置表还差哪些名字"（补表清单）。
//
// 用法：
//
//	go run ./cmd/builtin-audit                      # 全量语料，列前 40 个缺口
//	go run ./cmd/builtin-audit -limit 500           # 先跑一部分
//	go run ./cmd/builtin-audit -check               # 只校验内置数据文件
//	go run ./cmd/builtin-audit -overlay x.json      # 叠加一份覆盖文件再统计
//	go run ./cmd/builtin-audit -show 120            # 列更多缺口
//
// 判读要点：
//   - 「调用位置」= function-not-found 报出来的名字（工具会临时打开该码）。
//   - 「变量位置」= variable-source-unresolved 报出来的名字。
//   - 两类都按频次排序；**高频 + 全大写 + 跨很多文件** 的多半是内置名，
//     低频或带项目前缀（CL_/S_/WSHELPER_）的多半是用户自己的函数（归 P7-D）。
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/builtin"
	"github.com/darcyjoven/bdl-lsp/internal/config"
	"github.com/darcyjoven/bdl-lsp/internal/diag"
	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
	"github.com/darcyjoven/bdl-lsp/internal/server"
	"github.com/darcyjoven/bdl-lsp/parser"
)

func main() {
	root := flag.String("root", "/code/workspace/tiptop-gp", "语料根目录")
	limit := flag.Int("limit", 0, "最多处理文件数（0=全部）")
	show := flag.Int("show", 40, "每个分类最多列 N 个缺口")
	check := flag.Bool("check", false, "只校验内置数据文件格式后退出")
	overlay := flag.String("overlay", "", "额外的覆盖文件或目录（可多次用 -overlay）")
	names := flag.String("names", "", "只看这些名字（逗号分隔，用于确认某个名字是否已收录）")
	flag.Parse()

	if *check {
		os.Exit(runCheck())
	}

	pool, err := parser.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "builtin-audit: 初始化解析器: %v\n", err)
		os.Exit(2)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(pool, logger, "builtin-audit")
	srv.EnableResolvers(*root)

	if *overlay != "" {
		paths := strings.Split(*overlay, ",")
		if err := srv.ConfigureBuiltins(*root, paths, nil); err != nil {
			fmt.Fprintf(os.Stderr, "builtin-audit: 加载覆盖文件: %v\n", err)
			os.Exit(2)
		}
	}

	// function-not-found 默认 off（P7-B 的决定）；审计时要打开才能看到
	// "调用位置的缺口"，这是补表的主要输入之一。
	if err := srv.ApplyDiagnosticOverrides(map[string]config.DiagnosticLevel{
		string(diag.CodeFunctionNotFound): config.LevelWarning,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "builtin-audit: 打开 function-not-found: %v\n", err)
		os.Exit(2)
	}

	set := srv.BuiltinSet()
	printSetSummary(set)

	if *names != "" {
		printLookup(set, *names)
		return
	}

	files := collect(*root, *limit)
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "builtin-audit: %s 下没有 .4gl\n", *root)
		os.Exit(2)
	}

	// 每个名字要记"出现在多少个不同文件里"：跨文件高频是内置名的最强信号，
	// 只在单个文件里出现的几乎一定是用户变量。
	varStats := map[string]*stat{}
	callStats := map[string]*stat{}
	bump := func(m map[string]*stat, name, path string) {
		s, ok := m[name]
		if !ok {
			s = &stat{files: map[string]bool{}}
			m[name] = s
		}
		s.count++
		s.files[path] = true
	}

	ctx := context.Background()
	var totalDiags int
	for _, path := range files {
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			continue
		}
		diags := srv.AuditFile(ctx, fileURI(path), parser.LangBDL, string(src))
		totalDiags += len(diags)
		for _, d := range diags {
			code, _ := d.Code.(string)
			switch code {
			case string(diag.CodeVariableSourceUnresolved):
				if n := quotedName(d.Message); n != "" {
					bump(varStats, strings.ToUpper(n), path)
				}
			case string(diag.CodeFunctionNotFound):
				if n := quotedName(d.Message); n != "" {
					bump(callStats, strings.ToUpper(n), path)
				}
			}
		}
	}

	fmt.Printf("== 内置符号缺口审计 %s ==\n", *root)
	fmt.Printf("文件：%d，语义诊断合计：%d\n\n", len(files), totalDiags)
	printGaps("调用位置（function-not-found）", callStats, set, *show)
	printGaps("变量位置（variable-source-unresolved）", varStats, set, *show)
}

// runCheck 校验内置数据文件（与单测共用 builtin.Validate）。
func runCheck() int {
	data, err := builtin.EmbeddedData()
	if err != nil {
		fmt.Fprintf(os.Stderr, "读取内置数据: %v\n", err)
		return 2
	}
	problems := builtin.Validate(data)
	if len(problems) == 0 {
		set := builtin.Embedded()
		fmt.Printf("内置符号表校验通过：%d 条（Genero %s）\n", set.Len(), set.GeneroVersion())
		byKind := map[builtin.Kind]int{}
		for _, sym := range set.Symbols() {
			byKind[sym.Kind]++
		}
		for _, k := range []builtin.Kind{builtin.KindVariable, builtin.KindFunction,
			builtin.KindClass, builtin.KindMethod, builtin.KindConstant} {
			if n := byKind[k]; n > 0 {
				fmt.Printf("  %-10s %3d\n", k, n)
			}
		}
		return 0
	}
	for _, p := range problems {
		fmt.Fprintf(os.Stderr, "  - %s\n", p)
	}
	fmt.Fprintf(os.Stderr, "内置符号表有 %d 个问题\n", len(problems))
	return 1
}

func printSetSummary(set *builtin.Set) {
	if set == nil {
		fmt.Println("内置符号表：已关闭")
		return
	}
	fmt.Printf("内置符号表：%d 条（Genero %s）\n", set.Len(), set.GeneroVersion())
	for _, src := range set.Sources() {
		kind := "覆盖"
		if src.Builtin {
			kind = "内置"
		}
		status := ""
		if src.Err != nil {
			status = fmt.Sprintf("  !! %v", src.Err)
		}
		fmt.Printf("  [%s] %s  +%d 条  -%d 条%s\n",
			kind, src.Path, src.Symbols, src.Disabled, status)
	}
	fmt.Println()
}

func printLookup(set *builtin.Set, names string) {
	for _, raw := range strings.Split(names, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if set == nil {
			fmt.Printf("%-28s 内置表已关闭\n", name)
			continue
		}
		if sym, ok := set.Lookup(name); ok {
			fmt.Printf("%-28s ✓ %-9s %s\n", name, sym.Kind, sym.Signature)
			continue
		}
		if set.Disabled(name) {
			fmt.Printf("%-28s ✗ 已被 disable\n", name)
			continue
		}
		fmt.Printf("%-28s ✗ 不在表里\n", name)
	}
}

// stat 是一个名字的出现统计。
type stat struct {
	count int
	files map[string]bool
}

// gap 是一条缺口（排好序待展示）。
type gap struct {
	name  string
	count int
	files int
}

// printGaps 输出一类缺口：语料里出现、且内置表里没有的名字。
//
// 只列表里没有的——已经收录的名字不该再占据注意力。
func printGaps(title string, m map[string]*stat, set *builtin.Set, show int) {
	var rows []gap
	for name, s := range m {
		if set != nil {
			if _, ok := set.Lookup(name); ok {
				continue // 已收录
			}
			if set.Disabled(name) {
				continue // 用户显式禁用，不是缺口
			}
		}
		rows = append(rows, gap{name: name, count: s.count, files: len(s.files)})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].name < rows[j].name
	})

	fmt.Printf("== %s：%d 个不在内置表里的名字 ==\n", title, len(rows))
	if len(rows) == 0 {
		fmt.Println("  （无缺口）")
		fmt.Println()
		return
	}
	if show > 0 && len(rows) > show {
		rows = rows[:show]
	}
	fmt.Printf("  %-34s %8s %6s  %s\n", "名字", "次数", "文件数", "判断提示")
	fmt.Println("  " + strings.Repeat("-", 74))
	for _, r := range rows {
		fmt.Printf("  %-34s %8d %6d  %s\n", r.name, r.count, r.files, hintFor(r.name, r.count, r.files))
	}
	fmt.Println()
}

// hintFor 给一条粗判提示，减少人工翻语料的时间。
//
// 判据刻意保守：这只是"看一眼"的提示，最终仍要人确认。
// 误判代价不对称——把用户函数当内置会掩盖真实的链接缺失（P7-D 的核心问题），
// 所以带项目前缀的一律提示"疑似用户函数"。
func hintFor(name string, count, files int) string {
	switch {
	case strings.HasPrefix(name, "CL_"), strings.HasPrefix(name, "S_"),
		strings.HasPrefix(name, "G_"), strings.HasPrefix(name, "L_"),
		strings.HasPrefix(name, "WSHELPER_"), strings.HasPrefix(name, "WMS_"):
		return "疑似用户函数/变量（模组前缀）"
	case strings.HasPrefix(name, "FGL_"):
		return "**疑似内置**（fgl_ 前缀）"
	case len(name) > 30:
		return "疑似生成的全局名"
	case files >= 20 && count >= 200:
		return "**高频跨文件，优先核实**"
	case files <= 2:
		return "局部名字，多半不是内置"
	}
	return ""
}

// fileURI 与服务器/客户端用同一套编码（fileuri.FromPath）：
// 手写 "file://"+路径 在 Windows 上会得到非法的 `file://C:/…`。
func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	return fileuri.FromPath(abs)
}

// collect 列出语料下的 .4gl（不含 .per：内置表只服务 4GL 语义诊断）。
func collect(root string, limit int) []string {
	var out []string
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			// 跳过常见的无关目录，避免把构建产物当源码。
			switch d.Name() {
			case ".git", "node_modules", "artifacts", ".project", ".link":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.EqualFold(filepath.Ext(path), ".4gl") {
			out = append(out, path)
		}
		return nil
	})
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

// quotedName 从诊断消息里取出被 %q 包起来的名字。
func quotedName(msg string) string {
	i := strings.Index(msg, "\"")
	if i < 0 {
		return ""
	}
	rest := msg[i+1:]
	j := strings.Index(rest, "\"")
	if j < 0 {
		return ""
	}
	return rest[:j]
}
