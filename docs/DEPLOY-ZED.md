# 在 Zed 里使用部署功能

Zed 的扩展 API 不支持执行任意命令，所以部署走 **Zed task** 调用 `tiptop deploy`。
配置与 VSCode 完全共用：`.bdl-lsp/deploy.json`。

> 注：`deploy` 自 `bdl-lsp 0.3.2` 起移出，由 [tiptop](../../tiptop/) 提供；
> 语言服务器本身只有 `serve` / `check` / `version`。本文的 Zed 部分属于
> §D「暂停」范围（见 [PENDING](PENDING.md)），命令名已更正。

## 1. 准备配置

在工作区根创建 `.bdl-lsp/deploy.json`（可从
[tiptop 的 examples/deploy.json](../../tiptop/examples/deploy.json) 复制）：

```json
{
  "targets": {
    "test": {
      "host": "dev.example.com",
      "user": "genero",
      "remoteRoot": "/u1/app",
      "upload": { "include": ["**/*.4gl", "**/*.per"] },
      "compile": {
        "4gl": "fglcomp -M {file}",
        "per": "fglform {file}"
      }
    }
  },
  "defaultTarget": "test"
}
```

`.bdl-lsp/` 应当加入 `.gitignore`（含主机名，且不同开发者目标不同），
或只提交一份不含真实主机名的模板。

## 2. 添加 tasks

在工作区建 `.zed/tasks.json`：

```json
[
  {
    "label": "BDL: 部署预演",
    "command": "tiptop deploy -workspace \"$ZED_WORKTREE_ROOT\" -dry-run",
    "use_new_terminal": false,
    "allow_concurrent_runs": false,
    "reveal": "always"
  },
  {
    "label": "BDL: 部署当前文件",
    "command": "tiptop deploy -workspace \"$ZED_WORKTREE_ROOT\" \"$ZED_FILE\"",
    "use_new_terminal": false,
    "allow_concurrent_runs": false,
    "reveal": "always"
  },
  {
    "label": "BDL: 部署全部",
    "command": "tiptop deploy -workspace \"$ZED_WORKTREE_ROOT\"",
    "use_new_terminal": false,
    "allow_concurrent_runs": false,
    "reveal": "always"
  },
  {
    "label": "BDL: 列出部署目标",
    "command": "tiptop deploy -workspace \"$ZED_WORKTREE_ROOT\" -list",
    "use_new_terminal": false,
    "reveal": "always"
  }
]
```

`$ZED_WORKTREE_ROOT` 与 `$ZED_FILE` 是 Zed 提供的变量；`$ZED_FILE` 是当前
打开文件的绝对路径，正好匹配 `deploy` 的位置参数（相对路径也能用）。

运行方式：`Cmd/Ctrl + Shift + P` → `task: spawn` → 选对应 label。

## 3. 让 `tiptop` 可被 task 找到

task 走的是**登录 shell 的 PATH**，与 Zed 的 LSP 别名词径不同。三选一：

| 方式 | 做法 |
|---|---|
| 放进 PATH | 把 `tiptop` 所在目录加入 shell 的 PATH（`~/.zshrc` / `~/.bashrc`） |
| 软链 | `ln -s /path/to/tiptop/artifacts/bin/tiptop ~/.local/bin/tiptop` |
| 写绝对路径 | task 的 `command` 里直接写 `/path/to/tiptop ...` |

验证：在 Zed 的内置终端里跑 `tiptop deploy -list`，能列出目标即可。

## 4. 与 VSCode 的差异

| 能力 | VSCode | Zed |
|---|---|---|
| 触发方式 | 命令面板 / 编辑器菜单 | `task: spawn` |
| 输出 | 专用 OutputChannel + Problems 面板诊断 | 终端输出（**不发布诊断**） |
| 编译错误跳转 | Problems 面板双击定位 | 手动点终端里的 `file:line` |
| 取消 | 进度通知的取消按钮 | 终端 `Ctrl-C` |

Zed 侧目前只有终端输出。若需要 Problems 面板级别的集成，得等 Zed 扩展
API 支持任意命令（或改成 LSP 的 `workspace/executeCommand`）。

## 5. 常见问题

| 现象 | 原因 | 处理 |
|---|---|---|
| `tiptop: command not found` | task 用的是登录 shell PATH，看不到 tiptop 的路径 | 见 §3 |
| `目标 "x" 不存在` | 没配 `-target` 且无 `defaultTarget` | 配 `defaultTarget` 或显式 `-target` |
| `缺少外部命令 rsync`（但能用） | 已自动回退到 scp | 输出里会有 `↪ 回退到 scp`，属正常 |
| `缺 {file} 占位符` | `compile` 模板写错 | 模板必须含 `{file}` |
| 每次都要输密码 | 没用 SSH key | `ssh-copy-id` 或 `ssh-agent` |
