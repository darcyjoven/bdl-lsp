// Package cmdline 解析 bdl-lsp 的命令行参数。
package cmdline

import (
	"flag"
	"fmt"
)

// Options 是 serve 模式的启动参数。
type Options struct {
	// LogLevel 取值：debug / info / warn / error。
	LogLevel string
	// LogFile 为空时只写 stderr。
	LogFile string
	// Stdio 表示已显式声明 stdio 传输。
	//
	// 这个 flag 本身不改变行为（stdio 是唯一的传输方式），但必须接受：
	// vscode-languageclient 在 transport=stdio 时会自动给命令行追加
	// `--stdio`，不识别该参数会直接导致服务器启动失败（退出码 2）。
	Stdio bool
}

// Parse 解析 serve 子命令参数。
func Parse(args []string) (Options, error) {
	var opts Options

	fs := flag.NewFlagSet("bdl-lsp serve", flag.ContinueOnError)
	fs.StringVar(&opts.LogLevel, "log-level", "info", "日志级别：debug|info|warn|error")
	fs.StringVar(&opts.LogFile, "log-file", "", "额外写入的日志文件（默认仅 stderr）")
	fs.BoolVar(&opts.Stdio, "stdio", false, "以 stdio 传输（默认行为，接受该参数以兼容 LSP 客户端）")

	if err := fs.Parse(args); err != nil {
		return opts, fmt.Errorf("解析参数: %w", err)
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("未知参数: %v", fs.Args())
	}
	return opts, nil
}
