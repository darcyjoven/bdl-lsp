package server

// P2-1：数据库字段资料（`.project/.schema`）的读取、悬停扩展、补全与视图请求。
//
// 资料由 tiptop 生产（`tiptop schema import`），格式见 internal/schema。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newSchemaWorkspace 建一个带字段资料的工作区：
//
//	.project/.schema/tables.json + tables/{APA_FILE,IMA_FILE}.json
//	tiptop/aap/4gl/biz.4gl
func newSchemaWorkspace(t *testing.T) (root string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "tiptop/aap/4gl/biz.4gl"), "MAIN\nEND MAIN\n")
	writeFile(t, filepath.Join(root, ".project/.schema/tables.json"), `{
	  "schema": 1,
	  "importedAt": "2026-10-23T21:00:00+08:00",
	  "source": "gal_schema.csv",
	  "rowsTotal": 4,
	  "tables": {
	    "APA_FILE": {"name": "apa_file", "desc": "", "columns": 2, "file": "APA_FILE.json"},
	    "IMA_FILE": {"name": "ima_file", "desc": "物料主档", "columns": 2, "file": "IMA_FILE.json"}
	  }
	}`)
	writeFile(t, filepath.Join(root, ".project/.schema/tables/APA_FILE.json"), `{
	  "table": "APA_FILE",
	  "columns": [
	    {"name": "apa01", "desc": "物料编号", "type": "VARCHAR2", "length": 10},
	    {"name": "apa02", "desc": "数量", "type": "NUMBER", "precision": 5}
	  ]
	}`)
	writeFile(t, filepath.Join(root, ".project/.schema/tables/IMA_FILE.json"), `{
	  "table": "IMA_FILE",
	  "desc": "物料主档",
	  "columns": [
	    {"name": "ima01", "desc": "物料编号", "type": "CHAR", "length": 10},
	    {"name": "ima02", "desc": "数量", "type": "NUMBER", "precision": 8, "scale": 2}
	  ]
	}`)
	return root
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

const schemaRecordSrc = `DEFINE rec RECORD LIKE apa_file.*
MAIN
    LET rec.apa01 = "x"
    CALL biz_use()
END MAIN

FUNCTION biz_use()
    DISPLAY BY NAME rec.apa01
END FUNCTION
`

// TestSchemaRecordMembersKnown 有字段资料时 `RECORD LIKE t.*` 的列是已知成员。
func TestSchemaRecordMembersKnown(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()
	h.setRecordMemberLevel(t, "warning")

	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/biz.4gl"))
	h.sendDidOpen(uri, "bdl", schemaRecordSrc, 1)
	diags := h.drainDiagnostics(uri, 3*time.Second)
	if got := recordMemberDiags(diags); len(got) != 0 {
		t.Fatalf("apa01 是 apa_file 的列，不该报：%v", got)
	}
}

// TestSchemaRecordMemberUnknown 列以外的成员应报。
func TestSchemaRecordMemberUnknown(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()
	h.setRecordMemberLevel(t, "warning")

	src := strings.Replace(schemaRecordSrc, `DISPLAY BY NAME rec.apa01`, `DISPLAY BY NAME rec.nosuch`, 1)
	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/biz2.4gl"))
	h.sendDidOpen(uri, "bdl", src, 1)
	diags := h.drainDiagnostics(uri, 3*time.Second)
	got := recordMemberDiags(diags)
	if len(got) == 0 {
		t.Fatalf("nosuch 不是 apa_file 的列，应报一条")
	}
	msg, _ := got[0]["message"].(string)
	if !strings.Contains(msg, "nosuch") {
		t.Errorf("诊断消息 = %q", msg)
	}
}

// TestSchemaMissingTableStaysSilent 表不在资料里 → 不校验（宁漏勿误）。
func TestSchemaMissingTableStaysSilent(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()
	h.setRecordMemberLevel(t, "warning")

	src := strings.Replace(schemaRecordSrc, "apa_file.*", "unknown_file.*", 1)
	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/biz3.4gl"))
	h.sendDidOpen(uri, "bdl", src, 1)
	diags := h.drainDiagnostics(uri, 3*time.Second)
	if got := recordMemberDiags(diags); len(got) != 0 {
		t.Fatalf("表不在资料里时不该报：%v", got)
	}
}

// TestSchemaMembersComplete 补全：`rec.` 之后给出表里全部列，并带类型与中文说明。
func TestSchemaMembersComplete(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	src := "DEFINE rec RECORD LIKE apa_file.*\nMAIN\n    LET rec.\nEND MAIN\n"
	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/biz4.4gl"))
	h.sendDidOpen(uri, "bdl", src, 1)
	_ = h.drainDiagnostics(uri, 2*time.Second)

	resp := h.request(31, "textDocument/completion",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":2,"character":12}}`, uri))
	result, _ := resp["result"].(map[string]any)
	raw, _ := result["items"].([]any)
	if len(raw) == 0 {
		raw, _ = resp["result"].([]any)
	}
	byLabel := map[string]map[string]any{}
	for _, it := range raw {
		m, _ := it.(map[string]any)
		if label, _ := m["label"].(string); label != "" {
			byLabel[strings.ToLower(label)] = m
		}
	}
	apa01, ok := byLabel["apa01"]
	if !ok {
		t.Fatalf("补全缺少列 apa01（共 %d 项）", len(byLabel))
	}
	if detail, _ := apa01["detail"].(string); !strings.Contains(detail, "VARCHAR2(10)") {
		t.Errorf("补全项 detail 应带类型，实际 %q", detail)
	}
	if doc, _ := apa01["documentation"].(string); !strings.Contains(doc, "物料编号") {
		t.Errorf("补全项 documentation 应带中文说明，实际 %q", doc)
	}
	if _, has := byLabel["select"]; has {
		t.Errorf("不应出现关键字（精确成员优先）")
	}
}

// TestSchemaHoverLikeSingleColumn 悬停 `LIKE t.col` 的变量 → 追加该列说明。
func TestSchemaHoverLikeSingleColumn(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	src := "DEFINE fld LIKE apa_file.apa01\nMAIN\n    DISPLAY fld\nEND MAIN\n"
	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/biz5.4gl"))
	h.sendDidOpen(uri, "bdl", src, 1)
	_ = h.drainDiagnostics(uri, 2*time.Second)

	res := h.requestHover(t, uri, 0, 8) // 悬停 fld
	if res == nil {
		t.Fatal("应有悬停结果")
	}
	text := hoverTextOfResult(t, res)
	for _, want := range []string{"apa_file.apa01", "VARCHAR2(10)", "物料编号"} {
		if !strings.Contains(text, want) {
			t.Errorf("悬停应包含 %q，实际：\n%s", want, text)
		}
	}
}

// TestSchemaHoverRecordLikeListsColumns 悬停 `RECORD LIKE t.*` → 列出字段。
func TestSchemaHoverRecordLikeListsColumns(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/biz6.4gl"))
	h.sendDidOpen(uri, "bdl", schemaRecordSrc, 1)
	_ = h.drainDiagnostics(uri, 2*time.Second)

	res := h.requestHover(t, uri, 0, 8) // 悬停 rec
	if res == nil {
		t.Fatal("应有悬停结果")
	}
	text := hoverTextOfResult(t, res)
	for _, want := range []string{"apa_file", "apa01 · VARCHAR2(10) · 物料编号", "apa02 · NUMBER(5) · 数量"} {
		if !strings.Contains(text, want) {
			t.Errorf("悬停应包含 %q，实际：\n%s", want, text)
		}
	}
}

// TestSchemaHoverTableName 悬停表名本身 → 给出表摘要。
func TestSchemaHoverTableName(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/biz7.4gl"))
	h.sendDidOpen(uri, "bdl", "DEFINE r RECORD LIKE ima_file.*\nMAIN\nEND MAIN\n", 1)
	_ = h.drainDiagnostics(uri, 2*time.Second)

	res := h.requestHover(t, uri, 0, 24) // 悬停 `ima_file`
	if res == nil {
		t.Fatal("表名应能悬停出字段摘要")
	}
	text := hoverTextOfResult(t, res)
	for _, want := range []string{"ima_file", "物料主档", "ima01 · CHAR(10) · 物料编号"} {
		if !strings.Contains(text, want) {
			t.Errorf("悬停应包含 %q，实际：\n%s", want, text)
		}
	}
}

// TestSchemaTablesRequest 视图请求：正则过滤 + 总数。
func TestSchemaTablesRequest(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	call := func(params string) map[string]any {
		resp := h.request(nextRequestID(), MethodSchemaTables, params)
		res, _ := resp["result"].(map[string]any)
		if res == nil {
			t.Fatalf("没有 result：%v", resp)
		}
		return res
	}

	all := call("{}")
	if enabled, _ := all["enabled"].(bool); !enabled {
		t.Error("默认应启用（客户端没下发时的兜底是启用）")
	}
	if present, _ := all["present"].(bool); !present {
		t.Fatal("有资料时应 present")
	}
	if total, _ := all["total"].(float64); total != 2 {
		t.Errorf("表总数应为 2，实际 %v", all["total"])
	}
	if tables, _ := all["tables"].([]any); len(tables) != 2 {
		t.Errorf("应返回 2 张表，实际 %v", all["tables"])
	}

	// 正则：^APA
	one := call(`{"query":"^APA"}`)
	if matched, _ := one["matched"].(float64); matched != 1 {
		t.Errorf("^APA 应匹配 1 张，实际 %v", one["matched"])
	}
	if total, _ := one["total"].(float64); total != 2 {
		t.Errorf("total 应仍是 2（共 N 张表），实际 %v", one["total"])
	}
	tables, _ := one["tables"].([]any)
	if len(tables) == 1 {
		m, _ := tables[0].(map[string]any)
		if name, _ := m["name"].(string); name != "apa_file" {
			t.Errorf("匹配到的表应是 apa_file，实际 %q", name)
		}
	}

	// 非法正则：不中断，返回全部 + 错误提示
	bad := call(`{"query":"(ima"}`)
	if bad["filterError"] == nil || bad["filterError"] == "" {
		t.Error("非法正则应给 filterError")
	}
	if matched, _ := bad["matched"].(float64); matched != 2 {
		t.Errorf("非法正则应退回全部（matched=2），实际 %v", bad["matched"])
	}

	// limit
	lim := call(`{"limit":1}`)
	if returned, _ := lim["returned"].(float64); returned != 1 {
		t.Errorf("limit=1 应只返回 1 条，实际 %v", lim["returned"])
	}
}

// TestSchemaColumnsRequest 视图请求：单表列（惰性加载）。
func TestSchemaColumnsRequest(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	resp := h.request(nextRequestID(), MethodSchemaColumns, `{"table":"APA_FILE"}`)
	res, _ := resp["result"].(map[string]any)
	if res == nil {
		t.Fatalf("没有 result：%v", resp)
	}
	if table, _ := res["table"].(string); table != "apa_file" {
		t.Errorf("表名应回原写法 apa_file，实际 %q", table)
	}
	cols, _ := res["columns"].([]any)
	if len(cols) != 2 {
		t.Fatalf("apa_file 应有 2 列，实际 %d", len(cols))
	}
	first, _ := cols[0].(map[string]any)
	if name, _ := first["name"].(string); name != "apa01" {
		t.Errorf("第一列应是 apa01，实际 %q", name)
	}
	if desc, _ := first["desc"].(string); desc != "物料编号" {
		t.Errorf("列说明应是 物料编号，实际 %q", desc)
	}

	// 不存在的表：missing=true，不报错
	resp = h.request(nextRequestID(), MethodSchemaColumns, `{"table":"nosuch"}`)
	res, _ = resp["result"].(map[string]any)
	if missing, _ := res["missing"].(bool); !missing {
		t.Errorf("不存在的表应 missing=true，实际 %v", res)
	}
}

// TestSchemaDisabledByConfig 关掉开关：请求报 enabled=false，悬停不追加字段资料。
func TestSchemaDisabledByConfig(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()

	h.send(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"schema":{"enable":false}}}}`)
	time.Sleep(200 * time.Millisecond)

	resp := h.request(nextRequestID(), MethodSchemaTables, `{}`)
	res, _ := resp["result"].(map[string]any)
	if enabled, _ := res["enabled"].(bool); enabled {
		t.Errorf("关掉后 enabled 应为 false：%v", res)
	}
	if present, _ := res["present"].(bool); present {
		t.Errorf("关掉后不应再持有资料：%v", res)
	}

	src := "DEFINE rec RECORD LIKE apa_file.*\nMAIN\n    DISPLAY rec.apa01\nEND MAIN\n"
	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/biz8.4gl"))
	h.sendDidOpen(uri, "bdl", src, 1)
	_ = h.drainDiagnostics(uri, 2*time.Second)
	res = h.requestHover(t, uri, 0, 8)
	if res != nil {
		text := hoverTextOfResult(t, res)
		if strings.Contains(text, "字段资料") {
			t.Errorf("关掉后悬停不该有字段资料段：\n%s", text)
		}
	}
}

// hoverTextOfResult 取悬停**结果**里的 markdown 文本
// （hover_test.go 里的 hoverValue 收的是完整响应，两者不同）。
func hoverTextOfResult(t *testing.T, res map[string]any) string {
	t.Helper()
	contents, _ := res["contents"].(map[string]any)
	if contents == nil {
		return ""
	}
	v, _ := contents["value"].(string)
	return v
}

// TestRecordObjectName 多级限定不是简单变量（实测 Java 类访问误报的根因）。
func TestRecordObjectName(t *testing.T) {
	cases := []struct {
		text string
		want string
		ok   bool
	}{
		{"rec", "rec", true},
		{"  rec  ", "rec", true},
		{"g_bmb[l_ac]", "g_bmb", true},
		{"g_bmb [ 1 ]", "g_bmb", true},
		{"com.darcy.Hello", "", false},
		{"sr.apb", "", false},
		{"rec.field", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := recordObjectName(c.text)
		if got != c.want || ok != c.ok {
			t.Errorf("recordObjectName(%q) = (%q,%v)，期望 (%q,%v)", c.text, got, ok, c.want, c.ok)
		}
	}
}

// TestSchemaTableFor 只认整表展开。
func TestSchemaTableFor(t *testing.T) {
	cases := map[string]string{
		"RECORD LIKE maj_file.*": "maj_file",
		"RECORD LIKE APA_FILE.*": "APA_FILE",
		"record like t.*":        "t",
		"LIKE type_file.chr20":   "",
		"LIKE apa_file.apa01":    "",
		"":                       "",
		"RECORD":                 "",
	}
	for in, want := range cases {
		if got := schemaTableFor(in); got != want {
			t.Errorf("schemaTableFor(%q) = %q，期望 %q", in, got, want)
		}
	}
}

// TestSchemaTableColumnFor 单列声明 `LIKE t.col` 的解析。
func TestSchemaTableColumnFor(t *testing.T) {
	cases := []struct {
		in     string
		table  string
		column string
		ok     bool
	}{
		{"LIKE apa_file.apa01", "apa_file", "apa01", true},
		{"LIKE apa_file.apa01 ", "apa_file", "apa01", true},
		{"like ima_file.ima01", "ima_file", "ima01", true},
		{"LIKE apa_file.*", "", "", false},
		{"LIKE apa01", "", "", false},
		{"LIKE t.", "", "", false},
		{"", "", "", false},
	}
	for _, c := range cases {
		table, col, ok := schemaTableColumnFor(c.in)
		if table != c.table || col != c.column || ok != c.ok {
			t.Errorf("schemaTableColumnFor(%q) = (%q,%q,%v)，期望 (%q,%q,%v)",
				c.in, table, col, ok, c.table, c.column, c.ok)
		}
	}
}

// TestQualifierBeforeDot 取 `.` 左侧的变量名。
func TestQualifierBeforeDot(t *testing.T) {
	cases := map[string]string{
		"    LET rec.":       "rec",
		"    LET rec.ap":     "rec",
		"    g_bxj[3].":      "g_bxj",
		"    g_bxj[3].clear": "g_bxj",
		"    x := 1":         "",
		"":                   "",
	}
	for in, want := range cases {
		if got := qualifierBeforeDot(in); got != want {
			t.Errorf("qualifierBeforeDot(%q) = %q，期望 %q", in, got, want)
		}
	}
}
