package schema

import (
	"fmt"
	"strings"
)

// StmtKind 是识别出的语句类型。
type StmtKind int

const (
	// StmtCreate 是 `create table X ( ... )`。
	StmtCreate StmtKind = iota + 1
	// StmtAlter 是 `alter table X add <列定义>`。
	StmtAlter
)

// Stmt 是一条建表/加列语句。
type Stmt struct {
	Kind    StmtKind
	Table   string
	Columns []Column
}

// 约束关键字：出现在列定义位置时说明这一项是约束而不是列。
//
// `create table` 的 body 里混着 `primary key (a, b)` / `foreign key ...` /
// `unique (...)` / `constraint ...` / `check (...)`；把它们当列会凭空多出
// 名为 "primary" 的列（不致命但会污染索引）。
var constraintKeywords = map[string]bool{
	"primary": true, "foreign": true, "unique": true, "constraint": true,
	"check": true, "index": true, "key": true, "period": true,
	"references": true, "using": true, "partition": true,
}

// Parse 解析一份 .sch 文件的内容。
//
// 返回识别出的语句与"可疑语句"列表（供审计用）。解析刻意宽容：
// 语料里有手写笔误（`varchar2(40);:`）和截断的文件，遇到就跳过，
// 不让一处坏数据毁掉整张表。
func Parse(src []byte) ([]Stmt, []string) {
	text := stripComments(string(src))
	var stmts []Stmt
	var problems []string
	for _, raw := range splitStatements(text) {
		st, ok, problem := parseStatement(raw)
		if problem != "" {
			problems = append(problems, problem)
		}
		if ok {
			stmts = append(stmts, st)
		}
	}
	return stmts, problems
}

// stripComments 去掉 `/* ... */` 与 `--` 注释。
func stripComments(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		switch {
		case i+1 < len(s) && s[i] == '/' && s[i+1] == '*':
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				// 没闭合：后面全是注释
				return b.String()
			}
			i += 2 + end + 2
			b.WriteByte('\n') // 保留行边界，避免把两行拼成一行
		case i+1 < len(s) && s[i] == '-' && s[i+1] == '-':
			end := strings.IndexByte(s[i:], '\n')
			if end < 0 {
				return b.String()
			}
			i += end
		default:
			b.WriteByte(s[i])
			i++
		}
	}
	return b.String()
}

// splitStatements 按顶层 `;` 切分语句（括号内的分号不切）。
func splitStatements(s string) []string {
	var out []string
	depth := 0
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth > 0 {
				depth--
			}
		case ';':
			if depth == 0 {
				if st := strings.TrimSpace(s[start:i]); st != "" {
					out = append(out, st)
				}
				start = i + 1
			}
		}
	}
	if st := strings.TrimSpace(s[start:]); st != "" {
		out = append(out, st)
	}
	return out
}

// parseStatement 解析单条语句；ok=false 表示"与表结构无关"（索引、drop 等）。
//
// 语料里有 `varchar2(40);:` 这类笔误：`;` 切开后下一段以 `:` 开头，
// 而真正的 `create table` 可能就紧跟在同一段里。所以先在语句里找
// `create` / `alter` 起点，前面的垃圾丢掉。
func parseStatement(stmt string) (Stmt, bool, string) {
	stmt = trimToKeyword(stmt, "create", "alter")
	if stmt == "" {
		return Stmt{}, false, ""
	}
	fields := strings.Fields(stmt)
	if len(fields) < 3 {
		return Stmt{}, false, ""
	}
	lower := func(i int) string { return strings.ToLower(fields[i]) }
	if lower(0) != "create" && lower(0) != "alter" {
		return Stmt{}, false, ""
	}

	// CREATE [UNIQUE|OR REPLACE|GLOBAL TEMPORARY] TABLE <name> ( ... )
	if lower(0) == "create" {
		i := 1
		for i < len(fields) && lower(i) != "table" {
			if lower(i) == "index" || lower(i) == "view" || lower(i) == "sequence" ||
				lower(i) == "trigger" || lower(i) == "synonym" || lower(i) == "database" ||
				lower(i) == "schema" {
				return Stmt{}, false, "" // 与表结构无关
			}
			i++
		}
		if i >= len(fields) || len(fields) <= i+1 {
			return Stmt{}, false, ""
		}
		name := unquoteIdent(fields[i+1])
		body, ok := parenBody(stmt)
		if !ok {
			return Stmt{Kind: StmtCreate, Table: name}, true,
				fmt.Sprintf("create table %s 缺少列定义括号", name)
		}
		return Stmt{Kind: StmtCreate, Table: name, Columns: parseColumns(body)}, true, ""
	}

	// ALTER TABLE <name> ADD <列定义> | ADD ( 列定义, ... )
	if len(fields) < 4 || lower(1) != "table" {
		return Stmt{}, false, ""
	}
	name := unquoteIdent(fields[2])
	rest := stmt[len("alter"):]
	idx := strings.Index(strings.ToLower(rest), " add ")
	if idx < 0 {
		return Stmt{}, false, "" // drop/modify 等，不影响列集合
	}
	addPart := strings.TrimSpace(rest[idx+len(" add "):])
	// `alter table t add constraint pk primary key (a)`：加的是约束，不是列。
	if firstWord(addPart) != "" && constraintKeywords[firstWord(addPart)] {
		return Stmt{}, false, ""
	}
	if strings.HasPrefix(addPart, "(") {
		if inner, ok := parenBody(addPart); ok {
			return Stmt{Kind: StmtAlter, Table: name, Columns: parseColumns(inner)}, true, ""
		}
	}
	// 单列形式：`alter table t add col type;`
	cols := parseColumns(addPart)
	if len(cols) == 0 {
		return Stmt{}, false, fmt.Sprintf("alter table %s 认不出列定义", name)
	}
	return Stmt{Kind: StmtAlter, Table: name, Columns: cols[:1]}, true, ""
}

// trimToKeyword 把语句截到第一个关键字（整词）开始处；没有则返回空串。
func trimToKeyword(stmt string, keywords ...string) string {
	low := strings.ToLower(stmt)
	best := -1
	for _, kw := range keywords {
		for off := 0; ; {
			i := strings.Index(low[off:], kw)
			if i < 0 {
				break
			}
			i += off
			// 整词判断：前一个字符不能是标识符字符
			if i == 0 || !isIdentChar(low[i-1]) {
				if best < 0 || i < best {
					best = i
				}
				break
			}
			off = i + len(kw)
		}
	}
	if best < 0 {
		return ""
	}
	return strings.TrimSpace(stmt[best:])
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " \t\r\n("); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(strings.Trim(s, `"`+"'`"))
}

func isIdentChar(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c >= 0x80
}

// parenBody 取出第一对括号之间的内容（忽略字符串字面量里的括号）。
func parenBody(s string) (string, bool) {
	start := strings.IndexByte(s, '(')
	if start < 0 {
		return "", false
	}
	depth := 0
	inQuote := false
	for i := start; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
			// 字符串里的括号不计数
		case c == '(':
			depth++
		case c == ')':
			depth--
			if depth == 0 {
				return s[start+1 : i], true
			}
		}
	}
	// 没闭合（文件被截断）：把剩下的都当 body，至少能拿到大部分列。
	return s[start+1:], true
}

// parseColumns 按顶层逗号切分列定义。
func parseColumns(body string) []Column {
	var out []Column
	for _, part := range splitTopLevel(body) {
		if col, ok := parseColumn(part); ok {
			out = append(out, col)
		}
	}
	return out
}

func splitTopLevel(s string) []string {
	var out []string
	depth := 0
	inQuote := false
	start := 0
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == '\'':
			inQuote = !inQuote
		case inQuote:
		case c == '(':
			depth++
		case c == ')':
			if depth > 0 {
				depth--
			}
		case c == ',':
			if depth == 0 {
				out = append(out, strings.TrimSpace(s[start:i]))
				start = i + 1
			}
		}
	}
	if rest := strings.TrimSpace(s[start:]); rest != "" {
		out = append(out, rest)
	}
	return out
}

// parseColumn 解析一项列定义；约束项返回 ok=false。
func parseColumn(def string) (Column, bool) {
	def = strings.TrimSpace(def)
	if def == "" {
		return Column{}, false
	}
	fields := strings.Fields(def)
	if len(fields) == 0 {
		return Column{}, false
	}
	first := strings.ToLower(strings.Trim(fields[0], `"`+"'`"))
	if constraintKeywords[first] || first == "" {
		return Column{}, false
	}
	name := unquoteIdent(fields[0])
	if name == "" {
		return Column{}, false
	}
	// 类型是"名字之后、约束关键字之前"的部分；直接留整段原文即可，
	// 只有诊断消息会用到它。
	typ := strings.TrimSpace(def[len(fields[0]):])
	return Column{Name: name, Lower: strings.ToLower(name), Type: typ}, true
}

// unquoteIdent 去掉标识符两端的引号与 `schema.` 前缀。
func unquoteIdent(s string) string {
	s = strings.TrimSpace(s)
	s = strings.Trim(s, `"`+"'`")
	if i := strings.LastIndexByte(s, '.'); i >= 0 && i+1 < len(s) {
		s = s[i+1:]
	}
	return s
}
