# 未完成事项

> 盘点：2026-10-23
> 版本：`bdl-lsp 0.3.2` · `tiptop 0.1.0` · `vscode-bdl 0.5.0` · `zed-bdl 0.1.1`（**暂停**）
>
> 本文是**唯一**的"还剩什么"清单。`TODO.md` 是历史清单（按阶段记录勾选），
> 其头部已过期（见 §A6）。
> 新会话接手先读 [HANDOVER.md](HANDOVER.md)，再看本文。

---

## 0. 现状

| 仓库 | 版本 | 状态 |
|---|---|---|
| `bdl-lsp` | 0.3.2 | 语言服务；`link`/`deploy` 已分离出去 |
| `tiptop` | 0.1.0 | 采集 + 部署；依赖 bdl-lsp 公开包 |
| `vscode-bdl` | 0.5.0 | 平台包内置两个二进制；CI 四平台打包 |
| `zed-bdl` | 0.1.1 | **暂停**（用户 2026-10-22 决定） |

**最近完成的（不再追踪）**：P1–P8 全部功能 · 0.3.1 GLOBALS 按文档隔离 ·
0.3.2 link/deploy 分离 · 0.5.0 部署改 tiptop · CI 多平台一键打包 ·
`schema.Merge` 顺序相关缺陷修复。

**代码侧没有"必须做"的功能项了。** 剩下的是三类：分离的收尾残留（§A）、
需要机器/真实环境的验证（§B）、发布相关（§C）。

---

## A. 分离收尾残留（建议先做，都是我自己留下的）

> 分离（0.3.2）只改了 `bdl-lsp/vscode-bdl`，**没清干净引用**。
> 这些是"已知坏"或"死代码"，不含新功能，做完就能关。

### A1. `bdl-lsp/Makefile` 两个目标已坏

- **位置**：`Makefile:97`（`link-audit`）、`Makefile:101`（`link-status`）
- **症状**：`make link-status` → `bdl-lsp: 未知参数: [link status -root ...]`，exit 2。
  `link-audit` 更糟——`cmd/link-audit` 整个目录已移走。
- **做法**：这两个目标从 bdl-lsp 删除；`link status` 改调 tiptop
  （`tiptop link status -root <root>`），`link-audit` 若还需要，在 tiptop 重建。
- **验收**：`make link-status` 能打印统计，或明确移除且 `make help` 不再列出。

### A2. `bdl-lsp/scripts/fetch-gal-file.sh` 归属错了

- **位置**：整个文件；内部 5 处提示写的是 `bdl-lsp link ...`
  （`:8`、`:10`、`:14`、`:22`、`:64`）
- **为什么错**：它做的事是"在生产端用 sqlplus 导出 `DS.GAL_FILE` 为 CSV"——
  纯采集，属于 tiptop。
- **做法**：`git mv` 到 `tiptop/scripts/fetch-gal-file.sh`，把提示改成 `tiptop link ...`。
- **验收**：脚本在新位置跑 `bash -n` 通过；bdl-lsp 里不再有采集脚本。

### A3. `vscode-extension/src/client.ts` 还在下发已删掉的配置

- **位置**：`src/client.ts:172-189`（`sync: { enable, source, interval, onStart,
  command, file, db: {...} }`；`:172` 是 `sync: {`，`:189` 是它的收尾 `}`）
- **背景**：服务端 `LinkSettings` 已无 `Sync` 字段（`config.go:213`）。
- **现状**：服务端**忽略未知字段**（没有 `DisallowUnknownFields`），所以不会报错，
  纯属死代码 —— 但会让人以为"还有这个功能"。
- **做法**：删掉 `sync: {...}` 整块。
- **验收**：`npm run typecheck`；e2e 38/38 仍通过。

### A4. `bdl-lsp/docs/DEPLOY-ZED.md` 命令名过期

- **位置**：`:3`、`:40`、`:47`、`:54`、`:61`、`:83`（`bdl-lsp deploy` → `tiptop deploy`）
- **做法**：全文件替换为 `tiptop deploy`。

### A5. `bdl-lsp/docs/EDITOR-SUPPORT.md` 命令名过期

- **位置**：`:99`（`bdl-lsp link status`）、`:100`（`bdl-lsp deploy`）

### A6. `docs/TODO.md` 头部过期

- **位置**：文件头 1–15 行写"更新：2026-10-23 … 0.3.0 设置与文档轮"，
  未反映 0.3.1 / 0.3.2 / tiptop 分离。
- **已做**：头部已加指针指向本文（`TODO.md:3-5`）。
- **还剩**：§5/§6 里 `link import/status/sync`、`deploy` 仍写在 bdl-lsp 名下，
  需要标注归属；历史勾选**不要动**（那是阶段记录）。

### A7. 面向"当前"的文档里过期的命令与归属

需要改（当前指引）：

| 文件 | 问题 |
|---|---|
| `docs/FEATURES.md` | `:112` `:118` `:128` `:129` `:135` 等行把 `link import/sync/status`、`deploy` 写成 bdl-lsp 功能 |
| `docs/TESTING.md` | 命令矩阵里含 `bdl-lsp deploy` / `link` |
| `docs/PLAN.md` | 架构章节把 link/deploy 列为 bdl-lsp 组件 |

**不要改**（历史阶段记录，改了会失真）：`PLAN-P5-DEPLOY.md`、
`PLAN-P7.md`、`PLAN-P3A-GLOBALS.md` 等 `PLAN-P*.md`。
建议在这些文件顶部加一句"本篇是当时实现记录；`link`/`deploy` 已于 0.3.2
移到 tiptop"，而不是改写正文。

### A8. `tiptop` 没有自己的 CI

- **现状**：`tiptop/.github/workflows/` 不存在。只有 `vscode-bdl` 的
  `bundle-vsix.yml` 会构建它（作为平台包的一部分）。
- **缺口**：tiptop 自己的 `go test ./...`、`go vet`、gofmt **没有 CI 把关**。
- **做法**：照 `bdl-lsp/.github/workflows/build.yml` 建一份（Linux + Windows，
  CGO 构建 + `go test` + `make lint`）。注意它需要 `bdl-lsp` 同级检出
  （`replace` 指令），CI 里要 clone 两个仓库。
- **验收**：故意改坏一个测试 → CI 变红。

---

## B. 验证缺口（需要机器或真实环境）

> 这些都是"代码已就绪、缺一次实跑"。**没有一项是代码问题。**

| # | 事项 | 现状 | 验收 |
|---|---|---|---|
| B1 | **Windows 实机**（从未验证过） | 代码全部就绪 | CGO 构建、LSP 运行、`go test`、e2e、`scp` 部署、GLOBALS `%3A` URI 各跑一次 |
| B2 | **macOS 实机**（从未验证过） | CI 已产出 `darwin-x64` / `darwin-arm64` VSIX | 装包 → 打开 `.4gl` → 语言服务器运行中、`tiptop version` 可执行 |
| B3 | 真实 Genero 服务器 | 无本机 `fglcomp`/`fglform` | `ssh` 通、`fglcomp -M` 可用、单文件部署 + 远程编译实跑一次 |
| B4 | Zed dev extension 手工加载 | 无图形界面 | `zed: install dev extension` 能加载，LSP 起来，大纲层级正确 |
| B5 | `record-member-unknown` 默认级别 | 默认 `off`（`LIKE table.*` 占 87% 引用） | 真实使用反馈后决定是否改 `warning` |
| B6 | P3a `variable-source-unresolved` 人工标注 | `scripts/globals-audit.sh` **未做** | 抽 100 处人工标注，算出误报率 |
| B7 | `expression name:` 跳过规则抽查 | 已修，未逐条复核 | 跑语料差异清单，人工抽查是否误伤 |
| B8 | Windows URI 根因假说 | 未上机验证 | 打印两侧 `initialize` 入参再判断，不要先改代码 |

**优先级**：B1 和 B2 最值得做——CI 已经产出这两个平台的包，但**从来没有人装过**。
"CI 绿"不等于"能装上跑起来"。

---

## C. 发布相关

| # | 事项 | 说明 |
|---|---|---|
| C1 | **GitHub Release 自动化** | 目前只有 Actions artifact（**90 天过期**）。`bundle-vsix.yml` 加 `release` job，四个 VSIX 一起挂 Release |
| C2 | **`bdl-lsp` 没有 tag** | `git tag` 为空。`tiptop/go.mod` 写 `require ... v0.3.1` 但**没有对应 tag**，只能靠 `replace` 指本地目录 |
| C3 | `tiptop/go.mod` 的 `replace` | 发布前换成真实版本号；否则 `go install github.com/darcyjoven/tiptop@latest` 不可用 |
| C4 | tag 触发时 ref 不对 | `bundle-vsix.yml` 的 `${{ inputs.*_ref \|\| 'main' }}`：tag 推送时 `inputs` 为空 → 回落到 `main`，不会用同名 tag。要做 tag 精确对应需改成 `github.ref_name` 回退 |
| C5 | VSCode Marketplace / Open VSX | **未排期**（用户决定） |
| C6 | 二进制自动下载 / 校验 / 缓存 | "无感"体验，**未排期** |
| C7 | Zed 注册表发布 | **未排期** |
| C8 | `PROJECT-SETUP.md` 占位符 | `<用户名>`/`<生产主机>`/`<Oracle主机>` 需按实际环境替换（可选） |
| C9 | 文档截图 / 录屏占位 | `vscode-extension/docs/images/`、`zed-extension/docs/images/zed/README.md` 列了要补哪些图 |

**C1 + C2 + C3 是一组**：想把 tiptop 通过 `go install` 分发，必须先给 bdl-lsp
打 tag、再去掉 `replace`；想给用户一个"不用 90 天内下载"的产物，需要 C1。

---

## D. Zed 扩展（暂停中，`zed-bdl 0.1.1`）

**用户 2026-10-22 明确：暂停，本次不做。** 记录在此以免恢复时重新摸索。

| # | 事项 | 位置 |
|---|---|---|
| D1 | 命令仍是 `bdl-lsp link` / `bdl-lsp deploy` | `docs/USER-GUIDE.md:130,314,320,323,326,329,339,343,349` 等；`docs/images/zed/README.md:15` |
| D2 | 无内置二进制 | 需手工 `make build` + 配 `lsp.bdl-lsp.binary.path` |
| D3 | 版本信息过期 | `docs/USER-GUIDE.md:3` 写"语言服务器 `bdl-lsp 0.3.0`"，实际 0.3.2 |
| D4 | 部署 task 模板需改 tiptop | `docs/USER-GUIDE.md` §7.1 tasks.json 片段 |
| D5 | 与 VSCode 的能力差距 | 无"链接资料"TreeView（Zed 无该 API）、无 `bdl.deploy.enable` / `bdl.link.enable` 开关、无"重启服务器"命令、设置只能写 JSON |

**恢复时的工作量**：D1/D3/D4 纯文档（半小时）；D2 是设计问题（Zed 扩展能否
内置平台二进制）；D5 大部分是 Zed API 限制，**无解**，只能文档说明。

**注意**：`zed-extension/extension.toml` 的 `language_servers.bdl-lsp` 仍指向
`bdl-lsp`——这一层**不需要改**（语言服务器职责没变），只有"采集/部署"的命令名要改。

---

## E. 未排期功能 / 刻意不做

| # | 事项 | 状态 |
|---|---|---|
| E1 | P7-D 链接资料分片 | 触发条件 >8 MB；当前 415 KB，**远未到** |
| E2 | 跨文件重命名 | **刻意不做**：索引里的调用点是名字匹配，可能命中同名局部变量，漏改比不支持更糟。见 `PLAN-P6-INDEX.md §4` |
| E3 | 使用处语义着色 | 需要全文件符号解析，成本高收益低 |
| E4 | Code Action 批量修复（fix all） | 未排期 |
| E5 | 纯 Go SFTP | 仅在确有需求时评估 |
| E6 | Genero 5.0 语法 | 未排期（当前 2.50 基线） |
| E7 | 单文件远程编译超时；编译产物 `.42m` 是否拉回 | 现属 tiptop，待确认 |
| E8 | 安装时提示把 `.link/` 加 `.gitignore` | **刻意不做**（不擅自改用户文件） |
| E9 | 本地 Oracle 驱动（Instant Client / CGO） | **已取消**，只保留纯 Go `go-ora` |

---

## F. 已知风险跟踪

| # | 风险 | 说明 |
|---|---|---|
| F1 | **tiptop 与 bdl-lsp 版本漂移** | `tiptop/go.mod` 的 `replace` 指本地目录，而 CI 里两个仓库各自用 `main`。任一侧改了公开包签名，另一侧可能构建失败且**不报错到开发期**。缓解：给 bdl-lsp 打 tag（C2）+ tiptop 加 CI（A8） |
| F2 | ABI 15 ↔ Zed tree-sitter 运行时兼容 | 未在 Zed 实机验证 |
| F3 | 扩展与 LSP/tiptop 版本错配的回退策略 | 三个二进制版本独立递增，没有兼容矩阵 |
| F4 | Windows 无 rsync → scp 回退 | 稳定性/性能未实测 |
| F5 | 沙箱 `HOME`/`GOPROXY` 对文档/CI 的影响 | 本地构建需 `source scripts/dev-env.sh` |
| F6 | macOS 延后带来的构建差异 | CI 能构建，但**无人验证过产物**（同 B2） |

---

## G. 建议的推进顺序

1. **§A 全部**（半天，纯收尾，做完"分离"这件事才算真正闭合）
2. **§B1 + B2**（需要机器；这两项决定"能不能交付给别人用"）
3. **§C1 + C2 + C3**（发布闭环；做完才算"可分发"）
4. §D（等用户决定是否恢复 Zed）
5. §E / §F（持续跟踪）

---

## 附：下一会话开场语

```text
读 bdl-lsp/docs/PENDING.md，从 §A 开始做分离收尾残留。
```