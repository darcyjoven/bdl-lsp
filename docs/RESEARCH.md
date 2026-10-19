# 调研记录

> 日期：2026-10-19  
> 目的：为 `PLAN.md` 的技术决策提供证据；记录已跑过的验证、语料事实、CST 结构、未决问题。  
> 所有中间产物在 `bdl-lsp/artifacts/`（Git 忽略）。

---

## 1. 语料基线

语料目录：`/code/workspace/tiptop-gp`（Tiptop GP 真实工程）。

| 指标 | 数量 | 采集命令 |
|---|---:|---|
| `.4gl` 文件 | **7699** | `find tiptop-gp -name '*.4gl' \| wc -l` |
| `.per` 文件 | **7409** | `find tiptop-gp -name '*.per' \| wc -l` |
| 含 `SCREEN RECORD` 的 `.per` | **7320** | `grep -rl "SCREEN RECORD" --include=*.per . \| wc -l` |

### 1.1 已有解析基线

来自 `tree-sitter-bdl/BACKLOG.md`：

- 全量 7699 个 `.4gl`：pass 7671 / fail 28 / ERROR 2980 / MISSING 4。
- 说明：`ERROR` 计数是“含 ERROR 节点的文件数”或节点数，需在 LSP 基线脚本中重新定义并固定口径。
- 121 个 corpus 测试全部通过。

来自 `per-lint/docs/PLAN.md`：

- `tiptop/**/per/*.per` 共 7070 个，`tree-sitter parse -q` 通过 7068（99.97%）。
- 失败 2 个：`demo/per/aooi010.per`、`demo/per/apmi252.per`（含未注释中文说明文字）。
- 注意：7409 是全部 `.per`，7070 是 `tiptop/**/per/*.per` 子集；报告时需注明口径。

### 1.2 表单相关语句出现次数（grep，近似）

| 语句/关键字 | 出现次数 | 说明 |
|---|---:|---|
| `WITH FORM` | **8247** | 主要表单绑定方式（`OPEN WINDOW ... WITH FORM`） |
| `OPEN FORM` | **6** | 旧式/少数用法 |
| `DISPLAY BY NAME` | **51268** | 记录成员按名绑定，v1 暂不校验成员 |
| `INPUT BY NAME` | **5474** | 同上 |
| `CONSTRUCT BY NAME` | **4456** | 常带 `ON f1, f2` 显式字段列表 |
| `AFTER FIELD` | **49130** | 显式字段名，v1 校验 |
| `BEFORE FIELD` | **5394** | 显式字段名，v1 校验 |
| `NEXT FIELD` | **115947** | 显式字段名，v1 校验（含 `CURRENT`） |
| `INFIELD(` | **37535** | 显式字段名，v1 校验 |
| `SCREEN RECORD`（`.per` 文件数） | **7320** | 屏幕记录几乎普遍存在 |

结论：`WITH FORM` + 显式字段语句是字段校验的主要入口；`BY NAME` 成员校验风险高，放到 v1.1。

### 1.3 `.per` item 类型分布（grep，近似）

| 类型 | 次数 |
|---|---:|
| `Edit` | 79253 |
| `ButtonEdit` | 21315 |
| `CheckBox` | 11779 |
| `DateEdit` | 8580 |
| `ComboBox` | 8531 |
| `RadioGroup` | 1693 |
| `TextEdit` | 1228 |
| `IMAGE` | 620 |
| `TimeEdit` | 157 |
| `LABEL` | 137 |
| `SpinEdit` | 36 |
| 其他（ProgressBar/Image/WebComponent/Slider/...） | 少量 |

---

## 2. 真实样例

### 2.1 4GL 侧（`ammr110.4gl`）

```4gl
OPEN WINDOW r110_w AT p_row,p_col
     WITH FORM "amm/42f/ammr110"

CONSTRUCT BY NAME tm.wc ON mmg01,mmg03,mmg09,mmg02,mmg04
...
DISPLAY g_qryparam.multiret TO mmg04
NEXT FIELD mmg04
...
INPUT BY NAME tm2.s1,tm2.s2,tm2.s3,
                tm2.t1,tm2.t2,tm2.t3,
                tm2.u1,tm2.u2,tm2.u3,tm.a,
                tm.more WITHOUT DEFAULTS
...
AFTER FIELD a
...
INFIELD(mmg04)
```

### 2.2 `.per` 侧（`ammr110.per`）

```per
SCHEMA formonly

LAYOUT (spacing=normal, text=%"ammr110")
VBox VBox1
Grid gr3766
{
  ...
  [mmg01               ]  ...
}
END -- gr3766
...
END -- VBox1
END -- ammr110

ATTRIBUTES
Group group01:group01, text="QBE";
Edit mmg01=formonly.mmg01 type VARCHAR, tabIndex=1;
Edit mmg02=formonly.mmg02 type VARCHAR, tabIndex=2;
...
CheckBox a=formonly.a type VARCHAR, ...;
CheckBox more=formonly.more type CHAR, ...;
END -- ATTRIBUTES

INSTRUCTIONS
SCREEN RECORD Undefined(formonly.mmg01,formonly.mmg02,...,formonly.a,formonly.more)
END -- INSTRUCTIONS
```

要点：

- `ATTRIBUTES` 左侧是**表单 item 名**（`mmg01`、`s1`、`a`），右侧是绑定（`formonly.mmg01` 或 `mmg_file.mmg01`）。
- 4GL 的 `TO mmg04`、`AFTER FIELD a`、`NEXT FIELD mmg04`、`INFIELD(mmg04)` 使用 **item 名**，因此字段校验应以 item 名为准，而不是右侧列名。
- `SCREEN RECORD` 列出了 item 的绑定顺序；7320/7409 的 `.per` 有该小节。
- `LAYOUT` 中的 `[mmg01 ...]` 是 item tag，可用于反向检查“布局引用了未定义 item”。

### 2.3 记录/屏幕记录

```4gl
DEFINE sr RECORD
  order1 LIKE ima_file.ima01,
  mmg    RECORD LIKE mmg_file.*,
  ...
END RECORD

FOREACH r110_curs1 INTO sr.*
...
LET l_order[g_i] = sr.mmg.mmg01
```

`sr.mmg.mmg01` 是数据库记录成员，不是表单 item；**不能**把它当作表单字段校验，否则会大面积误报。这进一步支持 v1 只做显式字段语句的决定。

---

## 3. CST 结构（Tree-sitter）

使用 `tree-sitter parse` 解析手工 fixture（`bdl-lsp/artifacts/spike/fixtures/form_sample.4gl`）。关键节点如下。

### 3.1 `OPEN FORM`

```text
(form [9, 2] - [9, 37]
  name: (identifier [9, 12] - [9, 16])          ; cust
  form: (expression [9, 22] - [9, 37]
    (literal
      (string [9, 22] - [9, 37]))))              ; "customer/form"
```

### 3.2 `DISPLAY FORM`

```text
(display [10, 2] - [10, 19]
  name: (identifier [10, 15] - [10, 19]))        ; cust
```

### 3.3 `DISPLAY ... TO`

```text
(display [19, 2] - [19, 26]
  value: (expression ...)
  field: (variable [19, 17] - [19, 26]
    (identifier [19, 17] - [19, 26])))           ; cust_name
```

### 3.4 `CONSTRUCT ... ON`

```text
(construct [18, 2] - [18, 42]
  value: (variable [18, 20] - [18, 22])          ; wc
  field: (variable [18, 26] - [18, 35])          ; cust_name
  field: (variable [18, 37] - [18, 42]))         ; phone
```

### 3.5 `INPUT BY NAME` / `BEFORE FIELD` / `AFTER FIELD`

```text
(input [12, 2] - [17, 11]
  value: (variable ... )                         ; r.a
  value: (variable ... )                         ; r.b
  (branch [13, 4] - [14, 19]
    field: (identifier ...))                     ; BEFORE FIELD a
  (branch [15, 4] - [16, 18]
    field: (identifier ...)))                    ; AFTER FIELD b
```

`NEXT FIELD` 对应 `hook` 节点的 `field` 字段。

### 3.6 `OPEN WINDOW ... WITH FORM`

```text
(window [5, 2] - [5, 49]
  name: (identifier ...)
  coordinate: (number ...)
  coordinate: (number ...)
  form: (expression
    (literal (string ...))))                      ; "customer/form"
```

### 3.7 其他相关节点

- `form` 也可表示 `CLOSE FORM <name>`（无 `form` 字段）。
- `infield_function` 承载 `INFIELD(...)`。
- `branch` 还承载 `ON ACTION` 等；需结合父节点/字段名区分。
- `display`/`input`/`construct` 的 `field`/`value` 字段均为多值（`multiple: true`）。

这部分节点名来自 `tree-sitter-bdl/src/node-types.json`，实现时以生成的 `node-types.json` 为准，不硬编码字符串（可生成常量）。

---

## 4. 可行性验证

### 4.1 Go + CGO + 双 grammar

代码：`bdl-lsp/artifacts/spike/main.go`（临时，Git 忽略）。

```go
p := ts.NewParser()
lang := ts.NewLanguage(bdl.Language()) // 或 per.Language()
p.SetLanguage(lang)
tree := p.Parse(src, nil)
root := tree.RootNode()
fmt.Printf("root=%s error=%v children=%d\n", root.Kind(), root.HasError(), root.ChildCount())
```

结果：

```text
root=source_file error=false children=195   # ammr110.4gl
root=source_file error=false children=11    # ammr110.per
```

环境：Go 1.24.4 + gcc 13.3 + `go-tree-sitter v0.25.0`。

### 4.2 ABI 兼容性（重要）

首次使用 `go-tree-sitter v0.24.0` 时：

```text
panic: Incompatible language version 15. Expected minimum 13, maximum 14
```

原因：两个 grammar 由 tree-sitter CLI 0.26.5 生成，`parser.c` 中 `LANGUAGE_VERSION 15`；`go-tree-sitter v0.24.0` 支持到 ABI 14。

解决：升级到 `go-tree-sitter v0.25.0`（`api.h` 中 `TREE_SITTER_LANGUAGE_VERSION 15`，最低兼容 13），重新编译通过。

**结论**：`go.mod` 必须固定 `v0.25.0` 或更高兼容版本；grammar 升级需同步验证。

### 4.3 编译开销

- BDL parser.c 约 11.5 MB；首次冷编译（含依赖下载）约 20 s；热编译约 7 s。
- 双 grammar 的 spike 二进制约 5.6 MB。
- 说明：每次 CI 构建耗时可控；开发时增量编译可接受。

### 4.4 LSP 库与握手

代码：`bdl-lsp/artifacts/spike-lsp/main.go`（临时）。

- 依赖：`go.lsp.dev/protocol v0.12.0`、`go.lsp.dev/jsonrpc2 v0.10.0`、`go.uber.org/zap`。
- 用 `jsonrpc2.NewStream(stdin/stdout)` + 自定义 handler，不实现完整 `protocol.Server` 接口。
- 发送标准 `initialize` 请求，收到：

```json
{"jsonrpc":"2.0","result":{"capabilities":{"textDocumentSync":2,"definitionProvider":true},"serverInfo":{"name":"bdl-lsp-spike","version":"0.0.1"}},"id":1}
```

**结论**：协议层可行；`textDocumentSync: 2` 表示增量同步。

### 4.5 Tree-sitter CLI

- 本机 `tree-sitter-per/node_modules/.bin/tree-sitter` 版本 0.26.5。
- `$HOME` 不可写，需要 `HOME` 指向工作区；`~/.config/tree-sitter/config.json` 配置 `parser-directories: ["/code/workspace"]` 后可解析两个 grammar。
- BDL scanner 有非 static 函数 `printfLog` 警告；不影响解析，可后续修复。

---

## 5. Zed 扩展调研

来源：Zed 官方文档（Language Extensions / Developing Extensions）与 `zed_extension_api 0.7.0` 文档。

关键事实：

- Zed 扩展是 Git 仓库，含 `extension.toml`；语言服务器在 `[language_servers.<id>]` 中声明，`languages` 必须与 `languages/<lang>/config.toml` 的 `name` 一致。
- 扩展逻辑用 Rust 编译为 **WASM**，目标为 `wasm32-wasip2`；grammar 需要 wasi-sdk 编译。
- `Extension::language_server_command()` 返回 `zed::Command { command, args, env }`。
- 可用 `download_file`、`make_file_executable`、`latest_github_release`、`current_platform` 等 API 下载/安装 LSP。
- 一个扩展可注册多个 grammar；`file://` 可用于本地开发。
- Zed 不支持扩展注册任意用户命令；部署需要 CLI + task，或 LSP code action/command。

风险：

- 本机无 Rust/Zed，无法立即做真实加载验证；需要先完成最小骨架与 `cargo check`。
- grammar ABI 15 与 Zed 内置 Tree-sitter 运行时版本的兼容性需在真实 Zed 中验证（现有 `bdl-plugin` 使用同一 grammar，可参考）。
- Zed grammar 编译 WASM 时，BDL 的外部 scanner 需要被 wasi-sdk 编译；`printfLog` 警告需关注。

---

## 6. VSCode 扩展调研

- `vscode-languageclient@10.1.2` 要求 VSCode `^1.91.0`，内置 `vscode-languageserver-protocol@3.18.4`。
- `@vscode/vsce@4.0.0` 要求 Node >= 22；本机 Node 24 可满足。
- 高亮必须依赖 TextMate grammar；LSP 语义高亮只能作为增强（且用户可能关闭）。
- 二进制下载/校验/权限/平台识别需要自己实现；VSCode 没有内置的“安装 LSP 二进制”机制。
- 端到端测试需要 `@vscode/test-electron`；Linux CI 需 `xvfb`。

---

## 7. 未决/待验证问题

| # | 问题 | 验证方式 |
|---|---|---|
| R1 | Zed 是否能加载 ABI 15 的 grammar 与外部 scanner？ | 安装 Zed，dev extension 实测 |
| R2 | `tree-sitter-bdl` 的 scanner 在 WASI 下是否可编译？ | `cargo check` + Zed grammar 构建日志 |
| R3 | 字段上下文跟踪在真实模块中的准确率？ | 50 模块人工标注 + 诊断快照 |
| R4 | `.per` 路径解析（`42f` → `per`）在 Tiptop 目录结构中的覆盖率？ | 脚本抽样统计 |
| R5 | `SCREEN RECORD` 与 4GL 记录/数组的映射规则？ | 手册 + 语料抽样，v1.1 再实现 |
| R6 | 远程 `fglcomp`/`fglform` 输出格式与退出码？ | 测试服务器采集样本 |
| R7 | 15k 文件索引的真实耗时/内存？ | 基准脚本 |
| R8 | VSCode 市场发布策略与 publisher？ | 用户确认 |

---

## 8. 复现命令

```bash
# 环境
export HOME=/code/workspace/bdl-lsp/artifacts/home
export GOPATH=/code/workspace/bdl-lsp/artifacts/gopath
export GOMODCACHE=$GOPATH/pkg/mod
export GOCACHE=/code/workspace/bdl-lsp/artifacts/gocache
export GOPROXY=https://goproxy.cn,direct
export GOTOOLCHAIN=local

# Tree-sitter 解析
tree-sitter parse -q /code/workspace/tiptop-gp/tiptop/amm/4gl/ammr110.4gl
tree-sitter parse -q /code/workspace/tiptop-gp/tiptop/amm/per/ammr110.per

# Go 双 grammar
cd /code/workspace/bdl-lsp/artifacts/spike
go build -o spike .
./spike bdl /code/workspace/tiptop-gp/tiptop/amm/4gl/ammr110.4gl
./spike per /code/workspace/tiptop-gp/tiptop/amm/per/ammr110.per
```

> `artifacts/` 不进入 Git；spike 源码是临时验证程序，未提交。上述命令用于当前阶段复现，关键代码见第 4 节。
