package server

// GLOBALS 索引的"作用域"测试（P3a 收尾，0.3.1）。
//
// 用户报的现象：
//
//	"部分场景下会出现，一个字段来源于某个 globals 文件，且都是同一个文件，
//	 重新打开 vscode 后就正常。"
//
// 根因：globalsDefinitions / globalsTypeLines / unresolvedSourceChecks
// 遍历的是**所有文档**的 globalsEntries，而不是"当前文档"的那些。
// 于是同时打开两个文档时，一个文档会借用另一个文档的 GLOBALS 导入：
//   - 两个文档都引用同一个 .global → 同一个符号被列出多份来源
//     （悬停里出现两条一模一样的 "top.global:2"）；
//   - 两个文档引用**不同**的 .global → 变量来源检查会拿到别的文档的
//     符号集合，导致 "无法确定变量来源" 该报的不报、不该报的也不报。
//
// 症状随"当前开着哪些文件"变化：关掉另一个文档、重启 VSCode、
// 重新加载窗口就正常——用户无法复现规律。
//
// 修法见 globals.go 的 globalsEntriesOf / forgetGlobalsEntries。

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// globalsScopeSetup 构造：
//
//	dir/
//	  top.global   ← DEFINE g_counter
//	  main.4gl     ← GLOBALS "top.global"，使用 g_counter
//	  other.4gl    ← 也 GLOBALS "top.global"
func globalsScopeSetup(t *testing.T) (dir, mainURI, otherURI, globalURI string) {
	t.Helper()
	dir = t.TempDir()

	g := "GLOBALS\n    DEFINE g_counter INTEGER\nEND GLOBALS\n"
	if err := os.WriteFile(filepath.Join(dir, "top.global"), []byte(g), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir,
		"file://" + filepath.Join(dir, "main.4gl"),
		"file://" + filepath.Join(dir, "other.4gl"),
		"file://" + filepath.Join(dir, "top.global")
}

// TestGlobalsEntriesNotLeakedAcrossDocs 验证：文档 A 关闭后，
// 它的 GLOBALS 解析结果不应继续影响文档 B 的跳转。
func TestGlobalsEntriesNotLeakedAcrossDocs(t *testing.T) {
	dir, mainURI, otherURI, globalURI := globalsScopeSetup(t)
	h := initializeGlobals(t, dir)

	src := "GLOBALS \"top.global\"\nMAIN\n    DISPLAY g_counter\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	// 关闭 main.4gl
	closePayload := fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didClose","params":{"textDocument":{"uri":%q}}}`, mainURI)
	h.send(closePayload)
	time.Sleep(300 * time.Millisecond)

	// 打开 other.4gl：它也引用 top.global
	h.sendDidOpen(otherURI, "bdl", src, 1)
	h.drainDiagnostics(otherURI, 5*time.Second)

	// other.4gl 里的 g_counter 应能跳到 top.global
	locs := h.requestDefinition(t, otherURI, 2, 12)
	if len(locs) == 0 {
		t.Fatal("other.4gl 的 g_counter 应能跳到 top.global")
	}
	if locs[0]["uri"] != globalURI {
		t.Errorf("跳转目标应是 %s，实际 %v", globalURI, locs[0]["uri"])
	}
}

// TestGlobalsDefinitionsScopedToCurrentDoc 验证：跳转/悬停只应考虑
// **当前文档**引入的 .global，不应把别的文档的导入混进来。
//
// 用户报的现象："一个字段来源于某个 globals 文件，且都是同一个文件"
// ——正是两个文档都引用同一个 .global 时，同一个符号被列出多份来源。
func TestGlobalsDefinitionsScopedToCurrentDoc(t *testing.T) {
	dir, mainURI, otherURI, globalURI := globalsScopeSetup(t)
	h := initializeGlobals(t, dir)

	src := "GLOBALS \"top.global\"\nMAIN\n    DISPLAY g_counter\nEND MAIN\n"

	// 先打开 main.4gl（引用 top.global）
	h.sendDidOpen(mainURI, "bdl", src, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	// 再打开 other.4gl（也引用 top.global）
	h.sendDidOpen(otherURI, "bdl", src, 1)
	h.drainDiagnostics(otherURI, 5*time.Second)

	// 在 main.4gl 里请求定义：应只返回 **1 个** Location（top.global 里那一个）
	locs := h.requestDefinition(t, mainURI, 2, 12)
	if len(locs) == 0 {
		t.Fatal("main.4gl 的 g_counter 应能跳到 top.global")
	}
	if len(locs) > 1 {
		t.Errorf("同一符号应只有 1 个定义，实际 %d 个（重复的来源）", len(locs))
	}
	for i, l := range locs {
		if l["uri"] != globalURI {
			t.Errorf("定义 %d 的 URI 应是 %s，实际 %v", i, globalURI, l["uri"])
		}
	}
}

// TestGlobalsHoverSourcesNotDuplicated 验证：悬停里的"来源"列表不应重复。
func TestGlobalsHoverSourcesNotDuplicated(t *testing.T) {
	dir, mainURI, otherURI, _ := globalsScopeSetup(t)
	h := initializeGlobals(t, dir)

	src := "GLOBALS \"top.global\"\nMAIN\n    DISPLAY g_counter\nEND MAIN\n"

	h.sendDidOpen(mainURI, "bdl", src, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)
	h.sendDidOpen(otherURI, "bdl", src, 1)
	h.drainDiagnostics(otherURI, 5*time.Second)

	hov := h.requestHover(t, mainURI, 2, 12)
	if hov == nil {
		t.Fatal("悬停应有内容")
	}
	contents, _ := hov["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	if value == "" {
		t.Fatal("悬停内容为空")
	}
	// 统计 "top.global" 在悬停文本里出现的次数：应恰好 1 次
	count := 0
	for i := 0; i+len("top.global") <= len(value); i++ {
		if value[i:i+len("top.global")] == "top.global" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("来源里 top.global 出现 %d 次（重复），悬停内容：\n%s", count, value)
	}
}

// TestUnresolvedSourceNotAffectedByOtherDocs 验证：变量"来源未找到"诊断
// 只应依据**当前文档**的 GLOBALS 引入。
//
// 场景：main.4gl 引用 top.global（定义 g_counter）；
// other.4gl **不引用**任何 .global，却用了 g_counter。
//
// 当前实现里 unresolvedSourceChecks 遍历**所有文档**的 globalsEntries，
// 于是 other.4gl 会借用 main.4gl 的导入结果 → g_counter 被当成"有来源"，
// 不报 variable-source-unresolved。
//
// 后果：诊断结果取决于"当前还开着哪些文件"——关掉 main.4gl 就会变。
// 这与用户报的"重新打开 vscode 后就正常"完全一致。
func TestUnresolvedSourceNotAffectedByOtherDocs(t *testing.T) {
	dir, mainURI, otherURI, _ := globalsScopeSetup(t)
	h := initializeGlobals(t, dir)

	// main.4gl：引用 top.global，使用 g_counter
	mainSrc := "GLOBALS \"top.global\"\nMAIN\n    DISPLAY g_counter\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", mainSrc, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	// other.4gl：**不引用**任何 .global，却使用 g_counter
	otherSrc := "MAIN\n    DISPLAY g_counter\nEND MAIN\n"
	h.sendDidOpen(otherURI, "bdl", otherSrc, 1)
	diags := h.drainDiagnostics(otherURI, 5*time.Second)

	u := findUnresolvedDiags(diags)
	if len(u) == 0 {
		t.Error("other.4gl 没有引入 top.global，g_counter 应报 variable-source-unresolved")
	}
}

// TestGlobalsSameFileImportedTwice 验证：同一个文档里两次 GLOBALS 引入
// 同一个 .global 文件时，来源只应列一次。
//
// 用户报的现象："一个字段来源于某个 globals 文件，且都是同一个文件"
// ——"都是同一个文件"正是这种重复。
func TestGlobalsSameFileImportedTwice(t *testing.T) {
	dir := t.TempDir()
	g := "GLOBALS\n    DEFINE g_counter INTEGER\nEND GLOBALS\n"
	if err := os.WriteFile(filepath.Join(dir, "top.global"), []byte(g), 0o644); err != nil {
		t.Fatal(err)
	}
	mainURI := "file://" + filepath.Join(dir, "main.4gl")
	h := initializeGlobals(t, dir)

	src := "GLOBALS \"top.global\"\nGLOBALS \"top.global\"\nMAIN\n    DISPLAY g_counter\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", src, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	hov := h.requestHover(t, mainURI, 3, 12)
	if hov == nil {
		t.Fatal("悬停应有内容")
	}
	contents, _ := hov["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	count := 0
	for i := 0; i+len("top.global") <= len(value); i++ {
		if value[i:i+len("top.global")] == "top.global" {
			count++
		}
	}
	if count > 1 {
		t.Errorf("同一文件被引入两次时来源应只列 1 次，实际 %d 次：\n%s", count, value)
	}
}

// TestGlobalsEntriesClearedWhenImportRemoved 验证：用户删掉 GLOBALS 导入行后，
// 文档不应继续"看得见"那个 .global 里的符号。
func TestGlobalsEntriesClearedWhenImportRemoved(t *testing.T) {
	dir, mainURI, _, globalURI := globalsScopeSetup(t)
	h := initializeGlobals(t, dir)

	// 先带 GLOBALS 导入打开
	withImport := "GLOBALS \"top.global\"\nMAIN\n    DISPLAY g_counter\nEND MAIN\n"
	h.sendDidOpen(mainURI, "bdl", withImport, 1)
	h.drainDiagnostics(mainURI, 5*time.Second)

	locs := h.requestDefinition(t, mainURI, 2, 12)
	if len(locs) == 0 || locs[0]["uri"] != globalURI {
		t.Fatalf("带导入时应能跳到 %s，实际 %v", globalURI, locs)
	}

	// 删掉导入行（整文替换）
	noImport := "MAIN\n    DISPLAY g_counter\nEND MAIN\n"
	changes := fmt.Sprintf(`{"contentChanges":[{"text":%q}]}`, noImport)
	h.sendDidChange(mainURI, 2, changes)
	time.Sleep(300 * time.Millisecond)

	// 重新请求定义：不应再跳到 top.global
	locs = h.requestDefinition(t, mainURI, 1, 12)
	for _, l := range locs {
		if l["uri"] == globalURI {
			t.Errorf("删掉导入后不应再跳到 %s，实际 %v", globalURI, locs)
		}
	}
}
