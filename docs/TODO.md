# 待办与待确认

> 更新：2026-10-19  
> 当前阶段：**规划与环境准备，不写功能代码**。  
> 完整阶段见 [PLAN.md](PLAN.md)，环境细节见 [ENVIRONMENT.md](ENVIRONMENT.md)。完成后勾选，不删除历史。

---

## 0. 已确认决策

| # | 决定 |
|---|---|
| Q1 | `bdl-lsp` → `git@github.com:darcyjoven/bdl-lsp.git`；`vscode-extension` → `vscode-bdl`；`zed-extension` → `zed-bdl`。三个 `origin` 已配置。 |
| Q2 | 部署同步：`ssh + rsync` 为主，`scp` 为轻量备选/回退；v1 不做纯 Go SFTP。 |
| Q3 | 字段校验默认 `warning`，只在表单唯一解析成功时报告。 |
| Q4 | **不复用** `bdl` id；`bdl-plugin` 保持轻量，本插件是全功能版。Zed 扩展 id 定为 **`4gl-full`**，两者共存。 |
| Q5 | VSCode **先本地 VSIX**；市场发布暂不排期。 |
| Q6 | v1 在 **Windows + Linux** 验证通过；macOS 后续兼容。 |
| Q7 | 目标体验是**无感自动下载**；实现暂不排期，开发期先用本地路径。 |
| Q8 | 目标范围 **Genero 2.50–5.0**；当前 grammar 为 **2.50** 基线，**5.0 暂不排期**。 |
| Q9 | LSP 二进制归属 **`bdl-lsp` 自己的 Release**（tag `lsp-vX.Y.Z`）；发布动作暂不排期。 |

### 暂不排期

- Genero 5.0 语法支持。
- GitHub Release / 无感自动下载 / VSCode 市场发布 / Zed 注册表发布。
- 以上不阻塞 v1（2.50 + Linux/Windows 开发闭环）。

---

## 1. Git 与远程

- [x] 三个仓库 `git init -b main` 并完成首次提交。
- [x] `bdl-lsp` 配置 `origin` → `git@github.com:darcyjoven/bdl-lsp.git`。
- [x] `vscode-extension` 配置 `origin` → `git@github.com:darcyjoven/vscode-bdl.git`。
- [x] `zed-extension` 配置 `origin` → `git@github.com:darcyjoven/zed-bdl.git`。
- [x] 配置 GitHub SSH key（`ssh -T git@github.com` 已验证）。
- [ ] 首次 push 三个仓库（`git push -u origin main`）。
- [ ] `bdl-lsp` GitHub Actions：
  - [ ] Linux + Windows CGO 构建/测试；
  - [ ] 语料冒烟（可手动/定时）；
  - [ ] **不发布 Release**（暂不排期）。

---

## 2. 环境配置（P0，优先）

完整清单、安装命令、常见问题见 [ENVIRONMENT.md](ENVIRONMENT.md)。

### 2.1 Linux（本机）

- [ ] Go 1.24+、`CGO_ENABLED=1`、gcc。
- [ ] Node 24、pnpm、tree-sitter CLI 0.26.5。
- [ ] Rust stable + `wasm32-wasip2` + wasi-sdk。
- [ ] VSCode + CLI + `xvfb`（e2e）。
- [ ] OpenSSH client、`rsync`、`scp`、`gh` CLI。
- [ ] `bash bdl-lsp/scripts/dev-env.sh` 后 Go 构建缓存指向可写目录。
- [ ] 跑通 `go test`/grammar 解析冒烟（骨架就绪后）。

### 2.2 Windows（v1 必验）

- [ ] Go 1.24+ + MinGW-w64 gcc，`where gcc` 正确。
- [ ] `CGO_ENABLED=1`、`CC=gcc` 下能构建最小 CGO 程序。
- [ ] Node 24、pnpm、Git for Windows、tree-sitter CLI。
- [ ] VSCode + CLI。
- [ ] OpenSSH Client：`ssh -V`、`scp -V` 可用。
- [ ] rsync 可选（cwRsync/MSYS2/WSL）；v1 不强依赖。
- [ ] Rust + `wasm32-wasip2` + wasi-sdk。
- [ ] Zed Windows 官方版安装 + dev extension 加载。
- [ ] `git config --global core.autocrlf false`；`.4gl`/`.per` 保持 LF。
- [ ] 用 `scp` 连通测试服务器（或 dry-run 记录命令）。

### 2.3 账号/服务器

- [x] GitHub SSH key 已验证；`gh` 可选（发布暂不排期）。
- [ ] 测试服务器：Genero、`fglform`/`fglcomp`、SSH key 登录、`remoteRoot` 样例。
- [ ] 记录本地路径 ↔ 远程路径映射（Windows 盘符也要覆盖）。
- [ ] 不在仓库/配置样例中写密码、私钥、生产主机。

### 2.4 环境脚本

- [ ] `scripts/dev-env.sh`（已有，补 Windows 说明）。
- [ ] `scripts/dev-env.ps1`。
- [ ] `scripts/env-check.sh`、`scripts/env-check.ps1`。
- [ ] `scripts/corpus-check.sh`、`scripts/format-corpus.sh`、`scripts/field-check-corpus.sh`。
- [ ] `scripts/release-check.sh`（本地检查 tag/版本/校验和，CI 为主）。

---

## 3. P0 剩余技术验证

- [ ] Zed 扩展最小骨架：`extension.toml`（id `4gl-full`）、两个 language、空 `lib.rs`，`cargo check --target wasm32-wasip2` 通过。
- [ ] 验证 Zed 能加载 ABI 15 grammar + BDL scanner（Linux + Windows dev extension）。
- [ ] VSCode 最小客户端：启动返回固定 initialize 的 Go server，确认激活/连接。
- [ ] 验证本地 `bdl-lsp` 二进制路径启动：`--version`、日志、无 stdout 污染（Linux + Windows）。
- [ ] 用真实 `bdl-lsp` 二进制在 VSCode 中打开 `ammr110.4gl`，确认无 stdout 污染、日志可读。
- [ ] 在 `tree-sitter-bdl` / `tree-sitter-per` 中把 Go bindings 的 `go-tree-sitter` 升到 `v0.25.0`，补“真正解析文件”的测试（ABI 15）。
- [ ] 记录 Zed/VSCode 的 LSP 能力差异（semantic tokens、pull diagnostics、workspace configuration）。
- [ ] 验证 Windows 下 CGO 构建 + 运行 LSP（可先用 spike）。

---

## 4. 详细计划（下一步文档，先不写代码）

- [ ] `docs/PLAN-DETAILED-P1.md`
  - [ ] 包/文件清单与接口签名（`internal/server`、`document`、`parser`、`index`）
  - [ ] LSP 方法清单与能力协商字段
  - [ ] 每个任务的验收命令
  - [ ] VSCode 扩展文件级任务（本地路径优先；自动下载暂不排期）
  - [ ] 测试 fixture 清单
  - [ ] Windows 构建/验证步骤
- [ ] `docs/PLAN-DETAILED-P3.md`（字段校验）
  - [ ] 表单索引数据结构
  - [ ] 4GL 绑定/字段提取规则表
  - [ ] 诊断/忽略配置
  - [ ] 标注集与评估脚本
- [ ] `docs/FORMATTER.md`（规则表与示例）
- [ ] `docs/DEPLOY.md`（配置 schema、rsync/scp、安全、错误处理）
- [ ] `docs/TESTING.md`（测试矩阵、Linux/Windows 命令）
- [ ] `docs/GENERO-VERSIONS.md`（2.50 基线；5.0 暂不排期）

---

## 5. 工程骨架（P1）

### bdl-lsp

- [ ] `go.mod`：Go 1.24，固定 `go-tree-sitter v0.25.0`、`go.lsp.dev/*`、grammar 模块。
- [ ] `cmd/bdl-lsp/main.go`：`--version`、`--stdio`、`--log-level`、`deploy` 子命令占位。
- [ ] `internal/server`：initialize/shutdown/exit、method 分发、未知方法、panic 恢复。
- [ ] `internal/document`：didOpen/didChange/didClose、UTF-16 行列转换。
- [ ] `internal/parser`：parser 池、双 grammar、查询加载、错误节点统计。
- [ ] `internal/index`：工作区扫描、`FileIndex` 骨架、按需解析 `.per`。
- [ ] `internal/features`：documentSymbol、definition（函数/变量）、hover。
- [ ] 诊断：解析 ERROR/MISSING → `publishDiagnostics`。
- [ ] 配置：`bdl.generoVersion`（默认 2.50）。
- [ ] `Makefile`：`build`、`test`、`lint`、`corpus`、`release`。
- [ ] GitHub Actions：Linux + Windows 构建/测试（不发布 Release）。
- [ ] 本地产物：Linux amd64 + Windows amd64（v1）。

### vscode-extension（→ `vscode-bdl`）

- [ ] `package.json`：languages、grammars、configuration、commands、activationEvents。
- [ ] `src/extension.ts` + `client.ts`：启动 LSP、documentSelector、配置同步。
- [ ] `src/server/binary.ts`：**本地路径解析**（设置/环境变量/工作区）；自动下载/校验/回退暂不排期。
- [ ] `syntaxes/bdl.tmLanguage.json`、`syntaxes/per.tmLanguage.json`（先覆盖关键字/字符串/注释/数字/函数/类型）。
- [ ] `language-configuration/*.json`：注释、括号、自动闭合。
- [ ] 命令：`bdl.restartServer`、`bdl.showOutput`、`bdl.deploy.currentFile`（占位）。
- [ ] 打包：`vsce package` 生成 VSIX（先本地）。
- [ ] 测试：Linux + Windows 激活、server 启动、格式化/跳转 smoke。
- [ ] GitHub Actions：Linux + Windows 构建/测试。

### zed-extension（→ `zed-bdl`，id `4gl-full`）

- [ ] `extension.toml`：id `4gl-full`、grammars（bdl/per）、language_servers。
- [ ] `languages/bdl/*`：从 `bdl-plugin` 迁移/改进 queries（不共享 id）。
- [ ] `languages/per/*`：从 `tree-sitter-per/queries` 迁移 + outline/brackets。
- [ ] `src/lib.rs`：`language_server_command`；PATH/工作区/本地路径（下载暂不排期）。
- [ ] `Cargo.toml`：`zed_extension_api = "0.7.0"`，`cdylib`。
- [ ] `cargo check --target wasm32-wasip2`。
- [ ] Zed dev extension 手工验证清单（Linux + Windows）。
- [ ] GitHub Actions：wasm check。

---

## 6. 功能 Backlog（按优先级）

### 字段校验（P3）

- [ ] `.per` 索引：item 名、类型、绑定列、PHANTOM、定义位置。
- [ ] `SCREEN RECORD` 解析与字段集合。
- [ ] 4GL 表单绑定：`WITH FORM`、`OPEN FORM`、`DISPLAY FORM`。
- [ ] 字段引用：`ON` 列表、`BEFORE/AFTER FIELD`、`NEXT FIELD`、`INFIELD`、`DISPLAY TO`。
- [ ] 表单路径解析（`42f` → `per`）与多命中策略。
- [ ] 诊断 + relatedInformation + 忽略配置。
- [ ] 评估脚本与 50 模块标注。
- [ ] `.per` 内部校验：重复 item、布局 tag 未定义、SCREEN RECORD 引用不存在。

### 格式化（P4）

- [ ] 4GL 缩进（块结构表）。
- [ ] 关键字大小写/空白/逗号规则。
- [ ] `.per` 小节格式化（LAYOUT 不动）。
- [ ] 幂等/语义保持/ERROR 守卫测试。
- [ ] Format on save 集成。

### 部署（P5）

- [ ] 配置 schema + 示例（含 transport、include/exclude、delete=false）。
- [ ] `bdl-lsp deploy` 子命令：dry-run、rsync 主路径、scp 回退。
- [ ] Windows scp 逐文件上传与错误聚合。
- [ ] 远程输出解析（`file:line: error`）。
- [ ] VSCode 命令/输出通道/状态栏。
- [ ] Zed tasks 模板。
- [ ] 超时/失败/部分成功/回滚策略。
- [ ] 行尾/路径/大小写检查。

### 版本与增强（P6）

- [ ] 补全（关键字/函数/字段）。
- [ ] 重命名。
- [ ] Code Action（字段不存在时提示、忽略）。
- [ ] 语义高亮（VSCode + Zed）。
- [ ] 数据库 schema 索引。
- [ ] 纯 Go SFTP（仅在确有需求时评估）。
- [ ] 性能缓存与 macOS 兼容（5.0 未排期）。

---

## 7. 已知风险跟踪

- [ ] ABI 15 ↔ Zed Tree-sitter 运行时兼容性。
- [ ] BDL scanner 在 WASI 下的编译。
- [ ] 15k 文件索引性能。
- [ ] 字段上下文误报率。
- [ ] 无本机 `fglcomp`/`fglform` 导致的部署验证缺口。
- [ ] 扩展与 LSP 版本错配；回退策略。
- [ ] Windows 无 rsync → scp 回退的稳定性/性能。
- [ ] Zed Windows 行为差异（路径、进程、扩展加载）。
- [ ] macOS 延后带来的构建差异。
- [ ] 沙箱环境（HOME/GOPATH/GOPROXY）对文档/CI 的影响。

---

## 8. 完成定义（DoD）

一个任务完成需满足：

1. 代码 + 中文文档/注释（对外文档中文，代码注释英文亦可）。
2. 单元测试或语料验证通过。
3. `gofmt`/`go vet`/lint 通过（按仓库）。
4. 相关文档更新（PLAN/TODO/README/ENVIRONMENT）。
5. 不把中间产物、日志、缓存提交进 Git。
6. 影响用户行为的变更写明迁移/兼容说明。
7. 涉及平台的功能至少记录 Linux/Windows 验证结果；macOS 单独标注。
8. 涉及 Genero 版本的行为标注 2.50 基线（5.0 未排期）。
