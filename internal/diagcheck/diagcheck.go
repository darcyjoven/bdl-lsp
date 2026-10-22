// Package diagcheck 校验"诊断码"在三个地方保持一致（P7-B 收尾）。
//
// 一个诊断码同时出现在三处，三处漂移的后果各不相同：
//
//  1. Go 注册表（`internal/diag`）——事实来源，决定有没有这个码、默认级别；
//  2. `docs/DIAGNOSTICS.md`——用户查"这个码是什么意思、怎么关"的地方；
//     手写文档必然滞后（加码时忘了写），所以这里**生成**它；
//  3. VSCode `package.json` 的设置项——确保 `bdl.diagnostics` 存在、
//     类型为 object、级别枚举合法。枚举写错一个词，用户填进去的值就会
//     被当作未知级别（服务端按 error 处理，反而更显眼）。
//
// 只校验"能机械判定"的部分：码集合、默认级别、级别枚举。措辞、说明文字
// 的好坏不校验（那是人的事）。
package diagcheck

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/darcyjoven/bdl-lsp/internal/config"
	"github.com/darcyjoven/bdl-lsp/internal/diag"
)

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
	b.WriteString("`bdl.diagnostics` 未列出的码使用下表默认值；设置项是**唯一**入口，旧专用键已移除。\n\n")

	b.WriteString("| 诊断码 | 默认级别 | 说明 |\n")
	b.WriteString("|---|---|---|\n")
	for _, sp := range specs {
		fmt.Fprintf(&b, "| `%s` | `%s` | %s |\n", sp.Code, sp.Level, sp.Summary)
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
//
// contributes.configuration 有两种形态：单个对象（旧）或对象数组（分节，
// CSS 语言插件那种多个 title 的层次）。两种都要能读：分节是现在的形态，
// 单对象保留兼容，校验逻辑不必关心设置属于哪一节。
type vscodeConfig struct {
	Contributes struct {
		Configuration json.RawMessage `json:"configuration"`
	} `json:"contributes"`
}

// vscodeProp 是单个设置项里我们关心的字段。
type vscodeProp struct {
	Type                 string   `json:"type"`
	Default              any      `json:"default"`
	Enum                 []string `json:"enum"`
	AdditionalProperties struct {
		Enum []string `json:"enum"`
	} `json:"additionalProperties"`
}

// vscodeConfigSection 是 contributes.configuration 的一种形态。
type vscodeConfigSection struct {
	Properties map[string]vscodeProp `json:"properties"`
}

// vscodeConfigurationProps 把单对象/数组两种形态的 properties 合并出来。
func vscodeConfigurationProps(raw json.RawMessage) (map[string]vscodeProp, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	var sections []vscodeConfigSection
	if err := json.Unmarshal(raw, &sections); err == nil {
		props := map[string]vscodeProp{}
		for _, sec := range sections {
			for k, v := range sec.Properties {
				props[k] = v
			}
		}
		return props, nil
	}
	var single vscodeConfigSection
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("configuration 既不是对象也不是数组: %w", err)
	}
	return single.Properties, nil
}

// CheckVSCode 校验 VSCode `package.json` 的设置项。
//
// 校验两件事：
//  1. `bdl.diagnostics` 存在且 type=object；
//  2. 级别枚举 = 合法级别集合。
//
// 旧专用键已移除；这里刻意不再校验它们，避免把"已删的键"重新固化。
func CheckVSCode(path string, specs []diag.Spec) ([]Problem, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pkg vscodeConfig
	if err := json.Unmarshal(raw, &pkg); err != nil {
		return nil, fmt.Errorf("解析 %s: %w", path, err)
	}
	props, err := vscodeConfigurationProps(pkg.Contributes.Configuration)
	if err != nil {
		return nil, fmt.Errorf("解析 %s 的 configuration: %w", path, err)
	}
	var probs []Problem
	add := func(format string, args ...any) {
		probs = append(probs, Problem{Source: path, Detail: fmt.Sprintf(format, args...)})
	}

	wantLevels := AllLevels()

	// 1) bdl.diagnostics 的级别枚举
	if p, ok := props["bdl.diagnostics"]; ok {
		if p.Type != "object" {
			add("bdl.diagnostics 的 type = %q，期望 object", p.Type)
		}
		enum := p.Enum
		if len(enum) == 0 {
			enum = p.AdditionalProperties.Enum
		}
		if !sameStrings(enum, wantLevels) {
			add("bdl.diagnostics 的级别枚举 = %v，期望 %v", enum, wantLevels)
		}
	} else {
		add("缺少设置项 bdl.diagnostics（按码单独设级别的入口）")
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
