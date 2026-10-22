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
	// Q10 定稿：语法错误默认 warning，与 bdl.diagnostics 新键保持一致。
	if s.SyntaxDiagnostics != LevelWarning {
		t.Errorf("SyntaxDiagnostics = %q，期望 warning", s.SyntaxDiagnostics)
	}
	if s.MissingDiagnostics != LevelWarning {
		t.Errorf("MissingDiagnostics = %q，期望 warning", s.MissingDiagnostics)
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
	if err := m.Apply(json.RawMessage(`{"generoVersion":"3.00","missingDiagnostics":"off"}`)); err != nil {
		t.Fatal(err)
	}
	s := m.Get()
	if s.GeneroVersion != Genero30 {
		t.Errorf("GeneroVersion = %q", s.GeneroVersion)
	}
	if s.MissingDiagnostics != LevelOff {
		t.Errorf("MissingDiagnostics = %q", s.MissingDiagnostics)
	}
	// 未指定的项保持默认
	if s.SyntaxDiagnostics != LevelWarning {
		t.Errorf("SyntaxDiagnostics 应保持默认 warning，实际 %q", s.SyntaxDiagnostics)
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

func TestValidateErrors(t *testing.T) {
	cases := []Settings{
		{GeneroVersion: "9.99", SyntaxDiagnostics: LevelError, MissingDiagnostics: LevelWarning, Limits: Limits{MaxIssues: 1, MaxFileBytes: 1, ParseTimeoutMS: 1}},
		{GeneroVersion: Genero25, SyntaxDiagnostics: "fatal", MissingDiagnostics: LevelWarning, Limits: Limits{MaxIssues: 1, MaxFileBytes: 1, ParseTimeoutMS: 1}},
		{GeneroVersion: Genero25, SyntaxDiagnostics: LevelError, MissingDiagnostics: LevelWarning, Limits: Limits{MaxIssues: 0, MaxFileBytes: 1, ParseTimeoutMS: 1}},
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

func TestDiagnosticsMergeByCode(t *testing.T) {
	m := NewManager("")
	// 首次下发：条目写入。
	if err := m.Apply(json.RawMessage(`{"diagnostics":{"variable-source-unresolved":"off"}}`)); err != nil {
		t.Fatal(err)
	}
	if got := m.Get().Diagnostics["variable-source-unresolved"]; got != LevelOff {
		t.Fatalf("首次下发未生效：%q", got)
	}

	// 再次下发另一个码：前一个不被清掉（增量合并）。
	if err := m.Apply(json.RawMessage(`{"diagnostics":{"function-not-found":"hint"}}`)); err != nil {
		t.Fatal(err)
	}
	s := m.Get()
	if s.Diagnostics["variable-source-unresolved"] != LevelOff {
		t.Error("增量合并丢了已有条目")
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
	if err := m.Apply(json.RawMessage(`{"missingDiagnostics":"error"}`)); err != nil {
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
