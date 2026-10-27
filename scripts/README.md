# scripts

测试/批处理脚本统一放在这里；脚本产生的中间文件、日志写入 `../artifacts/`，不要散落在仓库根目录。

## 已提供

| 脚本 | 平台 | 用途 |
|---|---|---|
| `dev-env.sh` | Linux/macOS | 设置可写 `HOME`/`GOPATH`/`GOCACHE`/`CARGO_HOME`/`WASI_SDK_PATH`/`PATH` |
| `dev-env.ps1` | Windows | 设置 `CGO_ENABLED`/`CC`/`CARGO_HOME`/`WASI_SDK_PATH`/`PATH` |
| `env-check.sh` | Linux/macOS | 检查工具链（见仓库 [README](../README.md) 构建一节），缺失项退出码 1 |
| `env-check.ps1` | Windows | 同上 |
| `install-toolchain.sh` | Linux（无 root） | 安装 Rust/wasm target/wasi-sdk/gh/VSCode 到 `artifacts/toolchain/`；`--check` 只检查 |
| `check-sh-utf8-vars.mjs` | 任意（Node） | 检查 shell 脚本里「变量紧跟非 ASCII 字节」（`$VAR（` 要写 `${VAR}（`）；可传多个目录，默认扫本仓库 |

## 规划中

| 脚本 | 用途 |
|---|---|
| `build.sh` | 构建 `bdl-lsp`（本机平台） |
| `ci-local.sh` | `bdl-lsp` 本地 CI：fmt/vet/test/语料冒烟 |
| `corpus-check.sh` | 运行 `.4gl` / `.per` 全量解析基线 |
| `field-check-corpus.sh` | 字段校验标注集评估 |
| `format-corpus.sh` | 格式化幂等/语义保持评估 |
| `deploy-check.sh` | 部署 dry-run / rsync / scp 回退验证（需测试服务器） |
| `zed-check.sh` | Zed 扩展 `cargo check`（wasm32-wasip2） |

## 快速开始

```bash
cd bdl-lsp

# 1) 安装缺失工具链（普通开发机已有系统包时可跳过）
bash scripts/install-toolchain.sh

# 2) 加载环境（必须 source）
source scripts/dev-env.sh

# 3) 检查
bash scripts/env-check.sh
```

Windows：

```powershell
cd bdl-lsp
. .\scripts\dev-env.ps1
powershell -ExecutionPolicy Bypass -File scripts\env-check.ps1
```

## 约定

- Bash 脚本使用 `set -euo pipefail`（`dev-env.sh` 例外：它是 `source` 用的，不改动调用者的 shell 选项）。
- PowerShell 脚本使用 `$ErrorActionPreference = "Stop"`。
- 路径以脚本所在目录为基准，不依赖调用者 cwd。
- 不提交任何 `artifacts/` 下的输出；验证日志写到 `artifacts/logs/`。
- 需要网络/账号的步骤（下载、发布）显式提示并支持 dry-run。
- 发布（GitHub Release / 市场 / 注册表）**暂不排期**；`release.sh` 等脚本在排期后再加。
- Windows 优先保证 `scp` 路径；`rsync` 作为可选增强，不阻塞 v1。
- 无桌面 Linux 的 VSCode e2e 用 `--ozone-platform=headless`；Xvfb 需 root 安装。
- 变量紧跟非 ASCII 字节时必须写 `${VAR}`（如 `"连接 ${TARGET}（远端只读）"`）：bash 用 libc 的
  `isalnum()`（随 locale 的 ctype 表变）找变量名结尾，macOS 上 `$TARGET（` 的 `EF` 会被吞进变量名 →
  `set -u` 报 `unbound variable`（不带 `-u` 则静默丢字符）。用 `node scripts/check-sh-utf8-vars.mjs .` 检查。
- 只在 Linux/WSL 跑不写 `mapfile`/`readarray`（bash 4.0+）；要支持 macOS 自带 bash 3.2 时用
  `while IFS= read -r x; do arr+=("$x"); done < <(...)`，空数组在 `set -u` 下展开写 `${arr[@]+"${arr[@]}"}`。
