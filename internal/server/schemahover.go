package server

// 悬停里的"字段资料"扩展（PLUGIN-PLAN P2-1）。
//
// 三类场景：
//   - `DEFINE x LIKE ima_file.ima01` → 悬停 x 追加：`ima_file.ima01 · CHAR(10) · 物料编号`；
//   - `DEFINE r RECORD LIKE ima_file.*` → 悬停 r 附上该表**全部字段**（超过 20 行折叠）；
//   - 直接悬停表名（`ima_file`）→ 给出这张表与字段摘要。
//
// 一律"没有资料就不追加"（宁可不报）：字段资料缺失/表不存在/单列找不到都不报错。

import (
	"fmt"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/schema"
	"go.lsp.dev/protocol"
)

// schemaHoverMaxRows 是整表悬停最多列几行（超出折叠）。
const schemaHoverMaxRows = 20

// schemaAppendixForSymbol 依据声明类型文本生成悬停追加段（无资料时返回空串）。
func (s *Server) schemaAppendixForSymbol(typeText string) string {
	if strings.TrimSpace(typeText) == "" || s.schemaIndex() == nil {
		return ""
	}
	// 整表展开优先（`LIKE t.*`）：列出全部字段。
	if tb, ok := s.schemaColumnsForType(typeText); ok && tb != nil && len(tb.Columns) > 0 {
		var sb strings.Builder
		fmt.Fprintf(&sb, "\n\n---\n**%s**", tb.Name)
		if tb.Desc != "" {
			sb.WriteString(" · " + tb.Desc)
		}
		fmt.Fprintf(&sb, " · %d 个字段（字段资料）\n", len(tb.Columns))
		show := tb.Columns
		if len(show) > schemaHoverMaxRows {
			show = show[:schemaHoverMaxRows]
		}
		for _, c := range show {
			fmt.Fprintf(&sb, "\n- %s", c.HoverLine())
		}
		if len(tb.Columns) > len(show) {
			fmt.Fprintf(&sb, "\n- … 其余 %d 个字段见「数据库字段」视图", len(tb.Columns)-len(show))
		}
		return sb.String()
	}
	// 单列声明（`LIKE t.col`）：只追加那一列的说明。
	if c, table, ok := s.schemaColumnForType(typeText); ok {
		return fmt.Sprintf("\n\n---\n字段资料：**%s.%s** · %s",
			table, c.Name, strings.TrimPrefix(c.HoverLine(), c.Name+" · "))
	}
	return ""
}

// schemaHoverForTableName 悬停表名（源码里出现 `t.col` / `LIKE t.*` 的表）时给表摘要。
//
// 只在符号解析完全落空时调用：用户自己定义的符号永远优先。
func (s *Server) schemaHoverForTableName(word string) *protocol.Hover {
	ix := s.schemaIndex()
	if ix == nil || word == "" {
		return nil
	}
	tb, ok := ix.Table(word)
	if !ok || tb == nil {
		return nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "**%s**", tb.Name)
	if tb.Desc != "" {
		sb.WriteString(" · " + tb.Desc)
	}
	fmt.Fprintf(&sb, "\n\n数据库表 · %d 个字段（字段资料）", len(tb.Columns))
	show := tb.Columns
	if len(show) > schemaHoverMaxRows {
		show = show[:schemaHoverMaxRows]
	}
	for _, c := range show {
		fmt.Fprintf(&sb, "\n\n- %s", c.HoverLine())
	}
	if len(tb.Columns) > len(show) {
		fmt.Fprintf(&sb, "\n\n… 其余 %d 个字段见「数据库字段」视图", len(tb.Columns)-len(show))
	}
	return &protocol.Hover{Contents: protocol.MarkupContent{Kind: protocol.Markdown, Value: sb.String()}}
}

// schemaColumnHoverLines 供视图/其他能力复用：把列转成展示行。
func schemaColumnHoverLines(cols []schema.Column, max int) []string {
	if max <= 0 || max > len(cols) {
		max = len(cols)
	}
	out := make([]string, 0, max)
	for _, c := range cols[:max] {
		out = append(out, c.HoverLine())
	}
	return out
}
