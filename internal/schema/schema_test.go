package schema

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// writeData 造一份最小字段资料：
//
//	IMA_FILE  3 列（含中文说明）
//	APA_FILE  1 列
func writeData(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, SubDir), 0o755); err != nil {
		t.Fatal(err)
	}
	idx := `{
	  "schema": 1,
	  "importedAt": "2026-10-23T21:00:00+08:00",
	  "source": "gal_schema.csv",
	  "rowsTotal": 4,
	  "tables": {
	    "IMA_FILE": {"name": "ima_file", "desc": "", "columns": 3, "file": "IMA_FILE.json"},
	    "APA_FILE": {"name": "apa_file", "desc": "资料档", "columns": 1, "file": "APA_FILE.json"}
	  }
	}`
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(idx), 0o644); err != nil {
		t.Fatal(err)
	}
	ima := `{"table":"IMA_FILE","columns":[
	  {"name":"ima01","desc":"物料编号","type":"CHAR","length":10,"nullable":false},
	  {"name":"ima02","desc":"数量","type":"NUMBER","length":0,"precision":5,"scale":2},
	  {"name":"ima03","type":"VARCHAR2","charLength":20,"nullable":true,"defaultLength":"4"}
	]}`
	if err := os.WriteFile(filepath.Join(dir, SubDir, "IMA_FILE.json"), []byte(ima), 0o644); err != nil {
		t.Fatal(err)
	}
	apa := `{"table":"APA_FILE","desc":"资料档","columns":[{"name":"apa01","type":"VARCHAR2","length":10}]}`
	if err := os.WriteFile(filepath.Join(dir, SubDir, "APA_FILE.json"), []byte(apa), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadDataAndLazyTable(t *testing.T) {
	dir := writeData(t)
	d, err := LoadData(dir)
	if err != nil {
		t.Fatalf("LoadData: %v", err)
	}
	if d.Len() != 2 {
		t.Fatalf("表数应为 2，实际 %d", d.Len())
	}
	if d.ImportedAt != "2026-10-23T21:00:00+08:00" || d.RowsTotal != 4 {
		t.Errorf("元信息不对：%+v", d)
	}
	if names := d.TableNames(); len(names) != 2 || names[0] != "apa_file" || names[1] != "ima_file" {
		t.Errorf("表名应排序为 [apa_file ima_file]，实际 %v", names)
	}

	// 大小写不敏感 + 惰性加载
	tb, ok := d.Table("IMA_file")
	if !ok || tb == nil {
		t.Fatal("IMA_file 应能取到")
	}
	if len(tb.Columns) != 3 {
		t.Fatalf("列数应为 3，实际 %d", len(tb.Columns))
	}
	if !tb.Has("IMA02") {
		t.Error("Has 应大小写不敏感")
	}
	if c, ok := tb.Column("ima01"); !ok || c.Desc != "物料编号" {
		t.Errorf("单列查询不对：%+v ok=%v", c, ok)
	}

	// 表文件不存在 → 返回 false（不报错）
	if err := os.Remove(filepath.Join(dir, SubDir, "APA_FILE.json")); err != nil {
		t.Fatal(err)
	}
	fresh, err := LoadData(dir)
	if err != nil {
		t.Fatal(err)
	}
	if tb, ok := fresh.Table("apa_file"); ok || tb != nil {
		t.Errorf("表文件缺失时不应返回表：%+v ok=%v", tb, ok)
	}
	if _, ok := fresh.Table("nosuch"); ok {
		t.Error("不存在的表应返回 false")
	}
}

func TestLoadDataNoDataAndVersion(t *testing.T) {
	if _, err := LoadData(t.TempDir()); !errors.Is(err, ErrNoData) {
		t.Errorf("没有 tables.json 应返回 ErrNoData，实际 %v", err)
	}
	if _, err := LoadData(""); !errors.Is(err, ErrNoData) {
		t.Errorf("空目录应返回 ErrNoData，实际 %v", err)
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte(`{"schema":99,"tables":{}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadData(dir); err == nil {
		t.Error("版本过高应报错")
	}
	// 坏 JSON 也要报错（不能让视图静默空白）
	if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadData(dir); err == nil {
		t.Error("坏 JSON 应报错")
	}
}

func TestColumnDisplayAndHoverLine(t *testing.T) {
	cases := []struct {
		col     Column
		display string
		line    string
	}{
		{Column{Name: "ima01", Type: "CHAR", Length: 10, Desc: "物料编号"}, "CHAR(10)", "ima01 · CHAR(10) · 物料编号"},
		{Column{Name: "n", Type: "NUMBER", Precision: 5, Scale: 2}, "NUMBER(5,2)", "n · NUMBER(5,2)"},
		{Column{Name: "d", Type: "DATE"}, "DATE", "d · DATE"},
		{Column{Name: "e", Type: "", Desc: "没类型"}, "", "e · 没类型"},
		{Column{Name: "s", Type: "VARCHAR2", Length: 0, CharLength: 20}, "VARCHAR2", "s · VARCHAR2"},
	}
	for _, c := range cases {
		if got := c.col.Display(); got != c.display {
			t.Errorf("Display(%+v) = %q，期望 %q", c.col, got, c.display)
		}
		if got := c.col.HoverLine(); got != c.line {
			t.Errorf("HoverLine(%+v) = %q，期望 %q", c.col, got, c.line)
		}
	}
}

func TestSearchRegexAndFallback(t *testing.T) {
	dir := writeData(t)
	d, err := LoadData(dir)
	if err != nil {
		t.Fatal(err)
	}

	// 空 pattern：全部
	got, total, err := d.Search("", 0)
	if err != nil || total != 2 || len(got) != 2 {
		t.Fatalf("空 pattern：matched=%d total=%d err=%v", len(got), total, err)
	}

	// 正则 + 大小写不敏感
	got, total, err = d.Search("^APA", 0)
	if err != nil || total != 2 || len(got) != 1 || got[0].Name != "apa_file" {
		t.Fatalf("^APA：matched=%v total=%d err=%v", got, total, err)
	}
	got, _, _ = d.Search("(ima|apa)_file", 0)
	if len(got) != 2 {
		t.Errorf("分组正则应匹配两张表，实际 %d", len(got))
	}

	// limit 截断
	got, total, _ = d.Search("", 1)
	if len(got) != 1 || total != 2 {
		t.Errorf("limit=1 应返回 1 条、total 仍为 2：%d/%d", len(got), total)
	}

	// 非法正则：报错 + 返回全部（边打边过滤时不该闪成"没有匹配"）
	got, total, err = d.Search("(apg|aph", 0)
	if err == nil {
		t.Error("非法正则应返回 err（供界面提示）")
	}
	if len(got) != 2 || total != 2 {
		t.Errorf("非法正则应退回全部：matched=%d total=%d", len(got), total)
	}
}

func TestTableColumnAndHas(t *testing.T) {
	tb := &Table{Name: "t", Columns: []Column{{Name: "a", Lower: "a"}}, byName: map[string]bool{"a": true}}
	if _, ok := tb.Column("A"); !ok {
		t.Error("Column 应大小写不敏感（Lower 已填）")
	}
	if !tb.Has("A") {
		t.Error("Has 应大小写不敏感")
	}
	var nilT *Table
	if nilT.Has("a") {
		t.Error("nil 表的 Has 应返回 false")
	}
	if _, ok := nilT.Column("a"); ok {
		t.Error("nil 表的 Column 应返回 false")
	}
}

// TestResolveDataDir 钉住 W6 的字段资料目录选择规则。
func TestResolveDataDir(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		t.Helper()
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, FileName), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if got := ResolveDataDir(root, ""); got != DefaultDirName {
		t.Fatalf("没有任何资料时应返回默认新目录，实际 %q", got)
	}
	write(".project/.schema")
	if got := ResolveDataDir(root, ""); got != ".project/.schema" {
		t.Fatalf("只有旧布局时应回退 .project/.schema，实际 %q", got)
	}
	write(".project/schema")
	if got := ResolveDataDir(root, ""); got != DefaultDirName {
		t.Fatalf("新布局存在时应优先新目录，实际 %q", got)
	}
	if got := ResolveDataDir(root, "custom"); got != "custom" {
		t.Fatalf("显式配置应原样返回，实际 %q", got)
	}
}
