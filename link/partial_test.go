package link

// P1-3：局部更新（-modules / -files）的选择与合并。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darcyjoven/bdl-lsp/parser"
)

const partialCSV = `GAL01,GAL02,GAL03,GAL04
PROG1,APM,apmp100,Y
PROG1,AAP,saapt310,Y
PROG2,APM,apmp200,Y
PROG2,AAP,saapt310,Y
PROG3,AXC,axcp500,Y
NOPE,APM,apmp999,Y
OFF,APM,apmp111,N
`

func sortedNames(sel *PartialSelection) []string {
	names := sel.ProgramNames()
	out := append([]string(nil), names...)
	for i := 1; i < len(out); i++ { // 插入排序：数量很小
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

func TestSelectPrograms(t *testing.T) {
	cases := []struct {
		name        string
		modules     []string
		specs       []string
		want        string // 逗号连接的期望程序（已排序）
		wantMatched int
	}{
		{"公共文件（多个作业引用）", nil, []string{"aap/4gl/saapt310.4gl"}, "PROG1,PROG2", 2},
		{"只给文件名", nil, []string{"saapt310.4gl"}, "PROG1,PROG2", 2},
		{"模组/代码两段", nil, []string{"aap/saapt310"}, "PROG1,PROG2", 2},
		{"MAIN 程序本身", nil, []string{"apm/4gl/apmp100.4gl"}, "PROG1", 1},
		{"模组筛选", []string{"axc"}, nil, "PROG3", 1},
		{"模组大小写不敏感", []string{"AXC"}, nil, "PROG3", 1},
		{"模组+文件并集", []string{"axc"}, []string{"saapt310"}, "PROG1,PROG2,PROG3", 3},
		{"匹配不到", []string{"zzz"}, []string{"nope.4gl"}, "", 0},
		{"gal04=N 不参与", nil, []string{"apmp111"}, "", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sel, err := SelectPrograms([]byte(partialCSV), tc.modules, tc.specs)
			if err != nil {
				t.Fatalf("SelectPrograms: %v", err)
			}
			got := strings.Join(sortedNames(sel), ",")
			if got != tc.want {
				t.Errorf("受影响程序 = %q，期望 %q", got, tc.want)
			}
			if sel.MatchedRows != tc.wantMatched {
				t.Errorf("MatchedRows = %d，期望 %d", sel.MatchedRows, tc.wantMatched)
			}
		})
	}
}

func TestParseFileSpec(t *testing.T) {
	cases := []struct{ in, module, code string }{
		{"aap/4gl/saapt310.4gl", "aap", "saapt310"},
		{"aap/saapt310", "aap", "saapt310"},
		{"saapt310.4gl", "", "saapt310"},
		{`aap\4gl\saapt310.4gl`, "aap", "saapt310"},
		{"tiptop/aap/4gl/saapt310.4gl", "tiptop", "saapt310"},
		{"", "", ""},
	}
	for _, c := range cases {
		m, code := ParseFileSpec(c.in)
		if m != c.module || code != c.code {
			t.Errorf("ParseFileSpec(%q) = (%q,%q)，期望 (%q,%q)", c.in, m, code, c.module, c.code)
		}
	}
}

// writePartialProject 建一个最小项目：tiptop/{apm,aap,axc}/4gl。
func writePartialProject(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	files := map[string]string{
		"tiptop/apm/4gl/apmp100.4gl":  "MAIN\n    CALL shared_fn()\nEND MAIN\n",
		"tiptop/apm/4gl/apmp200.4gl":  "MAIN\n    CALL shared_fn()\nEND MAIN\n",
		"tiptop/aap/4gl/saapt310.4gl": "FUNCTION shared_fn()\n    DISPLAY \"x\"\nEND FUNCTION\n",
		"tiptop/axc/4gl/axcp500.4gl":  "MAIN\n    DISPLAY \"y\"\nEND MAIN\n",
	}
	for rel, src := range files {
		p := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func TestRunImportPartialMerges(t *testing.T) {
	root := writePartialProject(t)
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	now := time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)

	// 1) 全量：3 条链（PROG1/PROG2/PROG3）+ 1 个 orphan（NOPE 的文件不存在）
	full, err := RunImport(ImportRequest{
		CSVData:     []byte(partialCSV),
		ProjectRoot: root,
		Pool:        pool,
		Source:      "file",
		Write:       true,
		Now:         now,
	})
	if err != nil {
		t.Fatalf("全量导入: %v", err)
	}
	if len(full.Result.Chains) != 4 {
		t.Fatalf("全量应有 4 条链/组，实际 %d", len(full.Result.Chains))
	}

	// 2) 局部：只更新 aap/4gl/saapt310.4gl 涉及的两个作业
	part, err := RunImportPartial(PartialImportRequest{
		CSVData:     []byte(partialCSV),
		ProjectRoot: root,
		Pool:        pool,
		Source:      "file",
		Specs:       []string{"aap/4gl/saapt310.4gl"},
		Write:       true,
		Now:         now.Add(time.Hour),
	})
	if err != nil {
		t.Fatalf("局部更新: %v", err)
	}
	if got := len(part.Selection.Programs); got != 2 {
		t.Errorf("受影响作业应 2 个，实际 %d", got)
	}
	if part.NewChains != 2 || part.KeptChains != 2 {
		t.Errorf("重算/保留应为 2/2，实际 %d/%d", part.NewChains, part.KeptChains)
	}

	// 3) 读回：链数不变，PROG3（未受影响）仍在，meta 记了 partial
	idx, err := Load(filepath.Join(root, DefaultDataDir))
	if err != nil {
		t.Fatalf("读回资料: %v", err)
	}
	programs := map[string]bool{}
	for _, c := range idx.Chains() {
		programs[c.Program] = true
	}
	for _, want := range []string{"PROG1", "PROG2", "PROG3", "NOPE"} {
		if !programs[want] {
			t.Errorf("合并后应保留程序 %s（实际 %v）", want, programs)
		}
	}
	m := idx.Meta()
	if m == nil || m.Partial == nil {
		t.Fatal("meta 应记录 partial 块")
	}
	if m.Partial.Programs != 2 || m.Partial.ChainsUpdated != 2 {
		t.Errorf("partial 统计不对：%+v", m.Partial)
	}
	if m.GeneratedAt != now.Format(time.RFC3339) {
		t.Errorf("generatedAt 应保持全量时间（%s），实际 %s", now.Format(time.RFC3339), m.GeneratedAt)
	}
}

// TestRunImportPartialNeedsBaseline 没有全量资料时，局部更新必须明确报错，
// 而不是悄悄写出一份不完整的资料。
func TestRunImportPartialNeedsBaseline(t *testing.T) {
	root := writePartialProject(t)
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	_, err = RunImportPartial(PartialImportRequest{
		CSVData:     []byte(partialCSV),
		ProjectRoot: root,
		Pool:        pool,
		Specs:       []string{"aap/4gl/saapt310.4gl"},
	})
	if err == nil {
		t.Fatal("没有基线时应报错")
	}
	if !strings.Contains(err.Error(), "全量") {
		t.Errorf("错误信息应提示先做全量导入，实际：%v", err)
	}
}
