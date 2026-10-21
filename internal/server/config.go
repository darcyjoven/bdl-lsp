package server

import (
	"context"
	"encoding/json"

	"github.com/darcyjoven/bdl-lsp/internal/diag"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// handleDidChangeConfiguration 处理客户端下发的新配置。
//
// 配置变化会影响诊断级别与大文件阈值，因此需要重新解析所有已打开文档，
// 让用户立即看到效果（而不是等下次编辑）。
func (s *Server) handleDidChangeConfiguration(ctx context.Context, req jsonrpc2.Request) error {
	var params protocol.DidChangeConfigurationParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		s.logger.Warn("didChangeConfiguration 参数解析失败", "error", err)
		return nil
	}

	// 客户端可能把设置包在 "bdl" 键下，也可能直接是设置对象。
	raw, err := extractSettings(params.Settings)
	if err != nil {
		s.logger.Warn("配置内容解析失败", "error", err)
		return nil
	}

	if s.cfg == nil {
		return nil
	}
	if err := s.cfg.Apply(raw); err != nil {
		s.logger.Warn("应用新配置失败", "error", err)
		return nil
	}

	settings := s.cfg.Get()
	for _, problem := range settings.Validate() {
		s.logger.Warn("配置问题", "detail", problem)
	}
	// 拼错的诊断码是最常见的配置错误（多一个字母就永久不生效），
	// 必须显式告警而不是静默忽略。
	for _, code := range diag.ValidateSettings(settings) {
		s.logger.Warn("未知诊断码（配置不生效）", "code", code,
			"hint", "已注册的码见 bdl-lsp diagnostics 或文档")
	}
	s.logger.Info("配置已更新", "generoVersion", settings.GeneroVersion)

	// P3a：searchPaths 变化会影响 GLOBALS 解析结果，重建解析器
	// （缓存随重建丢弃；已打开文档走内存版不受影响）。
	if !searchPathsEqual(s.lastSearchPaths, settings.Globals.SearchPaths) {
		s.logger.Info("globals searchPaths 变化，重建解析器",
			"old", s.lastSearchPaths, "new", settings.Globals.SearchPaths)
		s.lastSearchPaths = append([]string(nil), settings.Globals.SearchPaths...)
		if s.rootPath != "" {
			s.newGlobalsResolver(s.rootPath)
		}
	}

	// P3b：perRoots 变化同理重建表单解析器。
	if !searchPathsEqual(s.lastPerRoots, settings.PerRoots) {
		s.logger.Info("perRoots 变化，重建表单解析器",
			"old", s.lastPerRoots, "new", settings.PerRoots)
		s.lastPerRoots = append([]string(nil), settings.PerRoots...)
		if s.rootPath != "" {
			s.newFormResolver(s.rootPath)
		}
	}

	// P7-C：内置符号表的 enable / paths / disable 变化都要重载。
	// 无法便宜地比较"路径集合有没有变"，但重载只读几个 JSON（几十 µs），
	// 每次配置变更都重载即可。
	s.newBuiltinLoader()

	// 重新诊断所有打开的文档
	for _, doc := range s.docs.All() {
		s.reparse(ctx, doc, nil)
	}
	return nil
}

// searchPathsEqual 比较两个 searchPaths（nil 与空切片视为相等）。
func searchPathsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// extractSettings 从客户端下发的内容里取出实际设置对象。
//
// 不同客户端的包装方式不一致：
//   - VSCode：{ "bdl": { ...设置... } }
//   - 其他：直接是设置对象
func extractSettings(v any) (json.RawMessage, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}

	var wrapper map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wrapper); err != nil {
		return raw, nil // 不是对象，按原样交给上层
	}
	if inner, ok := wrapper["bdl"]; ok {
		return inner, nil
	}
	return raw, nil
}
