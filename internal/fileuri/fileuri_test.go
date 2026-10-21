package fileuri

import (
	"net/url"
	"runtime"
	"strings"
	"testing"
)

// TestToPathWindows 直接测 Windows 归一化，不依赖宿主平台。
//
// 这是本包最重要的一组用例：CI 经常没有 Windows runner，
// 而 P7-A-2 的缺陷只在 Windows 上暴露。把纯函数抽出来测，
// 就能在 Linux 上守住 Windows 语义。
func TestToPathWindows(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		want string
	}{
		// ── P7-A-2 的核心回归：VSCode 编码了盘符冒号 ──
		{
			name: "VSCode 风格 %3A 小写盘符",
			uri:  "file:///c%3A/work/x.4gl",
			want: `C:\work\x.4gl`,
		},
		{
			name: "VSCode 风格 %3A 大写盘符",
			uri:  "file:///C%3A/work/x.4gl",
			want: `C:\work\x.4gl`,
		},
		// ── Zed 风格：冒号不编码 ──
		{
			name: "Zed 风格裸冒号",
			uri:  "file:///c:/work/x.4gl",
			want: `C:\work\x.4gl`,
		},
		{
			name: "Zed 风格大写盘符",
			uri:  "file:///C:/work/x.4gl",
			want: `C:\work\x.4gl`,
		},
		// ── 空格 / # / 非 ASCII ──
		{
			name: "空格 %20",
			uri:  "file:///c%3A/a%20b/y.4gl",
			want: `C:\a b\y.4gl`,
		},
		{
			name: "井号 %23（VSCode 也编码 #）",
			uri:  "file:///c%3A/a%23b/w.4gl",
			want: `C:\a#b\w.4gl`,
		},
		{
			name: "中文非 ASCII",
			uri:  "file:///C%3A/%E4%B8%AD%E6%96%87/z.4gl",
			want: `C:\中文\z.4gl`,
		},
		// ── UNC ──
		{
			name: "UNC 共享",
			uri:  "file://server/share/x.4gl",
			want: `\\server\share\x.4gl`,
		},
		// ── 无盘符（当前盘根） ──
		{
			name: "无盘符按根路径",
			uri:  "file:///work/a.4gl",
			want: `\work\a.4gl`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 直接调 Windows 分支的纯函数，绕开 runtime.GOOS
			u := mustParse(t, tc.uri)
			got := toPathWindows(u.host, u.path)
			if got != tc.want {
				t.Errorf("toPathWindows(%q)\n  得到 %q\n  期望 %q", tc.uri, got, tc.want)
			}
		})
	}
}

// TestToPathUnix 覆盖非 Windows 语义。
func TestToPathUnix(t *testing.T) {
	cases := []struct {
		name string
		uri  string
		want string
	}{
		{"普通绝对路径", "file:///home/u/x.4gl", "/home/u/x.4gl"},
		{"空格", "file:///home/a%20b/x.4gl", "/home/a b/x.4gl"},
		{"中文", "file:///home/%E4%B8%AD/x.4gl", "/home/中/x.4gl"},
		{"井号", "file:///tmp/a%23b/x.4gl", "/tmp/a#b/x.4gl"},
		{"UNC 转双斜杠", "file://server/share/x", "//server/share/x"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			u := mustParse(t, tc.uri)
			got := toPathUnix(u.host, u.path)
			if got != tc.want {
				t.Errorf("toPathUnix(%q)\n  得到 %q\n  期望 %q", tc.uri, got, tc.want)
			}
		})
	}
}

// TestToPathRejectsNonFile 非 file:// 必须报错，而不是静默返回乱码。
func TestToPathRejectsNonFile(t *testing.T) {
	for _, uri := range []string{
		"http://example.com/x.4gl",
		"untitled:Untitled-1",
		"git:/x.4gl",
	} {
		if _, err := ToPath(uri); err == nil {
			t.Errorf("ToPath(%q) 期望报错，实际成功", uri)
		}
	}
}

// TestToPathMalformed 畸形 URI 不应 panic。
func TestToPathMalformed(t *testing.T) {
	for _, uri := range []string{
		"file:///a%zzb", // 非法转义
		"file://",
		"",
	} {
		if _, err := ToPath(uri); err == nil {
			// file:// 与空串解析成功但路径为空，允许；关键是别 panic
			t.Logf("ToPath(%q) 无错误（可接受）", uri)
		}
	}
}

// TestFromPathWindows 覆盖编码方向，形态必须与 VSCode 对齐。
func TestFromPathWindows(t *testing.T) {
	cases := []struct {
		name string
		path string
		want string
	}{
		{
			name: "盘符冒号编码为 %3A（与 vscode-uri 一致）",
			path: `C:\work\x.4gl`,
			want: "file:///C%3A/work/x.4gl",
		},
		{
			name: "小写盘符归一大写",
			path: `c:\work\x.4gl`,
			want: "file:///C%3A/work/x.4gl",
		},
		{
			name: "空格",
			path: `C:\a b\y.4gl`,
			want: "file:///C%3A/a%20b/y.4gl",
		},
		{
			name: "井号",
			path: `C:\a#b\w.4gl`,
			want: "file:///C%3A/a%23b/w.4gl",
		},
		{
			name: "UNC",
			path: `\\server\share\x.4gl`,
			want: "file://server/share/x.4gl",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := FromPath(tc.path)
			if got != tc.want {
				t.Errorf("FromPath(%q)\n  得到 %q\n  期望 %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestFromPathUnix 覆盖 POSIX 路径编码。
func TestFromPathUnix(t *testing.T) {
	cases := []struct {
		path string
		want string
	}{
		{"/home/u/x.4gl", "file:///home/u/x.4gl"},
		{"/home/a b/x.4gl", "file:///home/a%20b/x.4gl"},
		{"/home/中/x.4gl", "file:///home/%E4%B8%AD/x.4gl"},
		{"/tmp/a#b/x.4gl", "file:///tmp/a%23b/x.4gl"},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			got := FromPath(tc.path)
			if got != tc.want {
				t.Errorf("FromPath(%q)\n  得到 %q\n  期望 %q", tc.path, got, tc.want)
			}
		})
	}
}

// TestRoundTrip 往返一致性：ToPath(FromPath(p)) 应还原 p（同平台语义）。
//
// Windows 路径在 Linux 上无法用 ToPath 验证（走 unix 分支），
// 所以这里按宿主平台选语义。
func TestRoundTrip(t *testing.T) {
	var paths []string
	if runtime.GOOS == "windows" {
		paths = []string{
			`C:\work\x.4gl`,
			`C:\a b\y.4gl`,
			`C:\a#b\w.4gl`,
			`C:\中文\z.4gl`,
			`\\server\share\x.4gl`,
		}
	} else {
		paths = []string{
			"/home/u/x.4gl",
			"/home/a b/y.4gl",
			"/tmp/a#b/w.4gl",
			"/home/中/z.4gl",
		}
	}

	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			uri := FromPath(p)
			back, err := ToPath(uri)
			if err != nil {
				t.Fatalf("ToPath(%q) 报错: %v", uri, err)
			}
			if back != p {
				t.Errorf("往返不一致：\n  原路径 %q\n  URI    %q\n  解回   %q", p, uri, back)
			}
		})
	}
}

// TestNoDoubleEncoding 确认已编码的输入不会被二次编码。
func TestNoDoubleEncoding(t *testing.T) {
	// % 本身必须编码，否则 file:///a%20b 会被误解成含空格
	got := FromPath("/tmp/a%b/x.4gl")
	if !strings.Contains(got, "a%25b") {
		t.Errorf("FromPath 未编码 %% 字符：%q", got)
	}
}

// ---------------------------------------------------------------------------
// 测试辅助
// ---------------------------------------------------------------------------

type uriParts struct{ host, path string }

// mustParse 解析 URI 并取出 host/path（已百分号解码）。
func mustParse(t *testing.T, raw string) uriParts {
	t.Helper()
	u, err := parseForTest(raw)
	if err != nil {
		t.Fatalf("解析 %q: %v", raw, err)
	}
	return u
}

// parseForTest 与 ToPath 用同一套解析，但只取 host/path，不做平台归一化。
// 这样 Windows 语义的用例可以在任何平台运行。
func parseForTest(raw string) (uriParts, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return uriParts{}, err
	}
	return uriParts{host: u.Host, path: u.Path}, nil
}
