package deploy

import (
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// glob 是编译后的包含/排除模式。
//
// 支持 `**`（跨目录匹配），这一点与 filepath.Match 不同——语料里的
// include 天然写成 `**/*.4gl`，用 filepath.Match 永远匹配不到任何东西。
//
// 模式一律用 `/` 分隔（配置里也这么写）；匹配前把文件路径的 `\` 归一成 `/`，
// 这样 Windows 上同一份配置行为一致。
type glob struct {
	raw      string
	segments []string
	// negate 表示以 `!` 开头的排除式（在 Exclude 里等价于普通模式）。
	negate bool
}

// compileGlob 编译一个 glob 模式。
func compileGlob(pattern string) (*glob, error) {
	if pattern == "" {
		return nil, fmt.Errorf("空的 glob")
	}
	g := &glob{raw: pattern}
	p := pattern
	if strings.HasPrefix(p, "!") {
		g.negate = true
		p = p[1:]
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	// 以 / 开头的模式锚定到根；这里保留首个空段来表示"必须从根开始"。
	if strings.HasPrefix(p, "/") {
		g.segments = append(g.segments, "")
		p = strings.TrimPrefix(p, "/")
	}
	if p == "" {
		return nil, fmt.Errorf("glob 只有 /：%q", pattern)
	}
	g.segments = append(g.segments, strings.Split(p, "/")...)
	for _, seg := range g.segments {
		if seg == "" || seg == "**" {
			continue
		}
		// 单段语法必须合法，否则部署时静默漏文件。
		if _, err := path.Match(seg, "probe"); err != nil {
			return nil, fmt.Errorf("非法的 glob 段 %q（来自 %q）: %w", seg, pattern, err)
		}
	}
	return g, nil
}

// match 判断相对路径（用 / 分隔）是否匹配。
func (g *glob) match(rel string) bool {
	rel = strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "./")
	parts := strings.Split(rel, "/")
	return matchSegments(g.segments, parts)
}

// matchSegments 递归匹配模式段与路径段。
//
// `**` 可匹配 0 个或多个路径段；其余段用 path.Match（单段通配）。
func matchSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	// 锚定到根：模式首段为空串，要求路径也从头开始（parts 已相对根）。
	if pattern[0] == "" {
		return matchSegments(pattern[1:], name)
	}
	if pattern[0] == "**" {
		// `**` 吃掉 0..n 段
		for i := 0; i <= len(name); i++ {
			if matchSegments(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	ok, err := path.Match(pattern[0], name[0])
	if err != nil || !ok {
		return false
	}
	return matchSegments(pattern[1:], name[1:])
}

// globSet 是一组编译后的模式。
type globSet []*glob

// compileGlobs 编译一组模式。
func compileGlobs(patterns []string) (globSet, error) {
	out := make(globSet, 0, len(patterns))
	for _, p := range patterns {
		g, err := compileGlob(p)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// any 判断是否有任一模式匹配。
func (s globSet) any(rel string) bool {
	for _, g := range s {
		if g.match(rel) {
			return true
		}
	}
	return false
}

// checkGlobs 校验一组模式串，返回带字段前缀的错误。
//
// 单独抽出来是为了让 config 的 Validate 能给出精确位置（include 还是 exclude）。
func checkGlobs(field string, patterns []string) []string {
	var problems []string
	for _, p := range patterns {
		if _, err := compileGlob(p); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", field, err))
		}
	}
	return problems
}

// relSlash 返回相对 root 的 / 分隔路径；失败时退回原路径。
func relSlash(root, file string) string {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return filepath.ToSlash(file)
	}
	return filepath.ToSlash(rel)
}
