package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 内置符号表（P7-C）的接入测试。
//
// 覆盖四条接线：变量来源白名单、函数可见性、悬停、补全，
// 外加三条"关得掉"的路径（enable=false / disable 列表 / 覆盖文件），
// 因为"关不掉"是这个功能最容易变成长期噪声的地方。

// builtinSource 只用内置名 + 一个明显不是内置的用户函数。
const builtinSource = `MAIN
    DEFINE l_cnt INTEGER
    DEFINE l_x STRING
    LET SQLCA.SQLCODE = 0
    LET l_cnt = ARR_CURR()
    DISPLAY STATUS
    DISPLAY fgl_getenv("HOME")
    LET l_x = UI.Interface.getName()
    CALL s_errmsg("x")
END MAIN
`

// sendBuiltinsConfig 下发 bdl.builtins 配置块。
func sendBuiltinsConfig(t *testing.T, h *harness, body string) {
	t.Helper()
	h.send(fmt.Sprintf(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"bdl":%s}}}`, body))
	// 配置变更会触发重诊断，稍等让服务端处理完。
	time.Sleep(150 * time.Millisecond)
}

// collectCodes 从诊断数组里挑出指定码的条数。
func countCode(diags []any, code string) int {
	n := 0
	for _, raw := range diags {
		d, _ := raw.(map[string]any)
		if c, _ := d["code"].(string); c == code {
			n++
		}
	}
	return n
}

// TestBuiltinWhitelistSilencesBuiltinNames 验证内置名不再报"变量来源不明"。
//
// 这是 P7-C 的核心收益：SQLCA/STATUS/fgl_* /ui 实测占全量诊断 98%。
func TestBuiltinWhitelistSilencesBuiltinNames(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/builtin1.4gl"
	h.sendDidOpen(uri, "bdl", builtinSource, 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if got := countCode(diags, "variable-source-unresolved"); got != 0 {
		for _, raw := range diags {
			d, _ := raw.(map[string]any)
			if c, _ := d["code"].(string); c == "variable-source-unresolved" {
				t.Logf("残留诊断: %v", d["message"])
			}
		}
		t.Errorf("内置名不应产生 variable-source-unresolved，实际 %d 条", got)
	}
}

// TestBuiltinWhitelistStillReportsUnknown 验证白名单不是"全放过"。
func TestBuiltinWhitelistStillReportsUnknown(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/builtin2.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY totally_unknown_name\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if countCode(diags, "variable-source-unresolved") == 0 {
		t.Fatal("非内置的未知名字仍应报告（白名单不能变成全放过）")
	}
}

// TestBuiltinEnableFalseRestoresNoise 验证 bdl.builtins.enable=false
// 回到 P7-C 之前的行为。
func TestBuiltinEnableFalseRestoresNoise(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()
	sendBuiltinsConfig(t, h, `{"builtins":{"enable":false}}`)

	const uri = "file:///tmp/builtin3.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY STATUS\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if countCode(diags, "variable-source-unresolved") == 0 {
		t.Fatal("enable=false 时 STATUS 应重新报未定义")
	}
}

// TestBuiltinDisableMakesNameReportable 验证 disable 优先于内置表。
//
// 语义：用户在项目里自己定义了 STATUS，要能恢复成普通标识符。
func TestBuiltinDisableMakesNameReportable(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()
	sendBuiltinsConfig(t, h, `{"builtins":{"disable":["STATUS"]}}`)

	const uri = "file:///tmp/builtin4.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY STATUS\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if countCode(diags, "variable-source-unresolved") == 0 {
		t.Fatal("disable 掉的内置名应重新报未定义")
	}
}

// TestBuiltinOverlayFileAddsName 验证项目覆盖文件（.bdl-lsp/builtins.json）
// 能新增内置名，且不需要改代码。
func TestBuiltinOverlayFileAddsName(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".bdl-lsp"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"schemaVersion":1,"generoVersion":"2.50","symbols":[
	  {"name":"COMPANY_WIDE_VAR","kind":"variable","doc":"公司级内置变量"}
	]}`
	if err := os.WriteFile(filepath.Join(root, ".bdl-lsp", "builtins.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// 用 rootUri 初始化，让覆盖文件按约定路径自动加载。
	h.send(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":%q,"capabilities":{}}}`,
		"file://"+root))
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/builtin5.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY COMPANY_WIDE_VAR\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if countCode(diags, "variable-source-unresolved") != 0 {
		t.Fatal("覆盖文件里的名字应被豁免诊断")
	}
}

// TestFunctionNotFoundSkipsBuiltinFunction 验证 function-not-found 也吃内置表。
//
// 注意：P7-D 之后 function-not-found 还要求**有链接资料**，所以这里
// 真正的断言是"一条都不报"；内置名豁免与用户函数的对比在
// links_test.go 的 TestLinkFunctionNotFoundWithData 里验证。
func TestFunctionNotFoundSkipsBuiltinFunction(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()
	sendBuiltinsConfig(t, h, `{"diagnostics":{"function-not-found":"warning"}}`)

	const uri = "file:///tmp/builtin6.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY ARG_VAL(1)\n    CALL cl_null(\"x\")\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)

	for _, raw := range diags {
		d, _ := raw.(map[string]any)
		code, _ := d["code"].(string)
		if code != "function-not-found" {
			continue
		}
		msg, _ := d["message"].(string)
		if strings.Contains(msg, "ARG_VAL") {
			t.Error("内置函数 ARG_VAL 不应报 function-not-found")
		}
		if strings.Contains(msg, "cl_null") {
			t.Error("没有链接资料时不应报任何 function-not-found")
		}
	}
}

// TestHoverBuiltinShowsSignature 验证内置名悬停给出签名与出处。
func TestHoverBuiltinShowsSignature(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/builtin7.4gl"
	h.openBDL(uri, `MAIN
    DISPLAY STATUS
END MAIN
`)

	// 第 1 行（0 基）`    DISPLAY STATUS`，STATUS 从第 12 列开始。
	resp := h.request(40, "textDocument/hover", fmt.Sprintf(
		`{"textDocument":{"uri":%q},"position":{"line":1,"character":13}}`, uri))
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatal("内置名悬停应有结果")
	}
	contents, _ := result["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	if !strings.Contains(value, "STATUS") {
		t.Errorf("悬停应包含名字，实际 %q", value)
	}
	if !strings.Contains(value, "内置变量") {
		t.Errorf("悬停应标注是内置变量，实际 %q", value)
	}
	if !strings.Contains(value, "bdl.builtins") {
		t.Errorf("悬停应说明来源可覆盖，实际 %q", value)
	}
}

// TestHoverBuiltinLosesToUserSymbol 验证用户自己的同名符号优先。
//
// 否则项目里 DEFINE status INTEGER 会被内置表冒充，悬停显示错误信息。
func TestHoverBuiltinLosesToUserSymbol(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/builtin8.4gl"
	h.openBDL(uri, `MAIN
    DEFINE status INTEGER
    LET status = 1
    DISPLAY status
END MAIN
`)

	// 第 3 行 `    DISPLAY status`，status 从第 12 列开始。
	resp := h.request(41, "textDocument/hover", fmt.Sprintf(
		`{"textDocument":{"uri":%q},"position":{"line":3,"character":13}}`, uri))
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatal("悬停应有结果")
	}
	contents, _ := result["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	if strings.Contains(value, "内置变量") {
		t.Errorf("用户定义的 status 不应显示成内置变量，实际 %q", value)
	}
}

// TestCompletionBuiltinPrefix 验证补全能按前缀给出内置函数。
func TestCompletionBuiltinPrefix(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/builtin9.4gl"
	h.openBDL(uri, `MAIN
    DISPLAY fgl_get
END MAIN
`)

	// 光标在 `fgl_get` 之后（第 1 行第 20 列）。
	resp := h.request(50, "textDocument/completion", fmt.Sprintf(
		`{"textDocument":{"uri":%q},"position":{"line":1,"character":20}}`, uri))
	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("补全无结果: %v", resp["result"])
	}
	items, _ := result["items"].([]any)
	var hit int
	for _, raw := range items {
		it, _ := raw.(map[string]any)
		label, _ := it["label"].(string)
		if strings.HasPrefix(strings.ToLower(label), "fgl_get") {
			hit++
		}
	}
	if hit == 0 {
		t.Fatalf("输入 fgl_get 应有内置函数候选，实际 %d 项", len(items))
	}
}

// TestCompletionBuiltinSkipsDisabled 验证 disable 掉的名字不进补全。
func TestCompletionBuiltinSkipsDisabled(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()
	sendBuiltinsConfig(t, h, `{"builtins":{"disable":["FGL_GETENV"]}}`)

	const uri = "file:///tmp/builtin10.4gl"
	h.openBDL(uri, `MAIN
    DISPLAY fgl_getenv
END MAIN
`)

	resp := h.request(51, "textDocument/completion", fmt.Sprintf(
		`{"textDocument":{"uri":%q},"position":{"line":1,"character":22}}`, uri))
	result, ok := resp["result"].(map[string]any)
	if !ok {
		return // 没有候选也算通过
	}
	items, _ := result["items"].([]any)
	for _, raw := range items {
		it, _ := raw.(map[string]any)
		label, _ := it["label"].(string)
		if strings.EqualFold(label, "FGL_GETENV") {
			t.Error("被 disable 的内置名不应出现在补全里")
		}
	}
}
