// record-audit 评估 `BY NAME rec.member` 记录成员校验的误报率（P6）。
//
// 用法：go run ./cmd/record-audit [-root 语料根] [-limit N]
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
)

func main() {
	root := flag.String("root", "/code/workspace/tiptop-gp", "语料根目录")
	limit := flag.Int("limit", 0, "最多处理文件数（0=全部）")
	flag.Parse()

	pool, err := parser.New()
	if err != nil {
		panic(err)
	}
	defer pool.Close()

	var files []string
	filepath.WalkDir(*root, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		if strings.HasSuffix(p, ".4gl") {
			files = append(files, p)
		}
		return nil
	})
	sort.Strings(files)
	if *limit > 0 && len(files) > *limit {
		files = files[:*limit]
	}

	var (
		totalFiles int
		byNameRefs int // 显式 `rec.member` 形态总数
		findings   int // 报出的成员不存在
		starRefs   int // `rec.*` 通配数
		checkedRec int // 记录类型可解析（能校验）
		likeRec    int // 记录类型是 LIKE（跳过）
	)
	examples := []string{}

	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		tree, perr := pool.Parse(parser.LangBDL, src, nil)
		if perr != nil || tree == nil {
			continue
		}
		root := tree.RootNode()
		if root.HasError() {
			tree.Close()
			continue
		}
		totalFiles++

		fi := index.Build(parser.LangBDL, tree, src, 1)

		// 与 internal/server 的 buildRecordScopes/containerLookup 保持同一逻辑：
		// 按函数隔离 + 模块级回退 + 内联 RECORD 直接取 Children。
		type scope struct {
			vars  map[string]map[string]bool
			types map[string]string
		}
		scopeKey := func(container, name string) string { return container + "\x00" + name }
		typeMembers := map[string]map[string]bool{}
		for _, sym := range fi.Symbols {
			if sym.Kind != index.KindRecord || len(sym.Children) == 0 {
				continue
			}
			m := map[string]bool{}
			opaque := len(sym.Children) == 0 // RECORD LIKE table.* → 静态不可见
			// index 在多变量 DEFINE 中可能错配名字与类型，TypeText 可信度检查
			if !strings.HasPrefix(strings.ToUpper(strings.TrimSpace(sym.TypeText)), "RECORD") {
				opaque = true
			}
			for _, c := range sym.Children {
				switch c.Kind {
				case index.KindField:
					m[c.Lower] = true
				case index.KindRecord:
					opaque = true
					if c.Lower != "" {
						m[c.Lower] = true
					}
				}
			}
			if opaque {
				continue // 含子记录组，静态无法完整校验
			}
			typeMembers[scopeKey(sym.Container, sym.Lower)] = m
		}
		scopes := map[string]*scope{}
		get := func(c string) *scope {
			if sc := scopes[c]; sc != nil {
				return sc
			}
			sc := &scope{vars: map[string]map[string]bool{}, types: map[string]string{}}
			scopes[c] = sc
			return sc
		}
		for _, sym := range fi.Symbols {
			switch sym.Kind {
			case index.KindRecord:
				sc := get(sym.Container)
				if m, ok := typeMembers[scopeKey(sym.Container, sym.Lower)]; ok && len(m) > 0 {
					sc.vars[sym.Lower] = m
					sc.types[sym.Lower] = "RECORD"
				} else {
					// 记录存在但静态不可校验：必须记下 key（nil），
					// 否则查找会回退到模块级撞上同名变量。
					sc.vars[sym.Lower] = nil
					sc.types[sym.Lower] = "RECORD（成员静态不可见）"
				}
			case index.KindParameter:
				// 参数遮蔽外层同名变量，且自身不可校验
				sc := get(sym.Container)
				sc.vars[sym.Lower] = nil // 无条件遮蔽
			case index.KindDefine:
				typ := strings.ToLower(strings.TrimSpace(sym.TypeText))
				if typ == "" {
					continue
				}
				sc := get(sym.Container)
				sc.types[sym.Lower] = sym.TypeText
				if m, ok := typeMembers[scopeKey(sym.Container, typ)]; ok {
					sc.vars[sym.Lower] = m
				} else if m, ok := typeMembers[scopeKey("", typ)]; ok {
					sc.vars[sym.Lower] = m
				}
			}
		}
		type span struct {
			start, end uint
			name       string
		}
		var spans []span
		for _, sym := range fi.Symbols {
			switch sym.Kind {
			case index.KindFunction, index.KindMain, index.KindReport:
				if sym.DeclEnd > sym.DeclStart {
					spans = append(spans, span{sym.DeclStart, sym.DeclEnd, sym.Name})
				}
			}
		}
		containerOf := func(pos uint) string {
			best := ""
			bestLen := ^uint(0)
			for _, sp := range spans {
				if pos >= sp.start && pos < sp.end {
					if l := sp.end - sp.start; l < bestLen {
						bestLen = l
						best = sp.name
					}
				}
			}
			return best
		}
		lookup := func(container, name string) (map[string]bool, bool) {
			if sc := scopes[container]; sc != nil {
				if m, ok := sc.vars[name]; ok {
					return m, true
				}
			}
			if container != "" {
				if sc := scopes[""]; sc != nil {
					if m, ok := sc.vars[name]; ok {
						return m, true
					}
				}
			}
			return nil, false
		}

		var walk func(n *ts.Node)
		walk = func(n *ts.Node) {
			if n == nil {
				return
			}
			if n.Kind() == "object" {
				obj := n.ChildByFieldName("object")
				prop := n.ChildByFieldName("property")
				if obj != nil && prop == nil {
					starRefs++
				}
				if obj != nil && prop != nil {
					byNameRefs++
					rec := strings.ToLower(lastIdent(string(src[obj.StartByte():min(obj.EndByte(), uint(len(src)))])))
					members, known := lookup(containerOf(n.StartByte()), rec)
					if !known || members == nil {
						likeRec++
						walkChildren(n, walk)
						return
					}
					checkedRec++
					mem := strings.ToLower(strings.TrimSpace(string(src[prop.StartByte():min(prop.EndByte(), uint(len(src)))])))
					if !members[mem] && !isBuiltinMethod(mem) {
						findings++
						if len(examples) < 15 {
							examples = append(examples,
								fmt.Sprintf("%s: rec=%s member=%s", path, rec, mem))
						}
					}
				}
			}
			walkChildren(n, walk)
		}
		walk(root)
		tree.Close()
	}

	fmt.Printf("文件总数          : %d\n", totalFiles)
	fmt.Printf("rec.member 引用   : %d\n", byNameRefs)
	fmt.Printf("rec.* 通配        : %d（语法树无 property，不校验）\n", starRefs)
	fmt.Printf("可校验（本地类型）: %d\n", checkedRec)
	fmt.Printf("跳过（LIKE 等）   : %d\n", likeRec)
	fmt.Printf("报告成员不存在    : %d", findings)
	if checkedRec > 0 {
		fmt.Printf("（占可校验的 %.1f%%）", float64(findings)*100/float64(checkedRec))
	}
	fmt.Println()
	if len(examples) > 0 {
		fmt.Println("\n样例：")
		for _, e := range examples {
			fmt.Printf("  %s\n", e)
		}
	}
}

// isBuiltinMethod 与 internal/server 的 isBuiltinMemberMethod 保持同步。
func isBuiltinMethod(name string) bool {
	switch name {
	case "clear", "appendelement", "insertelement", "deleteelement",
		"getlength", "getelement", "setelement", "getindex",
		"setkey", "getkey", "haskey", "keys", "values", "remove",
		"add", "length", "substring", "toupper", "tolower", "trim":
		return true
	}
	return false
}

func walkChildren(n *ts.Node, walk func(*ts.Node)) {
	c := n.ChildCount()
	for i := uint(0); i < c; i++ {
		walk(n.Child(i))
	}
}

// lastIdent 取字符串里最后一段标识符字符。
func lastIdent(s string) string {
	end := -1
	start := -1
	for i := len(s) - 1; i >= 0; i-- {
		ch := s[i]
		ok := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9' || ch == '_' || ch == '$' || ch == '#'
		if ok {
			if end < 0 {
				end = i + 1
			}
			start = i
			continue
		}
		if end >= 0 {
			break
		}
	}
	if start < 0 {
		return ""
	}
	return s[start:end]
}

func min(a, b uint) uint {
	if a < b {
		return a
	}
	return b
}
