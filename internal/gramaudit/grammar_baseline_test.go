package gramaudit

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/darcyjoven/bdl-lsp/parser"
)

// TestCorpusGrammarBaseline 断言真实语料里出错的文件没有变多。
//
// 为什么要钉住：改 grammar 最容易出的事故是"修一个坏两个"，而单测样例
// 覆盖不到 15108 个文件里的写法。这里允许文件变少（修好了），一旦出现
// 名单外的新文件就失败，并列出清单——这也顺带记录了 P7-C-2 的基线。
//
// 语料不存在时跳过（CI 里没有 tiptop-gp）。
func TestCorpusGrammarBaseline(t *testing.T) {
	root := os.Getenv("BDL_CORPUS_ROOT")
	if root == "" {
		root = "/code/workspace/tiptop-gp"
	}
	if _, err := os.Stat(filepath.Join(root, "tiptop")); err != nil {
		t.Skip("没有真实语料，跳过")
	}

	pool, err := parser.New()
	if err != nil {
		t.Fatalf("初始化解析器: %v", err)
	}
	defer pool.Close()

	rep := Run(pool, root, 0, 8, 0)
	var unknown []string
	for _, f := range rep.FileErrs {
		rel, rerr := filepath.Rel(root, f.Path)
		if rerr != nil {
			rel = f.Path
		}
		rel = filepath.ToSlash(rel)
		if _, ok := knownCorpusErrors[rel]; !ok {
			unknown = append(unknown, rel)
		}
	}
	if len(unknown) > 0 {
		t.Fatalf("出现新的解析失败文件（%d 个），先确认是 grammar 缺口还是语料问题：\n  %s",
			len(unknown), strings.Join(unknown, "\n  "))
	}
	if rep.ErrFiles > len(knownCorpusErrors) {
		t.Fatalf("出错文件数 %d 超过基线 %d", rep.ErrFiles, len(knownCorpusErrors))
	}
	t.Logf("语料 %d 文件，解析失败 %d 文件 / %d 节点（基线 %d 文件）",
		rep.Files, rep.ErrFiles, rep.ErrNodes, len(knownCorpusErrors))
}

// knownCorpusErrors 是 P7-C-2（2026-10-21）冻结的基线：
// 18 个文件在整份语料里解析失败，逐个确认过都是"语料自身的问题"
// （拼写、截断、多程序拼接、.per 文档附录里的全角标点），不是语法缺口。
// 详细分类见本文件的基线清单。
var knownCorpusErrors = map[string]struct{}{
	"tiptop/demo/per/apmi252.per":             {},
	"tiptop/demo/per/aooi010.per":             {},
	"tiptop/msv/4gl/findidx.4gl":              {},
	"topcust/cws/4gl/cws_get_start_works.4gl": {},
	"tiptop/asf/4gl/safi501.4gl":              {},
	"tiptop/asf/4gl/TX_asfi301_1.4gl":         {},
	"tiptop/asf/4gl/TX_asfi301_2.4gl":         {},
	"tiptop/asf/4gl/TX_asfi301_3.4gl":         {},
	"tiptop/apm/4gl/sapmt540 (3).4gl":         {},
	"tiptop/apm/4gl/sapmt720_sub.4gl":         {},
	"tiptop/apm/4gl/baofei.4gl":               {},
	"tiptop/asf/4gl/csfi510.4gl":              {},
	"tiptop/ghr/4gl/ghri0431.4gl":             {},
	"tiptop/ghr/4gl/ghri0471.4gl":             {},
	"topcust/csf/4gl/csfq012.4gl":             {},
	"topcust/csub/4gl/cs_restful.4gl":         {},
	"topcust/cxc/4gl/cxcq761.4gl":             {},
	"topcust/cxm/4gl/saxmt400.4gl":            {},
}
