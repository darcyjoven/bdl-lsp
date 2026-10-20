# P6 记录：语义高亮

> 版本：v1.0（LSP 侧已实现；Zed 侧靠 `highlights.scm`）  
> 日期：2026-10-21  
> 状态：**已完成**（验收输出见 §4）。  
> 上游：[P6 CodeAction 记录](PLAN-P6-CODEACTION.md)｜[粗略计划](PLAN.md) §7.8｜[待办](TODO.md)

---

## 1. 交付内容

```text
internal/server/semtok.go        # 新文件
├── handleSemanticTokensFull     # 处理 textDocument/semanticTokens/full
├── semanticTokensFor            # 从符号表生成 token + 相对编码
├── classifySymbol               # 符号种类 → (类型, 修饰位)
├── semanticTokenTypes/Modifiers # legend（顺序与常量绑定）
└── tokenLength                  # UTF-16 长度计算

internal/server/semtok_test.go   # 8 个协议级测试
internal/server/server.go        # 分发 + semanticTokensProvider 能力
```

## 2. 核心决策：只标注 TextMate 做不到的部分

**关键字、字符串、注释、数字一律不发。** 理由：

1. TextMate grammar 已经做得很准，而且和 tree-sitter grammar 同源
   （`scripts/generate-tm-grammars.mjs` 从同一份 `kw()` 抽取）；
2. 语义 token 再报一遍会与 TextMate **抢同一批 token**。客户端的合并
   规则不一致（有的语义优先、有的合成），结果可能比只用 TextMate 更差；
3. 数据量：7000 行文件光关键字就有几千个 token，纯属浪费带宽。

所以只发**用户自定义符号**——这才是 TextMate 永远猜不到的：

| 符号种类 | token 类型 | 修饰 |
|---|---|---|
| `function` / `main` / `report` | `function` | `declaration` |
| `define`（模块级/函数内） | `variable` | `declaration` |
| `define` 在 GLOBALS 块内 | `variable` | `declaration` + `static` |
| `constant` | `variable` | `declaration` + `readonly` |
| `type` / `record` / `screen_record` | `struct` | `declaration` |
| `field` / `form_item` | `property` | `declaration` |
| `parameter` | `parameter` | `declaration` |

效果：用户定义的 `show_name` 显示为函数色、`g_count` 显示为变量色，
再也不会和 `IF`/`DISPLAY` 混成一片。

## 3. 编码细节（容易出错的三个地方）

### 3.1 legend 顺序必须与常量一致

```go
const (
    tokTypeVariable = iota   // 0
    tokTypeParameter         // 1
    tokTypeFunction          // 2
    ...
)

func semanticTokenTypes() []protocol.SemanticTokenTypes {
    return []protocol.SemanticTokenTypes{
        protocol.SemanticTokenVariable,   // 必须与 tokTypeVariable 对齐
        ...
    }
}
```

客户端用**下标**解释 `data` 里的数字。这两处顺序一旦错位，所有 token 的
颜色都会错（而且不会有任何报错）——所以有单测断言 `tokenTypes[0] == "variable"`。

### 3.2 相对编码

LSP 要求 `data` 每 5 个数一组，且位置是**相对**的：

```text
[deltaLine, deltaChar, length, tokenType, tokenModifiers]
```

- `deltaLine == 0` 时，`deltaChar` 相对**上一个 token 的同一行**；
- `deltaLine > 0` 时，`deltaChar` 是**绝对列号**（相对于行首）。

写错这一点，token 会跑到行尾或者整篇偏移。有单测解码后断言严格升序。

### 3.3 length 是 UTF-16 码元，不是字节

```go
if r > 0xFFFF {
    length += 2   // 补充平面字符占 2 个码元
} else {
    length++
}
```

Genero 标识符通常纯 ASCII，但注释/字符串里可能有中文。虽然这里只标注
标识符，按规范算 UTF-16 是唯一正确的做法。

### 3.4 用 map 声明能力

```go
SemanticTokensProvider: map[string]any{
    "legend": map[string]any{...},
    "full":   true,
},
```

`go.lsp.dev/protocol v0.12` 的 `SemanticTokensOptions` 是**空壳**（只有
`WorkDoneProgressOptions`，没有 `legend`/`full` 字段），直接构造会编译失败。
用 map 表达，JSON 序列化结果符合规范。

## 4. 验收输出

```text
$ go test ./internal/server/ -run TestSemanticTokens -v
    --- PASS: TestSemanticTokensCapability      # legend 顺序 + full=true
    --- PASS: TestSemanticTokensFunction        # function + declaration，长度正确
    --- PASS: TestSemanticTokensConstantReadonly # CONSTANT 带 readonly
    --- PASS: TestSemanticTokensGlobalsStatic   # GLOBALS 块内带 static
    --- PASS: TestSemanticTokensSorted          # 严格按行/列升序
    --- PASS: TestSemanticTokensReliable        # 长度为正、类型不越界
    --- PASS: TestSemanticTokensEmptyDoc        # 空文档返回空而非报错
    --- PASS: TestSemanticTokensNoKeyword       # 不发 keyword token
ok

$ make e2e-vscode
[e2e] 用例 31/31 通过
  ✓ 语义高亮标注自定义符号 (42ms)
  ✓ 语义高亮不重复关键字 (35ms)
```

## 5. Zed 侧

Zed 的语义高亮走 tree-sitter `highlights.scm`，不消费 LSP semantic tokens。
因此：

- `.per` / `.4gl` 的高亮由 `zed-extension/languages/*/highlights.scm` 提供；
- 本服务器发的 semantic tokens 在 Zed 里**不会生效**（Zed 当前不支持
  LSP semanticTokens 用于文本着色）；
- 功能上不影响：Zed 的高亮已由 grammar 驱动，覆盖关键字/字符串/注释/数字。

两边的效果差异记录在 `zed-extension/README.md`「已知限制」。

## 6. 踩坑记录

| 现象 | 根因 | 修法 |
|---|---|---|
| `undefined: protocol.MethodTextDocumentSemanticTokensFull` | 常量名是 `MethodSemanticTokensFull`（无 `TextDocument` 前缀） | 改用正确常量 |
| `unknown field Legend in SemanticTokensOptions` | 该版本的 `SemanticTokensOptions` 是空壳 | 能力用 `map[string]any` 表达 |
| `const uri :=` 编译失败 | Go 的 `const` 必须用 `=` | 已在校验脚本外单独修正（这个错误在本轮出现两次，值得注意） |

## 7. 已知限制

| 限制 | 影响 | 计划 |
|---|---|---|
| 不发关键字/字符串/注释 token | 依赖 TextMate 提供这些高亮 | 设计如此（避免抢 token） |
| 只发定义处，不发使用处 | 变量使用处仍是 TextMate 的默认色 | 未排期（需要全文件符号解析，成本高收益低） |
| 不支持增量（`full/delta`） | 每次全量重算 | 7000 行约 3ms，不需要增量 |
| Zed 不消费 LSP semantic tokens | Zed 侧看不到语义着色 | 平台限制，文档说明 |
| `.per` 的 token 较少 | 只有 item/screen record 等 | 符合预期（表单本身符号就少） |
