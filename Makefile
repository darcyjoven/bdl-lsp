.PHONY: all build build-ps test test-race vet fmt lint check corpus regress diag-audit bench clean help grammars grammars-check keywords keywords-check manifest-check builtin-audit builtin-check e2e-vscode zed-check zed-check-windows

GO ?= go

# ---- 平台分支 ----
# Windows 下 CGO 需要本机 MinGW gcc；同时全局 go env（%APPDATA%\go\env）可能被
# 其他 SDK 改写（如 HarmonyOS 的 GOOS=android / GOARCH=arm64 / CC=clang /
# CGO_CFLAGS=--target=aarch64-linux-ohos），会让 CGO 直接编译失败。
# 这里用 GOENV=off 屏蔽该文件，再显式给出 Windows 原生工具链，互不干扰。
# Linux/macOS 流程保持原样。
ifeq ($(OS),Windows_NT)
  WIN_ARCH ?= amd64
  WIN_CC   ?= gcc
  WIN_CXX  ?= g++
  WIN_GOPROXY ?= https://goproxy.cn,direct
  export GOENV := off
  export GOOS := windows
  export GOARCH := $(WIN_ARCH)
  export CGO_ENABLED := 1
  export CC := $(WIN_CC)
  export CXX := $(WIN_CXX)
  # GOENV=off 会连带丢掉 env 文件里的 GOPROXY，这里补回
  export GOPROXY := $(WIN_GOPROXY)
  BIN := artifacts/bin/bdl-lsp.exe
  EXE := .exe
  # GnuWin32 make + 本地代码页会把 recipe 里的中文吐成乱码，Windows 下用 ASCII 提示
  MSG_BUILT := built
else
  BIN := artifacts/bin/bdl-lsp
  EXE :=
  MSG_BUILT := 已生成
endif

all: build

## build: 构建本地二进制到 artifacts/bin/bdl-lsp（Windows 为 bdl-lsp.exe）
build:
	$(GO) build -trimpath -o $(BIN) .
	@echo "$(MSG_BUILT) $(BIN)"

## test: 运行单元测试
test:
	$(GO) test ./...

## test-race: 带竞态检测运行测试（较慢）
test-race:
	$(GO) test -race ./...

## vet: go vet 静态检查
vet:
	$(GO) vet ./...

## fmt: 格式化代码
fmt:
	$(GO) fmt ./...

## lint: fmt 检查 + vet
lint: vet keywords-check builtin-check
	@GOFMT="$$(GOBIN="$$($(GO) env GOROOT)/bin" command -v gofmt || echo "$$($(GO) env GOROOT)/bin/gofmt")"; \
	out=$$("$$GOFMT" -l . | grep -v '^artifacts/' || true); \
	if [ -n "$$out" ]; then echo "以下文件未格式化："; echo "$$out"; exit 1; fi; \
	echo "格式检查通过"

## check: 用 testdata 样例做冒烟（应全部通过）
check: build
	$(BIN) check testdata/sample.4gl testdata/sample.per

## corpus: 对真实语料抽样做解析冒烟（需 tiptop-gp）
corpus: build
	@bash scripts/corpus-smoke.sh

## regress: 全量语料回归（较慢，发布前/大改后运行）
regress: build
	@bash scripts/corpus-regress.sh

## diag-audit: 统计语义诊断逐码分布（降噪基线；可用 CODES=code=level 覆盖）
diag-audit: build
	@$(GO) run ./cmd/diag-audit $(if $(CODES),-diagnostics $(CODES),) $(if $(LIMIT),-limit $(LIMIT),) -names 20

## builtin-audit: 内置符号表（P7-C）的缺口审计——列出语料里"不在表里"的名字
builtin-audit: build
	@$(GO) run ./cmd/builtin-audit $(if $(LIMIT),-limit $(LIMIT),) -show $(or $(SHOW),40)

## builtin-check: 校验内置符号数据文件（格式/排序/重名/kind）
builtin-check:
	@$(GO) run ./cmd/builtin-audit -check

## grammars: 从 grammar 源码重新生成 VSCode 的 TextMate 语法
grammars:
	@node scripts/generate-tm-grammars.mjs

## grammars-check: 校验 TextMate 语法为最新且能正确切分样例代码
grammars-check:
	@node scripts/generate-tm-grammars.mjs --check
	@node scripts/verify-tm-grammars.mjs

## keywords: 从 grammar 源码重新生成补全用的关键字清单
keywords:
	@node scripts/generate-keywords.mjs

## keywords-check: 校验关键字清单与 grammar 同步（grammar 加了关键字就必须重新生成）
keywords-check:
	@node scripts/generate-keywords.mjs --check

## manifest-check: 校验 VSCode 清单与语言配置（JSON/正则/扩展名映射）
manifest-check:
	@node scripts/verify-vscode-manifest.mjs

## e2e-vscode: VSCode 扩展端到端测试（构建 + 打包 + 真实扩展宿主）
# 有 bash 时走 .sh；Windows 无 bash 时自动改用 PowerShell 版
e2e-vscode:
ifeq ($(OS),Windows_NT)
	@powershell -NoProfile -ExecutionPolicy Bypass -File scripts/e2e-vscode.ps1
else
	@bash scripts/e2e-vscode.sh
endif

## build-ps: 用 PowerShell 构建（Windows 无 make 时的等价入口）
build-ps:
	@powershell -NoProfile -ExecutionPolicy Bypass -File scripts/build.ps1

## zed-check: 校验 Zed 扩展（清单 + Tree-sitter query）
# 需要 zed-extension 与两个 grammar 目录并列；脚本在 zed-extension/tests 下。
zed-check:
	@python3 ../zed-extension/tests/verify-manifest.py
	@node ../zed-extension/tests/verify-queries.mjs

## zed-check-windows: Windows 下的等价入口（无 python3 时只跑 query 校验）
zed-check-windows:
	@node ..\zed-extension\tests\verify-queries.mjs

## bench: LSP 端到端延迟基准（didOpen / didChange）
bench: build
	@python3 scripts/lsp-bench.py

## clean: 清理构建产物
clean:
	rm -rf artifacts/bin dist

## help: 列出可用目标
help:
	@sed -n 's/^## //p' Makefile
