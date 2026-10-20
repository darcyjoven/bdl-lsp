package config

import (
	"encoding/json"
	"os"
	"path/filepath"
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

func TestURIToPath(t *testing.T) {
	cases := map[string]string{
		"file:///tmp/x":       "/tmp/x",
		"file:///c:/work/app": "c:/work/app",
		"file:///tmp/a%20b":   "/tmp/a b",
	}
	for uri, want := range cases {
		got, err := uriToPath(uri)
		if err != nil {
			t.Errorf("uriToPath(%q) 报错: %v", uri, err)
			continue
		}
		if got != want {
			t.Errorf("uriToPath(%q) = %q，期望 %q", uri, got, want)
		}
	}
	if _, err := uriToPath("http://x"); err == nil {
		t.Error("非 file URI 应报错")
	}
}
