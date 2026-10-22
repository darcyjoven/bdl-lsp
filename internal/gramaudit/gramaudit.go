// Package gramaudit 统计语料里 grammar 解析失败（ERROR / MISSING）的分布，
// 产出"语法缺口清单"（P7-C-2）。
//
// 被两处使用：`cmd/grammar-audit`（人工看清单、改 grammar 前后 diff）
// 和 `TestCorpusGrammarBaseline`（断言"没有新的文件解析失败"——修一个坏两个
// 是 grammar 改动的典型事故）。
//
// # 判据
//
// 只记**最内层**的 ERROR。语料里一个拼写错误（`END TUNCTION`）会让
// tree-sitter 把整个文件包成一个根级 ERROR，记外层只会把缺口指到第 1 行的
// 注释，毫无用处。
//
// 一个 ERROR 节点的"上下文键"是 `父节点类型|行首词`：缺口通常是"某条语句
// 不认识"，行首词就是那条语句的名字；父节点类型用来区分"同一段坏代码在
// 不同上下文里要补的东西不一样"。
package gramaudit

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/darcyjoven/bdl-lsp/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
)

// Gap 是一个语法缺口（同一上下文键聚合后的结果）。
type Gap struct {
	Key    string `json:"key"`
	Parent string `json:"parent"`
	Head   string `json:"head"`
	// Sample 是归一化后的出错行（截断），用于肉眼判断缺口类型。
	Sample string `json:"sample"`
	// Count 是 ERROR 节点数，Files 是涉及文件数。
	Count int `json:"count"`
	Files int `json:"files"`
	// Examples 是若干"文件:行"，最多保留 2 条。
	Examples []string `json:"examples,omitempty"`
	// Missing 为 true 表示这是 MISSING 节点（缺少必填项），Kind 是缺的类型。
	Missing bool   `json:"missing,omitempty"`
	Kind    string `json:"kind,omitempty"`
}

// FileErr 是单个文件的错误统计。
//
// 缺口清单回答"补哪条语法"，per-file 统计回答"这次改动有没有让别的文件
// 变差"——每次改 grammar 都要跟基线 diff 一次。
type FileErr struct {
	Path    string `json:"path"`
	Errors  int    `json:"errors"`
	Missing int    `json:"missing"`
}

// Report 是一次审计的完整结果。
type Report struct {
	Root       string    `json:"root"`
	Files      int       `json:"files"`
	ErrFiles   int       `json:"filesWithError"`
	ErrNodes   int       `json:"errorNodes"`
	MissingCnt int       `json:"missingNodes"`
	Gaps       []Gap     `json:"gaps"`
	FileErrs   []FileErr `json:"fileErrors,omitempty"`
}

// Run 审计 root 下的所有 .4gl/.per。limit>0 时只处理前 limit 个文件，
// top>0 时只保留前 top 个缺口（JSON 里始终是过滤后的结果，够用即可）。
func Run(pool *parser.Pool, root string, limit, workers, top int) Report {
	files := Collect(root, limit)
	if len(files) == 0 {
		return Report{Root: root}
	}
	if workers <= 0 {
		workers = 8
	}

	var mu sync.Mutex
	rep := Report{Root: root, Files: len(files)}
	agg := map[string]*Gap{}
	filesPerKey := map[string]map[string]bool{}

	ch := make(chan string)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range ch {
				hits := scanFile(pool, path)
				mu.Lock()
				if hits.errs > 0 || hits.missing > 0 {
					rep.ErrFiles++
					rep.FileErrs = append(rep.FileErrs, FileErr{
						Path: path, Errors: hits.errs, Missing: hits.missing,
					})
				}
				rep.ErrNodes += hits.errs
				rep.MissingCnt += hits.missing
				for _, g := range hits.gaps {
					cur := agg[g.Key]
					if cur == nil {
						g2 := g
						g2.Examples = append([]string(nil), g.Examples...)
						cur = &g2
						agg[g.Key] = cur
						filesPerKey[g.Key] = map[string]bool{}
					}
					cur.Count += g.Count
					for _, ex := range g.Examples {
						file := ex
						if i := strings.LastIndex(ex, ":"); i > 0 {
							file = ex[:i]
						}
						filesPerKey[g.Key][file] = true
					}
				}
				mu.Unlock()
			}
		}()
	}
	for _, f := range files {
		ch <- f
	}
	close(ch)
	wg.Wait()

	for key, g := range agg {
		g.Files = len(filesPerKey[key])
		rep.Gaps = append(rep.Gaps, *g)
	}
	SortGaps(rep.Gaps)
	if top > 0 && len(rep.Gaps) > top {
		rep.Gaps = rep.Gaps[:top]
	}
	sort.Slice(rep.FileErrs, func(i, j int) bool {
		return rep.FileErrs[i].Path < rep.FileErrs[j].Path
	})
	return rep
}

// fileHits 是单个文件的扫描结果。
type fileHits struct {
	path    string
	gaps    []Gap
	errs    int
	missing int
}

// scanFile 解析单个文件并抽取缺口上下文。
func scanFile(pool *parser.Pool, path string) fileHits {
	src, err := os.ReadFile(path)
	if err != nil {
		return fileHits{path: path}
	}
	lang := parser.LangBDL
	if strings.HasSuffix(strings.ToLower(path), ".per") {
		lang = parser.LangPer
	}
	tree, err := pool.Parse(lang, src, nil)
	if err != nil || tree == nil {
		return fileHits{path: path}
	}
	defer tree.Close()

	root := tree.RootNode()
	if root == nil || !root.HasError() {
		return fileHits{path: path}
	}

	hits := fileHits{path: path}
	lines := strings.Split(string(src), "\n")
	var walk func(n *ts.Node)
	walk = func(n *ts.Node) {
		if n == nil {
			return
		}
		missing := n.IsMissing()
		if n.IsError() || missing {
			// 只记最内层：外层只是错误恢复的包装层。
			if missing || !hasInnerError(n) {
				hits.gaps = append(hits.gaps, makeGap(n, path, lines, missing))
				if missing {
					hits.missing++
				} else {
					hits.errs++
				}
			}
		}
		for i := uint(0); i < n.ChildCount(); i++ {
			walk(n.Child(i))
		}
	}
	walk(root)
	return hits
}

// hasInnerError 报告节点内部是否还有 ERROR/MISSING 节点。
func hasInnerError(n *ts.Node) bool {
	for i := uint(0); i < n.ChildCount(); i++ {
		c := n.Child(i)
		if c == nil {
			continue
		}
		if c.IsError() || c.IsMissing() || hasInnerError(c) {
			return true
		}
	}
	return false
}

// makeGap 把一个 ERROR/MISSING 节点转成聚合用的上下文。
func makeGap(n *ts.Node, path string, lines []string, missing bool) Gap {
	row := int(n.StartPosition().Row)
	line := ""
	if row >= 0 && row < len(lines) {
		line = lines[row]
	}
	sample := normalize(line)
	head := firstWord(sample)
	parent := ""
	if p := n.Parent(); p != nil {
		parent = p.Kind()
	}
	kind := ""
	if missing {
		kind = n.Kind()
	}
	key := parent + "|" + head
	if missing {
		key = "missing:" + kind + "@" + parent
	}
	return Gap{
		Key:      key,
		Parent:   parent,
		Head:     head,
		Sample:   truncate(sample, 72),
		Count:    1,
		Missing:  missing,
		Kind:     kind,
		Examples: []string{fmt.Sprintf("%s:%d", path, row+1)},
	}
}

// normalize 把一行压成可聚合的形式：去首尾空白、多空白压成一个空格。
//
// 不做更激进的归一化（比如把标识符换成占位符）：缺口清单是给人看的，
// 保留原样才能一眼看出"这是 `#{ ... }#` 标记"还是"这是 SQL 里的写法"。
func normalize(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// firstWord 取第一个词（只按空白切）。
func firstWord(s string) string {
	if i := strings.IndexByte(s, ' '); i >= 0 {
		return s[:i]
	}
	return s
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Collect 列出 root 下的待审计文件（.4gl / .per），limit>0 时截断。
func Collect(root string, limit int) []string {
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

// SortGaps 按 (节点数, 文件数, 键) 排序：节点数代表"修好后能少多少条 ERROR"。
func SortGaps(gaps []Gap) {
	sort.SliceStable(gaps, func(i, j int) bool {
		a, b := gaps[i], gaps[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		if a.Files != b.Files {
			return a.Files > b.Files
		}
		return a.Key < b.Key
	})
}

// WriteJSON 把结果写成 JSON（用于改 grammar 前后的逐文件对比）。
func WriteJSON(path string, rep Report) error {
	data, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(data, '\n'), 0o644)
}

// Percent 返回 a/b 的百分比（b 为 0 时返回 0）。
func Percent(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return float64(a) * 100 / float64(b)
}

// Print 把人读的报告打到 w。
func Print(w io.Writer, rep Report, examples int) {
	fmt.Fprintf(w, "== 语法缺口审计 %s ==\n", rep.Root)
	fmt.Fprintf(w, "文件            %d\n", rep.Files)
	fmt.Fprintf(w, "有 ERROR 的文件 %d（%.1f%%）\n", rep.ErrFiles, Percent(rep.ErrFiles, rep.Files))
	fmt.Fprintf(w, "ERROR 节点      %d\n", rep.ErrNodes)
	fmt.Fprintf(w, "MISSING 节点    %d\n", rep.MissingCnt)
	fmt.Fprintln(w)

	shown := 0
	for _, g := range rep.Gaps {
		shown++
		kind := "ERROR"
		if g.Missing {
			kind = "MISSING"
		}
		fmt.Fprintf(w, "%-38s %6d 节点 %5d 文件  [%s 父=%s]\n",
			truncate(g.Key, 38), g.Count, g.Files, kind, g.Parent)
		if g.Sample != "" {
			fmt.Fprintf(w, "    例：%s\n", g.Sample)
		}
		if examples > 0 {
			n := examples
			if len(g.Examples) < n {
				n = len(g.Examples)
			}
			for _, ex := range g.Examples[:n] {
				fmt.Fprintf(w, "    %s\n", ex)
			}
		}
	}
	if shown == 0 {
		fmt.Fprintln(w, "没有任何语法缺口 🎉")
	}
}
