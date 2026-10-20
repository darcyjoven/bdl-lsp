package server

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// requestPrepareRename 发 prepareRename 请求，返回 (result, error)。
func (h *harness) requestPrepareRename(t *testing.T, uri string, line, char int) (any, any) {
	t.Helper()
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%d,"method":"textDocument/prepareRename","params":{`+
			`"textDocument":{"uri":%q},"position":{"line":%d,"character":%d}}}`,
		nextRequestID(), uri, line, char)
	h.send(payload)
	msg := h.readMessage(5 * time.Second)
	if msg == nil {
		return nil, "no response"
	}
	return msg["result"], msg["error"]
}

// requestRename 发 rename 请求，返回 (result, error)。
func (h *harness) requestRename(t *testing.T, uri string, line, char int, newName string) (any, any) {
	t.Helper()
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%d,"method":"textDocument/rename","params":{`+
			`"textDocument":{"uri":%q},"position":{"line":%d,"character":%d},"newName":%q}}`,
		nextRequestID(), uri, line, char, newName)
	h.send(payload)
	msg := h.readMessage(5 * time.Second)
	if msg == nil {
		return nil, "no response"
	}
	return msg["result"], msg["error"]
}

// renameEdits 从 WorkspaceEdit 里取出某文件的编辑列表。
func renameEdits(result any, uri string) []map[string]any {
	m, _ := result.(map[string]any)
	if m == nil {
		return nil
	}
	changes, _ := m["changes"].(map[string]any)
	if changes == nil {
		return nil
	}
	raw, _ := changes[uri].([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, e := range raw {
		if em, ok := e.(map[string]any); ok {
			out = append(out, em)
		}
	}
	return out
}

// initPlainServer 起一个只有 initialize 的服务器。
func initPlainServer(t *testing.T) *harness {
	t.Helper()
	h, cleanup := startServer(t)
	t.Cleanup(cleanup)
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)
	return h
}

// TestRenameCapability 必须声明 renameProvider 与 prepareProvider。
func TestRenameCapability(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	msg := h.readMessage(3 * time.Second)

	caps, _ := msg["result"].(map[string]any)["capabilities"].(map[string]any)
	if caps == nil {
		t.Fatal("未返回 capabilities")
	}
	rp, ok := caps["renameProvider"].(map[string]any)
	if !ok {
		t.Fatalf("未声明 renameProvider：%v", caps)
	}
	if v, _ := rp["prepareProvider"].(bool); !v {
		t.Errorf("应声明 prepareProvider=true，实际 %v", rp)
	}
}

// TestRenameFunction 改名函数应改定义与全部调用。
func TestRenameFunction(t *testing.T) {
	h := initPlainServer(t)

	src := strings.Join([]string{
		"FUNCTION show_name(p_name)",
		"    DISPLAY p_name",
		"END FUNCTION",
		"MAIN",
		"    CALL show_name()",
		"    CALL show_name()",
		"END MAIN",
		"",
	}, "\n")
	const uri = "file:///tmp/rename_fn.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	// 定义行 show_name 的起始列（"FUNCTION " 之后）
	result, errObj := h.requestRename(t, uri, 0, 11, "display_name")
	if errObj != nil {
		t.Fatalf("rename 报错: %v", errObj)
	}
	edits := renameEdits(result, uri)
	if len(edits) != 3 {
		t.Fatalf("应改 3 处（1 定义 + 2 调用），实际 %d: %v", len(edits), edits)
	}
	for _, e := range edits {
		if nt, _ := e["newText"].(string); nt != "display_name" {
			t.Errorf("newText 应为 display_name，实际 %q", nt)
		}
	}
}

// TestRenameSkipsCommentAndString 注释与字符串里的同名文本不该被改。
func TestRenameSkipsCommentAndString(t *testing.T) {
	h := initPlainServer(t)

	src := strings.Join([]string{
		"-- show_name appears in a comment",
		"FUNCTION show_name()",
		"    DISPLAY \"show_name in string\"",
		"END FUNCTION",
		"",
	}, "\n")
	const uri = "file:///tmp/rename_trivia.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	result, errObj := h.requestRename(t, uri, 1, 11, "other_name")
	if errObj != nil {
		t.Fatalf("rename 报错: %v", errObj)
	}
	edits := renameEdits(result, uri)
	if len(edits) != 1 {
		t.Fatalf("只应改定义 1 处，实际 %d: %v", len(edits), edits)
	}
}

// TestRenameRejectsKeyword 新名字是关键字时必须拒绝。
func TestRenameRejectsKeyword(t *testing.T) {
	h := initPlainServer(t)

	src := "FUNCTION myfunc()\n    DISPLAY 1\nEND FUNCTION\n"
	const uri = "file:///tmp/rename_kw.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	_, errObj := h.requestRename(t, uri, 0, 11, "DISPLAY")
	if errObj == nil {
		t.Fatal("改成关键字应报错")
	}
	msg := fmt.Sprintf("%v", errObj)
	if !strings.Contains(msg, "关键字") {
		t.Errorf("错误信息应说明是关键字：%s", msg)
	}
}

// TestRenameRejectsInvalidIdent 非法标识符必须拒绝。
func TestRenameRejectsInvalidIdent(t *testing.T) {
	h := initPlainServer(t)
	src := "FUNCTION myfunc()\n    DISPLAY 1\nEND FUNCTION\n"
	const uri = "file:///tmp/rename_bad.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	for _, bad := range []string{"1abc", "has space", "a-b", ""} {
		if _, errObj := h.requestRename(t, uri, 0, 11, bad); errObj == nil {
			t.Errorf("非法名字 %q 应被拒绝", bad)
		}
	}
}

// TestRenameRejectsDuplicate 与已有符号同名必须拒绝。
func TestRenameRejectsDuplicate(t *testing.T) {
	h := initPlainServer(t)
	src := strings.Join([]string{
		"FUNCTION alpha_fn()",
		"END FUNCTION",
		"FUNCTION beta_fn()",
		"END FUNCTION",
		"",
	}, "\n")
	const uri = "file:///tmp/rename_dup.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	_, errObj := h.requestRename(t, uri, 0, 11, "beta_fn")
	if errObj == nil {
		t.Fatal("改成已存在的符号名应报错")
	}
	if !strings.Contains(fmt.Sprintf("%v", errObj), "同名") {
		t.Errorf("错误应说明重名：%v", errObj)
	}
}

// TestPrepareRenameOK 可改名时返回范围与占位符。
func TestPrepareRenameOK(t *testing.T) {
	h := initPlainServer(t)
	src := "FUNCTION myfunc()\n    DISPLAY 1\nEND FUNCTION\n"
	const uri = "file:///tmp/prep_ok.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	result, errObj := h.requestPrepareRename(t, uri, 0, 11)
	if errObj != nil {
		t.Fatalf("prepareRename 报错: %v", errObj)
	}
	m, _ := result.(map[string]any)
	if m == nil {
		t.Fatalf("应返回对象，实际 %v", result)
	}
	if ph, _ := m["placeholder"].(string); ph != "myfunc" {
		t.Errorf("placeholder 应为 myfunc，实际 %q", ph)
	}
	if m["range"] == nil {
		t.Error("应返回 range")
	}
}

// TestPrepareRenameOnKeyword 关键字上 prepareRename 应拒绝。
//
// 这样客户端不会弹输入框，而不是等用户输完再失败。
func TestPrepareRenameOnKeyword(t *testing.T) {
	h := initPlainServer(t)
	src := "MAIN\n    DISPLAY 1\nEND MAIN\n"
	const uri = "file:///tmp/prep_kw.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	_, errObj := h.requestPrepareRename(t, uri, 1, 4) // DISPLAY
	if errObj == nil {
		t.Fatal("关键字上 prepareRename 应报错")
	}
}

// TestPrepareRenameInComment 注释里 prepareRename 应拒绝。
func TestPrepareRenameInComment(t *testing.T) {
	h := initPlainServer(t)
	src := "-- some comment here\nMAIN\nEND MAIN\n"
	const uri = "file:///tmp/prep_cmt.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	_, errObj := h.requestPrepareRename(t, uri, 0, 4)
	if errObj == nil {
		t.Fatal("注释里 prepareRename 应报错")
	}
}

// TestRenameUnknownSymbol 非符号文本（内建函数、未声明的词）应拒绝改名。
//
// INFIELD 既是内建函数、也出现在 grammar 的关键字清单里，两种拒绝理由
// 都合理；这里只断言"被拒绝"，不绑定具体措辞。
func TestRenameUnknownSymbol(t *testing.T) {
	h := initPlainServer(t)
	src := "MAIN\n    DISPLAY INFIELD(x)\nEND MAIN\n"
	const uri = "file:///tmp/rename_unknown.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	// 第 2 行（0 基 1）"    DISPLAY INFIELD(x)"：INFIELD 从第 12 列开始，
	// 取第 14 列落在它中间。
	if _, errObj := h.requestRename(t, uri, 1, 14, "other"); errObj == nil {
		t.Fatal("非本文件符号应拒绝改名")
	}
}

// TestRenameUndeclaredWord 文件里未声明的词不能被改名。
func TestRenameUndeclaredWord(t *testing.T) {
	h := initPlainServer(t)
	src := "MAIN\n    DISPLAY some_undeclared_thing\nEND MAIN\n"
	const uri = "file:///tmp/rename_undecl.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	_, errObj := h.requestRename(t, uri, 1, 16, "renamed")
	if errObj == nil {
		t.Fatal("未声明的词应拒绝改名")
	}
	if !strings.Contains(fmt.Sprintf("%v", errObj), "不是本文件声明的符号") {
		t.Errorf("错误应说明未声明：%v", errObj)
	}
}
