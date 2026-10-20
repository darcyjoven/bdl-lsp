# 用法： . .\scripts\dev-env.ps1
#
# Windows 开发环境变量。与 dev-env.sh 对应；不覆盖 HOME。
$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent $PSScriptRoot
$Toolchain = Join-Path $Root "artifacts\toolchain"

$env:CGO_ENABLED = "1"
$env:CC = "gcc"
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

Write-Host "CGO_ENABLED=$env:CGO_ENABLED"
Write-Host "CC=$env:CC"
Write-Host "CARGO_HOME=$env:CARGO_HOME"
Write-Host "WASI_SDK_PATH=$env:WASI_SDK_PATH"
Write-Host "PATH 前缀已加入 $Toolchain\bin"
