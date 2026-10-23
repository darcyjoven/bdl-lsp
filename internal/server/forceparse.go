package server

// "强制解析当前文件"：让服务端对某个**已打开**的文件忽略大小/超时上限解析。
//
// 为什么需要：文件超过 `limits.maxFileBytes` / `maxFileLines` 时服务端会跳过
// 解析（只发一条 file-too-large / parse-timeout 提示），表现为"这个文件没有
// 大纲、没有语法诊断"。用户看到提示后应当能一键让它照常解析，
// 而不是去改全局上限（改大又会影响所有文件）。
//
// 有效期（用户 2026-10-23 确认）：**文件不关闭就一直生效**；didClose 时移除。

import (
	"context"
	"encoding/json"
	"fmt"

	"go.lsp.dev/jsonrpc2"
)

// MethodForceParse 是编辑器侧发来的自定义请求（params: {"uri": "..."}）。
const MethodForceParse = "bdl/forceParse"

type forceParseParams struct {
	URI string `json:"uri"`
}

// handleForceParse 把该 URI 记入"强制解析"集合并立即重解析一次。
func (s *Server) handleForceParse(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params forceParseParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, fmt.Errorf("解析 %s 参数: %w", MethodForceParse, err))
	}
	if params.URI == "" {
		return reply(ctx, nil, fmt.Errorf("%s 缺少 uri", MethodForceParse))
	}
	doc := s.docs.Get(params.URI)
	if doc == nil {
		return reply(ctx, nil, fmt.Errorf("%s: 文件未打开：%s", MethodForceParse, params.URI))
	}
	s.setForceParse(params.URI, true)
	s.logger.Info("强制解析当前文件", "uri", params.URI)
	// 调用点在 handle 内，parseMu 已持有；重解析后该文件不再受上限约束。
	s.reparseFull(ctx, doc)
	return reply(ctx, nil, nil)
}

// forceParseURI 报告该文件是否被用户要求"强制解析"。
func (s *Server) forceParseURI(uri string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.forceParse[uri]
}

// setForceParse 开关某个文件的强制解析标记。
func (s *Server) setForceParse(uri string, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.forceParse == nil {
		s.forceParse = map[string]bool{}
	}
	if on {
		s.forceParse[uri] = true
		return
	}
	delete(s.forceParse, uri)
}
