# BDL LSP

面向 **Genero BDL / 4GL** 与 **Form Specification（`.per`）** 的语言服务项目。

目标是在 VSCode 和 Zed 中获得接近现代语言的开发体验：

- 语法高亮、语义高亮
- 函数、变量、参数、表单字段的跳转与引用
- 文档/工作区符号、悬停信息
- 4GL 格式化（关键字大小写 + 缩进；`.per` 不格式化）
- 跨文件校验：4GL 中使用的表单字段是否真的存在于对应 `.per`
- `.per` 内部一致性校验
- 部署到服务器并调用 `fglform` / `fglcomp` 编译，编译结果回传为诊断
- Genero 以 **2.50** 为基线；5.0 暂不排期

> 当前状态：**P1 Block 1–7 + P2 Zed + P3a GLOBALS + P3b 字段校验 + P4 格式化 已完成**；下一步 P5 部署与远程编译。
> P3a：GLOBALS 模组全局变量的跨文件跳转、来源标注与两类可关闭警告，
> 见 [docs/PLAN-P3A-GLOBALS.md](docs/PLAN-P3A-GLOBALS.md)；
> P3b：4GL 字段存在性校验与 .per 内部一致性，见 [docs/PLAN-P3B-FIELD.md](docs/PLAN-P3B-FIELD.md)。
> 已确认：三个仓库远程、`ssh + rsync` 主 / `scp` 备、warning 默认、v1 验证 Linux + Windows、
> Zed 新 id `4gl-full`、Genero 以 2.50 为基线。
> 暂不排期：Genero 5.0、GitHub Release/无感下载、市场/注册表发布。

## 仓库组成

| 本地目录 | 远程仓库 | 内容 | 状态 |
|---|---|---|---|
| `bdl-lsp/` | `git@github.com:darcyjoven/bdl-lsp.git` | Go 语言服务、CLI、测试脚本、中文文档、过程产物 | 已初始化，remote 已配置 |
| `vscode-extension/` | `git@github.com:darcyjoven/vscode-bdl.git` | VSCode 客户端、TextMate 语法、VSIX 打包 | **v0.1.0 可用**，14 个 e2e 用例通过 |
| `zed-extension/` | `git@github.com:darcyjoven/zed-bdl.git` | Zed 扩展（Rust/WASM）、Tree-sitter 查询、LSP 启动配置 | **`4gl-full` 可用**，38 项 query 校验通过 |
| `tree-sitter-bdl/` | `github.com/darcyjoven/tree-sitter-bdl` | `.4gl` / `.global` 语法（已有仓库） | 已存在 |
| `tree-sitter-per/` | `github.com/darcyjoven/tree-sitter-per` | `.per` 语法（已有仓库） | 已存在 |

说明：

- `bdl-lsp` 是 LSP 二进制的归属仓库；Release/无感下载暂不排期，开发期用本地构建路径。
- 本地目录名与远程仓库名不同：`vscode-extension` → `vscode-bdl`，`zed-extension` → `zed-bdl`。
- 三个新仓库均已 `git init -b main` 并完成首次提交；三个仓库的 `origin` 均已配置并完成首次 push。

## 文档

- [功能清单](docs/FEATURES.md) —— 功能基线（标注来源：用户提供 / 协助规划）
- [P1 详细计划](docs/PLAN-DETAILED-P1.md) —— 当前阶段的实现清单与验收
- [粗略计划](docs/PLAN.md) —— 目标、范围、架构、里程碑、风险
- [技术链与工具](docs/TECH-STACK.md) —— 版本、选型理由、构建命令、本机环境注意事项
- [调研记录](docs/RESEARCH.md) —— 语料统计、CST 结构、可行性验证结果
- [环境配置](docs/ENVIRONMENT.md) —— Linux/Windows 工具链、验证命令、环境待办
- [待办与待确认](docs/TODO.md) —— 已确认决策、环境清单、下一步
- 阶段实现记录：[P3a GLOBALS](docs/PLAN-P3A-GLOBALS.md)｜[P3b 字段校验](docs/PLAN-P3B-FIELD.md)｜[P4 格式化](docs/PLAN-P4-FORMAT.md)

## 目录约定

为满足“文档、测试脚本、计划待办、中间文件、日志不污染交付仓库”的要求：

- **中文文档**：`bdl-lsp/docs/`
- **测试/批处理脚本**：`bdl-lsp/scripts/`
- **中间文件、解析结果、日志、缓存**：`bdl-lsp/artifacts/`（Git 忽略，仅保留 `.gitkeep`）
- **最终交付物**：各自仓库的源码、清单、资源文件

`vscode-extension/` 与 `zed-extension/` 只保留发布所需内容；跨仓库的测试脚本和过程记录统一放在 `bdl-lsp/`。

## 开发环境

- **v1 验证平台：Linux + Windows**；macOS 在 v1 之后补齐（代码保持可移植）。
- Linux 已就绪（本机验证）：Go 1.24.4 + CGO + gcc 13.3、Node 24.21 + pnpm 12.14、
  tree-sitter 0.26.5、Rust 1.99 + wasm32-wasip2 + wasi-sdk 34、VSCode CLI 1.142、gh 2.102、rsync/scp。
- Windows：脚本与清单已就绪（`scripts/dev-env.ps1`、`scripts/env-check.ps1`），待实机执行。
- 尚未具备：Zed 客户端（无图形界面）、`fglcomp`/`fglform`（依赖远程服务器）、真实测试服务器。

```bash
cd bdl-lsp
bash scripts/install-toolchain.sh    # 安装缺失工具链（幂等；无 root 也可用）
source scripts/dev-env.sh            # 环境变量
bash scripts/env-check.sh            # 环境检查
```

完整的 Linux/Windows 清单、验证命令、沙箱安装记录见 [环境配置](docs/ENVIRONMENT.md)。

## 快速开始

```bash
cd bdl-lsp
source scripts/dev-env.sh      # 加载工具链（沙箱/CI 需要）

make build                     # 构建 artifacts/bin/bdl-lsp
make test                      # 单元测试
make lint                      # gofmt + vet
make check                     # 样例冒烟
make bench                     # LSP 延迟基准
make regress                   # 全量语料回归（约 2.5 分钟）

# 解析检查（可用于 CI）
./artifacts/bin/bdl-lsp check testdata/sample.4gl testdata/sample.per
./artifacts/bin/bdl-lsp check --json testdata/broken.4gl

# 语料抽样
bash scripts/corpus-smoke.sh 60

# LSP 服务（编辑器通过 stdio 启动）
./artifacts/bin/bdl-lsp serve --log-level debug
```

## 当前能力

| 能力 | 状态 |
|---|---|
| LSP initialize / shutdown / exit | ✅ |
| didOpen / didChange（真增量）/ didClose | ✅ |
| tree-sitter 语法诊断（ERROR/MISSING） | ✅ |
| `.4gl` / `.global` / `.per` 双语言 | ✅ |
| UTF-16 ↔ 字节位置换算 | ✅ |
| `check` CLI（文本/JSON） | ✅ |
| 文档符号 / 工作区符号 / 悬停 | ✅ |
| 定义 / 引用跳转 | ✅（单文件内） |
| VSCode 扩展 | ✅ Block 6 |
| GitHub Actions（Linux + Windows） | ✅ Block 7 |
| Zed 扩展（`4gl-full`） | ✅ P2 |
| GLOBALS 模组全局变量（跨文件跳转/来源/警告） | ✅ P3a |
| 字段校验（4GL ↔ `.per`） | ✅ P3b |
| 4GL 格式化（`.per` 不格式化） | ✅ P4 |

性能（7138 行 / 329 KB）：`didOpen` 133 ms，`didChange` 往返 6.0 ms（含字段校验防抖）。

详见 [P1 详细计划](docs/PLAN-DETAILED-P1.md)。
