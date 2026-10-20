// Package logging 提供 LSP 服务使用的日志。
//
// 约定：
//   - LSP 使用 stdout 传输协议，因此日志只能写 stderr（以及可选的日志文件）。
//   - 使用标准库 log/slog；协议库（go.lsp.dev/protocol）需要 zap，仅在内部使用。
package logging

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// New 创建 logger。level 取值 debug/info/warn/error；logFile 非空时同时写文件。
func New(level, logFile string) (*slog.Logger, func(), error) {
	var lvl slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lvl = slog.LevelDebug
	case "info", "":
		lvl = slog.LevelInfo
	case "warn", "warning":
		lvl = slog.LevelWarn
	case "error":
		lvl = slog.LevelError
	default:
		return nil, nil, fmt.Errorf("未知日志级别 %q", level)
	}

	var writers []io.Writer
	writers = append(writers, os.Stderr)

	closer := func() {}
	if logFile != "" {
		if dir := filepath.Dir(logFile); dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, nil, fmt.Errorf("创建日志目录: %w", err)
			}
		}
		f, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
		if err != nil {
			return nil, nil, fmt.Errorf("打开日志文件: %w", err)
		}
		writers = append(writers, f)
		closer = func() { _ = f.Close() }
	}

	handler := slog.NewTextHandler(io.MultiWriter(writers...), &slog.HandlerOptions{Level: lvl})
	return slog.New(handler), closer, nil
}
