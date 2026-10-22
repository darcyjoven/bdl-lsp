package builtin

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestEmbeddedLoads(t *testing.T) {
	if err := EmbeddedErr(); err != nil {
		t.Fatalf("内置数据解析失败: %v", err)
	}
	set := Embedded()
	if set.Len() < 50 {
		t.Fatalf("内置表只有 %d 条，明显不完整", set.Len())
	}
	if set.GeneroVersion() != "2.50" {
		t.Errorf("GeneroVersion = %q，期望 2.50", set.GeneroVersion())
	}
	// 语料实测的高频名字必须收录（P7-C 的降噪目标全靠它们）：
	// 前 12 名占全量诊断的 98%，少一个就少降一截。
	for _, name := range []string{
		"SQLCA", "INT_FLAG", "STATUS", "PAGENO", "LINENO", "DIALOG",
		"UI", "BASE", "XML", "COM", "OS", "OM",
		"ARG_VAL", "INFIELD", "LENGTH", "SUM", "COUNT", "FGL_GETENV",
	} {
		if _, ok := set.Lookup(name); !ok {
			t.Errorf("内置表缺少语料高频名 %q", name)
		}
	}
}

func TestEmbeddedValidates(t *testing.T) {
	data, err := EmbeddedData()
	if err != nil {
		t.Fatal(err)
	}
	if problems := Validate(data); len(problems) > 0 {
		t.Fatalf("内置数据文件有问题:\n  %s", strings.Join(problems, "\n  "))
	}
}

func TestLookupCaseInsensitive(t *testing.T) {
	set := Embedded()
	for _, name := range []string{"sqlca", "SQLCA", "SqlCa"} {
		if _, ok := set.Lookup(name); !ok {
			t.Errorf("Lookup(%q) 应命中（大小写不敏感）", name)
		}
	}
	if _, ok := set.Lookup("cl_null"); ok {
		// 这是 lib/4gl/cl_null.4gl 里的用户函数，绝不能进内置表
		// （PLAN-P7 §4.3 明确警告过：会掩盖真实的链接缺失）。
		t.Error("cl_null 是用户函数，不应出现在内置表里")
	}
}

func TestIsFunctionOnlyFunctionKind(t *testing.T) {
	set := Embedded()
	if !set.IsFunction("ARG_VAL") {
		t.Error("ARG_VAL 应被识别为内置函数")
	}
	// 类名不是函数：`ui.Interface.x` 的 `ui` 是包前缀，
	// 把它当函数会让 function-not-found 失去意义。
	if set.IsFunction("ui") {
		t.Error("ui 是内置类，IsFunction 应为 false")
	}
	if !set.IsKnown("ui") {
		t.Error("ui 应被 IsKnown 命中（变量位置要豁免）")
	}
}

func TestPrefix(t *testing.T) {
	set := Embedded()
	got := set.Prefix("fgl_get", 0)
	if len(got) == 0 {
		t.Fatal("Prefix(fgl_get) 应有结果")
	}
	for _, s := range got {
		if !strings.HasPrefix(strings.ToLower(s.Name), "fgl_get") {
			t.Errorf("%q 不满足前缀", s.Name)
		}
	}
	if n := len(set.Prefix("fgl_get", 2)); n != 2 {
		t.Errorf("limit=2 应截断，实际 %d", n)
	}
	if got := set.Prefix("zzzz", 0); len(got) != 0 {
		t.Errorf("无匹配时应为空，实际 %d", len(got))
	}
	// 空前缀 = 全部（补全手工触发时用）。
	if got := len(set.Prefix("", 0)); got != set.Len() {
		t.Errorf("空前缀应返回全部 %d 条，实际 %d", set.Len(), got)
	}
}

func TestMergeFieldOverlay(t *testing.T) {
	set := NewSet()
	set.add(Symbol{Name: "STATUS", Kind: KindVariable, Doc: "旧说明"})
	// 只改 Doc 时不该丢 Kind/Signature。
	set.add(Symbol{Name: "status", Kind: KindVariable, Doc: "新说明"})
	sym, ok := set.Lookup("STATUS")
	if !ok {
		t.Fatal("应命中")
	}
	if sym.Doc != "新说明" {
		t.Errorf("Doc 应按非空字段覆盖，实际 %q", sym.Doc)
	}
	if sym.Name != "STATUS" {
		t.Errorf("名字大小写应保留首次出现的形态，实际 %q", sym.Name)
	}
	if sym.Kind != KindVariable {
		t.Errorf("Kind 应保持，实际 %q", sym.Kind)
	}
}

func TestMergeKindOverride(t *testing.T) {
	set := NewSet()
	set.add(Symbol{Name: "X", Kind: KindVariable})
	set.add(Symbol{Name: "X", Kind: KindFunction})
	sym, _ := set.Lookup("X")
	// Kind 是必填字段，以新条目为准（否则用户无法纠正分类）。
	if sym.Kind != KindFunction {
		t.Errorf("Kind 应被新条目覆盖，实际 %q", sym.Kind)
	}
}

func TestDisableWinsOverAdd(t *testing.T) {
	set := NewSet()
	set.add(Symbol{Name: "SUM", Kind: KindFunction})
	set.disabled["sum"] = true
	// finish 之前也不该能查到？——finish 负责删除，这里只验语义契约。
	done := set.finish()
	if _, ok := done.Lookup("SUM"); ok {
		t.Error("disable 应从表里移除")
	}
	if !done.Disabled("sum") {
		t.Error("Disabled(大小写不敏感) 应为 true")
	}
}

func TestParseRejectsBadJSON(t *testing.T) {
	_, err := parse([]byte("{not json"), Source{Path: "x.json"})
	if err == nil {
		t.Fatal("坏 JSON 应报错")
	}
}

func TestParseHigherSchemaKeepsFields(t *testing.T) {
	src := []byte(`{"schemaVersion":99,"generoVersion":"9.99","symbols":[{"name":"ZZZ","kind":"variable"}]}`)
	set, err := parse(src, Source{Path: "x.json"})
	if err == nil {
		t.Error("更高 schemaVersion 应给出提示（err 非 nil）")
	}
	if _, ok := set.Lookup("ZZZ"); !ok {
		t.Error("更高 schemaVersion 仍应尽力加载已认识的字段")
	}
}

func TestValidateCatchesProblems(t *testing.T) {
	bad := []byte(`{
  "schemaVersion": 1,
  "generoVersion": "2.50",
  "symbols": [
    {"name": "b", "kind": "function"},
    {"name": "a", "kind": "nope"},
    {"name": "B", "kind": "variable"},
    {"name": "", "kind": "variable"},
    {"name": "m", "kind": "method"}
  ]
}`)
	problems := Validate(bad)
	joined := strings.Join(problems, "\n")
	for _, want := range []string{"未按名字排序", "kind=\"nope\" 非法", "重名", "name 为空", "method 缺少 class"} {
		if !strings.Contains(joined, want) {
			t.Errorf("应报告 %q，实际:\n%s", want, joined)
		}
	}
}

func TestLoaderOverlayAndDisable(t *testing.T) {
	dir := t.TempDir()
	overlay := filepath.Join(dir, "overlay.json")
	body := `{
  "schemaVersion": 1,
  "generoVersion": "2.50",
  "disable": ["SUM"],
  "symbols": [
    {"name": "MY_BUILTIN", "kind": "function", "signature": "MY_BUILTIN(x)"},
    {"name": "STATUS", "kind": "variable", "doc": "覆盖后的说明"}
  ]
}`
	if err := os.WriteFile(overlay, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	l := NewLoader(nil)
	if err := l.Configure(dir, []string{"overlay.json"}, nil); err != nil {
		t.Fatal(err)
	}
	set := l.Get()
	if _, ok := set.Lookup("MY_BUILTIN"); !ok {
		t.Error("覆盖文件新增的符号应生效")
	}
	if _, ok := set.Lookup("SUM"); ok {
		t.Error("disable 应移除内置 SUM")
	}
	if !set.Disabled("sum") {
		t.Error("Disabled 应记录 disable 名字")
	}
	sym, _ := set.Lookup("STATUS")
	if sym.Doc != "覆盖后的说明" {
		t.Errorf("覆盖应改写 Doc，实际 %q", sym.Doc)
	}
	// 内置表本身的条目不应被覆盖文件"删除"（除了显式 disable）。
	if _, ok := set.Lookup("SQLCA"); !ok {
		t.Error("内置条目应保留")
	}
}

func TestLoaderConfigureDisableFromConfig(t *testing.T) {
	l := NewLoader(nil)
	if err := l.Configure(t.TempDir(), nil, []string{"STATUS"}); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Get().Lookup("STATUS"); ok {
		t.Error("配置里的 disable 应生效")
	}
}

func TestLoaderMissingOverlayIsNotFatal(t *testing.T) {
	l := NewLoader(nil)
	dir := t.TempDir()
	// 显式配置了不存在的文件：不报错、内置表照常可用。
	if err := l.Configure(dir, []string{"nope.json"}, nil); err != nil {
		t.Fatalf("缺失的覆盖文件不该让 Configure 失败: %v", err)
	}
	if l.Get().Len() < 50 {
		t.Error("内置表应仍然可用")
	}
}

func TestLoaderAutoDiscoversConventionPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".bdl-lsp"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"schemaVersion":1,"generoVersion":"2.50","symbols":[{"name":"PROJECT_ONLY","kind":"variable"}]}`
	if err := os.WriteFile(filepath.Join(dir, ".bdl-lsp", "builtins.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLoader(nil)
	// 不配 paths，只给 root：约定路径要自动加载。
	if err := l.Configure(dir, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Get().Lookup("PROJECT_ONLY"); !ok {
		t.Error(".bdl-lsp/builtins.json 应被自动加载")
	}
}

func TestLoaderReloadPicksUpChanges(t *testing.T) {
	dir := t.TempDir()
	overlay := filepath.Join(dir, "o.json")
	write := func(name string) {
		body := `{"schemaVersion":1,"generoVersion":"2.50","symbols":[{"name":"` + name + `","kind":"variable"}]}`
		if err := os.WriteFile(overlay, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("FIRST")
	l := NewLoader(nil)
	if err := l.Configure(dir, []string{"o.json"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Get().Lookup("FIRST"); !ok {
		t.Fatal("第一版应生效")
	}

	// 直接改文件 → Reload 后应看到新内容（不依赖 mtime 精度）。
	write("SECOND")
	if err := l.Reload(); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Get().Lookup("SECOND"); !ok {
		t.Error("重载后应看到新符号")
	}
	if _, ok := l.Get().Lookup("FIRST"); ok {
		t.Error("重载后旧符号应消失")
	}
}

func TestMaybeReloadThrottles(t *testing.T) {
	dir := t.TempDir()
	overlay := filepath.Join(dir, "o.json")
	body := `{"schemaVersion":1,"generoVersion":"2.50","symbols":[{"name":"A","kind":"variable"}]}`
	if err := os.WriteFile(overlay, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLoader(nil)
	if err := l.Configure(dir, []string{"o.json"}, nil); err != nil {
		t.Fatal(err)
	}
	// 刚 Configure 过，节流窗口内不应重载（否则每次按键都 stat）。
	if l.MaybeReload() {
		t.Error("节流窗口内不应重载")
	}
}

// TestMaybeReloadDetectsNewlyCreatedOverlay 验证"启动后才新建覆盖文件"能被发现。
//
// 这是最容易漏的一条：配置时约定路径还不存在，指纹里必须留它的空位，
// 否则用户新建了 .bdl-lsp/builtins.json 却毫无反应。
func TestMaybeReloadDetectsNewlyCreatedOverlay(t *testing.T) {
	dir := t.TempDir()
	l := NewLoader(nil)
	if err := l.Configure(dir, nil, nil); err != nil {
		t.Fatal(err)
	}
	// 绕过 2s 节流：直接清掉 last。
	l.mu.Lock()
	l.last = time.Time{}
	l.mu.Unlock()

	if err := os.MkdirAll(filepath.Join(dir, ".bdl-lsp"), 0o755); err != nil {
		t.Fatal(err)
	}
	body := `{"schemaVersion":1,"generoVersion":"2.50","symbols":[{"name":"LATE_ADDED","kind":"variable"}]}`
	if err := os.WriteFile(filepath.Join(dir, ".bdl-lsp", "builtins.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	l.mu.Lock()
	l.last = time.Time{}
	l.mu.Unlock()
	if !l.MaybeReload() {
		t.Fatal("新建的约定路径覆盖文件应触发重载")
	}
	if _, ok := l.Get().Lookup("LATE_ADDED"); !ok {
		t.Error("重载后应能看到新符号")
	}
}

// TestMaybeReloadDetectsDeletedOverlay 验证删掉覆盖文件后退回内置表。
func TestMaybeReloadDetectsDeletedOverlay(t *testing.T) {
	dir := t.TempDir()
	overlay := filepath.Join(dir, "o.json")
	body := `{"schemaVersion":1,"generoVersion":"2.50","symbols":[{"name":"TEMP_SYM","kind":"variable"}]}`
	if err := os.WriteFile(overlay, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	l := NewLoader(nil)
	if err := l.Configure(dir, []string{"o.json"}, nil); err != nil {
		t.Fatal(err)
	}
	if _, ok := l.Get().Lookup("TEMP_SYM"); !ok {
		t.Fatal("初始应加载")
	}
	if err := os.Remove(overlay); err != nil {
		t.Fatal(err)
	}
	l.mu.Lock()
	l.last = time.Time{}
	l.mu.Unlock()
	if !l.MaybeReload() {
		t.Fatal("删除覆盖文件应触发重载")
	}
	if _, ok := l.Get().Lookup("TEMP_SYM"); ok {
		t.Error("删除后应回退到只用内置表")
	}
}

func TestMaybeReloadNoOverlayIsNoop(t *testing.T) {
	l := NewLoader(nil)
	if err := l.Configure(t.TempDir(), nil, nil); err != nil {
		t.Fatal(err)
	}
	if l.MaybeReload() {
		t.Error("没有覆盖文件时不应重载")
	}
}

func TestKindLabel(t *testing.T) {
	cases := map[Kind]string{
		KindVariable: "内置变量",
		KindFunction: "内置函数",
		KindClass:    "内置类",
		KindMethod:   "内置方法",
		KindConstant: "内置常量",
	}
	for k, want := range cases {
		if got := k.Label(); got != want {
			t.Errorf("%q.Label() = %q，期望 %q", k, got, want)
		}
		if !k.Valid() {
			t.Errorf("%q 应 Valid", k)
		}
	}
	if Kind("nope").Valid() {
		t.Error("未知 kind 不应 Valid")
	}
}

func TestNilSetSafe(t *testing.T) {
	var s *Set
	if s.Len() != 0 || s.IsKnown("X") || s.IsFunction("X") || s.IsVariable("X") {
		t.Error("nil Set 应全部安全返回零值")
	}
	if s.Symbols() != nil || s.Sources() != nil || s.GeneroVersion() != "" {
		t.Error("nil Set 的读操作应返回零值")
	}
	var l *Loader
	if l.Get() == nil {
		t.Error("nil Loader 的 Get 应回退到内置表")
	}
}
