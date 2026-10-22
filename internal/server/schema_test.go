package server

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/diag"
)

// newSchemaWorkspace 建一个带 `.sch` 的工作区。
//
//	tiptop/aap/sch/apa_file.sch   create table apa_file (apa01 varchar2(10), apa02 number(5));
//	tiptop/aap/4gl/biz.4gl        DEFINE rec RECORD LIKE apa_file.*
func newSchemaWorkspace(t *testing.T) (root string, sch []string) {
	t.Helper()
	root = t.TempDir()
	writeFile(t, filepath.Join(root, "tiptop/aap/sch/apa_file.sch"),
		"create table apa_file (apa01 varchar2(10), apa02 number(5));\n")
	// 模组根探测要求存在 `<根>/<模组>/4gl` 目录，所以先放一个占位文件。
	writeFile(t, filepath.Join(root, "tiptop/aap/4gl/biz.4gl"), "MAIN\nEND MAIN\n")
	return root, nil
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

// TestSchemaRecordMembersKnown 有 schema 时 `RECORD LIKE t.*` 的列是已知成员。
func TestSchemaRecordMembersKnown(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root, _ := newSchemaWorkspace(t)
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
	root, _ := newSchemaWorkspace(t)
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
	if !strings.Contains(msg, "RECORD LIKE apa_file.*") && !strings.Contains(msg, "nosuch") {
		t.Errorf("诊断消息 = %q", msg)
	}
}

// TestSchemaMissingTableStaysSilent 表不在 schema 里 → 不校验（宁漏勿误）。
func TestSchemaMissingTableStaysSilent(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root, _ := newSchemaWorkspace(t)
	h.initWS(root)
	h.waitIndex()
	h.setRecordMemberLevel(t, "warning")

	src := strings.Replace(schemaRecordSrc, "apa_file.*", "unknown_file.*", 1)
	uri := fileURIOf(filepath.Join(root, "tiptop/aap/4gl/biz3.4gl"))
	h.sendDidOpen(uri, "bdl", src, 1)
	diags := h.drainDiagnostics(uri, 3*time.Second)
	if got := recordMemberDiags(diags); len(got) != 0 {
		t.Fatalf("表不在 schema 里时不该报：%v", got)
	}
}

// TestSchemaMembersComplete 补全：`rec.` 之后给出表里全部列。
func TestSchemaMembersComplete(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	root, _ := newSchemaWorkspace(t)
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
	labels := map[string]bool{}
	for _, it := range raw {
		m, _ := it.(map[string]any)
		if label, _ := m["label"].(string); label != "" {
			labels[strings.ToLower(label)] = true
		}
	}
	for _, want := range []string{"apa01", "apa02"} {
		if !labels[want] {
			t.Errorf("补全缺少列 %q（共 %d 项）", want, len(labels))
		}
	}
	// 有关键字混进来就说明走了旧路径
	if labels["select"] {
		t.Errorf("不应出现关键字")
	}
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

var _ = diag.CodeRecordMemberUnknown
