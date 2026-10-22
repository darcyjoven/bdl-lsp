# P5 记录：部署与远程编译

> 版本：v1.0（CLI 已完成；编辑器命令待接）  
> 日期：2026-10-21  
> 状态：**CLI 已完成并实测**（验收输出见 §5）；VSCode 命令 / Zed tasks 见 §7。  
> 上游：[粗略计划](PLAN.md) §7.4｜[P4 记录](PLAN-P4-FORMAT.md)｜[待办](TODO.md)  
> 示例配置：[tiptop/examples/deploy.json](../../tiptop/examples/deploy.json)（原在本仓库，随 0.3.2 分离移出）
>
> **2026-10-23 注**：本篇是当时的实现记录。`deploy` 已于 `bdl-lsp 0.3.2`
> 移到 **tiptop**（现为 `tiptop deploy`）；下文出现的 `bdl-lsp deploy` 均应按
> `tiptop deploy` 阅读，正文不改写。见 [PLAN-SEPARATION.md](PLAN-SEPARATION.md)
> 与 [PENDING.md](PENDING.md) §A4。

---

## 1. 交付内容

```text
internal/deploy/                # 新包（纯逻辑，可单测）
├── config.go                   # Config/Target/Upload 结构、默认值、校验
├── files.go                    # 按 include/exclude 收集、CRLF 检查、类型判定
├── glob.go                     # 支持 ** 的 glob 匹配（不用 filepath.Match）
├── ssh.go                      # ssh/scp 参数构造、shell 转义、Runner 抽象
├── parse.go                    # 编译输出解析（fglcomp/fglform → Diagnostic）
├── deploy.go                   # 编排：选择 → 上传（含回退）→ 编译 → 映射诊断
└── deploy_test.go              # 24 个单测

internal/deploycli/             # `bdl-lsp deploy` 子命令
├── deploycli.go                # 参数解析、文本/JSON 输出、退出码
└── deploycli_test.go           # 参数重排测试

main.go                         # 注册 deploy 子命令
examples/deploy.json            # 可直接改用的示例配置
```

## 2. 配置项

| 字段 | 默认 | 说明 |
|---|---|---|
| `host` | 必填 | 远程主机名/IP |
| `port` | `22` | SSH 端口 |
| `user` | 必填 | SSH 用户名 |
| `remoteRoot` | 必填 | 远程根目录，**必须是 POSIX 绝对路径** |
| `localRoot` | `.` | 本地根（相对配置文件所在工作区） |
| `upload.include` | `["**/*.4gl","**/*.per"]` | 包含的 glob（支持 `**`） |
| `upload.exclude` | `[]` | 排除的 glob，优先级高于 include |
| `upload.mode` | `rsync` | `rsync` / `scp` |
| `upload.fallback` | `scp` | 首选失败后的回退；`none` 表示不回退 |
| `upload.rsyncArgs` | `["-az"]` | 追加给 rsync 的参数 |
| `upload.delete` | `false` | rsync 是否 `--delete` |
| `compile` | `{}` | 键为扩展名（`4gl`/`per`），值必须含 `{file}` |
| `strictHostKey` | `true` | 严格校验 host key |
| `connectTimeout` | `20` | 单次连接超时（秒） |

## 3. 关键设计决策

### 3.1 配置未知字段直接报错

```go
dec.DisallowUnknownFields()
```

把 `remoteRoot` 写成 `remote_root` 时，若静默忽略，`remoteRoot` 为空会
命中"必填"校验——但如果写错的是 `localRoot`，就会静默用默认值 `"."`
部署**错误的目录**。部署操作影响的是远程服务器，任何静默容错都不划算。

### 3.2 `remoteRoot` 必须是 POSIX 绝对路径

```go
if !strings.HasPrefix(t.RemoteRoot, "/") { ... }
```

Windows 客户端最容易写 `C:\u1\app`。远程一定是 POSIX，与其让 rsync
在远端报难懂的错，不如本地直接拒绝并说明原因。

### 3.3 `**` 自己实现，不用 `filepath.Match`

`filepath.Match` 不支持 `**`：`filepath.Match("**/*.4gl", "a.4gl")` 是
**false**（`**` 被当作普通 `*`，且跨目录也不匹配）。而 include 最自然的
写法就是 `**/*.4gl`。若用它做校验，这些模式"合法但永不匹配"，
部署会静默传 0 个文件。

因此 `glob.go` 实现了段级递归匹配，`**` 匹配 0..n 段。

### 3.4 回退必须显式可见

```go
logf("  ↪ 回退到 %s（请在输出中确认，这不是静默降级）", ...)
res.Fallback = true
```

计划明确要求"不静默降级"。摘要里也会打上`（已从首选方式回退）`，
JSON 输出有 `fallback: true`。用户必须知道实际用的是哪种传输。

### 3.5 上传失败是"部署失败"（退出 1），不是"无法开始"（退出 2）

```go
if err := upload(...); err != nil {
    res.UploadErr = err
    return res, nil   // 不向上返回 error
}
```

区别很重要：
- **退出 2**：配置错、缺 ssh、目标不存在——重试没用，要先改东西；
- **退出 1**：上传/编译失败——环境或代码问题，修完可重试。

并且上传失败后**不再编译**：远程内容不完整，编译结果没有意义。

### 3.6 scp 逐文件而非 `scp -r`

```go
for _, f := range files { scp f.Abs → user@host:remoteRoot/f.Rel }
```

整目录递归会把 `.git`、构建产物一起搬走，且失败粒度只到目录。
逐文件能把每个失败精确聚合，满足"部分成功不能报成成功"的要求。

### 3.7 远程命令的引号陷阱（最花时间的一个坑）

```go
// 正确：直接把脚本作为单个参数交给 ssh
args = append(args, sshTarget(t), script)

// 错误：再包一层单引号
args = append(args, sshTarget(t), "'"+script+"'")
```

`ssh host 'mkdir -p /x'` 在终端里能用，是**本地 shell** 把引号剥掉了。
用 `exec` 直接调用时没有本地 shell，ssh 会把参数用空格拼成一条字符串
交给**远程**登录 shell——再加引号就变成多了一层，远程把整串当成文件名：

```text
zsh:1: no such file or directory: mkdir -p /tmp/bdl-deploy-it
```

单引号只在**需要插值进脚本的值**上使用（`shellQuote`），脚本本身不加。

## 4. 编译器输出解析

支持的格式（Genero fglcomp / fglform）：

```text
aapr106.4gl:232:12: error: -8403: Identifier not defined
aapt110.per:15: warning: Undefined item
/u1/app/sub/b.4gl:7: error: -1234: Syntax error
** Error: cannot open file
```

正则刻意宽松：不同 Genero 版本与语言设置下分隔符略有差异，**漏解析比
多解析更糟**（漏掉等于用户看不到错误）。无位置信息的行归为文件级诊断
（`Line=0`）。

远程路径映射规则：

| 编译器输出 | 映射结果 |
|---|---|
| `a.4gl`（相对） | `<localRoot>/a.4gl` |
| `/u1/app/a.4gl`（在 remoteRoot 下） | `<localRoot>/a.4gl` |
| `/other/a.4gl`（不在 remoteRoot 下） | 原样保留，让用户看到真实位置 |

## 5. 验收输出（真实 SSH 实测）

测试环境：本机 `sshd` 监听 22，`rsync`/`scp` 均可用，用 `echo` 模拟编译器。

```text
$ bdl-lsp deploy -dry-run
目标 it：darcy@127.0.0.1:22 → /tmp/bdl-deploy-it
本地根 .../artifacts/deploy-it/local，待处理 2 个文件
  upload per/f1.per → /tmp/bdl-deploy-it/per/f1.per
  upload src/main.4gl → /tmp/bdl-deploy-it/src/main.4gl
  compile src/main.4gl: echo '/tmp/bdl-deploy-it/src/main.4gl:2: error: -8403: fake error'

$ bdl-lsp deploy -target it          # rsync 主路径
  rsync → darcy@127.0.0.1:/tmp/bdl-deploy-it/（2 个文件）
  compile src/main.4gl
传输方式  : rsync
已上传    : 2
编译问题  : 1 error / 0 warning
  .../local/src/main.4gl:2: error[8403] fake error
结果      : 失败        （退出码 1）

$ bdl-lsp deploy -target it          # scp 模式
传输方式  : scp
已上传    : 2
$ find /tmp/bdl-deploy-it -type f
/tmp/bdl-deploy-it/per/f1.per
/tmp/bdl-deploy-it/src/main.4gl     ← 两个文件都真的到了

$ PATH=<无 rsync> bdl-lsp deploy -no-compile    # rsync 缺失 → 回退
  rsync 上传失败：缺少外部命令 rsync；...
  ↪ 回退到 scp（请在输出中确认，这不是静默降级）
传输方式  : scp（已从首选方式回退）

$ bdl-lsp deploy src/main.4gl        # 单文件部署
已上传    : 1
$ find /tmp/bdl-deploy-it -type f
/tmp/bdl-deploy-it/src/main.4gl     ← 只有指定的那个

$ bdl-lsp deploy -target nope        # 配置/参数错误
bdl-lsp deploy: 目标 "nope" 不存在；可用：it     （退出码 2）
```

```text
$ go test ./internal/deploy/ ./internal/deploycli/
ok    github.com/darcyjoven/bdl-lsp/internal/deploy     24 个测试
ok    github.com/darcyjoven/bdl-lsp/internal/deploycli   2 个测试
```

## 6. 踩坑记录（全部写进代码注释）

| 现象 | 根因 | 修法 |
|---|---|---|
| `zsh: no such file or directory: mkdir -p /x` | 给 ssh 的命令又包了一层单引号；exec 调用没有本地 shell 剥引号 | 脚本作为单个参数直传，不额外加引号 |
| 指定 `per/f1.per` 却部署了 `src/main.4gl` | 重排参数后仍用 `fs.Args()` 取文件（重排后已无文件）；且 `-target test` 的 `test` 被误判为文件 | 重排时返回文件列表；`hasFlagsAfterOperand` 跳过带值 flag 的值 |
| 远程报 `/tmp/bdl-deploy-it/../../../src` permission denied | `selectFiles` 用 `filepath.Abs`（进程 CWD）解析相对路径 | 相对 localRoot 解析，并拒绝越出根目录的路径 |
| 部分文件上传失败却显示"成功" | `tryScp` 的失败聚合后返回值被 `upload()` 丢掉 | 失败数进 `Result.UploadFailed` / `UploadErr`，`Ok()` 纳入判断 |

## 7. 待完成（本阶段剩余）

| 项 | 说明 |
|---|---|
| VSCode 命令与输出通道 | `bdl.deploy` 命令、`OutputChannel` 流式输出、状态栏进度 |
| VSCode 诊断发布 | 把 `Result.Diagnostics` 发到 Problems 面板（当前只打印） |
| Zed tasks 模板 | `.zed/tasks.json` 调用 `bdl-lsp deploy`；文档给模板 |
| 超时/取消 | 已有 `ConnectTimeout` 与 Ctrl-C；单文件编译超时未单独设 |
| 远程编译产物归档 | 未做（`.42m` 等是否需要拉回待确认） |
| Windows scp 实测 | 代码路径已就绪（逐文件 + 路径归一），需在 Windows 跑一次 |
| rsync `--files-from` 大文件集 | 已用临时文件，避免命令行长度限制；>10 万文件未压测 |

## 8. 安全约束（已实现）

- 部署**只在显式调用时**执行，不挂在保存/打开事件上。
- 不存储密码，依赖 SSH key / agent。
- `strictHostKey` 默认 `true`；关闭时用 `accept-new`，**绝不**用
  `StrictHostKeyChecking=no`（有单测断言这一点）。
- 远程命令参数经 `shellQuote` 转义；含空格的路径会被正确引用。
- 只允许配置里声明的目标（`Pick` 拒绝未声明名字）。
- 日志不输出密钥内容；命令行里的密码类参数不会被记录（本实现不传密码）。
