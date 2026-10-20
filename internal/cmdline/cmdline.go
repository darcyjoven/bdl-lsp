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
}

// Parse 解析 serve 子命令参数。
func Parse(args []string) (Options, error) {
	var opts Options

	fs := flag.NewFlagSet("bdl-lsp serve", flag.ContinueOnError)
	fs.StringVar(&opts.LogLevel, "log-level", "info", "日志级别：debug|info|warn|error")
	fs.StringVar(&opts.LogFile, "log-file", "", "额外写入的日志文件（默认仅 stderr）")

	if err := fs.Parse(args); err != nil {
		return opts, fmt.Errorf("解析参数: %w", err)
	}
	if fs.NArg() > 0 {
		return opts, fmt.Errorf("未知参数: %v", fs.Args())
	}
	return opts, nil
}
