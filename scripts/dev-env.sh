#!/usr/bin/env bash
# 用法： source scripts/dev-env.sh
#
# 本机沙箱内 $HOME 与 /home/darcy/go 不可写，因此把 Go / tree-sitter 的缓存
# 统一放到 bdl-lsp/artifacts/ 下。CI 不使用本脚本。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

export HOME="$ROOT/artifacts/home"
export GOPATH="$ROOT/artifacts/gopath"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$ROOT/artifacts/gocache"
export GOTOOLCHAIN=local
export CGO_ENABLED=1
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export PATH="$ROOT/artifacts/bin:$PATH"

mkdir -p "$HOME" "$GOPATH" "$GOCACHE" "$ROOT/artifacts/bin"

# tree-sitter CLI 需要可写的 HOME，并知道 grammar 仓库的位置
mkdir -p "$HOME/.config/tree-sitter"
if [ ! -f "$HOME/.config/tree-sitter/config.json" ]; then
  cat > "$HOME/.config/tree-sitter/config.json" <<'JSON'
{
  "parser-directories": ["/code/workspace"]
}
JSON
fi

echo "HOME=$HOME"
echo "GOPATH=$GOPATH"
echo "GOCACHE=$GOCACHE"
echo "GOPROXY=$GOPROXY"
