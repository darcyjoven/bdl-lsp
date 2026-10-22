package server

import (
	"sync"
	"unicode"
	"unicode/utf8"

	"github.com/darcyjoven/bdl-lsp/index"
	"github.com/darcyjoven/bdl-lsp/internal/document"
	"github.com/darcyjoven/bdl-lsp/internal/textpos"
	"github.com/darcyjoven/bdl-lsp/parser"
)

// indexCache 缓存每个文档的符号索引，按 (URI, 版本) 失效。
//
// 符号提取比解析便宜得多，但对大文件仍有可观开销（遍历全部节点），
// 而文档符号/悬停/跳转都是高频请求，因此必须缓存。
type indexCache struct {
	mu      sync.RWMutex
	entries map[string]*indexEntry
}

type indexEntry struct {
	version int32
	fi      *index.FileIndex
	// idx 与 fi 同时建立，二者都对应同一个版本的文本。
	idx *textpos.Index
}

func newIndexCache() *indexCache {
	return &indexCache{entries: make(map[string]*indexEntry)}
}

// get 返回缓存的索引；版本不匹配时返回 nil。
func (c *indexCache) get(uri string, version int32) *indexEntry {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e := c.entries[uri]
	if e == nil || e.version != version {
		return nil
	}
	return e
}

func (c *indexCache) put(uri string, version int32, fi *index.FileIndex, idx *textpos.Index) {
	c.mu.Lock()
	c.entries[uri] = &indexEntry{version: version, fi: fi, idx: idx}
	c.mu.Unlock()
}

func (c *indexCache) drop(uri string) {
	c.mu.Lock()
	delete(c.entries, uri)
	c.mu.Unlock()
}

func (c *indexCache) dropAll() {
	c.mu.Lock()
	c.entries = make(map[string]*indexEntry)
	c.mu.Unlock()
}

// indexFor 返回文档的符号索引与行索引，必要时重建。
func (s *Server) indexFor(doc *document.Document) (*index.FileIndex, *textpos.Index) {
	text, tree, version := doc.Snapshot()
	if tree == nil {
		return nil, nil
	}

	if e := s.indexes.get(doc.URI, version); e != nil {
		return e.fi, e.idx
	}

	idx := textpos.New(text)
	var fi *index.FileIndex
	if doc.Lang == parser.LangPer {
		fi = index.BuildPer(tree, text, version)
	} else {
		fi = index.Build(doc.Lang, tree, text, version)
	}
	fi.URI = doc.URI

	s.indexes.put(doc.URI, version, fi, idx)
	return fi, idx
}

// identAt 返回指定位置上的标识符文本；不在标识符上时返回空串。
//
// 实现说明：不依赖语法树定位 identifier 节点，而是直接从文本按标识符
// 字符向两侧扩展。原因：
//   - 语法树里的 name/identifier 节点层次不稳定（field_ref/object 包裹差异大）；
//   - 光标常停在标识符边界上，节点查询容易落到父节点；
//   - 文本扩展对注释、字符串内的词同样有效，行为更可预期。
func (s *Server) identAt(doc *document.Document, idx *textpos.Index, line, char int) string {
	text, _, _ := doc.Snapshot()
	if len(text) == 0 {
		return ""
	}
	offset := idx.Offset(line, char)
	return wordAt(text, offset)
}

// wordAt 从源码中按“标识符字符”向两侧扩展，得到光标所在的词。
//
// 用于悬停/跳转：不依赖语法树里的 identifier 节点位置，
// 光标落在标识符边界、注释或字符串内部时也能得到合理的词。
func wordAt(text []byte, offset int) string {
	if offset < 0 || offset > len(text) {
		return ""
	}
	start := offset
	for start > 0 {
		r, size := utf8.DecodeLastRune(text[:start])
		if !isIdentRune(r) {
			break
		}
		start -= size
	}
	end := offset
	for end < len(text) {
		r, size := utf8.DecodeRune(text[end:])
		if !isIdentRune(r) {
			break
		}
		end += size
	}
	if start == end {
		return ""
	}
	return string(text[start:end])
}

// isIdentRune 判断字符是否可出现在 Genero 标识符中。
//
// Genero 标识符：字母、数字、下划线；为支持中文变量名，非 ASCII 字母也算。
func isIdentRune(r rune) bool {
	if r == '_' {
		return true
	}
	if r < utf8.RuneSelf {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r)
}
