// rename.go 处理 textDocument/prepareRename 与 textDocument/rename（P6）。
//
// 范围与 references 保持一致：**只改当前文件**。跨文件改名需要工作区级
// 引用索引（PLAN §7.6），在索引完成前做跨文件改名会漏改——那比不支持更糟。
//
// 安全规则（宁可不改，不可改错）：
//   - 新名字必须是合法的 Genero 标识符；
//   - 不能改成关键字（会破坏解析）；
//   - 不能改成与文件内已有符号同名（会静默改变语义）；
//   - 光标不在可改名的符号上时，prepareRename 明确拒绝（而不是等到
//     执行时返回空编辑，那样用户以为改成功了）。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/completion"
	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// handlePrepareRename 判断光标处能否改名，返回可改名的范围。
//
// 客户端会先调这个：返回错误就完全不弹输入框，避免用户白输一遍。
func (s *Server) handlePrepareRename(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.PrepareRenameParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, err)
	}

	doc := s.docs.Get(string(params.TextDocument.URI))
	if doc == nil {
		return reply(ctx, nil, nil)
	}
	fi, idx := s.indexFor(doc)
	if fi == nil || idx == nil {
		return reply(ctx, nil, nil)
	}

	offset := idx.Offset(int(params.Position.Line), int(params.Position.Character))
	word := wordAt(doc.Text(), offset)
	if word == "" {
		return reply(ctx, nil, fmt.Errorf("光标不在标识符上"))
	}
	if reason := s.renameBlockReason(doc, fi, word, offset); reason != "" {
		return reply(ctx, nil, fmt.Errorf("%s", reason))
	}

	// 返回 {range, placeholder} 裸对象：LSP 3.15 的 PrepareRenameResult
	// 是"Range | {range, placeholder}"联合类型，go.lsp.dev/protocol v0.12
	// 没有对应结构体，用 map 表达即可（json 序列化结果符合规范）。
	start, end := wordRangeAt(doc.Text(), offset)
	return reply(ctx, map[string]any{
		"range":       byteRange(idx, start, end),
		"placeholder": word,
	}, nil)
}

// handleRename 执行改名，返回 WorkspaceEdit。
func (s *Server) handleRename(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.RenameParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, err)
	}

	doc := s.docs.Get(string(params.TextDocument.URI))
	if doc == nil {
		return reply(ctx, nil, nil)
	}
	fi, idx := s.indexFor(doc)
	if fi == nil || idx == nil {
		return reply(ctx, nil, nil)
	}

	offset := idx.Offset(int(params.Position.Line), int(params.Position.Character))
	word := wordAt(doc.Text(), offset)
	if word == "" {
		return reply(ctx, nil, fmt.Errorf("光标不在标识符上"))
	}
	if reason := s.renameBlockReason(doc, fi, word, offset); reason != "" {
		return reply(ctx, nil, fmt.Errorf("%s", reason))
	}

	newName := strings.TrimSpace(params.NewName)
	if reason := validateNewName(newName, word, fi); reason != "" {
		return reply(ctx, nil, fmt.Errorf("%s", reason))
	}

	occurrences := s.findOccurrences(doc, word)
	if len(occurrences) == 0 {
		return reply(ctx, nil, fmt.Errorf("找不到 %q 的出现位置", word))
	}

	edits := make([]protocol.TextEdit, 0, len(occurrences))
	for _, occ := range occurrences {
		edits = append(edits, protocol.TextEdit{
			Range:   byteRange(idx, occ.start, occ.end),
			NewText: newName,
		})
	}

	return reply(ctx, protocol.WorkspaceEdit{
		Changes: map[protocol.DocumentURI][]protocol.TextEdit{
			params.TextDocument.URI: edits,
		},
	}, nil)
}

// renameBlockReason 返回不可改名的原因；可改名时返回空串。
func (s *Server) renameBlockReason(doc *document.Document, fi *index.FileIndex,
	word string, offset int) string {

	_, tree, _ := doc.Snapshot()
	if tree == nil {
		return "文件无法解析"
	}
	// 注释/字符串里的词不是符号，改名会把注释改花。
	if offsetInTrivia(tree, uint(offset)) {
		return "光标位于注释或字符串内"
	}

	lower := strings.ToLower(word)

	// 先判断"是不是本文件声明的符号"：这是最关键的前置条件。
	// 顺序很重要——用户完全可能声明一个与关键字同名的标识符
	// （`DEFINE first INTEGER`，FIRST 是合法 BDL 关键字但也可作变量名），
	// 那种情况下应当能改名，不能因为"名字像关键字"就拒绝。
	if !hasSymbolNamed(fi, lower) {
		// 给更有用的原因：关键字 vs 内建函数/未声明标识符。
		if isKeyword(word, doc.Lang) {
			return fmt.Sprintf("%q 是关键字，不能改名", word)
		}
		return fmt.Sprintf("%q 不是本文件声明的符号，不能改名", word)
	}

	return ""
}

// isKeyword 判断是否是 BDL 或 .per 的关键字。
func isKeyword(word string, lang parser.Lang) bool {
	lower := strings.ToLower(word)
	list := completion.BDLKeywords
	if lang == parser.LangPer {
		list = completion.PERKeywords
	}
	for _, kw := range list {
		if strings.ToLower(kw) == lower {
			return true
		}
	}
	return false
}

// validateNewName 校验新名字。
func validateNewName(newName, oldName string, fi *index.FileIndex) string {
	if newName == oldName {
		return "新名字与原名相同"
	}
	if !isValidIdent(newName) {
		return fmt.Sprintf("%q 不是合法的 Genero 标识符（只能含字母、数字、下划线、$、#，且不能以数字开头）", newName)
	}

	lower := strings.ToLower(newName)
	// 改成关键字会破坏解析。
	for _, kw := range completion.BDLKeywords {
		if strings.ToLower(kw) == lower {
			return fmt.Sprintf("%q 是 BDL 关键字，不能作为名字", newName)
		}
	}
	// 文件里已有同名符号 → 会产生两个同名符号，静默改变语义。
	if hasSymbolNamed(fi, lower) {
		return fmt.Sprintf("%q 与文件内已有符号同名", newName)
	}
	return ""
}

// hasSymbolNamed 判断文件索引里是否有同名符号（大小写不敏感）。
func hasSymbolNamed(fi *index.FileIndex, lower string) bool {
	if fi == nil {
		return false
	}
	for _, sym := range fi.Symbols {
		if sym.Lower == lower {
			return true
		}
	}
	return false
}

// isValidIdent 判断是否是合法的 Genero 标识符。
//
// 规则：首字符为字母或下划线；后续字符为字母、数字、下划线、`$`、`#`。
// 与 completion.isIdentByte 保持一致的字符集。
func isValidIdent(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLetter := c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '_'
		isDigit := c >= '0' && c <= '9'
		isExtra := c == '$' || c == '#'
		switch {
		case i == 0 && isDigit:
			return false
		case i == 0 && !isLetter && !isExtra:
			return false
		case i > 0 && !isLetter && !isDigit && !isExtra:
			return false
		}
	}
	return true
}

// wordRangeAt 返回偏移所在词的起止字节。
func wordRangeAt(text []byte, offset int) (uint, uint) {
	if offset < 0 || offset > len(text) {
		return 0, 0
	}
	start := offset
	for start > 0 && isIdentByte(text[start-1]) {
		start--
	}
	end := offset
	for end < len(text) && isIdentByte(text[end]) {
		end++
	}
	return uint(start), uint(end)
}
