package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/wsindex"
)

// newWorkspace 建一个 tiptop 布局的临时工作区。
func newWorkspace(t *testing.T) (root string, files map[string]string) {
	t.Helper()
	root = t.TempDir()
	files = map[string]string{
		// 调用方（测试里会打开它）
		"tiptop/aap/4gl/aapr100.4gl": "MAIN\n  CALL aapr200()\nEND MAIN\n",
		// 被调用者的定义（测试里**不打开**）
		"tiptop/aap/4gl/aapr200.4gl": "FUNCTION aapr200()\n  DISPLAY \"x\"\nEND FUNCTION\n",
		// 另一个调用方（也不打开）
		"tiptop/aap/4gl/aapr300.4gl": "MAIN\n  CALL aapr200()\nEND MAIN\n",
	}
	for rel, content := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root, files
}

// initWS 以给定工作区根完成 initialize + initialized（后台索引由此启动）。
func (h *harness) initWS(root string) {
	h.t.Helper()
	uri := fileURIOf(root)
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":` +
		quoteJSON(uri) + `,"capabilities":{}}}`)
	h.readMessage(5 * time.Second)
	h.send(`{"jsonrpc":"2.0","method":"initialized","params":{}}`)
}

// waitIndex 等后台索引建完（最多 20 s）。
func (h *harness) waitIndex() *wsindex.Index {
	h.t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		st := h.srv.wsState()
		st.mu.RLock()
		ix, building, err := st.idx, st.building, st.err
		st.mu.RUnlock()
		if ix != nil && !building {
			return ix
		}
		if err != nil {
			h.t.Fatalf("索引失败: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
	h.t.Fatal("等待工作区索引超时")
	return nil
}

// TestWorkspaceSymbolIncludesUnopenedFiles 验证 workspace/symbol 能搜到没打开过的文件。
func TestWorkspaceSymbolIncludesUnopenedFiles(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root, _ := newWorkspace(t)
	h.initWS(root)
	ix := h.waitIndex()
	if ix.FileCount() != 3 {
		t.Fatalf("索引文件数 = %d，期望 3", ix.FileCount())
	}

	resp := h.request(20, "workspace/symbol", `{"query":"aapr2"}`)
	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("result 不是数组: %v", resp)
	}
	var found bool
	for _, raw := range result {
		sym, _ := raw.(map[string]any)
		if sym["name"] == "aapr200" {
			found = true
			loc, _ := sym["location"].(map[string]any)
			uri, _ := loc["uri"].(string)
			if !strings.HasSuffix(uri, "aapr200.4gl") {
				t.Errorf("符号位置 = %q，期望指向 aapr200.4gl", uri)
			}
		}
	}
	if !found {
		t.Fatalf("未搜到未打开文件里的 aapr200：%v", result)
	}
}

// TestDefinitionCrossFile 验证跳转能跳到没打开过的文件。
func TestDefinitionCrossFile(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root, files := newWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/aapr100.4gl"))
	h.openBDL(uri, files["tiptop/aap/4gl/aapr100.4gl"])

	// `CALL aapr200()`：第 1 行，aapr200 从第 7 列开始
	resp := h.request(21, "textDocument/definition",
		`{"textDocument":{"uri":`+quoteJSON(uri)+`},"position":{"line":1,"character":9}}`)
	locs, ok := resp["result"].([]any)
	if !ok || len(locs) == 0 {
		t.Fatalf("跨文件跳转无结果: %v", resp)
	}
	loc, _ := locs[0].(map[string]any)
	gotURI, _ := loc["uri"].(string)
	if !strings.HasSuffix(gotURI, "aapr200.4gl") {
		t.Fatalf("跳转到了 %q，期望 aapr200.4gl", gotURI)
	}
	rng, _ := loc["range"].(map[string]any)
	start, _ := rng["start"].(map[string]any)
	if start["line"] != float64(0) || start["character"] != float64(9) {
		t.Errorf("位置 = %v，期望 0:9", start)
	}
}

// TestReferencesCrossFile 验证引用能列出别的文件里的调用点。
func TestReferencesCrossFile(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root, files := newWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/aapr100.4gl"))
	h.openBDL(uri, files["tiptop/aap/4gl/aapr100.4gl"])

	resp := h.request(22, "textDocument/references",
		`{"textDocument":{"uri":`+quoteJSON(uri)+`},"position":{"line":1,"character":9},`+
			`"context":{"includeDeclaration":true}}`)
	locs, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("result 不是数组: %v", resp)
	}
	var inSelf, inOther bool
	for _, raw := range locs {
		loc, _ := raw.(map[string]any)
		u, _ := loc["uri"].(string)
		switch {
		case strings.HasSuffix(u, "aapr100.4gl"):
			inSelf = true
		case strings.HasSuffix(u, "aapr300.4gl"):
			inOther = true
		}
	}
	if !inSelf {
		t.Error("缺少本文件的调用点")
	}
	if !inOther {
		t.Errorf("缺少跨文件调用点（aapr300.4gl）：%v", locs)
	}
}

// TestIndexDisabled 验证 bdl.index.enable=false 时不建索引。
func TestIndexDisabled(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root, _ := newWorkspace(t)

	uri := fileURIOf(root)
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":` +
		quoteJSON(uri) + `,"capabilities":{},"initializationOptions":{"index":{"enable":false}}}}`)
	h.readMessage(5 * time.Second)
	h.send(`{"jsonrpc":"2.0","method":"initialized","params":{}}`)

	time.Sleep(300 * time.Millisecond)
	if ix := h.srv.workspaceIndex(); ix != nil {
		t.Fatalf("索引已关闭，不应建索引（files=%d）", ix.FileCount())
	}
}

// fileURIOf 把本地路径转成 file:// URI。
func fileURIOf(path string) string {
	return string(pathURI(path))
}

func quoteJSON(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
