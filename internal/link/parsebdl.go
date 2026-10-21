package link

import (
	"fmt"
	"os"

	"github.com/darcyjoven/bdl-lsp/internal/index"
	"github.com/darcyjoven/bdl-lsp/internal/parser"
)

// NewBDLParseFunc 返回"从 .4gl 提取可调用函数名"的真实实现。
//
// 只收 `KindFunction`：
//   - `KindReport` 不是用 CALL 调用的（`START REPORT`），收进来也不行；
//   - `KindMain` 更不可能被显式调用。
//
// 大小写不敏感（Genero 本身如此），统一返回小写。
func NewBDLParseFunc(pool *parser.Pool) ParseFunc {
	return func(path string) ([]string, error) {
		fi, err := parseFile(pool, path)
		if err != nil {
			return nil, err
		}
		out := make([]string, 0, 8)
		for _, sym := range fi.Symbols {
			if sym.Kind == index.KindFunction {
				out = append(out, sym.Lower)
			}
		}
		return out, nil
	}
}

// NewBDLScanner 返回"扫描某个 (模组,代码) 文件"的真实实现。
//
// 这是导入阶段最贵的一步：语料有 7765 个不同代码，每个都要读+解析
// （实测 8.7 ms/文件），所以 ImportCSV 会并行调用它。既然已经解析过，
// 就顺手把"本文件定义的函数"带出来——它能把外部函数的候选集缩小
// （定义过的名字不可能是"外部函数"）。
func NewBDLScanner(layout *Layout, pool *parser.Pool) *BDLScanner {
	return &BDLScanner{layout: layout, pool: pool}
}

// BDLScanner 是 Scanner 的真实实现。
type BDLScanner struct {
	layout *Layout
	pool   *parser.Pool
}

// Scan 实现 Scanner。
func (b *BDLScanner) Scan(module, code string) FileScan {
	path, ok := b.layout.Path(module, code)
	if !ok {
		// 磁盘上没这个文件：实测 800 条，全在语料里不存在的 W* 模组。
		return FileScan{}
	}
	src, err := ReadSource(path)
	if err != nil {
		return FileScan{Found: true} // 文件在但读不了：当"存在但无 MAIN"，不污染缺失统计
	}
	tree, err := b.pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		return FileScan{Found: true}
	}
	defer tree.Close()

	out := FileScan{Found: true}
	if fi := index.Build(parser.LangBDL, tree, src, 1); fi != nil {
		for _, sym := range fi.Symbols {
			switch sym.Kind {
			case index.KindMain:
				out.HasMain = true
			case index.KindFunction:
				out.Defines = append(out.Defines, sym.Lower)
			}
		}
	}
	// 语法树没看到 MAIN 时再扫一次文本兜底（`{ MAIN ... }` 之类）。
	// 注意 `{ }` 在语法里是块注释，ScanMainText 与之一致。
	if !out.HasMain && ScanMainText(src) {
		out.HasMain = true
	}
	return out
}

// parseFile 解析一个 .4gl 并建索引。
func parseFile(pool *parser.Pool, path string) (*index.FileIndex, error) {
	src, err := ReadSource(path)
	if err != nil {
		return nil, err
	}
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		return nil, err
	}
	defer tree.Close()
	fi := index.Build(parser.LangBDL, tree, src, 1)
	if fi == nil {
		return nil, fmt.Errorf("%s: 建索引返回 nil", path)
	}
	return fi, nil
}

// HasMainInSource 报告一份源码文本是否含 MAIN（不需要文件存在）。
//
// CLI 的 `link import --from-lsp` 之类场景用不上，但 server 侧判断
// "当前文档有没有 MAIN"用的是 index 里已有的索引，不走这里。
func HasMainInSource(pool *parser.Pool, src []byte) (bool, error) {
	tree, err := pool.Parse(parser.LangBDL, src, nil)
	if err != nil {
		return false, err
	}
	defer tree.Close()
	fi := index.Build(parser.LangBDL, tree, src, 1)
	for _, sym := range fi.Symbols {
		if sym.Kind == index.KindMain {
			return true, nil
		}
	}
	return false, nil
}

// FileExists 报告路径是否是一个可读的普通文件。
func FileExists(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir()
}
