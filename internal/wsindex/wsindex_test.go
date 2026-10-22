package wsindex

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

// newFixture 建一个 tiptop 布局的临时工作区：
//
//	tiptop/aap/4gl/aapr100.4gl  MAIN + CALL aapr200 + FUNCTION aapr100_help
//	tiptop/aap/4gl/aapr200.4gl  FUNCTION aapr200
//	topcust/cap/4gl/capr100.4gl CALL aapr200（跨模组调用）
func newFixture(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("tiptop/aap/4gl/aapr100.4gl", `MAIN
  CALL aapr200()
END MAIN

FUNCTION aapr100_help()
END FUNCTION
`)
	write("tiptop/aap/4gl/aapr200.4gl", `FUNCTION aapr200()
  DISPLAY "x"
END FUNCTION
`)
	write("topcust/cap/4gl/capr100.4gl", `MAIN
  CALL aapr200()
END MAIN
`)
	return root
}

func newBindings(t *testing.T) Bindings {
	t.Helper()
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	calls, err := index.NewCallCollector(pool)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(calls.Close)
	return Bindings{Pool: pool, Calls: calls}
}

func TestBuildAndQuery(t *testing.T) {
	root := newFixture(t)
	b := newBindings(t)
	ix, stats, err := Build(context.Background(), b, Options{Root: root, Grammar: "test"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 3 || stats.Parsed != 3 {
		t.Fatalf("统计 = %+v，期望 3 个文件全部解析", stats)
	}

	// 声明：aapr200 属于 aap 模组
	decls := ix.Lookup("aapr200")
	if len(decls) != 1 {
		t.Fatalf("aapr200 声明数 = %d，期望 1", len(decls))
	}
	if decls[0].Module != "aap" {
		t.Errorf("模组 = %q，期望 aap", decls[0].Module)
	}
	if decls[0].Line != 0 || decls[0].Col != 9 {
		t.Errorf("位置 = %d:%d，期望 0:9", decls[0].Line, decls[0].Col)
	}

	// MAIN 是 program 类的顶层符号
	if got := ix.Lookup("aapr100"); len(got) != 1 || got[0].Kind != string(index.KindMain) {
		t.Errorf("MAIN 程序符号 = %+v", got)
	}

	// 调用点跨文件：aapr100（aap）与 capr100（cap）都调用了 aapr200
	calls := ix.CallsOf("aapr200")
	if len(calls) != 2 {
		t.Fatalf("调用点数 = %d，期望 2：%+v", len(calls), calls)
	}

	// 子串搜索
	if got := ix.Search("aapr1", 0); len(got) != 2 { // aapr100 + aapr100_help
		t.Errorf("Search(aapr1) = %d 条，期望 2", len(got))
	}
	if got := ix.Search("", 2); len(got) != 2 {
		t.Errorf("limit 未生效：%d 条", len(got))
	}
}

func TestBuildExcludesIgnoredFiles(t *testing.T) {
	root := newFixture(t)
	b := newBindings(t)
	ix, stats, err := Build(context.Background(), b, Options{
		Root:    root,
		Grammar: "test",
		Exclude: []string{"tiptop/aap/4gl/aapr2*"},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 2 {
		t.Fatalf("排除后文件数 = %d，期望 2", stats.Total)
	}
	if got := ix.Lookup("aapr200"); len(got) != 0 {
		t.Errorf("被排除的文件不应进索引: %+v", got)
	}
	if got := ix.Lookup("aapr100"); len(got) != 1 {
		t.Errorf("未被排除的文件仍应在索引里")
	}
}

func TestBuildSkipsInvalidExclude(t *testing.T) {
	root := newFixture(t)
	b := newBindings(t)
	var logged []string
	_, stats, err := Build(context.Background(), b, Options{
		Root:    root,
		Grammar: "test",
		Exclude: []string{"aapr2[", "tiptop/aap/4gl/aapr2*"},
		Logf: func(format string, args ...any) {
			logged = append(logged, fmt.Sprintf(format, args...))
		},
	}, nil)
	if err != nil {
		t.Fatalf("非法排除模式不应让索引构建失败: %v", err)
	}
	if stats.Total != 2 {
		t.Fatalf("排除后文件数 = %d，期望 2（非法模式被跳过，合法模式仍生效）", stats.Total)
	}
	if len(logged) == 0 {
		t.Error("非法模式应通过 Logf 记录")
	}
}

func TestRebuildReusesUnchangedFiles(t *testing.T) {
	root := newFixture(t)
	b := newBindings(t)
	first, _, err := Build(context.Background(), b, Options{Root: root, Grammar: "g1"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	// 全未变：全部复用
	second, stats, err := Build(context.Background(), b, Options{Root: root, Grammar: "g1"}, first)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Reused != 3 || stats.Parsed != 0 {
		t.Fatalf("第二次统计 = %+v，期望 3 复用 0 解析", stats)
	}
	if len(second.Lookup("aapr200")) != 1 {
		t.Error("复用后应仍能查到 aapr200")
	}

	// 改一个文件：只有它重解析
	p := filepath.Join(root, "tiptop/aap/4gl/aapr200.4gl")
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("FUNCTION aapr200()\nEND FUNCTION\n\nFUNCTION aapr300()\nEND FUNCTION\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// mtime 精度：确保与旧值不同（部分文件系统只到秒）
	_ = os.Chtimes(p, fi.ModTime().Add(1e9), fi.ModTime().Add(1e9))

	third, stats, err := Build(context.Background(), b, Options{Root: root, Grammar: "g1"}, second)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Parsed != 1 || stats.Reused != 2 {
		t.Fatalf("第三次统计 = %+v，期望 1 解析 2 复用", stats)
	}
	if len(third.Lookup("aapr300")) != 1 {
		t.Error("改动的文件应重新索引出 aapr300")
	}

	// 解析器版本变了：整份作废
	_, stats, err = Build(context.Background(), b, Options{Root: root, Grammar: "g2"}, third)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Parsed != 3 || stats.Reused != 0 {
		t.Fatalf("grammar 变化后统计 = %+v，期望全部重解析", stats)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	root := newFixture(t)
	b := newBindings(t)
	ix, _, err := Build(context.Background(), b, Options{Root: root, Grammar: "g"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "sub", "index.idx")
	if err := Save(path, ix); err != nil {
		t.Fatal(err)
	}
	back, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if back.FileCount() != ix.FileCount() || len(back.Lookup("aapr200")) != 1 {
		t.Fatalf("往返后内容不一致")
	}
	if len(back.CallsOf("aapr200")) != 2 {
		t.Errorf("往返后调用点丢失")
	}
}

func TestBuildCancelled(t *testing.T) {
	root := newFixture(t)
	b := newBindings(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := Build(ctx, b, Options{Root: root, Grammar: "g"}, nil); err == nil {
		t.Fatal("已取消的 context 应返回错误")
	}
}

func TestModuleOf(t *testing.T) {
	cases := map[string]string{
		"/w/tiptop/aap/4gl/x.4gl":  "aap",
		"/w/topcust/cap/4gl/x.4gl": "cap",
		"/w/tiptop/aap/per/x.per":  "aap",
		"/w/whatever/x.4gl":        "",
	}
	for path, want := range cases {
		if got := moduleOf(path); got != want {
			t.Errorf("moduleOf(%q) = %q，期望 %q", path, got, want)
		}
	}
}
