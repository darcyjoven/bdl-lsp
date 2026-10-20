package server

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// requestCompletion 发 completion 请求并返回 Items。
func (h *harness) requestCompletion(t *testing.T, uri string, line, char int) []map[string]any {
	t.Helper()
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%d,"method":"textDocument/completion","params":{`+
			`"textDocument":{"uri":%q},"position":{"line":%d,"character":%d}}}`,
		nextRequestID(), uri, line, char)
	h.send(payload)
	msg := h.readMessage(5 * time.Second)
	if msg == nil {
		return nil
	}
	// CompletionList 与 CompletionItem[] 两种返回形态都接受。
	switch result := msg["result"].(type) {
	case map[string]any:
		items, _ := result["items"].([]any)
		return toItemMaps(items)
	case []any:
		return toItemMaps(result)
	}
	return nil
}

// toItemMaps 把 []any 转成 []map[string]any，丢掉非对象元素。
func toItemMaps(items []any) []map[string]any {
	out := make([]map[string]any, 0, len(items))
	for _, it := range items {
		if m, ok := it.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// itemLabels 提取 Label 列表。
func itemLabels(items []map[string]any) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it["label"].(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// hasLabel 判断是否含某个 Label（大小写不敏感）。
func hasLabel(items []map[string]any, want string) bool {
	for _, it := range items {
		if s, ok := it["label"].(string); ok && strings.EqualFold(s, want) {
			return true
		}
	}
	return false
}

// TestCompletionCapability 初始化的能力里必须声明补全，否则客户端不会请求。
func TestCompletionCapability(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	msg := h.readMessage(3 * time.Second)

	caps, _ := msg["result"].(map[string]any)["capabilities"].(map[string]any)
	if caps == nil {
		t.Fatal("initialize 未返回 capabilities")
	}
	comp, ok := caps["completionProvider"].(map[string]any)
	if !ok {
		t.Fatalf("未声明 completionProvider：%v", caps)
	}
	chars, _ := comp["triggerCharacters"].([]any)
	if len(chars) == 0 || chars[0] != "." {
		t.Errorf("触发字符应含 `.`，实际 %v", chars)
	}
}

// TestCompletionKeywords 空前缀时给出关键字。
func TestCompletionKeywords(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	// 第 2 行是空行（MAIN 之后的缩进行），光标在第 0 列 → 空前缀
	const uri = "file:///tmp/complete_kw.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\n    \nEND MAIN\n")

	items := h.requestCompletion(t, uri, 1, 4)
	if len(items) == 0 {
		t.Fatal("空前缀应给出候选")
	}
	if !hasLabel(items, "DISPLAY") {
		t.Errorf("应含关键字 DISPLAY，实际前几项 %v", firstN(itemLabels(items), 10))
	}
	// 关键字项的 kind 应为 14（Keyword）
	for _, it := range items {
		if s, _ := it["label"].(string); !strings.EqualFold(s, "DISPLAY") {
			continue
		}
		if k, _ := it["kind"].(float64); int(k) != 14 {
			t.Errorf("DISPLAY 的 kind 应为 14（Keyword），实际 %v", it["kind"])
		}
	}
}

// TestCompletionPrefixFilters 前缀应过滤候选。
func TestCompletionPrefixFilters(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/complete_prefix.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\n    DISP\nEND MAIN\n")

	items := h.requestCompletion(t, uri, 1, 8) // 光标在 DISP 之后
	if len(items) == 0 {
		t.Fatal("前缀 DISP 应有候选")
	}
	for _, it := range items {
		label, _ := it["label"].(string)
		if !strings.HasPrefix(strings.ToLower(label), "disp") {
			t.Errorf("候选 %q 不匹配前缀 DISP", label)
		}
	}
}

// TestCompletionLocalSymbol 同文件的函数应出现在候选里。
func TestCompletionLocalSymbol(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	src := strings.Join([]string{
		"FUNCTION show_name(p_name)",
		"    DISPLAY p_name",
		"END FUNCTION",
		"MAIN",
		"    CALL sho",
		"END MAIN",
		"",
	}, "\n")
	const uri = "file:///tmp/complete_sym.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	// 第 5 行（0 基 4）"    CALL sho"，光标在行尾
	items := h.requestCompletion(t, uri, 4, 12)
	if !hasLabel(items, "show_name") {
		t.Errorf("应给出同文件函数 show_name，实际 %v", firstN(itemLabels(items), 10))
	}
}

// TestCompletionAfterDotNoKeywords `.` 之后不应给关键字。
func TestCompletionAfterDotNoKeywords(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/complete_dot.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\n    LET x = r.\nEND MAIN\n")

	items := h.requestCompletion(t, uri, 1, 14) // 光标在 `.` 之后
	for _, it := range items {
		if k, _ := it["kind"].(float64); int(k) == 14 {
			label, _ := it["label"].(string)
			t.Errorf("`.` 之后不该给关键字 %q", label)
		}
	}
}

// TestCompletionInComment 注释里不给候选。
func TestCompletionInComment(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/complete_cmt.4gl"
	openDocAndDrain(t, h, uri, "bdl", "-- MAIN comment\nMAIN\nEND MAIN\n")

	items := h.requestCompletion(t, uri, 0, 6) // 光标在注释中间
	if len(items) != 0 {
		t.Errorf("注释里不该有候选，实际 %v", firstN(itemLabels(items), 5))
	}
}

// TestCompletionInString 字符串里不给候选。
func TestCompletionInString(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/complete_str.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\n    LET x = \"hel\"\nEND MAIN\n")

	items := h.requestCompletion(t, uri, 1, 16) // 光标在字符串 "hel 中间
	if len(items) != 0 {
		t.Errorf("字符串里不该有候选，实际 %v", firstN(itemLabels(items), 5))
	}
}

// TestCompletionPERKeywords .per 应给 per 关键字（LAYOUT）。
func TestCompletionPERKeywords(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/complete_per.per"
	openDocAndDrain(t, h, uri, "per", "LAY\n")

	items := h.requestCompletion(t, uri, 0, 3)
	if !hasLabel(items, "LAYOUT") {
		t.Errorf(".per 前缀 LAY 应给出 LAYOUT，实际 %v", firstN(itemLabels(items), 10))
	}
	if hasLabel(items, "FOREACH") {
		t.Error(".per 不该给 BDL 专属关键字 FOREACH")
	}
}

// TestCompletionResolve 确认候选时原样返回（不报错）。
func TestCompletionResolve(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	payload, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"id":      nextRequestID(),
		"method":  "completionItem/resolve",
		"params": map[string]any{
			"label":  "DISPLAY",
			"detail": "关键字",
		},
	})
	h.send(string(payload))
	msg := h.readMessage(5 * time.Second)
	if msg == nil {
		t.Fatal("resolve 无响应")
	}
	if msg["error"] != nil {
		t.Fatalf("resolve 报错: %v", msg["error"])
	}
	res, _ := msg["result"].(map[string]any)
	if res == nil || res["label"] != "DISPLAY" {
		t.Errorf("resolve 应原样返回，实际 %v", msg["result"])
	}
}

// firstN 取前 n 个元素（日志用）。
func firstN(ss []string, n int) []string {
	if len(ss) <= n {
		return ss
	}
	return ss[:n]
}

// TestCompletionFormFields 补全应给出光标处表单的字段（复用 P3b 解析器）。
//
// 关键断言：补全给的字段与字段校验认的字段一致——两者走同一个
// Resolver.FormFieldsAt/Check 作用域跟踪，不该出现"补全有、校验报不存在"。
func TestCompletionFormFields(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)

	// 先 OPEN WINDOW 绑定表单，再在 INPUT 里触发补全
	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM \nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	// 第 3 行（0 基 2）"  INPUT x FROM " 行尾 → 空前缀，应含表单字段
	items := h.requestCompletion(t, mainURI, 2, 16)
	if !hasLabel(items, "f01") {
		t.Errorf("应给出表单字段 f01，实际 %v", firstN(itemLabels(items), 15))
	}
	if !hasLabel(items, "f02") {
		t.Errorf("应给出表单字段 f02，实际 %v", firstN(itemLabels(items), 15))
	}
}

// TestCompletionFormFieldsBeforeBinding 绑定之前的补全不该给该表单字段。
//
// 回归：若忽略光标位置、直接用"全文件最后一个绑定"，会在 OPEN WINDOW
// 之前就提示字段，用户会写出引用未打开表单的代码。
func TestCompletionFormFieldsBeforeBinding(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)

	src := "MAIN\n  \n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	// 第 2 行（0 基 1）空白行，在 OPEN WINDOW 之前
	items := h.requestCompletion(t, mainURI, 1, 4)
	for _, it := range items {
		label, _ := it["label"].(string)
		detail, _ := it["detail"].(string)
		if (strings.EqualFold(label, "f01") || strings.EqualFold(label, "f02")) &&
			strings.Contains(detail, "表单字段") {
			t.Errorf("绑定之前的补全不该给表单字段 %q（detail=%q）", label, detail)
		}
	}
}
