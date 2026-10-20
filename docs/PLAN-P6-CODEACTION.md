# P6 记录：Code Action（快速修复）

> 版本：v1.0（已实现）  
> 日期：2026-10-21  
> 状态：**已完成**（验收输出见 §4）。  
> 上游：[P6 重命名记录](PLAN-P6-RENAME.md)｜[粗略计划](PLAN.md) §7.8｜[待办](TODO.md)

---

## 1. 交付内容

```text
internal/server/codeaction.go    # 新文件
├── handleCodeAction             # 按诊断码分派
├── actionsForDiagnostic         # field-not-in-form / .per 两个码
├── didYouMeanActions            # "改为 %q" 的 quickfix（首个 preferred）
├── closestMatches               # 按编辑距离挑候选，超阈值丢弃
├── editDistance                 # Levenshtein（单行滚动 DP）
├── maxSuggestDistance           # 阈值：len/3，夹在 1..4
├── fieldCandidatesAt            # 复用 P3b 的 FormFieldsAt
└── perItemNames                 # .per 已定义 item

internal/server/codeaction_test.go  # 10 个测试（6 协议级 + 4 单测）
internal/server/server.go           # 分发 + codeActionProvider（只声明 quickfix）
```

## 2. 唯一原则：只做安全的修复

判断标准：**一个修复如果在多数情况下会生成语义/语法错误的代码，就不做。**

一键自动修复产生的坏代码比看到报错更糟——报错会让人思考，自动修复会让人信任。

### 2.1 做了的

| 诊断码 | 修复 | 为什么安全 |
|---|---|---|
| `field-not-in-form` | 改为「最接近的表单字段」 | 候选来自**该光标处真实打开的表单**（P3b 解析器），编辑距离阈值内才提 |
| `form-layout-tag-undefined` | 改为「最接近的已定义 item」 | 候选来自同文件的 item 列表 |
| `form-screen-record-undefined` | 同上 | 同上 |

### 2.2 刻意不做的（附理由）

| 想法 | 为什么不做 |
|---|---|
| 「在表单里自动添加该 item」 | LAYOUT 列宽无法推断；ATTRIBUTES 里该写 `formonly.x` 还是 `schema.table.column` 取决于 SCHEMA，猜错就是语法/语义错误 |
| 「创建缺失的 .per 文件」 | 空表单没有任何 item，等于把编译错误换成运行时错误 |
| 删除/注释掉字段引用 | 破坏性操作，不该一键完成 |
| `form-duplicate-item` 的修复 | 删掉是破坏性的；合并语义不明（哪个是对的？） |

理由写进代码注释，避免以后被当成"遗漏"补上。

## 3. 关键实现细节

### 3.1 编辑距离阈值

```go
func maxSuggestDistance(word string) int {
    d := len(word) / 3
    if d < 1 { d = 1 }
    if d > 4 { d = 4 }
    return d
}
```

| 名字 | 阈值 | 效果 |
|---|---|---|
| `f11`（3） | 1 | 能建议 `f01`/`f02` |
| `good_filed`（10） | 3 | 能建议 `good_field`（距离 2） |
| `tot_amt`（7）→ `cust_name` | 2 | 距离 5，**不提建议**（避免噪声） |

阈值上限 4：再宽就会把不相关的短字段名互相推荐。

### 3.2 只处理本服务器发出的诊断

```go
if d.Source != "" && d.Source != "bdl-lsp" {
    continue
}
```

部署功能（P5）会把 `fglcomp` 的输出发布成诊断（source = `bdl-deploy`）。
那些诊断的码是编译器错误码，用字段建议去"修"它毫无意义。

### 3.3 只有首个动作标 preferred

```go
IsPreferred: i == 0,
```

多条同时标 `isPreferred` 会让编辑器的 auto-fix 无从选择（不同客户端行为不一致，
有的取第一个、有的干脆不执行）。明确只推荐最接近的那条。

### 3.4 大小写不同不算"拼错"

```go
if cl == lower { continue }
```

Genero 标识符大小写不敏感，`GOOD_FIELD` 与 `good_field` 是同一个字段——
编译器/校验根本不会报错，这里也就不该出现"改为 good_field"的动作。

## 4. 验收输出

```text
$ go test ./internal/server/ -run 'TestCodeAction' -v
    --- PASS: TestCodeActionCapability                    # 只声明 quickfix
    --- PASS: TestCodeActionFieldTypo                     # f11 → f01/f02
    --- PASS: TestCodeActionNoSuggestionForUnrelatedField # 毫不相似 → 无建议
    --- PASS: TestCodeActionIgnoresForeignSource          # bdl-deploy 诊断被忽略
    --- PASS: TestCodeActionNoDiagnostics                 # 无诊断 → 无动作
    --- PASS: TestCodeActionNoFixForDuplicateItem         # 重复 item 无修复
ok

$ go test ./internal/server/ -run 'TestEditDistance|TestMaxSuggest|TestClosestMatches' -v
    --- PASS: TestEditDistance          # 含经典用例 kitten→sitting = 3
    --- PASS: TestMaxSuggestDistance    # 1..4 区间
    --- PASS: TestClosestMatchesOrdering
    --- PASS: TestClosestMatchesLimit
ok

$ make e2e-vscode
[e2e] 用例 29/29 通过
  ✓ 字段拼错提供快速修复 (73ms)
  ✓ 无诊断时无快速修复 (11ms)
```

## 5. 踩坑记录

| 现象 | 根因 | 修法 |
|---|---|---|
| `tot_amt`→`cust_name` 距离断言成 8，实际 5 | 我心算按"只插入/删除"算，替换也是代价 1 | 用经典基准 `kitten`→`sitting`=3 验证 DP 实现，再按实测值写断言 |
| e2e 报 `Invalid argument 'kind' ... received {"value":"quickfix"}` | `vscode.executeCodeActionProvider` 的 kind 参数要**普通字符串**，传 `vscode.CodeActionKind.QuickFix` 对象会被拒 | 传 `"quickfix"` 字面量 |
| 第一版把 doc 包了层 `documentRef`，还引用了不存在的 `doc.full` | 过度抽象：直接用 `*document.Document`（已有 `Snapshot`/`Index`）就够 | 去掉包装层 |

## 6. 已知限制

| 限制 | 影响 | 计划 |
|---|---|---|
| 只支持 3 个诊断码 | 其他诊断没有修复 | 按需扩展（前提仍是"安全"） |
| 字段候选依赖光标处的表单绑定 | 光标在表单绑定之前时拿不到候选，不给建议 | 与补全一致，属预期 |
| 不提供批量修复（fix all） | 一个文件里多处拼错要逐个点 | 未排期 |
| 建议只按编辑距离 | 不考虑语义（如类型是否匹配） | 编辑距离对"拼错"场景够用 |
