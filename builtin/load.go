package builtin

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// dataFile 是数据文件的 JSON 结构（内置表与用户覆盖共用同一份 schema）。
//
// 带 schemaVersion 是为了以后能加字段而不炸旧文件：读到更高的版本就
// 记一条 warn 并尽力加载，而不是直接拒绝（用户更新覆盖表比我们发版快）。
type dataFile struct {
	SchemaVersion int      `json:"schemaVersion"`
	GeneroVersion string   `json:"generoVersion,omitempty"`
	Note          string   `json:"note,omitempty"`
	Disable       []string `json:"disable,omitempty"`
	Symbols       []Symbol `json:"symbols"`
}

// supportedSchema 是当前支持的覆盖文件 schema 版本。
const supportedSchema = 1

// embeddedDataPath 是内置数据的 embed 路径（同时用作文档里的引用）。
const embeddedDataPath = "data/builtins.json"

//go:embed data/builtins.json
var embeddedData []byte

// OverlayRelPath 是项目级覆盖文件相对工作区根的路径。
//
// 选 `.bdl-lsp/` 而不是 `.link/`：内置符号表属于"本项目的语言配置"，
// 和 link（可能含生产库信息、要 gitignore）不是一回事，内置表通常应该提交。
const OverlayRelPath = ".bdl-lsp/builtins.json"

var (
	embeddedOnce sync.Once
	embeddedSet  *Set
	embeddedErr  error
)

// Embedded 返回只含内置数据的符号表（进程内只解析一次）。
//
// 解析失败时返回空表而不是 panic：内置表坏了也不该让整个语言服务器
// 起不来，用户至少还能用语法诊断。
func Embedded() *Set {
	embeddedOnce.Do(func() {
		embeddedSet, embeddedErr = parse(embeddedData, Source{Path: embeddedDataPath, Builtin: true})
		if embeddedErr != nil {
			embeddedSet = NewSet().finish()
		}
	})
	return embeddedSet
}

// EmbeddedData 返回内置数据文件的原始内容（供 `builtin-audit -check` 校验）。
//
// 只读语义：返回内部切片，调用方不得改写（校验只解析，不改）。
func EmbeddedData() ([]byte, error) {
	if len(embeddedData) == 0 {
		return nil, fmt.Errorf("内置数据为空")
	}
	return embeddedData, nil
}

// EmbeddedErr 返回内置数据解析失败的原因（nil 表示正常）。
func EmbeddedErr() error { return embeddedErr }

// parse 解析一份数据文件并累加到一个新集合里。
func parse(data []byte, src Source) (*Set, error) {
	var f dataFile
	if err := json.Unmarshal(data, &f); err != nil {
		src.Err = err
		s := NewSet()
		s.sources = []Source{src}
		return s.finish(), fmt.Errorf("解析 %s: %w", src.Path, err)
	}
	s := NewSet()
	s.merge(&f, &src)
	s.sources = []Source{src}
	return s.finish(), src.Err
}

// merge 把一份数据合并进集合；返回时 src 里的计数已填好。
func (s *Set) merge(f *dataFile, src *Source) {
	if f.SchemaVersion > supportedSchema {
		// 不中断：高版本文件里我们认识的字段仍然有效。
		src.Err = fmt.Errorf("schemaVersion=%d 高于支持的 %d，按当前字段尽力加载",
			f.SchemaVersion, supportedSchema)
	}
	if s.version == "" {
		s.version = f.GeneroVersion
	}
	for _, sym := range f.Symbols {
		if strings.TrimSpace(sym.Name) == "" {
			continue
		}
		if sym.Kind == "" {
			sym.Kind = KindVariable
		}
		s.add(sym)
		src.Symbols++
	}
	for _, name := range f.Disable {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		s.disabled[strings.ToLower(name)] = true
		src.Disabled++
	}
}

// Overlay 是解析后的覆盖配置。
type Overlay struct {
	// Root 是工作区根（相对路径按它解析）。
	Root string
	// Paths 是显式配置的覆盖文件或目录（已解析为绝对路径或原样保留）。
	Paths []string
	// Disable 是配置里直接关闭的内置名。
	Disable []string
}

// Loader 负责加载内置表 + 覆盖层，并支持热更新。
//
// 热更新分两条路：
//   - 配置变更（didChangeConfiguration）→ Configure 显式重载；
//   - 用户直接编辑覆盖文件 → MaybeReload 按 mtime 变化重载（默认 2s 节流，
//     避免在每次按键的诊断里都 stat 一堆文件）。
type Loader struct {
	logger *slog.Logger

	mu    sync.Mutex
	set   *muSet
	over  Overlay
	stamp map[string]fileStamp // 路径 → 上次看到的 mtime/size
	last  time.Time            // 上次检查时间（节流）
}

// fileStamp 用于判断覆盖文件是否被改过。
type fileStamp struct {
	mod  time.Time
	size int64
}

// reloadInterval 是 MaybeReload 的最小检查间隔。
//
// 2s 的取值理由：用户改完 JSON 再切回编辑器，落到第一次诊断时基本已经
// 超过 2s；而 2s 内的连续按键完全不必重复 stat。
const reloadInterval = 2 * time.Second

// NewLoader 构造加载器。
func NewLoader(logger *slog.Logger) *Loader {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	l := &Loader{
		logger: logger,
		set:    &muSet{},
		stamp:  map[string]fileStamp{},
	}
	l.set.store(Embedded())
	return l
}

// Get 返回当前生效的符号表（永不为 nil）。
func (l *Loader) Get() *Set {
	if l == nil {
		return Embedded()
	}
	if s := l.set.load(); s != nil {
		return s
	}
	return Embedded()
}

// DisabledByConfig 报告名字是否被配置或覆盖文件禁用。
func (l *Loader) DisabledByConfig(name string) bool {
	if l == nil {
		return false
	}
	return l.Get().Disabled(name)
}

// Overlay 返回当前生效的覆盖配置（只读副本）。
func (l *Loader) Overlay() Overlay {
	if l == nil {
		return Overlay{}
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	return Overlay{
		Root:    l.over.Root,
		Paths:   append([]string(nil), l.over.Paths...),
		Disable: append([]string(nil), l.over.Disable...),
	}
}

// Configure 设置工作区根与覆盖文件，并立即重载。
//
// 先尝试解析内置表；覆盖层任何一份文件出错都只跳过该文件（记 warn），
// 不推翻已经可用的内置表——符号表不全比"完全不可用"好得多。
func (l *Loader) Configure(root string, paths, disable []string) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	l.over = Overlay{Root: root, Paths: append([]string(nil), paths...), Disable: append([]string(nil), disable...)}
	l.mu.Unlock()
	return l.Reload()
}

// Reload 强制重新读取所有覆盖文件。
func (l *Loader) Reload() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	over := Overlay{
		Root:    l.over.Root,
		Paths:   append([]string(nil), l.over.Paths...),
		Disable: append([]string(nil), l.over.Disable...),
	}
	l.mu.Unlock()

	base := Embedded()
	set := base.clone()

	files := l.resolveFiles(over)
	// 除内置表以外的来源要重新计算：clone 带过来的 sources 只有内置那一条。
	set.sources = append([]Source(nil), base.sources...)

	var errs []string
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			src := Source{Path: path, Err: err}
			set.sources = append(set.sources, src)
			errs = append(errs, fmt.Sprintf("%s: %v", path, err))
			continue
		}
		var f dataFile
		src := Source{Path: path}
		if jerr := json.Unmarshal(data, &f); jerr != nil {
			src.Err = jerr
			set.sources = append(set.sources, src)
			errs = append(errs, fmt.Sprintf("%s: %v", path, jerr))
			continue
		}
		set.merge(&f, &src)
		set.sources = append(set.sources, src)
		if src.Err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", path, src.Err))
		}
	}
	for _, name := range over.Disable {
		if n := strings.TrimSpace(name); n != "" {
			set.disabled[strings.ToLower(n)] = true
		}
	}

	final := set.finish()
	l.set.store(final)

	// 记录 mtime 指纹，供 MaybeReload 比对。
	//
	// **指纹覆盖"候选路径"而不是"实际读到的文件"**：约定路径
	// `.bdl-lsp/builtins.json` 在启动时往往还不存在，用户后来新建它时
	// 必须能被发现。所以这里对候选路径（含不存在、含目录）一律打点。
	l.mu.Lock()
	l.stamp = map[string]fileStamp{}
	for _, path := range l.candidatePaths(over) {
		l.stamp[path] = stampOf(path)
	}
	l.last = time.Now()
	l.mu.Unlock()

	for _, e := range errs {
		l.logger.Warn("内置符号覆盖文件有问题（已跳过）", "detail", e)
	}
	if len(over.Paths) > 0 || over.Root != "" {
		l.logger.Info("内置符号表已加载",
			"symbols", final.Len(),
			"overlayFiles", len(files),
			"generoVersion", final.GeneroVersion())
	}
	return nil
}

// MaybeReload 在覆盖文件发生变化时重载（默认 2s 内最多检查一次）。
//
// 返回 true 表示发生了重载，调用方可以据此重新诊断。
func (l *Loader) MaybeReload() bool {
	if l == nil {
		return false
	}
	l.mu.Lock()
	if !l.last.IsZero() && time.Since(l.last) < reloadInterval {
		l.mu.Unlock()
		return false
	}
	stamps := make(map[string]fileStamp, len(l.stamp))
	for k, v := range l.stamp {
		stamps[k] = v
	}
	l.last = time.Now()
	l.mu.Unlock()

	// 没有覆盖文件时不必反复 stat：内置表不可能在运行时变化。
	if len(stamps) == 0 {
		return false
	}

	changed := false
	for path, old := range stamps {
		if got := stampOf(path); got != old {
			changed = true
			break
		}
	}
	if !changed {
		return false
	}
	if err := l.Reload(); err != nil {
		l.logger.Warn("覆盖文件变更后重载失败", "error", err)
		return false
	}
	l.logger.Info("内置符号覆盖文件已变更，符号表已重载", "symbols", l.Get().Len())
	return true
}

// stampOf 取一个路径的指纹；不存在时为零值（"不存在"本身也是状态）。
func stampOf(path string) fileStamp {
	st, err := os.Stat(path)
	if err != nil {
		return fileStamp{}
	}
	return fileStamp{mod: st.ModTime(), size: st.Size()}
}

// candidatePaths 是"可能影响符号表"的全部路径（含不存在的、含目录）。
//
// 用途只有热更新：比对指纹判断要不要重载。目录也在里面，是因为
// "往目录里新增一个 .json" 只会改目录的 mtime，不改任何已存在文件的。
func (l *Loader) candidatePaths(over Overlay) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" {
			return
		}
		clean := filepath.Clean(p)
		if seen[clean] {
			return
		}
		seen[clean] = true
		out = append(out, clean)
	}

	if over.Root != "" {
		add(filepath.Join(over.Root, filepath.FromSlash(OverlayRelPath)))
	}
	for _, p := range over.Paths {
		full := p
		if !filepath.IsAbs(full) && over.Root != "" {
			full = filepath.Join(over.Root, full)
		}
		add(full)
		// 目录存在时把其中的 .json 也纳入指纹（编辑已存在文件只改文件 mtime）。
		if st, err := os.Stat(full); err == nil && st.IsDir() {
			if entries, derr := os.ReadDir(full); derr == nil {
				for _, e := range entries {
					if !e.IsDir() && strings.EqualFold(filepath.Ext(e.Name()), ".json") {
						add(filepath.Join(full, e.Name()))
					}
				}
			}
		}
	}
	return out
}

// resolveFiles 把覆盖配置展开成实际存在的文件列表（去重、保序）。
//
// 目录按 `builtins.json` 补全，这样用户可以直接配一个目录。
func (l *Loader) resolveFiles(over Overlay) []string {
	var out []string
	seen := map[string]bool{}
	addFile := func(path string) {
		if path == "" {
			return
		}
		clean := filepath.Clean(path)
		if seen[clean] {
			return
		}
		if st, err := os.Stat(clean); err != nil || st.IsDir() {
			return
		}
		seen[clean] = true
		out = append(out, clean)
	}

	resolve := func(p string) string {
		if filepath.IsAbs(p) {
			return p
		}
		if over.Root != "" {
			return filepath.Join(over.Root, p)
		}
		return p
	}

	// 1) 项目级约定路径（存在才加，不存在不报错——大多数项目不会有）。
	if over.Root != "" {
		addFile(filepath.Join(over.Root, filepath.FromSlash(OverlayRelPath)))
	}

	// 2) 显式配置的路径（文件或目录）。目录不存在时记 warn，
	//    因为这是用户明确写的，静默忽略会让人以为配错了也没提示。
	for _, p := range over.Paths {
		full := resolve(p)
		st, err := os.Stat(full)
		if err != nil || !st.IsDir() {
			addFile(full)
			continue
		}
		entries, derr := os.ReadDir(full)
		if derr != nil {
			l.logger.Warn("内置符号覆盖目录不可读", "dir", full, "error", derr)
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			if strings.EqualFold(filepath.Ext(name), ".json") {
				addFile(filepath.Join(full, name))
			}
		}
	}
	return out
}

// Validate 校验一份数据文件的内容，返回人类可读的问题列表。
//
// 给 `cmd/builtin-audit -check` 与单测用：数据文件是长期要维护的东西，
// 重复名、空名、未知 kind、乱序都会在几个月后变成"为什么这条没生效"。
func Validate(data []byte) []string {
	var problems []string
	var f dataFile
	if err := json.Unmarshal(data, &f); err != nil {
		return []string{fmt.Sprintf("JSON 解析失败: %v", err)}
	}
	if f.SchemaVersion != supportedSchema {
		problems = append(problems,
			fmt.Sprintf("schemaVersion=%d，期望 %d", f.SchemaVersion, supportedSchema))
	}
	if f.GeneroVersion == "" {
		problems = append(problems, "缺少 generoVersion")
	}
	seen := map[string]int{}
	lastName := ""
	for i, sym := range f.Symbols {
		where := fmt.Sprintf("symbols[%d]", i)
		name := strings.TrimSpace(sym.Name)
		if name == "" {
			problems = append(problems, where+": name 为空")
			continue
		}
		where = fmt.Sprintf("%s(%s)", where, name)
		if !sym.Kind.Valid() {
			problems = append(problems, fmt.Sprintf("%s: kind=%q 非法", where, sym.Kind))
		}
		if sym.Kind == KindMethod && sym.Class == "" {
			problems = append(problems, where+": method 缺少 class")
		}
		if prev, dup := seen[strings.ToLower(name)]; dup {
			problems = append(problems,
				fmt.Sprintf("%s: 与 symbols[%d] 重名（大小写不敏感）", where, prev))
		}
		seen[strings.ToLower(name)] = i
		// 排序只做提示，不算错误：乱序不影响功能，但会让 diff 噪声变大。
		if lastName != "" && strings.ToLower(name) < strings.ToLower(lastName) {
			problems = append(problems, fmt.Sprintf("%s: 未按名字排序（前一条是 %s）", where, lastName))
		}
		lastName = name
	}
	for i, name := range f.Disable {
		if strings.TrimSpace(name) == "" {
			problems = append(problems, fmt.Sprintf("disable[%d]: 空名字", i))
		}
	}
	return problems
}
