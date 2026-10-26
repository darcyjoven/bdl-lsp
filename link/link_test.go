package link

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darcyjoven/bdl-lsp/parser"
)

// fakeScanner 让导入逻辑不依赖磁盘：在 mains 集合里的算 MAIN，
// 在 missing 集合里的算"文件不存在"；defines/calls 用于外部函数统计。
type fakeScanner struct {
	mains   map[string]bool
	missing map[string]bool
	defines map[string][]string
}

func (f fakeScanner) Scan(module, code string) FileScan {
	k := strings.ToLower(module + "/" + code)
	if f.missing[k] {
		return FileScan{}
	}
	return FileScan{Found: true, HasMain: f.mains[k], Defines: f.defines[k]}
}

func TestImportCSVDataFacts(t *testing.T) {
	// 复刻真实 CSV 的关键形态：BOM、每列带引号、gal04 有 N、
	// 组内有重复行、有缺列行。
	csv := "\ufeff\"GAL01\",\"GAL02\",\"GAL03\",\"GAL04\"\r\n" +
		"\"prog_a\",\"APM\",\"apmt100\",\"Y\"\r\n" +
		"\"prog_a\",\"APM\",\"apmt100\",\"Y\"\r\n" + // 组内重复
		"\"prog_a\",\"APM\",\"s_helper\",\"Y\"\r\n" +
		"\"prog_b\",\"APM\",\"apmt200\",\"Y\"\r\n" +
		"\"prog_b\",\"APM\",\"apmt200\",\"N\"\r\n" + // 被过滤
		"\"prog_c\",\"APM\",\"apmt300\",\"y\"\r\n" + // 小写 y 也算
		"\"prog_d\",\"APM\",\"orphan1\",\"Y\"\r\n" +
		"\"prog_d\",\"APM\",\"orphan2\",\"Y\"\r\n" +
		"\"prog_e\",\"WPC\",\"ghost\",\"Y\"\r\n" + // 文件不存在
		"\"\",\"APM\",\"bad\",\"Y\"\r\n" // 缺程序编号

	finder := fakeScanner{
		mains: map[string]bool{
			"apm/apmt100": true,
			"apm/apmt200": true,
			"apm/apmt300": true,
		},
		missing: map[string]bool{"wpc/ghost": true},
	}
	res, err := ImportCSV([]byte(csv), ImportOptions{Scanner: finder})
	if err != nil {
		t.Fatal(err)
	}
	m := res.Meta

	if m.RowsTotal != 10 {
		t.Errorf("RowsTotal = %d，期望 10", m.RowsTotal)
	}
	if m.RowsFilteredN != 1 {
		t.Errorf("RowsFilteredN = %d，期望 1", m.RowsFilteredN)
	}
	// 10 行 - 1 行 gal04='N' - 1 行缺程序编号 = 8。
	if m.RowsY != 8 {
		t.Errorf("RowsY = %d，期望 8", m.RowsY)
	}
	if m.MissingFiles != 1 {
		t.Errorf("MissingFiles = %d，期望 1", m.MissingFiles)
	}
	if m.Chains != 3 {
		t.Errorf("Chains = %d，期望 3（prog_a/b/c）", m.Chains)
	}
	if m.Orphans != 2 {
		t.Errorf("Orphans = %d，期望 2（prog_d 无 MAIN、prog_e 全缺失）", m.Orphans)
	}
	// prog_a 组内重复行必须去重成 2 个文件。
	for _, c := range res.Chains {
		if c.Program == "prog_a" && len(c.Files) != 2 {
			t.Errorf("prog_a 文件数 = %d，期望 2（去重后）", len(c.Files))
		}
	}
	// 缺程序编号的行要报成问题，而不是静默丢。
	if len(res.Problems) == 0 || !strings.Contains(strings.Join(res.Problems, "\n"), "缺少程序编号") {
		t.Errorf("缺列行应当报问题，实际 %v", res.Problems)
	}
}

func TestImportCSVAmbiguousAndBOM(t *testing.T) {
	csv := "\ufeffGAL01,GAL02,GAL03,GAL04\n" +
		"grp,APM,one,Y\n" +
		"grp,APM,two,Y\n"
	finder := fakeScanner{mains: map[string]bool{"apm/one": true, "apm/two": true}}
	res, err := ImportCSV([]byte(csv), ImportOptions{Scanner: finder})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Chains) != 1 {
		t.Fatalf("链数 = %d，期望 1", len(res.Chains))
	}
	c := res.Chains[0]
	if !c.Ambiguous || c.Main == "" || len(c.Mains) != 1 {
		t.Fatalf("应标记 ambiguous 且保留两个 MAIN，实际 %+v", c)
	}
	// 两个 MAIN 都要能查到同一条链。
	idx := build([]*Chain{c}, &Meta{})
	if idx.FindByMain(c.Main) == nil {
		t.Error("主 MAIN 应可查")
	}
	if idx.FindByMain(c.Mains[0]) == nil {
		t.Error("次 MAIN 也应可查")
	}
	if res.Meta.Ambiguous != 1 {
		t.Errorf("Ambiguous = %d，期望 1", res.Meta.Ambiguous)
	}
}

func TestImportCSVRejectsBadHeader(t *testing.T) {
	_, err := ImportCSV([]byte("A,B,C\n1,2,3\n"), ImportOptions{})
	if err == nil || !strings.Contains(err.Error(), "GAL01") {
		t.Fatalf("缺表头应当报错，实际 %v", err)
	}
	if _, err := ImportCSV(nil, ImportOptions{}); err == nil {
		t.Fatal("空输入应当报错")
	}
}

func TestScanMainText(t *testing.T) {
	cases := []struct {
		name string
		src  string
		want bool
	}{
		{"普通", "MAIN\nDISPLAY 1\nEND MAIN\n", true},
		{"缩进+注释", "   MAIN   # 入口\n", true},
		{"小写", "main\n", true},
		// `{ }` 是块注释：被它包住的 MAIN 等于被注释掉。
		{"花括号包裹", "{\nMAIN\nEND MAIN\n}\n", false},
		{"代码与注释混排", "MAIN # 入口\nEND MAIN\n", true},
		{"注释后同行", "{ 说明 } MAIN\n", true},
		// `#{` 按语法的规则只是**行**注释（`seq("#", /.*/)`），下一行的 MAIN
		// 仍是代码。语料里 `#{ … }#` 被当成块注释用（533 处），语法树也照此
		// 解析——这是 grammar 的已知缺口，记在 TODO 的 P7-C-2。
		{"井号行注释", "#{\nMAIN\n}#\n", true},
		{"行注释里的", "# MAIN\n", false},
		{"变量名", "LET g_main = 1\n", false},
		{"函数调用", "CALL fgl_main()\n", false},
		{"没有", "FUNCTION f()\nEND FUNCTION\n", false},
	}
	for _, c := range cases {
		if got := ScanMainText([]byte(c.src)); got != c.want {
			t.Errorf("%s: ScanMainText = %v，期望 %v", c.name, got, c.want)
		}
	}
}

func TestStripBOM(t *testing.T) {
	if got := string(stripBOM([]byte("\ufeffabc"))); got != "abc" {
		t.Errorf("BOM 未去除: %q", got)
	}
	if got := string(stripBOM([]byte("abc"))); got != "abc" {
		t.Errorf("无 BOM 时不应改动: %q", got)
	}
}

func TestLayoutDetectAndPath(t *testing.T) {
	root := t.TempDir()
	for _, m := range []string{"apm", "sub"} {
		dir := filepath.Join(root, "tiptop", m, "4gl")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(root, "topcust", "cxm", "4gl"), 0o755); err != nil {
		t.Fatal(err)
	}
	// 大小写不同的文件名：语料里有 `AXMT620.4gl` 这种。
	if err := os.WriteFile(filepath.Join(root, "tiptop", "apm", "4gl", "AXMT620.4gl"), []byte("MAIN\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	l := NewLayout(root, nil)
	// tiptop 必须排第一（决定了同名模组的优先级）。
	roots := l.RootNames()
	if len(roots) != 2 || roots[0] != "tiptop" || roots[1] != "topcust" {
		t.Fatalf("模组根 = %v，期望 [tiptop topcust]", roots)
	}
	if dir, ok := l.ModuleDir("APM"); !ok || !strings.HasSuffix(filepath.ToSlash(dir), "apm") {
		t.Errorf("模组名大小写不敏感查找失败: %q %v", dir, ok)
	}
	// Path 也要大小写不敏感。
	if p, ok := l.Path("apm", "axmt620"); !ok || !strings.HasSuffix(filepath.ToSlash(p), "AXMT620.4gl") {
		t.Errorf("程序代码大小写不敏感查找失败: %q %v", p, ok)
	}
	if _, ok := l.Path("apm", "nope"); ok {
		t.Error("不存在的文件应返回 false")
	}
	// FileID 反推。
	mod, code, ok := l.FileID(filepath.Join(root, "tiptop", "apm", "4gl", "AXMT620.4gl"))
	if !ok || mod != "apm" || code != "axmt620" {
		t.Errorf("FileID = (%q,%q,%v)，期望 (apm,axmt620,true)", mod, code, ok)
	}
	if _, _, ok := l.FileID(filepath.Join(root, "elsewhere", "x.4gl")); ok {
		t.Error("不在模组根下的路径应返回 false")
	}
}

func TestLayoutDuplicateModuleProblem(t *testing.T) {
	root := t.TempDir()
	for _, base := range []string{"tiptop", "topcust"} {
		if err := os.MkdirAll(filepath.Join(root, base, "apm", "4gl"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	l := NewLayout(root, nil)
	l.RootNames() // 触发探测
	problems := l.Problems()
	if len(problems) == 0 || !strings.Contains(strings.Join(problems, "\n"), "apm") {
		t.Fatalf("两个模组根都有 apm 时应报问题，实际 %v", problems)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	chains := []*Chain{
		{Schema: SchemaVersion, Program: "p1", Module: "APM", Main: "apmt100",
			Files: []FileRef{{"APM", "apmt100"}, {"APM", "s_helper"}}},
		{Schema: SchemaVersion, Program: "p2", Module: "SUB", Files: []FileRef{{"SUB", "s_x"}}},
	}
	meta := &Meta{Schema: SchemaVersion, GeneratedAt: "2026-01-02T03:04:05+08:00",
		Chains: 1, Orphans: 1, RowsY: 2}

	if _, err := Load(dir); err != ErrNoData {
		t.Fatalf("没有资料时应返回 ErrNoData，实际 %v", err)
	}
	if err := Save(dir, chains, meta); err != nil {
		t.Fatal(err)
	}
	idx, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if idx.Len() != 2 {
		t.Fatalf("链数 = %d，期望 2", idx.Len())
	}
	if c := idx.FindByMain("apmt100"); c == nil || c.Program != "p1" {
		t.Errorf("FindByMain 失败: %+v", c)
	}
	// 子文件反查：s_helper 属于 p1。
	if cs := idx.ChainsOfFile("APM", "s_helper"); len(cs) != 1 || cs[0].Program != "p1" {
		t.Errorf("ChainsOfFile 失败: %+v", cs)
	}
	// 统计按"有 MAIN 的链"与"orphan"分别计：p1 有 MAIN，p2 没有。
	if st := idx.Stats(); st.Files != 3 || st.Chains != 1 || st.Orphans != 1 {
		t.Errorf("统计 = %+v，期望 3 文件、1 链、1 orphan", st)
	}
	m := idx.Meta()
	if m == nil || m.GeneratedAt != meta.GeneratedAt {
		t.Errorf("meta 未往返: %+v", m)
	}
	if m.GeneratedTime().IsZero() {
		t.Error("GeneratedTime 解析失败")
	}
}

// TestLoadSkipsBlankAndComment 验证空行与注释行被跳过。
func TestLoadSkipsBlankAndComment(t *testing.T) {
	dir := t.TempDir()
	body := `{"schema":1,"program":"ok","module":"APM","main":"ok","files":[["APM","ok"]]}` + "\n\n" +
		"// 注释行\n" +
		`{"schema":1,"program":"p2","module":"APM","main":"ok2","files":[]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, LinksFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	idx, err := Load(dir)
	if err != nil {
		t.Fatalf("空行/注释不应导致失败: %v", err)
	}
	if idx.Len() != 2 {
		t.Errorf("链数 = %d，期望 2", idx.Len())
	}
}

// TestLoadFailsOnCorruptLine 验证坏行**整体失败**而不是静默跳过。
//
// 取舍：静默跳过会让用户拿到"少了若干链"的资料却毫不知情，
// 表现为 function-not-found 冒出一堆误报；整份失败则会明确报
// link-data-invalid 并停用该功能，用户只需重新导入。
func TestLoadFailsOnCorruptLine(t *testing.T) {
	dir := t.TempDir()
	body := `{"schema":1,"program":"ok","module":"APM","main":"ok","files":[["APM","ok"]]}` + "\n" +
		"{坏行\n"
	if err := os.WriteFile(filepath.Join(dir, LinksFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("坏行应导致整体失败")
	}
}

// TestLoadFailsOnFutureSchema 验证高 schema 直接拒绝（避免按旧规则误解新数据）。
func TestLoadFailsOnFutureSchema(t *testing.T) {
	dir := t.TempDir()
	body := `{"schema":99,"program":"future","module":"APM","main":"future","files":[]}` + "\n"
	if err := os.WriteFile(filepath.Join(dir, LinksFileName), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("schema 高于支持值应被拒绝")
	}
}

func TestLoadRejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, LinksFileName), []byte("\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(dir); err == nil {
		t.Fatal("空文件应报错（否则会伪装成'没有资料'）")
	}
}

// fakeParse 用文件名当内容：`fn_a.4gl` 声明函数 a。
func fakeParse(path string) ([]string, error) {
	base := CodeOf(path)
	if strings.HasPrefix(base, "fn_") {
		return []string{strings.TrimPrefix(base, "fn_")}, nil
	}
	return nil, nil
}

func TestResolverVisibility(t *testing.T) {
	root := t.TempDir()
	dirs := map[string]string{}
	for _, m := range []string{"apm", "sub"} {
		d := filepath.Join(root, "tiptop", m, "4gl")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		dirs[m] = d
	}
	write := func(m, code string) {
		if err := os.WriteFile(filepath.Join(dirs[m], code+".4gl"), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("apm", "entry")
	write("apm", "fn_chain") // 链里可见
	write("sub", "fn_global")

	chains := []*Chain{{
		Schema: SchemaVersion, Program: "entry", Module: "APM", Main: "entry",
		Files: []FileRef{{"APM", "entry"}, {"APM", "fn_chain"}},
	}}
	idx := build(chains, &Meta{})
	layout := NewLayout(root, nil)
	r := NewResolver(idx, layout, []string{"sub"}, fakeParse)
	// 测试里手动就绪并注入全局函数（生产环境由 Start 后台预热）。
	r.sym.mu.Lock()
	r.sym.ready = true
	r.sym.global = map[string]bool{"global": true}
	r.sym.mu.Unlock()

	set, complete := r.VisibleFunctions(filepath.Join(dirs["apm"], "entry.4gl"), true)
	if !complete {
		t.Fatal("文件齐全时应 complete=true")
	}
	if !set["chain"] {
		t.Errorf("链文件里的函数应可见: %v", SortedKeys(set))
	}
	if !set["global"] {
		t.Errorf("全局模组的函数应可见: %v", SortedKeys(set))
	}

	// 链里有缺失文件 → 可见集不完整。
	chains2 := []*Chain{{
		Schema: SchemaVersion, Program: "entry", Module: "APM", Main: "entry",
		Files: []FileRef{{"APM", "entry"}, {"WPC", "gone"}},
	}}
	r2 := NewResolver(build(chains2, &Meta{}), layout, []string{"sub"}, fakeParse)
	r2.sym.mu.Lock()
	r2.sym.ready = true
	r2.sym.global = map[string]bool{"global": true}
	r2.sym.mu.Unlock()
	if _, complete := r2.VisibleFunctions(filepath.Join(dirs["apm"], "entry.4gl"), true); complete {
		t.Error("链里有缺失文件时必须 complete=false")
	}

	// 不在链接资料里的 MAIN 程序 → 它的链未知 → 不完整。
	if _, complete := r.VisibleFunctions(filepath.Join(dirs["apm"], "fn_chain.4gl"), true); complete {
		t.Error("链接资料里查不到的 MAIN 程序必须 complete=false")
	}

	// 没就绪 → 不完整。
	r3 := NewResolver(idx, layout, []string{"sub"}, fakeParse)
	if _, complete := r3.VisibleFunctions(filepath.Join(dirs["apm"], "entry.4gl"), true); complete {
		t.Error("索引未就绪时必须 complete=false")
	}
}

func TestResolverNonMainUnion(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "tiptop", "apm", "4gl")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, c := range []string{"fn_a", "fn_b", "fn_shared"} {
		if err := os.WriteFile(filepath.Join(d, c+".4gl"), []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// 同一子文件被两条链共享：本文件 + 两条链的并集。
	chains := []*Chain{
		{Schema: SchemaVersion, Program: "p1", Module: "APM", Main: "fn_a",
			Files: []FileRef{{"APM", "fn_a"}, {"APM", "fn_shared"}}},
		{Schema: SchemaVersion, Program: "p2", Module: "APM", Main: "fn_b",
			Files: []FileRef{{"APM", "fn_b"}, {"APM", "fn_shared"}}},
	}
	idx := build(chains, &Meta{})
	if got := len(idx.ChainsOfFile("APM", "fn_shared")); got != 2 {
		t.Fatalf("共享文件应反查到 2 条链，实际 %d", got)
	}
	r := NewResolver(idx, NewLayout(root, nil), nil, fakeParse)
	r.sym.mu.Lock()
	r.sym.ready = true
	r.sym.global = map[string]bool{}
	r.sym.mu.Unlock()

	set, complete := r.VisibleFunctions(filepath.Join(d, "fn_shared.4gl"), false)
	if !complete {
		t.Fatal("子文件的两个链都完整时应 complete=true")
	}
	// 子文件 + 它的链 → a 与 b 的并集都可见。
	for _, want := range []string{"shared", "a", "b", "global_placeholder"} {
		if want == "global_placeholder" {
			continue // 默认全局模组不存在，不算错
		}
		if !set[want] {
			t.Errorf("%s 应可见: %v", want, SortedKeys(set))
		}
	}
}

func TestSymbolIndexSyncAndAsync(t *testing.T) {
	root := t.TempDir()
	var paths []string
	for _, c := range []string{"fn_a", "fn_b", "fn_c"} {
		p := filepath.Join(root, c+".4gl")
		if err := os.WriteFile(p, []byte("x\n"), 0o600); err != nil {
			t.Fatal(err)
		}
		paths = append(paths, p)
	}
	s := NewSymbolIndex(fakeParse)
	set, complete := s.Functions(paths, 10)
	if !complete || !set["a"] || !set["b"] || !set["c"] {
		t.Fatalf("同步预算内应全部完成: complete=%v set=%v", complete, SortedKeys(set))
	}
	if s.CachedFiles() != 3 {
		t.Errorf("缓存 = %d，期望 3", s.CachedFiles())
	}

	// 超预算：立即返回不完整，后台补齐。
	s2 := NewSymbolIndex(fakeParse)
	done := make(chan struct{}, 4)
	s2.SetOnReady(func() { done <- struct{}{} })
	if _, complete := s2.Functions(paths, 1); complete {
		t.Error("超过同步预算时必须 complete=false")
	}
	select {
	case <-done:
	case <-timeAfter(t):
		t.Fatal("后台解析未触发回调")
	}
}

// timeAfter 是 time.After 的小包装，避免在测试里到处 import time。
func timeAfter(t *testing.T) <-chan time.Time {
	t.Helper()
	return time.After(5 * time.Second)
}

// TestImportExternals 验证"被调用但从无定义"的名字进了外部函数表，
// 而被 ≥1 个文件定义的、出现次数太少的都不进。
func TestImportExternals(t *testing.T) {
	csv := "GAL01,GAL02,GAL03,GAL04\n" +
		"p1,APM,apmt100,Y\n" +
		"p2,APM,apmt200,Y\n" +
		"p3,APM,apmt300,Y\n"
	sc := fakeScanner{
		mains: map[string]bool{"apm/apmt100": true, "apm/apmt200": true, "apm/apmt300": true},
		defines: map[string][]string{
			// lib_helper 由 apmt100 定义 → 即使被调用也不算外部
			"apm/apmt100": {"lib_helper"},
		},
	}
	corpus := &CorpusIndex{
		Defines: map[string]bool{"lib_helper": true},
		Calls: map[string]*CallStat{
			"lib_helper":  {Files: 3, Calls: 40}, // 有定义 → 排除
			"cl_err3":     {Files: 3, Calls: 90},
			"cl_qbe_init": {Files: 2, Calls: 900}, // 文件少但调用多 → 也算
			"typo_once":   {Files: 1, Calls: 2},   // 太少 → 不算
		},
	}
	res, err := ImportCSV([]byte(csv), ImportOptions{Scanner: sc, Corpus: corpus})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, e := range res.Meta.Externals {
		got[e.Name] = e.Files
	}
	if got["lib_helper"] != 0 {
		t.Errorf("lib_helper 有定义，不该进外部函数表：%v", got)
	}
	if got["typo_once"] != 0 {
		t.Errorf("只被 1 个文件调用，不该进外部函数表：%v", got)
	}
	if got["cl_err3"] != 3 {
		t.Errorf("cl_err3 被 3 个文件调用，应入表：%v", got)
	}
	// 双阈值：cl_qbe_init 只被 2 个文件调用，但调用 900 次，同样入表。
	if got["cl_qbe_init"] != 2 {
		t.Errorf("cl_qbe_init 调用次数远超阈值，应入表：%v", got)
	}
	if res.Meta.ExternalsTotal != 2 {
		t.Errorf("externalsTotal = %d，期望 2", res.Meta.ExternalsTotal)
	}
	if res.Meta.ExternalsMinFiles != DefaultExternalsMinFiles {
		t.Errorf("externalsMinFiles = %d，期望 %d", res.Meta.ExternalsMinFiles, DefaultExternalsMinFiles)
	}
}

// TestImportExternalsIgnore 验证内置名不记入外部函数表。
func TestImportExternalsIgnore(t *testing.T) {
	csv := "GAL01,GAL02,GAL03,GAL04\np1,APM,apmt100,Y\np2,APM,apmt200,Y\np3,APM,apmt300,Y\n"
	sc := fakeScanner{
		mains: map[string]bool{"apm/apmt100": true, "apm/apmt200": true, "apm/apmt300": true},
	}
	corpus := &CorpusIndex{
		Defines: map[string]bool{},
		Calls: map[string]*CallStat{
			"fgl_getenv": {Files: 3, Calls: 30},
			"arg_val":    {Files: 3, Calls: 30},
		},
	}
	res, err := ImportCSV([]byte(csv), ImportOptions{
		Scanner: sc,
		Corpus:  corpus,
		Ignore:  map[string]bool{"fgl_getenv": true, "arg_val": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Meta.Externals) != 0 || res.Meta.ExternalsTotal != 0 {
		t.Errorf("内置名不该进外部函数表：%+v", res.Meta.Externals)
	}
}

// TestImportExternalsTooMany 验证"太多就不启用"：超过 MaxExternals 时
// 名单照记，但标记 skipped，解析器据此**不做**豁免。
func TestImportExternalsTooMany(t *testing.T) {
	var csv strings.Builder
	csv.WriteString("GAL01,GAL02,GAL03,GAL04\n")
	sc := fakeScanner{mains: map[string]bool{}}
	corpus := &CorpusIndex{Defines: map[string]bool{}, Calls: map[string]*CallStat{}}
	for i := 0; i < 3; i++ {
		code := fmt.Sprintf("apmt%03d", i)
		fmt.Fprintf(&csv, "p%d,APM,%s,Y\n", i, code)
		sc.mains["apm/"+code] = true
	}
	for j := 0; j <= MaxExternals; j++ {
		corpus.Calls[fmt.Sprintf("ext_%04d", j)] = &CallStat{Files: 3, Calls: 9}
	}
	res, err := ImportCSV([]byte(csv.String()), ImportOptions{Scanner: sc, Corpus: corpus})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Meta.ExternalsSkipped {
		t.Fatalf("超过上限应标记 externalsSkipped")
	}
	if len(res.Meta.Externals) > MaxStoredExternals {
		t.Errorf("存进 meta 的名单应被截断到 %d，实际 %d", MaxStoredExternals, len(res.Meta.Externals))
	}
	// 解析器必须忽略这份被标记的名单。
	idx := &Index{meta: res.Meta}
	if names := externalNames(idx); len(names) != 0 {
		t.Errorf("skipped 的名单不该生效，实际 %d 个", len(names))
	}
}

// TestResolverUsesExternals 验证外部函数被当成"存在"，而普通未知名仍然报。
func TestResolverUsesExternals(t *testing.T) {
	root := t.TempDir()
	d := filepath.Join(root, "tiptop", "apm", "4gl")
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "apmt100.4gl"), []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	chains := []*Chain{{
		Schema: SchemaVersion, Program: "apmt100", Module: "APM", Main: "apmt100",
		Files: []FileRef{{"APM", "apmt100"}},
	}}
	idx := build(chains, &Meta{
		Externals:         []External{{Name: "cl_err3", Files: 186, Calls: 3200}},
		ExternalsTotal:    1,
		ExternalsMinFiles: DefaultExternalsMinFiles,
	})
	layout := NewLayout(root, nil)
	r := NewResolver(idx, layout, []string{"apm"}, fakeParse)
	r.sym.mu.Lock()
	r.sym.ready = true
	r.sym.global = map[string]bool{}
	r.sym.mu.Unlock()

	set, complete := r.VisibleFunctions(filepath.Join(d, "apmt100.4gl"), true)
	if !complete {
		t.Fatalf("文件齐全时应 complete=true")
	}
	if !set["cl_err3"] {
		t.Errorf("外部函数 cl_err3 应可见：%v", SortedKeys(set))
	}
	if set["cl_err_typo"] {
		t.Errorf("未知名不该可见")
	}
	if n := r.ExternalTotal(); n != 1 {
		t.Errorf("ExternalTotal = %d，期望 1", n)
	}
	if got := r.ExternalFunctions(); !got["cl_err3"] {
		t.Errorf("ExternalFunctions 应含 cl_err3：%v", got)
	}
}

// TestScanCorpusFacts 验证全语料扫描能同时拿到 MAIN/定义/调用。
func TestScanCorpusFacts(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "tiptop", "apm", "4gl")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"apmt100": "MAIN\n  CALL cl_err3(\"x\")\n  CALL cl_err3(\"y\")\nEND MAIN\n",
		"apmlib":  "FUNCTION helper()\n  LET x = 1\nEND FUNCTION\n",
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name+".4gl"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	layout := NewLayout(root, nil)
	got, err := ScanCorpus(layout, pool, 1)
	if err != nil {
		t.Fatal(err)
	}
	if got.Files != 2 {
		t.Errorf("扫描文件数 = %d，期望 2", got.Files)
	}
	if s := got.Scan("APM", "apmt100"); !s.Found || !s.HasMain {
		t.Errorf("apmt100 应存在且有 MAIN：%+v", s)
	}
	if !got.Defines["helper"] {
		t.Errorf("helper 应被识别为定义：%v", got.Defines)
	}
	if st := got.Calls["cl_err3"]; st == nil || st.Calls != 2 || st.Files != 1 {
		t.Errorf("cl_err3 调用统计不对：%+v", st)
	}

	// 进度回调：至少要有 0/total 与 total/total 两次（CLI 靠它显示进度）。
	var mu sync.Mutex
	var seen [][2]int
	got2, err := ScanCorpusWithProgress(layout, pool, 1, func(done, total int) {
		mu.Lock()
		seen = append(seen, [2]int{done, total})
		mu.Unlock()
	})
	if err != nil {
		t.Fatal(err)
	}
	if got2.Files != 2 {
		t.Fatalf("带进度扫描文件数 = %d，期望 2", got2.Files)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(seen) < 2 {
		t.Fatalf("进度回调次数 = %d，期望至少 2（0 与完成）", len(seen))
	}
	if seen[0] != [2]int{0, 2} {
		t.Errorf("首次进度 = %v，期望 [0 2]", seen[0])
	}
	if seen[len(seen)-1] != [2]int{2, 2} {
		t.Errorf("末次进度 = %v，期望 [2 2]", seen[len(seen)-1])
	}
}

// TestResolveDataDir 钉住 W6 的目录选择规则：
// 新布局优先 → 旧布局回退 → 都不存在时新布局；显式配置优先（不回退）。
func TestResolveDataDir(t *testing.T) {
	root := t.TempDir()
	write := func(rel string) {
		t.Helper()
		dir := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, LinksFileName), []byte("{}\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	if got := ResolveDataDir(root, ""); got != DefaultDataDir {
		t.Fatalf("没有任何资料时应返回默认新目录，实际 %q", got)
	}

	write(".link")
	if got := ResolveDataDir(root, ""); got != ".link" {
		t.Fatalf("只有 .link 时应回退 .link，实际 %q", got)
	}

	write(".project")
	if got := ResolveDataDir(root, ""); got != ".project" {
		t.Fatalf("只有 .project 根时应回退 .project，实际 %q", got)
	}

	write(".project/link")
	if got := ResolveDataDir(root, ""); got != DefaultDataDir {
		t.Fatalf("新布局存在时应优先新目录，实际 %q", got)
	}

	if got := ResolveDataDir(root, "custom/dir"); got != "custom/dir" {
		t.Fatalf("显式配置应原样返回（不回退），实际 %q", got)
	}
}
