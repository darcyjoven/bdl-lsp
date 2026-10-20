package textpos

import (
	"strings"
	"testing"
)

func TestOffsetASCII(t *testing.T) {
	text := []byte("abc\ndef\nghi")
	ix := New(text)

	cases := []struct{ line, col, want int }{
		{0, 0, 0}, {0, 3, 3},
		{1, 0, 4}, {1, 2, 6},
		{2, 0, 8}, {2, 3, 11},
		{3, 0, 11}, // 越界行 → 文本末尾
		{-1, 2, 0}, // 负行 → 0
	}
	for _, c := range cases {
		if got := ix.Offset(c.line, c.col); got != c.want {
			t.Errorf("Offset(%d,%d) = %d，期望 %d", c.line, c.col, got, c.want)
		}
	}
}

func TestOffsetUTF16BMP(t *testing.T) {
	// 每行 3 个中文字符，各占 3 字节 / 1 个 UTF-16 码元
	text := []byte("中文行\n第二行\n")
	ix := New(text)

	if got := ix.Offset(0, 0); got != 0 {
		t.Errorf("列 0 = %d", got)
	}
	if got := ix.Offset(0, 1); got != 3 {
		t.Errorf("列 1 应指向第 2 个汉字起始（字节 3），实际 %d", got)
	}
	if got := ix.Offset(0, 3); got != 9 {
		t.Errorf("列 3 应为行内容末尾（字节 9），实际 %d", got)
	}
	// 列超过行宽 → 钳制到行内容末尾（不含 \n）
	if got := ix.Offset(0, 99); got != 9 {
		t.Errorf("超宽列应钳制到 %d，实际 %d", 9, got)
	}
}

func TestOffsetSuppPlane(t *testing.T) {
	// 😀 是 U+1F600，UTF-8 4 字节，UTF-16 2 码元
	text := []byte("a😀b\n")
	ix := New(text)

	if got := ix.Offset(0, 1); got != 1 {
		t.Errorf("😀 起点应为字节 1，实际 %d", got)
	}
	if got := ix.Offset(0, 2); got != 1 {
		t.Errorf("代理对中间应钳制到字符起点 1，实际 %d", got)
	}
	if got := ix.Offset(0, 3); got != 5 {
		t.Errorf("'b' 起点应为字节 5，实际 %d", got)
	}
}

func TestPositionRoundTrip(t *testing.T) {
	texts := []string{
		"abc\ndef\n",
		"中文行\n第二行\n",
		"a😀b\nc",
		"",
		"\n\n\n",
		"no trailing newline",
		"混合 ascii 与 中文\nwith 😀 emoji",
	}
	for _, text := range texts {
		ix := New([]byte(text))
		for offset := 0; offset <= len(text); offset++ {
			line, col := ix.Position(offset)
			back := ix.Offset(line, col)
			// 代理对内部无法精确往返；其余应完全一致
			if back != offset && !inSurrogate(text, offset) {
				t.Fatalf("文本 %q offset %d → (%d,%d) → %d", text, offset, line, col, back)
			}
		}
	}
}

// inSurrogate 判断偏移是否落在补充平面字符的 UTF-8 序列中间。
func inSurrogate(text string, offset int) bool {
	if offset <= 0 || offset >= len(text) {
		return false
	}
	for i := 0; i < len(text); {
		_, size := decodeRune(text[i:])
		if size > 1 && i < offset && offset < i+size {
			return true
		}
		i += size
	}
	return false
}

func decodeRune(s string) (rune, int) {
	for _, r := range s {
		return r, len(string(r))
	}
	return 0, 1
}

func TestPoint(t *testing.T) {
	// "中文" = 6 字节，\n 在字节 6，第二行从字节 7 开始
	text := []byte("中文\nabc\n")
	ix := New(text)

	row, col := ix.Point(2)
	if row != 0 || col != 2 {
		t.Errorf("字节 2 应为 (0,2)，实际 (%d,%d)", row, col)
	}
	row, col = ix.Point(7)
	if row != 1 || col != 0 {
		t.Errorf("字节 7 应为 (1,0)，实际 (%d,%d)", row, col)
	}
	// Point 的列是字节列，不是 UTF-16 列：第 0 行 2 个汉字在 UTF-16 是 2 列，在字节是 6 列
	row, col = ix.Point(6)
	if row != 0 || col != 6 {
		t.Errorf("字节 6 应为 (0,6)，实际 (%d,%d)", row, col)
	}
}

func TestLineContentEndCRLF(t *testing.T) {
	ix := New([]byte("abc\r\ndef\r\n"))
	if got := ix.LineContentEnd(0); got != 3 {
		t.Errorf("CRLF 行内容末尾应为 3，实际 %d", got)
	}
	// "def" 位于字节 5..7，末尾（不含）为 8
	if got := ix.LineContentEnd(1); got != 8 {
		t.Errorf("第二行内容末尾应为 8，实际 %d", got)
	}
}

func TestOffsetClampsAtLineEnd(t *testing.T) {
	ix := New([]byte("ab\ncd\n"))
	// 第一行只有 2 列，请求第 5 列不应跨到下一行
	got := ix.Offset(0, 5)
	if got != 2 {
		t.Errorf("应钳制到行末字节 2，实际 %d", got)
	}
}

func TestAdvance(t *testing.T) {
	row, col := Advance(0, 0, "abc")
	if row != 0 || col != 3 {
		t.Errorf("单行插入 = (%d,%d)", row, col)
	}
	row, col = Advance(1, 2, "x\ny\nz")
	if row != 3 || col != 1 {
		t.Errorf("多行插入 = (%d,%d)，期望 (3,1)", row, col)
	}
}

func TestIndexOnLargeText(t *testing.T) {
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		sb.WriteString("    DISPLAY \"第 ")
		sb.WriteString(strings.Repeat("x", 10))
		sb.WriteString(" 行\"\n")
	}
	ix := New([]byte(sb.String()))
	if ix.LineCount() != 5001 {
		t.Fatalf("行数 = %d，期望 5001", ix.LineCount())
	}
	line, col := ix.Position(len(ix.Text()))
	if line != 5000 {
		t.Errorf("末位置行号 = %d，期望 5000", line)
	}
	if col != 0 {
		t.Errorf("末位置列 = %d，期望 0", col)
	}
}

func BenchmarkNewIndex(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		sb.WriteString("    DISPLAY \"hello\"\n")
	}
	text := []byte(sb.String())
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		New(text)
	}
}

func BenchmarkOffset(b *testing.B) {
	var sb strings.Builder
	for i := 0; i < 5000; i++ {
		sb.WriteString("    DISPLAY \"中文行\"\n")
	}
	ix := New([]byte(sb.String()))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		ix.Offset(2500, 10)
	}
}
