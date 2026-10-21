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
    "*": "warning",                       // 兜底默认
    "syntax-error": "error",              // 语法错误保持 error（见 Q10）
    "record-member-unknown": "off",
    "variable-source-unresolved": "warning"
  }
}
```

要点：

1. **集中注册表**：Go 侧建 `internal/diag/registry.go`，每个码登记 `{Code, Default, Description, Category}`。级别解析统一走 `registry.Severity(code)`，删除各功能里散落的 level 判断。
2. **向后兼容**：现有独立配置项（`syntaxDiagnostics`、`globals.*`、`fieldDiagnostics`、`recordMemberDiagnostics`）继续生效，优先级 `diagnostics.<code>` > 旧键 > 注册表默认。旧键在文档中标记 deprecated，**至少两个版本内不删**。
3. **拼错保护**：`diagnostics` 里出现注册表没有的码 → 记 `warn` 日志（客户端 toast 可选）。现在的配置拼错是静默失效，这个坑必须堵。
4. **清单防漂移**：仿照现有 `keywords-check`，新增 `make diagnostics-check`，校验三处一致：Go 注册表 ↔ `docs/DIAGNOSTICS.md`（自动生成）↔ VSCode `package.json` 的 enum 列表。

**关于"默认都是 warn"的冲突（需用户拍板，见 Q10）**：

- `syntax-error` 现在是 `error`。改成 `warning` 会让真实语法错误在 Problems 里降级为黄色波浪线，弱化作用。
- `record-member-unknown` 现在 `off`。改成 `warning` 会在 `LIKE table.*` 场景（占语料 87%）大面积误报。
- 建议：**新增检查项默认 warning**；`syntax-error` 保持 error；`record-member-unknown` 保持 off 直到 P7-D 带来 schema 或链接信息后再复评。

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

**数据完整性约束**（导入时校验，问题进报告而不是静默丢弃）：

| 检查 | 处理 |
|---|---|
| 一组内 0 个 MAIN | 允许（子模组情形）→ 归入 `orphan` |
| 一组内 ≥ 2 个 MAIN | 数据异常 → 报 warn，全部保留但标记 `ambiguous` |
| 程序代码重复出现在同组 | 去重 |
| `gal04 <> 'Y'` | **SQL 阶段过滤**（R4），导入时再兜底一次 |

### 5.3 文档结构（用户方案 vs 建议方案）

用户建议：每个模组一份链接文档（按 MAIN 所属模组分组），无 MAIN 的单独一个目录。

**量化前提**：15000 条明细行。若平均每组 10 个文件 → 约 1500 条链路；单条链路 JSON 实测约 200–400 字节（按 `axmt620` 样例量得 200 B）→ **全量约 300–600 KB，远小于分片阈值**。

| 方案 | 优点 | 缺点 |
|---|---|---|
| **A. 单文件 `links.jsonl`**（建议） | 一次读入、原子替换、天然可 diff/追加；加载 300–600 KB ≈ 10–20 ms；无跨文件查找 | 单文件较大，人工翻阅不便 |
| B. 按 MAIN 模组分片 `shards/<MODULE>.jsonl` + `index.json` | 单文件小、按模组可增量更新 | 打开文件要先查 `index.json` 再定位分片；分片数量 × 文件句柄；仍要一次加载 |
| C. 按程序代码哈希分片 | 均匀 | 完全不可读，无收益 |

**建议采用 A**，理由：

1. 采集是**全量导出**（Oracle 查询一次拿全表），不存在"只更新某模组"的场景，分片的增量优势用不上。
2. 1500 条链路的内存索引本来就要常驻，分片只是把同一个读放大成多次小读，没有净收益。
3. 单文件便于用户用 Excel/VSCode 直接打开核对、便于 `git diff` 看本次变化。

若用户仍希望按模组浏览，用**导出时拆分**满足，不影响运行时：

```bash
bdl-lsp link import links.csv --split-by-module
# 额外生成 shards/<MODULE>.jsonl 仅供人看，运行时不读
```

**目录设计**：

```text
<workspace>/.bdl-lsp/link/
├── links.jsonl        # 主数据（运行时只读这个）
├── links.meta.json    # schema 版本、生成时间、来源（db/csv/command）、统计、过滤行数
└── shards/            # 可选：仅供人读的分模组视图
    ├── AXM.jsonl
    └── _orphan.jsonl  # 无 MAIN 的组
```

`links.jsonl` 每行一条链路：

```json
{"schema":1,"program":"axmt620","module":"AXM","main":"axmt620","ambiguous":false,
 "files":[{"module":"ALM","code":"s_point"},{"module":"APM","code":"sapmp500"},
          {"module":"APM","code":"sapmt420_sub"}]}
```

`links.meta.json`：

```json
{"schema":1,"generatedAt":"2026-10-21T10:00:00+08:00","source":"db",
 "sourceDetail":"DS.GAL_FILE@10.0.0.5:1521/ORCL","rowsTotal":15000,"rowsFilteredN":1200,
 "chains":1500,"orphans":40,"ambiguous":2,"toolVersion":"0.7.0"}
```

**为什么不用数据库缓存/单文件二进制**：约 0.5 MB JSON 读取与解析完全够快，可读性带来的排错收益远大于性能收益。真要变慢（例如明细涨到 50 万行）再换 JSONL 流式读取或 gob，接口不变。

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

### 5.5 索引与性能

| 步骤 | 成本（实测/估算） | 策略 |
|---|---|---|
| 读 `links.jsonl` + 建内存索引 | 10–20 ms（300–600 KB） | 首次用到时加载，之后常驻 |
| 解析一个链路的全部文件 | 40–60 文件 × 2–5 ms ≈ **100–300 ms** | **后台异步**，不阻塞 didOpen |
| 隐含模组（sub+lib+qry ≈ 980 文件） | 2–4 s | **只建函数名索引**（不建完整符号），后台一次性 |
| 后续增量 | 单文件 mtime+size 变化重解析 | 复用 `globals` 的缓存模式 |

关键设计：**诊断不等待链接索引**。首轮诊断只跑"快检查"（语法、字段、变量来源），链接相关诊断在后台索引完成后**补发一次**。避免打开大文件时卡顿。

缓存落盘（可选，二期）：`<workspace>/.bdl-lsp/cache/link-symbols.json`，以 `(path, mtime, size)` 为键。首版可以只做内存缓存——后台 200ms 重建可接受。

### 5.6 采集：三种方式

统一接口，三种实现都产出同一份 `links.jsonl`：

```go
// internal/link/source
type Source interface {
    Name() string
    Fetch(ctx context.Context) ([]byte, Stat, error)   // 返回 CSV/JSONL 原始内容
}
```

**S1. 数据库直连（进程内，推荐）**

- 驱动：`github.com/sijms/go-ora/v2`（**纯 Go，无需 Oracle Instant Client**）——正好满足用户"oracle 驱动不需要下载，配置路径和登录方式"。
  已实测该模块可获取（`go list -m -versions` 成功，最新 `v2.9.0`）。
- 代价：二进制体积 +2–3 MB。可用 build tag `ora` 做可选编译，默认包含。
- 配置：

```jsonc
"bdl.link.sync": {
  "enable": true,
  "source": "db",
  "interval": 21600,                 // 秒；0 = 不自动同步
  "onStart": true,                   // 启动时跑一次
  "db": {
    "host": "10.0.0.5", "port": 1521, "service": "ORCL",
    "user": "readonly_user",
    "password": "${env:BDL_ORA_PASSWORD}",   // 不明文落盘
    "query": "SELECT gal01,gal02,gal03 FROM DS.GAL_FILE WHERE gal04='Y'"  // 可选覆盖
  }
}
```

- 密码来源优先级：系统环境变量 > 密钥文件（`.bdl-lsp/secrets.json`，权限校验 0600，已在 `.gitignore`）> 交互输入（首次）。
- 定时：LSP 进程内 goroutine，`interval` 到了跑一次；失败指数退避（1×/2×/4×，上限 1h），连续失败 3 次后降级为只记日志不再提醒。
- 主动运行：`bdl-lsp link sync --source db`（CLI）+ VSCode 命令「Genero BDL: 同步链接资料」+ Zed `.zed/tasks.json`。

**S2. 服务器脚本导出（零依赖）**

- 用户在 RedHat 服务器上用现成的 `sqlplus` 跑脚本，产出 CSV，拷到工作区。
- 提供 `scripts/fetch-gal-file.sh`：交互读账号密码（`read -s`，不落 shell history），`SET MARKUP CSV ON`，直出 CSV。
- 插件侧只需 `bdl-lsp link import <file.csv>`，无需任何 Oracle 库。
- 适用：生产库不允许直连、或客户端装不了驱动的场景。**这是最稳的兜底**。

**S3. 命令执行（半直连）**

- 配置里只给"命令/可执行位置"（如 `sqlplus` 或自写导出程序），凭证在**运行到需要时**交互输入。
- 执行方式：`exec.Command`，账号密码**走 stdin**（不进命令行参数，避免 `ps` 泄露），输出重定向到工作区。
- 与 S1 的差别：不链接 Oracle 库、不常驻连接；与 S2 的差别：由插件驱动、凭证不入脚本。
- Zed 无法弹密码框 → 交给 `.zed/tasks.json` 终端任务（终端里 `read -s` 交互）。

三种方式共用：**导入校验 + 原子写入 + 报告**。

### 5.7 语义规则与诊断

链路可用后，函数解析的可见性顺序（新增一级）：

```text
局部作用域 → 本文件 → GLOBALS 引入 → 链接文件（新增）→ 隐含模组（新增）
```

变量解析顺序**不变**（R2）：

```text
局部作用域 → 本文件 → GLOBALS 引入          ← 到此为止，不跨链接
```

新增诊断：

| 码 | 含义 | 默认 | 说明 |
|---|---|---|---|
| `function-not-found` | 调用的函数在本文件/GLOBALS/链接/隐含模组中都找不到 | warn | 无链接资料时**不报** |
| `link-file-missing` | 链接资料里的程序代码在磁盘上找不到 | warn | 指向上游数据问题 |
| `link-data-invalid` | 链接资料 schema/字段不合法 | warn | 导入时也进报告 |
| `link-data-stale` | 资料超过 `maxAgeDays` 未更新 | hint | 温和提示，不骚扰 |

用户明确要求：

> 为定义或引入就是使用了未定义变量，应该警告。

→ `variable-source-unresolved` 的语义**保持严格**（不放宽到链接），只按 P7-B 修掉调用点误报。

### 5.8 验收

- [ ] `bdl-lsp link import` 对合成样例（axmt620 那 4 行）产出正确链表，`gal04='N'` 的行不出现。
- [ ] `cmd/link-audit`：用真实资料（待用户提供）跑全量，报告 组数/孤儿/missing 文件占比。
- [ ] 打开 `axmt620.4gl`：`cl_*` 类跨文件函数不再报未定义；`GLOBALS` 变量行为不变。
- [ ] R2 反例测试：两个链接在一起的子文件，各自 `DEFINE` 同名变量但没有共享 GLOBALS → 仍报未定义（**确保没有把变量也放进链接可见性**）。
- [ ] 性能：打开一个 50 文件链路的可执行文件，首轮诊断 < 300 ms（链接索引在后台）。

---

## 6. P7-E：链接资料的用户界面

### 6.1 一个必须先说清的限制

用户要求："outline 最下方增加链接资料栏……点击可以跳转到 apm/4gl/sapmp500.4gl"。

**LSP 的 `textDocument/documentSymbol` 无法跨文件跳转**：`DocumentSymbol` 没有 URI/target 字段，它的 `range` 只能落在本文档内；能带跨文件位置的是扁平的 `SymbolInformation`，而它不支持层级。两者在同一个响应里不能混用。Zed 的 `outline.scm` 同理——它只能引用当前语法树。

所以"层级 + 跨文件跳转"在纯 outline 里做不到，必须用编辑器各自的视图能力。

### 6.2 推荐方案：VSCode 自定义 TreeView + Zed 降级

| 能力 | VSCode | Zed |
|---|---|---|
| 三级层级 | ✅ TreeView | ✅ `documentSymbol` 追加（同一文档内） |
| 点击跳转 | ✅ `command: vscode.open` | ❌ 只能定位到本文档末尾 |
| 位置 | Explorer 侧栏独立视图「链接资料」 | 大纲末尾 |

**VSCode TreeView**（满足全部要求）：

```text
链接资料                    ← 视图根（名称 + 可折叠）
└── axmt620                 ← L1：程序名
    ├── s_point   ALM       ← L2：直链子文件
    ├── sapmp500  APM
    ├── sub                 ← L2：隐含模组
    │   └── cs_asf          ← L3：模组下的文件
    └── lib
        └── cl_null
```

- 数据来源：新增 LSP 自定义请求 `bdl/linkTree`（或 `workspace/executeCommand` → `bdl.link.tree`），返回 JSON 树。
- 点击文件节点：`vscode.open` 到 `apm/4gl/sapmp500.4gl`，带 `selection` 定位到函数定义。
- 显示格式按用户要求：`APM/apmp500`；模组节点显示 `sub`、`lib`。

**Zed 降级**：把链路追加成 `documentSymbol` 的末尾若干条目（L1/L2/L3 层级靠 `children` 表达），**能看到但不能跳转**。在 Zed README 与文档中明确写这一限制，避免用户误以为是 BUG。

若后续 Zed 提供自定义视图 API，再补跳转。

### 6.3 验收

- [ ] VSCode：打开含 MAIN 的文件，「链接资料」视图出现三级结构，点击 `APM/apmp500` 跳到 `apm/4gl/sapmp500.4gl`。
- [ ] VSCode：打开不含 MAIN 的子文件 → 视图显示"反查：被哪些程序链接"或隐藏（见 Q13）。
- [ ] Zed：大纲末尾出现链接条目，层级正确（不跳转）。
- [ ] 无链接资料时视图显示引导文案（"未找到链接资料，运行『同步链接资料』"），不报错。

---

## 7. 新增配置项汇总

```jsonc
// 诊断级别（P7-B）
"bdl.diagnostics": { "*": "warning", "syntax-error": "error" }

// 内置符号（P7-C）
"bdl.builtins.enable": true

// 链接资料（P7-D）
"bdl.link.enable": true,
"bdl.link.dataDir": ".bdl-lsp/link",          // 相对工作区根
"bdl.link.moduleRoots": ["tiptop", "topcust"], // 空 = 自动探测
"bdl.link.implicitModules": ["sub", "lib", "qry"],
"bdl.link.maxAgeDays": 7,                      // 0 = 不提示过期
"bdl.link.sync": {
  "enable": false, "source": "db", "interval": 21600, "onStart": true,
  "db": { "host": "", "port": 1521, "service": "", "user": "",
          "password": "${env:BDL_ORA_PASSWORD}" },
  "command": { "path": "/usr/bin/sqlplus", "args": [], "passwordViaStdin": true }
}
```

VSCode `package.json` 需同步新增（当前 32 项 → 约 42 项），并跑 `make manifest-check`。

---

## 8. 待确认决策（需用户拍板）

| # | 问题 | 备选 | 建议 |
|---|---|---|---|
| **Q10** | 诊断默认级别是否真的"全部 warning"？ | (a) 全 warning (b) 语法保持 error、其余 warning (c) 维持现状 | **(b)**：语法错误本质是错误；`record-member-unknown` 维持 `off` 直到有 schema |
| **Q11** | 链接资料存储：单文件还是按模组分片？ | A 单文件 `links.jsonl` / B 分模组 `shards/` | **A**，另提供 `--split-by-module` 导出人读视图 |
| **Q12** | 链接资料放哪个仓库？ | (a) 工作区 `.bdl-lsp/link/`（运行时数据）(b) `bdl-lsp` 仓库 | **(a)**：数据随项目变化且可能含内部信息；`bdl-lsp` 只放 schema/样例/脚本 |
| **Q13** | 打开**子文件**（无 MAIN）时链接栏显示什么？ | (a) 反查"被哪些程序链接" (b) 隐藏视图 (c) 显示所属全部程序 | **(a)**：反查对排查"这个函数谁能用"最有用 |
| **Q14** | 特殊模组（sub/lib/qry，约 980 文件）的函数是否参与 `function-not-found` 判定？ | (a) 全量建函数名索引（后台 2–4 s）(b) 只在本文件打开时按需 (c) 不参与 | **(a)**，但只建函数名不建完整符号；否则 `cl_*` 仍会大面积误报 |
| **Q15** | 数据库直连是否默认编译进二进制？ | (a) 默认包含 (+2–3 MB) (b) build tag 可选 | **(a)**：体验优先，符合"无感"偏好 |

---

## 9. 风险

| 风险 | 影响 | 对策 |
|---|---|---|
| Windows URI 根因判断有偏差 | P7-A 修了但没解决 | 上机第一步先打印两个客户端的 `initialize` 入参对比，再动手 |
| 链接资料尚未拿到真实数据 | P7-D 只能合成验证 | schema + 合成样例先行；真实数据到位后跑 `cmd/link-audit` 复评 |
| `expression name:` 跳过规则误伤 | 漏报函数未定义 | 先跑语料对比 skip 前后的差异清单，人工抽查 |
| 内置表不全导致误报残留 | 噪声仍高 | `cmd/builtin-audit` 语料驱动补齐，设 <20 条未知的门槛 |
| 隐含模组 980 文件索引太慢 | 打开文件卡顿 | 只建函数名索引 + 后台异步 + 持久化缓存 |
| Zed 无法跳转链接文件 | 体验差异 | 文档明示 + VSCode 侧做全功能；Zed 等 API |
| Oracle 直连引入大依赖 | 二进制膨胀、构建变慢 | 纯 Go 驱动；必要时 build tag 分离 |
| 链接资料含生产库信息 | 泄露 | 默认 gitignore `.bdl-lsp/`；文档写明不要提交 |

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

---

## 11. 文档地图（本文相关）

| 文档 | 内容 |
|---|---|
| [HANDOVER.md](HANDOVER.md) | 会话接手：状态、基线、坑、下一步 |
| [TODO.md](TODO.md) | 待办清单（§下一步：P7） |
| [PLAN.md](PLAN.md) | 总规划；§7.8 链接资料设计、§8 P7 里程碑 |
| [FEATURES.md](FEATURES.md) | 功能基线编号（P7 新增 35–40） |
| [PLAN-P3A-GLOBALS.md](PLAN-P3A-GLOBALS.md) | GLOBALS 解析规则（P7-D 复用其缓存与路径模式） |
| [PLAN-P6-COMPLETION.md](PLAN-P6-COMPLETION.md) | 补全（P7-D 要接函数来源） |
