package diagcheck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darcyjoven/bdl-lsp/internal/diag"
)

func TestDocStable(t *testing.T) {
	a := Doc(diag.Specs())
	if a != Doc(diag.Specs()) {
		t.Fatal("同一份注册表生成的文档不稳定")
	}
	for _, sp := range diag.Specs() {
		if !strings.Contains(a, "`"+string(sp.Code)+"`") {
			t.Errorf("文档里缺诊断码 %q", sp.Code)
		}
	}
}

func TestCheckRegistryEveryCodeDocumented(t *testing.T) {
	if probs := CheckRegistry(diag.Specs()); len(probs) > 0 {
		for _, p := range probs {
			t.Error(p.String())
		}
	}
}

// TestLegacyGroupLevelsConsistent 检查线上数据本身的约束：
// 同一老键覆盖的多个码必须有同一默认级别，否则"老键的默认值"是二义的。
func TestLegacyGroupLevelsConsistent(t *testing.T) {
	levelOf := map[diag.Code]string{}
	for _, sp := range diag.Specs() {
		levelOf[sp.Code] = string(sp.Level)
	}
	for key, codes := range LegacyKeys {
		want := levelOf[codes[0]]
		for _, c := range codes {
			if levelOf[c] != want {
				t.Errorf("%s 覆盖的 %s 默认级别 %q ≠ %s 的 %q", key, c, levelOf[c], codes[0], want)
			}
		}
	}
}

// TestCheckVSCodeCatchesDrift 钉住"校验有牙"：默认值/枚举/缺键都要报。
func TestCheckVSCodeCatchesDrift(t *testing.T) {
	specs := diag.Specs()
	dir := t.TempDir()

	good := writeVSCode(t, dir, "package.json", func(props map[string]any) {})
	if probs, err := CheckVSCode(filepath.Join(dir, good), specs); err != nil || len(probs) > 0 {
		t.Fatalf("正常配置不应报错（err=%v probs=%v）", err, probs)
	}

	cases := []struct {
		name   string
		mutate func(map[string]any)
		want   string
	}{
		{"默认值与注册表不符", func(p map[string]any) {
			p["bdl.fieldDiagnostics"].(map[string]any)["default"] = "off"
		}, "bdl.fieldDiagnostics"},
		{"枚举写错一个词", func(p map[string]any) {
			p["bdl.syntaxDiagnostics"].(map[string]any)["enum"] = []string{"off", "error", "warn"}
		}, "bdl.syntaxDiagnostics"},
		{"缺老键", func(p map[string]any) {
			delete(p, "bdl.recordMemberDiagnostics")
		}, "bdl.recordMemberDiagnostics"},
		{"缺新键", func(p map[string]any) {
			delete(p, "bdl.diagnostics")
		}, "bdl.diagnostics"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name := writeVSCode(t, dir, "pkg.json", c.mutate)
			probs, err := CheckVSCode(filepath.Join(dir, name), specs)
			if err != nil {
				t.Fatalf("解析失败: %v", err)
			}
			if len(probs) == 0 {
				t.Fatalf("应报不一致，实际通过")
			}
			var hit bool
			for _, p := range probs {
				if strings.Contains(p.String(), c.want) {
					hit = true
				}
			}
			if !hit {
				t.Errorf("问题里没提到 %q：%v", c.want, probs)
			}
		})
	}
}

// TestCheckDocCatchesStale 文档与注册表不一致时要报。
func TestCheckDocCatchesStale(t *testing.T) {
	specs := diag.Specs()
	path := filepath.Join(t.TempDir(), "DIAGNOSTICS.md")
	if probs := CheckDoc(path, specs); len(probs) == 0 {
		t.Fatal("文档不存在时应报")
	}
	if err := os.WriteFile(path, []byte(Doc(specs)), 0o644); err != nil {
		t.Fatal(err)
	}
	if probs := CheckDoc(path, specs); len(probs) > 0 {
		t.Fatalf("刚生成的文档不该报：%v", probs)
	}
	if err := os.WriteFile(path, []byte("# 旧文档\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if probs := CheckDoc(path, specs); len(probs) == 0 {
		t.Fatal("文档内容过期时应报")
	}
}

// writeVSCode 生成一个"与注册表一致"的 package.json，mutate 用于改坏它。
func writeVSCode(t *testing.T, dir, name string, mutate func(map[string]any)) string {
	t.Helper()
	props := map[string]any{
		"bdl.diagnostics": map[string]any{
			"type": "object", "default": map[string]any{}, "enum": AllLevels(),
		},
	}
	levelOf := map[diag.Code]string{}
	for _, sp := range diag.Specs() {
		levelOf[sp.Code] = string(sp.Level)
	}
	for key, codes := range LegacyKeys {
		props[key] = map[string]any{
			"type": "string", "enum": AllLevels(), "default": levelOf[codes[0]],
		}
	}
	if mutate != nil {
		mutate(props)
	}
	pkg := map[string]any{
		"contributes": map[string]any{
			"configuration": map[string]any{"properties": props},
		},
	}
	data, err := json.MarshalIndent(pkg, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return name
}
