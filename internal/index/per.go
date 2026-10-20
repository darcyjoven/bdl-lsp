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
		}
	}
	return fi
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

	b.fi.Symbols = append(b.fi.Symbols, sym)
	b.fi.TopLevel = append(b.fi.TopLevel, sym)
}

// addPhantomItem 为 PHANTOM 字段建符号。
func (b *builder) addPhantomItem(item *ts.Node) {
	// PHANTOM 节点的名字通常在第 2 个 identifier 上；退化为取第一个 name。
	nameNode := findChild(item, "name")
	if nameNode == nil {
		nameNode = findChild(item, "identifier")
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
		// field_ref 形如 table.column；符号名用 column 部分。
		col := child.ChildByFieldName("column")
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
