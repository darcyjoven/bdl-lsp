# BDL LSP 项目粗略计划

> 版本：v0.4（草案）  
> 日期：2026-10-19  
> 状态：**粗略计划**。本文只确定目标、边界、技术链、阶段与验收标准；详细到文件/函数/用例的拆分在确认待定项后另写 `PLAN-DETAILED-P1.md`。  
> 约定：中文文档、测试脚本、中间产物、日志统一放在 `bdl-lsp/`。  
> v0.2 变更：纳入 Q1（仓库远程）、Q2（rsync 主/scp 备）、Q3（warning 默认）、Q6（v1 Linux+Windows、macOS 后续）；补充环境配置与本地发布拓扑。  
> v0.3 变更：`bdl-lsp` 增加远程与独立 Release（Q9）、恢复 Q4–Q9。  
> v0.4 变更：Zed 扩展 id 定为 `4gl-full`；Genero 5.0 支持与发布/自动下载实现**暂不排期**；本轮先完成环境、push 与 P1 详细计划。

---

## 1. 背景与现状

### 1.1 语言与工具链

Genero BDL（Business Development Language，4GL）由 Four Js 提供，典型文件：

| 文件 | 含义 | 编译/运行 |
|---|---|---|
| `*.4gl` | 程序模块（`MAIN` / `FUNCTION` / `REPORT`） | `fglcomp` → `.42m` |
| `*.global` | 全局模块（`GLOBALS`、全局变量、函数） | 同上，供其他模块引用 |
| `*.per` | Form Specification，界面/字段/布局/屏幕记录 | `fglform` → `.42f` |
| `*.42m` / `*.42f` | 编译产物 | `fglrun` / `fgllink` |

本项目只处理源码侧（`.4gl`、`.global`、`.per`），不实现编译器/运行时。

**Genero 版本策略**：

- 当前 `tree-sitter-bdl` 语法覆盖 **2.50**（用户口径 2.5）语法基线。
- 产品目标范围：**2.50–5.0**；v1 以 2.50 语料/语法为主要基线，权威校验由 `fglform`/`fglcomp` 完成。**5.0 暂不排期**，待有明确需求后再调研/升级 grammar。
- 版本差异通过配置/能力检测区分；未知语法降级为语法诊断，不阻塞打开文件。
- 详见 D8。

### 1.2 已有基础

| 资产 | 现状 |
|---|---|
| `tree-sitter-bdl` | `.4gl` 语法；121 个 corpus 测试通过；topprod 语料 7699 个文件，7671 通过、28 失败（另有 2980 个含 ERROR、4 个 MISSING） |
| `tree-sitter-per` | `.per` 语法；真实语料 7070 个，7068 通过（99.97%）；高亮/缩进查询；Go 绑定 |
| `bdl-plugin` | 已有 Zed 语法高亮扩展（仅 grammar，无 LSP） |
| `per-lint` | `.per` 语料、解析、错误注入验证的过程产物 |
| `tiptop-gp` | 真实工程语料：7699 个 `.4gl`、7409 个 `.per` |

### 1.3 本机已完成的可行性验证

1. **Go + CGO 同时链接两个 grammar 成功**：冷编译约 20 秒，产物约 5.6 MB，解析 `ammr110.4gl` / `ammr110.per` 均为 0 ERROR。
2. **ABI 兼容性已确认**：两个 grammar 由 tree-sitter CLI 0.26.5 生成，`LANGUAGE_VERSION = 15`；必须使用 `go-tree-sitter v0.25.0`，`v0.24.0` 会报 `Incompatible language version 15`。
3. **LSP 基础库可编译并完成 initialize 握手**：`go.lsp.dev/protocol v0.12.0` + `go.lsp.dev/jsonrpc2 v0.10.0` 在 Go 1.24.4 下运行，返回 `textDocumentSync: 2`、`definitionProvider: true`。
4. **CST 结构适合做字段校验**：`form`、`window.form`、`display.field`、`construct.field`、`branch.field`、`hook.field` 等节点可以直接定位表单名和字段名，详见 `docs/RESEARCH.md`。
5. **尚未验证**：Zed 扩展的 WASM 构建与真实加载、VSCode 扩展的端到端启动、二进制下载分发、服务器部署链路。

### 1.4 现有能力缺口

- 没有 LSP：跳转、补全、诊断、格式化、跨文件分析都依赖编辑器文本搜索。
- `.4gl` 与 `.per` 是两套 grammar，编辑器无法在打开 4GL 时理解表单字段。
- `.per` 的字段名与 4GL 里的 `AFTER FIELD`、`NEXT FIELD`、`INFIELD()`、`CONSTRUCT ... ON`、`DISPLAY ... TO` 等语句没有静态校验，错误通常到运行时才暴露。
- 没有统一构建/部署入口；改完代码后上传、`fglform`/`fglcomp` 编译依赖人工脚本。

---

## 2. 目标与非目标

### 2.1 v1 必须交付

| # | 能力 | 说明 |
|---|---|---|
| F1 | 语法高亮 | `.4gl`、`.global`、`.per`；静态高亮开箱可用，LSP 语义高亮作为增强 |
| F2 | 跳转/引用 | 函数、`MAIN`、`REPORT`、模块级变量、参数、局部变量、记录字段、常量、类型；4GL 字段 ↔ `.per` item |
| F3 | 符号/悬停 | 文档符号、工作区符号、悬停显示类型/定义位置/所属模块 |
| F4 | 诊断 | 语法错误、`.per` 内部一致性、4GL 字段不存在于表单 |
| F5 | 格式化 | 4GL 文档格式化（保守、可配置）；`.per` 仅格式化非布局小节 |
| F6 | 部署/编译 | 上传到目标服务器，调用 `fglform`/`fglcomp`，编译输出映射为诊断 |
| F7 | 双编辑器 | VSCode 扩展 + Zed 扩展共享同一个 Go LSP 与同一套配置 |
| F8 | 工程化 | 三个 Git 仓库、CI、跨平台构建、发布物、中文文档、测试基线 |

### 2.2 v1.1 / 后续增强

- 补全：关键字、函数、变量、表单字段、`ON ACTION` 动作名。
- 重命名（跨文件、大小写不敏感、含 `.per` 字段）。
- Code Action / Quick Fix：补字段、改字段名、忽略诊断。
- 范围格式化、格式化选中 SQL。
- LSP 语义高亮（semantic tokens）完整实现；VSCode 与 Zed 的语义高亮联动。
- `.per` 布局预览、字段跳转、`SCREEN RECORD` 结构可视化。
- 数据库 schema / `.sch` / 建表 SQL 索引：校验 `table.column` 是否存在。
- 部署增强：多目标、上传清单/回滚、编译产物归档；纯 Go 同步仅作为远期可选（v1 不做）。
- 测试运行、调试适配器（不属于本 LSP 的核心目标）。

### 2.3 非目标

- 不实现 Genero 编译器、运行时、数据库访问。
- 不追求 100% 语义正确；对无法静态确定的场景明确降级，宁可不报，不误报。
- 不做完整 IDE（调试、GUI 设计器、版本控制界面）。
- 不替换 `fglform` / `fglcomp` 的权威校验；LSP 是编辑期辅助，部署编译是最终验证。
- 不在 v1 支持移动端/Web IDE。

---

## 3. 交付物与仓库

### 3.1 仓库职责

| 本地目录 | 远程仓库 | 职责 | Git 状态 |
|---|---|---|---|
| `bdl-lsp` | `git@github.com:darcyjoven/bdl-lsp.git` | Go LSP 服务 + CLI + 文档 + 脚本 + 过程产物 + LSP Release | 已初始化，`origin` 已配置，待 push |
| `vscode-extension` | `git@github.com:darcyjoven/vscode-bdl.git` | VSCode 客户端、TextMate 语法、语言配置、命令、打包 | 已初始化，`origin` 已配置，待 push |
| `zed-extension` | `git@github.com:darcyjoven/zed-bdl.git` | Zed 扩展清单、Rust/WASM 启动逻辑、Tree-sitter 查询 | 已初始化，`origin` 已配置，待 push |
| `tree-sitter-bdl` | `github.com/darcyjoven/tree-sitter-bdl` | `.4gl` grammar（独立维护） | 已有仓库 |
| `tree-sitter-per` | `github.com/darcyjoven/tree-sitter-per` | `.per` grammar（独立维护） | 已有仓库 |

说明：

- 本地目录名与远程名不同：`vscode-extension` → `vscode-bdl`，`zed-extension` → `zed-bdl`；文档中“仓库”一般指本地目录。
- `bdl-lsp` 现在是自己源码与 LSP 二进制的**唯一 Release 源**；不再把二进制托管到扩展仓库。
- 三个仓库都已配置 `origin`，首次 push 需要 GitHub SSH 凭据。

### 3.1.1 分发拓扑（设计保留，暂不排期）

```text
bdl-lsp GitHub 仓库（源码 + CI + Release）
      │  tag: lsp-vX.Y.Z
      │  assets: bdl-lsp-linux-amd64 / bdl-lsp-windows-amd64.exe / SHA256SUMS
      ▼
GitHub Release
      ├── VSCode 扩展（自动下载，无感安装）
      └── Zed 扩展（自动下载，无感安装）
```

- LSP 版本与扩展版本解耦；扩展声明“最低 LSP 版本”，启动时校验。
- **当前不排发布计划**：开发/测试阶段使用本地构建的 `bdl-lsp`，通过 `bdl.server.path`/`BDL_LSP_PATH` 指定；GitHub Release 与无感下载作为后续工作。
- 发布启用后，扩展从 `bdl-lsp` Release 自动下载；扩展 CI 只负责客户端。

### 3.2 交付物清单

- `bdl-lsp` 源码 + 本地构建产物：v1 覆盖 Linux amd64 + Windows amd64；GitHub Release 暂不排期；macOS/arm64 后续。
- VSCode `.vsix`：**先本地安装**（Q5）；稳定后再评估市场发布。
- Zed 扩展：dev extension 可用；扩展 id 为 **`4gl-full`**（不复用 `bdl`，与轻量 `bdl-plugin` 共存）；注册表发布暂不排期。
- 中文文档：本计划、技术链、调研、环境配置、测试说明、部署说明、格式化规则。
- 环境配置：Linux/Windows 工具链清单、验证命令、账号/权限、CI 映射。
- 测试脚本与基线报告：语料解析、字段校验标注、格式化幂等、性能。
- 版本兼容矩阵：LSP 版本 ↔ 扩展版本 ↔ grammar revision ↔ Genero 2.50–5.0 支持范围。

---

## 4. 总体架构

```text
┌────────────────────┐        stdio / JSON-RPC 2.0        ┌──────────────────────────────┐
│ VSCode 扩展 (TS)    │ ────────────────────────────────► │ bdl-lsp (Go, CGO)            │
│ - 启动/下载二进制   │ ◄──────────────────────────────── │                              │
│ - TextMate 高亮     │                                   │  transport / dispatch         │
│ - 命令、部署入口    │                                   │  document store（增量同步）   │
└────────────────────┘                                   │  parser pool                 │
                                                         │   ├─ tree-sitter-bdl         │
┌────────────────────┐        stdio / JSON-RPC 2.0        │   └─ tree-sitter-per         │
│ Zed 扩展 (Rust/WASM)│ ────────────────────────────────► │  workspace index / cache     │
│ - grammar + queries │                                   │  features: nav / diag / fmt  │
│ - LSP 启动/下载     │                                   │  deploy driver               │
└────────────────────┘                                   └──────────────┬───────────────┘
                                                                        │ ssh + rsync（主）/ scp（备）
                                                                        ▼
                                                              ┌────────────────────┐
                                                              │ 部署/编译服务器     │
                                                              │ fglform / fglcomp  │
                                                              └────────────────────┘
```

### 4.1 分层

| 层 | 包（规划） | 职责 |
|---|---|---|
| 入口 | `cmd/bdl-lsp` | 参数解析、stdio/TCP、`deploy` 子命令、版本 |
| 传输 | `internal/server` | JSON-RPC 分发、生命周期、能力协商、取消、panic 兜底 |
| 文档 | `internal/document` | didOpen/didChange/didClose、增量编辑、行列索引、脏标记 |
| 解析 | `internal/parser` | parser 池、Tree-sitter 绑定、查询编译、错误统计 |
| 索引 | `internal/index` | 工作区扫描、文件索引、符号表、表单索引、磁盘缓存 |
| 语言模型 | `internal/bdl`、`internal/per` | 作用域、符号、记录字段、表单 item/SCREEN RECORD |
| 功能 | `internal/features` | definition/references/hover/symbols/completion/diagnostics/semantic tokens |
| 校验 | `internal/validate` | 4GL 字段存在性、`.per` 内部一致性 |
| 格式化 | `internal/format` | 4GL/`.per` 格式化、幂等与安全守卫 |
| 部署 | `internal/deploy` | 配置、上传、远程编译、输出解析 |
| 配置 | `internal/config` | LSP 设置、工作区配置、默认值 |
| 日志 | `internal/logging` | `slog`、文件/ stderr、脱敏 |

### 4.2 关键数据流

1. **打开文件**：`didOpen` → 解析 → 建立 `FileIndex` → 发布解析诊断；若是 `.per`，更新表单索引。
2. **跳转**：光标位置 → 最小 CST 节点 → 作用域解析（局部 → 参数 → 模块全局 → 导入/跨模块）→ `Location` 列表。
3. **字段校验**：4GL 中提取表单绑定与字段引用 → 解析 `.per` 路径 → 读取/解析表单 → 字段集合比对 → 诊断 + 关联位置。
4. **格式化**：文本 + 选项 → CST/词法流 → 生成最小 `TextEdit`；若范围内有 ERROR，则返回空编辑并记录原因。
5. **部署**：`workspace/executeCommand`（VSCode）或 CLI（Zed/终端）→ 读取配置 → 上传 → 远程编译 → 解析输出 → 生成诊断/摘要。

---

## 5. 功能范围与优先级

| 功能 | v1 | 后续 | 备注 |
|---|---|---|---|
| `.4gl` / `.global` / `.per` 高亮 | ✅ | 语义高亮增强 | VSCode TextMate；Zed `highlights.scm` |
| 文档/工作区符号 | ✅ | 层级优化 | 函数、报表、全局、记录、表单 item |
| 定义/引用 | ✅ | 重命名、调用层级 | 先做函数/变量/字段 |
| 悬停 | ✅ | 手册文档、类型推导增强 | 显示签名、类型、定义位置 |
| 语法诊断 | ✅ | 错误恢复增强 | 基于 ERROR/MISSING 节点 |
| `.per` 内部校验 | ✅ | 布局/属性语义完善 | 重复 item、SCREEN RECORD 引用、布局引用 |
| 4GL 字段存在性 | ✅（显式字段） | `BY NAME`、动态 API、schema 列 | 见 6.2 的降级策略 |
| 4GL 格式化 | ✅（缩进/大小写/空白） | 重排、对齐、SQL 格式化 | 保守，不改变换行结构 |
| `.per` 格式化 | ✅（非 LAYOUT 小节） | 布局对齐 | 绝不改 LAYOUT 的列位置 |
| 部署 + 编译 | ✅（ssh + rsync；scp 备选） | 多目标、回滚、编译产物归档 | 见 7.4 |
| 补全 | ⬜ | ✅ | 关键字/函数/字段 |
| Code Action | ⬜ | ✅ | 快速修复 |
| 语义高亮 | ⬜ | ✅ | 两编辑器均支持 |

---

## 6. 关键设计决定

### D1. 一个 LSP 同时服务 `.4gl` 与 `.per`

原因：字段校验、字段跳转、表单符号都要求跨语言理解；拆成两个 server 会重复索引、难以共享工作区状态。

- VSCode `documentSelector`：`bdl`（`.4gl`、`.global`）与 `per`（`.per`）。
- Zed：一个 language server 注册给两个 language（`BDL`、`Genero Form`），通过 `language_ids` 映射。

### D2. Go + CGO 静态链接 Tree-sitter，不使用 WASM/Wazero

- 已在 Go 1.24.4 / gcc 13.3 下验证。
- 解析性能、内存、与 C 生态一致性最好；两个 parser 一起只有约 5.6 MB 初始二进制。
- 代价：需要跨平台 C 工具链；通过 GitHub Actions 原生矩阵构建规避交叉编译。
- 被否方案：`wazero` + WASM grammar（依赖复杂、性能与 ABI 不确定）。

### D3. grammar 通过 Go module 固定 revision，不复制 `parser.c`

- `go.mod` 固定 `github.com/darcyjoven/tree-sitter-bdl`、`.../tree-sitter-per` 的 tag/伪版本；`go.sum` 锁定内容。
- 本地开发用 `go.work` 指向兄弟目录，发布构建用干净 checkout。
- 不 vendor 11 MB 的 `parser.c`，避免双份维护；CI 校验 grammar revision 与 `go.mod` 一致。
- 风险：grammar 仓库必须可被 CI 访问；若转私有，改用内部 GOPROXY 或 vendor。

### D4. LSP 基础库：`go.lsp.dev/protocol` + `go.lsp.dev/jsonrpc2`

- 选用 `protocol v0.12.0`、`jsonrpc2 v0.10.0`（LSP 3.16 类型，Go 1.17+，已验证）。
- v1 需要的文档同步、定义/引用、符号、格式化、推送诊断、语义高亮在 3.16 已覆盖；不依赖 3.17 的 inlay hints / pull diagnostics。
- 最新 `v1.0.1` 支持 3.17，但要求 Go 1.26；待工具链升级后作为独立任务验证，不阻塞 v1。
- 不使用 `tliron/glsp`：协议停留在 3.16、更新停滞、框架侵入性强；本项目自己控制分发与错误码。
- 采用自定义 method 分发（不实现庞大 `protocol.Server` 接口），保留对未知方法的兼容与错误码控制。

### D5. 二进制分发（设计保留，暂不排期）：`bdl-lsp` Release + 无感自动安装

- **暂不排发布计划**：当前只构建本地二进制；Release、自动下载与版本升级在后续排期时实现。
- 设计上 `bdl-lsp` 有独立远程仓库；未来可在 tag `lsp-vX.Y.Z` 上构建并发布资产。
- v1 资产：`bdl-lsp-linux-amd64`、`bdl-lsp-windows-amd64.exe` + `SHA256SUMS`；macOS/arm64 后续补。
- **VSCode（目标体验）**：扩展激活时检查“扩展要求的最低/目标 LSP 版本”，缺失或过旧则后台自动下载到 `globalStorageUri`，校验 SHA256、设置可执行权限并启动；只在耗时较长时显示进度，不要求用户选择路径。该实现暂不排期，开发期先用本地路径。
- **Zed（目标体验）**：`language_server_command` 首次调用时用 `download_file` 自动下载对应平台二进制，`make_file_executable` 后启动；通过 `set_language_server_installation_status` 显示“下载/安装中”。该实现暂不排期，开发期先用本地路径。
- **开发期路径**：设置 `bdl.server.path` / 环境变量 `BDL_LSP_PATH` / 工作区 `.bdl-lsp/bin/bdl-lsp`；发布启用后再加下载失败兜底。
- **缓存与回滚**：按版本缓存，保留上一个可用版本；启动失败可回退，不阻塞用户。
- **不默认内置 VSIX**：多平台体积大、更新慢；采用主流语言扩展常见的“按平台下载 + 本地缓存”模式（如 rust-analyzer），仅在企业离线场景提供“离线包”。
- 发布可重复：固定 Go/grammar revision、`-trimpath`、校验和、失败不覆盖已有 Release。

### D6. 部署复用同一个二进制，传输以 rsync 为主、scp 为备

- `bdl-lsp deploy ...` 作为 CLI 子命令；LSP 的 `workspace/executeCommand` 内部复用 `internal/deploy`。
- 传输：优先 `rsync -az`（默认不 `--delete`）；目标机/客户端无 rsync 或失败时回退 `scp`。
- VSCode 提供命令/状态栏；Zed 通过 `.zed/tasks.json` 调用 CLI（Zed 扩展 API 不提供任意命令注册）。
- 好处：同一套配置、同一套日志、可在 CI/终端复用；Windows 客户端可仅依赖 OpenSSH 的 `scp`。

### D7. 文档与过程产物集中

- 所有中文文档、测试脚本、语料报告、日志、缓存都放 `bdl-lsp/`。
- `vscode-extension` / `zed-extension` 只保留发布所需文件，便于市场审核与用户阅读。
- 运行期缓存不放仓库：优先 `$XDG_CACHE_HOME/bdl-lsp`，其次系统临时目录；部署日志由编辑器输出通道 + 可选文件保存。

### D8. Genero 版本策略：2.50 基线，5.0 暂不排期

- 当前 `tree-sitter-bdl` 覆盖 **2.50**（用户口径 2.5）语法；v1 以 2.50 语料/语法为主要基线，权威校验仍以 `fglform`/`fglcomp` 为准。
- 目标范围 **2.50–5.0**；但 **5.0 暂不排期**，v1 只承诺 2.50 基线可用。5.0 有明确需求后再调研/升级 grammar。
- 解析、诊断、格式化通过配置（如 `bdl.generoVersion`）了解目标版本；未知语法只报语法诊断，不阻塞打开/跳转。
- 未来升级到 5.0 时必须回归 2.50 语料，避免破坏旧项目。
- 维护“Genero 版本 × 功能”兼容矩阵，发布说明中标注。

---

## 7. 关键设计细节

### 7.1 解析与索引

- 每个文件解析后生成轻量 `FileIndex`：顶层符号、函数/报表体、变量、记录字段、导入、表单绑定、字段引用；随后释放 Tree。
- 解析使用 parser 池（`sync.Pool`），并行 worker 数默认 `GOMAXPROCS`，可配置上限。
- 打开文档保留 Tree 支持增量解析；关闭后释放，仅保留索引。
- 查询（`.scm`）编译一次，`go:embed` 进二进制；查询失败启动即报错。
- 工作区扫描只针对配置的源码根（默认整个 workspace），忽略 `.git`、`42f`、`node_modules`、构建目录。
- 索引缓存：按 `(绝对路径, mtime, size, grammar revision)` 存 gob/二进制；后续可替换 SQLite。
- 符号大小写：Genero 标识符大小写不敏感；内部统一小写，展示保留原文。

### 7.2 4GL 字段存在性校验（核心差异化）

**目标**：编辑期发现“4GL 里写了某个表单字段，但 `.per` 里没有”。

**表单解析**：

- 从 `.per` 的 `ATTRIBUTES` 提取 item 名（`Edit mmg01=...` → `mmg01`），保留类型、绑定列（`table.column` / `formonly.column`）、`PHANTOM` 标记、定义位置。
- 从 `INSTRUCTIONS` 提取 `SCREEN RECORD name(...)` 及其字段引用。
- 从 `LAYOUT` 提取布局 item tag，用于“布局引用了未定义 item”的反向检查。

**4GL 侧绑定**：

- `form`：`OPEN FORM <name> FROM "<path>"`（语料中仅 6 处）。
- `window.form`：`OPEN WINDOW ... WITH FORM "<path>"`（语料 8247 处，主要形式）。
- `display.name`：`DISPLAY FORM <form>`。
- 以函数为作用域，按语句顺序维护“当前表单”；多窗口/多表单时取最近绑定；无法确定时降级。

**字段引用（v1 只做显式字段，避免误报）**：

- `CONSTRUCT BY NAME ... ON f1, f2`（4456 处）
- `BEFORE FIELD f` / `AFTER FIELD f`（5394 / 49130 处）
- `NEXT FIELD f`（115947 处）
- `INFIELD(f)`（37535 处）
- `DISPLAY ... TO f`
- `FIELD ORDER` / 其他显式字段列表（按 CST 节点补充）

**v1 暂不校验**：

- `INPUT BY NAME rec.*` / `DISPLAY BY NAME rec.*` 的记录成员是否存在（需推断记录与表单的对应关系，容易误报）。
- `ui.Form.setFieldValue("form", "field", ...)` 等动态字符串 API。
- 通过变量/表达式动态打开的表单。

**路径解析**：

1. 字符串字面量路径（如 `amm/42f/ammr110`）→ 取 basename → 在 `.per` 索引中查找。
2. 查询顺序：工作区 `.per` 索引、配置的 `perRoots`、与当前文件相对的 `../per/`、`./`。
3. 唯一命中才校验；多个命中按配置的 root 优先级选择，仍不唯一则提示“表单不明确”，不报字段错误。
4. 表单名（`DISPLAY FORM cust`）→ `<name>.per`。

**诊断策略**：

- 默认 `warning`，可配置 `off` / `warning` / `error`。
- 仅当“表单唯一解析成功 + 字段属于明确的交互语句”时报告；否则静默（严格模式可开启）。
- 诊断包含 `relatedInformation` 指向 `.per` 文件与 item 位置，Code Action 可跳转。
- 字段名大小写不敏感匹配；`PHANTOM` 字段算存在。
- 提供忽略列表（按文件/字段/表单 glob），用于历史遗留。

**质量目标**：在人工标注的 50 个 Tiptop 模块上，误报率 < 5%，漏报不作为硬指标（优先不打扰）。

### 7.3 格式化

**4GL 格式化分三阶段**，每阶段都可独立开关：

1. **缩进**：按 CST 块结构（`IF/END IF`、`FOR/END FOR`、`WHILE`、`CASE`、`INPUT`、`DIALOG`、`MENU`、`FUNCTION`、`REPORT` 等）重排行首空白；保留空行。
2. **空白/大小写**：关键字大小写（默认大写）、逗号后空格、二元运算符两侧空格、括号内侧不留空格；不合并/拆分语句行。
3. **可选增强**：连续赋值/声明对齐、注释列对齐。默认关闭，避免大面积 diff。

`.per` 格式化只处理 `ATTRIBUTES`、`INSTRUCTIONS`、`TABLES` 等小节的行结构、逗号/分号与缩进；**LAYOUT 的字符画与列位置保持原样**（只允许去行尾空白）。

**安全守卫**：

- 格式化范围内存在 `ERROR` / `MISSING` 时，不返回编辑，并在日志说明。
- 所有编辑最小化；保留字符串、注释、预处理指令、SQL 字符串原文。
- 幂等性测试：`format(format(x)) == format(x)`。
- 语义保持测试：格式化前后解析树（忽略位置）等价；或至少不新增 ERROR。
- 范围格式化在 v1.1 提供；v1 只做整文档。

### 7.4 部署与远程编译

**传输策略：ssh + rsync 为主，scp 为备选。**

| 场景 | 方案 |
|---|---|
| Linux 客户端 + Linux 服务器 | `rsync` over ssh（默认） |
| Windows 客户端，无 rsync | OpenSSH `scp`（逐文件/目录） |
| rsync 不可用或失败 | 显式回退 `scp`，日志标注“已回退”，不静默降级 |
| macOS 客户端 | v1 之后补测；通常 rsync/scp 都可用 |

**配置**（工作区文件，建议 `.bdl-lsp/deploy.json`，两个编辑器共用）：

```json
{
  "targets": {
    "test": {
      "host": "dev.example.com",
      "port": 22,
      "user": "genero",
      "remoteRoot": "/u1/app",
      "localRoot": ".",
      "upload": {
        "include": ["**/*.4gl", "**/*.per"],
        "exclude": ["**/42f/**", "**/.git/**"],
        "mode": "rsync",
        "rsyncArgs": ["-az"],
        "fallback": "scp",
        "delete": false
      },
      "compile": {
        "per": "fglform {file}",
        "4gl": "fglcomp -M {file}"
      },
      "strictHostKey": true
    }
  },
  "defaultTarget": "test"
}
```

**流程**：

1. 解析配置，校验必填项、本地路径、远程路径；用 `exec.LookPath` 检查 `ssh` 与首选传输工具。
2. 计算待上传文件，尊重 include/exclude；`--dry-run` 调 `rsync --dry-run` 或列出 `scp` 清单。
3. 上传：
   - `rsync -az`（默认不 `--delete`；需要删除时显式配置）；
   - rsync 缺失/失败且 `fallback=scp` 时，用 `scp -p` 逐文件上传；
   - 上传到临时目录/临时文件名 + 远程 `mv`，避免半文件（rsync 使用 `--delay-updates` 或临时目标）。
4. 按类型调用远程编译命令（`.per` → `fglform`，`.4gl` → `fglcomp`）。
5. 捕获 stdout/stderr，按可配置正则解析 `file:line: error: message`，映射远程路径 → 本地路径。
6. 发布诊断（source = `fglform` / `fglcomp`），输出摘要；非零退出码作为整体失败。
7. 全部写入日志（脱敏，不记录密码/私钥内容）。

**安全**：

- 部署必须显式触发（命令/CLI），不在保存时自动执行。
- 不存储密码；使用 SSH key / agent；默认严格 host key；首次连接需人工确认或预置 known_hosts。
- 远程命令参数转义；上传采用临时文件 + rename，避免半文件。
- 只允许配置中声明的目标；禁止从工作区文件读取任意 shell 片段（编译命令除外，需用户显式配置）。
- `scp` 回退时逐文件错误要聚合报告，不能“部分成功却显示成功”。

**平台策略（v1）**：

- **Linux**：`ssh` + `rsync` 主路径必须验证通过；`scp` 作为备选也验证。
- **Windows**：验证 OpenSSH `scp` 路径（Windows 10+ 自带 OpenSSH Client）；`rsync` 通常需要 cwRsync/MSYS2/WSL，列为可选增强；文档明确“Windows 无 rsync 时自动用 scp”。
- **远程服务器**：假定为 Linux（Genero 运行环境）；路径使用 POSIX 风格；本地 Windows 盘符路径在配置中映射到 `remoteRoot`。
- **换行**：上传前检查 `.4gl`/`.per` 不应含 CRLF；发现时告警，避免远程编译出现奇怪错误。
- **macOS**：v1 之后验证；代码不依赖 Linux 专有命令。

### 7.5 VSCode 与 Zed 的差异

| 能力 | VSCode | Zed |
|---|---|---|
| LSP 启动 | `vscode-languageclient` | Rust/WASM `language_server_command` |
| 高亮 | TextMate grammar（必须）+ 语义高亮 | Tree-sitter queries + 语义高亮 |
| 二进制获取 | 设置/环境变量/下载/内置 | 扩展目录下载/`PATH`/工作区 |
| 任意命令 | 支持（部署、格式化、重跑索引） | 扩展 API 不支持任意命令；用 LSP command/code action + `.zed/tasks.json` |
| 配置 | `settings.json` + LSP configuration | `settings.json` + 初始化选项/工作区配置 |
| 测试 | `@vscode/test-electron` | `cargo check` + 手工 dev extension 检查 |

Zed 的部署入口设计为：LSP 提供 `workspace/executeCommand`（可选 Code Action），同时提供 `bdl-lsp deploy` CLI，Zed 用户在 `.zed/tasks.json` 中调用。文档中给出模板。

### 7.6 性能与缓存

| 指标 | 目标 |
|---|---|
| 冷启动到可响应 initialize | < 1.5 s |
| 打开 1 MB `.4gl` 并发布首轮诊断 | < 300 ms |
| 单文件增量重解析（典型 5k 行） | < 100 ms |
| 15k 文件后台索引 | < 60 s（8 核），不阻塞编辑 |
| 常驻内存（打开 20 个文件） | < 300 MB |
| 格式化 5k 行 | < 500 ms |

- 首次索引后台执行并显示进度；字段校验只解析被引用的 `.per`，不全量预解析。
- 缓存失效：mtime/size 变化、grammar revision 变化、配置根变化。
- 大文件上限与忽略规则可配置，避免误索引构建产物。

### 7.7 日志与可观测性

- `slog` JSON/文本，默认写 stderr（LSP 客户端不解析 stdout 之外的内容）。
- 文件日志：`--log-file` 或编辑器扩展传入；按大小轮转（如 10 MB × 3）。
- 级别：`error/warn/info/debug`；默认 `info`，LSP trace 时提升。
- 不记录文件全文、不记录密钥；路径可按需脱敏。
- 索引统计、解析耗时、诊断数量以 debug 日志输出，便于性能回归。

---

## 8. 里程碑（粗略）

> 时间单位为“工作块”，不是承诺日期；每个里程碑结束都可演示。

### P0 技术验证、环境与决策（进行中）

- ✅ Go/CGO + 双 grammar 解析。
- ✅ LSP 库 initialize 握手。
- ✅ CST 字段节点调研、语料统计。
- ✅ 仓库远程确定：`bdl-lsp`、`vscode-bdl`、`zed-bdl` 均已配置 `origin`。
- ⬜ **环境配置**：Linux 与 Windows 的 Go/CGO、Node/pnpm、tree-sitter CLI、Rust/wasm、OpenSSH、rsync/scp、VSCode；清单见 [ENVIRONMENT.md](ENVIRONMENT.md)。
- ⬜ Zed 扩展最小骨架 + WASM 构建（需安装 Rust/wasm target/wasi-sdk）。
- ⬜ VSCode 最小客户端 + 启动真实 `bdl-lsp`（Linux + Windows）。
- ⬜ 本地二进制闭环：`bdl-lsp` 构建 → 扩展通过本地路径启动（Release/自动下载暂不排期）。
- ⬜ 首次 push `bdl-lsp`、`vscode-bdl`、`zed-bdl`（需要 GitHub SSH 凭据）。
- ✅ 版本策略确定：当前 2.50 基线；5.0 暂不排期。
- 产出：`RESEARCH.md`、`ENVIRONMENT.md`、技术选型定稿。

### P1 LSP 骨架 + VSCode MVP（约 5–8 个工作块）

- 三个仓库骨架；`bdl-lsp` GitHub Actions（Linux/Windows、CGO、测试）；扩展仓库 CI。
- LSP：initialize、文档同步、日志、配置、解析诊断、文档符号。
- 4GL 函数/变量/记录字段的 definition/references/hover。
- VSCode：客户端、本地二进制路径管理（自动下载暂不排期）、TextMate 语法、语言配置、状态栏。
- 验收：**Linux 与 Windows** 上 VSIX 在样例工作区可高亮、跳转、显示语法错误（macOS 记录为后续项）。

### P2 Zed 扩展（约 2–3 个工作块）

- 扩展 id `4gl-full`（与 `bdl-plugin` 共存）、grammar 注册（bdl/per）、queries（highlights/outline/brackets/injections/indents）。
- `lib.rs` 通过本地路径启动 LSP（自动下载暂不排期）；平台识别。
- 验收：**Linux 与 Windows** 上 Zed dev extension 加载，两个语言高亮，LSP 启动，跳转可用（Zed 已有官方 Windows 版本，仍需实测扩展/路径差异）。

### P3 表单字段校验（约 5–7 个工作块）

- `.per` 索引、表单路径解析、表单上下文跟踪。
- 显式字段引用校验；关联诊断与跳转。
- `.per` 内部一致性检查。
- 语料标注与误报调优；性能优化。
- 验收：50 个模块人工标注，误报 < 5%；基线语料无误报。

### P4 格式化（约 5–7 个工作块）

- 4GL 三阶段格式化；`.per` 小节格式化。
- 幂等/语义保持/错误守卫测试；VSCode/Zed 格式化集成。
- 验收：200 文件语料幂等，格式化后无新增 ERROR，抽样人工 review 通过。

### P5 部署与远程编译（约 3–5 个工作块）

- 配置、CLI、上传（rsync 主 / scp 备）、远程编译、输出解析、诊断映射。
- VSCode 命令/状态栏；Zed 任务模板。
- 验收：Linux 上 rsync 与 scp 两条路径、Windows 上 scp 路径均通过；dry-run、真实服务器编译、失败/超时/部分成功场景。

### P6 打磨与发布（持续）

- 补全、重命名、Code Action、语义高亮。
- 缓存与性能、macOS 兼容与构建。
- 暂不排期：Genero 5.0、GitHub Release/无感下载、市场发布、Zed 注册表 PR。
- 文档完善、兼容矩阵、用户反馈迭代。

---

## 9. 测试与验收

### 9.1 测试层次

| 层次 | 内容 |
|---|---|
| 单元测试 | 解析、作用域、路径解析、字段集合、格式化 token 规则、配置、输出解析 |
| Golden 测试 | 诊断快照、格式化前后快照、引用/定义结果 |
| LSP 协议测试 | 内存/管道 JSON-RPC：initialize、didOpen、definition、formatting、shutdown |
| 语料回归 | 7699 `.4gl` / 7409 `.per` 解析不崩溃；错误数不高于基线 |
| 字段校验评估 | 人工标注 50 模块，统计 TP/FP/FN；历史兼容忽略表 |
| 格式化评估 | 200 文件：幂等、无新增 ERROR、diff 规模统计 |
| 性能 | 索引、打开、诊断、格式化基准；CI 记录趋势 |
| VSCode | `@vscode/test-electron`：激活、启动 server、命令、格式化（Linux + Windows） |
| Zed | `cargo check --target wasm32-wasip2`；手工 dev extension 清单（Linux + Windows） |
| 部署 | mock `ssh`/`rsync`/`scp`/编译输出；Linux 真实服务器；Windows scp 路径 |
| 环境 | Linux/Windows 工具链与构建冒烟，见 [ENVIRONMENT.md](ENVIRONMENT.md) |

### 9.2 验收基线

- 两个 grammar 的解析通过率不低于现有基线（`.per` 7068/7070，`.4gl` 7671/7699）。
- LSP 对 100% 的语料文件不崩溃、不超时；单文件失败不影响其他文件。
- 字段校验：默认配置下，在基线语料上**零误报**（按抽样人工确认）；标注集误报 < 5%。
- 格式化：幂等 100%；格式化后新增 ERROR = 0；不改 LAYOUT 列位置。
- 部署：dry-run 与真实执行均有明确日志；编译错误能回到对应本地文件行；Linux 上 rsync/scp 均验证，Windows 上 scp 验证。
- 发布：VSIX 在 **Linux + Windows** 可安装；Zed dev extension 在 **Linux + Windows** 可加载；Linux/Windows 二进制开箱可用。
- macOS：v1 不列为阻塞验收项；代码保持可移植，v1 后补构建与验证。

---

## 10. 发布与版本（暂不排期）

> 本轮明确：**暂不排发布计划**。以下仅保留版本规范与未来发布的设计约束，不在当前任务中执行。

- **版本号**：LSP 与两个扩展独立 semver；扩展声明兼容的 LSP 最低版本。
- **grammar 版本**：`tree-sitter-bdl` / `tree-sitter-per` 打 tag，LSP `go.mod` 固定；升级需跑语料回归（含 2.50 基线）。
- **当前分发方式**：本地构建 `bdl-lsp`，VSCode/Zed 通过 `bdl.server.path` / `BDL_LSP_PATH` / 工作区路径使用；VSCode 先本地 VSIX。
- **未来发布设计（未排期）**：
  - LSP 二进制由 `bdl-lsp` GitHub Release 发布，tag `lsp-vX.Y.Z`；
  - 扩展从该 Release 无感下载、校验 SHA256、缓存与回退；
  - 版本矩阵：`LSP ↔ VSCode/Zed 扩展 ↔ grammar revision ↔ Genero 2.50`（5.0 未排期）。
- **Zed 发布**：扩展 id 已定为 `4gl-full`；注册表 PR 暂不排期。
- **许可**：新仓库默认 MIT（与 grammar 一致）；正式发布前补 `THIRD_PARTY_NOTICES`。

## 11. 风险与对策

| 风险 | 影响 | 对策 |
|---|---|---|
| CGO 跨平台构建复杂 | 发布受阻 | v1 聚焦 Linux/Windows 原生构建；Windows 用 MinGW-w64；macOS 后续补；必要时 Zig CC |
| grammar ABI/版本漂移 | 运行崩溃 | 固定 go-tree-sitter v0.25.0 与 grammar revision；CI 解析冒烟；升级单独验证 |
| `.4gl` 语料本身有 2980 个 ERROR | 误报/噪声 | 解析错误只报语法；语义校验仅在无错误区域；提供忽略配置 |
| 表单路径/上下文不确定 | 字段误报 | 唯一解析才校验；模块级回退；严格模式可选；忽略列表 |
| `BY NAME` 记录映射复杂 | 误报 | v1 不做；v1.1 用置信度与显式开启 |
| 格式化破坏代码 | 用户不信任 | 保守三阶段、错误守卫、幂等/语义测试、默认仅缩进 |
| Zed 无任意命令 | 部署体验差异 | CLI + `.zed/tasks.json`；LSP command/code action 兜底 |
| Zed/Rust 本机缺失 | 无法本地验证 | 安装 rustup + wasm32-wasip2 + wasi-sdk；CI 持续 `cargo check` |
| 无 `fglcomp`/`fglform` 本机 | 无法验证权威诊断 | 服务器编译作为最终校验；mock 输出解析测试 |
| 15k 文件索引慢/占内存 | 体验差 | 懒解析 `.per`、并行、缓存、忽略规则、性能门槛 |
| 部署安全 | 生产事故 | 显式触发、dry-run、严格 host key、最小权限、日志脱敏 |
| Genero 5.0 语法未覆盖 | 5.0 新代码误报/不支持 | v1 明确 2.50 基线；5.0 差异调研后增量更新 grammar；未知语法只报解析错误 |
| Windows 无 rsync | 上传路径不可用 | 默认 `scp` 回退；文档给出 cwRsync/MSYS2/WSL 可选安装 |
| Zed 的 Windows 行为差异 | Zed 端在 Windows 上路径/进程/终端行为可能不同 | Zed 官方已支持 Windows；v1 在 Windows 实测 dev extension 与 LSP 启动，记录差异 |
| macOS 延后 | 部分用户暂不可用 | 代码避免 Linux 专有依赖；v1 后补 CI/构建/验证 |
| CI Release 权限/误发布 | 分发失败/覆盖旧版本 | tag 保护、Actions 最小权限、校验和、失败不覆盖已有 Release；保留多版本 |
| 与现有 `bdl-plugin` 扩展冲突 | 用户困惑 | 不复用 `bdl` id；新扩展用 `4gl-full`，两者可共存；文档说明功能差异 |
| 与现有 `bdl-plugin` 扩展冲突 | 用户困惑 | 复用 `bdl` id 并说明替换；或新 id（待确认） |

---

## 12. 目录规划

### 12.1 `bdl-lsp`

```text
bdl-lsp/
├── README.md
├── go.mod / go.sum
├── Makefile
├── .gitignore
├── cmd/bdl-lsp/              # main、deploy 子命令
├── internal/
│   ├── server/               # JSON-RPC/LSP 分发与生命周期
│   ├── document/             # 文档存储、增量同步
│   ├── parser/               # parser 池、tree-sitter、查询
│   ├── index/                # 工作区索引、缓存、表单索引
│   ├── bdl/                  # 4GL 符号/作用域/记录
│   ├── per/                  # 表单 item/SCREEN RECORD
│   ├── features/             # definition/references/hover/symbols/...
│   ├── validate/             # 字段存在性、per 校验
│   ├── format/               # 格式化
│   ├── deploy/               # 部署/远程编译
│   ├── config/               # 配置与默认值
│   └── logging/              # 日志
├── queries/                  # .scm（嵌入二进制）
├── testdata/                 # 小型 fixture、golden
├── docs/                     # 中文文档（含 ENVIRONMENT.md）
├── scripts/                  # 语料、性能、发布、环境脚本（dev-env.sh/ps1）
├── artifacts/                # 忽略：中间文件、日志、缓存
└── logs/ -> artifacts/logs   # 约定，不单独提交
```

### 12.2 `vscode-extension`

```text
vscode-extension/
├── package.json
├── tsconfig.json
├── esbuild.mjs
├── src/
│   ├── extension.ts
│   ├── client.ts
│   ├── server/binary.ts      # 查找/下载/校验/版本
│   ├── deploy/commands.ts
│   └── test/
├── syntaxes/bdl.tmLanguage.json
├── syntaxes/per.tmLanguage.json
├── language-configuration/{bdl,per}.json
├── assets/icon.png
├── README.md / CHANGELOG.md / LICENSE
├── .vscodeignore
└── .gitignore
```

### 12.3 `zed-extension`

```text
zed-extension/
├── extension.toml
├── Cargo.toml
├── src/lib.rs
├── languages/bdl/{config.toml,highlights.scm,outline.scm,brackets.scm,injections.scm,indents.scm}
├── languages/per/{config.toml,highlights.scm,outline.scm,brackets.scm,indents.scm}
├── README.md / LICENSE
└── .gitignore
```

---

## 13. 已确认决策

| # | 决定 |
|---|---|
| Q1 | `bdl-lsp` → `git@github.com:darcyjoven/bdl-lsp.git`；`vscode-extension` → `git@github.com:darcyjoven/vscode-bdl.git`；`zed-extension` → `git@github.com:darcyjoven/zed-bdl.git`。三个仓库 `origin` 均已配置，尚未 push。 |
| Q2 | 部署同步以 `ssh + rsync` 为主，`scp` 作为轻量备选/回退；v1 不做纯 Go SFTP。 |
| Q3 | 字段校验默认 `warning`，仅在表单唯一解析成功时报告。 |
| Q4 | **不复用** 现有 `bdl` id；`bdl-plugin` 是轻量语法插件，本插件是全功能版。Zed 扩展 id 定为 **`4gl-full`**，两者可共存。 |
| Q5 | VSCode **先本地 VSIX**；市场发布暂不排期。 |
| Q6 | v1 在 **Windows + Linux** 验证通过；macOS 后续兼容，不作为 v1 阻塞项。 |
| Q7 | 目标体验是**无感自动下载**（主流方式）；但实现暂不排期，开发期先用本地路径/本地二进制。 |
| Q8 | 目标范围 **Genero 2.50–5.0**；当前 `tree-sitter-bdl` 为 **2.50** 基线，**5.0 暂不排期**。 |
| Q9 | LSP 二进制归属 **`bdl-lsp` 自己的 Release**（tag `lsp-vX.Y.Z`）；发布动作暂不排期。 |

### 暂不排期

- Genero 5.0 语法支持。
- GitHub Release / 扩展无感下载 / VSCode 市场发布 / Zed 注册表发布。
- 以上在有明确需求时再排期，不阻塞 v1（2.50 + Linux/Windows 开发闭环）。

## 14. 下一步（先环境与计划，不写功能代码）

1. 按 [ENVIRONMENT.md](ENVIRONMENT.md) 完成 **Linux + Windows** 环境安装与验证，勾选 TODO 中的环境清单。
2. 首次 push 三个仓库：`bdl-lsp`、`vscode-bdl`、`zed-bdl`（需要 GitHub SSH 凭据）。
3. 建立 CI 基线（**只构建/测试，不发布**）：
   - `bdl-lsp`：Linux/Windows CGO 构建、测试、语料冒烟；
   - `vscode-bdl` / `zed-bdl`：客户端 CI。
4. 写 `docs/PLAN-DETAILED-P1.md`：把 P1 拆到包/文件/接口/测试用例，给出每步验收命令。
5. 固化 `scripts/dev-env.sh` / `dev-env.ps1` 与环境检查脚本。
6. 规划确认后再开始 LSP 骨架与编辑器最小客户端代码。

---

## 15. 附：术语

| 术语 | 含义 |
|---|---|
| BDL / 4GL | Genero Business Development Language |
| `.per` | Form Specification 文件 |
| item | `.per` `ATTRIBUTES` 中的表单字段/控件定义 |
| Screen Record | `.per` `INSTRUCTIONS` 中定义的屏幕记录，用于 4GL 数组/记录绑定 |
| LSP | Language Server Protocol |
| CGO | Go 调用 C 代码的机制，本项目用于链接 Tree-sitter parser |
| VSIX | VSCode 扩展包格式 |
| WASM | WebAssembly，Zed 扩展的运行形态 |
