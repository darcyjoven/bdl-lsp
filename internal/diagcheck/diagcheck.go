// Package diagcheck 校验"诊断码"在三个地方保持一致（P7-B 收尾）。
//
// 一个诊断码同时出现在三处，三处漂移的后果各不相同：
//
//  1. Go 注册表（`internal/diag`）——事实来源，决定有没有这个码、默认级别；
//  2. `docs/DIAGNOSTICS.md`——用户查"这个码是什么意思、怎么关"的地方；
//     手写文档必然滞后（加码时忘了写），所以这里**生成**它；
//  3. VSCode `package.json` 的设置项——决定用户在设置界面能看到什么、
//     默认值与服务端是否一致。`bdl.diagnostics` 的级别枚举写错一个词，
//     用户填进去的值就会被当作未知级别（服务端按 error 处理，反而更显眼）。
//
// 只校验"能机械判定"的部分：码集合、默认级别、级别枚举。措辞、说明文字
// 的好坏不校验（那是人的事）。
package diagcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/config"
	"github.com/darcyjoven/bdl-lsp/internal/diag"
)

// LegacyKeys 是"按码细分"之前的老配置键，它们仍要生效（至少保留两个版本），
// 所以每个老键必须与它覆盖的诊断码共用同一个默认级别——
// 否则同一个码的默认级别会在"老键路径"和"注册表路径"下不同，用户看到的行为
// 取决于走了哪条路。这是本轮校验里最容易被忽视的一条。
//
// 值是该老键覆盖的诊断码；同一组内的码默认级别必须一致。
var LegacyKeys = map[string][]diag.Code{
	"bdl.syntaxDiagnostics":                   {diag.CodeSyntaxError, diag.CodeMissingNode},
	"bdl.missingDiagnostics":                  {diag.CodeMissingNode},
	"bdl.fieldDiagnostics":                    {diag.CodeFieldNotInForm, diag.CodeFormDuplicateItem, diag.CodeFormLayoutTagUndefined, diag.CodeFormScreenRecordUndefined},
	"bdl.recordMemberDiagnostics":             {diag.CodeRecordMemberUnknown},
	"bdl.globals.fileNotFoundDiagnostics":     {diag.CodeGlobalsFileNotFound},
	"bdl.globals.unresolvedSourceDiagnostics": {diag.CodeVariableSourceUnresolved},
}

// AllLevels 是合法的级别取值（与 config 包的常量一致）。
func AllLevels() []string {
	return []string{
		string(config.LevelOff),
		string(config.LevelError),
		string(config.LevelWarning),
		string(config.LevelInfo),
		string(config.LevelHint),
	}
}

// Problem 是一条不一致。
type Problem struct {
	// Source 是出问题的地方（"docs/DIAGNOSTICS.md"、"package.json" 等）。
	Source string
	Detail string
}

func (p Problem) String() string { return p.Source + ": " + p.Detail }

// Doc 生成 `docs/DIAGNOSTICS.md` 的全文。
//
// 生成而不是手写：码表在 Go 里，文档跟着码表走，`make diagnostics-check`
// 逐字比对，改了一个码没改文档就会失败——人不会记得改，工具才会。
func Doc(specs []diag.Spec) string {
	var b strings.Builder
	b.WriteString("# 诊断码清单\n\n")
	b.WriteString("> 本文件由 `make diagnostics-write` 生成（来源：`internal/diag` 注册表），**不要手改**。\n")
	b.WriteString("> 新增或调整诊断码后运行 `make diagnostics-write` 重新生成，再提交。\n\n")
	b.WriteString("诊断码出现在 LSP `Diagnostic.code` 字段里，也是 `bdl.diagnostics` 设置项的键：\n\n")
	b.WriteString("```json\n{\n  \"bdl.diagnostics\": {\n    \"field-not-in-form\": \"warning\",\n    \"record-member-unknown\": \"off\"\n  }\n}\n```\n\n")
	b.WriteString("级别取值：`off`（关闭）、`error`（红）、`warning`（黄）、`information`（灰）、`hint`（淡灰）。\n")
	b.WriteString("优先级：`bdl.diagnostics.<码>` > 老专用键（见下表）> 下表默认值。\n\n")

	b.WriteString("| 诊断码 | 默认级别 | 说明 |\n")
	b.WriteString("|---|---|---|\n")
	for _, sp := range specs {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", sp.Code, sp.Level, sp.Summary)
	}

	b.WriteString("\n## 老配置键（保留至少两个版本，仍生效）\n\n")
	b.WriteString("| 老键 | 覆盖的诊断码 | 默认级别 |\n")
	b.WriteString("|---|---|---|\n")
	keys := make([]string, 0, len(LegacyKeys))
	for k := range LegacyKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	levelOf := map[diag.Code]config.DiagnosticLevel{}
	for _, sp := range specs {
		levelOf[sp.Code] = sp.Level
	}
	for _, k := range keys {
		codes := LegacyKeys[k]
		names := make([]string, 0, len(codes))
		for _, c := range codes {
			names = append(names, "`"+string(c)+"`")
		}
		fmt.Fprintf(&b, "| `%s` | %s | `%s` |\n", k, strings.Join(names, "、"), levelOf[codes[0]])
	}

	b.WriteString("\n## 动态诊断码\n\n")
	b.WriteString("`missing-<kind>` 是**动态码**（`kind` 来自 grammar 节点类型，数量随语法版本变化，无法穷举）：\n")
	b.WriteString("`bdl.diagnostics.missing-<kind>` 精确命中单个类型，`missing-node` 作为整簇兜底。\n")
	b.WriteString("`IsRegistered` 对 `missing-` 前缀一律放行，否则拼写检查会把合法动态码全报成未知。\n")
	return b.String()
}

// CheckDoc 校验现有文档与生成结果逐字一致；不一致返回空（需要重新生成）。
func CheckDoc(path string, specs []diag.Spec) []Problem {
	current, err := os.ReadFile(path)
	if err != nil {
		return []Problem{{Source: path, Detail: fmt.Sprintf("读不到（运行 make diagnostics-write 生成）：%v", err)}}
	}
	if string(current) == Doc(specs) {
		return nil
	}
	return []Problem{{Source: path, Detail: "内容与注册表不一致（运行 make diagnostics-write 重新生成）"}}
}

// vscodeConfig 是 package.json 里我们关心的部分。
type vscodeConfig struct {
	Contributes struct {
		Configuration struct {
			Properties map[string]struct {
				Type    string   `json:"type"`
				Default any      `json:"default"`
				Enum    []string `json:"enum"`
			} `json:"properties"`
		} `json:"configuration"`
	} `json:"contributes"`
}

// CheckVSCode 校验 VSCode `package.json` 的设置项。
//
// 校验三件事：
//  1. `bdl.diagnostics` 的级别枚举 = 合法级别集合；
//  2. 每个老键都存在、枚举 = 合法级别集合、`default` = 注册表默认级别；
//  3. 同一老键覆盖的多个码默认级别一致（否则"默认值"是二义的）。
func CheckVSCode(path string, specs []diag.Spec) ([]Problem, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg vscodeConfig
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	props := pkg.Contributes.Configuration.Properties
	var probs []Problem
	add := func(format string, args ...any) {
		probs = append(probs, Problem{Source: path, Detail: fmt.Sprintf(format, args...)})
	}

	levelOf := map[diag.Code]config.DiagnosticLevel{}
	for _, sp := range specs {
		levelOf[sp.Code] = sp.Level
	}
	wantLevels := AllLevels()

	// 1) 新键 bdl.diagnostics 的级别枚举
	if p, ok := props["bdl.diagnostics"]; ok {
		if p.Type != "object" {
			add("bdl.diagnostics 的 type = %q，期望 object", p.Type)
		}
		if !sameStrings(p.Enum, wantLevels) && len(p.Enum) > 0 {
			add("bdl.diagnostics 的 enum = %v，期望 %v", p.Enum, wantLevels)
		}
	} else {
		add("缺少设置项 bdl.diagnostics（按码单独设级别的入口）")
	}

	// 2/3) 老键：存在、枚举、默认值、组内一致
	keys := make([]string, 0, len(LegacyKeys))
	for k := range LegacyKeys {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		codes := LegacyKeys[k]
		want := levelOf[codes[0]]
		for _, c := range codes {
			if levelOf[c] != want {
				add("%s 覆盖的 %s 默认级别是 %q，与同组的 %s(%q) 不一致——老键的默认值会变成二义的",
					k, c, levelOf[c], codes[0], want)
			}
		}
		p, ok := props[k]
		if !ok {
			add("缺少老配置键 %s（覆盖 %s；老键至少保留两个版本）", k, codeList(codes))
			continue
		}
		if !sameStrings(p.Enum, wantLevels) {
			add("%s 的 enum = %v，期望 %v", k, p.Enum, wantLevels)
		}
		if got, _ := p.Default.(string); got != string(want) {
			add("%s 的 default = %q，注册表默认级别是 %q", k, got, want)
		}
	}
	return probs, nil
}

// CheckRegistry 校验注册表自身的完整性（码必须有说明）。
func CheckRegistry(specs []diag.Spec) []Problem {
	var probs []Problem
	seen := map[diag.Code]bool{}
	for _, sp := range specs {
		if seen[sp.Code] {
			probs = append(probs, Problem{Source: "internal/diag", Detail: "重复注册的码: " + string(sp.Code)})
		}
		seen[sp.Code] = true
		if strings.TrimSpace(sp.Summary) == "" {
			probs = append(probs, Problem{Source: "internal/diag", Detail: "码 " + string(sp.Code) + " 没有说明文字（summaries 里补一条）"})
		}
		if sp.Level == "" {
			probs = append(probs, Problem{Source: "internal/diag", Detail: "码 " + string(sp.Code) + " 没有默认级别"})
		}
	}
	return probs
}

func codeList(codes []diag.Code) string {
	names := make([]string, 0, len(codes))
	for _, c := range codes {
		names = append(names, string(c))
	}
	return strings.Join(names, "、")
}

// sameStrings 报告两个字符串切片是否同元素同顺序（级别列表的顺序即设置界面的顺序）。
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
