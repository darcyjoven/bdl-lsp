package ignore

import "testing"

func TestFile(t *testing.T) {
	m, problems := New("/work/proj", []string{"build/**", "*.bak.4gl"}, nil, nil)
	if len(problems) != 0 {
		t.Fatalf("不应有问题: %v", problems)
	}
	tests := []struct {
		path string
		want bool
	}{
		{"/work/proj/build/gen/a.4gl", true},
		{"/work/proj/src/a.bak.4gl", true},      // 无 / 的模式按 basename
		{"/work/proj/src/deep/a.bak.4gl", true}, // basename 在任意深度
		{"/work/proj/src/a.4gl", false},
		{"/other/build/a.4gl", false}, // 工作区外不匹配相对模式
	}
	for _, tc := range tests {
		if got := m.File(tc.path); got != tc.want {
			t.Errorf("File(%q) = %v，期望 %v", tc.path, got, tc.want)
		}
	}
	if !m.File("file:///work/proj/build/a.4gl") {
		t.Error("file:// URI 应能识别")
	}
}

func TestFieldIsCaseInsensitive(t *testing.T) {
	m, _ := New("/w", nil, []string{"tmp_*", "legacy.field"}, nil)
	if !m.Field("TMP_X") || !m.Field("legacy.field") {
		t.Error("字段匹配应大小写不敏感")
	}
	if m.Field("other") {
		t.Error("不应匹配无关字段")
	}
}

func TestFormVariants(t *testing.T) {
	m, _ := New("/w", nil, nil, []string{"cust", "**/per/old.per"})
	tests := []struct {
		literal string
		path    string
		want    bool
	}{
		{"cust", "/w/a/per/cust.per", true},
		{"CUST", "/w/a/per/cust.per", true},
		{"x", "/w/a/per/old.per", true},
		{"x", "/w/a/per/new.per", false},
	}
	for _, tc := range tests {
		if got := m.Form(tc.literal, tc.path); got != tc.want {
			t.Errorf("Form(%q,%q) = %v，期望 %v", tc.literal, tc.path, got, tc.want)
		}
	}
}

func TestInvalidPatternIsSkipped(t *testing.T) {
	m, problems := New("/w", []string{"a/[", "build/**"}, nil, nil)
	if len(problems) != 1 {
		t.Fatalf("应报告 1 个非法模式，实际 %v", problems)
	}
	if !m.File("/w/build/a.4gl") {
		t.Error("合法模式仍然生效")
	}
}

func TestNilMatcher(t *testing.T) {
	var m *Matcher
	if m.File("/w/a.4gl") || m.Field("x") || m.Form("x", "/w/x.per") {
		t.Error("nil Matcher 不应忽略任何东西")
	}
}
