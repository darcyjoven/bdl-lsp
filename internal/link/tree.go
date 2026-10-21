package link

import (
	"sort"
	"strings"
)

// LinkTree 是「链接资料」视图的数据（P7-E）：给编辑器 TreeView 用。
//
// 为什么由服务器组装而不是让插件自己读 links.jsonl：
//   - 插件不该知道资料格式（schema 会演进）；
//   - 文件路径要按 Layout 的模组根拼，只有服务器知道真实根在哪里；
//   - 反查（ChainsOfFile）需要内存索引，插件另建一份会重复几百毫秒。
type LinkTree struct {
	// Found=false 表示这个文件不在任何链里（子文件、缺失文件、或没有资料）。
	Found bool `json:"found"`
	// Program 是程序编号（GAL01，展示用）。
	Program string `json:"program,omitempty"`
	// Main 是命中的 MAIN 程序代码（空 = 只有 orphan 链）。
	Main string `json:"main,omitempty"`
	// Ambiguous 表示该组有多个 MAIN。
	Ambiguous bool `json:"ambiguous,omitempty"`
	// Files 是链里的文件，**按 (模组, 代码) 排序**，路径已解析。
	Files []TreeFile `json:"files,omitempty"`
	// Modules 是"全局可见模组"（sub/lib/qry）下的文件：模块名 → 文件。
	// 这些模组对每个程序都可见，逐个铺进 Files 会重复几千行，所以单列。
	Modules map[string][]TreeFile `json:"modules,omitempty"`
	// Refs 是"反查"结果：把当前文件链接进去的程序（最多 MaxRefs 个）。
	Refs []string `json:"refs,omitempty"`
	// RefTotal 是反查命中的总数（Refs 可能被截断）。
	RefTotal int `json:"refTotal,omitempty"`
}

// TreeFile 是树里的一个文件节点。
type TreeFile struct {
	Module string `json:"module"`
	Code   string `json:"code"`
	// Path 是磁盘路径（资料里的文件可能不存在，此时为空）。
	Path string `json:"path,omitempty"`
	// Main 表示这个文件自己含 MAIN。
	Main bool `json:"main,omitempty"`
}

// MaxRefs 是反查返回的程序数上限（实测最多 324 个，分页交给插件）。
const MaxRefs = 50

// Tree 组装某个文件的链接视图。
//
// target 为链内文件时返回该链；为 MAIN 自身时同上；不在链里时只做反查。
func (r *Resolver) Tree(module, code string) *LinkTree {
	if r == nil || r.idx == nil {
		return &LinkTree{}
	}
	t := &LinkTree{}
	chains := r.idx.ChainsOfFile(module, code)
	if len(chains) == 0 {
		chains = r.idx.ChainsOf(code)
	}
	// 优先选"自己就是 MAIN"的那条链：从主程序点进来时应看到自己的链。
	var picked *Chain
	for _, c := range chains {
		for _, m := range c.AllMains() {
			if strings.EqualFold(m, code) {
				picked = c
				break
			}
		}
		if picked != nil {
			break
		}
	}
	if picked == nil && len(chains) > 0 {
		picked = chains[0]
	}
	if picked != nil {
		t.Found = true
		t.Program = picked.Program
		t.Main = picked.Main
		t.Ambiguous = picked.Ambiguous
		mains := picked.AllMains()
		t.Files = make([]TreeFile, 0, len(picked.Files))
		for _, f := range picked.Files {
			tf := TreeFile{Module: strings.ToUpper(f[0]), Code: f[1]}
			if r.layout != nil {
				if p, ok := r.layout.Path(f[0], f[1]); ok {
					tf.Path = p
				}
			}
			for _, m := range mains {
				if strings.EqualFold(m, f[1]) {
					tf.Main = true
				}
			}
			t.Files = append(t.Files, tf)
		}
		sort.SliceStable(t.Files, func(i, j int) bool {
			if t.Files[i].Module != t.Files[j].Module {
				return t.Files[i].Module < t.Files[j].Module
			}
			return strings.ToLower(t.Files[i].Code) < strings.ToLower(t.Files[j].Code)
		})
	}

	// 全局模组：每个程序都可见，单列出来（插件侧懒加载）。
	// 三份清单会被每个文件的每次刷新复用，所以缓存在 resolver 上——
	// lib 一个模组就 983 个文件，每次重建纯属浪费。
	if len(r.global) > 0 {
		t.Modules = r.globalModuleFiles()
	}

	// 反查：把 (模组, 代码) 链接进去的程序。
	refs := r.idx.ChainsOfFile(module, code)
	t.RefTotal = len(refs)
	for _, c := range refs {
		if len(t.Refs) >= MaxRefs {
			break
		}
		name := c.Main
		if name == "" {
			name = c.Program
		}
		if name == "" {
			continue
		}
		t.Refs = append(t.Refs, name)
	}
	sort.Strings(t.Refs)
	return t
}

// globalModuleFiles 返回全局模组的文件清单（惰性构建、缓存）。
//
// 缓存生命周期跟着 resolver：资料重载会重建 resolver，模组文件增删
// 由下一次重载带走。这与"看链接资料"的语义一致——它本来就不是实时的。
func (r *Resolver) globalModuleFiles() map[string][]TreeFile {
	r.modOnce.Do(func() {
		out := map[string][]TreeFile{}
		if r.layout == nil {
			r.modFiles = out
			return
		}
		for _, mod := range r.global {
			files := r.layout.ModuleFiles(mod)
			if len(files) == 0 {
				continue
			}
			list := make([]TreeFile, 0, len(files))
			for _, f := range files {
				_, code, ok := r.layout.FileID(f)
				if !ok || code == "" {
					continue
				}
				list = append(list, TreeFile{Module: strings.ToUpper(mod), Code: code, Path: f})
			}
			sort.SliceStable(list, func(i, j int) bool {
				return strings.ToLower(list[i].Code) < strings.ToLower(list[j].Code)
			})
			out[strings.ToUpper(mod)] = list
		}
		r.modFiles = out
	})
	return r.modFiles
}
