// diag-audit 统计语义诊断在各诊断码上的分布（P7-B 降噪的度量工具）。
//
// 为什么需要它：`bdl-lsp check` 只报语法问题，而“变量来源不明”“函数找不到”
// 这类语义诊断才是噪声的主要来源。要判断某次 query/符号表改动是降噪还是
// 引入新噪声，必须有逐码计数的基线。
//
// 用法：
//
//	go run ./cmd/diag-audit                      # 全量语料
//	go run ./cmd/diag-audit -limit 24            # 取前 N 个文件
//	go run ./cmd/diag-audit -codes              # 打印已注册诊断码与默认级别
//	go run ./cmd/diag-audit -top 20 -examples 3 # 每条码给 3 个示例
//
// 输出：artifacts/logs/diag-audit-<时间>.log（同时打印到 stdout）
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/config"
	"github.com/darcyjoven/bdl-lsp/internal/diag"
	"github.com/darcyjoven/bdl-lsp/internal/server"
	"github.com/darcyjoven/bdl-lsp/parser"
)

// hit 是一条待展示的示例。
type hit struct {
	file string
	line int
	text string
}

func main() {
	root := flag.String("root", "/code/workspace/tiptop-gp", "语料根目录")
	limit := flag.Int("limit", 0, "最多处理文件数（0=全部）")
	top := flag.Int("top", 0, "只展示前 N 个码（0=全部）")
	examples := flag.Int("examples", 0, "每个码最多展示几条示例")
	printCodes := flag.Bool("codes", false, "打印已注册诊断码与默认级别后退出")
	names := flag.Int("names", 0, "额外列出 variable-source-unresolved 的前 N 个标识符（0=不列）")
	perFlag := flag.String("diagnostics", "", "覆盖诊断级别，形如 code=off,code2=hint")
	linkGlobal := flag.String("link-global", "", "覆盖全局可见模组（逗号分隔，默认 sub,lib,qry）")
	linkOff := flag.Bool("no-link", false, "不加载链接资料（对照 P7-D 之前的基线）")
	flag.Parse()

	if *printCodes {
		printRegistered()
		return
	}

	pool, err := parser.New()
	if err != nil {
		fmt.Fprintf(os.Stderr, "diag-audit: 初始化解析器: %v\n", err)
		os.Exit(2)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := server.New(pool, logger, "diag-audit")
	// 语料审计要看到 globals / 表单类诊断，必须初始化解析器
	// （P7-D 起同时尝试加载 .link 链接资料）。
	srv.EnableResolvers(*root)

	if overrides := parseOverrides(*perFlag); len(overrides) > 0 {
		if err := srv.ApplyDiagnosticOverrides(overrides); err != nil {
			fmt.Fprintf(os.Stderr, "diag-audit: 覆盖诊断级别: %v\n", err)
			os.Exit(2)
		}
	}
	if *linkOff {
		if err := srv.ConfigureLinks([]string{"__none__"}); err != nil {
			fmt.Fprintf(os.Stderr, "diag-audit: 关闭链接: %v\n", err)
			os.Exit(2)
		}
		srv.EnableLinks()
	} else if *linkGlobal != "" {
		mods := splitList(*linkGlobal)
		if err := srv.ConfigureLinks(mods); err != nil {
			fmt.Fprintf(os.Stderr, "diag-audit: 覆盖全局模组: %v\n", err)
			os.Exit(2)
		}
		srv.EnableLinks()
	}

	// 链接资料的状态会改变 function-not-found 的可用性，必须显式告知：
	// 否则"0 条"到底是"没错"还是"没启用"就分不清了。
	if res := srv.LinkResolver(); res != nil {
		if !srv.WaitLinkReady(120 * time.Second) {
			fmt.Fprintln(os.Stderr, "diag-audit: 链接索引预热超时，function-not-found 不会报告")
		}
		fmt.Fprintf(os.Stderr, "diag-audit: %s\n", res.Describe())
	} else {
		fmt.Fprintln(os.Stderr, "diag-audit: 没有链接资料（.link/links.jsonl），function-not-found 不启用")
	}

	if !srv.AuditEnabled() {
		fmt.Fprintln(os.Stderr, "diag-audit: 所有诊断码都已关闭，没有可统计的内容")
		return
	}

	files := collect(*root, *limit)
	if len(files) == 0 {
		fmt.Fprintf(os.Stderr, "diag-audit: %s 下没有 .4gl / .per\n", *root)
		os.Exit(2)
	}

	counts := map[string]int{}
	perFile := map[string]int{}
	byLang := map[string]map[string]int{}
	samples := map[string][]hit{}
	nameCounts := map[string]int{}
	// fnfNames 统计"报未定义的函数名"。P7-D 之后这个分布决定
	// function-not-found 能不能默认开：如果绝大多数是"语料里根本没有的
	// 库函数"，那是快照不全，不是用户写错。
	fnfNames := map[string]int{}

	ctx := context.Background()
	var total int
	for _, path := range files {
		src, rerr := os.ReadFile(path)
		if rerr != nil {
			continue
		}
		lang := parser.LangBDL
		if strings.HasSuffix(strings.ToLower(path), ".per") {
			lang = parser.LangPer
		}
		langKey := string(lang)
		if byLang[langKey] == nil {
			byLang[langKey] = map[string]int{}
		}

		uri := fileURI(path)
		diags := srv.AuditFile(ctx, uri, lang, string(src))
		lines := splitLines(string(src))
		total += len(diags)
		perFile[path] = len(diags)
		for _, d := range diags {
			code, _ := d.Code.(string)
			if code == "" {
				code = "(无码)"
			}
			counts[code]++
			byLang[langKey][code]++
			if code == string(diag.CodeFunctionNotFound) {
				if name := quotedInMessage(d.Message); name != "" {
					fnfNames[name]++
				}
			}
			if code == string(diag.CodeVariableSourceUnresolved) {
				if n := quotedName(d.Message); n != "" {
					nameCounts[strings.ToUpper(n)]++
				}
			}
			if len(samples[code]) < *examples {
				line := int(d.Range.Start.Line)
				samples[code] = append(samples[code], hit{
					file: path,
					line: line,
					text: lineText(lines, line),
				})
			}
		}
	}

	var out io.Writer = os.Stdout
	if logFile, closeLog := openLog(); logFile != nil {
		defer closeLog()
		out = io.MultiWriter(os.Stdout, logFile)
	}

	fmt.Fprintf(out, "== 诊断审计 %s ==\n", *root)
	fmt.Fprintf(out, "文件：%d（.4gl 与 .per 混算），诊断总数：%d\n", len(files), total)
	for _, langKey := range sortedKeys(byLang) {
		fmt.Fprintf(out, "  %-4s 文件诊断合计 %d\n", langKey, sum(byLang[langKey]))
	}
	fmt.Fprintln(out)

	rows := rank(counts)
	if *top > 0 && len(rows) > *top {
		rows = rows[:*top]
	}
	fmt.Fprintf(out, "%-32s %8s %7s\n", "诊断码", "条数", "占比")
	fmt.Fprintln(out, strings.Repeat("-", 50))
	for _, r := range rows {
		pct := 0.0
		if total > 0 {
			pct = float64(r.count) * 100 / float64(total)
		}
		fmt.Fprintf(out, "%-32s %8d %6.1f%%\n", r.code, r.count, pct)
		if len(samples[r.code]) > 0 {
			for _, s := range samples[r.code] {
				fmt.Fprintf(out, "    %s:%d  %s\n", filepath.Base(s.file), s.line+1, truncate(s.text, 100))
			}
		}
	}
	fmt.Fprintln(out)

	if *names > 0 && len(fnfNames) > 0 {
		nrows := rankNames(fnfNames)
		if len(nrows) > *names {
			nrows = nrows[:*names]
		}
		fmt.Fprintf(out, "function-not-found 的高频函数名（前 %d）：\n", len(nrows))
		for _, r := range nrows {
			fmt.Fprintf(out, "  %-28s %5d\n", r.code, r.count)
		}
		fmt.Fprintln(out)
	}

	if *names > 0 && len(nameCounts) > 0 {
		nrows := rankNames(nameCounts)
		if len(nrows) > *names {
			nrows = nrows[:*names]
		}
		fmt.Fprintf(out, "variable-source-unresolved 的高频标识符（前 %d）：\n", len(nrows))
		for _, r := range nrows {
			fmt.Fprintf(out, "  %-24s %5d\n", r.code, r.count)
		}
		fmt.Fprintln(out)
	}

	// 噪声最高的文件：定位下一轮要看的样本。
	type fileCount struct {
		path  string
		count int
	}
	frows := make([]fileCount, 0, len(perFile))
	for p, c := range perFile {
		if c > 0 {
			frows = append(frows, fileCount{p, c})
		}
	}
	sort.Slice(frows, func(i, j int) bool {
		if frows[i].count != frows[j].count {
			return frows[i].count > frows[j].count
		}
		return frows[i].path < frows[j].path
	})
	if len(frows) > 10 {
		frows = frows[:10]
	}
	fmt.Fprintln(out, "诊断最多的文件：")
	for _, f := range frows {
		fmt.Fprintf(out, "  %6d  %s\n", f.count, f.path)
	}
}

// printRegistered 打印注册表内容。
func printRegistered() {
	fmt.Printf("%-32s %s\n", "诊断码", "默认级别")
	fmt.Println(strings.Repeat("-", 50))
	for _, code := range diag.Registered() {
		fmt.Printf("%-32s %s\n", code, diag.DefaultLevel(code))
	}
}

// parseOverrides 解析 `code=level,code=level` 形式的覆盖。
func parseOverrides(s string) map[string]config.DiagnosticLevel {
	out := map[string]config.DiagnosticLevel{}
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		code, level, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(code)] = config.DiagnosticLevel(strings.TrimSpace(level))
	}
	return out
}

// collect 收集语料文件（排序后按 limit 截断）。
func collect(root string, limit int) []string {
	var files []string
	filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		lower := strings.ToLower(p)
		if strings.HasSuffix(lower, ".4gl") || strings.HasSuffix(lower, ".per") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	if limit > 0 && len(files) > limit {
		files = files[:limit]
	}
	return files
}

// openLog 在 artifacts/logs 下建本次日志文件（失败时只打 stdout）。
func openLog() (io.Writer, func()) {
	dir := filepath.Join("artifacts", "logs")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, func() {}
	}
	f, err := os.CreateTemp(dir, "diag-audit-*.log")
	if err != nil {
		return nil, func() {}
	}
	fmt.Fprintf(os.Stderr, "日志：%s\n", f.Name())
	return f, func() { f.Close() }
}

// fileURI 把本地路径转成 file:// URI（与服务器内部一致）。
func fileURI(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = path
	}
	u := url.URL{Scheme: "file", Path: filepath.ToSlash(abs)}
	return u.String()
}

func splitLines(s string) []string { return strings.Split(s, "\n") }

func lineText(lines []string, i int) string {
	if i < 0 || i >= len(lines) {
		return ""
	}
	return strings.TrimSpace(lines[i])
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

func sum(m map[string]int) int {
	t := 0
	for _, v := range m {
		t += v
	}
	return t
}

func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// quotedName 从消息里取出第一个双引号包住的标识符。
func quotedName(msg string) string {
	_, rest, ok := strings.Cut(msg, "\"")
	if !ok {
		return ""
	}
	name, _, ok := strings.Cut(rest, "\"")
	if !ok {
		return ""
	}
	return name
}

func rankNames(counts map[string]int) []row {
	rows := make([]row, 0, len(counts))
	for c, n := range counts {
		rows = append(rows, row{c, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].code < rows[j].code
	})
	return rows
}

type row struct {
	code  string
	count int
}

func rank(counts map[string]int) []row {
	rows := make([]row, 0, len(counts))
	for c, n := range counts {
		rows = append(rows, row{c, n})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].count != rows[j].count {
			return rows[i].count > rows[j].count
		}
		return rows[i].code < rows[j].code
	})
	return rows
}

// quotedInMessage 取出诊断消息里第一个双引号包裹的名字。
//
// function-not-found 的消息形如 `函数 "xxx" 在当前模块…`；直接从消息里取，
// 比再走一遍语法树便宜，也不用给 AuditFile 加返回值。
func quotedInMessage(msg string) string {
	i := strings.IndexByte(msg, '"')
	if i < 0 {
		return ""
	}
	j := strings.IndexByte(msg[i+1:], '"')
	if j < 0 {
		return ""
	}
	return msg[i+1 : i+1+j]
}

// splitList 把逗号分隔的参数切成非空项。
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
