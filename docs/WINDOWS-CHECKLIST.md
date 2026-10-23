# Windows 实机验收清单

> 用途：**PENDING §B1 的执行清单**。CI 自 2026-10-23 起 Linux + Windows 双绿，
> 但**从来没有人在这两个平台上装过包**——"CI 绿"只证明能编译、单元测试能过，
> 不证明 VSIX 能装上、语言服务器能在 VSCode 里跑起来、scp 部署能接通。
>
> 用法：逐条执行 → 通过就把 `- [ ]` 改成 `- [x]`；**不符就保持未勾**，把现象写在该条
> 的「记录」里，并在 [§9 汇总](#9-结果汇总) 登记。命令默认在 **PowerShell**（不是 cmd）里跑。

| 项 | 值 |
|---|---|
| 被测版本 | `bdl-lsp 0.3.11` · `tiptop 0.1.6` · `4gl-full 0.5.6` |
| 机器 | ____（Windows 版本 / 架构，如 Windows 11 23H2 x64） |
| 执行人 / 日期 | ____ |
| 总耗时 | ____ |
| 结论 | ⬜ 全部通过 ⬜ 有失败（见 §9） |

> 标记说明：**⭐** = 2026-10-23 这一轮（0.3.5–0.3.8 / 0.1.3 / 0.5.1–0.5.3）刚改的点，最值得盯；
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
npm run package            # typecheck + 打包 → dist-vsix\4gl-full-0.5.6-win32-x64.vsix
```

- [ ] 1.1 拿到 `4gl-full-0.5.6-win32-x64.vsix`
  - 记录（来源与文件名）：____

### 1.2 安装并确认内置二进制可用

```powershell
code --install-extension .\vsix\4gl-full-0.5.6-win32-x64.vsix --force
# 装完重启（或 Reload Window）VSCode

$ext = (Get-ChildItem "$env:USERPROFILE\.vscode\extensions" -Directory `
        -Filter "darcyjoven.4gl-full-*" | Sort-Object Name | Select-Object -Last 1).FullName
Get-ChildItem "$ext\bin"                       # 期望：bdl-lsp.exe、tiptop.exe、BUILD-INFO.json、若干 .dll
Get-Content    "$ext\bin\BUILD-INFO.json"
& "$ext\bin\bdl-lsp.exe" version               # 期望：bdl-lsp 0.3.11
& "$ext\bin\tiptop.exe"  version               # 期望：tiptop 0.1.6
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
- [ ] 1.7 **轻量档默认（P1-4）**：侧边栏**没有**「链接资料」视图；命令面板里搜
      `Genero BDL: 链接资料` 一条都没有（默认关）；工作区里**没有**被新建 `.project/` 目录
- [ ] 1.8 设置界面里设置分成 **5 组**：基础 / 生产环境 / 链接资料 / 部署 / 数据库字段
      （组标题不再带 "Genero BDL: " 前缀）
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
- [ ] 2.8 链接资料视图（侧边栏「链接资料」）能刷新、展开（没有 `.project/` 资料时给出提示而非报错）
  - 记录：____

---

## 3. ⭐ 本轮修复点回归（0.3.5–0.3.10 / 0.1.3–0.1.5 / 0.5.1–0.5.5）

> 本节涉及链接资料的项目，都**先打开** `bdl.link.enable`（P1-4 之后默认关）。

> 这些是本轮为了让 Windows 变绿而改的东西。**CI 只能证明单测过**，
> 真正的客户端行为（VSCode 收不收得到诊断、点击能不能打开）必须在真机上看。

### 3.1 ⭐ 带空格/中文的路径 + 盘符大小写

- [ ] 3.1 工作区路径含空格与中文（§0.3 就是），§2.2–2.4 全部正常
  - 记录：____
- [ ] 3.2 用**小写盘符**再打开一次同一目录（例如命令行 `code c:\users\...`），诊断与跳转仍正常
  - 记录：____
- [ ] 3.3 把工作区放到 `D:\` 等非系统盘（若本机有），重复 §2.3：悬停/跳转正常
  - 记录：____

### 3.2 ⭐ 链接资料诊断必须挂到 `.project\` 文件上

> 修复前：服务端手写 `"file://"+路径`，Windows 上生成非法 URI（`file://C:/…`），
> 客户端收不到这些诊断。**这是本轮最容易被忽略、但用户会真实遇到的一条。**

```powershell
New-Item -ItemType Directory -Force -Path "$ws\.project" | Out-Null
[IO.File]::WriteAllText("$ws\.project\links.jsonl", "{这不是 JSON}`n")
```

- [ ] 3.4 触发重载（重新打开工作区或执行 `Genero BDL: 重启语言服务器`）后，Problems 面板出现 `link-data-invalid`
  - 记录：____
- [ ] 3.5 该诊断**归属文件是 `.project\links.jsonl`**（点它能在编辑器里打开那个文件，而不是一个不存在的路径）
  - 记录：____

```powershell
# 3.6「旧资料不再告警」检查：写一份 2020 年的 meta
[IO.File]::WriteAllText("$ws\.project\links.jsonl",
  '{"schema":1,"program":"globals_main","module":"AAP","main":"globals_main","files":[["AAP","globals_main"]]}' + "`n")
[IO.File]::WriteAllText("$ws\.project\links.meta.json",
  '{"schema":1,"generatedAt":"2020-01-01T00:00:00+08:00","rowsTotal":1,"rowsY":1,"chains":1}')
```

- [ ] 3.6 **P1-6 起"过期"不再提示**：重启服务器后 Problems 面板**没有**任何来自
      `.project\links.meta.json` / `links.jsonl` 的提示（只有资料坏掉或还在旧 `.link/` 目录时才提示）
  - 记录：____

> 查完把这套假资料清掉（`Remove-Item "$ws\.project\links.jsonl","$ws\.project\links.meta.json"`），
> 否则 §3.3 的索引/符号搜索会受"缺文件"的资料影响。

### 3.2b ⭐ 旧目录 `.link/` 仍可用 + 一键迁移（P1-2）

```powershell
# 造一个"迁移前"的项目：把资料只放在旧目录 .link/
Remove-Item -Recurse -Force "$ws\.project" -ErrorAction SilentlyContinue
New-Item -ItemType Directory -Force -Path "$ws\.link" | Out-Null
[IO.File]::WriteAllText("$ws\.link\links.jsonl",
  '{"schema":1,"program":"globals_main","module":"AAP","main":"globals_main","files":[["AAP","globals_main"]]}' + "`n")
[IO.File]::WriteAllText("$ws\.link\links.meta.json",
  '{"schema":1,"generatedAt":"2026-10-23T00:00:00+08:00","rowsTotal":1,"rowsY":1,"chains":1}')
```

- [ ] 3.2b.1 重新打开工作区后：链接视图/跨文件跳转**照旧可用**（不是"突然没有资料"）
- [ ] 3.2b.2 Problems 面板出现 `link-data-legacy-dir`（归属文件是**旧目录**的 `.link\links.jsonl`），
      并且右下角弹出一次"迁移 / 稍后"提示
- [ ] 3.2b.3 点"迁移"（或执行命令 `Genero BDL: 迁移链接资料目录`）→ 文件出现在 `.project/`，
      语言服务器自动重启，链接资料仍然可用、提示消失
  - 记录：____

### 3.2c ⭐ 大文件提示 + 强制解析（P0-3）

```powershell
# 用一个明显超过默认上限（20000 行）的文件；没有就用生成一个
$big = "$ws\topcust\aap\4gl\big.4gl"
'MAIN' + ("`n  DISPLAY 1" * 30000) + "`nEND MAIN" | Set-Content -Encoding utf8 "$big"
```

- [ ] 3.2c.1 打开 `big.4gl`：Problems 里出现 **warning** 级的 `file-too-large`（不是 information），大纲为空
- [ ] 3.2c.2 **编辑器标题栏右侧**出现"强制解析"图标（`.4gl` 已解析的文件不该有这个图标）
- [ ] 3.2c.3 点它 → 状态栏提示"已强制解析（关闭该文件后失效）"；大纲出现内容、`file-too-large` 消失
- [ ] 3.2c.4 关闭再打开该文件 → `file-too-large` 又出现（有效期到关闭）
  - 记录：____

### 3.3 ⭐ 工作区索引给出的位置能打开

> 先**关掉** `globals_main.4gl` 的编辑器标签——这一步专门验"没打开的文件也在索引里"。

- [ ] 3.7 等索引建完（输出通道会打耗时；大项目几十秒），按 `Ctrl+T` 搜 `globals_probe`
- [ ] 3.8 从搜索结果回车 → **能打开 `globals_main.4gl` 并定位到该函数**（不是"文件不存在"或空白）
  - 记录：____

### 3.4 ⭐ tiptop 的 `secrets.json` 在 Windows 不再被误判

> 修复前：该文件按 unix 权限位校验，Windows 上一律报"权限过松（0666）"，等于这条密码来源不可用。

```powershell
New-Item -ItemType Directory -Force -Path "$ws\.project" | Out-Null
[IO.File]::WriteAllText("$ws\.project\secrets.json", '{"<你的只读账号>": "<密码>"}')

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
- [ ] 4.2 `build.ps1` 末尾的 `version` 自检打印 `bdl-lsp 0.3.11`
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

**不用手写**：打开「部署」总开关（默认关），并在「生产环境」一组填主机 / 端口 /
账号 / 远程根 —— 插件会在部署前把它合成为 `$ws\.project\deploy.json`（带 `createdBy` 标记）。
手写仍然支持（`tiptop deploy -config <文件>`）。

- [ ] 6.0 填一半（例如只填主机）时执行部署：弹出的提示要**说清缺哪一项**
      （"部署需要先配置：… remoteRoot"），而不是含糊失败
- [ ] 6.0b 合成出来的 `$ws\.project\deploy.json` 里 `createdBy` 是 `vscode-bdl <版本>`；
      把该文件手改一下（删掉 `createdBy`）再部署一次 → 原文件被备份成 `deploy.json.bak`

- [ ] 5.1.1 `deploy -list` 列出目标
  ```powershell
  & "$ext\bin\tiptop.exe" deploy -list -workspace "$ws"
  ```
  - 记录：____
- [ ] 5.1.2 `deploy -dry-run` 打印"要传什么、要编译什么"，且**不上传**
  - 记录：____

### 5.1b ⭐ link 局部同步（`-modules` / `-files`）

> **前置**：`bdl.link.enable` 现在**默认关**（轻量档）。先在设置界面
> 「链接资料」一组里把总开关打开，否则界面里看不到链接视图/命令。

> 只重算受影响作业的链，其余链保留——改一两个程序不用整份重跑（全量 10–20 秒）。

```powershell
# 先看"会影响几个作业"（不写盘）
& "$ext\bin\tiptop.exe" link import -csv .project\gal_file.csv -root "$ws" -files topcust/aap/4gl/globals_main.4gl -dry-run
# 按模组更新
& "$ext\bin\tiptop.exe" link sync -root "$ws" -modules lib,sub,qry -dry-run
```

- [ ] 5.1b.1 `-dry-run` 输出里有 **"受影响作业 N 个（程序名…）"**，且没有写盘
- [ ] 5.1b.2 去掉 `-dry-run` 执行后：`link status -root "$ws"` 出现 **"局部更新 … 作业 N 个 / 重算链 M 条"**，
      且**其他作业的链仍在**（对比更新前后 `links.jsonl` 的程序列表）
  - 记录：____

### 5.1c ⭐ 链接资料命令（在 VSCode 里，不用开终端）

> **前置**：`bdl.link.enable` **默认关**，先打开它，命令面板里才会出现
> `Genero BDL: 链接资料：…` 系列命令。

命令面板 `Ctrl+Shift+P` → 输入 `Genero BDL: 链接资料`：

- [ ] 5.1c.1 设置里有 **「Genero BDL: 生产环境」** 一节（主机/端口/服务名/只读账号），
      描述里写明"只写 Workspace、`settings.json` 不要进 git、密码用 `BDL_ORA_PASSWORD`
      或 `.project/secrets.json`"
- [ ] 5.1c.2 **没配数据库**时执行 `链接资料：全量更新`：弹出明确提示，带
      「打开设置」「改用 CSV 导入」两个按钮（不是静默失败、不是看不懂的报错）
- [ ] 5.1c.3 `链接资料：从 CSV 导入`：选一个 CSV → 提示已复制到 `.project/gal_file.csv`
      → 选"全量导入" → 输出通道能看到 tiptop 的进度与统计
- [ ] 5.1c.4 打开一个公共 `.4gl`（被多个作业引用）执行 `链接资料：更新当前文件`：
      预演对话框里显示 **"将更新 N 个作业：…"**；取消不会写盘；确认后其他作业的链仍在
      （用 `链接资料：查看状态` 看"局部更新"行）
- [ ] 5.1c.5 链接资料视图里「反查引用程序」那一行有 **重新同步** 按钮，点它等同 5.1c.4
- [ ] 5.1c.6 关掉 `bdl.link.enable`：命令面板里这些命令消失，视图按钮也消失
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
| 资料明明很久没更新，却没提示 | **设计如此**（P1-6 去掉 7 天过期提示）：同步只由命令触发，插件不因为资料"旧"而报警 |
| 刚打开大项目时 `Ctrl+T` 搜不全 | 工作区索引在后台建（几十秒），期间结果不全属正常 |
| `fglcomp: command not found` | 这是**远端**的问题（目标机没装 Genero），与本机扩展无关 |
| 打开大文件时出现 `file-too-large` / `parse-timeout` 警告 | **有意为之**（P0-3）：这个文件确实没被检查，点标题栏的"强制解析"即可；不想要就调大 `bdl.limits.*` |
| 出现 `link-data-legacy-dir`（hint） | 资料还在旧目录 `.link/`：能用，但建议执行「迁移链接资料目录」搬到 `.project/` |

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
| 3.2b | ⭐ 旧目录迁移 | ⬜ | |
| 3.2c | ⭐ 大文件提示 + 强制解析 | ⬜ | |
| 3.4–3.6 | ⭐ `.project` 诊断落点 + 旧资料不告警（P1-6） | ⬜ | |
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

- 被测版本：`bdl-lsp 0.3.11` / `tiptop 0.1.6` / `4gl-full 0.5.6`
- 机器与架构：
- 失败项与最小复现：
- 语言服务器输出（`Genero BDL: 显示输出` 整段）：