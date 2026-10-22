# P7 计划：缺陷修复 · 内置符号 · 链接资料索引 · 诊断级别统一

> 版本：v1.0（立项）  
> 日期：2026-10-21  
> 状态：**规划完成，待执行**。P7-A（两个已确认 BUG）为下一轮首先处理项。  
> 上游文档：[HANDOVER.md](HANDOVER.md)（接手）· [TODO.md](TODO.md)（待办）· [PLAN.md](PLAN.md)（总规划）  
> 约定：本文所有"已验证"结论均附实测证据；未验证的一律标 **假设（需确认）**。
>
> **2026-10-23 注**：本篇是当时的规划/实现记录。链接资料与部署已于
> `bdl-lsp 0.3.2` 移到 **tiptop**——下文 `bdl-lsp link import|status|sync`、
> `bdl-lsp deploy`、`cmd/link-audit` 均应按 tiptop 的对应实现阅读（其中
> `cmd/link-audit` 未随之迁移，审计数字已固化在本文）。正文不改写。
> 见 [PLAN-SEPARATION.md](PLAN-SEPARATION.md) 与 [PENDING.md](PENDING.md) §A。

---

## 0. 立项背景

P1–P6 已完成并推送（LSP 20 个方法、269 个测试、VSCode 32 项配置）。  
2026-10-21 在 **Windows** 实测发现 2 个缺陷，并提出 1 项架构性新功能（程序链接资料）与 1 项横切改造（诊断级别统一）。

本轮要解决的核心矛盾是一条**误报链**。以下是本次对真实语料的实测（24 个 `.4gl`，`/code/workspace/tiptop-gp`）：

| 指标 | 实测值 |
|---|---|
| 诊断总数 | 1733 |
| `variable-source-unresolved` | **1717（99.1%）** |
| `field-not-in-form` | 15 |
| `file-too-large` | 1 |

Top 未解析名字（前 6 名占 1216/1717 = 71%）：

| 名字 | 次数 | 真实身份 | 应由谁解决 |
|---|---|---|---|
| `cl_null` | 311 | 用户函数，定义在 `lib/4gl/cl_null.4gl` | **P7-D 链接资料**（`lib` 是默认链接模组） |
| `SQLCA` | 252 | Genero **内置变量** | **P7-C 内置符号** |
| `ARG_VAL` | 248 | Genero **内置函数** | **P7-C 内置符号** |
| `INT_FLAG` | 183 | Genero **内置变量** | **P7-C 内置符号** |
| `cl_gr_numfmt` | 150 | 用户函数，`sub/` 或 `lib/` | P7-D |
| `STATUS` | 98 | Genero **内置变量** | P7-C |

**结论**：当前 T5 变量来源检查在真实工程上的噪声已经高到不可用（单文件 50–61 条）。  
必须 P7-C（内置符号）+ P7-D（链接索引）+ P7-B（调用点误报修正）三者配合才能降到可接受水平。  
这决定了 P7 不是一个"锦上添花"的功能期，而是**可用性修复期**。

---

## 1. 阶段划分与执行顺序

| 阶段 | 内容 | 规模 | 依赖 | 建议顺序 |
|---|---|---|---|---|
| **P7-A** | Windows 两个 BUG：Zed 大纲层级、GLOBALS 路径识别 | 小（1–2 块） | 无 | **1（下一轮）** |
| **P7-B** | 诊断级别统一可配置 + 调用点误报修正 | 中（2 块） | 无 | 2 |
| **P7-C** | 内置函数/变量符号表 + grammar 缺口清单 | 中（2–3 块） | P7-B | 3 |
| **P7-D** | 程序链接资料：采集（3 种）+ 索引 + 可见性 | 大（6–8 块） | P7-B、P7-C | 4 |
| **P7-E** | 链接资料 UI（VSCode TreeView / Zed 降级） | 中（2 块） | P7-D | 5 |

顺序理由：

1. P7-A 是确定性缺陷、根因已定位、验收标准明确 → 先清障。
2. P7-B 先做，因为后面所有新诊断（`function-not-found`、链接类）都要挂进统一级别表；先有框架再加条目，避免二次返工。
3. P7-C 比 P7-D 小得多，且能立刻消掉约 40% 误报；P7-D 需要用户提供真实链接数据，可并行推进。
4. P7-E 纯展示层，依赖 P7-D 的索引结果，最后做。

---

## 2. P7-A：两个已确认缺陷（下一轮处理）

### 2.1 A-1 Zed 大纲把同一条 `DEFINE` 的多个变量变成层级

**现象**（用户报告）：

```bdl
DEFINE l_name    LIKE type_file.chr20,
       l_sql     STRING
```

Zed 大纲里 `l_sql` 出现在 `l_name` 的下一层；VSCode 正常（两者平级）。

**根因（已实测确认）**：

`zed-extension/languages/bdl/outline.scm` 的规则是：

```scheme
(define
  name: (identifier) @name) @item
```

而 grammar 中一条 `DEFINE` 是**单个节点带多个 `name:` 字段**（`rule/type.js` 的 `_variable_list` 为 `commaSep1(...)`）。实测语法树：

```text
(define [1, 4] - [3, 24]
  name: (identifier ...)     ← l_name
  types: (types ...)
  name: (identifier ...)     ← l_sql
  types: (types ...))
```

用 `tree-sitter query` 跑现有 outline 实测输出：

```text
pattern: 6
  capture: 1 - item, start: (2, 4), end: (2, 52)   ← 同一条 DEFINE，两个 match
  capture: 0 - name, ... text: `l_name`
pattern: 6
  capture: 1 - item, start: (2, 4), end: (2, 52)   ← item 区间完全相同
  capture: 0 - name, ... text: `l_sql`
```

Zed 的大纲按 `@item` 区间构建父子关系：两个 match 的 `@item` 区间**完全重合**，Zed 认为后一个是前一个的子节点 → 出现层级。

VSCode 之所以正常：`internal/server/symbols.go` 的 `toDocumentSymbol` 从 `index.FileIndex.TopLevel` 逐条生成，`collectDefines` 已把每个变量拆成独立 `Symbol`，天然扁平。

即：**这不是 LSP 服务端的问题，是 Zed query 的问题**。

**同样受影响的规则**（同一 `_variable_list` 结构，本次一并修）：

| 语法 | 示例 | 现状 |
|---|---|---|
| `DEFINE` | `DEFINE a INT, b STRING` | ❌ 层级错误 |
| `TYPE` | `TYPE t1 RECORD ... END RECORD, t2 STRING` | ❌ 同样错误 |
| `CONSTANT` | `CONSTANT c1 = 1, c2 = 2` | ❌ 同样错误 |

**修法**：把 `@item` 从"整条语句"改到"名字节点本身"，让每个 match 的 item 区间互不相同。

```scheme
; 修改前
(define name: (identifier) @name) @item
; 修改后
(define name: (identifier) @name @item)
```

`type`、`constant` 同样处理。实测改后输出（`/tmp/q3.scm`）：

```text
pattern: 2  capture: 0 - name ... text: `g_a`   capture: 1 - item ... text: `g_a`
pattern: 2  capture: 0 - name ... text: `g_b`   capture: 1 - item ... text: `g_b`
pattern: 2  capture: 0 - name ... text: `l_name`  capture: 1 - item ... `l_name`
pattern: 2  capture: 0 - name ... text: `l_sql`   capture: 1 - item ... `l_sql`
```

每个变量一条独立条目，区间不重合 → 层级消失。

**副作用评估**：`@item` 从语句缩到标识符后，大纲条目的高亮/定位范围变小（点条目跳到变量名而不是 `DEFINE` 行首）。对导航而言更准确，可接受。

**回归防护**：`zed-extension/tests/verify-queries.mjs` 增加一条新检查——

> 对含多变量 `DEFINE` / 多 `CONSTANT` / 多 `TYPE` 的 fixture 跑 outline query，断言 **所有 `@item` 的 (start,end) 两两不同**。

这条检查能在 CI 上直接复现本 BUG，不需要 GUI。这是必须项：Zed 的层级问题只在 GUI 可见，没有这条断言下次还会回归。

**验收**：

- [x] `node tests/verify-queries.mjs` 新增断言通过，且在旧 query 上**必然失败**（先写断言复现）。
- [ ] 手工验证（需 GUI）：`DEFINE a INT, b STRING` 在 Zed 大纲中 `a`、`b` 同级。
- [x] VSCode 大纲行为无变化（走 `documentSymbol`，不受 Zed `@item` 影响；go 测试覆盖）。

---

### 2.2 A-2 VSCode 下 GLOBALS 文件路径识别不到（Zed 可以）

**现象**（用户报告）：同一个 Windows 工程、同一份 `GLOBALS "../../config/top.global"`，Zed 能解析，VSCode 报找不到。

**根因（已定位，待 Windows 确认）**：

`internal/globals/globals.go` 的 `PathOfURI` 是手写实现，只做两件事：剥 `file://`、把 `%20` 换成空格。实测：

```text
file:///C:/work/my%20proj   -> "C:/work/my proj"     ✅
file:///C%3A/work/proj      -> "/C%3A/work/proj"     ❌
file:///c%3A/work/proj      -> "/c%3A/work/proj"     ❌
```

VSCode 的 `vscode.Uri.toString()` 在 Windows 上把盘符冒号**百分号编码**，实际下发的是 `file:///c%3A/...`；Zed 走自己的 URI 实现，下发 `file:///c:/...`（冒号不编码）。这正好解释"VSCode 认不到、Zed 认得到"。

`/c%3A/...` 不是合法 Windows 路径，于是：

1. `didOpen` 的文档 URI 解出 `/c%3A/...` → `resolveImports` 里 `docPath = /c%3A/...`；
2. R2 规则 `filepath.Join(filepath.Dir(docPath), "../../config/top.global")` → `\c%3A\config\top.global`；
3. `fileExists` 失败 → `StatusNotFound` → 报 `globals-file-not-found`。

同一函数还牵连三处：

| 调用点 | 影响 |
|---|---|
| `server.go:235` `s.rootPath = globals.PathOfURI(rootUri)` | `searchPaths` 全部失效 |
| `config.go` `uriToPath`（**第二份重复实现**） | `.bdl-lsp/config.json` 永远读不到 |
| `PathOfURI(docURI)` 各处 | 文档级路径全部错误 |

**修法**：新建 `internal/fileuri` 包，替换两处手写实现。

```go
// ToPath 把 file:// URI 解码为本地路径（全量百分号解码 + 盘符/UNC 归一）。
func ToPath(uri string) (string, error)
// FromPath 把本地路径编码为 file:// URI（盘符冒号不编码，与 LSP 惯例一致）。
func FromPath(p string) string
```

要求覆盖的用例（全部写成单测，**在 Linux 上即可复现 Windows BUG**）：

| 输入 URI | 期望（Windows 语义） |
|---|---|
| `file:///c%3A/work/x.4gl` | `c:\work\x.4gl` |
| `file:///C%3A/work/x.4gl` | `C:\work\x.4gl` |
| `file:///c:/work/x.4gl` | `c:\work\x.4gl` |
| `file:///work/a%20b/x.4gl` | `\work\a b\x.4gl` |
| `file://server/share/x` | `\\server\share\x`（UNC） |
| `file:///a%23b/x.4gl` | `\a#b\x.4gl`（`#` 也被 VSCode 编码） |
| `file:///d/%E4%B8%AD/x.4gl` | 非 ASCII 正常解码 |

另需一并修的两处健壮性问题：

1. **`rootUri` 为空时没有回退**。现在只读 `params.RootURI`；多根工作区或"只打开单个文件"时 `rootUri` 可能为 `null`，而 `workspaceFolders` 有值。修法：`rootUri → workspaceFolders[0].uri → rootPath` 三级回退。
2. **`config.uriToPath` 与 `globals.PathOfURI` 重复实现**，已出现行为漂移风险，统一到 `internal/fileuri`。

**确认步骤（下一轮第一步做，避免盲改）**：

在 Windows 上打开 VSCode，`--log-level debug` 启动 LSP，对比两个客户端的 `initialize` 入参：

```text
VSCode: rootUri = "file:///c%3A/..."      ← 预期
Zed   : rootUri = "file:///c:/..."        ← 预期
```

若实测与预期不符，按实测值调整用例表（修法不变，`fileuri` 对两种形态都兼容）。

**验收**：

- [x] `internal/fileuri` 单测覆盖上表 7 条 + 往返（8 个测试函数）。
- [x] `go test ./internal/globals ./internal/config ./internal/server` 全绿。
- [x] 协议级测试：用 `rootUri=""` + `workspaceFolders=[...]` 初始化，断言 `rootPath` 正确
      （`internal/server/rooturi_test.go`）。
- [ ] Windows 实机：打开 `axmt620.4gl`，`GLOBALS` 两行均无 `globals-file-not-found`。

---

## 3. P7-B：诊断级别统一可配置 + 调用点误报修正

### 3.1 B-1 所有诊断统一 warn / error / info / hint / off

**用户要求**：所有检查项都要能配置级别，默认 `warning`；之后新增功能默认带可配置项。

**现状盘点**（`internal/` 已实现的诊断码）：

| 诊断码 | 当前默认 | 已有独立配置项 | 备注 |
|---|---|---|---|
| `syntax-error` | **error** | `syntaxDiagnostics` | grammar ERROR 节点 |
| `missing-*` | warning | `missingDiagnostics` | grammar MISSING 节点 |
| `globals-file-not-found` | warning | `globals.fileNotFoundDiagnostics` | |
| `variable-source-unresolved` | warning | `globals.unresolvedSourceDiagnostics` | |
| `field-not-in-form` | warning | `fieldDiagnostics` | |
| `form-not-found` / `form-ambiguous` | warning | 共用 `fieldDiagnostics` | |
| `form-duplicate-item` / `form-layout-tag-undefined` / `form-screen-record-undefined` | warning | 共用 `fieldDiagnostics` | |
| `record-member-unknown` | **off** | `recordMemberDiagnostics` | 语料 87% 不可校验 |
| `file-too-large` | **information** | 无 | 提示不是错误 |
| `function-not-found` | **off** | 无（新增） | 实测 24 文件 3660 条，等 P7-C/D |

**设计**：新增统一配置块 `bdl.diagnostics`，按诊断码取值。

**实际落地时砍掉了 `"*"` 通配键**：注册表已经给每个码一个出厂默认，
通配键既多余、又会让"我明明关掉了却还报"难以排查（分不清是通配键还是
注册表默认生效）。要批量调级别就逐个码写，工具是 `make diag-audit`。

```jsonc
{
  "bdl.diagnostics": {
    "variable-source-unresolved": "warning",   // 默认值，写不写都一样
    "record-member-unknown": "off",            // 默认 off
    "function-not-found": "off"                // 默认 off
  }
}
```

要点：

1. **集中注册表**：Go 侧建 `internal/diag/registry.go`，每个码登记 Code + 默认级别。级别解析统一走 `diag.SeverityFor(code, settings, legacy)`，删除各功能里散落的 level 判断。
2. **向后兼容**：现有独立配置项（`syntaxDiagnostics`、`globals.*`、`fieldDiagnostics`、`recordMemberDiagnostics`）继续生效，优先级 `diagnostics.<code>` > 旧键 > 注册表默认。旧键在文档中标记 deprecated，**至少两个版本内不删**。

> **2026-10-23 更新（0.3.0）**：用户决定提前删除旧专用键，统一走
> `bdl.diagnostics`；本节以下关于旧键兼容的内容是当时的决策记录，不再代表当前行为。
3. **拼错保护**：`diagnostics` 里出现注册表没有的码 → 记 `warn` 日志（客户端 toast 可选）。现在的配置拼错是静默失效，这个坑必须堵。
4. **清单防漂移**：仿照现有 `keywords-check`，新增 `make diagnostics-check`，校验三处一致：Go 注册表 ↔ `docs/DIAGNOSTICS.md`（自动生成）↔ VSCode `package.json` 的 enum 列表。

**Q10 定稿：全部默认 `warning`。**

用户明确要求"默认都是 warn 警告"。这意味着 `syntax-error` 从 `error` 降为 `warning`。

需要记录的影响（不改变决定，只是写清后果）：

- 语法错误在 Problems 里变成黄色波浪线，不再是红色。用户可自行为 `syntax-error` 配回 `error`。
- `record-member-unknown` 保持 `off`（它不是"默认级别"，而是默认关闭的可选检查；用户也未要求开启）。
- 旧配置项里 `syntaxDiagnostics` 默认值是 `error`，需要同步改为 `warning` 以保持一致——否则走旧键的用户与走新键的用户行为不一致。

> **执行结果（2026-10-21，`0.1.2`）**：注册表、新配置键、拼错保护、
> query 误报修正、`function-not-found`、`cmd/diag-audit` 全部落地；
> `syntaxDiagnostics` 默认已改为 `warning`。详见 §10.5。
> 计划里写的 `make diagnostics-check`（Go 注册表 ↔ 文档 ↔ 清单三方校验）
> **未做**——当前只有 `cmd/diag-audit -codes` 能打印注册表内容，
> 防漂移脚本留到 P7-C 加内置符号表时一起做（那时码表会明显变大）。

### 3.2 B-2 调用点被当成"未定义变量"（实测发现的缺陷）

**现象**：`IF cl_user() THEN` 中的 `cl_user` 报 `variable-source-unresolved`。

**根因**：`internal/server/globals.go` 的 `queryVariables` 只跳过 `(call name: ...)`（`CALL` 语句的函数名），但表达式位置调用的语法结构是：

```text
(expression
  name: (variable (identifier))     ← 函数名
  params: (expression ...))         ← 参数
```

`(variable (identifier) @use)` 会命中这里的 `identifier`，于是**每个函数调用都被当成变量检查**。

这不仅是误报，更是**分类错误**：函数调用应该查"函数可见性"，不是"变量来源"。

**修法**（与 P7-D 联动）：

1. query 增加跳过：`(expression name: (variable (identifier) @skip))`。
   注意：`expression` 的 `name` 字段只在函数调用时出现（实测 `LET x = 1+2` 无 `name` 字段），先跑语料确认无副作用。
2. 新增独立诊断 `function-not-found`，由 P7-D 的链接索引判定；**没有链接资料时不报**（避免把现在的噪声换个名字继续存在）。
3. `variable-source-unresolved` 只保留真变量。用户明确的语义规则不变：

   > globals 变量只看当前文件定义或引入；不用索引链接的子文件。没定义/没引入就是未定义变量，应该警告。

**验收**：

- [x] 语料复测：24 文件 `variable-source-unresolved` **1717 → 889**（−48%）。
      剩余全不是“调用点”：是内置全局（`SQLCA`/`INT_FLAG`/`STATUS`…），
      属于 P7-C。全量基线见 §10.5。
- [x] 单测：`IF fn() THEN`、`CALL fn()` 都不再产生 `variable-source-unresolved`
      （另加 `WHENEVER ERROR CALL f`、`DISPLAY … TO FORMONLY.x`）。
- [x] `function-not-found` 默认 **off**，无链接资料时恒为 0 条；
      **实测**：一旦打开，24 个真实文件立刻报 3660 条（内置函数 + 跨模块），
      所以“默认 off”不是保守，是必须。

---

## 4. P7-C：内置函数与变量（用户提问的答复）

### 4.1 结论：分两类，走两条路

用户问："有部分 Genero BDL 内置的函数和变量在 tree-sitter 中没有定义，我是在插件中写，还是修改 tree-sitter 逻辑？"

**判据（一句话）**：

> 用 `tree-sitter parse` 解析该写法，**有 ERROR/MISSING → grammar 缺口 → 改 grammar**；  
> **没有 ERROR 但语义未知 → 插件侧符号表**。

因为这个判据可以直接跑，不存在模糊地带。本次实测结果：

| 写法 | 解析结果 | 归类 | 结论 |
|---|---|---|---|
| `DISPLAY fgl_getenv("X")` | 0 ERROR | 语义 | 插件侧 |
| `DISPLAY ARG_VAL(1)` | 0 ERROR | 语义 | 插件侧 |
| `LET s = STATUS` | 0 ERROR | 语义 | 插件侧 |
| `LET SQLCA.SQLCODE = 0` | 0 ERROR | 语义 | 插件侧 |
| `DISPLAY INT_FLAG` | 0 ERROR | 语义 | 插件侧 |
| `CALL SET_COUNT(g_rec)` | 0 ERROR | 语义 | 插件侧 |
| `DISPLAY ARRAY a TO sa.*` | 0 ERROR | 语义 | 插件侧 |
| `STARTLOG "x"` | **3 ERROR** | **语法** | **改 grammar** |
| `SET_COUNT 10`（无 CALL） | 4 ERROR | 语法 | 但这不是合法 BDL，**不补** |

### 4.2 为什么内置符号必须放插件侧（不能塞进 grammar）

1. **会破坏高亮**。塞进 grammar 意味着它们变成 keyword token，`highlights.scm` 会按关键字染色，用户自定义的同名符号也会被冒充（BDL 标识符大小写不敏感，`status` 是常见变量名）。
2. **会污染重命名与补全**。`internal/server/rename.go` 七道闸门里有"新名不是关键字"检查；内置函数一旦是关键字，`ARG_VAL` 就不能被当作普通标识符处理，语义判定全线复杂化。
3. **维护成本错位**。Genero 每个版本新增内置函数/类方法，grammar 改一次要重生成 `parser.c`（CGO）+ 全量语料回归（7699 文件）；插件侧改一行表即可。
4. **grammar 的职责是语法，不是名字表**。`rule/type.js` 里 `builtin` 目前只放 `NULL/TODAY/CURRENT`（真正的字面值常量），这个边界是对的，不要扩大。

**语法缺口则必须改 grammar**，因为语法树错了会连带高亮、大纲、格式化、缩进全部错误，插件侧无法补救。

### 4.3 实施计划

**C-1 内置符号表**（`internal/builtin`）

数据分四类，都带"来源版本"标注（Genero 2.50 基线）：

| 类别 | 例子 | 用途 |
|---|---|---|
| 内置变量 | `STATUS` `INT_FLAG` `SQLCA` `SQLERRD` `SQLSTATE` `STARTLOG_*` | 未定义变量白名单 + 悬停 |
| 内置函数 | `ARG_VAL` `SET_COUNT` `DOWNSHIFT` `UPSHIFT` `LENGTH` `fgl_*` | 函数可见性 + 补全 |
| 内置类/方法 | `base.Channel` `util.StringBuffer` `ui.Dialog` `om.*` `xml.*` | 补全 + `.` 后的成员 |
| 内置常量/字面值 | `NULL` `TRUE` `FALSE` `TODAY` `CURRENT` | 已在 grammar，不重复 |

落地方式：

1. ✅ **语料驱动**的种子表~~（原计划"从参考手册手工整理"）~~：先按 `diag-audit -names`
   的频次表定"必须覆盖哪些"，再补 `fgl_*` 家族；结果 96 条，见 §10.6。
   手工抄手册会漏、会抄错版本，频次表不会。
2. ✅ `cmd/builtin-audit`：扫语料，提取"所有未被定义、且不在表里"的标识符，按调用/变量分类输出（带文件数与粗判提示），人工确认后回填。这是防止遗漏的主要手段（内置函数名几百个，靠手抄一定漏）。
3. ✅ 四个消费点接完：① `variable-source-unresolved` 白名单（立刻降噪）
   ② `function-not-found` 白名单 ③ 补全 ④ 悬停显示签名/说明/出处。

**C-2 grammar 缺口清单**（`tree-sitter-bdl`，独立仓库）

1. `cmd/grammar-audit`：对语料逐文件解析，收集 ERROR 节点上下文，按"出现最多的未知语句"排序，产出缺口清单。
2. 已知缺口先入清单：`STARTLOG`（语料 2 个文件）、`fgl_report_configure` 相关语句。
3. 优先级：**出现频次高且是语句级**的先补。`STARTLOG` 只影响 2 个文件 → 低优先级，但清单要留档。
4. 每次补 grammar 必须：加 corpus 测试 → 重生成 → 全量语料回归（`make regress`）→ 三个仓库锁 SHA。

**注意**：`cl_null`、`cl_user`、`cl_gr_numfmt` **不是内置函数**，是 `lib/` `sub/` 模组里的用户函数（已实测：`lib/4gl/cl_null.4gl:16: FUNCTION cl_null`）。它们归 P7-D 解决，不要误加进内置表——加进去会掩盖真实的链接资料缺失问题。

### 4.4 可配置 + 可后续更新（用户 2026-10-21 追加要求）

> 用户原话："内置符号，应该有配置的地方，允许后续更新。"

这句话有两种读法，两种都要满足，所以做成**三层**：

| 层 | 谁维护 | 改什么 | 要不要重编译 |
|---|---|---|---|
| ① 内置数据 | 我们 | `internal/builtin/data/builtins.json`（`go:embed` 进二进制） | 要（但要改的只是 JSON，不碰 Go 代码） |
| ② 项目覆盖 | 用户 | `<工作区根>/.bdl-lsp/builtins.json` | **不用** |
| ③ 配置项 | 用户 | `bdl.builtins.enable` / `paths` / `disable` | **不用** |

#### 为什么要 JSON 而不是 Go 字面量

内置名单是会**长**的东西：Genero 版本升级会加函数，语料里也会发现漏项。
写成 Go 字面量的话，补一条名字 = 改代码 + 重新编译 + 重新发版；
写成数据文件就只是改一行 JSON。

代价是"运行时可能读到一个坏文件"，所以：

- 数据文件带 `schemaVersion`，读到**更高**版本不拒绝、按已知字段尽力加载；
- 覆盖文件解析失败**只跳过那一份**并记 warn，内置表照常可用
  （符号表不全 ≫ 完全不可用）；
- `make builtin-check`（`cmd/builtin-audit -check`）校验格式、排序、
  重名、`kind` 合法性、`method` 缺 `class` —— 这些错在三个月后会变成
  "为什么这条没生效"，必须让机器查。

#### 覆盖语义（三条，刻意保持可预测）

1. **同名按字段合并**，不是整条替换：用户只想补一句 `doc` 或改个
   `signature` 时，不该被迫把 `kind`/`category` 全重写一遍。
   `kind` 例外，以新条目为准（它是必填字段，用户改分类时必须生效）。
2. **`disable` 优先级最高**，在所有文件加载完之后统一应用。
   否则"先 disable 再 add"会让结果依赖文件顺序，用户永远搞不清
   自己那份覆盖到底生效了没有。
3. **目录会被展开**：`bdl.builtins.paths` 里写目录就加载其中所有
   `*.json`（按文件名序），写文件就只加载那一个。

#### 什么时候生效（热更新）

- `didChangeConfiguration` → 立即重载（配置路径/禁用列表变了）；
- 用户直接编辑覆盖文件 → `MaybeReload()` 按 mtime 检查，**2s 节流**。
  节流是必须的：这段代码在每次文档诊断前跑，不能每次按键都 stat 一堆文件。
  2s 的依据是"改完 JSON 再切回编辑器"基本都超过 2s。

#### 已注册的键

```jsonc
"bdl.builtins": {
  "enable": true,                       // 关掉 = 回到 P7-C 之前
  "paths": [".bdl-lsp/team-builtins.json"],  // 额外覆盖（相对工作区根）
  "disable": ["SUM", "LENGTH"]          // 本项目里不当内置
}
```

`disable` 的典型用途：项目自己 `FUNCTION sum(...)`，内置表会把它"吞掉"，
导致这个函数既不参与 `function-not-found` 也不在补全里排前面。
禁掉之后它恢复成普通标识符。

#### 刻意没做的事

- **没有 `"*": "warning"` 式通配键**（与诊断级别一致的理由：通配键会让
  "我明明关掉了"难以排查）；
- **没有自动下载/在线更新**：内置表随扩展发布，联网拉取名单会引入
  "服务器不可达就功能降级"的隐性故障，收益不值这个复杂度；
- **用户覆盖不写回内置文件**：项目差异留在项目里，才能跟着仓库走。

### 4.5 验收

- [ ] `cmd/builtin-audit` 语料复跑后"变量位置缺口"里不再有内置名
      （`FGL_*` 前缀应为 0 条）。
- [ ] 全量语料 `make diag-audit`：`variable-source-unresolved` 从
      **287448** 降到 1 万量级。
- [ ] 24 文件复测：`variable-source-unresolved` 再降一次（预期 < 150）。
- [ ] 补全里输入 `fgl_` 能给出候选。
- [ ] `make builtin-check` 通过，且纳入 `make lint`。

---

## 5. P7-D：程序链接资料

### 5.1 需求语义（用户口述，逐条落成规则）

> 一个可执行文件（含 MAIN 函数的 4gl）通常链接多个子文件（不含 MAIN）或子模组。

| # | 规则 | 来源 |
|---|---|---|
| R1 | 可执行文件 = 含 `MAIN` 的 `.4gl`；它链接的全部 `.4gl` 里的**函数都可以调用** | 用户 |
| R2 | **GLOBALS 变量不跨链接**：只认当前文件 `DEFINE` 或 `GLOBALS` 引入的；否则告警 | 用户（明确强调） |
| R3 | `sub` / `lib` / `qry` 模组**默认被所有可执行文件链接**，不出现在链接资料里 | 用户 |
| R4 | 链接资料中 `gal04='N'` 的行**在查询阶段就过滤**，落盘的只有实际链接 | 用户 |
| R5 | 每个链接资料含 **1 个或 0 个** MAIN 程序代码 | 用户 |
| R6 | 程序编号**不一定**是可执行文件名称 | 用户 |

R2 是容易误判的地方，也是明确的设计边界：**链接只放大"函数"的可见性，不放大"变量"的可见性**。  
所以 P7-D 只影响函数解析（定义跳转/引用/补全/`function-not-found`），`internal/globals` 的解析逻辑**一行不改**。

### 5.2 数据模型与主键（关键决策）

表结构：

```sql
CREATE TABLE DS.GAL_FILE (
  gal01 VARCHAR2(20) not null,   -- 程序编号
  gal02 VARCHAR2(5)  not null,   -- 模组
  gal03 VARCHAR2(80) not null,   -- 程序代码（4gl 去后缀）
  gal04 VARCHAR2(1)              -- 链接否
);
```

样例：

```text
程序编号   模组   程序代码       链接否
axmt620   ALM    s_point        Y
axmt620   APM    sapmp500       Y
axmt620   APM    sapmt420_sub   Y
axmt620   AXM    axmt620        Y
```

**问题**：R6 说程序编号不一定是可执行文件名。如果按"程序编号"建索引，打开 `axmt620.4gl` 时无法知道它属于哪一组。

**解法**：**索引主键用"MAIN 程序代码"，程序编号只作元数据。**

理由：链接资料每组的锚点就是那一条含 MAIN 的 `程序代码`（R5 保证唯一或不存在）。打开一个文件时先判断它有没有 `MAIN`：

- 有 MAIN → 用它的程序代码作为 `main` 键直接命中链路。
- 无 MAIN → 它是子文件；需要反向查"哪些程序链接了我"（用于"谁引用我"和补全无须，但 references 需要）。

这同时自然解决 R6：程序编号只是展示用的编号，不参与 join。

**真实数据验证**（15587 行，`gal04='Y'` 共 15536 行）：

| 组内 MAIN 数 | 组数 | 说明 |
|---|---|---|
| 恰好 1 个 | **5374**（91.9%） | 主线情形，主键策略成立 |
| 0 个 | **81**（文件存在的）+ 389（全缺失） | 归一化到 `orphan` |
| ≥ 2 个 | **5** | 标 `ambiguous`，全部保留 |

另：`GAL01` 有 **160 个不在自己组内**，证实"程序编号≠文件名"的判断，主键必须用 MAIN 程序代码。

**数据完整性约束**（导入时校验，问题进报告而不是静默丢弃）：

| 检查 | 处理 |
|---|---|
| 一组内 0 个 MAIN | 允许（子模组情形）→ 归入 `orphan`（实测 81 组） |
| 一组内 ≥ 2 个 MAIN | 数据异常 → 报 warn，全部保留但标记 `ambiguous`（实测 5 组，如 `aws_ttsrv2` 同时含 `aws_ttsrv2` 与 `aws_ttsrv2_transxml`） |
| 程序代码重复出现在同组 | 去重（实测存在，如 `apmt722` 组内重复列出自身） |
| `gal04 <> 'Y'` | **SQL 阶段过滤**（R4），导入时再兜底一次（实测 51 行） |
| 程序代码在磁盘上找不到 | **保留在 `links.jsonl` 中，不忽略**；不产生任何诊断（实测 800 条/700 个代码，集中在语料中不存在的 `W*` 模组）。用户 2026-10-21 明确：链接里存在但代码不存在，忽略即可，但 jsonl 要保存 |

### 5.2.0 现状：目前**根本没有**函数存在性判定（回答 Q14）

用户问"目前函数是否存在判定是怎么判定的"。实测代码后如实回答：

**没有判定。当前只做"定义跳转"，不做"函数是否存在"的检查。**

| 环节 | 现状 | 代码位置 |
|---|---|---|
| 符号索引 | 已收录 `KindMain` / `KindFunction` / `KindReport` | `internal/index/index.go:20-22, 189-197` |
| 跳转（definition） | 按 `局部→参数→模块级→GLOBALS` 顺序找，找不到返回 `nil`（**静默失败**） | `internal/server/navigation.go:69` |
| 调用点检查 | **不存在**。`CALL foo()` 与 `foo()` 完全不校验 | — |
| 未定义变量检查 | 有（`variable-source-unresolved`），但**已显式跳过函数名** | `internal/server/globals.go:250-251` |

也就是说：现在写 `CALL 不存在的函数()`，编辑器**毫无提示**。`function-not-found` 是 P7-D 新增的功能，不是修复。

**为什么当初跳过**：`queryVariables` 有注释——`pattern 2 跳过 call.name（函数名：来自其他模块，无跨文件函数索引）`。
在没有链接资料的前提下，跨文件函数无法判定，所以只能不查。链接资料正是解锁这个检查的前提。

**P7-D 要做的是**（按下面规则）：
1. 建立"可见函数集合" = 本文件函数 ∪ GLOBALS 引入 ∪ 链接文件函数 ∪ 全局模组（sub/lib/qry）的全部函数；
2. `function-not-found` = 调用名不在该集合中；
3. **无链接资料时整个检查不启用**（否则回到满屏误报）。

### 5.2.1 模组间可见性（Q14 二次定稿：扁平规则，无矩阵）

**用户最终规则（2026-10-21 第二次答复）**：

> `sub` `lib` `qry` 所有程序都默认可用，**包括它们自己**。

即：这三个模组是**全局可见**的——任何文件（含这三个模组内部的文件）都能调用其中的函数。
规则从"定向矩阵"简化成一条平铺规则：

```text
可见函数 = 本文件 ∪ GLOBALS 引入 ∪ 链接文件 ∪ (sub ∪ lib ∪ qry 的全部函数)
```

要点：

1. **不再有禁止方向**。之前 `lib → sub` 的冲突（实测 11 处反例）**自动消解**——
   `sub` 全局可见，`lib` 调 `s_errmsg` 完全合法。`allowModuleDeps` 白名单配置**一并删除**（已无必要）。
2. **包括它们自己**：`lib/` 文件可以调 `sub/` 的函数，`sub/` 可以调 `qry/` 的，任意方向都行。
3. 实现上就是一张**全局可见模组集合**（默认 `["sub","lib","qry"]`），不做按调用方的差异判断。
   代码比矩阵简单得多。
4. 这三个模组的函数来源**不依赖链接资料**：即使 CSV 里没有它们的组也能工作。
   （实测 CSV 里恰好也有：`sub` 组 524 文件 / `lib` 218 / `qry` 264 —— 但按本规则以全局集合为准，
   仍优先使用 CSV 数据，因为它更贴近实际编译产物；两者取并集。）
5. 允许用户配置覆盖：`bdl.link.globalModules`，默认 `["sub","lib","qry"]`。
   某些项目若把公共库放在别的模组（如 `clib`），改这里即可。


### 5.3 文档结构（已用真实数据定稿）

**真实数据实测**（`link.csv`，529042 B，sha256 `3ca6ff10…`）：

| 指标 | 实测值 |
|---|---|
| 数据行 | 15587 |
| `GAL04='Y'` | 15536 |
| `GAL04='N'`（SQL 阶段过滤掉） | **51** |
| 不同程序编号 `GAL01` | 5850 |
| 不同模组 `GAL02` | 92 |
| 去重后的 `(模组, 程序代码)` 文件数 | 7931 |
| 每程序文件数 | min 1 / **p50 = 1** / p90 = 2 / max 364 |
| >100 文件的链 | 4（`sub` 524、`aws_ttsrv2` 368、`cbmp100` 364、`cimi997` 360） |

**体积实测**（三种编码方案，剔除 sub/lib/qry 隐式组后 15536 行）：

| 方案 | 体积 | 说明 |
|---|---|---|
| A 每链全量文件列表 | **415 KB** | 最简单，可直接读 |
| B 文件池化（整数引用） | 346 KB | 省 17%，但人工不可读 |
| C 反向索引（文件→程序集合） | 292 KB | 省 30%，但丢失链结构 |

**定稿：方案 A 单文件**（415 KB）。用户要求"先单文件，慢再分"。理由：

1. 415 KB 读入 + 解析实测预计 < 20 ms，远低于任何可感知阈值。
2. 99% 的链只有 1–2 个文件（p50=1），全量列表的冗余其实很小。
3. 可读性 > 体积优化：用户要能直接打开核对。
4. 用户已明确"慢再分"，所以**不预先分片**，但预留切换点。

#### 5.3.1 分模组的触发条件（可测指标，不是感觉）

满足**任一条**即切到分片模式：

| 指标 | 阈值 | 监测点 |
|---|---|---|
| `links.jsonl` 体积 | > 8 MB | 加载时记 debug 日志 |
| 解析耗时 | > 200 ms | 同上 |
| 明细行数 | > 200000 | 导入时 |
| 单文件打开时索引重建 | > 500 ms | 打开可执行文件的冷启动计时 |

切换后布局（**按用户 Q12 的目录约定**）：

```text
<project>/.link/
├── links.jsonl          # 首版：单文件，全量
├── links.meta.json      # schema/生成时间/来源/统计/过滤行数
└── shards/              # 仅当触发分片时：按 MAIN 所属模组
    ├── AXM.jsonl
    ├── APM.jsonl
    └── _orphan.jsonl    # 无 MAIN 的组
```

> 用户说的"分模组就放在 `axc/.link` 下"——即 `<module>/.link/`。
> 两种我都支持：`shards/` 集中存放（默认，少建目录）与 `--layout=per-module`（生成 `<module>/.link/`）。
> **首版只做集中式 `shards/`**，per-module 仅在有需求时实现，避免首版铺开。

**目录设计（定稿，Q12）**：

```text
<project root>/.link/            ← 用户项目根目录，不是插件仓库，不是 .bdl-lsp
├── links.jsonl
└── links.meta.json
```

`.link` 命名刻意短（跨平台路径长度友好）、以点开头（隐藏目录）。插件安装时**自动把 `.link/` 追加到工作区推荐 `.gitignore`**（只提示，不擅自改用户 `.gitignore`）。

`links.jsonl` 每行一条链路：

```json
{"schema":1,"program":"axmt620","module":"AXM","main":"axmt620","ambiguous":false,
 "files":[["ALM","s_point"],["APM","sapmp500"],["APM","sapmt420_sub"]]}
```

> 文件用 `[模组, 程序代码]` 二元数组而非对象，省 40% 体积（实测）。

`links.meta.json`（用真实数字填充）：

```json
{"schema":1,"generatedAt":"2026-10-21T10:00:00+08:00","source":"db",
 "sourceDetail":"DS.GAL_FILE@<host>:1521/<service>",
 "rowsTotal":15587,"rowsFilteredN":51,"rowsY":15536,
 "chains":5846,"orphans":81,"ambiguous":5,"missingFiles":800,
 "resolvedFrom":["tiptop","topcust"],"toolVersion":"0.7.0"}
```

**为什么不用数据库缓存/二进制**：415 KB JSON 读取解析完全够快，可读性带来的排错收益远大于性能收益。真要变慢按 §5.3.1 切分片，接口不变。

### 5.3.2 真实数据的三个必须处理的事实

实测发现的问题（不处理就是静默错误）：

| 事实 | 数量 | 处理 |
|---|---|---|
| **组内 0 个 MAIN** | **81**（文件存在的）+ 389（全缺失无法判定） | 归入 `orphan`；不参与"打开可执行文件"的链路，但保留以便反查 |
| **组内 ≥2 个 MAIN** | **5**（如 `aws_ttsrv2` 同时含 `aws_ttsrv2` 和 `aws_ttsrv2_transxml`） | 标 `ambiguous`，**全部保留**；该组任一 MAIN 打开都关联整条链 |
| **程序代码找不到文件** | **800 条记录 / 700 个不同代码** | **原样保留在 `links.jsonl`**，运行时跳过、**不产生诊断**；meta 里只作统计（`missingFiles: 800`）。用户明确：忽略即可，但 jsonl 要保存。主要集中在 `WPC`(168) `WSUB`(139) `WMB`(97) `WZZ`(53) `WAG`(47) `AZZ`(41) `WLIB`(38) —— 这些 `W*` 模组在语料里**不存在**（另一套/已下线系统） |

另两个关键事实：

1. **`GAL01` 不一定出现在自己组内**：5689 个出现在，**160 个不在**（如 `aimt823 → [aimt822]`、`gglr113 → [gglr999]`）。
   → 说明 `GAL01` 只是编号，**不能当文件名用**。索引只能靠 `MAIN` 程序代码定位链（这印证了 §5.2 的决策）。
2. **同一个程序代码可被多个组共享**：6864 个只属 1 组，但 `aws_eflog` 被 **301 个组**引用、`aws_xml` 被 324 个组引用。
   → 反向索引（"谁链接了我"）答案可能很长，Q13 的反查必须先给数量、由用户确认再展开。


### 5.4 文件路径解析

链路里只有 `(模组, 程序代码)`，实际文件在两个目录层级之下。语料实测布局：

```text
/code/workspace/tiptop-gp/
├── tiptop/<module>/4gl/<code>.4gl     # 61 个模组目录（55 个含 4gl/）
└── topcust/<module>/4gl/<code>.4gl    # 26 个客户化模组目录（24 个含 4gl/）
```

所以需要**模组根**概念：

```jsonc
"bdl.link.moduleRoots": ["tiptop", "topcust"]   // 默认自动探测
```

解析顺序：

1. `<moduleRoot>/<module>/4gl/<code>.4gl`，模组目录**大小写不敏感**（数据是 `APM`，目录是 `apm`；Linux 文件系统敏感，必须自己匹配）。
2. 多个模组根都命中 → 取配置顺序第一个，并记 debug 日志（客户化覆盖是正常业务）。
3. 都不命中 → 该文件标记 `missing`，进导入报告，不产生诊断噪声。

**默认链接模组**（R3）：

```jsonc
"bdl.link.implicitModules": ["sub", "lib", "qry"]
```

这三个模组的函数对**所有**可执行文件可见。

### 5.5 索引与性能（按真实分布重算）

**关键事实**：链大小 **p50 = 1**、p90 = 2、只有 4 条链超过 100 文件。

| 步骤 | 成本 | 策略 |
|---|---|---|
| 读 `links.jsonl` + 建内存索引 | **10–20 ms**（415 KB 实测） | 首次用到时加载，之后常驻 |
| 解析一条典型链（1–2 文件） | **2–10 ms** | 前台直接做，不需要异步 |
| 解析最大的链（`sub` 524 / `cbmp100` 364） | 364 × 2–5 ms ≈ **0.7–1.8 s** | **后台异步**，不阻塞 didOpen |
| 隐含模组（sub+lib+qry = 983 文件） | 2–4 s（只建函数名索引） | 后台一次性，仅首次 |
| 后续增量 | 单文件 mtime+size 变化重解析 | 复用 `globals` 的缓存模式 |

结论：因为 **99% 的链只有 ≤2 个文件**，绝大多数情况根本不需要异步——只有那 4 条超大链需要。
简化设计：**链文件数 > 20 才走后台**，否则同步解析（更快、代码更简单）。

诊断不等待链接索引：首轮只跑"快检查"（语法、字段、变量来源），链接诊断在后台索引完成后补发一次。

缓存落盘（二期）：`<project>/.link/cache/link-symbols.json`，键 `(path, mtime, size)`。首版只做内存缓存。

### 5.6 采集：三套方式（Q15 定稿：双驱动并行）

统一接口，三种方式都产出同一份 `links.jsonl`（S1 Go 直连 / S2 脚本导出 / S3 命令执行）：

```go
// internal/link/source
type Source interface {
    Name() string
    Fetch(ctx context.Context) ([]byte, Stat, error)   // 返回 CSV/JSONL 原始内容
}
```

**S1. 纯 Go 直连 Oracle（唯一内置方案，Q15 三度定稿）**

用户最终决定（2026-10-21 第三次答复）：

> 既然不能直接调用 oracle 驱动，就保留一个 go 使用 `sijms/go-ora/v2` 查询 Oracle 方案，
> **忽略本地驱动**。

- 驱动：`github.com/sijms/go-ora/v2`（**纯 Go，无需任何 Oracle 客户端**）。
  已实测可获取（`go list -m -versions` 成功，最新 `v2.9.0`）。
- **本地驱动方案（原 S1a / `godror` + CGO）已取消**，不留构建标签、不留配置项。
  原因：纯 Go 无法调 Instant Client，走 CGO 会让构建、分发、跨平台全部复杂化，
  收益却只是"少装一个纯 Go 库"。
- 代价：二进制 +2–3 MB（当前 11 MB → 约 14 MB）。可接受。
- 配置：

```jsonc
"bdl.link.sync": {
  "enable": true,
  "source": "golang",                 // golang | script | command
  "interval": 21600,                  // 秒；0 = 不自动同步
  "onStart": true,
  "db": {
    "host": "", "port": 1521, "service": "",
    "user": "readonly_user",
    "password": "${env:BDL_ORA_PASSWORD}",
    "query": "SELECT gal01,gal02,gal03,gal04 FROM DS.GAL_FILE WHERE gal04='Y'"
  }
}
```

- 密码优先级：环境变量 > `<project>/.link/secrets.json`（0600）> 交互输入。
  **绝不写进 `settings.json`**。
- 定时：LSP 内 goroutine，到点跑；失败指数退避（1×/2×/4×，上限 1h），连续失败 3 次后只记日志。
- 主动运行：`bdl-lsp link sync`（CLI）+ VSCode 命令 + Zed `.zed/tasks.json`。

**S2. 服务器脚本导出（零依赖，最稳兜底）**

- 用户在 RedHat 服务器用 `sqlplus` 跑脚本出 CSV，拷进项目根 `.link/`。
- 提供 `scripts/fetch-gal-file.sh`：`read -s` 读账号密码（不进 shell history），
  `SET MARKUP CSV ON` 直出。脚本已按真实表结构写好 `gal04='Y'` 过滤。
- 插件侧只需 `bdl-lsp link import <file.csv>`。

**S3. 命令执行（半直连）**

- 配置里只给"命令位置"，凭证**运行时**交互输入。
- `exec.Command`，账号密码**走 stdin**（不进命令行参数，避免 `ps` 泄露）。
- Zed 无密码框 → 交给 `.zed/tasks.json` 终端任务。

**三种方式共用**：导入校验 → 原子写入 → 报告（`missingFiles` 仅作统计，**不产生诊断**）。

### 5.6.1 导入校验清单（用真实数据验证过）

| # | 校验 | 真实数据表现 |
|---|---|---|
| 1 | 表头 `GAL01..GAL04` 存在 | ✅（首行带 UTF-8 BOM，**必须按 `utf-8-sig` 读**）|
| 2 | `gal04='Y'` 过滤 | 51 行被过滤 |
| 3 | 字段全引号包裹 | ✅（`"aapq666","AAP",...`），CSV 解析器需处理 |
| 4 | 组内 MAIN 唯一性 | 5374 唯一 / 81 零 / 5 多 |
| 5 | 磁盘文件存在性 | 800 条找不到（`W*` 模组不在语料） |
| 6 | 模组名大小写 | 数据全大写（92 个），磁盘全小写 → **必须大小写不敏感** |
| 7 | 重复行 | 存在（如 `apmt722` 组列了自身两次）→ 去重 |


### 5.7 语义规则与诊断

链路可用后，函数解析的可见性顺序（按 §5.2.1 扁平规则）：

```text
局部作用域 → 本文件 → GLOBALS 引入 → 链接文件 → 模组可见性（qry 恒可见 / lib / sub）
```

变量解析顺序**不变**（R2，用户明确要求）：

```text
局部作用域 → 本文件 → GLOBALS 引入          ← 到此为止，不跨链接
```

新增诊断：

| 码 | 含义 | 默认 | 说明 |
|---|---|---|---|
| `function-not-found` | 调用的函数在本文件/GLOBALS/链接/全局模组中都找不到 | warning | 无链接资料时**整项不启用** |
| `link-data-invalid` | 链接资料 schema/字段不合法 | warning | 导入时进报告；畸形的 `links.jsonl` 才会报 |
| `link-data-stale` | 资料超过 `maxAgeDays` 未更新 | hint | 温和提示，不骚扰 |

**刻意不做的诊断（用户 2026-10-21 明确）**：

- ~~`link-file-missing`~~ **删除**。链接里存在但磁盘上找不到对应 4gl 文件 → **完全忽略**，
  不报任何诊断，也不在编辑器里提示。原因：实测 800 条这类记录（`W*` 模组整批不在语料），
  报出来纯属噪声，且大概率是另一个系统/已下线模块的数据。
  这类记录**仍然写进 `links.jsonl`**（用户要求保存原样），只是读取时跳过。

用户明确要求：

> 为定义或引入就是使用了未定义变量，应该警告。

→ `variable-source-unresolved` 的语义**保持严格**（不放宽到链接），只按 P7-B 修掉调用点误报。

### 5.8 验收

- [ ] `bdl-lsp link import` 对真实 `link.csv`（15587 行）产出：链 5846、orphan 81、ambiguous 5、missingFiles 800，`gal04='N'` 的 51 行不出现。
- [ ] `cmd/link-audit` 报告上述数字与 §5.3.2 实测值一致。
- [ ] 打开 `axmt620.4gl`：`cl_*` 类跨文件函数不再报未定义；`GLOBALS` 变量行为不变。
- [ ] R2 反例测试：两个链接在一起的子文件，各自 `DEFINE` 同名变量但没有共享 GLOBALS → 仍报未定义（**确保没有把变量也放进链接可见性**）。
- [ ] Q14 测试：`lib/` 调 `sub/` 的 `s_errmsg` **不报**（实测语料 11 处真实调用）；`sub/` 调 `lib/` 不报；任意文件调 `qry/` 不报；调用三者都不存在的函数 → 报 `function-not-found`。
- [ ] 缺失文件测试：`links.jsonl` 保留 800 条磁盘不存在的记录，加载时跳过且**零诊断**。
- [ ] 性能：打开 `cbmp100`（364 文件链）首轮诊断 < 300 ms（链接索引在后台）；打开普通链（1–2 文件）无异步、直接出结果。

---

## 6. P7-E：链接资料的用户界面

### 6.1 一个必须先说清的限制

用户要求："outline 最下方增加链接资料栏……点击可以跳转到 apm/4gl/sapmp500.4gl"。

**LSP 的 `textDocument/documentSymbol` 无法跨文件跳转**：`DocumentSymbol` 没有 URI/target 字段，它的 `range` 只能落在本文档内；能带跨文件位置的是扁平的 `SymbolInformation`，而它不支持层级。两者在同一个响应里不能混用。Zed 的 `outline.scm` 同理——它只能引用当前语法树。

所以"层级 + 跨文件跳转"在纯 outline 里做不到，必须用编辑器各自的视图能力。

### 6.2 定稿方案：VSCode 自定义 TreeView + Zed 降级

| 能力 | VSCode | Zed |
|---|---|---|
| 三级层级 | ✅ TreeView | ✅ `documentSymbol` 追加（同一文档内） |
| 点击跳转 | ✅ `command: vscode.open` | ❌ 只能定位到本文档末尾 |
| 位置 | Explorer 侧栏独立视图「链接资料」 | 大纲末尾 |

**VSCode TreeView**（满足全部要求）：

```text
链接资料                    ← 视图根
└── axmt620                 ← L1：程序名
    ├── ALM/s_point         ← L2：直链子文件（按用户要求显示 模组/程序代码）
    ├── APM/sapmp500
    ├── sub                 ← L2：隐含模组
    │   └── SUB/cs_asf      ← L3：模组下的文件
    └── lib
        └── LIB/cl_null
```

- 数据来源：LSP 自定义请求 `bdl/linkTree`，返回 JSON 树。
- 点击文件节点：`vscode.open` 到 `apm/4gl/sapmp500.4gl`，带 `selection` 定位到函数定义。
- 显示格式按用户要求：`APM/apmp500`（模组大写，与链接资料一致）。
- **隐式模组节点**（sub/lib/qry）下挂 983 个文件，默认**折叠**且**懒加载**——展开才枚举，避免视图初始化卡顿。

### 6.3 子文件（无 MAIN）的空白 + 反查（Q13 定稿）

打开不含 MAIN 的文件时：

```text
链接资料
   [ 反查引用程序 ]        ← 一个可点击的 TreeView 项（图标：search）
```

- **默认不执行反查**，视图只有这一个可点击项。
- 点击后异步执行，期间显示 `反查中…`（TreeItem 描述）。
- **必须先看规模再展开**：实测 `aws_xml` 被 **324 个组**引用，直接铺开会有几百项。
  所以反查结果分两步：

```text
  被 324 个程序链接
  ├── 显示前 50 个…      ← 点击继续加载
  ├── axmt620
  ├── cbmp100
  └── …
```

- 反查数据来自反向索引（`程序代码 → []程序`），构建成本 O(链数)，与正向索引同一遍扫描完成，不额外读盘。
- 若某文件不在任何链里（如 `W*` 模组那些缺失文件）→ 显示「未在任何链接资料中」。


**Zed 降级**：把链路追加成 `documentSymbol` 的末尾若干条目（L1/L2/L3 层级靠 `children` 表达），**能看到但不能跳转**。在 Zed README 与文档中明确写这一限制，避免用户误以为是 BUG。

若后续 Zed 提供自定义视图 API，再补跳转。

### 6.3 验收

- [ ] VSCode：打开含 MAIN 的文件，「链接资料」视图出现三级结构，点击 `APM/apmp500` 跳到 `apm/4gl/sapmp500.4gl`。
- [ ] VSCode：打开不含 MAIN 的子文件 → 视图**只有**「反查引用程序」一项；未点击时无网络/磁盘查询。
- [ ] VSCode：点击反查 → 显示"被 N 个程序链接"，超过 50 个时分页。
- [ ] VSCode：对 `aws_xml`（被 324 组引用）反查，UI 不卡顿、分页正常。
- [ ] Zed：大纲末尾出现链接条目，层级正确（不跳转）。
- [ ] 无链接资料时视图显示引导文案（"未找到 .link/links.jsonl，运行『同步链接资料』"），不报错。

---

## 7. 新增配置项汇总

```jsonc
// 诊断级别（P7-B，✅ 已实现）—— Q10 定稿：全部默认 warning
//
// 注意：**没有 `"*"` 通配键**。注册表已经给每个码一个出厂默认，
// 通配键既多余又会让"我关掉的那个码为什么又冒出来"难以排查。
// 键 = 诊断码，值 = off/error/warning/information/hint。
// 已注册的码（`go run ./cmd/diag-audit -codes` 或 `make diag-audit` 可打印）：
//   syntax-error, missing-node（+ 动态的 missing-<kind>）,
//   globals-file-not-found, variable-source-unresolved, field-not-in-form,
//   form-duplicate-item, form-layout-tag-undefined,
//   form-screen-record-undefined, record-member-unknown, file-too-large,
//   function-not-found
"bdl.diagnostics": {
  "variable-source-unresolved": "warning",
  "record-member-unknown": "off",
  "function-not-found": "off"          // 默认 off；内置表已就绪，可自行打开观察
}

// 内置符号（P7-C，✅ 已实现）—— 详见 §4.4
"bdl.builtins.enable": true,                   // false = 回到 P7-C 之前
"bdl.builtins.paths": [".bdl-lsp/team.json"],  // 额外覆盖文件/目录（相对项目根）
"bdl.builtins.disable": ["SUM"]                // 本项目里不当内置
// 项目约定路径 .bdl-lsp/builtins.json 自动加载，不需要写进 paths。

// 链接资料（P7-D）—— Q12：放用户项目根的 .link 目录
"bdl.link.enable": true,
"bdl.link.dataDir": ".link",                   // 相对项目根（用户定稿）
"bdl.link.moduleRoots": ["tiptop", "topcust"], // 空 = 自动探测
"bdl.link.maxAgeDays": 7,                      // 0 = 不提示过期

"bdl.link.globalModules": ["sub", "lib", "qry"], // Q14：对所有程序可见，含它们自己

// Q15：S1 Go 直连（唯一内置）/ S2 脚本导出 / S3 命令执行
"bdl.link.sync": {
  "enable": false,
  "source": "golang",                          // golang（唯一内置）| script | command
  "interval": 21600, "onStart": true,
  "db": {
    "host": "", "port": 1521, "service": "", "user": "",
    "password": "${env:BDL_ORA_PASSWORD}"
  },
  "command": {
    "path": "/usr/bin/sqlplus", "args": [], "passwordViaStdin": true
  }
}
```

VSCode `package.json` 现状：`bdl.diagnostics`（type=object，
`additionalProperties` 限定为 5 个级别字符串）+ `bdl.syntaxDiagnostics`
默认改 `warning` 已落地；`src/client.ts` 的 `collectSettings` 是**白名单**，
新增键必须显式转交，否则不会下发（踩过）。P7-C/D 的内置/链接配置届时再补。

---

## 8. 已确认决策（2026-10-21 用户答复，定稿）

| # | 问题 | **决定** | 落地影响 |
|---|---|---|---|
| **Q10** | 诊断默认级别 | **全部默认 `warning`** | 注册表统一 `Default = warning`；`syntax-error` 也降为 warning（用户明确要求）。`record-member-unknown` 仍可用 `off` |
| **Q11** | 链接资料存储 | **先单文件；若索引很慢再分模组** | 首版 `links.jsonl`；实现预留 shard 分支，触发条件写成可测指标（§5.3.1） |
| **Q12** | 链接资料位置 | 放**用户自己的项目**，不是插件仓库：项目根新建 **`.link` 目录**，单文件放里面；将来分模组则放 `<module>/.link` | 路径改为 `<project>/.link/links.jsonl`，**不是** `.bdl-lsp/link/` |
| **Q13** | 子文件（无 MAIN）链接栏 | **反查**，但默认**显示空白**，提供用户可点击的菜单项，点击后才执行反查 | VSCode TreeView 初始为空 + 「反查引用程序」按钮；反查异步、带进度 |
| **Q14** | sub/lib/qry 可见性 | **三者对所有程序默认可用，包括它们自己**（无方向限制） | §5.2.1 扁平全局集合；原 `lib→sub` 冲突自动消解 |
| **Q15** | Oracle 驱动 | **只用 `sijms/go-ora/v2`（纯 Go）**；本地驱动方案取消 | §5.6 S1；不留 CGO build tag、不留 `driver` 配置 |

### 8.1 原 Q14 冲突已消解（2026-10-21 第二次答复）

第一版规则"`lib` 不能用 `sub`"与语料实测的 11 处反例冲突
（`lib/4gl/cl_prt.4gl:466` 等调用 `sub/4gl/s_showmsg.4gl` 的 `s_errmsg`）。

用户改为"**sub/lib/qry 所有程序都默认可用，包括它们自己**"后：

- 冲突**自动消失**——`lib` 调 `sub` 的函数完全合法，那 11 处是真代码不是缺陷。
- `bdl.link.allowModuleDeps` 白名单配置**删除**（已无必要）。
- 结论：**没有遗留待确认项**。

> 保留这段记录，是因为它说明了"规则与语料冲突"这件事本身有价值：
> 如果当时硬按第一版规则实现，会凭空产出 11 条假告警。

---

## 9. 风险

| 风险 | 影响 | 对策 |
|---|---|---|
| Windows URI 根因判断有偏差 | P7-A 修了但没解决 | 上机第一步先打印两个客户端的 `initialize` 入参对比，再动手 |
| 全局模组函数量巨大（983 文件） | 首次索引 2–4 s | 只建函数名索引（不建完整符号）+ 后台一次性 + 内存缓存；不阻塞首轮诊断 |
| `expression name:` 跳过规则误伤 | 漏报函数未定义 | 先跑语料对比 skip 前后的差异清单，人工抽查 |
| 内置表不全导致误报残留 | 噪声仍高 | `cmd/builtin-audit` 语料驱动补齐，设 <20 条未知的门槛 |
| Zed 无法跳转链接文件 | 体验差异 | 文档明示 + VSCode 侧做全功能；Zed 等 API |
| 纯 Go 驱动 +2–3 MB 二进制 | 体积从 11 MB → 约 14 MB | 可接受；如需瘦身可后加 build tag 排除 `link sync` 子命令 |
| 链接资料含生产库信息 | 泄露 | `.link/` 加入推荐 `.gitignore`；文档写明不要提交 |

---

## 10. P7-A 执行记录（✅ 已完成 2026-10-21）

### 10.1 A-1 Zed 大纲层级

**复现优先**：先在 `zed-extension/tests/verify-queries.mjs` 加断言
"所有 `@item` 的 (start,end) 两两不同"，在**旧 query 上确认失败**：

```text
✗ bdl/outline.scm: 10 个 @item 区间重复
    重复区间：(20,0)..(21,17), (24,0)..(26,18), (29,0)..(33,22), …
```

**修复**（`languages/bdl/outline.scm`）——`@item` 从整条语句改绑到名字节点：

```scheme
; 修改前
(define name: (identifier) @name) @item
(constant (identifier) @name) @item
(type name: (identifier) @name) @item
; 修改后
(define name: (identifier) @name @item)
(constant (identifier) @name @item)
(type name: (identifier) @name @item)
```

**意外收获：`.per` 有同类缺陷**。

新断言在 `per/outline.scm` 上也报 2 处重复：

```text
✗ per/outline.scm: 2 个 @item 区间重复
    重复区间：(35,0)..(35,24), (36,0)..(36,25)     ← PHANTOM formonly.p_flag;
```

根因不同但机理相同：`field_ref` 有**两个 name 子节点**（表名 + 列名）：

```text
(field_ref
  (name ...)              ← 表名 formonly
  column: (name ...))     ← 列名 p_flag
```

而规则写的是 `(field_ref (name) @name)`，两个都命中 → 区间重合。
修法：只绑 `column` 字段，并把 `@item` 绑到列名节点：

```scheme
(phantom_item
  (field_ref
    column: (name) @name @item))
```

副作用（正面）：PHANTOM 在大纲里从"表名+列名两条"变成"列名一条"，更符合直觉。

**验证**：

| 检查 | 结果 |
|---|---|
| `node tests/verify-queries.mjs` | **40 项全通过**（新增 2 项区间断言） |
| `cargo check --target wasm32-wasip2` | 通过 |
| `python3 tests/verify-manifest.py` | 14 项通过 |
| 用户报告的原始写法 | `l_name` / `l_sql` 区间完全独立 |
| 真实文件 `ammr110.4gl`（654 行） | 16 条 @item，**0 重复**（修复前 5 条含重复） |

**新增 fixture**：`tests/fixtures/outline.4gl` / `outline.per`，
专门覆盖多变量 DEFINE / 多 CONSTANT / 多 TYPE / 多 PHANTOM。两者均 0 ERROR。

### 10.2 A-2 VSCode GLOBALS 路径

**根因确认**（无需 Windows 实机，按 vscode-uri 的编码规则复现）：

```text
VSCode: file:///C%3A/work/proj/x.4gl     ← 盘符冒号编码为 %3A，`#` 编码为 %23
Zed:    file:///C:/work/proj/x.4gl       ← 不编码
旧实现: → "/c%3A/work/proj/x.4gl"        ← 非法路径，全线失效
```

**修复**：新建 `internal/fileuri` 包（`ToPath` / `FromPath`），
用 `net/url` 做**完整百分号解码** + 盘符/UNC 归一。

替换了**三处**重复实现（原计划以为只有两处）：

| 位置 | 原实现 | 影响面 |
|---|---|---|
| `globals.PathOfURI` / `URIOfPath` | 只剥 `file://` + `%20` | GLOBALS 解析、文档相对路径 |
| `form.PathOfURI` / `URIOfPath` | 同上（第二份拷贝） | 表单路径解析、字段校验 |
| `config.uriToPath` | 同上（第三份拷贝） | `.bdl-lsp/config.json` 永远读不到 |

三处都改为委托 `fileuri`，导出名保持不变（不动调用点）。

**同时补上 `workspaceFolders` 回退**（`server.workspaceRootURI`）：

```text
rootUri → workspaceFolders[0].uri → rootPath → ""（无工作区）
```

原因：VSCode 在"打开单个文件"或多根工作区时 `rootUri` 为 `null`，
旧实现直接当空串 → `rootPath` 为空 → searchPaths / perRoots / config.json 静默失效。

**关键设计：Windows 语义可在 Linux 上单测**。

`toPathWindows` 抽成不依赖 `runtime.GOOS` 的纯函数，
所以 CI 没有 Windows runner 也能守住这段逻辑。
另外**刻意不用 `filepath.FromSlash`**——它在非 Windows 平台是恒等函数，
会让跨平台单测失效（初版踩过，测试立刻抓到）。

**验证**：

| 检查 | 结果 |
|---|---|
| `internal/fileuri` 单测 | 22 个用例全通过（Windows/POSIX/UNC/往返/拒绝非 file URI） |
| `go test -race ./...` | 20 个包全绿，**281 个测试函数** |
| `make lint` | 通过（vet + gofmt + keywords-check） |
| 语料回归（25 个真实 .4gl，`globals-file-not-found`） | **0**（无退化） |

**旧测试修正**：`config` 与 `globals` 各有一个测试断言"在 Linux 上也剥盘符"，
那是旧实现的猜测行为，已改为"按宿主平台正确"，
Windows 语义由 `fileuri` 的专门用例覆盖。

**遗留**：Windows 实机复验仍需用户在真机执行（本沙箱无 Windows）。
复验点：打开 `axmt620.4gl`，确认无 `globals-file-not-found`。

### 10.3 P7-B 第一批：悬停的两个问题（✅ 2026-10-21）

用户 Windows 实测反馈两个悬停问题，都已定位根因并修复。

#### B-1 多行 RECORD 悬停被压成一行

**现象**：`DEFINE g_qryparam RECORD ... END RECORD`（26 行）悬停显示为
**一行几百字符**，完全无法阅读。

**根因**：`index.typeText` 用 `normalizeSpace` 把换行压成空格。
但 `TypeText` 同时承担**语义判断**职责（`server/recordname.go` 用
`strings.HasPrefix(TypeText, "RECORD")` 判断是不是记录），
直接改它会让那些判断失效。

**修法**：职责分离。

| 字段 | 用途 | 处理 |
|---|---|---|
| `TypeText` | 语义判断（前缀匹配、相等比较） | 规范化（换行→空格），**行为不变** |
| `TypeTextRaw` | 展示（悬停） | 保留换行与缩进 |

`TypeTextRaw` 只做两件事：去行尾空白、tab→4 空格（markdown 里 tab 宽度不可控）。
不去公共缩进——代码块里有缩进更好读，且能反映真实嵌套。

**验证**（真实文件 `tiptop/config/top.global:206` 的 `g_qryparam`）：

```text
g_qryparam : RECORD
            state             LIKE type_file.chr1,  #程式的狀態(i→INPUT，c→CONSTRUCT，預設為i)
            form              LIKE type_file.chr20, #畫面代號(...)
            ...
            plant             LIKE type_file.chr10  #FUN-980030:指定跨Plant查詢資料
                              END RECORD
```

26 行逐行原样输出，含注释。

#### B-2 同名局部变量跨函数泄露

**现象**：`fa` 和 `fb` 各有一个局部变量 `i`，在 `fa` 里悬停 `i`
**同时显示 fa 的 INTEGER 和 fb 的 STRING**。

**根因**：hover 用 `fi.Lookup(小写名)` 扫**全文件**同名符号，没有作用域过滤。
而**跳转**（`resolveDefinition`）早就按作用域过滤了——**两处规则不一致**，
所以"能跳转但悬停是乱的"。

**修法**：抽出 `scopedSymbolsAt`，跳转与悬停共用同一套规则。

```text
优先级：当前函数局部 → 当前函数参数 → 模块级 → GLOBALS 块内 → GLOBALS 引入
```

顺带补上 **`ScopeGlobals`（GLOBALS 块内）**这一级——原实现里它不在优先级链中，
导致在 `.global` 文件的 GLOBALS 块内悬停 `gx` 会显示成模块级的同名变量。

**记录字段的特例**：记录字段的 `Container` 是**记录名**而不是函数名
（见 `index.collectRecordFields`），不能按函数作用域过滤。
改为"光标是否落在声明该字段的那个记录的区间内"判断
（`inRecordDeclaration`）。这个改动一度让 `TestHoverRecordField` 变红，
说明原来的测试是有价值的——它守住了"记录字段要有悬停"。

**验证**：

| 检查 | 结果 |
|---|---|
| 局部 `i` 隔离 | `fa` 只显示 INTEGER/fa，`fb` 只显示 STRING/fb |
| 局部优先于模块级 | 函数内 `i` 显示局部 INTEGER，不显示模块级 STRING |
| GLOBALS 块内定义 | 显示块内类型 + "模组全局变量"标注 |
| GLOBALS 引入变量 | 显示类型 + 来源文件与行号 |
| 记录字段 | `TestHoverRecordField` 恢复通过 |
| 语料抽样 | 29 个真实 RECORD 变量悬停，**0 个**多条目泄露 |

新增回归测试 `internal/server/hover_test.go`（6 个用例）。

#### B-3 Zed 大纲层级（✅ 已修复 2026-10-21）

用户反馈：修复后 Zed 里**仍有**同层级变量变成树结构。用户给出了三个具体写法：

| 写法 | 现象 |
|---|---|
| `DEFINE i,j INTEGER` | 两层结构（`i` 下有 `j`）❌ |
| `DEFINE i INTEGER,j INTEGER` | 两层结构 ❌ |
| `DEFINE i INTEGER` + `DEFINE j INTEGER` | 一层结构 ✅ |

**上一轮为什么没修好**：上一轮只把 `@item` 从"整条 DEFINE 语句"改到"名字节点"，
解决了"同一语句多变量"的情况。但用户报的三个写法里，
前两个恰好是**同一语句多变量**——说明还有别的机制在起作用。

**根因（读 Zed 源码定位）**：

Zed 的层级判定在 `crates/language/src/buffer.rs`：

```rust
items.sort_by_key(|item| (item.range.start, Reverse(item.range.end)));
let mut item_ends_stack = Vec::<Point>::new();
for item in items {
    while let Some(last_end) = item_ends_stack.last().copied() {
        if last_end < item.range.end { item_ends_stack.pop(); } else { break; }
    }
    depth = item_ends_stack.len();          // ← 层级深度
    item_ends_stack.push(item.range.end);
}
```

翻译：**只要前一条目的结束位置 ≥ 当前条目的结束位置，当前条目就被判为子级**
（`Point` 按 行,列 比较）。注意这**不是"区间相同"，而是"区间相交/重叠"**。

上一轮的断言只查"区间完全相同"，查不出这种相交——这是断言的盲区。

于是两种层级来源：

| 来源 | 例子 | 上一轮 |
|---|---|---|
| ① 区间**完全相同** | `DEFINE i,j INT` 两个变量的 item 都是整条语句 | ✅ 已修 |
| ② 区间**相交/包含** | 函数 item 覆盖整个函数体，内部变量落在其中 | ❌ 未修 |

用源码复刻的算法实测（7135 个真实 `.4gl`）：

```text
修复前：条目 532386  有层级文件 6941/7135  有层级项 345608
```

#### B-3 补修：函数内变量必须归属函数（✅ 2026-10-21，`7f80d21`）

**用户反馈**：上一轮修完后"zed 变量问题现在没有树结构了，但是又有新问题。
**函数内归属的变量应该归属函数，可以折叠，现在和函数同级别了**，修复此问题"。

**上一轮（`1ee95bf`）错在哪**：把 `function`/`report`/`main` 的 `@item`
从"整个节点"改成"名字节点"，假层级确实全平了（0/7135），
**但同时消灭了"变量归属函数"这个正确行为**——变量变成与函数平级，
在 Zed 里没法折叠。**方向反了**。

**关键证据：Zed 内置语言就是这么做的**。内置语言的 outline 查询在
`crates/grammars/src/<lang>/outline.scm`（**不是** Rust 源码里的字面量——
`grep outline` 在 `.rs` 里搜不到）：

```scheme
; rust/outline.scm
(function_item ... body: (_ . "{" @open "}" @close .)) @item   ; item 覆盖整块
; python/outline.scm
(function_definition "async"? @context "def" @context name: (_) @name) @item
```

所以 **"父 item 覆盖子 item 区间"是合法的层级表达**，不是 bug。
假层级要在**叶子侧**解决，不该动容器。

**最终模型（容器 + 叶子，`@item` 绑法相反）**：

| 类别 | 节点 | `@item` 绑 | `@name` 绑 |
|---|---|---|---|
| 容器 | `function` / `report` / `main` / `menu` / `dialog` | **整个节点** | 名字 / 标题 |
| 叶子 | `define` / `type` / `constant` / `label` | **名字 identifier** | 同节点 |

```scheme
(function name: (identifier) @name) @item    ; 容器：整块
(main . "MAIN" @name) @item                  ; 锚点只匹配开头 MAIN
(define  name: (identifier) @name @item)     ; 叶子：名字
```

`main` 的锚点 `.` 很关键：没有它会同时匹配开头和 `END MAIN` 的 MAIN，出现两条条目。
`menu` 同理（嵌套 MENU 时整块 item 会包含内层）：

```scheme
[
  (menu title: (_) @name)
  (menu !title) @item        ; title 可选，用否定字段避免重复匹配
]
```

`dialog` 没有 title 字段，保持整块即可（实测无层级问题）。

**结果**（7699 个真实 `.4gl` + 7409 个 `.per`）：

```text
@item 区间重复的文件：0
叶子当父级的事件：0
```

模拟 Zed depth 算法的实际大纲：

```text
f_outline   [86,0]..[94,12]
  ├─ l_local1   [87,9]..[87,17]
  ├─ l_local2   [88,9]..[88,17]
  ├─ l_single   [89,9]..[89,17]
  ├─ l_third1   [90,9]..[90,17]
  ├─ l_third2   [91,9]..[91,17]
f_single   [96,0]..[100,12]
  ├─ l_only   [97,9]..[97,15]
MAIN   [104,0]..[110,8]
  ├─ m_one   [105,9]..[105,14]
  ...
```

函数/MAIN/报表在顶层，内部变量归属其下（**可折叠**），
同一条 DEFINE 的多个变量**互为兄弟**（`l_local1`/`l_local2` 不嵌套），
独立的一条 DEFINE（`l_single`）也与它们平级。

#### B-3 回归防护：为什么上一轮的断言没抓住

上一轮的三条断言里，`isLeafItem()` 用"单行且 ≤64 列"判定叶子：

```js
const isLeafItem = (it) => it.start[0] === it.end[0] && it.end[1] - it.start[1] <= 64;
```

**但"函数容器"和"变量叶子"都是单行短标识符**——两者被混为一谈。
于是"把函数 `@item` 缩到名字节点"这个回归，断言全绿，用户照样报问题。

**修法**：用 `tree-sitter parse` 的语法树作**独立真相源**（`parseContainers()`），
按区间覆盖关系判断"这个 item 是否覆盖了整个定义块"，
再**直接检验用户要求的行为**——变量的 Zed 父级链上必须能找到所属容器：

```text
✓ bdl/outline.scm: 4 个容器的内部变量均归属所属容器（可折叠）
```

**断言 3 是独立信号**——对 `1ee95bf` 回归时，断言 1/2 仍绿：

```text
✓ bdl/outline.scm: 30 个 @item 区间两两不同          ← 仍绿
✓ bdl/outline.scm: 30 个条目，叶子条目均无子级        ← 仍绿
✗ bdl/outline.scm: 4 个变量未归属所属容器（会与容器平级、无法折叠）
    示例：function @ [86,0] 内 5 个变量（容器本身没有对应的 outline 条目）
```

对"DEFINE `@item` 绑整条语句"这个回归也变红：

```text
✗ bdl/outline.scm: 8 个 @item 区间重复（Zed 会误判为父子层级）
✗ bdl/outline.scm: 10 个变量未归属所属容器（会与容器平级、无法折叠）
```

fixture `outline.4gl` 的头部注释也一并重写（原文写的是"函数内部变量必须与
函数名平级"，与新模型矛盾），并补了"一条多变量 DEFINE + 独立 DEFINE 语句
并存"的场景。

#### B-4 P7-B 剩余项（未做）

诊断码集中注册表、`bdl.diagnostics.<code>` 配置、拼错保护、
修调用点误报、新增 `function-not-found`、语料复测降噪。见 TODO §B-4。

#### B-5 游标 / prepare statement（✅ 2026-10-21，第七轮）

需求见 TODO §B-5。落地内容：

| 项 | 实现 |
|---|---|
| `PREPARE p FROM sql` | 新增 `index.KindPrepare`；`TypeText/TypeTextRaw` 存 SQL 来源 |
| `DECLARE c CURSOR FOR p` | 新增 `index.KindCursor`；`Symbol.RefName` 记来源 prepare 名 |
| 悬停 | `hoverText` 两个新分支：`PREPARE p FROM <sql>` / `DECLARE c CURSOR FOR p` |
| 跳转 | **无需新代码**——那些名字是普通标识符，按作用域查找即命中 |
| 大纲/补全/语义高亮 | 分别补 `symbolDetail`、`toSymbolKind`、`itemForSymbol`、`classifySymbol` |

跳转"不用写代码"这点值得记下来：P7-B 抽出的 `scopedSymbolsAt` 是**按名字
和作用域**解析的，prepare/cursor 一旦进了符号表就自动可用。**这验证了
B-2 那次"悬停与跳转共用一套作用域规则"的重构是正确的投资**——
新语法元素接入时不需要在两处各写一遍。

##### 收集方案：走了三个方案才定（这一节是本文最重要的性能记录）

难点：prepare/declare 是**可执行语句**，能嵌在 `IF`/`FOREACH`/`MENU` 等
任意深度（实测 `ammr100.4gl` 的 `PREPARE` 就在 `IF` 里），所以要"找全"，
但又不能付全树遍历的代价。

| 方案 | 成本 | 结论 |
|---|---|---|
| ① 逐节点递归全树（Go 侧） | 整树遍历；`ammr100` 上让 Build 翻倍 | ❌ |
| ② tree-sitter query `(prepare)` / `(declare)` | **实测 228ns/节点**，线性；10678 节点的文件要 2.43ms | ❌ |
| ③ **源码里找关键字 → `DescendantForByteRange` 定位** | 只与关键字出现次数成正比 | ✅ 采用 |

方案 ③ 的形态：

```go
forEachSQLKeyword(b.src, func(off int) {          // 字节扫描，不加解析器
    stmt := sqlStmtNode(root.DescendantForByteRange(  // 只沿树下降一次
        uint(off), uint(off+7)), kinds)
    ...                                            // 按 KindId 确认种类
})
```

三个好处：

1. **成本与关键字次数成正比**。`ammr100.4gl` 只有 4 处命中，`apmi006.4gl` 9 处。
2. **误命中由类型检查兜住**：字符串/注释里的 `"prepare"` 会被定位到
   `string`/`comment` 节点，走父链也到不了 prepare 语句，直接丢弃。
3. **不需要"哪些节点可能含语句"的白名单**。方案 ① 期间维护过一份从
   grammar 的 `_statement` 出现位置推出来的白名单——它能通过语料验证，
   却把 `PREPARE` 写在 `MENU`/`DIALOG` 里的情况漏掉（语料里恰好没有这种
   写法，所以测试也发现不了）。方案 ③ 没有这个隐患。

实施中另外两处要点：

- 关键字比较用**预编译的 `KindId()`（uint16）**，不用 `Node.Kind()`——
  后者每次调用都 `C.GoString` 分配一个 Go 字符串。
- 同一条语句可能被命中多次（动态 SQL 字符串里也写着 `PREPARE`），
  按节点 id 去重。

##### 性能：一个反直觉的发现（cgo 调用次数才是成本）

追性能时踩到一个反直觉的事实，**差点把一次优化做反**，记在这里避免重犯：

`tree-sitter` 的 `ts_node__child` 每次都从头迭代子节点直到第 i 个
（`src/node.c`），所以 `for i := 0; i < NamedChildCount(); i++ { NamedChild(i) }`
在 C 侧是 **O(n²)**。据此把这种循环全改成 `NamedChildren(cursor)`（一次成型）
看起来是稳赚的——**实测却慢了 2~2.7 倍**：

| 全树遍历 1918 个节点 | 耗时 | cgo 调用数 |
|---|---|---|
| 逐个索引 `NamedChild(i)` | 880µs | ~3836 |
| 批量 `NamedChildren(cursor)` | 1680µs | ~7672 |

两个数一除正好是 cgo 调用数之比（1.91 ≈ 2.0）。结论：

> **每次 cgo 调用约 230ns，是成本的全部；C 侧的 O(i) 步进几乎免费。**

所以 `NamedChild(i)` 的 O(n²) 在真实规模下并不构成问题，而
`NamedChildren` 每个孩子要 3 次 cgo 调用（`Node` + `IsNamed` + `GotoNextSibling`）。
真正的最优解是**按孩子数量选**——实测交叉点约 55：

| 孩子数 | 逐个索引 | 批量 | 胜者 |
|---|---|---|---|
| 14 | 3.1µs | 6.8µs | 索引 |
| 54 | 19.4µs | 25.1µs | 索引 |
| 64 | 31.6µs | 29.1µs | 批量 |
| 163 | 172.6µs | 72.5µs | **批量（2.4 倍）** |
| 204 | 306.8µs | 87.6µs | **批量（3.5 倍）** |

顶层 root 在实码里常有上百个孩子（大量模块级 `DEFINE` + 几十个函数），
所以这一档最值钱。据此加了 `builder.forEachChild`，阈值取 **64**。

**最终性能**（400 次迭代，同一台机器）：

| 文件 | root 子节点 | 改动前 | 改动后 | 变化 |
|---|---|---|---|---|
| `apmt721_icd.4gl`（2.4KB，无关键字） | 少 | 28.7µs | 31.3µs | +9% |
| `apmi006.4gl`（30KB） | 少 | 417µs | 490µs | +18% |
| `ammr100.4gl`（24KB） | 192 | 636µs | 471µs | **−26%** |
| `aapi204.4gl` | 204 | 576µs | 398µs | **−31%** |

四个文件合计 1.66ms → 1.39ms，**平均快 16%**：大文件（root 孩子多）
因自适应遍历显著变快，中小文件为游标支持付 9~18% 的增量（绝对量几十µs）。

##### 验证

- **独立真相源交叉验证**：`sqlstmt_test.go` 用**全树递归扫描**做参照实现，
  在**全部 7699 个真实 .4gl** 上与生产实现比对符号集合，**完全一致**
  （prepare=37525 / cursor=36007）。日常套件里跑 300 个文件（约 4s），
  用 `CORPUS_LIMIT=0` 可跑全量。
- 全语料统计：prepare=37603 / cursor=36103（带 `RefName` 22787）。
- 单测：index 新增 11 个（7 个符号语义 + 4 个收集机制），server 新增 6 个
  （悬停 3 / 跳转 1 / 大纲 1 / 诊断不误报 1）。

### 10.4 P7 现已无遗留待确认项

2026-10-21 三次答复后，所有冲突已消解：

| 原待确认 | 结论 |
|---|---|
| `lib → sub` 11 处反例 | **消解**：Q14 改为"sub/lib/qry 对所有程序可用，含它们自己"，这 11 处是合法代码 |
| 本地 Oracle 驱动怎么接 | **消解**：Q15 取消本地驱动，只用纯 Go `go-ora` |
| 链接有、代码没有的文件怎么办 | **消解**：保留在 `links.jsonl`，运行时忽略，零诊断 |

所以 P7 可以**直接开工，不需要再问**。按 §1 的顺序：P7-A → B → C → D → E。

### 10.5 P7-B 第二批：诊断降噪（✅ 2026-10-21，`0.1.2`）

#### 做完的四件事

1. **`internal/diag` 集中注册表**（`registry.go`）
   - 11 个诊断码 + 出厂默认级别，`SeverityFor(code, settings, legacy)`
     统一解析：`bdl.diagnostics.<code>` > 旧专用键 > 注册表默认。
   - `ToSeverity` 成为全仓库**唯一**的“级别 → LSP 枚举”转换点。
     历史上 server 与 diag 各写一份 switch，`LevelInformation` 写错就是
     两处漂移的典型（编译期才发现）。
   - `ValidateSettings` 返回未注册的码；`didChangeConfiguration` 里记 warn
     ——拼错一个字母永久不生效的坑堵上了。
   - MISSING 节点的码是**动态**的 `missing-<kind>`，无法穷举，所以
     `IsRegistered` 对 `missing-` 前缀一律放行，级别解析分两级：
     `missing-<kind>` 精确命中 → `missing-node` 整簇兜底 → 旧键。

2. **`config.Settings.Diagnostics`**（`map[string]DiagnosticLevel`）
   - 增量合并（`nil` 表示未下发，保留现有值）；`Validate` 校验取值合法性。
   - `SyntaxDiagnostics` 默认由 `error` 改为 `warning`（Q10 定稿），
     旧键与新键行为一致。

3. **query 误报修正**（`queryVariables` 从 7 条 pattern 增到 10 条）
   - `(expression name: (variable (identifier) @skip))`：`IF fn() THEN` 里的
     函数名是 `expression name:` 字段，不是 `call` 节点——旧 query 漏了它。
   - `(whenever (variable (identifier) @skip))`：`WHENEVER ERROR CALL f` /
     `... GOTO lbl` 的目标不是变量。
   - 表单字段用的 4 条 pattern 改为捕获**整个 variable 节点**：
     `DISPLAY x TO FORMONLY.cn2` 的 field 是
     `(variable (object object: (variable …) property: …))`，直接子节点不是
     identifier，旧写法匹配不到。
   - `compileVariableQueries` 改为返回 `error`，失败时 `logger.Error`：
     静默禁用一个功能（零诊断）与“用户关掉了”在编辑器里长得一样，
     必须留痕。

4. **`function-not-found`**（新增，默认 **off**）+ **`cmd/diag-audit`**
   - 收集 `call name:` 与 `expression name:` 两处调用点，比对本文档
     Function/Report/Main + GLOBALS 引入的函数定义。
   - **实测 24 个真实 .4gl 报 3660 条（占全部 80%）**：内置函数
     （`ARG_VAL`/`LENGTH`…）与跨模块函数都还没有符号表。默认 off，
     待 P7-C（内置符号表）+ P7-D（链接索引）落地后重新度量再打开。
   - `cmd/diag-audit` 是这个结论的前提：它不起 LSP 连接，走
     `Server.AuditFile` 直跑完整语义诊断流水线，输出逐码计数、高频标识符、
     噪声最高的文件。`make diag-audit` 一行出基线。

#### 顺带修掉的两个 `.per` 误报（实测占全部诊断 13%）

跑全量语料才看见的两个缺陷，都属于“一个节点里藏了多个名字”：

| 缺陷 | 根因 | 修复前 → 后 |
|---|---|---|
| `form-layout-tag-undefined` | 一个 `item_tag` 节点常含**多个** tag（`[label1 \|alh01]`），旧实现把整段当一个 tag 去查 ATTRIBUTES，永远查不到 | **40167 → 17** |
| `form-screen-record-undefined` | SCREEN RECORD 引用的是**字段名**（`= formonly.more` 的 `more`），不是 item tag（`mor`）；旧校验只认 tag | **4850 → 5** |

修法：`splitLayoutTags` 按 `|` 切分并算出每段的字节区间（诊断位置要准）；
`FileIndex.FormFieldNames` 收集 `attribute_item.field` 的列名，与
`KindFormItem` 一起构成 `defined` 集合。

#### 语料基线（tiptop-gp，15108 个 .4gl + .per）

| 诊断码 | 修复前 | 修复后 | 说明 |
|---|---:|---:|---|
| `variable-source-unresolved` | 287448 | 287448 | 全是内置全局（见下） |
| `form-layout-tag-undefined` | 40167 | **17** | 已修 |
| `form-screen-record-undefined` | 4850 | **5** | 已修 |
| `field-not-in-form` | 1725 | 1725 | 待查（`CONSTRUCT BY NAME … ON a,b,c`） |
| `globals-file-not-found` | 107 | 107 | 真实缺失 |
| `form-duplicate-item` | 2 | 2 | 真实重复 |
| `function-not-found` | 3660 | 0 | 默认 off |
| **合计** | **334299** | **289304** | **−13.5%** |

`.per` 侧：45019 → **24**（−99.9%）。

`variable-source-unresolved` 的高频标识符（前 12）全部是**内置全局**：

```
SQLCA 148641 · INT_FLAG 61382 · STATUS 42508 · UI 9832 · BASE 6342
XML 2977 · COM 2636 · OS 2023 · SUM 2007 · TIME 1542 · DIALOG 557 · PAGENO 514
```

前 12 项合计 **28.4 万条，占剩余全部诊断 98%**——这正是 P7-C 要消掉的量。
换句话说：**P7-B 之后剩下的噪声几乎只剩“没有内置符号表”这一件事**，
而它已经在下一轮的排期里。

#### 验收

- [x] 单测：`internal/diag` 新增 8 个（级别映射/优先级/注册表/拼错保护）。
- [x] 单测：`internal/config` 新增 4 个（增量合并/未下发不清空/非法级别）。
- [x] 单测：server 新增 9 个（`WHENEVER` 函数名、表达式函数名、
      `FORMONLY.x` 字段、逐码 off/覆盖、`function-not-found` 默认关/开启/本地定义、
      `syntax-error` 逐码、MISSING 两级优先级）。
- [x] 单测：index 新增 2 个（单元格多 tag 切分 + 字段名收集）。
- [x] `go test -race ./...` 21 包全绿；`make lint` 通过。
- [x] VSCode 清单：`bdl.diagnostics`（object）+ `syntaxDiagnostics` 默认
      `warning`；`collectSettings` 显式转交（白名单，不列就不下发）。
- [x] 语料基线见上表。

#### 踩坑新增（已并入 HANDOVER §7）

- **22**：field 来自**隐藏规则别名**时（`(whenever name: …)` 的 `name` 来自
  `_names: field("name", $.variable)`），node-types.json 里父节点没有这个字段，
  query 编译报 **Impossible pattern**，而**失败是静默的**——整个 query 被
  丢掉，一个诊断都不产生。写法要退到结构匹配 `(whenever (variable …))`。
- **23**：`item_tag` 节点不等于“一个 tag”。布局单元格 `[a |b ]` 是一个节点
  含两个 tag；`DISPLAY … TO x.y` 的 field 是一个 variable 里嵌 object 再嵌
  variable。**凡是“节点文本 = 一个名字”的假设，都要在真实语料上验证一遍**。

---

### 10.6 P7-C-1 内置符号表（✅ 2026-10-21，`0.1.3`）

#### 为什么先做 C-1 而不是 C-2

P7-B 的度量已经指明：全量 28.9 万条诊断里，`variable-source-unresolved`
占 287448 条，其中前 12 个名字就占 98%。这 12 个全是**内置全局**
（`SQLCA`/`INT_FLAG`/`STATUS`/`UI`/`BASE`…），不是 grammar 问题——
`tree-sitter parse` 对它们零 ERROR。C-2 是"低频长尾"，C-1 是"主要噪声源"，
顺序没有悬念。

#### 落地结构

```
internal/builtin/
  builtin.go   Set/Symbol/Kind + 查询 API（Lookup/IsKnown/IsFunction/Prefix）
  load.go      dataFile schema、go:embed、覆盖层合并、Loader（热更新）、Validate
  data/builtins.json   96 条：8 变量 + 73 函数 + 14 类 + 1 常量
cmd/builtin-audit/main.go   缺口审计 + -check 校验
```

四条接入：

| 接入点 | 行为 |
|---|---|
| `unresolvedSourceChecks` | 内置名跳过 → 这是 −97% 的全部来源 |
| `functionNotFoundChecks` | 只认 `KindFunction`（类/方法不豁免，交给 P7-D） |
| 补全 | 排在跨文件符号之后、关键字之前；同名用户符号存在时跳过 |
| 悬停 | 只在本文档与 GLOBALS 都查不到时兜底；显示签名/类别/`Since`/可覆盖提示 |

#### 度量结果

| 指标 | P7-B | P7-C-1 | 变化 |
|---|---:|---:|---:|
| 全量诊断总数 | 289304 | **7886** | **−97.3%** |
| `variable-source-unresolved` | 287448 | **6030** | **−97.9%** |
| 24 文件样本 | 889 | **7** | −99.2% |
| 调用位置缺口（`FGL_*`） | — | **0** | 内置函数侧已封闭 |

剩余 6030 条里前 20 名全是用户跨模组全局（`S_*`、`WORKSERVICEIMPL…`），
属 P7-D。`field-not-in-form`（1725 条）因此升为第二大项，待查。

#### 与计划的两处偏差（都是刻意的）

1. **96 条，不是"≥200 条"**。验收原本写"内置表 ≥ 200 条"，实际做成 96。
   理由：表里每一条都必须有语料证据或手册依据。硬凑到 200 条只能靠
   编名字，而编错一个的代价是**掩盖真实的链接缺失**（这正是 P7-D 要解决的问题）。
   `FGL_*` 缺口已为 0，说明 96 条对当前语料是**充分**的；将来不够时，
   加一条只要改 JSON（§4.4）。
2. **新增了不在原计划的第四个消费点**：`function-not-found` 白名单。
   P7-B 把它默认关掉就是等内置表；不接这一处，等 P7-D 打开它时
   内置函数会一次性涌出几千条误报。

#### 踩到的坑

- **语料里的 `SUM` 有两个身份**：`SELECT SUM(x)`/`GROUP SUM` 是聚合函数，
  但同名用户变量也存在。结论：内置表对**变量位置**的豁免是"名字级"的，
  分不清声明与使用；这只会多放过，不会误报，可接受。
- **`ARR_CURR`/`ARR_COUNT`/`NOTFOUND` 是审计发现的漏项**（11950/3657/117 次），
  手册整理时被我漏掉了。这三个正好证明 §4.3 第 2 条"靠手抄一定漏"。
- **`Prefix("")` 必须返回全部**：补全的手工触发（Ctrl+Space）前缀为空，
  早期实现直接返回 nil，导致"内置名在补全里永远看不到"。

## 11. 真实数据验证记录（2026-10-21）

> 数据：`link.csv`（529042 B，sha256 `3ca6ff10…`，15587 行）。  
> 复现脚本建议：`go run ./cmd/link-audit -csv link.csv -module-roots tiptop,topcust`

### 11.1 本次核实的事实（全部实测，非估算）

| # | 事实 | 数值 | 对设计的影响 |
|---|---|---|---|
| 1 | `gal04='N'` 需 SQL 阶段过滤 | 51 行 | 已确认规则 R4 必要 |
| 2 | 组内恰好 1 个 MAIN | 5374 / 5849 组（91.9%） | **主键策略成立** |
| 3 | 组内 0 个 MAIN | 81（文件存在）+ 389（全缺失） | 需 `orphan` 类别 |
| 4 | 组内 ≥2 个 MAIN | 5 | 需 `ambiguous` 标记 |
| 5 | `GAL01` 不在自己组内 | 160 组 | 证实"编号≠文件名"，主键必须用 MAIN 代码 |
| 6 | 程序代码被多组共享 | `aws_xml` 被 324 组、`aws_eflog` 301 组 | 反查结果需分页（Q13） |
| 7 | 磁盘找不到文件 | 800 条 / 700 代码，集中在 `WPC/WSUB/WMB/WZZ/WAG/AZZ/WLIB` | 不报错，计入 `missingFiles` |
| 8 | 模组名大小写 | 数据 92 个全大写；磁盘全小写 | **必须大小写不敏感匹配** |
| 9 | CSV 含 UTF-8 BOM | 是 | 必须 `utf-8-sig` 读，否则首列名带 BOM |
| 10 | 链大小分布 | p50=1、p90=2、max=364 | 99% 无需异步 |
| 11 | 单文件 JSONL 体积 | 415 KB（剔除隐式组后） | 单文件够用 |
| 12 | CSV 也含 sub/lib/qry 成员 | sub 524 / lib 218 / qry 264 | 按 Q14 作为全局模组集合，与 CSV 取并集 |
| 13 | `lib → sub` 调用 | 11 文件 / 84 调用点 | 与 Q14 规则冲突，待确认 |
| 14 | `sub → lib` 调用 | 436 文件 / 13226 调用点 | 印证"sub 可用 lib" |
| 15 | `→ qry` 调用 | sub 9 点 / lib 20 点 | 印证"qry 谁都能用" |

### 11.2 由数据修正的早期判断

| 早期写法 | 实测后修正 |
|---|---|
| "全量 1–2 MB" | **415 KB**（约 1/4） |
| "隐含模组约 980 文件" | **983**（sub 518 + lib 204 + qry 261） |
| "加载 30–60 ms" | **10–20 ms** |
| "解析链路 100–300 ms" | 典型链 **2–10 ms**（p50=1 文件）；只有 4 条超大链需 0.7–1.8 s |
| "阶段 2：stale/DB 连接" | 保留，但 Q15 明确驱动为**双方案** |
| 链接目录 `.bdl-lsp/link/` | **`<project>/.link/`**（Q12 用户定稿） |

### 11.3 P7-C 内置符号表的复测（2026-10-21，同一语料）

| 指标 | P7-B 基线 | P7-C-1 后 | P7-D-2 后（全开） | 变化（相对基线） |
|---|---:|---:|---:|---:|
| 扫描文件 | 15108 | 15108 | 15108 | — |
| 诊断总数 | 289304 | **7886** | **9244** | −96.8% |
| `variable-source-unresolved` | 287448 | **6030** | 6030 | −97.9% |
| `field-not-in-form` | 1725 | 1725 | 1725 | 0（待查） |
| `globals-file-not-found` | 107 | 107 | 107 | 0 |
| `function-not-found` | 3660（24 文件） | 关 | **1358** | 已从 off 改为**默认 warning** |
| 其余表单类 | 24 | 24 | 24 | 0 |

> P7-D-2 的数字包含 `function-not-found`（默认已开），所以总数比 P7-C-1 的
> 7886 多 1358；把这一项关掉就是 7886，与上一轮**完全一致**（无回归）。

`builtin-audit`（800 文件抽样）：调用位置缺口 488 个（**全部** `CL_*`/`S_*`
用户函数）、`FGL_*` 缺口 **0**、变量位置缺口 204 个（全是用户全局）。

结论：**内置名这一半的噪声已经清干净**，剩下的都是 P7-D 的链接资料问题。

---

### 10.7 P7-D 程序链接资料（核心已落地，2026-10-21，`0.1.4`）

#### 落地结构

```
internal/link/
  link.go      FileRef/Chain/Meta/Index + Load/Save（原子写）+ ErrNoData
  layout.go    Layout：模组根探测、大小写不敏感 Path、FileID 反推
  import.go    CSV → 链（BOM、gal04 过滤、组内去重、orphan/ambiguous、缺失计数）
  maintext.go  文本层 MAIN 检测（与语法注释规则一致）
  symbols.go   SymbolIndex：文件级函数名缓存 + 全局模组后台预热 + 同步预算
  resolver.go  Resolver：可见函数 = 本文件 ∪ GLOBALS ∪ 链接 ∪ 全局模组
  parsebdl.go  真实 ParseFunc / MainFinder（tree-sitter）
  run.go       RunImport + Status（CLI 与审计共用同一段逻辑）
internal/linkcli/   `bdl-lsp link import|status`
cmd/link-audit/     真实 CSV 复跑与核对
```

#### 与计划不同的三处（都是实测逼出来的）

1. **ambiguous 从 5 变成 0**：计划里的 5 个多 MAIN 组全部来自
   `aws_ttsrv2_transxml.4gl` 的 `{ MAIN … }`。`{ }` 在语法里是**块注释**
   （`rule/fgl.js`：`seq("{", /[^}]*/, "}")`），所以那个 MAIN 是被注释掉的。
   早先的 Python 脚本按文本正则统计 → 多算了 4 组。现在 `ScanMainText`
   与语法注释规则一致（`#`/`--`/`{ }`），结果与语法树一致：**ambiguous = 0**。
   `Chain.Mains` 与 ambiguous 代码路径保留（防御性，合成数据单测覆盖）。

2. **单文件 753 KB，不是 415 KB**：计划打算把 `sub`/`lib`/`qry` 这类隐式组
   从文件里剔除。实际保留全部 5398 条链——P7-E 的"反查引用程序"需要这些
   索引，而加载实测只要 **~10 ms**（753KB / 5398 行），没有剔的理由。

3. **缺失文件不是"忽略后照常判定"，而是"不可判定"**：见 HANDOVER 坑 #27。
   影响面实测很小（5849 条链里 234 条，4.0%）。

#### 真实语料的最终数字（复跑 `cmd/link-audit`）

| 指标 | 计划估值 | 实测 |
|---|---:|---:|
| CSV 行数 | 15587 | 15587 |
| `gal04='N'` 过滤 | 51 | 51 |
| 有效行 | 15536 | 15536 |
| 链（有 MAIN） | 5846 | **5398** |
| orphan（无 MAIN） | 81 | **451**（其中 389 组是文件全缺失） |
| ambiguous | 5 | **0** |
| 缺失文件 | 800 | 800 |
| 去重文件数 | 7931 | 7931 |
| 最大链 | `cbmp100` 364 | `sub` 524 / `aws_ttsrv2` 368 / `cbmp100` 364 |
| 导入耗时 | — | **15 s**（8 核并行，含 7765 次 MAIN 判定） |

#### `function-not-found` 的复测结论：仍然默认 off

全量语料（15108 文件）打开后 **86612 条**，占 91.7%。逐名统计（全量）：

| 名字 | 次数 | 语料里存在吗 |
|---|---:|---|
| `cl_err3` | 27315 | **不存在**（`lib/4gl` 只有 `cl_err.4gl`，无 `cl_err3`） |
| `WSHelper_FillSOAP11WSError` | 12516 | **不存在**（Genero Web Service 生成库，快照里没有） |
| `cl_qbe_init/save/select/display_condition` | 18421 | **不存在**（没有 `cl_qbe.4gl`） |
| `WSHelper_CheckStax*` 等 12 个 | ~14800 | **不存在** |
| `cl_cmdrun` / `cl_cmdrun_wait` | 3755 | **不存在** |
| `SCR_LINE` | 27 | 存在，是**内置函数**，已补进内置表（0.1.4，86612 里已扣掉） |

前 20 个名字占 **99.9%**，全是"语料快照里根本没有的库函数"——不是用户写错，
也不是可见性算错。分布极度集中（2000 文件抽样：8014 条只来自 **30 个名字**，
其中 16 个名字就占 99.8%；只有 13 个名字出现 1 次）。语料是**部分快照**
（另有 800 条链接文件缺失、`W*` 模组整批不在），在这个前提下
`function-not-found` 无法给出干净结果。

因此：**默认 off 不变**，`bdl.diagnostics.function-not-found: warning` 按需打开；
打开后 `variable-source-unresolved` **仍是 6030 条**（R2 成立：链接不放大变量可见性）。

#### ✅ P7-D-2（2026-10-21，`0.1.5`）：语料推导的"外部函数"过滤

已落地。口径是用户定的：**函数名在项目里找不到定义，多半是语料快照缺库，
不是用户写错；只要不是太多就忽略，但必须记录下来。**

实现（`internal/link/corpus.go` + `import.go:buildExternals`）：

- `ScanCorpus` 用**与诊断同一套 query** 解析整份语料（7600 个 `.4gl`），
  同时得到"定义了哪些函数""谁在调谁、调了多少次"。链接导入本来就要解析
  7795 个文件，等于顺手做完，导入耗时 15 s → **24 s**。
- 候选 = 被调用 **≥3 个文件 或 ≥5 次**、且**整份语料**（含未链接文件）里
  没有任何 `FUNCTION` 定义的名字。双阈值不可省：`cl_err3` 是 3340 个文件，
  而 `WSHelper_FillSOAP11WSError` 只在 2 个生成程序里、每个上千次。
- 命中者写进 `links.meta.json` 的 `externals[]`（名字 + 文件数 + 次数，
  最多存 500 条，总数另记）；解析器把它们并入可见函数集。
- **兜底**：候选超过 2000 个时置 `externalsSkipped` 并**不启用**过滤
  ——那说明资料或项目根不对，宁可多报。
- 记录出口：`bdl-lsp link status` 与 `cmd/link-audit -externals`。

全量语料实测（15108 文件）：

| 指标 | P7-D | +P7-D-2 |
|---|---:|---:|
| 诊断总数 | 94498 | **9244** |
| `function-not-found` | 86612 | **1358**（−98.4%） |
| 外部函数名 | — | **62** 个 |
| `variable-source-unresolved` | 6030 | 6030（未受影响，R2 成立） |

因此 `function-not-found` **默认级别从 off 改为 warning**（Q10 的例外只剩
`record-member-unknown`）。没有链接资料时该项不产生任何诊断，对未启用的
项目零影响。

剩余 1358 条的性质：全部是"语料里**有**定义、但链接里看不到"的函数
（`aws_ttsrv_*`、`p200_log`、`cs_darcy_*`），属于真实的"库没链接进来"
问题——这正是打开这项诊断想看到的东西。

#### 附录：三个被实测否掉的中间方案（记录，避免重走）

1. **只统计链接文件**（第一版）：`externals` 只有 51 个名字，fnf 从 86612
   降到 6130。漏掉的原因很清楚——`aws_crm.4gl` 这类文件不在任何链上，
   它的调用根本没被统计到，`WSHelper_CheckStax*` 就漏了。
2. **文本正则统计调用**（第二版）：`VALUES(`、`SUM(`、`IN(` 这类 SQL/关键字
   写法被当成函数调用，候选暴涨到 2795 个，直接触发"太多不启用"。
   文本扫描只能用来认 `FUNCTION` 定义，不能用来认调用。
3. **只用解析结果、但只看链接文件**：见 1。

最终方案 = **整份语料解析一遍**（与诊断同一个 query，天然一致），
定义集与调用集都来自全语料，耗时只多 9 s。

---

### 10.8 P7-D-3 采集通道（✅ 2026-10-21，`0.1.6`）

`internal/linksync`：三套来源共用一个接口，产物是同一份 `links.jsonl`。

| 来源 | 实现 | 适用 |
|---|---|---|
| `golang` | `sijms/go-ora/v2` 纯 Go 直连，`database/sql` + `BuildUrl` | 本机能连到库（**无需 Oracle 客户端**） |
| `command` | `sh -c` / `cmd /C`，CSV 从 stdout 读回 | 已有现成导出脚本 |
| `file` | 读 `<dataDir>/gal_file.csv` | 服务器上 `sqlplus` 导出后拷回来 |

关键决策：

- **密码绝不进 settings.json**。优先级：`${env:VAR}`（配置里写引用）→
  环境变量 `BDL_ORA_PASSWORD` → `<dataDir>/secrets.json`（**权限不是 0600 就报错**，
  不"默默能用"）。`config.Validate()` 会主动指出"这看起来是明文密码"。
- 幂等与安全：采集失败**不动**已有资料（先解析成功再原子写盘）；
  采集给 60 s 超时上限，不会把编辑器卡死。
- LSP 侧后台同步：`bdl.link.sync.{enable,source,interval,onStart,command,file,db.*}`，
  默认**全关**；失败指数退避（上限 1 h），成功后自动重载资料并补发一轮诊断。
- S2 的脚本是 `scripts/fetch-gal-file.sh`：`read -s` 输密码（不进 history、
  不进 `ps`），`SET MARKUP CSV ON` 直出，`gal04='Y'` 在 SQL 里过滤。

命令：

```bash
bdl-lsp link sync -source golang -host db -service PROD -user readonly   # 直连
bdl-lsp link sync -source command -command ./export.sh                   # 命令
bdl-lsp link sync -source file -file .link/gal_file.csv                  # 文件
bdl-lsp link sync -source file -dry-run                                  # 只校验不写盘
```

实测：`-source file` 全流程（采集 → 导入 → 校验）24.5 s，结果与
`link import` 逐字节一致（走同一段 `RunImport`）。

---

### 10.9 P7-E 链接资料 UI（✅ 2026-10-21，`0.1.7`）

**服务器侧**：两个自定义请求，都不碰 LSP 标准方法。

| 请求 | 返回 |
|---|---|
| `bdl/linkTree` | `{found, program, main, ambiguous, files[], modules{}, refs[], refTotal}` |
| `bdl/linkReferences` | `{refs[], refTotal}`（只做反查，省一次组装） |

- `files[].path` 由服务器按 `Layout` 的模组根解析（插件不需要知道资料格式）；
- `modules{}` 是 `sub`/`lib`/`qry` 三个全局模组的文件清单，**缓存在 resolver 上**
  （lib 一个模组 983 个文件，每次重建纯属浪费）；
- 反查复用 `Index.ChainsOfFile`，同一遍扫描建好的索引，不额外读盘；
  上限 50 条（实测最多 324 组，分页交给插件）。

**性能**（实测真实语料，`TestTreeOnRealCorpus` 钉住）：

| 操作 | 首次 | 再次 |
|---|---:|---:|
| 最大的链（aws_ttsrv2，368 文件） | **3.4 ms** | 0.3 ms |

第一版逐文件 `os.ReadDir` 找大小写不匹配的名字，同一条链要 10 s 以上；
现在 `Layout` 按模组缓存"小写文件名 → 真实文件名"，请求路径只剩字符串查表。

**VSCode**：`artifacts` 之外的独立视图「链接资料」（Explorer 侧栏），三级结构

```text
链接资料
├── axmt620                ← L1 程序（MAIN）
│   ├── ALM/s_point        ← L2 链内文件（点击打开）
│   └── APM/sapmp500
├── [ 反查引用程序 ]        ← 子文件才有，点开才查询
├── LIB  (983 个文件)
└── SUB  (…)
```

- 文件节点点击 = `vscode.open`，直接跳到 `apm/4gl/sapmp500.4gl`；
- 反查**默认不执行**，点一下才取；一次 50 条，超出显示"显示更多…"；
- 没有资料/不在任何链里时显示引导文案，不报错。

**Zed 降级（与原计划的偏差）**：原计划把链路追加到 Zed 的 `documentSymbol`
末尾。落地时否掉了——`documentSymbol` 是 **VSCode 大纲的数据源**，追加进去
会让 VSCode 大纲里多出一堆不可跳转的假符号，是为了一个客户端污染另一个。
Zed 侧改为：用 `bdl-lsp link status` / `cmd/link-audit -externals` 看资料，
或直接用 VSCode 的视图。Zed README 已写明这一点。

**顺带修掉的两个 e2e 红**（都是本轮之前就红了）：

1. 语法错误断言 `severity === Error`，但 Q10 定稿默认是 **warning** →
   改成断言 `code === "syntax-error"` + 级别为 warning；
2. **补全里一个关键字都没有**：BDL 关键字 361 条排在最末，上限 200 条，
   内置符号（97 条）加进来后整类被截掉 → 上限提到 600 并加单测钉住。

---

### 10.10 P7-C-2 语法缺口清单（✅ 2026-10-21，`0.1.8`）

**结论先说**：语料 **15108 个文件里只有 18 个（0.12%）解析失败，共 266 个
ERROR 节点**。逐个确认后，**没有一个是高频语句级缺口**——全部是语料自身
的问题，按计划"低频项只留档不排期"处理。

工具：`make grammar-audit`（`internal/gramaudit` + `cmd/grammar-audit`）。

```bash
make grammar-audit                       # 前 30 个缺口
make grammar-audit TOP=60 JSON=/tmp/a.json
# 改完 grammar 后逐文件对比（真正的验收手段）
go run ./cmd/grammar-audit -json /tmp/before.json && ... && diff <(...) <(...)
```

#### 判据：只记"最内层" ERROR

语料里一个拼写错误（`END TUNCTION`）会让 tree-sitter 把**整个文件**包成
一个根级 ERROR。第一版按外层记，清单头一行是"第 1 行的注释"——毫无用处。
现在只记内部没有 ERROR 的节点，示例直接指到出错的语句。

#### 18 个文件的实际成因（逐个确认过）

| 文件 | 节点 | 成因 |
|---|---:|---|
| `demo/per/apmi252.per` | 97 | `.per` 尾部**文档附录**里的全角标点（`－`、`、`） |
| `demo/per/aooi010.per` | 48 | 同上 |
| `msv/4gl/findidx.4gl` | 67 | 手写开发文件：`smaillint` 拼写错 + 该文件本就残缺 |
| `cws/4gl/cws_get_start_works.4gl` | 11 | **SQL `CASE … END CASE` 表达式**（真实缺口，见下） |
| `asf/4gl/safi501.4gl` | 10 | 某处 `MENU` 未闭合引起的级联，未定位到具体语法缺口 |
| `asf/4gl/TX_asfi301_1/2/3.4gl` | 5×3 | 同上（三份内容几乎相同的副本） |
| `apm/4gl/sapmt540 (3).4gl` | 4 | 末行 `END TUNCTION` 拼写错 |
| `apm/4gl/sapmt720_sub.4gl` | 4 | SQL `CASE … END **IF**`（应为 `END CASE`） |
| `apm/4gl/baofei.4gl` | 2 | 赋值漏了 `LET`（`l_sql = " SELECT …"`） |
| `asf/4gl/csfi510.4gl` | 2 | 两个完整程序**拼接**在一个文件里 |
| `ghr/4gl/ghri0431.4gl` | 1 | 字符串被截断（`CALL cl_err('绱`） |
| `ghr/4gl/ghri0471.4gl` | 1 | 文件头是乱码 BOM（字面 `锘`） |
| `csf/4gl/csfq012.4gl` | 1 | `DEFINE` 列表漏逗号 |
| `csub/4gl/cs_restful.4gl` | 1 | 复制粘贴残留半行（`when i == 0 && j >`） |
| `cxc/4gl/cxcq761.4gl` | 1 | 多一个 `END IF` |
| `cxm/4gl/saxmt400.4gl` | 1 | 文件**被截断**（结尾停在 `#No.FUN-AA0048  --Begin`） |

#### 唯一的真实语法缺口：SQL 里的 `CASE … END CASE` 表达式

```sql
SELECT DISTINCT '2',sgm04,...,ta_ecd03,          -- 行尾逗号
                CASE ta_ecd03 WHEN 'N' THEN 1 ELSE sfbud10 END CASE
  INTO l_return2.*
  FROM sgm_file ...
```

`sql_body` 是外部扫描器（`src/scanner.c`），它靠"关键字在行首 + 括号深度 0
+ CASE 栈空"判断 SQL 语句结束。`CASE`/`END CASE` 在 SQL 表达式和 BDL 的
`CASE` 语句里长得一模一样，扫描器只能猜。

**试过三种消歧规则，全部被语料否掉**（这是本轮最重要的结论）：

| 尝试 | 结果 |
|---|---|
| 一律把 `END CASE` 当 SQL 表达式结束 | 18 → **20 文件 / 279 节点**（`axcp305`、`axcp312`、`aapq106` 回归） |
| 行首 `CASE` = BDL 语句，行内 = SQL 表达式 | 回归更严重（`cws_get_start_works` 的 `CASE` 恰好也在行首） |
| 再看 `END CASE` 后面的词（`INTO`/`FROM`/逗号 ⇒ 表达式） | 扫描器需要**向前多看一个词**，而 tree-sitter 的 lexer 不能回退；为了看这个词就得消费，消费后无法重扫 → 18 → **222 文件 / 16342 节点** |

结论：**不改**。收益是 1 个文件（另一个是 `END IF` 拼写错，改语法也修不好），
风险是任意文件。写进 `tree-sitter-bdl/BACKLOG.md`，等真出现大批 SQL CASE
写法（比如换一套语料）再处理，届时的正确做法是在**语法层**给 `select`
加一条 `sql_body END CASE` 的备选，而不是在扫描器里猜。

#### 回归防线

`internal/gramaudit/grammar_baseline_test.go`（`TestCorpusGrammarBaseline`）
冻结了这 18 个文件的**相对路径白名单**：

- 出现白名单外的文件 → 失败并列出；
- 文件数变少 → 通过（修好了）。

这比"总数 ≤ 18"强：总数不变也可能是"修好一个、坏掉一个"。

#### 一个坑（写进 ENVIRONMENT/HANDOVER）

`bindings/go/binding.go` 是 `#include "../../src/parser.c"` 的 cgo 绑定，
**Go 的构建缓存不跟踪被 include 的 C 文件**。改了 `src/scanner.c` 之后
直接 `go test` / `go run` 跑的还是旧编译产物（症状：改 grammar 前后
审计结果一模一样）。必须：

```bash
go build -a ./internal/parser   # 或者 go clean -cache
```

本轮就在这里白白多花了两轮：以为改动没生效，其实是缓存。

---

### 10.11 P7-B 收尾：诊断降噪 + 码表防漂移（✅ 2026-10-21，`0.1.9`）

两项：`field-not-in-form` 的 1725 条查清（**9309 → 8453**），
`make diagnostics-check` 落地。

#### 一、`field-not-in-form`：两类系统性误报

**结论：不是表单解析没命中，是校验语义错了两处。**

**(1) `CONSTRUCT BY NAME` 的 ON 列表是数据库列名**

4js 手册 "The CONSTRUCT sub-dialog" 写得很清楚：

> `CONSTRUCT BY NAME string-variable ON column-list`
> The `BY NAME` clause implicitly binds the form fields to the columns,
> where the form field identifiers **match the column names** specified in
> the column-list after the `ON` keyword.

即 ON 后面是**列**，Genero 拿列名去*隐式匹配*同名表单字段。列在表单里没有，
只说明"这一列不进查询条件"，代码完全合法。语料 4456 处 `CONSTRUCT BY NAME`，
其中 1725 处 ON 列表里有表单中没有的列——全部误报。

同一语句还有第二种写法 `CONSTRUCT var ON column-list FROM field-list`，
**只有 FROM 后面才是字段列表**。语法树里两者都用同一个 `field` 字段名，
所以校验按位置判断（`constructOnColumns`：找祖先 `construct` 节点，看该标识符
之前有没有 `FROM` 关键字）。

**(2) 表单的"字段名"不等于"item tag"**

Tiptop 表单大量这样写：

```
LAYOUT   { [aa1        ] }                              -- tag = aa1
ATTRIBUTES   Edit aa1 = FormOnly.ada01;                 -- 字段名 = ada01
```

4GL 里 `BEFORE/AFTER FIELD`、`NEXT FIELD`、`INPUT … FROM`、`DISPLAY … TO`
引用的是**字段名**（`ada01`），不是 tag（`aa1`）。索引一直在收集字段名
（`FormFieldNames`，当初为修 `form-screen-record-undefined` 的 4850 条误报），
但字段校验只看 `Symbols`（KindFormItem = tag）与 SCREEN RECORD 成员，
漏了这一项 → 236 条误报（如 `axdr222.4gl` 的 `INPUT … FROM ada01`）。

#### 二、结果与剩余项

| 指标 | 修前 | 修后 |
|---|---:|---:|
| 全量诊断总数 | 9309 | **8453** |
| `field-not-in-form` | 1725 | **869** |
| `variable-source-unresolved` | 6030 | 6030 |
| `function-not-found` | 1423 | 1423 |
| `globals-file-not-found` | 107 | 107 |

> 注：`function-not-found` 现在是 1423（不是 P7-D-2 文档里的 1358）——那之后
> 链接资料重建过，可见集变了。以本轮实测为准。

剩余 869 条抽样确认是**真**问题：代码引用的字段在表单里确实不存在
（`axdr222` 引用 `ada01`、而该表单只有 `to_apa15`；`aapr160` 的
`INPUT BY NAME tm.plant_1..8` 指向的 QBE 表单只有 12 个字段；
合并文件 `saapt110.4gl` 3 万行里混着别的程序的表单一侧）。

`-form <字面量>` 参数（本轮给 `cmd/field-audit` 加的）就是为逐个表单抽查用的。

#### 三、`make diagnostics-check`（码表防漂移）

一个诊断码同时在三处出现，漂移方式各不相同：

| 位置 | 漂移后果 |
|---|---|
| Go 注册表（`internal/diag`） | 事实来源 |
| `docs/DIAGNOSTICS.md` | 手写必然滞后，用户查不到新码 |
| VSCode `package.json` | 设置界面能填的级别/默认值可能与服务端不一致 |

做法：

- 注册表补 `summaries`（每码一句话）+ `Specs()`，作为**唯一来源**；
- 文档由 `make diagnostics-write` **生成**（13 个码 + 老键映射表 + 动态码说明），
  `make diagnostics-check` 逐字比对；
- `package.json` 校验三项：`bdl.diagnostics` 的级别枚举、六个老键的
  `default`/`enum`、**同一老键覆盖的码默认级别必须一致**（否则"老键的默认值"
  是二义的——这是最容易被忽视的一条，已单测钉住）；
- 并入 `make lint`，忘不掉。

校验有牙：故意把 `bdl.fieldDiagnostics.default` 改成 `off`，`make lint` 立刻失败
并指出是哪个键。

---

## 12. 文档地图（本文相关）

| 文档 | 内容 |
|---|---|
| [HANDOVER.md](HANDOVER.md) | 会话接手：状态、基线、坑、下一步 |
| [TODO.md](TODO.md) | 待办清单（§下一轮：P7） |
| [PLAN.md](PLAN.md) | 总规划；§8 P7 里程碑 |
| [FEATURES.md](FEATURES.md) | 功能基线编号（P7 新增 35–40） |
| [PLAN-P3A-GLOBALS.md](PLAN-P3A-GLOBALS.md) | GLOBALS 解析规则（P7-D 复用其缓存与路径模式） |
| [PLAN-P6-COMPLETION.md](PLAN-P6-COMPLETION.md) | 补全（P7-D 要接函数来源） |

