package cmdline

import "testing"

func TestParseDefaults(t *testing.T) {
	opts, err := Parse(nil)
	if err != nil {
		t.Fatalf("Parse(nil) 出错: %v", err)
	}
	if opts.LogLevel != "info" {
		t.Errorf("默认 LogLevel = %q，期望 info", opts.LogLevel)
	}
	if opts.LogFile != "" {
		t.Errorf("默认 LogFile = %q，期望空", opts.LogFile)
	}
	if opts.Stdio {
		t.Error("默认 Stdio 应为 false")
	}
}

// TestParseStdioFlag 防回归：vscode-languageclient 会自动追加 --stdio，
// 若不识别会导致服务器启动即失败。
func TestParseStdioFlag(t *testing.T) {
	opts, err := Parse([]string{"--stdio"})
	if err != nil {
		t.Fatalf("--stdio 应被接受，实际出错: %v", err)
	}
	if !opts.Stdio {
		t.Error("--stdio 应把 Stdio 置为 true")
	}
}

// TestParseClientStyleArgs 还原 VSCode 客户端的真实调用：
//
//	bdl-lsp serve --stdio [--log-level debug]
func TestParseClientStyleArgs(t *testing.T) {
	opts, err := Parse([]string{"--stdio", "--log-level", "debug"})
	if err != nil {
		t.Fatalf("客户端风格参数解析失败: %v", err)
	}
	if opts.LogLevel != "debug" {
		t.Errorf("LogLevel = %q，期望 debug", opts.LogLevel)
	}
	if !opts.Stdio {
		t.Error("Stdio 应为 true")
	}
}

func TestParseLogFile(t *testing.T) {
	opts, err := Parse([]string{"--log-file", "/tmp/x.log"})
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	if opts.LogFile != "/tmp/x.log" {
		t.Errorf("LogFile = %q", opts.LogFile)
	}
}

func TestParseUnknownFlag(t *testing.T) {
	if _, err := Parse([]string{"--nope"}); err == nil {
		t.Error("未知 flag 应报错")
	}
}

func TestParsePositionalArg(t *testing.T) {
	if _, err := Parse([]string{"extra"}); err == nil {
		t.Error("多余的位置参数应报错")
	}
}
