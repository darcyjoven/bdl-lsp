# P6 记录：代码补全

> 版本：v1.0（已实现）  
> 日期：2026-10-21  
> 状态：**已完成**（验收输出见 §5）。  
> 上游：[粗略计划](PLAN.md) §7.8｜[P5 记录](PLAN-P5-DEPLOY.md)｜[待办](TODO.md)

---

## 1. 交付内容

```text
internal/completion/            # 新包（纯逻辑，无 server 依赖）
├── completion.go               # Provide/SplitWord、上下文判断、候选组装
├── completion_test.go          # 15 个单测
└── keywords_gen.go             # 生成的清单（BDL 361 / PER 76）

internal/form/check.go          # 重构 + 新增
├── collectEvents / makeContainerOf   # 从 Check 抽出，供 FormFieldsAt 复用
└── FormFieldsAt                # 光标处的表单字段（与 Check 同一套作用域跟踪）

internal/server/complete.go     # 新文件
├── handleCompletion            # 组装候选
├── handleCompletionResolve     # 原样返回（声明能力用）
├── formFieldsAt                # 接 FormFieldsAt
├── crossFileSymbols            # 其它已打开文档的函数/报表
├── linePrefixAt                # 光标前行前缀
└── contextAt                   # 注释/字符串判定（语法树，非词法）

internal/server/complete_test.go  # 11 个协议级测试
scripts/generate-keywords.mjs     # 从 grammar 抽关键字 → keywords_gen.go
Makefile                          # keywords / keywords-check；lint 依赖 keywords-check
```

## 2. 候选来源与优先级

| 优先级 | 来源 | SortText 前缀 | 说明 |
|---|---|---|---|
| 1 | 表单字段 | `0` | 光标处当前表单的 item（P3b 解析器） |
| 2 | 局部变量/参数/字段 | `1` | 当前文件 |
| 3 | 常量/定义 | `2` | 当前文件 |
| 4 | 函数/报表/TYPE/记录 | `3`–`4` | 当前文件 |
| 5 | 跨文件符号 | `5` | 其它已打开文档 |
| 6 | 关键字 | `8` | 来自 grammar |

SortText 用"数字前缀 + 名字"实现**分档排序**：同档内按名字，跨档按来源。
这样同名时符号一定排在关键字前面（用户打 `DISPLAY` 想要自己的
`DISPLAY_REC` 函数，而不是关键字）。

## 3. 关键设计决策

### 3.1 关键字清单必须从 grammar 生成

手抄 300+ 关键字必然与 grammar 漂移。`scripts/generate-keywords.mjs` 复用
`generate-tm-grammars.mjs` 的 `kw()` 抽取逻辑，保证**高亮与补全用同一份清单**：

```text
tree-sitter-bdl/rule/*.js  ──kw()──▶  keywords_gen.go  ──▶  补全
tree-sitter-bdl/rule/*.js  ─────────▶  bdl.tmLanguage.json ──▶ 高亮
```

`make lint` 依赖 `keywords-check`：改了 grammar 却忘了重新生成，
CI 直接失败。这条防线很关键——没有它，漂移只会在用户抱怨
"新语法没补全"时才被发现。

### 3.2 表单字段补全复用字段校验的解析器

```go
fields, _ := s.formRes.FormFieldsAt(tree, text, form.PathOfURI(doc.URI), s.formQs, offset)
```

补全与校验共用 `Resolver` 的**同一套作用域跟踪**（按函数隔离 + 按语句
顺序推进）。收益：永远不会出现"补全给了这个字段，但保存时校验报
field-not-in-form"这种自相矛盾。

为此把 `Check` 里的两段逻辑抽出来共用：
- `collectEvents`：一次 query 扫描收集捕获与函数容器；
- `makeContainerOf`：字节偏移 → 容器索引的二分查找。

`FormFieldsAt` 与 `Check` 的唯一区别是**遇到光标之后的绑定就停止**——
补全要看的是"此刻"的表单，而不是整文件跑完的最终状态。

### 3.3 `.` 之后不给关键字

`. ` 之后用户明确要成员名。此时候选只保留表单字段与"可能是成员"的符号
（变量/常量/字段/参数），关键字一个都不给。

### 3.4 注释/字符串里不补全

用语法树判定而不是手写词法：

```go
node := root.DescendantForByteRange(offset, offset)
for n := node; n != nil; n = n.Parent() {
    switch n.Kind() {
    case "comment": return true, false
    case "string":  return false, true
    }
}
```

手写词法会在 `"a -- b"`（字符串里有 `--`）和 `-- it's`（注释里有引号）
上判断错误。沿祖先链向上找是必需的：光标在字符串中间时常落到更小的子节点。

### 3.5 不做类型推导

Genero 弱类型 + 大量运行时绑定（`WITH FORM 变量`、动态 SQL、`DEFINE ... LIKE`），
推导成员类型的收益远低于复杂度。当前做法是"多给"：记录成员用同名变量/字段
符号近似，宁可多列几个，也不会漏掉正确的那个。

### 3.6 只看已打开文档，不做全工作区索引

`crossFileSymbols` 遍历 `s.docs.All()`。全工作区索引需要后台任务 +
持久化缓存（PLAN §7.6 的 15k 文件目标），而补全里多几个"项目常用函数"
的收益有限。GLOBALS 变量已由 P3a 的跨文件索引覆盖，这是真正跨文件可见的部分。

## 4. 前缀处理的坑

```go
// 正确：不裁剪行尾空白
func SplitWord(line string) (word string, afterDot bool) {
    end := len(line)
    start := end
    for start > 0 && isIdentByte(line[start-1]) { start-- }
    return line[start:end], start > 0 && line[start-1] == '.'
}
```

最初实现先 `TrimRight(line, " \t")` 再取词。后果：`IF ` 之后（用户已经
打完关键字、准备写新词）当前词被算成 `"IF"`，补全只给 IF 开头的关键字，
等于什么都没给。**行尾空白意味着"开始新词"，必须保留这个信息。**

Genero 标识符字符比一般语言多：`_`、`$`、`#` 都合法（如 `p_text#`）。
只按 `unicode.IsLetter` 判断会把 `#` 切掉。

## 5. 验收输出

```text
$ go test ./internal/completion/ -v
    --- PASS: TestSplitWord
    --- PASS: TestProvideKeywords
    --- PASS: TestProvideKeywordCaseInsensitive
    --- PASS: TestProvidePERKeywords
    --- PASS: TestProvideFileSymbols
    --- PASS: TestProvideSymbolBeforeKeyword
    --- PASS: TestProvideAfterDotOnlyFields
    --- PASS: TestProvideSkipsComment
    --- PASS: TestProvideSkipsString
    --- PASS: TestProvideDedup
    --- PASS: TestProvideMaxItems
    --- PASS: TestProvideIncompleteOnlyForEmptyPrefix
    --- PASS: TestKeywordsAreSorted
    --- PASS: TestKeywordCountsSane
    --- PASS: TestSymbolKindMapping
ok

$ go test ./internal/server/ -run TestCompletion -v
    --- PASS: TestCompletionCapability          # 声明了 `.` 触发字符
    --- PASS: TestCompletionKeywords            # 空前缀给关键字，kind=14
    --- PASS: TestCompletionPrefixFilters       # 所有候选都匹配前缀
    --- PASS: TestCompletionLocalSymbol         # 同文件函数
    --- PASS: TestCompletionAfterDotNoKeywords  # `.` 之后无关键字
    --- PASS: TestCompletionInComment           # 注释里为空
    --- PASS: TestCompletionInString            # 字符串里为空
    --- PASS: TestCompletionPERKeywords         # .per 只给 per 关键字
    --- PASS: TestCompletionResolve             # 原样返回
    --- PASS: TestCompletionFormFields          # 表单字段 f01/f02
    --- PASS: TestCompletionFormFieldsBeforeBinding  # 绑定之前不给字段
ok

$ node scripts/generate-keywords.mjs
已生成 internal/completion/keywords_gen.go（BDL 361 个，PER 76 个）

$ make keywords-check          # 篡改清单后
关键字清单已过期：internal/completion/keywords_gen.go
请运行 node scripts/generate-keywords.mjs
make: *** [Makefile:91: keywords-check] Error 1
```

## 6. 踩坑记录

| 现象 | 根因 | 修法 |
|---|---|---|
| `IF ` 之后补全只给 IF 开头的关键字 | `SplitWord` 先裁剪了行尾空白，把上一个词当成当前词 | 不裁剪；行尾空白本身就表示"开始新词" |
| `p_text#` 的 `#` 被切掉 | 只按字母数字判断标识符 | 加上 `_` `$` `#` |
| 关键字清单与 grammar 漂移 | 手抄清单无法自动跟随 grammar | 生成脚本 + `make lint` 校验 |
| 补全给的字段校验却报不存在 | 两处各自实现作用域跟踪 | 抽出 `collectEvents`/`makeContainerOf`，补全复用 `FormFieldsAt` |
| 字符串里的 `--` 被当成注释 | 手写词法按行判断 | 用语法树 `DescendantForByteRange` + 祖先链 |

## 7. 已知限制

| 限制 | 影响 | 计划 |
|---|---|---|
| 不做类型推导 | 记录成员补全靠"同名近似"，可能多给 | 设计取舍，不做 |
| 无全工作区符号索引 | 只提示已打开文档里的函数 | 需后台索引（PLAN §7.6） |
| 表单字段不区分 READONLY / PHANTOM | 只读字段也会出现在候选里 | 影响小，未排期 |
| 不做代码片段（带参数占位符） | 补函数名不自动填参数 | 未排期 |
