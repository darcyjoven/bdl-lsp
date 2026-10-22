# 诊断码清单

> 本文件由 `make diagnostics-write` 生成（来源：`internal/diag` 注册表），**不要手改**。
> 新增或调整诊断码后运行 `make diagnostics-write` 重新生成，再提交。

诊断码出现在 LSP `Diagnostic.code` 字段里，也是 `bdl.diagnostics` 设置项的键：

```json
{
  "bdl.diagnostics": {
    "field-not-in-form": "warning",
    "record-member-unknown": "off"
  }
}
```

级别取值：`off`（关闭）、`error`（红）、`warning`（黄）、`information`（灰）、`hint`（淡灰）。
`bdl.diagnostics` 未列出的码使用下表默认值；设置项是**唯一**入口，旧专用键已移除。

| 诊断码 | 默认级别 | 说明 |
|---|---|---|
| `field-not-in-form` | `warning` | `INPUT … FROM`、`AFTER FIELD` 等引用的字段不在表单 `.per` 里（表单未同步或字段名写错） |
| `file-too-large` | `information` | 文件超过解析上限（`bdl.limits.maxFileBytes` / `maxFileLines`），已跳过语法检查 |
| `form-duplicate-item` | `warning` | 表单 `ATTRIBUTES` 里重复定义了同名 item |
| `form-layout-tag-undefined` | `warning` | 表单 `LAYOUT` 里引用了 `ATTRIBUTES` 未定义的 item tag |
| `form-screen-record-undefined` | `warning` | 表单 `SCREEN RECORD` 引用了未定义的 item 或字段名 |
| `function-not-found` | `warning` | 调用处的函数名在当前模块及可用模组中找不到定义（需链接资料） |
| `globals-file-not-found` | `warning` | `GLOBALS "路径"` 的目标文件找不到（路径写错或文件未随代码分发） |
| `link-data-invalid` | `warning` | 链接资料格式不合法（JSONL 解析失败、schema 版本过高） |
| `link-data-stale` | `hint` | 链接资料超过 `bdl.link.maxAgeDays` 未更新 |
| `missing-node` | `warning` | grammar 的 MISSING 节点整簇兜底码（缺少 END IF、缺少逗号等） |
| `record-member-unknown` | `off` | `BY NAME rec.member` 的成员不在 RECORD 声明里；`RECORD LIKE t.*` 用 `.sch` 展开（默认关） |
| `syntax-error` | `warning` | grammar 的 ERROR 节点（无法解析的片段） |
| `variable-source-unresolved` | `warning` | 变量使用处查不到任何定义（局部 / 模块级 / GLOBALS 引入均无） |

## 动态诊断码

`missing-<kind>` 是**动态码**（`kind` 来自 grammar 节点类型，数量随语法版本变化，无法穷举）：
`bdl.diagnostics.missing-<kind>` 精确命中单个类型，`missing-node` 作为整簇兜底。
`IsRegistered` 对 `missing-` 前缀一律放行，否则拼写检查会把合法动态码全报成未知。
