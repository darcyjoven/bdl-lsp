package server

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

// requestSemanticTokens 发 semanticTokens/full 请求，返回 data 数组。
func (h *harness) requestSemanticTokens(t *testing.T, uri string) []uint32 {
	t.Helper()
	payload := fmt.Sprintf(
		`{"jsonrpc":"2.0","id":%d,"method":"textDocument/semanticTokens/full","params":{`+
			`"textDocument":{"uri":%q}}}`,
		nextRequestID(), uri)
	h.send(payload)
	msg := h.readMessage(5 * time.Second)
	if msg == nil {
		return nil
	}
	res, _ := msg["result"].(map[string]any)
	if res == nil {
		return nil
	}
	raw, _ := res["data"].([]any)
	out := make([]uint32, 0, len(raw))
	for _, v := range raw {
		if f, ok := v.(float64); ok {
			out = append(out, uint32(f))
		}
	}
	return out
}

// decodedToken 是一条解码后的 token（绝对位置）。
type decodedToken struct {
	Line, Char, Length int
	Type, Mods         int
}

// decodeTokens 把 LSP 的相对编码还原成绝对位置。
//
// data 每 5 个 uint32 一组：deltaLine, deltaChar, length, type, mods。
func decodeTokens(data []uint32) []decodedToken {
	var out []decodedToken
	line, char := 0, 0
	for i := 0; i+4 < len(data); i += 5 {
		dLine := int(data[i])
		dChar := int(data[i+1])
		if dLine > 0 {
			line += dLine
			char = dChar
		} else {
			char += dChar
		}
		out = append(out, decodedToken{
			Line: line, Char: char, Length: int(data[i+2]),
			Type: int(data[i+3]), Mods: int(data[i+4]),
		})
	}
	return out
}

// TestSemanticTokensCapability 必须声明 legend 与 full。
func TestSemanticTokensCapability(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	msg := h.readMessage(3 * time.Second)

	caps, _ := msg["result"].(map[string]any)["capabilities"].(map[string]any)
	if caps == nil {
		t.Fatal("未返回 capabilities")
	}
	stp, ok := caps["semanticTokensProvider"].(map[string]any)
	if !ok {
		t.Fatalf("未声明 semanticTokensProvider：%v", caps)
	}
	legend, _ := stp["legend"].(map[string]any)
	if legend == nil {
		t.Fatal("缺 legend")
	}
	types, _ := legend["tokenTypes"].([]any)
	if len(types) == 0 {
		t.Error("legend.tokenTypes 为空")
	}
	if types[0] != "variable" {
		t.Errorf("tokenTypes 顺序必须与 semtok.go 常量一致（首项 variable），实际 %v", types[0])
	}
	mods, _ := legend["tokenModifiers"].([]any)
	if len(mods) != 3 {
		t.Errorf("应有 3 个修饰位，实际 %d: %v", len(mods), mods)
	}
	if v, _ := stp["full"].(bool); !v {
		t.Error("应声明 full=true")
	}
}

// TestSemanticTokensFunction 函数定义应标为 function + declaration。
func TestSemanticTokensFunction(t *testing.T) {
	h := initPlainServer(t)
	src := "FUNCTION my_func()\n    DISPLAY 1\nEND FUNCTION\n"
	const uri = "file:///tmp/semtok_fn.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	toks := decodeTokens(h.requestSemanticTokens(t, uri))
	if len(toks) == 0 {
		t.Fatal("应返回 token")
	}
	var found *decodedToken
	for i := range toks {
		if toks[i].Line == 0 && toks[i].Char == 9 { // "FUNCTION " 之后
			found = &toks[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("找不到 my_func 的 token，全部：%+v", toks)
	}
	if found.Type != tokTypeFunction {
		t.Errorf("类型应为 function(%d)，实际 %d", tokTypeFunction, found.Type)
	}
	if found.Mods&modDeclaration == 0 {
		t.Errorf("应有 declaration 修饰，实际 %d", found.Mods)
	}
	if found.Length != len("my_func") {
		t.Errorf("长度应为 %d，实际 %d", len("my_func"), found.Length)
	}
}

// TestSemanticTokensConstantReadonly 常量应带 readonly。
func TestSemanticTokensConstantReadonly(t *testing.T) {
	h := initPlainServer(t)
	src := "CONSTANT c_max = 100\nMAIN\n    DISPLAY c_max\nEND MAIN\n"
	const uri = "file:///tmp/semtok_const.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	toks := decodeTokens(h.requestSemanticTokens(t, uri))
	var found *decodedToken
	for i := range toks {
		if toks[i].Line == 0 {
			found = &toks[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("找不到常量 token，全部：%+v", toks)
	}
	if found.Type != tokTypeVariable {
		t.Errorf("常量类型应为 variable(%d)，实际 %d", tokTypeVariable, found.Type)
	}
	if found.Mods&modReadonly == 0 {
		t.Errorf("常量应有 readonly 修饰，实际 %d", found.Mods)
	}
}

// TestSemanticTokensGlobalsStatic GLOBALS 块内符号应带 static。
func TestSemanticTokensGlobalsStatic(t *testing.T) {
	h := initPlainServer(t)
	src := "GLOBALS\nDEFINE g_count INTEGER\nEND GLOBALS\nMAIN\n    DISPLAY g_count\nEND MAIN\n"
	const uri = "file:///tmp/semtok_glob.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	toks := decodeTokens(h.requestSemanticTokens(t, uri))
	var found *decodedToken
	for i := range toks {
		if toks[i].Line == 1 { // DEFINE g_count 行
			found = &toks[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("找不到 GLOBALS 符号 token，全部：%+v", toks)
	}
	if found.Mods&modStatic == 0 {
		t.Errorf("GLOBALS 符号应有 static 修饰，实际 %d", found.Mods)
	}
}

// TestSemanticTokensSorted 编码必须按行/列升序（LSP 硬要求）。
func TestSemanticTokensSorted(t *testing.T) {
	h := initPlainServer(t)
	src := strings.Join([]string{
		"FUNCTION alpha()",
		"    DEFINE x INTEGER",
		"END FUNCTION",
		"FUNCTION beta()",
		"    DEFINE y INTEGER",
		"END FUNCTION",
		"",
	}, "\n")
	const uri = "file:///tmp/semtok_sorted.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	toks := decodeTokens(h.requestSemanticTokens(t, uri))
	if len(toks) < 4 {
		t.Fatalf("token 太少：%+v", toks)
	}
	for i := 1; i < len(toks); i++ {
		prev, cur := toks[i-1], toks[i]
		if cur.Line < prev.Line {
			t.Fatalf("行号未升序：%d 在 %d 之后", cur.Line, prev.Line)
		}
		if cur.Line == prev.Line && cur.Char < prev.Char {
			t.Fatalf("同行列号未升序：%d 在 %d 之后", cur.Char, prev.Char)
		}
	}
}

// TestSemanticTokensReliable 每条 token 的 length 必须为正（否则客户端会崩）。
func TestSemanticTokensReliable(t *testing.T) {
	h := initPlainServer(t)
	src := "FUNCTION f1()\n    DEFINE a, b INTEGER\n    LET a = 1\nEND FUNCTION\n"
	const uri = "file:///tmp/semtok_len.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	for _, tok := range decodeTokens(h.requestSemanticTokens(t, uri)) {
		if tok.Length <= 0 {
			t.Errorf("token 长度必须为正，实际 %+v", tok)
		}
		if tok.Type < 0 || tok.Type > tokTypeKeyword {
			t.Errorf("类型越界：%+v", tok)
		}
	}
}

// TestSemanticTokensEmptyDoc 空文档返回空 data 而不是报错。
func TestSemanticTokensEmptyDoc(t *testing.T) {
	h := initPlainServer(t)
	const uri = "file:///tmp/semtok_empty.4gl"
	openDocAndDrain(t, h, uri, "bdl", "\n")

	toks := h.requestSemanticTokens(t, uri)
	if len(toks) != 0 {
		t.Errorf("空文档不该有 token，实际 %d 个", len(toks))
	}
}

// TestSemanticTokensNoKeyword 不应发关键字 token（交给 TextMate）。
func TestSemanticTokensNoKeyword(t *testing.T) {
	h := initPlainServer(t)
	// 只有关键字，没有用户符号
	src := "MAIN\n    IF 1 THEN\n        DISPLAY 1\n    END IF\nEND MAIN\n"
	const uri = "file:///tmp/semtok_kw.4gl"
	openDocAndDrain(t, h, uri, "bdl", src)

	for _, tok := range decodeTokens(h.requestSemanticTokens(t, uri)) {
		if tok.Type == tokTypeKeyword {
			t.Errorf("不该发 keyword token（TextMate 已覆盖），实际 %+v", tok)
		}
	}
}
