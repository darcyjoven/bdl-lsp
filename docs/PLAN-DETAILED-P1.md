# P1 详细计划：LSP 骨架 + 解析诊断

> 版本：v0.7（Block 6 完成）  
> 日期：2026-10-20  
> 状态：**Block 1–6 已完成**；Block 7 待做。  
> 上游：[粗略计划](PLAN.md)｜[功能清单](FEATURES.md)｜[环境](ENVIRONMENT.md)

---

## 0. 与 v0.1 的差异

v0.1 是纸面拆分；本轮实际落地 Block 1 后，以下内容按真实代码调整：

| 项 | v0.1 设想 | 实际 |
|---|---|---|
| document 同步 | Block 1 就要增量 | Block 1 整文替换；**Block 2 已改为真增量** |
| `check` 子命令 | 未规划 | **新增**，用于 CI/语料冒烟，价值很高 |
| grammar 依赖 | 准备用 tag | 暂用 `replace ../tree-sitter-bdl`、`../tree-sitter-per`（grammar 无 tag） |
| 测试 | 只测 parser | parser + document + textpos + **协议级 server 测试** |
| 二进制大小 | < 10 MB | 实测 **9.3 MB** |
| 全量解析 | 1 MB < 300 ms | 329 KB / 7138 行 **51 ms**（didOpen 往返） |
| 增量编辑 | < 100 ms | **0.7 ms**（didChange 往返，快 70 倍） |

---

## 1. 已完成：Block 1

### 1.1 交付内容

```text
bdl-lsp/
├── main.go                        # serve / check / version 子命令
├── go.mod / go.sum                # go-tree-sitter v0.25.0；grammar 用 replace
├── Makefile                       # build/test/test-race/vet/lint/check/corpus
├── internal/
│   ├── cmdline/cmdline.go         # --log-level / --log-file
│   ├── logging/logging.go         # slog → stderr（+ 可选文件）
│   ├── parser/parser.go           # 池 + 双 grammar + ABI 校验
│   ├── parser/issues.go           # ERROR/MISSING → Issue
│   ├── parser/parser_test.go      # 8 个测试 + 2 个 benchmark
│   ├── document/document.go       # Store / Document / LineIndex
│   ├── document/document_test.go  # 5 个测试
│   ├── check/check.go             # check 子命令（文本 / --json）
│   ├── server/server.go           # JSON-RPC 分发、生命周期、stdio
│   ├── server/diagnostics.go      # Issue → Diagnostic（UTF-16 换算）
│   └── server/server_test.go      # 6 个协议级测试
├── testdata/
│   ├── sample.4gl                 # 干净样例
│   ├── sample.per                 # 干净样例
│   └── broken.4gl                 # 故意语法错误
├── scripts/corpus-smoke.sh        # 语料抽样冒烟
└── artifacts/                     # Git 忽略；含嵌套 go.mod 隔离缓存
```

### 1.2 已实现能力

| 能力 | 状态 | 证据 |
|---|---|---|
| `initialize` 握手 | ✅ | 返回 `textDocumentSync=2`、`definitionProvider` 等 |
| `initialized` / `shutdown` / `exit` | ✅ | shutdown 先回 result，exit 关连接 |
| 未知方法 | ✅ | 请求回 `-32601`，通知忽略 |
| `didOpen` | ✅ | 解析并发布诊断 |
| `didChange` | ✅（整文替换） | 增量编辑待 Block 2 |
| `didClose` | ✅ | 清空诊断、释放语法树 |
| 双语言解析 | ✅ | `.4gl/.global` → bdl，`.per` → per |
| ERROR/MISSING 诊断 | ✅ | 上限 200 条/文件，防诊断风暴 |
| UTF-16 位置换算 | ✅ | 补充平面字符按 2 码元 |
| 日志 | ✅ | 仅 stderr，stdout 留给协议 |
| `check` CLI | ✅ | 支持 `--json`、`-q`、`--max-issues` |

### 1.3 验收结果

```text
$ make lint      → 格式检查通过 + vet 无输出
$ make test      → 3 个包全部 ok
$ go test -race  → 3 个包全部 ok（约 1s）
$ make check     → sample.4gl OK / sample.per OK
$ bdl-lsp check testdata/broken.4gl → 2 个问题，退出码 1
$ bash scripts/corpus-smoke.sh 60   → per 60/60、bdl 60/60 无错误
$ 最大语料 332 KB / 7138 行        → 55 ms（含进程启动）
$ BenchmarkParseBDL                → 23.4 µs/op
$ BenchmarkParsePer                → 24.2 µs/op
$ 二进制大小                       → 9.3 MB
```

`initialize` 实际响应：

```json
{"result":{"capabilities":{
  "textDocumentSync":2,
  "hoverProvider":true,
  "definitionProvider":true,
  "referencesProvider":true,
  "documentSymbolProvider":true
},"serverInfo":{"name":"bdl-lsp","version":"0.1.0-dev"}},"id":1}
```

> 注意：`hoverProvider` 等能力已**声明**，但对应 handler 在 Block 3/4 才实现。
> 在实现前，客户端发起这些请求会收到 `MethodNotFound`。这是 Block 1 的已知状态。

---

## 2. 已完成：Block 2 增量同步

### 2.1 交付内容

| 文件 | 内容 |
|---|---|
| `internal/textpos/textpos.go` | 行索引；UTF-16 列 ↔ 字节偏移 ↔ tree-sitter Point 的双向换算 |
| `internal/document/document.go` | `Change` 结构、`ApplyChanges`（文本 + `Tree.Edit`）、Store |
| `internal/server/documents.go` | didChange 处理、`range` 是否存在的判定、多段编辑 |
| `internal/server/diagnostics.go` | 用 `textpos.Index` 做位置换算；旧树生命周期修正 |
| `internal/parser/bench_test.go` | 全量 vs 增量基准 |
| `scripts/lsp-bench.py` | 端到端延迟基准（`make bench`） |

### 2.2 关键技术点

1. **`range` 字段的“是否存在”**  
   `go.lsp.dev/protocol v0.12.0` 把 `Range` 定义为**值类型**，无法区分
   “省略 range（整文替换）”与“range 全零（文件开头插入）”。
   因此 didChange 改用 `json.RawMessage` 重新解码，用 `*rawRange` 判空。

2. **UTF-16 ↔ 字节列**  
   编辑器用 UTF-16 码元计列（中文 1 列、emoji 2 列），tree-sitter 用字节列。
   `textpos.Offset` 反向换算时，若落在代理对中间，钳制到字符起点，保证位置单调。

3. **`Tree.Edit` 的坐标必须与旧文本一致**  
   顺序为：先用**旧**索引算出 `start/oldEnd/newEnd` 的字节与 `Point`，再改文本，
   最后 `Tree.Edit`。顺序颠倒会导致树位置漂移。

4. **旧树生命周期**  
   最初实现里 `reparse` 关闭了 oldTree，随后 `doc.SetTree` 又关闭一次，
   导致 `ts_tree_delete` **双重释放 → SIGSEGV**。
   改为：`SetTree` 独占负责关闭旧树，调用方不再单独关闭。

5. **`exit` 后进程不退**  
   stdio 模式下 `conn.Close()` 不会真正关闭 `os.Stdin`，读循环仍阻塞。
   改为 `ServeStream` 同时监听 `s.exited` 通道，并在退出前给读循环 200 ms 收尾。

6. **LSP 退出码**  
   规范要求：未先收到 `shutdown` 就收到 `exit`，进程应以非零码退出。
   用 `exitError` 携带退出码，`main` 通过 `errors.As` 取出。

### 2.3 验收结果

```text
$ make test                 → 4 个包全部 ok（36 个测试）
$ go test -race ./...       → 全部 ok
$ make bench                → 7138 行 / 329 KB
                              didOpen  → 诊断:  51.2 ms
                              didChange 往返:   0.7 ms（10 轮均值）
$ go test -bench='ParseFull|ParseIncremental' ./internal/parser/
    BenchmarkParseFull-8          15.9 ms/op    4.63 MB/s
    BenchmarkParseIncremental-8    0.19 ms/op  380.17 MB/s   ← 快 82 倍
```

边界用例（全部通过）：

| 场景 | 结果 |
|---|---|
| 空文件 | 无崩溃 |
| 纯注释 / 纯换行 | 无崩溃 |
| 中文 + emoji | 位置换算正确，无误报 |
| 100 KB 超长单行 | 正常解析 |
| CRLF 换行 | 无错误 |
| 越界编辑区间 | 钳制到行末，不崩溃 |
| 关闭后再 didChange | 静默忽略 |
| 未 shutdown 就 exit | 退出码 1 |
| shutdown + exit | 退出码 0 |

## 3. 已完成：Block 3 符号与悬停

### 3.1 交付内容

| 文件 | 内容 |
|---|---|
| `internal/index/index.go` | BDL 符号提取：main/function/report/define/constant/type/record/字段/参数 |
| `internal/index/per.go` | PER 符号提取：ATTRIBUTES item、PHANTOM、SCREEN RECORD 及其字段 |
| `internal/server/symbols.go` | `documentSymbol`、`workspace/symbol`、`hover` 三个 handler |
| `internal/server/idents.go` | 索引缓存（按 URI+版本失效）、光标取词 |
| `internal/index/index_test.go` | 10 个测试 |
| `internal/index/per_test.go` | 5 个测试 |
| `internal/server/server_test.go` | 新增 8 个符号/悬停测试 |

### 3.2 符号模型

```go
type Symbol struct {
    Name, Lower string          // Lower 用于大小写不敏感匹配
    Kind        Kind            // main/function/report/define/constant/type/record/field/parameter/form_item/screen_record
    Scope       Scope           // global / func / param
    Parent      string          // 所属记录/函数
    Container   string
    StartByte, EndByte uint     // 名字本身（精确跳转/高亮）
    DeclStart, DeclEnd uint     // 整个声明（大纲范围）
    TypeText    string          // "LIKE type_file.num5" / "EDIT"
    Signature   string          // "FUNCTION helper(p_val)"
    Children    []*Symbol       // 记录字段 / 函数参数与局部变量
}
```

`.per` 的 item 名取 `tag` 字段（`EDIT f1 = ...` 中的 `f1`），而不是右侧绑定列名——
4GL 的 `AFTER FIELD f1` 用的是 item 名，这一点在 P3 字段校验中至关重要。

### 3.3 实现要点

1. **`ChildrenByFieldName` 需要 cursor**  
   go-tree-sitter v0.25 的该方法签名是 `(fieldName string, cursor *TreeCursor) []Node`，
   不接受 `nil`。builder 持有一个复用 cursor，封装成 `fieldChildren` 返回 `[]*ts.Node`。

2. **符号重复添加**  
   `collectDefines` 内部已把符号写入 `fi.Symbols`，外层 `addDefines` 又追加一次，
   导致每个 DEFINE 出现两次。改为 `collectDefines` 负责 `Symbols`、
   `addDefines` 只负责 `TopLevel`。

3. **`TYPE ... RECORD` 要识别为记录**  
   `TYPE t_rec RECORD a, b END RECORD` 的节点是 `type`，其 `types` 字段下才是 `record`。
   需要下探一层，否则字段不会进入 `Children`。

4. **光标取词不依赖语法树**  
   最初用 `NamedDescendantForByteRange` 上溯找 `identifier`，实测：
   - 光标停在标识符边界时命中父节点；
   - `field_ref` / `object` 包裹层次差异大；
   - `DescendantForByteRange` 返回 `source_file`（根节点）。

   改为直接从文本按标识符字符向两侧扩展（`wordAt`），对注释/字符串内的词也有效，
   行为稳定可预期。

5. **RECORD 的 detail 要截断**  
   真实文件的 RECORD 类型文本夹带大量行注释，直接放进 `DocumentSymbol.Detail`
   会撑爆大纲。改为显示 `RECORD (n 字段)`，其他类型截断到 80 字符。

6. **索引缓存**  
   符号提取要遍历全部节点，大文件（7000+ 行）约 1 ms，但高频请求仍需缓存。
   按 `(URI, version)` 失效；`didClose` 时主动清理。

### 3.4 验收结果

```text
$ go test ./internal/index/   → 15 个测试通过
$ go test ./internal/server/  → 22 个测试通过（新增 8 个）
$ go test -race ./...         → 全部通过
```

真实文件 `ammr110.4gl`（7138 行）：

```text
documentSymbol: 10 顶层, 36 总计, 1.1 ms
  tm       RECORD (7 字段)
  g_sql    STRING
  main     MAIN
  r110_tm  FUNCTION r110_tm(p_row, p_col)
  r110     FUNCTION r110()
workspace/symbol: 10 个（仅全局符号，局部变量被过滤）
hover(FUNCTION r110_tm) → "FUNCTION r110_tm(p_row, p_col)"
```

`.per` 真实文件 `ammr110.per`：

```text
SCREEN RECORD 与 ATTRIBUTES item（mmg01、more 等）均正确提取
```

## 4. 已完成：Block 4 定义与引用

### 4.1 交付内容

| 文件 | 内容 |
|---|---|
| `internal/server/navigation.go` | `definition`、`references` 两个 handler |
| `internal/server/server.go` | 注册 `textDocument/definition`、`textDocument/references` |
| `internal/server/server_test.go` | 新增 8 个测试 |

### 4.2 作用域解析规则

按 Genero 语义，从内到外：

```text
1. rec.field   → 记录字段（先找 rec 定义，再在其 Children 里找 field）
2. 局部 DEFINE → 当前函数体内（Container 匹配）
3. 函数参数    → 当前函数（ScopeParam）
4. 模块级      → DEFINE / CONSTANT / TYPE / FUNCTION / REPORT / MAIN
```

- 每组命中即返回，不再继续外扩——内层遮蔽外层。
- 裸字段名（如 `f1` 不带 `rec.`）**不推断归属**，避免跨记录误跳。
- 同名多命中时返回全部位置，交给客户端选择。

### 4.3 引用查找的过滤

引用查找最容易误报，因此做了三层过滤：

| 过滤 | 原因 |
|---|---|
| 词边界（ASCII 标识符字符） | 避免 `r110` 匹配到 `r110_tm` |
| 注释、字符串、字面量内部 | 注释里写变量名不算引用 |
| `includeDeclaration` | LSP 上下文参数，默认排除定义本身 |

### 4.4 实现要点与踩坑

1. **光标可能落在词的任意位置**  
   最初 `qualifierAt` 假设 `offset - len(word)` 就是词首，实测光标常在词首或词中，
   导致 `rec.field` 判定失败、字段跳转返回 null。
   新增 `wordStartAt`：先向前回退到词首，再向后确认完整词，最后忽略大小写比对。

2. **`ReferenceContext` 是值类型**  
   `params.Context != nil` 无法编译；直接读 `params.Context.IncludeDeclaration`
   （零值就是 false，正好是“排除声明”的默认行为）。

3. **`TYPE ... RECORD` 的字段在下一层**  
   `define r RECORD ...` 的 `record` 在 `types` 字段里；
   而 `TYPE t RECORD ...` 的 `record` 又在 `type` 节点的 `types` 里。
   两者都用 `recordOf` 统一下探。

4. **`record LIKE table.*` 的字段未知**  
   真实语料里 `mmg RECORD LIKE mmg_file.*` 是“字段来自数据库表”。
   LSP 没有 schema 信息，因此该记录无 Children——跳转会落到记录本身。
   这符合“宁可不报”的原则；schema 索引是 P6 的独立任务。

### 4.5 验收结果

```text
$ go test ./internal/server/   → 30 个测试通过（新增 8 个）
$ go test -race ./...          → 全部通过
$ make lint                    → 通过
$ bash scripts/corpus-smoke.sh → 80 文件 0 错误
```

真实文件 `ammr110.4gl`：

```text
definition(CALL r110_tm @L126)  → L132（FUNCTION r110_tm 定义）
references(r110_tm)            → 2 处（定义 + 调用）
```

单测覆盖：

| 测试 | 验证点 |
|---|---|
| `TestDefinitionGlobalVariable` | 全局变量跳转定位到定义行 |
| `TestDefinitionFunction` | 函数调用跳转到 `FUNCTION` 行 |
| `TestDefinitionLocalVariablePrefersLocal` | 局部遮蔽全局时优先局部 |
| `TestDefinitionRecordField` | `r.f1` 跳到记录内字段 |
| `TestDefinitionNotFound` | 空白处返回 null |
| `TestReferencesFindsAllUses` | 定义 + 2 处使用 = 3 |
| `TestReferencesExcludesDeclaration` | `includeDeclaration=false` 时排除定义 |
| `TestReferencesExcludesCommentsAndStrings` | 注释/字符串内不计入引用 |

## 5. 已完成：Block 5 诊断增强

### 5.1 交付内容

| 文件 | 内容 |
|---|---|
| `internal/config/config.go` | 配置模型、默认值、文件加载、客户端下发合并、校验 |
| `internal/config/config_test.go` | 7 个测试 |
| `internal/parser/issues.go` | 严重级别分类、去重、同行相邻 ERROR 合并 |
| `internal/parser/parser.go` | `ParseWith`：C 层超时 + ctx 前后检查 |
| `internal/parser/errors.go` | `ErrTimeout` / `ErrTooLarge` |
| `internal/server/diagnostics.go` | 按配置映射级别、大文件跳过、诊断 code |
| `internal/server/config.go` | `workspace/didChangeConfiguration` 处理与全量重诊断 |
| `scripts/corpus-regress.sh` | 全量语料回归（通过率 + 耗时分位） |

### 5.2 配置项

```json
{
  "bdl": {
    "generoVersion": "2.50",
    "syntaxDiagnostics": "error",
    "missingDiagnostics": "warning",
    "limits": {
      "maxFileBytes": 2097152,
      "maxFileLines": 20000,
      "maxIssues": 200,
      "parseTimeoutMs": 2000
    }
  }
}
```

- 级别支持 `off` / `error` / `warning` / `information` / `hint`。
- `syntaxDiagnostics` 管 ERROR 节点，`missingDiagnostics` 管 MISSING 节点——
  后者通常表示“缺少必填结构”，与语法错误区别对待更贴合实际使用。
- 配置来源优先级：客户端下发 > `.bdl-lsp/config.json` > 内置默认。
- 客户端设置变更后**重新诊断所有已打开文档**，无需等用户再编辑。

### 5.3 诊断质量改进

| 改进 | 说明 |
|---|---|
| 严重级别可配 | ERROR 默认 error，MISSING 默认 warning |
| 去重 | 相同区间+类型+缺失标记的重复问题只报一次 |
| 同行合并 | 一行内相邻（≤3 字节）的多个 ERROR 合并为一条，避免红点成排 |
| 诊断 code | `syntax-error` / `missing-<kind>`，便于用户过滤 |
| 大文件保护 | 超限时只发一条 information 提示，不做语法检查 |
| 超时保护 | 超时不等于文件有错，**保留上次诊断**，避免编辑时闪烁 |

### 5.4 关键踩坑：`ParseCtx` 存在崩溃

go-tree-sitter v0.25 的 `Parser.ParseCtx` 实现：

```go
if ctx.Done() != nil {
    go func() {
        select {
        case <-ctx.Done():
            atomic.StoreUintptr(p.CancellationFlag(), 1)  // ← 危险
        case <-finish:
            return
        }
    }()
}
```

问题：解析结束后 parser 被 `Reset()` 放回池中复用，若那个 goroutine 迟到执行，
`CancellationFlag()` 可能已指向失效内存，导致 **SIGSEGV**。已实测复现：

```text
panic: runtime error: invalid memory address or nil pointer dereference
github.com/tree-sitter/go-tree-sitter.(*Parser).ParseCtx.func1()
```

**解决**：不用 `ParseCtx`，改为

1. 解析前检查 `ctx.Err()`；
2. 用 `SetTimeoutMicros` 在 C 层中断；
3. 解析后再次检查 `ctx.Err()`（覆盖解析期间被取消的情况），并释放已产生的树。

`-race -count=3` 压测通过。

### 5.5 全量语料回归结果

```text
样本：per=7409  bdl=7699
per: 7407/7409 通过（99.97%）
bdl: 7683/7699 通过（99.79%）

耗时：
per  n=7409  均值 3.4ms  p50 3ms  p95 5ms  p99 7ms   max 29ms
bdl  n=7699  均值 13.5ms p50 9ms  p95 32ms p99 87ms  max 865ms
```

对比既有基线：

| 语言 | 既有基线 | 本次 | 结论 |
|---|---|---|---|
| `.per` | 7068/7070（99.97%） | 7407/7409（99.97%） | 一致（样本口径更大） |
| `.4gl` | 7671/7699（99.64%） | 7683/7699（99.79%） | **略优** |

18 个失败文件逐一核对 `tree-sitter-bdl/BACKLOG.md`，**全部为已知的真实文件问题**：

| 文件 | 问题 |
|---|---|
| `aooi010.per` / `apmi252.per` | 含未注释中文说明文字 |
| `ghri0471.4gl` | 仅 3 字节（`e9 94 98`） |
| `cs_restful.4gl` | 使用 `&&`（BDL 非法） |
| `cxcq761.4gl` | 孤立 `END IF` |
| `baofei.4gl` / `findidx.4gl` | 多行字符串赋值缺逗号 |
| `sapmt540 (3).4gl` | `END RECORD` 多余尾逗号 |
| `ghri0431.4gl` / `saxmt400.4gl` | 文件截断 |
| `TX_asfi301_1/2/3.4gl` | 异常文件 |
| `safi501.4gl` | 缺 `END IF` |
| `csfi510.4gl` | 续行逗号被注释吞掉 |
| `csfq012.4gl` | 行末缺逗号 |
| `sapmt720_sub.4gl` / `cws_get_start_works.4gl` | SQL 内 `END CASE` 等已知语法缺口 |

这说明 **LSP 的诊断没有误报**——报出来的都是文件本身的问题。

### 5.6 复现命令

```bash
cd bdl-lsp
source scripts/dev-env.sh

make regress                      # 全量语料（约 2.5 分钟）
bash scripts/corpus-regress.sh --limit 300   # 快速抽样
```

## 6. 已完成：Block 6 VSCode 客户端

### 6.1 交付内容

```text
vscode-extension/
├── package.json                   # bdl/per 语言、10 项配置、3 条命令
├── tsconfig.json                  # strict + noUnusedLocals
├── esbuild.mjs                    # 单文件打包（444 KB）
├── .vscodeignore                  # 排除 src/test/node_modules
├── src/
│   ├── extension.ts               # 激活、命令、失败提示
│   ├── client.ts                  # LanguageClient、配置同步、重启
│   └── server/binary.ts           # 四级路径查找 + 可操作提示
├── syntaxes/
│   ├── bdl.tmLanguage.json        # 由 grammar 生成
│   └── per.tmLanguage.json
├── language-configuration/
│   ├── bdl.json                   # 注释/括号/自动闭合/缩进
│   └── per.json
└── test/
    ├── runTest.js                 # e2e 驱动（7 条沙箱适配）
    ├── fixtures/{sample.4gl,broken.4gl,sample.per}
    └── suite/index.js             # 14 个用例

bdl-lsp/scripts/
├── generate-tm-grammars.mjs       # 从 grammar 源码生成 TextMate 语法
├── verify-tm-grammars.mjs         # 41 项高亮切分校验
└── e2e-vscode.sh                  # 一条命令跑完整 e2e
```

### 6.2 已实现能力

| 能力 | 状态 | 证据 |
|---|---|---|
| 语言注册 `bdl` / `per` | ✅ | e2e「语言注册 bdl/per」 |
| 语法高亮 | ✅ | 41 项切分校验（关键字/多词短语/注释/字符串/数字/预处理/函数名/字段访问） |
| 大纲符号 | ✅ | e2e「文档大纲包含主要符号」 |
| 悬停 | ✅ | e2e「函数悬停有内容」 |
| 定义跳转 | ✅ | e2e「定义跳转可用」（CALL → FUNCTION） |
| 引用查找 | ✅ | e2e「引用查找可用」 |
| 语法诊断 | ✅ | e2e「语法错误产生 error 诊断」 |
| 增量同步 | ✅ | e2e「修复后诊断消失」 |
| 配置热更新 | ✅ | e2e「配置热更新生效」 |
| 服务器重启 | ✅ | e2e「重启服务器后仍可用」 |
| 二进制四级查找 | ✅ | `bdl.server.path` > `BDL_LSP_PATH` > `.bdl-lsp/bin` > PATH |
| 找不到二进制时提示 | ✅ | 列出已尝试路径 + 4 条解决办法 + 一键打开设置 |
| VSIX 打包 | ✅ | 111 KB，10 个文件 |

### 6.3 踩坑记录（都写进了代码注释）

| 现象 | 根因 | 修法 |
|---|---|---|
| 服务器启动即退，退出码 2 | `vscode-languageclient` 的 stdio 传输**自动追加 `--stdio`**，`cmdline.Parse` 不认 | 新增 `--stdio` flag + 6 个回归测试 |
| e2e 跑完无任何输出 | 测试入口放在 `/tmp`，`require("vscode")` 拿不到 API | 入口必须在扩展目录内 |
| 窗口起了又秒退 | `/dev/shm` 不可写 | `--disable-dev-shm-usage` |
| 无 X 也能跑 | 无 X server | `--ozone-platform=headless` |
| 静默退出、无错误 | `TMPDIR`/`XDG_RUNTIME_DIR` 不可写或每次被清 | 固定在 `artifacts/e2e/`，`chmod 700` |
| 后续运行全部失败 | 上轮 VSCode 子进程持有单实例锁 | 启动前 `pkill -f <vscode dir>` |
| 测试失败但退出码 0 | VSCode 不传递扩展测试结果 | 以 JSON 报告文件判定成败 |
| 缺省日志级别下提前退出 | 未知（VSCode 1.142 行为） | 必须带 `--verbose` |
| TextMate `END IF` 切成两个词 | 规则顺序 + 前缀冲突 | 多词层按长度降序，生成时校验前缀冲突 |

### 6.4 验收结果

```text
$ make grammars-check
✓ vscode-extension/syntaxes/bdl.tmLanguage.json 已是最新
✓ vscode-extension/syntaxes/per.tmLanguage.json 已是最新
✓ 41 项校验全部通过

$ make e2e-vscode
[e2e] 用例 14/14 通过
  ✓ 扩展可激活 (129ms)
  ✓ 语言注册 bdl/per (1ms)
  ✓ 命令已注册 (11ms)
  ✓ 识别 .4gl 语言 (142ms)
  ✓ 识别 .per 语言 (147ms)
  ✓ 干净文件无诊断 (153ms)
  ✓ 语法错误产生 error 诊断 (21ms)
  ✓ 修复后诊断消失 (29ms)
  ✓ 文档大纲包含主要符号 (55ms)
  ✓ 函数悬停有内容 (15ms)
  ✓ 定义跳转可用 (5ms)
  ✓ 引用查找可用 (35ms)
  ✓ 重启服务器后仍可用 (42ms)
  ✓ 配置热更新生效 (584ms)

$ npm run package
DONE Packaged: 4gl-full-0.1.0.vsix (10 files, 111.54 KB)
```

关键字覆盖度：

| 语言 | grammar 关键字数 | 分类表覆盖 | 未分类 |
|---|---|---|---|
| BDL | 361 | 361 + 3 个组合短语 | 0 |
| .per | 76 | 76 | 0 |

生成器会校验分类表与 grammar 的双向一致性：新增关键字未分类、或分类表里的词不在 grammar 中，都会打印警告；多词短语前缀冲突会让生成**直接失败**。

### 6.5 复现命令

```bash
cd bdl-lsp
source scripts/dev-env.sh

make grammars              # 从 grammar 重新生成 TextMate 语法
make grammars-check        # 校验语法为最新且切分正确
make e2e-vscode            # 完整 e2e（构建 + 打包 + 真实扩展宿主）
bash scripts/e2e-vscode.sh --keep   # 失败时保留日志

cd ../vscode-extension
npm run typecheck          # 类型检查
npm run build              # 打包到 dist/
npm run package            # 生成 VSIX
```

安装试用：

```bash
code --install-extension vscode-extension/4gl-full-0.1.0.vsix
```

---

## 7. 待做：Block 7 CI

| 任务 | 说明 |
|---|---|
| 7.1 | `bdl-lsp` GitHub Actions：Linux + Windows，CGO，`go test -race` |
| 7.2 | 语料冒烟（定时/手动，避免每次 PR 跑全量） |
| 7.3 | `vscode-bdl`：typecheck + build + vsce package 产物上传 |
| 7.4 | `zed-bdl`：`cargo check --target wasm32-wasip2` |

**不发布 Release**（用户已明确暂不排期）。

---

## 8. 依赖与顺序

```text
Block 1 ✅
   ├── Block 2 ✅（增量同步）
   │      └── Block 5 ✅（诊断增强）——可与 3 并行
   ├── Block 3 ✅（符号/悬停）
   │      └── Block 4 ✅（定义/引用）
   └── Block 6 ✅（VSCode，依赖 1+3+4+5）
          └── Block 7（CI）  ← 下一步
```

---

## 9. 已知限制

| 限制 | 影响 | 计划 |
|---|---|---|
| 跨文件跳转未实现 | 引用/定义只在当前文档内搜索 | 工作区索引（P1 后续或 P3） |
| `RECORD LIKE table.*` 无字段 | 跳转到记录本身，不下探字段 | 需 schema 索引（P6） |
| grammar 用 `replace` 指向兄弟目录 | 独立 checkout 无法构建 | 后续为 grammar 打 tag |
| 超时后不重试 | 极端慢文件会一直用旧诊断 | 可接受；超时阈值可配置 |
| 多段编辑假定互不影响 | 极端情况下位置可能偏差 | LSP 规定按顺序应用，已逐段处理 |
| 未在 Windows 实测 | Windows 路径/换行未验证 | 你已保证环境正常，CI 覆盖 |
| e2e 仅覆盖 Linux | Windows 上的扩展宿主行为未验证 | Block 7 的 CI 覆盖 |
| 高亮无主题定制 | 只提供 scope 名称，未随主题调色 | 后续可加 semantic token |

---

## 10. 复现命令

```bash
cd bdl-lsp
source scripts/dev-env.sh

make build                       # artifacts/bin/bdl-lsp
make test                        # 单元测试
go test -race ./...              # 竞态检测
make lint                        # gofmt + vet
make check                       # 样例冒烟
make bench                       # LSP 端到端延迟
make regress                     # 全量语料回归（约 2.5 分钟）
bash scripts/corpus-smoke.sh 60  # 语料抽样
make grammars                    # 重新生成 TextMate 语法
make grammars-check              # 校验高亮切分（41 项）
make e2e-vscode                  # VSCode 端到端（14 个用例）

./artifacts/bin/bdl-lsp check --json testdata/broken.4gl | jq
```
