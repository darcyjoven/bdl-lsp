package server

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// setRecordMemberLevel 把 recordMemberDiagnostics 设为指定级别。
func (h *harness) setRecordMemberLevel(t *testing.T, level string) {
	t.Helper()
	cfg := fmt.Sprintf(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":`+
		`{"settings":{"bdl":{"recordMemberDiagnostics":%q}}}}`, level)
	h.send(cfg)
	// didChangeConfiguration 无响应，等一小会确保服务器已处理。
	time.Sleep(120 * time.Millisecond)
}

// recordMemberDiags 过滤出 record-member-unknown 诊断。
func recordMemberDiags(diags []any) []map[string]any {
	var out []map[string]any
	for _, d := range diags {
		m, _ := d.(map[string]any)
		if m == nil {
			continue
		}
		if s, _ := m["code"].(string); s == codeRecordMemberUnknown {
			out = append(out, m)
		}
	}
	return out
}

// recordSrc 构造一个带 RECORD 类型与变量的源码。
func recordSrc(body string) string {
	return strings.Join([]string{
		"TYPE t_rec RECORD",
		"    a INTEGER,",
		"    b STRING",
		"END RECORD",
		"DEFINE tm t_rec",
		"MAIN",
		body,
		"END MAIN",
		"",
	}, "\n")
}

// TestRecordMemberDefaultOff 默认 off：即使成员不存在也不报。
//
// 这是 P6 明确决策：`DEFINE r LIKE table.*` 在语料里很常见，
// 静态看不到列名，默认开启会大面积误报。
func TestRecordMemberDefaultOff(t *testing.T) {
	h := initPlainServer(t)
	const uri = "file:///tmp/rec_off.4gl"
	// no_such 不是 t_rec 的成员
	h.sendDidOpen(uri, "bdl", recordSrc(`    DISPLAY BY NAME tm.no_such`), 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if got := recordMemberDiags(diags); len(got) != 0 {
		t.Errorf("默认 off 时不该报告，实际 %d 条: %v", len(got), got)
	}
}

// TestRecordMemberWarnsWhenEnabled 开启后，不存在的成员应报。
func TestRecordMemberWarnsWhenEnabled(t *testing.T) {
	h := initPlainServer(t)
	h.setRecordMemberLevel(t, "warning")

	const uri = "file:///tmp/rec_on.4gl"
	h.sendDidOpen(uri, "bdl", recordSrc(`    DISPLAY BY NAME tm.no_such`), 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	got := recordMemberDiags(diags)
	if len(got) != 1 {
		t.Fatalf("应报 1 条，实际 %d 条（全部诊断 %d 条）", len(got), len(diags))
	}
	msg, _ := got[0]["message"].(string)
	if !strings.Contains(msg, "no_such") {
		t.Errorf("消息应含成员名 no_such：%s", msg)
	}
	if !strings.Contains(msg, "t_rec") {
		t.Errorf("消息应含类型名 t_rec：%s", msg)
	}
	if got[0]["severity"] != float64(2) { // warning
		t.Errorf("级别应为 warning(2)，实际 %v", got[0]["severity"])
	}
}

// TestRecordMemberValidNoWarn 存在的成员不报（防误报）。
func TestRecordMemberValidNoWarn(t *testing.T) {
	h := initPlainServer(t)
	h.setRecordMemberLevel(t, "warning")

	const uri = "file:///tmp/rec_ok.4gl"
	h.sendDidOpen(uri, "bdl", recordSrc(`    DISPLAY BY NAME tm.a, tm.b`), 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if got := recordMemberDiags(diags); len(got) != 0 {
		t.Errorf("存在的成员不该报，实际 %v", got)
	}
}

// TestRecordMemberStarNotChecked `rec.*` 通配不该报。
//
// 语法树里 `tm.*` 没有 property 节点（`*` 被 grammar 吞掉），
// 本来就取不到成员名——这也正是它不需要逐个校验的原因。
func TestRecordMemberStarNotChecked(t *testing.T) {
	h := initPlainServer(t)
	h.setRecordMemberLevel(t, "warning")

	const uri = "file:///tmp/rec_star.4gl"
	h.sendDidOpen(uri, "bdl", recordSrc(`    DISPLAY BY NAME tm.*`), 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if got := recordMemberDiags(diags); len(got) != 0 {
		t.Errorf("`rec.*` 通配不该报，实际 %v", got)
	}
}

// TestRecordMemberUnknownTypeSkipped 记录类型未知时不校验。
//
// `DEFINE r LIKE table.*` 这类静态看不到列名，硬校验会大量误报。
func TestRecordMemberUnknownTypeSkipped(t *testing.T) {
	h := initPlainServer(t)
	h.setRecordMemberLevel(t, "warning")

	src := strings.Join([]string{
		"MAIN",
		"    DEFINE r LIKE type_file.num1", // 类型不是本地 RECORD
		"    DISPLAY BY NAME r.no_such_member",
		"END MAIN",
		"",
	}, "\n")
	const uri = "file:///tmp/rec_like.4gl"
	h.sendDidOpen(uri, "bdl", src, 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if got := recordMemberDiags(diags); len(got) != 0 {
		t.Errorf("类型未知时不该报，实际 %v", got)
	}
}

// TestRecordMemberLevelOff 显式 off 同样不报。
func TestRecordMemberLevelOff(t *testing.T) {
	h := initPlainServer(t)
	h.setRecordMemberLevel(t, "off")

	const uri = "file:///tmp/rec_explicit_off.4gl"
	h.sendDidOpen(uri, "bdl", recordSrc(`    DISPLAY BY NAME tm.no_such`), 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if got := recordMemberDiags(diags); len(got) != 0 {
		t.Errorf("off 时不该报告，实际 %v", got)
	}
}

// TestRecordMemberErrorLevel 设为 error 时级别应为 error。
func TestRecordMemberErrorLevel(t *testing.T) {
	h := initPlainServer(t)
	h.setRecordMemberLevel(t, "error")

	const uri = "file:///tmp/rec_error.4gl"
	h.sendDidOpen(uri, "bdl", recordSrc(`    INPUT BY NAME tm.bad_field`), 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	got := recordMemberDiags(diags)
	if len(got) != 1 {
		t.Fatalf("应报 1 条，实际 %d", len(got))
	}
	if got[0]["severity"] != float64(1) { // error
		t.Errorf("级别应为 error(1)，实际 %v", got[0]["severity"])
	}
}

// TestRecordMemberMultiple 多个错误成员应逐个报。
func TestRecordMemberMultiple(t *testing.T) {
	h := initPlainServer(t)
	h.setRecordMemberLevel(t, "warning")

	const uri = "file:///tmp/rec_multi.4gl"
	src := recordSrc(`    DISPLAY BY NAME tm.bad1, tm.a, tm.bad2`)
	h.sendDidOpen(uri, "bdl", src, 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	got := recordMemberDiags(diags)
	if len(got) != 2 {
		t.Fatalf("应报 2 条（bad1/bad2），实际 %d", len(got))
	}
	// 消息里应分别出现 bad1 与 bad2
	var msgs []string
	for _, d := range got {
		if s, _ := d["message"].(string); s != "" {
			msgs = append(msgs, s)
		}
	}
	joined := strings.Join(msgs, "\n")
	for _, want := range []string{"bad1", "bad2"} {
		if !strings.Contains(joined, want) {
			t.Errorf("消息应含 %q：%s", want, joined)
		}
	}
}
