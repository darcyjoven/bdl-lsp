# 未完成事项

> 盘点：2026-10-23
> 版本：`bdl-lsp 0.3.12` · `tiptop 0.1.2` · `vscode-bdl 0.5.1` · `zed-bdl 0.1.1`（**暂停**）
>
> 本文是**唯一**的"还剩什么"清单。历史的阶段清单（`TODO.md`）与各阶段实现记录
> （`PLAN.md`、`PLAN-P*.md`、`PLAN-DETAILED-P1.md`、`DEPLOY-ZED.md`）已于 2026-10-23 删除，
> 仍然有效的决策抢救进 [PLUGIN-PLAN.md](PLUGIN-PLAN.md) 附录 A。
> 新会话接手先读 [HANDOVER.md](HANDOVER.md)，再看本文。

---

## 0. 现状

| 仓库 | 版本 | 状态 |
|---|---|---|
| `bdl-lsp` | 0.3.12 | 语言服务；`link`/`deploy`/`schema` 已分离出去 |
| `tiptop` | 0.1.7 | 链接资料 + 表字段采集 + 部署；自带 CI（Linux + Windows 双绿） |
| `vscode-bdl` | 0.6.0 | 平台包内置两个二进制；新增「数据库字段」视图 |
| `zed-bdl` | 0.1.1 | **追平中**（用户 2026-10-23 重启，见 §D） |

**最近完成的（不再追踪）**：P1–P8 全部功能 · 0.3.1 GLOBALS 按文档隔离 ·
0.3.2 link/deploy 分离 · 0.5.0 部署改 tiptop · CI 多平台一键打包 ·
`schema.Merge` 顺序相关缺陷修复 · **§A 分离收尾残留（0.3.3 / 0.1.1 / 0.5.1，2026-10-23，见 §A）** ·
**Windows CI 修复（0.3.4 / 0.1.2，2026-10-23，见 §B8/B9、§F7/F8）**。

**代码侧没有"必须做"的功能项了。** 剩下的是三类：需要机器/真实环境的验证（§B）、
发布相关（§C）、**Zed 插件追平（§D，2026-10-23 重启）**。（§E/§F 持续跟踪。）

> **§D 的关键决定（2026-10-23）**：
> ① Zed **不**复刻 VSCode 的侧边栏 TreeView（Zed 无树视图 API）→
>    链接资料改用 **Code Lens 挂在 MAIN 头上**（服务端 `textDocument/codeLens`）；
> ② 该实现**两端共用** —— VSCode 侧作为**补充视图**与 TreeView 并存（§P 的 P1-8）；
> ③ **link 分析保持原状**（不移到 tiptop）。

---

## A. 分离收尾残留 —— ✅ 全部完成（2026-10-23）

> A1–A8 已全部落地并验证，本节保留为索引（编号可继续被引用）。
> 全场验证：bdl-lsp `make lint` + `go test ./...` 全绿；tiptop `make lint` +
> `go test ./...` 全绿；vscode `npm run typecheck` + **e2e 38/38** 通过。

| # | 原问题 | 结果 |
|---|---|---|
| A1 | `Makefile` 的 `link-audit` / `link-status` 已坏 | 两个目标从 bdl-lsp 删除（`cmd/link-audit` 随分离移走且未迁入 tiptop）。`make help` 不再列出；命令改由 `tiptop link status -root <root>` 提供（见 [TESTING](TESTING.md) §3.7、[README](../README.md)） |
| A2 | `scripts/fetch-gal-file.sh` 归属错 | 移到 `tiptop/scripts/fetch-gal-file.sh`，5 处提示改 `tiptop link ...`；`bash -n` 通过，bdl-lsp 不再有采集脚本 |
| A3 | `client.ts` 下发已删掉的 `sync` 配置 | 删掉整块 `link.sync`；连带清掉 USER-GUIDE / README / `package.json` 里旧 `bdl.link.sync.*` 说明（服务端与扩展都已无此设置） |
| A4 | `docs/DEPLOY-ZED.md` 命令名过期 | 全文改 `tiptop deploy`（含 §3 二进制查找与 FAQ），无 `bdl-lsp deploy` |
| A5 | `docs/EDITOR-SUPPORT.md` 命令名过期 | 改 `tiptop link status` / `tiptop deploy` |
| A6 | `docs/TODO.md` §5/§6 归属 | §5/§6 顶部加归属注记；头部补 0.3.1 / 0.3.2 与分离；**历史勾选未改写** |
| A7 | 当前文档的命令与归属 | `FEATURES`/`TESTING`/`PLAN` 更正命令与归属；`PLAN-P5-DEPLOY`/`PLAN-P7`/`PLAN-SEPARATION` 顶部加"当时记录 + 归属变更"注记，正文不动 |
| A8 | tiptop 没有自己的 CI | 新增 `tiptop/.github/workflows/build.yml`：Linux + Windows，CGO 构建 + `go test -race` + `make lint`，检出 bdl-lsp 兄弟目录满足 `replace`。已 push：Actions 第 1 次运行 **Linux 全绿**；Windows 卡在测试步（见 **B9**）。同时修掉 `bdl-lsp/scripts/build.ps1` 里让 Windows job 从来跑不到测试步的旧 grammar 检查 |

**顺手清掉的同类残留**（不在原清单，性质相同）：

- 服务端**用户可见**诊断文案里的 `bdl-lsp link import` → `tiptop link import`
  （`internal/server/links.go` 两条提示 + `diagnostics.go` 注释）；
- `link/` 包注释里的 `bdl-lsp link ...` / `cmd/link-audit`；
- `vscode-extension/docs/PROJECT-SETUP.md` 的一键脚本：新解析 `$TIPTOP`，
  `link sync/status`、`deploy` 改用 `"$TIPTOP"`（原来错用 `$BDL_LSP`）；
- `vscode-extension/package.json` 两处 `markdownDescription` 的过期命令；
- `docs/HANDOVER.md` 与 `README.md` 的版本/仓库数/命令同步（原停留在 0.3.1、三个仓库）；
- `examples/deploy.json` 移到 `tiptop/examples/deploy.json`（同 A2 性质：只被
  `tiptop deploy` 消费），并更新四处引用（README、DEPLOY-ZED、PLAN-P5-DEPLOY、VSCode USER-GUIDE）。

> 上表提到的 `TODO.md` / `DEPLOY-ZED.md` / `PLAN-P5-DEPLOY.md` 等文件已在随后的文档清理中删除
> （见 §P 的"已完成"），这些行保留为当时的改动记录。

---

## P. VSCode 插件改版（**发布前**；schema 除外）

> 设计与分析（含根因证据）：**[PLUGIN-PLAN.md](PLUGIN-PLAN.md)**。本节只列可勾选的待办。
> 原则：三档使用模式（§1），**默认 = 零配置轻量档**（不配任何东西、不碰敏感信息、只解析语法）。

| # | 事项 | 做法要点 | 验收 |
|---|---|---|---|
| ✅ P0-1 | ~~并发崩溃（最高优先）~~ **已修复 2026-10-23** | 真凶不是索引，而是**链接预热回调**（`link.SymbolIndex.StartGlobal` 的后台 `onReady` → `rediagnoseAll` → `reparse` → `Document.SetTree` 关掉 handler 正在遍历的树）。改为 `requestRediagnose()`（永不阻塞、后台等 `parseMu` 再算）+ `rediagnoseAllLocked()`（供已持锁的 `reparseFull` 调） | [`scripts/repro-index-crash.py`](../scripts/repro-index-crash.py)：修复前 8/8 崩溃 → 修复后 **20/20 OK**（含 4.5 万行文件）；`internal/server/lock_test.go` 三条锁纪律测试 |
| P0-1b | 崩溃回归进 CI | 复现脚本依赖真实语料（CI 没有）→ 补一条**合成工作区**的等价测试（造 `.link/links.jsonl` + 全局模组文件拉开预热窗口 + 一个大文件） | CI 里能复现并拦住回归 |
| ✅ P0-2 | ~~表单侧名字被误报"变量不存在"~~ **已修复 2026-10-23** | ① 语法跳过新增 `(input record: …)`（`INPUT ARRAY … FROM s_x.*` 落在 `record:` 字段）与 `CLEAR` 操作数；② 语义兜底 `form.Resolver.FormSideNames()`（item / SCREEN RECORD / `= table.column` 列名），`reparseFull`+`AuditFile` 改成先跑字段校验再跑 T5；③ 回归测试 `internal/server/t5form_test.go` | `saapt310.4gl` 4 条 → **0**；全语料 T5 **24284 → 19580（−4704，−19.4%）**，`function-not-found`/`field-not-in-form` 一条不变 |
| P0-2b | T5 剩余量级（**待确认语义**） | 剩余 19580 条的主体是**跨模块的模块级变量**（`L_AC` 1828、`G_RVV` 1715…）：它们在**别的模块**里确有 `DEFINE`，但不在当前文件或其 GLOBALS 里。算不算误报，取决于"模块级 DEFINE 是否跨模块可见"，需要真实项目口径 | 确认是噪声后再设计（类似链接可见性矩阵，属较大改动） |
| ✅ P0-3 | ~~未解析/超时**要告知用户** + 强制解析~~ **已实现 2026-10-23** | ① 解析超时以前只写日志 → 新增诊断码 `parse-timeout`；② `file-too-large` / `parse-timeout` 默认级别从 information 上调为 **warning**（"文件没被检查"必须看得见）；③ 新增自定义请求 `bdl/forceParse` + 命令 `bdl.forceParseCurrentFile`，`editor/title` 仅在出现这两条诊断时显示按钮（`bdl.parseSkipped` 上下文）；④ 有效期：`didClose` 时移除标记 | 新增测试：超限→提示→强制解析后能出大纲且不再提示→关闭再打开提示回来；超时→`parse-timeout` 提示。见 `internal/server/forceparse_test.go` |
| ✅ P1-1 | ~~二进制与**命令入口**~~ **已实现 2026-10-23** | `src/linktools.ts`：6 条命令（`importCsv`/`syncCurrent`/`syncModules`/`syncAll`/`status` + 视图按钮 `links.resync`），复用部署命令的二进制解析与输出通道；会写盘的一律先 `-dry-run` 解析"**受影响作业 N 个（名字）**"再确认（0 个直接提示）；没配 DB 时给"打开设置 / 改用 CSV 导入"按钮，且已有 `gal_file.csv` 时直接走 CSV 局部导入；`activationEvents` 补 6 条 `onCommand:`，命令面板 `when: config.bdl.link.enable` | e2e 40/40（含影响面解析、总开关关闭时命令不报错）；"VSIX 内容自检"**本来就有**（`bundle-vsix.yml` 校验 bin/dist/syntaxes 且拒绝夹带仓库文件），无需新增 |
| ✅ P1-2 | ~~`.link` → `.project`~~ **已实现 2026-10-23** | ① 默认值三处（`link.DefaultDataDir` / LSP 配置 / 插件设置）改 `.project`；② 读兼容：新目录没有资料而 `.link/` 有 → 读旧目录并记 `legacyDir`；③ 新诊断码 `link-data-legacy-dir`（hint，挂在旧目录 `links.jsonl` 上）；④ 插件命令 `bdl.migrateLinkDir` + 看到提示时弹出"迁移/稍后"，搬完自动重启服务器；⑤ tiptop/脚本/文档全部改 `.project/` | 旧项目仍可用且有迁移提示；`TestLegacyLinkDirStillReadWithHint`；e2e 命令清单含迁移命令 |
| ✅ P1-3 | ~~link **局部同步**~~ **已实现 2026-10-23**（后端；**插件侧的命令入口属 P1-1**，已随 P1-1 落地） | tiptop：`link import`/`sync` 新增 `-modules`、`-files`（+ `import` 的 `-dry-run`）：只重算受影响作业的链并**合并**进现有 `links.jsonl`（其余链保留）；只扫受影响文件（亚秒级）；`source=golang` + 默认 SQL 时条件下推到 Oracle；meta 记 `partial`，`link status` 显示局部更新；无全量基线时报错 | `link/partial_test.go`、`linksync/filter_test.go`；实测"公共文件被 2 个作业引用 → 受影响作业 2 个（PROG1, PROG2）"；合并后其余链保留 |
| ✅ P1-4 | ~~三档模式与默认值~~ **已实现 2026-10-23** | 默认 = **轻量档**：`bdl.link.enable`/`bdl.deploy.enable`/`bdl.schema.enable` 默认 **false**；相关命令用 `when: config.bdl.*.enable` 门控（默认隐藏）；不连库、不后台跑任务、不往工作区写文件 | e2e：五组设置 + 三项默认 false（含运行时实际值）+「轻量档不往项目里写文件」；代码里显式传默认值处（`linkEnabled`/`deployEnabled`/`client.ts`）一并改 false。**例外**：服务端 Go 侧兜底默认仍是"开"（无客户端设置时的兜底，见 PLUGIN-PLAN §1 注） |
| ✅ P1-5 | ~~设置重分组为 5 类~~ **已实现 2026-10-23** | ① 五组裸标题：基础 / 生产环境 / 链接资料 / 部署 / 数据库字段（`schema.*` 挪进第五组，新增 `schema.dataDir=.project/.schema`）；② 生产环境组新增 `prod.deploy.*`（13 键），每个键都写明"只写 Workspace、别把 `settings.json` 进 git、密码不放设置里"；③ **部署配置不手写**：`src/deployconfig.ts` 合成 `<项目>/.project/deploy.json`（带 `createdBy`），tiptop 结构体加元字段、默认目录改 `.project`（旧 `.bdl-lsp/` 只读兼容），外来文件覆盖前备份 `.bak`，缺设置时说清缺哪项；④ 顺手修掉「列出部署目标」错用语言服务器二进制 | e2e **42/42**（新增五组+默认轻量、轻量档不写项目、部署配置合成含备份与缺项）；tiptop `config_path_test.go` 三条 |
| ✅ P1-6 | ~~去掉 7 天过期与自动同步~~ **已实现 2026-10-23** | ① 删诊断码 `link-data-stale`（15 → 14 个码，`docs/DIAGNOSTICS.md` 重新生成）与服务端过期分支；② 删 `link.maxAgeDays`（Go 配置字段 + 两处校验 + 扩展设置 + `client.ts` 下发字段）；③ `link.Status` 去掉 `Stale/MaxAgeDays`，`LoadStatus(dir, modules)` 签名简化，`tiptop link status` 去掉 `-max-age` 与过期输出；④ 删 `linksync` 的 `Scheduler`（**全仓无人构造的死代码**）与 `Config.Interval/OnStart` | 反向测试 `TestLinkVeryOldDataIsSilent`：2020 年的 meta **完全静默**；三仓文档/清单同步清掉"过期"表述 |
| P1-7 | 用户文档重写为插件说明 | `vscode-extension/docs/USER-GUIDE.md` 按最终形态重写 → 作为 README/市场说明（同源） | 文档与 UI/命令一一对应；截图清单更新 |
| **P1-8** | **链接资料 Code Lens**（**补充视图**，与 TreeView 并存） | 服务端实现 `textDocument/codeLens`（bdl-lsp 侧，见 §D 的 Z-CL-1）：链内**子文件各一条 lens**，全部挂在该文件的 **MAIN 函数头上**，每条带自己的 `command` 可分别点击；**数量上限** `bdl.link.lensMax`（默认 10）控制行数，超出给一条「…共 N 个」汇总；函数级反查（每个函数定义上方「🔗 被 N 处调用」，调用点来自 `wsindex.CallsOf`） | **定位**：VSCode 已有侧边栏「链接资料」TreeView（三级树、可点、可分页），Code Lens 是**就地补充视图**——在 MAIN 头上多看一眼，不用切面板；**两者并存，不互相替代**。服务端实现**两端共用**（VSCode 同样支持 codeLens） |
| ✅ P2-1 | ~~schema（发布后一版）~~ **已实现 2026-10-23** | ① **tiptop `schema import/status`**：Oracle 导出 CSV → `.project/.schema/{tables.json, tables/<大写表>.json}`，表头两处拼写容错、`-tables`/`-only-new`/`-dry-run`、原子写、坏索引报错且保留原文件；② **bdl-lsp**：`internal/schema` 改成"只读总索引 + 表文件展开才加载并缓存"，旧 `.sch` 扫描与 `schema.paths` **整包下线**；请求 `bdl/schemaTables`（服务端正则过滤 + total/matched/returned，非法正则退回全部 + filterError）与 `bdl/schemaColumns`；悬停三段（`LIKE t.col` 附单列、`RECORD LIKE t.*` 列字段（>20 折叠）、悬停表名给表摘要）；`rec.` 补全带类型与中文说明；③ **插件 0.6.0**：独立视图「数据库字段」（正则查询框、展开加载、右键同步单表/复制 `LIKE t.col`/打开表文件、"共 N 张表"）、`从 CSV 导入` 命令（先 `-dry-run` 预演再确认）；④ 开关仍是 `bdl.schema.enable`（默认关）+ `schema.dataDir` | **真实数据实测**：整份 `demo.csv`（58,096 行）→ 2648 表 / 58096 列 / 2.4 s；LSP 侧 `^IMA_` 匹配 1、`ima_file` 317 列；悬停 `RECORD LIKE ima_file.*` 列出字段并折叠、`LIKE ima_file.ima01` 显示 `VARCHAR2(40) · 料件编号`。测试：`internal/schema`（格式/版本/惰性/正则/展示）、`internal/server`（成员校验 3 例 + 悬停 3 例 + 补全 + 两个请求 + 关开关）、`internal/schemacli` 11 例、e2e 43/43（新增"数据库字段端到端查询与正则过滤"） |

**已完成（2026-10-23 第二轮）**：

- ✅ 文档清理：删除 `TODO.md`、`PLAN-DETAILED-P1.md`、`PLAN.md`、全部 `PLAN-P*.md`、`DEPLOY-ZED.md`；
  有效决策抢救进 [PLUGIN-PLAN](PLUGIN-PLAN.md) 附录 A；
- ✅ 复现脚本 [`scripts/repro-index-crash.py`](../scripts/repro-index-crash.py) 落地（退出码 0=OK / 1=崩溃）；
- ✅ **P0-1 修复并验证**：链接预热回调纳入 `parseMu` 纪律（`requestRediagnose` / `rediagnoseAllLocked`），
  复现脚本 8/8 崩溃 → 20/20 正常，`go test ./...` 全绿；
- ✅ 样例 schema 数据放到 `artifacts/schema/demo.csv`（已 gitignore）；
- ✅ **P0-3 实现并验证**：`parse-timeout` 码 + 两条提示级别上调为 warning +
  `bdl/forceParse` 请求与 `bdl.forceParseCurrentFile` 命令（标题栏按钮按需出现）。
- ✅ **P1-2 实现并验证**：`.link/` → `.project/`（默认值 + 读兼容 + 迁移提示 + 一键迁移命令 +
  三仓文档/脚本同步）。
- ✅ **P1-3 实现并验证**：`link import`/`sync` 的 `-modules`/`-files`（+`-dry-run`）只重算受影响
  作业并合并写回；SQL 下推只对默认 SQL；meta 记 `partial`。
- ✅ **P1-1 实现并验证**：链接资料 6 条命令进 VSCode + 预演确认 + 生产环境设置节；e2e 40/40。
- ✅ **P1-4 实现并验证**：默认轻量档（三开关默认 false + 命令门控 + 不写工作区）。
- ✅ **P1-5 实现并验证**：设置五组（裸标题）+ `prod.deploy.*` + 插件合成
  `.project/deploy.json`；e2e 42/42。
- ✅ **P1-6 实现并验证**：去掉 7 天过期（`link-data-stale` + `maxAgeDays`）与后台自动同步
  （死代码 `Scheduler`）；同步只由命令触发。
- ✅ **P2-1 实现并验证**：数据库字段资料全链路（tiptop 导入 → bdl-lsp 读取/悬停/请求 → 插件视图）；
  真实数据 2648 表 / 58096 列跑通。

---

## B. 验证缺口（需要机器或真实环境）

> 这些都是"代码已就绪、缺一次实跑"。**没有一项是代码问题。**

| # | 事项 | 现状 | 验收 |
|---|---|---|---|
| B1 | **Windows 实机**（从未装过包） | ✅ CI 已双绿（构建 + 二进制自检 + `go test -race` + lint，见 B9），但**没有人在真机上装过 VSCode 扩展** | 按 **[WINDOWS-CHECKLIST.md](WINDOWS-CHECKLIST.md)** 逐条勾选：装包 → 内置二进制 → LSP 在 UI 里的表现 → ⭐路径/`.link` 诊断 → scp 部署 |
| B2 | **macOS 实机**（从未验证过） | CI 已产出 `darwin-x64` / `darwin-arm64` VSIX | 装包 → 打开 `.4gl` → 语言服务器运行中、`tiptop version` 可执行 |
| B3 | 真实 Genero 服务器 | 无本机 `fglcomp`/`fglform` | `ssh` 通、`fglcomp -M` 可用、单文件部署 + 远程编译实跑一次 |
| B4 | Zed dev extension 手工加载 | 无图形界面 | `zed: install dev extension` 能加载，LSP 起来，大纲层级正确 |
| B5 | `record-member-unknown` 默认级别 | 默认 `off`（`LIKE table.*` 占 87% 引用） | 真实使用反馈后决定是否改 `warning` |
| B6 | P3a `variable-source-unresolved` 人工标注 | `scripts/globals-audit.sh` **未做** | 抽 100 处人工标注，算出误报率 |
| B7 | `expression name:` 跳过规则抽查 | 已修，未逐条复核 | 跑语料差异清单，人工抽查是否误伤 |
| B8 | ~~Windows URI 根因假说~~ | ✅ **已定位并修复（2026-10-23）**：不是客户端入参问题，而是**服务端自己手写 path→URI**（`internal/server/links.go` 的 `fileURI`、`wsindex.go` 的 `pathURI`）在 Windows 上产出非法的 `file://C:/…`（盘符落进 authority）。链接资料的诊断挂不上文件、索引位置打不开 | 见 B9 |
| B9 | ~~Windows `go test` 在 CI 红~~ | ✅ **已解决（2026-10-23）**：bdl-lsp 与 tiptop 均 **Linux + Windows 双绿**。三类根因：① `scripts/build.ps1` 还在找兄弟 grammar 目录（构建步就红）；② 测试用 `"file://"+路径` 拼 URI（30 例连锁失败）；③ 生产侧同一个手写拼接（B8）；④ tiptop 缺 `.gitattributes`，Windows checkout 成 CRLF → `gofmt -l` 报全部文件 | 见 CI 运行记录 |

**优先级**：B1 和 B2 最值得做——CI 已经产出这两个平台的包，但**从来没有人装过**。
"CI 绿"不等于"能装上跑起来"。

> **Windows CI 这一轮还清掉了两条隐患**（都不在"等机器"的范畴，是真实缺陷）：
> - 服务端 path→URI 手写拼接（B8）——Windows 用户看不到 `link-data-*` 诊断、索引位置打不开；
> - tiptop `secrets.json` 的 0600 校验在 Windows 永远判"过松"（F7）——这条密码来源等于不可用。
>
> 教训已写进 [HANDOVER](HANDOVER.md) §7（坑 #37 手写 path→URI、#38 CRLF 与 gofmt）。

---

## C. 发布相关

| # | 事项 | 说明 |
|---|---|---|
| C1 | **GitHub Release 自动化** | 目前只有 Actions artifact（**90 天过期**）。`bundle-vsix.yml` 加 `release` job，四个 VSIX 一起挂 Release |
| C2 | **`bdl-lsp` 没有 tag** | `git tag` 为空。`tiptop/go.mod` 写 `require ... v0.3.1` 但**没有对应 tag**，只能靠 `replace` 指本地目录 |
| C3 | `tiptop/go.mod` 的 `replace` | 发布前换成真实版本号；否则 `go install github.com/darcyjoven/tiptop@latest` 不可用 |
| C4 | tag 触发时 ref 不对 | `bundle-vsix.yml` 的 `${{ inputs.*_ref \|\| 'main' }}`：tag 推送时 `inputs` 为空 → 回落到 `main`，不会用同名 tag。要做 tag 精确对应需改成 `github.ref_name` 回退 |
| C5 | VSCode Marketplace / Open VSX | **未排期**（用户决定） |
| C6 | 二进制自动下载 / 校验 / 缓存 | "无感"体验，**未排期** |
| C7 | Zed 注册表发布 | **未排期** |
| C8 | `PROJECT-SETUP.md` 占位符 | `<用户名>`/`<生产主机>`/`<Oracle主机>` 需按实际环境替换（可选） |
| C9 | 文档截图 / 录屏占位 | `vscode-extension/docs/images/`、`zed-extension/docs/images/zed/README.md` 列了要补哪些图 |

**C1 + C2 + C3 是一组**：想把 tiptop 通过 `go install` 分发，必须先给 bdl-lsp
打 tag、再去掉 `replace`；想给用户一个"不用 90 天内下载"的产物，需要 C1。

---

## D. Zed 扩展（**追平中**，`zed-bdl 0.1.1`）

> **用户 2026-10-23 决定重启**：按 VSCode 的进度追平 Zed 插件。
> **完整计划与设计依据见 [zed-extension/docs/PLAN.md](../../zed-extension/docs/PLAN.md)**
> （约 800 行：平台能力核实、Code Lens 方案、差距矩阵、已知坑）。
> 本节是**可勾选的待办清单**，按「是否需要动其他仓库」分成两类。
>
> **关键前提（已核实）**：Zed 无侧边栏树视图 API → 链接资料改用
> **Code Lens 挂在 MAIN 头上**（服务端 `textDocument/codeLens`，
> 与 §P 的 P1-8 **同一份实现**，两端共用）。

### D-A. 只需改 `zed-extension/`（不动其他仓库）

| # | 事项 | 做法要点 | 验收 |
|---|---|---|---|
| Z-A1 | 文档纠错（9 条） | `bdl-lsp link/deploy` → `tiptop link/deploy`（这两个子命令 0.3.2 已移除）；删 `link.maxAgeDays` 与整块 `link.sync.*`；`link.dataDir` 默认 `.project`；`link.enable`/`schema.enable` 默认 **false**；诊断码表同步 14 个码；补 `prod.*` 说明；修「Zed 不支持任意命令」「不消费语义 token」两处错误结论；版本号 0.3.0→0.3.11 | `zed-extension/README.md` + `docs/USER-GUIDE.md`；全文 grep 无过期命令名 |
| Z-A2 | **补轻量档默认**（真实行为差异） | VSCode 客户端在 `client.ts:147` 显式下发 `link/schema.enable=false`；**Zed 侧是原样透传**，服务端兜底返回 `true`（`internal/config/config.go:161,228`）→ Zed 用户开箱即用时链接资料与 schema 是**开着的**，与 VSCode 相反。修法：在 `language_server_workspace_configuration` 返回前把缺失的三开关补成 `false` | Zed 开箱即轻量档；与服务端兜底逻辑一致（兜底仍是"开"，那是无客户端设置时的安全兜底） |
| Z-A3 | 语义高亮落地 | 加 `languages/bdl/semantic_token_rules.json`（+ `per/`）；文档教用户开 `{"languages":{"Genero BDL":{"semantic_tokens":"combined"}}}`（**默认 off**，必须写清）；`verify-manifest.py` 加校验防误删 | 开 `semantic_tokens` 后自定义符号着色；`verify-manifest.py` 全绿 |
| Z-A4 | slash command 入口 | 实现 `run_slash_command` + `extension.toml` 声明 `[slash_commands.*]`；4 条：`/bdl-link-status`、`/bdl-deploy-dry-run`、`/bdl-deploy-list`、`/bdl-force-parse`；走 `zed::process::Command` 调 tiptop（需新增「定位 tiptop」四级查找）；**采集/导入类（会写盘）不进 slash command**，只给 task | 命令面板/Agent 面板可用；新增校验：extension.toml 声明与 Rust `match` 分支一一对应（写错名不会编译错，只会静默不响应） |
| Z-A5 | task 模板更新 | 命令名改 `tiptop`；补 `deploy -list`；补 4 条链接资料 task | `docs/USER-GUIDE.md` §7 |
| Z-A6 | Code Lens **实机验证** | 两个关键行为决定实现方式：① `code_lens: "on"` 时**多条同 range 的 lens 是否各占一行**；② 点击 lens 的 `command` 能否打开目标文件 | **有 GUI 的机器**上实测（本沙箱做不了） |
| Z-A7 | 「VSCode 独有 / Zed 降级」对照表 | USER-GUIDE 新增一节：Zed 无侧边栏树 → 用 MAIN 头上的 Code Lens；反查为**函数级** | 文档与实现一致 |
| Z-A8 | `.project/deploy.json` 生成助手 | Rust 侧从 `prod.deploy.*` 合成（对应 VSCode `src/deployconfig.ts` 的 `ensureDeployConfig`） | 与 VSCode 行为一致 |
| Z-A9 | 扩展版本 0.1.1 → 0.2.0 | 版本策略：每完成一轮完整改动 +1（与 HANDOVER 一致） | `extension.toml` + `Cargo.toml` 同步 |

### D-B. 需要动其他仓库（🔒 需授权）

| # | 事项 | 仓库 | 做法要点 | 验收 |
|---|---|---|---|---|
| 🔒 Z-B1 | **`workspace/executeCommand` 通道** | `bdl-lsp` | `ServerCapabilities`（`internal/server/server.go:363-394`）加 `ExecuteCommandProvider`；新增 `workspace/executeCommand` handler，把现有 `bdl/linkTree`/`bdl/linkReferences`/`bdl/forceParse` 三个自定义请求**转接**为标准命令（§6-①） | 协议级测试；VSCode 侧不受影响（继续用自定义请求） |
| 🔒 Z-B2 | **Code Lens 服务端实现**（**两端共用**） | `bdl-lsp` | 实现 `textDocument/codeLens`：链内子文件各一条 lens 挂 MAIN 头上（数据来自 `link.Resolver.Tree()`，**纯内存查询**）；`bdl.link.lensMax` 配置（默认 10）；函数级反查用 `wsindex.Index.CallsOf`（调用点已存 `Name/File/Line/Col`）；**依赖 Z-B1**（点击命令） | 与 §P 的 P1-8 同一份实现；协议级测试 |
| 🔒 Z-B3 | 二进制自动下载 | `bdl-lsp` + `tiptop` | Zed 扩展用 `latest_github_release` → `current_platform()` 拼资产名 → `download_file` → `make_file_executable`；**前置是 bdl-lsp 要有 GitHub Release**（§C1/C2/C3 未做） | 扩展侧安装即用，不用手工 `make build` |
| 🔒 Z-B4 | 文档同步 | `bdl-lsp/docs` | 本节（§D）；`HANDOVER.md` 的 §0/§4/§8；`FEATURES.md` 补 Code Lens 行 | 三处口径一致 |

### D-C. 已决定不做

| # | 事项 | 原因 |
|---|---|---|
| ❌ | Zed 侧**复刻** VSCode 侧边栏 TreeView | Zed 无树视图 API；改用 MAIN 头上的 Code Lens（§D 关键前提） |
| ❌ | 把链接树从 bdl-lsp **移到 tiptop** | **用户 2026-10-23 决策：link 分析保持原状。**（分析见 zed PLAN §3.6：移走省不下开销 —— `Resolver.Tree()` 本就是纯内存查询，真正解析源码的采集阶段早就在 tiptop） |
| ❌ | Call Hierarchy（`prepareCallHierarchy`） | **语义不等价**：call hierarchy 基于源码调用点，链接资料来自数据库的程序-文件映射；"链接进链但没被调用"的文件它看不到，而那正是业务要看的 |
| ❌ | LSP `documentLink` | **位置分散**在文中（每个子文件一个 link 节点散落各行），正是 Code Lens 要避免的 |
| ❌ | slash command 里做**会写盘**的采集/导入 | 误触成本高；这类操作只给 task（显式确认） |

**注意**：`zed-extension/extension.toml` 的 `language_servers.bdl-lsp` 仍指向
`bdl-lsp`——这一层**不需要改**（语言服务器职责没变），只有"采集/部署"的命令名要改。

---

## E. 未排期功能 / 刻意不做

| # | 事项 | 状态 |
|---|---|---|
| E1 | P7-D 链接资料分片 | 触发条件 >8 MB；当前 415 KB，**远未到** |
| E2 | 跨文件重命名 | **刻意不做**：索引里的调用点是名字匹配，可能命中同名局部变量，漏改比不支持更糟（见 [PLUGIN-PLAN](PLUGIN-PLAN.md) 附录 A） |
| E3 | 使用处语义着色 | 需要全文件符号解析，成本高收益低 |
| E4 | Code Action 批量修复（fix all） | 未排期 |
| E5 | 纯 Go SFTP | 仅在确有需求时评估 |
| E6 | Genero 5.0 语法 | 未排期（当前 2.50 基线） |
| E7 | 单文件远程编译超时；编译产物 `.42m` 是否拉回 | 现属 tiptop，待确认 |
| E8 | 安装时提示把 `.link/` 加 `.gitignore` | **刻意不做**（不擅自改用户文件） |
| E9 | 本地 Oracle 驱动（Instant Client / CGO） | **已取消**，只保留纯 Go `go-ora` |

---

## F. 已知风险跟踪

| # | 风险 | 说明 |
|---|---|---|
| F1 | **tiptop 与 bdl-lsp 版本漂移** | `tiptop/go.mod` 的 `replace` 指本地目录，而 CI 里两个仓库各自用 `main`。任一侧改了公开包签名，另一侧可能构建失败且**不报错到开发期**。缓解：给 bdl-lsp 打 tag（C2）+ tiptop 加 CI（A8） |
| F2 | ABI 15 ↔ Zed tree-sitter 运行时兼容 | 未在 Zed 实机验证 |
| F3 | 扩展与 LSP/tiptop 版本错配的回退策略 | 三个二进制版本独立递增，没有兼容矩阵 |
| F4 | Windows 无 rsync → scp 回退 | 稳定性/性能未实测 |
| F5 | 沙箱 `HOME`/`GOPROXY` 对文档/CI 的影响 | 本地构建需 `source scripts/dev-env.sh` |
| F6 | macOS 延后带来的构建差异 | CI 能构建，但**无人验证过产物**（同 B2） |
| F7 | ~~Windows 上 `.link/secrets.json` 不可用~~ | ✅ 已修（tiptop 0.1.2）：该项按 unix 权限位校验，而 Windows 的 mode 位是 Go 合成的（可写文件一律 0666）→ 永远判"权限过松"。改为 `secretsPermTooLoose()`：Windows 跳过校验（ACL 不归我们判断），文档说明 |
| F8 | ~~Windows `go test` 未绿~~ | ✅ 已修（2026-10-23）：见 B9，两个仓库 Linux + Windows 双绿 |
| F9 | bdl-lsp 仍无 tag | 同 C2；tiptop 的 CI 因此只能追 `main`，版本漂移风险仍在（F1） |

---

## G. 建议的推进顺序

1. ~~**§A 全部**~~ —— ✅ 已完成 2026-10-23
2. **§P 插件改版**（发布前）：P0-1 ✅、P0-2 ✅、P0-3 ✅、P1-2 ✅ →
   P1-3 ✅ → P1-1 ✅ → P1-4 ✅ + P1-5 ✅ → P1-6 ✅ → **P2-1 ✅（schema，提前到发布前做）** →
   现在只剩**发布闭环**：
   §B1 Windows 实机（按 `WINDOWS-CHECKLIST.md` 走一遍）+ B2 macOS → §C1 打 tag → C2 Release（四个 VSIX + 二进制）→ C3 市场/注册表（暂缓，用户已定）
   文档项：P1-7（用户文档重写）可与发布并行；§D/§E/§F 持续跟踪
3. **§D Zed 追平**（2026-10-23 重启）：
   - **Z-B1**（`bdl-lsp` 加 `workspace/executeCommand`）→ **Z-B2**（Code Lens 服务端实现，
     **§P 的 P1-8 与它共用同一份**）→ Zed 侧 Z-A1…Z-A7；
   - Zed 侧 **Z-A6（Code Lens 实机验证）**应**先做**——它决定 Z-B2 的实现方式；
   - Z-B3（自动下载）依赖 §C1/C2/C3。
4. **§B1 + §B2**（需要机器；这两项决定"能不能交付给别人用"）
5. **§C1 + C2 + C3**（发布闭环；做完才算"可分发"）
6. §E / §F（持续跟踪）

---

## 附：下一会话开场语

```text
读 bdl-lsp/docs/PENDING.md 与 docs/PLUGIN-PLAN.md，开始**发布闭环**（§G 第 3–4 步）：
先按 docs/WINDOWS-CHECKLIST.md 做 Windows 实机验收，再打 tag（C1）与 GitHub Release（C2）。
```

```text
做 Zed 追平：读 zed-extension/docs/PLAN.md（完整计划）+ 本文件 §D（待办清单）。
先做 Z-A6（Code Lens 实机验证），再做 Z-B1 → Z-B2。
```