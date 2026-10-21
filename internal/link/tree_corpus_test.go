package link

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestTreeOnRealCorpus 在真实语料上验证"链接资料 UI"的单次组装耗时。
//
// 为什么需要这条：`bdl/linkTree` 会在每次切换文件时被调用一次，
// 而服务器是串行处理请求的——这里慢 1 秒，编辑器里所有功能都慢 1 秒。
// 早期实现逐文件扫目录，一条 524 个文件的链要 10 秒以上。
//
// 语料不存在时跳过（CI 里没有 tiptop-gp）。
func TestTreeOnRealCorpus(t *testing.T) {
	root := os.Getenv("BDL_CORPUS_ROOT")
	if root == "" {
		root = "/code/workspace/tiptop-gp"
	}
	dataDir := filepath.Join(root, ".link")
	if _, err := os.Stat(filepath.Join(dataDir, LinksFileName)); err != nil {
		t.Skip("没有真实语料/链接资料，跳过")
	}
	layout := NewLayout(root, nil)
	idx, err := Load(dataDir)
	if err != nil {
		t.Skipf("加载链接资料失败：%v", err)
	}
	r := NewResolver(idx, layout, nil, nil)

	// 找一条"文件最多"的链，最坏情况才有意义。
	// 只看有 MAIN 的链：orphan 链（无 MAIN）在 UI 里是"反查"入口，
	// 不是"这个文件的链接链"。
	var biggest *Chain
	for _, c := range idx.Chains() {
		if c.Main == "" {
			continue
		}
		if biggest == nil || len(c.Files) > len(biggest.Files) {
			biggest = c
		}
	}
	if biggest == nil {
		t.Skip("没有链")
	}
	start := time.Now()
	tree := r.Tree(biggest.Module, biggest.Main)
	first := time.Since(start)
	if !tree.Found {
		t.Fatalf("最大的链应能查到：%+v", biggest)
	}
	if first > 500*time.Millisecond {
		t.Errorf("首次组装耗时 %s，超过 500ms（会让编辑器所有请求排队）", first)
	}
	// 第二次应该走缓存（模组清单不再扫盘）。
	start = time.Now()
	_ = r.Tree(biggest.Module, biggest.Main)
	second := time.Since(start)
	if second > 100*time.Millisecond {
		t.Errorf("第二次组装耗时 %s，缓存没生效", second)
	}
	t.Logf("链 %s：%d 个文件，首次 %s，再次 %s", biggest.Main, len(biggest.Files), first, second)
}
