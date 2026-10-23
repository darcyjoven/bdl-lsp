// Package diag 定义诊断码集中注册表与级别统一解析。
//
// 为什么需要它：诊断码此前散落在四个文件里（globals.go / form.go /
// recordname.go / diagnostics.go），级别各自由不同的配置键决定，既无法
// 逐码开关，也无法在配置里写错码时给出提示。本包把三者收拢：
//
//   - 所有语义诊断码集中注册，带出厂默认级别；
//   - 级别解析优先级：`bdl.diagnostics.<code>` > 注册表默认；
//   - 未注册的码会在 config 校验时记 warn（拼错保护）。
//
// 语法诊断有两个“簇”的码：
//   - `syntax-error` 固定；
//   - MISSING 节点的码是动态的 `missing-<kind>`（kind 来自 grammar 节点类型，
//     数量随语法版本变化，无法穷举）。
//
// 因此 MISSING 支持两级覆盖：`bdl.diagnostics.missing-<kind>` 精确命中单个
// 类型，`bdl.diagnostics.missing-node` 作为整簇兜底；两者都缺时用
// 注册表默认。`IsRegistered` 对 `missing-` 前缀一律放行，
// 否则拼写检查会把合法的动态码全报成未知。
package diag

import (
	"fmt"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/config"
	"go.lsp.dev/protocol"
)

// Code 是诊断码的类型别名（对外一律以字符串形式出现在 LSP 的 Diagnostic.Code）。
type Code string

// 语义诊断码（已注册，可用 `bdl.diagnostics.<code>` 单独设级别）。
const (
	// CodeGlobalsFileNotFound `GLOBALS "路径"` 的目标文件找不到。
	CodeGlobalsFileNotFound Code = "globals-file-not-found"
	// CodeVariableSourceUnresolved 变量使用处查不到任何定义（T5）。
	CodeVariableSourceUnresolved Code = "variable-source-unresolved"
	// CodeFieldNotInForm `INPUT ... FROM f` 等处的表单字段在 .per 里不存在。
	CodeFieldNotInForm Code = "field-not-in-form"
	// CodeFormDuplicateItem 表单 ATTRIBUTES 里重复定义 item。
	CodeFormDuplicateItem Code = "form-duplicate-item"
	// CodeFormLayoutTagUndefined LAYOUT 里引用了 ATTRIBUTES 未定义的 item tag。
	CodeFormLayoutTagUndefined Code = "form-layout-tag-undefined"
	// CodeFormScreenRecordUndefined .per 的 SCREEN RECORD 引用了未定义的 item。
	CodeFormScreenRecordUndefined Code = "form-screen-record-undefined"
	// CodeRecordMemberUnknown `BY NAME rec.member` 的成员不在 RECORD 声明里。
	CodeRecordMemberUnknown Code = "record-member-unknown"
	// CodeFileTooLarge 文件超过解析上限，已跳过语法检查。
	CodeFileTooLarge Code = "file-too-large"
	// CodeFunctionNotFound 调用处的函数名在当前模块及可用模组中找不到定义。
	//
	// 依赖链接资料（P7-D）：没有 `.link/links.jsonl` 时整项**不启用**，
	// 因为可见集不完整时报告就是误报。
	CodeFunctionNotFound Code = "function-not-found"
	// CodeLinkDataInvalid 链接资料格式不合法（JSONL 解析失败、schema 过高）。
	CodeLinkDataInvalid Code = "link-data-invalid"
	// CodeLinkDataStale 链接资料超过 maxAgeDays 未更新。
	CodeLinkDataStale Code = "link-data-stale"
	// CodeSyntaxError grammar 的 ERROR 节点（语法错误）。
	CodeSyntaxError Code = "syntax-error"
	// CodeMissingNode grammar 的 MISSING 节点整簇兜底码。
	// 精确到具体类型用 `missing-<kind>`（动态码）。
	CodeMissingNode Code = "missing-node"
)

// defaultLevel 是各诊断码的出厂默认级别。
//
// 取值依据（Q10 决策：默认全部 warning，下列两项例外）：
//   - record-member-unknown：记录类型可能来自 `LIKE table.*`，静态不可知，
//     实测误报率高，默认 off，等 P7-D 链接资料补齐类型信息后再评估；
//   - function-not-found：P7-C 内置符号表 + P7-D 链接资料 + P7-D-2 外部
//     函数过滤全部落地后，全量语料只剩 1358 条（0.09 条/文件，见
//     全量语料实测），噪声已可接受，默认改为 warning。
//     没有链接资料时整项**不产生任何诊断**，所以对未启用的项目零影响；
//   - file-too-large 是提示不是错误，默认 information。
var defaultLevel = map[Code]config.DiagnosticLevel{
	CodeGlobalsFileNotFound:       config.LevelWarning,
	CodeVariableSourceUnresolved:  config.LevelWarning,
	CodeFieldNotInForm:            config.LevelWarning,
	CodeFormDuplicateItem:         config.LevelWarning,
	CodeFormLayoutTagUndefined:    config.LevelWarning,
	CodeFormScreenRecordUndefined: config.LevelWarning,
	CodeRecordMemberUnknown:       config.LevelOff,
	CodeFileTooLarge:              config.LevelInfo,
	CodeFunctionNotFound:          config.LevelWarning,
	CodeLinkDataInvalid:           config.LevelWarning,
	// 过期只是"温和提示"，不该像错误一样占满 Problems。
	CodeLinkDataStale: config.LevelHint,
	// Q10 定稿：语法错误默认也是 warning（不是 error）。
	CodeSyntaxError: config.LevelWarning,
	CodeMissingNode: config.LevelWarning,
}

// summaries 是各诊断码的一句话说明（对外文档 `docs/DIAGNOSTICS.md` 与
// `make diagnostics-check` 的唯一来源）。
//
// 放在这里而不是文档里手写：码表是 Go 定义的，文档一旦手写就会漂——
// 加了一个码忘了写文档，用户看到的 Problems 里就多出一个查不到的码。
var summaries = map[Code]string{
	CodeGlobalsFileNotFound:       "`GLOBALS \"路径\"` 的目标文件找不到（路径写错或文件未随代码分发）",
	CodeVariableSourceUnresolved:  "变量使用处查不到任何定义（局部 / 模块级 / GLOBALS 引入均无）",
	CodeFieldNotInForm:            "`INPUT … FROM`、`AFTER FIELD` 等引用的字段不在表单 `.per` 里（表单未同步或字段名写错）",
	CodeFormDuplicateItem:         "表单 `ATTRIBUTES` 里重复定义了同名 item",
	CodeFormLayoutTagUndefined:    "表单 `LAYOUT` 里引用了 `ATTRIBUTES` 未定义的 item tag",
	CodeFormScreenRecordUndefined: "表单 `SCREEN RECORD` 引用了未定义的 item 或字段名",
	CodeRecordMemberUnknown:       "`BY NAME rec.member` 的成员不在 RECORD 声明里；`RECORD LIKE t.*` 用 `.sch` 展开（默认关）",
	CodeFileTooLarge:              "文件超过解析上限（`bdl.limits.maxFileBytes` / `maxFileLines`），已跳过语法检查",
	CodeFunctionNotFound:          "调用处的函数名在当前模块及可用模组中找不到定义（需链接资料）",
	CodeLinkDataInvalid:           "链接资料格式不合法（JSONL 解析失败、schema 版本过高）",
	CodeLinkDataStale:             "链接资料超过 `bdl.link.maxAgeDays` 未更新",
	CodeSyntaxError:               "grammar 的 ERROR 节点（无法解析的片段）",
	CodeMissingNode:               "grammar 的 MISSING 节点整簇兜底码（缺少 END IF、缺少逗号等）",
}

// Spec 是一个诊断码的完整定义。
type Spec struct {
	Code    Code
	Level   config.DiagnosticLevel
	Summary string
}

// Specs 返回全部已注册诊断码的定义（按码字典序）。
//
// 刻意不导出 summary 与 defaultLevel 两个 map：一处定义、一处读取，
// 调用方拿到的顺序与内容都稳定，文档生成与漂移校验才能逐字比对。
func Specs() []Spec {
	codes := Registered()
	out := make([]Spec, 0, len(codes))
	for _, c := range codes {
		out = append(out, Spec{Code: c, Level: DefaultLevel(c), Summary: summaries[c]})
	}
	return out
}

// SeverityFor 依据诊断码与配置决定诊断级别；ok=false 表示该诊断被关闭。
//
// 优先级：settings.Diagnostics[code]（`bdl.diagnostics.<code>`）> 注册表默认。
//
// settings 按值传入是有意的：调用方在一次诊断流程里只取一次 settings，
// 避免逐条诊断重复加锁读取配置。
func SeverityFor(code Code, settings config.Settings) (protocol.DiagnosticSeverity, bool) {
	level := DefaultLevel(code)
	if lv, ok := settings.Diagnostics[string(code)]; ok && lv != "" {
		level = lv
	}
	return ToSeverity(level)
}

// Enabled 报告某诊断码是否开启（不看语法，只回答“会不会发”）。
func Enabled(code Code, settings config.Settings) bool {
	_, ok := SeverityFor(code, settings)
	return ok
}

// ToSeverity 把配置里的级别字符串转换为 LSP 枚举；ok=false 表示已关闭。
//
// 这是全仓库唯一的“级别→LSP 枚举”转换点。历史上 server 与 diag 各写一份
// switch，`information` 写错成 `LevelInformation` 就是两处漂移的典型。
func ToSeverity(level config.DiagnosticLevel) (protocol.DiagnosticSeverity, bool) {
	switch level {
	case config.LevelOff:
		return 0, false
	case config.LevelError:
		return protocol.DiagnosticSeverityError, true
	case config.LevelWarning:
		return protocol.DiagnosticSeverityWarning, true
	case config.LevelInfo:
		return protocol.DiagnosticSeverityInformation, true
	case config.LevelHint:
		return protocol.DiagnosticSeverityHint, true
	default:
		// 未知级别按 error 处理：配置写错时宁可显眼，不要静默降级。
		return protocol.DiagnosticSeverityError, true
	}
}

// Registered 返回所有已注册的诊断码（按字典序，便于输出与测试比对）。
func Registered() []Code {
	codes := make([]Code, 0, len(defaultLevel))
	for c := range defaultLevel {
		codes = append(codes, c)
	}
	sort.Slice(codes, func(i, j int) bool { return codes[i] < codes[j] })
	return codes
}

// DefaultLevel 返回诊断码的出厂默认级别；未注册返回 warning。
func DefaultLevel(code Code) config.DiagnosticLevel {
	if lv, ok := defaultLevel[code]; ok {
		return lv
	}
	return config.LevelWarning
}

// IsRegistered 报告 code 是否已注册。
//
// `missing-<kind>` 是动态码（kind 来自 grammar 节点类型，无法穷举），
// 一律视为已注册；它们没有独立默认值，解析时落到 missing-node。
func IsRegistered(code Code) bool {
	if _, ok := defaultLevel[code]; ok {
		return true
	}
	return strings.HasPrefix(string(code), "missing-")
}

// String 返回 Code 的字符串形式。
func (c Code) String() string {
	return string(c)
}

// EnsureRegistered 检查 code 是否在注册表中；未注册时返回错误（拼错保护）。
func EnsureRegistered(code Code) error {
	if !IsRegistered(code) {
		return fmt.Errorf("未注册的诊断码: %q", code)
	}
	return nil
}

// ValidateSettings 校验配置里出现的诊断码是否都已注册。
//
// 返回按字典序排列的未知码。调用方（config 合并后 / server 初始化）
// 据此记一条 warn 日志——比静默忽略一个拼错的键有用得多。
func ValidateSettings(settings config.Settings) []string {
	var unknown []string
	for code := range settings.Diagnostics {
		if !IsRegistered(Code(code)) {
			unknown = append(unknown, code)
		}
	}
	sort.Strings(unknown)
	return unknown
}
