package glob

import "testing"

func TestDoubleStar(t *testing.T) {
	tests := []struct {
		pattern string
		rel     string
		want    bool
	}{
		{"**/*.4gl", "a.4gl", true},
		{"**/*.4gl", "x/y/a.4gl", true},
		{"**/*.4gl", "x/y/a.per", false},
		{"*.4gl", "a.4gl", true},
		{"*.4gl", "x/a.4gl", false},
		{"**/42f/**", "42f/a.4gl", true},
		{"**/42f/**", "m/42f/sub/a.4gl", true},
		{"**/42f/**", "m/a.4gl", false},
		{"src/**/*.per", "src/a/b.per", true},
		{"src/**/*.per", "src/a/b.4gl", false},
		{"src/**/*.per", "other/src/a/b.per", false},
	}
	for _, tc := range tests {
		g, err := Compile(tc.pattern)
		if err != nil {
			t.Fatalf("编译 %q 失败: %v", tc.pattern, err)
		}
		if got := g.Match(tc.rel); got != tc.want {
			t.Errorf("%q 匹配 %q = %v，期望 %v", tc.pattern, tc.rel, got, tc.want)
		}
	}
}

func TestCompileRejectsBadPattern(t *testing.T) {
	for _, p := range []string{"", "/", "a/[b"} {
		if _, err := Compile(p); err == nil {
			t.Errorf("Compile(%q) 应失败", p)
		}
	}
}

func TestCompileSetAndValidate(t *testing.T) {
	set, err := CompileSet([]string{"**/*.4gl", "build/**"})
	if err != nil {
		t.Fatal(err)
	}
	if !set.Match("a/b.4gl") || !set.Match("build/x.o") || set.Match("src/a.per") {
		t.Errorf("Set.Match 结果不符合预期")
	}
	if len(Validate("ignore.files", []string{"ok/**", "a/["})) != 1 {
		t.Errorf("Validate 应只报一个非法模式")
	}
}
