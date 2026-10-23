// audit.go 提供诊断审计入口：在不起 LSP 连接的前提下跑完整语义诊断流水线。
//
// 用途：`cmd/diag-audit` 与语料回归需要“每个诊断码各报了多少条”这一
// 数据，而 `bdl-lsp check` 只做语法、不跑语义诊断。走真实 LSP 会话能拿到
// 同样的结果，但每文件一次 initialize/管道开销，全量语料慢到不可用。
//
// 这里刻意只暴露最小面：审计不看语法问题，也不发布——只返回语义诊断列表。
package server

import (
	"context"
	"encoding/json"
	"time"

	"github.com/darcyjoven/bdl-lsp/builtin"
	"github.com/darcyjoven/bdl-lsp/internal/config"
	"github.com/darcyjoven/bdl-lsp/internal/diag"
	"github.com/darcyjoven/bdl-lsp/link"
	"github.com/darcyjoven/bdl-lsp/parser"
	"go.lsp.dev/protocol"
)

// EnableResolvers 初始化 GLOBALS / 表单解析器（等价于 initialize 阶段的工作）。
//
// 不调用时 globalsChecks / formChecks 静默跳过——审计在只关心
// variable-source-unresolved / function-not-found 时可以省掉这一步；
// 但要统计 globals-file-not-found 与表单类诊断就必须调用。
func (s *Server) EnableResolvers(rootPath string) {
	s.rootPath = rootPath
	s.newGlobalsResolver(rootPath)
	s.newFormResolver(rootPath)
	s.newBuiltinLoader()
	// P7-D：链接资料（没有 .project/links.jsonl 时静默跳过）。
	s.newLinkResolver()
}

// EnableLinks 显式重新加载链接资料（审计工具在改过配置后调用）。
func (s *Server) EnableLinks() {
	s.newLinkResolver()
}

// ConfigureLinks 覆盖链接资料配置（审计工具用），随后调用 EnableLinks 重载。
func (s *Server) ConfigureLinks(globalModules []string) error {
	raw, err := json.Marshal(map[string]any{"link": map[string]any{"globalModules": globalModules}})
	if err != nil {
		return err
	}
	return s.cfg.Apply(raw)
}

// LinkResolver 返回当前的链接解析器（nil 表示未启用/没有资料）。
func (s *Server) LinkResolver() *link.Resolver {
	return s.linkResolver()
}

// WaitLinkReady 等待链接索引预热完成（或超时）。
//
// 审计必须等：可见集没就绪时 function-not-found 一律不报，
// 不等就会得到"0 条"这种毫无意义的假象。
func (s *Server) WaitLinkReady(timeout time.Duration) bool {
	res := s.linkResolver()
	if res == nil {
		return false
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if res.Ready() {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return res.Ready()
}

// BuiltinSet 暴露当前生效的内置符号表，供 `cmd/builtin-audit` 打印统计。
//
// 返回 nil 表示内置表被关闭。
func (s *Server) BuiltinSet() *builtin.Set {
	return s.builtinSet()
}

// ConfigureBuiltins 覆盖内置符号表的覆盖层配置（审计工具用）。
func (s *Server) ConfigureBuiltins(root string, paths, disable []string) error {
	if s.builtins == nil {
		s.builtins = builtin.NewLoader(s.logger)
	}
	return s.builtins.Configure(root, paths, disable)
}

// AuditFile 对一份源码跑完整语义诊断流水线并返回结果（不含语法问题）。
//
// 内部按“打开 → 解析 → 建索引 → 各类检查”的顺序走，与 reparseFull 一致；
// 差别是不发布、不注册防抖定时器。
func (s *Server) AuditFile(ctx context.Context, uri string, lang parser.Lang, text string) []protocol.Diagnostic {
	doc := s.docs.Open(uri, lang, 1, text)
	defer s.docs.Close(uri)
	// 审计是"一个文件一个文件"跑的：每个文件审计完必须清掉它的
	// GLOBALS 解析结果，否则后一个文件会借用前一个文件的导入，
	// 统计出来的诊断分布是错的（这正是 P3a 收尾修掉的串味问题）。
	defer s.forgetGlobalsEntries(uri)

	tree, err := s.pool.ParseWith(ctx, lang, doc.Text(), nil, parser.ParseOptions{})
	if err != nil {
		return []protocol.Diagnostic{{Message: "解析失败: " + err.Error()}}
	}
	doc.SetTree(tree)

	fi, _ := s.indexFor(doc)
	var out []protocol.Diagnostic
	out = append(out, s.globalsChecks(ctx, doc, fi)...)
	if lang == parser.LangBDL {
		// 与 reparseFull 同序：先字段校验拿到引用的表单集合，
		// T5 再用这些表单的 item / SCREEN RECORD 名字兜底过滤。
		usedForms := s.formChecks(doc, &out)
		out = append(out, s.unresolvedSourceChecks(doc, fi, s.formSideNames(usedForms))...)
		s.recordMemberChecks(doc, &out)
		out = append(out, s.functionNotFoundChecks(doc, fi)...)
	} else {
		s.perChecks(doc, &out)
		out = append(out, s.unresolvedSourceChecks(doc, fi, nil)...)
	}
	return out
}

// AuditEnabled 报告审计是否会真的产生诊断（供 cmd 打印“已关闭”提示）。
//
// 全部诊断被关掉时跑全量语料毫无意义，工具应当直接告知。这里只按
// `bdl.diagnostics.*` 与注册表默认判断，不看旧专用配置键——它只是提示，
// 不做门禁。
func (s *Server) AuditEnabled() bool {
	settings := s.settings()
	for _, code := range diag.Registered() {
		if diag.Enabled(code, settings) {
			return true
		}
	}
	return false
}

// ApplyDiagnosticOverrides 覆盖诊断级别（审计工具用来临时开关某个码）。
//
// 走与客户端配置同一条 Apply 路径，保证覆盖语义与真实配置一致。
func (s *Server) ApplyDiagnosticOverrides(overrides map[string]config.DiagnosticLevel) error {
	if len(overrides) == 0 {
		return nil
	}
	raw, err := json.Marshal(map[string]any{"diagnostics": overrides})
	if err != nil {
		return err
	}
	return s.cfg.Apply(raw)
}
