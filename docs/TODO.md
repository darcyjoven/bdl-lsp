# 待办与待确认

> 更新：2026-10-21（第二轮：纳入 P7）  
> 当前阶段：**P1–P6 全部功能已完成**（Block 1–7 / Zed / GLOBALS / 字段校验 / 格式化 /
> 部署 / 补全 / 重命名 / Code Action / 语义高亮 / record 成员校验）。  
> **下一步：P7**（缺陷修复 · 内置符号 · 链接资料索引 · 诊断级别统一），
> 见 [PLAN-P7.md](PLAN-P7.md)。  
> 完整阶段见 [PLAN.md](PLAN.md)，环境细节见 [ENVIRONMENT.md](ENVIRONMENT.md)。完成后勾选，不删除历史。

---

> **新会话接手先读 [HANDOVER.md](HANDOVER.md)**（状态 / 基线 / 坑 / 下一步）。

---

## ★ 下一轮立即处理（P7-A）

> 2026-10-21 Windows 实测发现。完整根因分析与修法见 [PLAN-P7.md](PLAN-P7.md) §2。

### A-1 Zed 大纲：同一条 `DEFINE` 的多个变量变成层级

- [ ] 复现断言：`zed-extension/tests/verify-queries.mjs` 增加「所有 `@item` 的
      (start,end) 两两不同」检查，先在旧 query 上确认**失败**。
- [ ] 修 `zed-extension/languages/bdl/outline.scm`：`define` / `type` / `constant`
      三处把 `@item` 从整条语句改到名字节点（`(define name: (identifier) @name @item)`）。
- [ ] 断言转绿；`verify-queries.mjs` 全通过。
- [ ] 手工验证（需 GUI）：`DEFINE l_name LIKE type_file.chr20, l_sql STRING`
      在 Zed 大纲中 `l_name`、`l_sql` **同级**。

> 根因：grammar 里一条 `DEFINE` 是**单节点多 `name:` 字段**，现 query 产出多个
> `@item` 区间完全重合的 match，Zed 按区间建父子关系 → 误判为层级。
> VSCode 正常是因为 `internal/server/symbols.go` 走 `index.FileIndex`，本来就拆平了。

### A-2 VSCode 下 GLOBALS 文件路径识别不到（Zed 正常）

- [ ] Windows 实机抓两个客户端的 `initialize` 入参，确认 `rootUri` 形态
      （预期 VSCode `file:///c%3A/...`、Zed `file:///c:/...`）。
- [ ] 新建 `internal/fileuri`：`ToPath` / `FromPath`，全量百分号解码 + 盘符/UNC 归一。
- [ ] 单测覆盖 7 条用例（`%3A`、`%23`、`%20`、非 ASCII、UNC、大小写盘符、往返）。
- [ ] 替换 `internal/globals.PathOfURI` 与 `internal/config.uriToPath` 两处**重复实现**。
- [ ] `initialize` 的 `rootUri` 增加 `workspaceFolders[0].uri` → `rootPath` 回退。
- [ ] `go test -race ./...` 全绿；Windows 实机复验 `axmt620.4gl` 无 `globals-file-not-found`。

> 根因：`PathOfURI` 只剥 `file://` 与 `%20`，不解析 `%3A`。
> 实测 `file:///c%3A/work/proj` → `/c%3A/work/proj`（非法路径）→
> `s.rootPath`、`.bdl-lsp/config.json`、文档相对路径全线失效。

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
- [x] `bdl-lsp` GitHub Actions：
  - [x] Linux + Windows CGO 构建/测试（`.github/workflows/build.yml`）；
  - [x] 语料冒烟（CI 只跑 `make check`；全量 `make regress` 留本地按需）；
  - [x] **不发布 Release**（暂不排期）——已明确写进 workflow 注释。

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

- [ ] 工作区扫描（当前 `workspace/symbol`、`references` 只覆盖已打开文档）；
      P3a 已做**按需**加载 `.global` 文件（GLOBALS 导入），全量扫描与 P3b 共同评估。
- [x] 跨文件跳转（genero 的 `GLOBALS` / 导入）✅ P3a 已完成，
      见 [PLAN-P3A-GLOBALS.md](PLAN-P3A-GLOBALS.md)。
- [ ] `RECORD LIKE table.*` 的 schema 索引（P6）。
- [x] 配置 `bdl.generoVersion`（默认 2.50，Block 5 实现，Block 6 在客户端暴露）。
- [x] GitHub Actions：Linux + Windows 构建/测试（`.github/workflows/build.yml`，不发布 Release）。
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
- [x] GitHub Actions：Linux 构建/测试 + VSIX 打包（`.github/workflows/build.yml`）。

Block 7 已完成（CI 与健壮性）：

- [x] `scripts/verify-vscode-manifest.mjs`：22 项清单/语言配置校验，`make manifest-check`。
- [x] 修复 `language-configuration/per.json` 不是合法 JSON（`[` 自动闭合对缺 `"close":`）。
- [x] 修复 `language-configuration/bdl.json` 缩进规则用了 JS 不支持的 `(?i)` → 改 `flags: "i"`。
- [x] e2e 增至 14 个用例，新增「扩展名映射」与「语言配置生效」两组。
- [x] `test/runTest.js` 跨平台化：`Code.exe` 探测、`taskkill` 清理、Linux 专属参数按平台裁剪。
- [x] `scripts/e2e-vscode.ps1`：Windows 版 e2e 入口。
- [ ] Windows 下 e2e 实测（代码已就绪，等 Windows 机器跑一次）。
- [ ] 命令 `bdl.deploy.currentFile`（P5，占位未实现）。

### zed-extension（→ `zed-bdl`，id `4gl-full`）

Block P2 已完成：

- [x] `extension.toml`：id `4gl-full`、schema_version 1、grammars（bdl/per，锁 SHA）、
      language_servers（`Genero BDL`/`Genero Form` + language_ids → `bdl`/`per`）。
- [x] `languages/bdl/*`：config.toml、highlights（300 关键字全覆盖）、outline、
      brackets、indents、injections（sql_body → sql）。
- [x] `languages/per/*`：config.toml、highlights（76 关键字全覆盖）、outline、
      brackets、indents（**刻意不动 LAYOUT**）。
- [x] `src/lib.rs`：`language_server_command` 四级查找（设置 > `BDL_LSP_PATH` >
      工作区 `.bdl-lsp/bin` > PATH），失败给可操作提示；透传 settings。
- [x] `Cargo.toml`：`zed_extension_api = "0.7.0"`，`cdylib`，release 优化。
- [x] `cargo check/build --target wasm32-wasip2`：0 warning，产物 185 KB。
- [x] `tests/verify-manifest.py`：14 项清单/交叉引用校验。
- [x] `tests/verify-queries.mjs`：38 项 query 校验（节点名/字面量/编译/命中/
      关键字覆盖/真实语料抽样）。
- [x] GitHub Actions：wasm check + manifest + queries 三个 job。
- [ ] Zed dev extension 手工加载验证（Linux + Windows，本机无图形界面）。

### P2 踩坑记录（写进 query 注释与校验脚本）

| 现象 | 根因 | 修法 |
|---|---|---|
| `highlights.scm` 整份不生效 | query 里写了 grammar 没有的 token（`<>` `!=` `>=` `<=` `\|\|` `**` `{` `}`） | 以 `src/node-types.json` 的匿名 token 为准逐个核对 |
| `Impossible pattern` 编译失败 | `.per` 的 `menu_group.name` 实际是 `identifier \| 关键字` 联合，绑 `(name)` 非法 | 改绑 `(identifier)` |
| `constant` 的名字匹配不到 | 名字来自隐藏规则 `_constant_statement`，没有可直接绑的字段 | 用子节点匹配 `(constant (identifier) @name)` |
| 校验脚本误报大量"未知字面量" | 去注释时没跳过字符串内部的 `;`，正则跨行乱配 | 扫描时跟踪字符串状态 |
| 校验脚本报 `_` 是未知节点 | `_` 是 tree-sitter 通配符，不在 node-types 里 | 显式跳过 |
| `.per` 样例 8 个语法错误 | 按真实 Genero 写的 `KEY F1 ... ACTION ...`、`PAGE HEADER` 放 `OUTPUT` 里，grammar 不支持 | 按 grammar 实际规则重写样例 |

---

## 6. 功能 Backlog（按优先级）

### GLOBALS 模组全局变量（P3a ✅ 已完成 2026-10-20）

> 用户明确要求（2026-10-20）。完整设计与实现记录：
> [PLAN-P3A-GLOBALS.md](PLAN-P3A-GLOBALS.md)（§10 实现差异、§11 验收输出）。

- [x] T1 索引 GLOBALS 块：新增 `ScopeGlobals`；`GlobalsImport` 提取
      （含变量名形式）。验收：真实 `top.global` 索引出 274 个符号。
- [x] T2 `internal/globals` 解析器：相对路径（基准=引用文件目录）+
      `searchPaths`；按需加载 + mtime 缓存；禁 basename 隐式匹配；
      链式解析防环。验收：语料 7756 文件 8403 Resolved / 107 NotFound
      （与规划脚本数字吻合）；环/链/缓存/内存版优先各有单测。
- [x] T3 诊断 `globals-file-not-found`（默认 warning，可 off）；
      反向依赖刷新（编辑/打开 `.global` → 刷新引用方，含 NotFound
      登记候选位置的场景）。`relatedInformation` 未做（诊断结构改造
      与 P3b 共用，本轮单文件目标已够）。
- [x] T4 跳转四级作用域（新增 GLOBALS 级）+ 悬停来源标注
      （“来源：xxx.global:行号”）；定义方文件内悬停标注“模组全局变量”。
- [x] T5 诊断 `variable-source-unresolved`（默认 warning，可 off）：
      tree-sitter Query 只取 `variable` 首标识符、跳过 `call.name`；
      存在 Unresolvable 导入时全部放过。缓存 + 400ms 防抖（按键路径
      6.8ms，全量重算 27ms 只在停顿后跑）。
      `scripts/globals-audit.sh` 标注**未做**（语料 107 处缺失均为
      真实文件缺失，单测覆盖保守规则，人工标注留待真实使用反馈）。
- [x] T6 配置三项（`fileNotFoundDiagnostics` / `unresolvedSourceDiagnostics` /
      `searchPaths`）+ VSCode `package.json`（10→13 项）+ 客户端同步 +
      Zed 文档示例；e2e 14 → **16** 用例全通过。

### 字段校验（P3b ✅ 已完成 2026-10-20）

> 完整实现记录：[PLAN-P3B-FIELD.md](PLAN-P3B-FIELD.md)。

- [x] 复用 P3a 的跨文件索引模式（internal/form 与 internal/globals 平行结构）。
- [x] `.per` 索引：item 名、类型、PHANTOM、定义位置（既有）+
      LayoutRefs（新增，供内部校验）；修复 PHANTOM 与 SCREEN RECORD
      裸字段名提取（名字在 field_ref 里，原实现取不到）。
- [x] `SCREEN RECORD` 解析与字段集合（含 table.column 与裸名两种形态）。
- [x] 4GL 表单绑定：`WITH FORM`、`OPEN FORM` + `DISPLAY FORM`（命名绑定）、
      按函数隔离的作用域跟踪（`(function/main/report) @container`）。
- [x] 字段引用：`BEFORE/AFTER FIELD`、`ON CHANGE`（branch）、`NEXT FIELD`（hook）、
      `INPUT FROM`、`CONSTRUCT ON`、`DISPLAY TO`、`INFIELD()`——单 query 多 pattern。
- [x] 表单路径解析：R1a（../per/，语料 94%）、R1b、R1c（perRoots）、
      R2（basename 唯一）、R3（模块段消歧）；歧义放弃校验（Q3）。
- [x] 诊断 `field-not-in-form` + relatedInformation 指向 .per 文件 +
      配置 `fieldDiagnostics`（默认 warning，可 off）。
- [x] 评估：`cmd/field-audit` 全量 7699 文件 → 1725 条发现；
      自动标注（模块段一致性）可疑率 **2.8% < 5%**；人工抽查确认真阳性
      （语料存在 .4gl/.per 版本漂移，如 aapr106 的 apa18 不在 aapt110.per）。
- [x] `.per` 内部校验：重复 item、布局 tag 未定义、SCREEN RECORD 引用不存在
      （三个诊断码，共用 fieldDiagnostics 级别）。
- [ ] 忽略列表（按文件/字段/表单 glob）——未做；`off` 可整体关闭兜底，
      精细忽略待真实使用反馈再排期。
- [ ] 50 模块人工标注——以自动标注（模块一致性 97.2%）+ 抽样人工复核替代；
      完整人工标注待真实用户环境。

### 格式化（P4 ✅ 已完成 2026-10-21）

> 完整实现记录：[PLAN-P4-FORMAT.md](PLAN-P4-FORMAT.md)。

- [x] `.4gl` 缩进（块结构表）：按祖先链上块节点算深度；行首是
      `END/ELSE/CATCH` 回退一级；同一行开多个块（`DEFINE x RECORD`）
      只贡献一级缩进。
- [x] 关键字大小写：只改匿名 token，`upper`/`lower`/`unchanged` 可选；
      跳过 `&` 开头的预处理指令（`&INCLUDE` 大小写敏感，改了会破坏解析）。
- [x] 缩进字符与宽度：`indentStyle` auto/tab/space、`indentSize` 0=跟随编辑器；
      优先级：显式配置 > 编辑器请求（`insertSpaces`/`tabSize`）> 4 空格兜底。
- [x] **`.per` 不做任何格式化**（用户明确要求）；`.per` 请求直接返回空。
- [x] 空白/逗号/运算符**不改**——只改关键字与行首缩进，避免全量 diff。
- [x] 幂等/ERROR 守卫测试：`internal/format` 18 个单测、`internal/server`
      8 个协议级测试；`cmd/fmt-audit` 全语料 7699 文件
      → 成功 7683、拒绝 16（语法错误）、幂等失败 **0**、新增 ERROR **0**。
- [x] 含语法错误的文件整体拒绝格式化（`tree.HasError()`），返回原文。
- [x] Format on save：VSCode 通过 `contributes.configurationDefaults` 把
      `[bdl].editor.defaultFormatter` 设为本扩展，用户只需打开
      `editor.formatOnSave`（不替用户强制开启）；Zed 用 `format_on_save`。
- [ ] 空白/逗号/运算符规范化——不做：全量 diff 风险高、收益低。

### 部署（P5 ✅ CLI + VSCode 已完成 2026-10-21）

> 完整实现记录：[PLAN-P5-DEPLOY.md](PLAN-P5-DEPLOY.md)。

- [x] 配置 + 示例（`examples/deploy.json`）：targets/upload/compile、
      include/exclude、`mode`、`fallback`、`delete=false`、`strictHostKey`。
      未知字段直接报错（避免拼错字段名静默部署到错目录）。
- [x] `bdl-lsp deploy` 子命令：`-dry-run`、`-target`、`-list`、`-json`、
      `-no-compile`、`-no-upload`、可只部署指定文件。
- [x] rsync 主路径（`--delay-updates`、`--files-from` 避免命令行过长）。
- [x] scp 逐文件上传 + 失败聚合（"部分成功"不显示成成功）。
- [x] rsync 缺失/失败 → scp **显式**回退（摘要与 JSON 都标注，不静默降级）。
- [x] 远程输出解析：`file:line[:col]: error: -code: msg`，映射回本地路径。
- [x] 行尾检查（CRLF 上传前告警）、远程路径 POSIX 校验、越界路径拒绝。
- [x] 退出码区分：0 成功 / 1 部署失败 / 2 配置或参数错误。
- [x] 实机验证：本机 sshd 上 rsync、scp、回退、单文件四条路径全部通过。
- [x] VSCode：4 条命令（部署/当前文件/预演/列目标）、输出通道、
      诊断发布到 Problems、`bdl.deploy.target` 设置。
- [x] Zed tasks 模板：[DEPLOY-ZED.md](DEPLOY-ZED.md) 给出 `task: spawn`
      模板与 PATH 问题说明；Zed README 已链接。
- [ ] Windows scp 实测（代码路径已就绪，需在 Windows 跑一次）。
- [ ] 单文件远程编译超时；编译产物（`.42m`）是否拉回待确认。

### 版本与增强（P6 ✅ 功能已完成）

#### 补全（✅ 已完成 2026-10-21）

> 实现记录：[PLAN-P6-COMPLETION.md](PLAN-P6-COMPLETION.md)。

- [x] 关键字补全：清单由 `scripts/generate-keywords.mjs` 从 grammar 源码抽取
      （BDL 361 / PER 76），`make lint` 会校验不漂移（改 grammar 忘了重生成 → 失败）。
- [x] 同文件符号补全：函数、报表、MAIN、变量、常量、TYPE、记录、字段、参数。
- [x] 跨文件补全：其它已打开文档里的函数/报表/main（不做全工作区索引，见决策）。
- [x] **表单字段补全**：复用 P3b 的 `Resolver.FormFieldsAt`，按光标位置取当前
      表单（绑定之前的补全不给字段）。
- [x] `.` 之后只给字段/成员，不给关键字。
- [x] 注释/字符串里不补全（用语法树判定，字符串里的 `--` 也能正确处理）。
- [x] `completionItem/resolve` 已声明（部分客户端不支持时会不显示文档字段）。
- [ ] 记录成员按类型精确推导——不做：Genero 弱类型，收益低于复杂度。
- [ ] 全工作区符号索引——需后台索引 + 持久化缓存（PLAN §7.6），当前只看已打开文档。

#### 重命名（✅ 已完成 2026-10-21）

> 实现记录：[PLAN-P6-RENAME.md](PLAN-P6-RENAME.md)。

- [x] `textDocument/prepareRename` + `textDocument/rename`。
- [x] 范围与 references 一致：**只改当前文件**（跨文件需工作区索引，
      漏改比不支持更糟）。
- [x] 七道闸门：可解析 / 不在注释字符串 / 是本文件符号 / 新名合法 /
      新名不撞名 / 新名不是关键字。
- [x] prepareRename 拒绝时**报错**（客户端不弹输入框），而不是返回空编辑。
- [x] 关键字与"已声明符号"的检查顺序修正：`DEFINE first INTEGER` 里
      `first` 虽是关键字但也是合法符号，应当能改。
- [ ] 跨文件重命名——需工作区索引。

#### Code Action（✅ 已完成 2026-10-21）

> 实现记录：[PLAN-P6-CODEACTION.md](PLAN-P6-CODEACTION.md)。

- [x] `textDocument/codeAction`，只声明 quickfix。
- [x] 字段拼错 → 「改为 %q」：候选来自光标处真实表单（复用 P3b），
      编辑距离阈值 len/3（夹 1..4）内才提，首个标 isPreferred。
- [x] `.per` 布局 tag / SCREEN RECORD 引用不存在 → 同样的「改为 %q」。
- [x] 只处理 source=bdl-lsp 的诊断（部署的 fglcomp 诊断不碰）。
- [x] 刻意不做：自动加 item 到表单（列宽/schema 无法推断）、创建空 .per、
      删除字段引用（破坏性）、重复 item 修复（语义不明）。理由写进代码注释。
- [ ] 批量修复（fix all）——未排期。

#### 语义高亮（✅ 已完成 2026-10-21）

> 实现记录：[PLAN-P6-SEMTOK.md](PLAN-P6-SEMTOK.md)。

- [x] `textDocument/semanticTokens/full`，legend 6 类型 / 3 修饰位。
- [x] **只标注用户自定义符号**（函数/变量/常量/类型/字段/参数），
      不发关键字、字符串、注释、数字——那些 TextMate 已经做得很准，
      再报一遍会抢同一批 token，客户端合并规则不一致时反而更差。
- [x] GLOBALS 块内符号带 `static`；常量带 `readonly`；定义处带 `declaration`。
- [x] 相对编码 + UTF-16 长度（有单测解码后断言严格升序）。
- [x] Zed 侧不消费 LSP semantic tokens（走 `highlights.scm`），已在文档说明。
- [ ] 使用处的语义着色——未排期（需全文件符号解析，成本高收益低）。

#### record `.*` / `BY NAME` 成员校验（✅ 已完成 2026-10-21，默认 off）

> 实现记录：[PLAN-P6-RECORDMEMBER.md](PLAN-P6-RECORDMEMBER.md)。

- [x] `BY NAME rec.member` 的成员存在性校验（`INPUT`/`DISPLAY`/`CONSTRUCT`）。
- [x] `rec.*` 通配**不校验**：语法树里没有 property 节点（`*` 被 grammar 吞掉），
      语义上也不存在"某成员是否存在"的问题。
- [x] 作用域按函数隔离 + 模块级回退；类型成员表带容器前缀
      （不同作用域的同名记录不能互相覆盖）。
- [x] 五类"不可校验"按 opaque 处理：子记录组、`RECORD LIKE table.*`、
      多变量 DEFINE 错配、参数遮蔽、数组内置方法。
- [x] **默认 off**（`recordMemberDiagnostics`）：`LIKE table.*` 占语料 87% 的引用，
      静态看不到列名，硬开启会大面积误报。
- [x] 语料评估 `cmd/record-audit`：全量 7683 文件 →
      可校验 281160 处、报出 157 处（**0.1%**），抽查确认为真阳性。
- [ ] **上游待修**：`internal/index` 的 `collectDefines`在多变量 DEFINE 中会
      错配名字与类型节点（本轮用 TypeText 前缀检查兜底）。应单独修复并加测试。
- [ ] `LIKE table.*` 的成员校验——需数据库 schema 索引。

#### 其余 P6 项

- [ ] record `.*` 成员校验（`INPUT BY NAME rec.*` / `DISPLAY BY NAME rec.*`），最后做，默认关闭。
- [ ] 语义高亮（VSCode + Zed）。
- [ ] 数据库 schema 索引。
- [ ] 纯 Go SFTP（仅在确有需求时评估）。
- [ ] 性能缓存与 macOS 兼容（5.0 未排期）。

---

## 6.1 P7 Backlog（2026-10-21 立项）

> 完整设计：[PLAN-P7.md](PLAN-P7.md)。执行顺序 P7-A → B → C → D → E。  
> 立项依据（实测 24 个 `.4gl`）：诊断 1733 条，其中 `variable-source-unresolved`
> **1717 条（99.1%）**。噪声已高到不可用，P7 是**可用性修复期**而非增值期。

### P7-B 诊断级别统一 + 调用点误报修正

- [ ] `internal/diag/registry.go`：诊断码集中注册（Code/Default/Description/Category），
      级别解析统一走 `registry.Severity(code)`，删除散落的 level 判断。
- [ ] 新增统一配置 `bdl.diagnostics.<code>`，优先级
      `diagnostics.<code>` > 旧键（`syntaxDiagnostics` 等）> 注册表默认。
- [ ] 旧配置键保留并标注 deprecated（至少两个版本不删）。
- [ ] 拼错保护：`diagnostics` 里出现未注册的码 → 记 warn（现在静默失效）。
- [ ] `make diagnostics-check`：Go 注册表 ↔ `docs/DIAGNOSTICS.md` ↔ VSCode enum 三处一致。
- [ ] **修调用点误报**：`IF fn() THEN` / `LET x = fn(1)` 的函数名被当成变量检查。
      query 增加 `(expression name: (variable (identifier) @skip))`；语料对比 skip 前后差异。
- [ ] 新增 `function-not-found`（无链接资料时不报）。
- [ ] 复测：24 文件诊断 1717 → 新基线（预期 < 400）。
- [x] ~~Q10 待确认~~ → **已定稿：全部默认 `warning`**（用户 2026-10-21）。
      `syntax-error` 也从 `error` 降为 `warning`；旧键 `syntaxDiagnostics` 默认值同步改，
      避免新旧配置行为不一致。`record-member-unknown` 仍默认 `off`。

### P7-C 内置函数与变量（用户提问已答复：分两类）

> 判据：`tree-sitter parse` 有 ERROR → **grammar 缺口，改 grammar**；
> 无 ERROR 但语义未知 → **插件侧符号表**。实测 `STATUS`/`SQLCA`/`ARG_VAL`/
> `fgl_getenv`/`SET_COUNT` 都能正常解析（语义类）；`STARTLOG` 报 3 个 ERROR（语法类）。

- [ ] `internal/builtin`：内置变量 / 内置函数 / 内置类与方法 三类符号表（Genero 2.50 基线）。
- [ ] `cmd/builtin-audit`：扫语料提取"未定义且不在表里"的标识符，语料驱动补齐，门槛 <20 条未知。
- [ ] 接入三处：① `variable-source-unresolved` 白名单 ② 补全 ③ 悬停显示签名。
- [ ] 复测：24 文件 `variable-source-unresolved` 再降（预期 < 150）。
- [ ] **grammar 缺口清单**（`tree-sitter-bdl`）：`cmd/grammar-audit` 收集 ERROR 上下文按频次排序；
      已知 `STARTLOG`（2 个文件）`fgl_report_configure`。低频项只留档不排期。
- [ ] 注意：`cl_null` / `cl_user` / `cl_gr_numfmt` **不是内置函数**，是 `lib/`/`sub/` 的用户函数
      （已实测 `lib/4gl/cl_null.4gl:16`），归 P7-D，**不要**加进内置表。

### P7-D 程序链接资料索引（大项）

> 用户核心澄清：**链接只放大"函数"可见性，不放大"变量"可见性**。
> GLOBALS 变量只认当前文件定义/引入，不跨链接 —— `internal/globals` 逻辑一行不改。
>
> **真实数据已到位**：`link.csv`，529042 B，sha256 `3ca6ff10…`，**15587 行**。
> 全部设计已按实测值定稿，见 [PLAN-P7.md](PLAN-P7.md) §5.3 / §11。

- [ ] 数据模型：**索引主键 = MAIN 程序代码**（不是程序编号）。
      实测：组内恰好 1 个 MAIN = 5374/5849（91.9%）；0 个 = 81；≥2 个 = 5；
      另外 **160 个组的 `GAL01` 不在自己组内**，证实"编号≠文件名"。
- [ ] 文档结构（**Q12 定稿**）：`<项目根>/.link/links.jsonl`（**不是** `.bdl-lsp/link/`）
      + `links.meta.json`。实测单文件 **415 KB**（剔除隐式组后），读取 10–20 ms。
- [ ] 分片触发条件（Q11）：体积 > 8 MB / 解析 > 200 ms / 明细 > 200k / 索引重建 > 500 ms，
      任一满足才切 `shards/`；首版不预先分片。
- [ ] 导入校验：`gal04='Y'` 过滤（实测 51 行被过滤）；0 MAIN → orphan（81 组）；
      ≥2 MAIN → ambiguous（5 组）；重复代码去重；**CSV 必须按 `utf-8-sig` 读**（带 BOM）。
- [ ] 路径解析：`bdl.link.moduleRoots`（默认 `["tiptop","topcust"]`，自动探测）；
      模组目录**大小写不敏感**匹配（实测数据 92 个模组全大写，磁盘全小写）。
- [ ] 缺失文件不报错：实测 **800 条 / 700 个代码**找不到（集中在语料中不存在的
      `WPC`/`WSUB`/`WMB`/`WZZ`/`WAG`/`AZZ`/`WLIB`），计入 meta `missingFiles`。
- [x] ~~可见性矩阵~~ → **Q14 二次定稿：扁平规则**。`sub`/`lib`/`qry` **对所有程序默认可用，
      包括它们自己**，无方向限制。配置项 `bdl.link.globalModules = ["sub","lib","qry"]`。
      原 `lib→sub` 11 处反例**自动消解**（是合法代码），`allowModuleDeps` 白名单**删除**。
- [ ] 采集 **S1 纯 Go 直连**（**唯一内置方案**）：`github.com/sijms/go-ora/v2`（已实测模块可获取）；
      密码走环境变量/`.link/secrets.json`(0600)/交互输入，不明文落盘；
      进程内定时（`interval`）+ `onStart` + 指数退避；主动运行三入口（CLI/VSCode 命令/Zed task）。
- [x] ~~采集 S1a 本地 Oracle 驱动~~ → **Q15 三度定稿：取消**。纯 Go 无法直调 Instant Client，
      走 CGO 会让构建/分发/跨平台复杂化；只保留 `go-ora`。不留 build tag、不留 `driver` 配置。
- [ ] 采集 **S2 服务器脚本导出**：`scripts/fetch-gal-file.sh`（`read -s` 不走 history，
      `SET MARKUP CSV ON` 直出 CSV，已按真实表结构写好 `gal04='Y'` 过滤）+ `bdl-lsp link import`。
- [ ] 采集 **S3 命令执行**：只配驱动位置，凭证**走 stdin**（不进命令行参数，避免 `ps` 泄露）。
- [ ] 函数可见性：`局部 → 本文件 → GLOBALS → 链接文件 → 全局模组(sub/lib/qry)`；变量可见性**不变**。
- [ ] 诊断：`function-not-found` / `link-data-invalid` / `link-data-stale`。
- [x] ~~`link-file-missing`~~ → **不做**（Q2 答复）。链接有、代码无 → 忽略，零诊断；
      但记录**必须保留在 `links.jsonl`**。
- [ ] 性能（按实测分布）：链 p50=1 文件、p90=2 → 典型链 2–10 ms **同步**即可；
      **仅当链文件数 > 20** 才走后台（最大链 `cbmp100` 364 文件约 0.7–1.8 s）。
      全局模组涉及 983 文件（sub 518 + lib 204 + qry 261），只建函数名索引，后台一次性 2–4 s。
      首版仅内存缓存。
- [ ] `cmd/link-audit`：用真实 `link.csv` 复跑，核对 §11 的数字
      （链 5846 / orphan 81 / ambiguous 5 / missingFiles 800 / 过滤 51）。
- [ ] **R2 反例测试**：两个链接在一起的子文件各自 `DEFINE` 同名变量但无共享 GLOBALS
      → 仍报未定义（防止把变量也放进链接可见性）。
- [x] ~~Q14 待确认~~ → **已定稿：扁平规则**（sub/lib/qry 对所有程序可用，含自己）。
      全局模组**参与** `function-not-found` 判定（否则 `cl_*` 仍大面积误报）。

### P7-E 链接资料 UI

> LSP `documentSymbol` **无法跨文件跳转**（`DocumentSymbol` 无 URI/target 字段，
> 跨文件只能用扁平的 `SymbolInformation`，而它不支持层级）；Zed `outline.scm` 只能引用当前语法树。

- [ ] VSCode 自定义 **TreeView**「链接资料」：三级
      `程序名 → {直链子文件 | 隐含模组 → 模组下文件}`，显示为 `APM/apmp500`，
      点击 `vscode.open` 跳到 `apm/4gl/sapmp500.4gl`。
- [ ] LSP 自定义请求 `bdl/linkTree` 供视图取数。
- [ ] Zed 降级：`documentSymbol` 末尾追加链接条目（**可见不可跳**），文档明示限制。
- [ ] 无链接资料时显示引导文案，不报错。
- [x] ~~Q13 待确认~~ → **已定稿：默认空白 + 可点击「反查引用程序」**（用户 2026-10-21）。
      反查异步、**先显示数量再分页展开**（实测 `aws_xml` 被 324 组引用，不能一次铺开）。

### P7 新增配置项（约 12 项）

```jsonc
"bdl.diagnostics": { "*": "warning", "record-member-unknown": "off" }
"bdl.builtins.enable": true
"bdl.link.enable" / "dataDir"(".link") / "moduleRoots" / "maxAgeDays"
"bdl.link.globalModules": ["sub", "lib", "qry"]   // 全局可见模组
"bdl.link.sync": { enable / source / interval / onStart
                   db{...} / command{...} }
```

- [ ] VSCode `package.json` 32 项 → 约 40 项，`make manifest-check` 通过。
- [ ] 安装时**提示**把 `.link/` 加入 `.gitignore`（不擅自改用户文件）。

### P7 专项风险

- [ ] Windows URI 根因判断可能偏差 → 上机先打印两个客户端 `initialize` 入参再动手。
- [ ] `expression name:` 跳过规则可能误伤 → 先跑语料差异清单人工抽查。
- [ ] 链接资料含生产库信息 → `.link/` 建议 gitignore，文档写明不要提交。
- [ ] 纯 Go 驱动 `go-ora` 依赖 +2–3 MB 二进制（当前 11 MB）；
      本地驱动方案走 build tag `ora-native`，默认不编译。
- [x] ~~本地 Oracle 驱动路径配错~~ → 已取消该方案，无此风险。

### P7 真实数据验证（2026-10-21 已完成）

- [x] 分析 `link.csv`（529042 B / sha256 `3ca6ff10…` / 15587 行）。
- [x] 结论已回写 [PLAN-P7.md](PLAN-P7.md) §5.3 / §5.3.2 / §5.6.1 / §11。
- [x] 关键修正：体积 1–2 MB → **415 KB**；隐含模组 980 → **983**；
      典型链解析 100–300 ms → **2–10 ms**（p50=1 文件）。
- [x] ~~发现待确认项：`lib→sub` 11 处反例~~ → 已随 Q14 改为扁平规则而消解。
- [ ] 待做：`cmd/link-audit` 落地后，用同一份 CSV 复跑并核对数字一致。


---

## 7. 已知风险跟踪

- [ ] ABI 15 ↔ Zed Tree-sitter 运行时兼容性。
- [ ] BDL scanner 在 WASI 下的编译。
- [ ] 15k 文件索引性能。
- [ ] 字段上下文误报率（P3b）。
- [ ] P3a 变量来源校验误报率：保守规则已落地（只查 variable 首标识符、
      Unresolvable 导入时全部放过），`scripts/globals-audit.sh` 人工标注
      **未做**，留待真实使用反馈；用户可 off 兜底。
- [x] P3a 同名 `.global` 解析歧义——已禁止隐式 basename 匹配，
      只走 R2（引用文件目录）/R3（searchPaths）显式规则。
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
