package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestDefaultValues(t *testing.T) {
	s := Default()
	if s.GeneroVersion != Genero25 {
		t.Errorf("GeneroVersion = %q，期望 2.50", s.GeneroVersion)
	}
	if len(s.Diagnostics) != 0 {
		t.Errorf("Diagnostics 默认应为空（级别由注册表提供），实际 %v", s.Diagnostics)
	}
	if s.Limits.MaxIssues != 200 {
		t.Errorf("MaxIssues = %d", s.Limits.MaxIssues)
	}
	if problems := s.Validate(); len(problems) != 0 {
		t.Errorf("默认配置应合法：%v", problems)
	}
}

func TestApplyPartial(t *testing.T) {
	m := NewManager("")
	if err := m.Apply(json.RawMessage(`{"generoVersion":"3.00","diagnostics":{"missing-node":"off"}}`)); err != nil {
		t.Fatal(err)
	}
	s := m.Get()
	if s.GeneroVersion != Genero30 {
		t.Errorf("GeneroVersion = %q", s.GeneroVersion)
	}
	if s.Diagnostics["missing-node"] != LevelOff {
		t.Errorf("diagnostics[missing-node] = %q", s.Diagnostics["missing-node"])
	}
	// 未指定的项保持默认
	if s.Diagnostics["syntax-error"] != "" {
		t.Errorf("未下发的码不应出现在覆盖表，实际 %q", s.Diagnostics["syntax-error"])
	}
	if s.Limits.MaxIssues != 200 {
		t.Errorf("MaxIssues 应保持默认，实际 %d", s.Limits.MaxIssues)
	}
}

func TestApplyInvalidJSON(t *testing.T) {
	m := NewManager("")
	if err := m.Apply(json.RawMessage(`{bad json}`)); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

func TestApplyEmptyDiagnosticsClears(t *testing.T) {
	m := NewManager("")
	if err := m.Apply(json.RawMessage(`{"diagnostics":{"syntax-error":"off"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Diagnostics["syntax-error"]; got != LevelOff {
		t.Fatalf("设置未生效：%q", got)
	}
	// VSCode 删除用户覆盖后会下发空对象；必须清掉旧值，否则关掉的码永远回不来。
	if err := m.Apply(json.RawMessage(`{"diagnostics":{}}`)); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Diagnostics["syntax-error"]; got != "" {
		t.Errorf("空对象应清空覆盖表，实际 %q", got)
	}
}

func TestApplyKeepsProjectDiagnostics(t *testing.T) {
	m := NewManager("")
	// 模拟已加载的 .bdl-lsp/config.json 覆盖。
	m.fileDiagnostics = map[string]DiagnosticLevel{"function-not-found": LevelHint}

	// 客户端没有个人覆盖时下发空对象：项目配置必须保留。
	if err := m.Apply(json.RawMessage(`{"diagnostics":{}}`)); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Diagnostics["function-not-found"]; got != LevelHint {
		t.Errorf("项目配置被个人空设置抹掉：%q", got)
	}

	// 个人覆盖可以压过项目配置。
	if err := m.Apply(json.RawMessage(`{"diagnostics":{"function-not-found":"off"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Diagnostics["function-not-found"]; got != LevelOff {
		t.Errorf("个人覆盖未生效：%q", got)
	}
}

func TestValidateErrors(t *testing.T) {
	cases := []Settings{
		{GeneroVersion: "9.99", Limits: Limits{MaxIssues: 1, MaxFileBytes: 1, ParseTimeoutMS: 1}},
		{GeneroVersion: Genero25, Diagnostics: map[string]DiagnosticLevel{"syntax-error": "fatal"}, Limits: Limits{MaxIssues: 1, MaxFileBytes: 1, ParseTimeoutMS: 1}},
		{GeneroVersion: Genero25, Limits: Limits{MaxIssues: 0, MaxFileBytes: 1, ParseTimeoutMS: 1}},
	}
	for i, c := range cases {
		if problems := c.Validate(); len(problems) == 0 {
			t.Errorf("用例 %d 应报错", i)
		}
	}
}

func TestLoadWorkspaceConfig(t *testing.T) {
	dir := t.TempDir()
	cfgDir := filepath.Join(dir, ".bdl-lsp")
	if err := os.MkdirAll(cfgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := `{"generoVersion":"4.00","limits":{"maxIssues":50}}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	m := NewManager("file://" + dir)
	s := m.Get()
	if s.GeneroVersion != Genero40 {
		t.Errorf("GeneroVersion = %q，期望 4.00", s.GeneroVersion)
	}
	if s.Limits.MaxIssues != 50 {
		t.Errorf("MaxIssues = %d，期望 50", s.Limits.MaxIssues)
	}
	if m.Path() == "" {
		t.Error("Path() 应返回配置文件路径")
	}
}

func TestWorkspaceConfigMissingFallsBack(t *testing.T) {
	dir := t.TempDir()
	m := NewManager("file://" + dir)
	if s := m.Get(); s.GeneroVersion != Genero25 {
		t.Errorf("配置缺失时应使用默认值，实际 %q", s.GeneroVersion)
	}
}

// TestURIToPath 覆盖 uriToPath 的委托行为（实现已移到 internal/fileuri）。
//
// Windows 盘符的归一化语义在 fileuri 包里有专门的跨平台单测
// （TestToPathWindows），这里只断言"按宿主平台正确"，
// 不再断言"在 Linux 上也能猜出 Windows 盘符"——那正是 P7-A-2 要修掉的旧行为。
func TestURIToPath(t *testing.T) {
	posix := []struct{ uri, want string }{
		{"file:///tmp/x", "/tmp/x"},
		{"file:///tmp/a%20b", "/tmp/a b"},
		{"file:///tmp/a%23b", "/tmp/a#b"},
	}
	for _, c := range posix {
		got, err := uriToPath(c.uri)
		if err != nil {
			t.Errorf("uriToPath(%q) 报错: %v", c.uri, err)
			continue
		}
		if runtime.GOOS != "windows" && got != c.want {
			t.Errorf("uriToPath(%q) = %q，期望 %q", c.uri, got, c.want)
		}
	}

	// 盘符形态：Windows 归一成反斜杠绝对路径；POSIX 上原样保留前导斜杠
	// （`/c:/work` 在 Linux 上是合法路径，不做跨平台猜测）。
	driveWant := "/c:/work/app"
	if runtime.GOOS == "windows" {
		driveWant = `C:\work\app`
	}
	for _, uri := range []string{
		"file:///c:/work/app",   // Zed 风格：冒号不编码
		"file:///c%3A/work/app", // VSCode 风格：冒号编码（P7-A-2 的 bug 形态）
	} {
		got, err := uriToPath(uri)
		if err != nil {
			t.Errorf("uriToPath(%q) 报错: %v", uri, err)
			continue
		}
		if got != driveWant {
			t.Errorf("uriToPath(%q) = %q，期望 %q", uri, got, driveWant)
		}
	}

	if _, err := uriToPath("http://x"); err == nil {
		t.Error("非 file URI 应报错")
	}
}

func TestDiagnosticsReplaceByCode(t *testing.T) {
	m := NewManager("")
	// 首次下发：条目写入。
	if err := m.Apply(json.RawMessage(`{"diagnostics":{"variable-source-unresolved":"off"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Diagnostics["variable-source-unresolved"]; got != LevelOff {
		t.Fatalf("首次下发未生效：%q", got)
	}

	// 再次下发另一个码：diagnostics 是完整对象，旧条目不应保留。
	// 否则 VSCode 里删掉一个覆盖后，服务端仍会继续用旧值。
	if err := m.Apply(json.RawMessage(`{"diagnostics":{"function-not-found":"hint"}}`)); err != nil {
		t.Fatal(err)
	}
	s := m.Get()
	if _, ok := s.Diagnostics["variable-source-unresolved"]; ok {
		t.Error("完整下发应清掉未列出的旧条目")
	}
	if s.Diagnostics["function-not-found"] != LevelHint {
		t.Errorf("新条目未生效：%q", s.Diagnostics["function-not-found"])
	}

	// 覆盖同一个码。
	if err := m.Apply(json.RawMessage(`{"diagnostics":{"variable-source-unresolved":"error"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Diagnostics["variable-source-unresolved"]; got != LevelError {
		t.Errorf("覆盖未生效：%q", got)
	}
}

func TestIgnoreMergeAndValidate(t *testing.T) {
	m := NewManager("")
	if err := m.Apply(json.RawMessage(`{"ignore":{"files":["build/**"],"fields":["tmp_*"]}}`)); err != nil {
		t.Fatal(err)
	}
	if err := m.Apply(json.RawMessage(`{"ignore":{"forms":["cust"]}}`)); err != nil {
		t.Fatal(err)
	}
	s := m.Get()
	if len(s.Ignore.Files) != 1 || len(s.Ignore.Fields) != 1 || len(s.Ignore.Forms) != 1 {
		t.Fatalf("三组忽略规则应增量保留：%+v", s.Ignore)
	}

	bad := Default()
	bad.Ignore.Files = []string{"a/["}
	if got := bad.Validate(); len(got) == 0 {
		t.Error("非法 ignore.files glob 应被 Validate 报出")
	}

	// 显式清空（空数组）应生效，不能被当作"未下发"。
	if err := m.Apply(json.RawMessage(`{"ignore":{"files":[]}}`)); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Ignore.Files; len(got) != 0 {
		t.Errorf("显式清空未生效：%v", got)
	}
}

func TestDiagnosticsAbsentKeepsValues(t *testing.T) {
	m := NewManager("")
	if err := m.Apply(json.RawMessage(`{"diagnostics":{"file-too-large":"off"}}`)); err != nil {
		t.Fatal(err)
	}
	// 不下发 diagnostics 的配置不应清空它。
	if err := m.Apply(json.RawMessage(`{"generoVersion":"3.00"}`)); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Diagnostics["file-too-large"]; got != LevelOff {
		t.Errorf("未下发 diagnostics 时不应清空：%q", got)
	}
}

func TestValidateDiagnosticsLevel(t *testing.T) {
	s := Default()
	s.Diagnostics = map[string]DiagnosticLevel{"some-code": "verbose"}
	problems := s.Validate()
	found := false
	for _, p := range problems {
		if stringsContains(p, "diagnostics[") {
			found = true
		}
	}
	if !found {
		t.Errorf("非法级别应被 Validate 报出，实际 %v", problems)
	}
}

func stringsContains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
