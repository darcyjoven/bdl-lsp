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
	// SyntaxDiagnostics 语法错误（ERROR 节点）的级别，默认 error。
	SyntaxDiagnostics DiagnosticLevel `json:"syntaxDiagnostics"`
	// MissingDiagnostics 缺失节点（MISSING）的级别，默认 warning。
	MissingDiagnostics DiagnosticLevel `json:"missingDiagnostics"`
	// Globals 是 GLOBALS 引入（P3a）相关的配置。
	Globals GlobalsSettings `json:"globals"`
	// RecordMemberDiagnostics 是 `BY NAME rec.member` 记录成员校验（P6）
	// 的诊断级别。**默认 off**：记录成员静态上只能靠 TYPE..END RECORD
	// 的字段列表，而语料里 `DEFINE r LIKE table.*`（按数据库列展开）
	// 很常见，硬校验会大面积误报。由用户开启并观察。
	RecordMemberDiagnostics DiagnosticLevel `json:"recordMemberDiagnostics"`

	// FieldDiagnostics 是 4GL 字段存在性校验（P3b）的诊断级别，
	// 默认 warning（Q3 决策：只在表单唯一解析成功时报告）。
	FieldDiagnostics DiagnosticLevel `json:"fieldDiagnostics"`
	// Diagnostics 是按诊断码单独覆盖级别的配置，键为诊断码（如
	// "variable-source-unresolved"）、值为级别（off/warning/error/...）。
	// 优先级：bdl.diagnostics.<code> > 旧专用配置键 > 注册表默认。
	Diagnostics map[string]DiagnosticLevel `json:"diagnostics"`
	// Builtins 是内置函数/变量符号表（P7-C）的配置。
	Builtins BuiltinsSettings `json:"builtins"`
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
	// FileNotFoundDiagnostics 是 `GLOBALS "路径"` 目标文件找不到时的级别，
	// 默认 warning。
	FileNotFoundDiagnostics DiagnosticLevel `json:"fileNotFoundDiagnostics"`
	// UnresolvedSourceDiagnostics 是变量使用处查不到任何定义时的级别，
	// 默认 warning。用户明确要求可关闭（设为 off）。
	UnresolvedSourceDiagnostics DiagnosticLevel `json:"unresolvedSourceDiagnostics"`
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

// Default 返回默认配置。
func Default() Settings {
	return Settings{
		GeneroVersion: Genero25,
		// Q10 定稿：语法错误默认 warning（旧键默认值同步，否则走旧键与走
		// 新键的用户行为不一致）。想要红波浪线可用 bdl.diagnostics.syntax-error。
		SyntaxDiagnostics:  LevelWarning,
		MissingDiagnostics: LevelWarning,
		LogLevel:           "info",
		Globals: GlobalsSettings{
			FileNotFoundDiagnostics:     LevelWarning,
			UnresolvedSourceDiagnostics: LevelWarning,
		},
		FieldDiagnostics:        LevelWarning,
		RecordMemberDiagnostics: LevelOff,
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
	if s.SyntaxDiagnostics != "" {
		m.cur.SyntaxDiagnostics = s.SyntaxDiagnostics
	}
	if s.MissingDiagnostics != "" {
		m.cur.MissingDiagnostics = s.MissingDiagnostics
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
	if s.Globals.FileNotFoundDiagnostics != "" {
		m.cur.Globals.FileNotFoundDiagnostics = s.Globals.FileNotFoundDiagnostics
	}
	if s.Globals.UnresolvedSourceDiagnostics != "" {
		m.cur.Globals.UnresolvedSourceDiagnostics = s.Globals.UnresolvedSourceDiagnostics
	}
	// SearchPaths 用 nil 区分“未下发”与“显式清空”：
	// 客户端只发用户改过的键，JSON 省略数组时为 nil。
	if s.Globals.SearchPaths != nil {
		m.cur.Globals.SearchPaths = s.Globals.SearchPaths
	}

	// P3b：字段校验与表单根（PerRoots 同样用 nil 区分未下发与清空）
	if s.RecordMemberDiagnostics != "" {
		m.cur.RecordMemberDiagnostics = s.RecordMemberDiagnostics
	}
	if s.FieldDiagnostics != "" {
		m.cur.FieldDiagnostics = s.FieldDiagnostics
	}
	if s.PerRoots != nil {
		m.cur.PerRoots = s.PerRoots
	}

	// Diagnostics 按诊断码单独覆盖级别（nil 表示未下发，保留现有值）。
	if s.Diagnostics != nil {
		if m.cur.Diagnostics == nil {
			m.cur.Diagnostics = make(map[string]DiagnosticLevel)
		}
		for code, lv := range s.Diagnostics {
			m.cur.Diagnostics[code] = lv
		}
	}

	// P7-C：内置符号表
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

	for name, lvl := range map[string]DiagnosticLevel{
		"syntaxDiagnostics":  s.SyntaxDiagnostics,
		"missingDiagnostics": s.MissingDiagnostics,
	} {
		switch lvl {
		case LevelOff, LevelError, LevelWarning, LevelInfo, LevelHint:
		default:
			problems = append(problems, fmt.Sprintf("未知 %s %q（支持 off/error/warning/information/hint）", name, lvl))
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

	for name, lvl := range map[string]DiagnosticLevel{
		"globals.fileNotFoundDiagnostics":     s.Globals.FileNotFoundDiagnostics,
		"globals.unresolvedSourceDiagnostics": s.Globals.UnresolvedSourceDiagnostics,
		"fieldDiagnostics":                    s.FieldDiagnostics,
	} {
		// （format 的取值校验在下面单独做）
		switch lvl {
		case LevelOff, LevelError, LevelWarning, LevelInfo, LevelHint:
		case "":
			problems = append(problems, fmt.Sprintf("%s 未设置（应取 off/error/warning/information/hint）", name))
		default:
			problems = append(problems, fmt.Sprintf("未知 %s %q（支持 off/error/warning/information/hint）", name, lvl))
		}
	}
	return problems
}
