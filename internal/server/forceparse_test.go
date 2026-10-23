package server

// P0-3：文件因为**上限**或**超时**没被解析时，必须明确告知用户；
// 并且能对当前文件"强制解析"（有效期到文件关闭为止）。

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// initializeWithLimits 用给定的 initializationOptions 完成 initialize。
func initializeWithLimits(t *testing.T, dir, opts string) *harness {
	t.Helper()
	h, cleanup := startServer(t)
	t.Cleanup(cleanup)
	rootURI := testURI(dir)
	h.send(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":%q,"capabilities":{},"initializationOptions":%s}}`,
		rootURI, opts))
	h.readMessage(3 * time.Second)
	return h
}

// readPublish 读帧直到出现该 URI 的 publishDiagnostics，返回其诊断内容。
//
// 其它帧（含请求响应）直接跳过；超时返回 nil。
func readPublish(t *testing.T, h *harness, uri string, window time.Duration) []any {
	t.Helper()
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		msg := h.readMessageSoft(time.Until(deadline))
		if msg == nil {
			return nil
		}
		if msg["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		p, _ := msg["params"].(map[string]any)
		if p["uri"] != uri {
			continue
		}
		diags, _ := p["diagnostics"].([]any)
		return diags
	}
	return nil
}

func hasCode(diags []any, code string) bool {
	for _, raw := range diags {
		if d, _ := raw.(map[string]any); d != nil && d["code"] == code {
			return true
		}
	}
	return false
}

// TestTooLargeNoticeThenForceParse 超过行数上限 → 发 file-too-large；
// 强制解析后该文件按正常流程解析（能出大纲），不再发提示；
// 关闭文件再打开 → 提示回来（有效期语义）。
func TestTooLargeNoticeThenForceParse(t *testing.T) {
	dir := t.TempDir()
	uri := testURI(dir, "big.4gl")
	src := "MAIN\n" + strings.Repeat("  DISPLAY 1\n", 20) + "END MAIN\n"

	h := initializeWithLimits(t, dir, `{"limits":{"maxFileLines":5}}`)
	h.sendDidOpen(uri, "bdl", src, 1)

	first := readPublish(t, h, uri, 5*time.Second)
	if !hasCode(first, "file-too-large") {
		t.Fatalf("超过行数上限应发 file-too-large，实际：%v", first)
	}

	// 强制解析：服务端应把它记入集合、重解析，并在响应后给出一份"真"诊断。
	h.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":9,"method":"bdl/forceParse","params":{"uri":%q}}`, uri))
	after := readPublish(t, h, uri, 5*time.Second)
	if after == nil {
		t.Fatal("强制解析后没有收到新的 publishDiagnostics")
	}
	if hasCode(after, "file-too-large") {
		t.Fatalf("强制解析后不应再发 file-too-large：%v", after)
	}

	// 真的解析了吗？用 documentSymbol 证明（MAIN 应在大纲里）。
	h.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":10,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":%q}}}`, uri))
	syms := readResult(t, h, 10, 5*time.Second)
	if list, _ := syms.([]any); len(list) == 0 {
		t.Fatalf("强制解析后大纲应为非空，实际：%v", syms)
	}

	// 关闭 → 标记失效；重新打开 → 提示回来。
	h.send(fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didClose","params":{"textDocument":{"uri":%q}}}`, uri))
	h.readMessageSoft(500 * time.Millisecond)
	h.sendDidOpen(uri, "bdl", src, 2)
	reopened := readPublish(t, h, uri, 5*time.Second)
	if !hasCode(reopened, "file-too-large") {
		t.Fatalf("关闭再打开后应重新发 file-too-large（强制解析只到文件关闭），实际：%v", reopened)
	}
}

// TestParseTimeoutNotice 解析超时要发 parse-timeout（以前只写日志）。
func TestParseTimeoutNotice(t *testing.T) {
	dir := t.TempDir()
	uri := testURI(dir, "slow.4gl")
	// 足够大，1ms 的解析上限必然超时。
	src := "MAIN\n" + strings.Repeat("  DISPLAY 1\n", 5000) + "END MAIN\n"

	h := initializeWithLimits(t, dir, `{"limits":{"parseTimeoutMs":1}}`)
	h.sendDidOpen(uri, "bdl", src, 1)

	diags := readPublish(t, h, uri, 10*time.Second)
	if !hasCode(diags, "parse-timeout") {
		t.Fatalf("解析超时应发 parse-timeout，实际：%v", diags)
	}
}

// readResult 读帧直到 id 匹配的响应，返回其 result。
func readResult(t *testing.T, h *harness, id int, window time.Duration) any {
	t.Helper()
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		msg := h.readMessageSoft(time.Until(deadline))
		if msg == nil {
			return nil
		}
		if v, ok := msg["id"].(float64); ok && int(v) == id {
			return msg["result"]
		}
	}
	return nil
}
