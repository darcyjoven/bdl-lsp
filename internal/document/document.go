// Package document 维护打开文档的文本状态。
//
// 第一阶段（P1 Block 1）采用“整文替换”策略：didChange 时用新文本覆盖。
// Block 2 会在此基础上实现增量编辑与 UTF-16 行列映射。
package document

import (
	"sync"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
	ts "github.com/tree-sitter/go-tree-sitter"
)

// Document 是一份已打开文件的当前状态。
type Document struct {
	URI      string
	Lang     parser.Lang
	Version  int32
	Text     []byte
	Tree     *ts.Tree
	Lines    *LineIndex
	mu       sync.RWMutex
	changeID int
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

// Open 记录一份新打开的文档。
func (s *Store) Open(uri string, lang parser.Lang, version int32, text string) *Document {
	doc := &Document{
		URI:     uri,
		Lang:    lang,
		Version: version,
		Text:    []byte(text),
		Lines:   NewLineIndex(text),
	}
	s.mu.Lock()
	if old := s.docs[uri]; old != nil {
		old.Close()
	}
	s.docs[uri] = doc
	s.mu.Unlock()
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

// Close 移除并释放文档；返回被移除的文档（可能为 nil）。
func (s *Store) Close(uri string) *Document {
	s.mu.Lock()
	defer s.mu.Unlock()
	doc := s.docs[uri]
	if doc != nil {
		delete(s.docs, uri)
		doc.Close()
	}
	return doc
}

// CloseAll 释放所有文档。
func (s *Store) CloseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for uri, doc := range s.docs {
		doc.Close()
		delete(s.docs, uri)
	}
}

// Update 用整文替换更新文档内容，调用方需重新解析。
func (d *Document) Update(version int32, text string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.Version = version
	d.Text = []byte(text)
	d.Lines = NewLineIndex(text)
	d.changeID++
}

// SetTree 替换语法树，释放旧树。
func (d *Document) SetTree(tree *ts.Tree) {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Tree != nil {
		d.Tree.Close()
	}
	d.Tree = tree
}

// Snapshot 返回文本与语法树的只读快照。
func (d *Document) Snapshot() (text []byte, tree *ts.Tree, version int32) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.Text, d.Tree, d.Version
}

// Close 释放文档持有的语法树。
func (d *Document) Close() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.Tree != nil {
		d.Tree.Close()
		d.Tree = nil
	}
}

// LineIndex 记录每行的起始字节偏移，用于行列 ↔ 偏移转换。
//
// 注意：这里只做字节级转换；LSP 的 character 字段是 UTF-16 码元，
// Block 2 会补上非 ASCII 字符的换算。
type LineIndex struct {
	starts []int
}

// NewLineIndex 扫描文本，记录每行起始偏移。
func NewLineIndex(text string) *LineIndex {
	starts := make([]int, 0, 64)
	starts = append(starts, 0)
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return &LineIndex{starts: starts}
}

// LineCount 返回行数。
func (li *LineIndex) LineCount() int { return len(li.starts) }

// Offset 把 0 基行号与行内字节列转换为绝对字节偏移。
func (li *LineIndex) Offset(line, col int) int {
	if line < 0 {
		return 0
	}
	if line >= len(li.starts) {
		return li.starts[len(li.starts)-1]
	}
	return li.starts[line] + col
}

// LineCol 把绝对字节偏移转换为 0 基行号与行内字节列。
func (li *LineIndex) LineCol(offset int) (line, col int) {
	if offset < 0 {
		return 0, 0
	}
	lo, hi := 0, len(li.starts)-1
	for lo < hi {
		mid := (lo + hi + 1) / 2
		if li.starts[mid] <= offset {
			lo = mid
		} else {
			hi = mid - 1
		}
	}
	return lo, offset - li.starts[lo]
}
