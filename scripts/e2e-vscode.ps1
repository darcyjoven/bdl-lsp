<#
.SYNOPSIS
    VSCode 扩展端到端测试（Windows）。

.DESCRIPTION
    与 scripts/e2e-vscode.sh 对等的 Windows 入口，做同样四件事：
      1. 构建语言服务器（调用 build.ps1，保证 GOENV=off + 原生工具链）；
      2. 安装扩展依赖（缺 node_modules 时才装）；
      3. 类型检查 + 打包 dist；
      4. 在真实 VSCode 扩展宿主里跑测试套件。

    为什么需要单独一份：.sh 在 Windows 上依赖 bash（Git for Windows 自带，
    但不保证在 PATH），而这里的每一步都必须是原生 PowerShell 语义。

.PARAMETER Keep
    失败时保留日志与工作区。

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\e2e-vscode.ps1

.EXAMPLE
    powershell -ExecutionPolicy Bypass -File scripts\e2e-vscode.ps1 -Keep
#>
[CmdletBinding()]
param(
    [switch]$Keep
)

$ErrorActionPreference = 'Stop'

$ScriptDir = Split-Path -Parent $MyInvocation.MyCommand.Path
$LspDir    = Split-Path -Parent $ScriptDir
$ExtDir    = Join-Path (Split-Path -Parent $LspDir) 'vscode-extension'

function Write-Step($Text) { Write-Host "==> $Text" -ForegroundColor Cyan }
function Fail($Text) {
    Write-Host "  [FAIL] $Text" -ForegroundColor Red
    exit 1
}

# ---- 1/4 构建语言服务器 ----
Write-Step '1/4 构建语言服务器'
& (Join-Path $ScriptDir 'build.ps1')
# build.ps1 失败会直接 exit，走到这里说明它成功了。
# 不依赖 $LASTEXITCODE：脚本调用不保证设置它。
$LspBin = Join-Path $LspDir 'artifacts\bin\bdl-lsp.exe'
if (-not (Test-Path $LspBin)) { Fail "未找到 $LspBin" }
Write-Host "    语言服务器：$LspBin"

# ---- 2/4 安装依赖 ----
Write-Step '2/4 安装扩展依赖（如缺失）'
Push-Location $ExtDir
try {
    if (-not (Test-Path 'node_modules')) {
        # NODE_ENV=production 会让 npm 跳过 devDependencies，显式清空并包含
        $env:NODE_ENV = ''
        npm install --no-audit --no-fund --include=dev
        if ($LASTEXITCODE -ne 0) { Fail 'npm install 失败' }
    }

    # ---- 3/4 类型检查与打包 ----
    Write-Step '3/4 类型检查与打包'
    npm run typecheck
    if ($LASTEXITCODE -ne 0) { Fail '类型检查失败' }
    npm run build
    if ($LASTEXITCODE -ne 0) { Fail '打包失败' }

    # ---- 4/4 端到端 ----
    Write-Step '4/4 运行端到端测试'
    if (-not $env:BDL_E2E_BASE) {
        $env:BDL_E2E_BASE = Join-Path $LspDir 'artifacts\e2e'
    }
    $env:BDL_LSP_PATH = $LspBin
    if ($Keep) { $env:BDL_E2E_KEEP = '1' }

    node test/runTest.js
    if ($LASTEXITCODE -ne 0) { Fail 'e2e 失败' }
}
finally {
    Pop-Location
}

Write-Host ''
Write-Host 'e2e 全部通过' -ForegroundColor Green
