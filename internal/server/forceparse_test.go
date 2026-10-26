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
//
// W1：提示里必须给出**真实存在**的设置键 `bdl.limits.parseTimeoutMs`
// （曾经写成不存在的 `basic.limits.parseTimeoutMs`，照着改等于没改）。
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
	msg := diagnosticMessage(diags, "parse-timeout")
	if !strings.Contains(msg, "bdl.limits.parseTimeoutMs") {
		t.Fatalf("超时提示应给出真实设置键 bdl.limits.parseTimeoutMs，实际：%q", msg)
	}
	if strings.Contains(msg, "basic.") {
		t.Fatalf("超时提示不应出现不存在的 basic.* 键，实际：%q", msg)
	}
}

// diagnosticMessage 返回指定 code 的诊断文本；没有则返回空串。
func diagnosticMessage(diags []any, code string) string {
	for _, raw := range diags {
		if d, _ := raw.(map[string]any); d != nil && d["code"] == code {
			msg, _ := d["message"].(string)
			return msg
		}
	}
	return ""
}

// TestForceParseIgnoresTimeoutAndReportsDone 钉住 W1 的核心不变量：
//
//	① 超时上限只约束普通解析，**强制解析不限时**；
//	② 强制解析在后台完成后回执 `bdl/parseDone`（编辑器据此刷新大纲）；
//	③ 回执发出时服务端已经能给出非空大纲（即回执在 SetTree 之后）。
func TestForceParseIgnoresTimeoutAndReportsDone(t *testing.T) {
	dir := t.TempDir()
	uri := testURI(dir, "slow.4gl")
	// 足够大：1ms 上限必然超时；不限时解析则能在秒级内跑完。
	src := "MAIN\n" + strings.Repeat("  DISPLAY 1\n", 5000) + "END MAIN\n"

	h := initializeWithLimits(t, dir, `{"limits":{"parseTimeoutMs":1}}`)
	h.sendDidOpen(uri, "bdl", src, 1)
	if first := readPublish(t, h, uri, 10*time.Second); !hasCode(first, "parse-timeout") {
		t.Fatalf("1ms 上限应先发 parse-timeout，实际：%v", first)
	}

	h.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":21,"method":"bdl/forceParse","params":{"uri":%q}}`, uri))

	done, diags := waitParseDone(t, h, uri, 20*time.Second)
	if done == nil {
		t.Fatal("强制解析后没有收到 bdl/parseDone 回执")
	}
	if ok, _ := done["ok"].(bool); !ok {
		t.Fatalf("回执应为 ok=true，实际：%v", done)
	}
	if ms, _ := done["ms"].(float64); ms < 0 {
		t.Fatalf("回执应带非负耗时，实际：%v", done["ms"])
	}
	if n, _ := done["bytes"].(float64); int(n) != len(src) {
		t.Fatalf("回执字节数应等于文件大小 %d，实际：%v", len(src), done["bytes"])
	}
	if diags == nil {
		t.Fatal("强制解析后没有收到新的 publishDiagnostics")
	}
	if hasCode(diags, "parse-timeout") {
		t.Fatalf("强制解析不限时，不应再发 parse-timeout：%v", diags)
	}

	// 回执之后大纲必须非空（证明树已就绪）。
	h.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":22,"method":"textDocument/documentSymbol","params":{"textDocument":{"uri":%q}}}`, uri))
	syms := readResult(t, h, 22, 5*time.Second)
	if list, _ := syms.([]any); len(list) == 0 {
		t.Fatalf("强制解析后大纲应为非空，实际：%v", syms)
	}
}

// TestRequestForceReparseNeverBlocks 持 parseMu 期间调用必须立刻返回。
//
// 与 TestRequestRediagnoseNeverBlocks 同理：调用点（handle）本身持锁，
// 若 requestForceReparse 同步加锁就会自己锁死自己。
func TestRequestForceReparseNeverBlocks(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.srv.parseMu.Lock()
	done := make(chan struct{})
	go func() {
		h.srv.requestForceReparse("file:///does-not-matter.4gl")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("requestForceReparse 阻塞了调用方：可能同步等待 parseMu")
	}
	h.srv.parseMu.Unlock()
}

// waitParseDone 读帧直到收到该 URI 的 bdl/parseDone 与一份诊断发布。
func waitParseDone(t *testing.T, h *harness, uri string, window time.Duration) (map[string]any, []any) {
	t.Helper()
	deadline := time.Now().Add(window)
	var done map[string]any
	var diags []any
	for time.Now().Before(deadline) {
		msg := h.readMessageSoft(time.Until(deadline))
		if msg == nil {
			break
		}
		switch msg["method"] {
		case MethodParseDone:
			if p, _ := msg["params"].(map[string]any); p != nil && p["uri"] == uri {
				done = p
			}
		case "textDocument/publishDiagnostics":
			if p, _ := msg["params"].(map[string]any); p != nil && p["uri"] == uri {
				diags, _ = p["diagnostics"].([]any)
			}
		}
		if done != nil && diags != nil {
			break
		}
	}
	return done, diags
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
