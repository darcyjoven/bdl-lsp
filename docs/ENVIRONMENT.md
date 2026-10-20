# 环境配置

> 目标：在 **Linux + Windows** 上完成 v1 开发与验证；macOS 在 v1 之后补齐。  
> 本文只列“需要装什么、怎么验证、卡住怎么查”，不包含功能代码。  
> 配套：[粗略计划](PLAN.md)、[技术链](TECH-STACK.md)、[待办](TODO.md)。

---

## 1. 平台与组件矩阵

| 组件 | Linux | Windows | 用途 | v1 必需 |
|---|---|---|---|---|
| Go 1.24+ | ✅ | ✅ | LSP 服务 | 是 |
| C 编译器（gcc） | gcc | MinGW-w64 gcc | CGO 编译 Tree-sitter | 是 |
| Git | ✅ | Git for Windows | 仓库/工具 | 是 |
| Node 24 + pnpm | ✅ | ✅ | VSCode 扩展、grammar CLI | 是 |
| tree-sitter CLI 0.26.5 | ✅ | ✅ | grammar 生成/测试 | 是 |
| VSCode + CLI | ✅ | ✅ | 扩展开发/验证 | 是 |
| OpenSSH（ssh/scp） | ✅ | Windows 10+ 可选功能 | 部署/编译 | 是 |
| rsync | ✅ | 可选（cwRsync/MSYS2/WSL） | 部署主路径 | Linux 是；Windows 否（用 scp） |
| Rust + wasm32-wasip2 | ✅ | ✅ | Zed 扩展 | 是 |
| wasi-sdk | ✅ | ✅ | Zed grammar WASM | 是 |
| Zed | ✅ | ✅（官方 Windows 版） | Zed 端验证 | 是 |
| `gh` CLI | ✅ | ✅ | 发布 LSP 二进制 | 是 |
| `fglform` / `fglcomp` | 服务器 | 服务器 | 权威编译 | 否（远程） |

---

## 2. Linux 环境

### 2.1 基础工具

```bash
sudo apt-get update
sudo apt-get install -y \
  build-essential git curl jq make python3 \
  openssh-client rsync ca-certificates
```

验证：

```bash
gcc --version
git --version
make --version
ssh -V
rsync --version | head -1
```

### 2.2 Go

要求 **Go 1.24+**（本机已验证 1.24.4）。

```bash
go version
go env GOVERSION CGO_ENABLED
```

`CGO_ENABLED=1` 必须是 1（`go env -w CGO_ENABLED=1` 可选）。

### 2.3 Node / pnpm

要求 Node 24+（`@vscode/vsce` 要求 Node >= 22）。

```bash
node --version
corepack enable
corepack prepare pnpm@latest --activate
pnpm --version
```

### 2.4 tree-sitter CLI

优先使用 grammar 仓库锁定的 CLI，不要全局随意升级：

```bash
cd /code/workspace/tree-sitter-per
npm ci                      # 正常环境；本沙箱可能被 allowScripts 拦截
./node_modules/.bin/tree-sitter --version   # 期望 0.26.5
```

如果 CLI 二进制需要手工下载：从 `tree-sitter/tree-sitter` GitHub Release 取对应平台的 `tree-sitter-<os>-<arch>`，放到 `node_modules/.bin/` 并 `chmod +x`。

### 2.5 Rust / WASM（Zed 扩展）

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source "$HOME/.cargo/env"
rustup target add wasm32-wasip2
rustc --version
cargo --version
```

wasi-sdk：从 `WebAssembly/wasi-sdk` Release 下载，设置：

```bash
export WASI_SDK_PATH=/opt/wasi-sdk
$WASI_SDK_PATH/bin/clang --version
```

Zed 可以自动下载 wasi-sdk；CI 或离线环境需显式准备。

### 2.6 VSCode / 测试

```bash
code --version
# CI/无桌面 Linux 运行扩展测试需要：
sudo apt-get install -y xvfb
```

### 2.7 `gh` CLI

```bash
gh --version
gh auth status
```

用途：维护/排查 `bdl-lsp` 的 GitHub Release（CI 为主；本地 `gh` 作为备用）。

### 2.8 本沙箱的特殊处理

本机 `$HOME` 与 `/home/darcy/go` 不可写，使用：

```bash
cd /code/workspace/bdl-lsp
source scripts/dev-env.sh
```

脚本会把 `HOME`、`GOPATH`、`GOMODCACHE`、`GOCACHE` 指到 `bdl-lsp/artifacts/`，并设置 `GOPROXY=https://goproxy.cn,direct`。  
这是**本沙箱专用**；普通 Linux 开发机不需要覆盖 `HOME`。

---

## 3. Windows 环境

> Windows 是 v1 必验平台，重点验证 **VSCode/Zed 扩展 + CGO 构建 + scp 部署回退**。  
> Zed 已有官方 Windows 版本；仍需实测扩展加载、LSP 进程启动与路径差异。

### 3.1 Go + MinGW-w64

1. 安装 Go 1.24+（官方 MSI）。
2. 安装 MinGW-w64（MSYS2 或 winlibs 均可）：
   - MSYS2：`pacman -S mingw-w64-x86_64-gcc`
   - 把 `C:\msys64\mingw64\bin` 加入 `PATH`。
3. 验证：

```powershell
go version
go env CGO_ENABLED
gcc --version
where.exe gcc
```

4. 构建时确保：

```powershell
$env:CGO_ENABLED = "1"
$env:CC = "gcc"
go build ./...
```

如果 `gcc` 与 Go 位数不一致（32/64 位），CGO 会失败；统一用 x86_64。

### 3.2 Node / pnpm / Git

```powershell
node --version          # 24.x
corepack enable
corepack prepare pnpm@latest --activate
pnpm --version
git --version
```

Git for Windows 自带 OpenSSH 与 `scp`；建议：

```powershell
git config --global core.autocrlf false
```

`.4gl` / `.per` 必须保持 LF，避免远程编译/版本库出现整文件 diff。

### 3.3 OpenSSH：ssh / scp

Windows 10/11 自带可选功能 “OpenSSH Client”：

```powershell
ssh -V
scp -V
```

若未安装：设置 → 应用 → 可选功能 → 添加 “OpenSSH 客户端”。

### 3.4 rsync（可选）

Windows 默认没有 rsync。可选方案：

- cwRsync（独立安装，注意许可证）
- MSYS2：`pacman -S rsync`
- WSL：在 WSL 内运行部署

v1 的 Windows 验收走 **scp 回退**；rsync 作为增强，不阻塞。

### 3.5 VSCode

```powershell
code --version
```

安装后可用 `code --extensionDevelopmentPath` 调试扩展。

### 3.6 Rust / WASM

```powershell
winget install Rustlang.Rustup
rustup target add wasm32-wasip2
rustc --version
cargo --version
```

wasi-sdk 下载后设置 `WASI_SDK_PATH`。

### 3.7 Zed（Windows 官方版）

安装 Zed Windows 版（下载页或 `winget install -e --id ZedIndustries.Zed`），验证：

```powershell
zed --version
```

v1 需在 Windows 上实测 dev extension 加载、LSP 进程启动、路径/终端差异；若遇到上游问题，记录并降级为“Windows 以 VSCode 验收，Zed 在 Linux 验收”，但不默认跳过。

### 3.8 `gh` CLI

```powershell
winget install GitHub.cli
gh auth status
```

### 3.9 Windows 路径与部署映射

- 本地路径：`C:\work\app\...`
- 远程路径：`/u1/app/...`
- 配置中用 `localRoot` / `remoteRoot` 映射，禁止把盘符直接拼进远程命令。
- 上传前把路径分隔符统一为 `/`；检查文件名大小写（Linux 区分大小写）。
- 行尾：`.4gl` / `.per` 使用 LF；CRLF 会在上传/编译前告警。

---

## 4. 账号、凭据与权限

| 用途 | 需要 | 存放 |
|---|---|---|
| push `bdl-lsp` / `vscode-bdl` / `zed-bdl` | GitHub SSH key | `~/.ssh/`，不提交 |
| 上传 LSP Release | `gh` 登录、仓库 Release 权限 | `gh auth`，不写进仓库 |
| 部署到测试服务器 | SSH key/agent、known_hosts | 用户环境，不写进仓库 |
| `fglform`/`fglcomp` | 服务器账号/环境 | 服务器侧，不写进仓库 |

**禁止**：把密码、私钥、token、生产主机写进仓库或配置样例。部署配置只允许主机/路径/命令等非秘密信息，密码走 SSH agent。

---

## 5. 环境验证清单

在新机器上按顺序执行，全部通过才算环境就绪。

### 5.1 公共

- [ ] `git --version`
- [ ] `go version` ≥ 1.24
- [ ] `go env CGO_ENABLED` = 1
- [ ] `gcc --version`（Windows 为 MinGW-w64）
- [ ] `node --version` ≥ 24
- [ ] `pnpm --version`
- [ ] `tree-sitter --version` = 0.26.5（或 grammar 仓库内的 CLI）
- [ ] `ssh -V`
- [ ] `scp -V`
- [ ] `gh --version` + `gh auth status`
- [ ] `rustc --version` + `cargo --version`
- [ ] `rustup target list --installed` 包含 `wasm32-wasip2`
- [ ] `WASI_SDK_PATH/bin/clang --version`
- [ ] `code --version`（VSCode）
- [ ] Zed 可用（Linux + Windows 官方版）

### 5.2 Linux 额外

- [ ] `rsync --version`
- [ ] `xvfb-run --help`（CI/e2e 需要）
- [ ] `source bdl-lsp/scripts/dev-env.sh` 后 `go env GOMODCACHE` 指向可写目录

### 5.3 Windows 额外

- [ ] `where.exe gcc` 指向 MinGW-w64
- [ ] `$env:CGO_ENABLED=1; $env:CC="gcc"; go build` 能编译最小 CGO 程序
- [ ] `scp` 能连接测试服务器（或 dry-run）
- [ ] `git config --global core.autocrlf false`
- [ ] 仓库工作区没有 CRLF 的 `.4gl`/`.per`

### 5.4 项目级冒烟

- [ ] 两个 grammar 能解析样例（`tree-sitter parse -q`）
- [ ] Go + CGO 双 grammar 的 spike 能运行（见 RESEARCH.md）
- [ ] `cargo check --target wasm32-wasip2`（Zed 骨架就绪后）
- [ ] VSCode 扩展能激活（P1 后）
- [ ] 记录目标 Genero 版本（当前 2.50 基线，规划 2.50–5.0）

---

## 6. 环境相关待办

- [ ] 写 `scripts/dev-env.ps1`（Windows 对应 `dev-env.sh`）。
- [ ] 写 `scripts/env-check.sh` 与 `scripts/env-check.ps1`，把第 5 节清单自动化。
- [ ] 在 `scripts/README.md` 补充 Windows、rsync、scp、gh 的说明。
- [ ] 安装并验证 Linux 的 Rust/wasm/wasi-sdk、VSCode CLI、xvfb、gh。
- [ ] 准备 Windows 机器/虚拟机，安装 Go + MinGW-w64 + Node + Git + VSCode。
- [ ] 验证 Windows 下 `scp` 到测试服务器；记录命令与日志。
- [ ] 配置 GitHub SSH key，完成 `bdl-lsp` / `vscode-bdl` / `zed-bdl` 首次 push。
- [ ] `gh auth login`，验证对 `bdl-lsp` 有 Release/Actions 权限。
- [ ] 准备测试服务器（Genero、`fglform`/`fglcomp`、SSH），记录路径映射样例。
- [ ] 把环境验证结果记录到 `artifacts/env/`（日志/版本输出），不提交。

---

## 7. CI 映射

| 环境 | 用途 | 运行位置 |
|---|---|---|
| `bdl-lsp` 单测/语料/构建 | LSP | GitHub Actions（Linux/Windows）；本地脚本备用 |
| `vscode-bdl` CI | 扩展 typecheck/test/package | GitHub Actions（Linux + Windows） |
| `zed-bdl` CI | `cargo check --target wasm32-wasip2` | GitHub Actions（Linux） |
| LSP Release 构建 | Linux/Windows 二进制 | `bdl-lsp` GitHub Actions（tag `lsp-vX.Y.Z`） |
| 语料回归 | 7699 `.4gl` / 7409 `.per` | 本地（慢测试，按需） |

---

## 8. 常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| `Incompatible language version 15` | go-tree-sitter 太旧 | 固定 `v0.25.0`；检查 `go.mod` |
| `gcc: command not found`（Windows） | MinGW 不在 PATH | 加 `C:\msys64\mingw64\bin`；`where gcc` |
| `CGO_ENABLED=0` 导致 parser 不编译 | 环境变量/交叉编译 | 设置 `CGO_ENABLED=1`；不要用纯 Go 构建 |
| `go: writing stat cache: permission denied` | GOPATH 不可写（本沙箱） | `source scripts/dev-env.sh` |
| `proxy.golang.org ... i/o timeout` | IPv6/网络 | `GOPROXY=https://goproxy.cn,direct` |
| `rsync: command not found`（Windows） | 未安装 rsync | 用 scp 回退；或装 cwRsync/MSYS2 |
| `Host key verification failed` | known_hosts 未配置 | 人工确认主机指纹；不要把 StrictHostKeyChecking 设为 no |
| `vsce` 报 Node 版本过低 | Node < 22 | 升级 Node 24 |
| `cargo` 报 target 缺失 | 未装 wasm32-wasip2 | `rustup target add wasm32-wasip2` |
| Zed grammar 编译失败 | wasi-sdk 未配置 | 设置 `WASI_SDK_PATH`，查看 Zed 日志 |
| `gh: not logged in` | 未认证 | `gh auth login`；检查仓库权限 |

---

## 9. 参考

- [技术链与工具](TECH-STACK.md)
- [调研记录](RESEARCH.md)
- [待办](TODO.md)
- Zed 扩展文档：<https://zed.dev/docs/extensions/languages>
- Tree-sitter：<https://tree-sitter.github.io/tree-sitter/>
