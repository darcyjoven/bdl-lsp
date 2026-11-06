package link

import (
	"sort"
	"strings"
	"sync"
)

// Resolver 把「链接资料 + 模组布局 + 函数索引」组合成"某个文件能调用哪些函数"。
//
// 可见性规则（用户 2026-11 定稿）：
//
//	可见函数 = 本文件 ∪ GLOBALS 引入 ∪ 链接文件 ∪ (lib ∪ sub ∪ qry 链接程序的清单)
//
// 注意后半句：全局可见的是**这三条链接程序（GAL01=lib/sub/qry）在资料里
// 列出的文件**，不是这三个物理目录下的所有文件。物理目录只用来把
// (模组,代码) 解析成路径。扁平规则，没有方向限制，三个程序"包括它们自己"。
// 变量**不走**这条路（R2：链接不放大变量可见性）。
type Resolver struct {
	idx    *Index
	layout *Layout
	global []string // 全局链接程序（GAL01，默认 lib/sub/qry）
	// externals 是"外部函数"集合（P7-D-2）：被 ≥minFiles 个文件调用、
	// 却在整份资料里找不到定义。它们算作"存在"，用于消掉
	// "语料快照缺少某个库"造成的大片误报。
	externals map[string]bool
	sym       *SymbolIndex
	// maxSync 是同步解析的文件数上限，超过就走后台（见 maxSyncDefault）。
	maxSync int
	// globOnce/gset 缓存全局链接程序的文件集合（诊断预热 + 链接资料 UI 共用）。
	globOnce sync.Once
	gset     *globalSet

	started bool
}

// NewResolver 组装解析器。
//
// parse 为 nil 时退化成"只看全局模组名"的模式（测试用），
// 真实使用必须提供（server 用 parser + index.Build 实现）。
func NewResolver(idx *Index, layout *Layout, globalModules []string, parse ParseFunc) *Resolver {
	if len(globalModules) == 0 {
		globalModules = DefaultGlobalModules()
	}
	return &Resolver{
		idx:       idx,
		layout:    layout,
		global:    append([]string(nil), globalModules...),
		externals: externalNames(idx),
		sym:       NewSymbolIndex(parse),
		maxSync:   maxSyncDefault,
	}
}

// externalNames 从资料 meta 里取出"生效的外部函数"集合。
//
// 资料自己已经算好了名单与阈值（导入时统计，见 buildExternals），这里只
// 负责过滤与兜底：`externalsSkipped` 表示名单太大（资料本身可疑），
// 此时**不启用**，宁可多报。
func externalNames(idx *Index) map[string]bool {
	if idx == nil {
		return nil
	}
	meta := idx.Meta()
	if meta == nil || meta.ExternalsSkipped || len(meta.Externals) == 0 {
		return nil
	}
	minFiles := meta.ExternalsMinFiles
	if minFiles <= 0 {
		minFiles = DefaultExternalsMinFiles
	}
	minCalls := meta.ExternalsMinCalls
	if minCalls <= 0 {
		minCalls = DefaultExternalsMinCalls
	}
	out := make(map[string]bool, len(meta.Externals))
	for _, e := range meta.Externals {
		if e.Files >= minFiles || e.Calls >= minCalls {
			out[e.Name] = true
		}
	}
	return out
}

// ExternalFunctions 返回生效的外部函数集合（只读，供状态展示与诊断）。
func (r *Resolver) ExternalFunctions() map[string]bool {
	if r == nil || len(r.externals) == 0 {
		return nil
	}
	out := make(map[string]bool, len(r.externals))
	for n := range r.externals {
		out[n] = true
	}
	return out
}

// ExternalTotal 返回资料里记录的外部函数总数（含"未存进文件"的部分）。
func (r *Resolver) ExternalTotal() int {
	if r == nil || r.idx == nil || r.idx.Meta() == nil {
		return 0
	}
	return r.idx.Meta().ExternalsTotal
}

// DefaultGlobalModules 是全局链接程序的出厂值。
//
// `lib` / `sub` / `qry` 在资料里各是一条**没有 MAIN 的链接程序**（GAL01），
// 它们的清单对所有程序可见（含它们自己）。这是**程序编号**，不是物理目录名：
// 清单里的文件可以位于任何模组目录（例如 `clib` / `cqry`）。
func DefaultGlobalModules() []string { return []string{"sub", "lib", "qry"} }

// Index 返回链路索引。
func (r *Resolver) Index() *Index { return r.idx }

// Layout 返回模组布局。
func (r *Resolver) Layout() *Layout { return r.layout }

// Symbols 返回函数索引（供状态展示与测试）。
func (r *Resolver) Symbols() *SymbolIndex { return r.sym }

// GlobalModules 返回全局链接程序（GAL01）。
func (r *Resolver) GlobalModules() []string { return append([]string(nil), r.global...) }

// globalSet 是"全局链接程序"提供的文件集合。
//
// 来源只有一份：链接资料里 GAL01 = lib/sub/qry 的那几条链的 Files。
// 物理目录不参与决定"哪些文件算全局"，只负责把 (模组,代码) 解析成路径；
// 解析不到的原样留在清单里（missing 计数），不静默丢掉。
type globalSet struct {
	// paths 是解析到磁盘的文件（绝对路径，已排序、已去重），用于函数索引预热。
	paths []string
	// files 是**链接程序编号（GAL01，大写）** → 该程序清单里的文件条目。
	//
	// 键是程序而不是物理模组：一个程序的清单可以横跨多个模组目录
	//（lib 的清单里可能有 lib/ 与 clib/），视图要按程序分组展示。
	files map[string][]TreeFile
	// missing 是清单里有、磁盘上没有的条目数。
	missing int
	// programs 是资料里实际找到的全局链接程序（GAL01 原样，按配置顺序）。
	programs []string
}

// globalSetData 懒建并缓存全局链接程序的文件集合。
//
// 成本：只遍历这几条链的 Files（数百到一千条），且只建一次；
// 路径解析走 Layout 已缓存的 name 索引，微秒级。
func (r *Resolver) globalSetData() *globalSet {
	if r == nil {
		return nil
	}
	r.globOnce.Do(func() {
		gs := &globalSet{files: map[string][]TreeFile{}}
		r.gset = gs
		if r.idx == nil || r.layout == nil {
			return
		}
		seenPath := map[string]bool{}
		seenProgram := map[string]bool{}
		for _, prog := range r.global {
			c := r.idx.FindByProgram(prog)
			if c == nil {
				// 资料里没有这条链接程序：按约定"照常判定"，不拿磁盘兜底。
				continue
			}
			key := strings.ToUpper(c.Program)
			if key == "" || seenProgram[key] {
				continue
			}
			seenProgram[key] = true
			gs.programs = append(gs.programs, c.Program)
			// 只在同一条程序内去重：同一文件若同时属于两条全局程序，
			// 两个节点下都该看得到。
			seen := map[string]bool{}
			for _, f := range c.Files {
				if f.Code() == "" {
					continue
				}
				fk := f.Key()
				if seen[fk] {
					continue
				}
				seen[fk] = true
				tf := TreeFile{Module: strings.ToUpper(f.Module()), Code: f.Code()}
				if p, ok := r.layout.Path(f.Module(), f.Code()); ok {
					tf.Path = p
					if !seenPath[p] {
						seenPath[p] = true
						gs.paths = append(gs.paths, p)
					}
				} else {
					gs.missing++
				}
				gs.files[key] = append(gs.files[key], tf)
			}
		}
		// 组内按 (模组, 代码) 排序：sub/* 与 csub/* 各自连在一起，
		// 文件多了也一眼能看出"这个程序横跨哪几个物理目录"。
		for _, list := range gs.files {
			sort.SliceStable(list, func(i, j int) bool {
				if list[i].Module != list[j].Module {
					return list[i].Module < list[j].Module
				}
				return strings.ToLower(list[i].Code) < strings.ToLower(list[j].Code)
			})
		}
		sort.Strings(gs.paths)
	})
	return r.gset
}

// GlobalFiles 返回全局链接程序清单里、能在磁盘上找到的 .4gl 绝对路径。
func (r *Resolver) GlobalFiles() []string {
	gs := r.globalSetData()
	if gs == nil {
		return nil
	}
	return append([]string(nil), gs.paths...)
}

// GlobalPrograms 返回资料里实际找到的全局链接程序（GAL01）。
func (r *Resolver) GlobalPrograms() []string {
	gs := r.globalSetData()
	if gs == nil {
		return nil
	}
	return append([]string(nil), gs.programs...)
}

// GlobalMissing 返回全局清单里"资料有、磁盘没有"的条目数。
func (r *Resolver) GlobalMissing() int {
	gs := r.globalSetData()
	if gs == nil {
		return 0
	}
	return gs.missing
}

// SetOnReady 注册"索引就绪"回调（server 用它补发一轮诊断）。
func (r *Resolver) SetOnReady(fn func()) {
	r.sym.SetOnReady(fn)
}

// Start 后台预热全局链接程序的函数索引（幂等）。
func (r *Resolver) Start() {
	if r == nil || r.started {
		return
	}
	r.started = true
	files := r.GlobalFiles()
	if len(files) == 0 {
		// 资料里没有全局链接程序（或它们没有文件）：直接算就绪，
		// 避免永远不启用诊断。
		r.sym.mu.Lock()
		r.sym.ready = true
		cb := r.sym.onReady
		r.sym.mu.Unlock()
		if cb != nil {
			cb()
		}
		return
	}
	r.sym.StartGlobal(files)
}

// Ready 报告可见性信息是否已经可用。
func (r *Resolver) Ready() bool {
	if r == nil || r.idx == nil {
		return false
	}
	return r.sym.Ready()
}

// VisibleFunctions 返回路径对应的文件"能调用的全部函数名"（小写）。
//
// complete=false 表示可见集不完整（全局模组还在后台索引，或链文件太多
// 已排队后台），调用方**必须**放弃 function-not-found 判定——
// 宁可漏报不可误报，这是 P7-B 用 3660 条误报换来的教训。
func (r *Resolver) VisibleFunctions(path string, hasMain bool) (set map[string]bool, complete bool) {
	if r == nil || r.idx == nil {
		return nil, false
	}
	globals := r.sym.GlobalFunctions()
	if globals == nil {
		return nil, false // 还没就绪
	}
	set = globals
	// 外部函数（快照里缺失的库函数）算作"存在"。
	for n := range r.externals {
		set[n] = true
	}

	module, code, ok := r.layout.FileID(path)
	if !ok {
		// 不在模组根下：只能按文件名找链，已经尽力了。
		code = CodeOf(path)
	}

	var refs []FileRef
	if hasMain {
		chain := r.idx.FindByMain(code)
		if chain == nil {
			// 这个程序不在链接资料里 → 它的链未知，不能判定。
			return set, false
		}
		refs = chain.Files
	} else {
		var chains []*Chain
		if ok {
			chains = r.idx.ChainsOfFile(module, code)
		}
		if len(chains) == 0 {
			chains = r.idx.ChainsOf(code)
		}
		if len(chains) == 0 {
			// 子文件不属于任何链：没有额外可见性，但这一点是**确定**的。
			return set, true
		}
		for _, c := range chains {
			refs = append(refs, c.Files...)
		}
	}

	paths, missing := r.resolvePaths(refs)
	extra, done := r.sym.Functions(paths, r.maxSync)
	for n := range extra {
		set[n] = true
	}
	if missing > 0 {
		// 链里有磁盘上不存在的文件 → 那些文件里有什么函数**不可知**，
		// 可见集不完整，不能判定"没找到就是不存在"。
		//
		// 实测影响面很小：5849 条链里只有 234 条（4.0%）引用缺失文件，
		// 集中在整批不在语料里的 W* 模组（另一套/已下线系统）。
		// 代价可接受，换来的是"缺失文件零误报"（用户明确要求）。
		return set, false
	}
	return set, done
}

// resolvePaths 把 (模组,代码) 解析成磁盘路径。
//
// 缺失文件**完全忽略**（R：链接里有、磁盘上没有 → 零诊断），但要把
// 缺失数量报给调用方——它意味着可见集不完整。同一路径去重（子文件被
// 几百个组共享时，重复路径会让缓存查询做无用功）。
func (r *Resolver) resolvePaths(refs []FileRef) (paths []string, missing int) {
	seen := make(map[string]bool, len(refs))
	out := make([]string, 0, len(refs))
	for _, f := range refs {
		p, ok := r.layout.Path(f.Module(), f.Code())
		if !ok {
			missing++
			continue
		}
		if seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out, missing
}

// ChainSize 返回该文件所在链的文件数（用于诊断/日志，不是判定依据）。
func (r *Resolver) ChainSize(path string, hasMain bool) int {
	if r == nil || r.idx == nil {
		return 0
	}
	code := CodeOf(path)
	if hasMain {
		if c := r.idx.FindByMain(code); c != nil {
			return len(c.Files)
		}
		return 0
	}
	module, _, ok := r.layout.FileID(path)
	var chains []*Chain
	if ok {
		chains = r.idx.ChainsOfFile(module, code)
	}
	if len(chains) == 0 {
		chains = r.idx.ChainsOf(code)
	}
	n := 0
	for _, c := range chains {
		n += len(c.Files)
	}
	return n
}

// Describe 返回一行人类可读的摘要（日志用）。
func (r *Resolver) Describe() string {
	if r == nil || r.idx == nil {
		return "link: 未加载"
	}
	st := r.idx.Stats()
	var sb strings.Builder
	sb.WriteString("link: ")
	sb.WriteString(strings.Join(r.global, "/"))
	sb.WriteString(" 全局可见；链 ")
	sb.WriteString(itoa(st.Chains))
	sb.WriteString("，orphan ")
	sb.WriteString(itoa(st.Orphans))
	sb.WriteString("，文件 ")
	sb.WriteString(itoa(st.Files))
	if !r.sym.Ready() {
		sb.WriteString("（索引预热中）")
	}
	return sb.String()
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
