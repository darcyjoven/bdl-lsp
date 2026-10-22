# 会话交接（HANDOVER）

> 更新时间：2026-10-23（第十二轮：**VSCode 平台包内置 bdl-lsp + GitHub 矩阵构建 win/linux/mac**）
> 用途：新会话开场只读这一份就能接手。细节在各阶段实现记录里。
>
> 版本：`bdl-lsp` **0.3.1**（修 GLOBALS 索引串味）/ `vscode-extension` **0.4.0** / `zed-extension` **0.1.1**。
> 版本策略（用户定）：从 0.1.1 起算，**每完成一轮完整改动并提交就 +1**；
> 三个仓库各自独立记版本，不要求一致。

---

## 0. 下一步就做这个

**P1–P8 全部完成**。代码侧没有"必须做"的项了；剩下的都需要真实环境或使用反馈：

1. **Windows 实机验证**（最高价值）：`scripts\build.ps1`、`scripts\e2e-vscode.ps1`、
   GLOBALS 路径（`axmt620.4gl`）、部署 scp 回退。CI 的 windows job 只证明能编译。
2. **Zed GUI 验证**：dev extension 加载、outline 层级、LSP 启动。
3. **真实 Genero 服务器**：`fglcomp`/`fglform` 编译诊断的行号映射。
4. **`record-member-unknown` 默认级别**：开启后全语料 24 条（[PLAN-P6-SCHEMA.md](PLAN-P6-SCHEMA.md) §4.1），等使用反馈再定。
5. **P7-D 分片**：链接资料 > 8 MB 才触发（当前 415 KB）。

完整清单见 [TODO.md](TODO.md)：40 项未勾选，**"可做"档为 0**，其中 21 项要
Windows/真实服务器/GUI。测试命令见 [TESTING.md](TESTING.md)，
编辑器差异见 [EDITOR-SUPPORT.md](EDITOR-SUPPORT.md)。

**诊断码改动流程**：改 `internal/diag` 的 `summaries`/`defaultLevel` 后，跑
`make diagnostics-write` 重生成 `docs/DIAGNOSTICS.md`，再跑 `make lint`
（内含 `diagnostics-check`，会核对 VSCode `package.json` 里 `bdl.diagnostics` 的类型与级别枚举）。

**改 grammar 的硬规矩**：

1. `bindings/go/binding.go` 是 `#include "../../src/parser.c"`，**Go 构建缓存不跟踪
   被 include 的 C 文件** → 改完 `src/scanner.c` 必须 `go build -a ./internal/parser`，
   否则跑的还是旧产物（症状：改前改后审计结果一模一样）；
2. 每次改动都要跟 `make grammar-audit JSON=/tmp/before.json` 的逐文件结果 diff，
   并跑 `go test ./internal/gramaudit/`（`TestCorpusGrammarBaseline` 会拦住"修一个坏两个"）。
3. grammar 发版后要同步**两处版本**：bdl-lsp `go.mod` 的 tag、zed-extension
   `extension.toml` 的 40 位 SHA。只改一处会出现"LSP 新语法、Zed 旧染色"。

### 已完成（本轮 P8）

| 交付 | 入口 |
|---|---|
| 测试矩阵 | [TESTING.md](TESTING.md)：L1–L6、Linux/Windows 命令、语料回归、真实环境清单 |
| grammar tag | `tree-sitter-bdl v1.0.1` · `tree-sitter-per v1.0.0`；bdl-lsp 去 `replace`，CI 不再检出兄弟目录 |
| go-tree-sitter v0.25 | 两个 grammar 的 `bindings/go` 测试：ABI ≥15 + 真实文件解析 0 ERROR |
| 忽略列表 | `bdl.ignore.files/fields/forms`（`internal/glob` + `internal/ignore`）；文件命中同时排除出索引 |
| 部署当前文件 | VSCode `bdl.deployCurrentFile` + Zed task `$ZED_FILE`（复核 + 文档） |
| 编辑器差异 | [EDITOR-SUPPORT.md](EDITOR-SUPPORT.md)：语义高亮、pull diagnostics、配置外壳、链接视图、部署 |
| 稳定性修复 | `parseMu` 串行化请求与防抖重算，修掉偶发 `ts_query_cursor_next_match` SIGSEGV（坑 #31） |
| 链接导入进度 | `link import/sync` 扫描源码时每 400 个文件向 stderr 打一行进度（0.2.3） |
| 未保存文件崩溃 | `bdl/linkTree` / `bdl/linkReferences` 先判 `file:` scheme；`untitled:` 不再让 `Filename()` panic（0.2.4） |

## 1. 一句话状态

**P1–P8 全部功能已完成并推送**：语言智能（诊断/悬停/跳转/补全/重命名/格式化）、
GLOBALS、字段/表单、部署、索引、链接资料、内置符号、忽略列表、编辑器文档齐备。
剩下的是**需要真实环境的验证**（Windows / Zed GUI / 真实 Genero 服务器 / macOS）
和几个等使用反馈的决策，不再有"现在就能动手"的代码项。

## 2. 三个仓库（都已推送，工作树干净）

| 目录 | 远程 | 版本 | 内容 |
|---|---|---|---|
| `bdl-lsp/` | `github.com:darcyjoven/bdl-lsp.git` | `0.3.1` | Go LSP 服务器 + CLI |
| `vscode-extension/` | `github.com:darcyjoven/vscode-bdl.git` | `0.4.0` | VSCode 插件（平台包内置 `bdl-lsp`；设置分基础/链接资料/部署三层，链接与部署各有总开关） |
| `zed-extension/` | `github.com:darcyjoven/zed-bdl.git` | `0.1.1` | Zed 插件 |

> HEAD 不写死——每轮都变（这份文档已因此过期过两次）。用
> `git -C /code/workspace/<repo> log -1 --oneline` 现查。

两个 grammar 仓库**已打 tag**，bdl-lsp 不再使用 `replace`：

| 仓库 | Go 模块 tag | 用途 |
|---|---|---|
| `tree-sitter-bdl` | `v1.0.1` | `go.mod` 固定；Zed rev `ad54d64` |
| `tree-sitter-per` | `v1.0.0` | `go.mod` 固定；Zed rev `f76dab7` |

本地仍是兄弟目录（`/code/workspace/tree-sitter-bdl`、`tree-sitter-per`），
但构建走**模块缓存**；改 grammar 后必须提交+push+打新 tag，再升 `go.mod`。

## 3. 当前能力矩阵

### LSP 方法（20 个）

```text
initialize / initialized / shutdown / exit
textDocument/didOpen / didChange / didClose
textDocument/hover / definition / references
textDocument/documentSymbol / workspace/symbol
textDocument/formatting              ← P4
textDocument/completion / completionItem/resolve   ← P6
textDocument/prepareRename / rename  ← P6
textDocument/codeAction              ← P6
textDocument/semanticTokens/full     ← P6
workspace/didChangeConfiguration
```

### 功能与阶段

| 功能 | 阶段 | 记录文档 |
|---|---|---|
| 高亮（TextMate，grammar 生成）+ 语法诊断 | P1/P2 | [PLAN-DETAILED-P1](PLAN-DETAILED-P1.md) |
| 大纲 / 悬停 / 定义 / 引用 | P1 | 同上 |
| GLOBALS 模组全局变量（跨文件跳转/来源/两类警告） | P3a | [PLAN-P3A-GLOBALS](PLAN-P3A-GLOBALS.md) |
| 4GL 字段 ↔ `.per` 校验 + `.per` 内部一致性 | P3b | [PLAN-P3B-FIELD](PLAN-P3B-FIELD.md) |
| 4GL 格式化（关键字大小写 + 缩进；`.per` 不格式化） | P4 | [PLAN-P4-FORMAT](PLAN-P4-FORMAT.md) |
| 部署 + 远程编译（rsync 主 / scp 备） | P5 | [PLAN-P5-DEPLOY](PLAN-P5-DEPLOY.md)、[DEPLOY-ZED](DEPLOY-ZED.md) |
| 补全（关键字/符号/表单字段） | P6 | [PLAN-P6-COMPLETION](PLAN-P6-COMPLETION.md) |
| 重命名（仅当前文件） | P6 | [PLAN-P6-RENAME](PLAN-P6-RENAME.md) |
| Code Action（拼错→「改为 X」） | P6 | [PLAN-P6-CODEACTION](PLAN-P6-CODEACTION.md) |
| 语义高亮（仅自定义符号） | P6 | [PLAN-P6-SEMTOK](PLAN-P6-SEMTOK.md) |
| record `BY NAME` 成员校验（**默认 off**） | P6 | [PLAN-P6-RECORDMEMBER](PLAN-P6-RECORDMEMBER.md) |
| Zed 大纲层级 + Windows GLOBALS 路径 | P7-A | [PLAN-P7](PLAN-P7.md) |
| 诊断级别注册表 + 降噪（289304 → 7886） | P7-B | [PLAN-P7](PLAN-P7.md) |
| 内置符号表（96→97 条，三层可配置、可热更新） | P7-C-1 | [PLAN-P7](PLAN-P7.md) §10.6 |
| 链接资料索引（导入/状态/可见性/`link-data-*`） | **P7-D 核心** | [PLAN-P7](PLAN-P7.md) §10.7 |
| 忽略列表（文件/字段/表单，glob + `**`） | P8 | [TESTING](TESTING.md)、[FEATURES](FEATURES.md) |
| 编辑器能力差异（VSCode/Zed） | P8 | [EDITOR-SUPPORT](EDITOR-SUPPORT.md) |
| grammar tag / go-tree-sitter v0.25 | P8 | [TESTING](TESTING.md) §3.3 |

## 4. 验收基线（当前实测值）

| 项目 | 结果 |
|---|---|
| `go test -race ./internal/...` | **24 个含测试的包全绿**，**444 个测试函数**（server 174）；P8 新增 glob/ignore/wsindex 排除/协议级 ignore 用例 |
| VSCode e2e | **33/33** |
| `make lint` | 通过（含 `keywords-check` 防清单漂移） |
| 语料规模 | `.4gl` 7699 / `.per` 7409（`/code/workspace/tiptop-gp`） |
| 解析成功率 | `.4gl` 7683/7699（16 个真实语法错误） |
| 格式化审计（`cmd/fmt-audit`） | 成功 7683、幂等失败 **0**、新增 ERROR **0** |
| GLOBALS 解析 | 8403 resolved / 107 not-found |
| 字段校验（`cmd/field-audit`） | **869** 条发现（修语义前 1725），`-form <字面量>` 逐个表单抽查 |
| record 成员（`cmd/record-audit`） | 可校验 271760 处，报出 **20**（0.007%） |
| 性能（7138 行 / 329 KB） | didOpen 133ms，didChange 6.0ms |
| 索引构建 `index.Build`（P7-B-5 后实测，600 行级真实文件） | `ammr100.4gl` 471µs、`aapi204.4gl` 398µs（比 P7-B-5 之前**快 26~31%**）；`apmi006.4gl` 490µs、`apmt721_icd.4gl` 31µs（比之前 +9~18%，为游标支持付的增量） |
| 游标/prepare 符号规模（7699 个真实 .4gl） | prepare **37603** / cursor **36103**（其中 22787 带来源 prepare） |
| **诊断噪声（P7 立项基线，24 个真实 `.4gl`）** | 1733 条 → P7-C-1 后 **7 条**（`variable-source-unresolved`） |
| **全量诊断（15108 文件，P7-B 收尾后）** | 合计 **8453**：`variable-source-unresolved` 6030、`function-not-found` 1423、`field-not-in-form` **869**、`globals-file-not-found` 107、表单类 24 |
| 链接资料（真实 CSV 导入） | 5398 链 / 451 orphan / 0 ambiguous / 7931 文件 / 缺失 800；`.link/links.jsonl` **753 KB**，加载 ~10 ms，导入 **24 s**（含全语料 7600 文件解析） |
| 外部函数（P7-D-2） | **62** 个名字（`cl_err3` 3340 文件、`WSHelper_*` 29 个…），fnf 86612 → **1358** |
| ↑ 噪声归因 | `cl_null` 311（`lib/` 用户函数）、`SQLCA` 252、`ARG_VAL` 248、`INT_FLAG` 183、`cl_gr_numfmt` 150、`STATUS` 98 —— 内置符号与链接缺失各占一半 |

## 5. 配置项（VSCode `bdl.*` / Zed `settings.*` 同名）

```text
generoVersion                       2.50
server.path / server.logLevel / trace.server
diagnostics.<码>                    唯一诊断入口；默认 warning（例外见注册表）
diagnostics.syntax-error            默认 warning
diagnostics.record-member-unknown   默认 off
diagnostics.file-too-large          默认 information
diagnostics.link-data-stale         默认 hint
format.keywords / indentStyle / indentSize    unchanged / auto / 0
limits.maxFileBytes / maxFileLines / maxIssues / parseTimeoutMS
                                    2MB / 20000 / 200 / 2000
index.enable                        true
schema.enable / schema.paths        true / []
globals.searchPaths                 []
perRoots                            []
builtins.enable / .paths / .disable true / [] / []
ignore.files                        []         ← P8：整文件静默 + 不进索引
ignore.fields                       []         ← P8：只屏蔽 field-not-in-form
ignore.forms                        []         ← P8：屏蔽该表单的字段类诊断
link.enable                         true       ← P7-D
link.dataDir                        .link
link.moduleRoots                    []          （空 = 自动探测 tiptop/topcust）
link.globalModules                  [sub lib qry]
link.maxAgeDays                     7           （0 = 不提示过期）
link.sync.enable                    false       ← P7-D-3，默认关闭
link.sync.source                    golang      （golang/command/file）
link.sync.interval                  0           （秒，0 = 只启动时同步）
link.sync.onStart                   false
link.sync.command                   ""          （source=command）
link.sync.file                      ""          （source=file；空 = .link/gal_file.csv）
link.sync.db.host/port/service/user 1521…       （source=golang；密码见下）
  密码：环境变量 BDL_ORA_PASSWORD > <项目>/.link/secrets.json（必须 0600）
deploy.target                       ""          （VSCode 命令使用）
```

旧专用诊断键（`syntaxDiagnostics`、`missingDiagnostics`、`fieldDiagnostics`、
`recordMemberDiagnostics`、`globals.fileNotFoundDiagnostics`、
`globals.unresolvedSourceDiagnostics`）已在 0.3.0 删除，统一走
`diagnostics.<码>`。

## 6. 环境约束（每次开工先做）

```bash
cd /code/workspace/bdl-lsp
source scripts/dev-env.sh     # 必须！否则 Go/Rust/node/vsce 都可能找不到
```

- 沙箱：无 root、`$HOME` 只读、8C/15GB；缓存都在 `bdl-lsp/artifacts/`
- `make build` → `artifacts/bin/bdl-lsp`
- 过程产物、日志、缓存一律放 `artifacts/`（已在 `.gitignore`）
- **`go build` 不加 `-o` 会把二进制写进仓库根**（历史踩过，`field-audit` 曾被误提交）

## 7. 已知坑（写在这里避免重复踩）

1. **typed-nil 陷阱**：`ts.NewQuery` 返回 `*QueryError`（具体类型），
   赋给预声明的 `error` 接口后 `err != nil` 永远为真 → 功能静默禁用。
   必须单独声明 `qerr`。
2. **ssh 远程命令不要额外包引号**：`exec` 调用没有本地 shell 剥引号，
   加一层会让远程把整串当文件名。只对插值进脚本的值用 `shellQuote`。
3. **glob 不能用 `filepath.Match`**：它不支持 `**`，
   `**/*.4gl` 会"合法但永不匹配" → 静默传 0 个文件。
4. **格式化要跳过 `&` 开头的预处理指令**：grammar 外部扫描器大小写敏感，
   大写 `&include` 会破坏解析。
5. **记录/变量作用域必须按函数隔离**：整文件平坦映射会跨函数泄漏
   （P3b 踩过 2.8 万误报，P6 record 又踩过同类 5 次）。
6. **`vscode.executeCodeActionProvider` 的 kind 参数要字符串** `"quickfix"`，
   传 `CodeActionKind` 对象会报 Invalid argument。
7. **Go 的 `const` 必须用 `=`**，不能用 `:=`（本轮踩了两次）。
8. **e2e 前必须 `npm run build`**：`test:e2e` 已改成先 build，否则用旧 `dist` 会
   出现"命令不存在"的假失败。
9. **URI ↔ 路径的转换不能手写**（P7-A 踩）：`PathOfURI` 只剥 `file://` 与 `%20`，
   遇到 VSCode 在 Windows 上编码的 `%3A`（盘符冒号）就解出 `/c%3A/...` 非法路径。
   Zed 不编码冒号所以正常 → 表现为"VSCode 认不到、Zed 认得到"。
   修法与用例表见 [PLAN-P7](PLAN-P7.md) §2.2。
10. **Zed `outline.scm` 的 `@item` 必须唯一**：一条 `DEFINE a INT, b STRING` 是
    **单节点多 `name:` 字段**，若 `@item` 绑整条语句，多个 match 的区间完全重合，
    Zed 会按区间把它们判成父子层级（VSCode 不受影响，它走 `documentSymbol`）。
    绑定到名字节点本身即可；`type`/`constant` 同理。
11. **`(variable (identifier))` 会把函数调用名当变量**：表达式位置的
    `fn()` 语法是 `(expression name: (variable ...))`，只跳过 `(call name: ...)`
    会漏掉它 → 每个函数调用都被报"变量未定义"。P7-B 修。
12. **内置名字不要塞进 grammar**：会让高亮/重命名/补全把它当关键字，
    且自定义同名符号被冒充。判据：`tree-sitter parse` 有 ERROR 才改 grammar。
13. **`filepath.FromSlash` 在非 Windows 平台是恒等函数**（P7-A-2 踩）：
    想写"跨平台可测的 Windows 归一化"时不能用它，否则单测在 Linux 上
    静默拿到正斜杠、断言假通过。自己写 `strings.ReplaceAll(p, "/", "\")`。
14. **Zed `@item` 区间唯一**这条约束对 `.per` 同样有效（P7-A-1 意外发现）：
    `PHANTOM table.column` 的 `field_ref` 有两个 `name` 子节点（表名 + 列名），
    写 `(field_ref (name) @name)` 会让两者区间重合 → 大纲假层级。
    必须绑 `column:` 字段。
15. **同一个字段被两种用途共用会互相牵制**（P7-B-1 踩）：
    `Symbol.TypeText` 既用于展示（悬停）又用于语义判断
    （`recordname.go` 判 `RECORD` 前缀）。压换行会让语义判断失效，
    不压换行会让悬停没法读。
    正确做法是**拆成两个字段**（`TypeText` 语义 / `TypeTextRaw` 展示），
    而不是改其中一个的语义。
16. **Zed 层级判据是"区间相交"，不是"区间相同"**（P7-B-3 踩，最贵的坑）：
    源码判据是 `if last_end < item.range.end { pop } else { break }`——
    只要前一条目的**结束位置**不小于当前条目就算父子。
    P7-A-1 的断言只查"区间完全相同"，对这种相交完全无感——
    写这类断言要**直接复刻 Zed 的 depth 算法**，别自己发明近似条件。
    （原写的结论"@item 必须绑最小语义单元"是**错的**，见第 18 条。）
17. **跳转与悬停必须共用同一套作用域规则**（P7-B-2 踩）：
    原来跳转按作用域过滤、悬停不过滤，于是"能跳转但悬停列出别的函数的同名变量"。
    抽出 `scopedSymbolsAt` 给两处共用，别再各写一套。
18. **Zed 里"父 item 覆盖子 item 区间"是合法层级，不是 bug**（P7-B-3 补修踩）：
    内置语言的 outline 查询在 `crates/grammars/src/<lang>/outline.scm`
    （**不是** Rust 源码里的字面量，`grep outline` 在 `.rs` 里搜不到）。
    Rust/Python 都让函数的 `@item` 覆盖整块，另用 `@open`/`@close` 标折叠。
    所以：
    - 容器（函数/报表/MAIN/MENU）→ `@item` 绑**整个节点**
    - 叶子（DEFINE/TYPE/CONSTANT/LABEL）→ `@item` 绑**名字标识符**
    假层级要在**叶子侧**解决；把容器 `@item` 缩到名字节点会消灭
    "函数内变量归属函数"这个**正确行为**（用户会立刻发现变量没法折叠了）。
    一句话：**父覆盖子合法，兄弟区间相同才是不合法**。
19. **断言里的"容器/叶子"分类不能靠启发式**（P7-B-3 补修踩）：
    曾用"单行且 ≤64 列"判定叶子——但**函数容器和变量叶子都是单行短标识符**，
    两者被混为一谈，于是"把函数 `@item` 缩到名字节点"这个回归
    断言全绿、用户照样报问题。
    正确做法：用 `tree-sitter parse` 的语法树作**独立真相源**，
    按区间覆盖关系判断"这个 item 是否覆盖了整个定义块"。
    断言要直接检验**用户要求的行为**（变量的父级链上能找到所属函数），
    而不是"我的实现长什么样"。

20. **`index.Build` 的成本几乎全是 cgo 调用**（P7-B-5 实测，最重要的一条性能经验）：
    每次 Go↔C 调用约 **230ns**，而 tree-sitter 的 C 侧步进几乎免费。
    两个直接推论：
      - `NamedChild(i)` 在 tree-sitter 里是 O(i)（`ts_node__child` 每次
        从头迭代子节点），**但逐个索引取 n 个孩子的 O(n²) 并不构成问题**
        —— 全树遍历 1918 个节点时，耗时（880µs）正好等于 cgo 调用数
        （~3836）× 230ns；
      - 反过来，"看起来更优"的 `NamedChildren(cursor)` 每个孩子要 **3 次**
        cgo 调用（`Node` + `IsNamed` + `GotoNextSibling`），实测**慢 2~2.7 倍**。
    我按"消除 O(n²)"的思路把遍历全改成 `NamedChildren`，方向是**错的**。
    正解是**按孩子数量选**（实测交叉点约 55；顶层 root 常有上百个孩子，
    因为模块级 DEFINE 多）。实现见 `builder.forEachChild`，阈值 64。
    **教训：先量再做**——"复杂度更优"不等于"更快"，
    在这个 binding 上 cgo 调用次数才是唯一的一阶项。

21. **找"某类节点"不要遍历整棵树**（P7-B-5）：prepare/declare 是可执行语句，
    能嵌在任意深度，于是先试了两种全树方案——Go 侧递归（Build 翻倍）和
    tree-sitter query（实测 **228ns/节点**，10678 节点的文件要 2.43ms）。
    最终用「**源码字节里找关键字 → `DescendantForByteRange` 定位**」，
    成本只与关键字出现次数成正比（一次 Build 只多几十µs）。
    附带好处：不必维护"哪些节点可能含语句"的白名单——那种白名单会把
    `PREPARE` 写在 `MENU`/`DIALOG` 里的情况漏掉，且**语料里恰好没有这种
    写法，测试也发现不了**。
    另外 `Node.Kind()` 每次 `C.GoString` 分配字符串，
    类型比较要用 `KindId()`（uint16，零分配）。

22. **query 里的"Impossible pattern"会让整个 query 静默失效**（P7-B 踩）：
    隐藏规则别名产生的 field 在 `node-types.json` 里不存在于父节点上，
    写成 `(whenever name: (variable …))` 就编译不过——而**失败没有任何输出**，
    表现是"这个功能一条诊断都不产生"，和"用户把它关掉了"完全一样。
    两个动作都要做：
      - 写法退到结构匹配 `(whenever (variable …))`（先 `tree-sitter query` 验一遍）；
      - `compileXxxQueries` **返回 error 并 `logger.Error`**，不要再静默 return nil。

23. **"节点文本 = 一个名字"的假设一律要在真实语料上验一遍**（P7-B 踩，两个 4 万级误报）：
      - `item_tag` 节点可含**多个** tag：布局单元格 `[label1 |alh01 ]` 是一个节点
        两个 tag，按整段去查 ATTRIBUTES 永远查不到 → 40167 条误报；
      - `DISPLAY … TO x.y` 的 field 是 `(variable (object object: (variable …) property: …))`，
        直接子节点**不是** identifier，`(variable (identifier) @skip)` 匹配不到。
    推论：**query 里"跳过某处"的 pattern 要捕获整棵子树**（skip 是按 StartByte 比对的，
    内外层同起点，捕获外层一样准）。

24. **"内置名"是最容易被误加的一类数据，加错比不加更糟**（P7-C 踩）：
      - 语料里 `cl_null` 出现 **168067 次**、`cl_err` 137853 次，看着像内置，
        其实是 `lib/4gl/cl_null.4gl:16` 里的用户函数。加进内置表 → 链接缺失
        被永久掩盖，P7-D 做完了也发现不了。
      - 判据：**能不能在仓库里找到 `FUNCTION <name>` 定义**。找得到就一定是用户函数。
        单测里专门留了 `cl_null` 的否定断言，防止以后又有人"顺手补一个"。
      - 同理：`SUM` 在语料里既是聚合函数又是用户变量。内置表对**变量位置**
        的豁免是名字级的，分不清声明与使用——这只会多放过、不会误报，可接受。

25. **数据文件要能"关得掉"和"改得动"，否则三个月后没人敢动**（P7-C 踩）：
      - 内置表做成 `go:embed` 的 JSON 而不是 Go 字面量：补一条名字只改 JSON。
      - 覆盖层合并用**字段级**而不是整条替换：用户想改一句 `doc` 时不该被迫
        重写 `kind`/`category`。
      - `disable` 在**所有文件加载完之后**统一应用：否则结果依赖文件顺序，
        用户永远搞不清覆盖生效了没有。
      - `MaybeReload` 必须**节流**（2s）：它在每次诊断前跑，不节流就是每次按键 stat 一批文件。

26. **LSP 测试 harness 只能有"一个读泵 + 一个 bufio.Reader"**（P7-D 踩，浪费半天）：
      - 每帧 `bufio.NewReader(server)` 都会**预读 4KB**，把后面几帧吞进自己的
        缓冲里，函数一返回就丢了 → 表现为"随机丢诊断"。
      - 每次读取起一个 goroutine（原 `readMessage` 的写法）超时后那个
        goroutine 还挂在 `Read` 上，下一帧被它偷走 → 表现为"等待响应超时"。
      - 正解：`newHarness` 里起**一个**常驻泵把帧灌进 `chan map[string]any`，
        读取方只 `select` 通道 + 定时器。超时只是没人取，帧还在队列里。
      - 附带效果：server 包测试从 29 s 降到 9 s（不再靠超时推进）。

27. **"文件缺失"意味着"不可判定"，不是"忽略后照常判定"**（P7-D 踩）：
      - 链接里有、磁盘上没有的文件（实测 800 条，集中在语料里不存在的
        `W*` 模组）里到底有什么函数**不可知**。忽略它再判定，就会把这条链里
        所有"找不到"的函数都报成未定义。
      - 正解：可见集返回 `complete=false`，整项放弃判定。代价实测很小：
        5849 条链里只有 234 条（4.0%）引用缺失文件。
      - 同一条规则适用于"索引还在后台预热"与"这个程序不在链接资料里"。

28. **MAIN 检测必须有文本兜底**（P7-D 踩）：
      - `aws_ttsrv2_transxml.4gl` 的 `MAIN` 被 `{ … }` 包着，tree-sitter 不认；
        只信语法树会把它算成"非入口"，导致 4 个组（`aws_ttsrv2`/`aws_process`/
        `cbmp100`/`cimi997`）错判成"只有一个 MAIN"。
      - 兜底扫描要保守：行内除 `MAIN` 只允许 `#` 注释；跳过 `#{ … }#` 块注释。
      - 误判代价不对称：多认一个 MAIN 只让链标 `ambiguous`（行为仍正确），
        少认一个则那个入口**永远查不到自己的链接**。

29. **"文件存在但 0 行有效数据"必须当失败**（P7-D 踩）：
      - `links.jsonl` 被截断成空文件时，若 `Load` 返回"空的但有效"，服务器会认为
        资料可用、可见集为空 → `function-not-found` 把所有跨文件调用都当未定义。
      - 正解：0 条链 = 错误（客户端发 `link-data-invalid` 并停用）。
        坏行同理**整份失败**，不静默跳过——跳过会让用户拿到"少了若干链"的资料
        却毫不知情，比直接报错难查十倍。

30. **`initialize` 期间不要发 `publishDiagnostics`**（P7-D 踩）：
      - 通知会和 `initialize` 响应抢同一帧，客户端与测试都得处理乱序；
        测试里表现为"读到的第一帧不是响应"。
      - 正解：首个 `didOpen` 时再发（`maybePublishLinkStatus`），
        那时用户已经真的在看文件了。

31. **语法树是裸 C 指针，替换/关闭不能与 query 并行**（P8 踩，偶发 SIGSEGV）：
    两处会 `Close` 树：`Document.SetTree`（解析出新树时）与
    `ServeStream` 退出时的 `docs.CloseAll()`。防抖 timer（400ms 后
    `reparseFull`）或 `ctx` 取消都可能与正在 `ts_query_cursor_next_match` /
    `ts_tree_root_node` 的 handler 并行 → 段错误。
    症状：单独跑 server 测试全绿，全量/`-race` 下偶发崩溃，栈在 cgo 里，
    且常发生在 `t.Cleanup` 阶段。
    修法：`Server.parseMu` 把 `handle`、timer 重算、**退出时 CloseAll**
    串行化（jsonrpc2 本来就按帧顺序处理请求，不会降低吞吐）。
    **以后任何"后台重算 / 释放资源"都要共用这把锁**，不要各自直接调
    `reparseFull` 或 `docs.CloseAll`。

32. **glob 只有一份实现**（P8）：`internal/glob` 支持 `**`，部署 include/exclude
    与 `bdl.ignore.*` 共用。不要再用 `filepath.Match`（坑 #3），也不要复制第二份
    ——两套 glob 的锚定/空段行为一旦不一致，"同样的模式在部署生效、在忽略不生效"
    会非常难查。

33. **Go 模块 tag 必须 `v` 开头**（P8）：grammar 仓原来只有一个 `1.0.0`
    （没有 `v`），Go 的模块解析**不认**，所以之前只能靠 `replace`。
    P8 改打 `v1.0.1` / `v1.0.0` 并 push，`go mod tidy` 才能拉到。
    升 grammar 的完整顺序：commit → push → **tag + push tag** → 改 bdl-lsp
    `go.mod` → `go mod tidy` → 改 Zed `extension.toml` 的 40 位 SHA。

34. **自定义请求的 URI 必须先判 scheme**（0.2.4 踩，服务器直接退出）：
    VSCode 会为未保存的新文件发 `untitled:` URI；`go.lsp.dev/uri` 的
    `Filename()` 对非 `file:` scheme **直接 panic**，jsonrpc2 不 recover，
    整个 LSP 进程退出（用户看到 "Server process exited with code 2"）。
    所有"从客户端取 URI 再转路径"的入口都要先 `url.Parse` 判 `file:`；
    客户端也应在 `activeUri` 里跳过非 `file:`，双保险。不要靠 recover 掩盖。

35. **按文档缓存的东西，查询时必须按文档取**（0.3.1 踩，用户报"重开 VSCode
    就好了"）：
    `globalsEntries` 是 `docURI → 解析结果` 的全局表。旧实现查符号时遍历
    **整张表**，于是同时打开两个文档时，一个文档会借用另一个文档的
    GLOBALS 导入：
      - 两个文档都引用同一个 `.global` → 悬停里出现两条一模一样的
        `"top.global:2"`，跳转给出重复位置；
      - 两个文档引用**不同**的 `.global` → 变量来源检查拿到别的文档的
        符号集合，"无法确定变量来源"该报的不报。

    症状随"当前开着哪些文件"变化：关掉另一个文件、重启 VSCode、
    重新加载窗口就正常，用户完全无法复现规律——所以**不要**用
    "重启就好了"来解释问题，要去找"状态依赖了不该依赖的东西"。

    修法三件套（`internal/server/globals.go`）：
      1. 查符号只看**当前文档**的 entries（`globalsEntriesOf(docURI)`）；
      2. 文档关闭（`didClose`）时清掉它的条目（`forgetGlobalsEntries`）；
      3. 文档不再有 GLOBALS 导入时也清掉（删掉导入行后的陈旧状态）。
    另外：同一个 `.global` 被引入两次（路径写法不同但指向同一文件）时，
    按"定义位置"去重，否则悬停仍会列两条一样的来源。

    审计工具（`cmd/diag-audit`）逐文件跑，`AuditFile` 里也必须
    `defer s.forgetGlobalsEntries(uri)`，否则统计分布是错的。

## 8. 下一步候选（按建议顺序）

**P1–P8 全部完成**，代码侧没有待办。下面按"价值 / 成本"排序，且都需要
本沙箱之外的条件：

| 顺序 | 事项 | 需要什么 | 验收 |
|---|---|---|---|
| A | Windows 实机（构建 / e2e / GLOBALS 路径 / scp 部署） | Windows 10/11 + MinGW + VSCode | [TESTING](TESTING.md) §5 |
| B | Zed GUI（dev extension、outline、LSP 启动） | 有显示器的机器 | `make zed-check` + 手工 |
| C | 真实 Genero 服务器（远程编译诊断行号） | SSH + `fglcomp`/`fglform` | `deploy -dry-run` → `deploy` |
| D | `record-member-unknown` 默认级别 | 真实使用反馈 | [PLAN-P6-SCHEMA](PLAN-P6-SCHEMA.md) §4.1 |
| E | P7-D 分片（资料 > 8 MB） | 更大的真实链接导出 | [PLAN-P7](PLAN-P7.md) |
| F | macOS 构建/验证 | macOS 机器 | [TESTING](TESTING.md) §5 |

暂不排期：Genero 5.0 语法、GitHub Release / 二进制自动下载、Marketplace /
Zed registry 发布、跨文件重命名（刻意不做）、纯 Go SFTP、批量修复。

## 9. 常用命令

```bash
# 测试（完整矩阵见 docs/TESTING.md）
cd bdl-lsp && source scripts/dev-env.sh
go test -race ./...
make lint                      # vet + gofmt + keywords-check
make e2e-vscode                # 需要 VSCode 可执行文件
make bench                     # 性能基线

# 语料评估
go run ./cmd/fmt-audit                    # 格式化幂等 + ERROR
go run ./cmd/field-audit                  # 字段校验误报率
go run ./cmd/record-audit                 # record 成员误报率（约 20 分钟）
make builtin-audit                        # 内置符号表缺口（LIMIT/SHOW 可调）
make builtin-check                        # 校验 data/builtins.json（也在 make lint 里）
FMT_DEBUG=<file> go run ./cmd/fmt-audit   # 单文件格式化调试

# 手工验证 LSP
./artifacts/bin/bdl-lsp version
./artifacts/bin/bdl-lsp check testdata/sample.4gl

# 采集与部署在 tiptop（独立仓库，见 docs/PLAN-SEPARATION.md）
cd ../tiptop && source ../bdl-lsp/scripts/dev-env.sh && make build
./artifacts/bin/tiptop link status -root <ws>
./artifacts/bin/tiptop deploy -list -workspace <ws>
./artifacts/bin/tiptop deploy -dry-run -workspace <ws>
```

36. **`schema.Merge` 曾与文件顺序相关**（分离改造时发现，2026-10-22）：

    `LoadFiles` 按传入顺序合并。ALTER 先到会给表打 `AlterOnly` 标记，
    之后 CREATE 到达时旧代码**换了一张新表**，把 ALTER 已经加上的列整张丢掉。
    症状：间歇性"缺少列"（`internal/schema` 测试 2/10 概率失败；目录遍历顺序、
    多模组目录、并发导入都可能让 ALTER 先到）。

    修法：CREATE 到达时不再换表，只清 `AlterOnly` 并并入列（并集语义）。
    回归测试 `TestMergeOrderIndependent`（alter 先 / create 先两种顺序都断言）。
    教训：**任何"按顺序合并"的累积结构，都要问一句"顺序反过来还对吗"**——
    这类缺陷在本地跑十次九次是过的。

    
## 10. 文档地图

| 文档 | 内容 |
|---|---|
| [README](../README.md) | 仓库总览、快速开始、能力表 |
| [PLAN](PLAN.md) | 目标、架构、里程碑、风险（§7.1–7.7 是各功能设计） |
| [FEATURES](FEATURES.md) | 功能基线清单（编号 1–40）+ 阶段映射 |
| [TODO](TODO.md) | 已完成/待办逐项清单（最详细的进度记录；§下一轮 = 真实环境验证） |
| [TESTING](TESTING.md) | **测试矩阵**：L1–L6、Linux/Windows 命令、语料回归、常见失败 |
| [EDITOR-SUPPORT](EDITOR-SUPPORT.md) | **VSCode/Zed 能力差异**：语义高亮、诊断、配置、部署 |
| [PLAN-P7](PLAN-P7.md) | **P7 立项**：缺陷根因、内置符号、链接资料设计、待确认决策 |
| [ENVIRONMENT](ENVIRONMENT.md) | 工具链、沙箱约束 |
| [TECH-STACK](TECH-STACK.md) | 版本与选型理由 |
| [RESEARCH](RESEARCH.md) | 语料统计、CST 结构 |
| `PLAN-P*.md` | 各阶段实现记录：需求映射、决策、踩坑、验收输出 |
