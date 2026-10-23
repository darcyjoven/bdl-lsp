# Windows 实机验收清单

> 用途：**PENDING §B1 的执行清单**。CI 自 2026-10-23 起 Linux + Windows 双绿，
> 但**从来没有人在这两个平台上装过包**——"CI 绿"只证明能编译、单元测试能过，
> 不证明 VSIX 能装上、语言服务器能在 VSCode 里跑起来、scp 部署能接通。
>
> 用法：逐条执行 → 通过就把 `- [ ]` 改成 `- [x]`；**不符就保持未勾**，把现象写在该条
> 的「记录」里，并在 [§9 汇总](#9-结果汇总) 登记。命令默认在 **PowerShell**（不是 cmd）里跑。

| 项 | 值 |
|---|---|
| 被测版本 | `bdl-lsp 0.3.4` · `tiptop 0.1.2` · `4gl-full 0.5.1` |
| 机器 | ____（Windows 版本 / 架构，如 Windows 11 23H2 x64） |
| 执行人 / 日期 | ____ |
| 总耗时 | ____ |
| 结论 | ⬜ 全部通过 ⬜ 有失败（见 §9） |

> 标记说明：**⭐** = 本轮（0.3.4 / 0.1.2）刚修的点，最值得盯；
> **[需服务器]** = 要有真实 SSH + Genero 编译机的环境才能做，没有就跳过并如实标注。

---

## 0. 环境准备

### 0.1 工具链

```powershell
go version          # 期望 go1.24.x windows/amd64
gcc --version       # MinGW-w64 必需（CGO 编 tree-sitter）
where.exe gcc       # 期望 ...\mingw64\bin\gcc.exe
node --version      # 期望 v24.x（走源码/e2e 路线才需要）
code --version      # VSCode
ssh -V              # OpenSSH（部署项才需要）
```

- [ ] 0.1 上述命令**全部**有输出且版本达标
  - 记录：____

### 0.2 仓库与行尾

```powershell
git config --global core.autocrlf false   # 兜底；仓库里已有 .gitattributes
```

- [ ] 0.2 已设置 `core.autocrlf false`
  - 记录：____

### 0.3 构造一个自包含测试工作区

**刻意放在「带空格 + 中文」的路径下**——这是 Windows 路径/URI 最容易出问题的地方（见 §3）。

> 用下面这个 `Write-Fixture` 而不是 `Set-Content`：PowerShell 5.1 的
> `-Encoding UTF8` 会写 **BOM**（会让语法解析多出莫名其妙的错误），
> 且写出来是 CRLF（项目的 `.4gl/.global/.per` 要求 LF）。
> 这里用「字符串数组 + `-join "`n"`」显式构造 LF 内容，
> 并用 `[IO.File]::WriteAllText` 写无 BOM 的 UTF-8。

```powershell
$ws = "$env:USERPROFILE\bdl win 测试\topprod"
New-Item -ItemType Directory -Force -Path `
  "$ws\topcust\aap\4gl", "$ws\topcust\aap\per", "$ws\topcust\config" | Out-Null

function Write-Fixture($path, [string[]]$lines) {
  [IO.File]::WriteAllText($path, ($lines -join "`n") + "`n")
}

Write-Fixture "$ws\topcust\config\globals_defs.global" @(
  "GLOBALS"
  "    DEFINE g_counter INTEGER"
  "    DEFINE g_rec RECORD"
  "        id INTEGER"
  "    END RECORD"
  "END GLOBALS"
)

Write-Fixture "$ws\topcust\aap\4gl\globals_main.4gl" @(
  'GLOBALS "../../config/globals_defs.global"'
  "MAIN"
  "    DEFINE l_local INTEGER"
  "    LET l_local = g_counter"
  "    DISPLAY l_local"
  "    DISPLAY g_rec.id"
  "END MAIN"
  ""
  "FUNCTION globals_probe()"
  "    DISPLAY g_counter"
  "END FUNCTION"
)

Write-Fixture "$ws\topcust\aap\per\form_defs.per" @(
  "SCHEMA formonly"
  "LAYOUT"
  "GRID"
  "{"
  "  [good_field]"
  "}"
  "END"
  "END"
  "ATTRIBUTES"
  "EDIT good_field = formonly.good_field;"
  "END"
)

Write-Fixture "$ws\topcust\aap\4gl\field_main.4gl" @(
  "MAIN"
  '  OPEN WINDOW w WITH FORM "form_defs"'
  "  INPUT g_x FROM good_field"
  "    AFTER FIELD good_field"
  "    AFTER FIELD bad_field"
  "  END INPUT"
  "END MAIN"
)

Write-Fixture "$ws\topcust\aap\4gl\broken.4gl" @(
  "MAIN"
  '    DISPLAY "it''s unterminated'
  "END MAIN"
)

# 自检：首字节应是 47 4C 4F（"GLO"），不是 EF BB BF（BOM）
Format-Hex "$ws\topcust\config\globals_defs.global" | Select-Object -First 1
```

> 这份工作区里的 4 个 .4gl/.global/.per 文件与 `vscode-extension/test/fixtures/` 里的
> e2e 夹具同源，期望结果在 [§2](#2-语言服务在真机上的表现) 写明。

- [ ] 0.3 工作区已创建，`$ws` 路径含空格与中文，首字节**不是** `EF BB BF`
  - 记录（贴出实际路径与 Format-Hex 首行）：____

> **换 PowerShell 窗口后变量会丢**，先重新定义再继续（下面各节都用到）：
> ```powershell
> $ws  = "$env:USERPROFILE\bdl win 测试\topprod"
> $ext = (Get-ChildItem "$env:USERPROFILE\.vscode\extensions" -Directory `
>         -Filter "darcyjoven.4gl-full-*" | Sort-Object Name | Select-Object -Last 1).FullName
> ```

> **取证提示**：出问题时先执行命令 `Genero BDL: 显示输出`（输出通道），
> 里面是语言服务器 stderr 与客户端日志；失败项请把整段贴进 §9。

---

## 1. 装包：VSIX 能否真的装上、内置二进制能否跑

> 这是**最关键**的一段：CI 的 e2e 用的是"开发目录 + 扩展宿主"，**没有验证过 VSIX 安装路径**。

### 1.1 取得 win32-x64 VSIX（二选一）

**路线 A（推荐，用 CI 产物）**

```powershell
# 触发一次打包（也可在 GitHub 页面 Actions → bundle-vsix → Run workflow 里点）
gh workflow run bundle-vsix.yml -R darcyjoven/vscode-bdl `
  -f bdl_lsp_ref=main -f tiptop_ref=main

gh run list -R darcyjoven/vscode-bdl -w bundle-vsix -L 3      # 等它变绿
gh run download -R darcyjoven/vscode-bdl <run-id> -n vsix-win32-x64 -D .\vsix
Get-ChildItem .\vsix -Recurse -Filter *.vsix | Select-Object FullName
```

**路线 B（本机打包，需已克隆三个仓库）**

> 以下命令从**三个仓库的共同父目录**开始（`bdl-lsp\`、`tiptop\`、`vscode-extension\` 并列）。

```powershell
cd bdl-lsp   ; powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -Check
cd ..\tiptop ; powershell -ExecutionPolicy Bypass -File scripts\build.ps1
cd ..\vscode-extension
npm install
npm run package            # typecheck + 打包 → dist-vsix\4gl-full-0.5.1-win32-x64.vsix
```

- [ ] 1.1 拿到 `4gl-full-0.5.1-win32-x64.vsix`
  - 记录（来源与文件名）：____

### 1.2 安装并确认内置二进制可用

```powershell
code --install-extension .\vsix\4gl-full-0.5.1-win32-x64.vsix --force
# 装完重启（或 Reload Window）VSCode

$ext = (Get-ChildItem "$env:USERPROFILE\.vscode\extensions" -Directory `
        -Filter "darcyjoven.4gl-full-*" | Sort-Object Name | Select-Object -Last 1).FullName
Get-ChildItem "$ext\bin"                       # 期望：bdl-lsp.exe、tiptop.exe、BUILD-INFO.json、若干 .dll
Get-Content    "$ext\bin\BUILD-INFO.json"
& "$ext\bin\bdl-lsp.exe" version               # 期望：bdl-lsp 0.3.4
& "$ext\bin\tiptop.exe"  version               # 期望：tiptop 0.1.2
& "$ext\bin\bdl-lsp.exe" check "$ws\topcust\aap\4gl\broken.4gl"
```

- [ ] 1.2 安装成功，`bin\` 下两个 exe + `BUILD-INFO.json` 都在
  - 记录：____
- [ ] 1.3 两个 exe 直接运行**不报缺 DLL**（`0xc0000135` 之类）
  - 记录：____
- [ ] 1.4 `Genero BDL: 显示语言服务器信息` 命令输出的「路径 / 来源」指向扩展内置 `bin\bdl-lsp.exe`
  - 记录：____

### 1.5 在 VSCode 里打开工作区

```powershell
code "$ws"
```

- [ ] 1.5 打开 `$ws` 后，状态栏/输出里语言服务器**运行中**（命令 `Genero BDL: 显示语言服务器信息`）
  - 记录：____
- [ ] 1.6 重启机器或重启 VSCode 后再打开一次，服务器仍能起来（排除"只在首次会话可用"）
  - 记录：____

---

## 2. 语言服务在真机上的表现

打开 §0.3 的工作区，逐个文件看**真实 UI**（Problems 面板、悬停浮窗、跳转、补全）。

> 下面的"CLI 口径"是从这份夹具在 Linux 上实跑出来的（`bdl-lsp check`），
> 与 UI 里的数量可能因诊断级别设置略有差异，但**位置和归属文件应当一致**；
> 语义类（GLOBALS/字段）的期望同时被 e2e 套件断言（`vscode-extension/test/suite/index.js`）。

- [ ] 2.1 `broken.4gl`：出现**语法类**诊断，落在第 2 行第 13 列（未闭合字符串）
  - CLI 口径（可与 UI 对照）：
    ```powershell
    & "$ext\bin\bdl-lsp.exe" check "$ws\topcust\aap\4gl\broken.4gl"
    # 期望：2 个问题；`2:13 语法错误` 是主诊断
    ```
  - 记录：____
- [ ] 2.2 `globals_main.4gl`：**没有** `globals-file-not-found`；大纲（Outline）能看到 `MAIN`
  - 记录：____
- [ ] 2.3 光标停在 `g_counter` 上 → 悬停显示来源（`globals_defs.global`）
  - 记录：____
- [ ] 2.4 `g_counter` 上按 `F12` → 跳到 `topcust\config\globals_defs.global`（跨目录的 `../../config/` 引用）
  - 记录：____
- [ ] 2.5 `field_main.4gl`：`bad_field` 报 `field-not-in-form`，`good_field` **不报**；诊断可点开关联位置跳到 `.per`
  - 记录：____
- [ ] 2.6 格式化：在 `field_main.4gl` 上按 `Shift+Alt+F`，关键字大小写/缩进按设置生效；对 `.per` 执行格式化应被**拒绝**
  - 记录：____
- [ ] 2.7 补全：在 `MAIN` 里输入 `DIS` 能出关键字补全；输入一个已定义函数名能出符号补全
  - 记录：____
- [ ] 2.8 链接资料视图（侧边栏「链接资料」）能刷新、展开（没有 `.link/` 资料时给出提示而非报错）
  - 记录：____

---

## 3. ⭐ 本轮修复点回归（0.3.4 / 0.1.2）

> 这些是本轮为了让 Windows 变绿而改的东西。**CI 只能证明单测过**，
> 真正的客户端行为（VSCode 收不收得到诊断、点击能不能打开）必须在真机上看。

### 3.1 ⭐ 带空格/中文的路径 + 盘符大小写

- [ ] 3.1 工作区路径含空格与中文（§0.3 就是），§2.2–2.4 全部正常
  - 记录：____
- [ ] 3.2 用**小写盘符**再打开一次同一目录（例如命令行 `code c:\users\...`），诊断与跳转仍正常
  - 记录：____
- [ ] 3.3 把工作区放到 `D:\` 等非系统盘（若本机有），重复 §2.3：悬停/跳转正常
  - 记录：____

### 3.2 ⭐ 链接资料诊断必须挂到 `.link\` 文件上

> 修复前：服务端手写 `"file://"+路径`，Windows 上生成非法 URI（`file://C:/…`），
> 客户端收不到这些诊断。**这是本轮最容易被忽略、但用户会真实遇到的一条。**

```powershell
New-Item -ItemType Directory -Force -Path "$ws\.link" | Out-Null
[IO.File]::WriteAllText("$ws\.link\links.jsonl", "{这不是 JSON}`n")
```

- [ ] 3.4 触发重载（重新打开工作区或执行 `Genero BDL: 重启语言服务器`）后，Problems 面板出现 `link-data-invalid`
  - 记录：____
- [ ] 3.5 该诊断**归属文件是 `.link\links.jsonl`**（点它能在编辑器里打开那个文件，而不是一个不存在的路径）
  - 记录：____

```powershell
# 3.6「过期」检查需要一份**有效**的资料，否则会先走 3.4 的坏资料分支
[IO.File]::WriteAllText("$ws\.link\links.jsonl",
  '{"schema":1,"program":"globals_main","module":"AAP","main":"globals_main","files":[["AAP","globals_main"]]}' + "`n")
[IO.File]::WriteAllText("$ws\.link\links.meta.json",
  '{"schema":1,"generatedAt":"2020-01-01T00:00:00+08:00","rowsTotal":1,"rowsY":1,"chains":1}')
```

- [ ] 3.6 重启服务器后出现 `link-data-stale`（**hint 级**，Problems 面板可能要放宽级别才显示；挂在 `links.meta.json` 上）
  - 记录：____

> 查完把这套假资料清掉（`Remove-Item "$ws\.link\links.jsonl","$ws\.link\links.meta.json"`），
> 否则 §3.3 的索引/符号搜索会受"缺文件"的资料影响。

### 3.3 ⭐ 工作区索引给出的位置能打开

> 先**关掉** `globals_main.4gl` 的编辑器标签——这一步专门验"没打开的文件也在索引里"。

- [ ] 3.7 等索引建完（输出通道会打耗时；大项目几十秒），按 `Ctrl+T` 搜 `globals_probe`
- [ ] 3.8 从搜索结果回车 → **能打开 `globals_main.4gl` 并定位到该函数**（不是"文件不存在"或空白）
  - 记录：____

### 3.4 ⭐ tiptop 的 `secrets.json` 在 Windows 不再被误判

> 修复前：该文件按 unix 权限位校验，Windows 上一律报"权限过松（0666）"，等于这条密码来源不可用。

```powershell
New-Item -ItemType Directory -Force -Path "$ws\.link" | Out-Null
[IO.File]::WriteAllText("$ws\.link\secrets.json", '{"<你的只读账号>": "<密码>"}')

$ext = (Get-ChildItem "$env:USERPROFILE\.vscode\extensions" -Directory `
        -Filter "darcyjoven.4gl-full-*" | Sort-Object Name | Select-Object -Last 1).FullName
& "$ext\bin\tiptop.exe" link status -root "$ws"
```

- [ ] 3.9 `link status` 能跑（没有资料时提示"没有资料"属正常）
  - 记录：____
- [ ] 3.10 用 `-source golang -dry-run` 试一次采集：**不再**出现「权限过松（0666），请 chmod 600」；
  连不上库时报的是连接错误（这才是预期）
  ```powershell
  & "$ext\bin\tiptop.exe" link sync -root "$ws" -source golang `
      -host <主机> -port 1521 -service <服务名> -user <只读账号> -dry-run
  ```
  - 记录：____

### 3.5 ⭐ 行尾（CRLF）不再干扰 lint

- [ ] 3.11 源码路线下 gofmt 检查通过（与 `make lint` 同口径，排除 `artifacts/` 里的模块缓存）
  ```powershell
  cd bdl-lsp
  gofmt -l . | Select-String -NotMatch '^artifacts/'    # 期望：无输出
  ```
  - 记录：____

---

## 4. 源码构建与自检（源码路线，可与 §1 并行）

```powershell
cd bdl-lsp
powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -Check
```

- [ ] 4.1 构建成功，产物 `artifacts\bin\bdl-lsp.exe` 存在（PE 文件）
  - 记录：____
- [ ] 4.2 `build.ps1` 末尾的 `version` 自检打印 `bdl-lsp 0.3.4`
  - 记录：____
- [ ] 4.3 `-Check` 的样例冒烟通过（期望 `共 2 个文件，0 个有问题`）
  - 记录：____
- [ ] 4.4 全量单测通过：
  ```powershell
  cd bdl-lsp ; go test -race ./...
  ```
  - 记录：____
- [ ] 4.5 tiptop 同样构建 + 单测通过：
  ```powershell
  cd tiptop ; powershell -ExecutionPolicy Bypass -File scripts\build.ps1 ; go test -race ./...
  ```
  - 记录：____

---

## 5. 链接资料与部署 **[需服务器]**

> 需要：真实 SSH 可达的 Genero 机器（`fglcomp`/`fglform` 可用）。没有就跳过并标注"未测"。

### 5.1 准备部署配置

在 `$ws` 下建 `.bdl-lsp\deploy.json`（可从 `tiptop\examples\deploy.json` 复制），填真实主机/账号/远程根。

- [ ] 5.1.1 `deploy -list` 列出目标
  ```powershell
  & "$ext\bin\tiptop.exe" deploy -list -workspace "$ws"
  ```
  - 记录：____
- [ ] 5.1.2 `deploy -dry-run` 打印"要传什么、要编译什么"，且**不上传**
  - 记录：____

### 5.2 真实单文件部署 + scp 回退

- [ ] 5.2.1 Windows 无 rsync → 输出里出现**显式回退**：
  `↪ 回退到 scp（请在输出中确认，这不是静默降级）`
  - 记录（贴出那几行）：____
- [ ] 5.2.2 单文件部署成功：
  ```powershell
  & "$ext\bin\tiptop.exe" deploy -workspace "$ws" "$ws\topcust\aap\4gl\globals_main.4gl"
  ```
  - 记录：____
- [ ] 5.2.3 故意把源码改坏（删一个分号/写错标识符）再部署 → 远程 `fglcomp` 的错误**按行号映射回本地文件**，出现在输出里
  - 记录（贴出错误行与其对应的本地行号）：____
- [ ] 5.2.4 通过 VSCode 命令 `Genero BDL: 部署当前文件并编译` 走一遍（编辑器内路径，与命令行同一配置）
  - 记录：____
- [ ] 5.2.5 `Genero BDL: 部署预演（不上传）` 与 `Genero BDL: 列出部署目标` 两个命令可用
  - 记录：____

---

## 6. 自动化 e2e（可选，等价于 CI 的 Windows 行为在本机复现）

> 前提：三个仓库并列检出（`bdl-lsp\`、`tiptop\`、`vscode-extension\`）且
> `vscode-extension` 已 `npm install`。脚本会自己构建语言服务器、类型检查、
> 用 `dist` 起一个真实 VSCode 扩展宿主跑 38 条用例。

```powershell
cd bdl-lsp
powershell -ExecutionPolicy Bypass -File scripts\e2e-vscode.ps1
# 失败时想保留现场：加 -Keep
```

- [ ] 6.1 输出 `用例 38/38 通过` 且 `VSCode 退出码=0`
  - 记录：____

---

## 7. Windows 上**不要**误判为 Bug 的现象

| 现象 | 说明 |
|---|---|
| 部署时提示"缺少 rsync"并回退 scp | **设计如此**：Windows 默认没有 rsync，v1 的 Windows 验收就是走 scp 回退 |
| `record-member-unknown` 一条都不报 | 该诊断出厂默认 `off`（`LIKE table.*` 引用太多，误报风险高） |
| Problems 面板看不到 `link-data-stale` | 它是 **hint** 级，面板默认过滤可能不显示；放宽级别即可见 |
| 刚打开大项目时 `Ctrl+T` 搜不全 | 工作区索引在后台建（几十秒），期间结果不全属正常 |
| `fglcomp: command not found` | 这是**远端**的问题（目标机没装 Genero），与本机扩展无关 |

---

## 8. Zed（Windows）

**暂停中**（用户 2026-10-22 决定，见 PENDING §D）——本次**不测**，勾选留空。

---

## 9. 结果汇总

| # | 项目 | 结果 | 现象 / 证据 |
|---|---|---|---|
| 0.1 | 工具链 | ⬜ | |
| 0.2 | 行尾设置 | ⬜ | |
| 0.3 | 测试工作区 | ⬜ | |
| 1.1 | 取得 VSIX | ⬜ | |
| 1.2–1.4 | 装包 + 内置二进制 | ⬜ | |
| 1.5–1.6 | VSCode 内服务器运行 | ⬜ | |
| 2.1–2.8 | 语言服务 UI 表现 | ⬜ | |
| 3.1–3.3 | ⭐ 路径/盘符 | ⬜ | |
| 3.4–3.6 | ⭐ `.link` 诊断落点 | ⬜ | |
| 3.7–3.8 | ⭐ 索引位置可打开 | ⬜ | |
| 3.9–3.10 | ⭐ secrets.json | ⬜ | |
| 3.11 | ⭐ gofmt / CRLF | ⬜ | |
| 4.1–4.5 | 源码构建 + 单测 | ⬜ | |
| 5.1.1–5.2.5 | 部署（需服务器） | ⬜ | |
| 6.1 | e2e 38/38 | ⬜ | |

**失败项汇总**（每条写：编号 / 期望 / 实际 / 日志或截图路径）：

1.
2.

**回归信息带回 Linux 侧**（贴到 `PENDING.md` §B1 或开 issue 时用）：

- 被测版本：`bdl-lsp 0.3.4` / `tiptop 0.1.2` / `4gl-full 0.5.1`
- 机器与架构：
- 失败项与最小复现：
- 语言服务器输出（`Genero BDL: 显示输出` 整段）：