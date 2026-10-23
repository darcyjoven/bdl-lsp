// globals.go 把 Resolver 接进诊断流（P3a T3）。
//
// 数据流：
//
//	didOpen/didChange → reparse（语法诊断）
//	                  → globalsChecks（导入解析 + globals 诊断）
//	                  → publish（合并发布，LSP 诊断是全量替换）
//
//	文档 X 变更且 X 是别人的导入目标 → RefreshURI(X) + refreshImporters：
//	引用方的悬停/跳转/来源诊断依赖 X 的符号，需要重算。
package server

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/internal/diag"
	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/globals"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	"github.com/darcyjoven/bdl-lsp/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
	"go.lsp.dev/protocol"
)

// newGlobalsResolver 在 initialize 时创建（rootPath 已知后）。
//
// openDoc 回调让 Resolver 优先使用已打开文档的内存版本：
// 编辑器里打开的 .global 改动后，引用方无需等磁盘落盘就能看到新符号。
func (s *Server) newGlobalsResolver(rootPath string) {
	sp := s.settings().Globals.SearchPaths
	s.globalsRes = globals.NewResolver(s.pool, rootPath, sp, func(uri string) ([]byte, bool) {
		if doc := s.docs.Get(uri); doc != nil {
			return doc.Text(), true
		}
		return nil, false
	})
}

// globalsChecks 解析文档的 GLOBALS 导入并生成诊断（不发布，由调用方合并）。
//
// 副作用：更新 Resolver 的反向依赖表（供 refreshImporters 与跳转使用）。
// 配置为 off 时仍然执行解析——跳转/悬停依赖解析结果，只有诊断被关闭。
func (s *Server) globalsChecks(ctx context.Context, doc *document.Document, fi *index.FileIndex) []protocol.Diagnostic {
	if s.globalsRes == nil || fi == nil {
		// 解析失败（fi 为 nil）时也要清掉旧结果：否则文档解析出错期间
		// 仍会"看得见"上一次成功解析的 GLOBALS 符号。
		if doc != nil {
			s.forgetGlobalsEntries(doc.URI)
		}
		return nil
	}
	if doc.Lang != parser.LangBDL {
		return nil // .per 没有 GLOBALS 语法
	}
	if len(fi.GlobalsImports) == 0 {
		// 文档已经没有 GLOBALS 导入了（用户删掉了那几行）。
		// 必须清掉旧结果：否则这个文档会继续"看得见"已删除的
		// .global 里的符号，来源标注也还指向它。
		s.forgetGlobalsEntries(doc.URI)
		return nil
	}

	settings := s.settings()
	entries := s.globalsRes.Resolve(ctx, doc.URI, fi.GlobalsImports)

	// 缓存解析结果，供跳转/悬停查询（定义在哪、来源是什么）。
	// 注意：即使诊断关闭也缓存——导航能力不受诊断配置影响。
	s.globalsMu.Lock()
	s.globalsEntries[doc.URI] = entries
	s.globalsMu.Unlock()

	severity, enabled := diag.SeverityFor(diag.CodeGlobalsFileNotFound, settings)
	if !enabled {
		return nil
	}

	idx := doc.Index()
	var diags []protocol.Diagnostic
	for _, e := range entries {
		if e.Status != globals.StatusNotFound {
			continue
		}
		diags = append(diags, protocol.Diagnostic{
			Range:    byteRange(idx, e.Import.StartByte, e.Import.EndByte),
			Severity: severity,
			Source:   "bdl-lsp",
			Message:  fmt.Sprintf("GLOBALS 文件未找到：%q（相对当前文件目录解析）", e.Import.Literal),
			Code:     string(diag.CodeGlobalsFileNotFound),
		})
	}
	return diags
}

// globalsDefinitions 在**当前文档**的 GLOBALS 引入里查符号定义（跳转用，P3a T4）。
//
// 返回跨文件 Location 列表与来源描述（悬停用）。
// 跳过函数/报表类符号：.global 文件里不放函数（语料 57 个文件均无），
// 且函数调用名不该被导入解析劫持。
//
// 为什么必须限定"当前文档"（P3a 收尾修复）：
//
//	globalsEntries 是 docURI → 解析结果 的全局表。旧实现遍历**整张表**，
//	于是同时打开两个文档时，一个文档会借用另一个文档的 GLOBALS 导入：
//	  - 两个文档都引用同一个 .global → 同一符号被列出多份来源
//	    （悬停里出现两条一模一样的 "top.global:2"）；
//	  - 两个文档引用**不同**的 .global → 变量来源检查会拿到别的文档的
//	    符号集合，导致 "无法确定变量来源" 该报的不报、不该报的也不报。
//
//	症状随"当前开着哪些文件"变化：关掉另一个文档或重启 VSCode 就正常，
//	用户完全无法复现规律。正确做法是只看当前文档自己的导入。
func (s *Server) globalsDefinitions(docURI, word string) (locs []protocol.Location, sources []string) {
	entries := s.globalsEntriesOf(docURI)
	if len(entries) == 0 {
		return nil, nil
	}

	lower := strings.ToLower(word)
	// 按"定义位置"去重：同一个 .global 被引入两次（例如
	// GLOBALS "a.global" + GLOBALS "./a.global"），或两个 .global
	// 引入的是同一个物理文件，符号表里会出现两份同样的定义。
	// 不去重的话悬停会列出两条一模一样的来源。
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Status != globals.StatusResolved || e.Index == nil {
			continue
		}
		for _, sym := range e.Index.Symbols {
			if sym.Lower != lower || sym.Scope != index.ScopeGlobals {
				continue
			}
			switch sym.Kind {
			case index.KindFunction, index.KindReport, index.KindMain:
				continue
			}
			key := fmt.Sprintf("%s:%d", e.URI, sym.StartByte)
			if seen[key] {
				continue
			}
			seen[key] = true
			locs = append(locs, protocol.Location{
				URI:   protocol.DocumentURI(e.URI),
				Range: byteRange(e.Lines, sym.StartByte, sym.EndByte),
			})
			line, _ := e.Lines.Position(int(sym.StartByte))
			sources = append(sources, fmt.Sprintf("%s:%d", e.Path, line+1))
		}
	}
	return locs, sources
}

// globalsEntriesOf 返回某个文档自己的 GLOBALS 解析结果。
//
// 返回的是**快照副本**（切片头拷贝）：调用方在锁外遍历，
// 不能直接持有 map 里的切片（并发写入会与遍历竞争）。
func (s *Server) globalsEntriesOf(docURI string) []globals.Entry {
	s.globalsMu.RLock()
	defer s.globalsMu.RUnlock()
	entries := s.globalsEntries[docURI]
	if len(entries) == 0 {
		return nil
	}
	out := make([]globals.Entry, len(entries))
	copy(out, entries)
	return out
}

// forgetGlobalsEntries 在文档关闭时清掉它的 GLOBALS 解析结果。
//
// 不清会留下悬空条目：文档关闭后，别的文档仍会借用它的导入结果
// （见 globalsDefinitions 的注释）。这正是"关掉文件/重启 VSCode 后
// 结果才变"的另一个来源。
func (s *Server) forgetGlobalsEntries(docURI string) {
	s.globalsMu.Lock()
	delete(s.globalsEntries, docURI)
	s.globalsMu.Unlock()
}

// globalsSourceLines 返回悬停用的来源描述（不带跳转，轻量）。
func (s *Server) globalsSourceLines(docURI, word string) []string {
	_, sources := s.globalsDefinitions(docURI, word)
	return sources
}

// globalsTypeLines 返回 GLOBALS 命中符号的类型文本（悬停用）。
//
// 与 globalsDefinitions 的 sources 顺序对齐（同一遍历顺序）。
func (s *Server) globalsTypeLines(docURI, word string) []string {
	entries := s.globalsEntriesOf(docURI)

	lower := strings.ToLower(word)
	var out []string
	// 与 globalsDefinitions 同样的去重键：两份 sources 必须与两份 typeLines 一一对应。
	seen := map[string]bool{}
	for _, e := range entries {
		if e.Status != globals.StatusResolved || e.Index == nil {
			continue
		}
		for _, sym := range e.Index.Symbols {
			if sym.Lower != lower || sym.Scope != index.ScopeGlobals {
				continue
			}
			switch sym.Kind {
			case index.KindFunction, index.KindReport, index.KindMain:
				continue
			}
			key := fmt.Sprintf("%s:%d", e.URI, sym.StartByte)
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, sym.TypeText)
		}
	}
	return out
}

// wordOffset 找光标处词的字节偏移（悬停 Range 用）。
func wordOffset(doc *document.Document, idx *textpos.Index, word string) (int, bool) {
	if doc == nil || idx == nil || word == "" {
		return 0, false
	}
	text := doc.Text()
	lower := strings.ToLower(word)
	// 在文档里找第一个匹配（大小写不敏感）；
	// 悬停场景下光标处的词就是它，简单前向搜索足够。
	n := len(text)
	wl := len(word)
	for i := 0; i+wl <= n; i++ {
		if strings.EqualFold(string(text[i:i+wl]), word) {
			// 词边界检查：前后不能是标识符字符
			before := i == 0 || !isIdentByte(text[i-1])
			after := i+wl == n || !isIdentByte(text[i+wl])
			if before && after {
				return i, true
			}
		}
	}
	_ = lower
	return 0, false
}

// refreshImporters 在文档变更后刷新所有引用方的诊断。
//
// 文档（通常是 .global）内容变化会影响引用方的跳转目标与（T5 的）
// 来源诊断；LSP 诊断是全量替换，因此对每个引用方重跑完整的发布流程。
// 递归安全：这里只重发诊断，不触发其它文档的 refreshImporters。
func (s *Server) refreshImporters(ctx context.Context, uri string) {
	if s.globalsRes == nil {
		return
	}
	// 已打开文档走内存版：loadIndex 的 openDoc 分支总是重新解析，
	// 缓存里的旧条目不会被内存版本污染；Refresh 仍调用以清理
	// fromDoc=false 的条目（文档此前未被打开过的情况）。
	s.globalsRes.RefreshURI(uri)
	path := globals.PathOfURI(uri)
	importers := s.globalsRes.Importers(path)
	s.logger.Debug("refreshImporters", "uri", uri, "path", path, "importers", len(importers))

	for _, docURI := range importers {
		if docURI == uri {
			continue // 自己引用自己：诊断由本文档的常规流程发布
		}
		doc := s.docs.Get(docURI)
		if doc == nil {
			continue
		}
		// 引用方未打开时无法发布诊断（LSP 只对打开的文档发）。
		// 引用方的 T5 依赖目标符号，必须重算（走 reparseFull）。
		s.clearSourceDiags(docURI)
		s.reparseFull(ctx, doc)
	}
}

// ---------------------------------------------------------------------------
// P3a T5：变量来源未找到检查（F4）
// ---------------------------------------------------------------------------

// variableUse 是一次需要核对来源的变量使用。
type variableUse struct {
	word       string
	lower      string
	start, end uint
}

// 变量使用收集 query：多个 pattern 合一个，一次扫描全树。
//
//	pattern 1 `(variable (identifier) @use)` 只捕获 variable 的直接
//	identifier（首标识符）——rec.field 的字段侧是 object 的 property，
//	不在捕获范围（保守规则：字段来自记录结构或 schema）；
//	pattern 2 跳过 call.name（`CALL foo()` 的函数名）；
//	pattern 3 跳过 expression.name（`IF fn() THEN` / `LET x = fn()` 中的函数名，
//		grammar 把 `_function_expression` 解析为 expression name: 字段，不是 call 节点）；
//	pattern 4 跳过 whenever 下的 variable（`WHENEVER ERROR CALL f` /
//		`... GOTO lbl` 的目标是函数名或标号，不是变量）。**这里不能写
//		`(whenever name: ...)`**：name 字段来自隐藏规则 `_names` 的别名，
//		node-types.json 里 whenever 没有这个字段，query 会编译失败并报
//		"Impossible pattern"——而失败是静默的（整个 T5 被禁用，零诊断）；
//	pattern 5-10 跳过表单字段上下文（P3b 联动修正）：INPUT ... FROM f、
//	CONSTRUCT ... ON f、DISPLAY ... TO f、NEXT FIELD f、INFIELD(f) 里的
//	名字是**表单字段**不是程序变量——T5 把它们当变量查会全部误报
//	（实测：INPUT x FROM f01 的 f01 被 variable-source-unresolved 命中）。
//
// 性能实测（7138 行 / 329KB 文件）：
//   - TreeCursor 逐节点 DFS：didChange 0.7ms → 74ms（每次 CGO 调用太贵）；
//   - 两个独立 query 各扫一遍全树：25ms（query 是全树扫描，match
//     多少不影响代价，两个 query 就付两遍）；
//   - 合并为一个 query（本方案）：13ms。
//
// 逐一核对 node-types.json 后确认 name 字段含 variable 的节点**仅 call**：
// form/window/report/label 的 name 都是 identifier 直接节点，pattern 1
// 本来就不会匹配它们——写成 pattern 反而是 Impossible pattern，
// 整个 query 编译失败、T5 被静默禁用（实测踩过）。
const queryVariables = `
(variable (identifier) @use)
(call name: (variable (identifier) @skip))
(expression name: (variable (identifier) @skip))
(whenever (variable (identifier) @skip))
(input field: (variable) @skip)
(construct field: (variable) @skip)
(display field: (variable) @skip)
(hook field: (variable) @skip)
(infield_function params: (expression (variable (identifier) @skip)))
`

// queryFunctionCalls 专门收集函数调用点，用于 function-not-found 诊断。
//
//	(call name: (variable (identifier) @fn))   // CALL foo() / foo() 作为 call 节点
//	(expression name: (variable (identifier) @fn))  // IF fn() / LET x = fn() 中的函数名
//
// 注意：这里不跳过任何内容，只收集。误报由调用方按符号表过滤（内置函数、
// 跨模块函数等）。
// functionCalls 返回函数调用点。query 在 internal/index，与链接资料导入
// （P7-D-2 的外部函数统计）共用同一份，避免两处 query 漂移。
// callCollector 返回（必要时创建）复用的调用点收集器；不可用时返回 nil。
func (s *Server) callCollector() *index.CallCollector {
	s.fnCallOnce.Do(func() {
		c, err := index.NewCallCollector(s.pool)
		if err != nil {
			s.logger.Error("编译函数调用 query 失败", "err", err)
			return
		}
		s.fnCallCollector = c
	})
	return s.fnCallCollector
}

func (s *Server) functionCalls(tree *ts.Tree, src []byte) []index.Call {
	c := s.callCollector()
	if c == nil {
		return nil
	}
	return c.Calls(tree, src)
}

// variableQueries 是惰性编译并复用的 query（Query 只读可共享）。
type variableQueries struct {
	query     *ts.Query
	useIndex  uint // @use 的 capture 索引
	skipIndex uint // @skip 的 capture 索引
}

// compileVariableQueries 按 BDL 语言编译 query；失败时返回 nil 与错误。
//
// 返回错误而不是静默 nil 是必要的：query 里一个 "Impossible pattern"
// 就会让整个 T5 失效（零诊断），而“没有诊断”和“诊断关掉了”在编辑器里
// 长得一模一样。调用方必须把 err 记进日志。
func compileVariableQueries(pool *parser.Pool) (*variableQueries, error) {
	lang, err := pool.Language(parser.LangBDL)
	if err != nil {
		return nil, fmt.Errorf("取 BDL 语言: %w", err)
	}
	// 注意 err 与下面的 qerr 必须分开声明：
	// err 是 error 接口，而 ts.NewQuery 返回 *QueryError（具体类型）。
	// 若复用同一个 err，编译成功的 nil *QueryError 装进接口后
	// `err != nil` 为 true（typed-nil 陷阱），整个功能会被静默禁用
	// （实测踩过：单独测试全绿、集成后静默失效，极难定位）。
	q, qerr := ts.NewQuery(lang, queryVariables)
	if qerr != nil {
		return nil, fmt.Errorf("编译变量使用 query: %w", qerr)
	}
	useIdx, ok1 := q.CaptureIndexForName("use")
	skipIdx, ok2 := q.CaptureIndexForName("skip")
	if !ok1 || !ok2 {
		q.Close()
		return nil, fmt.Errorf("变量使用 query 缺少 @use/@skip 捕获")
	}
	return &variableQueries{query: q, useIndex: useIdx, skipIndex: skipIdx}, nil
}

// Close 释放 query。
func (v *variableQueries) Close() {
	if v == nil || v.query == nil {
		return
	}
	v.query.Close()
}

// collectVariableUses 用 tree-sitter query 一次扫描收集变量使用。
//
// 保守规则（规划 §5.3：宁可不报，不误报）：
//   - 只取 variable 节点的首标识符；
//   - 跳过 call 的 name 字段（函数名）；
//   - builtin 节点不是 variable，天然排除。
//
// skip 与 use 是同一节点（区间相等），用起始字节判断重叠。
func collectVariableUses(tree *ts.Tree, src []byte, qs *variableQueries) []variableUse {
	if tree == nil || qs == nil {
		return nil
	}
	root := tree.RootNode()
	if root == nil {
		return nil
	}

	type captured struct {
		node *ts.Node
		skip bool
	}
	var all []captured
	qc := ts.NewQueryCursor()
	defer qc.Close()
	matches := qc.Matches(qs.query, root, src)
	for m := matches.Next(); m != nil; m = matches.Next() {
		for _, c := range m.Captures {
			node := c.Node
			all = append(all, captured{
				node: &node,
				skip: c.Index == uint32(qs.skipIndex),
			})
		}
	}

	skipRanges := make(map[uint]bool, len(all))
	for _, c := range all {
		if c.skip {
			skipRanges[c.node.StartByte()] = true
		}
	}

	var uses []variableUse
	for _, c := range all {
		if c.skip || skipRanges[c.node.StartByte()] {
			continue
		}
		if word := parser.TextOf(c.node, src); word != "" {
			uses = append(uses, variableUse{
				word:  word,
				lower: strings.ToLower(word),
				start: c.node.StartByte(),
				end:   c.node.EndByte(),
			})
		}
	}
	return uses
}

// unresolvedSourceChecks 检查变量使用是否有任何可见定义（F4）。
//
// 查找集合（任一命中即放过）：
//   - 本文档全部符号（任何作用域——粗化处理：只要求"文档里有定义"，
//     不做作用域精确判定，避免把跨函数可见性误判为未定义）；
//   - GLOBALS 引入的符号（所有 Resolved 目标）。
//
// 两种措辞（同一 code，便于统一过滤）：
//
//	A. 所有导入都 Resolved：局部/模块级/GLOBALS 均无定义；
//	B. 存在未解析导入：Globals 无法核对来源。
//
// 存在 Unresolvable 导入（文件名是变量）时**全部放过**：
// 那些导入里的定义静态不可知，任何"未定义"结论都可能是误报。
func (s *Server) unresolvedSourceChecks(doc *document.Document, fi *index.FileIndex) []protocol.Diagnostic {
	if fi == nil {
		return nil
	}
	_, tree, _ := doc.Snapshot()
	if tree == nil {
		return nil
	}

	// query 惰性编译（首次使用时）。编译失败必须记日志：静默失败会让
	// T5 从此零诊断，与“用户关掉了”无法区分（踩坑 #22）。
	s.varQueriesOnce.Do(func() {
		var qerr error
		s.varQueries, qerr = compileVariableQueries(s.pool)
		if qerr != nil {
			s.logger.Error("变量使用 query 编译失败，变量来源诊断已禁用", "error", qerr)
		}
	})
	if s.varQueries == nil {
		return nil
	}

	uses := collectVariableUses(tree, doc.Text(), s.varQueries)
	if len(uses) == 0 {
		return nil
	}

	// 已知符号集合：本文档 + globals 引入
	known := map[string]bool{}
	for _, sym := range fi.Symbols {
		known[sym.Lower] = true
	}
	// 只看**当前文档**的 GLOBALS 引入（见 globalsDefinitions 的注释）：
	// 借用别的文档的导入会让"变量来源"诊断随开着哪些文件而变。
	entries := s.globalsEntriesOf(doc.URI)

	hasUnresolvable := false
	hasNotFound := false
	var notFoundLiteral string
	for _, e := range entries {
		switch e.Status {
		case globals.StatusUnresolvable:
			hasUnresolvable = true
		case globals.StatusNotFound:
			hasNotFound = true
			if notFoundLiteral == "" {
				notFoundLiteral = e.Import.Literal
			}
		case globals.StatusResolved:
			if e.Index != nil {
				for _, sym := range e.Index.Symbols {
					known[sym.Lower] = true
				}
			}
		}
	}
	// 存在静态无法核对的导入：全部放过（宁可不报）
	if hasUnresolvable {
		return nil
	}

	settings := s.settings()
	severity, enabled := diag.SeverityFor(diag.CodeVariableSourceUnresolved, settings)
	if !enabled {
		s.storeSourceDiags(doc.URI, nil) // off：缓存也清掉
		return nil
	}

	idx := doc.Index()
	var diags []protocol.Diagnostic
	for _, u := range uses {
		if known[u.lower] {
			continue
		}
		// P7-C：内置变量/函数/类名在语法树里就是普通 identifier，静态上
		// 永远找不到定义。实测它们占全量诊断的 98%，必须豁免。
		// 用户显式 disable 掉的名字不豁免（否则没法把内置名当普通标识符用）。
		if s.isBuiltinName(u.word) && !s.builtinDisabled(u.word) {
			continue
		}
		msg := fmt.Sprintf("无法确定变量 %q 的来源：局部、模块级与 GLOBALS 引入中均无定义", u.word)
		if hasNotFound {
			msg = fmt.Sprintf("无法确定变量 %q 的来源：GLOBALS %q 未找到", u.word, notFoundLiteral)
		}
		diags = append(diags, protocol.Diagnostic{
			Range:    byteRange(idx, u.start, u.end),
			Severity: severity,
			Source:   "bdl-lsp",
			Message:  msg,
			Code:     string(diag.CodeVariableSourceUnresolved),
		})
	}
	s.storeSourceDiags(doc.URI, diags)
	return diags
}

// functionNotFoundChecks 检查函数调用处的函数名是否在当前模块及可用模组
// 中有定义，找不到时报告 function-not-found。
//
// 可见集：
//
//	本文件 ∪ GLOBALS 引入 ∪ 链接文件 ∪ 全局模组(sub/lib/qry)
//
// 变量不走这条路（R2：链接只放大函数可见性）。
//
// **没有链接资料时整项不启用**：可见集不完整时报告就是误报——P7-B 实测
// 24 个文件开着一开就 3660 条（内置函数 + 跨模块函数全都"找不到"）。
// 同理，索引还在后台预热（complete=false）时也放弃本轮判定，
// 就绪后由 rediagnoseAll 补发一次。
func (s *Server) functionNotFoundChecks(doc *document.Document, fi *index.FileIndex) []protocol.Diagnostic {
	if fi == nil {
		return nil
	}
	// 级别先判：默认 warning；关掉时连 query 都不该编译（全树扫描很贵）。
	severity, enabled := diag.SeverityFor(diag.CodeFunctionNotFound, s.settings())
	if !enabled {
		return nil
	}
	// P7-D 前置条件：没有链接资料就没有"可见函数集"，无从判定。
	if s.linkResolver() == nil {
		return nil
	}

	_, tree, _ := doc.Snapshot()
	if tree == nil {
		return nil
	}

	calls := s.functionCalls(tree, doc.Text())
	if len(calls) == 0 {
		return nil
	}

	// 已知函数定义：本文档的 Function/Report/Main
	knownFn := map[string]bool{}
	for _, sym := range fi.Symbols {
		switch sym.Kind {
		case index.KindFunction, index.KindReport, index.KindMain:
			knownFn[sym.Lower] = true
		}
	}
	// GLOBALS 引入的符号里的函数定义也算已知。
	// 只看**当前文档**的导入（见 globalsDefinitions 的注释）。
	for _, e := range s.globalsEntriesOf(doc.URI) {
		if e.Status != globals.StatusResolved || e.Index == nil {
			continue
		}
		for _, sym := range e.Index.Symbols {
			switch sym.Kind {
			case index.KindFunction, index.KindReport:
				knownFn[sym.Lower] = true
			}
		}
	}

	// P7-D：并进链接可见集（本文件 ∪ GLOBALS 之外的部分）。
	// complete=false → 放弃本轮，不冒险误报。
	hasMain := false
	for _, sym := range fi.Symbols {
		if sym.Kind == index.KindMain {
			hasMain = true
			break
		}
	}
	if visible, complete := s.visibleLinkFunctions(filePathOf(doc.URI), hasMain); complete {
		for name := range visible {
			knownFn[name] = true
		}
	} else {
		return nil
	}

	idx := doc.Index()
	if idx == nil {
		return nil
	}

	var diags []protocol.Diagnostic
	seen := map[string]bool{} // 同一函数名同一位置只报一次
	for _, c := range calls {
		key := fmt.Sprintf("%s@%d", c.Lower, c.Start)
		if seen[key] {
			continue
		}
		seen[key] = true
		if knownFn[c.Lower] {
			continue
		}
		// P7-C：内置函数（ARG_VAL/LENGTH/fgl_* …）豁免。
		// 只认 KindFunction：类名（ui）与方法名（open）在调用点上的形态
		// 与函数不同，交给 P7-D 的链接索引处理，这里不越权。
		if s.isBuiltinFunction(c.Name) && !s.builtinDisabled(c.Name) {
			continue
		}
		diags = append(diags, protocol.Diagnostic{
			Range:    byteRange(idx, c.Start, c.End),
			Severity: severity,
			Source:   "bdl-lsp",
			Message:  fmt.Sprintf("函数 %q 在当前模块及可用模组中找不到定义", c.Name),
			Code:     string(diag.CodeFunctionNotFound),
		})
	}
	return diags
}

// cachedSourceDiags 返回文档缓存的 T5 诊断（可能为空；不重算）。
func (s *Server) cachedSourceDiags(uri string) []protocol.Diagnostic {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	return s.sourceDiags[uri]
}

// storeSourceDiags 写入 T5 诊断缓存。
func (s *Server) storeSourceDiags(uri string, diags []protocol.Diagnostic) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	s.sourceDiags[uri] = diags
}

// clearSourceDiags 清空缓存并取消挂起的重算定时器。
func (s *Server) clearSourceDiags(uri string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	delete(s.sourceDiags, uri)
	if t, ok := s.recheckTimers[uri]; ok {
		t.Stop()
		delete(s.recheckTimers, uri)
	}
}

// scheduleSourceRecheck 防抖重算：编辑停顿 400ms 后全量重发（含 T5）。
//
// 为什么不在 didChange 现场算：来源检查是全树 query，7138 行文件实测
// ~27ms，挂在每次按键上会让输入明显卡顿（基线 0.7ms）。清空缓存 +
// 防抖是语言服务器的常规做法：打字时警告短暂消失，停顿后恢复。
func (s *Server) scheduleSourceRecheck(uri string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	if t, ok := s.recheckTimers[uri]; ok {
		t.Stop()
	}
	s.recheckTimers[uri] = time.AfterFunc(400*time.Millisecond, func() {
		s.debounceMu.Lock()
		delete(s.recheckTimers, uri)
		s.debounceMu.Unlock()

		// 服务器已退出：不再发布（timer 可能活得比连接久，
		// 向已关闭的流写会卡死清理流程——测试里实测挂起）。
		select {
		case <-s.exited:
			return
		default:
		}

		doc := s.docs.Get(uri)
		if doc == nil {
			return
		}
		// 与 handle 共用 parseMu：reparseFull 会 SetTree（关闭旧树），
		// 不能和正在处理请求的 query 并行（见 handle 注释）。
		s.parseMu.Lock()
		defer s.parseMu.Unlock()
		// 拿锁期间文档可能被关闭：释放后再取一次。
		if doc = s.docs.Get(uri); doc == nil {
			return
		}
		s.reparseFull(context.Background(), doc)
	})
}
