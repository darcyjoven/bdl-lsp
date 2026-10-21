package server

import (
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/builtin"
	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	"go.lsp.dev/protocol"
)

// newBuiltinLoader 按当前配置重建内置符号表加载器。
//
// 覆盖文件解析失败只记 warn、不阻断：符号表不全比"完全不可用"好得多
// （失败原因在 builtin.Loader 内部已经记过日志）。
func (s *Server) newBuiltinLoader() {
	if s.builtins == nil {
		s.builtins = builtin.NewLoader(s.logger)
	}
	settings := s.settings()
	if !settings.BuiltinsEnabled() {
		// 关闭的语义是"回到 P7-C 之前"（所有内置名照常报未定义），
		// 由 builtinSet() 返回 nil 实现，这里不必再动加载器。
		s.logger.Info("内置符号表已关闭（bdl.builtins.enable=false），内置名将不再豁免诊断")
		return
	}
	if err := s.builtins.Configure(s.rootPath, settings.Builtins.Paths, settings.Builtins.Disable); err != nil {
		s.logger.Warn("内置符号表加载失败", "error", err)
	}
}

// builtinSet 返回当前生效的内置符号表（未启用时返回 nil）。
//
// 返回 nil 而不是空表，是为了让调用方的判断只剩一个 `if`：
// 关闭时诊断行为完全回到 P7-C 之前。
func (s *Server) builtinSet() *builtin.Set {
	if s == nil || s.builtins == nil {
		return nil
	}
	if !s.settings().BuiltinsEnabled() {
		return nil
	}
	return s.builtins.Get()
}

// isBuiltinName 报告名字是否属于内置符号（变量/函数/类/常量）。
func (s *Server) isBuiltinName(name string) bool {
	set := s.builtinSet()
	if set == nil {
		return false
	}
	return set.IsKnown(name)
}

// isBuiltinFunction 报告名字是否是内置函数。
func (s *Server) isBuiltinFunction(name string) bool {
	set := s.builtinSet()
	if set == nil {
		return false
	}
	return set.IsFunction(name)
}

// builtinCandidates 返回补全用的内置候选。
//
// 有前缀时只取前缀命中的（补全的候选集要小，否则每次按键都过一遍全表）；
// 前缀为空（手工触发）时给全表，交给 completion 的 MaxItems 截断。
// 被 disable 的名字不参与补全——用户明确说过"这个项目里它不算内置"。
func (s *Server) builtinCandidates(word string) []builtin.Symbol {
	set := s.builtinSet()
	if set == nil {
		return nil
	}
	syms := set.Prefix(word, 0)
	if len(syms) == 0 {
		return nil
	}
	out := syms[:0]
	for _, sym := range syms {
		if s.builtinDisabled(sym.Name) {
			continue
		}
		out = append(out, sym)
	}
	return out
}

// hoverBuiltin 生成内置符号的悬停内容（P7-C）。
//
// 只在"本文档与 GLOBALS 都查不到定义"时兜底调用：用户自己的同名符号
// 永远优先——否则项目里定义的 `status` 变量会被内置表冒充。
func (s *Server) hoverBuiltin(doc *document.Document, idx *textpos.Index, offset int, word string) *protocol.Hover {
	set := s.builtinSet()
	if set == nil || s.builtinDisabled(word) {
		return nil
	}
	sym, ok := set.Lookup(word)
	if !ok {
		return nil
	}

	var sb strings.Builder
	sb.WriteString("```bdl\n")
	if sym.Signature != "" {
		sb.WriteString(sym.Signature)
	} else {
		sb.WriteString(sym.Name)
	}
	sb.WriteString("\n```")
	if sym.Doc != "" {
		sb.WriteString("\n\n")
		sb.WriteString(sym.Doc)
	}
	sb.WriteString("\n\n---\n")
	sb.WriteString(sym.Kind.Label())
	if sym.Class != "" {
		sb.WriteString(" · ")
		sb.WriteString(sym.Class)
	}
	if sym.Category != "" {
		sb.WriteString(" · ")
		sb.WriteString(sym.Category)
	}
	if sym.Since != "" {
		sb.WriteString(" · Genero ")
		sb.WriteString(sym.Since)
	}
	sb.WriteString("\n\n来自内置符号表 `bdl.builtins`（可用 `.bdl-lsp/builtins.json` 覆盖）")

	return &protocol.Hover{
		Contents: protocol.MarkupContent{
			Kind:  protocol.Markdown,
			Value: sb.String(),
		},
		Range: ptrRange(byteRange(idx, uint(offset), uint(offset+len(word)))),
	}
}

// builtinDisabled 报告名字是否被配置/覆盖文件显式禁用。
//
// 禁用优先于内置表本身：用户写 disable 就是"这个项目里它不算内置"，
// 此时应当照常报"查不到定义"，否则用户没法把内置名恢复成普通标识符。
func (s *Server) builtinDisabled(name string) bool {
	if s == nil || s.builtins == nil {
		return false
	}
	return s.builtins.DisabledByConfig(name)
}

// maybeReloadBuiltins 在覆盖文件变更后重载符号表，返回是否发生了重载。
func (s *Server) maybeReloadBuiltins() bool {
	if s == nil || s.builtins == nil {
		return false
	}
	if !s.settings().BuiltinsEnabled() {
		return false
	}
	return s.builtins.MaybeReload()
}
