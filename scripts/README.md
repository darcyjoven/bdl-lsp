# scripts

测试/批处理脚本统一放在这里；脚本产生的中间文件、日志写入 `../artifacts/`，不要散落在仓库根目录。

规划中的脚本：

| 脚本 | 用途 |
|---|---|
| `dev-env.sh` | Linux/沙箱：设置可写 `HOME`/`GOPATH`/`GOMODCACHE`/`GOCACHE`/`GOPROXY` |
| `dev-env.ps1` | Windows：设置 `CGO_ENABLED`/`CC`/`PATH` 等 |
| `env-check.sh` / `env-check.ps1` | 按 ENVIRONMENT.md 清单检查工具链 |
| `ci-local.sh` | `bdl-lsp` 本地 CI：fmt/vet/test/语料冒烟 |
| `build.sh` | 构建 `bdl-lsp`（本机平台） |
| `corpus-check.sh` | 运行 `.4gl` / `.per` 全量解析基线 |
| `field-check-corpus.sh` | 字段校验标注集评估 |
| `format-corpus.sh` | 格式化幂等/语义保持评估 |
| `deploy-check.sh` | 部署 dry-run / rsync / scp 回退验证（需测试服务器） |
| `release.sh` | 本地发布备用：Linux/Windows 构建、校验和、上传 `bdl-lsp` Release |
| `zed-check.sh` | Zed 扩展 `cargo check`（wasm32-wasip2） |

约定：

- Bash 脚本使用 `set -euo pipefail`；PowerShell 脚本使用 `$ErrorActionPreference = "Stop"`。
- 路径以脚本所在目录为基准，不依赖调用者 cwd。
- 不提交任何 `artifacts/` 下的输出。
- 需要网络/账号的步骤（下载、发布）显式提示并支持 dry-run。
- `bdl-lsp` 有独立远程仓库：CI 在 tag `lsp-vX.Y.Z` 构建并发布资产；本地 `release.sh` 作为备用。
- Windows 优先保证 `scp` 路径；`rsync` 作为可选增强，不阻塞 v1。
