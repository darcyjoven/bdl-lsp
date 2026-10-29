package server

// workspace/executeCommand 通道（PLAN Z-P3-2）的协议级测试。
//
// 这条通道是给 Zed 扩展用的：Zed 的扩展 API 发不了 LSP 自定义请求，
// 只能通过标准 executeCommand 触发服务端能力。VSCode 侧继续用自定义请求，
// 所以这里除了验证"命令能跑通"，还要保证返回结构与自定义请求一致。

import (
	"fmt"
	"strings"
	"testing"
)

// findCommand 在 executeCommandProvider.commands 里找命令名。
func hasCommand(result map[string]any, want string) bool {
	caps, _ := result["capabilities"].(map[string]any)
	provider, _ := caps["executeCommandProvider"].(map[string]any)
	raw, _ := provider["commands"].([]any)
	for _, item := range raw {
		if s, _ := item.(string); s == want {
			return true
		}
	}
	return false
}

func TestInitializeAdvertisesExecuteCommands(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	resp := h.request(1, "initialize", `{"processId":null,"rootUri":null,"capabilities":{}}`)
	if resp["error"] != nil {
		t.Fatalf("initialize 失败：%v", resp["error"])
	}
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatalf("initialize 没有 result：%v", resp)
	}
	for _, want := range executeCommandIDs() {
		if !hasCommand(result, want) {
			t.Errorf("initialize 未声明 executeCommandProvider 命令 %q（result=%v）", want, result["capabilities"])
		}
	}
}

// TestExecuteCommandLinkTree 用与 bdl/linkTree 相同的数据验证 executeCommand 转发。
func TestExecuteCommandLinkTree(t *testing.T) {
	f := newLinkFixture(t)
	f.write(t, "apm", "axmt620", linkMainSrc)
	f.write(t, "apm", "helper_chain", "FUNCTION helper_in_chain()\n    DISPLAY 1\nEND FUNCTION\n")
	f.writeLinks(t,
		`{"schema":1,"program":"axmt620","module":"APM","main":"axmt620","files":[["APM","axmt620"],["APM","helper_chain"]]}`,
	)
	h := f.initialize(t)
	uri := f.uri("apm", "axmt620")

	resp := h.request(10, "workspace/executeCommand",
		fmt.Sprintf(`{"command":%q,"arguments":[%q]}`, CommandLinkTree, uri))
	if resp["error"] != nil {
		t.Fatalf("executeCommand 失败：%v", resp["error"])
	}
	result, _ := resp["result"].(map[string]any)
	if found, _ := result["found"].(bool); !found {
		t.Fatalf("linkTree found=false：%v", result)
	}
	if program, _ := result["program"].(string); program != "axmt620" {
		t.Errorf("program = %q，期望 axmt620", program)
	}
	files, _ := result["files"].([]any)
	if len(files) != 2 {
		t.Errorf("链内文件 = %d，期望 2：%v", len(files), files)
	}

	// 反查命令：返回结构与 bdl/linkReferences 一致（refs / refTotal）。
	resp = h.request(11, "workspace/executeCommand",
		fmt.Sprintf(`{"command":%q,"arguments":[%q]}`, CommandLinkReferences, uri))
	if resp["error"] != nil {
		t.Fatalf("linkReferences 失败：%v", resp["error"])
	}
	refsResult, _ := resp["result"].(map[string]any)
	if _, ok := refsResult["refTotal"]; !ok {
		t.Errorf("linkReferences 结果缺少 refTotal：%v", refsResult)
	}
}

// TestExecuteCommandSchema 验证 bdl.schemaTables / bdl.schemaColumns 的转发。
func TestExecuteCommandSchema(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	resp := h.request(20, "workspace/executeCommand",
		`{"command":"bdl.schemaTables","arguments":["ima"]}`)
	if resp["error"] != nil {
		t.Fatalf("schemaTables 失败：%v", resp["error"])
	}
	result, _ := resp["result"].(map[string]any)
	if present, _ := result["present"].(bool); !present {
		t.Fatalf("schemaTables present=false：%v", result)
	}
	tables, _ := result["tables"].([]any)
	if len(tables) != 1 {
		t.Fatalf("按 ima 过滤后应只剩 1 张表，实际 %d：%v", len(tables), tables)
	}
	first, _ := tables[0].(map[string]any)
	// 注意：索引的 map key 是表名大写，但 Entry.Name 保留资料里的原值（这里是 ima_file）。
	if name, _ := first["name"].(string); strings.ToLower(name) != "ima_file" {
		t.Errorf("表名 = %q，期望 ima_file（大小写不敏感）", name)
	}

	resp = h.request(21, "workspace/executeCommand",
		`{"command":"bdl.schemaColumns","arguments":["IMA_FILE"]}`)
	if resp["error"] != nil {
		t.Fatalf("schemaColumns 失败：%v", resp["error"])
	}
	colsResult, _ := resp["result"].(map[string]any)
	columns, _ := colsResult["columns"].([]any)
	if len(columns) != 2 {
		t.Errorf("IMA_FILE 应有 2 列，实际 %d：%v", len(columns), columns)
	}
}

// TestExecuteCommandForceParse 验证强制解析命令走的是同一个后台入口。
func TestExecuteCommandForceParse(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/force.4gl"
	h.openBDL(uri, "MAIN\nEND MAIN\n")

	resp := h.request(30, "workspace/executeCommand",
		fmt.Sprintf(`{"command":%q,"arguments":[%q]}`, CommandForceParse, uri))
	if resp["error"] != nil {
		t.Fatalf("forceParse 失败：%v", resp["error"])
	}
	if _, ok := resp["result"]; !ok {
		t.Errorf("forceParse 应有 result（null）：%v", resp)
	}
}

// TestExecuteCommandErrors 覆盖参数/命令错误（客户端据此显示可操作提示）。
func TestExecuteCommandErrors(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	cases := []struct {
		name   string
		params string
	}{
		{"未知命令", `{"command":"bdl.nope","arguments":[]}`},
		{"缺少参数", `{"command":"bdl.linkTree","arguments":[]}`},
		{"参数类型错", `{"command":"bdl.schemaColumns","arguments":[42]}`},
		{"文件未打开", `{"command":"bdl.forceParse","arguments":["file:///tmp/never-opened.4gl"]}`},
	}
	for i, c := range cases {
		resp := h.request(40+i, "workspace/executeCommand", c.params)
		if resp["error"] == nil {
			t.Errorf("%s：应有 error，实际 %v", c.name, resp)
		}
	}
}
