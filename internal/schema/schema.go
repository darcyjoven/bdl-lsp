// Package schema 解析 Genero/Oracle 的 **数据库 schema 文件**（`.sch`），
// 建立"表 → 列"的索引（P6：数据库 schema 索引）。
//
// 为什么需要：`DEFINE r RECORD LIKE maj_file.*` 这种声明在语料里出现
// **21144 次**，记录的成员全部来自数据库表结构，语法树里看不到。没有 schema
// 索引时，`record-member-unknown` 只能对这些记录整体静默（否则成片误报），
// 而它们恰好是记录成员用得最多的地方。
//
// # 语料里的两种语句
//
//	create table mmb_file ( mmb01 varchar2(20) NOT NULL, ... );   -- 3127 个文件
//	alter table bmy_file add ta_bmy01 varchar(40);               -- 182 个文件追加列
//
// 还有 `create unique index` / `create index` 等无关语句（138 处），以及
// 语料里的手写笔误（`varchar2(40);:`）。解析必须**宽容**：认不出来就跳过，
// 绝不能因为一行坏数据让整张表消失——少一列会导致"成员不存在"误报。
package schema

import (
	"os"
	"sort"
	"strings"
	"sync"
)

// Column 是表的一列。
type Column struct {
	// Name 是原始写法，Lower 是小写（Genero 大小写不敏感）。
	Name  string
	Lower string
	// Type 是类型原文（如 `varchar2(20)`），供悬停/补全展示。
	Type string
}

// Table 是一张表。
type Table struct {
	Name    string
	Lower   string
	Columns []Column
	// AlterOnly 表示只见到 `alter table` 而没见到 `create table`
	// （建表语句不在工作区里）。这类表的列可能不全，校验时要保守。
	AlterOnly bool

	byName map[string]bool
}

// Has 报告表里是否有某一列（大小写不敏感）。
func (t *Table) Has(col string) bool {
	if t == nil {
		return false
	}
	return t.byName[strings.ToLower(col)]
}

// Index 是全部 schema 文件的合并结果。
type Index struct {
	mu     sync.RWMutex
	tables map[string]*Table
	// Files 是解析过的文件数（供日志/诊断展示）。
	Files int
	// Problems 是解析时遇到的可疑语句（最多留 50 条，供审计）。
	Problems []string
}

// New 返回空索引。
func New() *Index {
	return &Index{tables: make(map[string]*Table, 1<<12)}
}

// Table 按名字查表（大小写不敏感），返回 nil 表示"没有这张表的信息"。
func (ix *Index) Table(name string) *Table {
	if ix == nil {
		return nil
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return ix.tables[strings.ToLower(strings.TrimSpace(name))]
}

// Len 返回表数。
func (ix *Index) Len() int {
	if ix == nil {
		return 0
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	return len(ix.tables)
}

// TableNames 返回全部表名（排序后，供测试/审计）。
func (ix *Index) TableNames() []string {
	if ix == nil {
		return nil
	}
	ix.mu.RLock()
	defer ix.mu.RUnlock()
	out := make([]string, 0, len(ix.tables))
	for name := range ix.tables {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// Merge 合并一份解析结果（线程安全）。
func (ix *Index) Merge(stmts []Stmt) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for _, st := range stmts {
		switch st.Kind {
		case StmtCreate:
			// 同一张表可能在两个模组目录里各有一份定义（语料实测 69 处，
			// 如 tiptop/amm/sch 与 topcust/*/sch 都有 ada_file）。
			// 取**并集**而不是覆盖：多出来的列最多让校验漏报，
			// 覆盖则会把真实存在的列判成"不存在"——那是误报。
			t := ix.tables[strings.ToLower(st.Table)]
			if t == nil || t.AlterOnly {
				t = &Table{Name: st.Table, Lower: strings.ToLower(st.Table), byName: map[string]bool{}}
				ix.tables[t.Lower] = t
			}
			for _, c := range st.Columns {
				t.addColumn(c)
			}
		case StmtAlter:
			t := ix.tables[strings.ToLower(st.Table)]
			if t == nil {
				t = &Table{Name: st.Table, Lower: strings.ToLower(st.Table), byName: map[string]bool{}, AlterOnly: true}
				ix.tables[t.Lower] = t
			}
			for _, c := range st.Columns {
				t.addColumn(c)
			}
		}
	}
}

func (t *Table) addColumn(c Column) {
	c.Lower = strings.ToLower(c.Name)
	if c.Lower == "" || t.byName[c.Lower] {
		return
	}
	t.byName[c.Lower] = true
	t.Columns = append(t.Columns, c)
}

// LoadFiles 解析并合并一批 .sch 文件。
//
// 单个文件读不出来/解析异常只记录在 Problems 里，不影响其它文件：
// 索引是"锦上添花"，缺一张表最多是少报几条诊断。
func LoadFiles(files []string) *Index {
	ix := New()
	for _, path := range files {
		src, err := os.ReadFile(path)
		if err != nil {
			ix.addProblem(path + ": " + err.Error())
			continue
		}
		stmts, problems := Parse(src)
		for _, p := range problems {
			ix.addProblem(path + ": " + p)
		}
		ix.Merge(stmts)
		ix.Files++
	}
	return ix
}

func (ix *Index) addProblem(msg string) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	if len(ix.Problems) < 50 {
		ix.Problems = append(ix.Problems, msg)
	}
}
