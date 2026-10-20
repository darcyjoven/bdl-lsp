package form

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

// setupForm 构造 tiptop 布局的测试树：
//
//	dir/
//	  amm/4gl/main.4gl     ← 引用方
//	  amm/per/ammr110.per  ← 表单（R1a 命中）
//	  axm/per/axmt200.per  ← R2/R3 用
//	  cfg/per/axmt200.per  ← R3 消歧用（同名）
func setupForm(t *testing.T) (root, fourgl string, r *Resolver) {
	t.Helper()
	root = t.TempDir()
	for _, d := range []string{"amm/4gl", "amm/per", "axm/per", "cfg/per"} {
		if err := os.MkdirAll(filepath.Join(root, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("amm/per/ammr110.per", perWithFields("f01", "f02", "more"))
	write("axm/per/axmt200.per", perWithFields("axm01"))
	write("cfg/per/axmt200.per", perWithFields("axm01", "axm02"))

	fourgl = filepath.Join(root, "amm", "4gl", "main.4gl")
	write("amm/4gl/main.4gl", "MAIN\nEND MAIN\n")

	pool, err := parser.New()
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	t.Cleanup(pool.Close)
	r = NewResolver(pool, root, nil, nil)
	return root, fourgl, r
}

// perWithFields 生成一个含指定字段的 .per 内容。
func perWithFields(fields ...string) string {
	s := "SCHEMA ds\n\nLAYOUT\nGRID\n{\n"
	for _, f := range fields {
		s += "  [" + f + "        ]\n"
	}
	s += "}\nEND\nEND\n\nATTRIBUTES\n"
	for _, f := range fields {
		s += "EDIT " + f + " = formonly." + f + ";\n"
	}
	s += "END\n"
	return s
}

func TestResolveR1aPerDirectory(t *testing.T) {
	// amm/4gl/main.4gl + "amm/42f/ammr110" → amm/per/ammr110.per（物理布局）
	_, fourgl, r := setupForm(t)
	res := r.Resolve("amm/42f/ammr110", fourgl)
	if res == nil {
		t.Fatal("R1a 应命中 ../per/")
	}
	if want := filepath.Join(filepath.Dir(filepath.Dir(fourgl)), "per", "ammr110.per"); res.Path != want {
		t.Errorf("路径期望 %q，实际 %q", want, res.Path)
	}
	if len(res.Index.Symbols) == 0 {
		t.Error("符号表为空")
	}
}

func TestResolveNotFound(t *testing.T) {
	_, fourgl, r := setupForm(t)
	if res := r.Resolve("no/such/form", fourgl); res != nil {
		t.Errorf("不存在的表单应返回 nil，实际 %v", res.Path)
	}
}

func TestResolveR2BasenameUnique(t *testing.T) {
	// ammr110 在工作区唯一 → basename 命中（R2 在 R1a 未命中时）
	_, fourgl, r := setupForm(t)
	// 字面量不带模块路径（无法走 R1a）
	res := r.Resolve("ammr110", filepath.Join(filepath.Dir(fourgl), "other.4gl"))
	_ = res
	// ammr110 唯一存在于 amm/per：R2 应命中
	if res == nil {
		t.Fatal("basename 唯一应命中")
	}
}

func TestResolveR3ModuleSegment(t *testing.T) {
	// axmt200 有两个（axm/per 与 cfg/per）：字面量首段 axm 消歧
	_, fourgl, r := setupForm(t)
	res := r.Resolve("axm/42f/axmt200", fourgl)
	if res == nil {
		t.Fatal("R3 模块段应消歧命中")
	}
	if filepath.Base(filepath.Dir(filepath.Dir(res.Path))) != "axm" {
		t.Errorf("应命中 axm 目录，实际 %q", res.Path)
	}
}

func TestResolveAmbiguousGivesNil(t *testing.T) {
	// axmt200 双份且字面量首段（cfg）能消歧 → 命中 cfg
	_, fourgl, r := setupForm(t)
	res := r.Resolve("cfg/42f/axmt200", fourgl)
	if res == nil {
		t.Fatal("cfg 段应命中 cfg/per")
	}
	if filepath.Base(filepath.Dir(filepath.Dir(res.Path))) != "cfg" {
		t.Errorf("应命中 cfg 目录，实际 %q", res.Path)
	}
	// 完全无法消歧的字面量（首段两边都不匹配）→ nil
	// 注：axmt200 双份，字面量 xyz/42f/axmt200 的 xyz 段匹配不到任何目录
	res2 := r.Resolve("xyz/42f/axmt200", fourgl)
	if res2 != nil {
		t.Errorf("无法消歧应返回 nil，实际 %q", res2.Path)
	}
}

func TestCheckFieldExists(t *testing.T) {
	_, fourgl, r := setupForm(t)
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	qs := CompileQueries(pool)
	if qs == nil {
		t.Fatal("query 编译失败")
	}
	t.Cleanup(qs.Close)

	src := []byte(`MAIN
  OPEN WINDOW w1 WITH FORM "amm/42f/ammr110"
  INPUT x FROM f01
    AFTER FIELD f01
    AFTER FIELD more
  END INPUT
END MAIN
`)
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)

	issues, _ := r.Check(tree, src, fourgl, qs)
	if len(issues) != 0 {
		for _, iss := range issues {
			t.Errorf("已存在字段不应报错：%s", iss.Message)
		}
	}
}

func TestCheckFieldMissing(t *testing.T) {
	_, fourgl, r := setupForm(t)
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	qs := CompileQueries(pool)
	if qs == nil {
		t.Fatal("query 编译失败")
	}
	t.Cleanup(qs.Close)

	src := []byte(`MAIN
  OPEN WINDOW w1 WITH FORM "amm/42f/ammr110"
  INPUT x FROM f01
    AFTER FIELD no_such
    NEXT FIELD also_missing
  END INPUT
  CONSTRUCT BY NAME q ON gone1, gone2
  DISPLAY "x" TO vanish
  IF INFIELD(ghost) THEN END IF
END MAIN
`)
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)

	issues, _ := r.Check(tree, src, fourgl, qs)
	got := map[string]bool{}
	for _, iss := range issues {
		got[iss.Field] = true
		if iss.Kind != "field-not-in-form" {
			t.Errorf("Kind 期望 field-not-in-form，实际 %q", iss.Kind)
		}
		if iss.FormPath == "" || iss.FormURI == "" {
			t.Errorf("问题应带表单位置：%+v", iss)
		}
	}
	for _, want := range []string{"no_such", "also_missing", "gone1", "gone2", "vanish", "ghost"} {
		if !got[want] {
			t.Errorf("缺失字段 %q 未被报告（全部：%v）", want, got)
		}
	}
}

func TestCheckNoFormNoCheck(t *testing.T) {
	// 没有 WITH FORM 绑定时字段引用不校验（宁可不报不误报）
	_, fourgl, r := setupForm(t)
	pool, _ := parser.New()
	t.Cleanup(pool.Close)
	qs := CompileQueries(pool)
	t.Cleanup(qs.Close)

	src := []byte(`MAIN
  INPUT x FROM f01
    AFTER FIELD any_field
  END INPUT
END MAIN
`)
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)

	if issues, _ := r.Check(tree, src, fourgl, qs); len(issues) != 0 {
		for _, iss := range issues {
			t.Errorf("无表单绑定不应校验：%s", iss.Message)
		}
	}
}

func TestCheckSecondFormSwitches(t *testing.T) {
	// 第二个 WITH FORM 之后字段按新表单校验（作用域跟踪）
	_, fourgl, r := setupForm(t)
	pool, _ := parser.New()
	t.Cleanup(pool.Close)
	qs := CompileQueries(pool)
	t.Cleanup(qs.Close)

	src := []byte(`MAIN
  OPEN WINDOW w1 WITH FORM "amm/42f/ammr110"
  INPUT x FROM f01
    AFTER FIELD f01
  END INPUT
  OPEN WINDOW w2 WITH FORM "axm/42f/axmt200"
  INPUT y FROM axm01
    AFTER FIELD axm01
    AFTER FIELD f01
  END INPUT
END MAIN
`)
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)

	issues, _ := r.Check(tree, src, fourgl, qs)
	if len(issues) != 1 {
		t.Fatalf("只应报 1 个（切换后的 f01），实际 %d", len(issues))
	}
	if issues[0].Field != "f01" {
		t.Errorf("期望 f01，实际 %q", issues[0].Field)
	}
}

func TestCheckDisplayFormByName(t *testing.T) {
	// OPEN FORM x FROM "p" + DISPLAY FORM x：按名字绑定切换
	_, fourgl, r := setupForm(t)
	pool, _ := parser.New()
	t.Cleanup(pool.Close)
	qs := CompileQueries(pool)
	t.Cleanup(qs.Close)

	src := []byte(`MAIN
  OPEN FORM ammr110 FROM "amm/42f/ammr110"
  DISPLAY FORM ammr110
  AFTER FIELD f01
END MAIN
`)
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)

	issues, _ := r.Check(tree, src, fourgl, qs)
	if len(issues) != 0 {
		for _, iss := range issues {
			t.Errorf("DISPLAY FORM 名字绑定应生效：%s", iss.Message)
		}
	}
}

func TestPhantomFieldCountsAsExists(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "m", "per"), 0o755); err != nil {
		t.Fatal(err)
	}
	per := "SCHEMA ds\nATTRIBUTES\nPHANTOM formonly.ph;\nEND\n"
	if err := os.WriteFile(filepath.Join(root, "m", "per", "f.per"), []byte(per), 0o644); err != nil {
		t.Fatal(err)
	}
	pool, _ := parser.New()
	t.Cleanup(pool.Close)
	r := NewResolver(pool, root, nil, nil)
	qs := CompileQueries(pool)
	t.Cleanup(qs.Close)

	src := []byte(`MAIN
  OPEN WINDOW w WITH FORM "m/42f/f"
  AFTER FIELD ph
END MAIN
`)
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)

	if issues, _ := r.Check(tree, src, filepath.Join(root, "m", "4gl", "x.4gl"), qs); len(issues) != 0 {
		for _, iss := range issues {
			t.Errorf("PHANTOM 字段应算存在：%s", iss.Message)
		}
	}
}

func TestScreenRecordFieldCountsAsExists(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "m", "per"), 0o755); err != nil {
		t.Fatal(err)
	}
	per := `SCHEMA ds
ATTRIBUTES
END
INSTRUCTIONS
  SCREEN RECORD sr(sr01, sr02)
END
`
	if err := os.WriteFile(filepath.Join(root, "m", "per", "f.per"), []byte(per), 0o644); err != nil {
		t.Fatal(err)
	}
	pool, _ := parser.New()
	t.Cleanup(pool.Close)
	r := NewResolver(pool, root, nil, nil)
	qs := CompileQueries(pool)
	t.Cleanup(qs.Close)

	src := []byte(`MAIN
  OPEN WINDOW w WITH FORM "m/42f/f"
  AFTER FIELD sr02
END MAIN
`)
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)

	if issues, _ := r.Check(tree, src, filepath.Join(root, "m", "4gl", "x.4gl"), qs); len(issues) != 0 {
		for _, iss := range issues {
			t.Errorf("SCREEN RECORD 字段应算存在：%s", iss.Message)
		}
	}
}

func TestOpenDocPreferred(t *testing.T) {
	_, fourgl, _ := setupForm(t)
	target := filepath.Join(filepath.Dir(filepath.Dir(fourgl)), "per", "ammr110.per")
	targetURI := URIOfPath(target)
	pool, _ := parser.New()
	t.Cleanup(pool.Close)

	memText := perWithFields("f01", "f02", "more", "memonly")
	r := NewResolver(pool, "", nil, func(uri string) ([]byte, bool) {
		if uri == targetURI {
			return []byte(memText), true
		}
		return nil, false
	})
	qs := CompileQueries(pool)
	t.Cleanup(qs.Close)

	src := []byte(`MAIN
  OPEN WINDOW w WITH FORM "amm/42f/ammr110"
  AFTER FIELD memonly
END MAIN
`)
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)

	if issues, _ := r.Check(tree, src, fourgl, qs); len(issues) != 0 {
		for _, iss := range issues {
			t.Errorf("内存版字段应可见：%s", iss.Message)
		}
	}
}

func TestImportersAndRefresh(t *testing.T) {
	_, fourgl, r := setupForm(t)
	docURI := URIOfPath(fourgl)
	target := filepath.Join(filepath.Dir(filepath.Dir(fourgl)), "per", "ammr110.per")

	pool, _ := parser.New()
	t.Cleanup(pool.Close)
	qs := CompileQueries(pool)
	t.Cleanup(qs.Close)
	src := []byte(`MAIN
  OPEN WINDOW w WITH FORM "amm/42f/ammr110"
  AFTER FIELD f01
END MAIN
`)
	tree, _ := pool.Parse(parser.LangBDL, src, nil)
	t.Cleanup(tree.Close)
	_, _ = r.Check(tree, src, fourgl, qs)

	r.AddEdge(docURI, target)
	if got := r.Importers(target); len(got) != 1 || got[0] != docURI {
		t.Fatalf("引用者期望 [%s]，实际 %v", docURI, got)
	}
	r.ClearEdges(docURI)
	if got := r.Importers(target); len(got) != 0 {
		t.Errorf("清边后应为空，实际 %v", got)
	}
}
