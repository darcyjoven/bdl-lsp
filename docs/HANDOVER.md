# 会话交接（HANDOVER）

> 更新时间：2026-10-21（第三轮：P7 决策全部定稿 + 真实链接数据已验证）  
> 用途：新会话开场只读这一份就能接手。细节在各阶段实现记录里。

---

## 0. 下一步就做这个（P7-A）

> 完整根因与修法：[PLAN-P7.md](PLAN-P7.md) §2。待办勾选项：[TODO.md](TODO.md) §下一轮。

1. **Zed 大纲层级 BUG**：`DEFINE a INT, b STRING` 在 Zed 大纲中 `b` 变成 `a` 的子节点。
   根因已实测定位：`outline.scm` 的 `(define name: (identifier) @name) @item`
   在"单节点多 `name:` 字段"结构下产出**多个区间重合的 `@item`**，
   Zed 按区间建父子关系。修法：`@item` 改到名字节点
   （`(define name: (identifier) @name @item)`），`type`/`constant` 同样。
   **先在 `verify-queries.mjs` 加断言复现，再改。**
2. **VSCode GLOBALS 路径 BUG**：`PathOfURI` 只剥 `file://` 和 `%20`，不解析 `%3A`。
   VSCode 在 Windows 上把盘符冒号编码成 `%3A`（`file:///c%3A/...`），Zed 不编码。
   **上机第一步先打印两个客户端的 `initialize` 入参确认形态**，再建
   `internal/fileuri` 替换 `globals.PathOfURI` 与 `config.uriToPath` 两处重复实现。

两个 BUG 都已确认到行、都有不需要 GUI 的复现手段（query 断言 / 单测）。

### 0.1 P7 的六项决策已全部定稿（2026-10-21）

| # | 决定 |
|---|---|
| Q10 | **诊断全部默认 `warning`**（含 `syntax-error`）；`record-member-unknown` 仍默认 off |
| Q11 | **先单文件** `links.jsonl`；慢再分模组（触发阈值已量化，见 PLAN-P7 §5.3.1） |
| Q12 | 链接资料放**用户项目根 `.link/`**（不是 `.bdl-lsp/`）；分模组时放 `<module>/.link` |
| Q13 | 子文件（无 MAIN）链接栏**默认空白** + 可点击「反查引用程序」，点击才查、分页展开 |
| Q14 | **扁平规则**：`sub`/`lib`/`qry` 对所有程序默认可用，**包括它们自己**，无方向限制 |
| Q15 | Oracle **只保留纯 Go `sijms/go-ora/v2`**；本地驱动方案取消 |

**2026-10-21 第二次答复修正了两项**：

- **Q14 改为扁平规则**（原文"`lib` 不能用 `sub`"）。原 `lib→sub` 的 11 处反例
  （`s_errmsg`×47 等）**自动消解**——它们是合法代码，不该告警。
  `allowModuleDeps` 白名单配置删除。
- **Q15 取消本地驱动**。纯 Go 无法直调 Instant Client，CGO 方案让构建/分发/跨平台
  全部复杂化，收益不划算。只留 `go-ora`，不留 build tag、不留 `driver` 配置。

**另有第 3 项改动（缺失文件）**：链接里有、磁盘上没有的 4gl（实测 800 条，
主要是语料中不存在的 `W*` 模组）→ **忽略，零诊断**，但**必须原样保存在
`links.jsonl`**。原计划的 `link-file-missing` 诊断**删除**。

> ✅ **P7 已无遗留待确认项，可以直接开工。**

### 0.2 真实链接数据已验证（15587 行）

`link.csv`（529042 B，sha256 `3ca6ff10…`）已分析，全部设计按实测值定稿：

| 指标 | 实测 |
|---|---|
| 有效行（`gal04='Y'`） | 15536（过滤掉 51 行 N） |
| 链接组（程序编号） | 5849 |
| 组内恰好 1 个 MAIN | **5374（91.9%）** → 主键策略成立 |
| 组内 0 个 MAIN | 81（另有 389 组全缺失无法判定） |
| 组内 ≥2 个 MAIN | 5 |
| `GAL01` 不在自己组内 | **160** → 证实编号≠文件名 |
| 磁盘找不到的文件 | 800 条 / 700 代码（`W*` 模组不在语料） |
| 单文件 JSONL 体积 | **415 KB**（原估 1–2 MB，实测小 4 倍） |
| 链大小 | p50=1、p90=2、max=364 → 99% 无需异步 |
| 模组大小写 | 数据 92 个全大写 vs 磁盘全小写 → 必须大小写不敏感 |
| CSV 编码 | 带 UTF-8 BOM → 必须 `utf-8-sig` 读 |

## 1. 一句话状态

**P1–P6 全部功能已完成并推送**；P7 已立项、六项决策已定稿、链接数据已验证，
**待执行**（缺陷修复 → 诊断框架 → 内置符号 → 链接索引 → 链接 UI）。
剩下还有需要真实环境才能验的项（Windows / Zed GUI / macOS）。



## 2. 三个仓库（都已推送，工作树干净）

| 目录 | 远程 | HEAD | 内容 |
|---|---|---|---|
| `bdl-lsp/` | `github.com:darcyjoven/bdl-lsp.git` | `882ef00` | Go LSP 服务器 + CLI |
| `vscode-extension/` | `github.com:darcyjoven/vscode-bdl.git` | `365625a` | VSCode 插件 |
| `zed-extension/` | `github.com:darcyjoven/zed-bdl.git` | `8fa1bff` | Zed 插件 |

依赖的两个 grammar 仓库（本地 `replace`，未打 tag）：

- `/code/workspace/tree-sitter-bdl`
- `/code/workspace/tree-sitter-per`

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
| Windows 缺陷修复 + 内置符号 + 链接资料索引 + 诊断级别统一 | **P7（待执行）** | [PLAN-P7](PLAN-P7.md) |

## 4. 验收基线（当前实测值）

| 项目 | 结果 |
|---|---|
| `go test -race ./...` | 19 个包全绿（13 个含测试），**269 个测试函数** |
| VSCode e2e | **33/33** |
| `make lint` | 通过（含 `keywords-check` 防清单漂移） |
| 语料规模 | `.4gl` 7699 / `.per` 7409（`/code/workspace/tiptop-gp`） |
| 解析成功率 | `.4gl` 7683/7699（16 个真实语法错误） |
| 格式化审计（`cmd/fmt-audit`） | 成功 7683、幂等失败 **0**、新增 ERROR **0** |
| GLOBALS 解析 | 8403 resolved / 107 not-found |
| 字段校验（`cmd/field-audit`） | 1725 条发现，模块一致率 97.2% |
| record 成员（`cmd/record-audit`） | 可校验 271760 处，报出 **20**（0.007%） |
| 性能（7138 行 / 329 KB） | didOpen 133ms，didChange 6.0ms |
| **诊断噪声（P7 立项基线，24 个真实 `.4gl`）** | 1733 条，其中 `variable-source-unresolved` **1717（99.1%）**；`field-not-in-form` 15；`file-too-large` 1 |
| ↑ 噪声归因 | `cl_null` 311（`lib/` 用户函数）、`SQLCA` 252、`ARG_VAL` 248、`INT_FLAG` 183、`cl_gr_numfmt` 150、`STATUS` 98 —— 内置符号与链接缺失各占一半 |

## 5. 配置项（VSCode `bdl.*` / Zed `settings.*` 同名）

```text
generoVersion                       2.50
syntaxDiagnostics                   error
missingDiagnostics                  warning
server.path / server.logLevel / trace.server
globals.fileNotFoundDiagnostics     warning
globals.unresolvedSourceDiagnostics warning
globals.searchPaths                 []
fieldDiagnostics                    warning
perRoots                            []
recordMemberDiagnostics             off        ← P6，默认关闭
format.keywords                     unchanged  （unchanged/upper/lower）
format.indentStyle                  auto       （auto/tab/space）
format.indentSize                   0          （0 = 跟随编辑器）
deploy.target                       ""
limits.maxFileBytes                 2097152
limits.maxFileLines                 20000
limits.maxIssues                    200
limits.parseTimeoutMs               2000
```

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

## 8. 下一步候选（按建议顺序）

### ★ P7（已立项，下一轮直接做）

见本文 §0 与 [PLAN-P7.md](PLAN-P7.md)。顺序：

| 阶段 | 内容 | 前置 |
|---|---|---|
| P7-A | Zed 大纲层级 + VSCode GLOBALS 路径（**下一轮**） | 无 |
| P7-B | 诊断级别统一 + 调用点误报修正 | 无 |
| P7-C | 内置符号表 + grammar 缺口清单 | P7-B |
| P7-D | 链接资料：三种采集 + 索引 + 函数可见性 | P7-B、P7-C、真实 GAL_FILE 数据 |
| P7-E | 链接资料 UI（VSCode TreeView / Zed 降级） | P7-D |

### A. Windows 实测（成本低，价值高）

代码路径已就绪，但从未在 Windows 跑过：

- `scripts/build.ps1`（无需 make）
- `scripts/e2e-vscode.ps1`
- 部署：Windows 无 rsync → 自动回退 scp（逐文件 + 路径归一已实现）

> 注：P7-A 的两个 BUG 正是 Windows 实测发现的，上机时一并复验。

### B. 数据库 schema 索引（能显著提升 record 校验覆盖面）

现状：`DEFINE r LIKE table.*` 占语料 **87%** 的 record 成员引用，静态看不到列名，
全部跳过。有 schema 索引后可校验比例从 13% → 接近 100%。

需要决策：schema 来源（`.sch`/`.sql` 文件？数据库连接？）。
可复用 P7-D 的 Oracle 直连基础设施（`go-ora`），两者一并评估。

### C. 工作区级符号索引（解锁跨文件重命名 + 更全的补全）

现状：重命名和补全都只看当前文件/已打开文档。
计划要求 15k 文件 < 60s，需要后台任务 + 持久化缓存。
P7-D 的链接索引是"按程序分组索引"的子集，可视为该方向的起步。

### D. Zed 侧验证

本机无 GUI，需在有显示器的机器上手动加载 dev extension。
注意 Zed 不消费 LSP semantic tokens（走 `highlights.scm`），已在 README 说明。

### E. 暂不排期

- Genero 5.0 语法
- GitHub Release / 二进制自动下载
- VSCode Marketplace / Zed registry 发布
- macOS

## 9. 常用命令

```bash
# 测试
cd bdl-lsp && source scripts/dev-env.sh
go test -race ./...
make lint                      # vet + gofmt + keywords-check
make e2e-vscode                # 需要 VSCode 可执行文件
make bench                     # 性能基线

# 语料评估
go run ./cmd/fmt-audit                    # 格式化幂等 + ERROR
go run ./cmd/field-audit                  # 字段校验误报率
go run ./cmd/record-audit                 # record 成员误报率（约 20 分钟）
FMT_DEBUG=<file> go run ./cmd/fmt-audit   # 单文件格式化调试

# 手工验证 LSP
./artifacts/bin/bdl-lsp version
./artifacts/bin/bdl-lsp check testdata/sample.4gl
./artifacts/bin/bdl-lsp deploy -list -workspace <ws>
./artifacts/bin/bdl-lsp deploy -dry-run -workspace <ws>
```

## 10. 文档地图

| 文档 | 内容 |
|---|---|
| [README](../README.md) | 仓库总览、快速开始、能力表 |
| [PLAN](PLAN.md) | 目标、架构、里程碑、风险（§7.1–7.7 是各功能设计） |
| [FEATURES](FEATURES.md) | 功能基线清单（编号 1–40）+ 阶段映射 |
| [TODO](TODO.md) | 已完成/待办逐项清单（最详细的进度记录；§下一轮 = P7-A） |
| [PLAN-P7](PLAN-P7.md) | **P7 立项**：缺陷根因、内置符号、链接资料设计、待确认决策 |
| [ENVIRONMENT](ENVIRONMENT.md) | 工具链、沙箱约束 |
| [TECH-STACK](TECH-STACK.md) | 版本与选型理由 |
| [RESEARCH](RESEARCH.md) | 语料统计、CST 结构 |
| `PLAN-P*.md` | 各阶段实现记录：需求映射、决策、踩坑、验收输出 |
