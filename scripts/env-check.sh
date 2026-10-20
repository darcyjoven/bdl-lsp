#!/usr/bin/env bash
# 环境检查（Linux/macOS）。用法：
#   bash scripts/env-check.sh
# 返回 0 = 必需项齐全；1 = 有必需项缺失。
set -uo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

# 本机沙箱：有 toolchain 时自动加载，普通开发机可忽略
if [ -f "$ROOT/scripts/dev-env.sh" ] && [ -d "$ROOT/artifacts/toolchain" ]; then
  # shellcheck disable=SC1091
  source "$ROOT/scripts/dev-env.sh" >/dev/null 2>&1 || true
fi

required_missing=0
optional_missing=0

ok()   { printf '  [ OK ] %s\n' "$*"; }
warn() { printf '  [WARN] %s\n' "$*"; optional_missing=$((optional_missing + 1)); }
bad()  { printf '  [FAIL] %s\n' "$*"; required_missing=$((required_missing + 1)); }

has() { command -v "$1" >/dev/null 2>&1; }

echo "== BDL LSP 环境检查 =="
echo "PWD=$PWD"
echo

echo "-- 基础工具"
if has go; then
  if go version | grep -qE 'go1\.(2[4-9]|[3-9][0-9])'; then ok "$(go version)"; else bad "Go 版本过低：$(go version)，需要 1.24+"; fi
else bad "缺少 go（需要 1.24+）"; fi
if has gcc; then ok "$(gcc --version | head -1)"; else bad "缺少 gcc（CGO 需要）"; fi
if has git; then ok "$(git --version)"; else bad "缺少 git"; fi
if has make; then ok "make $(make --version | head -1 | awk '{print $3}')"; else warn "缺少 make（可选）"; fi

echo
echo "-- Go / CGO"
if has go; then
  cgo="$(go env CGO_ENABLED 2>/dev/null || true)"
  if [ "$cgo" = "1" ]; then ok "CGO_ENABLED=1"; else bad "CGO_ENABLED=$cgo（需要 1）"; fi
  printf '  GOMODCACHE=%s\n' "$(go env GOMODCACHE 2>/dev/null || true)"
fi

echo
echo "-- Node / 前端"
if has node; then
  major="$(node --version | sed 's/^v//' | cut -d. -f1)"
  if [ "${major:-0}" -ge 24 ] 2>/dev/null; then ok "$(node --version)"; else warn "Node 版本较低：$(node --version)，建议 24+"; fi
else bad "缺少 node（需要 24+）"; fi
if has pnpm; then ok "pnpm $(pnpm --version)"; else bad "缺少 pnpm"; fi
if has npm; then ok "npm $(npm --version)"; else warn "缺少 npm（可选）"; fi

echo
echo "-- Tree-sitter"
if has tree-sitter; then
  ts_ver="$(tree-sitter --version 2>&1 | head -1)"
  if echo "$ts_ver" | grep -q '0\.26'; then ok "$ts_ver"; else warn "tree-sitter 版本不是 0.26.x：$ts_ver"; fi
else bad "缺少 tree-sitter CLI（期望 0.26.5）"; fi

echo
echo "-- Rust / WASM"
if has rustc; then ok "$(rustc --version)"; else bad "缺少 rustc（Zed 扩展需要）"; fi
if has cargo; then ok "$(cargo --version)"; else bad "缺少 cargo"; fi
if has rustup; then
  if rustup target list --installed 2>/dev/null | grep -qx 'wasm32-wasip2'; then ok "target wasm32-wasip2 已安装"; else bad "缺少 rust target wasm32-wasip2"; fi
else bad "缺少 rustup（无法检查/安装 wasm target）"; fi
if has clang; then
  if clang --version 2>&1 | grep -q 'wasi-sdk'; then ok "$(clang --version | head -1)"; else warn "clang 不是 wasi-sdk：$(clang --version | head -1)"; fi
else bad "缺少 wasi-sdk clang（设置 WASI_SDK_PATH）"; fi

echo
echo "-- 编辑器 / 部署"
if has code; then ok "VSCode CLI $(code --version 2>/dev/null | head -1)"; else warn "缺少 code CLI（VSCode 扩展开发/e2e 需要）"; fi
if has ssh; then ok "$(ssh -V 2>&1)"; else bad "缺少 ssh"; fi
if has scp; then ok "scp 可用"; else bad "缺少 scp"; fi
if has rsync; then ok "$(rsync --version | head -1)"; else warn "缺少 rsync（Windows 可用 scp 回退）"; fi
if has gh; then ok "$(gh --version | head -1)"; else warn "缺少 gh（发布暂不排期，可选）"; fi

echo
echo "-- 可选：Genero 编译器（通常只在服务器）"
if has fglform; then ok "$(fglform -V 2>&1 | head -1)"; else warn "未安装 fglform（远程编译/服务器验证时需要）"; fi
if has fglcomp; then ok "$(fglcomp -V 2>&1 | head -1)"; else warn "未安装 fglcomp（远程编译/服务器验证时需要）"; fi

echo
echo "-- 可选：Xvfb（无桌面 Linux 的 VSCode e2e）"
if has Xvfb; then
  if [ -x /usr/bin/xkbcomp ]; then ok "Xvfb 可用"; else warn "Xvfb 已安装但缺 /usr/bin/xkbcomp（需系统安装 x11-xkb-utils；或有桌面环境时可忽略）"; fi
else warn "未安装 Xvfb（有桌面环境/CI 时可忽略）"; fi

echo
if [ "$required_missing" -eq 0 ]; then
  echo "结果：必需项齐全（可选缺失 ${optional_missing} 项）"
  exit 0
else
  echo "结果：缺少 ${required_missing} 个必需项（可选缺失 ${optional_missing} 项）"
  exit 1
fi
