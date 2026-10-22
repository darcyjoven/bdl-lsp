# P6：数据库 schema 索引

> 状态：✅ 已完成（2026-10-21，`bdl-lsp 0.2.1`）
> 代码：`internal/schema`（解析）+ `internal/server/schema.go`（接入）

## 1. 问题

`DEFINE r RECORD LIKE maj_file.*` 在语料里出现 **21144 次**。这种记录的成员
全部来自数据库表结构，语法树里一个字段都没有。后果：

- `record-member-unknown` 只能对这类记录整体静默（否则成片误报），而那恰好
  是记录成员用得最多的地方——等于这条诊断在真实项目里完全无效；
- `r.` 之后补全只能猜"表单字段 + 同名符号"，对 `maj_file` 的 100 多列毫无命中率。

## 2. 数据从哪来：`.sch` 文件

Tiptop 布局里每个模组自带 DDL：`<模组根>/<模组>/sch/<表>.sch`。语料实测：

| 项 | 数量 |
|---|---:|
| `.sch` 文件 | **3127**（全部位于名为 `sch` 的目录，0 例外） |
| `create table` 语句 | 2945 |
| `alter table … add …` 文件 | 182 |
| `create index` 等无关语句 | 138 |
| 表名在多个模组目录重复 | 69（取**并集**） |
| 解析出的表 | **2879** |

两种语句都认：

```sql
create table mmb_file ( mmb01 varchar2(20) NOT NULL, ... );   -- 建表
alter table bmy_file add ta_bmy01 varchar(40);                -- 追加列
alter table ala_file add constraint ala_pk primary key (ala01) enable validate;  -- 加约束，忽略
```

解析是**手写**的（`internal/schema/parse.go`），不用 tree-sitter：DDL 只有两种
语句，手写状态机 60 行就够，而且能容忍语料里的手写笔误（`varchar2(40);:`）、
截断文件（没有闭合括号）、注释里的中文/括号。

三处必须宽容的地方，每处都对应真实样本：

1. `;` 切开后可能剩下以 `:` 开头的垃圾，真正的 `create table` 紧跟其后 →
   先找 `create` / `alter` 起点，丢掉前缀；
2. 括号没闭合（文件被截断）→ 把剩下的都当列定义，至少保住大部分列；
3. 同一张表两个模组定义不同 → 取并集。多出来的列最多让校验漏报，覆盖则会
   把真实存在的列判成"不存在"，那是误报。

## 3. 耗时

3127 个文件、2879 张表、**118 ms**（单线程读 + 解析）。因此放在
`initialize` 里**同步**加载：

- 异步会让 didOpen 首轮诊断与后续诊断用不同规则（先不校验、后校验），
  用户会看到"打开时不报、改一下就报"；
- 118 ms 对启动延迟无感，比"结果不确定"划算。

## 4. 用到哪

### 4.1 记录成员校验（`record-member-unknown`）

`membersOf` 遇到 `TypeText` 为 `RECORD LIKE t.*` 且 `t` 在 schema 里时，
用表的列作为成员集合，不再整体跳过。

实测（15108 文件，开启 `recordMemberDiagnostics: warning`）：

| 状态 | `record-member-unknown` |
|---|---:|
| 没有 schema（旧行为） | 0（全部静默） |
| 有 schema | **24**（0.16% 的文件，涉及 4 个文件） |

顺手修掉了一类真实误报：`com.darcy.Hello.newHello(...)` 这种**外部类限定名**，
旧逻辑取"最后一段标识符"当变量名，`darcy` 恰好与记录变量同名，于是把
`Hello` 判成 `darcy` 的成员（cbmi100.4gl 2 处）。现在 `object` 必须是**简单
变量**（`rec` 或 `arr[i]`），含 `.` 的一律不校验。

**但仍然默认 off**：24 条里有一个文件（`ghrr034.4gl`）占 17 条，其 `sr1`
是 `FOREACH ... INTO sr1.*` 的目标记录，源码里引用的 `sr1.hrar02`/`sr1.wshu`
确实不在 `DEFINE sr1 RECORD` 里——可能是构建版本与检出版本不一致的历史代码。
默认开启会让用户先看到一片红再自行判断，不如显式开启。

### 4.2 补全（`r.`）

`r` 是 `RECORD LIKE t.*` 时，`.` 之后直接给表的全部列，Detail 显示
`maj_file 的列（schema）`，按 `sortText` 排在最前；没有 schema 信息时
完全退回旧行为（表单字段 + 同名符号）。

这是 schema 索引**默认可见**的收益：不产生任何诊断，纯粹是把"猜"换成"知道"。

## 5. 配置

| 键 | 默认 | 说明 |
|---|---|---|
| `bdl.schema.enable` | `true` | 关掉后不解析 `.sch`，`LIKE t.*` 退回"不校验"、补全退回旧行为 |
| `bdl.schema.paths` | `[]` | 额外的 schema 目录（相对工作区根或绝对路径）；默认找 `<模组根>/<模组>/sch` |

## 6. 测试

| 测试 | 覆盖 |
|---|---|
| `internal/schema` `TestParseCreateAndAlter` | 建表/加列/多列 alter/约束过滤 |
| `TestParseToleratesCorpusTypos` | `;:` 笔误、截断文件 |
| `TestMergeAndLookup` | 大小写不敏感、alter 合并、缺失表返回 nil |
| `TestAlterOnlyTable` | 只有 alter 的表标记 `AlterOnly` |
| `TestCorpusSchema` | 真实语料：3127 文件 / 2879 表 / 可疑语句 0 |
| `internal/server` `TestSchemaRecordMembersKnown` | `LIKE t.*` 的合法列不报 |
| `TestSchemaRecordMemberUnknown` | 非列成员应报 |
| `TestSchemaMissingTableStaysSilent` | 表不在 schema → 静默（宁漏勿误） |
| `TestSchemaMembersComplete` | `rec.` 补全给出表的列 |
| `TestRecordObjectName` / `TestSchemaTableFor` / `TestQualifierBeforeDot` | 辅助函数边界 |
| `internal/completion` `TestProvideAfterDot*` | 精确成员优先、前缀过滤、无成员时退回 |
