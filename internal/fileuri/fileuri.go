// Package fileuri 在 LSP 的 file:// URI 与本地文件系统路径之间做转换。
//
// 为什么单独成包（P7-A-2）：
//
// 之前这段逻辑手写在两处（globals.PathOfURI、config.uriToPath），
// 而且两处都只剥 `file://` 前缀 + 把 `%20` 换成空格。这在 Linux 上
// "看起来能用"，在 Windows 上必然失效：
//
//	VSCode 下发 file:///c%3A/work/x.4gl   （盘符冒号被百分号编码）
//	Zed    下发 file:///c:/work/x.4gl     （不编码冒号）
//
// 旧实现把前者解成 `/c%3A/work/x.4gl` —— 非法路径，于是
// rootPath、.bdl-lsp/config.json、文档相对路径全线失效，
// 表现为"VSCode 认不到 GLOBALS 文件，Zed 可以"。
//
// 本包用 net/url 做完整百分号解码，并按运行时平台归一化盘符与 UNC。
package fileuri

import (
	"errors"
	"fmt"
	"net/url"
	"runtime"
	"strings"
)

// ErrNotFileURI 表示输入不是 file:// 形式的 URI。
var ErrNotFileURI = errors.New("不是 file URI")

const scheme = "file"

// ToPath 把 LSP 的 file:// URI 解码成本地文件系统路径。
//
// 接受的形态（必须全部覆盖，客户端之间不一致）：
//
//	file:///c%3A/work/x.4gl     VSCode / Windows（冒号被编码）
//	file:///C%3A/work/x.4gl     同上，大写盘符
//	file:///c:/work/x.4gl       Zed / Windows（冒号不编码）
//	file:///home/a%20b/x.4gl    Linux，空格被编码
//	file:///home/%E4%B8%AD/x     Linux，非 ASCII
//	file://server/share/x       UNC 路径
//	file:///c:/a%23b/x.4gl      `#` 被 VSCode 编码为 %23
//
// 返回值在该平台上可直接用于 os.Open / filepath 系列函数。
// 非 Windows 平台上，Windows 风格的输入不会被"纠正"，按普通路径处理
// （单测通过 build 无关的方式覆盖两种语义，见 toPathWindows）。
func ToPath(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", fmt.Errorf("解析 URI %q: %w", uri, err)
	}
	if u.Scheme != scheme {
		return "", fmt.Errorf("%w: %q", ErrNotFileURI, uri)
	}

	// UNC：file://server/share/x → Host="server", Path="/share/x"
	host := u.Host

	// u.Path 已经由 net/url 完成百分号解码（%3A → ':'、%23 → '#'、%E4%B8%AD → 中文）。
	p := u.Path

	if runtime.GOOS == "windows" {
		return toPathWindows(host, p), nil
	}
	return toPathUnix(host, p), nil
}

// toPathUnix 处理非 Windows 平台：UNC 转 //host/path，其余去掉前导斜杠的重复。
func toPathUnix(host, p string) string {
	if host != "" {
		// file://server/share/x → //server/share/x
		return "//" + host + p
	}
	if p == "" {
		return ""
	}
	return p
}

// toPathWindows 处理 Windows：把 `/c:/work/x` 归一成 `c:\work\x`，
// 把 UNC 归一成 `\\server\share\x`。
//
// 独立成函数是为了能在 Linux 上单测 Windows 语义（CI 没有 Windows runner
// 时也能守住这段逻辑）。注意：**不能依赖 filepath.FromSlash**——
// 在 Linux 上它什么都不做，单测就会拿到正斜杠。
func toPathWindows(host, p string) string {
	// UNC：/share/x → \\server\share\x
	if host != "" {
		return `\\` + host + strings.ReplaceAll(p, "/", `\`)
	}

	// 去掉前导斜杠：/c:/work/x → c:/work/x
	s := p
	for strings.HasPrefix(s, "/") {
		s = s[1:]
	}
	if s == "" {
		return ""
	}

	// 单字符盘符后面必须跟冒号：c:/work → C:\work
	// 此时冒号已由百分号解码还原（%3A → :）。
	if len(s) >= 2 && s[1] == ':' && isDriveLetter(s[0]) {
		return strings.ToUpper(s[:1]) + `:` + strings.ReplaceAll(s[2:], "/", `\`)
	}

	// 非盘符形态（如 file:///work/x 在 Windows 上是当前盘根）
	return `\` + strings.ReplaceAll(s, "/", `\`)
}

// isDriveLetter 判断 c 是否是 ASCII 字母（盘符）。
func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// FromPath 把本地路径编码成 file:// URI。
//
// 编码规则与 VSCode 保持一致（这样服务端返回的位置在 VSCode 里能正确打开，
// 在 Zed 里也能用）：
//
//	盘符冒号编码为 %3A        C:\x    → file:///C%3A/x
//	空格编码为 %20            a b     → a%20b
//	非 ASCII 按 UTF-8 百分号 中文     → %E4%B8%AD%E6%96%87
//	反斜杠统一成正斜杠
//
// `:` 之所以要编码，是因为它出现在 path 段里时按 RFC 3986 属于非法的
// pchar，VSCode 的 vscode-uri 就是这么做的；不编码会让部分客户端解析异常。
func FromPath(path string) string {
	if path == "" {
		return "file://"
	}

	// UNC：\\server\share\x
	if strings.HasPrefix(path, `\\`) {
		rest := strings.TrimPrefix(path, `\\`)
		parts := strings.SplitN(rest, `\`, 2)
		hostPart := parts[0]
		pathPart := ""
		if len(parts) > 1 {
			pathPart = "/" + strings.ReplaceAll(parts[1], `\`, "/")
		}
		return "file://" + hostPart + escapePath(pathPart)
	}

	p := strings.ReplaceAll(path, `\`, "/")

	// Windows 盘符：C:/x → /C:/x（先补前导斜杠，冒号由 escapePath 编码）
	if len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0]) {
		return "file:///" + escapePath(strings.ToUpper(p[:1])+p[1:])
	}

	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return "file://" + escapePath(p)
}

// escapePath 逐段做百分号编码，保留 `/` 作为分隔符。
//
// 不用 url.PathEscape 直接处理整串：它会把 `/` 也编码掉。
// 也不用 url.URL{Path:...}.String()：它会保留 `:` 不编码，
// 与 VSCode 的形态不一致（我们会因此生成 Zed 能读、VSCode 也读的 URI）。
func escapePath(p string) string {
	segs := strings.Split(p, "/")
	for i, seg := range segs {
		segs[i] = escapeSegment(seg)
	}
	return strings.Join(segs, "/")
}

// escapeSegment 对单个路径段做百分号编码。
//
// 保留字符集刻意与 vscode-uri 对齐：
//   - `:` 必须编码（Windows 盘符，RFC 3986 里 path 段不允许裸冒号）
//   - `#`、`?` 必须编码（否则会被当成 fragment / query）
//   - `%` 必须编码（否则会破坏既有转义）
//   - 字母数字与 `-_.~` 不编码（RFC 3986 unreserved）
//   - 非 ASCII 一律编码（UTF-8 字节逐个 %XX）
func escapeSegment(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if shouldEscape(c) {
			fmt.Fprintf(&b, "%%%02X", c)
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

// shouldEscape 判断单个字节是否需要百分号编码。
func shouldEscape(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z':
		return false
	case c >= 'A' && c <= 'Z':
		return false
	case c >= '0' && c <= '9':
		return false
	}
	switch c {
	case '-', '_', '.', '~':
		return false
	}
	// 其余（含 : # ? % 空格 以及所有 >= 0x80 的 UTF-8 字节）全部编码。
	return true
}
