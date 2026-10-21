package index

import (
	"os"
	"testing"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

const samplePer = `SCHEMA formonly

LAYOUT (text=%"sym")
GRID
{
  Name:  [f1        ]
  Phone: [f2        ]
}
END
END

ATTRIBUTES
EDIT f1 = formonly.cust_name, REQUIRED;
EDIT f2 = formonly.phone;
Group group01:group01, text="QBE";
ButtonEdit f3 = formonly.act, action=zoom;
END

INSTRUCTIONS
SCREEN RECORD sym_rec(formonly.cust_name, formonly.phone)
END
`

func buildPerIndex(t *testing.T, src string) *FileIndex {
	t.Helper()

	pool, err := parser.New()
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	t.Cleanup(pool.Close)

	tree, err := pool.Parse(parser.LangPer, []byte(src), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	t.Cleanup(tree.Close)

	if root := tree.RootNode(); root.HasError() {
		t.Fatalf("样例有语法错误：%s", root.ToSexp())
	}
	return BuildPer(tree, []byte(src), 1)
}

func TestPerItems(t *testing.T) {
	fi := buildPerIndex(t, samplePer)

	names := map[string]bool{}
	for _, sym := range fi.TopLevel {
		if sym.Kind == KindFormItem {
			names[sym.Name] = true
		}
	}
	for _, want := range []string{"f1", "f2", "group01", "f3"} {
		if !names[want] {
			t.Errorf("未找到 item %q，实际: %v", want, names)
		}
	}
}

func TestPerItemType(t *testing.T) {
	fi := buildPerIndex(t, samplePer)

	syms := fi.Lookup("f1")
	if len(syms) != 1 {
		t.Fatalf("f1 符号数 = %d", len(syms))
	}
	if got := syms[0].TypeText; got != "EDIT" {
		t.Errorf("f1 TypeText = %q，期望 EDIT", got)
	}

	syms = fi.Lookup("f3")
	if len(syms) != 1 {
		t.Fatal("未找到 f3")
	}
	if got := syms[0].TypeText; got != "ButtonEdit" {
		t.Errorf("f3 TypeText = %q，期望 ButtonEdit", got)
	}
}

func TestPerScreenRecord(t *testing.T) {
	fi := buildPerIndex(t, samplePer)

	syms := fi.Lookup("sym_rec")
	if len(syms) != 1 {
		t.Fatalf("sym_rec 符号数 = %d", len(syms))
	}
	rec := syms[0]
	if rec.Kind != KindScreenRec {
		t.Errorf("Kind = %v，期望 screen_record", rec.Kind)
	}
	if len(rec.Children) != 2 {
		t.Fatalf("字段数 = %d，期望 2：%+v", len(rec.Children), rec.Children)
	}
	got := map[string]bool{}
	for _, ch := range rec.Children {
		got[ch.Name] = true
		if ch.Kind != KindField {
			t.Errorf("%s Kind = %v", ch.Name, ch.Kind)
		}
	}
	if !got["cust_name"] || !got["phone"] {
		t.Errorf("字段 = %v", got)
	}
}

func TestPerCaseInsensitiveLookup(t *testing.T) {
	fi := buildPerIndex(t, samplePer)
	for _, name := range []string{"f1", "F1", "F1"} {
		if len(fi.Lookup(lower(name))) != 1 {
			t.Errorf("Lookup(%q) 未命中", name)
		}
	}
}

func TestRealPerFileSymbols(t *testing.T) {
	src, err := os.ReadFile("/code/workspace/tiptop-gp/tiptop/amm/per/ammr110.per")
	if err != nil {
		t.Skipf("缺少真实语料: %v", err)
	}

	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	tree, err := pool.Parse(parser.LangPer, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	fi := BuildPer(tree, src, 1)
	if len(fi.TopLevel) < 10 {
		t.Errorf("真实 .per 应有较多 item，实际 %d", len(fi.TopLevel))
	}

	// 已知 ammr110.per 有 mmg01 与 more
	if len(fi.Lookup("mmg01")) == 0 {
		t.Error("未找到 mmg01")
	}
	if len(fi.Lookup("more")) == 0 {
		t.Error("未找到 more")
	}
}

// 真实语料的单元格写法：一个 item_tag 里并排放多个 tag。
const multiTagPer = `SCHEMA ds

LAYOUT (text=%"Form")
GRID
{
   [label1     |alh01                ]
   [mor|label4                   ]
}
END
END

ATTRIBUTES
Group group1:Group1, text=%"QBE";
Label label1:Label1, text=%"alh01";
Edit alh01=alh_file.alh01, tabIndex=1;
CheckBox mor=formonly.more type VARCHAR, tabIndex=4;
Label label4:Label4, text=%"";
END

INSTRUCTIONS
SCREEN RECORD Rec1(alh_file.alh01, formonly.more)
END
`

func TestPerLayoutCellSplitsOnPipe(t *testing.T) {
	fi := buildPerIndex(t, multiTagPer)

	got := map[string]bool{}
	for _, ref := range fi.LayoutRefs {
		got[ref.Lower] = true
	}
	// 旧实现把整个单元格当一个 tag（"label1     |alh01"），一个都匹配不上。
	for _, want := range []string{"label1", "alh01", "mor", "label4"} {
		if !got[want] {
			t.Errorf("布局 tag %q 未收集到，实际 %v", want, got)
		}
	}
	if len(fi.LayoutRefs) != 4 {
		t.Errorf("期望 4 个 tag，实际 %d：%+v", len(fi.LayoutRefs), fi.LayoutRefs)
	}

	// 区间必须落在各自的 tag 上（诊断位置靠它）。
	for _, ref := range fi.LayoutRefs {
		if ref.EndByte <= ref.StartByte {
			t.Errorf("tag %q 区间非法：%d..%d", ref.Tag, ref.StartByte, ref.EndByte)
			continue
		}
		if slice := multiTagPer[ref.StartByte:ref.EndByte]; slice != ref.Tag {
			t.Errorf("tag %q 的区间指向 %q", ref.Tag, slice)
		}
	}
}

func TestPerFormFieldNamesCollected(t *testing.T) {
	fi := buildPerIndex(t, multiTagPer)

	got := map[string]bool{}
	for _, f := range fi.FormFieldNames {
		got[f.Lower] = true
	}
	// SCREEN RECORD 引用的是字段名：alh_file.alh01 的 alh01、formonly.more 的 more。
	if !got["alh01"] {
		t.Errorf("未收集字段名 alh01：%v", got)
	}
	if !got["more"] {
		t.Errorf("未收集字段名 more（Edit mor=formonly.more）：%v", got)
	}
	// item tag 不是字段名，不该混进来（mor 只在 tag 位置出现）。
	if got["mor"] {
		t.Errorf("item tag mor 不应被当成字段名")
	}
}
