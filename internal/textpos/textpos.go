// Package textpos 提供源码文本的位置换算。
//
// 需要处理两套坐标：
//   - LSP 的 Position：0 基行号 + UTF-16 码元列（编辑器口径）；
//   - tree-sitter 的 Point：0 基行号 + 字节列（解析器口径）。
//
// 两者在纯 ASCII 下一致，一旦出现中文/emoji 就会分叉，因此必须显式换算。
package textpos

import (
	"sort"
	"unicode/utf16"
	"unicode/utf8"
)

// Index 是一份文本的行索引，可在字节偏移与两种位置之间换算。
//
// Index 不可变：每次文本变更后重建。对 10k 行文件重建约 0.1 ms，
// 相对 tree-sitter 的解析耗时可忽略，换来的是实现简单、不会出现索引漂移。
type Index struct {
	text   []byte
	starts []int // 每行起始字节偏移
}

// New 扫描文本，构建行索引。
func New(text []byte) *Index {
	starts := make([]int, 0, 64)
	starts = append(starts, 0)
	for i, b := range text {
		if b == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &Index{text: text, starts: starts}
}

// Text 返回索引对应的文本（只读）。
func (ix *Index) Text() []byte { return ix.text }

// Size 返回文本字节数。
func (ix *Index) Size() int { return len(ix.text) }

// LineCount 返回行数；空文本算 1 行。
func (ix *Index) LineCount() int { return len(ix.starts) }

// LineStart 返回某行起始字节偏移；越界时钳制到边界。
func (ix *Index) LineStart(line int) int {
	if line <= 0 {
		return 0
	}
	if line >= len(ix.starts) {
		return len(ix.text)
	}
	return ix.starts[line]
}

// LineContentEnd 返回某行内容的结束偏移（不含 \n，也不含 \r\n 中的 \r）。
//
// LSP 的 Position 不会指向行终止符之后，换算与钳制都应以这里为准。
func (ix *Index) LineContentEnd(line int) int {
	if line < 0 {
		return 0
	}
	if line >= len(ix.starts)-1 {
		end := len(ix.text)
		// 最后一行可能仍以换行符结束
		if end > 0 && ix.text[end-1] == '\n' {
			end--
		}
		if end > 0 && ix.text[end-1] == '\r' {
			end--
		}
		return end
	}
	end := ix.starts[line+1] - 1 // 去掉 \n
	if end > 0 && ix.text[end-1] == '\r' {
		end--
	}
	return end
}

// Offset 把 LSP 位置（行 + UTF-16 列）换算为字节偏移。
//
// 列越界钳制到行尾；落在代理对中间时钳制到该字符起始处。
func (ix *Index) Offset(line, utf16Col int) int {
	if line <= 0 {
		if line < 0 {
			return 0
		}
	}
	if line >= len(ix.starts) {
		return len(ix.text)
	}
	start := ix.starts[line]
	if utf16Col <= 0 {
		return start
	}
	end := ix.LineContentEnd(line)
	if start >= end {
		return start
	}

	i, col := start, 0
	for i < end && col < utf16Col {
		r, size := utf8.DecodeRune(ix.text[i:])
		if r == utf8.RuneError && size == 1 {
			// 非法 UTF-8：按单码元前进，保证位置单调
			col++
			i++
			continue
		}
		w := utf16Width(r)
		if col+w > utf16Col {
			// 请求位置落在代理对中间：对齐到字符起点
			return i
		}
		col += w
		i += size
	}
	return i
}

// Position 把字节偏移换算为 LSP 位置（行 + UTF-16 列）。
func (ix *Index) Position(offset int) (line, col int) {
	if offset <= 0 {
		return 0, 0
	}
	if offset > len(ix.text) {
		offset = len(ix.text)
	}
	line = ix.lineOf(offset)
	rowStart := ix.starts[line]
	if rowStart >= offset {
		return line, 0
	}
	return line, utf16Len(ix.text[rowStart:offset])
}

// Point 把字节偏移换算为 tree-sitter 的 Point（行 + 字节列）。
func (ix *Index) Point(offset int) (row, byteCol int) {
	if offset <= 0 {
		return 0, 0
	}
	if offset > len(ix.text) {
		offset = len(ix.text)
	}
	row = ix.lineOf(offset)
	return row, offset - ix.starts[row]
}

// lineOf 返回偏移所在行号（二分查找最后一个 starts <= offset）。
func (ix *Index) lineOf(offset int) int {
	lo, hi := 0, len(ix.starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if ix.starts[mid] <= offset {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo
}

// utf16Len 返回一段 UTF-8 字节的 UTF-16 码元数。
func utf16Len(b []byte) int {
	n := 0
	for i := 0; i < len(b); {
		r, size := utf8.DecodeRune(b[i:])
		if r == utf8.RuneError && size == 1 {
			n++
			i++
			continue
		}
		n += utf16Width(r)
		i += size
	}
	return n
}

// utf16Width 返回一个 rune 的 UTF-16 码元数（补充平面为 2）。
func utf16Width(r rune) int {
	if r < 0x10000 {
		return 1
	}
	return len(utf16.Encode([]rune{r}))
}

// Advance 在给定起点上按插入文本推进位置，返回新终点。
//
// start 与结果都是 tree-sitter 的 Point（字节列）。
func Advance(row, byteCol int, inserted string) (int, int) {
	for i := 0; i < len(inserted); i++ {
		if inserted[i] == '\n' {
			row++
			byteCol = 0
			continue
		}
		byteCol++
	}
	return row, byteCol
}

// SortRanges 便于上层对多段编辑排序（预留）。
func SortRanges(offsets []int) { sort.Ints(offsets) }
