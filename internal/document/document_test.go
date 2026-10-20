package document

import (
	"testing"
)

func TestNewLineIndex(t *testing.T) {
	li := NewLineIndex("a\nbb\n\nccc")
	if got := li.LineCount(); got != 4 {
		t.Fatalf("LineCount = %d，期望 4", got)
	}

	cases := []struct{ line, col int }{{0, 0}, {1, 0}, {2, 0}, {3, 0}, {3, 2}}
	wantOffsets := []int{0, 2, 5, 6, 8}
	for i, c := range cases {
		if got := li.Offset(c.line, c.col); got != wantOffsets[i] {
			t.Errorf("Offset(%d,%d) = %d，期望 %d", c.line, c.col, got, wantOffsets[i])
		}
	}
}

func TestLineIndexRoundTrip(t *testing.T) {
	text := "SCHEMA ds\n\nMAIN\n  DISPLAY \"x\"\nEND MAIN\n"
	li := NewLineIndex(text)

	for offset := 0; offset <= len(text); offset++ {
		line, col := li.LineCol(offset)
		if got := li.Offset(line, col); got != offset {
			t.Fatalf("offset %d → (%d,%d) → %d，不闭合", offset, line, col, got)
		}
	}
}

func TestLineIndexOutOfRange(t *testing.T) {
	li := NewLineIndex("single line")
	if got := li.Offset(-1, 0); got != 0 {
		t.Errorf("负行号应回到 0，实际 %d", got)
	}
	if got := li.Offset(99, 0); got != 0 {
		t.Errorf("越界行号应回到最后一行起点，实际 %d", got)
	}
	if line, col := li.LineCol(-5); line != 0 || col != 0 {
		t.Errorf("负偏移应回到 (0,0)，实际 (%d,%d)", line, col)
	}
}

func TestDocumentUpdateAndSnapshot(t *testing.T) {
	store := NewStore()
	defer store.CloseAll()

	doc := store.Open("file:///a.4gl", "bdl", 1, "MAIN\nEND MAIN\n")
	text, tree, version := doc.Snapshot()
	if string(text) != "MAIN\nEND MAIN\n" || version != 1 || tree != nil {
		t.Fatalf("初始快照异常: %q v=%d tree=%v", text, version, tree)
	}

	doc.Update(2, "MAIN\n  DISPLAY \"x\"\nEND MAIN\n")
	text, _, version = doc.Snapshot()
	if version != 2 {
		t.Errorf("版本 = %d，期望 2", version)
	}
	if string(text) != "MAIN\n  DISPLAY \"x\"\nEND MAIN\n" {
		t.Errorf("内容未更新: %q", text)
	}
	if doc.Lines.LineCount() != 4 {
		t.Errorf("行索引未随内容更新: %d", doc.Lines.LineCount())
	}
}

func TestStoreGetClose(t *testing.T) {
	store := NewStore()
	store.Open("file:///a.4gl", "bdl", 1, "x")
	if store.Get("file:///a.4gl") == nil {
		t.Fatal("Get 应返回文档")
	}
	if store.Get("file:///missing.4gl") != nil {
		t.Error("不存在的 URI 应返回 nil")
	}

	if doc := store.Close("file:///a.4gl"); doc == nil {
		t.Error("Close 应返回被移除的文档")
	}
	if store.Get("file:///a.4gl") != nil {
		t.Error("Close 后不应再能获取")
	}
}

func TestStoreOpenReplacesOld(t *testing.T) {
	store := NewStore()
	defer store.CloseAll()

	first := store.Open("file:///a.4gl", "bdl", 1, "v1")
	second := store.Open("file:///a.4gl", "bdl", 2, "v2")
	if first == second {
		t.Fatal("重复打开应产生新文档对象")
	}
	if got := store.Get("file:///a.4gl"); got != second {
		t.Error("仓库应保存最新的文档")
	}
	if n := len(store.All()); n != 1 {
		t.Errorf("All() = %d，期望 1", n)
	}
}
