package diag

import (
	"testing"

	"github.com/darcyjoven/bdl-lsp/internal/config"
	"go.lsp.dev/protocol"
)

func TestToSeverityMapsAllLevels(t *testing.T) {
	cases := []struct {
		level config.DiagnosticLevel
		want  protocol.DiagnosticSeverity
		on    bool
	}{
		{config.LevelOff, 0, false},
		{config.LevelError, protocol.DiagnosticSeverityError, true},
		{config.LevelWarning, protocol.DiagnosticSeverityWarning, true},
		{config.LevelInfo, protocol.DiagnosticSeverityInformation, true},
		{config.LevelHint, protocol.DiagnosticSeverityHint, true},
		// 未知级别按 error：配置写错时要显眼，不能静默降级。
		{"fatal", protocol.DiagnosticSeverityError, true},
	}
	for _, c := range cases {
		got, on := ToSeverity(c.level)
		if on != c.on || (on && got != c.want) {
			t.Errorf("ToSeverity(%q) = (%v, %v)，期望 (%v, %v)", c.level, got, on, c.want, c.on)
		}
	}
}

func TestSeverityForPriorityNewKeyOverLegacy(t *testing.T) {
	// 新键 off 压过旧键 warning。
	settings := config.Settings{
		Globals:     config.GlobalsSettings{UnresolvedSourceDiagnostics: config.LevelWarning},
		Diagnostics: map[string]config.DiagnosticLevel{string(CodeVariableSourceUnresolved): config.LevelOff},
	}
	if _, on := SeverityFor(CodeVariableSourceUnresolved, settings, settings.Globals.UnresolvedSourceDiagnostics); on {
		t.Error("新键 off 应压过旧键 warning")
	}

	// 新键 error 压过旧键 warning。
	settings.Diagnostics[string(CodeVariableSourceUnresolved)] = config.LevelError
	sev, on := SeverityFor(CodeVariableSourceUnresolved, settings, settings.Globals.UnresolvedSourceDiagnostics)
	if !on || sev != protocol.DiagnosticSeverityError {
		t.Errorf("新键 error 应生效，得到 (%v, %v)", sev, on)
	}
}

func TestSeverityForFallsBackToLegacyThenDefault(t *testing.T) {
	// 旧键优先于注册表默认。
	settings := config.Settings{}
	sev, on := SeverityFor(CodeVariableSourceUnresolved, settings, config.LevelHint)
	if !on || sev != protocol.DiagnosticSeverityHint {
		t.Errorf("应使用旧键 hint，得到 (%v, %v)", sev, on)
	}

	// 旧键为空时用注册表默认（variable-source-unresolved 默认 warning）。
	sev, on = SeverityFor(CodeVariableSourceUnresolved, settings, "")
	if !on || sev != protocol.DiagnosticSeverityWarning {
		t.Errorf("应使用默认 warning，得到 (%v, %v)", sev, on)
	}

	// 空串新键值不应覆盖（配置里给了空值等于没给）。
	settings.Diagnostics = map[string]config.DiagnosticLevel{string(CodeVariableSourceUnresolved): ""}
	sev, on = SeverityFor(CodeVariableSourceUnresolved, settings, "")
	if !on || sev != protocol.DiagnosticSeverityWarning {
		t.Errorf("空值新键不应覆盖默认，得到 (%v, %v)", sev, on)
	}
}

func TestDefaultLevelsMatchDecisions(t *testing.T) {
	// 这三条是明确的产品决策，改动需要同步文档与插件清单。
	cases := map[Code]config.DiagnosticLevel{
		CodeVariableSourceUnresolved:  config.LevelWarning,
		CodeRecordMemberUnknown:       config.LevelOff,
		CodeFunctionNotFound:          config.LevelOff,
		CodeFileTooLarge:              config.LevelInfo,
		CodeGlobalsFileNotFound:       config.LevelWarning,
		CodeFieldNotInForm:            config.LevelWarning,
		CodeFormDuplicateItem:         config.LevelWarning,
		CodeFormLayoutTagUndefined:    config.LevelWarning,
		CodeFormScreenRecordUndefined: config.LevelWarning,
	}
	for code, want := range cases {
		if got := DefaultLevel(code); got != want {
			t.Errorf("DefaultLevel(%q) = %q，期望 %q", code, got, want)
		}
	}
}

func TestRegisteredIsSortedAndCoversAllConstants(t *testing.T) {
	codes := Registered()
	if len(codes) == 0 {
		t.Fatal("注册表为空")
	}
	for i := 1; i < len(codes); i++ {
		if codes[i-1] >= codes[i] {
			t.Fatalf("注册表未按字典序排列：%q 在 %q 之前", codes[i-1], codes[i])
		}
	}
	// 每个公开常量都必须注册，否则无法用 bdl.diagnostics 单独配置。
	for _, c := range []Code{
		CodeGlobalsFileNotFound, CodeVariableSourceUnresolved, CodeFieldNotInForm,
		CodeFormDuplicateItem, CodeFormLayoutTagUndefined, CodeFormScreenRecordUndefined,
		CodeRecordMemberUnknown, CodeFileTooLarge, CodeFunctionNotFound,
	} {
		if !IsRegistered(c) {
			t.Errorf("诊断码 %q 未注册", c)
		}
	}
}

func TestEnsureRegisteredRejectsTypos(t *testing.T) {
	if err := EnsureRegistered(CodeVariableSourceUnresolved); err != nil {
		t.Errorf("已注册的码不应报错：%v", err)
	}
	if err := EnsureRegistered("variable-source-unresolvedd"); err == nil {
		t.Error("拼错的码应报错")
	}
}

func TestValidateSettingsReportsUnknownCodes(t *testing.T) {
	settings := config.Settings{Diagnostics: map[string]config.DiagnosticLevel{
		string(CodeFunctionNotFound): config.LevelHint,
		"no-such-code":               config.LevelWarning,
		"aaa-typo":                   config.LevelOff,
	}}
	got := ValidateSettings(settings)
	if len(got) != 2 || got[0] != "aaa-typo" || got[1] != "no-such-code" {
		t.Errorf("未知码应按字典序返回 [aaa-typo no-such-code]，实际 %v", got)
	}
}

func TestValidateSettingsEmptyConfig(t *testing.T) {
	if got := ValidateSettings(config.Settings{}); len(got) != 0 {
		t.Errorf("空配置不应有未知码，实际 %v", got)
	}
}
