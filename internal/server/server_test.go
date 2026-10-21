package server

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/darcyjoven/bdl-lsp/internal/config"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"go.lsp.dev/protocol"
)

// pipeStream 在测试里充当 LSP 的双向流。
type pipeStream struct {
	io.Reader
	io.Writer
	closeOnce sync.Once
	closed    chan struct{}
}

func (p *pipeStream) Close() error {
	p.closeOnce.Do(func() { close(p.closed) })
	return nil
}

// harness 是一个最小 LSP 客户端，用于协议级测试。
type harness struct {
	t      *testing.T
	srv    *Server
	stream *pipeStream
	client *io.PipeWriter
	server *io.PipeReader
	// br 是**唯一**的读取缓冲。必须跨调用复用：每次 new 一个 bufio.Reader
	// 都会预读 4KB，把后面的帧吞进自己的缓冲里丢掉。
	br *bufio.Reader
	// frames 由**一个**常驻 goroutine 灌入。
	//
	// 早期实现是"每次读取起一个 goroutine"，超时后那个 goroutine 还挂在
	// Read 上，下一帧就会被它偷走再也没人收——测试里表现为莫名其妙的
	// "等待响应超时"。改成单一泵 + 通道后，超时只是没人取，帧还在队列里。
	frames chan map[string]any
}

func newHarness(t *testing.T) *harness {
	t.Helper()

	// client → server
	c2sR, c2sW := io.Pipe()
	// server → client
	s2cR, s2cW := io.Pipe()

	h := &harness{
		t:      t,
		stream: &pipeStream{Reader: c2sR, Writer: s2cW, closed: make(chan struct{})},
		client: c2sW,
		server: s2cR,
		br:     bufio.NewReader(s2cR),
		frames: make(chan map[string]any, 256),
	}
	go h.pump()
	return h
}

func (h *harness) send(payload string) {
	h.t.Helper()
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
	if _, err := io.WriteString(h.client, frame); err != nil {
		h.t.Fatalf("写入请求失败: %v", err)
	}
}

// pump 是唯一读取 server→client 流的 goroutine。
func (h *harness) pump() {
	defer close(h.frames)
	for {
		msg, err := readFrame(h.br)
		if err != nil {
			return
		}
		h.frames <- msg
	}
}

// readFrame 从缓冲里读出一帧 JSON-RPC 消息。
func readFrame(br *bufio.Reader) (map[string]any, error) {
	var length int
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return nil, err
		}
		line = strings.TrimSpace(line)
		if line == "" {
			break
		}
		if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
			n, convErr := strconv.Atoi(strings.TrimSpace(value))
			if convErr != nil {
				return nil, convErr
			}
			length = n
		}
	}
	if length == 0 {
		return nil, fmt.Errorf("缺少 Content-Length")
	}
	buf := make([]byte, length)
	if _, err := io.ReadFull(br, buf); err != nil {
		return nil, err
	}
	var msg map[string]any
	if err := json.Unmarshal(buf, &msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// readMessage 读取一帧并解析为通用 map（超时直接 Fatal）。
func (h *harness) readMessage(timeout time.Duration) map[string]any {
	h.t.Helper()
	msg, ok := h.nextFrame(timeout)
	if !ok {
		h.t.Fatal("等待响应超时")
	}
	return msg
}

// nextFrame 从通道取一帧；关闭或超时返回 ok=false。
func (h *harness) nextFrame(timeout time.Duration) (map[string]any, bool) {
	select {
	case msg, ok := <-h.frames:
		return msg, ok
	case <-time.After(timeout):
		return nil, false
	}
}

// readMessageSoft 与 readMessage 相同，但超时返回 nil 而不 Fatal。
//
// 用于"断言某个诊断**不该**出现"的用例：那里必须能等满一段时间再
// 平静地得出结论，而不是把超时当成测试失败。
func (h *harness) readMessageSoft(timeout time.Duration) map[string]any {
	if timeout <= 0 {
		return nil
	}
	msg, _ := h.nextFrame(timeout)
	return msg
}

// collectDiagnostics 在给定时间窗内收集某 URI 的全部诊断（超时不算失败）。
//
// 只用于"断言不该出现"的场景；需要"断言必须出现"时用 drainUntilCode。
func (h *harness) collectDiagnostics(uri string, window time.Duration) []any {
	h.t.Helper()
	deadline := time.Now().Add(window)
	var all []any
	for time.Now().Before(deadline) {
		msg := h.readMessageSoft(time.Until(deadline))
		if msg == nil {
			return all
		}
		if msg["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		p, _ := msg["params"].(map[string]any)
		if p["uri"] != uri {
			continue
		}
		diags, _ := p["diagnostics"].([]any)
		all = append(all, diags...)
	}
	return all
}

func startServer(t *testing.T) (*harness, func()) {
	t.Helper()

	pool, err := parser.New()
	if err != nil {
		t.Fatalf("parser.New(): %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(pool, logger, "test")

	h := newHarness(t)
	h.srv = srv
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := srv.ServeStream(ctx, h.stream); err != nil && ctx.Err() == nil {
			t.Logf("ServeStream 返回: %v", err)
		}
	}()

	cleanup := func() {
		cancel()
		_ = h.client.Close()
		_ = h.server.Close()
		_ = h.stream.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Log("ServeStream 未在超时内退出")
		}
		pool.Close()
	}
	return h, cleanup
}

func TestInitializeHandshake(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"clientInfo":{"name":"unit"},"rootUri":null,"capabilities":{}}}`)
	msg := h.readMessage(3 * time.Second)

	if msg["id"] != float64(1) {
		t.Fatalf("id = %v，期望 1", msg["id"])
	}
	result, ok := msg["result"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 result: %v", msg)
	}
	caps, ok := result["capabilities"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 capabilities: %v", result)
	}
	if got := caps["textDocumentSync"]; got != float64(2) {
		t.Errorf("textDocumentSync = %v，期望 2（增量）", got)
	}
	if caps["definitionProvider"] != true {
		t.Error("definitionProvider 应为 true")
	}
	info, _ := result["serverInfo"].(map[string]any)
	if info["name"] != "bdl-lsp" {
		t.Errorf("serverInfo.name = %v", info["name"])
	}
}

func TestUnknownMethodReturnsError(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":7,"method":"textDocument/typeDefinition","params":{}}`)
	msg := h.readMessage(3 * time.Second)

	errObj, ok := msg["error"].(map[string]any)
	if !ok {
		t.Fatalf("期望 error，实际: %v", msg)
	}
	if code, _ := errObj["code"].(float64); code != float64(-32601) {
		t.Errorf("错误码 = %v，期望 -32601（MethodNotFound）", errObj["code"])
	}
}

func TestShutdownThenExit(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	h.send(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
	msg := h.readMessage(3 * time.Second)
	if msg["id"] != float64(2) {
		t.Fatalf("shutdown id = %v", msg["id"])
	}

	h.send(`{"jsonrpc":"2.0","method":"exit","params":{}}`)
}

func TestDidOpenPublishesDiagnostics(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/broken.4gl"
	params := map[string]any{
		"textDocument": map[string]any{
			"uri":        uri,
			"languageId": "bdl",
			"version":    1,
			"text":       "MAIN\n    DISPLAY \"unterminated\nEND MAIN\n",
		},
	}
	raw, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": params})
	if err != nil {
		t.Fatal(err)
	}
	h.send(string(raw))

	msg := h.readMessage(5 * time.Second)
	if msg["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("期望 publishDiagnostics，实际: %v", msg["method"])
	}
	p, _ := msg["params"].(map[string]any)
	if p["uri"] != uri {
		t.Errorf("诊断 uri = %v", p["uri"])
	}
	diags, _ := p["diagnostics"].([]any)
	if len(diags) == 0 {
		t.Error("损坏文件应产生至少一条诊断")
	}
}

func TestDidCloseClearsDiagnostics(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/ok.4gl"
	openPayload := fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":"bdl","version":1,"text":"MAIN\nEND MAIN\n"}}}`, uri)
	h.send(openPayload)
	h.readMessage(5 * time.Second)

	closePayload := fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didClose","params":{"textDocument":{"uri":%q}}}`, uri)
	h.send(closePayload)

	msg := h.readMessage(5 * time.Second)
	if msg["method"] != "textDocument/publishDiagnostics" {
		t.Fatalf("期望 publishDiagnostics，实际: %v", msg["method"])
	}
	p, _ := msg["params"].(map[string]any)
	diags, _ := p["diagnostics"].([]any)
	if len(diags) != 0 {
		t.Errorf("关闭后应清空诊断，实际 %d 条", len(diags))
	}
}

func TestIgnoredNotificationDoesNotCrash(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","method":"$/setTrace","params":{"value":"verbose"}}`)

	// 服务端应继续工作：随后 initialize 仍能正常响应。
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	msg := h.readMessage(3 * time.Second)
	if msg["id"] != float64(1) {
		t.Fatalf("忽略未知通知后服务异常: %v", msg)
	}
}

// sendDidChange 发送一次增量编辑。
func (h *harness) sendDidChange(uri string, version int, changesJSON string) {
	h.t.Helper()
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didChange","params":{"textDocument":{"uri":%q,"version":%d},"contentChanges":%s}}`, uri, version, changesJSON)
	h.send(payload)
}

func (h *harness) sendDidOpen(uri, lang, text string, version int) {
	h.t.Helper()
	payload := fmt.Sprintf(`{"jsonrpc":"2.0","method":"textDocument/didOpen","params":{"textDocument":{"uri":%q,"languageId":%q,"version":%d,"text":%q}}}`, uri, lang, version, text)
	h.send(payload)
}

// drainDiagnostics 读取直到收到目标 uri 的 publishDiagnostics。
func (h *harness) drainDiagnostics(uri string, timeout time.Duration) []any {
	h.t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		msg := h.readMessage(time.Until(deadline))
		if msg["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		p, _ := msg["params"].(map[string]any)
		if p["uri"] != uri {
			continue
		}
		diags, _ := p["diagnostics"].([]any)
		return diags
	}
	h.t.Fatalf("未在 %v 内收到 %s 的诊断", timeout, uri)
	return nil
}

func TestDidChangeIncrementalInsertFixesError(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/inc.4gl"
	// 故意缺少 END MAIN
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"hello\"\n", 1)
	if diags := h.drainDiagnostics(uri, 5*time.Second); len(diags) == 0 {
		t.Fatal("缺少 END MAIN 应产生诊断")
	}

	// 在第 2 行（0 基，即文件末尾空行）插入 END MAIN
	h.sendDidChange(uri, 2, `[{"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":0}},"text":"END MAIN\n"}]`)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) != 0 {
		t.Errorf("补全 END MAIN 后应无诊断，实际 %d 条：%v", len(diags), diags)
	}
}

func TestDidChangeIncrementalDeleteKeepsDiagnostics(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/del.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"hi\"\nEND MAIN\n", 1)
	if diags := h.drainDiagnostics(uri, 5*time.Second); len(diags) != 0 {
		t.Fatalf("干净文件不应有诊断：%v", diags)
	}

	// 删除 END MAIN 的 "END "（第 2 行 0..4），只剩 MAIN，应产生错误
	h.sendDidChange(uri, 2, `[{"range":{"start":{"line":2,"character":0},"end":{"line":2,"character":4}},"text":""}]`)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) == 0 {
		t.Error("删除 END 后应产生语法错误")
	}
}

func TestDidChangeMultipleEdits(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/multi.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"a\"\n    DISPLAY \"b\"\nEND MAIN\n", 1)
	h.drainDiagnostics(uri, 5*time.Second)

	// 一次通知里两处替换：把两个字符串换成有语法错误的写法
	h.sendDidChange(uri, 2, `[
		{"range":{"start":{"line":1,"character":4},"end":{"line":1,"character":11}},"text":"DISPLAYX"},
		{"range":{"start":{"line":2,"character":4},"end":{"line":2,"character":11}},"text":"DISPLAYY"}
	]`)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) == 0 {
		t.Error("两次非法替换后应产生诊断")
	}
}

func TestDidChangeFullReplace(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/full.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\nEND MAIN\n", 1)
	if diags := h.drainDiagnostics(uri, 5*time.Second); len(diags) != 0 {
		t.Fatalf("干净文件不应有诊断：%v", diags)
	}

	// 不带 range → 整文替换为损坏内容
	h.sendDidChange(uri, 2, `[{"text":"MAIN\n    DISPLAY \"unterminated\n"}]`)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) == 0 {
		t.Error("整文替换为损坏内容后应产生诊断")
	}
}

func TestDidChangeUTF16Positions(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	const uri = "file:///tmp/utf16.4gl"
	// 含中文，UTF-16 列与字节列不同
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"中文\"\nEND MAIN\n", 1)
	if diags := h.drainDiagnostics(uri, 5*time.Second); len(diags) != 0 {
		t.Fatalf("干净文件不应有诊断：%v", diags)
	}

	// 第 1 行 "    DISPLAY " 共 12 列；UTF-16 列 13 指向第一个引号内
	// 删除第 1 行 12..18（"中文" + 收尾引号）→ 字符串未闭合
	h.sendDidChange(uri, 2, `[{"range":{"start":{"line":1,"character":12},"end":{"line":1,"character":18}},"text":""}]`)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) == 0 {
		t.Error("UTF-16 列定位错误：删除字符串后应产生诊断")
	}
}

func TestDidChangeUnknownDocumentIgnored(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	// 未打开就 didChange：应静默忽略，服务仍然可用
	h.sendDidChange("file:///tmp/unknown.4gl", 1, `[{"text":"x"}]`)

	h.send(`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	msg := h.readMessage(3 * time.Second)
	if msg["id"] != float64(2) {
		t.Fatalf("未知文档的 didChange 不应影响服务：%v", msg)
	}
}

func TestExitWithoutShutdownSetsExitCode(t *testing.T) {
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(pool, logger, "test")

	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.ServeStream(ctx, h.stream) }()

	h.send(`{"jsonrpc":"2.0","method":"exit","params":{}}`)

	select {
	case err := <-done:
		cancel()
		var exitErr interface{ ExitCode() int }
		if !errors.As(err, &exitErr) {
			t.Fatalf("期望 exitError，实际 %v", err)
		}
		if got := exitErr.ExitCode(); got != 1 {
			t.Errorf("退出码 = %d，期望 1", got)
		}
	case <-time.After(3 * time.Second):
		cancel()
		t.Fatal("exit 后服务未退出（可能死锁）")
	}
}

func TestCleanShutdownReturnsNil(t *testing.T) {
	pool, err := parser.New()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv := New(pool, logger, "test")

	h := newHarness(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- srv.ServeStream(ctx, h.stream) }()

	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)

	h.send(`{"jsonrpc":"2.0","id":2,"method":"shutdown"}`)
	h.readMessage(3 * time.Second)

	h.send(`{"jsonrpc":"2.0","method":"exit","params":{}}`)

	select {
	case err := <-done:
		if err != nil {
			t.Errorf("正常退出应返回 nil，实际 %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("shutdown+exit 后服务未退出")
	}
}

const symSource = `SCHEMA ds

DEFINE g_count LIKE type_file.num5

TYPE t_rec RECORD
    a LIKE type_file.num5,
    b STRING
END RECORD

MAIN
    DEFINE l_local INTEGER
    LET l_local = g_count
END MAIN

FUNCTION helper(p_val)
    DEFINE l_tmp STRING
    RETURN p_val
END FUNCTION
`

// requestSymbol 发起一次请求并等待同 id 的响应。
func (h *harness) request(id int, method, params string) map[string]any {
	h.t.Helper()
	h.send(fmt.Sprintf(`{"jsonrpc":"2.0","id":%d,"method":%q,"params":%s}`, id, method, params))
	for {
		msg := h.readMessage(5 * time.Second)
		if msg["method"] != nil {
			continue // 跳过诊断等通知
		}
		if msg["id"] == float64(id) {
			return msg
		}
	}
}

func (h *harness) openBDL(uri, text string) {
	h.t.Helper()
	h.sendDidOpen(uri, "bdl", text, 1)
	// 等首轮诊断，确保解析完成
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		msg := h.readMessage(time.Until(deadline))
		if msg["method"] == "textDocument/publishDiagnostics" {
			return
		}
	}
	h.t.Fatal("等待 didOpen 诊断超时")
}

func (h *harness) init() {
	h.t.Helper()
	h.send(`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}`)
	h.readMessage(3 * time.Second)
}

func TestDocumentSymbol(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/sym.4gl"
	h.openBDL(uri, symSource)

	resp := h.request(10, "textDocument/documentSymbol", fmt.Sprintf(`{"textDocument":{"uri":%q}}`, uri))
	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("result 不是数组: %v", resp)
	}
	if len(result) == 0 {
		t.Fatal("文档符号为空")
	}

	names := map[string]map[string]any{}
	for _, raw := range result {
		sym, _ := raw.(map[string]any)
		if name, _ := sym["name"].(string); name != "" {
			names[name] = sym
		}
	}

	for _, want := range []string{"g_count", "t_rec", "main", "helper"} {
		if _, ok := names[want]; !ok {
			t.Errorf("缺少顶层符号 %q，实际有 %v", want, keys(names))
		}
	}

	// helper 应有参数与局部变量子节点
	if helper, ok := names["helper"]; ok {
		children, _ := helper["children"].([]any)
		if len(children) < 2 {
			t.Errorf("helper 子符号 = %d，期望至少 2（参数+局部变量）", len(children))
		}
	}
}

func keys(m map[string]map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestDocumentSymbolNestedRecord(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/sym2.4gl"
	h.openBDL(uri, symSource)

	resp := h.request(11, "textDocument/documentSymbol", fmt.Sprintf(`{"textDocument":{"uri":%q}}`, uri))
	result, _ := resp["result"].([]any)

	var rec map[string]any
	for _, raw := range result {
		sym, _ := raw.(map[string]any)
		if sym["name"] == "t_rec" {
			rec = sym
		}
	}
	if rec == nil {
		t.Fatal("未找到 t_rec")
	}
	children, _ := rec["children"].([]any)
	if len(children) != 2 {
		t.Errorf("t_rec 字段数 = %d，期望 2", len(children))
	}
}

func TestHoverFunction(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/hover.4gl"
	h.openBDL(uri, symSource)

	// "FUNCTION helper(p_val)" 位于第 14 行（0 基），helper 从第 9 列开始
	resp := h.request(12, "textDocument/hover",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":14,"character":10}}`, uri))

	result, ok := resp["result"].(map[string]any)
	if !ok {
		t.Fatalf("hover 无结果: %v", resp)
	}
	contents, _ := result["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	if !strings.Contains(value, "FUNCTION helper(p_val)") {
		t.Errorf("hover 文本 = %q，未包含签名", value)
	}
}

func TestHoverRecordField(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/hover2.4gl"
	h.openBDL(uri, symSource)

	// "    a LIKE type_file.num5," 在第 5 行，列 4
	resp := h.request(13, "textDocument/hover",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":5,"character":5}}`, uri))

	result, _ := resp["result"].(map[string]any)
	if result == nil {
		t.Fatal("记录字段应有 hover")
	}
	contents, _ := result["contents"].(map[string]any)
	value, _ := contents["value"].(string)
	if !strings.Contains(value, "LIKE type_file.num5") {
		t.Errorf("hover 文本 = %q", value)
	}
}

func TestHoverOnWhitespaceReturnsNull(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/hover3.4gl"
	h.openBDL(uri, symSource)

	// 第 1 行是空行
	resp := h.request(14, "textDocument/hover",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":1,"character":0}}`, uri))
	if resp["result"] != nil {
		t.Errorf("空白处应返回 null，实际 %v", resp["result"])
	}
}

func TestWorkspaceSymbol(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	h.openBDL("file:///tmp/ws1.4gl", symSource)
	h.openBDL("file:///tmp/ws2.4gl", "FUNCTION another_fn(x)\n    RETURN x\nEND FUNCTION\n")

	resp := h.request(15, "workspace/symbol", `{"query":""}`)
	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("result 不是数组: %v", resp)
	}

	names := map[string]bool{}
	for _, raw := range result {
		sym, _ := raw.(map[string]any)
		if name, _ := sym["name"].(string); name != "" {
			names[name] = true
		}
	}
	for _, want := range []string{"helper", "another_fn"} {
		if !names[want] {
			t.Errorf("工作区符号缺少 %q，实际 %v", want, names)
		}
	}
	// 局部变量不应出现在工作区符号里
	if names["l_tmp"] {
		t.Error("局部变量不应出现在工作区符号中")
	}
}

func TestWorkspaceSymbolFilter(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	h.openBDL("file:///tmp/ws3.4gl", symSource)

	resp := h.request(16, "workspace/symbol", `{"query":"help"}`)
	result, _ := resp["result"].([]any)
	if len(result) == 0 {
		t.Fatal("按名字过滤应返回 helper")
	}
	for _, raw := range result {
		sym, _ := raw.(map[string]any)
		name, _ := sym["name"].(string)
		if !strings.Contains(strings.ToLower(name), "help") {
			t.Errorf("过滤结果包含不匹配的 %q", name)
		}
	}
}

func TestPerDocumentSymbol(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const perSrc = `SCHEMA formonly

LAYOUT (text=%"t")
GRID
{
  Name:  [f1        ]
}
END
END

ATTRIBUTES
EDIT f1 = formonly.cust_name;
ButtonEdit f2 = formonly.act;
END

INSTRUCTIONS
SCREEN RECORD rec_x(formonly.cust_name)
END
`
	const uri = "file:///tmp/form.per"
	h.sendDidOpen(uri, "per", perSrc, 1)
	for {
		msg := h.readMessage(5 * time.Second)
		if msg["method"] == "textDocument/publishDiagnostics" {
			break
		}
	}

	resp := h.request(17, "textDocument/documentSymbol", fmt.Sprintf(`{"textDocument":{"uri":%q}}`, uri))
	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("result 不是数组: %v", resp)
	}

	names := map[string]bool{}
	for _, raw := range result {
		sym, _ := raw.(map[string]any)
		if name, _ := sym["name"].(string); name != "" {
			names[name] = true
		}
	}
	for _, want := range []string{"f1", "f2", "rec_x"} {
		if !names[want] {
			t.Errorf(".per 符号缺少 %q，实际 %v", want, names)
		}
	}
}

func TestDefinitionGlobalVariable(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/def1.4gl"
	h.openBDL(uri, symSource)

	// 第 11 行 "    LET l_local = g_count" 中 g_count 在第 18 列
	resp := h.request(20, "textDocument/definition",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":11,"character":19}}`, uri))

	result, ok := resp["result"].([]any)
	if !ok || len(result) == 0 {
		t.Fatalf("g_count 应有定义: %v", resp)
	}
	loc, _ := result[0].(map[string]any)
	rng, _ := loc["range"].(map[string]any)
	start, _ := rng["start"].(map[string]any)
	if start["line"] != float64(2) {
		t.Errorf("g_count 定义在第 2 行，实际 %v", start["line"])
	}
}

func TestDefinitionFunction(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/def2.4gl"
	h.openBDL(uri, "MAIN\n    CALL helper(1)\nEND MAIN\n\nFUNCTION helper(x)\n    RETURN x\nEND FUNCTION\n")

	// 第 1 行 "    CALL helper(1)" 中 helper 从第 9 列开始
	resp := h.request(21, "textDocument/definition",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":1,"character":10}}`, uri))
	result, _ := resp["result"].([]any)
	if len(result) == 0 {
		t.Fatalf("helper 应有定义: %v", resp)
	}
	loc, _ := result[0].(map[string]any)
	rng, _ := loc["range"].(map[string]any)
	start, _ := rng["start"].(map[string]any)
	if start["line"] != float64(4) {
		t.Errorf("helper 定义在第 4 行，实际 %v", start["line"])
	}
}

func TestDefinitionLocalVariablePrefersLocal(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	// 局部与全局同名，光标在函数内应跳局部
	const src = `DEFINE g_val INTEGER

MAIN
    DEFINE g_val INTEGER
    LET g_val = 1
END MAIN
`
	const uri = "file:///tmp/def3.4gl"
	h.openBDL(uri, src)

	resp := h.request(22, "textDocument/definition",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":4,"character":8}}`, uri))
	result, _ := resp["result"].([]any)
	if len(result) == 0 {
		t.Fatalf("应有定义: %v", resp)
	}
	loc, _ := result[0].(map[string]any)
	rng, _ := loc["range"].(map[string]any)
	start, _ := rng["start"].(map[string]any)
	if start["line"] != float64(3) {
		t.Errorf("应优先跳局部定义（第 3 行），实际 %v", start["line"])
	}
}

func TestDefinitionRecordField(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const src = `MAIN
    DEFINE r RECORD
        f1 LIKE a.b,
        f2 INTEGER
    END RECORD
    LET r.f1 = 1
END MAIN
`
	const uri = "file:///tmp/def4.4gl"
	h.openBDL(uri, src)

	// 第 5 行 "    LET r.f1 = 1" 中 f1 在第 10 列
	resp := h.request(23, "textDocument/definition",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":5,"character":10}}`, uri))
	result, _ := resp["result"].([]any)
	if len(result) == 0 {
		t.Fatalf("r.f1 应跳到记录字段定义: %v", resp)
	}
	loc, _ := result[0].(map[string]any)
	rng, _ := loc["range"].(map[string]any)
	start, _ := rng["start"].(map[string]any)
	if start["line"] != float64(2) {
		t.Errorf("f1 定义在第 2 行，实际 %v", start["line"])
	}
}

func TestDefinitionNotFound(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/def5.4gl"
	h.openBDL(uri, symSource)

	resp := h.request(24, "textDocument/definition",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":13,"character":0}}`, uri))
	if resp["result"] != nil {
		t.Errorf("空行应返回 null，实际 %v", resp["result"])
	}
}

func TestReferencesFindsAllUses(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const src = `DEFINE g_count INTEGER

MAIN
    LET g_count = 1
    DISPLAY g_count
END MAIN
`
	const uri = "file:///tmp/ref1.4gl"
	h.openBDL(uri, src)

	// 第 3 行 "    LET g_count = 1"，g_count 在第 8 列
	resp := h.request(30, "textDocument/references",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":3,"character":9},"context":{"includeDeclaration":true}}`, uri))
	result, ok := resp["result"].([]any)
	if !ok {
		t.Fatalf("references 失败: %v", resp)
	}
	// 定义 1 次 + 使用 2 次
	if len(result) != 3 {
		t.Errorf("引用数 = %d，期望 3：%v", len(result), result)
	}
}

func TestReferencesExcludesDeclaration(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const src = `DEFINE g_count INTEGER

MAIN
    LET g_count = 1
    DISPLAY g_count
END MAIN
`
	const uri = "file:///tmp/ref2.4gl"
	h.openBDL(uri, src)

	resp := h.request(31, "textDocument/references",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":3,"character":9},"context":{"includeDeclaration":false}}`, uri))
	result, _ := resp["result"].([]any)
	if len(result) != 2 {
		t.Errorf("不含声明时应为 2，实际 %d：%v", len(result), result)
	}
}

func TestReferencesExcludesCommentsAndStrings(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const src = `DEFINE g_count INTEGER
-- g_count 在注释里
MAIN
    DISPLAY "g_count 在字符串里"
    LET g_count = 1
END MAIN
`
	const uri = "file:///tmp/ref3.4gl"
	h.openBDL(uri, src)

	resp := h.request(32, "textDocument/references",
		fmt.Sprintf(`{"textDocument":{"uri":%q},"position":{"line":4,"character":9},"context":{"includeDeclaration":true}}`, uri))
	result, _ := resp["result"].([]any)
	// 定义 1 + 使用 1；注释与字符串里的不算
	if len(result) != 2 {
		t.Errorf("引用数 = %d，期望 2（排除注释与字符串）：%v", len(result), result)
	}
}

func TestDiagnosticsSeverityConfigurable(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/sev.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"unterminated\n", 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) == 0 {
		t.Fatal("应有诊断")
	}
	d, _ := diags[0].(map[string]any)
	// Q10 定稿：语法错误默认 warning，不再是 error。
	if sev, _ := d["severity"].(float64); sev != 2 {
		t.Errorf("默认应为 warning(2)，实际 %v", d["severity"])
	}

	// 旧键改成 error 后应重新诊断（红波浪线仍可要回来）
	h.send(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"syntaxDiagnostics":"error"}}}`)
	diags = h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) == 0 {
		t.Fatal("配置变更后仍应有诊断")
	}
	d, _ = diags[0].(map[string]any)
	if sev, _ := d["severity"].(float64); sev != 1 {
		t.Errorf("应为 error(1)，实际 %v", d["severity"])
	}
}

func TestDiagnosticsOff(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/off.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"unterminated\n", 1)
	h.drainDiagnostics(uri, 5*time.Second)

	h.send(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"syntaxDiagnostics":"off"}}}`)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) != 0 {
		t.Errorf("关闭后应无诊断，实际 %d 条", len(diags))
	}
}

func TestDiagnosticsNestedSettings(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/nested.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"unterminated\n", 1)
	h.drainDiagnostics(uri, 5*time.Second)

	// VSCode 会把设置包在 "bdl" 键下
	h.send(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"bdl":{"syntaxDiagnostics":"information"}}}}`)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) == 0 {
		t.Fatal("嵌套设置应生效")
	}
	d, _ := diags[0].(map[string]any)
	if sev, _ := d["severity"].(float64); sev != 3 {
		t.Errorf("应为 information(3)，实际 %v", d["severity"])
	}
}

func TestLargeFileSkipsDiagnostics(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	// 把上限调低，构造超限内容
	h.send(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"limits":{"maxFileBytes":100}}}}`)

	const uri = "file:///tmp/big.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"unterminated\nEND MAIN\n"+strings.Repeat("-- padding\n", 30), 1)

	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) != 1 {
		t.Fatalf("超限文件应只有 1 条提示，实际 %d", len(diags))
	}
	d, _ := diags[0].(map[string]any)
	if code, _ := d["code"].(string); code != "file-too-large" {
		t.Errorf("code = %v，期望 file-too-large", d["code"])
	}
	if sev, _ := d["severity"].(float64); sev != 3 {
		t.Errorf("提示应为 information(3)，实际 %v", d["severity"])
	}
}

func TestDiagnosticsHaveCode(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/code.4gl"
	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"unterminated\n", 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) == 0 {
		t.Fatal("应有诊断")
	}
	d, _ := diags[0].(map[string]any)
	code, _ := d["code"].(string)
	if code == "" {
		t.Error("诊断应带 code 便于过滤")
	}
	if !strings.HasPrefix(code, "syntax-error") && !strings.HasPrefix(code, "missing-") {
		t.Errorf("code = %q，格式不符", code)
	}
}

// P7-B：语法诊断也吃 bdl.diagnostics 逐码配置。
func TestSyntaxErrorPerCodeOverride(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()
	h.init()

	const uri = "file:///tmp/sev2.4gl"
	// 新键把语法错误配成 error（默认已经是 warning）。
	h.send(`{"jsonrpc":"2.0","method":"workspace/didChangeConfiguration","params":{"settings":{"bdl":{"diagnostics":{"syntax-error":"error"}}}}}`)
	time.Sleep(120 * time.Millisecond)

	h.sendDidOpen(uri, "bdl", "MAIN\n    DISPLAY \"unterminated\n", 1)
	diags := h.drainDiagnostics(uri, 5*time.Second)
	if len(diags) == 0 {
		t.Fatal("应有诊断")
	}
	d, _ := diags[0].(map[string]any)
	if sev, _ := d["severity"].(float64); sev != 1 {
		t.Errorf("bdl.diagnostics.syntax-error=error 应生效，实际 %v", d["severity"])
	}
}

// P7-B：MISSING 节点的两级逐码配置（精确 missing-<kind> > 整簇 missing-node > 旧键）。
func TestMissingNodeSeverityPriority(t *testing.T) {
	iss := parser.Issue{Missing: true, Kind: "end_if"}

	// 1) 旧键兜底
	got, on := severityFor(iss, config.Settings{MissingDiagnostics: config.LevelWarning})
	if !on || got != protocol.DiagnosticSeverityWarning {
		t.Errorf("旧键 warning 应生效，得到 (%v,%v)", got, on)
	}

	// 2) 整簇 missing-node 压过旧键
	got, on = severityFor(iss, config.Settings{
		MissingDiagnostics: config.LevelWarning,
		Diagnostics:        map[string]config.DiagnosticLevel{"missing-node": config.LevelHint},
	})
	if !on || got != protocol.DiagnosticSeverityHint {
		t.Errorf("missing-node 应压过旧键，得到 (%v,%v)", got, on)
	}

	// 3) 精确 missing-<kind> 压过整簇
	got, on = severityFor(iss, config.Settings{
		Diagnostics: map[string]config.DiagnosticLevel{
			"missing-node":   config.LevelHint,
			"missing-end_if": config.LevelError,
		},
	})
	if !on || got != protocol.DiagnosticSeverityError {
		t.Errorf("missing-end_if 应压过 missing-node，得到 (%v,%v)", got, on)
	}

	// 4) 关掉整簇
	if _, on = severityFor(iss, config.Settings{
		Diagnostics: map[string]config.DiagnosticLevel{"missing-node": config.LevelOff},
	}); on {
		t.Error("missing-node=off 应关闭")
	}

	// 5) 语法错误走固定码 syntax-error
	got, on = severityFor(parser.Issue{Kind: "ERROR"}, config.Settings{
		Diagnostics: map[string]config.DiagnosticLevel{"syntax-error": config.LevelHint},
	})
	if !on || got != protocol.DiagnosticSeverityHint {
		t.Errorf("syntax-error 逐码配置应生效，得到 (%v,%v)", got, on)
	}
}
