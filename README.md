# BDL LSP

面向 **Genero BDL / 4GL** 与 **Form Specification（`.per`）** 的语言服务项目。

目标是在 VSCode 和 Zed 中获得接近现代语言的开发体验：

- 语法高亮、语义高亮
- 函数、变量、参数、表单字段的跳转与引用
- 文档/工作区符号、悬停信息
- 4GL 与 `.per` 格式化
- 跨文件校验：4GL 中使用的表单字段是否真的存在于对应 `.per`
- `.per` 内部一致性校验
- 部署到服务器并调用 `fglform` / `fglcomp` 编译，编译结果回传为诊断

> 当前状态：**粗略计划阶段**。代码尚未开始实现；本仓库已建立 Git 跟踪、目录规范和规划文档。
> 详细计划将在确认本文档中的待定项后拆分为可执行任务。

## 仓库组成

| 仓库 | 内容 | 状态 |
|---|---|---|
| `bdl-lsp/` | Go 语言服务、CLI、测试脚本、中文文档、过程产物 | 已初始化 |
| `vscode-extension/` | VSCode 客户端、TextMate 语法、VSIX 打包 | 已初始化 |
| `zed-extension/` | Zed 扩展（Rust/WASM）、Tree-sitter 查询、LSP 启动配置 | 已初始化 |
| `tree-sitter-bdl/` | `.4gl` / `.global` 语法（已有仓库） | 已存在 |
| `tree-sitter-per/` | `.per` 语法（已有仓库） | 已存在 |

三个新仓库均已 `git init -b main`。远程地址、发布渠道待确认。

## 文档

- [粗略计划](docs/PLAN.md) —— 目标、范围、架构、里程碑、风险
- [技术链与工具](docs/TECH-STACK.md) —— 版本、选型理由、构建命令、本机环境注意事项
- [调研记录](docs/RESEARCH.md) —— 语料统计、CST 结构、可行性验证结果
- [待办与待确认](docs/TODO.md) —— 近期任务、需要用户拍板的问题

## 目录约定

为满足“文档、测试脚本、计划待办、中间文件、日志不污染交付仓库”的要求：

- **中文文档**：`bdl-lsp/docs/`
- **测试/批处理脚本**：`bdl-lsp/scripts/`
- **中间文件、解析结果、日志、缓存**：`bdl-lsp/artifacts/`（Git 忽略，仅保留 `.gitkeep`）
- **最终交付物**：各自仓库的源码、清单、资源文件

`vscode-extension/` 与 `zed-extension/` 只保留发布所需内容；跨仓库的测试脚本和过程记录统一放在 `bdl-lsp/`。

## 开发环境（本机已确认）

- Go 1.24.4，CGO 可用，gcc 13.3
- Node 24.21.0，npm 12.1.0，pnpm 12.14.0
- `tree-sitter` CLI 0.26.5（`tree-sitter-per/node_modules/.bin/tree-sitter`）
- Rust、Zed、VSCode CLI、`fglcomp` / `fglform` **尚未安装**
- 沙箱内 `$HOME` 与 `/home/darcy/go` 不可写，Go/CLI 缓存需指向工作区；详见技术链文档

## 快速验证（已完成）

```bash
# 本机已验证：Go + CGO 同时链接两个 grammar，解析样例 0 错误
# 注意：spike 是临时验证程序，位于 artifacts/，未提交到 Git
cd bdl-lsp/artifacts/spike
./spike bdl /code/workspace/tiptop-gp/tiptop/amm/4gl/ammr110.4gl
./spike per /code/workspace/tiptop-gp/tiptop/amm/per/ammr110.per
```

具体结果与命令见 [调研记录](docs/RESEARCH.md)。
