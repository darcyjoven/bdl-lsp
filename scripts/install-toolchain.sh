#!/usr/bin/env bash
# 无 root 权限的 Linux 工具链安装（本机沙箱/CI 可用）。
#
# 用法：
#   bash scripts/install-toolchain.sh            # 安装缺失组件
#   bash scripts/install-toolchain.sh --check    # 只检查，不下载
#
# 安装位置：artifacts/toolchain/（Git 忽略）
# 已有系统包（go/gcc/node/git/ssh/rsync）不在此脚本内，需用系统包管理器安装。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TOOLCHAIN="$ROOT/artifacts/toolchain"
LOGS="$ROOT/artifacts/logs"
BIN="$TOOLCHAIN/bin"
CHECK_ONLY=0
[ "${1:-}" = "--check" ] && CHECK_ONLY=1

mkdir -p "$TOOLCHAIN" "$BIN" "$LOGS" "$ROOT/artifacts/home"

log() { printf '[install] %s\n' "$*"; }
have() { command -v "$1" >/dev/null 2>&1; }

# rustup/cargo 必须使用工作区内目录（本机 $HOME 只读）
export RUSTUP_HOME="$TOOLCHAIN/rustup"
export CARGO_HOME="$TOOLCHAIN/cargo"
export HOME="${HOME_OVERRIDE:-$ROOT/artifacts/home}"
mkdir -p "$HOME" "$RUSTUP_HOME" "$CARGO_HOME"

# ---------------------------------------------------------------- Rust / wasm
if [ -x "$TOOLCHAIN/cargo/bin/rustc" ]; then
  log "rustc 已存在：$("$TOOLCHAIN/cargo/bin/rustc" --version)"
else
  if [ "$CHECK_ONLY" = "1" ]; then
    log "缺少 rustc（需运行本脚本安装）"
  else
    log "安装 rustup + stable（minimal）"
    curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs -o "$TOOLCHAIN/rustup-init.sh"
    sh "$TOOLCHAIN/rustup-init.sh" -y --no-modify-path --profile minimal --default-toolchain stable \
        >>"$LOGS/rustup-install.log" 2>&1
  fi
fi
if [ -x "$TOOLCHAIN/cargo/bin/rustup" ]; then
  if ! "$TOOLCHAIN/cargo/bin/rustup" target list --installed 2>/dev/null | grep -qx wasm32-wasip2; then
    if [ "$CHECK_ONLY" = "1" ]; then log "缺少 target wasm32-wasip2"; else
      log "添加 target wasm32-wasip2"
      "$TOOLCHAIN/cargo/bin/rustup" target add wasm32-wasip2 >>"$LOGS/rustup-install.log" 2>&1
    fi
  fi
  ln -sfn ../cargo/bin/rustc "$BIN/rustc"
  ln -sfn ../cargo/bin/cargo "$BIN/cargo"
  ln -sfn ../cargo/bin/rustup "$BIN/rustup"
fi

# ---------------------------------------------------------------- wasi-sdk
WASI_VERSION="${WASI_VERSION:-34}"
WASI_NAME="wasi-sdk-${WASI_VERSION}.0"
if [ -x "$TOOLCHAIN/wasi-sdk/bin/clang" ]; then
  log "wasi-sdk 已存在：$("$TOOLCHAIN/wasi-sdk/bin/clang" --version | head -1)"
else
  if [ "$CHECK_ONLY" = "1" ]; then log "缺少 wasi-sdk"; else
    log "安装 $WASI_NAME（x86_64-linux）"
    curl -fL --retry 2 -o "$TOOLCHAIN/wasi-sdk.tar.gz" \
      "https://github.com/WebAssembly/wasi-sdk/releases/download/wasi-sdk-${WASI_VERSION}/${WASI_NAME}-x86_64-linux.tar.gz" \
      >>"$LOGS/wasi-download.log" 2>&1
    mkdir -p "$TOOLCHAIN/wasi-sdk"
    tar -xzf "$TOOLCHAIN/wasi-sdk.tar.gz" -C "$TOOLCHAIN/wasi-sdk" --strip-components=1
  fi
fi

# ---------------------------------------------------------------- gh CLI
GH_VERSION="${GH_VERSION:-2.102.0}"
if [ -x "$BIN/gh" ]; then
  log "gh 已存在：$("$BIN/gh" --version | head -1)"
else
  if [ "$CHECK_ONLY" = "1" ]; then log "缺少 gh"; else
    log "安装 gh $GH_VERSION（可选）"
    curl -fL --retry 2 -o "$TOOLCHAIN/gh.tar.gz" \
      "https://github.com/cli/cli/releases/download/v${GH_VERSION}/gh_${GH_VERSION}_linux_amd64.tar.gz" \
      >>"$LOGS/gh-download.log" 2>&1
    mkdir -p "$TOOLCHAIN/gh"
    tar -xzf "$TOOLCHAIN/gh.tar.gz" -C "$TOOLCHAIN/gh" --strip-components=1
    cp "$TOOLCHAIN/gh/bin/gh" "$BIN/gh"
    chmod +x "$BIN/gh"
  fi
fi

# ---------------------------------------------------------------- VSCode CLI
if [ -x "$TOOLCHAIN/vscode/bin/code" ]; then
  log "VSCode 已存在：$("$TOOLCHAIN/vscode/bin/code" --version | head -1)"
else
  if [ "$CHECK_ONLY" = "1" ]; then log "缺少 VSCode"; else
    log "安装 VSCode（linux-x64 stable）"
    curl -fL --retry 2 -o "$TOOLCHAIN/vscode.tar.gz" \
      "https://update.code.visualstudio.com/latest/linux-x64/stable" >>"$LOGS/vscode-download.log" 2>&1
    mkdir -p "$TOOLCHAIN/vscode"
    tar -xzf "$TOOLCHAIN/vscode.tar.gz" -C "$TOOLCHAIN/vscode" --strip-components=1
  fi
fi
[ -x "$TOOLCHAIN/vscode/bin/code" ] && ln -sfn ../vscode/bin/code "$BIN/code"

# ---------------------------------------------------------------- tree-sitter CLI
if [ -x "/code/workspace/tree-sitter-per/node_modules/.bin/tree-sitter" ]; then
  ln -sfn /code/workspace/tree-sitter-per/node_modules/.bin/tree-sitter "$BIN/tree-sitter"
  log "tree-sitter：$(readlink -f "$BIN/tree-sitter")"
else
  log "未找到 tree-sitter CLI（在 tree-sitter-per 中 npm ci 后再运行本脚本）"
fi

# ---------------------------------------------------------------- Xvfb（可选）
# 本机无 root，只能解包到 artifacts；注意 Xvfb 硬编码 /usr/bin/xkbcomp，
# 无 root 时无法启动。VSCode e2e 请用 --ozone-platform=headless。
if [ -x "$TOOLCHAIN/xvfb/usr/bin/Xvfb" ]; then
  ln -sfn ../xvfb/usr/bin/Xvfb "$BIN/Xvfb"
  log "Xvfb 已解包（无 root 时不可用；VSCode e2e 请用 --ozone-platform=headless）"
fi

log "完成。运行： source scripts/dev-env.sh && bash scripts/env-check.sh"
