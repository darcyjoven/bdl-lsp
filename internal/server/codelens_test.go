package server

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	"github.com/darcyjoven/bdl-lsp/internal/wsindex"
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

	// 磁盘上没有的文件 → 不能跳转，但仍要带 command（Zed 只显示 command.title，
	// 没有 command 会显示成假的「0 references」）。
	if lenses[1].Command == nil {
		t.Fatalf("文件不在磁盘上时也应带 command：%+v", lenses[1])
	}
	if lenses[1].Command.Command != CommandShowInfo {
		t.Errorf("命令应是 %s，实际 %s", CommandShowInfo, lenses[1].Command.Command)
	}
	if !strings.Contains(lenses[1].Command.Title, "gone") {
		t.Errorf("标题应含代码名：%q", lenses[1].Command.Title)
	}
}

func TestBuildLinkLensesLimitAndSummary(t *testing.T) {
	tree := sampleTree()
	tree.Files = append(tree.Files,
		link.TreeFile{Module: "APM", Code: "extra1", Path: "/p/extra1.4gl"},
		link.TreeFile{Module: "APM", Code: "extra2", Path: "/p/extra2.4gl"},
	)
	// max=2 → 前 2 条 + 1 条汇总
	lenses := buildLinkLenses(protocol.Range{}, tree, 2, fileuri.FromPath("/p/apm100.4gl"))
	if len(lenses) != 3 {
		t.Fatalf("lens 数 = %d，期望 3（2 条 + 汇总）：%+v", len(lenses), lenses)
	}
	// 汇总必须带 command（Zed 只显示 command.title）
	if lenses[2].Command == nil {
		t.Fatalf("汇总 lens 应带 command：%+v", lenses[2])
	}
	if lenses[2].Command.Command != CommandShowInfo {
		t.Errorf("汇总命令应是 %s，实际 %s", CommandShowInfo, lenses[2].Command.Command)
	}
	if !strings.Contains(lenses[2].Command.Title, "另有 2 个链内文件") {
		t.Errorf("汇总标题应说明还有多少：%q", lenses[2].Command.Title)
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

// ---------- 函数级反查（「被 N 处调用」） ----------

func TestBuildCallerLensesCountsAndFirstCaller(t *testing.T) {
	idx := textpos.New([]byte("FUNCTION helper()\n  DISPLAY \"x\"\nEND FUNCTION\n"))
	sym := &index.Symbol{
		Name:      "helper",
		Lower:     "helper",
		Kind:      index.KindFunction,
		DeclStart: 0,
		DeclEnd:   8,
	}
	fi := &index.FileIndex{TopLevel: []*index.Symbol{sym}}
	ix := &wsindex.Index{Files: []wsindex.File{{
		Path: "/root/def.4gl",
		Calls: []wsindex.Call{
			{Name: "helper", Lower: "helper", File: "/root/z.4gl", Line: 1},
			{Name: "helper", Lower: "helper", File: "/root/caller.4gl", Line: 3},
		},
	}}}

	lenses := buildCallerLenses(fi, idx, ix, nil)
	if len(lenses) != 1 {
		t.Fatalf("lens 数 = %d，期望 1：%+v", len(lenses), lenses)
	}
	cmd := lenses[0].Command
	if cmd == nil || cmd.Command != CommandOpenLink {
		t.Fatalf("应带 %s 命令：%+v", CommandOpenLink, lenses[0])
	}
	if !strings.Contains(cmd.Title, "被 2 处调用") {
		t.Errorf("标题应含调用数：%q", cmd.Title)
	}
	// 排序后首处应是 caller.4gl（不是 z.4gl）
	if !strings.Contains(cmd.Title, "caller.4gl") {
		t.Errorf("标题应指向排序后的首处 caller.4gl：%q", cmd.Title)
	}
	arg, _ := cmd.Arguments[0].(string)
	if arg != fileuri.FromPath("/root/caller.4gl") {
		t.Errorf("命令参数 = %q，期望首处调用文件", arg)
	}
	if lenses[0].Range.Start.Line != 0 {
		t.Errorf("lens 应挂在函数定义那一行：%+v", lenses[0].Range)
	}
}

func TestBuildCallerLensesSkipsUncalledAndNeedsIndex(t *testing.T) {
	idx := textpos.New([]byte("FUNCTION never()\nEND FUNCTION\n"))
	fi := &index.FileIndex{TopLevel: []*index.Symbol{{
		Name: "never", Lower: "never", Kind: index.KindFunction, DeclStart: 0, DeclEnd: 5,
	}}}
	empty := &wsindex.Index{}
	if got := buildCallerLenses(fi, idx, empty, nil); len(got) != 0 {
		t.Errorf("没有调用点就不该出 lens：%+v", got)
	}
	if got := buildCallerLenses(fi, idx, nil, nil); len(got) != 0 {
		t.Errorf("索引没就绪时不该出 lens：%+v", got)
	}
}

// TestBuildCallerLensesFiltersUnreachableModules 验证口径与「查找引用」一致：
// 不可达模组里的同名调用**不计入**（否则数字虚高 —— 4GL 里同名函数跨模组很常见）。
func TestBuildCallerLensesFiltersUnreachableModules(t *testing.T) {
	idx := textpos.New([]byte("FUNCTION helper()\nEND FUNCTION\n"))
	fi := &index.FileIndex{TopLevel: []*index.Symbol{{
		Name: "helper", Lower: "helper", Kind: index.KindFunction, DeclStart: 0, DeclEnd: 8,
	}}}
	ix := &wsindex.Index{Files: []wsindex.File{
		{Path: "/root/tiptop/aap/4gl/caller.4gl", Calls: []wsindex.Call{
			{Name: "helper", Lower: "helper", File: "/root/tiptop/aap/4gl/caller.4gl", Line: 1},
		}},
		{Path: "/root/tiptop/zzz/4gl/other.4gl", Calls: []wsindex.Call{
			{Name: "helper", Lower: "helper", File: "/root/tiptop/zzz/4gl/other.4gl", Line: 1},
		}},
	}}

	// 只允许 aap 模组 → zzz 的那处不该算进去
	lenses := buildCallerLenses(fi, idx, ix, map[string]bool{"aap": true})
	if len(lenses) != 1 {
		t.Fatalf("lens 数 = %d，期望 1：%+v", len(lenses), lenses)
	}
	if !strings.Contains(lenses[0].Command.Title, "被 1 处调用") {
		t.Errorf("不可达模组的调用不该计入：%q", lenses[0].Command.Title)
	}

	// 允许两个模组 → 2 处
	lenses = buildCallerLenses(fi, idx, ix, map[string]bool{"aap": true, "zzz": true})
	if !strings.Contains(lenses[0].Command.Title, "被 2 处调用") {
		t.Errorf("两个可达模组应计 2 处：%q", lenses[0].Command.Title)
	}
}

func TestBuildCallerLensesLimitAndSummary(t *testing.T) {
	const n = 12
	var sb strings.Builder
	var top []*index.Symbol
	ix := &wsindex.Index{}
	for i := 0; i < n; i++ {
		name := fmt.Sprintf("fn%02d", i)
		start := uint(sb.Len())
		sb.WriteString("FUNCTION " + name + "()\n")
		end := uint(sb.Len())
		top = append(top, &index.Symbol{
			Name: name, Lower: name, Kind: index.KindFunction, DeclStart: start, DeclEnd: end,
		})
		ix.Files = append(ix.Files, wsindex.File{
			Path:  fmt.Sprintf("/root/c%02d.4gl", i),
			Calls: []wsindex.Call{{Name: name, Lower: name, File: fmt.Sprintf("/root/c%02d.4gl", i), Line: 2}},
		})
	}
	idx := textpos.New([]byte(sb.String()))
	fi := &index.FileIndex{TopLevel: top}

	lenses := buildCallerLenses(fi, idx, ix, nil)
	if len(lenses) != callerLensLimit+1 {
		t.Fatalf("lens 数 = %d，期望 %d（上限 + 1 条汇总）", len(lenses), callerLensLimit+1)
	}
	// 汇总必须带 command（Zed 只显示 command.title；没有 command 会显示成假的「0 references」）
	last := lenses[len(lenses)-1]
	if last.Command == nil {
		t.Fatalf("汇总 lens 应带 command：%+v", last)
	}
	if last.Command.Command != CommandShowInfo {
		t.Errorf("汇总命令应是 %s，实际 %s", CommandShowInfo, last.Command.Command)
	}
	if !strings.Contains(last.Command.Title, "另有 2 个") {
		t.Errorf("汇总标题应说明还有多少：%q", last.Command.Title)
	}
}

func TestShortPathKeepsLastTwoSegments(t *testing.T) {
	if got := shortPath("/a/b/caller.4gl"); got != "b/caller.4gl" {
		t.Errorf("shortPath = %q", got)
	}
	if got := shortPath(`C:\proj\aap\4gl\x.4gl`); got != "4gl/x.4gl" {
		t.Errorf("Windows 路径 shortPath = %q", got)
	}
}

// TestCodeLensShowsCallerCount 端到端：打开被调用的文件，应看到「被 2 处调用」。
func TestCodeLensShowsCallerCount(t *testing.T) {
	root, files := newWorkspace(t)
	h, cleanup := startServer(t)
	defer cleanup()
	h.initWS(root)
	h.waitIndex()

	const rel = "tiptop/aap/4gl/aapr200.4gl"
	uri := fileURIOf(filepath.Join(root, filepath.FromSlash(rel)))
	h.openBDL(uri, files[rel])

	resp := h.request(70, "textDocument/codeLens",
		fmt.Sprintf(`{"textDocument":{"uri":%q}}`, uri))
	if resp["error"] != nil {
		t.Fatalf("codeLens 失败：%v", resp["error"])
	}
	raw, _ := resp["result"].([]any)
	for _, item := range raw {
		lens, _ := item.(map[string]any)
		cmd, _ := lens["command"].(map[string]any)
		if cmd == nil {
			continue
		}
		title, _ := cmd["title"].(string)
		if !strings.Contains(title, "被 2 处调用") {
			continue
		}
		if id, _ := cmd["command"].(string); id != CommandOpenLink {
			t.Errorf("command = %q，期望 %s", id, CommandOpenLink)
		}
		args, _ := cmd["arguments"].([]any)
		arg, _ := args[0].(string)
		if !strings.HasSuffix(arg, "aapr100.4gl") {
			t.Errorf("首处调用应是 aapr100.4gl（按文件排序），实际 %q", arg)
		}
		return
	}
	t.Fatalf("没有找到『被 2 处调用』lens：%v", raw)
}

// TestEveryLensHasCommand 锁定一条硬约束：**每条 lens 都必须带 command**。
//
// 原因（查 zed-industries/zed main 分支源码证实）：Zed 只显示
// `lens.command.title`；没有 command 的 lens 会先渲染占位，等
// `codeLens/resolve` 回来补标题，服务端不支持 resolve 时就永远占位，
// 最终回落到一个**假的**「0 references」文本
// （crates/editor/src/code_lens.rs 的 EMPTY_LENS_FALLBACK_TITLE）。
//
// 也就是说：以前"纯文本 lens"在 Zed 里要么显示成假的「0 references」，
// 要么什么都不显示 —— 这条测试防止以后又写回"无 command 的汇总行"。
func TestEveryLensHasCommand(t *testing.T) {
	tree := sampleTree()
	tree.Files = append(tree.Files,
		link.TreeFile{Module: "APM", Code: "extra1", Path: "/p/extra1.4gl"},
		link.TreeFile{Module: "APM", Code: "extra2", Path: "/p/extra2.4gl"},
	)
	// 链接 lens：含"文件不在磁盘"与"超出上限的汇总"两种边界
	for _, lens := range buildLinkLenses(protocol.Range{}, tree, 2, fileuri.FromPath("/p/apm100.4gl")) {
		if lens.Command == nil {
			t.Fatalf("链接 lens 缺 command（Zed 会显示成假的「0 references」）：%+v", lens)
		}
		if lens.Command.Title == "" {
			t.Errorf("链接 lens 的标题为空：%+v", lens.Command)
		}
		if !slices.Contains(executeCommandIDs(), lens.Command.Command) {
			t.Errorf("命令 %q 不在 executeCommandProvider.commands 里，Zed 会静默跳过", lens.Command.Command)
		}
	}

	// 反查 lens：含"超出上限的汇总"
	var sb strings.Builder
	var top []*index.Symbol
	ix := &wsindex.Index{}
	for i := 0; i < callerLensLimit+2; i++ {
		name := fmt.Sprintf("fn%02d", i)
		start := uint(sb.Len())
		sb.WriteString("FUNCTION " + name + "()\n")
		end := uint(sb.Len())
		top = append(top, &index.Symbol{
			Name: name, Lower: name, Kind: index.KindFunction, DeclStart: start, DeclEnd: end,
		})
		ix.Files = append(ix.Files, wsindex.File{
			Path:  fmt.Sprintf("/root/c%02d.4gl", i),
			Calls: []wsindex.Call{{Name: name, Lower: name, File: fmt.Sprintf("/root/c%02d.4gl", i), Line: 2}},
		})
	}
	idx := textpos.New([]byte(sb.String()))
	fi := &index.FileIndex{TopLevel: top}
	for _, lens := range buildCallerLenses(fi, idx, ix, nil) {
		if lens.Command == nil {
			t.Fatalf("反查 lens 缺 command（Zed 会显示成假的「0 references」）：%+v", lens)
		}
		if lens.Command.Title == "" {
			t.Errorf("反查 lens 的标题为空：%+v", lens.Command)
		}
		if !slices.Contains(executeCommandIDs(), lens.Command.Command) {
			t.Errorf("命令 %q 不在 executeCommandProvider.commands 里，Zed 会静默跳过", lens.Command.Command)
		}
	}
}
