package link

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// FileScan 是一个链接文件的扫描结果。
type FileScan struct {
	// Found=false 表示磁盘上找不到这个文件——**这是常态**，不是错误：
	// 实测 800 条记录落在语料里不存在的 W* 模组（另一套/已下线系统）。
	Found bool
	// HasMain 表示文件里有 MAIN（入口程序）。
	HasMain bool
	// Defines 是本文件定义的函数名（小写）。
	//
	// 只用来**缩小**外部函数的候选集（定义过的名字不可能是外部函数），
	// 与全语料粗扫的 Defines 取并集。
	Defines []string
	// Calls 是本文件里被调用的函数名（小写、去重）。
	Calls []string
	// callCount 是每个名字在本文件里的调用次数（小写 → 次数）。
	callCount map[string]int
	// callSeen 是 Calls 的去重集合。
	callSeen map[string]bool
}

// Scanner 扫描 (模组, 程序代码) 对应的源文件。
//
// 为什么要外部注入：判断"这一组里哪一条是 MAIN"必须读文件，而本包
// 不想依赖 tree-sitter（那会让纯数据逻辑也要 cgo）。真实实现由
// server/CLI 提供（解析 + 查语法树），测试用假实现。
//
// P7-D-2 起它还要带出 Defines/Calls：导入时**已经**要把每个文件解析一遍，
// 顺手统计"被调用但从无定义"的外部函数，边际成本≈0。
type Scanner interface {
	Scan(module, code string) FileScan
}

// ImportOptions 控制导入行为。
type ImportOptions struct {
	// Scanner 用于判定 MAIN 并统计函数名。为 nil 时所有组都按 orphan 处理
	// （只用于"没有源码"的离线校验场景）。
	Scanner Scanner
	// Ignore 里的名字不记入外部函数表（内置符号：ARG_VAL/fgl_* 之类
	// 本来就没有定义，记进去只会污染名单）。
	Ignore map[string]bool
	// ExternalsMinFiles / ExternalsMinCalls 是"外部函数"的判定阈值
	// （调用它的文件数 / 调用总次数），满足任一即算。0 表示用默认值。
	ExternalsMinFiles int
	ExternalsMinCalls int
	// Corpus 是整份语料的函数名画像（由 RunImport 粗扫全项目得到）。
	// 链接资料只覆盖被链接到的文件，不用它补齐的话，"语料里存在但没被
	// 链接"的函数会被误判成"不存在"而放过。
	Corpus *CorpusIndex
	// Source / SourceDetail 记进 meta，例如 "db" / "DS.GAL_FILE@host:1521/svc"。
	Source       string
	SourceDetail string
	// ToolVersion 记进 meta。
	ToolVersion string
	// Now 用于生成 generatedAt（测试注入固定时间）。
	Now time.Time
	// Workers 是判定 MAIN 时的并发度（0 = GOMAXPROCS）。
	Workers int
	// OnlyPrograms 非 nil 时**只处理**这些程序编号（CSV 的 gal01），
	// 其余行计入 Meta.RowsOutOfScope。P1-3 局部更新用。
	OnlyPrograms map[string]bool
	// Partial 非 nil 时记进 Meta.Partial（说明这次只更新了一部分）。
	Partial *PartialMeta
}

// ImportResult 是导入产物。
type ImportResult struct {
	Chains   []*Chain
	Meta     *Meta
	Problems []string
}

// ImportCSV 把 DS.GAL_FILE 的 CSV 导出转成链路列表。
//
// 输入形态（实测）：UTF-8 **带 BOM**、每个字段都用双引号包裹、
// 首行表头 `GAL01,GAL02,GAL03,GAL04`。BOM 必须处理，否则首列名会变成
// `\ufeffGAL01`，按表头定位列就全部落空。
func ImportCSV(data []byte, opts ImportOptions) (*ImportResult, error) {
	data = stripBOM(data)
	rows, header, err := readCSV(data)
	if err != nil {
		return nil, err
	}
	colProgram, ok1 := header["GAL01"]
	colModule, ok2 := header["GAL02"]
	colCode, ok3 := header["GAL03"]
	colLinked, ok4 := header["GAL04"]
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil, fmt.Errorf("表头缺少 GAL01..GAL04（实际列：%s）", headerNames(header))
	}

	res := &ImportResult{Meta: &Meta{
		Schema:       SchemaVersion,
		Source:       opts.Source,
		SourceDetail: opts.SourceDetail,
		ToolVersion:  opts.ToolVersion,
		RowsTotal:    len(rows),
	}}

	// ---- 第一遍：按程序编号分组，组内按 (模组,代码) 去重 ----
	var order []string
	groups := map[string]*importGroup{}
	for i, row := range rows {
		get := func(idx int) string {
			if idx < len(row) {
				return strings.TrimSpace(row[idx])
			}
			return ""
		}
		program := get(colProgram)
		module := get(colModule)
		code := get(colCode)
		linked := get(colLinked)

		if program == "" || code == "" {
			res.Problems = append(res.Problems,
				fmt.Sprintf("第 %d 行缺少程序编号或程序代码，已跳过", i+2))
			continue
		}
		// R4：gal04<>'Y' 在 SQL 阶段就该过滤，这里再兜底一次
		// （实测 CSV 里有 51 行 'N'）。大小写不敏感，容忍 'y'。
		if !strings.EqualFold(linked, "Y") {
			res.Meta.RowsFilteredN++
			continue
		}
		// 局部更新：不在本次范围内的程序整行跳过（不计入 RowsY）。
		if opts.OnlyPrograms != nil && !opts.OnlyPrograms[program] {
			res.Meta.RowsOutOfScope++
			continue
		}
		res.Meta.RowsY++

		g, ok := groups[program]
		if !ok {
			g = &importGroup{program: program, seen: map[string]bool{}}
			groups[program] = g
			order = append(order, program)
		}
		ref := FileRef{module, code}
		if g.seen[ref.Key()] {
			// 实测存在：`apmt722` 组把自身列了两次。
			continue
		}
		g.seen[ref.Key()] = true
		g.files = append(g.files, ref)
	}

	// ---- 第二遍：并行判定每个 (模组,代码) 是否有 MAIN ----
	// 7765 个不同代码 × 单文件解析 ≈ 实测 8.7 ms/文件，串行要 1 分钟以上，
	// 并行到 8 核约 10 s。这是批量工具，一次几秒可接受。
	scans := scanFiles(groups, order, opts)

	// ---- 第三遍：成链 ----
	for _, program := range order {
		g := groups[program]
		if len(g.files) == 0 {
			continue
		}
		c := &Chain{Schema: SchemaVersion, Program: program, Files: g.files}
		var mainRefs []FileRef
		for _, f := range g.files {
			info := scans.scanOf[f.Key()]
			if !info.Found {
				// 缺失文件原样保留（用户明确要求），统计由 lookupMains 统一做。
				continue
			}
			if info.HasMain {
				mainRefs = append(mainRefs, f)
			}
		}
		switch len(mainRefs) {
		case 0:
			c.Module = g.files[0].Module()
			res.Meta.Orphans++
		case 1:
			c.Main = mainRefs[0].Code()
			c.Module = mainRefs[0].Module()
			res.Meta.Chains++
		default:
			c.Main = mainRefs[0].Code()
			c.Module = mainRefs[0].Module()
			for _, m := range mainRefs[1:] {
				c.Mains = append(c.Mains, m.Code())
			}
			c.Ambiguous = true
			res.Meta.Chains++
			res.Meta.MultiMain++
			res.Meta.Ambiguous++
			res.Problems = append(res.Problems,
				fmt.Sprintf("程序 %s 有 %d 个 MAIN（%s），已全部保留并标记 ambiguous",
					program, len(mainRefs), mainNames(mainRefs)))
		}
		res.Chains = append(res.Chains, c)
	}

	res.Meta.MissingFiles = scans.missingTotal
	res.Meta.Partial = opts.Partial
	buildExternals(res, scans, opts)
	SortChains(res.Chains)
	return res, nil
}

// PartialMeta 记录"这次只更新了一部分"（P1-3 局部同步）。
//
// 为什么单独记：`generatedAt` 是**整份资料**的生成时间，局部更新后它不代表
// "所有链都是新的"。视图/状态命令据此说明"哪一部分是新算的"。
type PartialMeta struct {
	UpdatedAt string   `json:"updatedAt"`
	Modules   []string `json:"modules,omitempty"`
	Specs     []string `json:"specs,omitempty"`
	// Programs 是本次受影响的程序（作业）数。
	Programs int `json:"programs"`
	// ChainsUpdated 是本次重算写回的链数。
	ChainsUpdated int `json:"chainsUpdated"`
}

// PartialSelection 是"局部更新要动哪些程序"的结果。
type PartialSelection struct {
	// Programs 是受影响的程序编号（CSV gal01，原样大小写）。
	Programs map[string]bool
	// Files 是这些程序在 CSV 里出现的全部 (模组,代码)，去重后按出现序。
	// 用来把"源码扫描"缩小到这些文件（几毫秒，而不是全语料 20 多秒）。
	Files []FileRef
	// MatchedRows / RowsY 供 CLI 报告"匹配了几行、范围内几行"。
	MatchedRows int
	RowsY       int
	// Modules / Specs 是用户给的原始条件（记进 meta 与报告）。
	Modules []string
	Specs   []string
	// order 是受影响程序的出现顺序（报告用，保持与 CSV 一致）。
	order []string
}

// ProgramNames 返回受影响的程序编号，按 CSV 出现顺序。
func (s *PartialSelection) ProgramNames() []string {
	if s == nil {
		return nil
	}
	return append([]string(nil), s.order...)
}

// typeDirs 是"模组目录下再套的按类型命名的目录"，从文件路径里认模组时要跳过。
var typeDirs = map[string]bool{"4gl": true, "per": true, "42f": true, "4fd": true, "sch": true}

// SelectPrograms 按模组 / 文件条件挑出"本次要更新的程序"。
//
//	modules  命中 CSV 的 gal02（大小写不敏感）
//	files    每一项形如 `aap/4gl/saapt310.4gl`、`aap/saapt310`、`saapt310.4gl`：
//	         取最后一个路径段做程序代码（去扩展名），
//	         其余段里第一个非类型目录段做模组（类型目录见 typeDirs）。
//	两者取并集；都为空时返回空集合（调用方应改走全量导入）。
func SelectPrograms(data []byte, modules, files []string) (*PartialSelection, error) {
	data = stripBOM(data)
	rows, header, err := readCSV(data)
	if err != nil {
		return nil, err
	}
	colProgram, ok1 := header["GAL01"]
	colModule, ok2 := header["GAL02"]
	colCode, ok3 := header["GAL03"]
	colLinked, ok4 := header["GAL04"]
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return nil, fmt.Errorf("表头缺少 GAL01..GAL04（实际列：%s）", headerNames(header))
	}

	moduleSet := map[string]bool{}
	for _, m := range modules {
		if m = strings.ToUpper(strings.TrimSpace(m)); m != "" {
			moduleSet[m] = true
		}
	}
	type spec struct{ module, code string }
	var specs []spec
	for _, raw := range files {
		if strings.TrimSpace(raw) == "" {
			continue
		}
		m, code := ParseFileSpec(raw)
		specs = append(specs, spec{module: m, code: code})
	}

	sel := &PartialSelection{
		Programs: map[string]bool{},
		Modules:  modules,
		Specs:    files,
	}
	get := func(row []string, idx int) string {
		if idx < len(row) {
			return strings.TrimSpace(row[idx])
		}
		return ""
	}
	for _, row := range rows {
		program := get(row, colProgram)
		module := get(row, colModule)
		code := get(row, colCode)
		if program == "" || code == "" || !strings.EqualFold(get(row, colLinked), "Y") {
			continue
		}
		sel.RowsY++
		if moduleSet[strings.ToUpper(module)] {
			if !sel.Programs[program] {
				sel.order = append(sel.order, program)
			}
			sel.Programs[program] = true
			sel.MatchedRows++
			continue
		}
		for _, sp := range specs {
			if !strings.EqualFold(sp.code, code) {
				continue
			}
			if sp.module == "" || strings.EqualFold(sp.module, module) {
				if !sel.Programs[program] {
					sel.order = append(sel.order, program)
				}
				sel.Programs[program] = true
				sel.MatchedRows++
				break
			}
		}
	}
	if len(sel.Programs) == 0 {
		return sel, nil
	}

	// 第二遍：把这些程序涉及的文件收集起来（去重、按出现序），供限定扫描用。
	seen := map[string]bool{}
	for _, row := range rows {
		program := get(row, colProgram)
		if !sel.Programs[program] {
			continue
		}
		if !strings.EqualFold(get(row, colLinked), "Y") {
			continue
		}
		ref := FileRef{get(row, colModule), get(row, colCode)}
		if ref.Module() == "" || ref.Code() == "" || seen[ref.Key()] {
			continue
		}
		seen[ref.Key()] = true
		sel.Files = append(sel.Files, ref)
	}
	return sel, nil
}

// ParseFileSpec 解析 `-files` 的一项，返回模组与程序代码。
//
//	"aap/4gl/saapt310.4gl"        → ("aap", "saapt310")
//	"topcust/aap/4gl/saapt310.4gl" → ("aap", "saapt310")   ← 工作区相对路径
//	"aap/saapt310"                → ("aap", "saapt310")
//	"saapt310.4gl"                → ("", "saapt310")
//
// 模组取"**紧挨着类型目录**的那一段"（`<模组>/4gl/x.4gl`）；
// 没有类型目录时取文件名前一段（`<模组>/x.4gl`）。
// 这一步很关键：插件传进来的是工作区相对路径（顶层还可能有 `tiptop/`、
// `topcust/` 这类项目目录），取"第一个段"会认错模组。
func ParseFileSpec(raw string) (module, code string) {
	raw = strings.TrimSpace(strings.ReplaceAll(raw, "\\", "/"))
	if raw == "" {
		return "", ""
	}
	var segs []string
	for _, seg := range strings.Split(raw, "/") {
		if seg != "" {
			segs = append(segs, seg)
		}
	}
	if len(segs) == 0 {
		return "", ""
	}
	last := segs[len(segs)-1]
	code = last
	if i := strings.LastIndex(last, "."); i > 0 {
		code = last[:i]
	}
	for i, seg := range segs {
		if typeDirs[strings.ToLower(seg)] && i > 0 {
			return segs[i-1], code
		}
	}
	if len(segs) >= 2 {
		return segs[len(segs)-2], code
	}
	return "", code
}

// importGroup 是导入过程中的一个「程序编号」分组。
type importGroup struct {
	program string
	files   []FileRef
	seen    map[string]bool
}

// scanSet 是并行扫描的结果集合。
type scanSet struct {
	scanOf map[string]FileScan
	// scanOf 按去重后的 key 存；missingTotal 按**记录**计
	// （同一代码在多个组里缺失要分别计，与 §11 实测的 800 条口径一致）。
	missingTotal int
	// defines / callers 只用于外部函数统计。
	defines map[string]bool
}

func scanFiles(groups map[string]*importGroup, order []string, opts ImportOptions) *scanSet {
	out := &scanSet{scanOf: map[string]FileScan{}, defines: map[string]bool{}}
	if opts.Scanner == nil {
		// 没有 scanner：全部视为"文件存在但无 MAIN"，即全 orphan。
		return out
	}

	// 去重：同一个 (模组,代码) 可能被几百个组共享，只解析一次。
	type job struct {
		ref FileRef
		key string
	}
	seen := map[string]bool{}
	var jobs []job
	for _, program := range order {
		for _, f := range groups[program].files {
			k := f.Key()
			if seen[k] {
				continue
			}
			seen[k] = true
			jobs = append(jobs, job{ref: f, key: k})
		}
	}

	workers := opts.Workers
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > len(jobs) {
		workers = len(jobs)
	}
	if workers < 1 {
		workers = 1
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	ch := make(chan job)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range ch {
				info := opts.Scanner.Scan(j.ref.Module(), j.ref.Code())
				mu.Lock()
				out.scanOf[j.key] = info
				for _, d := range info.Defines {
					out.defines[d] = true
				}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()

	for _, program := range order {
		for _, f := range groups[program].files {
			if info, ok := out.scanOf[f.Key()]; ok && !info.Found {
				out.missingTotal++
			}
		}
	}
	return out
}

// DefaultExternalsMinFiles 是"外部函数"的默认判定阈值：被至少这么多个
// 文件调用，却在整份语料里找不到定义。
//
// 取 3 而不是 2 的理由：笔误（`cl_err33`）通常只出现在 1–2 处；
// 而"快照里缺了这个库"会成片出现（实测 `cl_err3` 27315 次 / 186 个文件）。
const DefaultExternalsMinFiles = 3

// DefaultExternalsMinCalls 是另一个维度的阈值：同一个文件里被反复调用。
//
// 必须双维度：Genero 的 Web Service 辅助函数（`WSHelper_*`）只在
// 2 个生成出来的程序里出现，但每个都调用上千次——只看文件数会漏掉它们。
const DefaultExternalsMinCalls = 5

// MaxExternals 是容忍上限。超过它说明"资料或语料本身不对"（比如
// dataDir 指到了错误的项目、导出只导了一半），此时**不启用**过滤：
// 宁可多报，也不要让一份坏资料把所有函数都变成"存在"。
const MaxExternals = 2000

// MaxStoredExternals 是写进 meta 的名单上限（只存前 N 个，按文件数排序）。
// 全量存也能存下（实测几百个），但资料文件要保持可读。
const MaxStoredExternals = 500

// buildExternals 统计"被调用但从无定义"的函数，写进 meta。
//
// 用户定的口径：这类名字大概率是**语料快照缺少的库函数**（不是用户写错），
// 只要不是太多就忽略，但要**记录下来**——`tiptop link status` 会列出来，
// 出问题时能一眼看到"哪些函数被当成外部函数放过了"。
func buildExternals(res *ImportResult, scans *scanSet, opts ImportOptions) {
	minFiles := opts.ExternalsMinFiles
	if minFiles <= 0 {
		minFiles = DefaultExternalsMinFiles
	}
	minCalls := opts.ExternalsMinCalls
	if minCalls <= 0 {
		minCalls = DefaultExternalsMinCalls
	}
	res.Meta.ExternalsMinFiles = minFiles
	res.Meta.ExternalsMinCalls = minCalls

	type cand struct {
		name  string
		files int
		calls int
	}
	var cands []cand
	// 候选来自**整份语料**的调用统计（不是链接文件，够全），
	// 定义集合同样来自全语料 + 已解析文件（够准）。
	for name, st := range corpusCalls(opts) {
		if scans.defines[name] || (opts.Corpus != nil && opts.Corpus.Defines[name]) {
			continue
		}
		if opts.Ignore[name] {
			continue
		}
		if st.Files < minFiles && st.Calls < minCalls {
			continue
		}
		cands = append(cands, cand{name: name, files: st.Files, calls: st.Calls})
	}
	// 文件数降序，同数按调用次数降序，再按名字：输出稳定，便于 diff。
	sort.Slice(cands, func(i, j int) bool {
		if cands[i].files != cands[j].files {
			return cands[i].files > cands[j].files
		}
		if cands[i].calls != cands[j].calls {
			return cands[i].calls > cands[j].calls
		}
		return cands[i].name < cands[j].name
	})

	res.Meta.ExternalsTotal = len(cands)
	if len(cands) > MaxExternals {
		res.Meta.ExternalsSkipped = true
		res.Problems = append(res.Problems, fmt.Sprintf(
			"外部函数候选 %d 个，超过上限 %d：资料可能不完整或项目根不对，**不启用**外部函数过滤",
			len(cands), MaxExternals))
		// 仍然记录（供排查），但不生效。
	}
	if len(cands) > MaxStoredExternals {
		cands = cands[:MaxStoredExternals]
	}
	for _, c := range cands {
		res.Meta.Externals = append(res.Meta.Externals, External{Name: c.name, Files: c.files, Calls: c.calls})
	}
	if res.Meta.ExternalsTotal > 0 {
		res.Problems = append(res.Problems, fmt.Sprintf(
			"外部函数（被 ≥%d 个文件或 ≥%d 次调用、整份语料无定义）%d 个，最多的是：%s",
			minFiles, minCalls, res.Meta.ExternalsTotal, joinExternalNames(res.Meta.Externals, 5)))
	}
}

// corpusCalls 返回全语料的调用统计；没有 Corpus 时退回"链接文件里的调用"。
func corpusCalls(opts ImportOptions) map[string]*CallStat {
	if opts.Corpus != nil {
		return opts.Corpus.Calls
	}
	return nil
}

// joinExternalNames 取前 n 个名字（带文件数）拼成一行，用于报告。
func joinExternalNames(list []External, n int) string {
	if n > len(list) {
		n = len(list)
	}
	parts := make([]string, 0, n)
	for _, e := range list[:n] {
		parts = append(parts, fmt.Sprintf("%s(%d 文件/%d 次)", e.Name, e.Files, e.Calls))
	}
	return strings.Join(parts, "、")
}

func mainNames(refs []FileRef) string {
	names := make([]string, 0, len(refs))
	for _, r := range refs {
		names = append(names, r.Code())
	}
	return strings.Join(names, ", ")
}

// readCSV 解析 CSV，返回数据行与"列名（大写）→ 列下标"。
func readCSV(data []byte) ([][]string, map[string]int, error) {
	r := csv.NewReader(bytes.NewReader(data))
	// 字段数不固定时不要报错：数据里有缺列的行，由调用方逐行判断。
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true

	var rows [][]string
	header := map[string]int{}
	for {
		rec, err := r.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, fmt.Errorf("解析 CSV: %w", err)
		}
		if len(header) == 0 {
			for i, name := range rec {
				header[strings.ToUpper(strings.TrimSpace(name))] = i
			}
			// 表头行本身不算数据行。
			continue
		}
		rows = append(rows, rec)
	}
	if len(header) == 0 {
		return nil, nil, fmt.Errorf("CSV 为空（没有表头）")
	}
	return rows, header, nil
}

// stripBOM 去掉 UTF-8 BOM。实测 CSV 带 BOM，不处理会让首列名变成
// `\ufeffGAL01`，按表头定位列全部落空。
func stripBOM(data []byte) []byte {
	return bytes.TrimPrefix(data, []byte{0xEF, 0xBB, 0xBF})
}

func headerNames(h map[string]int) string {
	names := make([]string, 0, len(h))
	for name := range h {
		names = append(names, name)
	}
	// 稳定输出
	for i := range names {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	return strings.Join(names, ",")
}
