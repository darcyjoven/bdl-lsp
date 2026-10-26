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

// MethodParseDone 是"强制解析完成"的服务端通知。
//
// W1（Windows 实机反馈）：强制解析改成后台执行后，请求会在解析开始前返回；
// 编辑器靠这个回执知道"解析完了、可以刷新大纲了"，并用它显示实测耗时。
const MethodParseDone = "bdl/parseDone"

// parseDoneParams 是 bdl/parseDone 的参数。
type parseDoneParams struct {
	URI     string `json:"uri"`
	MS      int64  `json:"ms"`
	Bytes   int    `json:"bytes"`
	Version int32  `json:"version"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
}

type forceParseParams struct {
	URI string `json:"uri"`
}

// handleForceParse 把该 URI 记入"强制解析"集合，并在**后台**重解析。
//
// W1（Windows 实机反馈）：以前是同步重解析，且仍受 `limits.parseTimeoutMs`
// 限制——大文件点了"强制解析"照样超时、拿不到语法树，表现为"大纲还是空的"。
// 现在：
//   - 登记后立刻回执（不占住 jsonrpc 请求线程）；
//   - 后台解析对该文件用 `Timeout: 0`（不限时，见 reparse/reparseFull）；
//   - 完成后发诊断 + `bdl/parseDone`（编辑器据此刷新大纲并提示耗时）。
//
// 锁纪律：调用点在 handle 内、已持 parseMu，所以真正的解析必须丢到
// requestForceReparse（等锁），否则会自己锁死自己（同 requestRediagnose）。
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
	s.logger.Info("强制解析当前文件（后台，不限时）", "uri", params.URI)
	s.requestForceReparse(params.URI)
	return reply(ctx, nil, nil)
}

// requestForceReparse 在后台对某个已打开文件做一次不限时重解析。
//
// 与 requestRediagnose 相同的理由：从不阻塞调用方，等 parseMu 后再动文档树。
// 区别：只重算这一个文件（不 rediagnoseAll），并在退出/关闭后放弃。
func (s *Server) requestForceReparse(uri string) {
	s.mu.Lock()
	shutdown := s.shutdown
	s.mu.Unlock()
	if shutdown {
		return
	}
	go func() {
		select {
		case <-s.exited:
			return
		default:
		}
		s.parseMu.Lock()
		defer s.parseMu.Unlock()
		select {
		case <-s.exited:
			return
		default:
		}
		doc := s.docs.Get(uri)
		if doc == nil {
			return // 等待期间文件被关闭
		}
		s.reparseFull(context.Background(), doc)
	}()
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
