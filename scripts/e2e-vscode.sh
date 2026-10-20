#!/usr/bin/env bash
# VSCode 扩展端到端测试。
#
# 做三件事：
#   1. 确保语言服务器二进制是最新构建（客户端要真实启动它）
#   2. 类型检查 + 打包产物（e2e 跑的是 dist，不是 src）
#   3. 在真实 VSCode 扩展宿主里跑测试套件
#
# 用法：
#   bash scripts/e2e-vscode.sh            # 完整流程
#   bash scripts/e2e-vscode.sh --keep     # 失败时保留日志与工作区
#   BDL_E2E_KEEP=1 bash scripts/e2e-vscode.sh
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BDL_LSP_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
EXT_DIR="$(cd "${BDL_LSP_DIR}/../vscode-extension" && pwd)"

KEEP="${BDL_E2E_KEEP:-}"
for arg in "$@"; do
  case "${arg}" in
    --keep) KEEP=1 ;;
    -h|--help)
      sed -n '2,12p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'
      exit 0
      ;;
    *)
      echo "未知参数：${arg}" >&2
      exit 2
      ;;
  esac
done

# shellcheck source=/dev/null
source "${BDL_LSP_DIR}/scripts/dev-env.sh"

echo "==> 1/4 构建语言服务器"
make -C "${BDL_LSP_DIR}" build

LSP_BIN="${BDL_LSP_DIR}/artifacts/bin/bdl-lsp"
if [ ! -x "${LSP_BIN}" ]; then
  echo "未找到 ${LSP_BIN}" >&2
  exit 1
fi
echo "    语言服务器：${LSP_BIN}"

echo "==> 2/4 安装扩展依赖（如缺失）"
cd "${EXT_DIR}"
if [ ! -d node_modules ]; then
  # NODE_ENV=production 会让 npm 跳过 devDependencies，这里显式包含
  NODE_ENV= npm install --no-audit --no-fund --include=dev
fi

echo "==> 3/4 类型检查与打包"
npm run typecheck
npm run build

echo "==> 4/4 运行端到端测试"
export BDL_E2E_BASE="${BDL_E2E_BASE:-${BDL_LSP_DIR}/artifacts/e2e}"
export BDL_LSP_PATH="${LSP_BIN}"
if [ -n "${KEEP}" ]; then
  export BDL_E2E_KEEP=1
fi

node test/runTest.js
