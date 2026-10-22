package format

import (
	"strings"
	"testing"

	"github.com/darcyjoven/bdl-lsp/parser"
)

// fmtOnce 解析并格式化一次（失败终止测试）。
func fmtOnce(t *testing.T, src string, opts Options) string {
	t.Helper()
	pool, err := parser.New()
	if err != nil {
		t.Fatalf("parser.New: %v", err)
	}
	t.Cleanup(pool.Close)
	tree, err := pool.Parse(parser.LangBDL, []byte(src), nil)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	t.Cleanup(tree.Close)
	out, ok := Format(tree, []byte(src), opts)
	if !ok {
		t.Fatalf("拒绝格式化（语法树有 ERROR）：%s", src)
	}
	return out
}

// fmtTwice 验证幂等：format(format(x)) == format(x)。
func fmtTwice(t *testing.T, src string, opts Options) string {
	t.Helper()
	once := fmtOnce(t, src, opts)
	twice := fmtOnce(t, once, opts)
	if once != twice {
		t.Errorf("幂等性失败：\n--- 第一次 ---\n%s\n--- 第二次 ---\n%s", once, twice)
	}
	return once
}

const sampleNesting = `MAIN
DEFINE x INTEGER
IF x > 0 THEN
DISPLAY "pos"
ELSE
DISPLAY "neg"
END IF
CASE x
WHEN 1
DISPLAY "one"
OTHERWISE
DISPLAY "many"
END CASE
INPUT x FROM f
BEFORE FIELD f
DISPLAY "bf"
AFTER FIELD f
DISPLAY "af"
END INPUT
END MAIN
`

func TestIndentSpaces(t *testing.T) {
	out := fmtTwice(t, sampleNesting, Options{IndentUnit: "    "})
	want := `MAIN
    DEFINE x INTEGER
    IF x > 0 THEN
        DISPLAY "pos"
    ELSE
        DISPLAY "neg"
    END IF
    CASE x
        WHEN 1
            DISPLAY "one"
        OTHERWISE
            DISPLAY "many"
    END CASE
    INPUT x FROM f
        BEFORE FIELD f
            DISPLAY "bf"
        AFTER FIELD f
            DISPLAY "af"
    END INPUT
END MAIN
`
	if out != want {
		t.Errorf("缩进结果不符：\n--- 期望 ---\n%s\n--- 实际 ---\n%s", want, out)
	}
}

func TestIndentTabs(t *testing.T) {
	out := fmtTwice(t, "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n", Options{IndentUnit: "\t"})
	want := "MAIN\n\tIF x THEN\n\t\tDISPLAY y\n\tEND IF\nEND MAIN\n"
	if out != want {
		t.Errorf("tab 缩进结果不符：\n--- 期望 ---\n%q\n--- 实际 ---\n%q", want, out)
	}
}

func TestIndentTwoSpaces(t *testing.T) {
	out := fmtOnce(t, "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n", Options{IndentUnit: "  "})
	if !strings.Contains(out, "\n  IF x THEN") || !strings.Contains(out, "\n    DISPLAY y") {
		t.Errorf("2 空格缩进不正确：\n%s", out)
	}
}

func TestKeywordUpper(t *testing.T) {
	// 输入用小写关键字；标识符 myvar、字符串内容、注释都不许改
	src := "main\nif myvar > 0 then\n-- if comment then\nDISPLAY \"if then else\"\nend if\nend main\n"
	out := fmtOnce(t, src, Options{KeywordCase: "upper", IndentUnit: "   "})
	if !strings.Contains(out, "MAIN") || !strings.Contains(out, "IF myvar > 0 THEN") {
		t.Errorf("关键字未大写：%q", out)
	}
	if !strings.Contains(out, "-- if comment then") {
		t.Errorf("注释被改写：%q", out)
	}
	if !strings.Contains(out, `"if then else"`) {
		t.Errorf("字符串内容被改写：%q", out)
	}
	if !strings.Contains(out, "myvar") {
		t.Errorf("标识符被改写：%q", out)
	}
}

func TestKeywordLower(t *testing.T) {
	src := "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n"
	out := fmtOnce(t, src, Options{KeywordCase: "lower"})
	if !strings.Contains(out, "main") || !strings.Contains(out, "if x then") || !strings.Contains(out, "end if") {
		t.Errorf("关键字未小写：%q", out)
	}
}

func TestKeywordUnchanged(t *testing.T) {
	src := "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n"
	out := fmtOnce(t, src, Options{KeywordCase: "unchanged", IndentUnit: "  "})
	if !strings.Contains(out, "IF x THEN") {
		t.Errorf("unchanged 不应改大小写：%q", out)
	}
}

func TestMixedCaseKeywordsNormalized(t *testing.T) {
	// 混合大小写关键字统一到目标大小写
	src := "Main\nIf x Then\nDisplay y\nEnd If\nEnd Main\n"
	out := fmtOnce(t, src, Options{KeywordCase: "upper", IndentUnit: "  "})
	if !strings.Contains(out, "MAIN") || !strings.Contains(out, "IF x THEN") || !strings.Contains(out, "DISPLAY y") {
		t.Errorf("混合大小写未统一：%q", out)
	}
}

func TestBlankLinesPreserved(t *testing.T) {
	src := "MAIN\n\nDISPLAY x\n\n\nDISPLAY y\nEND MAIN\n"
	out := fmtOnce(t, src, Options{IndentUnit: "    "})
	// 空行保持为空行（不添加缩进）
	lines := strings.Split(out, "\n")
	if lines[1] != "" || lines[3] != "" || lines[4] != "" {
		t.Errorf("空行被改动：%q", out)
	}
}

func TestBlockCommentInteriorUntouched(t *testing.T) {
	// 多行块注释内部的行不改缩进（防止改坏注释排版）
	src := "MAIN\n{ comment line 1\n     comment line 2\ncomment line 3 }\nDISPLAY x\nEND MAIN\n"
	out := fmtOnce(t, src, Options{IndentUnit: "    "})
	// 注释起始行可以有缩进，内部行保持原样
	if !strings.Contains(out, "     comment line 2") {
		t.Errorf("块注释内部被改缩进：%q", out)
	}
	if !strings.Contains(out, "    DISPLAY x") {
		t.Errorf("注释后的语句缩进不正确：%q", out)
	}
}

func TestErrorTreeRefusesFormatting(t *testing.T) {
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	src := []byte("MAIN\n DISPLAY \"unterminated\nEND MAIN\n")
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(tree.Close)
	out, ok := Format(tree, src, Options{KeywordCase: "upper", IndentUnit: "    "})
	if ok {
		t.Error("语法错误时应拒绝格式化")
	}
	if out != string(src) {
		t.Error("拒绝格式化时应返回原文")
	}
}

func TestFunctionAndReport(t *testing.T) {
	src := `FUNCTION f(p)
DEFINE p INTEGER
LET p = 1
END FUNCTION
REPORT r(p)
DEFINE p INTEGER
OUTPUT
TOP MARGIN 2
FORMAT
ON EVERY ROW
PRINTX p
END REPORT
`
	out := fmtTwice(t, src, Options{IndentUnit: "  "})
	want := `FUNCTION f(p)
  DEFINE p INTEGER
  LET p = 1
END FUNCTION
REPORT r(p)
  DEFINE p INTEGER
  OUTPUT
    TOP MARGIN 2
  FORMAT
    ON EVERY ROW
      PRINTX p
END REPORT
`
	if out != want {
		t.Errorf("函数/报表缩进不符：\n--- 期望 ---\n%s\n--- 实际 ---\n%s", want, out)
	}
}

func TestRecordAndGlobals(t *testing.T) {
	src := `GLOBALS
DEFINE g_a INTEGER
DEFINE g_b INTEGER
END GLOBALS
TYPE t_rec RECORD
a INTEGER,
b STRING
END RECORD
`
	out := fmtTwice(t, src, Options{IndentUnit: "   "})
	want := `GLOBALS
   DEFINE g_a INTEGER
   DEFINE g_b INTEGER
END GLOBALS
TYPE t_rec RECORD
   a INTEGER,
   b STRING
END RECORD
`
	if out != want {
		t.Errorf("GLOBALS/RECORD 缩进不符：\n--- 期望 ---\n%s\n--- 实际 ---\n%s", want, out)
	}
}

// TestSameLineBlocksCountOnce 验证同一行开启的多个块只贡献一级缩进。
//
// `DEFINE l_rec RECORD` 同时开启 define 与 record 两个块；若按祖先个数
// 累加，记录体每行会多缩进一级（回归用例）。
func TestSameLineBlocksCountOnce(t *testing.T) {
	src := `MAIN
DEFINE l_rec RECORD
id INTEGER,
name VARCHAR(50)
END RECORD
END MAIN
`
	out := fmtTwice(t, src, Options{IndentUnit: "   "})
	want := `MAIN
   DEFINE l_rec RECORD
      id INTEGER,
      name VARCHAR(50)
   END RECORD
END MAIN
`
	if out != want {
		t.Errorf("同一行多块缩进不符：\n--- 期望 ---\n%s\n--- 实际 ---\n%s", want, out)
	}
}

func TestExistingIndentReplaced(t *testing.T) {
	// 已有错误缩进被修正（不是追加）
	src := "MAIN\n          DISPLAY x\nEND MAIN\n"
	out := fmtOnce(t, src, Options{IndentUnit: "    "})
	if strings.Contains(out, "          DISPLAY") {
		t.Errorf("原缩进未被替换：%q", out)
	}
	if !strings.Contains(out, "    DISPLAY x") {
		t.Errorf("缩进未修正为 4 空格：%q", out)
	}
}

func TestTryCatch(t *testing.T) {
	src := "MAIN\nTRY\nDISPLAY x\nCATCH\nDISPLAY y\nEND TRY\nEND MAIN\n"
	out := fmtTwice(t, src, Options{IndentUnit: "  "})
	want := "MAIN\n  TRY\n    DISPLAY x\n  CATCH\n    DISPLAY y\n  END TRY\nEND MAIN\n"
	if out != want {
		t.Errorf("TRY/CATCH 缩进不符：\n--- 期望 ---\n%q\n--- 实际 ---\n%q", want, out)
	}
}

func TestNestedIfElse(t *testing.T) {
	src := `MAIN
IF a THEN
IF b THEN
DISPLAY x
ELSE
DISPLAY y
END IF
END IF
END MAIN
`
	out := fmtTwice(t, src, Options{IndentUnit: "  "})
	want := `MAIN
  IF a THEN
    IF b THEN
      DISPLAY x
    ELSE
      DISPLAY y
    END IF
  END IF
END MAIN
`
	if out != want {
		t.Errorf("嵌套 IF/ELSE 缩进不符：\n--- 期望 ---\n%s\n--- 实际 ---\n%s", want, out)
	}
}

func TestNoIndentUnitNoEdits(t *testing.T) {
	// 缩进单元为空：只做关键字大小写
	src := "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n"
	out := fmtOnce(t, src, Options{KeywordCase: "upper", IndentUnit: ""})
	if out != "MAIN\nIF x THEN\nDISPLAY y\nEND IF\nEND MAIN\n" {
		t.Errorf("空缩进单元不应改动空白：%q", out)
	}
}

func TestPreprocUntouched(t *testing.T) {
	// 预处理指令（#include 等）不改写
	src := "#include \"globals.4gl\"\nMAIN\nDISPLAY x\nEND MAIN\n"
	out := fmtOnce(t, src, Options{KeywordCase: "upper", IndentUnit: "  "})
	if !strings.HasPrefix(out, "#include \"globals.4gl\"") {
		t.Errorf("预处理指令被改写：%q", out)
	}
}
