# 插件功能清单（基线）

> 版本：v0.3（同步 P1–P8 完成状态；补忽略列表/部署当前文件）  
> 日期：2026-10-22  
> 用途：作为功能验收基线；后续按本文件逐项确认“已做/未做/不做”。  
> 来源标记：**🧑 用户提供** = 用户原始需求明确要求；**🔀 用户方向 + 协助细化** = 用户提出方向，由规划补充为可执行功能；**🤖 协助规划** = 依据语言与编辑器常规能力补充。

---

## 1. 用户明确要求的功能

| # | 功能 | 来源 | 优先级 | 状态 |
|---|---|---|---|---|
| 1 | 语法高亮（`.4gl` / `.global` / `.per`） | 🧑 用户提供 | v1 | ✅ VSCode TextMate + Zed query |
| 2 | 函数、变量跳转 | 🧑 用户提供 | v1 | ✅ 单文件内；GLOBALS 跨文件见 #12 |
| 3 | 4GL 格式化（关键字大小写 + 缩进）；`.per` **不**格式化 | 🧑 用户提供 | v1 | ✅ P4 完成 |
| 4 | 4GL 中校验字段是否存在 | 🧑 用户提供 | v1（显式字段）；record `.*` 最后 | ✅ P3b 已完成（语料 2.8% 可疑率、e2e 18/18），见 [PLAN-P3B-FIELD.md](PLAN-P3B-FIELD.md) |
| 5 | 4GL 与 `.per` 文件的联动功能 | 🧑 用户提供 | v1 | ✅ P3b：.per 变更刷新 4gl 诊断、relatedInformation 跳转 |
| 6 | Go 实现的 LSP 服务 | 🧑 用户提供 | v1 | ✅ |
| 7 | VSCode 插件 | 🧑 用户提供 | v1 | ✅（v0.1.1 VSIX，e2e 33/33） |
| 8 | Zed 插件 | 🧑 用户提供 | P2 | ✅（`4gl-full` v0.1.1，47 项 query 校验） |
| 9 | 基于 `tree-sitter-bdl` / `tree-sitter-per` | 🧑 用户提供 | v1 | ✅ |
| 10 | 三个仓库的 Git 管理 | 🧑 用户提供 | 已完成 | ✅ |
| 11 | 文档/脚本/中间产物集中 `bdl-lsp` | 🧑 用户提供 | 已完成 | ✅ |
| 12 | GLOBALS 模组全局变量：跳转到 global 文件、来源标注、两类警告（可关闭） | 🧑 用户提供 | P3a | ✅ 已完成（语料 8403 处引用验证、e2e 16/16），见 [PLAN-P3A-GLOBALS.md](PLAN-P3A-GLOBALS.md) |

### 1.1 记录（record）`.*` 校验的说明

用户确认：**record 的 `.*`（如 `INPUT BY NAME rec.*`、`DISPLAY BY NAME rec.*`）校验需要，但放在最后做。**

| 子项 | 内容 | 排期 |
|---|---|---|
| 4.1 | 显式字段校验：`AFTER FIELD`、`BEFORE FIELD`、`NEXT FIELD`、`INFIELD()`、`CONSTRUCT ... ON`、`DISPLAY ... TO` | v1（P3） |
| 4.2 | 跨文件：表单路径解析、`.per` item 集合、`SCREEN RECORD` | v1（P3） |
| 4.3 | record `BY NAME` 成员校验 | ✅ P6 完成（默认 off，误报率 0.1%） |

---

### 1.2 GLOBALS 模组全局变量的说明

用户确认（2026-10-20 原话）：

> global 语法会引入一些比全局变量还高一层的模组全局变量，所有引入相同
> global 文件的都可以使用这些变量。如果是 global 变量，当前 4gl 没有标记
> 其来源，也没有实现跳转到 global 文件，如果 global 文件没有找到，要警告，
> 且变量来源没有找到的也要警告，变量来源找不到警告做成配置项目，允许关闭。

拆解为 5 个功能点（详见 [PLAN-P3A-GLOBALS.md](PLAN-P3A-GLOBALS.md) §1.1）：

| # | 功能点 | 类别 |
|---|---|---|
| F1 | GLOBALS 引入的变量跳转到定义（`.global` 文件内） | 导航 |
| F2 | 悬停标注变量来源（哪个 global 文件） | 信息展示 |
| F3 | `GLOBALS "路径"` 目标文件找不到 → 警告 | 诊断 |
| F4 | 变量来源查不到 → 警告 | 诊断 |
| F5 | F4 警告可配置关闭 | 配置 |

---

## 2. 用户给方向、由规划细化的功能

| # | 功能 | 用户方向 | 细化内容 | 排期 |
|---|---|---|---|---|
| 12 | 部署服务器 + 上传编译 | 🧑 “部署服务器后上传编译” | `ssh + rsync` 主 / `scp` 回退；远程 `fglform`/`fglcomp`；输出解析为诊断；dry-run | P5 |
| 13 | 无感使用体验 | 🧑 “参考主流方式，最好是无感的” | 本地路径自动解析；目标版本校验；缓存与回退（自动下载因发布未排期而暂缓） | P1 基础 / 发布后增强 |
| 14 | 与轻量插件共存 | 🧑 “现有 bdl-plugin 是轻量级插件，本插件是全功能的” | Zed 扩展 id 定为 `4gl-full`，与 `bdl` 并存 | P2 |
| 15 | Genero 版本范围 | 🧑 “目标版本 2.50–5.0，目前 tree-sitter bdl 版本是 2.5” | v1 以 2.50 为基线；`bdl.generoVersion` 配置；**5.0 暂不排期** | v1 / 暂缓 |

---

## 3. 规划补充的功能

### 3.1 v1 核心（P1–P5）

| # | 功能 | 价值 | 排期 |
|---|---|---|---|
| 16 | 语法错误诊断（ERROR/MISSING） | 打开即报解析问题 | P1 |
| 17 | 文档符号（outline） | 大纲导航 | P1 |
| 18 | 工作区符号 | 跨文件快速跳转 | P1 |
| 19 | 引用查找（references） | 看谁引用了该符号 | P1 |
| 20 | 悬停（hover） | 签名、类型、定义位置 | P1 |
| 21 | `.per` 内部一致性校验 | 重复 item、布局 tag 未定义、SCREEN RECORD 引用不存在 | P3 |
| 22 | 格式化安全守卫 | 有 ERROR 不格式化、幂等、语义保持 | P4 |
| 23 | 部署 dry-run | 先看清单再上传 | P5 |
| 24 | 编译诊断映射 | 远程路径 → 本地文件行 | P5 |
| 25 | 索引缓存与性能门槛 | 15k 文件 < 60s；打开 < 300ms | P1 |

### 3.2 后续增强（P6 / 按需）

| # | 功能 | 说明 |
|---|---|---|
| 26 | 补全 | 关键字、函数、变量、表单字段、`ON ACTION` 动作名 |
| 27 | 重命名 | 跨文件、大小写不敏感、含 `.per` 字段 |
| 28 | Code Action / Quick Fix | 字段不存在时提示、忽略诊断、跳转 `.per` |
| 29 | 语义高亮（semantic tokens） | VSCode + Zed |
| 30 | 调用层级（call hierarchy） | 函数调用关系 |
| 31 | 数据库 schema 索引 | 校验 `table.column` 是否存在 |
| 32 | `.per` 结构可视化 | `SCREEN RECORD` 结构、item 列表 |
| 33 | 范围格式化 / SQL 片段格式化 | 只格式化选中部分 |
| 34 | 部署增强 | 多目标、回滚、编译产物归档 |

### 3.3 P7（2026-10-21 立项，待执行）

> 完整设计：[PLAN-P7.md](PLAN-P7.md)。

| # | 功能 | 说明 | 阶段 |
|---|---|---|---|
| 35 | Zed 大纲层级修复 | **容器/叶子模型**：容器（函数/报表/MAIN/菜单）`@item` 绑整块 → 内部变量归属函数可折叠；叶子（DEFINE/TYPE/常量）`@item` 绑名字 → 同语句多变量平级 | P7-A / P7-B-3 |
| 36 | Windows GLOBALS 路径识别 | `internal/fileuri` 全量 URI 解码（`%3A`/`%23`/UNC）+ `workspaceFolders` 回退 | P7-A |
| 37 | 诊断级别统一可配置 | `bdl.diagnostics.<code>` 集中注册表（11 个码）；优先级 新键 > 旧键 > 注册表默认；拼错记 warn；`syntaxDiagnostics` 默认已改 warning | P7-B（✅ 0.1.2） |
| 38 | 内置符号表 | `internal/builtin`：97 条内置变量/函数/类/常量（`data/builtins.json`，`go:embed`）；豁免 `variable-source-unresolved` 与 `function-not-found`；补全与悬停接入 | P7-C-1（✅ 0.1.3，0.1.4 补 `SCR_LINE`） |
| 39 | 程序链接资料索引 | `bdl-lsp link import/status`；导入/状态/可见性/`link-data-*` 诊断 | P7-D（✅ 0.1.4） |
| 38c | 语法缺口审计（P7-C-2） | `make grammar-audit`：全语料解析，按"最内层 ERROR 上下文"聚合排序，输出缺口清单 + JSON（改 grammar 前后逐文件 diff）；`TestCorpusGrammarBaseline` 冻结 18 个已知坏文件的基线（出现新文件即失败）。实测 18/15108 = 0.12% | P7-C-2（✅ 0.1.8） |
| 38f | 数据库 schema 索引（P6） | 解析工作区 3127 个 `.sch`（2879 表，118 ms，`initialize` 同步）；`RECORD LIKE t.*` 记录成员可校验（开启 `recordMemberDiagnostics` 后语料仅剩 24 条），`rec.` 补全给出表的全部列 | P6（✅ 0.2.1） |
| 38e | 工作区符号索引（P6） | 后台索引全工作区（7756 文件 24.7 s，增量 22 ms）并持久化快照；`workspace/symbol` / 跳转 / 引用覆盖未打开文件。可达模组（本模组 + sub/lib/qry）+ 唯一命中两条保守规则 | P6（✅ 0.2.0） |
| 38d | 诊断码表防漂移（P7-B 收尾） | `make diagnostics-check` 校验注册表 ↔ `docs/DIAGNOSTICS.md` ↔ VSCode `package.json`（枚举、六个老键的 `default`、组内默认级别一致）；文档由 `make diagnostics-write` 生成，并入 `make lint` | P7-B（✅ 0.1.9） |
| 39d | 链接资料视图（P7-E） | 自定义请求 `bdl/linkTree` / `bdl/linkReferences`；VSCode 独立 TreeView「链接资料」（程序 → 链内文件三级，点击跳转）+ 反查引用程序（默认不查、分页 50）；实测最大链组装 3.4 ms | P7-E（✅ 0.1.7） |
| 39b | 采集通道（P7-D-3） | `bdl-lsp link sync -source golang|command|file`：纯 Go 直连 Oracle（`go-ora`，无需客户端）/ 命令 stdout / 已导出的 CSV。密码只从环境变量或 `.link/secrets.json`(0600) 取 | P7-D-3（✅ 0.1.6） |
| 39c | 后台自动同步 | `bdl.link.sync.*`（默认**全关**）：`onStart` 启动时跑一次、`interval` 定时；失败指数退避上限 1 h；成功后自动重载资料并补发诊断 | P7-D-3（✅ 0.1.6） |
| 39a | 外部函数过滤 | 被 ≥3 文件或 ≥5 次调用、全语料无定义的名字 → 视为存在并记进 `links.meta.json`；候选 >2000 个则**不启用** | P7-D-2（✅ 0.1.5） |
| 40 | 链接资料 UI | VSCode 自定义 TreeView（三级 + 跨文件跳转）；Zed 降级为可见不可跳 | P7-E |
| 41 | 游标 / prepare statement | `PREPARE`/`DECLARE CURSOR` 声明符号；`EXECUTE`/`FETCH`/`OPEN`/`CLOSE`/`FOREACH` 悬停与跳转；大纲/补全/语义高亮同步接入 | P7-B-5（✅ 2026-10-21） |
| 42 | 诊断降噪 | 变量使用 query 补 4 类跳过（表达式函数名 / `WHENEVER` 目标 / 表单字段含点号）；`.per` 布局单元格多 tag 与 SCREEN RECORD 字段名修正；全量诊断 33.4 万 → 28.9 万 | P7-B-4/6（✅ 0.1.2） |
| 43 | 诊断度量工具 | `cmd/diag-audit` + `make diag-audit`：逐码计数、高频标识符、噪声最高文件（降噪基线，可 CI 化） | P7-B（✅ 0.1.2） |
| 44 | `function-not-found` | 函数调用点找不到定义。P7-C（内置侧）+ P7-D（链接侧）+ P7-D-2（外部函数过滤）全部落地后全量语料只剩 0.09 条/文件，**0.1.5 起默认 warning**；没有链接资料时不产生任何诊断 | P7-B → P7-D-2（✅ 0.1.5，默认开） |
| 45 | 内置符号可配置/可更新 | 三层：内置 JSON（改数据不改代码）· 项目覆盖 `<root>/.bdl-lsp/builtins.json`（自动发现、2s 节流热更新）· `bdl.builtins.enable/paths/disable` | P7-C-1（✅ 0.1.3） |
| 46 | 内置符号缺口审计 | `cmd/builtin-audit` + `make builtin-audit` / `make builtin-check`：按频次+文件数列出"不在表里"的名字，带内置/用户粗判；`-check` 进 `make lint` | P7-C-1（✅ 0.1.3） |
| 47 | 程序链接资料导入 | `bdl-lsp link import -csv <导出.csv>`：CSV（UTF-8 BOM、`gal04='Y'` 过滤、组内去重）→ `<root>/.link/links.jsonl` + `links.meta.json`，原子写入；判定 MAIN 用语法树 + 文本兜底（兼容 `{ MAIN }` 写法） | P7-D（✅ 0.1.4） |
| 48 | 链接资料状态 | `bdl-lsp link status`：链/orphan/ambiguous/文件数、生成时间与天数、缺失文件数、模组根、全局模组；过期时提示重新导入 | P7-D（✅ 0.1.4） |
| 49 | 函数跨文件可见性 | 可见函数 = 本文件 ∪ GLOBALS ∪ 链接文件 ∪ 全局模组（`sub`/`lib`/`qry` 扁平规则，含它们自己）。**只放大函数，变量可见性不变（R2）** | P7-D（✅ 0.1.4） |
| 50 | `function-not-found` 启用条件 | 必须有链接资料且索引预热完成才判定；资料缺失/链里有磁盘上不存在的文件/索引未就绪 → **一律不报**（宁可漏报不可误报） | P7-D（✅ 0.1.4） |
| 51 | 链接资料自身诊断 | `link-data-invalid`（JSONL 坏了 → 停用并提示重导）· `link-data-stale`（超 `maxAgeDays` 未更新 → hint）；都落在 `.link/` 里的文件上，不污染源码 | P7-D（✅ 0.1.4） |
| 52 | 链接配置 | `bdl.link.enable/dataDir/moduleRoots/globalModules/maxAgeDays`；资料目录与内置表一样按 mtime 热重载（2s 节流），导入后无需重启编辑器 | P7-D（✅ 0.1.4） |
| 53 | **忽略列表** | `bdl.ignore.files/fields/forms`（glob，支持 `**`）：文件命中 → 不发布任何诊断且排除出工作区索引；字段命中 → 只屏蔽 `field-not-in-form`；表单命中 → 屏蔽该表单的字段类诊断。配置热更新立即生效；忽略不关闭解析/悬停/导航 | P8（✅ 0.2.2） |
| 54 | **部署当前文件** | VSCode `bdl.deployCurrentFile`：先保存脏文档，只把当前 `.4gl/.global/.per` 交给 `deploy`；Zed 用 task `$ZED_FILE`（见 [DEPLOY-ZED](DEPLOY-ZED.md)）。dry-run / 编译诊断行为与整包部署一致 | P8 复核（✅ 0.2.2） |

---

## 4. 明确不做

| # | 不做 | 原因 |
|---|---|---|
| N1 | Genero 编译器/运行时/数据库访问 | 超出 LSP 范围 |
| N2 | 100% 语义正确 | 无法静态确定时明确降级，宁可不报 |
| N3 | 完整 IDE（调试器、GUI 设计器） | 不做 |
| N4 | 替代 `fglform` / `fglcomp` | LSP 是编辑期辅助，编译是最终验证 |
| N5 | 移动端 / Web IDE | v1 不支持 |
| N6 | 纯 Go SFTP | 已选 rsync/scp |
| N7 | Genero 5.0 语法支持 | **暂不排期** |
| N8 | GitHub Release / 自动下载 / 市场与注册表发布 | **暂不排期** |
| N9 | 把内置函数/变量写进 grammar | 会破坏高亮/重命名/补全语义：内置名变关键字后，用户自定义同名符号被冒充（P7-C 结论） |
| N10 | 用 `documentSymbol` 做跨文件跳转 | LSP 该结构没有 URI/target 字段；跨文件需扁平 `SymbolInformation`，与层级不共存（P7-E 结论） |
| N11 | 让 GLOBALS 变量跨链接可见 | 用户明确要求：链接只放大函数可见性，不放大变量可见性（P7-D 规则 R2） |

---

## 5. 按阶段汇总

| 阶段 | 功能编号 | 状态 |
|---|---|---|
| 已完成 | 10, 11 | ✅ |
| P1 | 1(基础), 2, 6, 7, 9, 16, 17, 18, 19, 20, 25 | ✅ 完成 |
| P2 | 8, 14 | ✅ 完成 |
| P3 | 4.1, 4.2, 5, 21 | ✅ P3a/P3b 完成（2026-10-20） |
| P4 | 3, 22 | ✅ 完成（2026-10-21） |
| P5 | 12, 23, 24 | ✅ CLI + VSCode 完成（2026-10-21） |
| P6 | 4.3, 26–34 | ✅ 补全/重命名/CodeAction/语义高亮/record 成员 全部完成（2026-10-21） |
| P7 | 35–40 | ✅ 全部完成（2026-10-21） |
| **P8** | **53, 54** | ✅ 收尾完成（2026-10-22）：忽略列表、部署当前文件复核、TESTING/编辑器差异文档、grammar tag、go-tree-sitter v0.25 |
| 暂不排期 | 15(5.0), N7, N8 | — |

---

## 6. 验收口径（按功能）

| 功能 | 验收方式 |
|---|---|
| 高亮 | 打开样例文件，关键字/字符串/注释/数字着色 |
| 跳转 | 函数/变量/字段可跳转；成功率 > 90% |
| 格式化 | 幂等；格式化后无新增 ERROR；`.per` 一律不格式化 |
| 字段校验 | 默认 `warning`；表单唯一解析才报；标注集误报 < 5% |
| record `.*` | 单独开关（默认关闭）；标注集误报 < 10% 才默认开启 |
| LSP | initialize/文档同步/诊断/符号/跳转全部可用 |
| VSCode | VSIX 本地安装；高亮 + 跳转 + 诊断 |
| Zed | dev extension 加载；高亮 + LSP 跳转 |
| 部署 | dry-run + 真实上传 + 编译诊断映射 |
| 内置符号（P7-C） | 语料 `cmd/builtin-audit` 后"未知未分类"< 20 条；`STATUS`/`SQLCA`/`ARG_VAL` 不再误报 |
| 链接资料（P7-D） | 真实 GAL_FILE 全量导入后，跨文件函数可见；**R2 反例测试**：链接的子文件间变量仍不可见 |
| 诊断级别（P7-B） | 每个诊断码可配 warn/error/info/hint/off；拼错有告警；`make diag-audit` 给出逐码基线 |
