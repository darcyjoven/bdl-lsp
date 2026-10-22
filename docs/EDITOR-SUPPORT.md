# 编辑器能力差异（VSCode / Zed）

> 更新：2026-10-22（P8）  
> 用途：说清楚同一套 `bdl-lsp` 在两个客户端里"哪些能力完整、哪些降级、
> 哪些是客户端本身不支持"，避免把客户端限制当成 LSP 缺陷。  
> 配套：[README](../README.md)、[FEATURES](FEATURES.md)、[DEPLOY-ZED](DEPLOY-ZED.md)、
> [TESTING](TESTING.md)、[VSCode 手册](https://github.com/darcyjoven/vscode-bdl/blob/main/docs/USER-GUIDE.md)、
> [Zed 手册](https://github.com/darcyjoven/zed-bdl/blob/main/docs/USER-GUIDE.md)。

一句话：**语言智能（诊断/悬停/跳转/补全/重命名/格式化）两边都可用；
UI 与命令类能力（链接资料视图、部署、语义高亮）只有 VSCode 完整，Zed 走替代路径。**

> **二进制分发差异（2026-10-23）**：VSCode 平台包已内置 `bdl-lsp`，安装即用；
> Zed 本轮不做内置，仍需按 README 自行准备二进制。

---

## 1. 能力矩阵

| 能力 | VSCode | Zed | 说明 |
|---|---|---|---|
| 语法高亮 | ✅ TextMate（`syntaxes/*.tmLanguage.json`） | ✅ Tree-sitter `highlights.scm` | 两边实现不同，不共用文件 |
| 语法诊断 | ✅ push | ✅ push | 服务器用 `publishDiagnostics` 推送，不实现 pull |
| 语义诊断（GLOBALS/字段/链接/内置） | ✅ | ✅ | 与客户端无关 |
| 悬停 | ✅ | ✅ | |
| 跳转定义 / 查找引用 | ✅ | ✅ | 跨文件依赖工作区索引 |
| 文档大纲 | ✅ `documentSymbol` | ✅ outline（`outline.scm`）+ LSP | Zed 优先用 query，层级规则见 HANDOVER 坑 #16–19 |
| 工作区符号 | ✅ | ✅ | 需要客户端实现 `workspace/symbol`；结果来自后台索引 |
| 补全 | ✅ | ✅ | 关键字/符号/表单字段 |
| 重命名 | ✅（仅当前文件） | ✅（仅当前文件） | 跨文件重命名刻意不做 |
| Code Action（字段拼错→改为 X） | ✅ | ✅ | 走 `textDocument/codeAction` |
| 格式化 | ✅ | ✅ | `.per` 两边都直接返回空 |
| **语义高亮（semantic tokens）** | ✅ `textDocument/semanticTokens/full` | ❌ **不消费** | Zed 用 `highlights.scm` 做静态高亮；自定义符号的语义着色只在 VSCode 可见 |
| 诊断拉取（pull diagnostics） | 未使用 | 未使用 | 服务器只 push；这是 LSP 实现选择，不是编辑器差异 |
| 链接资料视图（程序→文件树） | ✅ TreeView + 反查命令 | ❌ | 依赖 VSCode 专有 API 与自定义 LSP 请求；Zed 无等价视图 |
| 部署 | ✅ 命令面板 + OutputChannel + Problems 诊断 | ⚠️ Zed task + 终端输出 | Zed 扩展 API 不能执行任意命令；编译错误不发布为 Problems |
| 配置热更新 | ✅ `synchronize.configurationSection` | ✅ `language_server_workspace_configuration` | 服务器都能收到 `didChangeConfiguration` |
| 工作区配置来源 | VSCode 设置 `bdl.*` | Zed 设置 `lsp.bdl-lsp.settings`（初始化选项同） | 键名相同，外层不同 |
| 服务器日志 | OutputChannel | Zed 日志（`zed: open log`） | |
| URI 编码 | 盘符冒号编码为 `%3A` | 不编码 | 已由 `internal/fileuri` 统一处理（HANDOVER 坑 #9） |

图例：✅ 完整；⚠️ 可用但有降级；❌ 客户端不支持。

---

## 2. 配置写法

### VSCode

```jsonc
// .vscode/settings.json
{
  "bdl.server.path": "/path/to/bdl-lsp",
  "bdl.diagnostics": { "record-member-unknown": "off" },
  "bdl.ignore.files": ["build/**", "**/*.bak.4gl"],
  "bdl.ignore.fields": ["tmp_*"],
  "bdl.link.enable": true
}
```

扩展通过 `workspace.getConfiguration("bdl")` 与
`synchronize.configurationSection: "bdl"` 下发；服务器收到的是
`{"bdl": {...}}`，`extractSettings` 会自动拆包。

### Zed

```jsonc
// ~/.config/zed/settings.json
{
  "lsp": {
    "bdl-lsp": {
      "binary": { "path": "/path/to/bdl-lsp" },
      "settings": {
        "diagnostics": { "record-member-unknown": "off" },
        "ignore": {
          "files": ["build/**", "**/*.bak.4gl"],
          "fields": ["tmp_*"]
        },
        "link": { "enable": true }
      },
      "initialization_options": { /* 与 settings 相同结构，可选 */ }
    }
  }
}
```

注意：Zed 里**没有** `bdl.` 前缀；`languages/*/config.toml` 只负责
grammar/缩进等编辑行为，不承载 LSP 配置。`settings` 会在服务器重启后
通过 `workspace/didChangeConfiguration` 下发；改完若未生效，用
`editor: restart language server`。

---

## 3. 为什么这些能力只在 VSCode

| 能力 | 根因 | 替代方案 |
|---|---|---|
| 语义高亮 | Zed 的渲染走 Tree-sitter query；截至本版本不消费 LSP semantic tokens | `highlights.scm` 覆盖 300/76 个关键字；自定义符号靠 `outline.scm` + 悬停 |
| 链接资料视图 | 需要 VSCode `TreeDataProvider` 与自定义 LSP 请求（`bdl/linkTree` 等） | 用 `bdl-lsp link status` 看统计；跳转仍可通过 LSP |
| 部署诊断 | Zed 扩展不能执行任意命令，也没有 Problems 面板注入 API | `.zed/tasks.json` 调 `bdl-lsp deploy`，看终端输出（见 [DEPLOY-ZED](DEPLOY-ZED.md)） |
| 一键部署当前文件 | VSCode 有 `bdl.deployCurrentFile` 命令 | Zed 用 task：`bdl-lsp deploy -workspace "$ZED_WORKTREE_ROOT" "$ZED_FILE"` |

> 如果未来 Zed 扩展 API 支持任意命令或 pull diagnostics，优先补：
> 部署诊断发布 → 链接资料视图 → semantic tokens。不要为了"对齐"在
> LSP 里塞客户端专有逻辑。

---

## 4. 诊断与忽略列表（P8）

`bdl.ignore` 在两边语义一致（服务器端实现，与客户端无关）：

```jsonc
"ignore": {
  "files":  ["build/**", "**/*.bak.4gl"],  // 整文件静默 + 不进索引
  "fields": ["tmp_*"],                      // 只屏蔽 field-not-in-form
  "forms":  ["cust", "**/per/old.per"]      // 屏蔽该表单的字段类诊断
}
```

差异只在**配置外壳**：VSCode 用扁平键 `bdl.ignore.files`，Zed 用嵌套
`ignore.files`。热更新后服务器会重诊断已打开文件；但
`ignore.files` 对**工作区索引**的排除在下次建索引时生效——改完这一项
建议重启语言服务器（VSCode：`Genero BDL: 重启语言服务器`；Zed：
`editor: restart language server`）。

---

## 5. 验证清单

| 项 | VSCode | Zed |
|---|---|---|
| 扩展能否加载 | `make e2e-vscode`（真实扩展宿主） | 需 GUI：`zed: install dev extension` |
| 清单/语言配置 | `make manifest-check` | `make zed-check`（verify-manifest.py） |
| query 是否可编译 | — | `make zed-check`（verify-queries.mjs） |
| 语义高亮 | e2e「语义高亮标注自定义符号」 | 手工：看关键字/自定义符号着色；**没有**语义 token |
| 部署 | e2e「部署预演列出计划」 | 手工：`task: spawn` → 部署 task |
| URI/路径 | 已有 Linux e2e；Windows 待实机 | 不编码冒号，通常不受影响 |

完整命令与期望结果见 [TESTING](TESTING.md) §3.5–3.6。
