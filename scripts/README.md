# scripts

测试/批处理脚本统一放在这里；脚本产生的中间文件、日志写入 `../artifacts/`，不要散落在仓库根目录。

规划中的脚本：

| 脚本 | 用途 |
|---|---|
| `dev-env.sh` | 设置本机可写的 `HOME`/`GOPATH`/`GOMODCACHE`/`GOCACHE`/`GOPROXY` |
| `build.sh` | 构建 `bdl-lsp`（本机平台） |
| `corpus-check.sh` | 运行 `.4gl` / `.per` 全量解析基线 |
| `field-check-corpus.sh` | 字段校验标注集评估 |
| `format-corpus.sh` | 格式化幂等/语义保持评估 |
| `release.sh` | 多平台构建、校验和、GitHub Release |
| `zed-check.sh` | Zed 扩展 `cargo check`（wasm32-wasip2） |

约定：

- Bash 脚本使用 `set -euo pipefail`。
- 路径以脚本所在目录为基准，不依赖调用者 cwd。
- 不提交任何 `artifacts/` 下的输出。
- 需要网络/账号的步骤（下载、发布）显式提示并支持 dry-run。
