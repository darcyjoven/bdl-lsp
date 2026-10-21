package index

import (
	"strings"
	"sync"

	"github.com/darcyjoven/bdl-lsp/internal/parser"
	sitter "github.com/tree-sitter/go-tree-sitter"
)

// FunctionCallsQuery 是"函数调用点"的 query 源。
//
// 两种形态都要：
//   - `CALL foo(…)`      → `(call name: …)`
//   - `IF foo() THEN`    → `(expression name: …)`
//
// 第二条是 P7-B 的教训：只抓 `call` 节点会漏掉所有"当成表达式用"的函数
// （`ARG_VAL(1)`、`FGL_GETENV(…)` 大量出现在条件里）。
const FunctionCallsQuery = `
(call name: (variable (identifier) @fn))
(expression name: (variable (identifier) @fn))
`

// CallCollector 复用同一个 query 收集函数调用名。
//
// Cursor 每次新建（tree-sitter 的 cursor 不可并发复用），query 本身只读，
// 因此内部用一把锁保证"编译一次、多处安全使用"。锁的代价相对解析
// （实测 8.7 ms/文件）可以忽略。
type CallCollector struct {
	mu     sync.Mutex
	query  *sitter.Query
	fnIdx  uint
	broken bool
}

// NewCallCollector 编译 query。失败时返回 error，调用方应显式记录：
// query 编译失败是**静默失效**那一类 bug（P7-B 坑 #22）。
func NewCallCollector(pool *parser.Pool) (*CallCollector, error) {
	lang, err := pool.Language(parser.LangBDL)
	if err != nil {
		return nil, err
	}
	q, qerr := sitter.NewQuery(lang, FunctionCallsQuery)
	if qerr != nil {
		return nil, qerr
	}
	fnIdx, ok := q.CaptureIndexForName("fn")
	if !ok {
		q.Close()
		return nil, errNoFnCapture
	}
	return &CallCollector{query: q, fnIdx: fnIdx}, nil
}

// Close 释放 query。
func (c *CallCollector) Close() {
	if c == nil || c.query == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.query.Close()
	c.query = nil
	c.broken = true
}

// Broken 报告是否已释放/不可用。
func (c *CallCollector) Broken() bool {
	if c == nil {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.broken || c.query == nil
}

// Call 是一个函数调用点。
type Call struct {
	Name  string // 源码里的写法（保留大小写）
	Lower string // 小写，用于查表
	Start uint
	End   uint
}

// Calls 返回源码里全部函数调用点（按出现顺序）。
func (c *CallCollector) Calls(tree *sitter.Tree, src []byte) []Call {
	if c == nil || tree == nil {
		return nil
	}
	root := tree.RootNode()
	if root == nil {
		return nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.broken || c.query == nil {
		return nil
	}

	var out []Call
	qc := sitter.NewQueryCursor()
	defer qc.Close()
	matches := qc.Matches(c.query, root, src)
	for m := matches.Next(); m != nil; m = matches.Next() {
		for _, cap := range m.Captures {
			if cap.Index != uint32(c.fnIdx) {
				continue
			}
			node := cap.Node
			word := parser.TextOf(&node, src)
			if word == "" {
				continue
			}
			out = append(out, Call{
				Name:  word,
				Lower: strings.ToLower(word),
				Start: node.StartByte(),
				End:   node.EndByte(),
			})
		}
	}
	return out
}

// Names 返回源码里被调用的函数名（小写，已去重，顺序稳定）。
func (c *CallCollector) Names(tree *sitter.Tree, src []byte) []string {
	calls := c.Calls(tree, src)
	if len(calls) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(calls))
	out := make([]string, 0, len(calls))
	for _, call := range calls {
		if seen[call.Lower] {
			continue
		}
		seen[call.Lower] = true
		out = append(out, call.Lower)
	}
	return out
}
