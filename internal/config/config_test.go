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
	if s.SyntaxDiagnostics != LevelError {
		t.Errorf("SyntaxDiagnostics = %q，期望 error", s.SyntaxDiagnostics)
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
	if s.SyntaxDiagnostics != LevelError {
		t.Errorf("SyntaxDiagnostics 应保持默认，实际 %q", s.SyntaxDiagnostics)
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
