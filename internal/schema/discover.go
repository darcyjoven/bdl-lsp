package schema

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/link"
)

// Discover 找出工作区里的 schema 文件。
//
// 约定位置：`<root>/<模组根>/<模组>/sch/*.sch`（语料实测 3127 个全部如此）。
// extra 是用户额外配置的目录（相对 root，也支持绝对路径），用于 schema
// 集中放在别处的项目。
//
// 只认名为 `sch` 的目录：语料里 0 个 .sch 在别处，而工作区全量 walk
// 会把 node_modules/构建产物一起扫进来。
func Discover(root string, moduleRoots, extra []string) []string {
	var out []string
	seen := map[string]bool{}
	add := func(p string) {
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}

	for _, base := range link.NewLayout(root, moduleRoots).RootDirs() {
		walkSch(base, add)
	}
	for _, p := range extra {
		if p == "" {
			continue
		}
		if !filepath.IsAbs(p) {
			p = filepath.Join(root, p)
		}
		// 额外目录本身可能就叫 sch，也可能是个父目录。
		if fi, err := os.Stat(p); err == nil && fi.IsDir() {
			if strings.EqualFold(filepath.Base(p), "sch") {
				walkSchDir(p, add)
			} else {
				walkSch(p, add)
			}
		}
	}
	sort.Strings(out)
	return out
}

// walkSch 在 base 下找名为 sch 的目录。
func walkSch(base string, add func(string)) {
	_ = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		name := d.Name()
		if name == "node_modules" || name == ".git" {
			return filepath.SkipDir
		}
		if strings.EqualFold(name, "sch") {
			walkSchDir(p, add)
			return filepath.SkipDir
		}
		return nil
	})
}

// walkSchDir 收集一个 sch 目录下的 .sch 文件（不递归子目录）。
func walkSchDir(dir string, add func(string)) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".sch") {
			continue
		}
		add(filepath.Join(dir, e.Name()))
	}
}

// LoadWorkspace 扫描并解析工作区里的全部 schema 文件。
func LoadWorkspace(root string, moduleRoots, extra []string) *Index {
	return LoadFiles(Discover(root, moduleRoots, extra))
}
