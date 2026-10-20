# P1 详细计划：LSP 骨架 + 解析诊断

> 版本：v0.2（已按实现修正）  
> 日期：2026-10-20  
> 状态：**Block 1 已完成**；Block 2–7 待做。  
> 上游：[粗略计划](PLAN.md)｜[功能清单](FEATURES.md)｜[环境](ENVIRONMENT.md)

---

## 0. 与 v0.1 的差异

v0.1 是纸面拆分；本轮实际落地 Block 1 后，以下内容按真实代码调整：

| 项 | v0.1 设想 | 实际 |
|---|---|---|
| document 同步 | Block 1 就要增量 | **Block 1 用整文替换**，增量放到 Block 2 |
| `check` 子命令 | 未规划 | **新增**，用于 CI/语料冒烟，价值很高 |
| grammar 依赖 | 准备用 tag | 暂用 `replace ../tree-sitter-bdl`、`../tree-sitter-per`（grammar 无 tag） |
| 测试 | 只测 parser | parser + document + **协议级 server 测试**（管道模拟客户端） |
| 二进制大小 | < 10 MB | 实测 **9.3 MB** |
| 性能 | 1 MB < 300 ms | 实测 332 KB / 7138 行 **55 ms**（含进程启动） |

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

## 2. 待做：Block 2 增量同步

**目标**：didChange 走真正的增量编辑，避免每次全文重解析。

| 任务 | 说明 |
|---|---|
| 2.1 | 解析 `TextDocumentContentChangeEvent`：有 `range` 时按区间替换，无 `range` 时整文替换 |
| 2.2 | 把 LSP 的 UTF-16 行列转换为字节偏移（与 `diagnostics.go` 的反向换算） |
| 2.3 | 调用 `Tree.Edit(&ts.InputEdit{...})` 让旧树位置失效 |
| 2.4 | `pool.Parse(lang, newText, oldTree)` 复用旧树 |
| 2.5 | `LineIndex` 增量维护（或整文重建，先测性能再决定） |
| 2.6 | 测试：多轮编辑后诊断位置正确；编辑后 `Tree.Edit` 不 panic |

**验收**：

```bash
go test -run TestDidChangeIncremental ./internal/server/
# 典型 5k 行文件增量重解析 < 100 ms
```

**风险**：`Tree.Edit` 的 `Point` 用字节列，LSP 用 UTF-16 列，必须换算后再调用，否则位置漂移。

---

## 3. 待做：Block 3 符号与悬停

| 任务 | 说明 |
|---|---|
| 3.1 | `internal/index`：扫描工作区，建 `FileIndex`（符号、作用域、导入） |
| 3.2 | BDL 符号提取：`main`、`function`、`report`、`define`、`constant`、`type`、`record` 字段 |
| 3.3 | PER 符号提取：`ATTRIBUTES` item、`SCREEN RECORD` |
| 3.4 | `textDocument/documentSymbol`：按层级返回 |
| 3.5 | `workspace/symbol`：全局符号 |
| 3.6 | `textDocument/hover`：签名、类型、定义位置 |
| 3.7 | 大小写不敏感匹配（Genero 标识符不区分大小写） |

**验收**：`ammr110.4gl` 中悬停 `show_name` 返回函数签名；文档符号列出所有函数。

---

## 4. 待做：Block 4 定义与引用

| 任务 | 说明 |
|---|---|
| 4.1 | 光标 → 最小 CST 节点 → 标识符 |
| 4.2 | 作用域解析：局部 → 参数 → 模块全局 → 跨模块 |
| 4.3 | `textDocument/definition`：返回定义位置；多命中返回全部 |
| 4.4 | `textDocument/references`：同作用域规则下的所有出现 |
| 4.5 | 记录字段：`rec.field` → `DEFINE record` 内的字段声明 |
| 4.6 | 索引缓存（gob）与失效（mtime/size/grammar rev） |

**验收**：函数/变量/记录字段跳转成功率 > 90%（抽样 50 个真实文件）。

---

## 5. 待做：Block 5 诊断增强

| 任务 | 说明 |
|---|---|
| 5.1 | 诊断去重与合并（相邻 ERROR 合并为大区间） |
| 5.2 | 区分 ERROR（error）与 MISSING（warning）严重级别 |
| 5.3 | 大文件保护：> 2 MB 或 > 20k 行时跳过解析并提示 |
| 5.4 | 解析超时保护（`ParseCtx`） |
| 5.5 | 语料回归脚本：7699 `.4gl` + 7409 `.per`，统计通过率与耗时 |

**验收**：全量语料零崩溃；通过率不低于既有基线（`.per` 7068/7070）。

---

## 6. 待做：Block 6 VSCode 客户端

| 任务 | 说明 |
|---|---|
| 6.1 | `package.json`：`bdl`/`per` 语言、配置项、命令、激活事件 |
| 6.2 | `src/binary.ts`：路径解析（设置 > 环境变量 > 工作区 > PATH），找不到时给可操作提示 |
| 6.3 | `src/client.ts`：启动 LSP、`documentSelector`、配置同步、重启命令 |
| 6.4 | `syntaxes/*.tmLanguage.json`：关键字/字符串/注释/数字/函数/类型 |
| 6.5 | `language-configuration/*.json`：注释、括号、自动闭合 |
| 6.6 | `vsce package` 本地 VSIX |
| 6.7 | e2e：headless 模式下激活 + 打开样例 + 断言诊断 |

**验收**：本机 `code --install-extension` 后打开 `testdata/broken.4gl` 出现错误波浪线。

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
   ├── Block 2（增量同步）
   │      └── Block 5（诊断增强）——可与 3 并行
   ├── Block 3（符号/悬停）
   │      └── Block 4（定义/引用）
   └── Block 6（VSCode）——需 1+3+4+5 才完整，但可先接 1 做联调
          └── Block 7（CI）
```

---

## 9. 已知限制（Block 1）

| 限制 | 影响 | 计划 |
|---|---|---|
| 无增量编辑 | 每次编辑全文重解析 | Block 2 |
| hover/definition 等仅声明未实现 | 客户端请求会 MethodNotFound | Block 3/4 |
| grammar 用 `replace` 指向兄弟目录 | 独立 checkout 无法构建 | 后续为 grammar 打 tag |
| 无工作区索引 | 无跨文件符号 | Block 3 |
| 未做性能保护 | 超大文件可能慢 | Block 5 |
| 未在 Windows 实测 | Windows 路径/换行未验证 | 你已保证环境正常，CI 覆盖 |

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
bash scripts/corpus-smoke.sh 60  # 语料抽样

./artifacts/bin/bdl-lsp check --json testdata/broken.4gl | jq
```
