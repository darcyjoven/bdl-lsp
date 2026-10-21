package globals

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

// setup 在临时目录里构造文件树并返回 Resolver。
//
//	dir/
//	  app/4gl/main.4gl          ← 引用方文档
//	  config/top.global         ← 存在的 global 文件
//	  alt/config/top.global     ← searchPaths 命中用的第二个位置
func setup(t *testing.T) (root, docPath string, r *Resolver) {
	t.Helper()

	root = t.TempDir()
	appDir := filepath.Join(root, "app", "4gl")
	cfgDir := filepath.Join(root, "config")
	altDir := filepath.Join(root, "alt", "config")
	for _, d := range []string{appDir, cfgDir, altDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	// global 文件：块内两个变量，其中一个 record
	g := `DATABASE ds
GLOBALS
   DEFINE g_counter INTEGER
   DEFINE g_rec RECORD
       id INTEGER
   END RECORD
END GLOBALS
`
	if err := os.WriteFile(filepath.Join(cfgDir, "top.global"), []byte(g), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(altDir, "top.global"), []byte(g), 0o644); err != nil {
		t.Fatal(err)
	}

	// 引用方文档（内容不影响 Resolver，路径才影响）
	docPath = filepath.Join(appDir, "main.4gl")
	if err := os.WriteFile(docPath, []byte("MAIN\nEND MAIN\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pool, err := parser.New()
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	t.Cleanup(pool.Close)

	r = NewResolver(pool, root, nil, nil)
	return root, docPath, r
}

func docURI(path string) string { return URIOfPath(path) }

func imp(literal string) index.GlobalsImport {
	return index.GlobalsImport{Literal: literal}
}

func TestResolveRelativePath(t *testing.T) {
	_, doc, r := setup(t)

	// app/4gl/main.4gl + ../../config/top.global → root/config/top.global ✓
	entries := r.Resolve(context.Background(), docURI(doc), []index.GlobalsImport{
		imp("../../config/top.global"),
	})
	if len(entries) != 1 {
		t.Fatalf("应返回 1 条，实际 %d", len(entries))
	}
	e := entries[0]
	if e.Status != StatusResolved {
		t.Fatalf("期望 Resolved，实际 %v（path=%q）", e.Status, e.Path)
	}
	if want := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(doc))), "config", "top.global"); e.Path != want {
		t.Errorf("路径期望 %q，实际 %q", want, e.Path)
	}
	if e.Index == nil {
		t.Fatal("Resolved 时 Index 不应为 nil")
	}
	// 块内符号应以 ScopeGlobals 收集
	var count int
	for _, sym := range e.Index.Symbols {
		if sym.Scope == index.ScopeGlobals {
			count++
		}
	}
	if count < 2 { // g_counter + g_rec（+ record 字段也算 globals 作用域）
		t.Errorf("global 符号数 %d，期望 ≥2", count)
	}
}

func TestResolveNotFound(t *testing.T) {
	_, doc, r := setup(t)

	entries := r.Resolve(context.Background(), docURI(doc), []index.GlobalsImport{
		imp("../../config/missing.global"),
	})
	if entries[0].Status != StatusNotFound {
		t.Errorf("期望 NotFound，实际 %v", entries[0].Status)
	}
	if entries[0].Path != "" || entries[0].Index != nil {
		t.Error("NotFound 时 Path/Index 应为空")
	}
}

func TestResolveUnresolvableVariable(t *testing.T) {
	_, doc, r := setup(t)

	v := index.GlobalsImport{IsVariable: true, Name: "g_file"}
	entries := r.Resolve(context.Background(), docURI(doc), []index.GlobalsImport{v})
	if entries[0].Status != StatusUnresolvable {
		t.Errorf("期望 Unresolvable，实际 %v", entries[0].Status)
	}
}

func TestResolveSearchPaths(t *testing.T) {
	root, doc, _ := setup(t)
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	// searchPaths 指向 alt/config：字面量 top.global 在 R2 落空后由 R3 命中
	r := NewResolver(pool, root, []string{"alt/config"}, nil)

	entries := r.Resolve(context.Background(), docURI(doc), []index.GlobalsImport{
		imp("top.global"), // app/4gl/ 下没有 → 走 searchPaths
	})
	e := entries[0]
	if e.Status != StatusResolved {
		t.Fatalf("期望 Resolved，实际 %v", e.Status)
	}
	if want := filepath.Join(root, "alt", "config", "top.global"); e.Path != want {
		t.Errorf("searchPaths 命中路径期望 %q，实际 %q", want, e.Path)
	}
}

func TestResolveAbsoluteLiteral(t *testing.T) {
	_, _, r := setup(t)
	abs := filepath.Join(filepath.Dir(filepath.Dir(URIOfPath(""))), "") // 占位，下面用真实路径
	_ = abs

	// 用 setup 里已知存在的文件构造绝对路径
	dir := t.TempDir()
	target := filepath.Join(dir, "x.global")
	if err := os.WriteFile(target, []byte("GLOBALS\n   DEFINE g_a INTEGER\nEND GLOBALS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := r.Resolve(context.Background(), "file:///does/not/matter.4gl", []index.GlobalsImport{
		imp(target),
	})
	if entries[0].Status != StatusResolved {
		t.Errorf("绝对路径应 Resolved，实际 %v", entries[0].Status)
	}
}

func TestResolveUnknownExtensionStillParsed(t *testing.T) {
	// 语料实测：目标有 .global/.4gl/.inc —— 扩展名不决定可否解析
	dir := t.TempDir()
	inc := filepath.Join(dir, "shared.inc")
	if err := os.WriteFile(inc, []byte("GLOBALS\n   DEFINE g_inc INTEGER\nEND GLOBALS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := NewResolver(pool, dir, nil, nil)

	entries := r.Resolve(context.Background(), docURI(filepath.Join(dir, "main.4gl")), []index.GlobalsImport{
		imp("shared.inc"),
	})
	if entries[0].Status != StatusResolved {
		t.Fatalf("期望 Resolved，实际 %v", entries[0].Status)
	}
	found := false
	for _, sym := range entries[0].Index.Symbols {
		if sym.Name == "g_inc" && sym.Scope == index.ScopeGlobals {
			found = true
		}
	}
	if !found {
		t.Error(".inc 目标里的 GLOBALS 符号未提取")
	}
}

func TestCacheReuseAndInvalidate(t *testing.T) {
	_, doc, r := setup(t)
	ctx := context.Background()
	docu := docURI(doc)

	r.Resolve(ctx, docu, []index.GlobalsImport{imp("../../config/top.global")})
	if got := r.Stats(); got != 1 {
		t.Fatalf("缓存条目期望 1，实际 %d", got)
	}

	// 未变更：复用缓存（无法直接观测，用 mtime 未变 + 二次调用不报错间接验证）
	r.Resolve(ctx, docu, []index.GlobalsImport{imp("../../config/top.global")})
	if got := r.Stats(); got != 1 {
		t.Errorf("重复解析不应增加缓存条目，实际 %d", got)
	}

	// 修改文件（mtime+size 变化）→ 缓存应重建
	target := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(doc))), "config", "top.global")
	if err := os.WriteFile(target, []byte("GLOBALS\n   DEFINE g_counter INTEGER\n   DEFINE g_new INTEGER\nEND GLOBALS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	entries := r.Resolve(ctx, docu, []index.GlobalsImport{imp("../../config/top.global")})
	hasNew := false
	for _, sym := range entries[0].Index.Symbols {
		if sym.Name == "g_new" {
			hasNew = true
		}
	}
	if !hasNew {
		t.Error("文件变更后缓存未失效（g_new 不可见）")
	}

	// Refresh 显式失效
	r.Refresh(target)
	r.Resolve(ctx, docu, []index.GlobalsImport{imp("../../config/top.global")})
	if got := r.Stats(); got != 1 {
		t.Errorf("Refresh 后应只有 1 个条目，实际 %d", got)
	}
}

func TestOpenDocPreferredOverDisk(t *testing.T) {
	_, doc, _ := setup(t)
	target := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(doc))), "config", "top.global")
	targetURI := URIOfPath(target)

	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)

	// 内存版与磁盘版不同：内存版多一个变量
	memText := "GLOBALS\n   DEFINE g_counter INTEGER\n   DEFINE g_memonly INTEGER\nEND GLOBALS\n"
	r := NewResolver(pool, "", nil, func(uri string) ([]byte, bool) {
		if uri == targetURI {
			return []byte(memText), true
		}
		return nil, false
	})

	entries := r.Resolve(context.Background(), docURI(doc), []index.GlobalsImport{imp("../../config/top.global")})
	if entries[0].Status != StatusResolved {
		t.Fatalf("期望 Resolved，实际 %v", entries[0].Status)
	}
	hasMem := false
	for _, sym := range entries[0].Index.Symbols {
		if sym.Name == "g_memonly" {
			hasMem = true
		}
	}
	if !hasMem {
		t.Error("应优先使用已打开文档的内存版本")
	}
}

func TestImportersReverseDependency(t *testing.T) {
	_, doc, r := setup(t)
	ctx := context.Background()
	docu := docURI(doc)
	target := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(doc))), "config", "top.global")

	if got := r.Importers(target); len(got) != 0 {
		t.Fatalf("解析前不应有引用者，实际 %v", got)
	}
	r.Resolve(ctx, docu, []index.GlobalsImport{imp("../../config/top.global")})

	got := r.Importers(target)
	if len(got) != 1 || got[0] != docu {
		t.Fatalf("引用者期望 [%s]，实际 %v", docu, got)
	}

	// 导入列表变为空 → 旧引用边应被清除
	r.Resolve(ctx, docu, nil)
	if got := r.Importers(target); len(got) != 0 {
		t.Errorf("导入清空后引用边应移除，实际 %v", got)
	}
}

func TestCircularImportsTerminated(t *testing.T) {
	// A 引 B，B 引 A：应被缓存挡住，不无限递归（用测试超时兜底）
	dir := t.TempDir()
	a := filepath.Join(dir, "a.global")
	b := filepath.Join(dir, "b.global")
	if err := os.WriteFile(a, []byte(`GLOBALS "b.global"`+"\nGLOBALS\n   DEFINE g_a INTEGER\nEND GLOBALS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(b, []byte(`GLOBALS "a.global"`+"\nGLOBALS\n   DEFINE g_b INTEGER\nEND GLOBALS\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := NewResolver(pool, dir, nil, nil)

	done := make(chan struct{})
	go func() {
		defer close(done)
		entries := r.Resolve(context.Background(), docURI(a), []index.GlobalsImport{imp("b.global")})
		if entries[0].Status != StatusResolved {
			t.Errorf("期望 Resolved，实际 %v", entries[0].Status)
		}
	}()
	select {
	case <-done:
		// 正常终止
	case <-time.After(5 * time.Second):
		t.Fatal("环引用导致无限递归")
	}
}

func TestChainImportsResolved(t *testing.T) {
	// a.4gl → mid.global → top.global：两级都应进缓存与反向依赖
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "top.global"),
		[]byte("GLOBALS\n   DEFINE g_top INTEGER\nEND GLOBALS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mid.global"),
		[]byte(`GLOBALS "top.global"`+"\nGLOBALS\n   DEFINE g_mid INTEGER\nEND GLOBALS\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	r := NewResolver(pool, dir, nil, nil)

	r.Resolve(context.Background(), docURI(filepath.Join(dir, "a.4gl")), []index.GlobalsImport{imp("mid.global")})

	if got := r.Stats(); got != 2 {
		t.Errorf("链式导入应缓存 2 个文件，实际 %d", got)
	}
	if got := r.Importers(filepath.Join(dir, "top.global")); len(got) != 1 {
		t.Errorf("top.global 的引用者应为 mid.global，实际 %v", got)
	} else if got[0] != docURI(filepath.Join(dir, "mid.global")) {
		t.Errorf("引用者 URI 不符: %v", got[0])
	}
}

// TestURIPathRoundTrip 覆盖 URI ↔ 路径往返（实现已委托 internal/fileuri）。
//
// 注意 Windows 段：fileuri.FromPath 按 VSCode 的形态把盘符冒号编码成
// `%3A`（`file:///C%3A/work/x.4gl`）。这是 P7-A-2 修正的一部分——
// 旧实现不编码冒号，而 VSCode 会编码，两者不一致导致 Zed 能用、VSCode 不能用。
// 现在服务端统一生成 `%3A` 形态，两种客户端都能正确解析。
func TestURIPathRoundTrip(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Windows 上做完整往返：编码、解码都必须还原原始路径
		for _, p := range []string{
			`C:\work\x.4gl`,
			`C:\a b\y.4gl`,
			`C:\中文\z.4gl`,
		} {
			uri := URIOfPath(p)
			if back := PathOfURI(uri); back != p {
				t.Errorf("往返失败：%q → %q → %q", p, uri, back)
			}
		}
		return
	}

	// POSIX：正斜杠路径完全可逆
	cases := []struct{ path, uri string }{
		{"/code/workspace/x.4gl", "file:///code/workspace/x.4gl"},
		{"/code/a b/x.4gl", "file:///code/a%20b/x.4gl"},
		{"/tmp/a#b/x.4gl", "file:///tmp/a%23b/x.4gl"},
	}
	for _, c := range cases {
		if got := URIOfPath(c.path); got != c.uri {
			t.Errorf("URIOfPath(%q) = %q，期望 %q", c.path, got, c.uri)
		}
		if got := PathOfURI(c.uri); got != c.path {
			t.Errorf("PathOfURI(%q) = %q，期望 %q", c.uri, got, c.path)
		}
	}

	// Windows 路径的编码形态：盘符冒号必须是 %3A（与 vscode-uri 一致）
	if got := URIOfPath(`C:\work\x.4gl`); got != "file:///C%3A/work/x.4gl" {
		t.Errorf("URIOfPath(Windows) = %q，期望 %q", got, "file:///C%3A/work/x.4gl")
	}
}
