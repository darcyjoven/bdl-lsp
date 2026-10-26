// Package schema 读取数据库"字段资料"（PLUGIN-PLAN P2-1）。
//
// 资料由 tiptop 生产（`tiptop schema import`，来源是 Oracle 导出 CSV），
// 落在项目里的 `.project/schema/`（旧 `.project/.schema/` 只读兼容）：
//
//	tables.json           总索引：表 → {name, desc, columns, file}
//	tables/<TABLE>.json   一张表的列（展开/悬停时才读）
//
// 设计取向（与链接资料一致）：
//   - bdl-lsp **只读**，写盘是 tiptop 的职责；
//   - 一个表一个文件 + 惰性加载：语料量级（上千表 / 几万列）下，
//     悬停只读一个表文件，视图展开也只读当前展开的表；
//   - 旧版"扫各模组下分散的 sch/*.sch"已按用户要求**下线**（P2-1），
//     现在只有这一种来源。
package schema

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// ErrNoData 表示数据目录里没有字段资料（还没导入过）。
var ErrNoData = errors.New("没有字段资料（.project/schema/tables.json 不存在；旧目录 .project/.schema 也认）")

// FileName 是总索引文件名。
const FileName = "tables.json"

// SubDir 是每表文件所在的子目录名。
const SubDir = "tables"

// DefaultDirName 是默认字段资料目录（相对项目根）：`.project/schema`。
//
// W6（Windows 实机反馈）：与链接资料一起目录化——`.project` 根只留
// `secrets.json`，字段资料在 `.project/schema/`（注意没有点号）。
const DefaultDirName = ".project/schema"

// LegacyDirNames 是历史字段资料目录（**只读兼容**，按“新→旧”排列）。
var LegacyDirNames = []string{".project/.schema"}

// HasData 报告目录下是否存在总索引 tables.json。
func HasData(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, FileName))
	return err == nil
}

// ResolveDataDir 选择实际使用的字段资料目录。
//
// 规则与 link.ResolveDataDir 一致：显式配置优先；否则新目录 → 历史目录；
// 都不存在时返回默认新目录（供首次导入写盘）。
func ResolveDataDir(root, configured string) string {
	if configured != "" && configured != DefaultDirName {
		return configured
	}
	for _, d := range append([]string{DefaultDirName}, LegacyDirNames...) {
		dir := d
		if !filepath.IsAbs(dir) {
			dir = filepath.Join(root, d)
		}
		if HasData(dir) {
			return d
		}
	}
	return DefaultDirName
}

// SchemaVersion 是资料格式版本（与 tiptop 的写入端一致）。
const SchemaVersion = 1

// Column 是表里的一列。
type Column struct {
	// Name 是原始写法，Lower 是小写（Genero 大小写不敏感）。
	Name  string `json:"name"`
	Lower string `json:"-"`
	// Desc 是中文说明（Oracle 导出里的 column_desc），可能为空。
	Desc string `json:"desc,omitempty"`
	// Type 是类型原文（如 CHAR / NUMBER / VARCHAR2）。
	Type string `json:"type,omitempty"`
	// Length 是 data_length（为 0 时写入端已回退 char_length）。
	Length     int `json:"length,omitempty"`
	CharLength int `json:"charLength,omitempty"`
	Precision  int `json:"precision,omitempty"`
	Scale      int `json:"scale,omitempty"`
	// Nullable 表示可空。
	Nullable bool `json:"nullable,omitempty"`
	// DefaultLength 是 Oracle 导出里 `default_legth` 列的值。实测它是**默认长度**
	// （0/2/4/8…）而不是默认值表达式，所以只保留数据、不在悬停/补全里展示。
	DefaultLength string `json:"defaultLength,omitempty"`
}

// Display 返回 `CHAR(10)` 这样的展示文本（长度/精度按类型拼装）。
func (c Column) Display() string {
	t := strings.ToUpper(strings.TrimSpace(c.Type))
	if t == "" {
		return ""
	}
	switch {
	case c.Scale > 0:
		return fmt.Sprintf("%s(%d,%d)", t, c.Precision, c.Scale)
	case c.Precision > 0 && strings.Contains(t, "NUMBER"):
		return fmt.Sprintf("%s(%d)", t, c.Precision)
	case c.Length > 0:
		return fmt.Sprintf("%s(%d)", t, c.Length)
	default:
		return t
	}
}

// HoverLine 返回悬停/补全里的一行：`ima01 · CHAR(10) · 物料编号`。
func (c Column) HoverLine() string {
	parts := []string{c.Name}
	if d := c.Display(); d != "" {
		parts = append(parts, d)
	}
	if c.Desc != "" {
		parts = append(parts, c.Desc)
	}
	return strings.Join(parts, " · ")
}

// Table 是一张表的列。
type Table struct {
	Name  string
	Lower string
	Desc  string
	// Columns 按资料里的顺序。
	Columns []Column

	byName map[string]bool
}

// Column 按名查列（大小写不敏感）。
func (t *Table) Column(name string) (Column, bool) {
	if t == nil {
		return Column{}, false
	}
	lower := strings.ToLower(name)
	for _, c := range t.Columns {
		if c.Lower == lower {
			return c, true
		}
	}
	return Column{}, false
}

// Has 报告表里是否有某一列（大小写不敏感）。
func (t *Table) Has(col string) bool {
	if t == nil {
		return false
	}
	return t.byName[strings.ToLower(col)]
}

// Entry 是总索引里的一张表。
type Entry struct {
	Name    string `json:"name"`
	Desc    string `json:"desc,omitempty"`
	Columns int    `json:"columns"`
	File    string `json:"file"`
}

// indexFile 是 tables.json 的结构。
type indexFile struct {
	Schema     int               `json:"schema"`
	ImportedAt string            `json:"importedAt,omitempty"`
	Source     string            `json:"source,omitempty"`
	RowsTotal  int               `json:"rowsTotal,omitempty"`
	Tables     map[string]*Entry `json:"tables"`
}

// tableFile 是单张表文件的结构。
type tableFile struct {
	Table   string   `json:"table"`
	Desc    string   `json:"desc,omitempty"`
	Columns []Column `json:"columns"`
}

// Data 是一份已加载的字段资料（表文件按需读取并缓存）。
type Data struct {
	dir        string
	Schema     int
	ImportedAt string
	Source     string
	RowsTotal  int
	entries    map[string]*Entry // 大写表名 → 条目
	names      []string          // 排序后的表名（原写法）

	mu    sync.Mutex
	cache map[string]*Table
}

// LoadData 读取数据目录里的总索引（不读任何表文件）。
//
// 目录不存在或没有 tables.json → ErrNoData（"还没导入过"是常态，不是错误）。
func LoadData(dir string) (*Data, error) {
	if dir == "" {
		return nil, ErrNoData
	}
	raw, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoData
		}
		return nil, fmt.Errorf("读取 %s: %w", filepath.Join(dir, FileName), err)
	}
	var idx indexFile
	if err := json.Unmarshal(raw, &idx); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", filepath.Join(dir, FileName), err)
	}
	if idx.Schema > SchemaVersion {
		return nil, fmt.Errorf("字段资料版本 %d 高于本程序支持的 %d，请升级 bdl-lsp",
			idx.Schema, SchemaVersion)
	}
	d := &Data{
		dir:        dir,
		Schema:     idx.Schema,
		ImportedAt: idx.ImportedAt,
		Source:     idx.Source,
		RowsTotal:  idx.RowsTotal,
		entries:    make(map[string]*Entry, len(idx.Tables)),
		cache:      make(map[string]*Table, 64),
	}
	for key, e := range idx.Tables {
		if e == nil {
			continue
		}
		upper := strings.ToUpper(key)
		cp := *e
		if cp.Name == "" {
			cp.Name = key
		}
		if cp.File == "" {
			cp.File = upper + ".json"
		}
		d.entries[upper] = &cp
		d.names = append(d.names, cp.Name)
	}
	sort.Strings(d.names)
	return d, nil
}

// Len 是表数。
func (d *Data) Len() int {
	if d == nil {
		return 0
	}
	return len(d.entries)
}

// TableNames 返回全部表名（按字典序，原写法）。
func (d *Data) TableNames() []string {
	if d == nil {
		return nil
	}
	return append([]string(nil), d.names...)
}

// Entry 按名查索引条目（大小写不敏感）。
func (d *Data) Entry(name string) (*Entry, bool) {
	if d == nil {
		return nil, false
	}
	e, ok := d.entries[strings.ToUpper(name)]
	return e, ok
}

// Table 返回一张表的列（**惰性**读表文件并缓存）。
//
// 返回 false 表示资料里没有这张表，或者表文件读不出来/坏了——
// 调用方一律按"没有更精确的信息"处理，不报错（宁可不报）。
func (d *Data) Table(name string) (*Table, bool) {
	if d == nil {
		return nil, false
	}
	upper := strings.ToUpper(name)
	e, ok := d.entries[upper]
	if !ok {
		return nil, false
	}
	d.mu.Lock()
	if t, hit := d.cache[upper]; hit {
		d.mu.Unlock()
		return t, t != nil
	}
	d.mu.Unlock()

	t := d.readTable(upper, e)
	d.mu.Lock()
	d.cache[upper] = t // nil 也缓存：坏文件不要每次悬停都重读
	d.mu.Unlock()
	return t, t != nil
}

func (d *Data) readTable(upper string, e *Entry) *Table {
	raw, err := os.ReadFile(filepath.Join(d.dir, SubDir, e.File))
	if err != nil {
		return nil
	}
	var tf tableFile
	if err := json.Unmarshal(raw, &tf); err != nil {
		return nil
	}
	t := &Table{Name: e.Name, Lower: strings.ToLower(e.Name), Desc: tf.Desc, byName: map[string]bool{}}
	if t.Desc == "" {
		t.Desc = e.Desc
	}
	for _, c := range tf.Columns {
		if c.Name == "" {
			continue
		}
		if c.Lower == "" {
			c.Lower = strings.ToLower(c.Name)
		}
		t.Columns = append(t.Columns, c)
		t.byName[c.Lower] = true
	}
	return t
}

// Search 按**正则**过滤表名（大小写不敏感），返回匹配的表与表总数。
//
// pattern 为空表示不过滤。正则**非法**时：返回全部表 + err（界面把 err 显示出来）。
// 为什么是"全部"而不是"零条"：查询框是边打边过滤的，用户打到一半
// （`(apg|aph`）时正则必然非法；这时显示"没有匹配"会让人以为资料死了，
// 显示全部 + 一行"正则无效"更不吓人，也不会闪。
//
// limit<=0 表示不限制。
func (d *Data) Search(pattern string, limit int) (matched []*Entry, total int, err error) {
	if d == nil {
		return nil, 0, nil
	}
	total = len(d.names)
	if pattern == "" {
		matched = d.allEntries()
	} else {
		re, cerr := regexp.Compile("(?i)" + pattern)
		if cerr != nil {
			err = cerr
			matched = d.allEntries()
		} else {
			for _, name := range d.names {
				if re.MatchString(name) {
					matched = append(matched, d.entries[strings.ToUpper(name)])
				}
			}
		}
	}
	if limit > 0 && len(matched) > limit {
		matched = matched[:limit]
	}
	return matched, total, err
}

func (d *Data) allEntries() []*Entry {
	out := make([]*Entry, 0, len(d.names))
	for _, name := range d.names {
		out = append(out, d.entries[strings.ToUpper(name)])
	}
	return out
}
