package parser

import (
	"strings"
	"testing"

	ts "github.com/tree-sitter/go-tree-sitter"
)

// buildLargeBDL 生成一个接近真实规模的 BDL 文件：
// 多个函数，每个函数包含若干语句，总行数约 lines。
func buildLargeBDL(lines int) []byte {
	var sb strings.Builder
	sb.WriteString("SCHEMA ds\nGLOBALS \"globals.4gl\"\n\n")
	sb.WriteString("DEFINE g_count LIKE type_file.num5\n\n")

	funcs := lines / 20
	if funcs < 1 {
		funcs = 1
	}
	for i := 0; i < funcs; i++ {
		sb.WriteString("FUNCTION f")
		sb.WriteString(itoa(i))
		sb.WriteString("(p_a, p_b)\n")
		sb.WriteString("    DEFINE l_total LIKE type_file.num10\n")
		sb.WriteString("    DEFINE l_msg   STRING\n\n")
		sb.WriteString("    LET l_total = p_a + p_b\n")
		sb.WriteString("    IF l_total > 0 THEN\n")
		sb.WriteString("        LET l_msg = \"positive: \", l_total\n")
		sb.WriteString("        DISPLAY l_msg\n")
		sb.WriteString("    ELSE\n")
		sb.WriteString("        DISPLAY \"zero or negative\"\n")
		sb.WriteString("    END IF\n")
		sb.WriteString("    RETURN l_total\n")
		sb.WriteString("END FUNCTION\n\n")
	}
	sb.WriteString("MAIN\n    CALL f0(1, 2)\nEND MAIN\n")
	return []byte(sb.String())
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [8]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// simulateEdit 模拟一次小编辑：在第 5 行行尾插入一个字符。
func simulateEdit(src []byte, line int) []byte {
	lines := strings.SplitAfter(string(src), "\n")
	if line >= len(lines) {
		return src
	}
	lines[line] = strings.TrimSuffix(lines[line], "\n") + " " + "\n"
	return []byte(strings.Join(lines, ""))
}

func TestIncrementalParseIsCorrect(t *testing.T) {
	pool, err := New()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	src := buildLargeBDL(2000)
	poolTree, err := pool.Parse(LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer poolTree.Close()

	if root := poolTree.RootNode(); root.HasError() {
		t.Fatalf("生成的内容不应有语法错误")
	}

	// 必须先用 Tree.Edit 把旧树搬到新坐标，否则增量解析会读到错误位置。
	// 这里模拟“在第 5 行行尾插入一个空格”。
	line := 5
	lineStart := 0
	for i := 0; i < line; i++ {
		lineStart += strings.IndexByte(string(src[lineStart:]), '\n') + 1
	}
	lineEnd := lineStart + strings.IndexByte(string(src[lineStart:]), '\n')
	poolTree.Edit(&ts.InputEdit{
		StartByte:      uint(lineEnd),
		OldEndByte:     uint(lineEnd),
		NewEndByte:     uint(lineEnd + 1),
		StartPosition:  ts.NewPoint(uint(line), uint(lineEnd-lineStart)),
		OldEndPosition: ts.NewPoint(uint(line), uint(lineEnd-lineStart)),
		NewEndPosition: ts.NewPoint(uint(line), uint(lineEnd-lineStart+1)),
	})

	edited := simulateEdit(src, 5)
	tree2, err := pool.Parse(LangBDL, edited, poolTree)
	if err != nil {
		t.Fatal(err)
	}
	defer tree2.Close()

	if root := tree2.RootNode(); root.HasError() {
		t.Error("增量解析结果不应有语法错误")
	}
}

func BenchmarkParseFull(b *testing.B) {
	pool, err := New()
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()

	src := buildLargeBDL(5000)
	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tree, err := pool.Parse(LangBDL, src, nil)
		if err != nil {
			b.Fatal(err)
		}
		tree.Close()
	}
}

func BenchmarkParseIncremental(b *testing.B) {
	pool, err := New()
	if err != nil {
		b.Fatal(err)
	}
	defer pool.Close()

	src := buildLargeBDL(5000)
	edited := simulateEdit(src, 5)

	base, err := pool.Parse(LangBDL, src, nil)
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(base.Close)

	b.SetBytes(int64(len(src)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// 每次复用同一棵“已编辑”的树；真实场景中树会随文档演进。
		tree, err := pool.Parse(LangBDL, edited, base)
		if err != nil {
			b.Fatal(err)
		}
		tree.Close()
	}
}
