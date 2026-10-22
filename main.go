// Command bdl-lsp 是 Genero BDL / Form 的语言服务器。
//
// 子命令：
//
//	bdl-lsp serve              以 stdio 提供 LSP（默认）
//	bdl-lsp check <file>...    解析文件并输出问题（用于冒烟测试/CI）
//	bdl-lsp deploy             上传到远程服务器并远程编译（见 internal/deploy）
//	bdl-lsp link import|status 导入/查看程序链接资料（见 internal/link）
//	bdl-lsp version            打印版本
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/darcyjoven/bdl-lsp/internal/check"
	"github.com/darcyjoven/bdl-lsp/internal/cmdline"
	"github.com/darcyjoven/bdl-lsp/internal/deploycli"
	"github.com/darcyjoven/bdl-lsp/internal/linkcli"
	"github.com/darcyjoven/bdl-lsp/internal/logging"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"github.com/darcyjoven/bdl-lsp/internal/server"
)

// version 由 -ldflags "-X main.version=..." 注入。
//
// 版本策略（用户 2026-10-21 定）：从 0.1.1 起算；每完成一轮完整改动
// 并提交，就 +1。三个仓库（bdl-lsp / vscode-extension / zed-extension）
// 各自独立记版本，不要求一致。
var version = "0.2.1"

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	sub := "serve"
	if len(args) > 0 {
		sub = args[0]
		args = args[1:]
	}

	switch sub {
	case "version", "--version", "-v":
		fmt.Printf("bdl-lsp %s\n", version)
		return 0
	case "check":
		return runCheck(args)
	case "deploy":
		return deploycli.Run(args, version)
	case "link":
		return linkcli.Run(args, version)
	case "serve", "--stdio":
		return runServe(args)
	default:
		// 兼容 `bdl-lsp --log-level=debug` 这类直接给 flag 的用法
		return runServe(append([]string{sub}, args...))
	}
}

func runCheck(args []string) int {
	return check.Run(args, version)
}

func runServe(args []string) int {
	opts, err := cmdline.Parse(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bdl-lsp: %v\n", err)
		return 2
	}

	logger, closeLog, err := logging.New(opts.LogLevel, opts.LogFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "bdl-lsp: 初始化日志失败: %v\n", err)
		return 2
	}
	defer closeLog()

	pool, err := parser.New()
	if err != nil {
		logger.Error("初始化 tree-sitter 失败", "error", err)
		return 1
	}
	defer pool.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	srv := server.New(pool, logger, version)
	logger.Info("bdl-lsp 启动", "version", version, "mode", "stdio")

	if err := srv.Serve(ctx); err != nil {
		var exitErr interface{ ExitCode() int }
		if errors.As(err, &exitErr) {
			logger.Warn("服务退出", "error", err)
			return exitErr.ExitCode()
		}
		logger.Error("服务退出", "error", err)
		return 1
	}
	logger.Info("bdl-lsp 正常退出")
	return 0
}
