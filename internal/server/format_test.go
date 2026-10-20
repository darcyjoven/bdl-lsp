package server

// P4：格式化的协议级测试。
//
// 覆盖：
//   - .4gl 格式化返回整文替换编辑（关键字大小写 + 缩进）
//   - .per 不做任何格式化（返回 nil——用户明确决定）
//   - 配置生效：keywords=upper / indentStyle=tab / indentSize
//   - 编辑器请求的 tabSize/insertSpaces 在 auto 模式下生效
//   - 语法错误文件拒绝格式化

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"
)

func openDocAndDrain(t *testing.T, h *harness, uri, lang, text string) {
	t.Helper()
	h.sendDidOpen(uri, lang, text, 1)
	h.drainDiagnostics(uri, 5*time.Second)
}

// requestFormatting 发 formatting 请求并返回 result。
func (h *harness) requestFormatting(t *testing.T, uri string, tabSize int, insertSpaces bool, requestID int) any {
	t.Helper()
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"textDocument/formatting","params":{"textDocument":{"uri":%q},"options":{"tabSize":%d,"insertSpaces":%t}}}`,
		requestID, uri, tabSize, insertSpaces)
	h.send(payload)
	msg := h.readMessage(5 * time.Second)
	return msg["result"]
}

var fmtCounter int

func nextFmtID() int {
	fmtCounter++
	return 1300 + fmtCounter
}

func TestFormatting4GL(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/fmt.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n")

	res := h.requestFormatting(t, uri, 4, true, nextFmtID())
	edits, ok := res.([]any)
	if !ok || len(edits) != 1 {
		t.Fatalf("期望 1 个编辑，实际 %v", res)
	}
	e, _ := edits[0].(map[string]any)
	newText, _ := e["newText"].(string)
	if newText == "" {
		t.Fatal("newText 为空")
	}
	// 默认（无配置）：keywords=unchanged、auto 缩进（请求 4 空格）
	if want := "MAIN\n    IF x THEN\n        DISPLAY y\n    END IF\nEND MAIN\n"; newText != want {
		t.Errorf("格式化结果不符：\n--- 期望 ---\n%q\n--- 实际 ---\n%q", want, newText)
	}
}

func TestFormattingKeywordsUpper(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	// 配置：关键字大写
	cfg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params": map[string]any{
			"settings": map[string]any{
				"bdl": map[string]any{
					"format": map[string]any{
						"keywords": "upper",
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(cfg)
	h.send(string(raw))

	const uri = "file:///tmp/fmt2.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\nif x then\ndisplay y\nend if\nend main\n")

	res := h.requestFormatting(t, uri, 4, true, nextFmtID())
	edits, _ := res.([]any)
	if len(edits) != 1 {
		t.Fatalf("期望 1 个编辑，实际 %v", res)
	}
	e, _ := edits[0].(map[string]any)
	newText, _ := e["newText"].(string)
	if want := "MAIN\n    IF x THEN\n        DISPLAY y\n    END IF\nEND MAIN\n"; newText != want {
		t.Errorf("大写格式化不符：\n--- 期望 ---\n%q\n--- 实际 ---\n%q", want, newText)
	}
}

func TestFormattingIndentConfigWins(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	// 配置：tab 缩进（覆盖编辑器请求的 4 空格）
	cfg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params": map[string]any{
			"settings": map[string]any{
				"bdl": map[string]any{
					"format": map[string]any{
						"indentStyle": "tab",
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(cfg)
	h.send(string(raw))

	const uri = "file:///tmp/fmt3.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n")

	// 请求说 4 空格，但配置 tab 优先
	res := h.requestFormatting(t, uri, 4, true, nextFmtID())
	edits, _ := res.([]any)
	e, _ := edits[0].(map[string]any)
	newText, _ := e["newText"].(string)
	if want := "MAIN\n\tIF x THEN\n\t\tDISPLAY y\n\tEND IF\nEND MAIN\n"; newText != want {
		t.Errorf("tab 缩进不符：\n--- 期望 ---\n%q\n--- 实际 ---\n%q", want, newText)
	}
}

func TestFormattingIndentSizeConfig(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	cfg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params": map[string]any{
			"settings": map[string]any{
				"bdl": map[string]any{
					"format": map[string]any{
						"indentStyle": "space",
						"indentSize":  2,
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(cfg)
	h.send(string(raw))

	const uri = "file:///tmp/fmt4.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n")

	res := h.requestFormatting(t, uri, 8, true, nextFmtID())
	edits, _ := res.([]any)
	e, _ := edits[0].(map[string]any)
	newText, _ := e["newText"].(string)
	if want := "MAIN\n  IF x THEN\n    DISPLAY y\n  END IF\nEND MAIN\n"; newText != want {
		t.Errorf("2 空格缩进不符：\n--- 期望 ---\n%q\n--- 实际 ---\n%q", want, newText)
	}
}

func TestFormattingFollowsEditorTabSize(t *testing.T) {
	// auto 模式（默认）：跟随编辑器请求（insertSpaces=false → tab）
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/fmt5.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n")

	res := h.requestFormatting(t, uri, 4, false, nextFmtID())
	edits, _ := res.([]any)
	e, _ := edits[0].(map[string]any)
	newText, _ := e["newText"].(string)
	if want := "MAIN\n\tIF x THEN\n\t\tDISPLAY y\n\tEND IF\nEND MAIN\n"; newText != want {
		t.Errorf("跟随编辑器 tab 不符：%q", newText)
	}
}

func TestFormattingPERRefused(t *testing.T) {
	// 用户决定：.per 不做任何格式化
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/fmt.per"
	openDocAndDrain(t, h, uri, "per", "SCHEMA ds\nLAYOUT\nGRID\n{\n}\nEND\nEND\n")

	res := h.requestFormatting(t, uri, 4, true, nextFmtID())
	if res == nil {
		return // 正确：nil
	}
	edits, ok := res.([]any)
	if !ok {
		t.Fatalf("期望 nil，实际 %v", res)
	}
	if len(edits) != 0 {
		t.Errorf(".per 不应产生任何编辑，实际 %d 条", len(edits))
	}
}

func TestFormattingBrokenFileRefused(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/fmtbroken.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\nDISPLAY \"unterminated\nEND MAIN\n")

	res := h.requestFormatting(t, uri, 4, true, nextFmtID())
	if res == nil {
		return // 正确：拒绝
	}
	edits, ok := res.([]any)
	if !ok {
		t.Fatalf("期望 nil，实际 %v", res)
	}
	if len(edits) != 0 {
		t.Errorf("语法错误文件不应格式化，实际 %d 条编辑", len(edits))
	}
}

func TestFormattingIdempotentViaServer(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/fmt6.4gl"
	openDocAndDrain(t, h, uri, "bdl", "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n")

	res1 := h.requestFormatting(t, uri, 4, true, nextFmtID())
	edits1, _ := res1.([]any)
	e1, _ := edits1[0].(map[string]any)
	text1, _ := e1["newText"].(string)

	// 把格式化结果写回文档，再格式化一次
	h.sendDidChange(uri, 2, fmt.Sprintf(`[{"text":%q}]`, text1))
	h.drainDiagnostics(uri, 5*time.Second)

	res2 := h.requestFormatting(t, uri, 4, true, nextFmtID())
	edits2, _ := res2.([]any)
	if len(edits2) == 0 {
		return // 无变化 = 幂等
	}
	e2, _ := edits2[0].(map[string]any)
	text2, _ := e2["newText"].(string)
	if text1 != text2 {
		t.Errorf("幂等性失败：\n第一次 %q\n第二次 %q", text1, text2)
	}
}
