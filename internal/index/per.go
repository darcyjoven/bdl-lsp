package index

import (
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
)

// BuildPer 从 .per 语法树提取表单符号。
//
// 提取内容：
//   - ATTRIBUTES 中的每个 item（editor/checkbox/group...），名称为 item tag；
//   - INSTRUCTIONS 中的 SCREEN RECORD 及其字段引用。
//
// 不提取 LAYOUT 中的 item_tag：它只表示“布局引用了某 item”，
// 真正的定义在 ATTRIBUTES；布局引用校验属于 P3。
func BuildPer(tree *ts.Tree, src []byte, version int32) *FileIndex {
	fi := &FileIndex{Lang: parser.LangPer, Version: version}
	if tree == nil {
		return fi
	}
	root := tree.RootNode()
	if root == nil {
		return fi
	}

	b := &builder{src: src, fi: fi, cursor: tree.Walk()}
	defer b.cursor.Close()

	count := root.NamedChildCount()
	for i := uint(0); i < count; i++ {
		child := root.NamedChild(i)
		if child == nil {
			continue
		}
		switch child.Kind() {
		case "attributes":
			b.collectPerItems(child)
		case "instructions":
			b.collectScreenRecords(child)
		case "layout":
			b.collectLayoutRefs(child)
		}
	}
	return fi
}

// collectLayoutRefs 收集 LAYOUT 段里的 item tag 引用（P3b 内部校验用）。
//
// item_tag 只出现在 area（GRID 等容器的文本块）里，节点文本含方括号，
// 剥掉括号取名字。布局 tag 是“引用”——定义在 ATTRIBUTES；
// 引用未定义的 tag 会在内部校验里报告。
func (b *builder) collectLayoutRefs(layout *ts.Node) {
	// 用 cursor DFS 找全部 item_tag（嵌套在 layout_section/grid/area 下）
	cursor := b.cursor
	cursor.Reset(*layout)
	for {
		node := cursor.Node()
		if node.Kind() == "item_tag" {
			b.addLayoutCell(node)
		}
		if cursor.GotoFirstChild() {
			continue
		}
		for {
			if cursor.GotoNextSibling() {
				break
			}
			if !cursor.GotoParent() {
				return
			}
			// 回到 layout 本身之上就结束
			if cursor.Node().Id() == layout.Id() {
				return
			}
		}
	}
}

// stripBrackets 去掉布局 tag 两侧的方括号（[f01] → f01）。
func stripBrackets(s string) string {
	if len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']' {
		return s[1 : len(s)-1]
	}
	return s
}

// addLayoutCell 把一个 item_tag 节点拆成若干 tag 引用。
//
// **一个 item_tag 里可能有多个 tag**：布局单元格的写法是 `[a |b ]`，
// 一个方括号里并排放多个 item（语料里非常常见：
// `[label1     |alh01                ]`）。旧实现把整段内容当成一个 tag
// 去查 ATTRIBUTES，永远查不到，于是每个单元格都报 form-layout-tag-undefined
// ——实测 tiptop 语料 40167 条，占全部诊断 12%。
func (b *builder) addLayoutCell(node *ts.Node) {
	text := b.text(node)
	// splitLayoutTags 的相对偏移已含方括号的 +1，这里直接加上节点起点
	for _, seg := range splitLayoutTags(stripBrackets(text), hasBrackets(text)) {
		tag := seg.text
		if tag == "" {
			continue
		}
		start := node.StartByte() + seg.start
		b.fi.LayoutRefs = append(b.fi.LayoutRefs, LayoutRef{
			Tag:       tag,
			Lower:     strings.ToLower(tag),
			StartByte: start,
			EndByte:   start + uint(len(tag)),
		})
	}
}

// hasBrackets 报告文本是否被方括号包住。
func hasBrackets(s string) bool {
	return len(s) >= 2 && s[0] == '[' && s[len(s)-1] == ']'
}

// layoutTagSpan 是一个 tag 在单元格内容里的相对位置。
type layoutTagSpan struct {
	text       string
	start, end uint
}

// splitLayoutTags 按 `|` 切分单元格内容，去两侧空白，返回每段的文本与
// 相对字节区间（相对 inner 起点）。
func splitLayoutTags(inner string, bracketed bool) []layoutTagSpan {
	base := uint(0)
	if bracketed {
		base = 1 // 跳过 '['
	}
	var out []layoutTagSpan
	start := 0
	for i := 0; i <= len(inner); i++ {
		if i < len(inner) && inner[i] != '|' {
			continue
		}
		seg := inner[start:i]
		rel := start
		for len(seg) > 0 && (seg[0] == ' ' || seg[0] == '\t') {
			seg = seg[1:]
			rel++
		}
		for len(seg) > 0 && (seg[len(seg)-1] == ' ' || seg[len(seg)-1] == '\t') {
			seg = seg[:len(seg)-1]
		}
		if seg != "" {
			out = append(out, layoutTagSpan{text: seg, start: base + uint(rel), end: base + uint(rel+len(seg))})
		}
		start = i + 1
	}
	return out
}

// collectPerItems 遍历 ATTRIBUTES 小节。
func (b *builder) collectPerItems(attrs *ts.Node) {
	count := attrs.NamedChildCount()
	for i := uint(0); i < count; i++ {
		item := attrs.NamedChild(i)
		if item == nil {
			continue
		}
		switch item.Kind() {
		case "attribute_item", "untyped_attribute_item":
			b.addPerItem(item)
		case "phantom_item":
			b.addPhantomItem(item)
		}
	}
}

// addPerItem 为一个普通 item 建符号。
//
// tag 是 item 名（1..n），name 是 screen record 中的字段名（可选）。
func (b *builder) addPerItem(item *ts.Node) {
	tagNode := item.ChildByFieldName("tag")
	if tagNode == nil {
		return
	}
	name := b.text(tagNode)
	if name == "" {
		return
	}

	sym := &Symbol{
		Name:      name,
		Lower:     strings.ToLower(name),
		Kind:      KindFormItem,
		Scope:     ScopeGlobal,
		StartByte: tagNode.StartByte(),
		EndByte:   tagNode.EndByte(),
		DeclStart: item.StartByte(),
		DeclEnd:   item.EndByte(),
	}
	if it := findChild(item, "item_type"); it != nil {
		sym.TypeText = b.text(it)
	}

	// 记录字段名：SCREEN RECORD 引用的是 `= formonly.more` / `= alh_file.alh01`
	// 的**列部分**，不是 item tag。`Edit mor=formonly.more` 里两者不同，
	// 只认 tag 会把合法的 SCREEN RECORD 报成未定义（实测 4850 条）。
	if fr := item.ChildByFieldName("field"); fr != nil {
		if col := fr.ChildByFieldName("column"); col != nil {
			if fieldName := b.text(col); fieldName != "" {
				b.fi.FormFieldNames = append(b.fi.FormFieldNames, LayoutRef{
					Tag:       fieldName,
					Lower:     strings.ToLower(fieldName),
					StartByte: col.StartByte(),
					EndByte:   col.EndByte(),
				})
			}
		}
	}

	b.fi.Symbols = append(b.fi.Symbols, sym)
	b.fi.TopLevel = append(b.fi.TopLevel, sym)
}

// addPhantomItem 为 PHANTOM 字段建符号。
func (b *builder) addPhantomItem(item *ts.Node) {
	// phantom_item: seq(PHANTOM, field_ref, type_clause?, ";")
	// 名字在 field_ref 的 column 字段里（node-types 实测：无 name/identifier 子节点）。
	var nameNode *ts.Node
	if fr := findChild(item, "field_ref"); fr != nil {
		nameNode = fr.ChildByFieldName("column")
		if nameNode == nil {
			nameNode = findChild(fr, "name")
		}
	}
	if nameNode == nil {
		return
	}
	name := b.text(nameNode)
	if name == "" {
		return
	}
	sym := &Symbol{
		Name:      name,
		Lower:     strings.ToLower(name),
		Kind:      KindFormItem,
		Scope:     ScopeGlobal,
		StartByte: nameNode.StartByte(),
		EndByte:   nameNode.EndByte(),
		DeclStart: item.StartByte(),
		DeclEnd:   item.EndByte(),
		TypeText:  "PHANTOM",
	}
	b.fi.Symbols = append(b.fi.Symbols, sym)
	b.fi.TopLevel = append(b.fi.TopLevel, sym)
}

// collectScreenRecords 遍历 INSTRUCTIONS 小节。
func (b *builder) collectScreenRecords(inst *ts.Node) {
	count := inst.NamedChildCount()
	for i := uint(0); i < count; i++ {
		entry := inst.NamedChild(i)
		if entry == nil || entry.Kind() != "screen_record_entry" {
			continue
		}
		b.addScreenRecord(entry)
	}
}

// addScreenRecord 为 SCREEN RECORD 建符号，字段作为子符号。
func (b *builder) addScreenRecord(entry *ts.Node) {
	nameNode := entry.ChildByFieldName("name")
	if nameNode == nil {
		return
	}
	name := b.text(nameNode)
	if name == "" {
		return
	}

	sym := &Symbol{
		Name:      name,
		Lower:     strings.ToLower(name),
		Kind:      KindScreenRec,
		Scope:     ScopeGlobal,
		StartByte: nameNode.StartByte(),
		EndByte:   nameNode.EndByte(),
		DeclStart: entry.StartByte(),
		DeclEnd:   entry.EndByte(),
	}

	count := entry.NamedChildCount()
	for i := uint(0); i < count; i++ {
		child := entry.NamedChild(i)
		if child == nil || child.Kind() != "field_ref" {
			continue
		}
		// field_ref 形如 table.column 或裸字段名（SCREEN RECORD sr(sr01)）。
		// column 部分**有字段名**；裸名是名为 "name" 的**节点类型**
		// （parse 实测：无字段前缀，ChildByFieldName 取不到，要用 findChild）。
		col := child.ChildByFieldName("column")
		if col == nil {
			col = findChild(child, "name")
		}
		if col == nil {
			continue
		}
		colName := b.text(col)
		if colName == "" {
			continue
		}
		field := &Symbol{
			Name:      colName,
			Lower:     strings.ToLower(colName),
			Kind:      KindField,
			Scope:     ScopeFunc,
			Parent:    name,
			Container: name,
			StartByte: col.StartByte(),
			EndByte:   col.EndByte(),
			DeclStart: child.StartByte(),
			DeclEnd:   child.EndByte(),
		}
		sym.Children = append(sym.Children, field)
		b.fi.Symbols = append(b.fi.Symbols, field)
	}

	b.fi.Symbols = append(b.fi.Symbols, sym)
	b.fi.TopLevel = append(b.fi.TopLevel, sym)
}
