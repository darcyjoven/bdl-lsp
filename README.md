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
- Genero 以 **2.50** 为基线；5.0 暂不排期

> 当前状态：**粗略计划阶段（v0.4）**。代码尚未开始实现；本仓库已建立 Git 跟踪、目录规范和规划文档。
> 已确认：三个仓库远程、`ssh + rsync` 主 / `scp` 备、warning 默认、v1 验证 Linux + Windows、
> Zed 新 id `4gl-full`、Genero 以 2.50 为基线。
> 暂不排期：Genero 5.0、GitHub Release/无感下载、市场/注册表发布。

## 仓库组成

| 本地目录 | 远程仓库 | 内容 | 状态 |
|---|---|---|---|
| `bdl-lsp/` | `git@github.com:darcyjoven/bdl-lsp.git` | Go 语言服务、CLI、测试脚本、中文文档、过程产物 | 已初始化，remote 已配置 |
| `vscode-extension/` | `git@github.com:darcyjoven/vscode-bdl.git` | VSCode 客户端、TextMate 语法、VSIX 打包 | 已初始化，remote 已配置 |
| `zed-extension/` | `git@github.com:darcyjoven/zed-bdl.git` | Zed 扩展（Rust/WASM）、Tree-sitter 查询、LSP 启动配置 | 已初始化，remote 已配置 |
| `tree-sitter-bdl/` | `github.com/darcyjoven/tree-sitter-bdl` | `.4gl` / `.global` 语法（已有仓库） | 已存在 |
| `tree-sitter-per/` | `github.com/darcyjoven/tree-sitter-per` | `.per` 语法（已有仓库） | 已存在 |

说明：

- `bdl-lsp` 是 LSP 二进制的归属仓库；Release/无感下载暂不排期，开发期用本地构建路径。
- 本地目录名与远程仓库名不同：`vscode-extension` → `vscode-bdl`，`zed-extension` → `zed-bdl`。
- 三个新仓库均已 `git init -b main` 并完成首次提交；三个仓库的 `origin` 均已配置并完成首次 push。

## 文档

- [粗略计划](docs/PLAN.md) —— 目标、范围、架构、里程碑、风险
- [技术链与工具](docs/TECH-STACK.md) —— 版本、选型理由、构建命令、本机环境注意事项
- [调研记录](docs/RESEARCH.md) —— 语料统计、CST 结构、可行性验证结果
- [环境配置](docs/ENVIRONMENT.md) —— Linux/Windows 工具链、验证命令、环境待办
- [待办与待确认](docs/TODO.md) —— 已确认决策、环境清单、下一步

## 目录约定

为满足“文档、测试脚本、计划待办、中间文件、日志不污染交付仓库”的要求：

- **中文文档**：`bdl-lsp/docs/`
- **测试/批处理脚本**：`bdl-lsp/scripts/`
- **中间文件、解析结果、日志、缓存**：`bdl-lsp/artifacts/`（Git 忽略，仅保留 `.gitkeep`）
- **最终交付物**：各自仓库的源码、清单、资源文件

`vscode-extension/` 与 `zed-extension/` 只保留发布所需内容；跨仓库的测试脚本和过程记录统一放在 `bdl-lsp/`。

## 开发环境

- **v1 验证平台：Linux + Windows**；macOS 在 v1 之后补齐（代码保持可移植）。
- 本机已确认：Go 1.24.4、CGO、gcc 13.3、Node 24.21.0、pnpm 12.14.0、tree-sitter CLI 0.26.5。
- 本机缺失：Rust/wasm target、Zed、VSCode CLI、`fglcomp`/`fglform`、Windows 构建环境。
- 沙箱内 `$HOME` 与 `/home/darcy/go` 不可写，Go/CLI 缓存需指向工作区。
- 完整的 Linux/Windows 环境清单、验证命令和待办见 [环境配置](docs/ENVIRONMENT.md)。

## 快速验证（已完成）

```bash
# 本机已验证：Go + CGO 同时链接两个 grammar，解析样例 0 错误
# 注意：spike 是临时验证程序，位于 artifacts/，未提交到 Git
cd bdl-lsp/artifacts/spike
./spike bdl /code/workspace/tiptop-gp/tiptop/amm/4gl/ammr110.4gl
./spike per /code/workspace/tiptop-gp/tiptop/amm/per/ammr110.per
```

具体结果与命令见 [调研记录](docs/RESEARCH.md)。
