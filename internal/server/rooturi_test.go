package server

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
	"go.lsp.dev/protocol"
)

// TestWorkspaceRootFallback 覆盖 initialize 时工作区根的选取顺序（P7-A-2）。
//
// 背景：旧实现只读 params.RootURI。VSCode 在"打开单个文件"或配置了多根
// 工作区时会让 rootUri 为 null，此时 rootPath 为空，于是
// globals.searchPaths / perRoots / .bdl-lsp/config.json 全部静默失效。
//
// 这是纯函数，直接测三级回退顺序，不需要真实 LSP 会话。
func TestWorkspaceRootFallback(t *testing.T) {
	cases := []struct {
		name   string
		params protocol.InitializeParams
		want   string
	}{
		{
			name:   "rootUri 优先",
			params: protocol.InitializeParams{RootURI: "file:///w/root"},
			want:   "file:///w/root",
		},
		{
			name: "rootUri 空时回退 workspaceFolders",
			params: protocol.InitializeParams{
				WorkspaceFolders: []protocol.WorkspaceFolder{
					{URI: "file:///w/folder", Name: "folder"},
				},
			},
			want: "file:///w/folder",
		},
		{
			name:   "rootUri 与 folders 都空时回退 rootPath",
			params: protocol.InitializeParams{RootPath: "/w/legacy"},
			want:   "/w/legacy",
		},
		{
			name:   "三级都空返回空串",
			params: protocol.InitializeParams{},
			want:   "",
		},
		{
			name: "rootUri 存在时不看 folders",
			params: protocol.InitializeParams{
				RootURI: "file:///w/root",
				WorkspaceFolders: []protocol.WorkspaceFolder{
					{URI: "file:///w/other", Name: "other"},
				},
			},
			want: "file:///w/root",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := workspaceRootURI(&tc.params); got != tc.want {
				t.Errorf("workspaceRootURI() = %q，期望 %q", got, tc.want)
			}
		})
	}
}

// TestInitializeWithoutRootURI 确认 rootUri 缺失时服务器仍能初始化，
// 且 rootPath 通过 workspaceFolders 正确落地。
func TestInitializeWithoutRootURI(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	// rootUri 为 null，只给 workspaceFolders
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":` +
		`{"processId":null,"rootUri":null,"capabilities":{},` +
		`"workspaceFolders":[{"uri":"file:///tmp/proj","name":"proj"}]}}`)
	resp := h.readMessage(3 * time.Second)
	if resp["error"] != nil {
		t.Fatalf("initialize 返回错误: %v", resp["error"])
	}

	srv := h.srv
	if srv.rootPath == "" {
		t.Error("rootPath 为空：workspaceFolders 回退失效")
	}
	if srv.cfg == nil {
		t.Error("配置管理器未初始化")
	}
	// 期望解成本地路径 /tmp/proj（Windows 上同一 URI 解成 \tmp\proj，
	// 用 filepath.FromSlash 表达"按宿主平台的写法"，断言仍然有效）
	if want := filepath.FromSlash("/tmp/proj"); srv.rootPath != want {
		t.Errorf("rootPath = %q，期望 %q", srv.rootPath, want)
	}
}

// TestPercentEncodingSurvivesInitialize 是 P7-A-2 的端到端回归。
//
// 旧实现的 PathOfURI 只替换 %20，其它编码（%23、%3A）原样留在路径里。
// 这里用 `%23`（`#`，VSCode 会编码）验证整条链路：
// initialize → rootPath → 传给解析器。
func TestPercentEncodingSurvivesInitialize(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":` +
		`{"processId":null,"rootUri":"file:///tmp/a%23b/proj","capabilities":{}}}`)
	resp := h.readMessage(3 * time.Second)
	if resp["error"] != nil {
		t.Fatalf("initialize 返回错误: %v", resp["error"])
	}

	if want := filepath.FromSlash("/tmp/a#b/proj"); h.srv.rootPath != want {
		t.Errorf("rootPath = %q，期望 %q（百分号未完整解码）", h.srv.rootPath, want)
	}
	if strings.Contains(h.srv.rootPath, "%23") {
		t.Errorf("rootPath 残留编码：%q", h.srv.rootPath)
	}
}

// TestToPathDecodesAllEncodings 覆盖 fileuri 在本包依赖的通用性质。
func TestToPathDecodesAllEncodings(t *testing.T) {
	cases := []struct{ uri, want string }{
		{"file:///tmp/a%20b", "/tmp/a b"},
		{"file:///tmp/a%23b", "/tmp/a#b"},
		{"file:///tmp/%E4%B8%AD", "/tmp/中"},
	}
	for _, c := range cases {
		got, err := fileuri.ToPath(c.uri)
		if err != nil {
			t.Errorf("ToPath(%q) 报错: %v", c.uri, err)
			continue
		}
		// 期望值按宿主平台表达：Windows 上同一 URI 解成 \tmp\a b。
		// 校验的是"百分号解码正确"，不是"在 Windows 上也返回 POSIX 路径"。
		if want := filepath.FromSlash(c.want); got != want {
			t.Errorf("ToPath(%q) = %q，期望 %q", c.uri, got, want)
		}
	}
}
