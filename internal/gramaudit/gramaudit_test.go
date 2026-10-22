package gramaudit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darcyjoven/bdl-lsp/parser"
)

// TestRunSmallTree 在临时目录里钉住审计逻辑本身：
// 干净文件不计、坏文件按"最内层 ERROR"定位到正确的行。
func TestRunSmallTree(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "ok.4gl"),
		[]byte("MAIN\n  DISPLAY \"x\"\nEND MAIN\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := "MAIN\n  LET x = 'a'\n  LET y = 1\nEND TUNCTION\n"
	if err := os.WriteFile(filepath.Join(dir, "bad.4gl"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	pool, err := parser.New()
	if err != nil {
		t.Fatalf("初始化解析器: %v", err)
	}
	defer pool.Close()

	rep := Run(pool, dir, 0, 2, 0)
	if rep.Files != 2 {
		t.Fatalf("文件数 = %d，想要 2", rep.Files)
	}
	if rep.ErrFiles != 1 {
		t.Fatalf("出错文件数 = %d，想要 1（ok.4gl 必须干净）", rep.ErrFiles)
	}
	if len(rep.FileErrs) != 1 || !strings.HasSuffix(rep.FileErrs[0].Path, "bad.4gl") {
		t.Fatalf("出错的应当是 bad.4gl，实际 %+v", rep.FileErrs)
	}
	// 最内层定位：错误在最后一行（`END TUNCTION`），不是第 1 行。
	if len(rep.Gaps) == 0 {
		t.Fatal("没有缺口")
	}
	found := false
	for _, g := range rep.Gaps {
		for _, ex := range g.Examples {
			if strings.HasSuffix(ex, "bad.4gl:4") {
				found = true
			}
		}
	}
	if !found {
		t.Fatalf("错误没有定位到第 4 行：%+v", rep.Gaps)
	}
}
