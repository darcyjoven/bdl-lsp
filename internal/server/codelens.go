package server

// 链接资料 Code Lens（PLAN Z-P3-3）。
//
// 为什么这么设计：Zed **没有侧边栏树视图 API**，链接资料的"就地可看/可点"版本
// 就是把链内子文件各做一条 Code Lens 挂在 **MAIN 程序块**头上（用户 2026-10-23 决定）。
// 数据来自 `link.Resolver.Tree()`（纯内存查询，不碰语法树、不发诊断）——与
// VSCode 侧 TreeView 用的是同一份数据，所以两端行为一致，不会各说各话。
//
// 上限：`link.lensMax`（默认 10）。超出时只显示前 N 条 + 一条**汇总**（带 command，
// 点了会弹提示说明"还有多少"，见 `CommandShowInfo`）。
//
// ★ Zed 的两条硬事实（查 zed-industries/zed main 分支源码证实，2026-10-29）：
//
//  1. **没有 command 的 lens 在 Zed 里不会显示自己的标题**。Zed 只从
//     `lens.command.title` 取文本；没有 command 时它先渲染占位，等
//     `codeLens/resolve` 回来补标题；服务端不支持 resolve（`resolveProvider`
//     为 false/absent）时就永远占位，最后回落到一个**假的**「0 references」
//     文本（crates/editor/src/code_lens.rs 的 `EMPTY_LENS_FALLBACK_TITLE`）。
//     → 所以"纯文本 lens"在 Zed 里是个陷阱：要么显示成假的「0 references」，
//     要么什么都不显示。**汇总信息必须带 command**。
//  2. **点击走 `workspace/executeCommand`**，且命令必须在
//     `executeCommandProvider.commands` 里声明过，否则被静默跳过
//     （crates/project/src/lsp_store.rs 的 `apply_code_action`）。
//     → 汇总用的命令也要加进 `executeCommandIDs()`。
//
// 反向请求 `window/showDocument`：Zed **支持**
// （crates/project/src/lsp_store.rs 注册了 `on_request::<lsp::request::ShowDocument>`），
// 所以 `bdl.openLink` 的"请客户端打开该文件"是有出路的。

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
	"github.com/darcyjoven/bdl-lsp/internal/globals"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	"github.com/darcyjoven/bdl-lsp/internal/wsindex"
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

	doc := s.docs.Get(uriStr)
	if doc == nil {
		return reply(ctx, []protocol.CodeLens{}, nil)
	}
	fi, idx := s.indexFor(doc)
	if fi == nil || idx == nil {
		return reply(ctx, []protocol.CodeLens{}, nil)
	}

	settings := s.settings()
	lenses := make([]protocol.CodeLens, 0, 8)

	// ① 链接资料：MAIN 头上每个链内子文件一条（需要链接功能开启 + 资料命中）。
	if settings.LinkEnabled() {
		if mainRange, ok := mainRangeOf(fi, idx); ok {
			tree := s.linkTreeFor(uriStr)
			lenses = append(lenses, buildLinkLenses(mainRange, tree, settings.LinkLensMax(), uriStr)...)
		}
	}

	// ② 函数级反查：函数定义那行标「被 N 处调用」（依赖工作区索引，与链接资料无关）。
	//    口径与「查找引用」一致：只算可达模组（本模组 + 全局模组）。
	curPath := globals.PathOfURI(uriStr)
	lenses = append(lenses, buildCallerLenses(
		fi, idx, s.workspaceIndex(), s.reachableModules(moduleOfPath(curPath)),
	)...)

	return reply(ctx, lenses, nil)
}

// mainRangeOf 找出 MAIN 程序块在文档里的行区间（只取声明那一行）。
func mainRangeOf(fi *index.FileIndex, idx *textpos.Index) (protocol.Range, bool) {
	for _, sym := range fi.TopLevel {
		if sym == nil || sym.Kind != index.KindMain {
			continue
		}
		return declLineRange(sym, idx), true
	}
	return protocol.Range{}, false
}

// declLineRange 把一个符号的声明区间收成**单行**范围（LSP 建议 lens 只跨一行）。
func declLineRange(sym *index.Symbol, idx *textpos.Index) protocol.Range {
	start, end := clampRange(sym.DeclStart, sym.DeclEnd, idx.Size())
	rng := byteRange(idx, start, end)
	if rng.Start.Line != rng.End.Line {
		rng.End = protocol.Position{Line: rng.Start.Line, Character: rng.Start.Character + 1}
	} else if rng.End.Character == rng.Start.Character {
		rng.End.Character++
	}
	return rng
}

// callerLensLimit 是一个文件里最多显示多少条「被 N 处调用」lens（超出给一条汇总）。
//
// 4GL 一个文件里被调用的函数可能有几十个，全挂上会把编辑器刷满；
// 汇总结尾会提示用「查找引用」看全部。
const callerLensLimit = 10

// buildCallerLenses 给"确实被调用过"的顶层函数各挂一条计数 lens。
//
// 这是 VSCode 侧 TreeView「反查引用程序」在 Zed 上的**函数级降级形态**：
// Zed 没有侧边栏树、也没有 QuickPick 列表，所以只把**调用处数量**标在函数定义那行，
// 点它打开第一处调用；要看全部调用点用 Zed 原生的「查找引用」
// （`editor: find all references`，走我们的 `referencesProvider`）。
//
// 数据来自工作区索引（`wsindex.Index.CallsOfMany`）；索引还没建好就返回空（宁可不显示）。
//
// 口径与「查找引用」一致（`wsReferences`）：只算**能跨文件看到**的调用点
// —— 本模组 + 全局模组（sub/lib/qry）。否则别的模组里的同名调用也会被算进来，
// 数字虚高（同一个函数名在不同模组重复很常见）。reachable 由调用方算出。
func buildCallerLenses(
	fi *index.FileIndex,
	idx *textpos.Index,
	ix *wsindex.Index,
	reachable map[string]bool,
) []protocol.CodeLens {
	if fi == nil || idx == nil || ix == nil {
		return []protocol.CodeLens{}
	}

	// 先收集本文件里"是函数"的顶层符号（只有函数能被跨文件调用）。
	names := make([]string, 0, len(fi.TopLevel))
	syms := make(map[string]*index.Symbol, len(fi.TopLevel))
	for _, sym := range fi.TopLevel {
		if sym == nil || sym.Kind != index.KindFunction || sym.Name == "" {
			continue
		}
		names = append(names, sym.Name)
		syms[strings.ToLower(sym.Name)] = sym
	}
	if len(names) == 0 {
		return []protocol.CodeLens{}
	}

	// 一次遍历索引拿到全部调用点（避免逐个函数全表扫描：7000+ 文件时
	// 逐个 CallsOf 会让单次 codeLens 请求变成秒级）。
	all := ix.CallsOfMany(names)

	lenses := make([]protocol.CodeLens, 0, 4)
	shown := 0
	skipped := 0
	var skippedRange protocol.Range
	for _, name := range names {
		sym := syms[strings.ToLower(name)]
		calls := all[name]
		if len(calls) == 0 {
			continue
		}
		// 过滤不可达模组（与 wsReferences 同一把尺子）。reachable 为 nil = 不过滤。
		visible := make([]wsindex.Call, 0, len(calls))
		for _, c := range calls {
			if reachable == nil || reachable[moduleOfPath(c.File)] {
				visible = append(visible, c)
			}
		}
		if len(visible) == 0 {
			continue
		}
		// 稳定排序：先文件后行，避免"点开首处"每次都不一样。
		sort.Slice(visible, func(i, j int) bool {
			if visible[i].File != visible[j].File {
				return visible[i].File < visible[j].File
			}
			return visible[i].Line < visible[j].Line
		})
		rng := declLineRange(sym, idx)
		if shown >= callerLensLimit {
			skipped++
			skippedRange = rng
			continue
		}
		first := visible[0]
		lenses = append(lenses, protocol.CodeLens{
			Range: rng,
			Command: &protocol.Command{
				Title: fmt.Sprintf("🔗 %s 被 %d 处调用（点开首处 %s:%d）",
					sym.Name, len(visible), shortPath(first.File), first.Line+1),
				Command:   CommandOpenLink,
				Arguments: []interface{}{fileuri.FromPath(first.File)},
			},
		})
		shown++
	}
	if skipped > 0 {
		// 汇总行**必须带 command**（Zed 只显示 command.title；没有 command 的
		// lens 会变成假的「0 references」）。点了弹提示说明还剩多少。
		lenses = append(lenses, protocol.CodeLens{
			Range: skippedRange,
			Command: &protocol.Command{
				Title:     fmt.Sprintf("…另有 %d 个被调用的函数（点此查看提示）", skipped),
				Command:   CommandShowInfo,
				Arguments: []interface{}{fmt.Sprintf("本文件还有 %d 个被调用的函数没显示（上限 %d）。要看全部，请用编辑器的「查找引用」。", skipped, callerLensLimit)},
			},
		})
	}
	return lenses
}

// shortPath 把绝对路径截成"末两段"，让 lens 标题保持短（Windows 反斜杠也处理）。
func shortPath(p string) string {
	normalized := strings.ReplaceAll(p, "\\", "/")
	parts := strings.Split(normalized, "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return strings.Join(parts, "/")
}

// buildLinkLenses 生成挂在 MAIN 头上的 lens（纯函数，便于单测）。
//
//   - 链内每个**子文件**一条，标题 `模组/代码`；文件不在磁盘上时给 `bdl.showInfo`（只提示，不跳转）；
//   - 不给当前文件自己挂（MAIN 头上列自己没意义）；
//   - 超过 max 条时只显示前 max 条，再补一条汇总（带 `bdl.showInfo`）。
//
// ★ 每条 lens 都**必须带 command**：Zed 只显示 `command.title`，没有 command
// 的 lens 会被渲染成假的「0 references」（详见文件头注释）。
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
		uri   string // 空 = 文件不在磁盘上（只提示，不跳转）
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
		} else {
			// 文件不在磁盘上：不能跳转，但仍要显示自己的标题（Zed 只显示
			// command.title，没有 command 的 lens 会变成假的「0 references」）。
			lens.Command = &protocol.Command{
				Title:     tgt.title,
				Command:   CommandShowInfo,
				Arguments: []interface{}{fmt.Sprintf("%s：链接资料里有这个文件，但磁盘上找不到（通常是语料快照缺的模组）。", tgt.title)},
			}
		}
		lenses = append(lenses, lens)
	}
	if len(targets) > max {
		// 汇总行同样必须带 command（否则 Zed 显示成假的「0 references」）。
		lenses = append(lenses, protocol.CodeLens{
			Range: mainRange,
			Command: &protocol.Command{
				Title:     fmt.Sprintf("…另有 %d 个链内文件（上限 %d）", len(targets)-max, max),
				Command:   CommandShowInfo,
				Arguments: []interface{}{fmt.Sprintf("这条链接链还有 %d 个文件没显示（上限 %d，可在 link.lensMax 调整）。", len(targets)-max, max)},
			},
		})
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
