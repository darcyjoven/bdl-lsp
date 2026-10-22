package link

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

// CallStat 是一个函数名在全语料里的调用统计。
type CallStat struct {
	Files int
	Calls int
}

// CorpusIndex 是"整份语料扫一遍"的结果：每个 .4gl 的 MAIN/定义/调用。
//
// 为什么整份扫而不是只扫链接里的文件：
//  1. 外部函数统计需要**全语料**视角——链接资料只覆盖被链接到的文件，
//     一个函数可能在语料里存在却不在任何链上（只被 GLOBALS 引用，或属于
//     没有 MAIN 的 orphan 组）。只看链接文件会把这类真问题当"不存在"放过。
//  2. 代价可接受：链接里的 7795 个文件本来就要解析（导入 15 s），
//     全语料 7600 个 .4gl 基本是同一批文件，等于顺手多做一遍。
//
// 它同时实现 Scanner，import 直接用，不再单独扫描。
type CorpusIndex struct {
	byKey map[string]FileScan
	// Files 是扫到的 .4gl 数量（供报告）。
	Files int
	// Defines 是语料里定义过的函数名（小写）。
	Defines map[string]bool
	// Calls 是语料里被调用过的函数名（小写）及统计。
	Calls map[string]*CallStat
}

// Scan 实现 Scanner：按 (模组, 程序代码) 取扫描结果。
func (c *CorpusIndex) Scan(module, code string) FileScan {
	if c == nil {
		return FileScan{}
	}
	return c.byKey[strings.ToLower(module)+"/"+strings.ToLower(code)]
}

// ScanCorpus 解析整份语料并汇总。
func ScanCorpus(layout *Layout, pool *parser.Pool, workers int) (*CorpusIndex, error) {
	return ScanCorpusWithProgress(layout, pool, workers, nil)
}

// ScanCorpusWithProgress 与 ScanCorpus 相同，但每扫描约 200 个文件回调一次
// progress(done, total)。回调可能从多个 worker 并发调用，实现方需自行保证
// 线程安全；它只用于终端进度，不要在里面做重活。
func ScanCorpusWithProgress(layout *Layout, pool *parser.Pool, workers int, progress func(done, total int)) (*CorpusIndex, error) {
	files, err := corpusFiles(layout.RootDirs())
	if err != nil {
		return nil, err
	}
	calls, err := index.NewCallCollector(pool)
	if err != nil {
		return nil, err
	}
	defer calls.Close()

	out := &CorpusIndex{
		byKey:   make(map[string]FileScan, len(files)),
		Files:   len(files),
		Defines: make(map[string]bool, 1<<14),
		Calls:   make(map[string]*CallStat, 1<<14),
	}
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	total := len(files)
	var done atomic.Int64
	if progress != nil {
		progress(0, total)
	}

	var mu sync.Mutex
	var wg sync.WaitGroup
	ch := make(chan string)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for path := range ch {
				module, code, ok := layout.FileID(path)
				if !ok {
					continue
				}
				scan := scanOnePath(pool, calls, path)
				n := done.Add(1)
				mu.Lock()
				out.byKey[module+"/"+code] = scan
				for _, d := range scan.Defines {
					out.Defines[d] = true
				}
				mu.Unlock()
				if progress != nil && (n == int64(total) || n%200 == 0) {
					progress(int(n), total)
				}
			}
		}()
	}
	for _, f := range files {
		ch <- f
	}
	close(ch)
	wg.Wait()

	// 调用统计按"文件数/次数"合并（在锁外做，避免长时间持锁）。
	for _, scan := range out.byKey {
		for _, name := range scan.Calls {
			st := out.Calls[name]
			if st == nil {
				st = &CallStat{}
				out.Calls[name] = st
			}
			st.Calls += scan.callCount[name]
			st.Files++
		}
	}
	return out, nil
}

// scanOnePath 解析一个 .4gl，得到 MAIN/定义/调用。
func scanOnePath(pool *parser.Pool, calls *index.CallCollector, path string) FileScan {
	src, err := ReadSource(path)
	if err != nil {
		return FileScan{Found: true}
	}
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		return FileScan{Found: true}
	}
	defer tree.Close()

	out := FileScan{Found: true, callCount: map[string]int{}}
	if fi := index.Build(parser.LangBDL, tree, src, 1); fi != nil {
		for _, sym := range fi.Symbols {
			switch sym.Kind {
			case index.KindMain:
				out.HasMain = true
			case index.KindFunction:
				out.Defines = append(out.Defines, sym.Lower)
			}
		}
	}
	if !out.HasMain && ScanMainText(src) {
		out.HasMain = true
	}
	for _, call := range calls.Calls(tree, src) {
		out.callCount[call.Lower]++
		if _, seen := out.callSeen[call.Lower]; !seen {
			if out.callSeen == nil {
				out.callSeen = map[string]bool{}
			}
			out.callSeen[call.Lower] = true
			out.Calls = append(out.Calls, call.Lower)
		}
	}
	return out
}

// corpusFiles 列出模组根下全部 .4gl 文件。
func corpusFiles(roots []string) ([]string, error) {
	var out []string
	for _, root := range roots {
		entries, err := os.ReadDir(root)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
				continue
			}
			dir := filepath.Join(root, e.Name(), "4gl")
			subs, err := os.ReadDir(dir)
			if err != nil {
				continue
			}
			for _, s := range subs {
				if s.IsDir() || !strings.EqualFold(filepath.Ext(s.Name()), ".4gl") {
					continue
				}
				out = append(out, filepath.Join(dir, s.Name()))
			}
		}
	}
	return out, nil
}
