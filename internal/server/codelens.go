package server

// 链接资料 Code Lens（PLAN Z-P3-3）。
//
// 为什么这么设计：Zed **没有侧边栏树视图 API**，链接资料的"就地可看/可点"版本
// 就是把链内子文件各做一条 Code Lens 挂在 **MAIN 程序块**头上（用户 2026-10-23 决定）。
// 数据来自 `link.Resolver.Tree()`（纯内存查询，不碰语法树、不发诊断）——与
// VSCode 侧 TreeView 用的是同一份数据，所以两端行为一致，不会各说各话。
//
// 上限：`link.lensMax`（默认 10）。超出时只显示前 N 条 + 一条**纯文本**汇总
// （不挂 command：点了不会有反应，但能告诉用户"还有多少"）。
//
// 点击行为**待实机确认**（PLAN Z-P2-1）：Zed 是否把 lens 的 command 转成
// `workspace/executeCommand`、以及是否支持服务端反向的 `window/showDocument`。
// 所以 `bdl.openLink` 的实现是"请客户端打开该文件"，用户也可以从
// `lsp command selector` 手动触发（兜底），不会因为点击不生效就完全没法用。

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	"github.com/darcyjoven/bdl-lsp/link"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
	"go.lsp.dev/uri"
)

const (
	// CommandOpenLink 是 Code Lens 点击时执行的命令（arguments: [目标文件 URI]）。
	CommandOpenLink = "bdl.openLink"
	// MethodShowDocument 是服务端请求客户端打开文件（LSP 3.16 `window/showDocument`）。
	MethodShowDocument = protocol.MethodShowDocument
)

// handleCodeLens 处理 `textDocument/codeLens`。
//
// 只在"有 MAIN 且链接资料命中"时返回 lens；其余情况返回空数组（不是错误）：
// 文本编辑器请求 lens 很频繁，用错误会让客户端把它记成服务端故障。
func (s *Server) handleCodeLens(
	ctx context.Context,
	reply jsonrpc2.Replier,
	req jsonrpc2.Request,
) error {
	var params protocol.CodeLensParams
	if raw := req.Params(); len(raw) > 0 {
		if err := json.Unmarshal(raw, &params); err != nil {
			return reply(ctx, nil, fmt.Errorf("解析 %s 参数: %w", protocol.MethodTextDocumentCodeLens, err))
		}
	}
	uriStr := string(params.TextDocument.URI)

	settings := s.settings()
	if !settings.LinkEnabled() {
		return reply(ctx, []protocol.CodeLens{}, nil)
	}
	doc := s.docs.Get(uriStr)
	if doc == nil {
		return reply(ctx, []protocol.CodeLens{}, nil)
	}
	fi, idx := s.indexFor(doc)
	if fi == nil || idx == nil {
		return reply(ctx, []protocol.CodeLens{}, nil)
	}
	mainRange, ok := mainRangeOf(fi, idx)
	if !ok {
		return reply(ctx, []protocol.CodeLens{}, nil)
	}

	tree := s.linkTreeFor(uriStr)
	return reply(ctx, buildLinkLenses(mainRange, tree, settings.LinkLensMax(), uriStr), nil)
}

// mainRangeOf 找出 MAIN 程序块在文档里的行区间（只取声明那一行）。
func mainRangeOf(fi *index.FileIndex, idx *textpos.Index) (protocol.Range, bool) {
	for _, sym := range fi.TopLevel {
		if sym == nil || sym.Kind != index.KindMain {
			continue
		}
		start, end := clampRange(sym.DeclStart, sym.DeclEnd, idx.Size())
		rng := byteRange(idx, start, end)
		if rng.Start.Line != rng.End.Line {
			// LSP 建议 lens 的 range 落在单行内；跨行时收成起始行那一行。
			rng.End = protocol.Position{Line: rng.Start.Line, Character: rng.Start.Character + 1}
		} else if rng.End.Character == rng.Start.Character {
			rng.End.Character++
		}
		return rng, true
	}
	return protocol.Range{}, false
}

// buildLinkLenses 生成挂在 MAIN 头上的 lens（纯函数，便于单测）。
//
//   - 链内每个**子文件**一条，标题 `模组/代码`；文件不在磁盘上时给纯文本（无 command）；
//   - 不给当前文件自己挂（MAIN 头上列自己没意义）；
//   - 超过 max 条时只显示前 max 条，再补一条纯文本汇总。
func buildLinkLenses(
	mainRange protocol.Range,
	tree *link.LinkTree,
	max int,
	currentURI string,
) []protocol.CodeLens {
	if tree == nil || !tree.Found || len(tree.Files) == 0 {
		return []protocol.CodeLens{}
	}
	if max <= 0 {
		max = 10
	}

	type target struct {
		title string
		uri   string // 空 = 文件不在磁盘上（纯文本 lens）
	}
	targets := make([]target, 0, len(tree.Files))
	for _, f := range tree.Files {
		fileURI := ""
		if f.Path != "" {
			fileURI = fileuri.FromPath(f.Path)
		}
		if fileURI != "" && sameURI(fileURI, currentURI) {
			continue
		}
		title := f.Module + "/" + f.Code
		if f.Path == "" {
			title += "（文件不在磁盘）"
		}
		targets = append(targets, target{title: title, uri: fileURI})
	}
	if len(targets) == 0 {
		return []protocol.CodeLens{}
	}

	lenses := make([]protocol.CodeLens, 0, minInt(len(targets), max)+1)
	for i, tgt := range targets {
		if i >= max {
			break
		}
		lens := protocol.CodeLens{Range: mainRange}
		if tgt.uri != "" {
			lens.Command = &protocol.Command{
				Title:     tgt.title,
				Command:   CommandOpenLink,
				Arguments: []interface{}{tgt.uri},
			}
		}
		lenses = append(lenses, lens)
	}
	if len(targets) > max {
		lenses = append(lenses, protocol.CodeLens{Range: mainRange})
	}
	return lenses
}

// openLink 处理 `bdl.openLink`：请客户端打开目标文件（LSP 3.16 `window/showDocument`）。
//
// ★ 为什么必须异步：jsonrpc2 v0.10 的读循环是**同步**调用 handler 的
// （`conn.run` 里直接 `handler(...)`），所以在 handler 内调用 `conn.Call`
// 会等一个永远到不了的响应——响应本身也要由同一个读循环处理（实测：3 秒超时）。
// 因此这里另起 goroutine 发反向请求，再在 goroutine 里回执；handler 立刻返回，
// 读循环继续跑。**新增任何"服务端主动请求客户端"的能力时都要遵守这条。**
//
// 客户端是否实现/响应 `window/showDocument` 由实机确认（PLAN Z-P2-1）；
// 不支持时用户会拿到一条明确错误，仍可从 `lsp command selector` 手动触发。
func (s *Server) openLink(ctx context.Context, reply jsonrpc2.Replier, uriStr string) error {
	s.mu.Lock()
	conn := s.conn
	s.mu.Unlock()
	if conn == nil {
		return reply(ctx, nil, fmt.Errorf("%s: 当前连接不支持反向请求", CommandOpenLink))
	}
	go func() {
		callCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		var result protocol.ShowDocumentResult
		if _, err := conn.Call(callCtx, MethodShowDocument, protocol.ShowDocumentParams{
			URI:       uri.URI(uriStr),
			TakeFocus: true,
		}, &result); err != nil {
			_ = reply(ctx, nil, fmt.Errorf("%s: 打开 %s 失败：%w", CommandOpenLink, uriStr, err))
			return
		}
		if !result.Success {
			_ = reply(ctx, nil, fmt.Errorf("%s: 客户端拒绝打开 %s", CommandOpenLink, uriStr))
			return
		}
		_ = reply(ctx, map[string]any{"opened": uriStr}, nil)
	}()
	return nil
}

// sameURI 比较两个 URI 是否指向同一文件：先按 file:// 解成路径，再用现成的
// sameFile（大小写不敏感、filepath.Clean）；解不出来就退回字符串比较。
// minInt 取较小值（不覆盖 Go 1.21+ 的内建 min，读代码时更明确）。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func sameURI(a, b string) bool {
	if a == b {
		return true
	}
	pathA, errA := fileuri.ToPath(a)
	pathB, errB := fileuri.ToPath(b)
	if errA == nil && errB == nil {
		return sameFile(pathA, pathB)
	}
	return strings.EqualFold(a, b)
}
