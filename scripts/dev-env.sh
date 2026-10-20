#!/usr/bin/env bash
# 用法： source scripts/dev-env.sh
#
# 本机沙箱内 $HOME 与 /home/darcy/go 不可写，因此把 Go、Rust、Node 工具链
# 的缓存统一放到 bdl-lsp/artifacts/ 下。普通开发机/CI 不需要本脚本。
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLCHAIN="$ROOT/artifacts/toolchain"

export HOME="$ROOT/artifacts/home"
export GOPATH="$ROOT/artifacts/gopath"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$ROOT/artifacts/gocache"
export GOTOOLCHAIN=local
export CGO_ENABLED=1
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"

# Rust / WASM
export RUSTUP_HOME="$TOOLCHAIN/rustup"
export CARGO_HOME="$TOOLCHAIN/cargo"
export WASI_SDK_PATH="$TOOLCHAIN/wasi-sdk"

# gofmt 在 GOROOT/bin 下，且本机 /usr/local/bin 未收录，显式加入 PATH
GOROOT_BIN="$("$(command -v go)" env GOROOT 2>/dev/null)/bin"

# 本机安装的 CLI：rustc/cargo、wasi clang、code、gh、tree-sitter、Xvfb
export PATH="$TOOLCHAIN/bin:$CARGO_HOME/bin:$TOOLCHAIN/wasi-sdk/bin:$ROOT/artifacts/bin:$GOROOT_BIN:$PATH"

# 本机 Xvfb 依赖自带的 libXfont2（系统未安装）；其他工具不受影响
export LD_LIBRARY_PATH="$TOOLCHAIN/xvfb/usr/lib/x86_64-linux-gnu${LD_LIBRARY_PATH:+:$LD_LIBRARY_PATH}"
export XKB_CONFIG_ROOT="$TOOLCHAIN/xvfb/usr/share/X11/xkb"

# 无桌面环境的 VSCode e2e：优先用 headless ozone，失败再退回 Xvfb
export VSCODE_HEADLESS_ARGS="--ozone-platform=headless --disable-gpu --no-sandbox"

mkdir -p "$HOME" "$GOPATH" "$GOCACHE" "$ROOT/artifacts/bin" "$TOOLCHAIN/bin"

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
echo "CARGO_HOME=$CARGO_HOME"
echo "WASI_SDK_PATH=$WASI_SDK_PATH"
