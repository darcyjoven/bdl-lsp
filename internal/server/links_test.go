package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// 链接资料（P7-D）的接入测试。
//
// 覆盖 PLAN-P7 §5.8 的验收项：
//  1. 打开含 MAIN 的文件，链接文件里的函数不再报未定义；
//  2. **R2 反例**：链接在一起的两个子文件各自 DEFINE 同名变量 → 变量仍报未定义；
//  3. Q14 扁平规则：lib 调 sub、sub 调 lib、任意文件调 qry 都不报；
//     三者都没有的函数 → 报；
//  4. 缺失文件（链接里有、磁盘上没有）→ 零诊断；
//  5. 没有链接资料 → function-not-found 整项不启用。

// linkFixture 建一个最小项目树：tiptop/{apm,lib,sub}/4gl + .link/links.jsonl。
type linkFixture struct {
	root string
	dirs map[string]string // "apm" -> <root>/tiptop/apm/4gl
}

func newLinkFixture(t *testing.T) *linkFixture {
	t.Helper()
	root := t.TempDir()
	f := &linkFixture{root: root, dirs: map[string]string{}}
	for _, m := range []string{"apm", "lib", "sub", "qry"} {
		dir := filepath.Join(root, "tiptop", m, "4gl")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f.dirs[m] = dir
	}
	return f
}

// write 写一个 4gl 文件（模块名小写，模拟真实语料）。
func (f *linkFixture) write(t *testing.T, module, code, src string) string {
	t.Helper()
	path := filepath.Join(f.dirs[module], code+".4gl")
	if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// uri 返回文件的 file:// URI。
func (f *linkFixture) uri(module, code string) string {
	return "file://" + filepath.Join(f.dirs[module], code+".4gl")
}

// writeLinks 写 .link/links.jsonl（每行一条链）。
func (f *linkFixture) writeLinks(t *testing.T, lines ...string) {
	t.Helper()
	dir := filepath.Join(f.root, ".link")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := strings.Join(lines, "\n") + "\n"
	if err := os.WriteFile(filepath.Join(dir, "links.jsonl"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// initialize 用 fixture 根目录初始化服务器（链接资料在 initialize 时加载）。
func (f *linkFixture) initialize(t *testing.T) *harness {
	t.Helper()
	h, cleanup := startServer(t)
	t.Cleanup(cleanup)
	h.send(fmt.Sprintf(
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":%q,"capabilities":{}}}`,
		"file://"+f.root))
	h.readMessage(3 * time.Second)
	return h
}

// drainUntilCode 一直读到出现指定诊断码（或超时），返回期间收到的全部诊断。
//
// 为什么不能只读第一帧：链接索引是**后台预热**的，首轮诊断可能因为
// "可见集不完整"而跳过 function-not-found，就绪后会补发一轮。
// 这正是真实使用路径，测试必须覆盖到。
func drainUntilCode(t *testing.T, h *harness, uri, code string, timeout time.Duration) []any {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var all []any
	for time.Now().Before(deadline) {
		msg := h.readMessage(time.Until(deadline))
		if msg == nil || msg["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		p, _ := msg["params"].(map[string]any)
		if p["uri"] != uri {
			continue
		}
		diags, _ := p["diagnostics"].([]any)
		all = append(all, diags...)
		for _, raw := range diags {
			d, _ := raw.(map[string]any)
			if c, _ := d["code"].(string); c == code {
				return all
			}
		}
	}
	return all
}

// codes 收集诊断码。
func codes(diags []any) []string {
	out := make([]string, 0, len(diags))
	for _, raw := range diags {
		d, _ := raw.(map[string]any)
		if c, _ := d["code"].(string); c != "" {
			out = append(out, c)
		}
	}
	return out
}

// countOf 数某个码出现的次数。
func countOf(diags []any, code string) int {
	n := 0
	for _, raw := range diags {
		d, _ := raw.(map[string]any)
		if c, _ := d["code"].(string); c == code {
			n++
		}
	}
	return n
}

// messagesOf 收集某个码的诊断消息。
func messagesOf(diags []any, code string) []string {
	var out []string
	for _, raw := range diags {
		d, _ := raw.(map[string]any)
		if c, _ := d["code"].(string); c != code {
			continue
		}
		if msg, _ := d["message"].(string); msg != "" {
			out = append(out, msg)
		}
	}
	return out
}

const linkMainSrc = `MAIN
    CALL helper_in_chain()
    CALL cl_null(1)
    CALL s_errmsg("x")
    CALL qry_lookup(1)
    CALL not_anywhere()
END MAIN
`

// TestLinkFunctionNotFoundWithData 是 P7-D 的主验收用例。
func TestLinkFunctionNotFoundWithData(t *testing.T) {
	f := newLinkFixture(t)
	f.write(t, "apm", "axmt620", linkMainSrc)
	f.write(t, "apm", "helper_chain", "FUNCTION helper_in_chain()\n    DISPLAY 1\nEND FUNCTION\n")
	f.write(t, "lib", "cl_null", "FUNCTION cl_null(v)\n    DISPLAY v\nEND FUNCTION\n")
	f.write(t, "sub", "s_errmsg", "FUNCTION s_errmsg(m)\n    DISPLAY m\nEND FUNCTION\n")
	f.write(t, "qry", "qry_lookup", "FUNCTION qry_lookup(k)\n    DISPLAY k\nEND FUNCTION\n")
	f.writeLinks(t,
		`{"schema":1,"program":"axmt620","module":"APM","main":"axmt620","files":[["APM","axmt620"],["APM","helper_chain"]]}`,
	)

	h := f.initialize(t)
	h.sendDiagnosticsConfig(t, map[string]string{"function-not-found": "warning"})

	uri := f.uri("apm", "axmt620")
	h.sendDidOpen(uri, "bdl", linkMainSrc, 1)
	diags := drainUntilCode(t, h, uri, "function-not-found", 20*time.Second)

	msgs := messagesOf(diags, "function-not-found")
	// 只有 not_anywhere 该报。
	if len(msgs) != 1 {
		t.Fatalf("期望只报 not_anywhere，实际 %d 条：%v（全部诊断码 %v）", len(msgs), msgs, codes(diags))
	}
	if !strings.Contains(msgs[0], "not_anywhere") {
		t.Errorf("应报 not_anywhere，实际 %q", msgs[0])
	}
	joined := strings.Join(msgs, "\n")
	for _, name := range []string{"helper_in_chain", "cl_null", "s_errmsg", "qry_lookup"} {
		if strings.Contains(joined, name) {
			t.Errorf("%s 应可见（链接文件或全局模组），不该报", name)
		}
	}
}

// TestLinkVisibilityIsFlatAcrossGlobalModules 验证 Q14 扁平规则（含"它们自己"）。
func TestLinkVisibilityIsFlatAcrossGlobalModules(t *testing.T) {
	f := newLinkFixture(t)
	// lib 调 sub（历史冲突点，实测 11 处），sub 调 lib，qry 调 lib。
	f.write(t, "lib", "lib_caller", "FUNCTION lib_caller()\n    CALL s_errmsg(\"x\")\nEND FUNCTION\n")
	f.write(t, "sub", "s_errmsg", "FUNCTION s_errmsg(m)\n    DISPLAY m\nEND FUNCTION\n")
	f.write(t, "sub", "sub_caller", "FUNCTION sub_caller()\n    CALL cl_null(1)\nEND FUNCTION\n")
	f.write(t, "lib", "cl_null", "FUNCTION cl_null(v)\n    DISPLAY v\nEND FUNCTION\n")
	f.write(t, "qry", "qry_caller", "FUNCTION qry_caller()\n    CALL cl_null(1)\nEND FUNCTION\n")
	// 一个含 MAIN 的程序，链接资料里只有它自己。
	// 正对照：这个文件调了一个真的不存在的函数，必须被报出来。
	// 否则"没报"可能只是因为索引没就绪、检查根本没跑。
	f.write(t, "apm", "entry", "MAIN\n    CALL ghost_control()\nEND MAIN\n")
	f.writeLinks(t,
		`{"schema":1,"program":"entry","module":"APM","main":"entry","files":[["APM","entry"]]}`,
	)

	h := f.initialize(t)
	h.sendDiagnosticsConfig(t, map[string]string{"function-not-found": "warning"})

	for _, c := range []struct{ module, code string }{
		{"lib", "lib_caller"}, {"sub", "sub_caller"}, {"qry", "qry_caller"},
	} {
		uri := f.uri(c.module, c.code)
		src := readFileString(t, f.dirs[c.module], c.code)
		h.sendDidOpen(uri, "bdl", src, 1)
		// 这些文件不含 MAIN，也不在任何链里 → 只有全局模组可见。
		// 用软收集：期望零条，不能让"等超时"变成测试失败。
		diags := h.collectDiagnostics(uri, 1500*time.Millisecond)
		if n := countOf(diags, "function-not-found"); n != 0 {
			t.Errorf("%s/%s 调全局模组函数不应报，实际 %d 条：%v",
				c.module, c.code, n, messagesOf(diags, "function-not-found"))
		}
	}

	// 正对照必须命中，证明上面的"零条"不是因为检查没启用。
	entryURI := f.uri("apm", "entry")
	h.sendDidOpen(entryURI, "bdl", readFileString(t, f.dirs["apm"], "entry"), 1)
	diags := drainUntilCode(t, h, entryURI, "function-not-found", 20*time.Second)
	msgs := messagesOf(diags, "function-not-found")
	if len(msgs) != 1 || !strings.Contains(msgs[0], "ghost_control") {
		t.Fatalf("正对照应报 ghost_control，实际 %v", msgs)
	}
}

// TestLinkDoesNotMakeVariablesVisible 是 R2 的反例测试。
//
// 用户明确强调：**链接只放大函数可见性，不放大变量**。
// 两个链接在一起的子文件各自 DEFINE 同名变量、没有共享 GLOBALS，
// 那么对"另一个文件里的变量"仍然必须报未定义。
func TestLinkDoesNotMakeVariablesVisible(t *testing.T) {
	f := newLinkFixture(t)
	f.write(t, "apm", "entry", `MAIN
    CALL sub_a()
    DISPLAY shared_var
END MAIN
`)
	f.write(t, "apm", "sub_a", "FUNCTION sub_a()\n    DEFINE shared_var INTEGER\n    LET shared_var = 1\n    DISPLAY shared_var\nEND FUNCTION\n")
	f.writeLinks(t,
		`{"schema":1,"program":"entry","module":"APM","main":"entry","files":[["APM","entry"],["APM","sub_a"]]}`,
	)

	h := f.initialize(t)
	uri := f.uri("apm", "entry")
	h.sendDidOpen(uri, "bdl", readFileString(t, f.dirs["apm"], "entry"), 1)
	diags := drainUntilCode(t, h, uri, "variable-source-unresolved", 10*time.Second)

	msgs := messagesOf(diags, "variable-source-unresolved")
	if len(msgs) != 1 || !strings.Contains(msgs[0], "shared_var") {
		t.Fatalf("entry 里的 shared_var 未在本文件定义，应报未定义（链接不能放大变量），实际：%v", msgs)
	}
	// sub_a 自己 DEFINE 过，自己用不该报。
	subURI := f.uri("apm", "sub_a")
	h.sendDidOpen(subURI, "bdl", readFileString(t, f.dirs["apm"], "sub_a"), 1)
	subDiags := h.drainDiagnostics(subURI, 10*time.Second)
	if n := countOf(subDiags, "variable-source-unresolved"); n != 0 {
		t.Errorf("sub_a 自己定义的变量不该报，实际 %d 条", n)
	}
}

// TestLinkMissingFilesAreSilent 验证"链接里有、磁盘上没有"完全静默。
func TestLinkMissingFilesAreSilent(t *testing.T) {
	f := newLinkFixture(t)
	f.write(t, "apm", "entry", "MAIN\n    CALL wpc_ghost()\nEND MAIN\n")
	// 第二条文件在磁盘上不存在（模拟语料里整批缺失的 W* 模组）。
	f.writeLinks(t,
		`{"schema":1,"program":"entry","module":"APM","main":"entry","files":[["APM","entry"],["WPC","wpc_ghost"]]}`,
	)

	h := f.initialize(t)
	h.sendDiagnosticsConfig(t, map[string]string{"function-not-found": "warning"})
	uri := f.uri("apm", "entry")
	h.sendDidOpen(uri, "bdl", "MAIN\n    CALL wpc_ghost()\nEND MAIN\n", 1)

	// wpc_ghost 的函数体不可知（文件不存在）→ 链不完整 → 不能断定它不存在。
	diags := h.collectDiagnostics(uri, 2*time.Second)
	if n := countOf(diags, "function-not-found"); n != 0 {
		t.Errorf("缺失文件里的函数不该报未定义，实际 %d 条：%v",
			n, messagesOf(diags, "function-not-found"))
	}
	// 也不该有任何"文件缺失"类诊断（用户明确要求）。
	for _, c := range codes(diags) {
		if strings.Contains(c, "missing") && strings.Contains(c, "link") {
			t.Errorf("不该有链接缺失诊断，实际 %q", c)
		}
	}
}

// TestNoLinkDataDisablesFunctionNotFound 验证没有资料时整项不启用。
func TestNoLinkDataDisablesFunctionNotFound(t *testing.T) {
	f := newLinkFixture(t)
	f.write(t, "apm", "entry", "MAIN\n    CALL ghost()\nEND MAIN\n")
	// 不写 .link

	h := f.initialize(t)
	h.sendDiagnosticsConfig(t, map[string]string{"function-not-found": "warning"})
	uri := f.uri("apm", "entry")
	h.sendDidOpen(uri, "bdl", "MAIN\n    CALL ghost()\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if n := countOf(diags, "function-not-found"); n != 0 {
		t.Errorf("没有链接资料时不该报 function-not-found，实际 %d 条", n)
	}
}

// TestLinkAmbiguousIndexesAllMains 验证多 MAIN 组：每个 MAIN 都能查到整条链。
func TestLinkAmbiguousIndexesAllMains(t *testing.T) {
	f := newLinkFixture(t)
	f.write(t, "apm", "main_a", "MAIN\n    CALL shared_fn()\n    CALL ghost_ctrl()\nEND MAIN\n")
	f.write(t, "apm", "main_b", "MAIN\n    CALL shared_fn()\nEND MAIN\n")
	f.write(t, "apm", "shared", "FUNCTION shared_fn()\n    DISPLAY 1\nEND FUNCTION\n")
	f.writeLinks(t,
		`{"schema":1,"program":"grp","module":"APM","main":"main_a","mains":["main_b"],"ambiguous":true,"files":[["APM","main_a"],["APM","main_b"],["APM","shared"]]}`,
	)

	h := f.initialize(t)
	h.sendDiagnosticsConfig(t, map[string]string{"function-not-found": "warning"})

	// main_a：链里看不到 shared_fn 就会报 → 必须不报；ghost_ctrl 必须报。
	uriA := f.uri("apm", "main_a")
	h.sendDidOpen(uriA, "bdl", readFileString(t, f.dirs["apm"], "main_a"), 1)
	diagsA := drainUntilCode(t, h, uriA, "function-not-found", 20*time.Second)
	msgsA := messagesOf(diagsA, "function-not-found")
	if len(msgsA) != 1 || !strings.Contains(msgsA[0], "ghost_ctrl") {
		t.Fatalf("main_a 只应报 ghost_ctrl（shared_fn 在链里可见），实际 %v", msgsA)
	}
	// main_b 是第二个 MAIN，同样要能查到整条链。
	uriB := f.uri("apm", "main_b")
	h.sendDidOpen(uriB, "bdl", readFileString(t, f.dirs["apm"], "main_b"), 1)
	diagsB := h.collectDiagnostics(uriB, 2*time.Second)
	if n := countOf(diagsB, "function-not-found"); n != 0 {
		t.Errorf("main_b 应能通过链看到 shared_fn，实际 %d 条：%v",
			n, messagesOf(diagsB, "function-not-found"))
	}
}

// TestLinkInvalidDataReportsDiagnostic 验证坏资料会报 link-data-invalid。
func TestLinkInvalidDataReportsDiagnostic(t *testing.T) {
	f := newLinkFixture(t)
	f.write(t, "apm", "probe", "MAIN\n    DISPLAY 1\nEND MAIN\n")
	dir := filepath.Join(f.root, ".link")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "links.jsonl"), []byte("{这不是 JSON}\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	h := f.initialize(t)
	// 资料自身的问题在首个 didOpen 时发布（避免和 initialize 响应抢帧）。
	h.sendDidOpen(f.uri("apm", "probe"), "bdl", "MAIN\n    DISPLAY 1\nEND MAIN\n", 1)
	target := "file://" + filepath.Join(dir, "links.jsonl")
	diags := drainUntilCode(t, h, target, "link-data-invalid", 10*time.Second)
	if n := countOf(diags, "link-data-invalid"); n == 0 {
		t.Fatalf("坏资料应报 link-data-invalid，实际诊断码 %v", codes(diags))
	}
}

// TestLinkStaleDataReportsHint 验证过期资料报 hint（不是错误）。
func TestLinkStaleDataReportsHint(t *testing.T) {
	f := newLinkFixture(t)
	f.write(t, "apm", "entry", "MAIN\n    DISPLAY 1\nEND MAIN\n")
	f.writeLinks(t,
		`{"schema":1,"program":"entry","module":"APM","main":"entry","files":[["APM","entry"]]}`,
	)
	dir := filepath.Join(f.root, ".link")
	meta := `{"schema":1,"generatedAt":"2020-01-01T00:00:00+08:00","rowsTotal":1,"rowsY":1,"chains":1}`
	if err := os.WriteFile(filepath.Join(dir, "links.meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}

	h := f.initialize(t)
	h.sendDidOpen(f.uri("apm", "entry"), "bdl", readFileString(t, f.dirs["apm"], "entry"), 1)
	target := "file://" + filepath.Join(dir, "links.meta.json")
	diags := drainUntilCode(t, h, target, "link-data-stale", 10*time.Second)
	if n := countOf(diags, "link-data-stale"); n == 0 {
		t.Fatalf("过期资料应报 link-data-stale，实际诊断码 %v", codes(diags))
	}
	for _, raw := range diags {
		d, _ := raw.(map[string]any)
		if c, _ := d["code"].(string); c != "link-data-stale" {
			continue
		}
		if sev, _ := d["severity"].(float64); sev != 4 {
			t.Errorf("过期提示应为 hint(4)，实际 %v", d["severity"])
		}
	}
}

// readFileString 读 fixture 里的文件内容。
func readFileString(t *testing.T, dir, code string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, code+".4gl"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}
