package schema

import (
	"os"
	"path/filepath"
	"testing"
)

const sampleDDL = `
/*
 檔案代號:mmb_file
*/
create table mmb_file
(
mmb01       varchar2(20) NOT NULL,   /*需求单编号*/
mmb02       number(5) NOT NULL,
mmb09       number(15,3),
mmb12       date,
MMB13       varchar2(1),
   primary key (mmb01, mmb02),
   constraint fk_x foreign key (mmb03) references other_file (o01)
);
create unique index mmb_idx on mmb_file (mmb01);
alter table mmb_file add ta_mmb01 varchar(40);
alter table bmy_file add ( ta_bmy01 varchar(40), ta_bmy02 number );
`

func TestParseCreateAndAlter(t *testing.T) {
	stmts, problems := Parse([]byte(sampleDDL))
	if len(problems) != 0 {
		t.Errorf("不应有可疑语句：%v", problems)
	}
	var create *Stmt
	var alters []Stmt
	for i := range stmts {
		switch stmts[i].Kind {
		case StmtCreate:
			create = &stmts[i]
		case StmtAlter:
			alters = append(alters, stmts[i])
		}
	}
	if create == nil {
		t.Fatal("没解析出 create table")
	}
	if create.Table != "mmb_file" {
		t.Errorf("表名 = %q", create.Table)
	}
	if len(create.Columns) != 5 {
		t.Fatalf("列数 = %d，期望 5：%+v", len(create.Columns), create.Columns)
	}
	if create.Columns[0].Name != "mmb01" || create.Columns[0].Type == "" {
		t.Errorf("第一列 = %+v", create.Columns[0])
	}
	// 约束项不能被当成列
	for _, c := range create.Columns {
		switch c.Lower {
		case "primary", "constraint", "foreign", "key":
			t.Errorf("约束被当成列：%+v", c)
		}
	}
	if len(alters) != 2 {
		t.Fatalf("alter 数 = %d，期望 2", len(alters))
	}
	if alters[0].Table != "mmb_file" || len(alters[0].Columns) != 1 {
		t.Errorf("alter[0] = %+v", alters[0])
	}
	if alters[1].Table != "bmy_file" || len(alters[1].Columns) != 2 {
		t.Errorf("alter 括号多列 = %+v", alters[1])
	}
}

func TestParseToleratesCorpusTypos(t *testing.T) {
	// 语料里真实存在的笔误与截断
	src := `alter table bmy_file add ta_bmy02  varchar2(40);:
create table trunc_file (
tr01 varchar2(10) NOT NULL,
tr02 number(5)
`
	stmts, _ := Parse([]byte(src))
	var trunc, alter bool
	for _, st := range stmts {
		if st.Kind == StmtCreate && st.Table == "trunc_file" {
			trunc = true
			if len(st.Columns) != 2 {
				t.Errorf("截断文件应仍解析出 2 列，实际 %d", len(st.Columns))
			}
		}
		if st.Kind == StmtAlter && st.Table == "bmy_file" && len(st.Columns) == 1 {
			alter = true
		}
	}
	if !trunc || !alter {
		t.Errorf("容错失败：trunc=%v alter=%v", trunc, alter)
	}
}

// TestMergeOrderIndependent 验证 CREATE / ALTER 的到达顺序不影响结果。
//
// 背景：LoadFiles 按传入的文件顺序合并；ALTER 先到会把表标成 AlterOnly，
// 之后 CREATE 到达时若换一张新表，ALTER 加的列就丢了。Directory 遍历顺序、
// 多模组目录、并发导入都可能让 ALTER 先到，症状是间歇性的"缺少列"。
func TestMergeOrderIndependent(t *testing.T) {
	create, problems := Parse([]byte("create table apa_file (apa01 varchar2(10), apa02 number(5));"))
	if len(problems) != 0 {
		t.Fatalf("create 解析有可疑语句：%v", problems)
	}
	alter, problems := Parse([]byte("alter table apa_file add apa03 date;"))
	if len(problems) != 0 {
		t.Fatalf("alter 解析有可疑语句：%v", problems)
	}

	for _, order := range []struct {
		name  string
		stmts []Stmt
	}{
		{"create 先", append(append([]Stmt{}, create...), alter...)},
		{"alter 先", append(append([]Stmt{}, alter...), create...)},
	} {
		t.Run(order.name, func(t *testing.T) {
			ix := New()
			ix.Merge(order.stmts)
			tb := ix.Table("APA_FILE")
			if tb == nil {
				t.Fatal("查不到 apa_file")
			}
			if tb.AlterOnly {
				t.Error("有 create 的表不应标 AlterOnly")
			}
			for _, want := range []string{"apa01", "apa02", "apa03"} {
				if !tb.Has(want) {
					t.Errorf("缺少列 %s：%+v", want, tb.Columns)
				}
			}
		})
	}
}

func TestMergeAndLookup(t *testing.T) {
	ix := LoadFiles(writeSchemaFiles(t, map[string]string{
		"a.sch": "create table apa_file (apa01 varchar2(10), apa02 number(5));",
		"b.sch": "alter table apa_file add apa03 date;",
		"c.sch": "create table orphan_file (o01 varchar2(10));",
	}))
	if ix.Files != 3 {
		t.Errorf("Files = %d", ix.Files)
	}
	tb := ix.Table("APA_FILE") // 大小写不敏感
	if tb == nil {
		t.Fatal("查不到 apa_file")
	}
	for _, want := range []string{"apa01", "apa02", "apa03"} {
		if !tb.Has(want) {
			t.Errorf("缺少列 %s：%+v", want, tb.Columns)
		}
	}
	if tb.AlterOnly {
		t.Error("有 create 的表不应标 AlterOnly")
	}
	orphan := ix.Table("orphan_file")
	if orphan == nil || len(orphan.Columns) != 1 {
		t.Errorf("普通表解析失败：%+v", orphan)
	}
	if got := ix.Table("no_such_table"); got != nil {
		t.Errorf("不存在的表应返回 nil，实际 %+v", got)
	}
	if ix.Len() != 2 {
		t.Errorf("表数 = %d，期望 2", ix.Len())
	}
}

func TestAlterOnlyTable(t *testing.T) {
	ix := LoadFiles(writeSchemaFiles(t, map[string]string{
		"a.sch": "alter table lone_file add l01 varchar2(10);",
	}))
	tb := ix.Table("lone_file")
	if tb == nil || !tb.AlterOnly {
		t.Fatalf("只有 alter 的表应标 AlterOnly：%+v", tb)
	}
}

func writeSchemaFiles(t *testing.T, files map[string]string) []string {
	t.Helper()
	dir := t.TempDir()
	var out []string
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		out = append(out, p)
	}
	return out
}
