#!/usr/bin/env python3
"""复现/回归：后台工作区索引与请求处理并发时，tree-sitter 崩溃（P0-1）。

用法：
    python3 scripts/repro-index-crash.py [工作区根] [要打开的 .4gl]

默认：工作区根 /code/workspace/tiptop-gp，打开 tiptop/axm/4gl/saxmt600.4gl（41486 行）。

判定：
    - 工作区是**全量语料**时：v0.3.4 起会 SIGSEGV（进程退出、stdout 关闭）→ 打印 CRASH；
    - 工作区只放这一个文件时：正常返回诊断 → 打印 OK。
    两者的差别就是"索引有没有在后台跑"，用来证明崩溃来自并发而不是文件大小。

修复目标：两种情况都必须是 OK。
"""
import json
import os
import subprocess
import sys
import time

BIN = os.environ.get(
    "BDL_LSP_BIN",
    os.path.join(os.path.dirname(os.path.abspath(__file__)), "..", "artifacts", "bin", "bdl-lsp"),
)
ROOT = sys.argv[1] if len(sys.argv) > 1 else "/code/workspace/tiptop-gp"
TARGET = sys.argv[2] if len(sys.argv) > 2 else os.path.join(ROOT, "tiptop/axm/4gl/saxmt600.4gl")

# 放宽限制，让大文件真的走到"解析 + 语义检查"（默认限制下会被直接跳过，掩盖问题）。
LIMITS = {"maxFileBytes": 20971520, "maxFileLines": 200000, "parseTimeoutMs": 20000}


def frame(obj):
    body = json.dumps(obj).encode()
    return b"Content-Length: %d\r\n\r\n" % len(body) + body


def read_msg(f):
    line = f.readline()
    if not line:
        return None
    n = int(line.split(b":")[1].strip())
    f.readline()
    return json.loads(f.read(n))


def main():
    if not os.path.exists(BIN):
        sys.exit(f"找不到语言服务器：{BIN}（先 make build，或用 BDL_LSP_BIN 指定）")
    print(f"server  : {BIN}")
    print(f"rootUri : file://{ROOT}")
    print(f"didOpen : {TARGET}")

    p = subprocess.Popen([BIN, "serve"], stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                         stderr=subprocess.DEVNULL)
    uri = "file://" + TARGET
    p.stdin.write(frame({"jsonrpc": "2.0", "id": 1, "method": "initialize", "params": {
        "processId": None, "rootUri": "file://" + ROOT, "capabilities": {},
        "initializationOptions": {"limits": LIMITS}}}))
    p.stdin.flush()
    read_msg(p.stdout)
    p.stdin.write(frame({"jsonrpc": "2.0", "method": "initialized", "params": {}}))
    p.stdin.flush()

    with open(TARGET, encoding="utf-8", errors="replace") as f:
        text = f.read()
    p.stdin.write(frame({"jsonrpc": "2.0", "method": "textDocument/didOpen", "params": {
        "textDocument": {"uri": uri, "languageId": "bdl", "version": 1, "text": text}}}))
    p.stdin.flush()

    start = time.time()
    result = "TIMEOUT"
    while time.time() - start < 40:
        msg = read_msg(p.stdout)
        if msg is None:
            result = "CRASH"  # stdout 关闭 = 服务端挂了
            break
        if msg.get("method") == "textDocument/publishDiagnostics":
            result = "OK"
            break

    print(f"结果    : {result}（{time.time() - start:.1f}s，退出码 {p.poll()}）")
    p.kill()
    # 非 0 退出即视为失败（供 CI/回归使用）
    sys.exit(0 if result == "OK" else 1)


if __name__ == "__main__":
    main()