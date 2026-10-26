# BDL LSP

面向 **Genero BDL / 4GL** 与 **Form Specification（`.per`）** 的语言服务（Go 实现，stdio LSP + `check` CLI）。

- 语法高亮（配合编辑器 TextMate）、语义高亮
- 函数/变量/参数/字段的悬停、跳转、引用、大纲、工作区符号
- 补全、重命名（仅当前文件）、快速修复、4GL 格式化（`.per` 不格式化）
- GLOBALS 模组全局变量（跨文件跳转/来源/警告）、表单字段存在性校验、`.per` 内部一致性
- 内置符号表（97 条）、忽略列表（glob）、后台工作区符号索引
- 数据库字段资料（`.project/schema`）的悬停、成员校验、列补全
- 链接资料的跨文件函数可见性与导航

> 采集与部署**不在本仓库**：链接资料/表字段资料采集与部署由
> [tiptop](https://github.com/darcyjoven/tiptop) 负责，bdl-lsp **只读** `<项目>/.project/{link/,schema/}`（旧布局 `.project` 根 / `.link/` / `.project/.schema/` 只读兼容）。
> 编辑器客户端在 [vscode-bdl](https://github.com/darcyjoven/vscode-bdl)（VSCode）与 zed-bdl（Zed，暂停）。
>
> 当前状态：功能已全部完成（`bdl-lsp 0.3.13` / `tiptop 0.1.8` / `vscode-bdl 0.6.2`），剩余发布闭环。

## 文档

| 文档 | 内容 |
|---|---|
| [docs/FEATURES.md](docs/FEATURES.md) | **本仓库唯一功能文档**（面向开发者，详细到每个功能） |
| [docs/DIAGNOSTICS.md](docs/DIAGNOSTICS.md) | 14 个诊断码与默认级别（`make diagnostics-write` 生成，构建输入） |
| [scripts/README.md](scripts/README.md) | 测试/构建/语料脚本说明 |
| [vscode-bdl/README.md](https://github.com/darcyjoven/vscode-bdl/blob/main/README.md) | **唯一用户引导**（安装、三档用法、全部设置、建工作区） |
| [vscode-bdl/docs/FEATURES.md](https://github.com/darcyjoven/vscode-bdl/blob/main/docs/FEATURES.md) | 三组件整合功能文档 |
| [vscode-bdl/docs/TODO.md](https://github.com/darcyjoven/vscode-bdl/blob/main/docs/TODO.md) | 待办清单（含 Windows 实机验收） |
| [vscode-bdl/docs/OPEN-ITEMS.md](https://github.com/darcyjoven/vscode-bdl/blob/main/docs/OPEN-ITEMS.md) | 待确认记录 |
| [tiptop/docs/FEATURES.md](https://github.com/darcyjoven/tiptop/blob/main/docs/FEATURES.md) | tiptop 功能文档 |

## 仓库组成

| 本地目录 | 远程仓库 | 内容 |
|---|---|---|
| `bdl-lsp/` | `git@github.com:darcyjoven/bdl-lsp.git` | Go 语言服务、CLI、测试脚本、过程产物 |
| `tiptop/` | `git@github.com:darcyjoven/tiptop.git` | 链接/表字段资料采集 + 部署 |
| `vscode-extension/` | `git@github.com:darcyjoven/vscode-bdl.git` | VSCode 客户端、TextMate 语法、VSIX 打包 |
| `zed-extension/` | `git@github.com:darcyjoven/zed-bdl.git` | Zed 扩展（`4gl-full`，暂停） |
| `tree-sitter-bdl/` / `tree-sitter-per/` | 各自仓库 | `.4gl`/`.global` 与 `.per` 语法 |

## 目录约定

- 中文/功能文档：`docs/`
- 测试与批处理脚本：`scripts/`
- 中间文件、解析结果、日志、缓存：`artifacts/`（Git 忽略）

## 构建与快速开始

```bash
cd bdl-lsp
source scripts/dev-env.sh            # 加载工具链（受限环境/CI 需要）
bash scripts/install-toolchain.sh    # 幂等安装缺失工具链（无 root 可用）
bash scripts/env-check.sh            # 环境检查

make build                     # 构建 artifacts/bin/bdl-lsp
make test                      # 单元测试
make lint                      # gofmt + vet + 各类一致性检查
make check                     # 样例冒烟
make bench                     # LSP 延迟基准
make regress                   # 全量语料回归（约 2.5 分钟）

./artifacts/bin/bdl-lsp check testdata/sample.4gl testdata/sample.per
./artifacts/bin/bdl-lsp serve --log-level debug
```

- v1 验证平台 Linux + Windows；Windows 用 `scripts/*.ps1`。
- 语言服务与 CLI 的完整能力见 [docs/FEATURES.md](docs/FEATURES.md)。