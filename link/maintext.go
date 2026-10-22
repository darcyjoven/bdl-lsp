package link

import "strings"

// ScanMainText 用**文本**判断一份源码里有没有 MAIN 语句。
//
// 为什么不只用 tree-sitter：语料里存在这种写法
//
//	{
//	MAIN
//	...
//	}
//
// `aws_ttsrv2_transxml.4gl` 就是这样，tree-sitter 不把它认成 `main` 节点，
// 于是 4 个组（aws_ttsrv2 / aws_process / cbmp100 / cimi997）会被错判成
// "只有一个 MAIN"，而实际上 transxml 也是一个程序入口。
//
// 文本扫描必须与语法的注释规则一致（见 tree-sitter-bdl `rule/fgl.js`）：
//
//	`--` 行注释、`#` 行注释、`{ ... }` 块注释
//
// 前两版都错在这里：第一版不认 `{ }` 块注释，把 `aws_ttsrv2_transxml.4gl`
// 里被注释掉的 `{ MAIN ... }` 当成真入口，凭空造出 4 个 ambiguous 组。
// **Tiptop 代码大量用 `{ }` 注释掉整段**，这条不是边角情况。
//
// 判错的代价不对称：多认一个 MAIN 只会让那条链标 `ambiguous`（两个入口
// 都关联整条链，行为仍然正确）；少认一个则会让那个入口**完全查不到自己的
// 链接**，function-not-found 永远不启用。
func ScanMainText(src []byte) bool {
	// inBrace 对应语法的 `seq("{", /[^}]*/, "}")`：不嵌套，遇到 `}` 就结束。
	inBrace := false
	for _, raw := range strings.Split(string(src), "\n") {
		var code strings.Builder
		for i := 0; i < len(raw); i++ {
			c := raw[i]
			if inBrace {
				if c == '}' {
					inBrace = false
				}
				continue
			}
			switch {
			case c == '{':
				inBrace = true
			case c == '#': // `#` 行注释：行尾剩余全是注释
				i = len(raw)
			case c == '-' && i+1 < len(raw) && raw[i+1] == '-': // `--` 行注释
				i = len(raw)
			default:
				code.WriteByte(c)
			}
		}
		// 注释剔除后必须**只剩** MAIN。
		if strings.EqualFold(strings.TrimSpace(code.String()), "MAIN") {
			return true
		}
	}
	return false
}
