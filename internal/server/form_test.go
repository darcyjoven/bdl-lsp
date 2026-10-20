package server

// P3b：字段存在性校验的协议级测试。
//
// 覆盖：
//   - 4gl 里字段不在 .per 中 → field-not-in-form 警告（含 relatedInformation）
//   - 字段存在 → 无警告
//   - 配置 off → 无警告
//   - .per 内部校验：重复 item / 布局 tag 未定义 / SCREEN RECORD 引用
//   - .per 变更刷新引用方（4gl 的字段诊断）

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupFormTree 构造：
//
//	dir/
//	  amm/4gl/main.4gl     ← 引用 amm/42f/ammr110
//	  amm/per/ammr110.per  ← 含 f01/f02
func setupFormTree(t *testing.T) (dir, mainURI, perURI string) {
	t.Helper()
	dir = t.TempDir()
	for _, d := range []string{"amm/4gl", "amm/per"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	per := "SCHEMA ds\n\nLAYOUT\nGRID\n{\n  [f01      ]\n  [f02      ]\n}\nEND\nEND\n\nATTRIBUTES\nEDIT f01 = formonly.f01;\nEDIT f02 = formonly.f02;\nEND\n"
	if err := os.WriteFile(filepath.Join(dir, "amm", "per", "ammr110.per"), []byte(per), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir,
		"file://" + filepath.Join(dir, "amm", "4gl", "main.4gl"),
		"file://" + filepath.Join(dir, "amm", "per", "ammr110.per")
}

func initializeFormServer(t *testing.T, dir string) *harness {
	t.Helper()
	h, cleanup := startServer(t)
	t.Cleanup(cleanup)
	rootURI := "file://" + dir
	h.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":%q,"capabilities":{}}}`, rootURI))
	h.readMessage(3 * time.Second)
	return h
}

func findFieldDiags(diags []any, code string) []map[string]any {
	var out []map[string]any
	for _, d := range diags {
		m, _ := d.(map[string]any)
		if m == nil {
			continue
		}
		if m["code"] == code {
			out = append(out, m)
		}
	}
	return out
}

func TestFieldNotInFormWarns(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)

	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM f01\n    AFTER FIELD no_such\n  END INPUT\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	found := findFieldDiags(diags, "field-not-in-form")
	if len(found) != 1 {
		t.Fatalf("期望 1 条 field-not-in-form，实际 %d（全部 %d 条）", len(found), len(diags))
	}
	d := found[0]
	if d["severity"] != float64(2) { // warning
		t.Errorf("severity 期望 warning(2)，实际 %v", d["severity"])
	}
	if msg, _ := d["message"].(string); msg == "" || !contains(msg, "no_such") {
		t.Errorf("消息应含字段名：%q", msg)
	}
	// relatedInformation 指向 .per
	rel, _ := d["relatedInformation"].([]any)
	if len(rel) == 0 {
		t.Error("应带 relatedInformation 指向表单文件")
	} else {
		ri, _ := rel[0].(map[string]any)
		loc, _ := ri["location"].(map[string]any)
		if loc == nil || !contains(fmt.Sprint(loc["uri"]), "ammr110.per") {
			t.Errorf("relatedInformation 应指向 ammr110.per：%v", loc)
		}
	}
}

func TestFieldExistsNoWarn(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)

	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM f01\n    AFTER FIELD f02\n  END INPUT\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if found := findFieldDiags(diags, "field-not-in-form"); len(found) != 0 {
		for _, d := range found {
			t.Errorf("存在字段不应警告：%v", d["message"])
		}
	}
}

func TestFieldDiagnosticsOff(t *testing.T) {
	dir, mainURI, _ := setupFormTree(t)
	h := initializeFormServer(t, dir)

	cfg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params": map[string]any{
			"settings": map[string]any{
				"bdl": map[string]any{
					"fieldDiagnostics": "off",
				},
			},
		},
	}
	raw, _ := json.Marshal(cfg)
	h.send(string(raw))

	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM f01\n    AFTER FIELD no_such\n  END INPUT\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if found := findFieldDiags(diags, "field-not-in-form"); len(found) != 0 {
		t.Errorf("配置 off 后不应出现，实际 %d 条", len(found))
	}
}

func TestPerDuplicateItem(t *testing.T) {
	dir, _, perURI := setupFormTree(t)
	h := initializeFormServer(t, dir)

	// 重复 f01
	per := "SCHEMA ds\nATTRIBUTES\nEDIT f01 = formonly.a;\nEDIT f01 = formonly.b;\nEND\n"
	h.sendDidOpen(perURI, "per", per, 1)
	diags := h.drainDiagnostics(perURI, 5*time.Second)

	if found := findFieldDiags(diags, "form-duplicate-item"); len(found) != 1 {
		t.Fatalf("期望 1 条重复 item，实际 %d（全部 %d）", len(found), len(diags))
	}
}

func TestPerLayoutTagUndefined(t *testing.T) {
	dir, _, perURI := setupFormTree(t)
	h := initializeFormServer(t, dir)

	per := "SCHEMA ds\nLAYOUT\nGRID\n{\n  [defined  ]\n  [undefined]\n}\nEND\nEND\nATTRIBUTES\nEDIT defined = formonly.d;\nEND\n"
	h.sendDidOpen(perURI, "per", per, 1)
	diags := h.drainDiagnostics(perURI, 5*time.Second)

	found := findFieldDiags(diags, "form-layout-tag-undefined")
	if len(found) != 1 {
		t.Fatalf("期望 1 条布局 tag 未定义，实际 %d（全部 %d）", len(found), len(diags))
	}
	if msg, _ := found[0]["message"].(string); !contains(msg, "undefined") {
		t.Errorf("消息应含 tag 名：%q", msg)
	}
}

func TestPerScreenRecordUndefined(t *testing.T) {
	dir, _, perURI := setupFormTree(t)
	h := initializeFormServer(t, dir)

	per := "SCHEMA ds\nATTRIBUTES\nEDIT f01 = formonly.f01;\nEND\nINSTRUCTIONS\n  SCREEN RECORD sr(f01, missing)\nEND\n"
	h.sendDidOpen(perURI, "per", per, 1)
	diags := h.drainDiagnostics(perURI, 5*time.Second)

	found := findFieldDiags(diags, "form-screen-record-undefined")
	if len(found) != 1 {
		t.Fatalf("期望 1 条 SCREEN RECORD 未定义引用，实际 %d（全部 %d）", len(found), len(diags))
	}
}

func TestPerChangeRefreshes4gl(t *testing.T) {
	dir, mainURI, perURI := setupFormTree(t)
	h := initializeFormServer(t, dir)

	// 1. 打开 4gl：no_such 不在表单 → 警告
	src := "MAIN\n  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n  INPUT x FROM f01\n    AFTER FIELD no_such\n  END INPUT\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)
	if len(findFieldDiags(diags, "field-not-in-form")) != 1 {
		t.Fatalf("初始应有 1 条警告，实际 %d", len(findFieldDiags(diags, "field-not-in-form")))
	}

	// 2. 打开 .per 并把 no_such 加进 ATTRIBUTES
	per := "SCHEMA ds\nATTRIBUTES\nEDIT f01 = formonly.f01;\nEDIT f02 = formonly.f02;\nEDIT no_such = formonly.n;\nEND\n"
	h.sendDidOpen(perURI, "per", per, 1)
	_ = h.drainDiagnostics(perURI, 5*time.Second)

	// 3. 4gl 的警告应消失（内存版 .per 优先）
	diags = h.drainDiagnostics(mainURI, 5*time.Second)
	if found := findFieldDiags(diags, "field-not-in-form"); len(found) != 0 {
		t.Errorf(".per 增加字段后 4gl 警告应消失，实际 %d 条", len(found))
	}
}
