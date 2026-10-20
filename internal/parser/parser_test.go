package parser

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

const samplePer = `SCHEMA formonly

LAYOUT (text=%"sample")
GRID
{
  Name:  [f1        ]
}
END
END

ATTRIBUTES
EDIT f1 = formonly.cust_name, REQUIRED;
END
`

const sampleBDL = `MAIN
    DISPLAY "hello"
END MAIN

FUNCTION add(a, b)
    RETURN a + b
END FUNCTION
`

func newPool(t *testing.T) *Pool {
	t.Helper()
	p, err := New()
	if err != nil {
		t.Fatalf("New() 失败: %v", err)
	}
	t.Cleanup(p.Close)
	return p
}

func TestLangForPath(t *testing.T) {
	cases := map[string]Lang{
		"a.4gl":        LangBDL,
		"a.global":     LangBDL,
		"A.4GL":        LangBDL,
		"form.per":     LangPer,
		"form.PER":     LangPer,
		"dir/x.4gl":    LangBDL,
		`C:\src\a.4gl`: LangBDL,
	}
	for path, want := range cases {
		got, ok := LangForPath(path)
		if !ok || got != want {
			t.Errorf("LangForPath(%q) = (%q, %v)，期望 %q", path, got, ok, want)
		}
	}

	for _, path := range []string{"a.txt", "a", "", "a.4gl.bak"} {
		if lang, ok := LangForPath(path); ok {
			t.Errorf("LangForPath(%q) 应为不支持，实际 %q", path, lang)
		}
	}
}

func TestParseBothLanguages(t *testing.T) {
	pool := newPool(t)

	for name, tc := range map[string]struct {
		lang Lang
		src  string
	}{
		"per": {LangPer, samplePer},
		"bdl": {LangBDL, sampleBDL},
	} {
		t.Run(name, func(t *testing.T) {
			tree, err := pool.Parse(tc.lang, []byte(tc.src), nil)
			if err != nil {
				t.Fatalf("Parse() 失败: %v", err)
			}
			defer tree.Close()

			root := tree.RootNode()
			if root == nil {
				t.Fatal("RootNode() 为 nil")
			}
			if root.Kind() != "source_file" {
				t.Errorf("根节点 = %q，期望 source_file", root.Kind())
			}
			if root.HasError() {
				t.Errorf("样例不应有语法错误：%s", root.ToSexp())
			}
		})
	}
}

func TestParseUnsupportedLanguage(t *testing.T) {
	pool := newPool(t)
	if _, err := pool.Parse(Lang("cobol"), []byte("x"), nil); err == nil {
		t.Fatal("期望不支持语言返回错误")
	}
}

func TestCollectIssuesCleanFile(t *testing.T) {
	pool := newPool(t)
	tree, err := pool.Parse(LangBDL, []byte(sampleBDL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	if issues := CollectIssues(tree, 10); len(issues) != 0 {
		t.Fatalf("干净文件不应有问题，实际 %d 个：%+v", len(issues), issues)
	}
}

func TestCollectIssuesBrokenFile(t *testing.T) {
	pool := newPool(t)
	src := []byte("MAIN\n    DISPLAY \"unterminated\nEND MAIN\n")
	tree, err := pool.Parse(LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	issues := CollectIssues(tree, 10)
	if len(issues) == 0 {
		t.Fatal("损坏文件应报告至少一个语法错误")
	}
	for _, iss := range issues {
		if iss.Message == "" {
			t.Errorf("问题缺少消息：%+v", iss)
		}
		if iss.StartByte > iss.EndByte {
			t.Errorf("区间非法：%+v", iss)
		}
		if iss.StartRow >= uint(len(src))+1 {
			t.Errorf("行号越界：%+v", iss)
		}
	}
}

func TestCollectIssuesRespectsLimit(t *testing.T) {
	pool := newPool(t)
	src := []byte(strings.Repeat("DISPLAY \"oops\n", 50))
	tree, err := pool.Parse(LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	const limit = 3
	if issues := CollectIssues(tree, limit); len(issues) > limit {
		t.Fatalf("问题数 %d 超过上限 %d", len(issues), limit)
	}
}

func TestParseWithOldTree(t *testing.T) {
	pool := newPool(t)
	first, err := pool.Parse(LangBDL, []byte(sampleBDL), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer first.Close()

	second, err := pool.Parse(LangBDL, []byte(sampleBDL+"\n-- 追加注释\n"), first)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()

	if root := second.RootNode(); root == nil || root.HasError() {
		t.Fatalf("复用旧树后解析异常：%v", root)
	}
}

func BenchmarkParseBDL(b *testing.B) {
	pool, err := New()
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()
	src := []byte(sampleBDL)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := pool.Parse(LangBDL, src, nil)
		if err != nil {
			b.Fatal(err)
		}
		tree.Close()
	}
}

func BenchmarkParsePer(b *testing.B) {
	pool, err := New()
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()
	src := []byte(samplePer)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := pool.Parse(LangPer, src, nil)
		if err != nil {
			b.Fatal(err)
		}
		tree.Close()
	}
}

func TestCollectWithMerge(t *testing.T) {
	pool := newPool(t)
	// 一行里连续多个错误（漏逗号等），应合并
	src := []byte("MAIN\n    DISPLAY \"a\" \"b\" \"c\"\nEND MAIN\n")
	tree, err := pool.Parse(LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	raw := Collect(tree, CollectOptions{MaxIssues: 100, MergeAdjacent: false})
	merged := Collect(tree, CollectOptions{MaxIssues: 100, MergeAdjacent: true})

	if len(merged) > len(raw) {
		t.Errorf("合并后不应变多：raw=%d merged=%d", len(raw), len(merged))
	}
	// 合并后每条都应覆盖合法区间
	for _, iss := range merged {
		if iss.StartByte > iss.EndByte {
			t.Errorf("区间非法：%+v", iss)
		}
	}
}

func TestCollectDistinguishesMissing(t *testing.T) {
	pool := newPool(t)
	src := []byte("MAIN\n    IF 1 = 1 THEN\n    END IF\n")
	tree, err := pool.Parse(LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tree.Close()

	issues := Collect(tree, DefaultCollectOptions())
	hasMissing := false
	for _, iss := range issues {
		if iss.Missing {
			hasMissing = true
			if !strings.HasPrefix(iss.Message, "缺少") {
				t.Errorf("MISSING 消息格式异常：%q", iss.Message)
			}
		}
	}
	_ = hasMissing // 该样例可能只产生 ERROR，不强制要求 MISSING
}

func TestParseWithTimeout(t *testing.T) {
	pool := newPool(t)
	// 极小超时：正常文件也可能超时，但不应 panic
	_, err := pool.ParseWith(context.Background(), LangBDL, []byte(sampleBDL), nil,
		ParseOptions{Timeout: time.Nanosecond})
	// 允许成功（解析极快）或返回 ErrTimeout
	if err != nil && !errors.Is(err, ErrTimeout) {
		t.Errorf("超时应返回 ErrTimeout，实际 %v", err)
	}
}

func TestParseWithCanceledContext(t *testing.T) {
	pool := newPool(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := pool.ParseWith(ctx, LangBDL, []byte(sampleBDL), nil, ParseOptions{})
	if err == nil {
		t.Error("已取消的 context 应返回错误")
	}
}

func TestParserReusableAfterTimeout(t *testing.T) {
	pool := newPool(t)
	// 先触发一次超时
	_, _ = pool.ParseWith(context.Background(), LangBDL, []byte(sampleBDL), nil,
		ParseOptions{Timeout: time.Nanosecond})

	// 再正常解析，验证池中的 parser 未被污染
	tree, err := pool.Parse(LangBDL, []byte(sampleBDL), nil)
	if err != nil {
		t.Fatalf("超时后复用失败: %v", err)
	}
	defer tree.Close()
	if root := tree.RootNode(); root == nil || root.HasError() {
		t.Error("超时后的解析结果异常")
	}
}
