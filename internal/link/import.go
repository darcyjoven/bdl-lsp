package link

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"runtime"
	"strings"
	"sync"
	"time"
)

// MainFinder 报告 (模组, 程序代码) 对应的源文件里有没有 MAIN。
//
// 为什么要外部注入：判断"这一组里哪一条是 MAIN"必须读文件，而本包
// 不想依赖 tree-sitter（那会让纯数据逻辑也要 cgo）。真实实现由
// server/CLI 提供（解析 + 查 KindMain），测试用假实现。
//
// found=false 表示磁盘上找不到这个文件——**这是常态**，不是错误：
// 实测 800 条记录落在语料里不存在的 W* 模组（另一套/已下线系统）。
type MainFinder interface {
	Lookup(module, code string) (hasMain bool, found bool)
}

// ImportOptions 控制导入行为。
type ImportOptions struct {
	// MainFinder 用于判定 MAIN。为 nil 时所有组都按 orphan 处理
	// （只用于"没有源码"的离线校验场景）。
	MainFinder MainFinder
	// Source / SourceDetail 记进 meta，例如 "db" / "DS.GAL_FILE@host:1521/svc"。
	Source       string
	SourceDetail string
	// ToolVersion 记进 meta。
	ToolVersion string
	// Now 用于生成 generatedAt（测试注入固定时间）。
	Now time.Time
	// Workers 是判定 MAIN 时的并发度（0 = GOMAXPROCS）。
	Workers int
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
	mains := lookupMains(groups, order, opts)

	// ---- 第三遍：成链 ----
	for _, program := range order {
		g := groups[program]
		if len(g.files) == 0 {
			continue
		}
		c := &Chain{Schema: SchemaVersion, Program: program, Files: g.files}
		var mainRefs []FileRef
		for _, f := range g.files {
			info := mains.mainLookup[f.Key()]
			if !info.found {
				// 缺失文件原样保留（用户明确要求），统计由 lookupMains 统一做。
				continue
			}
			if info.hasMain {
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

	res.Meta.MissingFiles = mains.missingTotal
	SortChains(res.Chains)
	return res, nil
}

// importGroup 是导入过程中的一个「程序编号」分组。
type importGroup struct {
	program string
	files   []FileRef
	seen    map[string]bool
}

// mainLookup 是并行判定 MAIN 的结果集合。
type mainLookup map[string]mainInfo

type mainInfo struct {
	hasMain bool
	found   bool
}

// mainLookupResult 同时带回统计（missingTotal 按记录计，不按去重计）。
type mainLookupResult struct {
	mainLookup
	missingTotal int
}

func lookupMains(groups map[string]*importGroup, order []string, opts ImportOptions) *mainLookupResult {
	out := &mainLookupResult{mainLookup: mainLookup{}}
	if opts.MainFinder == nil {
		// 没有 finder：全部视为"文件存在但无 MAIN"，即全 orphan。
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
				hasMain, found := opts.MainFinder.Lookup(j.ref.Module(), j.ref.Code())
				mu.Lock()
				out.mainLookup[j.key] = mainInfo{hasMain: hasMain, found: found}
				mu.Unlock()
			}
		}()
	}
	for _, j := range jobs {
		ch <- j
	}
	close(ch)
	wg.Wait()

	// 缺失统计按**记录**算（同一代码在多个组里缺失要分别计），
	// 与 §11 实测的 800 条口径一致。
	for _, program := range order {
		for _, f := range groups[program].files {
			if info, ok := out.mainLookup[f.Key()]; ok && !info.found {
				out.missingTotal++
			}
		}
	}
	return out
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
