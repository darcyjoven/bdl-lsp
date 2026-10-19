# 待办与待确认

> 更新：2026-10-19  
> 说明：这里是“下一步做什么”的短清单；完整阶段见 [PLAN.md](PLAN.md)。完成后移动/勾选，不删除历史。

---

## 1. 需要用户先确认

- [ ] **Q1 仓库远程**：`github.com/darcyjoven/{bdl-lsp,vscode-extension,zed-extension}`？公开还是私有？何时 push？
- [ ] **Q2 部署方式**：MVP 用系统 `ssh` + `rsync`，还是直接做纯 Go SFTP？
- [ ] **Q3 字段校验**：默认 `warning` 且“表单唯一解析才报”，是否接受？
- [ ] **Q4 Zed 扩展 id**：复用现有 `bdl`（替换 `bdl-plugin`）还是新 id？
- [ ] **Q5 VSCode 发布**：先本地 VSIX，还是一开始就准备市场发布？
- [ ] **Q6 平台范围**：v1 是否必须同时支持 Linux/macOS/Windows？
- [ ] **Q7 LSP 分发**：下载安装（推荐）还是 VSIX 内置？
- [ ] **Q8 Genero 版本**：目标 2.50 / 3.x / 4.x？是否有旧式 `SCREEN` 表单需求？

> 未确认前，按计划中的“建议默认”推进文档与骨架，不阻塞。

---

## 2. P0 剩余技术验证

- [ ] 安装 Rust stable + `wasm32-wasip2` target + wasi-sdk。
- [ ] 创建 Zed 扩展最小骨架：`extension.toml`、两个 language、空 `lib.rs`，`cargo check --target wasm32-wasip2` 通过。
- [ ] 验证 Zed 是否能加载 ABI 15 grammar + BDL scanner（真实 dev extension）。
- [ ] 创建 VSCode 最小客户端：启动一个返回固定 initialize 的 Go server，确认扩展激活/连接。
- [ ] 验证 GitHub Release 二进制下载：平台识别、chmod、`--version`、SHA256。
- [ ] 用真实 `bdl-lsp` 二进制在 VSCode 中打开 `ammr110.4gl`，确认无 stdout 污染、日志可读。
- [ ] 在 `tree-sitter-bdl` / `tree-sitter-per` 中把 Go bindings 的 `go-tree-sitter` 依赖升到 `v0.25.0`，并补“真正解析文件”的测试（ABI 15）。
- [ ] 记录 Zed/VSCode 的 LSP 能力差异（semantic tokens、pull diagnostics、workspace configuration）。

---

## 3. 详细计划（下一步文档）

- [ ] `docs/PLAN-DETAILED-P1.md`
  - [ ] 包/文件清单与接口签名（`internal/server`、`document`、`parser`、`index`）
  - [ ] LSP 方法清单与能力协商字段
  - [ ] 每个任务的验收命令
  - [ ] VSCode 扩展文件级任务
  - [ ] 测试 fixture 清单
- [ ] `docs/PLAN-DETAILED-P3.md`（字段校验）
  - [ ] 表单索引数据结构
  - [ ] 4GL 绑定/字段提取规则表
  - [ ] 诊断/忽略配置
  - [ ] 标注集与评估脚本
- [ ] `docs/FORMATTER.md`（规则表与示例）
- [ ] `docs/DEPLOY.md`（配置 schema、安全、错误处理）
- [ ] `docs/TESTING.md`（测试矩阵、CI 命令）

---

## 4. 工程骨架（P1）

### bdl-lsp

- [ ] `go.mod`：Go 1.24，固定 `go-tree-sitter v0.25.0`、`go.lsp.dev/*`、grammar 模块。
- [ ] `cmd/bdl-lsp/main.go`：`--version`、`--stdio`、`--log-level`、`deploy` 子命令占位。
- [ ] `internal/server`：initialize/shutdown/exit、method 分发、未知方法、panic 恢复。
- [ ] `internal/document`：didOpen/didChange/didClose、UTF-16 行列转换。
- [ ] `internal/parser`：parser 池、双 grammar、查询加载、错误节点统计。
- [ ] `internal/index`：工作区扫描、`FileIndex` 骨架、按需解析 `.per`。
- [ ] `internal/features`：documentSymbol、definition（函数/变量）、hover。
- [ ] 诊断：解析 ERROR/MISSING → `publishDiagnostics`。
- [ ] `scripts/dev-env.sh`、`scripts/corpus-check.sh`、`scripts/build.sh`。
- [ ] `Makefile`：`build`、`test`、`lint`、`corpus`、`release`。
- [ ] CI：Go 1.24、CGO、`go test -race`、语料冒烟、三平台构建。

### vscode-extension

- [ ] `package.json`：languages、grammars、configuration、commands、activationEvents。
- [ ] `src/extension.ts` + `client.ts`：启动 LSP、documentSelector、配置同步。
- [ ] `src/server/binary.ts`：查找/下载/校验/版本检查。
- [ ] `syntaxes/bdl.tmLanguage.json`、`syntaxes/per.tmLanguage.json`（先覆盖关键字/字符串/注释/数字/函数/类型）。
- [ ] `language-configuration/*.json`：注释、括号、自动闭合。
- [ ] 命令：`bdl.restartServer`、`bdl.showOutput`、`bdl.deploy.currentFile`（占位）。
- [ ] 打包：`vsce package` 生成 VSIX。
- [ ] 测试：激活 + server 启动 + 格式化/跳转 smoke。

### zed-extension

- [ ] `extension.toml`：id、grammars（bdl/per）、language_servers。
- [ ] `languages/bdl/*`：从 `bdl-plugin` 迁移/改进 queries。
- [ ] `languages/per/*`：从 `tree-sitter-per/queries` 迁移 + outline/brackets。
- [ ] `src/lib.rs`：`language_server_command`、PATH/工作区/下载三条路径。
- [ ] `Cargo.toml`：`zed_extension_api = "0.7.0"`，`cdylib`。
- [ ] `cargo check --target wasm32-wasip2`。
- [ ] Zed dev extension 手工验证清单。

---

## 5. 功能 Backlog（按优先级）

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

- [ ] 配置 schema + 示例。
- [ ] `bdl-lsp deploy` 子命令：dry-run、上传、编译。
- [ ] 远程输出解析（`file:line: error`）。
- [ ] VSCode 命令/输出通道/状态栏。
- [ ] Zed tasks 模板。
- [ ] 超时/失败/部分成功/回滚策略。

### 增强（P6）

- [ ] 补全（关键字/函数/字段）。
- [ ] 重命名。
- [ ] Code Action（字段不存在时提示、忽略）。
- [ ] 语义高亮（VSCode + Zed）。
- [ ] 数据库 schema 索引。
- [ ] 纯 Go SFTP。
- [ ] 性能缓存与跨平台发布。

---

## 6. 已知风险跟踪

- [ ] ABI 15 ↔ Zed Tree-sitter 运行时兼容性。
- [ ] BDL scanner 在 WASI 下的编译。
- [ ] 15k 文件索引性能。
- [ ] 字段上下文误报率。
- [ ] 无本机 `fglcomp`/`fglform` 导致的部署验证缺口。
- [ ] 沙箱环境（HOME/GOPATH/GOPROXY）对 CI 与文档的影响。

---

## 7. 完成定义（DoD）

一个任务完成需满足：

1. 代码 + 中文文档/注释（对外文档中文，代码注释英文亦可）。
2. 单元测试或语料验证通过。
3. `gofmt`/`go vet`/lint 通过（按仓库）。
4. 相关文档更新（PLAN/TODO/README）。
5. 不把中间产物、日志、缓存提交进 Git。
6. 影响用户行为的变更写明迁移/兼容说明。
