# 测试指南（测试矩阵与命令）

> 更新：2026-10-22（P8：忽略列表 / grammar v1 tag / 编辑器能力差异）  
> 用途：说明"改了什么之后该跑什么、在 Linux/Windows 上分别怎么跑、期望看到什么"。  
> 配套：[HANDOVER](HANDOVER.md)（当前基线）、[ENVIRONMENT](ENVIRONMENT.md)（工具链安装）、
> [TODO](TODO.md)（未完成项）。

---

## 1. 快速判据（提交前至少跑这些）

```bash
cd bdl-lsp
source scripts/dev-env.sh      # 沙箱/无 root 环境必须；普通机器可跳过

make lint                      # vet + gofmt + 关键字/内置表/诊断码一致性
go test ./internal/...         # 单元测试（约 1 分钟）
make check                     # testdata 冒烟
```

三个都通过才能提交。改到 grammar 或诊断码时还要跑 §3.3、§3.4。

**期望基线**（2026-10-22 实测，完整见 [HANDOVER §4](HANDOVER.md#4-验收基线当前实测值)）：

| 项 | 期望 |
|---|---|
| `go test -race ./internal/...` | 20 个包全绿，300+ 测试函数 |
| `make lint` | 通过 |
| VSCode e2e | 33/33 |
| 语料解析 | `.4gl` 7683/7699（16 个真实语法错误） |
| 格式化审计 | 幂等失败 0、新增 ERROR 0 |

---

## 2. 测试分层

| 层 | 内容 | 何时跑 | 依赖 |
|---|---|---|---|
| L1 单元 | `go test ./internal/...` | 每次改动 | Go + gcc（CGO） |
| L2 静态 | `make lint` | 每次改动 | gofmt/vet/node |
| L3 冒烟 | `make check`、`make grammars-check`、`make manifest-check` | 每次改动 | testdata / grammar 源码 |
| L4 端到端 | `make e2e-vscode` | 改扩展、LSP 协议、诊断发布 | VSCode CLI |
| L5 语料回归 | `make corpus` / `make regress` / 三个 audit | 改 grammar、索引、字段、格式化 | 真实语料 `tiptop-gp` |
| L6 真实环境 | Windows 实机、Zed GUI、真实 SSH 服务器、`fglcomp` | 发布前/有条件时 | 见 §5 |

---

## 3. 命令矩阵

### 3.1 构建与冒烟

| 目的 | Linux | Windows（PowerShell） |
|---|---|---|
| 构建 LSP | `make build` → `artifacts/bin/bdl-lsp` | `scripts\build.ps1` → `artifacts\bin\bdl-lsp.exe` |
| 构建 + 单测 + 冒烟 | `make build && make test && make check` | `scripts\build.ps1 -Test -Check` |
| 环境自检 | `bash scripts/env-check.sh` | `scripts\env-check.ps1` |
| 版本 | `./artifacts/bin/bdl-lsp version` | `.\artifacts\bin\bdl-lsp.exe version` |
| 单文件解析 | `./artifacts/bin/bdl-lsp check testdata/sample.4gl` | `.\artifacts\bin\bdl-lsp.exe check testdata\sample.4gl` |

> Windows 上 `make` 不是必需：`scripts\build.ps1` 与 Makefile 的 Windows 分支
> 做同样的事（`GOENV=off` + 显式 MinGW 工具链）。装了 GnuWin32 make 也能用
> `make build`，但中文提示会变 ASCII。

### 3.2 单元测试与静态检查

| 目的 | Linux | Windows |
|---|---|---|
| 单元测试 | `go test ./...` | `go test ./...`（在 `GOENV=off` 下） |
| 竞态 | `go test -race ./internal/...` | `go test -race ./internal/...` |
| 静态 | `make lint` | `go vet ./...` + `gofmt -l .`（见下） |
| 清单/关键字 | `make keywords-check builtin-check diagnostics-check` | 同左（需要 Node） |

Windows 没有 bash 时 `make lint` 里的 gofmt 管道不可用，等价命令：

```powershell
$env:GOENV = 'off'
go vet ./...
$bad = gofmt -l . | Where-Object { $_ -notmatch '^artifacts/' }
if ($bad) { $bad; exit 1 }
```

### 3.3 grammar 仓库（tree-sitter-bdl / tree-sitter-per）

两个仓库各自独立，**不是** bdl-lsp 的子模块：

```bash
cd tree-sitter-bdl        # 或 tree-sitter-per
go test ./bindings/go/    # ABI 15 + 真实 .4gl/.per 解析测试

cd ../bdl-lsp
make keywords-check       # grammar 关键字变化 → 重新生成补全清单
make grammars-check       # grammar 变化 → 重新生成 TextMate 语法并校验切分
go test ./internal/gramaudit/   # 语料语法基线（TestCorpusGrammarBaseline）
```

规则（见 [HANDOVER §7](HANDOVER.md#7-已知坑写在这里避免重复踩)）：

- 改 `src/scanner.c` 后**必须** `go build -a ./internal/parser`，否则 Go 构建缓存
  不会重编被 `#include` 的 C 文件；
- 每次 grammar 改动都要 `make grammar-audit JSON=/tmp/before.json` 与改动后
  结果逐文件 diff，防止"修一个坏两个"；
- 发版：`git tag vX.Y.Z && git push origin HEAD vX.Y.Z`，bdl-lsp 的 `go.mod`
  固定 tag；Zed 的 `extension.toml` 固定 40 位 SHA。**两者都要更新**。

### 3.4 诊断码改动

```bash
cd bdl-lsp
# 改 internal/diag 的 summaries / defaultLevel 之后：
make diagnostics-write     # 重新生成 docs/DIAGNOSTICS.md
make diagnostics-check     # 校验 注册表 ↔ 文档 ↔ VSCode package.json
make lint                  # diagnostics-check 已在 lint 里
```

新增/删除诊断码还要同步 VSCode `package.json` 的 `bdl.diagnostics` 枚举
（`diagnostics-check` 会拦），并补一条协议级测试（参考
`internal/server/server_test.go` 的 `TestDiagnostics*`）。

### 3.5 VSCode 扩展

```bash
cd vscode-extension

npm run typecheck          # tsc --noEmit
npm run build              # esbuild → dist/（e2e 跑的是 dist）
npm run test:e2e           # 先 build，再真实扩展宿主
npm run package            # VSIX（本地）

# 推荐从 bdl-lsp 侧一键跑（会先构建 LSP 二进制）
cd ../bdl-lsp && make e2e-vscode
```

Windows：

```powershell
cd bdl-lsp
scripts\e2e-vscode.ps1            # 构建 LSP + typecheck + build + e2e
scripts\e2e-vscode.ps1 -Keep      # 失败时保留日志与临时工作区
```

> e2e 前必须 `npm run build`（`test:e2e` 已包含）；否则跑的是旧 `dist`，
> 症状是"命令不存在"这类假失败。

### 3.6 Zed 扩展

```bash
cd bdl-lsp
make zed-check             # verify-manifest.py + verify-queries.mjs
# Windows（无 python3 时）：
make zed-check-windows     # 只跑 query 校验

cd zed-extension
cargo check                # wasm32-wasip2 目标需先装 Rust 工具链
```

`verify-queries.mjs` 会真正用 tree-sitter CLI 对 fixture 跑一遍 query；
`verify-manifest.py` 校验 `extension.toml` 的 grammar rev（必须 40 位 SHA）、
语言名映射等。改了 grammar 的 commit 后必须同步 `extension.toml` 的 `rev`。

### 3.7 语料回归与审计

需要真实语料（默认 `/code/workspace/tiptop-gp`）：

```bash
cd bdl-lsp

make corpus                # 抽样冒烟（快）
make regress               # 全量解析回归（慢）
make diag-audit            # 逐诊断码计数（降噪基线）
make builtin-audit         # 内置符号表缺口（LIMIT/SHOW 可调）
make grammar-audit         # 语法缺口（TOP/JSON）
# 链接资料（属于 tiptop，不在本仓库）：
../tiptop/artifacts/bin/tiptop link import -csv <导出.csv> -root /code/workspace/tiptop-gp
../tiptop/artifacts/bin/tiptop link status -root /code/workspace/tiptop-gp

go run ./cmd/fmt-audit     # 格式化幂等 + 新增 ERROR
go run ./cmd/field-audit   # 字段校验
go run ./cmd/record-audit  # record 成员（约 20 分钟）
```

LSP 端到端延迟：

```bash
make bench                 # python3 scripts/lsp-bench.py（didOpen/didChange）
```

### 3.8 手工验证 LSP

```bash
./artifacts/bin/bdl-lsp version
./artifacts/bin/bdl-lsp check testdata/sample.4gl testdata/sample.per
```

部署单独验证（`deploy` 自 0.3.2 起在 tiptop）：

```bash
../tiptop/artifacts/bin/tiptop deploy -list -workspace /path/to/ws
../tiptop/artifacts/bin/tiptop deploy -dry-run -workspace /path/to/ws
../tiptop/artifacts/bin/tiptop deploy -workspace /path/to/ws path/to/file.4gl   # 当前文件
```

---

## 4. 一页速查（按改动类型）

| 改动 | 必跑 |
|---|---|
| 任意 Go 代码 | `make lint` + `go test ./internal/...` |
| 新增/改诊断码 | §3.4 + 相关包的协议级测试 |
| 改字段/表单逻辑 | `go test ./internal/form/ ./internal/server/` + `cmd/field-audit` |
| 改 grammar | §3.3 全套 + `make regress` |
| 改 config 键 | `go test ./internal/config/` + VSCode `package.json` + `make diagnostics-check` |
| 改 VSCode 扩展 | `npm run typecheck` + `make e2e-vscode` |
| 改 Zed query / 清单 | `make zed-check` |
| 改部署（在 tiptop） | `cd ../tiptop && make lint && go test ./...` + `tiptop deploy -dry-run` |
| 改索引 | `go test ./internal/wsindex/ ./internal/server/` + `make bench` |
| 只改文档 | 无需跑测试；但要更新 [TODO](TODO.md) 勾选与 [HANDOVER](HANDOVER.md) 状态 |

---

## 5. 需要真实环境的测试（本沙箱做不了）

| 项目 | 环境 | 验收方式 |
|---|---|---|
| Windows 构建 + 运行 | Windows 10/11 + MinGW-w64 | CI `windows` job；`scripts\build.ps1 -Test -Check` |
| Windows e2e / URI 编码 | Windows + VSCode | `scripts\e2e-vscode.ps1`；打开 `axmt620.4gl` 确认无 `globals-file-not-found` |
| Windows 部署（scp 回退） | Windows + 真实 SSH | `tiptop deploy -dry-run` → `tiptop deploy`，确认自动回退 |
| Zed 加载扩展 | 有显示器的 Linux/macOS/Windows | `zed: install dev extension`，确认高亮/大纲/LSP 状态 |
| 远程编译诊断 | 真实 Genero 服务器（`fglcomp`/`fglform`） | `tiptop deploy` 后看 Problems 面板行号映射 |
| 大仓库性能 | 15k 文件级项目 | `make bench`、索引日志耗时 < 60s |

这些项目的代码路径已就绪，但**没有自动化证据**；不要把它们标成"已验证"。

---

## 6. 常见失败

| 现象 | 原因 | 处理 |
|---|---|---|
| `gcc: command not found` / CGO 报错 | 没装 C 编译器 | Linux `build-essential`；Windows MinGW-w64 |
| `Incompatible language version 15` | go-tree-sitter < v0.25 | 三个仓库统一 v0.25.0（grammar 已打 tag） |
| `go: ... connection refused` / 拉不到 grammar | 模块代理或网络 | `GOPROXY=https://goproxy.cn,direct`；tag 要 push 到远端 |
| `permission denied` 写 `$HOME` | 沙箱只读 | `source scripts/dev-env.sh`（缓存改到 `artifacts/`） |
| e2e "命令不存在" | 用了旧 `dist` | 先 `npm run build` 或直接用 `make e2e-vscode` |
| `tree-sitter` 解析结果没变 | Go 缓存没重编 scanner | `go build -a ./internal/parser` |
| diagnostics 检查失败 | 文档/枚举没同步 | `make diagnostics-write` 后重跑 `make lint` |
| Zed 高亮全失效 | query 编译失败 | `make zed-check` 看具体节点名/字面量 |

---

## 7. 提交前检查清单

- [ ] `make lint` 通过
- [ ] `go test ./internal/...` 通过（改并发/生命周期时用 `-race`）
- [ ] 新增功能有测试；协议级改动有 e2e 或 harness 测试
- [ ] 文档同步：README / FEATURES / TODO / HANDOVER / 本文件（按改动范围）
- [ ] `git status` 无 `artifacts/`、二进制、日志等过程产物
- [ ] 版本号按仓库各自 +1（bdl-lsp `main.go`、VSCode `package.json`、Zed `extension.toml`）
