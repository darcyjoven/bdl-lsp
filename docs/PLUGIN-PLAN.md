# VSCode 插件改版计划（发布前 / 发布后）

> 讨论范围：**只谈 VSCode 插件**（`vscode-extension`）。`tiptop`、`bdl-lsp` 只作为支撑方出现。
> 盘点：2026-10-23。**本文件是这次改版的唯一设计依据**，待办勾选在 [PENDING](PENDING.md) §P。
>
> 结论先说：**发布前 = 除 schema 外的全部**（含 `.project` 目录迁移、设置重分组、三档默认、
> link 局部同步、命令入口、两个 bug）；**schema 功能（`.schema` 数据 + 视图 + 悬停扩展）= 发布后一版**。

---

## 0. 任务清单与优先级

| # | 类型 | 事项 | 归属 | 优先级 |
|---|---|---|---|---|
| P0-1 | **BUG（严重）· 已修** | 链接预热回调在外部关文档树 → tree-sitter 崩溃、服务整个挂掉（大文件 100% 复现） | bdl-lsp | ✅ 已修复（2026-10-23） |
| P0-2 | **BUG · 已修** | `INPUT ARRAY … FROM s_x.*`、`CLEAR azp02` 等表单侧名字被报"变量不存在" | bdl-lsp | ✅ 已修复（2026-10-23，语料 -19.4%） |
| P0-3 | **UX · 已实现** | 文件因大小/超时/未解析时不告知用户；"强制解析"入口 | bdl-lsp + 插件 | ✅ 已实现（2026-10-23） |
| P1-1 | 功能 | 二进制确认进包 + 功能命令进 VSCode 命令面板（链接导入/同步/状态、schema、部署） | 插件（+tiptop） | 高（发布前） |
| P1-2 | 结构 | `.link` → `.project`；CSV 落 `.project/`；密码放 `.project/secrets.json` | bdl-lsp + tiptop + 插件 | 高（发布前） |
| P1-3 | 功能 | `link sync` 局部更新（无参=全量 / 模块 / 具体 4gl）+ 合并进现有资料 | tiptop | 高（发布前） |
| P1-4 | 设计 | 三档使用模式与默认值（默认 = 零配置轻量档） | 插件 + 两个后端 | 高（发布前） |
| P1-5 | 设置 | 设置重分组为 5 类；生产环境配置（DB/部署）不再用文件配置 | 插件 | 中（发布前） |
| P1-6 | 功能 | 去掉 7 天过期与后台自动同步；改为用户主动更新（含"更新当前文件资料"） | 三处 | 中（发布前） |
| P1-7 | 文档 | 用户文档重写为插件说明（README/市场说明） | 插件 | 中（发布前，最后做） |
| P2-1 | 新功能 | **schema**：`.project/.schema`、视图、正则查询、hover 扩展、同步策略 | 插件 + tiptop | **发布后一版** |

---

## 1. 三档使用模式（设计的第 0 原则）

| 能力 | ① 全功能 | ② 敏感信息进项目 | ③ **轻量（默认）** |
|---|---|---|---|
| 语法解析 / 诊断 / 高亮 / 大纲 | ✅ | ✅ | ✅ |
| 跳转 / 悬停 / 引用 / 补全 / 格式化 / 重命名 / Code Action | ✅ | ✅ | ✅ |
| GLOBALS 跨文件 | ✅ | ✅ | ✅（有 `.global` 就生效） |
| 字段 ↔ `.per` 校验 | ✅ | ✅ | ✅（目录里找得到表单就生效） |
| 链接资料（可见性/视图/反查） | ✅ | ✅（自己放资料） | ❌ 默认不启用 |
| 部署（上传 + 远程编译） | ✅ | ✅ | ❌ 默认不启用 |
| 数据库连接（采集/同步） | ✅ | ✅（密码在 `.project/secrets.json`） | ❌ 不连、不问 |
| schema 字段资料 | ✅ | ✅ | ❌（发布后一版） |

**"轻量档"的硬约束**（默认必须满足）：

1. **零配置开箱即用**：不弹"请先配置 X"，命令面板里与配置相关的命令**默认隐藏**
   （`when: config.bdl.link.enable` 一类），而不是点了再报错。
2. **不联网、不连库、不后台跑任务**：没有隐式同步、没有定时器。
3. **不往项目里写文件**：不开 `.project/`、不生成日志到工作区。
4. 需要升级到①/②时：**只在设置面板里开开关**，不要求手写 JSON、不要求找命令行。

> 现状差距：`bdl.link.enable` 与 `bdl.deploy.enable` 现在默认 **true**，
> `record-member-unknown` 等诊断默认也在响。按本档要求，**默认值要按上表重排**（见 §5.3）。

---

## 2. P0-1：链接预热回调关闭文档树导致的崩溃（已修复）

### 2.1 用户看到的现象

> "有些文件的 outline 分析不出来，语法错误解析好像也没进行"、"把最大行数/字节数改大 10 倍也没用"。

### 2.2 复现与证据（2026-10-23 实测）

环境：Linux 沙箱，`bdl-lsp 0.3.4`，语料 `/code/workspace/tiptop-gp`（含 `.link/`，5398 条链）。
脚本：[`scripts/repro-index-crash.py`](../scripts/repro-index-crash.py)（退出码 0=OK / 1=崩溃）。

| 场景 | 结果 |
|---|---|
| 默认限制（2 MB / 20000 行）打开 `tiptop/axm/4gl/saxmt600.4gl`（41486 行 / 1.7 MB） | 不崩；发一条 `file-too-large`(**information**)；**大纲 0 个符号** |
| 限制放宽到 20 MB / 200000 行，打开同一文件 | **服务端 SIGSEGV，退出码 2**（1.3 s），**8/8 全崩** |
| 同一文件、工作区换成只含该文件的临时目录 | ✅ 正常（0.8 s） |
| 同一文件、**把 `.link/` 移走但保留全量索引** | ✅ 正常（0.7–0.8 s，**3/3**） |
| 同一文件、不通过 LSP（直接 `form.Check`） | ✅ 正常（`issues=5, paths=21`） |

崩溃栈（每次都一样）：

```text
SIGSEGV: segmentation violation
signal arrived during cgo execution
  _Cfunc_ts_node_parent
  (*Node).Parent
  internal/form.constructOnColumns      check.go:269
  internal/form.(*Resolver).Check       check.go:222
  internal/server.formChecks            form.go:68
  internal/server.reparseFull           diagnostics.go:146
  internal/server.handleDidOpen         documents.go:36
```

**根因（已定位并修复）**：不是"文件太大"，也不是后台工作区索引，而是**链接资料预热完成后的回调**：

```text
link.SymbolIndex.StartGlobal           // 后台 goroutine 预热全局模组（983 文件 ≈ 2 s）
  └─ onReady 回调 = Server.requestRediagnose
       └─ rediagnoseAll → reparse → Document.SetTree   // ← 关闭旧树！
```

`handleDidOpen`（持 `parseMu`）刚为这个 4 万行文件建好树并正在跑字段校验（约 1 s）；
与此同时预热 goroutine 完成，回调**没有持 `parseMu`** 就去重诊断所有文档 → `SetTree`
把 handler 正在遍历的那棵树 `Close()` 掉 → `ts_node_parent` 读已释放内存 → SIGSEGV。
小文件在预热完成前就跑完了，所以只有大文件表现稳定。

> 这也解释了默认限制下的现象：文件被跳过 → 没大纲；放宽限制 → 真的去解析 → 撞上预热窗口 → 崩。

**修复（已提交）**：把这条回调纳入 `parseMu` 的纪律，但**不能直接加锁**——回调还有一条
**同步**路径（`link.Resolver.Start`：没有全局模组时立即就绪并同步回调，此时调用方
`maybeReloadLinks → reparseFull → handle` **已持有 `parseMu`**，直接加锁会自锁死，
实测让两个链接测试挂起）。最终实现：

| 函数 | 语义 |
|---|---|
| `requestRediagnose()` | 供回调使用：**永不阻塞**，丢到独立 goroutine，等 `parseMu` → 拿锁重算（并检查 shutdown） |
| `rediagnoseAllLocked()` | 供已在 `parseMu` 内的调用方使用（`reparseFull` 遇到资料重载时） |

回归测试（`internal/server/lock_test.go`）：① 持锁期间调用必须立即返回（防自锁死）；
② 持锁期间**不得**发生重诊断，释放后必须补发一轮；③ `Locked` 版本不得再次加锁。

**验证**：修复前 `scripts/repro-index-crash.py` 8/8 崩溃；修复后 **20/20 全部 OK**
（含 4.5 万行的 `saapt110.4gl`），`go test ./...` 全绿，
`go test ./internal/server/ -run TestRequestRediagnose` 覆盖锁纪律。

### 2.3 还剩的加固项

1. **CI 回归**：复现脚本依赖真实语料（CI 没有），需要在 CI 里补一条**合成工作区**的等价测试
   （造 `.link/links.jsonl` + 多个"全局模组"文件把预热窗口拉开 + 一个大文件），
   或者把语料抽样放进 CI 缓存。
2. **同类路径审计**：现在只有这一条异步回调会"从外部改文档树"。审计结论写进代码注释
   （`parseMu` 的说明），后续新增任何后台 → 文档树的路径都必须走 `requestRediagnose` 一类入口。
3. 明确 **工作区索引是否该跳过超大文件**（它现在解析一切，包括 `limits` 已排除诊断的文件）——
   属于性能优化，不是本次崩溃的原因。

### 2.4 产品要求（P0-3）· **已实现 2026-10-23**

用户明确要求：**因配置而没解析的文件要告知用户，并在文件右上角给一个"强制解析"按钮**。

| 情形 | 实现（2026-10-23） |
|---|---|
| 超过 `maxFileBytes` / `maxFileLines` | 发 `file-too-large`，默认级别 **information → warning** |
| 解析**超时**（`limits.parseTimeoutMs`） | 新增诊断码 **`parse-timeout`**（默认 warning），消息给出两条出路；强制解析仍超时时消息改为"建议提高上限" |
| 服务崩溃/重启 | 仍靠 P0-1 的修复保证不崩；插件侧的异常退出提示留到 P1 一并做 |

**实现**（与当初设想的差异：不用 `executeCommand`，改成自定义请求，语义更直白）：

| 位置 | 内容 |
|---|---|
| 服务端 | `const MethodForceParse = "bdl/forceParse"`（params `{"uri":…}`）→ 记入 `forceParse` 集合 → 立即 `reparseFull`；`reparse`/`reparseFull` 对集合内的文件跳过 `tooLargeReason` |
| 插件 | 命令 `bdl.forceParseCurrentFile` + `editor/title` 按钮（codicon `$(refresh)`），`when: bdl.parseSkipped && (resourceLangId == bdl \|\| per)` |
| 按钮何时出现 | 监听诊断变化，该文件出现 `file-too-large` / `parse-timeout` 时才 `setContext("bdl.parseSkipped", true)`——不为每个 `.4gl` 挂一个没用的图标；解析成功后按钮自动消失 |
| 有效期 | `didClose` 时移除标记（用户确认的语义）；关闭再打开又回到"按上限跳过" |
| 测试 | `internal/server/forceparse_test.go`：超限→提示→强制解析（大纲非空、提示消失）→关闭再打开提示回来；超时→`parse-timeout` 提示 |

---

## 3. P0-2：表单侧名字被误报为"变量不存在"（已修复）

### 3.1 用户报的

```4gl
INPUT ARRAY g_sgm WITHOUT DEFAULTS FROM s_sgm.*
INPUT ARRAY g_sfa WITHOUT DEFAULTS FROM s_sfa.*
INPUT ARRAY g_tc_shb WITHOUT DEFAULTS FROM s_tc_shb.*
INPUT ARRAY g_tc_shb_2 WITHOUT DEFAULTS FROM s_tc_shb_2.*
```

`s_sgm` / `s_sfa` … 是**画面的表格（SCREEN RECORD / screen array）名**，却报 `variable-source-unresolved`。

### 3.2 证据（真实语料 + 语法树）

在 `tiptop/aap/4gl/saapt310.4gl` 上跑服务端，得到 4 条同类误报：`s_apd`、`s_apg`、`s_aph`、`azp02`。
语法树实测（`tree-sitter parse`）——同名 capture `field:` 在不同语句里落到**不同的字段名**：

| 写法 | 树结构 | 现有 skip 覆盖？ |
|---|---|---|
| `INPUT x FROM f01` | `(input field: (variable f01))` | ✅ `(input field: …)` |
| `INPUT g_x FROM s_rec.*` | `(input field: (variable (object (variable s_rec))))` | ✅ 起始字节相同，被 skip 覆盖 |
| **`INPUT ARRAY g_arr FROM s_arr.*`** | **`(input record: (variable s_arr))`** | ❌ **漏了** → 误报 |
| **`CONSTRUCT g_wc2 ON … FROM s_apg[1].apg02,…`** | CONSTRUCT 的 **FROM 侧** | ❌ 只在 `ON` 侧跳过 |
| **`CLEAR azp02`**（formonly 字段） | `clear` 语句 | ❌ 没有跳过 |
| `DISPLAY ARRAY g_sgm TO s_sgm.*` | `(display record: (identifier s_sgm))` | ✅（是 identifier，不匹配 `(variable …)`） |

本质：`variable-source-unresolved`（T5）用的是**语法上下文白名单**，而"表单侧的名字"能出现在**很多**语句
的很多字段位置——枚举不完，且每加一条 pattern 都有"Impossible pattern 静默禁用整个 query"的风险（本项目踩过）。

### 3.3 已实施的修复

1. **语法跳过**：`queryVariables` 新增 `(input record: (variable) @skip)`
   ——`INPUT ARRAY … FROM s_rec.*` 的屏幕记录名落在 `record:` 字段（不是 `field:`）。
   另把 `CLEAR <x>` 的操作数整体跳过：Genero 不区分"程序变量 / 表单字段"，
   静态无法判定，按"宁可不报"处理（真实误报见下）。
2. **语义兜底**：`form.Resolver.FormSideNames()` 汇集**本文档引用到的表单**里的
   表单侧名字（item tag、SCREEN RECORD 及其字段、`= table.column` 的**列名**），
   T5 命中这些名字就不再报（`DISPLAY t5field`、`CLEAR s_rec` 这类靠它兜住）。
   `reparseFull` / `AuditFile` 都改成"先跑字段校验拿表单集合，再跑 T5"。
3. **回归测试**：`internal/server/t5form_test.go` 两条——表单侧名字（含只有列名的
   `t5field`、没有表单绑定的 `INPUT ARRAY`）必须静默；真正未定义的程序变量仍要报。

**语料实测**（`make diag-audit`，全量 15108 文件）：

| 指标 | 修复前 | 修复后 | 差值 |
|---|---|---|---|
| `variable-source-unresolved` | 24284 | **19580** | **−4704（−19.4%）** |
| 诊断总数 | 26707 | 22003 | −4704 |
| `function-not-found` / `field-not-in-form` | 1423 / 869 | 1423 / 869 | 0（无副作用） |
| `saapt310.4gl` 的误报 | 4 条 | **0 条** | −4 |

### 3.4 还没做的（**需要你确认语义**）

剩余 19580 条 T5 的主体**不再是表单名字**，而是**跨模块的模块级变量**，例如
`L_AC`（1828 条）、`G_RVV`（1715）、`G_SFS`（1128）——这些名字在语料里确有
`DEFINE`，但定义在**别的模块**的文件里（不是当前文件、也不在它 import 的 GLOBALS 里）。

它们算不算误报，取决于 Genero 的可见性模型：

- 若"模块级 `DEFINE` 不跨模块可见"（Genero 手册的常规理解）→ 这些是**真报**，工具没错；
- 若这套项目的构建方式让它们跨模块可见（共享模块编译成库、老式 4GL 习惯）→
  T5 需要按"模块/库"聚合可见性，那会是一次更大的改动（类似 P7-D 的链接可见性矩阵）。

**建议**：先别动，等你在真实项目里看一眼这 1.9 万条是否构成噪声；如果需要再开一项。

### 3.5 原方案（保留备查）

1. **补语法跳过**（便宜）：`(input record: (variable) @skip)`；`clear` 语句；
   `construct` 的 FROM 侧（按位置判断，与 `constructOnColumns` 同思路）。
2. **语义兜底**（治本）：一个使用处的名字若**命中本文档所引用表单的 item 名或 SCREEN RECORD 名**，
   就不报 T5——这些名字在 Genero 里本来就不是程序变量。表单索引已经存在
   （`internal/form` 的 `indexLookup`/`PerIndex`），复用即可，成本可控。
3. **回归基线**：
   - 单测：上面 4 种写法的 fixture，断言"不产生 T5"；
   - 语料：`saapt310.4gl` 的 4 条误报归零，且**全语料 T5 总数不上升**（`make diag-audit` 对比）。

### 3.6 注意

`INPUT BY NAME g_rec.*`（`g_rec` 是**程序 record**）走的是 `value:` 字段，**继续校验**是对的——
不要为了省事把整个 `input` 语句都跳过。

---

## 4. P1-1：打包与命令入口

### 4.1 事实核查（先纠正一个前提）

- **平台包确实内置了两个二进制**：`scripts/package-vsix.mjs` 会把
  `bdl-lsp(.exe)`、`tiptop(.exe)` 复制到 `bin/`，`.vscodeignore` **没有**排除 `bin/`，
  win32 目标还会把 MinGW 运行库 DLL 一起打进去；`bin/` 在 `.gitignore` 里（不入库，由打包脚本生成）。
- 会"看起来依赖本地程序"的情况有三种，需要分别处理：

| 情况 | 现象 | 处理 |
|---|---|---|
| 装的是 **dev 包**（`npm run package:dev` / 早期 artifact） | 没有 `bin/`，回落到 PATH/工作区/设置里的二进制 | 文档写清"dev 包不含二进制"；命令报错文案指向"装平台包" |
| 装了平台包但**平台不匹配**（x64 包装在 arm64 上） | 解析失败 | 提示里带上"包与系统/芯片不匹配，重装对应包" |
| 用 `bdl.server.path` 指了本地二进制 | 看起来"依赖本地程序" | 保持可用，但 `显示语言服务器信息` 要如实显示来源 |

- **新增 CI 自检**：`bundle-vsix.yml` 在打包后断言 VSIX 内确实有 `extension/bin/bdl-lsp.exe`
  与 `extension/bin/tiptop.exe`（现在只校验打包成功，不校验内容）。

### 4.2 命令入口（用户要求："从 VSCode 中启动，而不是让用户找可执行文件"）

新增/补齐命令（`contributes.commands` + 命令面板 + 视图按钮）：

| 命令 | 作用 | 归属 |
|---|---|---|
| `bdl.link.importCsv` | 选 CSV → `tiptop link import` → 落 `.project/`（覆盖源文件） | 插件 |
| `bdl.link.syncAll` | 全量 `tiptop link sync`（需要 DB 配置） | 插件 |
| `bdl.link.syncCurrent` | 只更新**当前打开文件**相关的链接资料（§6） | 插件 |
| `bdl.link.syncModules` | 选 lib/sub/qry 等模块更新（§6） | 插件 |
| `bdl.link.status` | 打印资料统计到输出通道 | 插件 |
| `bdl.links.refresh`（已有） | 重新加载资料 | 插件 |
| `bdl.links.resync`（新增，用户点名） | 链接资料视图里"反查引用程序"处的**重新同步** | 插件 |
| `bdl.deploy.*`（已有 4 条） | 部署（配置来自设置，见 §5.3） | 插件 |
| `bdl.forceParseCurrentFile` | 强制解析当前文件（§2.4） | 插件 |

配套：
- `activationEvents` 现在只有 `onLanguage:bdl|per` → **命令在没打开 `.4gl` 时不会激活**，
  必须补 `onCommand:` 系列（或 `onStartupFinished` + 命令条件）。
- **没配置数据库时**：这些命令不是静默失败，而是给出明确提示（"还没有配置数据库连接：
  打开设置 → Genero BDL → 生产环境，或在 `.project/secrets.json` 写密码"），并提供
  "打开设置"按钮（`vscode.openSettings` 带 query）。
- 所有会写盘的命令，写之前给一次确认（"将写入 `.project/links.jsonl`，是否继续"），
  避免用户在不知情时改动项目。

### 4.3 稳定性提示

`显示输出` 里要有：语言服务器**异常退出**（非 0 退出码）时的明确日志 + 自动重启一次 + 通知用户。
配合 §2.1 的崩溃修好后，这条是"兜底可见"。

---

## 5. P1-2 / P1-5：`.project` 目录与设置重组

> P1-2（目录迁移）**已实现 2026-10-23**，见下面的「已实现」小节；
> P1-5（设置重分组）待做。

### 5.1 新目录布局

```text
<项目根>/
└── .project/                 # 唯一的项目级资料目录（原 .link 迁到这里）
    ├── links.jsonl           # 链接资料（原 .link/links.jsonl）
    ├── links.meta.json
    ├── gal_file.csv          # 导入用的 CSV（导入后**覆盖**这份，不再散在别处）
    ├── secrets.json          # 数据库密码（0600；Windows 见 PENDING F7）
    ├── deploy.json           # 部署目标：**由插件从设置生成**，用户不手写
    └── .schema/              # 数据库字段资料（发布后一版才用，先占位）
```

#### 已实现（2026-10-23）

| 位置 | 实现 |
|---|---|
| 默认值 | `link.DefaultDataDir` / `link.dataDir` 配置默认值 / 插件设置默认值 全部 `.link` → **`.project`** |
| 读兼容 | `newLinkResolver`：配置仍是默认值、`.project/links.jsonl` 不存在而 `.link/links.jsonl` 存在 → 读旧目录（**不写**），并记下 `legacyDir` |
| 提示 | 新诊断码 **`link-data-legacy-dir`**（hint，挂在**旧目录**的 `links.jsonl` 上）：说明资料在旧目录、可用命令迁移 |
| 迁移命令 | 插件 `bdl.migrateLinkDir`：确认后把 `.link/` 下的文件搬到 `.project/`（不覆盖已有 `links.jsonl`，不动用户文件），随后自动重启语言服务器 |
| 主动提示 | 插件监听诊断，看到 `link-data-legacy-dir` 时弹一次"迁移 / 稍后"——不用用户去翻 Problems |
| 测试 | `internal/server` 新增 `TestLegacyLinkDirStillReadWithHint`（旧目录资料**照旧生效** + 提示出现）；e2e 的命令清单纳入 `bdl.migrateLinkDir` |
| 文档 | tiptop/bdl-lsp/插件的当前文档与脚本（含 `fetch-gal-file.sh` 的默认输出路径）全部改为 `.project/`，并注明旧目录兼容 |

约定（与设计一致）：**读** `.project/` 优先、旧 `.link/` 兼容；**写**只写 `.project/`。
- `.project/` 里的 `secrets.json` / `deploy.json` 是**插件自己生成的内部文件**（用户不再配置它们），
  因此**不做任何"加 .gitignore"提示**，也不动用户文件（旧决定 `E8` 保持）。
- 涉及改动：`bdl-lsp`（`link.DefaultDataDir`、`config` 默认值、诊断消息文案）、
  `tiptop`（CLI 默认目录、secrets/csv 落点）、插件（默认 `dataDir`、新建目录的时机）。

### 5.2 设置重分组（5 类）

| 新分组（settings 里的显示名） | 旧键 → 新键 | 默认（轻量档） |
|---|---|---|
| **1. 基础**（`basic`） | `generoVersion`、`logLevel`、`trace`、`diagnostics.*`、`format.*`、`limits.*`、`ignore.*`、`index.*`、`builtins.*`、`globals.*`、`perRoots` | 保持现在的默认（零配置可用） |
| **2. 生产环境**（`prod`，**新增**） | 新：`prod.oracle.host/port/service/user`、`prod.deploy.host/user/remoteRoot/...`；密码不进设置 | 空（不连、不提示） |
| **3. 链接资料** | `link.enable`（**默认 false**）、`link.dataDir`（默认 `.project`）、`link.moduleRoots`、`link.globalModules` | 关闭 |
| **4. 部署** | `deploy.enable`（**默认 false**）、`deploy.target` | 关闭 |
| **5. 数据库字段**（发布后） | `schema.enable`（默认 false）、`schema.dataDir`（`.project/.schema`） | 关闭 |

设置面板的注意点：
- 组的标题不要重复"Genero BDL："前缀（面板本身已经分组显示）——用户点名要求。
- **生产环境组要有一条醒目警告**：*这些值请写在 **Workspace** 设置里；
  并且**不要让 `settings.json`（无论工作区的还是 `~/.vscode/settings.json`）进 git 管理***
  （密码类的值本插件一律不放进设置，见 §5.3；但主机名/账号仍不该提交）。
- `link.enable=false` 时，链接相关命令用 `when` 隐藏（§4.2）。

### 5.3 "不要文件配置"怎么落地（已确认方案）

用户只改 **VSCode 设置**；插件是 `.project/*.json` 的**唯一写入者**：

- 部署：设置 → 插件合成 `.project/deploy.json`（tiptop 的配置模型 `targets/upload/compile` 不变）
  → 调 `tiptop deploy -config .project/deploy.json`。
- 数据库：设置 → 插件拼 `tiptop link sync -host … -service … -user …`（密码从
  `.project/secrets.json` 或环境变量取，**永不进 settings**）。
- 好处：tiptop 不用改配置模型；用户侧不出现"手写 JSON"。
- 风险：合成文件是"插件与 tiptop 的契约"，需要版本标记（文件里写 `"createdBy": "vscode-bdl 0.6.x"`），
  并在插件里检测"用户手改过"的情况（mtime 比对）给出提示。

---

## 6. P1-3：link 局部同步（新参数 + 合并）· **已实现 2026-10-23**

### 6.1 现状

`tiptop link sync`（`-source golang|command|file`）每次都是**全量**：查 `DS.GAL_FILE` 全表 →
重新扫全语料判定 MAIN → **覆盖** `links.jsonl`（实测 ~24 s / 7600 文件）。

### 6.2 新参数（语义）· 已实现

实现位置：`bdl-lsp/link`（选择/合并的纯逻辑）+ `tiptop`（CLI 与 SQL 下推）。

| 项 | 实现 |
|---|---|
| `-modules lib,sub,qry` | 命中 CSV 的 `gal02`（大小写不敏感） |
| `-files aap/4gl/saapt310.4gl` | `link.ParseFileSpec`：最后一段去扩展名做代码，第一个非类型目录段做模组；也接受 `aap/saapt310`、`saapt310.4gl`、反斜杠 |
| 受影响作业 | 上述条件命中行的 `gal01` 集合（**并集**）；一层公共文件被多个 MAIN 引用时，这里就是"N 个作业" |
| 采集下推 | `source=golang` + 默认 SQL 时追加 `AND (UPPER(gal02) IN … OR (UPPER(gal02),LOWER(gal03)) IN …)`；自定义 `-query` 不做下推（只在内存过滤） |
| 只扫受影响文件 | `link.ScanPathList` 只读这些程序的文件（几十个），全量 20 多秒 → **亚秒级** |
| 合并 | `mergeChains`：受影响的程序用新链替换，其余链（含 orphan）原样保留 |
| meta | `partial{updatedAt, modules, specs, programs, chainsUpdated}`；`generatedAt` 保持上一次全量的时间（局部更新不能代表全部内容） |
| 前提 | 必须有全量基线，否则报错提示先跑全量（`ErrNoData` 路径） |
| 报告 | `PartialImportOutcome.Report`："受影响作业 N 个（名字…）/ 本次重算链 / 保留旧链 / 范围内有效行"；`-dry-run` 只打印不写盘 |
| `link status` | 显示"局部更新 时间：作业 N 个 / 重算链 M 条 / 模组|文件 …（其余链仍是上一次全量导入的结果）" |
| 测试 | `link/partial_test.go`（选择、文件规格解析、合并、无基线报错）、`linksync/filter_test.go`（SQL 子句与只对默认 SQL 下推） |

**实测**（临时工程 / 3 个作业，其中一个公共文件被 2 个作业引用）：

```text
$ tiptop link import -csv links.csv -root /tmp/p13ws -files aap/4gl/saapt310.4gl -dry-run
文件条件        aap/4gl/saapt310.4gl
受影响作业      2 个（PROG1, PROG2）
本次重算链      2 条
保留旧链        2 条
范围内有效行    7（范围外跳过 3）
```

### 6.3 原参数说明（保留）

```text
tiptop link sync                      # 无参：全量（保持现状）
tiptop link sync -modules lib,sub,qry # 只更新这些模组（gal02）的链接
tiptop link sync -files aap/4gl/saapt310.4gl[,cxm/4gl/x.4gl]  # 只更新这些 4gl 的链接
```

- `-files` 解析 `<模组>/<类型目录>/<文件名>.4gl` → 模组 `AAP`、程序码 `saapt310`（大小写不敏感）；
  **匹配到多个程序就全部更新**。
- SQL：`gal04='Y' AND (UPPER(gal02) IN (…) OR (UPPER(gal02),LOWER(gal03)) IN (…))`。
- **合并语义**（关键）：读现有 `links.jsonl` → 只重算受影响程序的链 → 其余链原样保留 →
  meta 里记 `partial: {"modules":[…],"files":[…],"updatedAt":…,"rowsTotal":…}`。
  这样单文件更新是**秒级**，而不是 24 s。
- **连带范围（用户 2026-10-23 确认）**：
  - 指定的是 **MAIN 程序**的文件 → 它自己只属于一条链 → 直接更新那一条；
  - 指定的是 **lib/sub/qry（全局模组）**下的文件 → 更新它自己所在的那部分；
  - 指定的是**被多个 MAIN 引用的公共文件**（如 `saapt310.4gl`）→ 先把受影响的作业
    **列出来并计数**，提示"将更新 N 个作业（程序）"，**用户确认后再写盘**；
  - 无论哪种，都只更新显式指定集合所覆盖的链，meta 里注明"局部更新"与时间。

### 6.4 与插件配合

| 入口 | 行为 |
|---|---|
| 链接资料视图 → 「重新同步」（用户点名放在"反查引用程序"旁） | 对**当前文件**执行局部同步 |
| 命令 `链接资料：更新当前文件` | 同上（当前编辑器文件 → `-files`） |
| 命令 `链接资料：按模组更新` | 弹出多选（lib/sub/qry/当前模组…）→ `-modules` |
| 命令 `链接资料：全量更新` | 无参 `sync`（明确提示"会耗时 ~24 s，确认？"） |
| 命令 `链接资料：从 CSV 导入` | 选文件 → 复制到 `.project/gal_file.csv` → `link import` |
| 命令 `链接资料：查看状态` | `link status` → 输出通道 |

### 6.5 去掉的东西（用户明确要求，属 P1-6）

- **`link.maxAgeDays` 配置 + `link-data-stale` 诊断**：删掉（"7 天失效"这套不再用）；
- **后台自动同步**：`onStart`/`interval` 相关字段与实现删掉（`internal/linksync.Config` 里的
  `Enable/Interval/OnStart`），资料何时更新由用户点命令决定。
- 文档/设置里同步删除，避免"看起来还有这个功能"（这正是 §A3 那类残留的根源）。

---

## 7. P2-1：schema（发布后一版做）

> 用户明确：**schema 功能放到正式发布之后一个版本**；本节只留设计，不排进发布前待办。
> 数据来源附件（Oracle 导出，6 万多行）暂未拿到，格式按下面设计先定，拿到再校准。

### 7.1 数据来源与模型（`.project/.schema/`）

**样例数据（用户已提供，不入库）**：`bdl-lsp/artifacts/schema/demo.csv`
（Oracle 导出，58,100 行 / 3.1 MB，放在 `artifacts/` 下由 `.gitignore` 忽略）。
表头实测为：

```text
table_name,column_name,column_desc,data_type,data_length,char_length,data_precicison,data_scale,nullable,default_legth
```

注意表头有两个拼写错误（`data_precicison`、`default_legth`）——**导入器必须容错**
（按位置或别名匹配，不要按严格列名）。校准时只读前 1000 行。

从用户给的 SQL 看，每条记录是：

```
table_name, column_name, column_desc(中文说明), data_type,
data_length, char_length, data_precision, data_scale, nullable, default_length
```

存储设计（性能优先，与 link 的资料分离）：
- 一个**总索引** `.project/.schema/tables.json`：`{table: {desc?, columns: N, file: "…"}}`；
- 每表一个文件 `.project/.schema/tables/<TABLE>.json`（列数组 + 中文说明）；
- 这样 hover 只需要读一个表文件，视图展开也只读一表；
- 导入：`.project/gal_schema.csv`（或用户选的 CSV）→ 生成上述结构；
  记录 `rowsTotal/importedAt/source`；
- 与旧 `.sch` 的关系：**不再读各模组下分散的 `sch/*.sch`**（用户明确不要），
  `schema.paths` 之类配置随之下线。

### 7.2 同步策略（发布后）

- 全量：查全表 → 与本地逐表 diff → 只重写变化的表文件（并在输出里给出"新增/变更/删除"计数）；
- 选表：视图里多选表 → 只更新这些表（右键菜单）；
- 只新表：`-only-new` → 只写"本地不存在的表"（不碰已有表，避免覆盖人工修订过的说明）。

### 7.3 视图与查询

- 独立的「数据库字段」视图（与「链接资料」并列，各自有启用总开关）；
- 顶部查询框：支持**正则**匹配表名（如 `^APF` / `(apg|aph)0`)；
- 树结构：表 → 字段（字段**展开才加载**，避免 6 万行一次渲染）；
  字段节点显示 `列名 · 类型(长度) · 中文说明`；
- 右键表/字段：`同步选中的表`、`复制列定义`、`在 schema 文件中打开`；
- 与 link 一样：**不后台静默同步**，所有同步都是点击触发，跑完给输出摘要。

### 7.4 悬停扩展（发布后）

| 场景 | 现状 | 发布后 |
|---|---|---|
| `DEFINE x LIKE ima_file.ima01` | 显示 4GL 类型 | 追加：`ima01 · CHAR(10) · 物料编号`（schema 里没有就**不报错**，只少这一段） |
| `DEFINE r RECORD LIKE ima_file.*` | 只显示记录名 | 悬停列出该表**全部字段**（列名 · 类型 · 中文说明），超过 N 行折叠 |
| `rec.` 补全 | 有 schema 时给出列 | 补全项带上中文说明（`detail`/`documentation`） |

### 7.5 分工（用户给的边界）

- **产出数据**（连库/解析 CSV/生成 `.schema`）：**tiptop**（与 link 一致）；
- **语法分析能放插件侧的就放插件侧**：`LIKE t.col` 这种**纯语法 + 查表**的 hover 扩展由插件/bdl-lsp 直接做
  （数据是死的，不涉及连库）；
- 任何"需要写盘/覆盖项目资料"的分析，**跑完让用户二次确认**再落盘。

---

## 8. P1-7：文档

1. **用户文档以 `vscode-extension/docs/USER-GUIDE.md` 为准**：
   - 按本计划的最终形态重写（三档模式、5 组设置、`.project` 目录、命令清单、常见问题）；
   - 然后**把它作为插件说明**（`vscode-extension/README.md` 指向/内联用户指南），
     市场说明与 README 不再各写一份；
   - 时机：**等功能与设置定稿后再写**（否则写完即过期），放在发布前最后一步。
2. **截图/录屏位**：`docs/images/vscode/README.md` 列了要补的图，按新 UI（两个视图、命令面板）更新清单。
3. **本次清理**（见 [PENDING](PENDING.md) §P 的已完成记录）：
   - 删：`TODO.md`、`PLAN-DETAILED-P1.md`、`PLAN.md`、全部 `PLAN-P*.md`、`DEPLOY-ZED.md`；
   - 保留：`PENDING`、`HANDOVER`、`FEATURES`、`TESTING`、`PLAN-SEPARATION`、
     `WINDOWS-CHECKLIST`、`DIAGNOSTICS`、`ENVIRONMENT`、`TECH-STACK`、`RESEARCH`、`EDITOR-SUPPORT`、
     `vscode-extension/docs/{USER-GUIDE,PROJECT-SETUP,RELEASE}.md`、zed 文档（暂停中，不动）。

---

## 9. 已确认 / 待确认

**已确认（用户 2026-10-23）**：

1. `.project/secrets.json`、`.project/deploy.json` 是插件内部文件，**用户不需要配置、不做提示**；
   但**设置里要醒目提示**：生产环境类配置只写 workspace 设置，且 `settings.json` 不要进 git 管理。
2. 局部同步的连带语义：MAIN → 只更新自己那条链；lib/sub/qry → 更新自己；
   **被多个 MAIN 引用的公共文件 → 列出并计数受影响的作业，用户确认后再更新**。
3. "强制解析"有效期：**文件不关闭就一直生效**（关闭时失效）。
4. schema 样例数据已放到 `bdl-lsp/artifacts/schema/demo.csv`（不追踪）。

**待确认**：schema 数据正式导入时，按前 1000 行校准列语义（尤其 `data_length/char_length`
在 VARCHAR2/NUMBER 上的取舍）——发布后一版再定，不阻塞发布。

## 附录 A：被删文档里"仍然有效"的决策（抢救）

> 删除阶段记录时把下面这些结论搬进本附录，避免以后重新摸索。

- **P3a GLOBALS**：作用域必须**按函数隔离**（整文件线性跟踪会让表单/全局引用跨函数泄漏，
  实测 2.8 万条误报 → 隔离后归零）；`searchPaths` 只补解析根，不做 basename 隐式匹配。
- **P3b 字段校验**：`CONSTRUCT … ON` 后面是**数据库列名**不是表单字段（4456 处误报的来源）；
  record `.*` 校验**默认 off**（`LIKE table.*` 占引用 87%，开启后全语料仅 24 条但仍偏噪）。
- **P4 格式化**：`.per` **不格式化**；格式化遇到 ERROR 返回空编辑并记录原因（不改坏文件）。
- **P5 部署**：传输 rsync 主 / scp 备；回退必须**显式打印**（静默降级会让用户以为在用 rsync）。
- **P6 索引**：缓存用 `(mtime,size)` 而不是内容哈希（哈希会把冷启动从 1 s 拖到 10 s+）；
  **跨文件重命名刻意不做**（索引里的调用点是名字匹配，可能命中同名局部变量，漏改比不支持更糟）。
- **P6 record 成员**：`LIKE t.*` 的成员校验依赖 `.sch`（现在要换成 `.schema`）；
  schema 缺失时**静默**（宁漏勿误）。
- **P7 链接资料**：可见性三条铁律——本文件 ∪ GLOBALS ∪ 链内文件 ∪ 全局模组（`sub/lib/qry` 扁平规则，无矩阵）；
  **只放大函数，不放宽变量**；没有资料时 `function-not-found` **一律不报**。
- **P7 降噪**：诊断级别统一走 `bdl.diagnostics.<码>` 注册表，专用旧键（`fieldDiagnostics` 等）已删除；
  注册表 ↔ 文档 ↔ `package.json` 三处一致性由 `make diagnostics-check` 保证。
- **grammar 硬规矩**：改 `scanner.c` 后必须 `go build -a ./internal/parser`（Go 不跟踪被 include 的 C 文件）；
  每个 query pattern 的字段顺序必须与 grammar 一致，写错会"Impossible pattern"**静默禁用整条 query**。
- **分离（0.3.2）**：`link`/`deploy` 的**采集与部署**归 tiptop，bdl-lsp 只读 `.link`/`.project` 资料；
  `deploy` 不是 bdl-lsp 的子命令；`link status` 在 tiptop。
- **schema（原 P6）**：解析 `.sch` 是**手写** DDL 解析（不用 tree-sitter）；`schema.Merge` 曾与文件顺序相关
  （ALTER 先到会被 CREATE 换表丢掉列），修法是"并集合并 + 顺序无关回归测试"。

## 附录 B：崩溃复现脚本（现在当手测，修好后转回归测试）

脚本：[`scripts/repro-index-crash.py`](../scripts/repro-index-crash.py)（退出码 0=OK / 1=崩，直接可进 CI）。

```bash
cd bdl-lsp && source scripts/dev-env.sh

# 1) 全量语料 + 打开 4 万行文件 → 现状 CRASH（退出码 1）
python3 scripts/repro-index-crash.py

# 2) 对照：同样的文件，但工作区只含它一个文件 → 现状 OK（退出码 0）
mkdir -p /tmp/tinyws/aap/4gl
cp /code/workspace/tiptop-gp/tiptop/axm/4gl/saxmt600.4gl /tmp/tinyws/aap/4gl/
python3 scripts/repro-index-crash.py /tmp/tinyws /tmp/tinyws/aap/4gl/saxmt600.4gl
```

2026-10-23 实测：全量语料 **8/8 崩溃**（1.3 s，退出码 2，SIGSEGV in `ts_node_parent`）；
单文件工作区 **0/8 崩溃**（0.8 s 正常返回）。两者差别只有"后台索引有没有在跑"。