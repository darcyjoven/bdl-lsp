# P6：工作区符号索引

> 状态：✅ 已完成（2026-10-21，`bdl-lsp 0.2.0`）
> 代码：`internal/wsindex`（索引）+ `internal/server/wsindex.go`（接入）
> 目标（[PLAN.md](PLAN.md) §7.6）：15k 文件后台索引 < 60 s，不阻塞编辑。

## 1. 为什么做

在此之前，`workspace/symbol`、`textDocument/definition`、`references` 只覆盖
**已打开的文档**。真实项目是 7600 个 `.4gl`，于是用户体验是"有时候能跳、有时候
不能"——跳不过去时用户无法判断是自己写错了还是工具不支持。

索引解决的是"符号在哪"这一个问题，不碰"能不能调用"（那是 P7-D 链接资料的
职责）。两者分开：索引永远可以建（只看磁盘），链接资料要对得上数据库导出。

## 2. 设计

### 收什么

每个 `.4gl` / `.global` 收两类记录：

| 记录 | 内容 | 用途 |
|---|---|---|
| 声明 | 模组级符号（MAIN 程序、FUNCTION、REPORT、DEFINE、CONSTANT、TYPE、RECORD、PREPARE、CURSOR） | 符号搜索、跳转 |
| 调用点 | `CALL f()` 与 `IF f()` 里的函数名 | 跨文件引用 |

**只收模组级**：局部变量/参数/记录字段在同一文件的 per-document 索引里已经
有了，放进工作区索引只会把 `workspace/symbol` 变成噪声（实测局部符号数量是
顶层符号的 5~6 倍）。

`MAIN` 的符号名在语法树里是字面量 `main`，7600 个 `main` 毫无用处，
所以索引里用**文件名**（tiptop 布局下文件名即程序代码）作为它的名字。

### 存哪

快照 gob+gzip，落在系统缓存目录：`$XDG_CACHE_HOME/bdl-lsp/<工作区路径>.idx`。

不放工作区里的 `.bdl-lsp/`：索引是纯缓存，用户不该在自己的仓库里看到它，
也不该被 git 状态打扰。缓存目录不可用时才退回 `<root>/.bdl-lsp/symbols.idx`。

### 失效

| 失效条件 | 做法 |
|---|---|
| 文件内容变了 | 按 (mtime, size) 判断；只重解析变化的文件 |
| 解析器/提取逻辑变了 | 快照里存 `Grammar` 指纹（`bdl-lsp <版本> / wsindex <格式版本>`），不一致整份作废 |
| 工作区变了 | 快照里存 `Root`，不一致直接不用 |
| 用户关了索引 | `bdl.index.enable=false`，不建也不读 |

只用 mtime+size 而不做内容哈希：7600 个文件做哈希会把冷启动从 1 s 拖到 10 s+，
而 (mtime,size) 能挡住绝大多数情况（rsync 换内容时 size 通常也变）。

## 3. 实测（真实语料 15108 文件 / 7756 个 `.4gl`+.global，8 核）

| 指标 | 目标 | 实测 |
|---|---|---|
| 首次全量索引 | < 60 s | **24.7 s**（7756 文件全部解析） |
| 冷启动载入快照 | — | **0.56 s**（10.6 MB） |
| 无变化增量重建 | — | **22 ms**（全部复用，只做 stat） |
| 索引失败文件 | 0 | **0** |

启动顺序：`initialized` → 后台 goroutine → 先挂旧快照（跳转立刻可用）→
增量刷新 → 存盘。整个过程中编辑不受影响，也没有任何弹窗。

## 4. 接入点与规则

| 功能 | 行为 |
|---|---|
| `workspace/symbol` | 已打开文档的结果在前（更新更准），索引结果补齐未打开的文件，按 (文件,行,名) 去重 |
| `textDocument/definition` | 本地找不到时查索引：**只在可达模组**（本模组 + `sub`/`lib`/`qry`）里找，且**要求唯一命中** |
| `textDocument/references` | 本文件结果 + 索引里其他文件的调用点（同样限可达模组），仅对函数/报表类符号 |

两条保守规则，都是为了"宁可漏报不可误报"：

1. **只在可达模组里找**。Genero 的函数可见性由链接决定；索引里可能出现别的
   模组的同名函数，跨过去就是跳到别人的代码。本模组 + 全局模组是安全上界
   （与 P7-D 的三条铁律一致），漏掉的（通过非全局模组依赖间接可见的）不报。
2. **多命中就不跳**。同名函数在可达模组里出现多次时返回空——猜错比跳不过去
   更糟（用户会以为自己找对了）。

### 为什么没做跨文件重命名

索引让"找到所有调用点"这件事变得可能，但仍然没开：

- 索引里的调用点是**名字匹配**，不是解析结果。`CALL f()` 里的 `f` 可能被
  同名的局部变量/记录成员遮蔽；跨文件重命名会把这些一起改掉。
- 可见性由链接资料决定，而链接资料可能缺失/过期；漏改一处就是运行时错误，
  比"只支持本文件重命名"糟得多。

要做得对，需要把"可见集"（`link.Resolver.VisibleFunctions`）与索引结合起来，
并接受"链接资料不全时拒绝重命名"。留作后续项。

## 5. 测试

| 测试 | 覆盖 |
|---|---|
| `internal/wsindex` `TestBuildAndQuery` | 声明/调用点/模组识别/搜索/limit |
| `TestRebuildReusesUnchangedFiles` | 增量复用、改一个文件只重解析一个、Grammar 变全作废 |
| `TestSaveLoadRoundTrip` | 快照往返 |
| `TestBuildCancelled` | ctx 取消 |
| `internal/server` `TestWorkspaceSymbolIncludesUnopenedFiles` | 搜到未打开文件里的符号 |
| `TestDefinitionCrossFile` | 跳到未打开的文件（行/列精确） |
| `TestReferencesCrossFile` | 列出别的文件里的调用点 |
| `TestIndexDisabled` | `bdl.index.enable=false` 不建索引 |
