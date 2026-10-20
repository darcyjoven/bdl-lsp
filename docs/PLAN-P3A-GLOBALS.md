# P3a 规划：GLOBALS 模组全局变量 —— 跨文件解析、跳转与诊断

> 版本：v1.0（规划，未实现）  
> 日期：2026-10-20  
> 状态：**待实现**。P2（Zed 扩展）已完成，本功能是下一步。  
> 上游：[粗略计划](PLAN.md)｜[P1/P2 详细记录](PLAN-DETAILED-P1.md)｜[待办](TODO.md)  
> 来源：**用户明确要求**（2026-10-20，原话摘录见 §1.1）

---

## 1. 需求与语义

### 1.1 用户原话

> global 语法会引入一些比全局变量还高一层的模组全局变量，所有引入相同 global
> 文件的都可以使用这些变量。如果是 global 变量，当前 4gl 没有标记其来源，
> 也没有实现跳转到 global 文件，如果 global 文件没有找到，要警告，且变量
> 来源没有找到的也要警告，变量来源找不到警告做成配置项目，允许关闭。

拆成可验收的功能点：

| # | 功能点 | 类别 |
|---|---|---|
| F1 | `.4gl` 里的 GLOBALS 引入变量可**跳转到定义**（定义在 `.global` 文件里） | 导航 |
| F2 | 悬停等处**标注变量来源**（来自哪个 global 文件） | 信息展示 |
| F3 | `GLOBALS "路径"` 的目标文件**找不到时要警告** | 诊断 |
| F4 | 变量**来源找不到时要警告**（用了变量但任何作用域都查不到定义） | 诊断 |
| F5 | F4 的警告做成**配置项，允许关闭** | 配置 |

### 1.2 Genero 作用域层级

```text
局部（函数内 DEFINE）
  ↓ 查找顺序自上而下
参数（函数参数）
模块级（模块顶层 DEFINE，仅本模块可见）
GLOBALS 引入（模组全局变量，所有引入同一文件的模块共享）   ← 本功能
```

“比全局变量还高一层的模组全局变量”：模块级 DEFINE 只在本模块可见；
GLOBALS 引入的变量跨模块共享（凡 `GLOBALS "同一个文件"` 的模块都可见）。

### 1.3 两种语法形式（grammar 已支持，实测见 §2）

**定义侧**——`GLOBALS ... END GLOBALS` 块，通常在 `.global` 文件里：

```4gl
DATABASE ds                          -- 模块级语句，不共享
GLOBALS
   DEFINE g_aaz  RECORD LIKE aaz_file.*
   DEFINE g_rep  LIKE type_file.num5
END GLOBALS
```

只有**块内**的声明是模组全局变量；块外的模块级 DEFINE 不共享。
（tiptop 语料中 57 个 `.global` 文件全部把声明放在块内，该差异实际影响小，
但实现必须区分——用独立的 Scope 而不是复用 ScopeGlobal。）

**引用侧**——`GLOBALS "相对路径"`：

```4gl
GLOBALS "../../config/top.global"
```

路径基准是**引用文件所在目录**（不是工作区根、不是 cwd）。语料证据：

| 引用文件 | 字面量 | 解析结果 |
|---|---|---|
| `tiptop/amm/4gl/x.4gl` | `../../config/top.global` | `tiptop/config/top.global` ✅ 存在 |
| `topcust/csf/4gl/x.4gl` | `../../../tiptop/config/top.global` | `tiptop/config/top.global` ✅ 存在 |
| `tiptop/rest/axd/4gl/x.4gl` | `../../config/top.global` | `tiptop/rest/config/top.global` ❌ 不存在 |

第三行正是 §2 语料里 97 处未解析的来源（目录层级不同的模块树），
也是 §5.4 `searchPaths` 配置的动机。

---

## 2. 现状与证据

### 2.1 代码现状（缺口）

| 位置 | 现状 | 影响 |
|---|---|---|
| `internal/index/index.go` `visitTop` | `case "globals", ...: // 其余顶层结构暂不建符号` | **GLOBALS 块内容完全不索引**。实测：块形式样例 0 个符号；真实 `top.global`（176 个 DEFINE）也是 0 个符号 |
| `internal/server/navigation.go` | 解析顺序只有 局部→参数→模块级 三级 | 引入的变量跳转不到定义 |
| `internal/server` 整体 | 只索引**已打开**的文档 | `.global` 文件不在编辑器里打开时完全不可见 |
| `internal/config` | 只有 `syntaxDiagnostics` / `missingDiagnostics` | 无本功能所需配置项 |
| `internal/server/diagnostics.go` | 诊断无 `relatedInformation` | F3/F4 想关联“引用处↔目标文件”需要小改（与 P3b 字段校验共用） |

### 2.2 语料证据（tiptop-gp，规划依据）

| 统计项 | 数值 | 对设计的影响 |
|---|---|---|
| `.4gl` 总数 | 7699 | — |
| 引用 GLOBALS 文件的 `.4gl` | **7582（98%）** | 几乎每个文件都触发解析，必须快且带缓存 |
| 每文件引用条数 | 1 条占 89%，最多 5 条 | 按需解析开销可控 |
| 引用目标扩展名 | `.global` 8009、`.4gl` 422、`.inc` 79、`.global1` 1 | **解析必须与扩展名无关**（不能只认 `.global`） |
| `.global` 文件数 | 57，全部为块形式定义 | 规模小，适合缓存 |
| `.global` 链式引用 | 0 个 | 不需递归，但实现仍做深度上限（防环） |
| 未解析引用 | 107 处 / 10 个目标 | F3 警告的真实触发量；97 处为 `../../config/top.global` |
| 同名 `top.global` | 2 个（`tiptop/config` 与 `topcust/config`） | **禁止隐式按文件名匹配**，否则会解析到错误的文件 |
| `GLOBALS` 行尾带注释 | 存在（`GLOBALS #FUN-AC0060 ...`） | 提取导入要走**语法树字段**，不做行正则 |
| `#GLOBALS ...`（被注释） | 15 处 | grammar 不会解析 ✓（实测确认），无需处理 |

补充实测：

- `bdl-lsp check` 已能解析 `.global` 文件（`LangForPath` 已含该扩展名），0 语法问题；
- 文件名可以是**变量**（grammar：`filename: choice(variable, string)`）——
  静态无法解析，需降级处理（见 §5.3 规则 R3）。

---

## 3. 总体方案

```text
┌─────────────────────────────────────────────────────────────┐
│ internal/index                                               │
│  · GLOBALS 块内容 → 新增 ScopeGlobals 符号（定义侧）         │
│  · 新增 GlobalsImport{Literal, 位置}（引用侧，从语法树提取） │
└──────────────┬──────────────────────────────────────────────┘
               │
┌──────────────▼──────────────────────────────────────────────┐
│ internal/globals（新包）                                      │
│  · Resolver：字面量 → 绝对路径（基准=引用文件目录，           │
│    附加 searchPaths）→ 读盘 → 解析 → FileIndex 缓存          │
│  · 缓存按 mtime+size 失效；文件已打开时优先用内存版本        │
│  · 反向依赖表：global 路径 → 引用它的 URI 集合               │
└──────────────┬──────────────────────────────────────────────┘
               │
┌──────────────▼──────────────────────────────────────────────┐
│ internal/server                                              │
│  · didOpen/didChange：提取导入 → 解析 → 合并诊断发布         │
│  · handleDefinition：四级作用域（新增 GLOBALS 级）           │
│  · handleHover：命中 globals 来源时追加“来源：…”            │
│  · .global 文件变更 → 刷新所有引用方的诊断                   │
└─────────────────────────────────────────────────────────────┘
```

---

## 4. 数据模型

### 4.1 `internal/index` 扩展

```go
// 新增作用域：GLOBALS ... END GLOBALS 块内的声明（定义侧）。
// 与 ScopeGlobal（模块级）区分：后者不跨文件共享。
const ScopeGlobals Scope = "globals-block"

// GlobalsImport 是一条 GLOBALS "file" 引用。
type GlobalsImport struct {
    Literal    string // 去引号后的路径字面量
    StartByte  uint   // 字面量（含引号）区间，诊断定位用
    EndByte    uint
    IsVariable bool   // 文件名是变量而非字符串字面量
}

// FileIndex 新增：
//   GlobalsImports []GlobalsImport  —— 引用侧列表（按出现顺序）
//   GlobalBlockSyms []*Symbol       —— 块内符号（ScopeGlobals），便于整块取用
```

`visitTop` 的 `case "globals"` 改为：

1. 块形式：遍历 `constant` / `type` / `define` 子节点，
   用现有 `collectDefines` / `makeTypeDecl` 等以 **ScopeGlobals** 收集；
2. 文件形式：读 `filename` 字段，字符串则剥引号记入 `GlobalsImports`，
   变量则记 `IsVariable=true`。

### 4.2 `internal/globals`（新包）

```go
type Entry struct {           // 一条导入的解析结果
    Literal   string          // 原始字面量
    Path      string          // 解析出的绝对路径（NotFound/Unresolvable 时为空）
    Status    Status          // Resolved / NotFound / Unresolvable
    Index     *index.FileIndex // Resolved 时的符号表
    URI       string          // 文件 URI
}

type Status int
const (
    Resolved EntryStatus = iota  // 找到且成功解析
    NotFound                      // 目标文件不存在 → F3 警告
    Unresolvable                  // 文件名是变量，静态无法解析 → 静默降级
)

type Resolver struct { ... }
// Resolve(docURI string, imports []index.GlobalsImport) []Entry
//   基准 = filepath.Dir(docPath)；再依次尝试 searchPaths（相对工作区根）
// Refresh(path string)  // mtime 失效检测 + 重读
// Importers(path string) []string  // 反向依赖
```

解析一条导入的规则（**按序**）：

| # | 规则 | 结果 |
|---|---|---|
| R1 | `IsVariable=true` | `Unresolvable`，只记 debug 日志，**不发诊断**（编译期才知值） |
| R2 | `Join(引用文件目录, 字面量)` 存在 | `Resolved` |
| R3 | 依次尝试 `Join(searchPath, 字面量)`（searchPath 相对工作区根） | 第一个存在的 → `Resolved` |
| R4 | 都不存在 | `NotFound` → F3 警告 |

**禁止**：按 basename 在工作区里隐式搜索（§2.2：两个同名 `top.global`
内容不同，隐式匹配会解析到错误文件）。

**环与深度**：解析 `.global` 文件时如其中还有文件引用（语料中为 0，但语法
允许），递归解析，深度上限 8，超过记日志并停止。

### 4.3 缓存与一致性

| 场景 | 行为 |
|---|---|
| 首次解析某 `.global` | 读盘 → 解析 → 缓存（key = 规范化绝对路径） |
| 再次解析 | mtime+size 都没变 → 用缓存；变了 → 重读重解析 |
| `.global` 已在编辑器打开 | **优先用内存版本**（`document.Store` 里的文本） |
| `.global` 被 didChange | 更新内存索引 → 通过反向依赖表**刷新所有引用方**的诊断 |
| `.global` 被 didClose | 回退用磁盘版本 |
| Windows 路径 | 规范化时统一小写比较（大小写不敏感文件系统）；字面量里的 `/` 交给 `filepath.Join` 处理 |

---

## 5. 功能设计

### 5.1 跳转（F1）

`handleDefinition` 的解析顺序从三级扩为**四级**：

```text
1) rec.field 形式（记录字段）
2) 局部 DEFINE / 3) 参数
4) 模块级 DEFINE / CONSTANT / TYPE / FUNCTION ...
5) GLOBALS 引入（新增）：按导入顺序查每个 Resolved 的 .global 索引
```

- 命中返回**跨文件** `Location`（URI 指向 `.global` 文件，编辑器会自动打开）；
- 同名多命中（如模块级与 globals 重名、或多个导入文件都有）→ 全部返回，
  与现有“同名多命中全部返回，交给用户选择”的策略一致；
- 只对**变量类符号**（define/constant/type/record）生效，
  函数/报表不走 globals 级（`.global` 文件里不放函数，语料 57 个文件均无）。

### 5.2 悬停来源标注（F2）

命中 globals 来源时，悬停内容追加一行：

```text
g_aaz — RECORD LIKE aaz_file.*
来源：tiptop/config/top.global（GLOBALS 引入，第 6 行）
```

- “第 N 行”按定义处换算；
- 多个导入文件都定义了同名变量时全部列出；
- 模块级/局部命中时**不**显示来源行（避免噪音）。
  （现状悬停只显示类型与定义位置；本功能只在 globals 命中时加一行。）

### 5.3 诊断（F3 / F4）

| code | 触发 | 默认级别 | relatedInformation |
|---|---|---|---|
| `globals-file-not-found` | 导入解析为 `NotFound`（R4） | warning | 指向 `GLOBALS "..."` 字面量 |
| `variable-source-unresolved` | 变量使用处查不到任何定义 | warning（**可 off**） | 指向变量使用处 |

`variable-source-unresolved` 的两种消息措辞（同一 code，便于统一过滤）：

```text
情形 A（所有导入都 Resolved）：
  无法确定变量 “g_xxx” 的来源：局部、模块级与 GLOBALS 引入中均无定义
情形 B（存在未解析的导入）：
  无法确定变量 “g_xxx” 的来源：GLOBALS “../../config/top.global” 未找到
```

**F4 的误报控制**（这是本功能最大的风险点，策略必须保守）：

| 规则 | 说明 |
|---|---|
| 只查 `variable` 节点的**首标识符** | 不查 `rec.field` 的字段侧、不查函数调用名、不查标签 |
| 排除 `RECORD LIKE table.*` 记录的字段 | 表列来自数据库 schema，静态无法判定（P6 做 schema 索引前一律放过） |
| 排除 grammar 已识别的 builtin | 与现有索引的 builtin 判定一致 |
| 排除所有 `Unresolvable` 导入无法核对的场景 | 情形 B 的措辞已区分“查不到”与“没法查” |
| 标注驱动调优 | `scripts/globals-audit.sh` 对 50 个模块输出全部告警，人工标注误报，目标 **< 5%**（与 P3b 字段校验同一标准） |

> 宁可不报，不误报——这是项目一贯原则（PLAN.md §2.3）。
> 因此 F4 的检查范围比“所有标识符”窄得多；宁可漏掉存疑场景。

### 5.4 配置（F5）

```jsonc
{
  "bdl": {
    "globals": {
      "fileNotFoundDiagnostics": "warning",       // off/error/warning/information/hint
      "unresolvedSourceDiagnostics": "warning",   // 同上；用户要求“允许关闭”→ off
      "searchPaths": []                            // 附加解析根，相对工作区根，如 ["tiptop/config"]
    }
  }
}
```

| 项 | 默认 | 说明 |
|---|---|---|
| `fileNotFoundDiagnostics` | `warning` | F3 的级别；沿用现有 `DiagnosticLevel` 枚举（含 off），与 `syntaxDiagnostics` 一致 |
| `unresolvedSourceDiagnostics` | `warning` | F4 的级别；**off 即完全关闭**（用户明确要求） |
| `searchPaths` | `[]` | 解决 §1.3 第三行的目录层级问题：配置后 `../../config/top.global` 这类“在本模块树内解析不到、但工作区里有”的引用可命中 |

落地同步点：

- `internal/config`：`Settings.Globals` 结构 + `Default`/`merge`/`Validate`；
- VSCode `package.json` 的 `contributes.configuration`（10 项配置变 13 项）；
- Zed README 的 `lsp.bdl-lsp.settings` 示例；
- 工作区文件 `.bdl-lsp/config.json` 天然支持（走同一 `Settings` 反序列化）。

---

## 6. 任务拆分（每步可独立验收）

### T1 索引 GLOBALS 块（定义侧）

- `index.Build` 支持块形式；新增 `ScopeGlobals`；
- 块外模块级 DEFINE 仍为 `ScopeGlobal`（语义区分）；
- `GlobalsImport` 提取（含 `IsVariable`）。
- **验收**：
  - 单测：块形式样例产出正确数量/Scope 的符号；
  - `top.global` 索引出 176 个符号（其中 43 个 record）；
  - `go test ./internal/index/` 全绿；`make regress` 通过率不降。

### T2 Resolver 与路径解析

- 新包 `internal/globals`；R1–R4 规则；缓存 + mtime 失效；深度上限。
- **验收**：
  - 单测（临时目录构造）：相对路径、searchPaths 命中、NotFound、
    IsVariable 降级、mtime 变化后重读；
  - 对 tiptop 语料跑一遍统计：8404 处 Resolved / 107 处 NotFound
    （与 §2.2 脚本数字对得上）。

### T3 诊断接线（F3 + 反向依赖）

- didOpen/didChange 提取导入 → 解析 → `globals-file-not-found` 发布；
- 诊断结构支持 `relatedInformation`（与 P3b 共用的前置改造）；
- 反向依赖：编辑 `.global` 刷新引用方。
- **验收**：协议级测试（`server_test.go` 风格）：缺文件出 warning、
  补上文件后消失（增量同步）、级别改 off 后不出现（配置热更新）。

### T4 跳转与悬停（F1 + F2）

- `handleDefinition` 四级作用域；
- `handleHover` 来源行。
- **验收**：协议级测试：`CALL`/`DISPLAY` 里的 globals 变量跳到 `.global`
  文件内位置（跨文件 Location 的 URI 断言）；悬停含“来源：”行。

### T5 变量来源校验（F4）

- 按 §5.3 的保守规则实现；
- `scripts/globals-audit.sh`：对 50 个模块输出告警清单供人工标注。
- **验收**：误报 < 5%（人工标注）；配置 off 后完全静默。

### T6 配置项与两端客户端同步

- `internal/config` 三项 + 校验；
- VSCode `package.json` 配置声明 + `verify-vscode-manifest.mjs` 扩展校验；
- Zed README 示例；
- e2e 新增用例：“GLOBALS 变量跳转到 global 文件”、“缺文件出现警告”。
- **验收**：`make manifest-check`、`make e2e-vscode`（用例 14 → 16）全绿。

依赖顺序：T1 → T2 → T3 → T4 → T5 → T6；T3 的 relatedInformation 改造
与 T4 可并行。

---

## 7. 测试计划汇总

| 层 | 内容 |
|---|---|
| 单元（index） | 块形式索引数量/Scope；块外不入 globals；GlobalsImport 提取（含变量名、行尾注释样例） |
| 单元（globals） | R1–R4；searchPaths；缓存失效；深度上限；Windows 路径规范化 |
| 单元（config） | 新增三项的默认值、merge、Validate |
| 协议级（server） | F3 诊断出现/消失/关闭；F1 跨文件 Location；F2 悬停来源；.global 编辑后引用方刷新 |
| e2e（VSCode） | 真实宿主里跳转进 `.global` 文件；缺文件警告可见 |
| 语料回归 | `make regress` 不降；globals-audit 误报 < 5% |

---

## 8. 风险与对策

| 风险 | 对策 |
|---|---|
| F4 误报率高（rec.* 链、动态 SQL、窗口/表单名） | 保守规则 + 标注脚本调优；用户可 off；必要时先降默认为 information 再升 |
| 大工作区冷启动慢 | 按需解析（只在文档 didOpen 时解析其导入），不做全盘扫描；57 个 `.global` 的语料规模下缓存后无压力 |
| 同名 `.global` 解析歧义 | 禁止 basename 隐式匹配（§2.2 证据）；只走 R2/R3 显式规则 |
| `.global` 内存版与磁盘版不一致 | 打开优先内存版 + 反向依赖刷新（§4.3） |
| Windows 大小写/分隔符 | 路径规范化统一处理；CI 的 Windows job 覆盖 |
| 用户目录层级差异导致大量 F3 告警 | `searchPaths` 配置（默认空 = 不启用）；文档给出 tiptop 场景的示例 |

---

## 9. 非目标（本轮明确不做）

| 项 | 原因 | 去处 |
|---|---|---|
| 跨文件 references（查某 global 变量被哪些模块引用） | 需要全工作区扫描，收益/成本比低 | P6（配合工作区索引增强） |
| 导入的 globals 变量进文档大纲 | 大纲噪音大（top.global 有 176 个） | P6 可选（折叠分组） |
| `RECORD LIKE table.*` 字段校验 | 需 schema 索引 | P6（既有计划） |
| 工作区全量预扫描 | 按需解析已满足本功能 | 与 P3b 共同评估 |
| 变量名形式的 GLOBALS 文件解析 | 静态不可知 | 永久降级（R1） |

---

## 10. 验收标准（DoD）

1. §1.1 的 F1–F5 全部有对应的自动化测试且通过；
2. tiptop 语料：8404 处引用解析正确、107 处未解析全部产生 `globals-file-not-found`；
3. `make test-race` / `make lint` / `make regress` / `make e2e-vscode` 全绿；
4. globals-audit 人工标注误报 < 5%；
5. 配置项在 VSCode 与 Zed 两端都有文档与示例；
6. 本文档状态改为“已完成”，并在 [PLAN-DETAILED-P1.md](PLAN-DETAILED-P1.md)
   补 §7C 记录（踩坑、验收输出、复现命令）。
