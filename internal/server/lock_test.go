package server

import (
	"testing"
	"time"
)

// 这组测试钉住 P0-1 的不变量：链接预热回调触发的"全部重诊断"
// （requestRediagnose）不能阻塞调用方，也不能在 handler 遍历文档树时
// 并发地 SetTree 关掉那棵树。
//
// 背景（2026-10-23 实测）：link.SymbolIndex.StartGlobal 在后台 goroutine
// 里回调 onReady；回调里直接重诊断会在 handler 正跑字段校验（大文件约 1 s）
// 时关掉它的树 → `ts_node_parent` 读已释放内存 → SIGSEGV。全量语料 +
// 4 万行文件 8/8 复现；把 `.link/` 移走（后台索引照跑）后 0/3 不复现。
//
// 另一半：没有全局模组时 link.Resolver.Start **同步**回调，而调用链
// （maybeReloadLinks → reparseFull → handle）**已持有 parseMu** ——
// 回调里直接加锁就是自锁死（曾让两个链接测试挂起）。

// TestRequestRediagnoseNeverBlocks 持锁期间调用必须立刻返回（不自锁死）。
func TestRequestRediagnoseNeverBlocks(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.srv.parseMu.Lock()
	done := make(chan struct{})
	go func() {
		h.srv.requestRediagnose()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		t.Fatal("requestRediagnose 阻塞了调用方：回调可能已经持有 parseMu")
	}
	h.srv.parseMu.Unlock()
}

// TestRequestRediagnoseSerializesWithRequests 重诊断必须等 parseMu：
// 持锁期间不得重解析，释放后才发生。
//
// 观测的是**帧**而不是诊断内容：干净文件的 publishDiagnostics 里
// diagnostics 是空数组，用内容无法区分"发布过"和"没发布"。
func TestRequestRediagnoseSerializesWithRequests(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	const src = "MAIN\n    DISPLAY \"x\"\nEND MAIN\n"
	uri := testURI(t.TempDir(), "main.4gl")
	h.sendDidOpen(uri, "bdl", src, 1)

	if !waitPublish(uri, h, 3*time.Second) {
		t.Fatal("didOpen 后没有收到 publishDiagnostics")
	}

	// 持锁请求重诊断：这段时间里不该有新的发布（重解析被 parseMu 挡住）。
	h.srv.parseMu.Lock()
	h.srv.requestRediagnose()
	if waitPublish(uri, h, 300*time.Millisecond) {
		h.srv.parseMu.Unlock()
		t.Fatal("持 parseMu 期间发生了重诊断：会与请求处理并发关闭文档树")
	}
	h.srv.parseMu.Unlock()

	// 释放锁后必须补发一轮。
	if !waitPublish(uri, h, 3*time.Second) {
		t.Fatal("释放 parseMu 后没有发生重诊断：请求被丢弃了")
	}
}

// waitPublish 等到该 URI 的 publishDiagnostics 帧（内容不计）。
func waitPublish(uri string, h *harness, window time.Duration) bool {
	deadline := time.Now().Add(window)
	for time.Now().Before(deadline) {
		msg := h.readMessageSoft(time.Until(deadline))
		if msg == nil {
			return false
		}
		if msg["method"] != "textDocument/publishDiagnostics" {
			continue
		}
		p, _ := msg["params"].(map[string]any)
		if p["uri"] == uri {
			return true
		}
	}
	return false
}

// TestRediagnoseAllLockedAssumesParseMuHeld 持锁版本不得再次加锁：
// reparseFull 在 parseMu 内遇到资料重载时调用它。
func TestRediagnoseAllLockedAssumesParseMuHeld(t *testing.T) {
	h, cleanup := startServer(t)
	defer cleanup()

	h.srv.parseMu.Lock()
	defer h.srv.parseMu.Unlock()

	done := make(chan struct{})
	go func() {
		h.srv.rediagnoseAllLocked()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("rediagnoseAllLocked 阻塞了：它不应再次获取 parseMu")
	}
}
