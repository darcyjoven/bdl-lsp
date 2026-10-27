package completion

import (
	"fmt"
	"strings"
	"testing"

	"github.com/darcyjoven/bdl-lsp/builtin"
	"github.com/darcyjoven/bdl-lsp/index"
)

// labels 提取结果里的 Label 列表，便于断言。
func labels(items []Item) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, it.Label)
	}
	return out
}

// has 判断结果里是否有某个 Label（大小写不敏感）。
func has(items []Item, label string) bool {
	for _, it := range items {
		if strings.EqualFold(it.Label, label) {
			return true
		}
	}
	return false
}

// symbols 造一个最简单的文件索引。
func fileWith(syms ...*index.Symbol) *index.FileIndex {
	return &index.FileIndex{Symbols: syms}
}

func sym(name string, kind index.Kind) *index.Symbol {
	return &index.Symbol{Name: name, Lower: strings.ToLower(name), Kind: kind}
}

func TestSplitWord(t *testing.T) {
	tests := []struct {
		line     string
		wantWord string
		wantDot  bool
	}{
		{"DISPLAY ", "", false},
		{"DISPLAY fo", "fo", false},
		{"    LET x = ", "", false},
		{"LET f01.", "", true},
		{"LET f01.fi", "fi", true},
		{"CALL show_", "show_", false},
		{"p_text#", "p_text#", false},
		{"a$b", "a$b", false},
		{"IF x = 1 THEN", "THEN", false},
	}
	for _, tc := range tests {
		word, dot := SplitWord(tc.line)
		if word != tc.wantWord || dot != tc.wantDot {
			t.Errorf("SplitWord(%q) = (%q, %v)，期望 (%q, %v)",
				tc.line, word, dot, tc.wantWord, tc.wantDot)
		}
	}
}

// TestProvideKeywordsWithBuiltins 守住"关键字不会被上限截掉"。
//
// 回归背景：BDL 关键字 361 条排在 SortText 最后，内置符号（97 条）加进来
// 之后总条数超过上限，空前缀补全里一个关键字都没有——e2e「补全给出关键字」
// 就是这么红的。
func TestProvideKeywordsWithBuiltins(t *testing.T) {
	var builtins []builtin.Symbol
	for i := 0; i < 97; i++ {
		builtins = append(builtins, builtin.Symbol{
			Name: fmt.Sprintf("g_builtin_%03d", i), Kind: builtin.KindFunction,
		})
	}
	res := Provide(Context{Lang: "bdl"}, nil, nil, nil,
		Options{Keywords: true, Builtins: builtins})
	got := map[string]bool{}
	for _, it := range res.Items {
		got[it.Label] = true
	}
	for _, want := range []string{"DISPLAY", "END", "CALL"} {
		if !got[want] {
			t.Fatalf("补全缺少关键字 %q（共 %d 项）", want, len(res.Items))
		}
	}
}

func TestProvideKeywords(t *testing.T) {
	res := Provide(Context{Lang: "bdl", Line: "DIS", Word: "DIS"}, nil, nil, nil,
		Options{Keywords: true})
	if len(res.Items) == 0 {
		t.Fatal("应有候选")
	}
	// "DIS" 前缀应给出 DISPLAY / DISPLAY_ARRAY 之类
	found := false
	for _, it := range res.Items {
		if strings.HasPrefix(strings.ToUpper(it.Label), "DIS") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("前缀 DIS 应有匹配，实际 %v", labels(res.Items))
	}
	// 所有项都必须匹配前缀
	for _, it := range res.Items {
		if !strings.HasPrefix(strings.ToLower(it.Label), "dis") {
			t.Errorf("不该出现不匹配前缀的项: %q", it.Label)
		}
	}
}

func TestProvideKeywordCaseInsensitive(t *testing.T) {
	// 用户常打小写，清单是大写；两者都要能匹配。
	res := Provide(Context{Lang: "bdl", Line: "dis", Word: "dis"}, nil, nil, nil,
		Options{Keywords: true})
	if len(res.Items) == 0 {
		t.Fatal("小写前缀也应有候选")
	}
}

// TestProvideKeywordLowercase 守住"format.keywords=lower 时补全也给小写"。
//
// 否则用户补全出 DISPLAY，格式化又把它改成 display，来回打脸。
func TestProvideKeywordLowercase(t *testing.T) {
	res := Provide(Context{Lang: "bdl", Line: "dis", Word: "dis"}, nil, nil, nil,
		Options{Keywords: true, KeywordCase: "lower"})
	if !has(res.Items, "display") {
		t.Errorf("应给出小写 display，实际 %v", labels(res.Items))
	}
	for _, it := range res.Items {
		if it.Kind != KindKeyword {
			continue
		}
		if it.Label != strings.ToLower(it.Label) {
			t.Errorf("关键字应全小写，实际 %q", it.Label)
		}
	}
}

// TestProvideKeywordUpperUnchanged：只有 lower 才改写，upper/unchanged 保持清单原样。
func TestProvideKeywordUpperUnchanged(t *testing.T) {
	for _, kc := range []string{"", "unchanged", "upper"} {
		res := Provide(Context{Lang: "bdl", Line: "dis", Word: "dis"}, nil, nil, nil,
			Options{Keywords: true, KeywordCase: kc})
		if !has(res.Items, "DISPLAY") {
			t.Errorf("KeywordCase=%q 时应保持大写 DISPLAY，实际 %v", kc, labels(res.Items))
		}
	}
}

func TestProvidePERKeywords(t *testing.T) {
	res := Provide(Context{Lang: "per", Line: "LAY", Word: "LAY"}, nil, nil, nil,
		Options{Keywords: true})
	if !has(res.Items, "LAYOUT") {
		t.Errorf(".per 应有 LAYOUT，实际 %v", labels(res.Items))
	}
	// .per 不应出现 BDL 专属关键字（如 FOREACH）
	if has(res.Items, "FOREACH") {
		t.Error(".per 不应给 BDL 的 FOREACH")
	}
}

func TestProvideFileSymbols(t *testing.T) {
	fi := fileWith(
		sym("show_name", index.KindFunction),
		sym("l_count", index.KindDefine),
		sym("c_max", index.KindConstant),
	)
	res := Provide(Context{Lang: "bdl", Line: "show", Word: "show"}, fi, nil, nil,
		Options{Keywords: true})
	if !has(res.Items, "show_name") {
		t.Errorf("应给出同名函数，实际 %v", labels(res.Items))
	}
	if has(res.Items, "l_count") {
		t.Error("不匹配前缀的符号不该出现")
	}
}

func TestProvideSymbolBeforeKeyword(t *testing.T) {
	// 同名时符号应排在关键字前面（SortText 前缀小）
	fi := fileWith(sym("DISPLAY_REC", index.KindFunction))
	res := Provide(Context{Lang: "bdl", Line: "DISPLAY", Word: "DISPLAY"}, fi, nil, nil,
		Options{Keywords: true})
	if len(res.Items) < 2 {
		t.Fatalf("应有符号与关键字两类，实际 %v", labels(res.Items))
	}
	if res.Items[0].Kind != KindFunction {
		t.Errorf("符号应排在关键字之前，实际首项 %q（kind=%d）",
			res.Items[0].Label, res.Items[0].Kind)
	}
}

func TestProvideAfterDotOnlyFields(t *testing.T) {
	fi := fileWith(sym("l_rec", index.KindRecord), sym("show_name", index.KindFunction))
	res := Provide(Context{Lang: "bdl", Line: "LET f01.", Word: "", AfterDot: true},
		fi, nil, []string{"f01", "f02"}, Options{Keywords: true})
	if len(res.Items) == 0 {
		t.Fatal("`.` 之后应给字段")
	}
	for _, it := range res.Items {
		if it.Kind == KindKeyword {
			t.Errorf("`.` 之后不该给关键字，实际含 %q", it.Label)
		}
	}
	// 函数不该作为成员出现
	if has(res.Items, "show_name") {
		t.Error("`.` 之后不该给函数名")
	}
}

func TestProvideSkipsComment(t *testing.T) {
	res := Provide(Context{Lang: "bdl", Line: "-- DIS", Word: "DIS", InComment: true},
		nil, nil, nil, Options{Keywords: true})
	if len(res.Items) != 0 {
		t.Errorf("注释里不补全，实际 %v", labels(res.Items))
	}
}

func TestProvideSkipsString(t *testing.T) {
	res := Provide(Context{Lang: "bdl", Line: `LET x = "DIS`, Word: "DIS", InString: true},
		nil, nil, nil, Options{Keywords: true})
	if len(res.Items) != 0 {
		t.Errorf("字符串里不补全，实际 %v", labels(res.Items))
	}
}

func TestProvideDedup(t *testing.T) {
	// 同名的同文件符号与跨文件符号只应出现一次
	fi := fileWith(sym("foo", index.KindFunction))
	cross := []*index.Symbol{sym("foo", index.KindFunction)}
	res := Provide(Context{Lang: "bdl", Line: "foo", Word: "foo"}, fi, cross, nil,
		Options{Keywords: true})
	count := 0
	for _, it := range res.Items {
		if strings.EqualFold(it.Label, "foo") {
			count++
		}
	}
	if count != 1 {
		t.Errorf("同名项应去重为 1，实际 %d", count)
	}
}

func TestProvideMaxItems(t *testing.T) {
	res := Provide(Context{Lang: "bdl", Line: "", Word: ""}, nil, nil, nil,
		Options{Keywords: true, MaxItems: 5})
	if len(res.Items) > 5 {
		t.Errorf("应截断到 5 项，实际 %d", len(res.Items))
	}
	if !res.IsIncomplete {
		t.Error("空前缀（全量候选）时应标记 IsIncomplete")
	}
}

func TestProvideIncompleteOnlyForEmptyPrefix(t *testing.T) {
	res := Provide(Context{Lang: "bdl", Line: "IF", Word: "IF"}, nil, nil, nil,
		Options{Keywords: true})
	if res.IsIncomplete {
		t.Error("有前缀且未截断时不该标记 IsIncomplete")
	}
}

func TestKeywordsAreSorted(t *testing.T) {
	// 生成的清单按"短的在前、同长字母序"排；UI 依赖这个顺序
	for i := 1; i < len(BDLKeywords); i++ {
		a, b := BDLKeywords[i-1], BDLKeywords[i]
		if len(a) > len(b) || (len(a) == len(b) && a > b) {
			t.Fatalf("关键字清单未排序：%q 在 %q 之前", a, b)
		}
	}
}

func TestKeywordCountsSane(t *testing.T) {
	// 防生成脚本漏读（grammar 拆分文件时容易只读到一个子集）
	if len(BDLKeywords) < 250 {
		t.Errorf("BDL 关键字数量异常少：%d", len(BDLKeywords))
	}
	if len(PERKeywords) < 50 {
		t.Errorf("PER 关键字数量异常少：%d", len(PERKeywords))
	}
}

func TestSymbolKindMapping(t *testing.T) {
	tests := []struct {
		kind index.Kind
		want Kind
	}{
		{index.KindFunction, KindFunction},
		{index.KindMain, KindFunction},
		{index.KindReport, KindFunction},
		{index.KindDefine, KindVariable},
		{index.KindConstant, KindConstant},
		{index.KindType, KindStruct},
		{index.KindRecord, KindStruct},
		{index.KindField, KindField},
		{index.KindParameter, KindVariable},
		{index.KindFormItem, KindField},
	}
	for _, tc := range tests {
		it, ok := itemForSymbol(sym("x", tc.kind))
		if !ok {
			t.Errorf("%s 应能映射", tc.kind)
			continue
		}
		if it.Kind != tc.want {
			t.Errorf("%s 应映射到 %d，实际 %d", tc.kind, tc.want, it.Kind)
		}
	}
}

// P7-C：内置符号补全。
func TestBuiltinCandidates(t *testing.T) {
	builtins := []builtin.Symbol{
		{Name: "FGL_GETENV", Kind: builtin.KindFunction, Signature: "FGL_GETENV(name)"},
		{Name: "STATUS", Kind: builtin.KindVariable, Doc: "SQL 返回码"},
		{Name: "ui", Kind: builtin.KindClass},
	}

	res := Provide(Context{Lang: "bdl", Line: "DISPLAY fgl_get", Word: "fgl_get"},
		nil, nil, nil, Options{Builtins: builtins})
	if len(res.Items) != 1 || res.Items[0].Label != "FGL_GETENV" {
		t.Fatalf("前缀 fgl_get 应命中 FGL_GETENV，实际 %+v", res.Items)
	}
	if res.Items[0].Kind != KindFunction {
		t.Errorf("Kind 应为函数，实际 %v", res.Items[0].Kind)
	}
	if !strings.Contains(res.Items[0].Documentation, "FGL_GETENV") {
		t.Errorf("Documentation 应含签名，实际 %q", res.Items[0].Documentation)
	}

	// 类名映射到 KindClass（`. ` 后能给成员时才有意义，这里只验映射）。
	res = Provide(Context{Lang: "bdl", Line: "x = ui", Word: "ui"}, nil, nil, nil,
		Options{Builtins: builtins})
	if len(res.Items) != 1 || res.Items[0].Kind != KindClass {
		t.Fatalf("ui 应映射为类，实际 %+v", res.Items)
	}
}

// P7-C：用户自己的同名符号优先，内置项不重复出现。
func TestBuiltinSkipsNameAlreadyPresent(t *testing.T) {
	fi := &index.FileIndex{Symbols: []*index.Symbol{
		{Name: "STATUS", Lower: "status", Kind: index.KindDefine},
	}}
	res := Provide(Context{Lang: "bdl", Line: "DISPLAY status", Word: "status"},
		fi, nil, nil, Options{Builtins: []builtin.Symbol{
			{Name: "STATUS", Kind: builtin.KindVariable, Doc: "内置"},
		}})
	var n int
	for _, it := range res.Items {
		if strings.EqualFold(it.Label, "STATUS") {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("同名内置项应被跳过，实际出现 %d 次", n)
	}
	if res.Items[0].Detail != "DEFINE" && !strings.Contains(res.Items[0].Detail, "INTEGER") {
		// 只要第一项不是内置说明即可
		if strings.Contains(res.Items[0].Detail, "内置") {
			t.Errorf("第一项应是用户符号，实际 %+v", res.Items[0])
		}
	}
}
