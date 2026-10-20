.PHONY: all build test test-race vet fmt lint check corpus regress bench clean help grammars grammars-check e2e-vscode

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
lint: vet
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

## grammars: 从 grammar 源码重新生成 VSCode 的 TextMate 语法
grammars:
	@node scripts/generate-tm-grammars.mjs

## grammars-check: 校验 TextMate 语法为最新且能正确切分样例代码
grammars-check:
	@node scripts/generate-tm-grammars.mjs --check
	@node scripts/verify-tm-grammars.mjs

## e2e-vscode: VSCode 扩展端到端测试（构建 + 打包 + 真实扩展宿主）
e2e-vscode:
	@bash scripts/e2e-vscode.sh

## bench: LSP 端到端延迟基准（didOpen / didChange）
bench: build
	@python3 scripts/lsp-bench.py

## clean: 清理构建产物
clean:
	rm -rf artifacts/bin dist

## help: 列出可用目标
help:
	@sed -n 's/^## //p' Makefile
