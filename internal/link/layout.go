package link

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// Layout 描述「链接资料里的 (模组, 程序代码) → 磁盘路径」怎么解析。
//
// 语料实测布局（两套模组根，客户化在第二套）：
//
//	<项目根>/tiptop/<模组>/4gl/<代码>.4gl     # 61 个模组目录
//	<项目根>/topcust/<模组>/4gl/<代码>.4gl    # 26 个客户化模组目录
//
// 两个坑：
//  1. **模组名大小写不一致**：链接资料里全是 `APM`，磁盘上全是 `apm`。
//     Linux 文件系统区分大小写，必须自己建大小写不敏感映射，不能直接拼。
//  2. 多个模组根可能都有同一个模组 → 按配置顺序取第一个（默认 `tiptop`
//     在前，即标准代码优先）。命中多个只记 debug，不算错误。
type Layout struct {
	// ProjectRoot 是项目根（绝对路径）。
	ProjectRoot string
	// Roots 是模组根（相对 ProjectRoot）。为空时自动探测。
	Roots []string

	// dirs 缓存 lower(模组) → 模组绝对目录（含 `4gl` 子目录之前的那层）。
	dirs map[string]string
	// errs 是解析过程中的问题（供导入报告使用）。
	errs []string
	// mu 保护 dirs/errs/Roots：导入阶段会用多个 goroutine 并行查路径。
	mu sync.Mutex
}

// DefaultRoots 是模组根的出厂默认值。
//
// 探测失败时的兜底：这两个是所有 Tiptop 项目都有的（标准 + 客户化）。
var DefaultRoots = []string{"tiptop", "topcust"}

// NewLayout 构造布局解析器。roots 为空时自动探测。
func NewLayout(projectRoot string, roots []string) *Layout {
	return &Layout{ProjectRoot: projectRoot, Roots: append([]string(nil), roots...)}
}

// RootDirs 返回模组根的绝对路径（自动探测后）。
func (l *Layout) RootDirs() []string {
	names := l.RootNames()
	out := make([]string, 0, len(names))
	for _, n := range names {
		out = append(out, filepath.Join(l.ProjectRoot, n))
	}
	return out
}

// RootNames 返回实际使用的模组根名字（自动探测的结果会被缓存）。
func (l *Layout) RootNames() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.Roots) == 0 {
		l.Roots = l.detectRoots()
	}
	return l.Roots
}

// Problems 返回解析过程中的问题（重复模组等），供导入报告显示。
func (l *Layout) Problems() []string {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	// 触发一次建表，否则"同一模组存在于多个模组根"这类问题不会出现。
	l.buildLocked()
	return append([]string(nil), l.errs...)
}

// detectRoots 探测项目根下哪些子目录像"模组根"。
//
// 判据：该子目录里至少有一个孙目录带 `4gl` 子目录。这个判据对
// `tiptop/` `topcust/` 成立，也不会把 `docs/` `scripts/` 之类误判。
func (l *Layout) detectRoots() []string {
	if l.ProjectRoot == "" {
		return nil
	}
	entries, err := os.ReadDir(l.ProjectRoot)
	if err != nil {
		return nil
	}
	var found []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		mods, err := os.ReadDir(filepath.Join(l.ProjectRoot, e.Name()))
		if err != nil {
			continue
		}
		for _, m := range mods {
			if !m.IsDir() {
				continue
			}
			if st, err := os.Stat(filepath.Join(l.ProjectRoot, e.Name(), m.Name(), "4gl")); err == nil && st.IsDir() {
				found = append(found, e.Name())
				break
			}
		}
	}
	if len(found) == 0 {
		return nil
	}
	// 默认顺序优先：tiptop 在前，topcust 在后，其余按字典序。
	rank := func(name string) int {
		switch strings.ToLower(name) {
		case "tiptop":
			return 0
		case "topcust":
			return 1
		}
		return 2
	}
	sort.SliceStable(found, func(i, j int) bool {
		ri, rj := rank(found[i]), rank(found[j])
		if ri != rj {
			return ri < rj
		}
		return found[i] < found[j]
	})
	return found
}

// build 建立 lower(模组) → 目录 的映射。调用方必须持有 l.mu。
func (l *Layout) buildLocked() {
	if l.dirs != nil {
		return
	}
	l.dirs = map[string]string{}
	for _, root := range l.rootNamesLocked() {
		rootDir := filepath.Join(l.ProjectRoot, root)
		entries, err := os.ReadDir(rootDir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dir := filepath.Join(rootDir, e.Name())
			if st, err := os.Stat(filepath.Join(dir, "4gl")); err != nil || !st.IsDir() {
				continue
			}
			key := strings.ToLower(e.Name())
			if prev, dup := l.dirs[key]; dup {
				// 两个模组根都有这个模组：先出现的优先（配置顺序）。
				l.errs = append(l.errs,
					fmt.Sprintf("模组 %s 同时存在于 %s 与 %s，使用前者",
						e.Name(), prev, dir))
				continue
			}
			l.dirs[key] = dir
		}
	}
}

// rootNamesLocked 是 RootNames 的无锁版本（调用方持锁）。
func (l *Layout) rootNamesLocked() []string {
	if len(l.Roots) == 0 {
		l.Roots = l.detectRoots()
	}
	return l.Roots
}

// ModuleDir 返回模组的绝对目录（大小写不敏感）。
func (l *Layout) ModuleDir(module string) (string, bool) {
	if l == nil || module == "" {
		return "", false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buildLocked()
	dir, ok := l.dirs[strings.ToLower(module)]
	return dir, ok
}

// Path 返回 (模组, 代码) 对应的 .4gl 绝对路径。
func (l *Layout) Path(module, code string) (string, bool) {
	dir, ok := l.ModuleDir(module)
	if !ok {
		return "", false
	}
	// 代码本身也可能大小写不一致（资料 `Sapmp500` vs 文件 `sapmp500`），
	// 所以先按原样试，失败再扫目录做大小写不敏感匹配。
	direct := filepath.Join(dir, "4gl", code+".4gl")
	if st, err := os.Stat(direct); err == nil && !st.IsDir() {
		return direct, true
	}
	want := strings.ToLower(code + ".4gl")
	entries, err := os.ReadDir(filepath.Join(dir, "4gl"))
	if err != nil {
		return "", false
	}
	for _, e := range entries {
		if !e.IsDir() && strings.ToLower(e.Name()) == want {
			return filepath.Join(dir, "4gl", e.Name()), true
		}
	}
	return "", false
}

// ModuleFiles 返回模组 `4gl` 目录下全部 .4gl 文件（绝对路径，已排序）。
//
// 用于全局模组（sub/lib/qry）：这些模组的函数对所有程序可见，且**不依赖
// 链接资料**——即使 CSV 里没有它们的组也能工作。所以只能扫盘。
func (l *Layout) ModuleFiles(module string) []string {
	dir, ok := l.ModuleDir(module)
	if !ok {
		return nil
	}
	entries, err := os.ReadDir(filepath.Join(dir, "4gl"))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(entries))
	for _, e := range entries {
		if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".4gl") {
			out = append(out, filepath.Join(dir, "4gl", e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

// CodeOf 从路径推导程序代码（文件名去 `.4gl`，保留原始大小写）。
func CodeOf(path string) string {
	base := filepath.Base(path)
	if ext := filepath.Ext(base); strings.EqualFold(ext, ".4gl") || strings.EqualFold(ext, ".per") {
		return base[:len(base)-len(ext)]
	}
	return base
}

// FileID 从路径反推 (模组, 程序代码)。路径不在任何模组根下时 ok=false。
//
// 语料布局 `<root>/<模组根>/<模组>/4gl/<代码>.4gl`；模组名取磁盘上的
// 真实大小写，代码取文件名（去 `.4gl`）。
func (l *Layout) FileID(path string) (module, code string, ok bool) {
	if l == nil || path == "" {
		return "", "", false
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		abs = filepath.Clean(path)
	}
	abs = filepath.ToSlash(abs)
	roots := l.RootNames()
	for _, root := range roots {
		rootDir := filepath.ToSlash(filepath.Join(l.ProjectRoot, root))
		prefix := strings.TrimSuffix(rootDir, "/") + "/"
		if !strings.HasPrefix(strings.ToLower(abs), strings.ToLower(prefix)) {
			continue
		}
		rest := abs[len(prefix):]
		parts := strings.Split(rest, "/")
		// 期望 <模组>/4gl/<文件>；只有 4gl 这一层是硬要求，别的布局不强求。
		if len(parts) < 3 || !strings.EqualFold(parts[1], "4gl") {
			continue
		}
		// 返回**小写**：调用方拿它去查链路索引（键是小写的）。
		return strings.ToLower(parts[0]), strings.ToLower(CodeOf(parts[len(parts)-1])), true
	}
	return "", "", false
}
