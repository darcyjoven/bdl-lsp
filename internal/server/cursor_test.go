package server

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// 游标 / prepare statement 的悬停与跳转回归（P7-B-5）。
//
// 背景：grammar 里 prepare/execute 的游标名用字段 `cursor` 承接
// （tree-sitter-bdl 的 rule/sql.js `_cid`），语义上却是 prepared statement。
// 修复前这些名字完全不被索引，悬停一律落空，跳转跳不回去。

// cursorSource 覆盖两种命名空间 + 四种来源形式。
const cursorSource = `MAIN
    DEFINE l_sql STRING
    DEFINE l_rec RECORD
        a INTEGER
    END RECORD
    PREPARE p_fill FROM l_sql
    DECLARE c_fill CURSOR FOR p_fill
    DECLARE c_static CURSOR FOR SELECT * FROM t1
    EXECUTE p_fill
    OPEN c_fill
    FETCH NEXT c_fill INTO l_rec
    CLOSE c_fill
END MAIN
`

// 行号基准（0 基）：
//
//	5  PREPARE p_fill FROM l_sql
//	6  DECLARE c_fill CURSOR FOR p_fill
//	7  DECLARE c_static CURSOR FOR SELECT * FROM t1
//	8  EXECUTE p_fill
//	9  OPEN c_fill
//	10 FETCH NEXT c_fill INTO l_rec
//	11 CLOSE c_fill

// TestHoverPrepareStatement 验证 PREPARE 声明与 EXECUTE 引用处都有悬停，
// 且展示 SQL 来源、不是 "function"。
func TestHoverPrepareStatement(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/curprepare.4gl"
	h.openBDL(uri, cursorSource)

	// 声明处：PREPARE p_fill（第 5 行，"p_fill" 从第 12 列开始）
	declValue := hoverValue(t, h.request(40, "textDocument/hover",
		`{"textDocument":{"uri":"`+uri+`"},"position":{"line":5,"character":14}}`))
	if !strings.Contains(declValue, "PREPARE p_fill") {
		t.Errorf("PREPARE 声明处悬停应显示 `PREPARE p_fill`：\n%s", declValue)
	}
	if !strings.Contains(strings.ToUpper(declValue), "L_SQL") {
		t.Errorf("PREPARE 声明处悬停应显示 SQL 来源 l_sql：\n%s", declValue)
	}
	if strings.Contains(strings.ToLower(declValue), "function") {
		t.Errorf("PREPARE 不该被当成 function：\n%s", declValue)
	}

	// 引用处：EXECUTE p_fill（第 8 行，"p_fill" 从第 12 列开始）
	useValue := hoverValue(t, h.request(41, "textDocument/hover",
		`{"textDocument":{"uri":"`+uri+`"},"position":{"line":8,"character":14}}`))
	if !strings.Contains(useValue, "PREPARE p_fill") {
		t.Errorf("EXECUTE 处悬停应指向 prepare statement：\n%s", useValue)
	}
}

// TestHoverCursor 验证游标声明与 OPEN/FETCH/CLOSE 引用处的悬停。
func TestHoverCursor(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/curcursor.4gl"
	h.openBDL(uri, cursorSource)

	cases := []struct {
		name       string
		line, char int
		reqID      int
	}{
		{"DECLARE 声明处", 6, 14, 42},
		{"OPEN 引用处", 9, 11, 43},
		{"FETCH 引用处", 10, 17, 44},
		{"CLOSE 引用处", 11, 12, 45},
	}
	for _, c := range cases {
		value := hoverValue(t, h.request(c.reqID, "textDocument/hover",
			`{"textDocument":{"uri":"`+uri+`"},"position":{"line":`+strconv.Itoa(c.line)+`,"character":`+strconv.Itoa(c.char)+`}}`))
		if !strings.Contains(value, "DECLARE c_fill CURSOR") {
			t.Errorf("%s 悬停应显示游标声明：\n%s", c.name, value)
		}
		// 游标绑的 prepare 要在悬停里能看出来，否则用户还得自己回头找。
		if !strings.Contains(value, "p_fill") {
			t.Errorf("%s 悬停应标明来源 prepare p_fill：\n%s", c.name, value)
		}
	}
}

// TestCursorStaticSQLHasNoPrepareRef 验证静态 SQL 来源的游标悬停不编造 prepare 名。
func TestCursorStaticSQLHasNoPrepareRef(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/curstatic.4gl"
	h.openBDL(uri, cursorSource)

	// DECLARE c_static CURSOR FOR SELECT ...（第 7 行）
	value := hoverValue(t, h.request(46, "textDocument/hover",
		`{"textDocument":{"uri":"`+uri+`"},"position":{"line":7,"character":14}}`))
	if !strings.Contains(value, "DECLARE c_static CURSOR") {
		t.Errorf("静态 SQL 游标悬停应显示游标声明：\n%s", value)
	}
	if strings.Contains(value, "FOR ") {
		t.Errorf("静态 SQL 来源没有 prepare 名，不该出现 `FOR <名字>`：\n%s", value)
	}
}

// TestDefinitionPrepareAndCursor 验证 prepare/cursor 的跳转定义。
//
// 关键：`OPEN c_fill` 必须跳到游标的 DECLARE（不是 prepare 的 PREPARE），
// `EXECUTE p_fill` 必须跳到 PREPARE。两个命名空间各跳各的。
func TestDefinitionPrepareAndCursor(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/curdef.4gl"
	h.openBDL(uri, cursorSource)

	cases := []struct {
		name       string
		line, char int
		reqID      int
		wantLine   int
	}{
		// EXECUTE p_fill → PREPARE 在第 5 行
		{"EXECUTE→PREPARE", 8, 14, 47, 5},
		// DECLARE ... FOR p_fill 里的来源名 → PREPARE 在第 5 行
		{"DECLARE 来源→PREPARE", 6, 33, 48, 5},
		// OPEN / FETCH / CLOSE 的游标名 → DECLARE 在第 6 行
		{"OPEN→DECLARE", 9, 11, 49, 6},
		{"FETCH→DECLARE", 10, 17, 50, 6},
		{"CLOSE→DECLARE", 11, 12, 51, 6},
	}
	for _, c := range cases {
		resp := h.request(c.reqID, "textDocument/definition",
			`{"textDocument":{"uri":"`+uri+`"},"position":{"line":`+strconv.Itoa(c.line)+`,"character":`+strconv.Itoa(c.char)+`}}`)
		line, ok := firstLocationLine(resp["result"])
		if !ok {
			t.Errorf("%s：没有跳转结果", c.name)
			continue
		}
		if line != c.wantLine {
			t.Errorf("%s：跳到第 %d 行，期望第 %d 行", c.name, line, c.wantLine)
		}
	}
}

// TestPrepareCursorInOutline 验证两者出现在大纲里且带正确的 Detail。
func TestPrepareCursorInOutline(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/curoutline.4gl"
	h.openBDL(uri, cursorSource)

	resp := h.request(52, "textDocument/documentSymbol",
		`{"textDocument":{"uri":"`+uri+`"}}`)

	details := map[string]string{}
	collectDetails(resp["result"], details)

	// PREPARE p_fill 的 Detail 应说明来源 SQL；游标应说明绑的 prepare。
	if d, ok := details["p_fill"]; !ok {
		t.Errorf("大纲里没有 p_fill，实际有 %v", detailKeys(details))
	} else if !strings.Contains(d, "PREPARE") {
		t.Errorf("p_fill 的 Detail = %q，应含 PREPARE", d)
	}
	if d, ok := details["c_fill"]; !ok {
		t.Errorf("大纲里没有 c_fill，实际有 %v", detailKeys(details))
	} else if !strings.Contains(d, "CURSOR") {
		t.Errorf("c_fill 的 Detail = %q，应含 CURSOR", d)
	} else if !strings.Contains(d, "p_fill") {
		t.Errorf("c_fill 的 Detail = %q，应标明来源 p_fill", d)
	}
}

// firstLocationLine 取出第一个跳转结果的行号。
//
// textDocument/definition 的结果既可能是单个 Location，
// 也可能是 Location[]（按作用域命中数决定），两种都要认。
func firstLocationLine(result any) (int, bool) {
	switch v := result.(type) {
	case map[string]any:
		return locationLine(v)
	case []any:
		if len(v) == 0 {
			return 0, false
		}
		loc, _ := v[0].(map[string]any)
		return locationLine(loc)
	}
	return 0, false
}

func locationLine(loc map[string]any) (int, bool) {
	if loc == nil {
		return 0, false
	}
	rng, _ := loc["range"].(map[string]any)
	if rng == nil {
		return 0, false
	}
	start, _ := rng["start"].(map[string]any)
	if start == nil {
		return 0, false
	}
	line, ok := start["line"].(float64)
	return int(line), ok
}

// collectDetails 把大纲里的 name→detail 收进 map（递归子节点）。
func collectDetails(result any, out map[string]string) {
	syms, ok := result.([]any)
	if !ok {
		return
	}
	for _, item := range syms {
		sym, _ := item.(map[string]any)
		if sym == nil {
			continue
		}
		name, _ := sym["name"].(string)
		detail, _ := sym["detail"].(string)
		if name != "" {
			out[name] = detail
		}
		collectDetails(sym["children"], out)
	}
}

// detailKeys 返回 map 的键（测试失败时给出实际有哪些名字）。
//
// 不能叫 keys——server_test.go 已有同名但签名不同的辅助函数，
// Go 不支持重载。
func detailKeys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

// TestCursorNamesDontTriggerUnresolvedDiag 验证 prepare/cursor 名不触发
// variable-source-unresolved。
//
// 这些名字是 SQL 句柄不是变量，queryVariables 的 `(variable (identifier))`
// 本来就抓不到它们（`cursor:` 字段是裸 identifier，不包 variable 节点），
// 但这条不变量值得钉住：一旦有人往 query 里加 identifier 匹配，
// 7 万多个 prepare/cursor 名会瞬间变成诊断噪声。
func TestCursorNamesDontTriggerUnresolvedDiag(t *testing.T) {
	dir := t.TempDir()
	h := initializeGlobals(t, dir)
	uri := testURI(dir, "cur.4gl")

	h.sendDidOpen(uri, "bdl", cursorSource, 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)

	u := findUnresolvedDiags(diags)
	for _, d := range u {
		msg, _ := d["message"].(string)
		if msg == "" {
			continue
		}
		// 诊断消息形如 `无法确定变量 "x" 的来源`，只要不出现
		// prepare/cursor 名就算干净。
		for _, name := range []string{"p_fill", "c_fill", "c_static"} {
			if strings.Contains(msg, name) {
				t.Errorf("prepare/cursor 名 %s 被当成未定义变量：%s", name, msg)
			}
		}
	}
}
