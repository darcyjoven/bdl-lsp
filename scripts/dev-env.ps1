# 用法： . .\scripts\dev-env.ps1
#
# Windows 开发环境变量。与 dev-env.sh 对应；不覆盖 HOME。
#
# 关于 GOENV=off：
#   %APPDATA%\go\env 是**全局**的，装过 HarmonyOS 之类 SDK 后可能被写成
#   GOOS=android / GOARCH=arm64 / CC=clang / CGO_CFLAGS=--target=aarch64-linux-ohos，
#   之后任何 CGO 构建都会报 "unrecognized command-line option"。
#   Makefile 的 Windows 分支与 scripts\build.ps1 都用 GOENV=off 屏蔽了它，
#   这里保持一致，避免"用 make 能过、直接 go build 不能过"的割裂。
#
# 注意 GOENV=off 的副作用：env 文件里的 GOPROXY 也一并失效，
# 所以必须显式给出 GOPROXY，否则会退回 proxy.golang.org（国内经常超时）。
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$Toolchain = Join-Path $Root "artifacts\toolchain"

$env:GOENV = "off"
$env:GOOS = "windows"
if (-not $env:GOARCH) {
    $env:GOARCH = "amd64"
}
$env:CGO_ENABLED = "1"
$env:CC = "gcc"
$env:CXX = "g++"
if (-not $env:GOPROXY) {
    $env:GOPROXY = "https://goproxy.cn,direct"
}

$env:RUSTUP_HOME = Join-Path $Toolchain "rustup"
$env:CARGO_HOME = Join-Path $Toolchain "cargo"
$env:WASI_SDK_PATH = Join-Path $Toolchain "wasi-sdk"

$env:Path = @(
    (Join-Path $Toolchain "bin"),
    (Join-Path $env:CARGO_HOME "bin"),
    (Join-Path $env:WASI_SDK_PATH "bin"),
    (Join-Path $Root "artifacts\bin"),
    $env:Path
) -join ";"

New-Item -ItemType Directory -Force -Path (Join-Path $Root "artifacts\bin") | Out-Null
New-Item -ItemType Directory -Force -Path (Join-Path $Toolchain "bin") | Out-Null

Write-Host "GOENV=$env:GOENV（屏蔽 %APPDATA%\go\env）"
Write-Host "GOOS/GOARCH=$env:GOOS/$env:GOARCH"
Write-Host "CGO_ENABLED=$env:CGO_ENABLED"
Write-Host "CC=$env:CC  CXX=$env:CXX"
Write-Host "GOPROXY=$env:GOPROXY"
Write-Host "CARGO_HOME=$env:CARGO_HOME"
Write-Host "WASI_SDK_PATH=$env:WASI_SDK_PATH"
Write-Host "PATH 前缀已加入 $Toolchain\bin"
