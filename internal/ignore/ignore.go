// Package ignore 实现 `bdl.ignore`：按文件/字段/表单 glob 忽略诊断（P8）。
//
// 三类模式彼此独立：
//   - Files 匹配当前文件（工作区相对路径；不含 `/` 的模式也会按 basename
//     匹配，所以 `*.bak.4gl` 能命中任意目录）；
//   - Fields 匹配字段名（大小写不敏感）；
//   - Forms 匹配表单字面量、文件名或相对路径（大小写不敏感）。
//
// 为什么不在发布诊断后由客户端过滤：客户端过滤器看不到"字段属于哪张表单"，
// 也拿不到解析结果；这里的匹配必须基于解析器给出的 Literal/FormPath。
package ignore

import (
	"path/filepath"
	"strings"

	"github.com/darcyjoven/bdl-lsp/glob"
	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
)

// Matcher 是编译后的忽略规则。nil 与空 Matcher 都表示"什么都不忽略"。
type Matcher struct {
	root   string
	files  glob.Set
	fields glob.Set
	forms  glob.Set
}

// New 编译忽略规则。
//
// 非法模式会被跳过并记入 problems（而不是整体失败）：一条写错不该让
// 其余忽略规则失效；调用方负责把 problems 写进日志。
func New(root string, files, fields, forms []string) (*Matcher, []string) {
	m := &Matcher{root: root}
	var problems []string
	m.files, problems = compile(problems, "ignore.files", files, false)
	m.fields, problems = compile(problems, "ignore.fields", fields, true)
	m.forms, problems = compile(problems, "ignore.forms", forms, true)
	return m, problems
}

func compile(problems []string, field string, patterns []string, lower bool) (glob.Set, []string) {
	var set glob.Set
	for _, p := range patterns {
		if lower {
			p = strings.ToLower(p)
		}
		g, err := glob.Compile(p)
		if err != nil {
			problems = append(problems, field+": "+err.Error())
			continue
		}
		set = append(set, g)
	}
	return set, problems
}

// Empty 报告是否没有任何规则。
func (m *Matcher) Empty() bool {
	return m == nil || (m.files.Empty() && m.fields.Empty() && m.forms.Empty())
}

// File 报告某个文件（URI 或本地路径）是否被整体忽略。
func (m *Matcher) File(uriOrPath string) bool {
	if m == nil || m.files.Empty() {
		return false
	}
	p := uriOrPath
	if strings.HasPrefix(p, "file://") {
		if decoded, err := fileuri.ToPath(p); err == nil && decoded != "" {
			p = decoded
		}
	}
	rel := m.rel(p)
	if rel == "" {
		return false
	}
	// 相对路径优先；无 `/` 的模式（如 *.bak.4gl）同时按 basename 匹配。
	return m.files.Match(rel) || m.files.Match(filepath.Base(rel))
}

// Field 报告某个字段名是否被忽略。
func (m *Matcher) Field(name string) bool {
	if m == nil || m.fields.Empty() || name == "" {
		return false
	}
	return m.fields.Match(strings.ToLower(name))
}

// Form 报告某张表单是否被忽略。
//
// literal 是 4GL 里的原始字面量（可能不带扩展名），formPath 是解析到的
// .per 绝对路径（可能为空）。两者都试，用户写 `cust` / `cust.per` /
// `**/per/cust.per` 都能命中。
func (m *Matcher) Form(literal, formPath string) bool {
	if m == nil || m.forms.Empty() {
		return false
	}
	var candidates []string
	add := func(s string) {
		if s != "" {
			candidates = append(candidates, strings.ToLower(s))
		}
	}
	base := func(p string) string {
		p = strings.ReplaceAll(p, "\\", "/")
		return p[strings.LastIndex(p, "/")+1:]
	}
	add(strings.TrimSpace(literal))
	if l := strings.TrimSpace(literal); l != "" {
		add(base(l))
		add(strings.TrimSuffix(base(l), ".per"))
	}
	if formPath != "" {
		add(formPath)
		add(m.rel(formPath))
		add(base(formPath))
		add(strings.TrimSuffix(base(formPath), ".per"))
	}
	for _, c := range candidates {
		if m.forms.Match(c) {
			return true
		}
	}
	return false
}

// rel 把本地路径转成相对工作区根的 / 分隔路径；无法相对化时退回原路径。
func (m *Matcher) rel(p string) string {
	p = strings.ReplaceAll(p, "\\", "/")
	if m.root == "" {
		return p
	}
	if r, err := filepath.Rel(m.root, filepath.FromSlash(p)); err == nil {
		r = strings.ReplaceAll(r, "\\", "/")
		if r != ".." && !strings.HasPrefix(r, "../") {
			return strings.TrimPrefix(r, "./")
		}
	}
	return p
}
