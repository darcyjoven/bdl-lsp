#!/usr/bin/env python3
"""LSP 端到端延迟基准。

用法：
    python3 scripts/lsp-bench.py [文件路径] [轮数]

测量两项：
  1. didOpen → 首轮诊断（含解析全量）
  2. didChange → 诊断（增量重解析往返）

结果写入 artifacts/logs/lsp-bench-<时间>.log，并打印摘要。
"""
import json
import os
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
BIN = os.path.join(ROOT, "artifacts", "bin", "bdl-lsp")
DEFAULT_FILE = "/code/workspace/tiptop-gp/tiptop/axr/4gl/s_t300_gl.4gl"


class Client:
    def __init__(self):
        self.proc = subprocess.Popen(
            [BIN],
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
        )

    def send(self, msg):
        body = json.dumps(msg).encode()
        self.proc.stdin.write(b"Content-Length: %d\r\n\r\n" % len(body) + body)
        self.proc.stdin.flush()

    def recv(self):
        length = None
        while True:
            line = self.proc.stdout.readline()
            if not line:
                return None
            line = line.strip()
            if not line:
                break
            if line.lower().startswith(b"content-length:"):
                length = int(line.split(b":")[1])
        return json.loads(self.proc.stdout.read(length))

    def wait_method(self, method):
        while True:
            msg = self.recv()
            if msg is None:
                raise RuntimeError("服务端提前关闭连接")
            if msg.get("method") == method:
                return msg

    def close(self):
        try:
            self.send({"jsonrpc": "2.0", "id": 99, "method": "shutdown"})
            self.send({"jsonrpc": "2.0", "method": "exit"})
        except Exception:
            pass
        try:
            self.proc.wait(timeout=5)
        except subprocess.TimeoutExpired:
            self.proc.kill()


def main():
    path = sys.argv[1] if len(sys.argv) > 1 else DEFAULT_FILE
    rounds = int(sys.argv[2]) if len(sys.argv) > 2 else 10

    if not os.path.exists(BIN):
        print(f"缺少 {BIN}，请先 make build", file=sys.stderr)
        return 2

    with open(path, "r", encoding="utf-8", errors="replace") as f:
        text = f.read()

    uri = "file:///bench.4gl"
    c = Client()
    c.send({"jsonrpc": "2.0", "id": 1, "method": "initialize",
            "params": {"processId": None, "rootUri": None, "capabilities": {}}})
    c.recv()

    t0 = time.perf_counter()
    c.send({"jsonrpc": "2.0", "method": "textDocument/didOpen",
            "params": {"textDocument": {"uri": uri, "languageId": "bdl",
                                        "version": 1, "text": text}}})
    c.wait_method("textDocument/publishDiagnostics")
    open_ms = (time.perf_counter() - t0) * 1000

    lat = []
    for i in range(rounds):
        c.send({"jsonrpc": "2.0", "method": "textDocument/didChange",
                "params": {"textDocument": {"uri": uri, "version": i + 2},
                           "contentChanges": [{"range": {"start": {"line": 100, "character": 0},
                                                         "end": {"line": 100, "character": 0}},
                                               "text": " "}]}})
        t0 = time.perf_counter()
        c.wait_method("textDocument/publishDiagnostics")
        lat.append((time.perf_counter() - t0) * 1000)

    c.close()

    lines = text.count("\n")
    summary = [
        f"文件: {path}",
        f"规模: {lines} 行, {len(text)} 字节",
        f"首轮 didOpen → 诊断: {open_ms:.1f} ms",
        f"增量 didChange 往返 (n={rounds}): "
        f"平均 {sum(lat)/len(lat):.1f} ms, "
        f"最小 {min(lat):.1f} ms, 最大 {max(lat):.1f} ms",
    ]
    print("\n".join(summary))

    logdir = os.path.join(ROOT, "artifacts", "logs")
    os.makedirs(logdir, exist_ok=True)
    logpath = os.path.join(logdir, time.strftime("lsp-bench-%Y%m%d-%H%M%S.log"))
    with open(logpath, "w", encoding="utf-8") as f:
        f.write("\n".join(summary) + "\n")
    print(f"日志: {logpath}")
    return 0


if __name__ == "__main__":
    sys.exit(main())
