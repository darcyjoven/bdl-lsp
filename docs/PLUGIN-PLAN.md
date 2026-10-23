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
| P0-1 | **BUG（严重）** | 后台索引与请求处理并发 → tree-sitter 崩溃、服务整个挂掉（大文件 100% 复现） | bdl-lsp | **最高**（发布前） |
| P0-2 | BUG | `INPUT ARRAY … FROM s_x.*` 等表单侧名字被报"变量不存在" | bdl-lsp | 高（发布前） |
| P0-3 | **UX** | 文件因大小/超时/未解析时不告知用户；要加"强制解析"入口 | bdl-lsp + 插件 | 高（发布前） |
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

## 2. P0-1：并发导致 tree-sitter 崩溃（本次分析的最大发现）

### 2.1 用户看到的现象

> "有些文件的 outline 分析不出来，语法错误解析好像也没进行"、"把最大行数/字节数改大 10 倍也没用"。

### 2.2 复现与证据（2026-10-23 实测）

环境：Linux 沙箱，`bdl-lsp 0.3.4`，语料 `/code/workspace/tiptop-gp`。

| 场景 | 结果 |
|---|---|
| 默认限制（2 MB / 20000 行），打开 `tiptop/axm/4gl/saxmt600.4gl`（41486 行 / 1.7 MB） | 不崩；发一条 `file-too-large`(**information**) 提示；**大纲 0 个符号** |
| 限制放宽到 20 MB / 200000 行，打开同一文件 | **服务端 SIGSEGV，退出码 2**（1.3 s 内），**8/8 次全崩** |
| 同一文件、**只把工作区换成只含该文件的临时目录**（索引只有 1 个文件） | ✅ 正常（0.8 s 返回诊断） |
| 同一文件、不通过 LSP（直接调用 `form.Check`） | ✅ 正常（`issues=5, paths=21`） |

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

**结论**：崩溃与"文件多大"只有间接关系——**真正的条件是"后台工作区索引正在跑"**：

- 全量语料（7756 个文件，索引要跑 ~25 s）+ 大文件（handler 侧 tree-sitter 工作要 ~1 s）
  → 两者重叠窗口足够大 → **tree-sitter 状态被并发访问**，`ts_node_parent` 读到已失效的子树 → SIGSEGV；
- 只含一个文件的临时工作区 → 索引瞬间结束 → 不重叠 → 正常。

这解释了用户的三条现象：默认限制下大文件被跳过（没大纲）；放宽限制后**服务崩了**（连语法诊断也没有了，
注意 VSCode 会自动重启语言服务器，用户看到的就是"有些文件一直出不来"）。

**为什么 `parseMu` 没挡住**：`parseMu` 只串行化了 *请求处理* 与 *防抖重算*（P7-B 那次 `ts_query_cursor_next_match`
崩溃的修法）。**后台工作区索引（`internal/wsindex` 的 worker 池）从未纳入这个互斥**，它与 handler 并发使用
同一个 `parser.Pool`，并且和 handler 共享编译好的 query（`index.CallCollector` 等）。

### 2.3 修复方向（发布前必须做）

1. **把 tree-sitter 的所有使用纳入同一个保护**，两种实现选其一：
   - **(a) 全局串行**：给 `parser.Pool` 的 Parse/Query 使用加一把进程级互斥（索引 worker 也走它）。
     代价：冷启动索引变慢（需实测：当前 7756 文件 24.7 s，串行化后可能 60 s+）。
   - **(b) 隔离资源**（推荐）：索引侧用**自己的 Pool 实例**与**自己的 Query/Collector 实例**，
     不共享任何 tree-sitter 对象；同时复核 `index.CallCollector` 是否被 handler 与索引同时使用。
2. **加回归测试**：用语料里的 4 万行文件 + "先全量索引再打开该文件"的顺序做集成测试，
   当前能 8/8 复现；修好后必须 0/8。
3. **顺手加兜底**：崩溃兜底（进程级 `recover` 挡不住 SIGSEGV，只能靠不崩 + 索引隔离；
   插件侧可以检测"语言服务器异常退出"并提示 + 自动重启，见 §4.3）。
4. 明确 **索引是否该跳过超大文件**（现在是"全量解析一切"，即使 `limits` 已经把它排除在诊断之外）——
   建议索引也遵守一个（更大的）阈值，并把跳过的文件数写进日志。

### 2.4 顺带的产品要求（P0-3）

用户明确要求：**因配置而没解析的文件要告知用户，并在文件右上角给一个"强制解析"按钮**。

| 情形 | 现状 | 应改为 |
|---|---|---|
| 超过 `maxFileBytes` / `maxFileLines` | 发 `file-too-large`（information，容易被忽略） | 保持诊断，但级别建议 `warning`；**同时**在编辑器标题栏出现 "强制解析" 按钮 |
| 解析**超时**（`limits.parseTimeoutMs`） | **只写日志**，用户什么都看不到 | 发同类诊断："解析超时，已跳过；可提高 `basic.limits.parseTimeoutMs` 或点强制解析" |
| 服务崩溃/重启 | 无感（用户只看到功能时有时无） | 输出通道 + 通知："语言服务器异常退出，已重启（详情见日志）" |

"强制解析"实现：命令 `bdl.forceParseCurrentFile` + `editor/title` 菜单（`when: resourceLangId == bdl || per`），
行为 = 对该 URI 关闭本次阈值/超时限制重解析一次（用 `workspace/executeCommand` 通知服务端，
或客户端临时把该文件加入白名单）。**只对当前文件、只到下次编辑为止**，避免变成"永久绕过"。

---

## 3. P0-2：表单侧名字被误报为"变量不存在"

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

### 3.3 修复方案（三层，建议全做）

1. **补语法跳过**（便宜）：`(input record: (variable) @skip)`；`clear` 语句；
   `construct` 的 FROM 侧（按位置判断，与 `constructOnColumns` 同思路）。
2. **语义兜底**（治本）：一个使用处的名字若**命中本文档所引用表单的 item 名或 SCREEN RECORD 名**，
   就不报 T5——这些名字在 Genero 里本来就不是程序变量。表单索引已经存在
   （`internal/form` 的 `indexLookup`/`PerIndex`），复用即可，成本可控。
3. **回归基线**：
   - 单测：上面 4 种写法的 fixture，断言"不产生 T5"；
   - 语料：`saapt310.4gl` 的 4 条误报归零，且**全语料 T5 总数不上升**（`make diag-audit` 对比）。

### 3.4 注意

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

约定：
- **读**：`.project/` 优先；`.project/` 不存在而 `.link/` 存在时，读 `.link/` 并发一条
  "资料目录已迁移，建议移动"的提示（不自动搬，避免动用户文件）；提供命令 `bdl.migrateLinkDir` 一键迁移。
- **写**：只写 `.project/`，永不写 `.link/`。
- `.gitignore` 提示：`.project/secrets.json`、`.project/deploy.json` 不该提交（`E8 刻意不做` 的旧决定保留：
  不改用户文件，只在资料视图里给一次性提示 + 文档写清）。
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
- **生产环境组要有一条醒目警告**：*建议只写在 **Workspace** 设置里；不要提交
  `~/.vscode/settings.json` 到 git*（可以用 `markdownDescription` + `enumItemLabels` 呈现）。
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

## 6. P1-3：link 局部同步（新参数 + 合并）

### 6.1 现状

`tiptop link sync`（`-source golang|command|file`）每次都是**全量**：查 `DS.GAL_FILE` 全表 →
重新扫全语料判定 MAIN → **覆盖** `links.jsonl`（实测 ~24 s / 7600 文件）。

### 6.2 新参数（语义）

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
- 边界（需要实现时定）：被更新的程序可能**引用**别的文件/被别的程序引用 → 是否连带更新
  "调用它/被它调用"的程序？默认只更新显式指定的集合，meta 里注明"局部更新"，
  视图/状态命令显示"资料是部分更新的"。

### 6.3 与插件配合

| 入口 | 行为 |
|---|---|
| 链接资料视图 → 「重新同步」（用户点名放在"反查引用程序"旁） | 对**当前文件**执行局部同步 |
| 命令 `链接资料：更新当前文件` | 同上（当前编辑器文件 → `-files`） |
| 命令 `链接资料：按模组更新` | 弹出多选（lib/sub/qry/当前模组…）→ `-modules` |
| 命令 `链接资料：全量更新` | 无参 `sync`（明确提示"会耗时 ~24 s，确认？"） |
| 命令 `链接资料：从 CSV 导入` | 选文件 → 复制到 `.project/gal_file.csv` → `link import` |
| 命令 `链接资料：查看状态` | `link status` → 输出通道 |

### 6.4 去掉的东西（用户明确要求）

- **`link.maxAgeDays` 配置 + `link-data-stale` 诊断**：删掉（"7 天失效"这套不再用）；
- **后台自动同步**：`onStart`/`interval` 相关字段与实现删掉（`internal/linksync.Config` 里的
  `Enable/Interval/OnStart`），资料何时更新由用户点命令决定。
- 文档/设置里同步删除，避免"看起来还有这个功能"（这正是 §A3 那类残留的根源）。

---

## 7. P2-1：schema（发布后一版做）

> 用户明确：**schema 功能放到正式发布之后一个版本**；本节只留设计，不排进发布前待办。
> 数据来源附件（Oracle 导出，6 万多行）暂未拿到，格式按下面设计先定，拿到再校准。

### 7.1 数据模型（`.project/.schema/`）

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

## 9. 待确认

1. **`.project/` 里的 `deploy.json` 与 `secrets.json` 是否要提示加 `.gitignore`**：
   旧决定是"不擅自改用户文件"。现在密码进了 `.project/secrets.json`，是否改成立即在视图里提示一次？
2. **局部同步的连带范围**：更新 `aap/4gl/saapt310.4gl` 时，是否连带更新"引用它的程序"？
   （影响"链接资料是否完整"的观感）
3. **强制解析的粒度**：只对当前文件、到下次编辑有效（本计划默认），还是要"本次会话永久"？
4. **schema 附件**：拿到 Oracle 导出后需要按前 1000 行校准列语义（尤其 `data_length/char_length` 的取舍）。

---

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