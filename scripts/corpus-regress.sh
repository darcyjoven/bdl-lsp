#!/usr/bin/env bash
# 全量语料回归：解析并统计通过率、耗时。
#
# 用法：
#   bash scripts/corpus-regress.sh [语料根目录] [--limit N]
#
# 结果写入 artifacts/logs/corpus-regress-<时间>.log 和同名的 .csv。
#
# 与 corpus-smoke.sh 的区别：
#   - smoke 抽样，用于快速冒烟；
#   - regress 跑全量（默认），用于发布前基线对比与性能观察。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
BIN="$ROOT/artifacts/bin/bdl-lsp"
CORPUS="/code/workspace/tiptop-gp"
LIMIT=0

while [ $# -gt 0 ]; do
  case "$1" in
    --limit) LIMIT="$2"; shift 2 ;;
    -h|--help) sed -n 's/^# //p' "$0" | head -20; exit 0 ;;
    *) CORPUS="$1"; shift ;;
  esac
done

LOGDIR="$ROOT/artifacts/logs"
STAMP="$(date +%Y%m%d-%H%M%S)"
LOG="$LOGDIR/corpus-regress-$STAMP.log"
CSV="$LOGDIR/corpus-regress-$STAMP.csv"
mkdir -p "$LOGDIR"

if [ ! -x "$BIN" ]; then
  echo "缺少 ${BIN}，请先 make build" >&2
  exit 2
fi
if [ ! -d "$CORPUS" ]; then
  echo "语料目录不存在：$CORPUS" >&2
  exit 2
fi

mapfile -t per_files < <(find "$CORPUS" -name '*.per' -type f | sort)
mapfile -t bdl_files < <(find "$CORPUS" -name '*.4gl' -type f | sort)

if [ "$LIMIT" -gt 0 ]; then
  per_files=("${per_files[@]:0:$LIMIT}")
  bdl_files=("${bdl_files[@]:0:$LIMIT}")
fi

echo "file,lang,status,issues,ms" > "$CSV"

run_group() {
  local lang="$1"; shift
  local files=("$@")
  local total=${#files[@]}
  local ok=0 fail=0
  local start end elapsed

  start=$(date +%s%N)
  for f in "${files[@]}"; do
    local t0 t1 ms
    t0=$(date +%s%N)
    local out
    if out=$("$BIN" check --json "$f" 2>/dev/null); then
      ok=$((ok + 1))
      echo "$f,$lang,OK,0,0" >> "$CSV"
    else
      fail=$((fail + 1))
      local count
      count=$(printf '%s' "$out" | python3 -c 'import json,sys; d=json.load(sys.stdin); print(len(d[0]["issues"]) if d else 0)' 2>/dev/null || echo "?")
      echo "$f,$lang,FAIL,$count,0" >> "$CSV"
    fi
    t1=$(date +%s%N)
    ms=$(( (t1 - t0) / 1000000 ))
    # 回填耗时（简化：CSV 最后一行替换太麻烦，另存耗时统计即可）
    echo "$ms" >> "$LOGDIR/.times-$lang"
  done
  end=$(date +%s%N)
  elapsed=$(( (end - start) / 1000000 ))

  printf '%-6s 总数 %-6d 通过 %-6d 失败 %-6d 总耗时 %sms\n' "$lang" "$total" "$ok" "$fail" "$elapsed"
  printf '%s|%d|%d|%d|%d\n' "$lang" "$total" "$ok" "$fail" "$elapsed" >> "$LOGDIR/.summary-$STAMP"
}

{
  echo "== 全量语料回归 $(date -Iseconds) =="
  echo "语料根：$CORPUS"
  echo "样本：per=${#per_files[@]} bdl=${#bdl_files[@]}"
  echo

  rm -f "$LOGDIR/.times-per" "$LOGDIR/.times-bdl"
  rm -f "$LOGDIR/.summary-$STAMP"

  [ "${#per_files[@]}" -gt 0 ] && run_group per "${per_files[@]}"
  [ "${#bdl_files[@]}" -gt 0 ] && run_group bdl "${bdl_files[@]}"

  echo
  echo "== 耗时分布 =="
  for lang in per bdl; do
    if [ -f "$LOGDIR/.times-$lang" ]; then
      python3 - "$LOGDIR/.times-$lang" "$lang" <<'PY'
import sys, statistics
path, lang = sys.argv[1], sys.argv[2]
vals = [int(x) for x in open(path).read().split() if x.isdigit()]
if vals:
    vals.sort()
    p = lambda q: vals[min(len(vals)-1, int(len(vals)*q))]
    print(f"{lang:5s} n={len(vals):5d} 均值 {statistics.mean(vals):6.1f}ms "
          f"p50 {p(.5):5d}ms p95 {p(.95):5d}ms p99 {p(.99):5d}ms max {vals[-1]:5d}ms")
PY
    fi
    rm -f "$LOGDIR/.times-$lang"
  done

  echo
  echo "== 汇总 =="
  if [ -f "$LOGDIR/.summary-$STAMP" ]; then
    while IFS='|' read -r lang total ok fail ms; do
      if [ "$total" -gt 0 ]; then
        pct=$(awk "BEGIN {printf \"%.2f\", $ok*100/$total}")
        echo "$lang: $ok/$total 通过（${pct}%）"
      fi
    done < "$LOGDIR/.summary-$STAMP"
    rm -f "$LOGDIR/.summary-$STAMP"
  fi
} | tee "$LOG"

echo
echo "日志：$LOG"
echo "明细：$CSV"
echo
echo "说明：语料中已知存在语法错误的文件会计入失败，这是预期行为。"
echo "      对比时请以“通过数不低于基线”为准，而不是追求 100%。"
