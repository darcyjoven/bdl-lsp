# NEXT：剩余待办（唯一实时清单）

> 这份文件是**新会话的入口**：其余文档（`PENDING.md` 等）是历史与归档，
> 想知道"现在还剩什么"只看这一份。
>
> 生成于 2026-10-24（功能已全部完成：`bdl-lsp 0.3.13` / `tiptop 0.1.8` / `vscode-bdl 0.6.2`）。

---

## 0. 一句话开场（复制这句给新会话）

```text
读 bdl-lsp/docs/NEXT.md，从第 1 项「发布闭环」开始做：先按 docs/WINDOWS-CHECKLIST.md 做 Windows 实机验收，再打 tag（C1）并给 bundle-vsix.yml 加 release job（C2）。
```

---

## 1. 发布闭环（**唯一必须做的事**，按顺序）

| # | 事项 | 现状 | 要做的事 |
|---|---|---|---|
| **C0** | 真机验证（Windows） | 从未有人在真机上装过插件；CI 只覆盖"构建 + 单测 + 单测级二进制自检 + VSIX 内容自检" | 按 [`WINDOWS-CHECKLIST.md`](WINDOWS-CHECKLIST.md)（**71 项**）逐条勾选：装包 → 内置二进制 → 语言服务 UI → ⭐路径/迁移/大文件 → 链接资料命令 → **数据库字段（视图/悬停/多选/直连库同步/进度）** → 部署（scp 回退） |
| **C0b** | 真机验证（macOS） | CI 有 `darwin-x64` / `darwin-arm64` 两个包，但没在真机装过 | 在至少一台 Intel 或 Apple Silicon 上装包并做 C0 的抽样（语言服务能起、跳转/诊断可用、VSIX 不夹带） |
| **C1** | 打 tag | `bdl-lsp` 至今**没有 tag**；`tiptop` 的 `go.mod` 用 `replace ../bdl-lsp`，CI 只能追 `main` → 版本漂移（见 §3 F1/F9） | 打 `bdl-lsp v0.3.13`、`tiptop v0.1.8`、`vscode-bdl v0.6.2`；之后 `bundle-vsix.yml` 用 `bdl_lsp_ref=v0.3.13` / `tiptop_ref=v0.1.8` 出包，产物可复现 |
| **C2** | GitHub Release | `bundle-vsix.yml` 只有 `workflow_dispatch` 的 `bdl_lsp_ref` / `tiptop_ref` 输入，**没有 release job**；现在只有 Actions artifact（90 天过期） | 给 `bundle-vsix.yml` 加 `release` job（或新 workflow）：4 个平台 VSIX（`win32-x64` / `linux-x64` / `darwin-x64` / `darwin-arm64`）一起挂 Release，附 `BUILD-INFO.json` 内容与 sha256 校验和；触发方式建议"tag push 或手动输入 release_tag" |
| **C3** | 市场 / 注册表 | 用户已定：**暂缓** | 想发时再做：`vsce publish`（市场）+ Open VSX；先用 `vsce package` 的产物人工验证一遍 |

**验收门槛**：C0 + C0b 全绿（或把问题记进本文件）之后才 C1/C2。

---

## 2. 真机才能验的两件事（沙箱里做不了）

| # | 事项 | 为什么没验 | 怎么验 |
|---|---|---|---|
| **D1** | `tiptop schema sync -source golang`（直连 Oracle 采集表字段） | 沙箱没有 Oracle；只验证了 SQL 生成、`-source file` 全链路、密码来源与参数校验的单元测试 | 真库里跑一次：`export BDL_ORA_PASSWORD=...; tiptop schema sync -root . -host … -service … -user …`（应用 schema 与账号不同时加 `-owner`）。核对：表数/列数与 DBA 导出一致、中文说明来自 `ALL_COL_COMMENTS`、`nullable` 与 `default_length` 映射正确；随后在 VSCode 里点 `数据库字段：从数据库同步` 看进度与结果 |
| **D2** | `tiptop link sync -source golang`（直连 Oracle 采集链接资料） | 同上（这条早就在，但同样没在真库跑过） | `tiptop link sync -root . -host … -service … -user …`，与"导出 CSV 再 `link import`"的结果对比（链数/文件数应一致） |

---

## 3. 代码层遗留（都不是阻塞项，按价值排序）

| # | 事项 | 说明 | 价值 |
|---|---|---|---|
| **P0-1b** | 崩溃回归进 CI | `scripts/repro-index-crash.py` 依赖真实语料（CI 没有）→ 造一个**合成工作区**：`.link/links.jsonl` + 全局模组文件（拉开索引预热窗口）+ 一个 4 万行大文件，断言"不崩、能出大纲" | 中（P0-1 的修复目前只靠人工脚本证明） |
| **P0-2b** | T5 剩余量级（**等你拍板**） | `variable-source-unresolved` 全语料 19580 条，主体是**跨模块的模块级变量**（`L_AC` 1828、`G_RVV` 1715…）。算不算误报取决于"模块级 `DEFINE` 是否跨模块可见" | 高（若确认是噪声，需要按模块/库聚合可见性，改动不小） |
| **F1/F9** | 版本漂移 | `tiptop` 依赖 `bdl-lsp` 公开包但用 `replace ../bdl-lsp`（无 tag）；C1 打 tag 后建议把 `replace` 换成版本号依赖，并让 CI 按 tag 校验 | 中（C1 之后顺手做） |
| **P2-1+** | schema 增量采集 | 目前 `schema sync` 是"全量查询 → 逐表逐字节比较只写变化的表"；可加"按表 diff（只查变化的表）"或 `-since` 时间戳过滤 | 低（2648 表 2.4 s，够用） |
| **P1-7** | 用户文档收尾 | **本轮已完成**：插件首页 `README.md` 重写为"三档引导手册"（含 11 处配图占位）、`USER-GUIDE` 加了指针与三档说明 | 已完成；剩"首页与详细手册重复内容再合并一次"（可选，低优先） |
| **§D** | Zed 扩展 | 暂停中；真要恢复时按 `zed-extension/` 的 README 走（LSP 已就绪，缺真机验证） | 低（用户暂缓） |
| **§E/§F** | 长期项 | Genero 5.0 兼容、更多语料问题跟踪等，见 `PENDING.md` §E/§F | 低 |

---

## 4. 相关文档索引（需要细节时再看）

| 文档 | 内容 |
|---|---|
| [`PENDING.md`](PENDING.md) | 全量待办与已完成记录（历史归档，§G 是旧的推进顺序） |
| [`WINDOWS-CHECKLIST.md`](WINDOWS-CHECKLIST.md) | Windows 实机验收 71 项（C0 用） |
| [`HANDOVER.md`](HANDOVER.md) | 仓库组成、版本、构建与测试、常见坑 |
| [`PLUGIN-PLAN.md`](PLUGIN-PLAN.md) | 插件改版设计与实现记录（P0/P1/P2 各项） |
| `vscode-extension/docs/USER-GUIDE.md` | 插件完整操作手册（细节版） |
| `vscode-extension/docs/PROJECT-SETUP.md` | 从生产端拉源码建工作区（打包/Git/资料/部署） |