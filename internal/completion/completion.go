// Package completion 生成补全候选：关键字、同文件符号、GLOBALS 变量、
// 表单字段、内置符号（P7-C）。
//
// 设计取舍：
//   - 补全走"上下文判断 + 候选集合"，不做完整类型推导。Genero 是弱类型
//     且大量依赖运行时，做类型推导的收益远低于复杂度。
//   - 关键字清单来自 grammar 源码（keywords_gen.go），不手抄，
//     避免与 grammar 漂移。
//   - 触发字符只有 `.`（记录成员/表单字段访问），其余靠前缀匹配。
package completion

import (
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/builtin"
	"github.com/darcyjoven/bdl-lsp/internal/index"
)

// Kind 是补全项种类，映射到 LSP CompletionItemKind。
type Kind int

// 与 protocol.CompletionItemKind 的数值一致（LSP 规范固定值）。
const (
	KindText     Kind = 1
	KindMethod   Kind = 2
	KindFunction Kind = 3
	KindField    Kind = 5
	KindVariable Kind = 6
	KindClass    Kind = 7
	KindModule   Kind = 9
	KindKeyword  Kind = 14
	KindConstant Kind = 21
	KindStruct   Kind = 22
)

// Item 是一个补全候选。
type Item struct {
	// Label 是显示文本（也是插入文本，除非 InsertText 非空）。
	Label string
	// InsertText 非空时替换 Label 作为插入内容（用于带占位符的片段）。
	InsertText string
	// Kind 是种类。
	Kind Kind
	// Detail 是右侧简要说明（如 "FUNCTION" / "LIKE type_file.num5"）。
	Detail string
	// Documentation 是悬停/展开时的说明。
	Documentation string
	// SortText 控制排序；空则按 Label。
	SortText string
}

// Context 描述补全发生的位置。
type Context struct {
	// Lang 是 bdl 或 per。
	Lang string
	// Line 是光标所在行的**前缀**（光标之前的部分）。
	Line string
	// Word 是光标前的当前词（可能为空）。
	Word string
	// AfterDot 表示光标紧跟在 `.` 之后（成员访问）。
	AfterDot bool
	// InComment 表示光标在注释里（不补全）。
	InComment bool
	// InString 表示光标在字符串里（不补全）。
	InString bool
	// TriggerChar 是触发字符（`.` 等），手工触发时为空。
	TriggerChar string
}

// Result 是一次补全的结果。
type Result struct {
	Items []Item
	// IsIncomplete 表示候选集不完整（如只给了前缀匹配的一部分）。
	IsIncomplete bool
}

// Options 控制补全行为。
type Options struct {
	// MaxItems 是返回上限，0 表示用默认 200。
	MaxItems int
	// Keywords 关闭时不给关键字候选。
	Keywords bool
	// Fields 关闭时不给表单字段候选。
	Fields bool
	// Builtins 是内置符号候选（P7-C；可为空）。
	//
	// 由调用方从 builtin.Set 取出后传入，completion 包不持有全局状态：
	// 这样补全逻辑保持纯函数，热更新符号表时不需要动这里。
	Builtins []builtin.Symbol
}

// 默认上限：太多会把编辑器卡住，也淹没真正想要的项。
const defaultMaxItems = 200

// Provide 生成补全候选。
//
// file 是当前文件的索引（可为 nil），symbols 是跨文件候选（工作区符号，
// 可为空），fields 是当前可用的表单字段名（可为空）。
func Provide(ctx Context, file *index.FileIndex, symbols []*index.Symbol,
	fields []string, opts Options) Result {

	if ctx.InComment || ctx.InString {
		// 注释/字符串里补关键字只会干扰写作。
		return Result{}
	}
	max := opts.MaxItems
	if max <= 0 {
		max = defaultMaxItems
	}

	prefix := strings.ToLower(ctx.Word)
	var items []Item
	seen := map[string]bool{}

	add := func(it Item) {
		key := strings.ToLower(it.Label) + "\x00" + it.Detail
		if seen[key] {
			return
		}
		// 前缀过滤：空前缀（刚触发）时全给，否则按前缀匹配。
		if prefix != "" && !strings.HasPrefix(strings.ToLower(it.Label), prefix) {
			return
		}
		seen[key] = true
		items = append(items, it)
	}

	// 1) 成员访问（`.` 之后）：只给字段/记录成员，不给关键字。
	//    这里没有类型信息，给的是"当前表单字段 + 同名符号"，属于尽力而为。
	if ctx.AfterDot {
		for _, f := range fields {
			add(Item{Label: f, Kind: KindField, Detail: "表单字段", SortText: "0" + f})
		}
		if file != nil {
			for _, sym := range file.Symbols {
				switch sym.Kind {
				case index.KindDefine, index.KindConstant, index.KindField,
					index.KindParameter, index.KindFormItem:
				default:
					continue
				}
				add(Item{
					Label: sym.Name, Kind: KindField,
					Detail: symbolDetail(sym), SortText: "1" + sym.Name,
				})
			}
		}
		sortItems(items)
		return Result{Items: truncate(items, max)}
	}

	// 2) 表单字段（P3b 已解析出当前绑定的表单）
	if opts.Fields {
		for _, f := range fields {
			add(Item{Label: f, Kind: KindField, Detail: "表单字段", SortText: "0" + f})
		}
	}

	// 3) 当前文件符号（函数/变量/常量/类型）
	if file != nil {
		for _, sym := range file.Symbols {
			it, ok := itemForSymbol(sym)
			if !ok {
				continue
			}
			add(it)
		}
	}

	// 4) 跨文件符号（工作区函数/报表/GLOBALS 变量）
	for _, sym := range symbols {
		it, ok := itemForSymbol(sym)
		if !ok {
			continue
		}
		// 跨文件项排序靠后：同文件的名字更可能是用户想输的。
		it.SortText = "5" + it.Label
		add(it)
	}

	// 4.5) 内置符号（P7-C）。排序排在跨文件符号之后、关键字之前：
	//      同名的用户符号更可能是用户想输的，内置名作为兜底。
	//      同名的内置项直接跳过——用户自己定义了 `status` 时，
	//      列表里出现两个 STATUS 只会让人困惑。
	already := make(map[string]bool, len(items))
	for _, it := range items {
		already[strings.ToLower(it.Label)] = true
	}
	for _, b := range opts.Builtins {
		if already[strings.ToLower(b.Name)] {
			continue
		}
		add(Item{
			Label:         b.Name,
			Kind:          kindForBuiltin(b.Kind),
			Detail:        b.Kind.Label(),
			Documentation: builtinDoc(b),
			SortText:      "6" + b.Name,
		})
	}

	// 5) 关键字（最后加：排序上让符号优先）
	if opts.Keywords {
		kws := BDLKeywords
		if ctx.Lang == "per" {
			kws = PERKeywords
		}
		for _, kw := range kws {
			add(Item{
				Label: kw, Kind: KindKeyword, Detail: "关键字",
				SortText: "8" + kw,
			})
		}
	}

	sortItems(items)
	// 空前缀（手工触发、或刚输入一个字母）时标记不完整：编辑器会随着
	// 继续输入重新请求，避免一次返回 400 项。
	incomplete := prefix == ""
	return Result{Items: truncate(items, max), IsIncomplete: incomplete}
}

// kindForBuiltin 把内置符号种类映射到补全图标。
func kindForBuiltin(k builtin.Kind) Kind {
	switch k {
	case builtin.KindFunction:
		return KindFunction
	case builtin.KindClass:
		return KindClass
	case builtin.KindMethod:
		return KindMethod
	case builtin.KindConstant:
		return KindConstant
	default:
		return KindVariable
	}
}

// builtinDoc 拼出内置符号的补全说明（签名 + 一句话 + 出处）。
func builtinDoc(sym builtin.Symbol) string {
	var sb strings.Builder
	if sym.Signature != "" {
		sb.WriteString(sym.Signature)
	}
	if sym.Doc != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString(sym.Doc)
	}
	if sym.Since != "" {
		if sb.Len() > 0 {
			sb.WriteString("\n\n")
		}
		sb.WriteString("Genero " + sym.Since)
	}
	return sb.String()
}

// itemForSymbol 把索引符号转成补全项。
//
// 未列出的 Kind 一律当普通文本项处理（而不是丢弃）：新加符号种类时
// 补全仍然可用，只是排序与图标没那么精确。
func itemForSymbol(sym *index.Symbol) (Item, bool) {
	if sym == nil || sym.Name == "" {
		return Item{}, false
	}
	switch sym.Kind {
	case index.KindFunction:
		return Item{Label: sym.Name, Kind: KindFunction, Detail: "FUNCTION",
			SortText: "3" + sym.Name}, true
	case index.KindMain:
		return Item{Label: sym.Name, Kind: KindFunction, Detail: "MAIN",
			SortText: "4" + sym.Name}, true
	case index.KindReport:
		return Item{Label: sym.Name, Kind: KindFunction, Detail: "REPORT",
			SortText: "4" + sym.Name}, true
	case index.KindDefine:
		return Item{Label: sym.Name, Kind: KindVariable, Detail: symbolDetail(sym),
			SortText: "2" + sym.Name}, true
	case index.KindConstant:
		return Item{Label: sym.Name, Kind: KindConstant, Detail: "CONSTANT",
			SortText: "2" + sym.Name}, true
	case index.KindType:
		return Item{Label: sym.Name, Kind: KindStruct, Detail: "TYPE",
			SortText: "3" + sym.Name}, true
	case index.KindRecord:
		return Item{Label: sym.Name, Kind: KindStruct, Detail: "RECORD",
			SortText: "3" + sym.Name}, true
	case index.KindField:
		return Item{Label: sym.Name, Kind: KindField, Detail: symbolDetail(sym),
			SortText: "1" + sym.Name}, true
	case index.KindParameter:
		return Item{Label: sym.Name, Kind: KindVariable, Detail: "参数",
			SortText: "1" + sym.Name}, true
	case index.KindFormItem:
		return Item{Label: sym.Name, Kind: KindField, Detail: "表单 item",
			SortText: "1" + sym.Name}, true
	case index.KindScreenRec:
		return Item{Label: sym.Name, Kind: KindStruct, Detail: "SCREEN RECORD",
			SortText: "3" + sym.Name}, true
	case index.KindPrepare:
		return Item{Label: sym.Name, Kind: KindVariable, Detail: "PREPARE（prepared statement）",
			SortText: "1" + sym.Name}, true
	case index.KindCursor:
		return Item{Label: sym.Name, Kind: KindVariable, Detail: "游标",
			SortText: "1" + sym.Name}, true
	default:
		return Item{Label: sym.Name, Kind: KindText, Detail: symbolDetail(sym),
			SortText: "7" + sym.Name}, true
	}
}

// symbolDetail 组合符号的类型/签名文本作为补全项的右侧说明。
func symbolDetail(sym *index.Symbol) string {
	if sym == nil {
		return ""
	}
	if sym.Signature != "" {
		return sym.Signature
	}
	return sym.TypeText
}

// sortItems 按 SortText（空则 Label）排序，稳定排序保证同键顺序可预期。
func sortItems(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		a, b := items[i].sortKey(), items[j].sortKey()
		return a < b
	})
}

// sortKey 返回排序键。
func (it Item) sortKey() string {
	if it.SortText != "" {
		return it.SortText
	}
	return it.Label
}

// truncate 截断到 max 项。
func truncate(items []Item, max int) []Item {
	if len(items) <= max {
		return items
	}
	return items[:max]
}

// SplitWord 从行前缀里切出当前词（光标前的连续标识符字符）。
//
// Genero 标识符允许字母、数字、下划线、`$`、`#`（如 `p_text#`），
// 因此不能只用 unicode.IsLetter。
//
// 注意**不裁剪行尾空白**：`IF ` 之后用户已经在写新词，当前词应当是空，
// 于是补全给出全量候选；若裁掉空格会把前缀当成 "IF"，
// 结果只能匹配 IF 开头的关键字，等于什么都没给。
func SplitWord(line string) (word string, afterDot bool) {
	end := len(line)
	start := end
	for start > 0 && isIdentByte(line[start-1]) {
		start--
	}
	return line[start:end], start > 0 && line[start-1] == '.'
}

// isIdentByte 判断是否是标识符字符（Genero 允许 $ 与 #）。
func isIdentByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	case c == '_' || c == '$' || c == '#':
		return true
	}
	return false
}
