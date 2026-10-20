# P4 记录：4GL 格式化

> 版本：v1.0（已实现）  
> 日期：2026-10-21  
> 状态：**已完成**（验收输出见 §5）。  
> 上游：[粗略计划](PLAN.md) §7.3｜[P3b 记录](PLAN-P3B-FIELD.md)｜[待办](TODO.md)  
> 来源：用户原始需求「.per 不要进行任何格式化，.4gl 增加配置项：关键字格式化为
> 大写/小写/不变，缩进可选择 tab 或者空格，也允许选择个数」

---

## 1. 交付内容

```text
internal/format/                # 新包（无 server 依赖，可单测）
├── format.go                   # Format(tree, src, opts) (string, bool)
└── format_test.go              # 18 个单测

internal/server/format.go       # 新文件
├── handleFormatting             # 处理 textDocument/formatting
└── resolveIndentUnit            # 配置 > 编辑器请求 > 4 空格 的优先级

internal/config/config.go       # FormatSettings{Keywords, IndentStyle, IndentSize}
internal/server/server.go       # 注册 documentFormattingProvider 能力 + 方法分发

cmd/fmt-audit/                  # 语料评估工具（幂等性 + 新增 ERROR 计数）
├── main.go                     # 全量遍历 / -limit N
└── debug.go                    # FMT_DEBUG=<file> 单文件调试

vscode-extension/
├── package.json                # bdl.format.keywords / indentStyle / indentSize
├── src/client.ts               # collectSettings 下发 format 段
└── test/suite/index.js         # 2 个 e2e 用例（格式化、.per 拒绝）

zed-extension/README.md         # format.* 配置说明
```

## 2. 用户需求 → 实现映射

| 用户原话 | 实现 |
|---|---|
| 「.per 不要进行任何格式化」 | `handleFormatting` 对 `LangPer` **直接返回 nil**；不做任何尝试性格式化 |
| 「关键字格式化为大写/小写/不变」 | `format.keywords` = `unchanged`（默认）/ `upper` / `lower` |
| 「缩进可选择 tab 或者空格」 | `format.indentStyle` = `auto`（默认，跟随编辑器）/ `tab` / `space` |
| 「也允许选择个数」 | `format.indentSize` = `0`（默认，跟随编辑器）… `16` |

## 3. 设计决策

### 3.1 只做两件事

格式化**只**做：关键字大小写、行首缩进。**不动**空白、标点、运算符、换行，
不做行合并/拆分。

理由：Genero 语料里大量手工对齐的 SQL 与记录声明，一旦重排会产出全量 diff，
review 成本远大于收益。用户需求本身也只要求这两项。

### 3.2 关键字改写只碰匿名 token

```go
if n.IsNamed() {
    continue // 标识符、字面量、注释一律不动
}
```

Tree-sitter 里关键字是**匿名**节点，`named` 节点是标识符/字面量/注释。
这条规则保证 `define l_name string` 只会变成 `DEFINE l_name STRING`，
`l_name` 与 `"string"` 不会被误改。

### 3.3 `.per` 请求返回空而不是报错

返回 `nil` 的 `TextEdit` 数组，编辑器因此认为"没有需要改的地方"，
不会弹出错误提示，也不会走编辑器自带的 fallback 格式化器
（VSCode 对没有 provider 的语言才会 fallback）。这是把
「`.per` 不要格式化」落到实处的关键——如果注册了 provider 却返回
错误，编辑器可能提示失败；如果不注册，编辑器可能用别的 provider。

### 3.4 缩进宽度的三级优先级

```text
显式配置 format.indentStyle/indentSize
  > 编辑器请求的 insertSpaces / tabSize
    > 4 空格兜底
```

`auto` + `IndentSize=0`（默认）时完全跟随编辑器——VSCode 用户改
`editor.tabSize` 即生效，Zed 用户改 `tab_size` 即生效，无需改插件配置。
需要团队统一风格时再显式固定。

### 3.5 Format on save

扩展**不**替用户强制打开 `editor.formatOnSave`——那是用户自己的编辑习惯。
扩展只通过 `contributes.configurationDefaults` 把 `[bdl].editor.defaultFormatter`
固定为本扩展，避免用户装了别的格式化插件后被抢走。用户想保存即格式化，
自己打开 `editor.formatOnSave` 即可；Zed 侧同理用 `format_on_save`。
`.per` 不设默认格式化器，保持"绝不格式化表单"。

## 4. 缩进算法

每行独立计算深度：

```text
depth = |{ 祖先链上的块节点 b : b.startLine < 本行 }|   （按 startLine 去重）
if 行首 token ∈ {END, ELSE, CATCH}: depth -= 1
indent = indentUnit × max(depth, 0)
```

块节点集合（`blockNodes`）：`if` / `while` / `for` / `foreach` / `try` /
`case` / `when` / `otherwise` / `function` / `main` / `report` / `input` /
`display` / `menu` / `construct` / `dialog` / `prompt` / `record` /
`globals` / `begin` / `branch` / `report_*_section` / `define`。

### 4.1 为什么按「行」不按「语句」

按语句需要知道每条语句在原文的哪一行结束，而 Genero 里一条语句可以跨任意多行
（`DEFINE` 变量列表、SQL、记录声明）。按行算深度只需要祖先链 + 行号比较，
天然支持任意跨行，且每行独立、天然幂等。

### 4.2 同一行开多个块只算一级（回归用例）

`DEFINE l_rec RECORD` 同时开启 `define` 与 `record` 两个块。若按祖先**个数**
累加，记录体每行会多缩进一级：

```text
错误：  DEFINE l_rec RECORD
              id INTEGER,      ← 6 空格（应为 3）
```

修正：祖先链按 `startLine` 去重，每个起始行最多贡献一级。

### 4.3 多行块注释内部不动

```go
if node.Kind() == "comment" && lineOfByte(lineStarts, node.StartByte()) < line {
    continue
}
```

块注释 `/* ... */` 内部的 `*` 手工对齐是作者刻意排版，重排会破坏注释的
可读性（典型的 ASCII 表格）。单行注释 `--` 后面没有内容要排，不受影响。

## 5. 验收输出

```text
$ go test ./internal/format/ -v
    --- PASS: TestIndentSpaces
    --- PASS: TestIndentTabs
    --- PASS: TestIndentTwoSpaces
    --- PASS: TestKeywordUpper
    --- PASS: TestKeywordLower
    --- PASS: TestKeywordUnchanged           # 默认不动
    --- PASS: TestMixedCaseKeywordsNormalized
    --- PASS: TestBlankLinesPreserved
    --- PASS: TestBlockCommentInteriorUntouched
    --- PASS: TestErrorTreeRefusesFormatting # 含 ERROR → ok=false
    --- PASS: TestFunctionAndReport
    --- PASS: TestRecordAndGlobals
    --- PASS: TestSameLineBlocksCountOnce    # DEFINE x RECORD 不多缩进
    --- PASS: TestExistingIndentReplaced     # 修错而不是追加
    --- PASS: TestTryCatch
    --- PASS: TestNestedIfElse
    --- PASS: TestNoIndentUnitNoEdits
    --- PASS: TestPreprocUntouched           # &INCLUDE 不被大写
ok

$ go test ./internal/server/ -run 'Formatting' -v
    --- PASS: TestFormatting4GL
    --- PASS: TestFormattingKeywordsUpper
    --- PASS: TestFormattingIndentConfigWins     # 配置压过编辑器请求
    --- PASS: TestFormattingIndentSizeConfig
    --- PASS: TestFormattingFollowsEditorTabSize # auto 时跟随编辑器
    --- PASS: TestFormattingPERRefused           # .per 返回空
    --- PASS: TestFormattingBrokenFileRefused
    --- PASS: TestFormattingIdempotentViaServer
ok

$ go run ./cmd/fmt-audit            # 全量 7699 个 .4gl，keywords=upper
文件总数        : 7699
格式化成功      : 7683
拒绝（语法错误）: 16
内容有变化      : 7656
幂等失败        : 0
新增 ERROR      : 0

$ make e2e-vscode
[e2e] 用例 20/20 通过
  ✓ 格式化关键字转大写并归一缩进 (477ms)
  ✓ per 文件拒绝格式化 (77ms)
```

## 6. 踩坑记录（全部写进代码注释）

| 现象 | 根因 | 修法 |
|---|---|---|
| `&INCLUDE "x"` 被大写后出现 4 个 ERROR | grammar 的外部扫描器对 `&include`/`&define` **大小写敏感** | 关键字改写跳过首字节是 `&` 的 token |
| `DEFINE l_rec RECORD` 的记录体多缩进一级 | `define` 与 `record` 两个块都在同一行开始，按祖先个数累加算了两次 | 祖先链按 `startLine` 去重 |
| 跨行 `DEFINE a, b, c` 的后续行丢失缩进 | `define` 不在 `blockNodes` 里，无块可依 | 把 `define` 加入块集合 |
| 全量审计报「新增 ERROR: 0」但实际有 5 个 | `countErrNodes` 是个返回 0 的空桩，审计形同虚设 | 用真实的 `*ts.Node` 递归替换 |

## 7. 已知限制

| 限制 | 影响 | 计划 |
|---|---|---|
| 不做空白/逗号/运算符规范化 | 手工对齐的 SQL 保持原样 | **设计如此**，不做 |
| 不做行合并/拆分 | 一行多条语句保持一行 | **设计如此**，不做 |
| 含语法错误的文件整体拒绝 | 编辑中途保存被跳过 | 设计如此（避免破坏半成品） |
| 缩进不感知续行（`LET a = 很长表达式` 的续行） | 续行按块深度缩进，不做悬挂缩进 | 未排期 |
| `.per` 不做任何格式化 | LAYOUT 需手工对齐 | **用户明确要求** |
