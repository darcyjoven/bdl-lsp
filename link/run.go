package link

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/darcyjoven/bdl-lsp/builtin"
	"github.com/darcyjoven/bdl-lsp/parser"
)

// builtinNames 返回内置符号表里全部名字（小写）。
//
// 用途：导入时统计"外部函数"要排除内置名——`ARG_VAL`/`fgl_*` 本来就没有
// 定义，记进外部函数名单只会让名单变得没法读。
func builtinNames() map[string]bool {
	set := builtin.Embedded()
	if set == nil {
		return nil
	}
	out := make(map[string]bool, set.Len())
	for _, sym := range set.Symbols() {
		out[strings.ToLower(sym.Name)] = true
	}
	return out
}

// ImportRequest 是一次"CSV → links.jsonl"的完整导入请求。
//
// 让 tiptop 的 CLI 与审计工具共用同一段逻辑：`tiptop link import` 与
// （已移除的）`cmd/link-audit` 必须给出**完全一致**的数字，否则用户没法拿审计结果
// 去核对 CLI 的输出。
type ImportRequest struct {
	// CSVPath 是 DS.GAL_FILE 导出的 CSV。
	//
	// 与 CSVData 二选一：采集通道（P7-D-3）从数据库/命令直接拿到字节，
	// 不必先落盘到临时文件。
	CSVPath string
	// CSVData 是内存里的 CSV（优先于 CSVPath）。
	CSVData []byte
	// ProjectRoot 是项目根（用于解析模组路径与 `.link`）。
	ProjectRoot string
	// DataDir 是资料目录；空则 `<ProjectRoot>/.link`。
	DataDir string
	// ModuleRoots 是模组根；空则自动探测。
	ModuleRoots []string
	// Pool 是解析器池（必须有）。
	Pool *parser.Pool
	// Source / SourceDetail 记进 meta。
	Source       string
	SourceDetail string
	// ToolVersion 记进 meta。
	ToolVersion string
	// Write 为 true 时写盘。
	Write bool
	// Now 可注入固定时间（测试）。
	Now time.Time
	// Progress 是可选的源码扫描进度回调（CLI 用；后台同步传 nil）。
	// 可能被多个 worker 并发调用，实现方需保证线程安全。
	Progress func(done, total int)
}

// ImportOutcome 是导入结果 + 实际使用的布局信息。
type ImportOutcome struct {
	Result *ImportResult
	Layout *Layout
	// DataDir 是最终使用的资料目录（绝对路径）。
	DataDir string
	// Elapsed 是导入耗时（用于告知用户"为什么花了几十秒"）。
	Elapsed time.Duration
}

// RunImport 执行导入。参数不合法或 CSV 读不了时返回 error。
func RunImport(req ImportRequest) (*ImportOutcome, error) {
	if req.Pool == nil {
		return nil, fmt.Errorf("缺少解析器池")
	}
	if len(req.CSVData) == 0 && req.CSVPath == "" {
		return nil, fmt.Errorf("缺少 CSV 路径或内容")
	}
	data := req.CSVData
	if len(data) == 0 {
		var err error
		data, err = os.ReadFile(req.CSVPath)
		if err != nil {
			return nil, fmt.Errorf("读取 %s: %w", req.CSVPath, err)
		}
	}

	root := req.ProjectRoot
	if root == "" {
		root, _ = os.Getwd()
	}
	root, _ = filepath.Abs(root)

	dataDir := req.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(root, DefaultDataDir)
	} else if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(root, dataDir)
	}

	layout := NewLayout(root, req.ModuleRoots)
	// 触发一次探测，让 meta 记下实际使用的模组根。
	roots := layout.RootNames()

	start := time.Now()
	// 全语料的函数名画像：用来区分"项目里根本没有这个函数"（外部函数，
	// 忽略）与"有但链接里看不到"（真问题，照报）。
	corpus, err := ScanCorpusWithProgress(layout, req.Pool, 0, req.Progress)
	if err != nil {
		return nil, err
	}

	res, err := ImportCSV(data, ImportOptions{
		Ignore:       builtinNames(),
		Corpus:       corpus,
		Scanner:      corpus,
		Source:       req.Source,
		SourceDetail: req.SourceDetail,
		ToolVersion:  req.ToolVersion,
		Now:          req.Now,
	})
	if err != nil {
		return nil, err
	}
	res.Meta.ResolvedFrom = append([]string(nil), roots...)
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	res.Meta.GeneratedAt = now.Format(time.RFC3339)
	// 布局解析出的问题（同一模组存在于多个模组根）也要让用户看到。
	res.Problems = append(res.Problems, layout.Problems()...)

	out := &ImportOutcome{Result: res, Layout: layout, DataDir: dataDir, Elapsed: time.Since(start)}
	if req.Write {
		if err := Save(dataDir, res.Chains, res.Meta); err != nil {
			return out, err
		}
	}
	return out, nil
}

// Report 把导入结果格式化成人类可读的文本（CLI 与审计工具共用）。
func (o *ImportOutcome) Report(w *strings.Builder) {
	if o == nil || o.Result == nil {
		return
	}
	m := o.Result.Meta
	fmt.Fprintf(w, "== 链接资料导入 ==\n")
	fmt.Fprintf(w, "CSV 行数        %d\n", m.RowsTotal)
	fmt.Fprintf(w, "gal04='N' 过滤  %d\n", m.RowsFilteredN)
	fmt.Fprintf(w, "有效行          %d\n", m.RowsY)
	fmt.Fprintf(w, "链（有 MAIN）   %d\n", m.Chains)
	fmt.Fprintf(w, "orphan（无 MAIN）%d\n", m.Orphans)
	fmt.Fprintf(w, "ambiguous       %d\n", m.Ambiguous)
	fmt.Fprintf(w, "缺失文件        %d\n", m.MissingFiles)
	fmt.Fprintf(w, "模组根          %s\n", strings.Join(m.ResolvedFrom, ", "))
	fmt.Fprintf(w, "耗时            %s\n", o.Elapsed.Round(time.Millisecond))

	if len(o.Result.Problems) > 0 {
		// 只打印前 20 条：真实数据里 ambiguous 就 5 条，模组冲突一般也少；
		// 但如果数据脏到几百条，逐条打印会淹掉统计。
		fmt.Fprintf(w, "\n问题 %d 条（最多显示 20）：\n", len(o.Result.Problems))
		for i, p := range o.Result.Problems {
			if i >= 20 {
				fmt.Fprintf(w, "  … 其余 %d 条省略\n", len(o.Result.Problems)-20)
				break
			}
			fmt.Fprintf(w, "  - %s\n", p)
		}
	}
}

// TopChains 返回文件数最多的 n 条链（审计用，验证"最大链 364/524"之类）。
func TopChains(idx *Index, n int) []*Chain {
	if idx == nil {
		return nil
	}
	chains := append([]*Chain(nil), idx.Chains()...)
	// 简单插入排序：n 很小（10 以内），不需要 sort.Slice 的比较器开销。
	for i := 1; i < len(chains); i++ {
		for j := i; j > 0 && len(chains[j].Files) > len(chains[j-1].Files); j-- {
			chains[j], chains[j-1] = chains[j-1], chains[j]
		}
	}
	if n > 0 && len(chains) > n {
		chains = chains[:n]
	}
	return chains
}

// Status 汇总一份已加载的链接资料（`tiptop link status` 用）。
type Status struct {
	DataDir string
	Present bool
	Meta    *Meta
	Stats   Stats
	// Age 只用于展示"生成于 N 天前"（信息，不是告警：P1-6 去掉了过期提示）。
	Age           time.Duration
	Error         string
	GlobalModules []string
	GlobalFiles   int
}

// LoadStatus 读取资料目录并汇总状态（不解析任何源码，快）。
//
// P1-6：不再有"过期"判定——同步只由命令触发，插件也不会因为资料"旧"而报警。
// Age 仍然返回，供界面展示"生成于 N 天前"。
func LoadStatus(dataDir string, globalModules []string) Status {
	st := Status{DataDir: dataDir, GlobalModules: globalModules}
	idx, err := Load(dataDir)
	if err != nil {
		st.Error = err.Error()
		if err == ErrNoData {
			st.Error = ""
		}
		return st
	}
	st.Present = true
	st.Meta = idx.Meta()
	st.Stats = idx.Stats()
	if st.Meta != nil {
		st.Age = st.Meta.Age()
	}
	return st
}

// ---- P1-3：局部更新（只重算受影响程序的链，其余链原样保留） ----

// PartialImportRequest 是一次"只更新一部分"的导入请求。
//
// 与全量导入的差别：
//   - 只扫**受影响程序涉及的文件**（几十个），不是全语料 7600 个
//     —— 单文件更新从 20 多秒降到亚秒级；
//   - 写盘时**合并**：受影响的程序用新链替换，其余程序保留旧链。
type PartialImportRequest struct {
	CSVPath string
	CSVData []byte
	// ProjectRoot 是项目根；DataDir 空则 `<root>/.project`。
	ProjectRoot string
	DataDir     string
	ModuleRoots []string
	Pool        *parser.Pool
	// Modules / Specs 是筛选条件，至少一个非空。
	Modules      []string
	Specs        []string
	Source       string
	SourceDetail string
	ToolVersion  string
	Write        bool
	Now          time.Time
	Progress     func(done, total int)
}

// PartialImportOutcome 是局部更新的结果（含合并后的全量链，便于报告与测试）。
type PartialImportOutcome struct {
	Selection *PartialSelection
	Meta      *Meta
	Chains    []*Chain
	// NewChains / KeptChains 分别是本次重算与保留的链数。
	NewChains  int
	KeptChains int
	DataDir    string
	Elapsed    time.Duration
	Problems   []string
}

// RunImportPartial 执行局部更新。
//
// 前置条件：资料目录里已经有一份**全量**导入的资料（局部合并需要基线；
// 没有基线时报错让用户先跑一次全量，而不是悄悄写出一份不完整的资料）。
func RunImportPartial(req PartialImportRequest) (*PartialImportOutcome, error) {
	if req.Pool == nil {
		return nil, fmt.Errorf("缺少解析器池")
	}
	if len(req.Modules) == 0 && len(req.Specs) == 0 {
		return nil, fmt.Errorf("局部更新至少要给一个 -modules 或 -files")
	}
	root := req.ProjectRoot
	if root == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("取当前目录: %w", err)
		}
		root = cwd
	}
	root, _ = filepath.Abs(root)

	dataDir := req.DataDir
	if dataDir == "" {
		dataDir = filepath.Join(root, DefaultDataDir)
	} else if !filepath.IsAbs(dataDir) {
		dataDir = filepath.Join(root, dataDir)
	}

	data := req.CSVData
	if len(data) == 0 {
		b, err := os.ReadFile(req.CSVPath)
		if err != nil {
			return nil, fmt.Errorf("读取 CSV %s: %w", req.CSVPath, err)
		}
		data = b
	}

	sel, err := SelectPrograms(data, req.Modules, req.Specs)
	if err != nil {
		return nil, err
	}
	if len(sel.Programs) == 0 {
		return nil, fmt.Errorf("没有匹配到任何程序：模组 %v / 文件 %v（CSV 有效行 %d）",
			req.Modules, req.Specs, sel.RowsY)
	}

	// 基线：没有全量资料就不做局部更新。
	base, err := Load(dataDir)
	if err != nil {
		if err == ErrNoData {
			return nil, fmt.Errorf("还没有全量资料（%s）：先跑一次全量 `tiptop link import` 或 `tiptop link sync`",
				filepath.Join(dataDir, LinksFileName))
		}
		return nil, fmt.Errorf("读取已有资料: %w", err)
	}

	layout := NewLayout(root, req.ModuleRoots)
	roots := layout.RootNames()

	start := time.Now()
	// 只扫受影响程序的文件（几十个而不是 7600 个）。
	paths := make([]string, 0, len(sel.Files))
	for _, ref := range sel.Files {
		if p, ok := layout.Path(ref.Module(), ref.Code()); ok {
			paths = append(paths, p)
		}
	}
	corpus, err := ScanPathList(layout, req.Pool, paths, req.Progress)
	if err != nil {
		return nil, err
	}

	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	fresh, err := ImportCSV(data, ImportOptions{
		Ignore:       builtinNames(),
		Corpus:       corpus,
		Scanner:      corpus,
		OnlyPrograms: sel.Programs,
		Source:       req.Source,
		SourceDetail: req.SourceDetail,
		ToolVersion:  req.ToolVersion,
		Now:          now,
		Partial: &PartialMeta{
			UpdatedAt: now.Format(time.RFC3339),
			Modules:   req.Modules,
			Specs:     req.Specs,
			Programs:  len(sel.Programs),
		},
	})
	if err != nil {
		return nil, err
	}

	// 合并：受影响的程序用新链替换，其余保留。
	merged, kept := mergeChains(base.Chains(), fresh.Chains, sel.Programs)

	meta := base.Meta()
	if meta == nil {
		meta = &Meta{Schema: SchemaVersion}
	}
	meta.Schema = SchemaVersion
	meta.Source = req.Source
	meta.SourceDetail = req.SourceDetail
	meta.ToolVersion = req.ToolVersion
	meta.ResolvedFrom = append([]string(nil), roots...)
	meta.RowsOutOfScope = fresh.Meta.RowsOutOfScope
	meta.Partial = fresh.Meta.Partial
	meta.Partial.ChainsUpdated = len(fresh.Chains)
	if meta.Partial == nil {
		meta.Partial = &PartialMeta{UpdatedAt: now.Format(time.RFC3339), Programs: len(sel.Programs), ChainsUpdated: len(fresh.Chains)}
	}
	recountMeta(meta, merged, layout)

	out := &PartialImportOutcome{
		Selection:  sel,
		Meta:       meta,
		Chains:     merged,
		NewChains:  len(fresh.Chains),
		KeptChains: kept,
		DataDir:    dataDir,
		Elapsed:    time.Since(start),
		Problems:   append(append([]string(nil), layout.Problems()...), fresh.Problems...),
	}
	if req.Write {
		if err := Save(dataDir, merged, meta); err != nil {
			return out, err
		}
	}
	return out, nil
}

// mergeChains 用新链替换受影响的程序，其余旧链原样保留（返回合并结果与保留数）。
func mergeChains(old, fresh []*Chain, affected map[string]bool) ([]*Chain, int) {
	out := make([]*Chain, 0, len(old)+len(fresh))
	kept := 0
	for _, c := range old {
		if c == nil || affected[c.Program] {
			continue
		}
		out = append(out, c)
		kept++
	}
	out = append(out, fresh...)
	SortChains(out)
	return out, kept
}

// recountMeta 按合并后的链重算统计；MissingFiles 用 os.Stat 现查（不解析源码）。
func recountMeta(meta *Meta, chains []*Chain, layout *Layout) {
	meta.Chains, meta.Orphans, meta.Ambiguous, meta.MultiMain, meta.MissingFiles = 0, 0, 0, 0, 0
	for _, c := range chains {
		if c == nil {
			continue
		}
		if c.Main == "" {
			meta.Orphans++
		} else {
			meta.Chains++
		}
		if c.Ambiguous {
			meta.Ambiguous++
			meta.MultiMain++
		}
		for _, f := range c.Files {
			if _, ok := layout.Path(f.Module(), f.Code()); !ok {
				meta.MissingFiles++
			}
		}
	}
}

// Report 把局部更新结果格式化成人类可读文本（CLI 与插件都用它展示"将更新几个作业"）。
func (o *PartialImportOutcome) Report(w *strings.Builder) {
	if o == nil {
		return
	}
	fmt.Fprintf(w, "== 链接资料局部更新 ==\n")
	if len(o.Selection.Modules) > 0 {
		fmt.Fprintf(w, "模组条件        %s\n", strings.Join(o.Selection.Modules, ", "))
	}
	if len(o.Selection.Specs) > 0 {
		fmt.Fprintf(w, "文件条件        %s\n", strings.Join(o.Selection.Specs, ", "))
	}
	names := o.Selection.ProgramNames()
	fmt.Fprintf(w, "受影响作业      %d 个", len(names))
	if len(names) > 0 {
		show := names
		if len(show) > 10 {
			show = show[:10]
		}
		fmt.Fprintf(w, "（%s%s）", strings.Join(show, ", "),
			map[bool]string{true: " …", false: ""}[len(names) > 10])
	}
	fmt.Fprintln(w)
	fmt.Fprintf(w, "本次重算链      %d 条\n", o.NewChains)
	fmt.Fprintf(w, "保留旧链        %d 条\n", o.KeptChains)
	fmt.Fprintf(w, "范围内有效行    %d（范围外跳过 %d）\n", o.Selection.RowsY, o.Meta.RowsOutOfScope)
	fmt.Fprintf(w, "模组根          %s\n", strings.Join(o.Meta.ResolvedFrom, ", "))
	fmt.Fprintf(w, "耗时            %s\n", o.Elapsed.Round(time.Millisecond))
	if len(o.Problems) > 0 {
		fmt.Fprintf(w, "\n问题 %d 条（最多显示 20）：\n", len(o.Problems))
		for i, p := range o.Problems {
			if i >= 20 {
				fmt.Fprintf(w, "  … 其余 %d 条省略\n", len(o.Problems)-20)
				break
			}
			fmt.Fprintf(w, "  - %s\n", p)
		}
	}
}
