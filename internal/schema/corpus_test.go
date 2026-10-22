package schema

import (
	"os"
	"testing"
	"time"
)

// TestCorpusSchema 用真实语料核对解析覆盖度（语料不在时跳过）。
func TestCorpusSchema(t *testing.T) {
	root := os.Getenv("BDL_CORPUS_ROOT")
	if root == "" {
		root = "/code/workspace/tiptop-gp"
	}
	if _, err := os.Stat(root + "/tiptop"); err != nil {
		t.Skip("没有真实语料")
	}
	start := time.Now()
	files := Discover(root, nil, nil)
	ix := LoadFiles(files)
	t.Logf("文件 %d，表 %d，耗时 %v，可疑语句 %d 条", len(files), ix.Len(), time.Since(start), len(ix.Problems))

	if len(files) < 3000 {
		t.Errorf("发现文件数 = %d，期望 ≥3000", len(files))
	}
	// 2945 条 create table，69 个表名在多个模组目录里重复 → 约 2879 张表
	if ix.Len() < 2800 {
		t.Errorf("表数 = %d，期望 ≥2800", ix.Len())
	}
	for _, p := range ix.Problems[:min(5, len(ix.Problems))] {
		t.Logf("可疑：%s", p)
	}
	// 抽查几张真实表
	for _, name := range []string{"mmb_file", "apa_file", "ima_file"} {
		tb := ix.Table(name)
		if tb == nil {
			t.Errorf("缺少表 %s", name)
			continue
		}
		if len(tb.Columns) < 5 {
			t.Errorf("表 %s 只有 %d 列", name, len(tb.Columns))
		}
	}
}
