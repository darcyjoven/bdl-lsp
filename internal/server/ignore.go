package server

import (
	"github.com/darcyjoven/bdl-lsp/internal/ignore"
)

// newIgnoreMatcher 按当前配置重新编译忽略规则（P8）。
//
// 每次配置变更都重编：忽略列表只影响诊断发布，重新编译是纯内存操作，
// 代价可以忽略。非法 glob 只记日志、不中断——否则一个笔误会让整份配置
// 看起来"没生效"。
func (s *Server) newIgnoreMatcher() {
	settings := s.settings()
	m, problems := ignore.New(s.rootPath, settings.Ignore.Files, settings.Ignore.Fields, settings.Ignore.Forms)
	for _, p := range problems {
		s.logger.Warn("忽略列表模式无效", "detail", p)
	}
	s.ignMu.Lock()
	s.ign = m
	s.ignMu.Unlock()
	if !m.Empty() {
		s.logger.Info("诊断忽略列表生效",
			"files", len(settings.Ignore.Files),
			"fields", len(settings.Ignore.Fields),
			"forms", len(settings.Ignore.Forms))
	}
}

// ignoreMatcher 返回当前忽略匹配器（可能为 nil）。
func (s *Server) ignoreMatcher() *ignore.Matcher {
	s.ignMu.RLock()
	defer s.ignMu.RUnlock()
	return s.ign
}

// ignoredFile 报告某个文档是否被 `bdl.ignore.files` 整体忽略。
func (s *Server) ignoredFile(uri string) bool {
	return s.ignoreMatcher().File(uri)
}
