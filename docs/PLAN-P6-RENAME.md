# P6 记录：重命名

> 版本：v1.0（已实现）  
> 日期：2026-10-21  
> 状态：**已完成**（验收输出见 §4）。  
> 上游：[P6 补全记录](PLAN-P6-COMPLETION.md)｜[粗略计划](PLAN.md) §7.8｜[待办](TODO.md)

---

## 1. 交付内容

```text
internal/server/rename.go        # 新文件
├── handlePrepareRename          # 判断能否改名，返回范围 + 占位符
├── handleRename                 # 生成 WorkspaceEdit
├── renameBlockReason            # 不可改名的原因
├── validateNewName              # 校验新名字
├── hasSymbolNamed / isValidIdent
└── wordRangeAt                  # 光标所在词的精确范围

internal/server/rename_test.go   # 11 个协议级测试
internal/server/server.go        # 分发 + renameProvider 能力（prepareProvider: true）
```

## 2. 范围决策：只改当前文件

与 `textDocument/references` 保持一致——**只重命名当前文件内的出现**。

跨文件改名需要工作区级引用索引（PLAN §7.6 的 15k 文件目标）。在索引完成前
做"跨文件改名"会**漏改其他文件的引用**，那比不支持改名更糟：用户以为改完
了，实际留下编译不过的代码。

如果将来加了工作区索引，这里只需把 `findOccurrences` 换成跨文档版本。

## 3. 安全规则（宁可不改，不可改错）

`prepareRename` 与 `rename` 都走 `renameBlockReason`，六道闸门：

| # | 规则 | 拒绝原因 |
|---|---|---|
| 1 | 文件能解析 | 文件无法解析 |
| 2 | 不在注释/字符串里 | 光标位于注释或字符串内 |
| 3 | **必须是本文件声明的符号** | `%q 不是本文件声明的符号，不能改名` |
| 4 | 内侧（描述性） | 是关键字（若该词恰好是关键字） |
| 5 | 新名字合法 | 非法标识符（字符集/首字符） |
| 6 | 新名字不撞名 | 与文件内已有符号同名 |
| 7 | 新名字不是关键字 | 是 BDL 关键字 |

### 3.1 检查顺序的坑

最初把"是关键字就拒绝"放在"是否声明符号"**之前**，导致：

```genero
DEFINE first INTEGER    -- FIRST 是 BDL 关键字（ORDER BY FIRST）
```

这样的变量**无法改名**——因为它"看起来像关键字"。但 Genero 允许关键字
作标识符（在非歧义位置），用户声明的 `first` 是合法符号。

修正：先判断"是否本文件声明的符号"，再对**未声明**的词区分原因
（关键字 / 内建函数 / 拼错）。

### 3.2 为什么 prepareRename 要报错而不是返回 null

客户端拿到 `prepareRename` 的错误会**不弹输入框**；返回 `null` 有的客户端
会退化成"能改但什么都不改"。用户输完新名字才发现没生效，比一开始就说
"这里不能改"差得多。

## 4. 验收输出

```text
$ go test ./internal/server/ -run 'TestRename|TestPrepareRename' -v
    --- PASS: TestRenameCapability            # 声明 renameProvider + prepareProvider
    --- PASS: TestRenameFunction              # 1 定义 + 2 调用 = 3 处
    --- PASS: TestRenameSkipsCommentAndString # 只改定义 1 处
    --- PASS: TestRenameRejectsKeyword        # 改成 DISPLAY 被拒
    --- PASS: TestRenameRejectsInvalidIdent   # 1abc / has space / a-b / 空
    --- PASS: TestRenameRejectsDuplicate      # 改成已有符号名被拒
    --- PASS: TestPrepareRenameOK             # 返回 {range, placeholder}
    --- PASS: TestPrepareRenameOnKeyword      # 关键字上拒绝
    --- PASS: TestPrepareRenameInComment      # 注释里拒绝
    --- PASS: TestRenameUnknownSymbol         # INFIELD（内建）拒绝
    --- PASS: TestRenameUndeclaredWord        # 未声明词拒绝，理由可读
ok

$ make e2e-vscode
[e2e] 用例 27/27 通过
  ✓ 重命名函数同时改调用 (58ms)
  ✓ 重命名拒绝关键字 (12ms)
```

## 5. 踩坑记录

| 现象 | 根因 | 修法 |
|---|---|---|
| 名为 `first` 的变量无法改名 | 关键字检查在"已声明符号"检查之前 | 调整顺序：先判符号，再对未声明词细分原因 |
| `prepareRename` 返回类型不存在 | `go.lsp.dev/protocol v0.12` 没有 `PrepareRenameResult` | 返回 `map[string]any{"range":…,"placeholder":…}`，序列化即符合规范 |
| `const uri := "..."` 编译失败 | Go 的 `const` 必须用 `=`，不能用 `:=` | 改为 `const uri = ...` |

## 6. 已知限制

| 限制 | 影响 | 计划 |
|---|---|---|
| 只改当前文件 | 跨文件引用不会同步改 | 需工作区索引（PLAN §7.6） |
| 不检查新名字是否与**其他文件**符号冲突 | 改名后可能与其他文件的全局符号重名 | 同上 |
| 不改表单字段与 `.per` 的对应关系 | 改 4gl 里的字段名不会改 `.per` | 未排期（跨文件语义） |
| 不做"重命名预览/选中全部" | 依赖编辑器自身 UI | 客户端职责 |
| 关键字清单来自 grammar，可能偏保守 | 极少数合法标识符被判为关键字 | 可点击帮助信息覆盖；（当前无覆盖入口） |
