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
