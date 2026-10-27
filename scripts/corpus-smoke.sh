#!/usr/bin/env bash
# 语料冒烟：抽样解析 tiptop-gp 的 .4gl / .per，统计通过率。
#
# 用法：
#   bash scripts/corpus-smoke.sh [样本数] [语料根目录]
#
# 输出：artifacts/logs/corpus-smoke-<时间>.log
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/artifacts/bin/bdl-lsp"
CORPUS="${2:-/code/workspace/tiptop-gp}"
LIMIT="${1:-200}"
LOGDIR="$ROOT/artifacts/logs"
LOG="$LOGDIR/corpus-smoke-$(date +%Y%m%d-%H%M%S).log"

mkdir -p "$LOGDIR"

if [ ! -x "$BIN" ]; then
  echo "缺少 ${BIN}，请先 make build" >&2
  exit 2
fi
if [ ! -d "$CORPUS" ]; then
  echo "语料目录不存在：$CORPUS" >&2
  exit 2
fi

# 固定抽样：排序后均匀取 LIMIT 个，保证可复现。
mapfile -t per_files < <(find "$CORPUS" -name '*.per' -type f | sort | awk -v n="$LIMIT" 'NR % 37 == 0' | head -n "$LIMIT")
mapfile -t bdl_files < <(find "$CORPUS" -name '*.4gl' -type f | sort | awk -v n="$LIMIT" 'NR % 37 == 0' | head -n "$LIMIT")

{
  echo "== 语料冒烟 $(date -Iseconds) =="
  echo "语料根：$CORPUS"
  echo "抽样：per=${#per_files[@]} bdl=${#bdl_files[@]}（步长 37）"
  echo

  echo "-- .per"
  if [ "${#per_files[@]}" -gt 0 ]; then
    "$BIN" check -q "${per_files[@]}" || true
  else
    echo "（未找到 .per）"
  fi

  echo
  echo "-- .4gl"
  if [ "${#bdl_files[@]}" -gt 0 ]; then
    "$BIN" check -q "${bdl_files[@]}" || true
  else
    echo "（未找到 .4gl）"
  fi
} | tee "$LOG"

echo
echo "日志：$LOG"
echo "提示：语料中已知存在语法错误的文件会被计入“有问题”，这是预期行为。"
