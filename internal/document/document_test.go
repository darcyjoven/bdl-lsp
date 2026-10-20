package document

import (
	"testing"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

func TestNewAndSnapshot(t *testing.T) {
	doc := New("file:///a.4gl", parser.LangBDL, 1, "MAIN\nEND MAIN\n")

	text, tree, version := doc.Snapshot()
	if string(text) != "MAIN\nEND MAIN\n" {
		t.Errorf("文本 = %q", text)
	}
	if version != 1 {
		t.Errorf("版本 = %d", version)
	}
	if tree != nil {
		t.Error("初始不应有语法树")
	}
	if doc.LineCount() != 3 {
		t.Errorf("行数 = %d，期望 3", doc.LineCount())
	}
}

func TestStoreOpenGetClose(t *testing.T) {
	store := NewStore()
	defer store.CloseAll()

	store.Open("file:///a.4gl", parser.LangBDL, 1, "x")
	if store.Get("file:///a.4gl") == nil {
		t.Fatal("Get 应返回文档")
	}
	if store.Get("file:///missing.4gl") != nil {
		t.Error("不存在的 URI 应返回 nil")
	}
	if store.Len() != 1 {
		t.Errorf("Len = %d", store.Len())
	}

	if doc := store.Close("file:///a.4gl"); doc == nil {
		t.Error("Close 应返回被移除的文档")
	}
	if store.Len() != 0 {
		t.Error("Close 后应为空")
	}
}

func TestStoreOpenReplacesOld(t *testing.T) {
	store := NewStore()
	defer store.CloseAll()

	first := store.Open("file:///a.4gl", parser.LangBDL, 1, "v1")
	second := store.Open("file:///a.4gl", parser.LangBDL, 2, "v2")
	if first == second {
		t.Fatal("重复打开应产生新对象")
	}
	if got := store.Get("file:///a.4gl"); got != second {
		t.Error("仓库应保存最新文档")
	}
	if store.Len() != 1 {
		t.Errorf("Len = %d，期望 1", store.Len())
	}
}

func TestApplyFullChange(t *testing.T) {
	doc := New("file:///a.4gl", parser.LangBDL, 1, "old")
	tree, err := doc.ApplyChanges(2, []Change{{Full: true, Text: "new content"}})
	if err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}
	if tree != nil {
		t.Error("整文替换应丢弃旧树")
	}

	text, _, version := doc.Snapshot()
	if string(text) != "new content" {
		t.Errorf("文本 = %q", text)
	}
	if version != 2 {
		t.Errorf("版本 = %d", version)
	}
}

func TestApplyIncrementalInsert(t *testing.T) {
	doc := New("file:///a.4gl", parser.LangBDL, 1, "MAIN\nEND MAIN\n")

	// 在第 0 行行尾插入 " -- 注释"
	tree, err := doc.ApplyChanges(2, []Change{{
		StartLine: 0, StartCol: 4,
		EndLine: 0, EndCol: 4,
		Text: " -- 注释",
	}})
	if err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}
	if tree != nil {
		t.Error("无旧树时不应返回树")
	}

	text, _, _ := doc.Snapshot()
	if string(text) != "MAIN -- 注释\nEND MAIN\n" {
		t.Errorf("文本 = %q", text)
	}
}

func TestApplyIncrementalDelete(t *testing.T) {
	doc := New("file:///a.4gl", parser.LangBDL, 1, "MAIN\n  DISPLAY \"x\"\nEND MAIN\n")

	// 删除第 1 行整行（含换行）
	tree, err := doc.ApplyChanges(2, []Change{{
		StartLine: 1, StartCol: 0,
		EndLine: 2, EndCol: 0,
		Text: "",
	}})
	if err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}
	_ = tree

	text, _, _ := doc.Snapshot()
	if string(text) != "MAIN\nEND MAIN\n" {
		t.Errorf("文本 = %q", text)
	}
}

func TestApplyMultipleChanges(t *testing.T) {
	doc := New("file:///a.4gl", parser.LangBDL, 1, "aaa\nbbb\nccc\n")

	_, err := doc.ApplyChanges(2, []Change{
		{StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 3, Text: "AAA"},
		{StartLine: 2, StartCol: 0, EndLine: 2, EndCol: 3, Text: "CCC"},
	})
	if err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}

	text, _, _ := doc.Snapshot()
	if string(text) != "AAA\nbbb\nCCC\n" {
		t.Errorf("文本 = %q", text)
	}
}

func TestApplyUTF16Columns(t *testing.T) {
	// 中文在 UTF-16 中每字 1 列，在字节中每字 3 字节
	doc := New("file:///a.4gl", parser.LangBDL, 1, "中文行\n")
	if _, err := doc.ApplyChanges(2, []Change{{
		StartLine: 0, StartCol: 1,
		EndLine: 0, EndCol: 3,
		Text: "X",
	}}); err != nil {
		t.Fatalf("ApplyChanges: %v", err)
	}

	text, _, _ := doc.Snapshot()
	if string(text) != "中X\n" {
		t.Errorf("文本 = %q，期望 %q", text, "中X\n")
	}
}

func TestApplyInvalidRange(t *testing.T) {
	doc := New("file:///a.4gl", parser.LangBDL, 1, "abc\n")
	if _, err := doc.ApplyChanges(2, []Change{{
		StartLine: 5, StartCol: 0,
		EndLine: 0, EndCol: 0,
		Text: "x",
	}}); err == nil {
		t.Error("倒序区间应报错")
	}

	// 错误不应污染文本
	text, _, version := doc.Snapshot()
	if string(text) != "abc\n" || version != 1 {
		t.Errorf("失败后文本/版本被修改：%q v=%d", text, version)
	}
}

func TestApplyThenCloseIsSafe(t *testing.T) {
	doc := New("file:///a.4gl", parser.LangBDL, 1, "MAIN\nEND MAIN\n")
	if _, err := doc.ApplyChanges(2, []Change{{
		StartLine: 0, StartCol: 0, EndLine: 0, EndCol: 4, Text: "MAIN",
	}}); err != nil {
		t.Fatal(err)
	}
	doc.Close()
	doc.Close() // 幂等
}
