# 环境检查（Windows）。用法：
#   powershell -ExecutionPolicy Bypass -File scripts\env-check.ps1
# 返回 0 = 必需项齐全；1 = 有必需项缺失。
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
if (Test-Path (Join-Path $PSScriptRoot "dev-env.ps1")) {
    . (Join-Path $PSScriptRoot "dev-env.ps1")
}

$requiredMissing = 0
$optionalMissing = 0

function Ok($msg)   { Write-Host "  [ OK ] $msg" }
function Warn($msg) { Write-Host "  [WARN] $msg"; $script:optionalMissing++ }
function Bad($msg)  { Write-Host "  [FAIL] $msg"; $script:requiredMissing++ }
function Has($cmd)  { return $null -ne (Get-Command $cmd -ErrorAction SilentlyContinue) }

Write-Host "== BDL LSP 环境检查（Windows） =="
Write-Host "PWD=$PWD"
Write-Host ""

Write-Host "-- 基础工具"
if (Has "go") {
    $goVersion = (go version)
    if ($goVersion -match "go1\.(2[4-9]|[3-9][0-9])") { Ok $goVersion } else { Bad "Go 版本过低：$goVersion，需要 1.24+" }
} else { Bad "缺少 go（需要 1.24+）" }
if (Has "gcc") { Ok ((gcc --version | Select-Object -First 1)) } else { Bad "缺少 MinGW-w64 gcc（CGO 需要）" }
if (Has "git") { Ok (git --version) } else { Bad "缺少 git" }

Write-Host ""
Write-Host "-- Go / CGO"
if (Has "go") {
    $cgo = (go env CGO_ENABLED)
    if ($cgo -eq "1") { Ok "CGO_ENABLED=1" } else { Bad "CGO_ENABLED=$cgo（需要 1）" }
    if ($env:CC) { Ok "CC=$env:CC" } else { Warn "CC 未设置（dev-env.ps1 会设为 gcc）" }

    # 全局 go env 文件（%APPDATA%\go\env）可能被别的 SDK 写脏。
    # 典型：装过 HarmonyOS SDK 后出现 GOOS=android / GOARCH=arm64 /
    # CC=clang / CGO_CFLAGS=--target=aarch64-linux-ohos，之后任何 CGO
    # 构建都会报 "unrecognized command-line option"。
    # 这里只做提示，不判失败：Makefile 与 build.ps1 都用 GOENV=off 兜住了。
    $envFile = Join-Path $env:APPDATA 'go\env'
    if (Test-Path $envFile) {
        $risky = @()
        foreach ($line in (Get-Content $envFile -ErrorAction SilentlyContinue)) {
            if ($line -match '^\s*(GOOS|GOARCH|CC|CXX|CGO_CFLAGS|CGO_LDFLAGS|GOFLAGS)\s*=') {
                $risky += $line.Trim()
            }
        }
        if ($risky.Count -gt 0) {
            Warn "全局 go env 含可能污染 CGO 的项（将用 GOENV=off 屏蔽）："
            foreach ($r in $risky) { Write-Host "         $r" }
        } else {
            Ok "全局 go env 无 CGO 污染项"
        }
    } else {
        Ok "无全局 go env 文件"
    }
}

Write-Host ""
Write-Host "-- Node / 前端"
if (Has "node") {
    $nodeMajor = [int]((node --version).TrimStart("v").Split(".")[0])
    if ($nodeMajor -ge 24) { Ok (node --version) } else { Warn "Node 版本较低：$(node --version)，建议 24+" }
} else { Bad "缺少 node（需要 24+）" }
if (Has "pnpm") { Ok "pnpm $(pnpm --version)" } else { Bad "缺少 pnpm" }

Write-Host ""
Write-Host "-- Tree-sitter"
if (Has "tree-sitter") { Ok (tree-sitter --version) } else { Bad "缺少 tree-sitter CLI（期望 0.26.5）" }

Write-Host ""
Write-Host "-- Rust / WASM"
if (Has "rustc") { Ok (rustc --version) } else { Bad "缺少 rustc（Zed 扩展需要）" }
if (Has "cargo") { Ok (cargo --version) } else { Bad "缺少 cargo" }
if (Has "rustup") {
    $targets = (rustup target list --installed) -join "`n"
    if ($targets -match "wasm32-wasip2") { Ok "target wasm32-wasip2 已安装" } else { Bad "缺少 rust target wasm32-wasip2" }
} else { Bad "缺少 rustup" }
if (Has "clang") {
    $clangVersion = (clang --version | Select-Object -First 1)
    if ($clangVersion -match "wasi-sdk") { Ok $clangVersion } else { Warn "clang 不是 wasi-sdk：$clangVersion" }
} else { Bad "缺少 wasi-sdk clang（设置 WASI_SDK_PATH）" }

Write-Host ""
Write-Host "-- 编辑器 / 部署"
if (Has "code") { Ok (code --version | Select-Object -First 1) } else { Warn "缺少 code CLI（VSCode 扩展开发/e2e 需要）" }
if (Has "ssh") { Ok ((ssh -V 2>&1 | Out-String).Trim()) } else { Bad "缺少 ssh（OpenSSH Client）" }
if (Has "scp") { Ok "scp 可用" } else { Bad "缺少 scp（OpenSSH Client）" }
if (Has "rsync") { Ok "rsync 可用" } else { Warn "缺少 rsync（v1 Windows 用 scp 回退，可接受）" }
if (Has "gh") { Ok ((gh --version | Select-Object -First 1)) } else { Warn "缺少 gh（发布暂不排期，可选）" }

Write-Host ""
Write-Host "-- 可选：Genero 编译器（通常只在服务器）"
if (Has "fglform") { Ok "fglform 可用" } else { Warn "未安装 fglform（远程编译/服务器验证时需要）" }
if (Has "fglcomp") { Ok "fglcomp 可用" } else { Warn "未安装 fglcomp（远程编译/服务器验证时需要）" }

Write-Host ""
if ($requiredMissing -eq 0) {
    Write-Host "结果：必需项齐全（可选缺失 $optionalMissing 项）"
    exit 0
} else {
    Write-Host "结果：缺少 $requiredMissing 个必需项（可选缺失 $optionalMissing 项）"
    exit 1
}
