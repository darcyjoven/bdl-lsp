// form.go 把表单解析器接进诊断流（P3b）。
//
// 数据流（与 globals.go 平行）：
//
//	4gl didOpen/didChange → reparseFull（语法 + globals + T5 + 字段校验）
//	                     → formChecks（query 提取绑定与引用 → 校验）
//
//	.per didOpen/didChange → perInternalChecks（重复 item/布局 tag/SCREEN RECORD）
//	                      → refreshFormImporters：引用该 .per 的 4gl 重算
//
// 性能：字段校验是全树 query（单 query 多 pattern），与 T5 同价位；
// 沿用缓存+防抖策略（didChange 清缓存，停顿 400ms 后 reparseFull 恢复）。
package server

import (
	"context"
	"fmt"
	"strings"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/internal/diag"
	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/form"
	"github.com/darcyjoven/bdl-lsp/parser"
	"go.lsp.dev/protocol"
)

// formQueriesAlias 是 form 包 queries 类型的别名（Server 字段引用用；
// 编译失败时为 nil，功能静默禁用）。
type formQueriesAlias = form.Queries

// newFormResolver 在 initialize 时创建表单解析器（perRoots 来自配置）。
func (s *Server) newFormResolver(rootPath string) {
	s.formRes = form.NewResolver(s.pool, rootPath, s.settings().PerRoots, func(uri string) ([]byte, bool) {
		if doc := s.docs.Get(uri); doc != nil {
			return doc.Text(), true
		}
		return nil, false
	})
}

// formChecks 在 4gl 文档上执行字段校验并把诊断追加到 issues。
//
// 返回值是**本文档引用到的表单路径**（供 T5 兜底过滤"表单侧名字"用，
// 见 unresolvedSourceChecks 的 formNames 参数）。
//
// 副作用：登记 doc → .per 的引用边（供 .per 变更时刷新）。
// 配置 off 时仍执行解析与登记——relatedInformation 与后续能力依赖解析结果，
// 只是诊断被关（与 globals 的策略一致）。
func (s *Server) formChecks(doc *document.Document, issues *[]protocol.Diagnostic) []string {
	if s.formRes == nil || doc == nil || doc.Lang != parser.LangBDL {
		return nil
	}

	// query 惰性编译（编译失败则本功能静默禁用）
	if s.formQs == nil {
		s.formQs = form.CompileQueries(s.pool)
	}
	if s.formQs == nil {
		return nil
	}

	text, tree, _ := doc.Snapshot()
	if tree == nil {
		return nil
	}

	// 清旧边再登记（引用可能已变）
	s.formRes.ClearEdges(doc.URI)

	fieldIssues, usedPaths := s.formRes.Check(tree, text, form.PathOfURI(doc.URI), s.formQs)

	// 登记引用边（无问题的场合也要——.per 变更时才能刷新本文档）
	for _, p := range usedPaths {
		s.formRes.AddEdge(doc.URI, p)
	}

	severity, enabled := diag.SeverityFor(diag.CodeFieldNotInForm, s.settings())
	if !enabled {
		return usedPaths
	}

	idx := doc.Index()
	ign := s.ignoreMatcher()
	var diags []protocol.Diagnostic
	for _, iss := range fieldIssues {
		// P8：按表单/字段忽略。表单忽略覆盖三类字段类诊断；
		// 字段忽略只针对 field-not-in-form（另外两类没有字段名）。
		if ign.Form(iss.Literal, iss.FormPath) {
			continue
		}
		if iss.Kind == string(diag.CodeFieldNotInForm) && ign.Field(iss.Field) {
			continue
		}
		d := protocol.Diagnostic{
			Range:    byteRange(idx, iss.Start, iss.End),
			Severity: severity,
			Source:   "bdl-lsp",
			Message:  iss.Message,
			Code:     iss.Kind,
		}
		// relatedInformation 指向表单文件（P3b 规划：可跳转到表单）
		if iss.FormURI != "" {
			d.RelatedInformation = []protocol.DiagnosticRelatedInformation{{
				Location: protocol.Location{
					URI: protocol.DocumentURI(iss.FormURI),
					Range: protocol.Range{
						Start: protocol.Position{Line: 0, Character: 0},
						End:   protocol.Position{Line: 0, Character: 0},
					},
				},
				Message: fmt.Sprintf("表单 %q", iss.Literal),
			}}
		}
		diags = append(diags, d)
	}

	// 写缓存（off 时清空缓存与 T5 一致）
	if !enabled {
		diags = nil
	}
	s.storeFieldDiags(doc.URI, diags)
	*issues = append(*issues, diags...)
	return usedPaths
}

// cachedFieldDiags 返回缓存的字段/表单诊断（didChange 快速路径用）。
func (s *Server) cachedFieldDiags(uri string) []protocol.Diagnostic {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	return s.fieldDiags[uri]
}

// storeFieldDiags 写入字段诊断缓存。
func (s *Server) storeFieldDiags(uri string, diags []protocol.Diagnostic) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	s.fieldDiags[uri] = diags
}

// clearFieldDiags 清空字段诊断缓存（didOpen/didClose 用）。
func (s *Server) clearFieldDiags(uri string) {
	s.debounceMu.Lock()
	defer s.debounceMu.Unlock()
	delete(s.fieldDiags, uri)
}

// perInternalChecks 执行 .per 文件的内部一致性校验（T6）。
//
// 检查项（全部是 .per 单文件内可判定的）：
//  1. 重复 item tag（ATTRIBUTES 里同名 item 定义两次）；
//  2. 布局 tag 未定义（LAYOUT 的 [tag] 在 ATTRIBUTES 里没有）；
//  3. SCREEN RECORD 引用不存在的 item。
//
// 级别：三个码各自可被 `bdl.diagnostics.<code>` 覆盖，缺省用注册表默认。
func (s *Server) perInternalChecks(doc *document.Document, fi *index.FileIndex) []protocol.Diagnostic {
	if doc == nil || fi == nil || doc.Lang != parser.LangPer {
		return nil
	}

	settings := s.settings()
	dupSev, dupOn := diag.SeverityFor(diag.CodeFormDuplicateItem, settings)
	tagSev, tagOn := diag.SeverityFor(diag.CodeFormLayoutTagUndefined, settings)
	scrSev, scrOn := diag.SeverityFor(diag.CodeFormScreenRecordUndefined, settings)
	if !dupOn && !tagOn && !scrOn {
		return nil // 三条全关：连索引都省了
	}

	idx := doc.Index()
	var diags []protocol.Diagnostic

	// 1) 重复 item tag
	seen := map[string]bool{}
	for _, sym := range fi.Symbols {
		if sym.Kind != index.KindFormItem {
			continue
		}
		if seen[sym.Lower] {
			diags = append(diags, protocol.Diagnostic{
				Range:    byteRange(idx, sym.StartByte, sym.EndByte),
				Severity: dupSev,
				Source:   "bdl-lsp",
				Message:  fmt.Sprintf("重复的表单字段 %q（ATTRIBUTES 中已定义）", sym.Name),
				Code:     string(diag.CodeFormDuplicateItem),
			})
		}
		seen[sym.Lower] = true
	}

	// 2) 布局 tag 未定义（LAYOUT 的 [tag] 不在 ATTRIBUTES）
	//
	// defined 只收 KindFormItem（ATTRIBUTES 的定义）。不能把 SCREEN RECORD
	// 的字段（KindField）也算进去——那是“引用”不是定义，算进去会让
	// 检查 3 永远不触发（SCREEN RECORD 引用自身字段即视为已定义，循环）。
	defined := map[string]bool{}
	for _, sym := range fi.Symbols {
		if sym.Kind == index.KindFormItem {
			defined[sym.Lower] = true
		}
	}
	// 字段名也算“已定义”：SCREEN RECORD 引用的是 `= formonly.more` 的列名，
	// 不是 item tag（`Edit mor=formonly.more` 里两者不同）。
	for _, f := range fi.FormFieldNames {
		defined[f.Lower] = true
	}
	if tagOn {
		for _, ref := range fi.LayoutRefs {
			if defined[ref.Lower] {
				continue
			}
			// 含点号的 tag（如 table.column 形式）跳过：列来自数据库，
			// 静态无法判定（宁可不报不误报）
			if strings.ContainsAny(ref.Tag, ".") {
				continue
			}
			diags = append(diags, protocol.Diagnostic{
				Range:    byteRange(idx, ref.StartByte, ref.EndByte),
				Severity: tagSev,
				Source:   "bdl-lsp",
				Message:  fmt.Sprintf("布局 tag %q 未在 ATTRIBUTES 中定义", ref.Tag),
				Code:     string(diag.CodeFormLayoutTagUndefined),
			})
		}
	}

	// 3) SCREEN RECORD 引用不存在的 item
	if scrOn {
		for _, sym := range fi.Symbols {
			if sym.Kind != index.KindScreenRec {
				continue
			}
			for _, child := range sym.Children {
				if defined[child.Lower] {
					continue
				}
				if strings.ContainsAny(child.Name, ".") {
					continue // table.column 形式：列来自数据库，跳过
				}
				diags = append(diags, protocol.Diagnostic{
					Range:    byteRange(idx, child.StartByte, child.EndByte),
					Severity: scrSev,
					Source:   "bdl-lsp",
					Message: fmt.Sprintf("SCREEN RECORD %q 引用的字段 %q 未在 ATTRIBUTES 中定义",
						sym.Name, child.Name),
					Code: string(diag.CodeFormScreenRecordUndefined),
				})
			}
		}
	}

	return diags
}

// refreshFormImporters 在 .per 文档变更后刷新引用方（4gl）的诊断。
func (s *Server) refreshFormImporters(ctx context.Context, uri string) {
	if s.formRes == nil {
		return
	}
	s.formRes.RefreshURI(uri)
	path := form.PathOfURI(uri)

	for _, docURI := range s.formRes.Importers(path) {
		if docURI == uri {
			continue
		}
		doc := s.docs.Get(docURI)
		if doc == nil {
			continue
		}
		s.clearSourceDiags(docURI)
		s.reparseFull(ctx, doc)
	}
}

// perChecks 包装 perInternalChecks 供 reparseFull 调用（追加到 issues）。
func (s *Server) perChecks(doc *document.Document, issues *[]protocol.Diagnostic) {
	if doc == nil || doc.Lang != parser.LangPer {
		return
	}
	fi, _ := s.indexFor(doc)
	if fi == nil {
		return
	}
	*issues = append(*issues, s.perInternalChecks(doc, fi)...)
}
