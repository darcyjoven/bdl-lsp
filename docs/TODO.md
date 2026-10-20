# 待办与待确认

> 更新：2026-10-20  
> 当前阶段：**P1 实现中**（Block 1–6 已完成，Block 7 待做）。  
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
- [x] 首次 push 三个仓库（`git push -u origin main`）。
- [ ] `bdl-lsp` GitHub Actions：
  - [ ] Linux + Windows CGO 构建/测试；
  - [ ] 语料冒烟（可手动/定时）；
  - [ ] **不发布 Release**（暂不排期）。

---

## 2. 环境配置（P0，优先）

完整清单、安装命令、常见问题见 [ENVIRONMENT.md](ENVIRONMENT.md)。

### 2.1 Linux（本机，已完成基础安装）

- [x] Go 1.24.4、`CGO_ENABLED=1`、gcc 13.3。
- [x] Node 24.21、pnpm 12.14、tree-sitter CLI 0.26.5。
- [x] Rust 1.99 + `wasm32-wasip2` + wasi-sdk 34（`scripts/install-toolchain.sh`）。
- [x] VSCode CLI 1.142（headless 模式可用）。
- [x] OpenSSH、`rsync`、`scp`、`gh` 2.102。
- [x] `scripts/dev-env.sh` 后 Go 构建缓存指向可写目录。
- [x] `scripts/env-check.sh` 通过（必需项齐全）。
- [x] 冒烟：双 grammar 解析、Go/CGO、`zed_extension_api` wasm 编译。
- [ ] Linux Xvfb（需 root 安装 `xvfb x11-xkb-utils xkb-data`）；当前用 headless 替代。
- [ ] 骨架就绪后跑 `go test`。

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

- [x] `scripts/dev-env.sh`。
- [x] `scripts/dev-env.ps1`。
- [x] `scripts/env-check.sh`、`scripts/env-check.ps1`。
- [x] `scripts/install-toolchain.sh`（无 root 安装）。
- [ ] `scripts/corpus-check.sh`、`scripts/format-corpus.sh`、`scripts/field-check-corpus.sh`（P1/P3 时加）。

---

## 3. P0 剩余技术验证

- [x] `zed_extension_api 0.7.0` + `wasm32-wasip2` 编译验证（`artifacts/spike-rust`，可生成 `.wasm`）。
- [ ] Zed 扩展最小骨架：`extension.toml`（id `4gl-full`）、两个 language、`lib.rs`。
- [ ] 验证 Zed 能加载 ABI 15 grammar + BDL scanner（Linux + Windows dev extension）。
- [ ] VSCode 最小客户端：启动返回固定 initialize 的 Go server，确认激活/连接。
- [ ] 验证本地 `bdl-lsp` 二进制路径启动：`--version`、日志、无 stdout 污染（Linux + Windows）。
- [ ] 用真实 `bdl-lsp` 二进制在 VSCode 中打开 `ammr110.4gl`，确认无 stdout 污染、日志可读。
- [ ] 在 `tree-sitter-bdl` / `tree-sitter-per` 中把 Go bindings 的 `go-tree-sitter` 升到 `v0.25.0`，补“真正解析文件”的测试（ABI 15）。
- [ ] 记录 Zed/VSCode 的 LSP 能力差异（semantic tokens、pull diagnostics、workspace configuration）。
- [x] 本机验证 CGO 构建 + 双 grammar 运行（Linux）。
- [ ] 验证 Windows 下 CGO 构建 + 运行 LSP（可先用 spike）。
- [x] 无桌面 VSCode CLI headless 模式验证（用于 e2e）。

---

## 4. 详细计划（下一步文档）

- [x] `docs/FEATURES.md`（功能基线，含来源标记）
- [x] `docs/PLAN-DETAILED-P1.md`
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
  - [ ] record `.*` 成员校验（最后做，默认关闭）
- [ ] `docs/FORMATTER.md`（规则表与示例）
- [ ] `docs/DEPLOY.md`（配置 schema、rsync/scp、安全、错误处理）
- [ ] `docs/TESTING.md`（测试矩阵、Linux/Windows 命令）
- [ ] `docs/GENERO-VERSIONS.md`（2.50 基线；5.0 暂不排期）

---

## 5. 工程骨架（P1）

### bdl-lsp

Block 1 已完成：

- [x] `go.mod`：Go 1.24，`go-tree-sitter v0.25.0`、`go.lsp.dev/*`；grammar 暂用 `replace`。
- [x] `main.go`：`serve` / `check` / `version` 子命令，`--log-level` / `--log-file`。
- [x] `internal/server`：initialize/shutdown/exit、method 分发、未知方法、stdio。
- [x] `internal/document`：Store/Document/LineIndex（didChange 暂为整文替换）。
- [x] `internal/parser`：parser 池、双 grammar、ABI 校验、ERROR/MISSING 收集。
- [x] 诊断：解析 ERROR/MISSING → `publishDiagnostics`（UTF-16 换算）。
- [x] `internal/check`：`check` CLI（文本 / `--json` / `-q`）。
- [x] `Makefile`：`build`/`test`/`test-race`/`vet`/`lint`/`check`/`corpus`。
- [x] 测试：parser 8 个、document 5 个、server 6 个（协议级）+ 2 个 benchmark。
- [x] `scripts/corpus-smoke.sh`：语料抽样冒烟。

Block 2 已完成：

- [x] `internal/textpos`：UTF-16 ↔ 字节 ↔ tree-sitter Point 换算。
- [x] `internal/document`：`Change` / `ApplyChanges` / `Tree.Edit` / Store。
- [x] didChange 增量编辑（含多段、整文替换、非法区间钳制）。
- [x] 旧树生命周期修正（修复 `ts_tree_delete` 双重释放 SIGSEGV）。
- [x] `exit` 后进程挂起修正 + LSP 退出码语义。
- [x] `scripts/lsp-bench.py` + `make bench`。
- [x] 测试增至 36 个；`go test -race` 通过。

Block 3 已完成：

- [x] `internal/index`：BDL 与 PER 符号提取（函数/报表/记录/字段/参数/表单 item/SCREEN RECORD）。
- [x] `textDocument/documentSymbol`、`workspace/symbol`、`textDocument/hover`。
- [x] 索引缓存（按 URI+版本失效）。
- [x] 大小写不敏感匹配；光标取词不依赖语法树。

Block 4 已完成：

- [x] `textDocument/definition`：局部 → 参数 → 全局 → `rec.field` 记录字段。
- [x] `textDocument/references`：词边界 + 排除注释/字符串 + includeDeclaration。
- [x] 修复光标落在词首/词中时 `rec.field` 判定失败。

Block 5 已完成：

- [x] `internal/config`：generoVersion、诊断级别、limits、工作区配置、客户端热更新。
- [x] 诊断增强：级别可配、去重、同行合并、诊断 code、大文件提示。
- [x] 解析超时（C 层）与 ctx 检查；修复 `ParseCtx` 崩溃。
- [x] `scripts/corpus-regress.sh` + `make regress`。
- [x] 全量回归：per 7407/7409、bdl 7683/7699；18 个失败均为已知文件问题。

Block 6 已完成（VSCode 客户端）：

- [x] `package.json`：`bdl`/`per` 语言、10 项配置、3 条命令、激活事件。
- [x] `src/server/binary.ts`：设置 > `BDL_LSP_PATH` > 工作区 > PATH 四级查找，失败给可操作提示。
- [x] `src/client.ts`：启动 LSP、documentSelector、配置同步、追踪级别、重启。
- [x] `src/extension.ts`：激活、命令注册、失败只提示一次。
- [x] `scripts/generate-tm-grammars.mjs`：从 grammar 源码生成 TextMate 语法（361 + 76 个关键字全分类）。
- [x] `scripts/verify-tm-grammars.mjs`：41 项高亮切分校验（含多词短语、注释/字符串屏蔽）。
- [x] `language-configuration/{bdl,per}.json`：注释、括号、自动闭合、缩进。
- [x] `vsce package`：VSIX 111 KB / 10 个文件。
- [x] e2e：真实 VSCode 扩展宿主 14 个用例全通过。
- [x] 修复服务器不识别 `--stdio`（客户端会自动追加）。
- [x] `scripts/e2e-vscode.sh` + `make e2e-vscode`。

待做：

- [ ] 工作区扫描（当前 `workspace/symbol`、`references` 只覆盖已打开文档）。
- [ ] 跨文件跳转（genero 的 `GLOBALS` / 导入）。
- [ ] `RECORD LIKE table.*` 的 schema 索引（P6）。
- [x] 配置 `bdl.generoVersion`（默认 2.50，Block 5 实现，Block 6 在客户端暴露）。
- [ ] GitHub Actions：Linux + Windows 构建/测试（不发布 Release）。
- [ ] 为 grammar 打 tag，去掉 `replace`。

### vscode-extension（→ `vscode-bdl`）

Block 6 已完成：

- [x] `package.json`：languages、grammars、configuration、commands、activationEvents。
- [x] `src/extension.ts` + `client.ts`：启动 LSP、documentSelector、配置同步。
- [x] `src/server/binary.ts`：**本地路径解析**（设置/环境变量/工作区/PATH）；自动下载/校验/回退暂不排期。
- [x] `syntaxes/bdl.tmLanguage.json`、`syntaxes/per.tmLanguage.json`（关键字/字符串/注释/数字/函数/类型/字段访问/预处理）。
- [x] `language-configuration/*.json`：注释、括号、自动闭合、缩进规则。
- [x] 命令：`bdl.restartServer`、`bdl.showServerInfo`、`bdl.showOutput`。
- [x] 打包：`vsce package` 生成 VSIX（本地）。
- [x] 测试：Linux 下 14 个 e2e 用例（激活/server 启动/诊断/跳转/引用/大纲/悬停/配置热更新）。
- [ ] Windows 下 e2e（Block 7 CI 覆盖）。
- [ ] 命令 `bdl.deploy.currentFile`（P5，占位未实现）。
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
- [ ] record `.*` 成员校验（`INPUT BY NAME rec.*` / `DISPLAY BY NAME rec.*`），最后做，默认关闭。
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
