# P7 计划：缺陷修复 · 内置符号 · 链接资料索引 · 诊断级别统一

> 版本：v1.0（立项）  
> 日期：2026-10-21  
> 状态：**规划完成，待执行**。P7-A（两个已确认 BUG）为下一轮首先处理项。  
> 上游文档：[HANDOVER.md](HANDOVER.md)（接手）· [TODO.md](TODO.md)（待办）· [PLAN.md](PLAN.md)（总规划）  
> 约定：本文所有"已验证"结论均附实测证据；未验证的一律标 **假设（需确认）**。

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

- [ ] `node tests/verify-queries.mjs` 新增断言通过，且在旧 query 上**必然失败**（先写断言复现）。
- [ ] 手工验证（需 GUI）：`DEFINE a INT, b STRING` 在 Zed 大纲中 `a`、`b` 同级。
- [ ] VSCode 大纲行为无变化（本来就正常）。

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

- [ ] `internal/fileuri` 单测覆盖上表 7 条 + 往返（`FromPath(ToPath(x)) == x`）。
- [ ] `go test ./internal/globals ./internal/config ./internal/server` 全绿。
- [ ] 协议级测试：用 `rootUri=""` + `workspaceFolders=[...]` 初始化，断言 `rootPath` 正确。
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
| `file-too-large` | warning | 无 | 应归入 limits |

**设计**：新增统一配置块 `bdl.diagnostics`，按诊断码取值。

```jsonc
{
  "bdl.diagnostics": {
    "*": "warning",                       // 兜底默认 = warning（Q10 定稿）
    "record-member-unknown": "off",       // 用户可关；默认仍 off
    "variable-source-unresolved": "warning"
  }
}
```

要点：

1. **集中注册表**：Go 侧建 `internal/diag/registry.go`，每个码登记 `{Code, Default, Description, Category}`。级别解析统一走 `registry.Severity(code)`，删除各功能里散落的 level 判断。
2. **向后兼容**：现有独立配置项（`syntaxDiagnostics`、`globals.*`、`fieldDiagnostics`、`recordMemberDiagnostics`）继续生效，优先级 `diagnostics.<code>` > 旧键 > 注册表默认。旧键在文档中标记 deprecated，**至少两个版本内不删**。
3. **拼错保护**：`diagnostics` 里出现注册表没有的码 → 记 `warn` 日志（客户端 toast 可选）。现在的配置拼错是静默失效，这个坑必须堵。
4. **清单防漂移**：仿照现有 `keywords-check`，新增 `make diagnostics-check`，校验三处一致：Go 注册表 ↔ `docs/DIAGNOSTICS.md`（自动生成）↔ VSCode `package.json` 的 enum 列表。

**Q10 定稿：全部默认 `warning`。**

用户明确要求"默认都是 warn 警告"。这意味着 `syntax-error` 从 `error` 降为 `warning`。

需要记录的影响（不改变决定，只是写清后果）：

- 语法错误在 Problems 里变成黄色波浪线，不再是红色。用户可自行为 `syntax-error` 配回 `error`。
- `record-member-unknown` 保持 `off`（它不是"默认级别"，而是默认关闭的可选检查；用户也未要求开启）。
- 旧配置项里 `syntaxDiagnostics` 默认值是 `error`，需要同步改为 `warning` 以保持一致——否则走旧键的用户与走新键的用户行为不一致。

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

- [ ] 语料复测：24 文件诊断从 1717 → 记录新基线（预期 < 400，剩余主要是 `cl_*` 跨文件函数）。
- [ ] 单测：`IF fn() THEN`、`LET x = fn(1)`、`CALL fn()` 三种形态都不再产生 `variable-source-unresolved`。
- [ ] `function-not-found` 在无链接资料时恒为 0 条。

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

1. 手工种子表（我从 Genero BDL 参考手册整理 `fgl_*` 与 `ARG_VAL`/`SET_COUNT`/`STATUS` 等高频项）。
2. `cmd/builtin-audit`：扫语料，提取"所有未被定义、且不在表里"的标识符，按调用/变量分类输出，人工确认后回填。这是防止遗漏的主要手段（内置函数名几百个，靠手抄一定漏）。
3. 三个消费点依次接：① `variable-source-unresolved` 白名单（立刻降噪）② 补全 ③ 悬停显示签名/说明。

**C-2 grammar 缺口清单**（`tree-sitter-bdl`，独立仓库）

1. `cmd/grammar-audit`：对语料逐文件解析，收集 ERROR 节点上下文，按"出现最多的未知语句"排序，产出缺口清单。
2. 已知缺口先入清单：`STARTLOG`（语料 2 个文件）、`fgl_report_configure` 相关语句。
3. 优先级：**出现频次高且是语句级**的先补。`STARTLOG` 只影响 2 个文件 → 低优先级，但清单要留档。
4. 每次补 grammar 必须：加 corpus 测试 → 重生成 → 全量语料回归（`make regress`）→ 三个仓库锁 SHA。

**注意**：`cl_null`、`cl_user`、`cl_gr_numfmt` **不是内置函数**，是 `lib/` `sub/` 模组里的用户函数（已实测：`lib/4gl/cl_null.4gl:16: FUNCTION cl_null`）。它们归 P7-D 解决，不要误加进内置表——加进去会掩盖真实的链接资料缺失问题。

**验收**：

- [ ] 内置表 ≥ 200 条，`cmd/builtin-audit` 语料复跑后"未知且未分类"数量 < 20。
- [ ] 24 文件复测：`variable-source-unresolved` 再降一次（预期 < 150）。
- [ ] 补全里输入 `fgl_` 能给出候选。

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
// 诊断级别（P7-B）—— Q10 定稿：全部默认 warning
"bdl.diagnostics": { "*": "warning", "record-member-unknown": "off" }

// 内置符号（P7-C）
"bdl.builtins.enable": true

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

VSCode `package.json` 需同步新增（当前 32 项 → 约 40 项），并跑 `make manifest-check`。

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

## 10. 本轮（下一轮）执行清单

只做 P7-A，两件事，都可在一轮内完成：

1. **A-1 Zed 大纲层级**
   - [ ] 先给 `verify-queries.mjs` 加"`@item` 区间两两不同"断言，在旧 query 上确认失败
   - [ ] 改 `outline.scm`：`define` / `type` / `constant` 三个规则
   - [ ] 断言转绿；`cargo check` + 全部 query 校验通过
2. **A-2 VSCode GLOBALS 路径**
   - [ ] Windows 实机抓 `initialize` 入参（确定 URI 形态）
   - [ ] 新建 `internal/fileuri` + 单测（7 条用例）
   - [ ] 替换 `globals.PathOfURI` / `config.uriToPath`；补 `workspaceFolders` 回退
   - [ ] `go test -race ./...` 全绿
   - [ ] Windows 实机复验 `axmt620.4gl` 无 `globals-file-not-found`
3. 收尾：更新 TODO / HANDOVER / 本文档的状态；三个仓库提交推送。

### 10.1 P7 现已无遗留待确认项

2026-10-21 三次答复后，所有冲突已消解：

| 原待确认 | 结论 |
|---|---|
| `lib → sub` 11 处反例 | **消解**：Q14 改为"sub/lib/qry 对所有程序可用，含它们自己"，这 11 处是合法代码 |
| 本地 Oracle 驱动怎么接 | **消解**：Q15 取消本地驱动，只用纯 Go `go-ora` |
| 链接有、代码没有的文件怎么办 | **消解**：保留在 `links.jsonl`，运行时忽略，零诊断 |

所以 P7 可以**直接开工，不需要再问**。按 §1 的顺序：P7-A → B → C → D → E。

---

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

