// Package document 维护打开文档的文本状态与语法树。
//
// 关键设计：
//   - 文本用 []byte 保存，配合 textpos.Index 做 UTF-16 ↔ 字节偏移换算；
//   - 每次内容变更都先把旧语法树 Tree.Edit 到新坐标，再交给 Parser 增量重解析；
//   - 所有位置换算集中在 textpos，本包不重复实现。
package document

import (
	"fmt"
	"sync"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	ts "github.com/tree-sitter/go-tree-sitter"
)

// Change 是一次文本编辑。坐标与 LSP 一致：0 基行号 + UTF-16 列。
//
// Full 为 true 时表示整文替换，此时忽略区间字段。
type Change struct {
	Full      bool
	StartLine int
	StartCol  int
	EndLine   int
	EndCol    int
	Text      string
}

// Document 是一份已打开文件的当前状态。
type Document struct {
	URI     string
	Lang    parser.Lang
	Version int32

	mu    sync.RWMutex
	text  []byte
	index *textpos.Index
	tree  *ts.Tree
}

// New 构造文档。text 会被复制，调用方之后修改原切片不影响文档。
func New(uri string, lang parser.Lang, version int32, text string) *Document {
	buf := []byte(text)
	return &Document{
		URI:     uri,
		Lang:    lang,
		Version: version,
		text:    buf,
		index:   textpos.New(buf),
	}
}

// Snapshot 返回当前文本、语法树与版本。返回的切片只读，不要在调用方修改。
func (d *Document) Snapshot() (text []byte, tree *ts.Tree, version int32) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.text, d.tree, d.Version
}

// Text 返回当前文本（只读）。
func (d *Document) Text() []byte {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.text
}

// Index 返回当前行索引（只读）。
func (d *Document) Index() *textpos.Index {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.index
}

// LineCount 返回当前行数。
func (d *Document) LineCount() int {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.index.LineCount()
}

// SetTree 替换语法树并释放旧树。
func (d *Document) SetTree(tree *ts.Tree) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tree != nil && d.tree != tree {
		d.tree.Close()
	}
	d.tree = tree
}

// ReleaseTree 释放语法树但不改动文本（用于关闭前）。
func (d *Document) ReleaseTree() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.tree != nil {
		d.tree.Close()
		d.tree = nil
	}
}

// Close 释放文档资源。
func (d *Document) Close() { d.ReleaseTree() }

// ApplyChanges 按顺序应用一批编辑，并把旧语法树同步到新坐标。
//
// 返回的 tree 是已 Edit 的旧树，可直接传给 Parser.Parse 做增量重解析；
// 无旧树时返回 nil（退化为全量解析）。
//
// 应用过程中文档文本会被就地更新，因此即使后续解析失败，文本也保持一致。
func (d *Document) ApplyChanges(version int32, changes []Change) (*ts.Tree, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, ch := range changes {
		if err := d.applyLocked(ch); err != nil {
			return d.tree, fmt.Errorf("第 %d 个编辑: %w", i+1, err)
		}
	}
	d.Version = version
	return d.tree, nil
}

// applyLocked 应用单个编辑；调用方必须持有写锁。
func (d *Document) applyLocked(ch Change) error {
	if ch.Full {
		d.text = []byte(ch.Text)
		d.index = textpos.New(d.text)
		// 整文替换时坐标全部失效，直接丢弃旧树最安全。
		if d.tree != nil {
			d.tree.Close()
			d.tree = nil
		}
		return nil
	}

	start := d.index.Offset(ch.StartLine, ch.StartCol)
	end := d.index.Offset(ch.EndLine, ch.EndCol)
	if start > end {
		return fmt.Errorf("编辑区间非法：start=%d > end=%d", start, end)
	}
	if end > len(d.text) {
		return fmt.Errorf("编辑区间越界：end=%d > 文本长度 %d", end, len(d.text))
	}

	startRow, startCol := d.index.Point(start)
	oldEndRow, oldEndCol := d.index.Point(end)

	newText := []byte(ch.Text)
	newEndByte := start + len(newText)
	newEndRow, newEndCol := textpos.Advance(startRow, startCol, ch.Text)

	// 先改文本，再改树：树的位置以字节/point 描述，与文本内容无关。
	merged := make([]byte, 0, len(d.text)-(end-start)+len(newText))
	merged = append(merged, d.text[:start]...)
	merged = append(merged, newText...)
	merged = append(merged, d.text[end:]...)
	d.text = merged
	d.index = textpos.New(d.text)

	if d.tree != nil {
		d.tree.Edit(&ts.InputEdit{
			StartByte:      uint(start),
			OldEndByte:     uint(end),
			NewEndByte:     uint(newEndByte),
			StartPosition:  ts.NewPoint(uint(startRow), uint(startCol)),
			OldEndPosition: ts.NewPoint(uint(oldEndRow), uint(oldEndCol)),
			NewEndPosition: ts.NewPoint(uint(newEndRow), uint(newEndCol)),
		})
	}
	return nil
}

// Store 保存所有打开的文档。并发安全。
type Store struct {
	mu   sync.RWMutex
	docs map[string]*Document
}

// NewStore 创建空文档仓库。
func NewStore() *Store {
	return &Store{docs: make(map[string]*Document)}
}

// Open 记录一份新打开的文档；同 URI 的旧文档会被关闭。
func (s *Store) Open(uri string, lang parser.Lang, version int32, text string) *Document {
	doc := New(uri, lang, version, text)

	s.mu.Lock()
	old := s.docs[uri]
	s.docs[uri] = doc
	s.mu.Unlock()

	if old != nil {
		old.Close()
	}
	return doc
}

// Get 返回文档；不存在时返回 nil。
func (s *Store) Get(uri string) *Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.docs[uri]
}

// All 返回当前所有文档的快照。
func (s *Store) All() []*Document {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Document, 0, len(s.docs))
	for _, doc := range s.docs {
		out = append(out, doc)
	}
	return out
}

// Len 返回打开的文档数量。
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.docs)
}

// Close 移除并释放文档；返回被移除的文档（可能为 nil）。
func (s *Store) Close(uri string) *Document {
	s.mu.Lock()
	doc := s.docs[uri]
	delete(s.docs, uri)
	s.mu.Unlock()

	if doc != nil {
		doc.Close()
	}
	return doc
}

// CloseAll 释放所有文档。
func (s *Store) CloseAll() {
	s.mu.Lock()
	docs := s.docs
	s.docs = make(map[string]*Document)
	s.mu.Unlock()

	for _, doc := range docs {
		doc.Close()
	}
}
