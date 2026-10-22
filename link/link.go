// Package link 提供「程序链接资料」（P7-D）：谁链接了哪些 4GL 文件。
//
// # 它解决什么问题
//
// Genero 的编译产物（可执行文件）由多个 .4gl 组成：一个含 MAIN 的主文件 +
// 若干子文件/子模组。**只有链接进来的文件里的函数才可调用**，这个信息既不
// 在源码里也不能静态推导，必须从外部资料（数据库表 DS.GAL_FILE）取。
//
// 没有它，"调用了一个不存在的函数"根本无法判定——语料实测：24 个真实文件
// 一开 `function-not-found` 就报 3660 条，全是内置函数与跨模块函数。
//
// # 三条不可越界的规则（用户 2026-10-21 明确）
//
//  1. **链接只放大"函数"的可见性，不放大"变量"**。GLOBALS 变量只认当前
//     文件 DEFINE 的与 GLOBALS 块引入的，跨链接不共享。所以本包不参与
//     `variable-source-unresolved` 的判定（那是 internal/globals 的地盘）。
//  2. `sub` / `lib` / `qry` 三个模组**对所有程序可见**（含它们自己），
//     扁平规则，无方向限制，不做矩阵。
//  3. 链接里有、磁盘上没有的文件**完全忽略**：不报诊断、不进编辑器提示，
//     但**必须原样留在 links.jsonl 里**（实测 800 条，都是语料里不存在的
//     W* 模组，即另一套/已下线系统）。
//
// # 主键为什么是 MAIN 程序代码
//
// 表里每行是 `(程序编号 GAL01, 模组 GAL02, 程序代码 GAL03, 链接否 GAL04)`。
// 直觉会按 GAL01 建索引，但实测有 **160 个组的 GAL01 不在自己组内**
// （`aimt823 → [aimt822]`），说明"编号≠文件名"，用它 join 会直接落空。
// 每组的锚点其实是那一条**含 MAIN 的程序代码**（R5：唯一或不存在），
// 所以索引主键用 MAIN 的程序代码，GAL01 只作展示用的元数据。
package link

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// SchemaVersion 是 links.jsonl / links.meta.json 的当前 schema 版本。
const SchemaVersion = 1

// 默认文件名。
const (
	// LinksFileName 是链路数据文件名。
	LinksFileName = "links.jsonl"
	// MetaFileName 是元数据文件名。
	MetaFileName = "links.meta.json"
	// DefaultDataDir 是默认资料目录（相对项目根）。
	//
	// 用 `.link` 而不是 `.bdl-lsp/link`：点开头是隐藏目录，名字短对
	// Windows 路径长度友好；且它与 `.bdl-lsp`（编辑器配置）语义不同——
	// 这份数据是从外部系统同步来的，通常不提交进仓库。
	DefaultDataDir = ".link"
)

// FileRef 是链路里的一个文件：`[模组, 程序代码]`。
//
// 用二元数组而不是对象，实测省约 40% 体积（一个链 524 个文件时差别明显）。
type FileRef [2]string

// Module 返回模组（大写形式按链接资料原样保留）。
func (f FileRef) Module() string { return f[0] }

// Code 返回程序代码（4GL 去后缀的文件名）。
func (f FileRef) Code() string { return f[1] }

// Key 返回用于比对的规范化键（全小写，`模组/代码`）。
func (f FileRef) Key() string {
	return strings.ToLower(f[0]) + "/" + strings.ToLower(f[1])
}

// Chain 是 links.jsonl 的一行：一条链接资料。
//
// Main 为空表示这条链**没有 MAIN**（orphan，实测 81 组）：可能是子模组
// 自己的编译单元，也可能是数据缺失。这类链仍要保留——反查"谁链接了我"
// 时需要它。
type Chain struct {
	Schema int `json:"schema"`
	// Program 是程序编号 GAL01。它**不一定是文件名**（R6），只作展示。
	Program string `json:"program"`
	// Module 是 MAIN 所在模组 GAL02（orphan 时为第一条的模组）。
	Module string `json:"module,omitempty"`
	// Main 是含 MAIN 的程序代码 GAL03，**索引主键**。orphan 时为空；
	// 多个 MAIN 时是字典序第一个（其余在 Mains 里）。
	Main string `json:"main,omitempty"`
	// Mains 是除 Main 之外**其余**含 MAIN 的程序代码。
	//
	// 只在数据异常（一组内 ≥2 个 MAIN，实测 5 组，如 aws_ttsrv2 同时列出
	// 自己与 aws_ttsrv2_transxml）时非空。全部保留：每个 MAIN 打开时
	// 都应当关联整条链，否则会有一半的入口查不到自己的链接。
	Mains []string `json:"mains,omitempty"`
	// Ambiguous 表示该组内有 ≥2 个 MAIN。数据异常但不丢弃。
	Ambiguous bool `json:"ambiguous,omitempty"`
	// Files 是链里的全部文件（含 MAIN 自身），已去重。
	Files []FileRef `json:"files"`

	// 以下字段不进 JSON，加载后计算。
	byMain string // 规范化主键（全小写）
}

// AllMains 返回该链全部含 MAIN 的程序代码（正常情况 0 或 1 个）。
func (c *Chain) AllMains() []string {
	if c == nil {
		return nil
	}
	out := make([]string, 0, 1+len(c.Mains))
	if c.Main != "" {
		out = append(out, c.Main)
	}
	out = append(out, c.Mains...)
	return out
}

// HasFile 报告链里是否含 (模组, 代码)（大小写不敏感）。
func (c *Chain) HasFile(module, code string) bool {
	if c == nil {
		return false
	}
	for _, f := range c.Files {
		if strings.EqualFold(f[0], module) && strings.EqualFold(f[1], code) {
			return true
		}
	}
	return false
}

// External 是一个"外部函数"记录：被调用、但从无定义。
type External struct {
	Name  string `json:"name"`
	Files int    `json:"files"`
	Calls int    `json:"calls"`
}

// Meta 是 links.meta.json：导入统计与来源。
//
// 存在的理由：用户要能一眼看出"这份资料是什么时候、从哪来的、跳过了多少"。
// 没有它，链接数据出问题时只能靠猜。
type Meta struct {
	Schema        int      `json:"schema"`
	GeneratedAt   string   `json:"generatedAt"`
	Source        string   `json:"source,omitempty"`       // db / script / command / file
	SourceDetail  string   `json:"sourceDetail,omitempty"` // 例如 DS.GAL_FILE@host:1521/svc
	RowsTotal     int      `json:"rowsTotal"`
	RowsFilteredN int      `json:"rowsFilteredN"` // gal04<>'Y' 被过滤的行数
	RowsY         int      `json:"rowsY"`
	Chains        int      `json:"chains"`       // 有 MAIN 的链
	Orphans       int      `json:"orphans"`      // 无 MAIN 的组
	Ambiguous     int      `json:"ambiguous"`    // 有多个 MAIN 的组
	MissingFiles  int      `json:"missingFiles"` // 磁盘上找不到的记录数
	MultiMain     int      `json:"multiMain,omitempty"`
	ResolvedFrom  []string `json:"resolvedFrom,omitempty"` // 参与解析的模组根
	// Externals 是"被 ≥ExternalsMinFiles 个文件调用、却在整份资料里找不到
	// 定义"的函数。它们多半是**语料快照缺少的库函数**（实测 `cl_err3`
	// 27315 次 / 186 个文件），而不是用户写错。
	//
	// 只存前 MaxStoredExternals 个（按文件数降序），总数见 ExternalsTotal。
	Externals         []External `json:"externals,omitempty"`
	ExternalsTotal    int        `json:"externalsTotal,omitempty"`
	ExternalsMinFiles int        `json:"externalsMinFiles,omitempty"`
	ExternalsMinCalls int        `json:"externalsMinCalls,omitempty"`
	ExternalsSkipped  bool       `json:"externalsSkipped,omitempty"`
	ToolVersion       string     `json:"toolVersion,omitempty"`
	// GeneratedAtTime 是 GeneratedAt 的解析结果（不落盘）。
	generatedAt time.Time
}

// GeneratedTime 返回导入时间（解析失败时零值）。
func (m *Meta) GeneratedTime() time.Time {
	if m == nil {
		return time.Time{}
	}
	if !m.generatedAt.IsZero() {
		return m.generatedAt
	}
	if m.GeneratedAt == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, m.GeneratedAt)
	if err != nil {
		return time.Time{}
	}
	m.generatedAt = t
	return t
}

// Age 返回资料年龄（未解析出时间时返回 0）。
func (m *Meta) Age() time.Duration {
	t := m.GeneratedTime()
	if t.IsZero() {
		return 0
	}
	return time.Since(t)
}

// Index 是加载后的链路索引（只读）。
type Index struct {
	chains []*Chain
	byMain map[string]*Chain   // MAIN 代码（小写）→ 链
	byCode map[string][]*Chain // 任意文件代码（小写）→ 含它的链
	meta   *Meta

	// 统计（加载时按实际内容重算，不盲信 meta 里的数字）
	stat Stats
}

// Stats 是从链路内容实际数出来的统计。
type Stats struct {
	Chains    int // 有 MAIN
	Orphans   int // 无 MAIN
	Ambiguous int
	Files     int // 不同 (模组,代码) 数
}

// Stats 返回加载时重算的统计。
func (i *Index) Stats() Stats {
	if i == nil {
		return Stats{}
	}
	return i.stat
}

// Meta 返回元数据（可能为 nil）。
func (i *Index) Meta() *Meta {
	if i == nil {
		return nil
	}
	return i.meta
}

// Chains 返回全部链（顺序与文件一致）。调用方不得修改。
func (i *Index) Chains() []*Chain {
	if i == nil {
		return nil
	}
	return i.chains
}

// Len 返回链数。
func (i *Index) Len() int {
	if i == nil {
		return 0
	}
	return len(i.chains)
}

// FindByMain 按 MAIN 程序代码查链（大小写不敏感）。
func (i *Index) FindByMain(code string) *Chain {
	if i == nil || code == "" {
		return nil
	}
	return i.byMain[strings.ToLower(code)]
}

// ChainsOf 返回包含该文件代码的全部链（大小写不敏感），用于反查。
//
// 实测同一个代码最多被 324 个组共享（`aws_xml`），调用方要准备分页。
func (i *Index) ChainsOf(code string) []*Chain {
	if i == nil || code == "" {
		return nil
	}
	return i.byCode[strings.ToLower(code)]
}

// ChainsOfFile 返回包含 (模组, 代码) 的链。比 ChainsOf 更精确，
// 但资料里同一代码偶尔出现在不同模组（客户化覆盖），所以两者都要有。
func (i *Index) ChainsOfFile(module, code string) []*Chain {
	all := i.ChainsOf(code)
	if module == "" || len(all) == 0 {
		return all
	}
	want := strings.ToLower(module)
	out := make([]*Chain, 0, len(all))
	for _, c := range all {
		for _, f := range c.Files {
			if strings.EqualFold(f[0], want) && strings.EqualFold(f[1], code) {
				out = append(out, c)
				break
			}
		}
	}
	return out
}

// build 从链列表建索引。
func build(chains []*Chain, meta *Meta) *Index {
	idx := &Index{
		chains: chains,
		byMain: make(map[string]*Chain, len(chains)),
		byCode: make(map[string][]*Chain),
		meta:   meta,
	}
	files := map[string]bool{}
	for _, c := range chains {
		c.Schema = SchemaVersion
		c.byMain = strings.ToLower(c.Main)
		mains := c.AllMains()
		if len(mains) > 0 {
			idx.stat.Chains++
			for _, m := range mains {
				key := strings.ToLower(m)
				// 多个链抢同一个 MAIN 是数据错误，保留先到的（不 panic：
				// 资料坏了也要能用，问题由 link-data-invalid 报告）。
				if _, dup := idx.byMain[key]; !dup {
					idx.byMain[key] = c
				}
			}
		} else {
			idx.stat.Orphans++
		}
		if c.Ambiguous {
			idx.stat.Ambiguous++
		}
		for _, f := range c.Files {
			key := strings.ToLower(f[1])
			if key == "" {
				continue
			}
			idx.byCode[key] = appendUnique(idx.byCode[key], c)
			files[f.Key()] = true
		}
	}
	idx.stat.Files = len(files)
	return idx
}

// appendUnique 追加链，已存在则跳过（同一链内重复列同一代码时去重）。
func appendUnique(list []*Chain, c *Chain) []*Chain {
	for _, e := range list {
		if e == c {
			return list
		}
	}
	return append(list, c)
}

// Load 从资料目录读取 links.jsonl + links.meta.json。
//
// meta 缺失不是错误（老数据/手工造的测试数据都可能没有），只在诊断里
// 记为"未知来源"。links.jsonl 缺失返回 ErrNoData，调用方据此决定
// "整个链接功能不启用"。
func Load(dir string) (*Index, error) {
	if dir == "" {
		return nil, ErrNoData
	}
	f, err := os.Open(filepath.Join(dir, LinksFileName))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNoData
		}
		return nil, fmt.Errorf("打开 %s: %w", LinksFileName, err)
	}
	defer f.Close()

	var chains []*Chain
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	line := 0
	for sc.Scan() {
		line++
		text := strings.TrimSpace(sc.Text())
		if text == "" || strings.HasPrefix(text, "//") {
			continue
		}
		var c Chain
		if err := json.Unmarshal([]byte(text), &c); err != nil {
			return nil, fmt.Errorf("%s 第 %d 行: %w", LinksFileName, line, err)
		}
		if c.Schema > SchemaVersion {
			return nil, fmt.Errorf("%s 第 %d 行: schema=%d 高于支持的 %d", LinksFileName, line, c.Schema, SchemaVersion)
		}
		if c.Program == "" && c.Main == "" {
			return nil, fmt.Errorf("%s 第 %d 行: program 与 main 都为空", LinksFileName, line)
		}
		chains = append(chains, &c)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("读取 %s: %w", LinksFileName, err)
	}
	if len(chains) == 0 {
		// 文件在但一条链都没有：不能当成"有效但空"的资料。
		// 否则 function-not-found 会把所有跨文件调用都当未定义（灾难性误报）。
		return nil, fmt.Errorf("%s 里没有任何链（0 行有效数据）", LinksFileName)
	}

	meta := loadMeta(dir)
	return build(chains, meta), nil
}

// loadMeta 读元数据；任何问题都只返回 nil（元数据是辅助信息，不该阻断）。
func loadMeta(dir string) *Meta {
	data, err := os.ReadFile(filepath.Join(dir, MetaFileName))
	if err != nil {
		return nil
	}
	var m Meta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil
	}
	return &m
}

// ErrNoData 表示还没有链接资料。
var ErrNoData = errors.New("没有链接资料（.link/links.jsonl 不存在）")

// Save 把链与元数据写入目录（原子：先写临时文件再 rename）。
//
// 原子性不是洁癖：LSP 可能同时在读，写到一半的 JSONL 会让它读到
// 半行数据然后报 link-data-invalid。
func Save(dir string, chains []*Chain, meta *Meta) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("创建 %s: %w", dir, err)
	}
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	for _, c := range chains {
		c.Schema = SchemaVersion
		if err := enc.Encode(c); err != nil {
			return fmt.Errorf("编码链 %s: %w", c.Program, err)
		}
	}
	if err := writeFileAtomic(filepath.Join(dir, LinksFileName), []byte(sb.String())); err != nil {
		return err
	}
	if meta != nil {
		meta.Schema = SchemaVersion
		if meta.GeneratedAt == "" {
			meta.GeneratedAt = time.Now().Format(time.RFC3339)
		}
		data, err := json.MarshalIndent(meta, "", "  ")
		if err != nil {
			return fmt.Errorf("编码 meta: %w", err)
		}
		data = append(data, '\n')
		if err := writeFileAtomic(filepath.Join(dir, MetaFileName), data); err != nil {
			return err
		}
	}
	return nil
}

// writeFileAtomic 写临时文件再 rename，避免读到半截文件。
func writeFileAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	f, err := os.CreateTemp(dir, ".tmp-"+filepath.Base(path)+"-*")
	if err != nil {
		return fmt.Errorf("创建临时文件: %w", err)
	}
	tmp := f.Name()
	defer func() {
		// 失败路径上清理；成功 rename 后该文件已不存在。
		_ = os.Remove(tmp)
	}()
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("写 %s: %w", tmp, err)
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return fmt.Errorf("同步 %s: %w", tmp, err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("关闭 %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("重命名到 %s: %w", path, err)
	}
	return nil
}

// SortChains 按 program 排序（稳定输出，方便 diff）。
func SortChains(chains []*Chain) {
	sort.SliceStable(chains, func(i, j int) bool {
		if chains[i].Program != chains[j].Program {
			return chains[i].Program < chains[j].Program
		}
		return chains[i].Main < chains[j].Main
	})
}
