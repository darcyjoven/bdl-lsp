# 待办与待确认

> 更新：2026-10-21（**P6 工作区符号索引完成：全项目跳转/引用/符号搜索**，`bdl-lsp 0.2.0`）  
> 当前阶段：**P1–P6 全部功能已完成**；**P7 全部收尾项已完成**
> （Zed 大纲层级、诊断降噪、内置符号表、语法缺口审计、链接资料索引、外部函数过滤、
> 采集通道、链接资料视图）。  
> **下一步候选**：
> ① P6 质量债剩余（record `.*` / `LIKE table.*` schema 索引、忽略列表）；
> ② 各仓小遗留（grammar 打 tag、`bdl.deploy.currentFile`）。
> 见 [PLAN-P7.md](PLAN-P7.md)。  
> 完整阶段见 [PLAN.md](PLAN.md)，环境细节见 [ENVIRONMENT.md](ENVIRONMENT.md)。完成后勾选，不删除历史。
>
> **新会话接手先读 [HANDOVER.md](HANDOVER.md)**（状态 / 基线 / 坑 / 下一步）。

---

## ★ 剩余待办速查（2026-10-21 盘点）

未勾选共 **55 项**（[PLAN-P7.md](PLAN-P7.md) 另有 16 项，全是 P7-D/E 与 P7-C-2 的验收标准）。
已经清掉了 27 项"实际已完成但没打勾"的历史项与 10 项 P7-D 落地项，所以这个数字现在可以信。

| 档 | 约数 | 内容 |
|---|---:|---|
| **P7 路线图** | **1** | P7-C-1 ✅ · P7-C-2 ✅ · P7-D 核心 ✅ · P7-D-2 ✅ · P7-D-3 ✅ · P7-E ✅ · 剩余：P7-D 分片（等体积/耗时触发条件，当前 415 KB 远未到） |
| P7-B 收尾 | 0 | ✅ `field-not-in-form` 查清（9309→8453） · ✅ `make diagnostics-check` |
| P6 / 质量债 | 7 | record `.*`、schema 索引、跨文件重命名（刻意不做，见理由）、忽略列表… |
| 各仓小遗留 | 6 | 工作区扫描、grammar 打 tag、`bdl.deploy.currentFile`… |
| **要机器 / 要人** | 21 | Windows 环境 10 · 账号服务器 3 · GUI 手工验证 3 · Windows e2e/scp 3 · Zed 加载 1 · Xvfb 已解决 |
| 文档债 | 2 | 只剩 `TESTING.md`；其余已并入既有文档 |
| 风险跟踪（非任务） | 15 | §7 已知风险 11 · P7 专项风险 4 |

> 也就是说：**代码侧只剩 P7-C/D/E 三个阶段**，其余是环境验证和风险清单。

---

## ★ P7-A 已完成（2026-10-21）

> 执行记录见 [PLAN-P7.md](PLAN-P7.md) §10。下一轮做 **P7-B**。

### A-1 Zed 大纲：同一条 `DEFINE` 的多个变量变成层级 ✅

- [x] 复现断言：`verify-queries.mjs` 增加「所有 `@item` 的 (start,end) 两两不同」，
      在旧 query 上确认为**失败**（bdl 报 10 处重复）。
- [x] 修 `languages/bdl/outline.scm`：`define` / `type` / `constant` 三处
      把 `@item` 改绑到名字节点。
- [x] **额外发现并修复 `.per` 的同类缺陷**：`PHANTOM table.column` 的
      `field_ref` 有两个 name（表名 + 列名），旧规则 `(field_ref (name) @name)`
      让两者区间重合。改为只绑 `column:` 字段。
- [x] 断言转绿；`verify-queries.mjs` **40 项全通过**；`cargo check` 通过。
- [x] 新增 fixture `tests/fixtures/outline.4gl` / `outline.per`（均 0 ERROR）。
- [ ] 手工验证（需 GUI）：`DEFINE l_name LIKE type_file.chr20, l_sql STRING`
      在 Zed 大纲中 `l_name`、`l_sql` **同级**。

### A-2 VSCode 下 GLOBALS 文件路径识别不到（Zed 正常）✅

- [x] 按 vscode-uri 规则复现编码形态（无需 Windows 实机）：
      VSCode `file:///C%3A/work/x`（冒号编码）、Zed `file:///C:/work/x`（不编码）。
- [x] 新建 `internal/fileuri`（`ToPath` / `FromPath`），完整百分号解码 +
      盘符/UNC 归一；**Windows 语义可在 Linux 上单测**（`toPathWindows` 纯函数）。
- [x] 单测 **22 个用例**（`%3A`、`%23`、`%20`、非 ASCII、UNC、大小写盘符、往返、
      拒绝非 file URI、防双重编码）。
- [x] 替换**三处**重复实现（原计划以为两处）：`globals` / `form` / `config`。
- [x] `initialize` 增加 `rootUri → workspaceFolders[0].uri → rootPath` 三级回退。
- [x] `go test -race ./...` 20 包全绿（281 个测试函数）；`make lint` 通过。
- [x] 语料回归：25 个真实 `.4gl`，`globals-file-not-found` = **0**（无退化）。
- [ ] **Windows 实机复验**（本沙箱无 Windows）：打开 `axmt620.4gl`，
      确认无 `globals-file-not-found`。

## ★ P7-B 第一批已修：悬停的两个问题（2026-10-21）

> 用户 Windows 实测反馈。执行记录见 [PLAN-P7.md](PLAN-P7.md) §10.3。

### B-1 多行 RECORD 悬停被压成一行 ✅

- [x] 复现：`DEFINE g_qryparam RECORD ... END RECORD`（26 行）悬停变成
      **一行几百字符**，完全没法读。
- [x] 根因：`index.typeText` 用 `normalizeSpace` 把换行压成空格；
      `TypeText` 同时被用于**语义判断**（recordname.go 判 `RECORD` 前缀），
      不能直接改。
- [x] 修法：新增 `Symbol.TypeTextRaw`（保留换行与缩进，仅去行尾空白、
      tab→4 空格）；`TypeText` 语义不变，悬停优先用 `TypeTextRaw`。
- [x] 验证：真实文件 `tiptop/config/top.global:206` 的 `g_qryparam`
      悬停**逐行原样输出**（26 行，含注释）。

### B-2 同名局部变量跨函数泄露 ✅

- [x] 复现：`fa` 和 `fb` 各有一个局部 `i`，在 `fa` 里悬停 `i`
      **同时显示 fa 的 INTEGER 和 fb 的 STRING**。
- [x] 根因：hover 用 `fi.Lookup(小写名)` 扫**全文件**同名符号，
      没有作用域过滤；而**跳转**早就按作用域过滤了——两处规则不一致。
- [x] 修法：抽出 `scopedSymbolsAt`（跳转与悬停共用同一套规则），
      优先级：`当前函数局部 → 当前函数参数 → 模块级 → GLOBALS 块内 → GLOBALS 引入`。
- [x] 顺带修正：补上 `ScopeGlobals`（GLOBALS 块内）这一级——原来它不在
      优先级链里，导致块内定义会显示成模块级同名变量。
- [x] 记录字段回归：字段的 `Container` 是**记录名**不是函数名，
      不能按函数过滤；改为"光标是否落在声明该字段的记录区间内"判断。
      （`TestHoverRecordField` 一度因此变红，已修好。）
- [x] 语料抽样：29 个真实 RECORD 变量悬停，0 个出现多条目泄露。

### B-3 Zed 大纲仍有层级 ✅ 已修复（两轮）

> 用户 2026-10-21 给了三个具体写法：`DEFINE i,j INTEGER` 与
> `DEFINE i INTEGER,j INTEGER` 都会变两层，`DEFINE i INTEGER` +
> `DEFINE j INTEGER` 正常。

- [x] **读 Zed 源码定位根因**：`crates/language/src/buffer.rs` 的 depth 算法——
      判据是"前一条目结束位置 ≥ 当前条目结束位置"，即**区间相交/重叠**，
      不只是"区间完全相同"。上一轮断言只查后者，是盲区。
- [x] 识别出两种层级来源：① 区间完全相同（同语句多变量，上一轮已修）
      ② 区间相交（函数 item 覆盖整个函数体）。
- [x] **第 1 轮（`1ee95bf`）**：把 `function`/`report`/`main` 的 `@item`
      绑到最小语义单元（名字节点）。假层级全平（0/7135），
      **但把"变量归属函数"这个正确行为一起消灭了** —— 方向错了。
- [x] **第 2 轮（`7f80d21`）**：改用**容器/叶子模型**，
      `@item` 绑法相反（容器绑整块、叶子绑名字标识符）。
      关键证据：Zed 内置语言的 outline 查询在
      `crates/grammars/src/<lang>/outline.scm`，Rust/Python 都让函数
      `@item` 覆盖整块 + `@open`/`@close` 标折叠——
      **"父 item 覆盖子 item 区间"是合法层级表达，不是 bug**。
- [x] **断言 3（关键）**：容器内部变量的 Zed 父级链上必须能找到该容器。
      真相源是 `tree-sitter parse` 的语法树。
      断言 1/2 抓不住 `1ee95bf` 回归——"函数容器"和"变量叶子"都是
      单行短标识符，启发式区分不了。
- [x] 全语料实测（7699 个 `.4gl` + 7409 个 `.per`）：0 处假层级。
- [ ] 手工验证（需 GUI）：
      ① 同一条 DEFINE 的多个变量在大纲里平级；
      ② **函数内变量归属所属函数、可折叠**。

### B-5 游标 / prepare statement 支持（✅ 已完成，第七轮）

> 用户 2026-10-21 提出的第三组问题。示例：
>
> ```4gl
> PREPARE p_admin_wo_fill_sfa_p FROM l_sql
> DECLARE p_admin_wo_fill_sfa CURSOR FOR p_admin_wo_fill_sfa_p
> EXECUTE p_admin_wo_fill_sfa_p
> ```

- [x] 摸清 grammar 现状（`tree-sitter-bdl/rule/sql.js`）：
      **节点已齐全，不需要改 grammar**（除了下面那条 `FETCH ... FROM` 缺口）。

  | 语句 | 节点 | 关键字段 |
  |---|---|---|
  | `PREPARE p FROM sql` | `prepare` | `cursor:` = prepare 名，`sql:` = 来源表达式 |
  | `DECLARE c CURSOR FOR p` | `declare` | `cursor:` ×2 —— ①游标名 ②来源 prepare 名 |
  | `EXECUTE p` | `execute` | `cursor:` = prepare 名 |
  | `OPEN c` / `CLOSE c` | `open` / `close` | `cursor:` = 游标名 |
  | `FETCH NEXT c` | `fetch` | `cursor:` = 游标名 |

  注意 `_cid` 规则（`field("cursor", $.identifier)`）被 prepare/execute 共用，
  字段名叫 `cursor` 但在 `prepare`/`execute` 里实际是 prepare statement——
  实现时按**父节点类型**区分，不要按字段名。

  `declare` 的两个 `cursor:` 字段靠**出现顺序**区分：第一个是游标自身，
  第二个是 `FOR` 之后的来源。

- [x] `PREPARE <name> FROM <sql>` → 声明一个 **prepare statement** 符号
      （新增 `index.KindPrepare`）。
- [x] `DECLARE <name> CURSOR FOR <prepare>` → 声明一个 **cursor** 符号
      （新增 `index.KindCursor`），并记下它绑的 prepare（`Symbol.RefName`）。
- [x] `EXECUTE` / `FETCH` / `OPEN` / `CLOSE` / `FOREACH` 等语句使用
      prepare statement / cursor 时，**悬停与跳转**都已生效。

      > 跳转不需要额外代码：这些语句里的名字是普通标识符，
      > 按作用域查找就能命中新符号。`DECLARE c CURSOR FOR p` 里的 `p`
      > 同理——`identAt` 拿到的就是 `p`，命中 prepare 符号。

- [x] **收集实现**（关键决定，走了三个方案才定）：

      最终用「**源码里找关键字 → `DescendantForByteRange` 定位节点**」，
      成本只与关键字出现次数成正比（实测每次 Build 只多几十微秒）。
      前两个方案都因太慢被否，详见 [PLAN-P7.md](PLAN-P7.md) §10.4。

      好处之一是**不需要"哪些节点可能含语句"的白名单**——那种白名单会把
      `PREPARE` 写在 `MENU`/`DIALOG` 里的情况漏掉。

      正确性由一份独立真相源守住：`sqlstmt_test.go` 用**全树递归扫描**
      做参照，在**全部 7699 个真实 .4gl** 上比对，符号集合完全一致
      （prepare=37525 / cursor=36007）。
- [x] **不是 grammar 缺口**：曾怀疑 `FETCH NEXT FROM c` 报 ERROR 是缺 `FROM`，
      语料检索（7699 个 `.4gl`，17491 条 FETCH）显示 **0 处** `FETCH ... FROM`，
      真实写法都是 `FETCH [NEXT|ABSOLUTE|FIRST|LAST|...] <cursor>`。
      grammar 的 `fetch` 规则是对的。

### B-4 后续待办（原有 P7-B 内容）—— ✅ 全部完成（详见 PLAN-P7 §10.5）

- [x] `internal/diag/registry.go`：11 个诊断码集中注册，级别统一解析
      （`SeverityFor` 优先级：`bdl.diagnostics.<code>` > 旧专用键 > 注册表默认）。
- [x] 新增 `bdl.diagnostics.<code>` 配置（`config.Settings.Diagnostics`，
      增量合并；`syntaxDiagnostics` 默认同步改为 `warning`）。
- [x] 拼错保护：`didChangeConfiguration` 里对未注册的码记 warn；
      MISSING 的 `missing-<kind>` 是动态码，按前缀放行。
- [x] **修调用点误报**：补 `(expression name:)`（`IF fn() THEN`）；
      另补 `(whenever (variable))`（`WHENEVER ERROR CALL f` 的 `f`）；
      表单 4 条 pattern 改为捕获整个 variable 节点（`DISPLAY … TO FORMONLY.cn2`）。
- [x] `function-not-found`：已实现，**默认 off**——实测 24 个真实文件报 3660 条
      （内置函数与跨模块函数都还没有符号表），P7-C/D 之后重新度量再打开。
- [x] 语料复测：24 文件 `variable-source-unresolved` **1717 → 889**；
      全量 15108 文件诊断 **334299 → 289304**（−13.5%），剩余 98% 是内置全局。

### B-6 P7-B 收尾：新增的度量工具与已知遗留

- [x] `cmd/diag-audit`：不起 LSP 连接直跑语义诊断流水线，输出逐码计数、
      高频标识符、噪声最高的文件；`make diag-audit`（`LIMIT=` / `CODES=` 可调）。
- [x] 顺带修掉两个 `.per` 误报（合计占全部诊断 13%）：
      - `form-layout-tag-undefined` 40167 → **17**（一个 `item_tag` 常含多个
        tag：`[label1 |alh01]`，旧实现把整段当一个 tag）；
      - `form-screen-record-undefined` 4850 → **5**（SCREEN RECORD 引用的是
        **字段名** `= formonly.more` 的 `more`，不是 item tag `mor`）。

- [x] **`field-not-in-form` 1725 条查清**：两类系统性误报，共 **9309 → 8453**。
      - `CONSTRUCT BY NAME var ON <列表>` 的列表是**数据库列名**，不是表单字段
        （4js 手册"The CONSTRUCT sub-dialog"：BY NAME 用列名去**隐式匹配**同名
        表单字段）→ 跳过 ON 列表，只校验 `FROM` 后面的字段列表（−620）；
      - 表单里 `Edit aa1=FormOnly.ada01` 这种写法，4GL 的 `FROM`/`AFTER FIELD`
        引用的是**字段名 ada01**，不是 item tag `aa1` → 字段名也算存在（−236）。
      剩余 869 条抽样确认为真：代码引用的字段表单里确实没有（如 `axdr222` 引用
      `ada01` 而表单只有 `to_apa15`；合并文件 `saapt110.4gl` 3 万行引用别的表单
      的字段）。
- [x] `make diagnostics-check` 防漂移：`internal/diag.Specs()` 是唯一来源，
      生成 `docs/DIAGNOSTICS.md`（`make diagnostics-write`）并校验 VSCode
      `package.json` 的 `bdl.diagnostics` 枚举 + 六个老键的 `default`/`enum`；
      并入 `make lint`。故意改坏一个默认值即失败（有单测钉住）。

## 0. 已确认决策

| # | 决定 |
|---|---|
| Q1 | `bdl-lsp` → `git@github.com:darcyjoven/bdl-lsp.git`；`vscode-extension` → `vscode-bdl`；`zed-extension` → `zed-bdl`。三个 `origin` 已配置。 |
| Q2 | 部署同步：`ssh + rsync` 为主，`scp` 为轻量备选/回退；v1 不做纯 Go SFTP。 |
| Q3 | 字段校验默认 `warning`，只在表单唯一解析成功时报告。 |
| Q4 | **不复用** `bdl` id；`bdl-plugin` 保持轻量，本插件是全功能版。Zed 扩展 id 定为 **`4gl-full`**，两者共存。 |
| Q5 | VSCode **先本地 VSIX**；市场发布暂不排期。 |
| Q6 | v1 在 **Windows + Linux** 验证通过；macOS 后续兼容。 |
| Q7 | 目标体验是**无感自动下载**；实现暂不排期，开发期先用本地路径。 |
| Q8 | 目标范围 **Genero 2.50–5.0**；当前 grammar 为 **2.50** 基线，**5.0 暂不排期**。 |
| Q9 | LSP 二进制归属 **`bdl-lsp` 自己的 Release**（tag `lsp-vX.Y.Z`）；发布动作暂不排期。 |

### 暂不排期

- Genero 5.0 语法支持。
- GitHub Release / 无感自动下载 / VSCode 市场发布 / Zed 注册表发布。
- 以上不阻塞 v1（2.50 + Linux/Windows 开发闭环）。

---

## 1. Git 与远程

- [x] 三个仓库 `git init -b main` 并完成首次提交。
- [x] `bdl-lsp` 配置 `origin` → `git@github.com:darcyjoven/bdl-lsp.git`。
- [x] `vscode-extension` 配置 `origin` → `git@github.com:darcyjoven/vscode-bdl.git`。
- [x] `zed-extension` 配置 `origin` → `git@github.com:darcyjoven/zed-bdl.git`。
- [x] 配置 GitHub SSH key（`ssh -T git@github.com` 已验证）。
- [x] 首次 push 三个仓库（`git push -u origin main`）。
- [x] `bdl-lsp` GitHub Actions：
  - [x] Linux + Windows CGO 构建/测试（`.github/workflows/build.yml`）；
  - [x] 语料冒烟（CI 只跑 `make check`；全量 `make regress` 留本地按需）；
  - [x] **不发布 Release**（暂不排期）——已明确写进 workflow 注释。

---

## 2. 环境配置（P0，优先）

完整清单、安装命令、常见问题见 [ENVIRONMENT.md](ENVIRONMENT.md)。

### 2.1 Linux（本机，已完成基础安装）

- [x] Go 1.24.4、`CGO_ENABLED=1`、gcc 13.3。
- [x] Node 24.21、pnpm 12.14、tree-sitter CLI 0.26.5。
- [x] Rust 1.99 + `wasm32-wasip2` + wasi-sdk 34（`scripts/install-toolchain.sh`）。
- [x] VSCode CLI 1.142（headless 模式可用）。
- [x] OpenSSH、`rsync`、`scp`、`gh` 2.102。
- [x] `scripts/dev-env.sh` 后 Go 构建缓存指向可写目录。
- [x] `scripts/env-check.sh` 通过（必需项齐全）。
- [x] 冒烟：双 grammar 解析、Go/CGO、`zed_extension_api` wasm 编译。
- [x] Linux Xvfb：已无 root 装到 `artifacts/toolchain/bin/`（`Xvfb` + `xkbcomp`），
      `artifacts/logs/xvfb.log` 有实跑记录；headless 仍作为默认。
- [x] 骨架就绪后跑 `go test`（现 303+ 个测试函数全绿）。

### 2.2 Windows（v1 必验）

- [ ] Go 1.24+ + MinGW-w64 gcc，`where gcc` 正确。
- [ ] `CGO_ENABLED=1`、`CC=gcc` 下能构建最小 CGO 程序。
- [ ] Node 24、pnpm、Git for Windows、tree-sitter CLI。
- [ ] VSCode + CLI。
- [ ] OpenSSH Client：`ssh -V`、`scp -V` 可用。
- [ ] rsync 可选（cwRsync/MSYS2/WSL）；v1 不强依赖。
- [ ] Rust + `wasm32-wasip2` + wasi-sdk。
- [ ] Zed Windows 官方版安装 + dev extension 加载。
- [ ] `git config --global core.autocrlf false`；`.4gl`/`.per` 保持 LF。
- [ ] 用 `scp` 连通测试服务器（或 dry-run 记录命令）。

### 2.3 账号/服务器

- [x] GitHub SSH key 已验证；`gh` 可选（发布暂不排期）。
- [ ] 测试服务器：Genero、`fglform`/`fglcomp`、SSH key 登录、`remoteRoot` 样例。
- [ ] 记录本地路径 ↔ 远程路径映射（Windows 盘符也要覆盖）。
- [ ] 不在仓库/配置样例中写密码、私钥、生产主机。

### 2.4 环境脚本

- [x] `scripts/dev-env.sh`。
- [x] `scripts/dev-env.ps1`。
- [x] `scripts/env-check.sh`、`scripts/env-check.ps1`。
- [x] `scripts/install-toolchain.sh`（无 root 安装）。
- [x] 语料脚本：以 `scripts/corpus-smoke.sh` / `scripts/corpus-regress.sh`
      + `cmd/fmt-audit` / `cmd/field-audit` / `cmd/record-audit` / `cmd/diag-audit` 落地
      （未沿用原定文件名，功能全覆盖）。

---

## 3. P0 剩余技术验证

- [x] `zed_extension_api 0.7.0` + `wasm32-wasip2` 编译验证（`artifacts/spike-rust`，可生成 `.wasm`）。
- [x] Zed 扩展最小骨架：`extension.toml`（id `4gl-full`）、两个 language、`lib.rs`。
- [ ] 验证 Zed 能加载 ABI 15 grammar + BDL scanner（Linux + Windows dev extension）。
- [x] VSCode 最小客户端：激活/连接已验证（e2e **33/33**）。
- [x] 本地二进制路径启动：Linux 已验证（`--version` + 日志 + 无 stdout 污染，e2e 走真实二进制）；
      Windows 待上机（见 §2.2）。
- [x] 真实 `bdl-lsp` 二进制在 VSCode 中打开真实文件：e2e 33/33（fixture 而非 `ammr110.4gl`，
      但"无 stdout 污染 / 日志可读"这两项已由连接可用与日志断言覆盖）。
- [ ] 在 `tree-sitter-bdl` / `tree-sitter-per` 中把 Go bindings 的 `go-tree-sitter` 升到 `v0.25.0`，补“真正解析文件”的测试（ABI 15）。
- [ ] 记录 Zed/VSCode 的 LSP 能力差异（semantic tokens、pull diagnostics、workspace configuration）。
- [x] 本机验证 CGO 构建 + 双 grammar 运行（Linux）。
- [ ] 验证 Windows 下 CGO 构建 + 运行 LSP（可先用 spike）。
- [x] 无桌面 VSCode CLI headless 模式验证（用于 e2e）。

---

## 4. 详细计划（下一步文档）

- [x] `docs/FEATURES.md`（功能基线，含来源标记）
- [x] `docs/PLAN-DETAILED-P1.md`
  - [x] 包/文件清单与接口签名（`internal/server`、`document`、`parser`、`index`）
  - [x] LSP 方法清单与能力协商字段
  - [x] 每个任务的验收命令
  - [x] VSCode 扩展文件级任务（本地路径优先；自动下载暂不排期）
  - [x] 测试 fixture 清单
  - [x] Windows 构建/验证步骤
- [x] `docs/PLAN-DETAILED-P3.md`（字段校验）→ **以 [PLAN-P3B-FIELD.md](PLAN-P3B-FIELD.md) 落地**，不再单开。
  - [x] 表单索引数据结构（`internal/form` + `index.FileIndex.FormItemNames/FormFieldNames`）
  - [x] 4GL 绑定/字段提取规则表
  - [ ] 诊断/忽略配置（**忽略列表仍未做**，见下方"字段校验"小节）
  - [x] 标注集与评估脚本（`cmd/field-audit`，模块一致率 97.2%）
  - [x] record `.*` 成员校验（最后做，默认关闭）
- [x] `docs/FORMATTER.md`（规则表与示例）→ **以 [PLAN-P4-FORMAT.md](PLAN-P4-FORMAT.md) 落地**。
- [x] `docs/DEPLOY.md`（配置 schema、rsync/scp、安全、错误处理）→ **以 [PLAN-P5-DEPLOY.md](PLAN-P5-DEPLOY.md)
      + [DEPLOY-ZED.md](DEPLOY-ZED.md) 落地**。
- [ ] `docs/TESTING.md`（测试矩阵、Linux/Windows 命令）
- [x] `docs/GENERO-VERSIONS.md`（2.50 基线；5.0 暂不排期）→ **并入 [TECH-STACK.md](TECH-STACK.md) §ABI/Genero 版本**。

---

## 5. 工程骨架（P1）

### bdl-lsp

Block 1 已完成：

- [x] `go.mod`：Go 1.24，`go-tree-sitter v0.25.0`、`go.lsp.dev/*`；grammar 暂用 `replace`。
- [x] `main.go`：`serve` / `check` / `version` 子命令，`--log-level` / `--log-file`。
- [x] `internal/server`：initialize/shutdown/exit、method 分发、未知方法、stdio。
- [x] `internal/document`：Store/Document/LineIndex（didChange 暂为整文替换）。
- [x] `internal/parser`：parser 池、双 grammar、ABI 校验、ERROR/MISSING 收集。
- [x] 诊断：解析 ERROR/MISSING → `publishDiagnostics`（UTF-16 换算）。
- [x] `internal/check`：`check` CLI（文本 / `--json` / `-q`）。
- [x] `Makefile`：`build`/`test`/`test-race`/`vet`/`lint`/`check`/`corpus`。
- [x] 测试：parser 8 个、document 5 个、server 6 个（协议级）+ 2 个 benchmark。
- [x] `scripts/corpus-smoke.sh`：语料抽样冒烟。

Block 2 已完成：

- [x] `internal/textpos`：UTF-16 ↔ 字节 ↔ tree-sitter Point 换算。
- [x] `internal/document`：`Change` / `ApplyChanges` / `Tree.Edit` / Store。
- [x] didChange 增量编辑（含多段、整文替换、非法区间钳制）。
- [x] 旧树生命周期修正（修复 `ts_tree_delete` 双重释放 SIGSEGV）。
- [x] `exit` 后进程挂起修正 + LSP 退出码语义。
- [x] `scripts/lsp-bench.py` + `make bench`。
- [x] 测试增至 36 个；`go test -race` 通过。

Block 3 已完成：

- [x] `internal/index`：BDL 与 PER 符号提取（函数/报表/记录/字段/参数/表单 item/SCREEN RECORD）。
- [x] `textDocument/documentSymbol`、`workspace/symbol`、`textDocument/hover`。
- [x] 索引缓存（按 URI+版本失效）。
- [x] 大小写不敏感匹配；光标取词不依赖语法树。

Block 4 已完成：

- [x] `textDocument/definition`：局部 → 参数 → 全局 → `rec.field` 记录字段。
- [x] `textDocument/references`：词边界 + 排除注释/字符串 + includeDeclaration。
- [x] 修复光标落在词首/词中时 `rec.field` 判定失败。

Block 5 已完成：

- [x] `internal/config`：generoVersion、诊断级别、limits、工作区配置、客户端热更新。
- [x] 诊断增强：级别可配、去重、同行合并、诊断 code、大文件提示。
- [x] 解析超时（C 层）与 ctx 检查；修复 `ParseCtx` 崩溃。
- [x] `scripts/corpus-regress.sh` + `make regress`。
- [x] 全量回归：per 7407/7409、bdl 7683/7699；18 个失败均为已知文件问题。

Block 6 已完成（VSCode 客户端）：

- [x] `package.json`：`bdl`/`per` 语言、10 项配置、3 条命令、激活事件。
- [x] `src/server/binary.ts`：设置 > `BDL_LSP_PATH` > 工作区 > PATH 四级查找，失败给可操作提示。
- [x] `src/client.ts`：启动 LSP、documentSelector、配置同步、追踪级别、重启。
- [x] `src/extension.ts`：激活、命令注册、失败只提示一次。
- [x] `scripts/generate-tm-grammars.mjs`：从 grammar 源码生成 TextMate 语法（361 + 76 个关键字全分类）。
- [x] `scripts/verify-tm-grammars.mjs`：41 项高亮切分校验（含多词短语、注释/字符串屏蔽）。
- [x] `language-configuration/{bdl,per}.json`：注释、括号、自动闭合、缩进。
- [x] `vsce package`：VSIX 111 KB / 10 个文件。
- [x] e2e：真实 VSCode 扩展宿主 14 个用例全通过。
- [x] 修复服务器不识别 `--stdio`（客户端会自动追加）。
- [x] `scripts/e2e-vscode.sh` + `make e2e-vscode`。

待做：

- [x] 工作区扫描（P6，✅ 0.2.0）：`internal/wsindex` 后台索引 + 快照，
      `workspace/symbol` / 跳转 / 引用都覆盖未打开的文件。
      实测 7756 文件首次 **24.7 s**、载入快照 0.56 s、无变化增量 22 ms。
      见 [PLAN-P6-INDEX.md](PLAN-P6-INDEX.md)。
- [x] 跨文件跳转（genero 的 `GLOBALS` / 导入）✅ P3a 已完成，
      见 [PLAN-P3A-GLOBALS.md](PLAN-P3A-GLOBALS.md)。
- [ ] `RECORD LIKE table.*` 的 schema 索引（P6）。
- [x] 配置 `bdl.generoVersion`（默认 2.50，Block 5 实现，Block 6 在客户端暴露）。
- [x] GitHub Actions：Linux + Windows 构建/测试（`.github/workflows/build.yml`，不发布 Release）。
- [ ] 为 grammar 打 tag，去掉 `replace`。

### vscode-extension（→ `vscode-bdl`）

Block 6 已完成：

- [x] `package.json`：languages、grammars、configuration、commands、activationEvents。
- [x] `src/extension.ts` + `client.ts`：启动 LSP、documentSelector、配置同步。
- [x] `src/server/binary.ts`：**本地路径解析**（设置/环境变量/工作区/PATH）；自动下载/校验/回退暂不排期。
- [x] `syntaxes/bdl.tmLanguage.json`、`syntaxes/per.tmLanguage.json`（关键字/字符串/注释/数字/函数/类型/字段访问/预处理）。
- [x] `language-configuration/*.json`：注释、括号、自动闭合、缩进规则。
- [x] 命令：`bdl.restartServer`、`bdl.showServerInfo`、`bdl.showOutput`。
- [x] 打包：`vsce package` 生成 VSIX（本地）。
- [x] 测试：Linux 下 14 个 e2e 用例（激活/server 启动/诊断/跳转/引用/大纲/悬停/配置热更新）。
- [x] GitHub Actions：Linux 构建/测试 + VSIX 打包（`.github/workflows/build.yml`）。

Block 7 已完成（CI 与健壮性）：

- [x] `scripts/verify-vscode-manifest.mjs`：22 项清单/语言配置校验，`make manifest-check`。
- [x] 修复 `language-configuration/per.json` 不是合法 JSON（`[` 自动闭合对缺 `"close":`）。
- [x] 修复 `language-configuration/bdl.json` 缩进规则用了 JS 不支持的 `(?i)` → 改 `flags: "i"`。
- [x] e2e 增至 14 个用例，新增「扩展名映射」与「语言配置生效」两组。
- [x] `test/runTest.js` 跨平台化：`Code.exe` 探测、`taskkill` 清理、Linux 专属参数按平台裁剪。
- [x] `scripts/e2e-vscode.ps1`：Windows 版 e2e 入口。
- [ ] Windows 下 e2e 实测（代码已就绪，等 Windows 机器跑一次）。
- [ ] 命令 `bdl.deploy.currentFile`（P5，占位未实现）。

### zed-extension（→ `zed-bdl`，id `4gl-full`）

Block P2 已完成：

- [x] `extension.toml`：id `4gl-full`、schema_version 1、grammars（bdl/per，锁 SHA）、
      language_servers（`Genero BDL`/`Genero Form` + language_ids → `bdl`/`per`）。
- [x] `languages/bdl/*`：config.toml、highlights（300 关键字全覆盖）、outline、
      brackets、indents、injections（sql_body → sql）。
- [x] `languages/per/*`：config.toml、highlights（76 关键字全覆盖）、outline、
      brackets、indents（**刻意不动 LAYOUT**）。
- [x] `src/lib.rs`：`language_server_command` 四级查找（设置 > `BDL_LSP_PATH` >
      工作区 `.bdl-lsp/bin` > PATH），失败给可操作提示；透传 settings。
- [x] `Cargo.toml`：`zed_extension_api = "0.7.0"`，`cdylib`，release 优化。
- [x] `cargo check/build --target wasm32-wasip2`：0 warning，产物 185 KB。
- [x] `tests/verify-manifest.py`：14 项清单/交叉引用校验。
- [x] `tests/verify-queries.mjs`：38 项 query 校验（节点名/字面量/编译/命中/
      关键字覆盖/真实语料抽样）。
- [x] GitHub Actions：wasm check + manifest + queries 三个 job。
- [ ] Zed dev extension 手工加载验证（Linux + Windows，本机无图形界面）。

### P2 踩坑记录（写进 query 注释与校验脚本）

| 现象 | 根因 | 修法 |
|---|---|---|
| `highlights.scm` 整份不生效 | query 里写了 grammar 没有的 token（`<>` `!=` `>=` `<=` `\|\|` `**` `{` `}`） | 以 `src/node-types.json` 的匿名 token 为准逐个核对 |
| `Impossible pattern` 编译失败 | `.per` 的 `menu_group.name` 实际是 `identifier \| 关键字` 联合，绑 `(name)` 非法 | 改绑 `(identifier)` |
| `constant` 的名字匹配不到 | 名字来自隐藏规则 `_constant_statement`，没有可直接绑的字段 | 用子节点匹配 `(constant (identifier) @name)` |
| 校验脚本误报大量"未知字面量" | 去注释时没跳过字符串内部的 `;`，正则跨行乱配 | 扫描时跟踪字符串状态 |
| 校验脚本报 `_` 是未知节点 | `_` 是 tree-sitter 通配符，不在 node-types 里 | 显式跳过 |
| `.per` 样例 8 个语法错误 | 按真实 Genero 写的 `KEY F1 ... ACTION ...`、`PAGE HEADER` 放 `OUTPUT` 里，grammar 不支持 | 按 grammar 实际规则重写样例 |

---

## 6. 功能 Backlog（按优先级）

### GLOBALS 模组全局变量（P3a ✅ 已完成 2026-10-20）

> 用户明确要求（2026-10-20）。完整设计与实现记录：
> [PLAN-P3A-GLOBALS.md](PLAN-P3A-GLOBALS.md)（§10 实现差异、§11 验收输出）。

- [x] T1 索引 GLOBALS 块：新增 `ScopeGlobals`；`GlobalsImport` 提取
      （含变量名形式）。验收：真实 `top.global` 索引出 274 个符号。
- [x] T2 `internal/globals` 解析器：相对路径（基准=引用文件目录）+
      `searchPaths`；按需加载 + mtime 缓存；禁 basename 隐式匹配；
      链式解析防环。验收：语料 7756 文件 8403 Resolved / 107 NotFound
      （与规划脚本数字吻合）；环/链/缓存/内存版优先各有单测。
- [x] T3 诊断 `globals-file-not-found`（默认 warning，可 off）；
      反向依赖刷新（编辑/打开 `.global` → 刷新引用方，含 NotFound
      登记候选位置的场景）。`relatedInformation` 未做（诊断结构改造
      与 P3b 共用，本轮单文件目标已够）。
- [x] T4 跳转四级作用域（新增 GLOBALS 级）+ 悬停来源标注
      （“来源：xxx.global:行号”）；定义方文件内悬停标注“模组全局变量”。
- [x] T5 诊断 `variable-source-unresolved`（默认 warning，可 off）：
      tree-sitter Query 只取 `variable` 首标识符、跳过 `call.name`；
      存在 Unresolvable 导入时全部放过。缓存 + 400ms 防抖（按键路径
      6.8ms，全量重算 27ms 只在停顿后跑）。
      `scripts/globals-audit.sh` 标注**未做**（语料 107 处缺失均为
      真实文件缺失，单测覆盖保守规则，人工标注留待真实使用反馈）。
- [x] T6 配置三项（`fileNotFoundDiagnostics` / `unresolvedSourceDiagnostics` /
      `searchPaths`）+ VSCode `package.json`（10→13 项）+ 客户端同步 +
      Zed 文档示例；e2e 14 → **16** 用例全通过。

### 字段校验（P3b ✅ 已完成 2026-10-20）

> 完整实现记录：[PLAN-P3B-FIELD.md](PLAN-P3B-FIELD.md)。

- [x] 复用 P3a 的跨文件索引模式（internal/form 与 internal/globals 平行结构）。
- [x] `.per` 索引：item 名、类型、PHANTOM、定义位置（既有）+
      LayoutRefs（新增，供内部校验）；修复 PHANTOM 与 SCREEN RECORD
      裸字段名提取（名字在 field_ref 里，原实现取不到）。
- [x] `SCREEN RECORD` 解析与字段集合（含 table.column 与裸名两种形态）。
- [x] 4GL 表单绑定：`WITH FORM`、`OPEN FORM` + `DISPLAY FORM`（命名绑定）、
      按函数隔离的作用域跟踪（`(function/main/report) @container`）。
- [x] 字段引用：`BEFORE/AFTER FIELD`、`ON CHANGE`（branch）、`NEXT FIELD`（hook）、
      `INPUT FROM`、`CONSTRUCT ON`、`DISPLAY TO`、`INFIELD()`——单 query 多 pattern。
- [x] 表单路径解析：R1a（../per/，语料 94%）、R1b、R1c（perRoots）、
      R2（basename 唯一）、R3（模块段消歧）；歧义放弃校验（Q3）。
- [x] 诊断 `field-not-in-form` + relatedInformation 指向 .per 文件 +
      配置 `fieldDiagnostics`（默认 warning，可 off）。
- [x] 评估：`cmd/field-audit` 全量 7699 文件 → 1725 条发现；
      自动标注（模块段一致性）可疑率 **2.8% < 5%**；人工抽查确认真阳性
      （语料存在 .4gl/.per 版本漂移，如 aapr106 的 apa18 不在 aapt110.per）。
- [x] `.per` 内部校验：重复 item、布局 tag 未定义、SCREEN RECORD 引用不存在
      （三个诊断码，共用 fieldDiagnostics 级别）。
- [ ] 忽略列表（按文件/字段/表单 glob）——未做；`off` 可整体关闭兜底，
      精细忽略待真实使用反馈再排期。
- [x] 50 模块人工标注——已以自动标注（模块一致率 97.2%）+ 抽样人工复核替代。
      完整人工标注待真实用户环境。

### 格式化（P4 ✅ 已完成 2026-10-21）

> 完整实现记录：[PLAN-P4-FORMAT.md](PLAN-P4-FORMAT.md)。

- [x] `.4gl` 缩进（块结构表）：按祖先链上块节点算深度；行首是
      `END/ELSE/CATCH` 回退一级；同一行开多个块（`DEFINE x RECORD`）
      只贡献一级缩进。
- [x] 关键字大小写：只改匿名 token，`upper`/`lower`/`unchanged` 可选；
      跳过 `&` 开头的预处理指令（`&INCLUDE` 大小写敏感，改了会破坏解析）。
- [x] 缩进字符与宽度：`indentStyle` auto/tab/space、`indentSize` 0=跟随编辑器；
      优先级：显式配置 > 编辑器请求（`insertSpaces`/`tabSize`）> 4 空格兜底。
- [x] **`.per` 不做任何格式化**（用户明确要求）；`.per` 请求直接返回空。
- [x] 空白/逗号/运算符**不改**——只改关键字与行首缩进，避免全量 diff。
- [x] 幂等/ERROR 守卫测试：`internal/format` 18 个单测、`internal/server`
      8 个协议级测试；`cmd/fmt-audit` 全语料 7699 文件
      → 成功 7683、拒绝 16（语法错误）、幂等失败 **0**、新增 ERROR **0**。
- [x] 含语法错误的文件整体拒绝格式化（`tree.HasError()`），返回原文。
- [x] Format on save：VSCode 通过 `contributes.configurationDefaults` 把
      `[bdl].editor.defaultFormatter` 设为本扩展，用户只需打开
      `editor.formatOnSave`（不替用户强制开启）；Zed 用 `format_on_save`。
- （不做）空白/逗号/运算符规范化：全量 diff 风险高、收益低。

### 部署（P5 ✅ CLI + VSCode 已完成 2026-10-21）

> 完整实现记录：[PLAN-P5-DEPLOY.md](PLAN-P5-DEPLOY.md)。

- [x] 配置 + 示例（`examples/deploy.json`）：targets/upload/compile、
      include/exclude、`mode`、`fallback`、`delete=false`、`strictHostKey`。
      未知字段直接报错（避免拼错字段名静默部署到错目录）。
- [x] `bdl-lsp deploy` 子命令：`-dry-run`、`-target`、`-list`、`-json`、
      `-no-compile`、`-no-upload`、可只部署指定文件。
- [x] rsync 主路径（`--delay-updates`、`--files-from` 避免命令行过长）。
- [x] scp 逐文件上传 + 失败聚合（"部分成功"不显示成成功）。
- [x] rsync 缺失/失败 → scp **显式**回退（摘要与 JSON 都标注，不静默降级）。
- [x] 远程输出解析：`file:line[:col]: error: -code: msg`，映射回本地路径。
- [x] 行尾检查（CRLF 上传前告警）、远程路径 POSIX 校验、越界路径拒绝。
- [x] 退出码区分：0 成功 / 1 部署失败 / 2 配置或参数错误。
- [x] 实机验证：本机 sshd 上 rsync、scp、回退、单文件四条路径全部通过。
- [x] VSCode：4 条命令（部署/当前文件/预演/列目标）、输出通道、
      诊断发布到 Problems、`bdl.deploy.target` 设置。
- [x] Zed tasks 模板：[DEPLOY-ZED.md](DEPLOY-ZED.md) 给出 `task: spawn`
      模板与 PATH 问题说明；Zed README 已链接。
- [ ] Windows scp 实测（代码路径已就绪，需在 Windows 跑一次）。
- [ ] 单文件远程编译超时；编译产物（`.42m`）是否拉回待确认。

### 版本与增强（P6 ✅ 功能已完成）

#### 补全（✅ 已完成 2026-10-21）

> 实现记录：[PLAN-P6-COMPLETION.md](PLAN-P6-COMPLETION.md)。

- [x] 关键字补全：清单由 `scripts/generate-keywords.mjs` 从 grammar 源码抽取
      （BDL 361 / PER 76），`make lint` 会校验不漂移（改 grammar 忘了重生成 → 失败）。
- [x] 同文件符号补全：函数、报表、MAIN、变量、常量、TYPE、记录、字段、参数。
- [x] 跨文件补全：其它已打开文档里的函数/报表/main（不做全工作区索引，见决策）。
- [x] **表单字段补全**：复用 P3b 的 `Resolver.FormFieldsAt`，按光标位置取当前
      表单（绑定之前的补全不给字段）。
- [x] `.` 之后只给字段/成员，不给关键字。
- [x] 注释/字符串里不补全（用语法树判定，字符串里的 `--` 也能正确处理）。
- [x] `completionItem/resolve` 已声明（部分客户端不支持时会不显示文档字段）。
- （不做）记录成员按类型精确推导：Genero 弱类型，收益低于复杂度。
- [x] 全工作区符号索引（✅ 0.2.0）：`internal/wsindex`，后台建 + gob+gzip 快照，
      按 (mtime,size) 增量、按解析器指纹整份失效。见 [PLAN-P6-INDEX.md](PLAN-P6-INDEX.md)。

#### 重命名（✅ 已完成 2026-10-21）

> 实现记录：[PLAN-P6-RENAME.md](PLAN-P6-RENAME.md)。

- [x] `textDocument/prepareRename` + `textDocument/rename`。
- [x] 范围与 references 一致：**只改当前文件**（跨文件需工作区索引，
      漏改比不支持更糟）。
- [x] 七道闸门：可解析 / 不在注释字符串 / 是本文件符号 / 新名合法 /
      新名不撞名 / 新名不是关键字。
- [x] prepareRename 拒绝时**报错**（客户端不弹输入框），而不是返回空编辑。
- [x] 关键字与"已声明符号"的检查顺序修正：`DEFINE first INTEGER` 里
      `first` 虽是关键字但也是合法符号，应当能改。
- [ ] 跨文件重命名——索引已就绪（0.2.0），但仍**刻意不做**：索引里的调用点是
      名字匹配，可能命中同名局部变量；且可见性取决于链接资料是否完整，
      漏改一处比不支持更糟。要做得对需要把 `link.Resolver.VisibleFunctions`
      与索引结合，并允许"资料不全时拒绝重命名"。见 PLAN-P6-INDEX.md §4。

#### Code Action（✅ 已完成 2026-10-21）

> 实现记录：[PLAN-P6-CODEACTION.md](PLAN-P6-CODEACTION.md)。

- [x] `textDocument/codeAction`，只声明 quickfix。
- [x] 字段拼错 → 「改为 %q」：候选来自光标处真实表单（复用 P3b），
      编辑距离阈值 len/3（夹 1..4）内才提，首个标 isPreferred。
- [x] `.per` 布局 tag / SCREEN RECORD 引用不存在 → 同样的「改为 %q」。
- [x] 只处理 source=bdl-lsp 的诊断（部署的 fglcomp 诊断不碰）。
- [x] 刻意不做：自动加 item 到表单（列宽/schema 无法推断）、创建空 .per、
      删除字段引用（破坏性）、重复 item 修复（语义不明）。理由写进代码注释。
- [ ] 批量修复（fix all）——未排期。

#### 语义高亮（✅ 已完成 2026-10-21）

> 实现记录：[PLAN-P6-SEMTOK.md](PLAN-P6-SEMTOK.md)。

- [x] `textDocument/semanticTokens/full`，legend 6 类型 / 3 修饰位。
- [x] **只标注用户自定义符号**（函数/变量/常量/类型/字段/参数），
      不发关键字、字符串、注释、数字——那些 TextMate 已经做得很准，
      再报一遍会抢同一批 token，客户端合并规则不一致时反而更差。
- [x] GLOBALS 块内符号带 `static`；常量带 `readonly`；定义处带 `declaration`。
- [x] 相对编码 + UTF-16 长度（有单测解码后断言严格升序）。
- [x] Zed 侧不消费 LSP semantic tokens（走 `highlights.scm`），已在文档说明。
- [ ] 使用处的语义着色——未排期（需全文件符号解析，成本高收益低）。

#### record `.*` / `BY NAME` 成员校验（✅ 已完成 2026-10-21，默认 off）

> 实现记录：[PLAN-P6-RECORDMEMBER.md](PLAN-P6-RECORDMEMBER.md)。

- [x] `BY NAME rec.member` 的成员存在性校验（`INPUT`/`DISPLAY`/`CONSTRUCT`）。
- [x] `rec.*` 通配**不校验**：语法树里没有 property 节点（`*` 被 grammar 吞掉），
      语义上也不存在"某成员是否存在"的问题。
- [x] 作用域按函数隔离 + 模块级回退；类型成员表带容器前缀
      （不同作用域的同名记录不能互相覆盖）。
- [x] 五类"不可校验"按 opaque 处理：子记录组、`RECORD LIKE table.*`、
      多变量 DEFINE 错配、参数遮蔽、数组内置方法。
- [x] **默认 off**（`recordMemberDiagnostics`）：`LIKE table.*` 占语料 87% 的引用，
      静态看不到列名，硬开启会大面积误报。
- [x] 语料评估 `cmd/record-audit`：全量 7683 文件 →
      可校验 281160 处、报出 157 处（**0.1%**），抽查确认为真阳性。
- [x] **`collectDefines` 多变量 DEFINE 错配**：已修，回归测试
      `TestMultiVariableDefineTypes`（`internal/index/index_test.go`）。
- [x] `LIKE table.*` 的成员校验（✅ 0.2.1）：schema 索引落地后 `RECORD LIKE t.*`
      可校验；`record-member-unknown` 仍默认 off（开启后语料 24 条，见 PLAN-P6-SCHEMA.md §4.1）。

#### 其余 P6 项

- [x] record `.*` 成员校验（`INPUT BY NAME rec.*` / `DISPLAY BY NAME rec.*`）—— 已完成，默认关闭
      （见 [PLAN-P6-RECORDMEMBER.md](PLAN-P6-RECORDMEMBER.md)）。
- [x] 语义高亮（VSCode + Zed）—— 已完成（见 [PLAN-P6-SEMTOK.md](PLAN-P6-SEMTOK.md)；Zed 走 `highlights.scm`）。
- [x] 数据库 schema 索引（✅ 0.2.1）：`internal/schema` 解析 `<模组根>/<模组>/sch/*.sch`
      （3127 文件 → 2879 表，118 ms），`RECORD LIKE t.*` 的成员可校验、`rec.` 补全给全部列。
      见 [PLAN-P6-SCHEMA.md](PLAN-P6-SCHEMA.md)。
- [ ] 纯 Go SFTP（仅在确有需求时评估）。
- [ ] 性能缓存与 macOS 兼容（5.0 未排期）。

---

## 6.1 P7 Backlog（2026-10-21 立项）

> 完整设计：[PLAN-P7.md](PLAN-P7.md)。执行顺序 P7-A → B → C → D → E。  
> 立项依据（实测 24 个 `.4gl`）：诊断 1733 条，其中 `variable-source-unresolved`
> **1717 条（99.1%）**。噪声已高到不可用，P7 是**可用性修复期**而非增值期。  
> **收工状态（0.1.2）**：全量 15108 文件 334299 → **289304** 条；
> 剩余 98% 是内置全局，正是 P7-C 的目标。

### P7-B 诊断级别统一 + 调用点误报修正 —— ✅ 已完成（0.1.2）

- [x] `internal/diag/registry.go`：诊断码集中注册（Code/Default/Description/Category），
      级别解析统一走 `diag.SeverityFor(code, settings, legacy)`。
- [x] 新增统一配置 `bdl.diagnostics.<code>`，优先级
      `diagnostics.<code>` > 旧键（`syntaxDiagnostics` 等）> 注册表默认。
- [x] 旧配置键保留并标注 deprecated（至少两个版本不删）。
- [x] 拼错保护：`diagnostics` 里出现未注册的码 → 记 warn。
- [ ] `make diagnostics-check`：Go 注册表 ↔ `docs/DIAGNOSTICS.md` ↔ VSCode enum 三处一致。
      （留到 P7-C，码表变大时一起做）
- [x] **修调用点误报**：`IF fn() THEN` / `LET x = fn(1)` 的函数名被当成变量检查。
      query 增加 `(expression name: (variable (identifier) @skip))`。
- [x] 新增 `function-not-found`（默认 off；实测开着会报 3660 条，等 P7-C/D）。
- [x] 复测：24 文件 `variable-source-unresolved` 1717 → **889**；
      全量见 §B-6 与 [PLAN-P7.md](PLAN-P7.md) §10.5。
- [x] ~~Q10 待确认~~ → **已定稿：全部默认 `warning`**（用户 2026-10-21）。
      `syntax-error` 也从 `error` 降为 `warning`；旧键 `syntaxDiagnostics` 默认值同步改，
      避免新旧配置行为不一致。`record-member-unknown` 仍默认 `off`。

### P7-C 内置函数与变量（用户提问已答复：分两类）

> 判据：`tree-sitter parse` 有 ERROR → **grammar 缺口，改 grammar**；
> 无 ERROR 但语义未知 → **插件侧符号表**。实测 `STATUS`/`SQLCA`/`ARG_VAL`/
> `fgl_getenv`/`SET_COUNT` 都能正常解析（语义类）；`STARTLOG` 报 3 个 ERROR（语法类）。

> **P7-B 已把目标量化**（`make diag-audit`，全量 15108 文件）：
> `variable-source-unresolved` 287448 条里，前 12 个名字占了 28.4 万条（98%）：
>
> ```
> SQLCA 148641 · INT_FLAG 61382 · STATUS 42508 · UI 9832 · BASE 6342
> XML 2977 · COM 2636 · OS 2023 · SUM 2007 · TIME 1542 · DIALOG 557 · PAGENO 514
> ```
>
> 只要内置表覆盖这十来个名字，全量诊断立刻从 28.9 万掉到 1 万量级。
> 长尾（`WORKSERVICEIMPL…` 之类）是用户的跨模组全局，归 P7-D 或保持不报。

- [x] `internal/builtin`：内置变量/函数/类/常量符号表（Genero 2.50 基线）**96 条**。
      数据在 `internal/builtin/data/builtins.json`（`go:embed`），补一条名字只改 JSON。
- [x] `cmd/builtin-audit`：扫语料提取"未定义且不在表里"的标识符，按频次+文件数排序，
      带"疑似内置/疑似用户"粗判提示。`make builtin-audit` / `make builtin-check`。
- [x] 接入三处：① `variable-source-unresolved` 白名单 ② `function-not-found` 白名单
      ③ 补全（`fgl_` 前缀等）④ 悬停显示签名与出处。
- [x] 复测（2026-10-21）：全量 15108 文件诊断 **289304 → 7886（−97.3%）**；
      `variable-source-unresolved` **287448 → 6030**；24 文件样本 **889 → 7**。
      剩余 6030 条里前 20 名全是用户跨模组全局（`S_*`、`WORKSERVICEIMPL…`）→ P7-D。
- [x] 配置与后续更新（用户追加要求"应该有配置的地方，允许后续更新"）：
      `bdl.builtins.enable` / `paths` / `disable` + 项目覆盖文件
      `<root>/.bdl-lsp/builtins.json`（自动发现、2s 节流热更新）。
      设计见 [PLAN-P7.md](PLAN-P7.md) §4.4。
- [x] 确认 `cl_null` / `cl_user` / `cl_gr_numfmt` **不是内置函数**（是 `lib/4gl/cl_null.4gl:16`
      一类用户函数），归 P7-D；单测里有专门断言防止误加。
- [x] **grammar 缺口清单**（`tree-sitter-bdl`，即 P7-C-2）：`make grammar-audit`
      （`internal/gramaudit` + `cmd/grammar-audit`）收集最内层 ERROR 上下文按频次排序。
      **实测 15108 文件里只有 18 个解析失败（266 节点），逐个人工确认全是语料自身问题**
      （拼写 / 截断 / 多程序拼接 / `.per` 文档附录全角标点），唯一真实缺口是
      SQL `CASE … END CASE` 表达式（1 个文件），三种消歧规则都被语料否掉 → **不改，留档**。
      基线由 `TestCorpusGrammarBaseline` 冻结（白名单 + 允许变少）。详见 PLAN-P7 §10.10。

### P7-D 程序链接资料索引（大项）

> 用户核心澄清：**链接只放大"函数"可见性，不放大"变量"可见性**。
> GLOBALS 变量只认当前文件定义/引入，不跨链接 —— `internal/globals` 逻辑一行不改。
>
> **真实数据已到位**：`link.csv`，529042 B，sha256 `3ca6ff10…`，**15587 行**。
> 全部设计已按实测值定稿，见 [PLAN-P7.md](PLAN-P7.md) §5.3 / §11。

- [x] 数据模型：**索引主键 = MAIN 程序代码**（不是程序编号）。
      实测：组内恰好 1 个 MAIN = 5374/5849（91.9%）；0 个 = 81；≥2 个 = 5；
      另外 **160 个组的 `GAL01` 不在自己组内**，证实"编号≠文件名"。
- [x] 文档结构（**Q12 定稿**）：`<项目根>/.link/links.jsonl`（**不是** `.bdl-lsp/link/`）
      + `links.meta.json`。实测单文件 **415 KB**（剔除隐式组后），读取 10–20 ms。
- [ ] 分片触发条件（Q11）：体积 > 8 MB / 解析 > 200 ms / 明细 > 200k / 索引重建 > 500 ms，
      任一满足才切 `shards/`；首版不预先分片。
- [x] 导入校验：`gal04='Y'` 过滤（实测 51 行被过滤）；0 MAIN → orphan（实测 451 组，含 389 组文件全缺失）；
      ≥2 MAIN → ambiguous（5 组）；重复代码去重；**CSV 必须按 `utf-8-sig` 读**（带 BOM）。
- [x] 路径解析：`bdl.link.moduleRoots`（默认 `["tiptop","topcust"]`，自动探测）；
      模组目录**大小写不敏感**匹配（实测数据 92 个模组全大写，磁盘全小写）。
- [x] 缺失文件不报错：实测 **800 条 / 700 个代码**找不到（集中在语料中不存在的
      `WPC`/`WSUB`/`WMB`/`WZZ`/`WAG`/`AZZ`/`WLIB`），计入 meta `missingFiles`。
- [x] ~~可见性矩阵~~ → **Q14 二次定稿：扁平规则**。`sub`/`lib`/`qry` **对所有程序默认可用，
      包括它们自己**，无方向限制。配置项 `bdl.link.globalModules = ["sub","lib","qry"]`。
      原 `lib→sub` 11 处反例**自动消解**（是合法代码），`allowModuleDeps` 白名单**删除**。
- [x] 采集 **S1 纯 Go 直连**（**唯一内置方案**）：`github.com/sijms/go-ora/v2`（已实测模块可获取）；
      密码走环境变量/`.link/secrets.json`(0600)/交互输入，不明文落盘；
      进程内定时（`interval`）+ `onStart` + 指数退避；主动运行三入口（CLI/VSCode 命令/Zed task）。
- [x] ~~采集 S1a 本地 Oracle 驱动~~ → **Q15 三度定稿：取消**。纯 Go 无法直调 Instant Client，
      走 CGO 会让构建/分发/跨平台复杂化；只保留 `go-ora`。不留 build tag、不留 `driver` 配置。
- [x] 采集 **S2 服务器脚本导出**：`scripts/fetch-gal-file.sh`（`read -s` 不走 history，
      `SET MARKUP CSV ON` 直出 CSV，已按真实表结构写好 `gal04='Y'` 过滤）+ `bdl-lsp link import`。
- [x] 采集 **S3 命令执行**：只配驱动位置，凭证**走 stdin**（不进命令行参数，避免 `ps` 泄露）。
- [x] 函数可见性：`局部 → 本文件 → GLOBALS → 链接文件 → 全局模组(sub/lib/qry)`；变量可见性**不变**。
- [x] 诊断：`function-not-found` / `link-data-invalid` / `link-data-stale`。
- [x] ~~`link-file-missing`~~ → **不做**（Q2 答复）。链接有、代码无 → 忽略，零诊断；
      但记录**必须保留在 `links.jsonl`**。
- [x] 性能（按实测分布）：链 p50=1 文件、p90=2 → 典型链 2–10 ms **同步**即可；
      **仅当链文件数 > 20** 才走后台（最大链 `cbmp100` 364 文件约 0.7–1.8 s）。
      全局模组涉及 983 文件（sub 518 + lib 204 + qry 261），只建函数名索引，后台一次性 2–4 s。
      首版仅内存缓存。
- [x] `cmd/link-audit`：用真实 `link.csv` 复跑，核对 §11 的数字
      （链 5846 / orphan 81 / ambiguous 5 / missingFiles 800 / 过滤 51）。
- [x] **R2 反例测试**：两个链接在一起的子文件各自 `DEFINE` 同名变量但无共享 GLOBALS
      → 仍报未定义（防止把变量也放进链接可见性）。
- [x] ~~Q14 待确认~~ → **已定稿：扁平规则**（sub/lib/qry 对所有程序可用，含自己）。
      全局模组**参与** `function-not-found` 判定（否则 `cl_*` 仍大面积误报）。

### P7-D-2 外部函数过滤（✅ 2026-10-21，`0.1.5`）

> 用户 2026-10-21 定稿：函数名在项目里找不到定义，多半是语料快照缺库，
> **只要不是太多就忽略，但必须记录**。

- [x] `link import` 统计"被调用但从无定义"的名字 + 文件数 + 调用次数，
      写进 `links.meta.json` 的 `externals[]`（导入时顺带解析全语料，+9 s）。
- [x] 解析器把满足阈值（≥3 文件 或 ≥5 次）的 externals 并入可见集；
      笔误（1–2 处）仍然报。
- [x] 兜底：候选 >2000 个 → `externalsSkipped`，**不启用**过滤（宁可多报）。
- [x] 度量：全量语料 fnf 86612 → **1358**（−98.4%），诊断总数 94498 → 9244；
      已写进 PLAN-P7 §10.7/§11 与 HANDOVER §4。
- [x] `function-not-found` 默认级别改为 **warning**（Q10 例外只剩
      `record-member-unknown`）。
- [x] 记录出口：`bdl-lsp link status`、`cmd/link-audit -externals`。

### P7-C-2 grammar 缺口（1 项新增）

- [ ] `#{ … }#` 块注释：语法里 `#` 只是**行注释**（`seq("#", /.*/)`），
      语料里有 **533 处** `#{` 当块注释用 → 被注释掉的代码仍被解析，
      会产出 fnf/变量类误报。修法：`comment` 规则增加
      `seq("#{", /[^}]*/, "}#")`，需要 `tree-sitter generate` 重新生成 parser.c。

### P7-E 链接资料 UI

> LSP `documentSymbol` **无法跨文件跳转**（`DocumentSymbol` 无 URI/target 字段，
> 跨文件只能用扁平的 `SymbolInformation`，而它不支持层级）；Zed `outline.scm` 只能引用当前语法树。

- [x] VSCode 自定义 **TreeView**「链接资料」：三级
      `程序名 → {直链子文件 | 隐含模组 → 模组下文件}`，显示为 `APM/apmp500`，
      点击 `vscode.open` 跳到 `apm/4gl/sapmp500.4gl`。
- [x] LSP 自定义请求 `bdl/linkTree` / `bdl/linkReferences` 供视图取数
      （实测最大链 368 文件：首次 3.4 ms、再次 0.3 ms）。
- [x] Zed 降级：**改成不塞 `documentSymbol`**（那会让 VSCode 大纲多出不可跳转的
      假符号），改为 CLI（`link status`）+ 文档说明。见 PLAN-P7 §10.9。
- [x] 无链接资料时显示引导文案，不报错。
- [x] ~~Q13 待确认~~ → **已定稿：默认空白 + 可点击「反查引用程序」**（用户 2026-10-21）。
      反查异步、**先显示数量再分页展开**（实测 `aws_xml` 被 324 组引用，不能一次铺开）。

### P7 新增配置项（约 12 项）

```jsonc
"bdl.diagnostics": { "*": "warning", "record-member-unknown": "off" }
"bdl.builtins.enable": true
"bdl.link.enable" / "dataDir"(".link") / "moduleRoots" / "maxAgeDays"
"bdl.link.globalModules": ["sub", "lib", "qry"]   // 全局可见模组
"bdl.link.sync": { enable / source / interval / onStart
                   db{...} / command{...} }
```

- [x] VSCode `package.json` → **40 项**，`make manifest-check` 通过。
- [x] ~~安装时**提示**把 `.link/` 加入 `.gitignore`~~ → **不做**（用户 2026-10-21
      决定）：`.link/` 里是程序链接索引（模块名、程序码、文件名），不含连接串与
      口令；README 已写明建议 gitignore，再弹提示属于打扰。"不擅自改用户文件"
      的取舍在此之前已经定了，运行时提示这一条不再需要。

### P7 专项风险

- [ ] Windows URI 根因判断可能偏差 → 上机先打印两个客户端 `initialize` 入参再动手。
- [ ] `expression name:` 跳过规则可能误伤 → 先跑语料差异清单人工抽查。
- [ ] 链接资料含生产库信息 → `.link/` 建议 gitignore，文档写明不要提交。
- [ ] 纯 Go 驱动 `go-ora` 依赖 +2–3 MB 二进制（当前 11 MB）；
      本地驱动方案走 build tag `ora-native`，默认不编译。
- [x] ~~本地 Oracle 驱动路径配错~~ → 已取消该方案，无此风险。

### P7 真实数据验证（2026-10-21 已完成）

- [x] 分析 `link.csv`（529042 B / sha256 `3ca6ff10…` / 15587 行）。
- [x] 结论已回写 [PLAN-P7.md](PLAN-P7.md) §5.3 / §5.3.2 / §5.6.1 / §11。
- [x] 关键修正：体积 1–2 MB → **415 KB**；隐含模组 980 → **983**；
      典型链解析 100–300 ms → **2–10 ms**（p50=1 文件）。
- [x] ~~发现待确认项：`lib→sub` 11 处反例~~ → 已随 Q14 改为扁平规则而消解。
- [ ] 待做：`cmd/link-audit` 落地后，用同一份 CSV 复跑并核对数字一致。


---

## 7. 已知风险跟踪

- [ ] ABI 15 ↔ Zed Tree-sitter 运行时兼容性。
- [ ] BDL scanner 在 WASI 下的编译。
- [ ] 15k 文件索引性能。
- [ ] 字段上下文误报率（P3b）。
- [ ] P3a 变量来源校验误报率：保守规则已落地（只查 variable 首标识符、
      Unresolvable 导入时全部放过），`scripts/globals-audit.sh` 人工标注
      **未做**，留待真实使用反馈；用户可 off 兜底。
- [x] P3a 同名 `.global` 解析歧义——已禁止隐式 basename 匹配，
      只走 R2（引用文件目录）/R3（searchPaths）显式规则。
- [ ] 无本机 `fglcomp`/`fglform` 导致的部署验证缺口。
- [ ] 扩展与 LSP 版本错配；回退策略。
- [ ] Windows 无 rsync → scp 回退的稳定性/性能。
- [ ] Zed Windows 行为差异（路径、进程、扩展加载）。
- [ ] macOS 延后带来的构建差异。
- [ ] 沙箱环境（HOME/GOPATH/GOPROXY）对文档/CI 的影响。

---

## 8. 完成定义（DoD）

一个任务完成需满足：

1. 代码 + 中文文档/注释（对外文档中文，代码注释英文亦可）。
2. 单元测试或语料验证通过。
3. `gofmt`/`go vet`/lint 通过（按仓库）。
4. 相关文档更新（PLAN/TODO/README/ENVIRONMENT）。
5. 不把中间产物、日志、缓存提交进 Git。
6. 影响用户行为的变更写明迁移/兼容说明。
7. 涉及平台的功能至少记录 Linux/Windows 验证结果；macOS 单独标注。
8. 涉及 Genero 版本的行为标注 2.50 基线（5.0 未排期）。
