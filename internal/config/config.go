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
	// Limits 资源保护阈值。
	Limits Limits `json:"limits"`
	// LogLevel 日志级别。
	LogLevel string `json:"logLevel"`
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

// Default 返回默认配置。
func Default() Settings {
	return Settings{
		GeneroVersion:      Genero25,
		SyntaxDiagnostics:  LevelError,
		MissingDiagnostics: LevelWarning,
		LogLevel:           "info",
		Globals: GlobalsSettings{
			FileNotFoundDiagnostics:     LevelWarning,
			UnresolvedSourceDiagnostics: LevelWarning,
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

// uriToPath 把 file:// URI 转换为本地路径（最小实现，避免额外依赖）。
func uriToPath(uri string) (string, error) {
	const prefix = "file://"
	if !strings.HasPrefix(uri, prefix) {
		return "", fmt.Errorf("不是 file URI: %s", uri)
	}
	p := strings.TrimPrefix(uri, prefix)
	// Windows: file:///C:/x → /C:/x → C:/x
	if len(p) > 2 && p[0] == '/' && p[2] == ':' {
		p = p[1:]
	}
	// 百分号解码（仅处理常见字符）
	p = strings.ReplaceAll(p, "%20", " ")
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

	if s.Limits.MaxIssues <= 0 {
		problems = append(problems, "limits.maxIssues 必须为正数")
	}
	if s.Limits.MaxFileBytes <= 0 {
		problems = append(problems, "limits.maxFileBytes 必须为正数")
	}
	if s.Limits.ParseTimeoutMS <= 0 {
		problems = append(problems, "limits.parseTimeoutMs 必须为正数")
	}

	for name, lvl := range map[string]DiagnosticLevel{
		"globals.fileNotFoundDiagnostics":     s.Globals.FileNotFoundDiagnostics,
		"globals.unresolvedSourceDiagnostics": s.Globals.UnresolvedSourceDiagnostics,
	} {
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
