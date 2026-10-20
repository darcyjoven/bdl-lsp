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

	"github.com/darcyjoven/bdl-lsp/internal/parser"
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
	stream *pipeStream
	client *io.PipeWriter
	server *io.PipeReader
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
	}
	return h
}

func (h *harness) send(payload string) {
	h.t.Helper()
	frame := fmt.Sprintf("Content-Length: %d\r\n\r\n%s", len(payload), payload)
	if _, err := io.WriteString(h.client, frame); err != nil {
		h.t.Fatalf("写入请求失败: %v", err)
	}
}

// readMessage 读取一帧并解析为通用 map。
func (h *harness) readMessage(timeout time.Duration) map[string]any {
	h.t.Helper()

	type result struct {
		msg map[string]any
		err error
	}
	ch := make(chan result, 1)

	go func() {
		br := bufio.NewReader(h.server)
		var length int
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				ch <- result{err: err}
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				break
			}
			if name, value, ok := strings.Cut(line, ":"); ok && strings.EqualFold(strings.TrimSpace(name), "Content-Length") {
				n, convErr := strconv.Atoi(strings.TrimSpace(value))
				if convErr != nil {
					ch <- result{err: convErr}
					return
				}
				length = n
			}
		}
		if length == 0 {
			ch <- result{err: fmt.Errorf("缺少 Content-Length")}
			return
		}
		buf := make([]byte, length)
		if _, err := io.ReadFull(br, buf); err != nil {
			ch <- result{err: err}
			return
		}
		var msg map[string]any
		if err := json.Unmarshal(buf, &msg); err != nil {
			ch <- result{err: err}
			return
		}
		ch <- result{msg: msg}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			h.t.Fatalf("读取响应失败: %v", res.err)
		}
		return res.msg
	case <-time.After(timeout):
		h.t.Fatal("等待响应超时")
		return nil
	}
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
