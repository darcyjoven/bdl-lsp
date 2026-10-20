// codeaction.go 处理 textDocument/codeAction（P6）。
//
// 只提供**真正安全**的修复。判断标准：一个修复如果在多数情况下会生成
// 语义错误或语法错误的代码，就不做——一键自动修复产生坏代码比看到报错更糟。
//
// 当前提供：
//   - 字段名拼错 → "改为 %q"（quickfix）。候选来自光标处表单的真实字段
//     （复用 P3b 解析器），按编辑距离挑最接近的，超过阈值就不提。
//   - `.per` 布局 tag / SCREEN RECORD 引用不存在 → 同样的"改为 %q"。
//
// 刻意**不**提供（理由记在这里，避免以后被当成遗漏补上）：
//   - "在表单里自动添加该 item"：LAYOUT 列宽、以及 ATTRIBUTES 里该写
//     `formonly.x` 还是 `schema.table.column` 都无法可靠推断，
//     生成的 .per 大概率语法/语义错误。
//   - "创建缺失的 .per 文件"：空表单没有 item，等于把编译错误换成运行时错误。
//   - 删除/注释掉字段引用：破坏性操作，不该一键完成。
package server

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/form"
	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"go.lsp.dev/jsonrpc2"
	"go.lsp.dev/protocol"
)

// 诊断码（与 internal/form 及 .per 内部校验的码一致）。
const (
	codeFieldNotInForm    = "field-not-in-form"
	codePerLayoutTagUndef = "form-layout-tag-undefined"
	codePerScreenRecUndef = "form-screen-record-undefined"
)

// maxSuggestDistance 是"建议改名"允许的最大编辑距离。
//
// 太大时建议没意义（把 `tot_amt` 改成 `cust_name` 纯属噪声），太小又抓不到
// 真实拼写错误。用 len/3 且夹在 1..4：`good_filed` → `good_field` 距离 2
// 命中；`tot_amt` → `cust_name` 距离远超上限，不提。
func maxSuggestDistance(word string) int {
	d := len(word) / 3
	if d < 1 {
		d = 1
	}
	if d > 4 {
		d = 4
	}
	return d
}

// handleCodeAction 处理"代码操作"。
func (s *Server) handleCodeAction(ctx context.Context, reply jsonrpc2.Replier, req jsonrpc2.Request) error {
	var params protocol.CodeActionParams
	if err := json.Unmarshal(req.Params(), &params); err != nil {
		return reply(ctx, nil, err)
	}

	doc := s.docs.Get(string(params.TextDocument.URI))
	if doc == nil {
		return reply(ctx, nil, nil)
	}

	// 只给本服务器发出的诊断提供修复（source 必须匹配），否则可能给
	// 部署编译错误之类的外部诊断生成不合适的动作。
	var out []protocol.CodeAction
	for i := range params.Context.Diagnostics {
		d := &params.Context.Diagnostics[i]
		if d.Source != "" && d.Source != "bdl-lsp" {
			continue
		}
		out = append(out, s.actionsForDiagnostic(doc, d)...)
	}
	if len(out) == 0 {
		return reply(ctx, nil, nil)
	}
	return reply(ctx, out, nil)
}

// actionsForDiagnostic 返回某条诊断可用的修复。
func (s *Server) actionsForDiagnostic(doc *document.Document, d *protocol.Diagnostic) []protocol.CodeAction {
	switch diagCode(d) {
	case codeFieldNotInForm:
		field := textInRange(doc, d.Range)
		if field == "" {
			return nil
		}
		return didYouMeanActions(doc, *d, field, s.fieldCandidatesAt(doc, d.Range.Start), "字段")

	case codePerLayoutTagUndef, codePerScreenRecUndef:
		name := textInRange(doc, d.Range)
		if name == "" {
			return nil
		}
		return didYouMeanActions(doc, *d, name, s.perItemNames(doc), "item")

	default:
		// form-duplicate-item 等没有安全修复：删掉是破坏性的，合并语义不明。
		return nil
	}
}

// diagCode 取诊断码字符串（Code 是 any，可能是 string 或其他类型）。
func diagCode(d *protocol.Diagnostic) string {
	switch v := d.Code.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprintf("%v", v)
	}
}

// didYouMeanActions 生成"改为 X"的 quickfix（最多 3 个候选，首个 preferred）。
func didYouMeanActions(doc *document.Document, d protocol.Diagnostic,
	word string, cands []string, what string) []protocol.CodeAction {

	suggestions := closestMatches(word, cands, 3)
	if len(suggestions) == 0 {
		return nil
	}

	actions := make([]protocol.CodeAction, 0, len(suggestions))
	for i, sug := range suggestions {
		actions = append(actions, protocol.CodeAction{
			Title: fmt.Sprintf("改为 %q（最接近的%s）", sug, what),
			Kind:  protocol.QuickFix,
			// 只把第一条标 preferred：多条同时"推荐"会让编辑器的
			// auto-fix 无从选择。
			IsPreferred: i == 0,
			Diagnostics: []protocol.Diagnostic{d},
			Edit: &protocol.WorkspaceEdit{
				Changes: map[protocol.DocumentURI][]protocol.TextEdit{
					protocol.DocumentURI(doc.URI): {{
						Range:   d.Range,
						NewText: sug,
					}},
				},
			},
		})
	}
	return actions
}

// fieldCandidatesAt 返回指定位置可用表单的字段名。
func (s *Server) fieldCandidatesAt(doc *document.Document, pos protocol.Position) []string {
	if s.formRes == nil || doc == nil || doc.Lang != parser.LangBDL {
		return nil
	}
	text, tree, _ := doc.Snapshot()
	if tree == nil {
		return nil
	}
	if s.formQs == nil {
		s.formQs = form.CompileQueries(s.pool)
	}
	if s.formQs == nil {
		return nil
	}
	offset := doc.Index().Offset(int(pos.Line), int(pos.Character))
	if offset < 0 {
		return nil
	}
	fields, _ := s.formRes.FormFieldsAt(tree, text, form.PathOfURI(doc.URI), s.formQs, uint(offset))
	return fields
}

// perItemNames 返回 .per 文件里已定义的 item 名。
func (s *Server) perItemNames(doc *document.Document) []string {
	fi, _ := s.indexFor(doc)
	if fi == nil {
		return nil
	}
	var out []string
	for _, sym := range fi.Symbols {
		if sym.Kind == index.KindFormItem && sym.Name != "" {
			out = append(out, sym.Name)
		}
	}
	return out
}

// textInRange 取诊断范围覆盖的文本（去掉首尾空白）。
func textInRange(doc *document.Document, rng protocol.Range) string {
	text, _, _ := doc.Snapshot()
	idx := doc.Index()
	start := idx.Offset(int(rng.Start.Line), int(rng.Start.Character))
	end := idx.Offset(int(rng.End.Line), int(rng.End.Character))
	if start < 0 || end > len(text) || start >= end {
		return ""
	}
	return strings.TrimSpace(string(text[start:end]))
}

// closestMatches 返回按编辑距离升序的候选（超过阈值的丢弃）。
//
// 距离为 0（仅大小写不同）不算"拼错"，直接跳过：Genero 标识符大小写不敏感，
// `GOOD_FIELD` 与 `good_field` 是同一个字段，报错本身就不该发生。
func closestMatches(word string, cands []string, limit int) []string {
	lower := strings.ToLower(word)
	maxDist := maxSuggestDistance(word)

	type scored struct {
		name string
		dist int
	}
	var hits []scored
	seen := map[string]bool{}
	for _, c := range cands {
		cl := strings.ToLower(c)
		if cl == lower || seen[cl] {
			continue
		}
		d := editDistance(lower, cl)
		if d > maxDist {
			continue
		}
		seen[cl] = true
		hits = append(hits, scored{name: c, dist: d})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].dist != hits[j].dist {
			return hits[i].dist < hits[j].dist
		}
		return strings.ToLower(hits[i].name) < strings.ToLower(hits[j].name)
	})
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]string, 0, len(hits))
	for _, h := range hits {
		out = append(out, h.name)
	}
	return out
}

// editDistance 计算 Levenshtein 距离（插入/删除/替换，代价均为 1）。
//
// 标准 DP + 单行滚动数组。字段名都很短（< 40 字符），性能不成问题。
func editDistance(a, b string) int {
	if a == b {
		return 0
	}
	la, lb := len(a), len(b)
	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}
	prev := make([]int, lb+1)
	cur := make([]int, lb+1)
	for j := 0; j <= lb; j++ {
		prev[j] = j
	}
	for i := 1; i <= la; i++ {
		cur[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if a[i-1] == b[j-1] {
				cost = 0
			}
			cur[j] = min3(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[lb]
}

// min3 返回三个整数的最小值。
func min3(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}
