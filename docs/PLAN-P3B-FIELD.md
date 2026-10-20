# P3b 记录：4GL 字段存在性校验

> 版本：v1.0（已实现）  
> 日期：2026-10-20  
> 状态：**已完成**（验收输出见 §5）。  
> 上游：[粗略计划](PLAN.md) §7.2｜[P3a 记录](PLAN-P3A-GLOBALS.md)｜[待办](TODO.md)  
> 来源：用户原始需求 #4「4GL 中校验字段是否存在」（FEATURES §1）

---

## 1. 交付内容

```text
internal/form/                  # 新包
├── form.go                     # Resolver：路径解析（R1a/R1b/R1c/R2/R3）、缓存、反向依赖
├── check.go                    # Queries + Check：query 提取、按函数作用域跟踪、字段校验
└── form_test.go                # 13 个单测

internal/index/per.go           # 增强
├── LayoutRefs 提取（LAYOUT 的 [tag] 引用）
├── phantom_item 名字从 field_ref 提取（原实现取不到）
└── screen record 裸字段名支持（field_ref 无 column 字段时用 name 节点）

internal/server/form.go         # 接线
├── formChecks：4gl 字段校验（诊断 + relatedInformation + 缓存）
├── perInternalChecks：.per 内部一致性（重复 item/布局 tag/SCREEN RECORD）
└── refreshFormImporters：.per 变更 → 刷新引用方

internal/config                 # fieldDiagnostics / perRoots
cmd/field-audit/                # 语料评估工具（含模块一致性自动标注）
```

## 2. 路径解析规则（语料实测 8209 处 WITH FORM）

| 规则 | 命中 | 说明 |
|---|---|---|
| R1a | 7769 (94%) | `<4gl 目录>/../per/<basename>.per`（tiptop 物理布局） |
| R1b | — | 字面量按引用文件目录拼 |
| R1c | — | perRoots 配置根 |
| R2 | 331 (4%) | 工作区 basename 索引唯一命中 |
| R3 | 7 (0.1%) | 多命中时用字面量首段（模块名）消歧——消歧成功率 100% |
| 未命中 | 102 (1%) | 表单确实不存在（真实缺失） |

歧义处理（Q3 决策）：无法消歧时返回 nil，不校验字段（宁可不报不误报）。

## 3. 作用域跟踪（本功能最大的坑）

**规划**：以函数为作用域、按语句顺序维护"当前表单"。

**实测踩坑**：最初实现为整文件线性跟踪，全量语料跑出 **28224 条**字段问题。
抽查发现 saxmt400_slk.4gl（15000 行、15 个 `WITH FORM`）里函数 A 打开的
表单泄漏到函数 B 的字段引用上——全部是误报。

**修正**：query 增加 `(function)/(main)/(report) @container` 捕获，
表单绑定状态按容器索引隔离（二分查找字节偏移所在函数）。
修正后全量语料降到 **1725 条**（-94%）。

## 4. 语料评估（自动标注）

```text
$ go run ./cmd/field-audit/          # 全量 7699 个 .4gl

文件数            : 7699
含表单引用的文件  : 6006
字段问题总数      : 1725
涉及表单数        : 330

自动标注（模块段一致性：字面量 aap/42f/x 的 aap 应在解析路径中）:
  模块一致   : 1677 (97.2%)
  模块不一致 : 48 (2.8%)  ← 可疑误报（解析到别的模块的表单）
  无模块段   : 0
```

- 2.8% 可疑率 **< 5% 目标**；
- 人工抽查确认发现为真阳性：`aapr106.4gl:232` 的 `CONSTRUCT ... ON apa18`
  引用表单 `aapt110`，`aapt110.per` 里确实没有 `apa18`（在 `aapt110_g.per` 里）
  ——语料存在真实的 .4gl/.per 版本漂移；
- 48 处模块不一致多为跨模块表单引用（R1a 落空后 basename 在别的模块命中），
  属正常行为而非解析错误。

## 5. 验收输出

```text
$ go test ./internal/form/ -v
    --- PASS: TestResolveR1aPerDirectory
    --- PASS: TestResolveNotFound
    --- PASS: TestResolveR2BasenameUnique
    --- PASS: TestResolveR3ModuleSegment
    --- PASS: TestResolveAmbiguousGivesNil
    --- PASS: TestCheckFieldExists
    --- PASS: TestCheckFieldMissing        # 6 类字段引用全部捕获
    --- PASS: TestCheckNoFormNoCheck       # 无绑定不校验
    --- PASS: TestCheckSecondFormSwitches  # 表单切换作用域
    --- PASS: TestCheckDisplayFormByName   # OPEN FORM + DISPLAY FORM
    --- PASS: TestPhantomFieldCountsAsExists
    --- PASS: TestScreenRecordFieldCountsAsExists
    --- PASS: TestOpenDocPreferred
    --- PASS: TestImportersAndRefresh
ok

$ go test ./internal/server/ -run 'Field|Per' -v
    --- PASS: TestFieldNotInFormWarns          # 含 relatedInformation
    --- PASS: TestFieldExistsNoWarn
    --- PASS: TestFieldDiagnosticsOff          # 配置 off
    --- PASS: TestPerDuplicateItem             # .per 重复 item
    --- PASS: TestPerLayoutTagUndefined        # 布局 tag 未定义
    --- PASS: TestPerScreenRecordUndefined     # SCREEN RECORD 引用
    --- PASS: TestPerChangeRefreshes4gl        # .per 变更刷新 4gl
ok

$ make e2e-vscode
[e2e] 用例 18/18 通过
  ✓ 字段不存在于表单产生警告 (85ms)
  ✓ 存在的表单字段不产生警告 (17ms)

$ make bench          # 7138 行 / 329KB
首轮 didOpen → 诊断: 133.3 ms      （规划目标 < 300ms ✅）
增量 didChange 往返: 平均 6.0 ms    （规划目标 < 100ms ✅；字段校验走防抖）
```

## 6. 踩坑记录（全部写进代码注释）

| 现象 | 根因 | 修法 |
|---|---|---|
| query 编译失败，功能静默禁用 | `(form form: ... name: ...)` 字段顺序与 grammar 相反（name 在前）→ Impossible pattern | 按 grammar 顺序写 pattern；注释提醒 |
| 全量语料 2.8 万条误报 | 整文件线性作用域跟踪跨函数泄漏 | `(function/main/report) @container` + 按容器隔离状态 |
| T5 把表单字段误判为未定义变量 | `INPUT ... FROM f` 的 f 是 variable 节点，T5 的变量检查把它当程序变量 | T5 skip query 增加 input/construct/display/hook/infield 五类字段上下文 |
| PHANTOM 字段提取不到 | phantom_item 的名字在 field_ref 里（无 name/identifier 直接子节点） | findChild(field_ref) + column/name 双路取 |
| SCREEN RECORD 裸字段名提取不到 | `sr(sr01)` 的 field_ref 无 column 字段；裸名是名为 "name" 的**节点类型**（ChildByFieldName 取不到） | findChild(fr, "name") 兜底 |
| .per 内部校验 SCREEN RECORD 永不触发 | defined 集合把 SCREEN RECORD 自身的 KindField 也算进去（循环：引用自身即视为已定义） | defined 只收 KindFormItem |
| map 复合字面量漏写 {} | `namedForms := map[string]*Resolved`（类型不是表达式） | 加 `{}` |

## 7. 已知限制

| 限制 | 影响 | 计划 |
|---|---|---|
| `INPUT BY NAME rec.*` 记录成员校验未做 | BY NAME 的隐式字段引用不校验 | P6（用户确认最后做，默认关闭） |
| 动态表单（变量路径）不校验 | `WITH FORM var` 静态无法解析 | 永久降级 |
| 函数内多窗口切换的 CURRENT WINDOW 语义未跟踪 | 极少数多窗口函数可能归因到错误的表单 | 误报率已达标（2.8%），暂不处理 |
| record `.*` 与 schema 列校验 | 需数据库 schema 索引 | P6 |
| e2e 仅 Linux | Windows 未跑 | 与 P3a 相同，等 Windows 机器 |
