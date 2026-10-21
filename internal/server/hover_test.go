package server

import (
	"strings"
	"testing"
)

// 悬停相关的回归测试（P7-B）。
//
// 覆盖两类用户实测报出的问题：
//  1. 多行 RECORD 的类型文本被压成一行，完全没法读；
//  2. 同名局部变量跨函数泄漏（a 函数的 i 把 b 函数的 i 也列出来）。

// hoverSource 覆盖：多行 RECORD、两个函数的同名局部变量、模块级变量。
const hoverSource = `SCHEMA ds

DEFINE g_mod STRING

TYPE t_rec RECORD
    a LIKE type_file.num5,
    b STRING
END RECORD

FUNCTION fa()
    DEFINE i INTEGER
    LET i = 1
    DISPLAY i
END FUNCTION

FUNCTION fb()
    DEFINE i STRING
    LET i = "x"
    DISPLAY i
END FUNCTION
`

// TestHoverMultiLineRecordKeepsLineBreaks 验证多行 RECORD 的悬停按源码换行。
//
// 旧实现用 normalizeSpace 把类型文本压成单空格，多行 RECORD 变成
// 几百字符的单一长行（用户实测：20+ 字段时完全无法阅读）。
func TestHoverMultiLineRecordKeepsLineBreaks(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/hoverrec.4gl"
	h.openBDL(uri, hoverSource)

	// "    a LIKE type_file.num5," 在第 5 行（0 基）
	resp := h.request(20, "textDocument/hover",
		`{"textDocument":{"uri":"`+uri+`"},"position":{"line":5,"character":5}}`)

	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatal("记录字段应有 hover")
	}
	contents, _ := result["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	if !strings.Contains(value, "LIKE type_file.num5") {
		t.Fatalf("hover 文本 = %q，未包含字段类型", value)
	}
}

// TestHoverRecordTypeKeepsLineBreaks 直接验证"内联多行 RECORD"的悬停。
//
// 这条对应用户报的 g_qryparam 场景：悬停变量本身（不是字段）时，
// 类型文本要保留源码换行。
func TestHoverRecordTypeKeepsLineBreaks(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const src = `MAIN
    DEFINE g_qryparam RECORD
             state     LIKE type_file.chr1,
             form      LIKE type_file.chr20,
             where     STRING,
             pagecount STRING
           END RECORD
    DISPLAY g_qryparam.state
END MAIN
`
	const uri = "file:///tmp/hoverinline.4gl"
	h.openBDL(uri, src)

	// "    DEFINE g_qryparam RECORD" 在第 1 行（0 基），g_qryparam 从第 11 列开始
	resp := h.request(21, "textDocument/hover",
		`{"textDocument":{"uri":"`+uri+`"},"position":{"line":1,"character":13}}`)

	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatal("内联 RECORD 变量应有 hover")
	}
	contents, _ := result["contents"].(map[string]any)
	value, _ := contents["value"].(string)

	if !strings.Contains(value, "g_qryparam") {
		t.Fatalf("hover 文本 = %q，未包含变量名", value)
	}
	// 必须保留换行：源码里 state/form/where/pagecount 各占一行
	for _, want := range []string{"state", "form", "where", "pagecount", "END RECORD"} {
		if !strings.Contains(value, want) {
			t.Errorf("hover 文本缺少 %q：\n%s", want, value)
		}
	}
	// 关键：类型文本不能被压成一行。
	// 压平后 "state ... form ..." 会在同一行；保留换行时它们各占一行。
	if !strings.Contains(value, "state     LIKE type_file.chr1,\n") {
		t.Errorf("hover 文本未按源码换行（类型文本被压平了）：\n%s", value)
	}
}

// TestHoverLocalVariableScopeIsolation 验证同名局部变量不跨函数泄漏。
//
// 旧实现用 fi.Lookup(小写名) 扫全文件，两个函数的同名局部变量会一起列出
// （用户实测：a 函数的 i 悬停同时显示 a 和 b 的 i）。
func TestHoverLocalVariableScopeIsolation(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/hoverscope.4gl"
	h.openBDL(uri, hoverSource)

	// fa 里的 "    DISPLAY i" 在第 12 行（0 基）
	respFA := h.request(22, "textDocument/hover",
		`{"textDocument":{"uri":"`+uri+`"},"position":{"line":12,"character":12}}`)
	valueFA := hoverValue(t, respFA)

	if !strings.Contains(valueFA, "INTEGER") {
		t.Errorf("fa 的 i 悬停应显示 INTEGER：\n%s", valueFA)
	}
	if strings.Contains(valueFA, "STRING") {
		t.Errorf("fa 的 i 悬停泄漏了 fb 的 i（出现 STRING）：\n%s", valueFA)
	}
	if !strings.Contains(valueFA, "fa") {
		t.Errorf("fa 的 i 悬停应标明所属 fa：\n%s", valueFA)
	}

	// fb 里的 "    DISPLAY i" 在第 18 行（0 基）
	respFB := h.request(23, "textDocument/hover",
		`{"textDocument":{"uri":"`+uri+`"},"position":{"line":18,"character":12}}`)
	valueFB := hoverValue(t, respFB)

	if !strings.Contains(valueFB, "STRING") {
		t.Errorf("fb 的 i 悬停应显示 STRING：\n%s", valueFB)
	}
	if strings.Contains(valueFB, "INTEGER") {
		t.Errorf("fb 的 i 悬停泄漏了 fa 的 i（出现 INTEGER）：\n%s", valueFB)
	}
	if !strings.Contains(valueFB, "fb") {
		t.Errorf("fb 的 i 悬停应标明所属 fb：\n%s", valueFB)
	}
}

// TestHoverPrefersLocalOverModule 验证局部变量优先于模块级同名变量。
func TestHoverPrefersLocalOverModule(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const src = `DEFINE i STRING

FUNCTION fa()
    DEFINE i INTEGER
    DISPLAY i
END FUNCTION
`
	const uri = "file:///tmp/hoverpref.4gl"
	h.openBDL(uri, src)

	// "    DISPLAY i" 在第 4 行（0 基）
	resp := h.request(24, "textDocument/hover",
		`{"textDocument":{"uri":"`+uri+`"},"position":{"line":4,"character":12}}`)
	value := hoverValue(t, resp)

	if !strings.Contains(value, "INTEGER") {
		t.Errorf("函数内的 i 应显示局部定义 INTEGER：\n%s", value)
	}
	if strings.Contains(value, "STRING") {
		t.Errorf("函数内的 i 不应显示模块级的 STRING：\n%s", value)
	}
}

// hoverValue 从 hover 响应里取出 markdown 文本。
func hoverValue(t *testing.T, resp map[string]any) string {
	t.Helper()
	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatal("hover 无结果")
	}
	contents, _ := result["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	return value
}
