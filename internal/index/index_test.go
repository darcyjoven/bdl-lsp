package index

import (
	"os"
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

// --- P3a：GLOBALS 模组全局变量 ---

const sampleGlobalsBlock = `DATABASE ds
GLOBALS
   DEFINE g_aaz RECORD LIKE aaz_file.*
   DEFINE g_rep LIKE type_file.num5
   CONSTANT C_G = 10
   TYPE t_g RECORD
       a INTEGER
   END RECORD
END GLOBALS

MAIN
    DEFINE l_local INTEGER
    LET l_local = 1
END MAIN
`

func TestGlobalsBlockSymbols(t *testing.T) {
	fi := buildIndex(t, sampleGlobalsBlock)

	if !fi.HasGlobalsBlock {
		t.Fatal("应识别出 GLOBALS 块定义")
	}

	// 块内符号：2 个 DEFINE（其一为 record）+ 1 个 CONSTANT + 1 个 TYPE(record)
	want := map[string]struct {
		kind  Kind
		scope Scope
	}{
		"g_aaz": {KindRecord, ScopeGlobals},
		"g_rep": {KindDefine, ScopeGlobals},
		"C_G":   {KindConstant, ScopeGlobals},
		"t_g":   {KindRecord, ScopeGlobals},
	}
	for _, sym := range fi.Symbols {
		w, ok := want[sym.Name]
		if !ok {
			continue
		}
		if sym.Kind != w.kind {
			t.Errorf("%s: kind 期望 %v，实际 %v", sym.Name, w.kind, sym.Kind)
		}
		if sym.Scope != w.scope {
			t.Errorf("%s: scope 期望 %v，实际 %v", sym.Name, w.scope, sym.Scope)
		}
		delete(want, sym.Name)
	}
	for name := range want {
		t.Errorf("GLOBALS 块内未找到符号 %q", name)
	}

	// 块内 record 的字段也要收集
	var fieldCount int
	for _, sym := range fi.Symbols {
		if sym.Kind == KindField {
			fieldCount++
		}
	}
	if fieldCount == 0 {
		t.Error("GLOBALS 块内 record 的字段没有被收集")
	}

	// MAIN 里的局部变量仍是 ScopeFunc，不受影响
	for _, sym := range fi.Symbols {
		if sym.Name == "l_local" && sym.Scope != ScopeFunc {
			t.Errorf("l_local 应为 ScopeFunc，实际 %v", sym.Scope)
		}
	}

	// 块内符号也应进 TopLevel（定义方文件的大纲需要）
	top := map[string]bool{}
	for _, sym := range fi.TopLevel {
		top[sym.Name] = true
	}
	for name := range map[string]bool{"g_aaz": true, "g_rep": true, "C_G": true, "t_g": true} {
		if !top[name] {
			t.Errorf("块内符号 %q 未进 TopLevel", name)
		}
	}
}

func TestGlobalsImportLiteral(t *testing.T) {
	src := `GLOBALS "../../config/top.global"
MAIN
    DEFINE n INTEGER
END MAIN
`
	fi := buildIndex(t, src)

	if len(fi.GlobalsImports) != 1 {
		t.Fatalf("应提取 1 条 GLOBALS 导入，实际 %d", len(fi.GlobalsImports))
	}
	imp := fi.GlobalsImports[0]
	if imp.Literal != "../../config/top.global" {
		t.Errorf("字面量期望 ../../config/top.global，实际 %q", imp.Literal)
	}
	if imp.IsVariable {
		t.Error("字符串字面量不应标记为变量形式")
	}
	// 位置应指向含引号的区间：验证在原文里能截出 "..."（带引号）
	if imp.EndByte <= imp.StartByte {
		t.Fatalf("导入区间非法: [%d, %d)", imp.StartByte, imp.EndByte)
	}
	segment := src[imp.StartByte:imp.EndByte]
	if segment != `"../../config/top.global"` {
		t.Errorf("区间文本期望含引号的字面量，实际 %q", segment)
	}
}

func TestGlobalsImportTrailingComment(t *testing.T) {
	// 语料实测存在：GLOBALS 行尾带注释；被注释的 #GLOBALS 不解析
	src := `GLOBALS "../4gl/aws_ttsrv2_global.4gl"   #TIPTOP 服務使用的變數檔
#GLOBALS "../../config/top.global"
MAIN
    DEFINE n INTEGER
END MAIN
`
	fi := buildIndex(t, src)

	if len(fi.GlobalsImports) != 1 {
		t.Fatalf("应只提取 1 条（#GLOBALS 是注释不解析），实际 %d", len(fi.GlobalsImports))
	}
	if fi.GlobalsImports[0].Literal != "../4gl/aws_ttsrv2_global.4gl" {
		t.Errorf("字面量不正确: %q", fi.GlobalsImports[0].Literal)
	}
}

func TestGlobalsImportVariable(t *testing.T) {
	src := `GLOBALS g_file_var
MAIN
    DEFINE n INTEGER
END MAIN
`
	fi := buildIndex(t, src)

	if len(fi.GlobalsImports) != 1 {
		t.Fatalf("应提取 1 条变量形式导入，实际 %d", len(fi.GlobalsImports))
	}
	imp := fi.GlobalsImports[0]
	if !imp.IsVariable {
		t.Error("变量形式应标记 IsVariable")
	}
	if imp.Literal != "" {
		t.Errorf("变量形式 Literal 应为空，实际 %q", imp.Literal)
	}
	if imp.Name != "g_file_var" {
		t.Errorf("变量名期望 g_file_var，实际 %q", imp.Name)
	}
}

func TestGlobalsMultipleImports(t *testing.T) {
	// 语料实测：每文件 1 条占 89%，最多 5 条
	src := `GLOBALS "a.global"
GLOBALS "b.4gl"
GLOBALS "c.inc"
MAIN
    DEFINE n INTEGER
END MAIN
`
	fi := buildIndex(t, src)
	if len(fi.GlobalsImports) != 3 {
		t.Fatalf("应提取 3 条导入，实际 %d", len(fi.GlobalsImports))
	}
	for i, want := range []string{"a.global", "b.4gl", "c.inc"} {
		if fi.GlobalsImports[i].Literal != want {
			t.Errorf("第 %d 条期望 %q，实际 %q", i, want, fi.GlobalsImports[i].Literal)
		}
	}
}

func TestModuleDefineNotGlobalsScope(t *testing.T) {
	// 块外的模块级 DEFINE 仍是 ScopeGlobal（不跨文件共享）——语义必须区分
	src := `GLOBALS
    DEFINE g_in_block INTEGER
END GLOBALS

DEFINE g_outside_block INTEGER

MAIN
    DEFINE n INTEGER
END MAIN
`
	fi := buildIndex(t, src)
	scopes := map[string]Scope{}
	for _, sym := range fi.Symbols {
		if sym.Kind == KindDefine || sym.Kind == KindRecord {
			scopes[sym.Name] = sym.Scope
		}
	}
	if scopes["g_in_block"] != ScopeGlobals {
		t.Errorf("块内 g_in_block 应为 ScopeGlobals，实际 %v", scopes["g_in_block"])
	}
	if scopes["g_outside_block"] != ScopeGlobal {
		t.Errorf("块外 g_outside_block 应为 ScopeGlobal，实际 %v", scopes["g_outside_block"])
	}
}

func TestRealGlobalFileIndexing(t *testing.T) {
	// 规划验收项：真实 top.global（176 个 DEFINE、43 个 record）能全部索引。
	// 语料不在本机/CI 时跳过，本地跑 make test 时生效。
	const path = "/code/workspace/tiptop-gp/tiptop/config/top.global"
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("语料不存在: %v", err)
	}

	pool, err := parser.New()
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	t.Cleanup(pool.Close)
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	t.Cleanup(tree.Close)
	if root := tree.RootNode(); root.HasError() {
		t.Fatalf("top.global 有语法错误：%s", root.ToSexp())
	}

	fi := Build(parser.LangBDL, tree, src, 1)
	if !fi.HasGlobalsBlock {
		t.Fatal("top.global 应含 GLOBALS 块")
	}

	var defines, records int
	for _, sym := range fi.Symbols {
		switch sym.Kind {
		case KindDefine:
			defines++
		case KindRecord:
			records++
		}
	}
	t.Logf("top.global: symbols=%d defines=%d records=%d", len(fi.Symbols), defines, records)

	// 修复前是 0 个符号；176 个 DEFINE 里 43 个是 RECORD LIKE（算 record）
	if defines+records < 170 {
		t.Errorf("符号数过少：defines=%d records=%d（期望合计 ≥170）", defines, records)
	}
}
