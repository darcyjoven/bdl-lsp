package server

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
	"github.com/darcyjoven/bdl-lsp/link"
	"go.lsp.dev/protocol"
)

// 链接资料 Code Lens（PLAN Z-P3-3）的测试。
//
// 分两层：
//   1. buildLinkLenses 是纯函数 → 直接单测（数量/顺序/纯文本 lens/上限汇总/自我排除）；
//   2. 协议层：真起服务、开一个含 MAIN 的文件、请求 textDocument/codeLens；
//      以及 bdl.openLink 是否真的向客户端发了 window/showDocument 反向请求。

func sampleTree() *link.LinkTree {
	return &link.LinkTree{
		Found:   true,
		Program: "APM100",
		Main:    "apm100",
		Files: []link.TreeFile{
			{Module: "APM", Code: "apm100", Path: "/p/apm100.4gl", Main: true},
			{Module: "APM", Code: "helper_chain", Path: "/p/helper_chain.4gl"},
			{Module: "SUB", Code: "gone", Path: ""}, // 链接里有、磁盘上没有
		},
	}
}

func TestBuildLinkLensesBasic(t *testing.T) {
	mainRange := protocol.Range{
		Start: protocol.Position{Line: 3, Character: 0},
		End:   protocol.Position{Line: 3, Character: 4},
	}
	lenses := buildLinkLenses(mainRange, sampleTree(), 10, fileuri.FromPath("/p/apm100.4gl"))

	// 链内 3 个文件，其中"自己"（apm100）不挂 → 2 条
	if len(lenses) != 2 {
		t.Fatalf("lens 数 = %d，期望 2：%+v", len(lenses), lenses)
	}
	for _, lens := range lenses {
		if lens.Range != mainRange {
			t.Errorf("lens 没挂在 MAIN 的 range 上：%+v", lens.Range)
		}
	}

	if lenses[0].Command == nil || lenses[0].Command.Command != CommandOpenLink {
		t.Fatalf("第一条 lens 应带 %s 命令：%+v", CommandOpenLink, lenses[0])
	}
	if got := lenses[0].Command.Title; got != "APM/helper_chain" {
		t.Errorf("标题 = %q，期望 APM/helper_chain", got)
	}
	args, _ := lenses[0].Command.Arguments[0].(string)
	if !strings.HasSuffix(args, "helper_chain.4gl") {
		t.Errorf("命令参数应是目标文件 URI，实际 %q", args)
	}

	// 磁盘上没有的文件 → 纯文本 lens（没有 command，点了不会误导）
	if lenses[1].Command != nil {
		t.Errorf("文件不在磁盘上时不该带 command：%+v", lenses[1].Command)
	}
}

func TestBuildLinkLensesLimitAndSummary(t *testing.T) {
	tree := sampleTree()
	tree.Files = append(tree.Files,
		link.TreeFile{Module: "APM", Code: "extra1", Path: "/p/extra1.4gl"},
		link.TreeFile{Module: "APM", Code: "extra2", Path: "/p/extra2.4gl"},
	)
	// max=2 → 前 2 条 + 1 条纯文本汇总
	lenses := buildLinkLenses(protocol.Range{}, tree, 2, fileuri.FromPath("/p/apm100.4gl"))
	if len(lenses) != 3 {
		t.Fatalf("lens 数 = %d，期望 3（2 条 + 汇总）：%+v", len(lenses), lenses)
	}
	if lenses[2].Command != nil {
		t.Errorf("汇总 lens 应是纯文本：%+v", lenses[2].Command)
	}
	// max<=0 按默认 10 处理
	if got := len(buildLinkLenses(protocol.Range{}, tree, 0, "")); got != 5 {
		t.Errorf("max<=0 时应按默认 10 显示全部 5 条（自己也算进来，因为 current 为空），实际 %d", got)
	}
}

func TestBuildLinkLensesEmptyWhenNotLinked(t *testing.T) {
	if got := buildLinkLenses(protocol.Range{}, &link.LinkTree{}, 10, ""); len(got) != 0 {
		t.Errorf("没有链接资料时应返回空：%+v", got)
	}
	if got := buildLinkLenses(protocol.Range{}, nil, 10, ""); len(got) != 0 {
		t.Errorf("tree 为 nil 时应返回空：%+v", got)
	}
}

// TestCodeLensProtocol 真起服务，验证 textDocument/codeLens 的端到端数据路径。
func TestCodeLensProtocol(t *testing.T) {
	f := newLinkFixture(t)
	f.write(t, "apm", "apm100", "MAIN\n    CALL helper_chain()\nEND MAIN\n")
	f.write(t, "apm", "helper_chain", "FUNCTION helper_chain()\n    DISPLAY 1\nEND FUNCTION\n")
	f.writeLinks(t,
		`{"schema":1,"program":"APM100","module":"APM","main":"apm100","files":[["APM","apm100"],["APM","helper_chain"]]}`,
	)
	h := f.initialize(t)
	uri := f.uri("apm", "apm100")
	h.openBDL(uri, "MAIN\n    CALL helper_chain()\nEND MAIN\n")

	resp := h.request(50, "textDocument/codeLens",
		fmt.Sprintf(`{"textDocument":{"uri":%q}}`, uri))
	if resp["error"] != nil {
		t.Fatalf("codeLens 失败：%v", resp["error"])
	}
	raw, _ := resp["result"].([]any)
	if len(raw) != 1 {
		t.Fatalf("应有 1 条 lens（apm100 自己不计），实际 %d：%v", len(raw), raw)
	}
	lens, _ := raw[0].(map[string]any)
	rng, _ := lens["range"].(map[string]any)
	start, _ := rng["start"].(map[string]any)
	if line, _ := start["line"].(float64); line != 0 {
		t.Errorf("lens 应挂在 MAIN 那一行（line 0），实际 %v", line)
	}
	cmd, _ := lens["command"].(map[string]any)
	if id, _ := cmd["command"].(string); id != CommandOpenLink {
		t.Errorf("lens command = %v，期望 %s", cmd["command"], CommandOpenLink)
	}
	args, _ := cmd["arguments"].([]any)
	if len(args) != 1 {
		t.Fatalf("lens command 参数 = %v", args)
	}
	if target, _ := args[0].(string); !strings.HasSuffix(target, "helper_chain.4gl") {
		t.Errorf("lens 指向 %q，期望 helper_chain.4gl", target)
	}
}

// TestCodeLensAdvertised 确认 capability 已声明（Zed 依赖它才发请求）。
func TestCodeLensAdvertised(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	resp := h.request(1, "initialize", `{"processId":null,"rootUri":null,"capabilities":{}}`)
	result, _ := resp["result"].(map[string]any)
	caps, _ := result["capabilities"].(map[string]any)
	if _, ok := caps["codeLensProvider"]; !ok {
		t.Fatalf("initialize 未声明 codeLensProvider：%v", caps)
	}
}

// TestExecuteCommandOpenLinkSendsShowDocument 验证 bdl.openLink 真的发出
// window/showDocument 反向请求（Zed 是否响应由实机确认，PLAN Z-P2-1）。
func TestExecuteCommandOpenLinkSendsShowDocument(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const target = "file:///tmp/link-target.4gl"
	h.send(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":60,"method":"workspace/executeCommand","params":{"command":%q,"arguments":[%q]}}`,
		CommandOpenLink, target))

	deadline := time.Now().Add(5 * time.Second)
	sawShowDocument := false
	for time.Now().Before(deadline) {
		msg := h.readMessage(time.Until(deadline))
		if msg == nil {
			break
		}
		if msg["method"] == MethodShowDocument {
			sawShowDocument = true
			params, _ := msg["params"].(map[string]any)
			if got, _ := params["uri"].(string); got != target {
				t.Errorf("showDocument 的 uri = %q，期望 %q", got, target)
			}
			// 作为客户端回复：成功打开
			h.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%v,"result":{"success":true}}`, msg["id"]))
			continue
		}
		if msg["id"] == float64(60) {
			if respErr := msg["error"]; respErr != nil {
				t.Fatalf("executeCommand 失败：%v", respErr)
			}
			opened, _ := msg["result"].(map[string]any)
			if got, _ := opened["opened"].(string); got != target {
				t.Errorf("结果 = %v，期望 opened=%q", msg["result"], target)
			}
			if !sawShowDocument {
				t.Errorf("没有收到 window/showDocument 反向请求")
			}
			return
		}
	}
	t.Fatal("等待 window/showDocument / executeCommand 响应超时")
}
