// Package parser 封装 tree-sitter 的解析器池与语言绑定。
//
// 设计要点：
//   - 两个 grammar（bdl / per）通过 Go 绑定以 CGO 静态链接进二进制；
//   - Parser 不是并发安全的，用 sync.Pool 复用；
//   - Language 只需构造一次，进程内共享；
//   - grammar ABI 必须是 15，因此 go-tree-sitter 固定 v0.25.0（v0.24 只支持到 14）。
package parser

import (
	"fmt"
	"sync"

	bdl "github.com/darcyjoven/tree-sitter-bdl/bindings/go"
	per "github.com/darcyjoven/tree-sitter-per/bindings/go"
	ts "github.com/tree-sitter/go-tree-sitter"
)

// Lang 表示受支持的语言。
type Lang string

const (
	// LangBDL 对应 .4gl / .global。
	LangBDL Lang = "bdl"
	// LangPer 对应 .per。
	LangPer Lang = "per"
)

// LangForPath 依据文件扩展名判断语言。未知扩展名返回 false。
func LangForPath(path string) (Lang, bool) {
	switch ext(path) {
	case ".4gl", ".global":
		return LangBDL, true
	case ".per":
		return LangPer, true
	default:
		return "", false
	}
}

func ext(path string) string {
	for i := len(path) - 1; i >= 0; i-- {
		switch path[i] {
		case '.':
			return lower(path[i:])
		case '/', '\\':
			return ""
		}
	}
	return ""
}

func lower(s string) string {
	b := []byte(s)
	for i := range b {
		if b[i] >= 'A' && b[i] <= 'Z' {
			b[i] += 'a' - 'A'
		}
	}
	return string(b)
}

// Pool 是并发安全的解析器池。
type Pool struct {
	bdlLang *ts.Language
	perLang *ts.Language
	pool    sync.Pool
}

// New 构造解析器池，并校验两个 grammar 的 ABI。
func New() (*Pool, error) {
	bdlLang := ts.NewLanguage(bdl.Language())
	perLang := ts.NewLanguage(per.Language())

	// ABI 不匹配时尽早失败，避免运行期 panic。
	for name, lang := range map[string]*ts.Language{"bdl": bdlLang, "per": perLang} {
		if v := lang.AbiVersion(); v != 15 {
			return nil, fmt.Errorf("grammar %s ABI=%d，期望 15（请使用 go-tree-sitter v0.25.0）", name, v)
		}
	}

	p := &Pool{bdlLang: bdlLang, perLang: perLang}
	p.pool.New = func() any {
		return ts.NewParser()
	}
	return p, nil
}

// Close 释放池中缓存的所有 Parser。
func (p *Pool) Close() {
	for {
		v := p.pool.Get()
		if v == nil {
			return
		}
		if parser, ok := v.(*ts.Parser); ok {
			parser.Close()
		}
		// sync.Pool 没有“取空”语义；用固定次数退出避免死循环。
		if p.pool.New == nil {
			return
		}
		p.pool = sync.Pool{New: p.pool.New}
		return
	}
}

// Language 返回指定语言的 tree-sitter Language。
func (p *Pool) Language(lang Lang) (*ts.Language, error) {
	switch lang {
	case LangBDL:
		return p.bdlLang, nil
	case LangPer:
		return p.perLang, nil
	default:
		return nil, fmt.Errorf("未知语言 %q", lang)
	}
}

// Parse 用池中的 Parser 解析源码。返回的 Tree 由调用方负责 Close。
func (p *Pool) Parse(lang Lang, src []byte, oldTree *ts.Tree) (*ts.Tree, error) {
	target, err := p.Language(lang)
	if err != nil {
		return nil, err
	}

	parser, _ := p.pool.Get().(*ts.Parser)
	if parser == nil {
		return nil, fmt.Errorf("解析器池状态异常")
	}
	defer p.pool.Put(parser)

	if err := parser.SetLanguage(target); err != nil {
		return nil, fmt.Errorf("设置语言 %s: %w", lang, err)
	}
	tree := parser.Parse(src, oldTree)
	if tree == nil {
		return nil, fmt.Errorf("解析 %s 返回空语法树", lang)
	}
	return tree, nil
}
