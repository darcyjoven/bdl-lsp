# 环境配置

> 目标：在 **Linux + Windows** 上完成 v1 开发与验证；macOS 在 v1 之后补齐。  
> 本文只列“需要装什么、怎么验证、卡住怎么查”，不包含功能代码。  
> 配套：[粗略计划](PLAN.md)、[技术链](TECH-STACK.md)、[待办](TODO.md)。  
> **本机（沙箱）已安装的组件与验证结果见第 9 节。**

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
| `gh` CLI | ✅ | ✅ | 可选（发布暂不排期） | 否 |
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

普通开发机（`$HOME` 可写）：

```bash
curl --proto '=https' --tlsv1.2 -sSf https://sh.rustup.rs | sh
source "$HOME/.cargo/env"
rustup target add wasm32-wasip2
rustc --version
cargo --version
```

无 root/沙箱（`$HOME` 不可写）：用脚本安装到 `artifacts/toolchain/`：

```bash
cd bdl-lsp
bash scripts/install-toolchain.sh
source scripts/dev-env.sh     # 设置 RUSTUP_HOME/CARGO_HOME/WASI_SDK_PATH/PATH
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
```

无桌面 Linux 运行扩展 e2e 的两种方式：

1. **headless ozone（推荐，无需 root）**：
   ```bash
   code --ozone-platform=headless --disable-gpu --no-sandbox --version
   ```
   本机已验证可用（VSCode 1.142.0）。
2. **Xvfb**（需要 root 安装 `xvfb x11-xkb-utils xkb-data`）：
   ```bash
   sudo apt-get install -y xvfb x11-xkb-utils xkb-data
   xvfb-run -a code --version
   ```
   注意：Xvfb 硬编码调用 `/usr/bin/xkbcomp`，即使用 `PATH`/`XKB_BINDIR` 指向别处也无法绕过，无 root 时不可用。

### 2.7 `gh` CLI

```bash
gh --version
gh auth status
```

用途：可选；发布暂不排期。`gh` 仅用于未来 Release 或排查。

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
| 上传 LSP Release | （暂不排期）`gh` 登录、仓库 Release 权限 | 需要时配置 |
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
- [ ] 无桌面时 `code --ozone-platform=headless --version` 可用（推荐）；或 `xvfb-run`（需 root 装 xvfb）
- [ ] `source bdl-lsp/scripts/dev-env.sh` 后 `go env GOMODCACHE` 指向可写目录

### 5.3 Windows 额外

- [ ] `where.exe gcc` 指向 MinGW-w64
- [ ] `$env:CGO_ENABLED=1; $env:CC="gcc"; go build` 能编译最小 CGO 程序
- [ ] `scp` 能连接测试服务器（或 dry-run）
- [ ] `git config --global core.autocrlf false`
- [ ] 仓库工作区没有 CRLF 的 `.4gl`/`.per`

### 5.4 项目级冒烟

- [x] 两个 grammar 能解析样例（`tree-sitter parse -q`）
- [x] Go + CGO 双 grammar 的 spike 能运行（见第 9 节）
- [x] `cargo check --target wasm32-wasip2` + `zed_extension_api 0.7.0` 编译通过
- [ ] VSCode 扩展能激活（P1 后）
- [x] 记录目标 Genero 版本（当前 2.50 基线；5.0 暂不排期）

---

## 6. 环境相关待办

- [x] `scripts/dev-env.sh`（Linux/沙箱）。
- [x] `scripts/dev-env.ps1`（Windows）。
- [x] `scripts/env-check.sh` 与 `scripts/env-check.ps1`。
- [x] `scripts/install-toolchain.sh`（无 root 安装 Rust/wasi-sdk/gh/VSCode）。
- [ ] 在 `scripts/README.md` 补充 Windows、rsync、scp、gh 的说明。
- [x] 安装并验证 Linux 的 Rust/wasm/wasi-sdk、VSCode CLI、gh。
- [ ] Linux Xvfb：需 root 安装 `xvfb x11-xkb-utils xkb-data`；当前用 headless 替代。
- [ ] 准备 Windows 机器/虚拟机，安装 Go + MinGW-w64 + Node + Git + VSCode + Zed。
- [ ] Windows 上运行 `scripts\env-check.ps1` 并记录结果。
- [ ] 验证 Windows 下 `scp` 到测试服务器；记录命令与日志。
- [x] 配置 GitHub SSH key，完成三个仓库首次 push。
- [ ] `gh` 可选；发布暂不排期，需要时再登录。
- [ ] 准备测试服务器（Genero、`fglform`/`fglcomp`、SSH），记录路径映射样例。
- [ ] 把环境验证结果记录到 `artifacts/logs/`（已有 `env-check.log`），不提交。

---

## 7. CI 映射

| 环境 | 用途 | 运行位置 |
|---|---|---|
| `bdl-lsp` 单测/语料/构建 | LSP | GitHub Actions（Linux/Windows）；本地脚本备用 |
| `vscode-bdl` CI | 扩展 typecheck/test/package | GitHub Actions（Linux + Windows） |
| `zed-bdl` CI | `cargo check --target wasm32-wasip2` | GitHub Actions（Linux + Windows） |
| LSP 本地构建 | Linux/Windows 二进制 | 本地/CI；Release 暂不排期 |
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
| Xvfb `Failed to activate virtual core keyboard` | 缺 `/usr/bin/xkbcomp`，且无 root | 用 `code --ozone-platform=headless`；或 root 安装 xvfb/x11-xkb-utils |
| `rustup` 报 `could not create home directory` | `$HOME` 只读 | `source scripts/dev-env.sh`（设置 RUSTUP_HOME/CARGO_HOME） |
| wasi clang 报 `cannot open crt1.o` | 未指定 sysroot | `--sysroot=$WASI_SDK_PATH/share/wasi-sysroot` |

---

## 9. 本机（沙箱）安装记录

> 环境：Ubuntu 24.04、无 root、`$HOME` 只读、8 核 / 15 GB。
> 工具链统一放在 `bdl-lsp/artifacts/toolchain/`（Git 忽略），由 `scripts/install-toolchain.sh` 安装。

| 组件 | 版本 | 位置 | 验证 |
|---|---|---|---|
| Go | 1.24.4（系统） | `/usr/local/go` | `go version`；CGO 构建 spike 通过 |
| gcc | 13.3.0（系统） | `/usr/bin/gcc` | CGO 编译通过 |
| Node / npm / pnpm | 24.21.0 / 12.1.0 / 12.14.0 | 系统 + corepack | `node -v` 等 |
| tree-sitter CLI | 0.26.5 | 链接到 `toolshed` 的 `tree-sitter-per/node_modules/.bin` | 解析 `.4gl`/`.per` 通过 |
| Rust | 1.99.1 | `artifacts/toolchain/cargo` | `rustc --version` |
| wasm32-wasip2 | 已安装 | `artifacts/toolchain/rustup` | `rustup target list --installed` |
| wasi-sdk | 34（clang 23.1.0） | `artifacts/toolchain/wasi-sdk` | 编译 wasm 成功（带 sysroot） |
| VSCode CLI | 1.142.0 | `artifacts/toolchain/vscode` | `code --version`；headless 模式可用 |
| gh CLI | 2.102.0 | `artifacts/toolchain/bin/gh` | `gh --version` |
| OpenSSH / rsync | 9.6p1 / 3.2.7 | 系统 | `ssh -V`、`rsync --version` |
| Xvfb | 21.1.12（解包） | `artifacts/toolchain/xvfb` | **不可用**：缺 `/usr/bin/xkbcomp` 且无 root |
| `fglform` / `fglcomp` | — | — | 未安装（依赖远程服务器） |
| Zed | — | — | 未安装（无图形界面；Windows 端后续验证） |

### 已验证的项目级冒烟

```bash
source scripts/dev-env.sh
bash scripts/env-check.sh          # 必需项齐全

# Go + CGO + 双 grammar
cd artifacts/spike && go build -o spike2 . && ./spike2 bdl <file>.4gl && ./spike2 per <file>.per

# Zed 扩展 API + wasm 目标
cd artifacts/spike-rust && cargo check --target wasm32-wasip2
cargo build --target wasm32-wasip2 --release   # 产出 .wasm

# tree-sitter
tree-sitter parse -q <file>.4gl && tree-sitter parse -q <file>.per
```

### 本机限制（需在 Windows/桌面环境补）

1. **Zed 未安装**：无图形界面，无法验证 dev extension 真实加载；已用 `zed_extension_api 0.7.0` + `wasm32-wasip2` 编译验证 API 可用。
2. **Xvfb 不可用**：VSCode e2e 暂用 `--ozone-platform=headless`；CI（有 root）可装 Xvfb。
3. **无 `fglform`/`fglcomp`**：部署/编译只能先做 mock 输出解析。
4. **无测试服务器**：`rsync`/`scp` 只验证了本地命令可用，未做真实上传。
5. **Windows 未验证**：本机无 Windows 环境；脚本与清单已就绪（`dev-env.ps1`、`env-check.ps1`），需在 Windows 机器执行。

### 重装/复现

```bash
cd bdl-lsp
bash scripts/install-toolchain.sh        # 安装缺失组件（幂等）
source scripts/dev-env.sh
bash scripts/env-check.sh
```

---

## 10. 参考

- [技术链与工具](TECH-STACK.md)
- [调研记录](RESEARCH.md)
- [待办](TODO.md)
- Zed 扩展文档：<https://zed.dev/docs/extensions/languages>
- Tree-sitter：<https://tree-sitter.github.io/tree-sitter/>
