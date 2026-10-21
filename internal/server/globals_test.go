package server

// P3a：GLOBALS 模组全局变量的协议级测试。
//
// 覆盖规划 §6 T3 的验收：
//   - 缺文件出现 warning（code=globals-file-not-found，range 在字面量上）
//   - 文件补上后诊断消失（增量同步）
//   - 配置改 off 后不出现（热更新）
//   - .global 文件打开触发引用方诊断刷新（反向依赖）
//
// 测试都在临时目录构造文件树；rootUri 指向该目录。

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setupGlobalsTree 构造一个引用方文档与（可选的）目标 global 文件。
//
//	dir/
//	  main.4gl        ← GLOBALS "top.global"
//	  top.global      ← exists 控制是否创建
func setupGlobalsTree(t *testing.T, exists bool) (dir string, mainURI, globalURI string) {
	t.Helper()
	dir = t.TempDir()

	mainSrc := "GLOBALS \"top.global\"\nMAIN\n    DISPLAY \"x\"\nEND MAIN\n"
	if err := os.WriteFile(filepath.Join(dir, "main.4gl"), []byte(mainSrc), 0o644); err != nil {
		t.Fatal(err)
	}
	if exists {
		g := "GLOBALS\n    DEFINE g_counter INTEGER\nEND GLOBALS\n"
		if err := os.WriteFile(filepath.Join(dir, "top.global"), []byte(g), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, "file://" + filepath.Join(dir, "main.4gl"), "file://" + filepath.Join(dir, "top.global")
}

// initializeGlobals 启动服务器并以 rootUri=dir 完成 initialize。
func initializeGlobals(t *testing.T, dir string) *harness {
	t.Helper()
	h, cleanup := startServer(t)
	t.Cleanup(cleanup)

	rootURI := "file://" + dir
	h.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":%q,"capabilities":{}}}`, rootURI))
	h.readMessage(3 * time.Second)
	return h
}

func findGlobalsDiags(diags []any) []map[string]any {
	var out []map[string]any
	for _, d := range diags {
		m, _ := d.(map[string]any)
		if m == nil {
			continue
		}
		if m["code"] == "globals-file-not-found" {
			out = append(out, m)
		}
	}
	return out
}

func TestGlobalsFileNotFoundDiagnostic(t *testing.T) {
	dir, mainURI, _ := setupGlobalsTree(t, false)
	h := initializeGlobals(t, dir)

	h.sendDidOpen(mainURI, "bdl", "GLOBALS \"top.global\"\nMAIN\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	g := findGlobalsDiags(diags)
	if len(g) != 1 {
		t.Fatalf("期望 1 条 globals-file-not-found，实际 %d 条（全部诊断 %d 条）", len(g), len(diags))
	}
	d := g[0]
	if d["severity"] != float64(protocolSeverityWarning) {
		t.Errorf("severity 期望 warning(2)，实际 %v", d["severity"])
	}
	if msg, _ := d["message"].(string); msg == "" {
		t.Error("message 为空")
	}
	// range 应落在 GLOBALS "top.global" 行（第 0 行）的字面量上
	r, _ := d["range"].(map[string]any)
	start, _ := r["start"].(map[string]any)
	if start["line"] != float64(0) {
		t.Errorf("诊断应指向第 0 行，实际 %v", start["line"])
	}
	// 字面量（含引号）从第 8 列开始
	if start["character"] != float64(8) {
		t.Errorf("诊断应从字面量起点（列 8）开始，实际 %v", start["character"])
	}
}

func TestGlobalsFileFoundNoDiagnostic(t *testing.T) {
	dir, mainURI, _ := setupGlobalsTree(t, true)
	h := initializeGlobals(t, dir)

	h.sendDidOpen(mainURI, "bdl", "GLOBALS \"top.global\"\nMAIN\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if g := findGlobalsDiags(diags); len(g) != 0 {
		t.Errorf("目标存在时不应有 globals 诊断，实际 %d 条", len(g))
	}
}

func TestGlobalsNotFoundHealsWhenFileCreated(t *testing.T) {
	dir, mainURI, _ := setupGlobalsTree(t, false)
	h := initializeGlobals(t, dir)

	h.sendDidOpen(mainURI, "bdl", "GLOBALS \"top.global\"\nMAIN\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)
	if g := findGlobalsDiags(diags); len(g) != 1 {
		t.Fatalf("初始应 1 条警告，实际 %d", len(g))
	}

	// 补上文件后：一次 didChange 触发重发，警告应消失
	g := "GLOBALS\n    DEFINE g_counter INTEGER\nEND GLOBALS\n"
	if err := os.WriteFile(filepath.Join(dir, "top.global"), []byte(g), 0o644); err != nil {
		t.Fatal(err)
	}
	h.sendDidChange(mainURI, 2, `[{"text":"GLOBALS \"top.global\"\nMAIN\nEND MAIN\n"}]`)

	diags = h.drainDiagnostics(mainURI, 5*time.Second)
	if g2 := findGlobalsDiags(diags); len(g2) != 0 {
		t.Errorf("文件补上后警告应消失，实际 %d 条", len(g2))
	}
}

func TestGlobalsNotFoundDisabledByConfig(t *testing.T) {
	dir, mainURI, _ := setupGlobalsTree(t, false)
	h := initializeGlobals(t, dir)

	// 热更新：off 后重新诊断
	cfg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params": map[string]any{
			"settings": map[string]any{
				"bdl": map[string]any{
					"globals": map[string]any{
						"fileNotFoundDiagnostics": "off",
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(cfg)
	h.send(string(raw))

	h.sendDidOpen(mainURI, "bdl", "GLOBALS \"top.global\"\nMAIN\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if g := findGlobalsDiags(diags); len(g) != 0 {
		t.Errorf("配置 off 后不应出现 globals 诊断，实际 %d 条", len(g))
	}
}

func TestGlobalsOpenTargetRefreshesImporters(t *testing.T) {
	dir, mainURI, globalURI := setupGlobalsTree(t, false)
	h := initializeGlobals(t, dir)

	// 1. 打开引用方：警告出现
	h.sendDidOpen(mainURI, "bdl", "GLOBALS \"top.global\"\nMAIN\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)
	if g := findGlobalsDiags(diags); len(g) != 1 {
		t.Fatalf("初始应 1 条警告，实际 %d", len(g))
	}

	// 2. 磁盘上补上 global 文件（内容带一个变量，供后续跳转测试用）
	g := "GLOBALS\n    DEFINE g_counter INTEGER\nEND GLOBALS\n"
	if err := os.WriteFile(filepath.Join(dir, "top.global"), []byte(g), 0o644); err != nil {
		t.Fatal(err)
	}

	// 3. 打开 global 文件本身 → refreshImporters 应重发引用方诊断
	h.sendDidOpen(globalURI, "bdl", g, 1)
	// 先等 global 自己的诊断（无警告，但会 publish 一条）
	_ = h.drainDiagnostics(globalURI, 5*time.Second)
	// 再等引用方的刷新：警告应消失
	diags = h.drainDiagnostics(mainURI, 5*time.Second)
	if g2 := findGlobalsDiags(diags); len(g2) != 0 {
		t.Errorf("global 文件打开后引用方警告应消失，实际 %d 条", len(g2))
	}
}

func TestGlobalsImportInPERIgnored(t *testing.T) {
	// .per 没有 GLOBALS 语法：即使文件内容意外匹配也不产生 globals 诊断
	dir := t.TempDir()
	h := initializeGlobals(t, dir)

	perURI := "file://" + filepath.Join(dir, "form.per")
	h.sendDidOpen(perURI, "per", "SCHEMA ds\nLAYOUT\nGRID\n{\n}\nEND\nEND\n", 1)
	diags := h.drainDiagnostics(perURI, 5*time.Second)

	if g := findGlobalsDiags(diags); len(g) != 0 {
		t.Errorf(".per 不应产生 globals 诊断，实际 %d 条", len(g))
	}
}

// protocolSeverityWarning 是 protocol.DiagnosticSeverityWarning 的数值
// （JSON 里序列化为数字）。
const protocolSeverityWarning = 2

// --- P3a T4：跳转与悬停 ---

// setupGlobalsNav 构造跳转/悬停测试树：
//
//	dir/
//	  top.global   ← 块内定义 g_counter（第 2 行）与 g_rec
//	  main.4gl     ← GLOBALS "top.global" + MAIN 里使用 g_counter
func setupGlobalsNav(t *testing.T) (dir, mainURI, globalURI string) {
	t.Helper()
	dir = t.TempDir()

	g := "GLOBALS\n    DEFINE g_counter INTEGER\n    DEFINE g_rec RECORD\n        id INTEGER\n    END RECORD\nEND GLOBALS\n"
	if err := os.WriteFile(filepath.Join(dir, "top.global"), []byte(g), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir,
		"file://" + filepath.Join(dir, "main.4gl"),
		"file://" + filepath.Join(dir, "top.global")
}

const mainUsesGlobals = "GLOBALS \"top.global\"\nMAIN\n    DISPLAY g_counter\n    LET g_rec.id = 1\nEND MAIN\n"

func TestGlobalsDefinitionJumpsToGlobalFile(t *testing.T) {
	dir, mainURI, globalURI := setupGlobalsNav(t)
	h := initializeGlobals(t, dir)

	h.sendDidOpen(mainURI, "bdl", mainUsesGlobals, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	// 光标在 DISPLAY g_counter 的 g_counter 上（第 2 行，列 12）
	locs := h.requestDefinition(t, mainURI, 2, 12)
	if len(locs) == 0 {
		t.Fatal("GLOBALS 引入的变量应能跳转")
	}
	// 跳转目标必须在 .global 文件里
	if locs[0]["uri"] != globalURI {
		t.Errorf("跳转 URI 期望 %s，实际 %v", globalURI, locs[0]["uri"])
	}
	// g_counter 定义在 top.global 第 2 行（1 基）→ LSP 0 基第 1 行
	r, _ := locs[0]["range"].(map[string]any)
	start, _ := r["start"].(map[string]any)
	if start["line"] != float64(1) {
		t.Errorf("定义行期望 1（0 基），实际 %v", start["line"])
	}
}

func TestGlobalsRecordFieldJumpsThroughGlobals(t *testing.T) {
	dir, mainURI, _ := setupGlobalsNav(t)
	h := initializeGlobals(t, dir)

	h.sendDidOpen(mainURI, "bdl", mainUsesGlobals, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	// g_rec.id：rec 本身来自 GLOBALS，字段解析要穿过导入
	locs := h.requestDefinition(t, mainURI, 3, 13)
	if len(locs) == 0 {
		t.Fatal("rec.field 形式（rec 来自 GLOBALS）应能跳转")
	}
}

func TestGlobalsHoverShowsSource(t *testing.T) {
	dir, mainURI, _ := setupGlobalsNav(t)
	h := initializeGlobals(t, dir)

	h.sendDidOpen(mainURI, "bdl", mainUsesGlobals, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	hov := h.requestHover(t, mainURI, 2, 12)
	if hov == nil {
		t.Fatal("悬停 GLOBALS 引入的变量应有内容")
	}
	contents, _ := hov["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	if value == "" {
		t.Fatal("悬停内容为空")
	}
	if !stringsContains(value, "来源") {
		t.Errorf("悬停应包含来源标注，实际：%q", value)
	}
	if !stringsContains(value, "top.global") {
		t.Errorf("悬停应指出来源文件，实际：%q", value)
	}
}

func TestGlobalsHoverInDefiningFileAnnotated(t *testing.T) {
	dir, _, globalURI := setupGlobalsNav(t)
	h := initializeGlobals(t, dir)

	g := "GLOBALS\n    DEFINE g_counter INTEGER\nEND GLOBALS\n"
	h.sendDidOpen(globalURI, "bdl", g, 1)
	h.drainDiagnostics(globalURI, 5*time.Second)

	// 在定义方文件里悬停 g_counter：标注“模组全局变量”
	hov := h.requestHover(t, globalURI, 1, 12)
	if hov == nil {
		t.Fatal("悬停应有内容")
	}
	contents, _ := hov["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	if !stringsContains(value, "模组全局变量") {
		t.Errorf("定义方悬停应标注模组全局变量，实际：%q", value)
	}
}

func TestGlobalsDefinitionScopeOrder(t *testing.T) {
	// 局部变量遮蔽 globals 变量：跳转应命中局部，不命中 globals
	dir, mainURI, _ := setupGlobalsNav(t)
	h := initializeGlobals(t, dir)

	src := "GLOBALS \"top.global\"\nMAIN\n    DEFINE g_counter STRING\n    DISPLAY g_counter\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	locs := h.requestDefinition(t, mainURI, 3, 12)
	if len(locs) == 0 {
		t.Fatal("应能跳转")
	}
	// 命中的是本文件里的局部定义（URI 是 main.4gl）
	if locs[0]["uri"] != mainURI {
		t.Errorf("局部遮蔽时应跳本文件定义，实际 %v", locs[0]["uri"])
	}
}

// requestDefinition 发 definition 请求并返回 Location 数组（map 形式）。
func (h *harness) requestDefinition(t *testing.T, uri string, line, char int) []map[string]any {
	t.Helper()
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"textDocument/definition","params":{"textDocument":{"uri":%q},"position":{"line":%d,"character":%d}}}`, nextRequestID(), uri, line, char)
	h.send(payload)
	msg := h.readMessage(5 * time.Second)
	res, _ := msg["result"].([]any)
	out := make([]map[string]any, 0, len(res))
	for _, r := range res {
		m, _ := r.(map[string]any)
		if m != nil {
			out = append(out, m)
		}
	}
	return out
}

// requestHover 发 hover 请求并返回结果（nil 表示无结果）。
func (h *harness) requestHover(t *testing.T, uri string, line, char int) map[string]any {
	t.Helper()
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":"textDocument/hover","params":{"textDocument":{"uri":%q},"position":{"line":%d,"character":%d}}}`, nextRequestID(), uri, line, char)
	h.send(payload)
	msg := h.readMessage(5 * time.Second)
	res, _ := msg["result"].(map[string]any)
	return res
}

// nextRequestID 生成递增请求 id（避免与其他测试的 id 冲突）。
var requestIDCounter int

func nextRequestID() int {
	requestIDCounter++
	return 900 + requestIDCounter
}

// stringsContains 是 strings.Contains 的薄封装（测试可读性）。
func stringsContains(s, sub string) bool {
	return len(sub) > 0 && len(s) >= len(sub) && contains(s, sub)
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// --- P3a T5：变量来源未找到 ---

func findUnresolvedDiags(diags []any) []map[string]any {
	var out []map[string]any
	for _, d := range diags {
		m, _ := d.(map[string]any)
		if m == nil {
			continue
		}
		if m["code"] == "variable-source-unresolved" {
			out = append(out, m)
		}
	}
	return out
}

func TestUnresolvedSourceWarnsForUnknownVariable(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	// g_undefined 没有任何定义（无导入、无 DEFINE）
	h.sendDidOpen(mainURI, "bdl", "MAIN\n    DISPLAY g_undefined\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	u := findUnresolvedDiags(diags)
	if len(u) != 1 {
		t.Fatalf("期望 1 条 variable-source-unresolved，实际 %d（全部 %d 条）", len(u), len(diags))
	}
	// range 应指向 g_undefined（第 1 行）
	r, _ := u[0]["range"].(map[string]any)
	start, _ := r["start"].(map[string]any)
	if start["line"] != float64(1) {
		t.Errorf("诊断应指向第 1 行，实际 %v", start["line"])
	}
	if msg, _ := u[0]["message"].(string); !stringsContains(msg, "g_undefined") {
		t.Errorf("消息应含变量名：%q", msg)
	}
}

func TestUnresolvedSourceSilentWhenDefined(t *testing.T) {
	// DEFINE 过的变量、globals 引入的变量都不告警
	dir, mainURI, _ := setupGlobalsNav(t)
	h := initializeGlobals(t, dir)

	src := "GLOBALS \"top.global\"\nMAIN\n    DEFINE l_local INTEGER\n    DISPLAY g_counter\n    DISPLAY l_local\n    LET g_rec.id = 1\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if u := findUnresolvedDiags(diags); len(u) != 0 {
		for _, d := range u {
			t.Errorf("已定义变量不应告警：%v", d["message"])
		}
	}
}

func TestUnresolvedSourceSkipsCallAndField(t *testing.T) {
	// 函数调用名、rec.field 字段侧不检查（规划 §5.3 保守规则）
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	src := "MAIN\n    DEFINE r RECORD\n        id INTEGER\n    END RECORD\n    CALL cl_null(r.id)\n    LET r.id = 1\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	u := findUnresolvedDiags(diags)
	for _, d := range u {
		msg, _ := d["message"].(string)
		// cl_null 是函数名（call.name）：不查
		if stringsContains(msg, "cl_null") {
			t.Errorf("函数调用名不应检查：%q", msg)
		}
	}
}

func TestUnresolvedSourceMentionsMissingGlobals(t *testing.T) {
	// 情形 B：存在未解析导入时，措辞指向缺失的 GLOBALS
	dir, mainURI, _ := setupGlobalsTree(t, false)
	h := initializeGlobals(t, dir)

	src := "GLOBALS \"top.global\"\nMAIN\n    DISPLAY g_counter\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	u := findUnresolvedDiags(diags)
	if len(u) != 1 {
		t.Fatalf("期望 1 条（g_counter 无法核对），实际 %d", len(u))
	}
	if msg, _ := u[0]["message"].(string); !stringsContains(msg, "top.global") {
		t.Errorf("情形 B 措辞应指向缺失的 GLOBALS 文件：%q", msg)
	}
}

func TestUnresolvedSourceSilentWithUnresolvableImport(t *testing.T) {
	// 存在变量形式的导入（静态无法核对）→ 全部放过
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	src := "GLOBALS g_file_var\nMAIN\n    DISPLAY g_counter\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if u := findUnresolvedDiags(diags); len(u) != 0 {
		t.Errorf("Unresolvable 导入存在时应全部放过，实际 %d 条", len(u))
	}
}

func TestUnresolvedSourceDisabledByConfig(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	cfg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params": map[string]any{
			"settings": map[string]any{
				"bdl": map[string]any{
					"globals": map[string]any{
						"unresolvedSourceDiagnostics": "off",
					},
				},
			},
		},
	}
	raw, _ := json.Marshal(cfg)
	h.send(string(raw))

	h.sendDidOpen(mainURI, "bdl", "MAIN\n    DISPLAY g_undefined\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if u := findUnresolvedDiags(diags); len(u) != 0 {
		t.Errorf("配置 off 后不应出现，实际 %d 条", len(u))
	}
}

// --- P7-B 诊断降噪：query 覆盖面回归 ---
//
// 下面每条都是语料里实测踩到的误报，改动 queryVariables 时不能回退。

// sendDiagnosticsConfig 下发 bdl.diagnostics 逐码配置。
func (h *harness) sendDiagnosticsConfig(t *testing.T, diagnostics map[string]string) {
	t.Helper()
	cfg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params": map[string]any{
			"settings": map[string]any{
				"bdl": map[string]any{"diagnostics": diagnostics},
			},
		},
	}
	raw, _ := json.Marshal(cfg)
	h.send(string(raw))
	time.Sleep(120 * time.Millisecond)
}

func TestUnresolvedSkipsFunctionNameInWhenever(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	// WHENEVER ERROR CALL f：f 是函数名；GOTO lbl：lbl 是标号。
	// 语法树里都是 (whenever (variable (identifier)))，不是 call 节点。
	src := "MAIN\n    WHENEVER ERROR CALL cl_err_msg_log\n    WHENEVER ERROR GOTO lbl\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if u := findUnresolvedDiags(diags); len(u) != 0 {
		t.Errorf("WHENEVER 的函数名/标号不应报未定义，实际 %d 条", len(u))
	}
}

func TestUnresolvedSkipsFunctionNameInExpression(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	// IF fn() THEN 的 fn 是 (expression name: (variable ...)) 而非 call 节点。
	src := "MAIN\n    IF fn_check() THEN\n        DISPLAY 1\n    END IF\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if u := findUnresolvedDiags(diags); len(u) != 0 {
		t.Errorf("表达式里的函数名不应报未定义，实际 %d 条", len(u))
	}
}

func TestUnresolvedSkipsFormFieldWithDot(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	// DISPLAY ... TO FORMONLY.cn2：field 是
	// (variable (object object: (variable ...) property: ...))，直接子节点
	// 不是 identifier——query 必须捕获外层 variable 才能跳过。
	src := "MAIN\n    DEFINE r RECORD\n        cn2 VARCHAR(10)\n    END RECORD\n" +
		"    DISPLAY r.cn2 TO FORMONLY.cn2\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if u := findUnresolvedDiags(diags); len(u) != 0 {
		t.Errorf("表单字段 FORMONLY.cn2 不应报未定义，实际 %d 条", len(u))
	}
}

// --- P7-B：逐码配置 ---

func TestDiagnosticsPerCodeDisablesSingleCode(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	// 先确认默认会报，再用 bdl.diagnostics 单独关掉它。
	h.sendDiagnosticsConfig(t, map[string]string{"variable-source-unresolved": "off"})
	h.sendDidOpen(mainURI, "bdl", "MAIN\n    DISPLAY g_undefined\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if u := findUnresolvedDiags(diags); len(u) != 0 {
		t.Errorf("逐码 off 后不应出现，实际 %d 条", len(u))
	}
}

func TestDiagnosticsPerCodeOverridesLegacyKey(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	// 新键 error 应压过旧键 warning。
	cfg := map[string]any{
		"jsonrpc": "2.0",
		"method":  "workspace/didChangeConfiguration",
		"params": map[string]any{
			"settings": map[string]any{
				"bdl": map[string]any{
					"globals":     map[string]any{"unresolvedSourceDiagnostics": "warning"},
					"diagnostics": map[string]any{"variable-source-unresolved": "error"},
				},
			},
		},
	}
	raw, _ := json.Marshal(cfg)
	h.send(string(raw))
	time.Sleep(120 * time.Millisecond)

	h.sendDidOpen(mainURI, "bdl", "MAIN\n    DISPLAY g_undefined\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	u := findUnresolvedDiags(diags)
	if len(u) != 1 {
		t.Fatalf("期望 1 条，实际 %d 条", len(u))
	}
	// LSP severity：1=Error 2=Warning
	if sev, _ := u[0]["severity"].(float64); sev != 1 {
		t.Errorf("severity = %v，期望 1（error）", u[0]["severity"])
	}
}

// findFunctionNotFoundDiags 过滤出 function-not-found 诊断。
func findFunctionNotFoundDiags(diags []any) []map[string]any {
	var out []map[string]any
	for _, d := range diags {
		m, _ := d.(map[string]any)
		if m == nil {
			continue
		}
		if m["code"] == "function-not-found" {
			out = append(out, m)
		}
	}
	return out
}

func TestFunctionNotFoundOffByDefault(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	// 未定义的函数调用：默认 off（实测 24 个真实文件报 3660 条，
	// 内置函数与跨模块函数还没有符号表，不能默认开）。
	h.sendDidOpen(mainURI, "bdl", "MAIN\n    CALL no_such_fn()\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if f := findFunctionNotFoundDiags(diags); len(f) != 0 {
		t.Errorf("默认应关闭，实际 %d 条", len(f))
	}
}

// TestFunctionNotFoundNeedsLinkData 验证 P7-D 的前置条件：
// 即使显式打开，**没有链接资料时也不报**。
//
// 理由：没有链接资料就不知道"这个程序链接了哪些文件"，可见集不完整，
// 报告就是误报（实测 24 个文件 3660 条）。宁可漏报不可误报。
// 真正报出 function-not-found 的用例在 links_test.go（那里有 .link 数据）。
func TestFunctionNotFoundNeedsLinkData(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	h.sendDiagnosticsConfig(t, map[string]string{"function-not-found": "warning"})
	h.sendDidOpen(mainURI, "bdl", "MAIN\n    CALL no_such_fn()\nEND MAIN\n", 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if f := findFunctionNotFoundDiags(diags); len(f) != 0 {
		t.Fatalf("没有链接资料时不应报 function-not-found，实际 %d 条", len(f))
	}
}

func TestFunctionNotFoundAcceptsLocalDefinition(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	mainURI := "file://" + filepath.Join(dir, "main.4gl")

	h.sendDiagnosticsConfig(t, map[string]string{"function-not-found": "warning"})
	src := "MAIN\n    CALL helper()\nEND MAIN\n\nFUNCTION helper()\n    DISPLAY 1\nEND FUNCTION\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	if f := findFunctionNotFoundDiags(diags); len(f) != 0 {
		t.Errorf("本模块内定义的函数不应报，实际 %d 条", len(f))
	}
}
