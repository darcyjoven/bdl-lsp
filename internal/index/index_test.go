package index

import (
	"testing"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

const sampleBDL = `SCHEMA ds
GLOBALS "globals.4gl"

DEFINE g_count LIKE type_file.num5
DEFINE g_name  LIKE type_file.chr20

CONSTANT C_MAX = 100

TYPE t_rec RECORD
    a LIKE type_file.num5,
    b STRING
END RECORD

MAIN
    DEFINE l_local INTEGER
    LET l_local = 1
END MAIN

FUNCTION helper(p_val)
    DEFINE l_tmp STRING
    RETURN p_val
END FUNCTION
`

func buildIndex(t *testing.T, src string) *FileIndex {
	t.Helper()

	pool, err := parser.New()
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	t.Cleanup(pool.Close)

	tree, err := pool.Parse(parser.LangBDL, []byte(src), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	t.Cleanup(tree.Close)

	if root := tree.RootNode(); root.HasError() {
		t.Fatalf("样例有语法错误：%s", root.ToSexp())
	}
	return Build(parser.LangBDL, tree, []byte(src), 1)
}

func TestBuildTopLevelSymbols(t *testing.T) {
	fi := buildIndex(t, sampleBDL)

	want := map[string]Kind{
		"g_count": KindDefine,
		"g_name":  KindDefine,
		"C_MAX":   KindConstant,
		"t_rec":   KindRecord,
		"main":    KindMain,
		"helper":  KindFunction,
	}

	got := map[string]Kind{}
	for _, sym := range fi.TopLevel {
		got[sym.Name] = sym.Kind
	}

	for name, kind := range want {
		if got[name] != kind {
			t.Errorf("顶层符号 %q: 期望 %v，实际 %v", name, kind, got[name])
		}
	}
}

func TestFunctionSignatureAndParams(t *testing.T) {
	fi := buildIndex(t, sampleBDL)

	syms := fi.Lookup("helper")
	if len(syms) != 1 {
		t.Fatalf("helper 符号数 = %d，期望 1", len(syms))
	}
	fn := syms[0]

	if fn.Kind != KindFunction {
		t.Errorf("Kind = %v", fn.Kind)
	}
	if fn.Signature != "FUNCTION helper(p_val)" {
		t.Errorf("签名 = %q", fn.Signature)
	}

	var param *Symbol
	for _, ch := range fn.Children {
		if ch.Name == "p_val" {
			param = ch
		}
	}
	if param == nil {
		t.Fatalf("未找到参数 p_val，children: %+v", fn.Children)
	}
	if param.Kind != KindParameter || param.Scope != ScopeParam {
		t.Errorf("参数 Kind/Scope = %v/%v", param.Kind, param.Scope)
	}
}

func TestFunctionLocalDefines(t *testing.T) {
	fi := buildIndex(t, sampleBDL)

	syms := fi.Lookup("helper")
	fn := syms[0]

	var local *Symbol
	for _, ch := range fn.Children {
		if ch.Name == "l_tmp" {
			local = ch
		}
	}
	if local == nil {
		t.Fatal("未找到函数内 DEFINE l_tmp")
	}
	if local.Scope != ScopeFunc {
		t.Errorf("局部变量 Scope = %v，期望 func", local.Scope)
	}
	if local.Container != "helper" {
		t.Errorf("Container = %q，期望 helper", local.Container)
	}
}

func TestRecordFields(t *testing.T) {
	fi := buildIndex(t, sampleBDL)

	syms := fi.Lookup("t_rec")
	if len(syms) != 1 {
		t.Fatalf("t_rec 符号数 = %d", len(syms))
	}
	rec := syms[0]

	if rec.Kind != KindRecord {
		t.Errorf("Kind = %v，期望 record", rec.Kind)
	}

	fields := map[string]bool{}
	for _, ch := range rec.Children {
		fields[ch.Name] = ch.Kind == KindField
	}
	if !fields["a"] || !fields["b"] {
		t.Errorf("记录字段 = %+v，期望 a 与 b", fields)
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	fi := buildIndex(t, sampleBDL)

	for _, name := range []string{"helper", "HELPER", "Helper"} {
		if syms := fi.Lookup(lower(name)); len(syms) != 1 {
			t.Errorf("Lookup(%q) = %d 个，期望 1", name, len(syms))
		}
	}
}

func TestTypeText(t *testing.T) {
	fi := buildIndex(t, sampleBDL)

	syms := fi.Lookup("g_count")
	if len(syms) != 1 {
		t.Fatal("未找到 g_count")
	}
	if got := syms[0].TypeText; got != "LIKE type_file.num5" {
		t.Errorf("TypeText = %q", got)
	}
}

func TestSymbolRangesAreInsideSource(t *testing.T) {
	fi := buildIndex(t, sampleBDL)

	for _, sym := range fi.Symbols {
		if sym.StartByte >= sym.EndByte {
			t.Errorf("%s 区间为空: %d..%d", sym.Name, sym.StartByte, sym.EndByte)
		}
		if sym.DeclStart > sym.DeclEnd {
			t.Errorf("%s 声明区间非法: %d..%d", sym.Name, sym.DeclStart, sym.DeclEnd)
		}
		if sym.DeclEnd > uint(len(sampleBDL)) {
			t.Errorf("%s 声明区间越界: %d > %d", sym.Name, sym.DeclEnd, len(sampleBDL))
		}
	}
}

func TestBuildNilTree(t *testing.T) {
	fi := Build(parser.LangBDL, nil, nil, 0)
	if fi == nil || len(fi.Symbols) != 0 {
		t.Errorf("nil 树应返回空索引，实际 %+v", fi)
	}
}

func TestBrokenFileStillBuilds(t *testing.T) {
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	src := []byte("MAIN\n    DISPLAY \"unterminated\n")
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	fi := Build(parser.LangBDL, tree, src, 1)
	if fi == nil {
		t.Fatal("损坏文件也应返回索引（可能为空）")
	}
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}
