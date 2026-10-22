package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"

	"github.com/darcyjoven/bdl-lsp/internal/link"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

// 链接资料 UI 的自定义请求（P7-E）。
//
// 为什么不用 `documentSymbol` 塞进去：`DocumentSymbol` 没有 URI 字段，
// range 只能落在本文档内，跨文件跳转做不到；而扁平的 `SymbolInformation`
// 又不能表达层级。所以层级用编辑器自己的视图（VSCode TreeView），
// 数据从这两个自定义请求取。
//
// 为什么由服务器组装：文件路径要按 Layout 的模组根拼、反查要用内存索引，
// 插件另建一份既重复又容易和诊断看到的不一致。
const (
	// MethodLinkTree 返回某个文件的链接视图（链内文件 + 全局模组 + 反查）。
	MethodLinkTree = "bdl/linkTree"
	// MethodLinkReferences 只做反查（打开子文件时的按钮用，省一次组装）。
	MethodLinkReferences = "bdl/linkReferences"
)

// linkTreeParams 是 bdl/linkTree 的参数。
type linkTreeParams struct {
	// TextDocument 用标准形状，客户端不用记自定义结构。
	TextDocument protocol.TextDocumentIdentifier `json:"textDocument"`
}

// handleLinkTree 返回链接视图。
//
// 没有资料时返回 `{"found":false}`（不是错误）：插件据此显示引导文案。
func (s *Server) handleLinkTree(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var p linkTreeParams
	if err := json.Unmarshal(req.Params(), &p); err != nil {
		return reply(ctx, nil, fmt.Errorf("解析 %s 参数: %w", MethodLinkTree, err))
	}
	res := s.linkTreeFor(string(p.TextDocument.URI))
	return reply(ctx, res, nil)
}

// handleLinkReferences 返回反查结果。
func (s *Server) handleLinkReferences(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var p linkTreeParams
	if err := json.Unmarshal(req.Params(), &p); err != nil {
		return reply(ctx, nil, fmt.Errorf("解析 %s 参数: %w", MethodLinkReferences, err))
	}
	tree := s.linkTreeFor(string(p.TextDocument.URI))
	return reply(ctx, map[string]any{
		"refs":     tree.Refs,
		"refTotal": tree.RefTotal,
	}, nil)
}

// linkTreeFor 是两条请求共用的实现。
func (s *Server) linkTreeFor(uriStr string) *link.LinkTree {
	s.links.mu.RLock()
	res := s.links.resolver
	s.links.mu.RUnlock()
	if res == nil {
		return &link.LinkTree{}
	}
	// untitled:/vscode-notebook: 等不是文件 URI，Filename() 会 panic。
	// 未保存的新文件也会发 bdl/linkTree，这里必须先按 scheme 拒绝。
	u, err := url.Parse(uriStr)
	if err != nil || u.Scheme != uri.FileScheme {
		return &link.LinkTree{}
	}
	path := uri.URI(uriStr).Filename()
	module, code, ok := res.Layout().FileID(path)
	if !ok {
		return &link.LinkTree{}
	}
	return res.Tree(module, code)
}
