<#
.SYNOPSIS
    构建 bdl-lsp（Windows 原生）。

.DESCRIPTION
    给不想装 make 的机器用的等价入口，做的事与 Makefile 的 Windows 分支一致：
      1. 屏蔽全局 go env 文件（GOENV=off），避免被其他 SDK 污染；
      2. 显式指定原生工具链（GOOS/GOARCH/CGO_ENABLED/CC/CXX）；
      3. 因 GOENV=off 会丢掉 env 文件里的 GOPROXY，这里补回；
      4. 调用 go build 产出 artifacts\bin\bdl-lsp.exe；
      5. 可选跑测试与冒烟。

    为什么必须 GOENV=off：
      go env -w 写的是 %APPDATA%\go\env，是全局的。只要装过 HarmonyOS 之类
      的 SDK，它可能把 GOOS=android、GOARCH=arm64、CC=clang、
      CGO_CFLAGS=--target=aarch64-linux-ohos 写进去，之后任何 CGO 构建
      都会失败并报 "unrecognized command-line option"。

    兼容性说明（踩过的坑）：
      - PowerShell 5.1 在 $ErrorActionPreference='Stop' 下，对原生命令做
        `2>&1` 重定向会把 stderr 变成 NativeCommandError 终止错误。
        因此本脚本用 'Continue'，全部靠显式的退出码/文件存在性判断。
      - `go build -o <路径>` 不会自动补 .exe（只有省略 -o 时才补），
        所以这里显式处理扩展名。

.PARAMETER Arch
    目标架构，默认 amd64。可用 arm64（需对应 MinGW 工具链）。

.PARAMETER CC
    C 编译器，默认 gcc（MinGW-w64）。

.PARAMETER Out
    输出路径，默认 artifacts\bin\bdl-lsp.exe。

.PARAMETER Version
    注入版本号（写到 main.version），默认 0.1.0-dev。

.PARAMETER Proxy
    GOPROXY，默认 https://goproxy.cn,direct。

.PARAMETER Test
    构建后运行 go test ./...

.PARAMETER Race
    构建后运行 go test -race ./...（隐含 -Test）。

.PARAMETER Check
    构建后在 testdata 上跑冒烟（sample.4gl / sample.per，应全部通过）。

.PARAMETER Clean
    构建前删除输出目录。

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\build.ps1

.EXAMPLE
    # 构建 + 测试 + 冒烟
    powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -Test -Check

.EXAMPLE
    # 指定架构与编译器
    powershell -ExecutionPolicy Bypass -File scripts\build.ps1 -Arch arm64 -CC clang
#>
[CmdletBinding()]
param(
    [string]$Arch = "amd64",
    [string]$CC = "gcc",
    [string]$Out = "",
    [string]$Version = "0.1.0-dev",
    [string]$Proxy = "https://goproxy.cn,direct",
    [switch]$Test,
    [switch]$Race,
    [switch]$Check,
    [switch]$Clean
)

# 刻意用 Continue：原生命令的 stderr 在 Stop 下会抛 NativeCommandError。
# 所有失败都靠显式检查 $LASTEXITCODE / Test-Path 兜住。
$ErrorActionPreference = "Continue"

$Root = Split-Path -Parent $PSScriptRoot
if (-not $Out) {
    $Out = Join-Path $Root "artifacts\bin\bdl-lsp.exe"
}

function Step($msg) { Write-Host "==> $msg" -ForegroundColor Cyan }
function Info($msg) { Write-Host "    $msg" }
function Fail($msg) {
    Write-Host "  [FAIL] $msg" -ForegroundColor Red
    exit 1
}

# ---------------------------------------------------------------------------
# 0. 前置检查
# ---------------------------------------------------------------------------

Step "检查工具链"

if (-not (Get-Command go -ErrorAction SilentlyContinue)) {
    Fail "找不到 go，请先安装 Go 1.24+ 并加入 PATH"
}
Info "go      : $((go version) -join ' ')"

if (-not (Get-Command $CC -ErrorAction SilentlyContinue)) {
    Fail "找不到 C 编译器 '$CC'（CGO 必需）。请安装 MinGW-w64 并把它的 bin 目录加入 PATH。"
}
$gccVersion = (& $CC --version) | Select-Object -First 1
Info "cc      : $gccVersion"

# grammar 与 bdl-lsp 是兄弟目录：go.mod 里用 replace ../tree-sitter-bdl。
foreach ($dir in @("..\tree-sitter-bdl", "..\tree-sitter-per")) {
    $p = [System.IO.Path]::GetFullPath((Join-Path $Root $dir))
    if (-not (Test-Path $p)) {
        Fail "缺少 grammar 目录：$p`n（go.mod 用 replace 指向兄弟目录，需与 bdl-lsp 同级）"
    }
}
Info "grammar : ..\tree-sitter-bdl, ..\tree-sitter-per"

# ---------------------------------------------------------------------------
# 1~5. 设置环境 → 依赖 → 构建 → 验证 → 测试
# ---------------------------------------------------------------------------
#
# 这些变量只影响本进程与它启动的子进程，不改动系统的 go env 文件。
# 结束后（含失败）一律恢复，避免污染同一控制台里的后续命令。

$saved = @{}
foreach ($name in @("GOENV", "GOOS", "GOARCH", "CGO_ENABLED", "CC", "CXX", "GOPROXY")) {
    $saved[$name] = [Environment]::GetEnvironmentVariable($name, "Process")
}

try {
    Step "设置构建环境"
    $env:GOENV = "off"
    $env:GOOS = "windows"
    $env:GOARCH = $Arch
    $env:CGO_ENABLED = "1"
    $env:CC = $CC
    $env:CXX = "g++"
    $env:GOPROXY = $Proxy

    Info "GOENV=$env:GOENV  GOOS=$env:GOOS  GOARCH=$env:GOARCH"
    Info "CGO_ENABLED=$env:CGO_ENABLED  CC=$env:CC  CXX=$env:CXX"
    Info "GOPROXY=$env:GOPROXY"

    # go build 的 output 不会自动加 .exe（只有省略 -o 时才会），这里兜一层
    if (-not $Out.ToLower().EndsWith(".exe")) {
        $Out = "$Out.exe"
    }
    $outDir = Split-Path -Parent $Out

    if ($Clean -and (Test-Path $outDir)) {
        Step "清理构建产物"
        Remove-Item -Recurse -Force $outDir
        Info "已删除 $outDir"
    }

    Step "解析依赖"
    # GOENV=off 时 go 不读 env 文件，GOPROXY 必须由环境变量提供（上面已设）
    & go mod download
    if ($LASTEXITCODE -ne 0) { Fail "go mod download 失败（检查网络或 GOPROXY）" }
    Info "依赖已就绪"

    Step "构建 $Out"
    if (-not (Test-Path $outDir)) {
        New-Item -ItemType Directory -Force -Path $outDir | Out-Null
    }

    & go build -trimpath -ldflags "-X main.version=$Version" -o $Out .
    if ($LASTEXITCODE -ne 0) { Fail "go build 失败" }
    if (-not (Test-Path $Out)) { Fail "构建完成但找不到产物：$Out" }

    $sizeMB = [math]::Round((Get-Item $Out).Length / 1MB, 2)
    Info "产物：$Out ($sizeMB MB)"

    # 立刻验证产物能跑：CGO 链接问题常常到运行时才暴露
    # （典型：缺 libwinpthread-1.dll，进程一启动就 0xc0000135）
    Step "验证产物"
    $versionOut = & $Out version
    if ($LASTEXITCODE -ne 0) {
        Fail "产物无法运行（exit=$LASTEXITCODE）`n（常见原因：MinGW 运行库缺失，把 mingw64\bin 加入 PATH）"
    }
    Info ($versionOut -join ' ')

    if ($Race) { $Test = $true }

    if ($Test) {
        Step "运行单元测试"
        if ($Race) {
            & go test -race ./...
        } else {
            & go test ./...
        }
        if ($LASTEXITCODE -ne 0) { Fail "测试失败" }
        Info "测试通过"
    }

    if ($Check) {
        Step "运行冒烟（testdata）"
        & $Out check testdata/sample.4gl testdata/sample.per
        if ($LASTEXITCODE -ne 0) { Fail "冒烟未通过" }
    }
}
finally {
    foreach ($name in @($saved.Keys)) {
        [Environment]::SetEnvironmentVariable($name, $saved[$name], "Process")
    }
}

Write-Host ""
Write-Host "完成。" -ForegroundColor Green
Write-Host ""
Write-Host "在 VSCode 中使用这个二进制：" -ForegroundColor Yellow
Write-Host "  方式一：设置 bdl.server.path = $Out"
Write-Host "  方式二：把 bdl-lsp.exe 复制到工作区 .bdl-lsp\bin\ 下"
Write-Host "  方式三：把 $outDir 加入 PATH"
Write-Host ""
Write-Host "验证："
Write-Host "  & '$Out' version"
