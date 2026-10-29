# 功能清单（bdl-lsp）

> 面向开发者，详细到每个功能，可快速浏览 / 当验收表用；本文件是 bdl-lsp 的**唯一功能文档**，
> 等价于全量总清单 `vscode-bdl/docs/FEATURES.md` 的 **bdl-lsp 部分**。
>
> | 项目 | 值 |
> |---|---|
> | 版本 | `bdl-lsp v0.3.14`（配套 `tiptop v0.1.8` / `vscode-bdl v0.6.2`） |
> | 生成日期 | 2026-10-24 |
> | 完整版 | <https://github.com/darcyjoven/vscode-bdl/blob/main/docs/FEATURES.md> |
> | tiptop 切片 | <https://github.com/darcyjoven/tiptop/blob/main/docs/FEATURES.md> |
> | 权威细节 | 诊断码见 [`DIAGNOSTICS.md`](DIAGNOSTICS.md)（`make diagnostics-write` 生成，构建输入） |

> **维护规则**：待办（`vscode-bdl/docs/TODO.md`）完成后，把新功能补进本文对应条目；
> 用户可见的新功能还要同步更新 `vscode-bdl/README.md`（唯一用户引导）与对应组件的功能文档。


## 0. 职责与边界

| 做 | 不做 |
|---|---|
| 解析、诊断、跳转、补全、格式化；字段 / 表单 / GLOBALS 校验；跨文件函数可见性 | 不采集资料（`link`/`schema` 采集在 tiptop）、不部署（`deploy` 在 tiptop） |

- 交付物：`bdl-lsp` 二进制（stdio LSP + `check` CLI）。
- 与 tiptop **只通过文件系统交互**：只读 `<项目>/.project/{link/,schema/}`（旧布局 `.project` 根 / `.link/` 只读兼容）。
- 配置入口：编辑器 LSP `settings`（VSC 侧 47 键）与项目级 `<项目>/.bdl-lsp/config.json`。

---

## 1. 语言与解析

| # | 功能 | 说明 / 入口 |
|---|---|---|
| A1 | 双语言支持 | `.4gl` / `.global` → language id `bdl`；`.per` → `per` |
| A2 | tree-sitter 解析 | `tree-sitter-bdl` / `tree-sitter-per` grammar；Go cgo；go-tree-sitter v0.25.0（grammar ABI 15） |
| A3 | 文档同步（真增量） | `didOpen` / `didChange` / `didClose`；`TextDocumentSyncKindIncremental` |
| A4 | UTF-16 ↔ 字节位置换算 | `internal/textpos` |
| A5 | 语法错误诊断 | `ERROR` → `syntax-error`；`MISSING` → `missing-<kind>`（兜底 `missing-node`） |
| A6 | 解析资源上限 | `bdl.limits.maxFileBytes` / `maxFileLines` / `maxIssues` / `parseTimeoutMs`；超限 → `file-too-large` / `parse-timeout` |
| A7b | 链接资料 Code Lens | `textDocument/codeLens`（挂在 MAIN 行；数据来自链接资料内存树；上限 `bdl.link.lensMax`）；点击命令 `bdl.openLink` → 反向 `window/showDocument` |
| A7 | 强制解析当前文件 | 自定义请求 `bdl/forceParse`（编辑器标题栏按钮，上下文 `bdl.parseSkipped`）；登记后立即回执，**后台不限时**解析（`Timeout 0`，不再受 `limits.parseTimeoutMs` 限制）；成功发诊断 + `bdl/parseDone` 回执（含耗时/字节数/版本），客户端据此刷新大纲 |
| A8 | 解析检查 CLI | `bdl-lsp check [--json] [--max-issues N] [-q] <文件...>` |
| A9 | Genero 版本基线 | `bdl.generoVersion`（默认 `2.50`）；5.0 暂不排期 |

---

## 2. 编辑体验（LSP 服务端能力）

| # | 功能 | 说明 / 入口 |
|---|---|---|
| B1 | 悬停（hover） | 变量来源、定义位置、函数信息；`LIKE t.col` 追加字段资料；悬停表名给表摘要 |
| B2 | 跳转定义（definition） | 当前文件、GLOBALS、链接资料内文件 |
| B3 | 查找引用（references） | 工作区可解析引用 |
| B4 | 文档符号（大纲 / 面包屑） | `documentSymbol`，`internal/server/symbols.go`；MAIN 程序块的 `detail` 为 `MAIN`——VSCode 客户端据此判定「链接」按钮可用性 |
| B5 | 工作区符号 | `workspace/symbol`；后台全工作区索引 + 持久化快照，覆盖未打开文件 |
| B6 | 补全 | 关键字、变量、函数、表单字段、表名、列；`.` 触发记录成员/表单字段；`Ctrl+Space` 手动触发 |
| B7 | 重命名 | `prepare` + `rename`；**只改当前文件**、大小写不敏感、不动字符串 |
| B8 | 快速修复（Code Action） | 仅 `quickfix`：字段名写错时的改名建议 |
| B9 | 语义高亮 | `semanticTokens/full`；只发用户自定义符号（关键字/字符串交给 TextMate）；函数 `declaration`、常量 `readonly`、GLOBALS 块 `static` |
| B10 | 格式化 | `.4gl` / `.global`；`bdl.format.keywords`（unchanged/upper/lower，`lower` 同时作用于关键字补全）+ `indentStyle` + `indentSize`；安全守卫：有 `ERROR` 不格式化、幂等；**`.per` 一律不格式化** |
| B11 | 游标 / PREPARE 符号 | `PREPARE` / `DECLARE CURSOR` 声明；`EXECUTE`/`FETCH`/`OPEN`/`CLOSE`/`FOREACH` 悬停与跳转；接入大纲/补全/语义高亮 |

---

## 3. 符号与作用域

| # | 功能 | 说明 / 入口 |
|---|---|---|
| C1 | 按函数隔离作用域 | 变量跟踪以函数为界（整文件线性跟踪会跨函数泄漏出大量误报） |
| C2 | GLOBALS 模组全局变量 | 跨文件跳转到 `.global` 定义、悬停标注来源；`globals-file-not-found`、`variable-source-unresolved` 两类警告（可关） |
| C3 | GLOBALS 按文档隔离 | 符号查找/来源检查/函数可见性只看**当前文档**引入的 `.global`；文档关闭或删导入行即清缓存 |
| C4 | GLOBALS 解析根 | `bdl.globals.searchPaths`（相对工作区根） |
| C5 | 内置符号表 | 97 条内置变量/函数/类/常量（`builtin/data/builtins.json`，`go:embed`）；豁免 `variable-source-unresolved` 与 `function-not-found`；接入补全与悬停 |
| C6 | 内置符号三层配置 | ① 内置 JSON ② 项目覆盖 `<root>/.bdl-lsp/builtins.json`（自动发现，2s 节流热更新）③ `bdl.builtins.enable/paths/disable` |

---

## 4. 诊断与校验

### 4.1 诊断码（14 个静态码 + 动态 `missing-<kind>`）

| 诊断码 | 默认级别 | 触发条件 |
|---|---|---|
| `syntax-error` | warning | grammar `ERROR` 节点 |
| `missing-node` | warning | grammar `MISSING` 节点整簇兜底（缺 `END IF`、逗号等） |
| `file-too-large` | warning | 超过 `bdl.limits.maxFileBytes` / `maxFileLines` |
| `parse-timeout` | warning | 超过 `bdl.limits.parseTimeoutMs`（提示文案给出的就是该键；强制解析后不限时） |
| `globals-file-not-found` | warning | `GLOBALS "路径"` 目标文件找不到 |
| `variable-source-unresolved` | warning | 变量使用处查不到任何定义（局部/模块级/GLOBALS 均无） |
| `field-not-in-form` | warning | `INPUT … FROM`、`AFTER FIELD` 等引用字段不在对应 `.per` |
| `form-duplicate-item` | warning | `.per` `ATTRIBUTES` 重复定义同名 item |
| `form-layout-tag-undefined` | warning | `.per` `LAYOUT` 引用未定义 tag |
| `form-screen-record-undefined` | warning | `.per` `SCREEN RECORD` 引用未定义 item/字段 |
| `record-member-unknown` | **off** | `BY NAME rec.member` 成员不在 `RECORD` 声明里；`RECORD LIKE t.*` 用字段资料展开 |
| `function-not-found` | warning | 调用函数在当前模块及可用模组找不到定义（**需链接资料**） |
| `link-data-invalid` | warning | 链接资料 JSONL 解析失败 / schema 版本过高 |
| `link-data-legacy-dir` | hint | 资料仍在旧目录 `.link/`；`bdl.migrateLinkDir` 可一键迁移 |

> `missing-<kind>` 是动态码：`bdl.diagnostics.missing-<kind>` 精确命中单个类型，`missing-node` 兜底。

### 4.2 诊断控制与质量

| # | 功能 | 说明 / 入口 |
|---|---|---|
| D1 | 级别唯一入口 | `bdl.diagnostics.<code>`：`off`/`error`/`warning`/`information`/`hint`；未列出用默认值；旧专用键已删除 |
| D2 | 忽略列表 | `bdl.ignore.files`（屏蔽该文件所有诊断并排除出索引）、`bdl.ignore.fields`（只屏蔽 `field-not-in-form`）、`bdl.ignore.forms`；glob 支持 `**`，配置热更新 |
| D3 | 诊断降噪 | 作用域按函数隔离；变量 query 跳过表达式函数名 / `WHENEVER` 目标 / 含点号表单字段；`.per` 布局单元格多 tag 与 `SCREEN RECORD` 字段名修正 |
| D4 | 诊断关联信息 | `field-not-in-form` 带 `relatedInformation` 跳转 `.per` 对应位置 |
| D5 | 诊断码表防漂移 | `make diagnostics-check` 校验注册表 ↔ `docs/DIAGNOSTICS.md` ↔ VSC `package.json`；`make diagnostics-write` 生成文档；并入 `make lint` |
| D6 | 诊断度量 | `cmd/diag-audit` + `make diag-audit`：逐码计数、高频标识符、噪声最高文件 |
| D7 | 外部函数过滤（配合 tiptop） | 被 ≥3 文件或 ≥5 次调用且全语料无定义的名字视为存在，记入 `links.meta.json`；候选 >2000 个则不启用 |

---

## 5. 跨文件资料：链接资料（读取侧）

> 采集与写侧在 tiptop（`tiptop link import|sync|status`）；bdl-lsp 只读。

| # | 功能 | 说明 / 入口 |
|---|---|---|
| F10 | 资料读取与热重载 | 只读 `.project/link/`（`bdl.link.dataDir`；旧布局回退），按 mtime 热重载（2s 节流），导入后无需重启编辑器 |
| F11 | 旧目录兼容与迁移 | 仍读旧 `.link/` 并发 `link-data-legacy-dir`（hint）；命令 `bdl.migrateLinkDir` 可一键迁移（客户端提供入口） |
| F12 | 跨文件函数可见性 | 可见函数 = 本文件 ∪ GLOBALS ∪ 链接文件 ∪ 全局模组（`sub`/`lib`/`qry` 扁平规则）；**只放大函数，变量可见性不变** |
| F13 | `function-not-found` 启用条件 | 必须有链接资料且索引预热完成；资料缺失/链内文件不在磁盘/索引未就绪 → 一律不报（宁漏勿误） |
| F14 | 资料自身诊断 | `link-data-invalid`（JSONL 坏了 → 停用并提示重导）；诊断落在资料文件上，不污染源码 |
| F15 | 链接资料数据服务 | 自定义请求 `bdl/linkTree`、`bdl/linkReferences`（反查默认不查、分页 50） |
| F15a | **链接资料 Code Lens**（PLAN Z-P3-3） | `textDocument/codeLens`：把链内**子文件各一条** lens 挂在 **MAIN 程序块**那一行（标题 `模组/代码`，命令 `bdl.openLink` → 服务端反向请求 `window/showDocument` 打开目标文件）；文件不在磁盘时给纯文本 lens（无命令）；不给当前文件自己挂；上限 `bdl.link.lensMax`（默认 10，超出显示前 N 条 + 一条纯文本汇总）。Zed 没有树视图 API，这是链接资料的"就地可点"形态；VSCode 侧仍以 TreeView 为主，Code Lens 作为补充视图共用同一份内存数据 |
| F16 | 链接资料配置 | `bdl.link.enable` / `dataDir` / `moduleRoots` / `globalModules` / `lensMax`（Code Lens 上限，默认 10） |
| F17 | 外部函数过滤 | 见 D7；结果写入 `links.meta.json`，读取时据此豁免 |
| F18 | 性能 | 最大链组装 3.4 ms；工作区索引 7756 文件 24.7 s、增量 22 ms |

产物格式（只读）：

```text
<项目根>/.project/link/
├── links.jsonl      # 每行一条链：程序 → 链内文件/函数
└── links.meta.json  # 生成时间、partial 记录、外部函数表
```

---

## 6. 数据库字段资料（读取侧）

> 采集与写侧在 tiptop（`tiptop schema import|sync|status`）；产物 `<项目>/.project/schema/`。

| # | 功能 | 说明 / 入口 |
|---|---|---|
| G9 | 表文件按需加载 | 只读总索引；某表文件展开时才加载并缓存（几千张表也不卡） |
| G10 | 字段悬停扩展 | `DEFINE x LIKE t.col` → 追加 `t.col · 类型(长度) · 中文说明`；`RECORD LIKE t.*` → 列出该表字段（超 20 行折叠）；悬停表名 → 表摘要 |
| G11 | 记录成员校验 | `record-member-unknown`（默认 off）；`RECORD LIKE t.*` 用字段资料展开成员 |
| G12 | 列补全 | `r.` 补全带每列类型与中文说明 |
| G13 | 字段资料数据服务 | 自定义请求 `bdl/schemaTables`（正则过滤 + `total/matched/returned`）、`bdl/schemaColumns`（展开才取） |
| G14 | 字段资料配置 | `bdl.schema.enable` / `bdl.schema.dataDir`（默认 `.project/schema`）；旧「扫各模组 `sch/*.sch`」与 `bdl.schema.paths` 已下线 |

产物格式（只读）：

```text
<项目根>/.project/schema/
├── tables.json             # 索引：importedAt / source / rowsTotal / 每表列数与文件名
└── tables/<大写表名>.json   # 列清单：name/desc/type/length/charLength/precision/scale/nullable/default
```

---

## 7. 部署（已移到 tiptop）

自 `bdl-lsp 0.3.2` 起，**部署、链接资料采集、字段资料采集都不在 bdl-lsp 内**，由
[tiptop](https://github.com/darcyjoven/tiptop) 提供（`tiptop deploy` / `link` / `schema`）。
bdl-lsp 侧只保留读取与校验相关能力。

---

## 8. 编辑器集成（客户端在 vscode-bdl / zed-bdl）

LSP 服务端能力由编辑器客户端消费：

- VSCode：`vscode-bdl`（30 条命令、2 个 TreeView、47 项设置、进度通知）；本文件只描述服务端协议。
- Zed：`zed-bdl` 的 `4gl-full` **0.2.0**（追平中；配置经 `initializationOptions` 下发、语义高亮规则文件、9 条只读 slash command、`.zed` 工程生成脚本、自动下载）；TreeView 用 **Code Lens** 替代（服务端已实现，点击行为待 Zed 实机确认）。
- 自定义请求/通知：`bdl/forceParse`、`bdl/linkTree`、`bdl/linkReferences`、`bdl/schemaTables`、`bdl/schemaColumns`、`bdl/parseDone`（服务端→客户端通知）。
- **标准命令通道** `workspace/executeCommand`（PLAN Z-P3-2）：`initialize` 声明 `executeCommandProvider`，命令为 `bdl.linkTree` / `bdl.linkReferences` / `bdl.schemaTables` / `bdl.schemaColumns` / `bdl.forceParse` / `bdl.openLink`（Code Lens 点击用），参数是位置数组（`[uri]` / `[query]` / `[query, limit]` / `[table]`），**返回结构与对应自定义请求完全一致**。Zed 扩展 API 发不了自定义请求，只能走这条；VSCode 侧继续用自定义请求，不受影响。

---

## 9. CLI 参考

| 子命令 | 说明 |
|---|---|
| `bdl-lsp serve [--log-level <级别>]` | 以 stdio 启动语言服务（编辑器调用；`--stdio` 兼容） |
| `bdl-lsp check [--json] [--max-issues N] [-q] [--version] <文件...>` | 批量解析检查；`--json` 结构化输出，`-q` 只列有问题的文件 |
| `bdl-lsp version` | 打印版本 |

---

## 10. 配置（bdl-lsp 读取的键）

| 组 | 键 | 默认 |
|---|---|---|
| 基础 | `bdl.server.path` / `bdl.tools.path` / `bdl.server.logLevel` / `bdl.trace.server` | 空 / 空 / `info` / `off` |
| 基础 | `bdl.generoVersion` | `2.50` |
| 基础 | `bdl.diagnostics` | `{}`（逐码调级，唯一入口） |
| 基础 | `bdl.format.keywords` / `indentStyle` / `indentSize` | `unchanged` / `auto` / `0`（`keywords=lower` 也让补全关键字小写） |
| 基础 | `bdl.index.enable` | `true` |
| 基础 | `bdl.globals.searchPaths` / `bdl.perRoots` | `[]` / `[]` |
| 基础 | `bdl.builtins.enable` / `paths` / `disable` | `true` / `[]` / `[]` |
| 基础 | `bdl.ignore.files` / `fields` / `forms` | `[]` / `[]` / `[]` |
| 基础 | `bdl.limits.maxFileBytes` / `maxFileLines` / `maxIssues` / `parseTimeoutMs` | `2097152` / `20000` / `200` / `2000` |
| 链接资料 | `bdl.link.enable` / `dataDir` / `moduleRoots` / `globalModules` | `false` / `.project/link` / `[]` / `["sub","lib","qry"]` |
| 数据库字段 | `bdl.schema.enable` / `dataDir` | `false` / `.project/schema` |

> `bdl.prod.*`（Oracle 连接与部署服务器）与 `bdl.deploy.*` 属于 tiptop/插件侧，bdl-lsp 不读。

项目级配置 `<项目>/.bdl-lsp/config.json`（可提交）：

```jsonc
{
  "generoVersion": "2.50",
  "diagnostics": { "record-member-unknown": "warning" },
  "globals": { "searchPaths": ["tiptop/config", "topcust/config"] },
  "perRoots": ["tiptop/aap/per"],
  "schema": { "enable": true, "dataDir": ".project/schema" },
  "link":   { "enable": true, "dataDir": ".project/link", "moduleRoots": [], "globalModules": ["sub","lib","qry"] },
  "ignore": { "files": ["**/backup/**", "**/tmp/**"] }
}
```

另外 `<项目>/.bdl-lsp/builtins.json` 用于覆盖内置符号表。

---

## 11. 开发者工具与质量门

| # | 工具 | 说明 |
|---|---|---|
| L1 | `make build` / `test` / `test-race` / `vet` / `fmt` / `lint` | 构建与基础检查；`lint` = vet + `keywords-check` + `builtin-check` + `diagnostics-check` |
| L2 | `make check` / `corpus` / `regress` | 样例冒烟；全语料解析；全量回归（真实语料 7070 个，通过 7068，99.97%） |
| L3 | `make bench` + `scripts/lsp-bench.py` | LSP 延迟基准（`didOpen` 133 ms、`didChange` 往返 6.0 ms） |
| L4 | `make builtin-audit` / `builtin-check` | `cmd/builtin-audit`：按频次+文件数列出不在内置表里的名字并粗判归属；`-check` 进 lint |
| L5 | `make diag-audit` | `cmd/diag-audit`：逐码计数、高频标识符、噪声最高文件 |
| L6 | `make grammar-audit` | `cmd/grammar-audit`：按「最内层 ERROR 上下文」聚合排序；`TestCorpusGrammarBaseline` 冻结 18 个已知坏文件基线（18/15108 = 0.12%） |
| L7 | `make diagnostics-check` / `diagnostics-write` | 诊断码注册表 ↔ 文档 ↔ VSC manifest 三方一致性 |
| L8 | `cmd/field-audit` / `fmt-audit` / `record-audit` | 字段校验、格式化、record 成员的语料评估 |
| L9 | `make grammars` / `grammars-check` / `keywords` / `keywords-check` / `manifest-check` | 生成/校验 TextMate 语法与关键字表、VSC 清单校验 |
| L10 | `make e2e-vscode` / `zed-check` / `zed-check-windows` | 无头 VSCode 端到端；Zed 查询校验 |
| L11 | `scripts/` | `install-toolchain.sh`（幂等）、`dev-env.sh`、`env-check.sh`、`corpus-smoke.sh`、`corpus-regress.sh`、`repro-index-crash.py`、`generate-keywords.mjs`、`generate-tm-grammars.mjs`、`verify-tm-grammars.mjs`、`verify-vscode-manifest.mjs` |
| L12 | `.github/workflows/release.yml` | tag `v*` → 四个平台二进制（linux/darwin-amd64/darwin-arm64/windows）+ `SHA256SUMS` 发到 Release；各平台原生构建，Windows 静态链接 MinGW 运行库 |

---

## 12. 编辑器支持矩阵

| 能力 | VSCode | Zed（`4gl-full` v0.1.1，暂停） |
|---|---|---|
| 语法高亮 / LSP 基础 | ✅ | ✅ |
| 链接/数据库字段 TreeView、反查、部署命令 | ✅ | ➖（可见不可跳 / 用 task） |
| 语义高亮 | ✅ | ➖ |
| 真机验证 | Linux 通过，Windows/macOS 待验 | 缺真机 |

---

## 13. 明确不做

| # | 不做 | 原因 |
|---|---|---|
| N1 | Genero 编译器 / 运行时 / 数据库访问 | 超出 LSP 范围（部署归 tiptop） |
| N2 | 100% 语义正确 | 无法静态确定时明确降级，宁可不报 |
| N3 | 完整 IDE（调试器、GUI 设计器） | 不做 |
| N4 | 替代 `fglform` / `fglcomp` | 编辑期辅助，编译才是最终验证 |
| N5 | 移动端 / Web IDE | v1 不支持 |
| N6 | 纯 Go SFTP | 已选 rsync/scp（tiptop 侧） |
| N7 | Genero 5.0 语法 | 暂不排期 |
| N8 | GitHub Release / 自动下载 / 市场与注册表发布 | 暂不排期 |
| N9 | 把内置函数/变量写进 grammar | 内置名变关键字会冒充用户自定义同名符号 |
| N10 | 用 `documentSymbol` 做跨文件跳转 | 该结构无 URI/target 字段；跨文件需扁平 `SymbolInformation` |
| N11 | GLOBALS 变量跨链接可见 | 链接只放大函数可见性，不放大变量可见性 |
| N12 | 跨文件重命名 / 类型与赋值兼容性检查 | 有意不做（变量只查「有没有来源」） |
| N13 | 链接资料过期告警 | 设计如此（`maxAgeDays` 已删除） |

---

## 14. 相关文档

| 文档 | 内容 |
|---|---|
| [`DIAGNOSTICS.md`](DIAGNOSTICS.md) | 诊断码与默认级别（`make diagnostics-write` 生成，构建输入，不算独立功能文档） |
| [`../README.md`](../README.md) | 仓库入口：构建、工具链、快速开始 |
| [vscode-bdl/docs/FEATURES.md](https://github.com/darcyjoven/vscode-bdl/blob/main/docs/FEATURES.md) | 三个组件按类型整合的完整功能文档 |
| [vscode-bdl/README.md](https://github.com/darcyjoven/vscode-bdl/blob/main/README.md) | 唯一用户引导 |
| [vscode-bdl/docs/TODO.md](https://github.com/darcyjoven/vscode-bdl/blob/main/docs/TODO.md) | 待办清单 |
| [vscode-bdl/docs/OPEN-ITEMS.md](https://github.com/darcyjoven/vscode-bdl/blob/main/docs/OPEN-ITEMS.md) | 待确认记录 |
| [tiptop/docs/FEATURES.md](https://github.com/darcyjoven/tiptop/blob/main/docs/FEATURES.md) | tiptop 单组件功能清单 |

---

## 附录 A：需求来源与验收口径

> 从旧版功能基线（`FEATURES.md` v0.3，2026-10-22）保留的可追溯信息；功能本体见上文各节。

### A.1 需求来源

| # | 功能 | 来源 | 状态 |
|---|---|---|---|
| 1 | 语法高亮（`.4gl` / `.global` / `.per`） | 用户提出 | ✅ VSCode TextMate + Zed query |
| 2 | 函数、变量跳转 | 用户提出 | ✅ 单文件内 + GLOBALS 跨文件（C2） |
| 3 | 4GL 格式化（关键字大小写 + 缩进），`.per` 不格式化 | 用户提出 | ✅ B10 |
| 4 | 4GL 中校验字段是否存在 | 用户提出 | ✅ `field-not-in-form`；record `.*` 最后做 |
| 5 | 4GL 与 `.per` 联动（.per 变更刷新诊断、relatedInformation 跳转） | 用户提出 | ✅ D4 / G 节 |
| 6 | Go 实现的 LSP 服务 | 用户提出 | ✅ |
| 7 | VSCode 插件 | 用户提出 | ✅ vscode-bdl |
| 8 | Zed 插件 | 用户提出 | ✅ `4gl-full` v0.1.1（暂停） |
| 9 | 基于 `tree-sitter-bdl` / `tree-sitter-per` | 用户提出 | ✅ A2 |
| 10 | 仓库 Git 管理 | 用户提出 | ✅ |
| 11 | 文档/脚本/中间产物集中 bdl-lsp | 用户提出 | ✅ |
| 12 | GLOBALS 模组全局变量：跳转 + 来源标注 + 两类可关警告 | 用户提出 | ✅ C2 |
| 13 | 部署服务器 + 上传编译 | 用户给方向 | ✅ 现由 tiptop 提供 |
| 14 | 无感使用体验 | 用户给方向 | ◐ 本地路径/缓存已做；自动下载随发布暂缓 |
| 15 | 与轻量插件共存 | 用户给方向 | ✅ Zed id `4gl-full` |
| 16 | Genero 版本范围 2.50–5.0 | 用户给方向 | ✅ 以 2.50 为基线；5.0 暂不排期（见 OPEN-ITEMS） |

### A.2 记录（record）`.*` 校验的分期

| 子项 | 内容 | 状态 |
|---|---|---|
| 4.1 | 显式字段校验：`AFTER FIELD`、`BEFORE FIELD`、`NEXT FIELD`、`INFIELD()`、`CONSTRUCT ... ON`、`DISPLAY ... TO` | ✅ |
| 4.2 | 跨文件：表单路径解析、`.per` item 集合、`SCREEN RECORD` | ✅ |
| 4.3 | record `BY NAME` 成员校验 | ✅ 默认 off（`record-member-unknown`） |

### A.3 验收口径

| 功能 | 验收方式 |
|---|---|
| 高亮 | 打开样例文件，关键字/字符串/注释/数字着色 |
| 跳转 | 函数/变量/字段可跳转；成功率 > 90% |
| 格式化 | 幂等；格式化后无新增 `ERROR`；`.per` 一律不格式化 |
| 字段校验 | 默认 `warning`；表单唯一解析才报；标注集误报 < 5% |
| record `.*` | 单独开关（默认关闭）；标注集误报 < 10% 才默认开启 |
| LSP | initialize / 文档同步 / 诊断 / 符号 / 跳转全部可用 |
| VSCode | VSIX 本地安装；高亮 + 跳转 + 诊断 |
| Zed | dev extension 加载；高亮 + LSP 跳转 |
| 部署 | dry-run + 真实上传 + 编译诊断映射（tiptop） |
| 内置符号 | 语料 `make builtin-audit` 后「未知未分类」< 20 条；`STATUS`/`SQLCA`/`ARG_VAL` 不再误报 |
| 链接资料 | 真实 GAL_FILE 全量导入后跨文件函数可见；R2 反例：链接的子文件间变量仍不可见 |
| 诊断级别 | 每码可配 warn/error/info/hint/off；拼错有告警；`make diag-audit` 给出逐码基线 |