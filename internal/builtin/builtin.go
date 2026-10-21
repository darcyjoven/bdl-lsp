// Package builtin 维护 Genero BDL 的内置符号表（内置变量 / 函数 / 类与方法 /
// 常量），供三处使用：变量来源白名单、函数可见性、补全与悬停。
//
// # 为什么放插件侧，不进 grammar
//
// 见 docs/PLAN-P7.md §4.2：内置名一旦成为 grammar 的 keyword token，
// 高亮会按关键字染色并冒充用户自定义的同名符号（BDL 标识符大小写不敏感，
// `status` 是很常见的变量名），重命名与补全的语义判定也会全线复杂化。
// grammar 的职责是语法，名字表不是它的职责。
//
// # 为什么是数据文件，不是 Go 字面量
//
// 内置名单会随 Genero 版本增长，也会因为“语料里发现漏项”而补。
// 如果写死在 Go 里，每次补一条都要改代码、重新编译、重新发版。
// 所以分两层：
//
//  1. **内置数据**：data/builtins.json 用 go:embed 打进二进制。
//     维护者补名单只改 JSON，不动 Go（`make builtin-check` 校验格式）。
//  2. **用户覆盖**：项目里放 `.bdl-lsp/builtins.json`（或配置
//     `bdl.builtins.paths`）即可增补/改写/禁用，**不用重编译、不用改扩展**。
//     同名按字段合并（非空字段覆盖），`disable` 列表里的名字整条移除，
//     且 disable 的优先级高于任何 add（否则“关不掉”会很难排查）。
package builtin

import (
	"sort"
	"strings"
	"sync"
)

// Kind 是内置符号的种类。
type Kind string

const (
	// KindVariable 是内置全局变量（STATUS / INT_FLAG / SQLCA …）。
	KindVariable Kind = "variable"
	// KindFunction 是内置函数（ARG_VAL / LENGTH / fgl_* …）。
	KindFunction Kind = "function"
	// KindClass 是内置类（base.Channel / xml.DomDocument …）。
	// 在代码里通常作为包名前缀出现：`ui.Interface`。
	KindClass Kind = "class"
	// KindMethod 是内置类的方法，Class 字段指向所属类。
	KindMethod Kind = "method"
	// KindConstant 是内置常量（传统 4GL 的 TRUE/FALSE 等）。
	// 真正被 grammar 当字面值处理的不在此列，这里只登记“名字合法”这一事实。
	KindConstant Kind = "constant"
)

// Valid 报告 kind 是否是已知种类。
func (k Kind) Valid() bool {
	switch k {
	case KindVariable, KindFunction, KindClass, KindMethod, KindConstant:
		return true
	}
	return false
}

// Label 返回给用户看的中文种类名（悬停/补全的 Detail）。
func (k Kind) Label() string {
	switch k {
	case KindVariable:
		return "内置变量"
	case KindFunction:
		return "内置函数"
	case KindClass:
		return "内置类"
	case KindMethod:
		return "内置方法"
	case KindConstant:
		return "内置常量"
	}
	return "内置符号"
}

// Symbol 是一条内置符号。
type Symbol struct {
	// Name 是原始大小写；查找一律大小写不敏感（BDL 标识符不敏感）。
	Name string `json:"name"`
	// Kind 是种类，必填。
	Kind Kind `json:"kind"`
	// Signature 是展示用签名，如 `ARG_VAL(index)`；可为空。
	Signature string `json:"signature,omitempty"`
	// Doc 是一句话说明；可为空。
	Doc string `json:"doc,omitempty"`
	// Since 是引入版本（"2.50" / "3.00"），用于将来按 generoVersion 过滤。
	Since string `json:"since,omitempty"`
	// Category 是手册里的分类（string/dialog/sql/os…），仅用于展示与排查。
	Category string `json:"category,omitempty"`
	// Class 仅 KindMethod 使用，指向所属类的名字。
	Class string `json:"class,omitempty"`
}

// Set 是合并后的只读内置符号表。
//
// 零值不可用；用 NewSet 或 Loader 构造。Set 构造完成后不再修改，
// 因此可以并发读，不需要加锁。
type Set struct {
	byName   map[string]Symbol // key 为小写名字
	disabled map[string]bool   // key 为小写名字
	version  string            // 数据声明的 Genero 基线版本
	sources  []Source
	names    []string // 已排序的原始名字，供稳定遍历
}

// Source 记录一份数据文件的加载情况（排查"我的覆盖为什么没生效"用）。
type Source struct {
	// Path 是文件路径；内置数据为 "(embedded)"。
	Path string
	// Builtin 标记是否来自嵌入数据。
	Builtin bool
	// Symbols 是该文件声明的符号条数。
	Symbols int
	// Disabled 是该文件 disable 的名字条数。
	Disabled int
	// Err 非 nil 表示该文件加载失败（已被跳过，不影响其他来源）。
	Err error
}

// NewSet 构造一个空集合（主要给测试与合并用）。
func NewSet() *Set {
	return &Set{
		byName:   map[string]Symbol{},
		disabled: map[string]bool{},
	}
}

// Len 返回合并后可用符号数。
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.byName)
}

// GeneroVersion 返回数据文件声明的 Genero 基线版本。
func (s *Set) GeneroVersion() string {
	if s == nil {
		return ""
	}
	return s.version
}

// Sources 返回各数据来源的加载记录。
func (s *Set) Sources() []Source {
	if s == nil {
		return nil
	}
	out := make([]Source, len(s.sources))
	copy(out, s.sources)
	return out
}

// Lookup 按名字查内置符号（大小写不敏感）。
func (s *Set) Lookup(name string) (Symbol, bool) {
	if s == nil || name == "" {
		return Symbol{}, false
	}
	sym, ok := s.byName[strings.ToLower(name)]
	return sym, ok
}

// IsKnown 报告名字是否是任何一类内置符号。
//
// 用于变量来源诊断的白名单：内置名在语法树里就是一个普通 identifier，
// 静态分析找不到定义，但这不代表用户写错了。
func (s *Set) IsKnown(name string) bool {
	_, ok := s.Lookup(name)
	return ok
}

// IsFunction 报告名字是否是内置函数。
//
// 只认 KindFunction：类名（`ui`）与方法名（`open`）在调用点上的形态
// 与函数不同（前者是包前缀，后者要靠类解析），留给 P7-D 的链接索引
// 与类成员解析处理，这里不越权。
func (s *Set) IsFunction(name string) bool {
	sym, ok := s.Lookup(name)
	return ok && sym.Kind == KindFunction
}

// IsVariable 报告名字是否是内置变量或常量。
func (s *Set) IsVariable(name string) bool {
	sym, ok := s.Lookup(name)
	if !ok {
		return false
	}
	return sym.Kind == KindVariable || sym.Kind == KindConstant
}

// Symbols 返回按名字排序的全部符号（补全/审计用）。
func (s *Set) Symbols() []Symbol {
	if s == nil {
		return nil
	}
	out := make([]Symbol, 0, len(s.names))
	for _, n := range s.names {
		out = append(out, s.byName[strings.ToLower(n)])
	}
	return out
}

// Prefix 返回名字以 prefix 开头的符号（大小写不敏感），最多 limit 条
// （limit <= 0 表示不限）。函数与变量混排，按名字排序，保证结果稳定。
//
// prefix 为空表示"全部"：补全的手工触发（Ctrl+Space）就是这种情形，
// 与关键字清单的处理保持一致。
func (s *Set) Prefix(prefix string, limit int) []Symbol {
	if s == nil {
		return nil
	}
	p := strings.ToLower(prefix)
	var out []Symbol
	for _, n := range s.names {
		if !strings.HasPrefix(strings.ToLower(n), p) {
			continue
		}
		out = append(out, s.byName[strings.ToLower(n)])
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// add 合并一条符号。
//
// 同名按字段合并而不是整条替换：用户只想补一句 Doc 或改个 Signature 时，
// 不该被迫把 Kind/Category 全部重写一遍（整条替换一旦漏字段就会悄悄丢信息）。
// 约定：新条目的非空字段覆盖旧值，Kind 以新条目为准（必填）。
func (s *Set) add(sym Symbol) {
	key := strings.ToLower(sym.Name)
	if key == "" {
		return
	}
	if old, ok := s.byName[key]; ok {
		if sym.Kind != "" {
			old.Kind = sym.Kind
		}
		if sym.Signature != "" {
			old.Signature = sym.Signature
		}
		if sym.Doc != "" {
			old.Doc = sym.Doc
		}
		if sym.Since != "" {
			old.Since = sym.Since
		}
		if sym.Category != "" {
			old.Category = sym.Category
		}
		if sym.Class != "" {
			old.Class = sym.Class
		}
		// 名字保留最初的大小写，避免同一符号在不同来源里抖动。
		s.byName[key] = old
		return
	}
	s.byName[key] = sym
}

// finish 应用 disable 列表并生成排序好的名字索引。
//
// disable 必须最后统一应用：否则“先 disable 再 add”会让结果依赖文件顺序，
// 用户永远搞不清自己那份覆盖到底生效了没有。
func (s *Set) finish() *Set {
	for key := range s.disabled {
		delete(s.byName, key)
	}
	s.names = make([]string, 0, len(s.byName))
	for _, sym := range s.byName {
		s.names = append(s.names, sym.Name)
	}
	sort.Slice(s.names, func(i, j int) bool {
		return strings.ToLower(s.names[i]) < strings.ToLower(s.names[j])
	})
	return s
}

// clone 深拷贝一个集合，供"在基础表上叠覆盖"使用。
func (s *Set) clone() *Set {
	c := NewSet()
	for k, v := range s.byName {
		c.byName[k] = v
	}
	for k := range s.disabled {
		c.disabled[k] = true
	}
	c.version = s.version
	c.sources = append([]Source(nil), s.sources...)
	return c
}

// Disabled 报告某名字是否被显式禁用（含内置表里根本没有的名字）。
func (s *Set) Disabled(name string) bool {
	if s == nil {
		return false
	}
	return s.disabled[strings.ToLower(name)]
}

// muSet 是一个可热替换的 Set 包装，供 Loader 与消费方共享。
type muSet struct {
	mu  sync.RWMutex
	set *Set
}

func (m *muSet) load() *Set {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.set
}

func (m *muSet) store(s *Set) {
	m.mu.Lock()
	m.set = s
	m.mu.Unlock()
}
