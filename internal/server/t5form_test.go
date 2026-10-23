package server

// P0-2：表单侧的名字（item / SCREEN RECORD / `formonly.列名`）不能被报成
// "变量不存在"（variable-source-unresolved）。
//
// 三种真实形态（2026-10-23 在语料 `saapt310.4gl` 上实测）：
//
//	INPUT ARRAY g_x WITHOUT DEFAULTS FROM s_rec.*   ← record: 字段（语法跳过）
//	CLEAR x / 表达式里直接用表单字段               ← 语义兜底 + CLEAR 整体跳过
//	CONSTRUCT g ON col1 FROM s_arr[1].col2         ← 既有 construct 跳过已覆盖
//
// 反例必须保住：真正没有定义的程序变量仍要报。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// setupT5FormTree 构造一个带 SCREEN RECORD 与"字段名≠item tag"的表单：
//
//	dir/amm/4gl/main.4gl
//	dir/amm/per/ammr110.per
//	  item f01 / alias01（= formonly.t5field）
//	  SCREEN RECORD s_rec(...)
func setupT5FormTree(t *testing.T) (dir, mainURI string) {
	t.Helper()
	dir = t.TempDir()
	for _, d := range []string{"amm/4gl", "amm/per"} {
		if err := os.MkdirAll(filepath.Join(dir, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	per := "SCHEMA ds\n\n" +
		"LAYOUT\nGRID\n{\n  [f01      ]\n}\nEND\nEND\n\n" +
		"ATTRIBUTES\n" +
		"EDIT f01 = formonly.f01;\n" +
		// 关键：item tag 是 alias01，真正的列名是 t5field
		//（4GL 里可以按列名引用，只有 FormFieldNames 里有它）
		"EDIT alias01 = formonly.t5field;\n" +
		"END\n\n" +
		"INSTRUCTIONS\n" +
		"SCREEN RECORD s_rec(formonly.f01)\n" +
		"END\n"
	if err := os.WriteFile(filepath.Join(dir, "amm", "per", "ammr110.per"), []byte(per), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, testURI(dir, "amm", "4gl", "main.4gl")
}

// t5Tokens 把 variable-source-unresolved 诊断还原成源码里的标识符
// （测试源码全是 ASCII，LSP 的 UTF-16 列号可直接当字节列号用）。
func t5Tokens(t *testing.T, diags []any, src string) map[string]bool {
	t.Helper()
	lines := strings.Split(src, "\n")
	out := map[string]bool{}
	for _, raw := range diags {
		d, _ := raw.(map[string]any)
		if d == nil || d["code"] != "variable-source-unresolved" {
			continue
		}
		r, _ := d["range"].(map[string]any)
		start, _ := r["start"].(map[string]any)
		end, _ := r["end"].(map[string]any)
		ln := int(start["line"].(float64))
		ch := int(start["character"].(float64))
		endCh := int(end["character"].(float64))
		if ln >= len(lines) || endCh > len(lines[ln]) {
			t.Fatalf("诊断范围越界：line=%d [%d,%d)", ln, ch, endCh)
		}
		out[lines[ln][ch:endCh]] = true
	}
	return out
}

func TestT5IgnoresFormSideNames(t *testing.T) {
	dir, mainURI := setupT5FormTree(t)
	h := initializeFormServer(t, dir)

	src := "MAIN\n" +
		"  OPEN WINDOW w WITH FORM \"amm/42f/ammr110\"\n" +
		"  INPUT ARRAY g_arr WITHOUT DEFAULTS FROM s_rec.*\n" +
		fmt.Sprintf("  CLEAR %s\n", "s_rec") +
		"  DISPLAY t5field\n" + // 只有 FormFieldNames 里有 t5field
		"  DISPLAY nowhere_var\n" + // 反例：真正未定义的变量
		"END MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	got := t5Tokens(t, diags, src)
	for _, name := range []string{"s_rec", "t5field"} {
		if got[name] {
			t.Errorf("表单侧名字 %q 不应报 variable-source-unresolved（实际：%v）", name, got)
		}
	}
	if !got["nowhere_var"] {
		t.Errorf("真正未定义的程序变量应报告，实际只有 %v", got)
	}
}

// TestT5FormSideWithoutFormBindingStillSilent 没有关联表单时，
// `INPUT ARRAY … FROM s_rec.*` 也不能报——这条靠语法跳过（record: 字段），
// 不依赖表单能否解析。
func TestT5FormSideWithoutFormBindingStillSilent(t *testing.T) {
	dir, mainURI := setupT5FormTree(t)
	h := initializeFormServer(t, dir)

	// 故意不 OPEN WINDOW：表单解析不出任何 .per
	src := "MAIN\n" +
		"  INPUT ARRAY g_arr WITHOUT DEFAULTS FROM s_rec.*\n" +
		"  DISPLAY nowhere_var\n" +
		"END MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	diags := h.drainDiagnostics(mainURI, 5*time.Second)

	got := t5Tokens(t, diags, src)
	if got["s_rec"] {
		t.Errorf("INPUT ARRAY 的屏幕记录名不应报 T5（语法跳过），实际：%v", got)
	}
	if !got["nowhere_var"] {
		t.Errorf("反例应仍然报告，实际只有 %v", got)
	}
}
