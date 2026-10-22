package server

// P8：`bdl.ignore` 的协议级测试。
//
// 覆盖：
//   - files：命中文件后语法/字段诊断全部静默
//   - fields：只屏蔽 field-not-in-form
//   - forms：按字面量/文件名屏蔽整张表单的字段诊断
//   - 配置热更新后立即生效（无需重开文件）

import (
	"encoding/json"
	"testing"
	"time"
)

func sendIgnoreConfig(t *testing.T, h *harness, ignore map[string]any) {
	t.Helper()
	cfg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params": map[string]any{
			"settings": map[string]any{"bdl": map[string]any{"ignore": ignore}},
		},
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	h.send(string(raw))
}

func TestIgnoreFileSuppressesAllDiagnostics(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)
	sendIgnoreConfig(t, h, map[string]any{"files": []string{"**/main.4gl"}})

	// 同时有语法错误与字段错误：两条都不应发布。
	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  DISPLAY \"unterminated\n  AFTER FIELD no_such\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)
	if len(diags) != 0 {
		t.Fatalf("被忽略文件不应有任何诊断，实际 %d 条：%v", len(diags), diags)
	}
}

func TestIgnoreFieldSuppressesFieldDiagnostic(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)
	sendIgnoreConfig(t, h, map[string]any{"fields": []string{"NO_SUCH"}})

	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM f01\n    AFTER FIELD no_such\n    AFTER FIELD other_missing\n  END INPUT\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	found := findFieldDiags(diags, "field-not-in-form")
	if len(found) != 1 {
		t.Fatalf("应只剩 other_missing 一条，实际 %d 条", len(found))
	}
	if msg, _ := found[0]["message"].(string); !contains(msg, "other_missing") {
		t.Errorf("被忽略的 no_such 仍在报：%q", msg)
	}
}

func TestIgnoreFormSuppressesFieldDiagnostic(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)
	sendIgnoreConfig(t, h, map[string]any{"forms": []string{"ammr110"}})

	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM f01\n    AFTER FIELD no_such\n  END INPUT\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)
	if found := findFieldDiags(diags, "field-not-in-form"); len(found) != 0 {
		t.Fatalf("整张表单被忽略后不应报字段，实际 %d 条", len(found))
	}
}

func TestIgnoreConfigHotReload(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)

	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM f01\n    AFTER FIELD no_such\n  END INPUT\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	if found := findFieldDiags(h.drainDiagnostics(mainURI, 5*time.Second), "field-not-in-form"); len(found) != 1 {
		t.Fatalf("基线应有 1 条字段诊断，实际 %d", len(found))
	}

	// 热更新：配置变更后服务器会重诊断已打开文档，无需重开。
	sendIgnoreConfig(t, h, map[string]any{"files": []string{"**/main.4gl"}})
	if diags := h.drainDiagnostics(mainURI, 5*time.Second); len(diags) != 0 {
		t.Fatalf("热更新后应立即静默，实际 %d 条", len(diags))
	}
}
