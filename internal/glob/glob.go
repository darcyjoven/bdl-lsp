// Package glob 提供支持 `**` 的 glob 匹配（部署 include/exclude 与
// 诊断忽略列表共用）。
//
// 为什么不直接用 filepath.Match：它不支持 `**`，`**/*.4gl` 这种写法
// "合法但永不匹配"，会静默地传 0 个文件 / 忽略 0 个路径（P5 踩过）。
//
// 约定：
//   - 模式一律用 `/` 分隔；匹配前把路径的 `\` 归一成 `/`，Windows 行为一致；
//   - `**` 匹配 0 个或多个路径段，其余段用单段通配（path.Match）；
//   - 以 `/` 开头表示锚定到根，否则从根开始逐段匹配（与 shell 的 glob 不同，
//     这里不做"末段任意深度"的隐式展开，需要时写 `**`）。
package glob

import (
	"fmt"
	"path"
	"strings"
)

// Pattern 是编译后的匹配模式。
type Pattern struct {
	raw      string
	segments []string
	// negate 表示以 `!` 开头的模式（部署的 exclude 里等价于普通模式）。
	negate bool
}

// Compile 编译一个 glob 模式。
func Compile(pattern string) (*Pattern, error) {
	if pattern == "" {
		return nil, fmt.Errorf("空的 glob")
	}
	g := &Pattern{raw: pattern}
	p := pattern
	if strings.HasPrefix(p, "!") {
		g.negate = true
		p = p[1:]
	}
	p = strings.ReplaceAll(p, "\\", "/")
	p = strings.TrimPrefix(p, "./")
	// 以 / 开头的模式锚定到根；保留首个空段表示"必须从根开始"。
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
		// 单段语法必须合法，否则匹配永远失败且没有任何提示。
		if _, err := path.Match(seg, "probe"); err != nil {
			return nil, fmt.Errorf("非法的 glob 段 %q（来自 %q）: %w", seg, pattern, err)
		}
	}
	return g, nil
}

// String 返回原始模式。
func (g *Pattern) String() string { return g.raw }

// Negated 报告模式是否以 `!` 开头。
func (g *Pattern) Negated() bool { return g.negate }

// Match 判断相对路径（用 / 分隔）是否匹配。
func (g *Pattern) Match(rel string) bool {
	rel = strings.TrimPrefix(strings.ReplaceAll(rel, "\\", "/"), "./")
	return matchSegments(g.segments, strings.Split(rel, "/"))
}

// matchSegments 递归匹配模式段与路径段。
func matchSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	// 锚定到根：模式首段为空串。
	if pattern[0] == "" {
		return matchSegments(pattern[1:], name)
	}
	if pattern[0] == "**" {
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

// Set 是一组编译后的模式。
type Set []*Pattern

// CompileSet 编译一组模式。
func CompileSet(patterns []string) (Set, error) {
	out := make(Set, 0, len(patterns))
	for _, p := range patterns {
		g, err := Compile(p)
		if err != nil {
			return nil, err
		}
		out = append(out, g)
	}
	return out, nil
}

// Match 判断是否有任一模式匹配。
func (s Set) Match(rel string) bool {
	for _, g := range s {
		if g.Match(rel) {
			return true
		}
	}
	return false
}

// Empty 报告集合是否为空。
func (s Set) Empty() bool { return len(s) == 0 }

// Validate 校验一组模式串，返回带字段前缀的错误。
func Validate(field string, patterns []string) []string {
	var problems []string
	for _, p := range patterns {
		if _, err := Compile(p); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", field, err))
		}
	}
	return problems
}
