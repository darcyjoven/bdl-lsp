package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// requestCodeAction 发 codeAction 请求并返回动作列表。
//
// diags 是客户端"看到"的诊断（真实编辑器会把 publishDiagnostics 的结果
// 原样传回来），这里用测试里构造的诊断模拟。
func (h *harness) requestCodeAction(t *testing.T, uri string, rng map[string]any,
	diags []map[string]any) []map[string]any {

	t.Helper()
	params := map[string]any{
		"textDocument": map[string]any{"uri": uri},
		"range":        rng,
		"context":      map[string]any{"diagnostics": diags},
	}
	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      nextRequestID(),
		"method":  "textDocument/codeAction",
		"params":  params,
	})
	h.send(string(payload))
	msg := h.readMessage(5 * time.Second)
	if msg == nil {
		return nil
	}
	raw, _ := msg["result"].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// fieldDiag 构造一条与服务器实际发出格式一致的字段诊断。
func fieldDiag(uri string, line, startChar, endChar int, field string) map[string]any {
	return map[string]any{
		"range": map[string]any{
			"start": map[string]any{"line": line, "character": startChar},
			"end":   map[string]any{"line": line, "character": endChar},
		},
		"severity": 2,
		"source":   "bdl-lsp",
		"code":     codeFieldNotInForm,
		"message":  fmt.Sprintf("字段 %q 不在表单中", field),
	}
}

// actionTitles 提取动作标题。
func actionTitles(actions []map[string]any) []string {
	out := make([]string, 0, len(actions))
	for _, a := range actions {
		if s, ok := a["title"].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// firstEditNewText 取第一个动作里的第一个编辑的 newText。
func firstEditNewText(action map[string]any, uri string) string {
	edit, _ := action["edit"].(map[string]any)
	if edit == nil {
		return ""
	}
	changes, _ := edit["changes"].(map[string]any)
	if changes == nil {
		return ""
	}
	edits, _ := changes[uri].([]any)
	if len(edits) == 0 {
		return ""
	}
	e, _ := edits[0].(map[string]any)
	s, _ := e["newText"].(string)
	return s
}

// TestCodeActionCapability 必须声明 codeActionProvider 且只给 quickfix。
func TestCodeActionCapability(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	msg := h.readMessage(3 * time.Second)

	caps, _ := msg["result"].(map[string]any)["capabilities"].(map[string]any)
	if caps == nil {
		t.Fatal("未返回 capabilities")
	}
	ca, ok := caps["codeActionProvider"].(map[string]any)
	if !ok {
		t.Fatalf("未声明 codeActionProvider：%v", caps)
	}
	kinds, _ := ca["codeActionKinds"].([]any)
	if len(kinds) == 0 || kinds[0] != "quickfix" {
		t.Errorf("应只声明 quickfix，实际 %v", kinds)
	}
}

// TestCodeActionFieldTypo 字段拼错时应给出"改为 X"的修复。
func TestCodeActionFieldTypo(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)

	// 表单里有 f01/f02；这里故意写 f11（与 f01 编辑距离 1）
	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM f11\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	// "  INPUT x FROM f11" 里 f11 的列范围
	line := docLineText(src, 2)
	start := strings.Index(line, "f11")
	if start < 0 {
		t.Fatal("fixture 里找不到 f11")
	}
	diag := fieldDiag(mainURI, 2, start, start+3, "f11")
	rng := map[string]any{
		"start": map[string]any{"line": 2, "character": start},
		"end":   map[string]any{"line": 2, "character": start + 3},
	}

	actions := h.requestCodeAction(t, mainURI, rng, []map[string]any{diag})
	if len(actions) == 0 {
		t.Fatal("字段拼错应给出修复")
	}
	first := firstEditNewText(actions[0], mainURI)
	if first != "f01" && first != "f02" {
		t.Errorf("首个建议应为 f01 或 f02，实际 %q（标题 %v）", first, actionTitles(actions))
	}
	// 第一个必须标为 preferred
	if v, _ := actions[0]["isPreferred"].(bool); !v {
		t.Error("首个动作应标 isPreferred")
	}
}

// TestCodeActionNoSuggestionForUnrelatedField 完全不相似的字段名不给建议。
func TestCodeActionNoSuggestionForUnrelatedField(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)

	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM completely_different_name\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	line := docLineText(src, 2)
	start := strings.Index(line, "completely_different_name")
	diag := fieldDiag(mainURI, 2, start, start+len("completely_different_name"),
		"completely_different_name")
	rng := map[string]any{
		"start": map[string]any{"line": 2, "character": start},
		"end":   map[string]any{"line": 2, "character": start + len("completely_different_name")},
	}

	actions := h.requestCodeAction(t, mainURI, rng, []map[string]any{diag})
	if len(actions) != 0 {
		t.Errorf("毫不相似的字段名不该给建议，实际 %v", actionTitles(actions))
	}
}

// TestCodeActionIgnoresForeignSource 别的插件（如部署）的诊断不应被处理。
func TestCodeActionIgnoresForeignSource(t *testing.T) {
	h := initPlainServer(t)
	src := "MAIN\n    DISPLAY x\nEND MAIN\n"
	const uri = "file:///tmp/ca_foreign.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	diag := fieldDiag(uri, 1, 12, 13, "x")
	diag["source"] = "bdl-deploy" // 部署编译诊断
	diag["code"] = codeFieldNotInForm

	rng := map[string]any{
		"start": map[string]any{"line": 1, "character": 12},
		"end":   map[string]any{"line": 1, "character": 13},
	}
	if actions := h.requestCodeAction(t, uri, rng, []map[string]any{diag}); len(actions) != 0 {
		t.Errorf("外部来源的诊断不该产生动作，实际 %v", actionTitles(actions))
	}
}

// TestCodeActionNoDiagnostics 没有诊断时返回空（不刷无意义动作）。
func TestCodeActionNoDiagnostics(t *testing.T) {
	h := initPlainServer(t)
	const uri = "file:///tmp/ca_none.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\nEND MAIN\n")

	rng := map[string]any{
		"start": map[string]any{"line": 0, "character": 0},
		"end":   map[string]any{"line": 0, "character": 4},
	}
	if actions := h.requestCodeAction(t, uri, rng, nil); len(actions) != 0 {
		t.Errorf("无诊断时不该有动作，实际 %v", actionTitles(actions))
	}
}

// TestCodeActionNoFixForDuplicateItem 重复 item 故意不提供修复。
//
// 删掉是破坏性的、合并语义不明——宁可不给，也不要一键产生坏表单。
func TestCodeActionNoFixForDuplicateItem(t *testing.T) {
	h := initPlainServer(t)
	const uri = "file:///tmp/ca_dup.per"
	openDocAndDrain(t, h, uri, "per", "SCHEMA ds\nLAYOUT\nGRID\n{\n  [f01      ]\n}\nEND\nEND\nATTRIBUTES\nEDIT f01 = formonly.f01;\nEDIT f01 = formonly.f01;\nEND\n")

	diag := map[string]any{
		"range": map[string]any{
			"start": map[string]any{"line": 10, "character": 5},
			"end":   map[string]any{"line": 10, "character": 8},
		},
		"severity": 2,
		"source":   "bdl-lsp",
		"code":     "form-duplicate-item",
		"message":  "重复 item",
	}
	rng := map[string]any{
		"start": map[string]any{"line": 10, "character": 5},
		"end":   map[string]any{"line": 10, "character": 8},
	}
	if actions := h.requestCodeAction(t, uri, rng, []map[string]any{diag}); len(actions) != 0 {
		t.Errorf("重复 item 不该提供修复，实际 %v", actionTitles(actions))
	}
}

// docLineText 取源码的第 n 行（0 基）。
func docLineText(src string, n int) string {
	lines := strings.Split(src, "\n")
	if n < 0 || n >= len(lines) {
		return ""
	}
	return lines[n]
}

// TestEditDistance 校验 Levenshtein 距离实现。
func TestEditDistance(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "", 3},
		{"", "abc", 3},
		{"good_field", "good_filed", 2}, // 转置算 2（替换+替换）
		{"f01", "f02", 1},
		{"f01", "f11", 1},
		{"tot_amt", "cust_name", 5},
		// 经典基准用例（验证 DP 实现本身没写错）
		{"kitten", "sitting", 3},
		{"a", "b", 1},
	}
	for _, tc := range tests {
		if got := editDistance(tc.a, tc.b); got != tc.want {
			t.Errorf("editDistance(%q, %q) = %d，期望 %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestMaxSuggestDistance 阈值应随名字长度增长并被限制在 1..4。
func TestMaxSuggestDistance(t *testing.T) {
	tests := []struct {
		word string
		want int
	}{
		{"a", 1},                      // 1/3=0 → 下限 1
		{"abc", 1},                    // 3/3=1
		{"good_field", 3},             // 10/3=3
		{"a_very_long_field_name", 4}, // 22/3=7 → 上限 4
	}
	for _, tc := range tests {
		if got := maxSuggestDistance(tc.word); got != tc.want {
			t.Errorf("maxSuggestDistance(%q) = %d，期望 %d", tc.word, got, tc.want)
		}
	}
}

// TestClosestMatchesOrdering 结果应按距离升序，且排除同名。
func TestClosestMatchesOrdering(t *testing.T) {
	cands := []string{"good_field", "good_fields", "totally_other", "GOOD_FIELD"}
	got := closestMatches("good_filed", cands, 3)
	if len(got) == 0 {
		t.Fatal("应有候选")
	}
	// good_field 距离 2，good_fields 距离 3；totally_other 远超阈值
	for _, g := range got {
		if strings.EqualFold(g, "totally_other") {
			t.Errorf("不应建议 %q", g)
		}
		if strings.EqualFold(g, "good_filed") {
			t.Errorf("不应建议原词本身 %q", g)
		}
	}
	if !strings.EqualFold(got[0], "good_field") {
		t.Errorf("首选应为 good_field，实际 %v", got)
	}
}

// TestClosestMatchesLimit 候选数应被 limit 截断。
func TestClosestMatchesLimit(t *testing.T) {
	cands := []string{"aa1", "aa2", "aa3", "aa4", "aa5"}
	got := closestMatches("aa0", cands, 2)
	if len(got) != 2 {
		t.Errorf("应截断到 2，实际 %d: %v", len(got), got)
	}
}
