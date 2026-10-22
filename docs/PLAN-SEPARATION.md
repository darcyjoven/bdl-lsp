# PLAN: 将 link / deploy 从 bdl-lsp 分离

> 状态：**已完成**（2026-10-22）。发布前闸门已通过。
> 创建：2026-10-22
>
> 完成情况见文末「实施结果」。
>
> **2026-10-23 补记（实施差异）**：实际执行比下文更彻底——`bdl-lsp` 的
> `link` / `deploy` 子命令**全部移除**（**未**保留只读的 `link status`），
> `cmd/link-audit` 也**未**迁到 tiptop（审计用途已由固化在
> [PLAN-P7.md](PLAN-P7.md) 的数字覆盖）。分离的文档/脚本收尾残留见
> [PENDING.md](PENDING.md) §A。

---

## 1. 动机

`bdl-lsp` 的核心职责是"提供 Genero BDL 的语言智能"。
`link`（链接资料采集/导入）和 `deploy`（远程部署编译）是**项目管理工具**，
不属于语言服务。它们应该独立成 `tiptop` 工具，bdl-lsp 只读取 `.link/links.jsonl`。

分离后：
- bdl-lsp 二进制更小（去掉 go-ora、ssh）
- 支持多种链接结构（tiptop 100、DSC、鼎新等可各自实现采集器）
- deploy 完全独立，不耦合 LSP
- 第三方系统可复用 bdl-lsp 的 LSP 能力

---

## 2. 分离边界

### 留在 bdl-lsp（只读）

| 包 | 职责 | 为什么留 |
|---|---|---|
| `internal/link/link.go` | links.jsonl schema + Load/Save | LSP 需要读链接数据 |
| `internal/link/resolver.go` | 可见函数集计算 | function-not-found 诊断依赖 |
| `internal/link/corpus.go` | 语料索引 | Resolver 依赖 |
| `internal/link/layout.go` | 模组根探测 | Resolver 依赖 |
| `internal/link/symbols.go` | 符号索引 | Resolver 依赖 |
| `internal/link/maintext.go` | MAIN 扫描 | Resolver 依赖 |
| `internal/link/parsebdl.go` | BDL 解析辅助 | Resolver 依赖 |

### 移到 tiptop

| 包 | 职责 | 为什么移 |
|---|---|---|
| `internal/link/import.go` | CSV 导入 → links.jsonl | 采集逻辑 |
| `internal/link/run.go` | ImportRequest/RunImport 编排 | 采集逻辑 |
| `internal/linksync/` | Oracle/命令/文件采集 | 采集逻辑 |
| `internal/linkcli/` | CLI 入口 | CLI 工具 |
| `internal/deploy/` | SSH 上传 + 远程编译 | 部署逻辑 |
| `internal/deploycli/` | CLI 入口 | CLI 工具 |
| `cmd/link-audit/`、`cmd/deploy*` | 审计命令 | 审计工具 |

### bdl-lsp 保留的 CLI 子命令

- `bdl-lsp check`（语法检查）
- `bdl-lsp version`
- `bdl-lsp link status`（只读，显示资料状态）

### tiptop 提供的 CLI 子命令

- `tiptop link import -csv <file>`
- `tiptop link sync -source golang/command/file`
- `tiptop link status`
- `tiptop deploy [-target] [-dry-run] [files...]`

---

## 3. 实施步骤

### Phase 1：新建 tiptop 仓库（风险：低）

1. 创建 `tiptop` 仓库，从 bdl-lsp 复制：
   - `internal/deploy/` + `internal/deploycli/`
   - `internal/link/import.go` + `internal/link/run.go`
   - `internal/linksync/` + `internal/linkcli/`
   - `cmd/link-audit/`、`cmd/deploy*`
2. tiptop 的 `main.go` 只注册 `link` 和 `deploy` 子命令
3. tiptop 的 `go.mod` 引用 bdl-lsp 的 `internal/link`（只读部分）作为库
4. 验证：`tiptop deploy -dry-run` 和 `tiptop link import` 正常工作

### Phase 2：bdl-lsp 移走采集代码（风险：中）

1. bdl-lsp 移走 `internal/link/import.go`、`internal/link/run.go`、`internal/linksync/`、`internal/linkcli/`、`internal/deploy/`、`internal/deploycli/`
2. bdl-lsp 的 `main.go` 移走 `link`、`deploy` 子命令，保留 `link status`
3. 确保 `internal/link` 包不 import `linksync` 或 go-ora
4. 验证：`go build`、`go test ./...` 通过，二进制变小

### Phase 3：更新扩展（风险：低）

1. VSCode 扩展：deploy 命令改为调 `tiptop deploy`
2. VSCode 扩展：link sync 命令改为调 `tiptop link sync`
3. Zed 扩展：同上
4. VSCode 平台包 CI：多打一个 `tiptop` 二进制

### Phase 4：文档更新（风险：低）

1. 更新 `PROJECT-SETUP.md` 的命令名
2. 更新 `USER-GUIDE.md` 的设置说明
3. 更新 `README.md`

---

## 4. 发布前闸门

**发布前必须确认以下之一**：

- [x] Phase 1-4 全部完成，tiptop 已发布
- [ ] 或：明确推迟分离，发布时注明"link/deploy 将在后续版本分离"

**不允许**：发布后再做分离（会造成用户命令名变更的 breaking change）。

---

## 5. 为什么这个顺序

1. **Deploy 先**：已完全解耦（server 不引用 deploy），风险最低
2. **Link 采集次之**：需要确认 `internal/link` 不 import `linksync`
3. **扩展更新最后**：依赖 tiptop 二进制可用

---

## 6. 不做的事

- 不改 `links.jsonl` 的 schema（已有用户数据）
- 不把 `link.Resolver` 移走（LSP 依赖）
- 不让 tiptop 调 LSP 的 API（文件系统是唯一接口）
- 不把 GLOBALS 解析移走（语言服务的一部分）


---

## 实施结果（2026-10-22）

### 边界调整

实施时发现 `import.go` / `run.go` 依赖 `link` 包的未导出内部
（`buildExternals` / `lookupMains` / `scanFiles` / `importGroup`），
拆成两个包会造成循环依赖。因此**边界改为**：

- `bdl-lsp/link`（公开包）保留**数据格式 + 全语料扫描 + 导入算法**；
- `tiptop` 只做**程序**：CLI、采集（Oracle/命令/文件）、部署。

即"库留在 bdl-lsp，程序搬到 tiptop"。真正移出 bdl-lsp 的：
`internal/deploy`、`internal/deploycli`、`internal/linksync`、
`internal/linkcli`、`cmd/link-audit`，以及 `main.go` 的 `link` / `deploy`
子命令与 `bdl.link.sync.*` 配置。

### 提升为公开包的

`internal/{link,parser,index,builtin,glob}` → 顶层同名包，
共 89 处 import 改名。

### 成果

| 指标 | 前 | 后 |
|---|---|---|
| bdl-lsp 二进制 | ~28 MB | **12 MB** |
| bdl-lsp 依赖 | 含 go-ora、ssh | 无 go-ora（`go tool nm` 验证为 0） |
| bdl-lsp 子命令 | serve / check / link / deploy | serve / check / version |
| tiptop 二进制 | — | 26 MB（含 tree-sitter + go-ora） |

### 验证

- bdl-lsp：`go build`、`go vet`、`go test ./...`（21 个包全过）
- tiptop：`make build` + `make lint`；`link import` / `link status` /
  `deploy -list` / `deploy -dry-run` 冒烟通过
- VSCode 扩展：e2e **38/38 通过**；平台 VSIX 内置两个二进制并验证可执行
- 顺带修掉一个既有缺陷：`schema.Merge` 的 CREATE/ALTER 顺序相关性
  （见 HANDOVER 坑 #36）

### 后续

- Zed 扩展**暂停**，本次未适配（用户 2026-10-22 决定）
- GitHub Release 自动化仍未做（仅 Actions artifacts）
