# 技术链与工具规划

> 配套文档：[未完成事项](PENDING.md)、[调研记录](RESEARCH.md)  
> 本文件记录**已验证**的版本与命令，以及后续选型的理由。所有版本在进入实现时写入锁文件（`go.mod`/`go.sum`、`pnpm-lock.yaml`、`Cargo.lock`）。
> v1 验证平台：**Linux + Windows**；macOS 在 v1 之后补齐。环境安装清单见 [ENVIRONMENT.md](ENVIRONMENT.md)。

---

## 1. 技术链总览

```text
Genero 源码 (.4gl/.global/.per)
        │
        ├─ tree-sitter-bdl  ──┐
        ├─ tree-sitter-per  ──┤  C parser（ABI 15）
        │                     ▼
        │             go-tree-sitter v0.25.0（CGO）
        │                     │
        │             bdl-lsp (Go 1.24)
        │              ├─ go.lsp.dev/protocol v0.12.0（LSP 3.16 类型）
        │              ├─ go.lsp.dev/jsonrpc2 v0.10.0（JSON-RPC 2.0）
        │              ├─ 索引 / 校验 / 格式化 / 部署
        │              └─ stdio（默认）/ TCP（调试）
        │                     │
        ├──────── VSCode 扩展（TypeScript + vscode-languageclient 10）
        └──────── Zed 扩展（Rust/WASM + zed_extension_api 0.7）
```

---

## 2. 本机环境（已确认）

| 工具 | 版本/状态 | 用途 |
|---|---|---|
| OS | Linux x86_64（Ubuntu 24.04 系） | 开发/验证 |
| Go | **1.24.4**，`CGO_ENABLED=1` | LSP 服务 |
| gcc | **13.3.0** | CGO 编译 parser |
| Node | **24.21.0** | 前端/grammar 工具 |
| npm / pnpm | **12.1.0 / 12.14.0** | VSCode 扩展 |
| tree-sitter CLI | **0.26.5**（`tree-sitter-per/node_modules/.bin/tree-sitter`） | grammar 生成/测试/解析 |
| Python | 3.12.3 | 语料/批处理脚本 |
| jq | 1.7 | 语料统计/脚本 |
| git | 2.43.0 | 版本管理 |
| make | 4.3 | 构建入口 |
| Rust / cargo | **未安装** | Zed 扩展 |
| Zed | **未安装** | Zed 端到端验证 |
| VSCode CLI | **未安装** | VSCode 端到端验证 |
| `fglform` / `fglcomp` | **未安装** | 权威编译校验（依赖远程/用户环境） |

### 2.1 已存在的 grammar 资产

| 仓库 | 关键信息 |
|---|---|
| `tree-sitter-bdl` | `LANGUAGE_VERSION 15`，`src/parser.c` 约 11.5 MB，外部 scanner `scanner.c`；corpus 121/121 |
| `tree-sitter-per` | `LANGUAGE_VERSION 15`，`src/parser.c` 约 0.8 MB，无 scanner；语料 7068/7070 |

两个仓库都已有 Go 绑定：`bindings/go/binding.go`，模块路径分别为 `github.com/darcyjoven/tree-sitter-bdl`、`github.com/darcyjoven/tree-sitter-per`。

> 注意：两个 grammar 仓库自身的 `go.mod` 仍声明 `go-tree-sitter v0.24.0`，与 ABI 15 不匹配；LSP 通过自己的 `require v0.25.0` 走 MVS 覆盖。建议 grammar 仓库同步升级依赖，并补一个真正调用 `SetLanguage`/`Parse` 的测试，避免单独引用 grammar 模块时踩 ABI 15。

---

## 3. 选型明细

### 3.1 LSP 服务端（Go）

| 依赖 | 版本 | 说明 |
|---|---|---|
| Go | 1.24.x | `go.mod` 写 `go 1.24`；不启用 Go 1.26 新特性 |
| `github.com/tree-sitter/go-tree-sitter` | **v0.25.0** | 支持 grammar ABI 15；v0.24.0 不兼容 |
| `github.com/darcyjoven/tree-sitter-bdl` | 固定 revision/tag | Go 绑定 + C parser |
| `github.com/darcyjoven/tree-sitter-per` | 固定 revision/tag | 同上 |
| `go.lsp.dev/protocol` | **v0.12.0** | LSP 3.16 类型；Go 1.17+；v1 所需能力已覆盖；v1.0.1 支持 3.17 但需 Go 1.26，暂不升级 |
| `go.lsp.dev/jsonrpc2` | **v0.10.0** | JSON-RPC 2.0 连接/流；自定义 method 分发 |
| `go.lsp.dev/uri` | v0.3.0（间接） | URI 转换 |
| `go.uber.org/zap` | v1.27.x（间接） | `go.lsp.dev/protocol` 内部依赖；业务日志仍用 `slog` |
| `github.com/fsnotify/fsnotify` | v1.10.1（候选） | 服务端文件监听兜底；客户端 watcher 优先 |
| 标准库 `log/slog` | Go 1.24 | 结构化日志 |
| 标准库 `flag` | Go 1.24 | CLI/子命令；不引入 cobra |
| 标准库 `encoding/json` | Go 1.24 | 配置/协议 |
| `github.com/google/go-cmp` | v0.7.x（测试） | 断言/快照 |

**为什么不用 `tliron/glsp`**：协议停留在 3.16、2024 年后无新版本、框架绑定强；本项目选择更轻的 `jsonrpc2` + 自定义分发，后续可平滑升级 3.17。

**为什么不用 `sourcegraph/go-lsp`**：协议覆盖旧、维护停滞。

**分发方式**：`jsonrpc2.NewStream(stdin/stdout)` + 自定义 handler。已验证 initialize 往返；不实现 `protocol.Server` 的全部 80+ 方法。

### 3.2 编辑器客户端

#### VSCode

| 依赖 | 版本 | 说明 |
|---|---|---|
| VSCode engine | `^1.91.0` | `vscode-languageclient@10.1.2` 的最低要求 |
| `vscode-languageclient` | **10.1.2** | LSP 客户端；内置 3.18 协议 |
| `@types/vscode` | 1.91.x | 与 engine 对齐，避免误用新 API |
| TypeScript | 锁定 CI 验证通过的版本（优先 7.0.x，必要时回退 5.9.x） | 类型检查 |
| esbuild | 0.28.2 | 打包扩展（单文件 `out/extension.js`） |
| `@vscode/vsce` | 4.0.0 | VSIX 打包/发布（要求 Node >= 22） |
| `@vscode/test-electron` | 最新稳定（锁定） | 端到端测试 |
| `vitest` | 最新稳定（锁定） | 单元测试 |
| `vscode-textmate` + `vscode-oniguruma` | 可选 | 离线校验 TextMate 语法 token |

**高亮策略**：TextMate grammar 是基础（打开即高亮，不依赖 LSP）；LSP 语义高亮作为增强（v1.1）。TextMate 关键字列表由脚本从 grammar 源文件提取，避免手工漂移。

**二进制管理**：查找顺序

1. 设置 `bdl.server.path`
2. 环境变量 `BDL_LSP_PATH`
3. 工作区 `.bdl-lsp/bin/bdl-lsp`
4. 扩展 `globalStorageUri` 缓存
5. 本地构建路径（开发期）；未来从 `bdl-lsp` Release 无感下载（暂不排期）

#### Zed

| 依赖 | 版本 | 说明 |
|---|---|---|
| Zed | 稳定版（Linux/Windows，CI 跟踪） | 目标编辑器 |
| Rust | stable（锁定 CI 版本） | 扩展编译 |
| target | `wasm32-wasip2` | Zed 扩展运行形态 |
| wasi-sdk | 最新（Zed 可自动下载；CI 显式安装） | grammar WASM 编译 |
| `zed_extension_api` | **0.7.0** | 扩展 API（LSP 启动、下载、平台识别） |

**关键 API**（已查文档）：

- `zed::current_platform()` → OS/架构
- `zed::download_file(url, path, type)` → 下载到扩展工作目录
- `zed::make_file_executable(path)`
- `zed::latest_github_release(repo, options)` / `github_release_by_tag_name`
- `Worktree::which()` / `read_text_file()` / `root_path()`
- `Extension::language_server_command()` 返回 `zed::Command`
- `set_language_server_installation_status()` 提示安装状态

**启动源**：开发期使用 PATH/工作区/本地构建的 LSP 二进制；未来从 `bdl-lsp` Release 无感下载（`current_platform` + `download_file` + `make_file_executable`），该实现暂不排期。v1 需在 Linux 与 Windows 验证 Zed 扩展加载与 LSP 启动。

**限制**：Zed 扩展不能注册任意命令；部署用 CLI + `.zed/tasks.json`，LSP 暴露 `workspace/executeCommand` 作为可选入口。

### 3.3 Grammar 工具链

| 工具 | 版本 | 说明 |
|---|---|---|
| tree-sitter CLI | 0.26.5 | 生成/测试/解析；ABI 15 |
| Node | 24.x | CLI 运行环境 |
| tree-sitter-bdl / per | 固定 tag/revision | 由 Go module 固定 |

**ABI 结论**：grammar 由 tree-sitter 0.26.5 生成，`LANGUAGE_VERSION=15`；Go 绑定必须 `go-tree-sitter >= v0.25.0`。若未来 grammar 升级 ABI，需要同步升级 Go 绑定并跑语料回归。

**Genero 版本**：当前 `tree-sitter-bdl` 覆盖 **2.50**（即 2.5）语法；**5.0 暂不排期**。解析/诊断通过 `bdl.generoVersion` 等配置区分；未知语法只报语法诊断。未来升级 5.0 grammar 必须回归 2.50 语料。

**本机 CLI 配置**（`~/.cache` 不可写）：

```bash
export HOME=/code/workspace/bdl-lsp/artifacts/home
mkdir -p "$HOME/.config/tree-sitter"
cat > "$HOME/.config/tree-sitter/config.json" <<'EOF'
{ "parser-directories": ["/code/workspace"] }
EOF

tree-sitter parse -q /code/workspace/tiptop-gp/tiptop/amm/per/ammr110.per
```

`tree-sitter-bdl` 的 scanner 有非 static 函数 `printfLog`，CLI 会给出 warning；不影响解析，建议后续在 grammar 仓库改为 `static`。

### 3.4 部署/远程编译

| 方案 | 阶段 | 说明 |
|---|---|---|
| 系统 `ssh` + `rsync` | **v1 主路径** | Linux 客户端默认；增量上传、dry-run |
| 系统 `ssh` + `scp` | **v1 备选/回退** | Windows 10+ 自带 OpenSSH Client；无 rsync 时逐文件上传 |
| Go `golang.org/x/crypto/ssh` + `github.com/pkg/sftp` | 远期可选 | v1 不做；只有 Windows 无 rsync/scp 的极端场景才考虑 |
| 远程编译 | v1 | 调 `fglform` / `fglcomp`，解析 `file:line: error:` 输出 |
| 本地编译 | 可选 | 若用户机器装有 Genero，可切换本地权威校验 |

**安全基线**：不存密码、默认严格 host key、显式触发、dry-run、日志脱敏、命令参数转义。

### 3.5 CI/CD

| 项 | 方案 |
|---|---|
| `bdl-lsp` CI | GitHub Actions：Linux/Windows、CGO、fmt/vet/test、语料冒烟；本地 `scripts/ci-local.sh` 备用 |
| `bdl-lsp` 发布 | **暂不排期**；未来 tag `lsp-vX.Y.Z` → Actions 构建 + Release |
| 扩展 CI | `vscode-bdl`、`zed-bdl` 各自 GitHub Actions |
| Go 检查 | `gofmt`/`gofumpt`、`go vet`、`golangci-lint`、`go test -race`、语料冒烟 |
| 构建矩阵（v1） | **Linux amd64 + Windows amd64**（`bdl-lsp` Actions）；arm64/macOS 后续 |
| 编译方式 | GitHub Actions：ubuntu + windows runner；Linux gcc / Windows MinGW-w64；macOS 后续 |
| VSCode | `pnpm install` → typecheck → test → `vsce package`；**先本地安装**，市场发布后续 |
| Zed | `cargo check --target wasm32-wasip2`（Linux/Windows）；注册表 PR 暂不排期 |
| 缓存 | Go module/build、pnpm store、Cargo target；注意 cgo 缓存命中率 |

**构建矩阵建议**：

| 目标 | 环境 | CC | v1 |
|---|---|---|---|
| linux/amd64 | Ubuntu 22.04/24.04 | gcc | ✅ 必验 |
| windows/amd64 | Windows 10/11 + MinGW-w64 | gcc (MinGW-w64) | ✅ 必验 |
| linux/arm64 | 交叉工具链/ARM runner | gcc-aarch64-linux-gnu | 后续 |
| darwin/amd64 | macOS 13 | clang | 后续 |
| darwin/arm64 | macOS 14 | clang | 后续 |

**产物命名**：

```text
bdl-lsp-linux-amd64
bdl-lsp-windows-amd64.exe
SHA256SUMS
# v1 之后：
# bdl-lsp-linux-arm64
# bdl-lsp-darwin-amd64
# bdl-lsp-darwin-arm64
```

构建参数：`CGO_ENABLED=1`、`-trimpath`、`-ldflags "-s -w -X main.version=<tag>"`。

### 3.6 配置与日志

| 项 | 方案 |
|---|---|
| 编辑器设置 | VSCode `settings.json`（`bdl.*`）；Zed `settings.json` + 初始化选项；含 `bdl.generoVersion`（默认 2.50） |
| 工作区配置 | `.bdl-lsp/config.json`、`.bdl-lsp/deploy.json`（两个编辑器共用）；`bdl.generoVersion` 可覆盖 |
| 配置校验 | 启动时校验并给出可读错误；未知字段警告 |
| 日志 | `slog`；stderr + 可选文件；简单大小轮转（不引入额外库） |
| 缓存 | `$XDG_CACHE_HOME/bdl-lsp` 或系统临时目录；不放仓库 |
| 诊断 source | `bdl-lsp`、`fglform`、`fglcomp` |

---

## 4. 本机环境坑与固定脚本

### 4.1 为什么需要自定义环境变量

沙箱内：

- `$HOME`（`/home/darcy`）下的 `~/.cache` 不可写 → tree-sitter CLI 需要可写 HOME。
- `/home/darcy/go` 是只读的 Go 模块/构建缓存 → 必须把 `GOPATH`、`GOMODCACHE`、`GOCACHE` 指到工作区。
- 默认 `proxy.golang.org` 可能走 IPv6 超时 → 使用 `GOPROXY=https://goproxy.cn,direct`。
- `GOSUMDB` 在本机临时关闭以避免只读缓存问题；**CI/生产构建必须开启**，并提交完整 `go.sum`。

### 4.2 固定环境脚本（建议写入 `scripts/dev-env.sh`）

```bash
#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export HOME="$ROOT/artifacts/home"
export GOPATH="$ROOT/artifacts/gopath"
export GOMODCACHE="$GOPATH/pkg/mod"
export GOCACHE="$ROOT/artifacts/gocache"
export GOPROXY="${GOPROXY:-https://goproxy.cn,direct}"
export GOTOOLCHAIN=local
export CGO_ENABLED=1
mkdir -p "$HOME" "$GOPATH" "$GOCACHE"
```

使用：

```bash
source scripts/dev-env.sh
go build ./...
```

### 4.3 已验证命令

> `artifacts/spike` 与 `artifacts/spike-lsp` 是临时验证程序，未提交到 Git；关键代码片段见 [RESEARCH.md](RESEARCH.md)。以下命令用于本机复现。

```bash
# 1) Go + CGO + 双 grammar 解析
cd bdl-lsp/artifacts/spike
source ../../scripts/dev-env.sh   # 脚本落地后
go build -o spike .
./spike bdl /code/workspace/tiptop-gp/tiptop/amm/4gl/ammr110.4gl
./spike per /code/workspace/tiptop-gp/tiptop/amm/per/ammr110.per

# 2) LSP initialize 握手
cd bdl-lsp/artifacts/spike-lsp
go build -o spikelsp .
printf 'Content-Length: 107\r\n\r\n%s' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"processId":null,"rootUri":null,"capabilities":{}}}' \
  | ./spikelsp
```

已验证输出：

- 解析：`root=source_file error=false`（两个样例）。
- LSP：`{"result":{"capabilities":{"textDocumentSync":2,"definitionProvider":true},...}}`。
- 冷编译约 20 s，热编译约 7 s；spike 二进制约 5.6 MB。

### 4.4 本机还缺什么

| 缺失 | 影响 | 解决 |
|---|---|---|
| Rust + wasm target | 无法编译/验证 Zed 扩展 | `rustup` + `rustup target add wasm32-wasip2` |
| wasi-sdk | grammar WASM 编译 | Zed 自动下载或 CI 安装 |
| Zed | 无法端到端加载 dev extension | 在桌面环境安装，或仅 CI/手工检查 |
| VSCode | 无法运行扩展 e2e | 安装 VSCode + `xvfb`（CI）；本机可先做单元测试 |
| `fglcomp`/`fglform` | 无法本机权威校验 | 依赖测试服务器；部署模块做 mock + 真机验证 |
| Windows 构建环境 | 无法验证 Windows 二进制 | 安装 Go + MinGW-w64，按 [ENVIRONMENT.md](ENVIRONMENT.md) 检查 |
| GitHub 远程 | 无法 push/发布 | 三个仓库 remote 已配置；push 需 SSH 凭据 |
| `gh` CLI | 可选；发布暂不排期 | 需要时安装并 `gh auth login` |

---

## 5. 备选方案与被否原因

| 方案 | 结论 | 原因 |
|---|---|---|
| Tree-sitter WASM + `wazero` | 否 | 两个 grammar 的 WASM 构建/加载链复杂，性能与内存不如 CGO；Go 生态绑定不成熟 |
| 复制 `parser.c` 进 LSP 仓库 | 否 | BDL parser 11.5 MB，双份维护、review 噪音大；Go module 已可固定 |
| `tliron/glsp` | 否 | LSP 3.16、更新停滞、框架侵入 |
| 最新 `go.lsp.dev/* v1.0.1` | 暂缓 | 要求 Go 1.26；等工具链升级后评估 |
| 完整 AST pretty-printer | 暂缓 | 风险高、diff 大；先做保守的缩进/空白/大小写 |
| `.per` 全文件格式化 | 否 | LAYOUT 是字符画，列位置有语义；只格式化小节 |
| 纯 Go SFTP（v1） | 否 | 用户已选 ssh + rsync 主、scp 备；v1 不做纯 Go 同步 |
| VSIX 内置多平台二进制 | 暂缓 | 体积大、更新不灵活；下载 + 手动路径优先 |
| 单仓库 monorepo | 否 | 用户明确要求三个独立仓库；编辑器扩展需独立发布 |

---

## 6. 版本锁定与升级策略

1. **锁文件必须提交**：`go.mod`/`go.sum`、`pnpm-lock.yaml`、`Cargo.lock`。
2. **grammar 升级流程**：grammar 仓库 tag → LSP `go.mod` 升级 → 语料回归（解析率、诊断、格式化）→ 发布。
3. **依赖升级**：Renovate/Dependabot 提案；CI 必须绿；LSP 协议/编辑器客户端大版本单独验证。
4. **兼容矩阵**：在 `README` 与发布说明中列出 `LSP vX.Y.Z ↔ VSCode vX.Y.Z ↔ Zed vX.Y.Z ↔ grammar rev`。
5. **不追最新**：`go.lsp.dev` v1.0.1、TypeScript 7 等新版本先在分支验证，不直接进主线。

---

## 7. 工具清单（按角色）

| 角色 | 工具 |
|---|---|
| 语言服务开发 | Go 1.24、gcc、tree-sitter CLI、go-tree-sitter、go.lsp.dev |
| VSCode 扩展 | Node 24、pnpm、TypeScript、esbuild、vscode-languageclient、vsce |
| Zed 扩展 | Rust stable、wasm32-wasip2、wasi-sdk、zed_extension_api |
| Grammar 维护 | tree-sitter CLI 0.26.5、Node 24、语料脚本 |
| 测试 | `go test`、`go test -race`、go-cmp、vitest、@vscode/test-electron、cargo check |
| 语料/分析 | Python 3.12、jq、grep、tree-sitter parse |
| 构建/发布 | 本地 Go 构建、`gh` CLI、`sha256sum`；扩展仓库 GitHub Actions |
| 部署验证 | OpenSSH（ssh/scp）、rsync、Genero 服务器（`fglform`/`fglcomp`） |
| 文档 | Markdown、Mermaid（可选） |
