// Package config 定义语言服务器的可配置项。
//
// 配置来源（优先级从高到低）：
//  1. LSP 客户端 via `workspace/didChangeConfiguration`；
//  2. 工作区文件 `.bdl-lsp/config.json`；
//  3. 内置默认值。
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/darcyjoven/bdl-lsp/glob"
	"github.com/darcyjoven/bdl-lsp/internal/fileuri"
)

// GeneroVersion 是目标 Genero 版本。
type GeneroVersion string

const (
	// Genero25 是 v1 的基线版本（当前 grammar 覆盖范围）。
	Genero25 GeneroVersion = "2.50"
	// Genero30 / Genero40 预留：语法差异尚无明确需求，暂不排期。
	Genero30 GeneroVersion = "3.00"
	Genero40 GeneroVersion = "4.00"
)

// DiagnosticLevel 是语法/语义诊断的严重级别。
type DiagnosticLevel string

const (
	LevelOff     DiagnosticLevel = "off"
	LevelError   DiagnosticLevel = "error"
	LevelWarning DiagnosticLevel = "warning"
	LevelInfo    DiagnosticLevel = "information"
	LevelHint    DiagnosticLevel = "hint"
)

// Limits 是资源保护阈值。
type Limits struct {
	// MaxFileBytes 超过该大小的文件不做语法诊断（仍可高亮）。
	MaxFileBytes int `json:"maxFileBytes"`
	// MaxFileLines 超过该行数的文件不做语法诊断。
	MaxFileLines int `json:"maxFileLines"`
	// MaxIssues 单文件最多报告的诊断条数。
	MaxIssues int `json:"maxIssues"`
	// ParseTimeoutMS 单次解析超时（毫秒）。
	ParseTimeoutMS int `json:"parseTimeoutMs"`
}

// Settings 是完整配置。
type Settings struct {
	// GeneroVersion 目标 Genero 版本，默认 2.50。
	GeneroVersion GeneroVersion `json:"generoVersion"`
	// Globals 是 GLOBALS 引入（P3a）相关的配置。
	Globals GlobalsSettings `json:"globals"`
	// Diagnostics 是**唯一**的诊断级别入口：键为诊断码（如
	// "variable-source-unresolved"）、值为级别（off/warning/error/...）。
	// 未列出的码使用 internal/diag 注册表的出厂默认值。
	Diagnostics map[string]DiagnosticLevel `json:"diagnostics"`
	// Builtins 是内置函数/变量符号表（P7-C）的配置。
	Builtins BuiltinsSettings `json:"builtins"`
	// Link 是程序链接资料（P7-D）的配置。
	Link LinkSettings `json:"link"`
	// Index 是工作区符号索引（P6）的配置。
	Index IndexSettings `json:"index"`
	// Schema 是数据库 schema 索引（P6）的配置。
	Schema SchemaSettings `json:"schema"`
	// Ignore 是诊断忽略列表（P8）：按文件/字段/表单 glob 屏蔽诊断。
	Ignore IgnoreSettings `json:"ignore"`
	// PerRoots 是 .per 表单的附加解析根（相对工作区根，按序尝试）。
	PerRoots []string `json:"perRoots"`
	// Format 是 4GL 格式化配置（P4）。.per 不做任何格式化（用户决定）。
	Format FormatSettings `json:"format"`
	// Limits 资源保护阈值。
	Limits Limits `json:"limits"`
	// LogLevel 日志级别。
	LogLevel string `json:"logLevel"`
}

// FormatSettings 控制 4GL 格式化（P4）。
//
// 用户要求（2026-10-20）：关键字大写/小写/不变；缩进 tab/空格可选、
// 个数可选。.per 不做任何格式化（server 层直接拒绝）。
type FormatSettings struct {
	// Keywords 是关键字大小写：unchanged（默认）/ upper / lower。
	Keywords string `json:"keywords"`
	// IndentStyle 是缩进风格：auto（默认，跟随编辑器请求的
	// tabSize/insertSpaces）/ tab / space。
	IndentStyle string `json:"indentStyle"`
	// IndentSize 是每级缩进的空格数：0（默认，跟随编辑器）/ 正数。
	// IndentStyle=tab 时忽略。
	IndentSize int `json:"indentSize"`
}

// GlobalsSettings 控制 GLOBALS 模组全局变量相关的解析与诊断（P3a）。
type GlobalsSettings struct {
	// SearchPaths 是附加的解析根（相对工作区根，按序尝试）。
	//
	// 动机：语料里 97 处 `../../config/top.global` 因模块目录层级不同
	// 解析不到（rest/aps/4gl 在 tiptop/rest/config 下没有 top.global，
	// 真实文件在 tiptop/config）。配置 ["tiptop/config"] 后这类引用可命中。
	// 默认空（不启用）。
	SearchPaths []string `json:"searchPaths"`
}

// BuiltinsSettings 控制内置符号表（P7-C）。
//
// 内置表是"Genero 自带的名字"白名单：STATUS/SQLCA/INT_FLAG/ARG_VAL/
// fgl_* / ui.* 等。没有它，这些名字在静态分析里全都"查不到定义"，
// 实测占全量诊断的 98%。
//
// 为什么要有配置：内置名单会随 Genero 版本增长，也会因为"语料里发现
// 漏项"而补。内置数据（internal/builtin/data/builtins.json）随二进制
// 发布，用户不该为了补一个名字去改扩展；所以提供三条口子：
// 覆盖文件、路径列表、禁用列表。
type BuiltinsSettings struct {
	// Enable 关闭后所有内置名都不再参与诊断白名单（等价于 P7-C 之前的行为）。
	// 默认 true。用 *bool 区分"未下发"与"显式 false"。
	Enable *bool `json:"enable,omitempty"`
	// Paths 是额外的覆盖文件或目录（相对工作区根，按序加载，后者覆盖前者）。
	// 目录里的 *.json 全部加载。项目约定路径 `.bdl-lsp/builtins.json`
	// 会自动加载，不需要写在这里。
	Paths []string `json:"paths"`
	// Disable 直接关闭某些内置名（大小写不敏感），优先级高于任何 add。
	// 典型用途：项目里自己定义了同名函数，不想被内置表"吞掉"。
	Disable []string `json:"disable"`
}

// BuiltinsEnabled 报告内置符号表是否启用（默认启用）。
func (s Settings) BuiltinsEnabled() bool {
	if s.Builtins.Enable == nil {
		return true
	}
	return *s.Builtins.Enable
}

// IndexSettings 是 `bdl.index`：工作区符号索引（P6）。
//
// 默认启用：没有它 `workspace/symbol` / 跳转 / 引用只看得到已打开的文件，
// 用户会因为"时而能跳时而不能"而困惑。7600 个文件的首次索引约 20-25 s
// （后台、不阻塞编辑），之后按 (mtime,size) 增量刷新。
// SchemaSettings 是 `bdl.schema`：数据库 schema 索引（P6）。
//
// 默认启用：`DEFINE r RECORD LIKE t.*` 在语料里出现 21144 次，成员全在
// 数据库表结构里。没有 schema 索引时这些记录只能整体不校验。
// 解析的是工作区里的 `.sch` 文件（`<模组根>/<模组>/sch/*.sch`），
// 不连数据库、不读生产库信息。
type SchemaSettings struct {
	// Enable 关闭后不解析 .sch 文件（`LIKE t.*` 的记录退回"不校验"）。
	Enable *bool `json:"enable,omitempty"`
	// Paths 是额外的 schema 目录（相对工作区根，或绝对路径）。
	Paths []string `json:"paths,omitempty"`
}

// SchemaEnabled 报告 schema 索引是否启用（默认启用）。
func (s Settings) SchemaEnabled() bool {
	if s.Schema.Enable == nil {
		return true
	}
	return *s.Schema.Enable
}

type IndexSettings struct {
	// Enable 关闭后不建索引（超大仓库 / 只想编辑单个文件时用）。
	Enable *bool `json:"enable,omitempty"`
}

// IndexEnabled 报告工作区索引是否启用（默认启用）。
func (s Settings) IndexEnabled() bool {
	if s.Index.Enable == nil {
		return true
	}
	return *s.Index.Enable
}

// IgnoreSettings 是 `bdl.ignore`：按文件/字段/表单忽略诊断（P8）。
//
// 为什么需要：真实项目里有构建产物、历史遗留目录，也有少量长期存在的
// 字段/表单版本漂移。逐条 `bdl.diagnostics.<码> = off` 会一刀切关掉整个
// 诊断码，范围太大；忽略列表把范围收窄到"某类文件/某个字段/某张表单"。
//
// 语义（详见 docs/FEATURES.md）：
//   - Files：匹配当前文件（工作区相对路径，`**` 跨目录；不含 `/` 的模式
//     同时按 basename 匹配），命中后**该文件的全部诊断**都不发布，并且
//     不进入工作区符号索引（大仓库排除 build/ 产物）；
//   - Fields：匹配字段名（大小写不敏感），只屏蔽 `field-not-in-form`；
//   - Forms：匹配表单字面量/文件名/相对路径，屏蔽该表单的全部字段类诊断
//     （当前是 `field-not-in-form`；以后新增的字段类码一并覆盖）。
//
// 忽略只影响**诊断与索引**，不影响解析/悬停/跳转：用户点开被忽略的文件
// 仍然有语法高亮与导航。
type IgnoreSettings struct {
	Files  []string `json:"files"`
	Fields []string `json:"fields"`
	Forms  []string `json:"forms"`
}

// LinkSettings 控制程序链接资料（P7-D）。
//
// 链接资料回答"这个可执行文件链接了哪些 4gl 文件"，从而决定哪些**函数**
// 可被调用。没有它，`CALL 不存在的函数()` 无法判定——实测 24 个真实文件
// 一开检查就报 3660 条误报。
//
// 三条铁律（用户 2026-10-21 明确）：
//  1. 只放大**函数**可见性，绝不放大变量（R2）；
//  2. sub/lib/qry 对所有程序可见，含它们自己（扁平规则，无矩阵）；
//  3. 链接里有、磁盘上没有的文件完全忽略，不报诊断。
type LinkSettings struct {
	// Enable 关闭后不做任何链接相关诊断（默认启用）。
	Enable *bool `json:"enable,omitempty"`
	// DataDir 是资料目录（相对项目根），默认 `.link`。
	DataDir string `json:"dataDir"`
	// ModuleRoots 是模组根（相对项目根）。空 = 自动探测。
	ModuleRoots []string `json:"moduleRoots"`
	// GlobalModules 是对所有程序可见的模组，默认 ["sub","lib","qry"]。
	GlobalModules []string `json:"globalModules"`
	// MaxAgeDays 是资料过期提示阈值（天），0 = 不提示。
	MaxAgeDays int `json:"maxAgeDays"`
	// 注意：这里**没有**采集（sync）配置。
	// 生成 .project/ 资料是 tiptop 的职责，bdl-lsp 只读它。
	// 见 docs/PLAN-SEPARATION.md。
}

// LinkEnabled 报告链接资料功能是否启用（默认启用）。
func (s Settings) LinkEnabled() bool {
	if s.Link.Enable == nil {
		return true
	}
	return *s.Link.Enable
}

// LinkDataDir 返回资料目录（默认 `.link`）。
func (s Settings) LinkDataDir() string {
	if s.Link.DataDir == "" {
		return ".project"
	}
	return s.Link.DataDir
}

// Default 返回默认配置。
func Default() Settings {
	return Settings{
		GeneroVersion: Genero25,
		LogLevel:      "info",
		Format: FormatSettings{
			Keywords:    "unchanged",
			IndentStyle: "auto",
			IndentSize:  0,
		},
		Limits: Limits{
			MaxFileBytes:   2 * 1024 * 1024, // 2 MB
			MaxFileLines:   20000,
			MaxIssues:      200,
			ParseTimeoutMS: 2000,
		},
	}
}

// Manager 保存当前配置，支持热更新。
type Manager struct {
	mu   sync.RWMutex
	cur  Settings
	path string // 工作区配置文件路径，可为空
	// fileDiagnostics 是 `.bdl-lsp/config.json` 里的诊断覆盖，作为客户端
	// 覆盖的基底。客户端下发空 `diagnostics` 时恢复到这里，而不是清空，
	// 否则个人设置会把团队共享的项目配置抹掉。
	fileDiagnostics map[string]DiagnosticLevel
}

// NewManager 创建配置管理器；rootURI 非空时会尝试读取工作区配置。
func NewManager(rootURI string) *Manager {
	m := &Manager{cur: Default()}
	if rootURI != "" {
		if path, ok := configPath(rootURI); ok {
			m.path = path
			if err := m.loadFile(path); err != nil {
				// 配置读取失败不致命：保持默认值，错误由调用方记录日志。
				_ = err
			}
		}
	}
	return m
}

// configPath 由 rootURI 推导 `.bdl-lsp/config.json` 的路径。
func configPath(rootURI string) (string, bool) {
	root, err := uriToPath(rootURI)
	if err != nil || root == "" {
		return "", false
	}
	return filepath.Join(root, ".bdl-lsp", "config.json"), true
}

// uriToPath 把 file:// URI 转换为本地路径。
//
// 委托 internal/fileuri（P7-A-2）。旧版本在这里手写第二份实现，
// 只处理 `%20`；VSCode 在 Windows 上把盘符冒号编码成 `%3A`，
// 旧实现解出 `/c%3A/...` → `.bdl-lsp/config.json` 永远读不到。
func uriToPath(uri string) (string, error) {
	p, err := fileuri.ToPath(uri)
	if err != nil {
		return "", err
	}
	if p == "" {
		return "", fmt.Errorf("空的 file URI: %s", uri)
	}
	return p, nil
}

func (m *Manager) loadFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var s Settings
	if err := json.Unmarshal(data, &s); err != nil {
		return fmt.Errorf("解析 %s: %w", path, err)
	}
	m.mu.Lock()
	if s.Diagnostics != nil {
		m.fileDiagnostics = make(map[string]DiagnosticLevel, len(s.Diagnostics))
		for code, lv := range s.Diagnostics {
			m.fileDiagnostics[code] = lv
		}
	}
	m.mu.Unlock()
	m.merge(&s)
	return nil
}

// Apply 用客户端下发的设置覆盖当前配置。
//
// 客户端通常只下发用户显式修改的键，因此用“非零值覆盖”的合并策略。
func (m *Manager) Apply(raw json.RawMessage) error {
	var s Settings
	if err := json.Unmarshal(raw, &s); err != nil {
		return fmt.Errorf("解析客户端设置: %w", err)
	}
	m.merge(&s)
	return nil
}

// merge 把非零值合并进当前配置，并校验取值。
func (m *Manager) merge(s *Settings) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if s.GeneroVersion != "" {
		m.cur.GeneroVersion = s.GeneroVersion
	}
	if s.LogLevel != "" {
		m.cur.LogLevel = s.LogLevel
	}
	if s.Limits.MaxFileBytes > 0 {
		m.cur.Limits.MaxFileBytes = s.Limits.MaxFileBytes
	}
	if s.Limits.MaxFileLines > 0 {
		m.cur.Limits.MaxFileLines = s.Limits.MaxFileLines
	}
	if s.Limits.MaxIssues > 0 {
		m.cur.Limits.MaxIssues = s.Limits.MaxIssues
	}
	if s.Limits.ParseTimeoutMS > 0 {
		m.cur.Limits.ParseTimeoutMS = s.Limits.ParseTimeoutMS
	}

	// Globals（P3a）
	// SearchPaths 用 nil 区分“未下发”与“显式清空”：
	// 客户端只发用户改过的键，JSON 省略数组时为 nil。
	if s.Globals.SearchPaths != nil {
		m.cur.Globals.SearchPaths = s.Globals.SearchPaths
	}

	// P3b：字段校验与表单根（PerRoots 同样用 nil 区分未下发与清空）
	if s.PerRoots != nil {
		m.cur.PerRoots = s.PerRoots
	}

	// Diagnostics 按诊断码覆盖级别：每次下发都是完整对象，空对象表示
	// “清掉个人覆盖”，因此以项目文件里的覆盖为基底整体重建，而不是在
	// 当前值上合并。nil 仍表示未下发、保留现有值。
	if s.Diagnostics != nil {
		next := make(map[string]DiagnosticLevel, len(m.fileDiagnostics)+len(s.Diagnostics))
		for code, lv := range m.fileDiagnostics {
			next[code] = lv
		}
		for code, lv := range s.Diagnostics {
			next[code] = lv
		}
		m.cur.Diagnostics = next
	}

	// P7-D：链接资料
	if s.Index.Enable != nil {
		v := *s.Index.Enable
		m.cur.Index.Enable = &v
	}
	if s.Schema.Enable != nil {
		v := *s.Schema.Enable
		m.cur.Schema.Enable = &v
	}
	if len(s.Schema.Paths) > 0 {
		m.cur.Schema.Paths = append([]string(nil), s.Schema.Paths...)
	}
	if s.Link.Enable != nil {
		v := *s.Link.Enable
		m.cur.Link.Enable = &v
	}
	if s.Link.DataDir != "" {
		m.cur.Link.DataDir = s.Link.DataDir
	}
	if s.Link.ModuleRoots != nil {
		m.cur.Link.ModuleRoots = s.Link.ModuleRoots
	}
	if s.Link.GlobalModules != nil {
		m.cur.Link.GlobalModules = s.Link.GlobalModules
	}
	if s.Link.MaxAgeDays > 0 {
		m.cur.Link.MaxAgeDays = s.Link.MaxAgeDays
	}
	if s.Builtins.Enable != nil {
		v := *s.Builtins.Enable
		m.cur.Builtins.Enable = &v
	}
	if s.Builtins.Paths != nil {
		m.cur.Builtins.Paths = s.Builtins.Paths
	}
	if s.Builtins.Disable != nil {
		m.cur.Builtins.Disable = s.Builtins.Disable
	}

	// P8：忽略列表（nil 区分未下发与显式清空）
	if s.Ignore.Files != nil {
		m.cur.Ignore.Files = append([]string(nil), s.Ignore.Files...)
	}
	if s.Ignore.Fields != nil {
		m.cur.Ignore.Fields = append([]string(nil), s.Ignore.Fields...)
	}
	if s.Ignore.Forms != nil {
		m.cur.Ignore.Forms = append([]string(nil), s.Ignore.Forms...)
	}

	// P4：格式化
	if s.Format.Keywords != "" {
		m.cur.Format.Keywords = s.Format.Keywords
	}
	if s.Format.IndentStyle != "" {
		m.cur.Format.IndentStyle = s.Format.IndentStyle
	}
	if s.Format.IndentSize > 0 {
		m.cur.Format.IndentSize = s.Format.IndentSize
	}
}

// Get 返回当前配置的快照。
func (m *Manager) Get() Settings {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.cur
}

// Path 返回工作区配置文件路径（可能为空）。
func (m *Manager) Path() string {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.path
}

// Validate 检查配置取值是否合法，返回所有问题。
func (s Settings) Validate() []string {
	var problems []string

	switch s.GeneroVersion {
	case Genero25, Genero30, Genero40:
	default:
		problems = append(problems, fmt.Sprintf("未知 generoVersion %q（支持 2.50/3.00/4.00）", s.GeneroVersion))
	}

	// 逐码覆盖：只校验级别取值。码是否已注册由 internal/diag 校验
	// （config 不能反过来 import diag，会形成循环依赖）。
	for code, lvl := range s.Diagnostics {
		switch lvl {
		case LevelOff, LevelError, LevelWarning, LevelInfo, LevelHint:
		default:
			problems = append(problems, fmt.Sprintf("未知 diagnostics[%q] 级别 %q（支持 off/error/warning/information/hint）", code, lvl))
		}
	}

	for i, p := range s.Link.ModuleRoots {
		if strings.TrimSpace(p) == "" {
			problems = append(problems, fmt.Sprintf("link.moduleRoots[%d] 为空", i))
		}
	}
	for i, p := range s.Link.GlobalModules {
		if strings.TrimSpace(p) == "" {
			problems = append(problems, fmt.Sprintf("link.globalModules[%d] 为空", i))
		}
	}
	if s.Link.MaxAgeDays < 0 {
		problems = append(problems, fmt.Sprintf("link.maxAgeDays 不能为负（%d）", s.Link.MaxAgeDays))
	}

	for i, p := range s.Builtins.Paths {
		if strings.TrimSpace(p) == "" {
			problems = append(problems, fmt.Sprintf("builtins.paths[%d] 为空", i))
		}
	}
	for i, n := range s.Builtins.Disable {
		if strings.TrimSpace(n) == "" {
			problems = append(problems, fmt.Sprintf("builtins.disable[%d] 为空", i))
		}
	}

	switch s.Format.Keywords {
	case "", "unchanged", "upper", "lower":
	default:
		problems = append(problems, fmt.Sprintf("未知 format.keywords %q（支持 unchanged/upper/lower）", s.Format.Keywords))
	}
	switch s.Format.IndentStyle {
	case "", "auto", "tab", "space":
	default:
		problems = append(problems, fmt.Sprintf("未知 format.indentStyle %q（支持 auto/tab/space）", s.Format.IndentStyle))
	}
	if s.Format.IndentSize < 0 {
		problems = append(problems, "format.indentSize 不能为负数")
	}
	if s.Format.IndentSize > 16 {
		problems = append(problems, "format.indentSize 过大（上限 16）")
	}

	if s.Limits.MaxIssues <= 0 {
		problems = append(problems, "limits.maxIssues 必须为正数")
	}
	if s.Limits.MaxFileBytes <= 0 {
		problems = append(problems, "limits.maxFileBytes 必须为正数")
	}
	if s.Limits.ParseTimeoutMS <= 0 {
		problems = append(problems, "limits.parseTimeoutMs 必须为正数")
	}

	for i, p := range s.Link.ModuleRoots {
		if strings.TrimSpace(p) == "" {
			problems = append(problems, fmt.Sprintf("link.moduleRoots[%d] 为空", i))
		}
	}
	for i, p := range s.Link.GlobalModules {
		if strings.TrimSpace(p) == "" {
			problems = append(problems, fmt.Sprintf("link.globalModules[%d] 为空", i))
		}
	}
	if s.Link.MaxAgeDays < 0 {
		problems = append(problems, fmt.Sprintf("link.maxAgeDays 不能为负（%d）", s.Link.MaxAgeDays))
	}

	for i, p := range s.Builtins.Paths {
		if strings.TrimSpace(p) == "" {
			problems = append(problems, fmt.Sprintf("builtins.paths[%d] 为空", i))
		}
	}
	for i, n := range s.Builtins.Disable {
		if strings.TrimSpace(n) == "" {
			problems = append(problems, fmt.Sprintf("builtins.disable[%d] 为空", i))
		}
	}

	// P8：忽略列表的 glob 语法。写错不是致命错误（该条永不匹配），
	// 但必须让用户看到日志，否则会以为"忽略没生效"。
	problems = append(problems, glob.Validate("ignore.files", s.Ignore.Files)...)
	problems = append(problems, glob.Validate("ignore.fields", s.Ignore.Fields)...)
	problems = append(problems, glob.Validate("ignore.forms", s.Ignore.Forms)...)
	return problems
}
