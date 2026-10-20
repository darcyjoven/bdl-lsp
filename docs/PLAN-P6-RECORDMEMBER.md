# P6 记录：`BY NAME rec.member` 记录成员校验

> 版本：v1.0（已实现，**默认关闭**）  
> 日期：2026-10-21  
> 状态：**已完成**（语料评估见 §4；默认 off，由用户开启）。  
> 上游：[P6 语义高亮](PLAN-P6-SEMTOK.md)｜[粗略计划](PLAN.md) §7.2｜[待办](TODO.md)

---

## 1. 交付内容

```text
internal/server/recordname.go       # 新文件
├── checkRecordMembers              # 遍历 object 节点，按作用域校验
├── buildRecordScopes               # 按函数容器建"变量 → 成员集合"
├── membersOf                       # 从符号 Children 提成员 + opaque 判定
├── containerLookup                 # 字节偏移 → 容器名（取最内层）
├── lookupRecord / lookupRecordType  # 两级作用域查找
├── recordMemberAt                  # 单个 `rec.member` 的校验
└── isBuiltinMemberMethod           # 排除数组/集合内置方法

internal/server/recordname_test.go  # 8 个协议级测试
internal/config                     # RecordMemberDiagnostics（默认 off）
internal/server/diagnostics.go      # 接入 reparseFull
cmd/record-audit/                   # 语料误报率评估工具
```

## 2. 语料形态（决定功能边界）

`tiptop-gp` 全量扫描：

| 形态 | 数量 | 处理 |
|---|---|---|
| `DISPLAY BY NAME` | 51267 | 主体 |
| `INPUT BY NAME` | 5474 | 主体 |
| `CONSTRUCT BY NAME` | 4456 | 主体 |
| `BY NAME rec.*` | 507（0.8%） | **不校验** |

### 2.1 为什么 `rec.*` 不需要校验

语法树里 `DISPLAY BY NAME tm.*` 长这样：

```text
(display field: (variable (object object: (variable (identifier)))))
```

**没有 `property` 节点**——`*` 被 grammar 当作通配吞掉了。这不是缺陷：
`. *` 的语义就是"这个记录的所有字段"，本来就不存在"某个具体成员是否存在"
的问题。而 `tm.a` 有 `property: (identifier)`，可以逐个校验。

## 3. 作用域：五个真实踩坑

这些坑都是 P3b「整文件线性跟踪」那一个陷阱的变体。语料评估时逐个暴露：

### 3.1 按函数隔离（同名变量不同类型）

`aapg111.4gl`：

```genero
FUNCTION g111()
   DEFINE sr1 RECORD          -- 内联记录，含 npq02/npp02
      npq02 LIKE npq_file.npq02,
      ...
   END RECORD
END FUNCTION

FUNCTION aapg111_grdata()
   DEFINE sr1 sr1_t           -- 具名类型，不含 npq02
END FUNCTION
```

文件级平坦映射会把 `g111` 里的 `sr1.npp02` 判成"`sr1_t` 没有 `npp02`"。

### 3.2 模块级声明对函数内可见

顶层 `DEFINE tm t_rec` 的 `Container` 是 `""`，而它在 `MAIN` 里可用。
只查函数作用域会漏掉绝大多数真实用法。所以查找是**两级**：

```text
使用处所在函数 → 模块级
```

函数内同名优先——这正是 §3.1 的修法。

### 3.3 内联 RECORD 是它自己的类型

`DEFINE x RECORD ... END RECORD` 的成员就挂在符号的 `Children` 上，
不该再去按名字找 `TYPE` 声明。

### 3.4 类型表必须带容器前缀

```go
scopeKey := func(container, name string) string { return container + "\x00" + name }
```

只用符号名做 key，会让不同作用域的同名记录互相覆盖。实测
`s_aapt110_gl.4gl`：`t110_g_gl_1` 的 `l_api RECORD LIKE api_file.*`
（不可校验）被模块级同名 `l_api`（20 个可见成员）覆盖，导致 61 处
`l_api.api041` 误报。**这一个修复把误报率从 1.1% 降到 0.4%**。

### 3.5 参数遮蔽外层

`REPORT r(rvu)` + 体内 `DEFINE rvu RECORD LIKE rvu_file.*` 是常见写法。
参数必须**无条件**覆盖外层同名条目（符号表按出现顺序遍历，只在"不存在时"
写入会漏掉）。参数本身按"已知但不可校验"处理（`nil`）。

## 4. 语料评估（自动）

```text
$ go run ./cmd/record-audit            # 全量 7683 个 .4gl
文件总数          : 7683
rec.member 引用   : 2085436
rec.* 通配        : 99174（语法树无 property，不校验）
可校验（本地类型）: 271760
跳过（LIKE 等）   : 1813676
报告成员不存在    : 20（占可校验的 0.0%）
```

**0.007%**（索引 bug 修复前是 157 条 / 0.1%），且抽查确认真阳性：

```genero
-- abaq102-2.4gl:613（记录里只有 tc_ibb03/tc_ibb04）
" WHERE tc_ibb01<>' ' AND tc_ibb03=",g_tc_ibb_m.tc_ibb003,"AND ...
```

```genero
-- ghrr034.4gl:307（该文件的 sr1 是内联 RECORD，成员是 hrat01/hrat02/...
-- 与 m1..m12，从来没有 hrct05 / wshu）
LET sr.hrct05 = sr1.hrct05
```

> 修完下面的 index bug 后，157 条降到 20 条——**其余 137 条都是那个 bug
> 造成的假阳性**，这是本轮最有价值的一次修复。

## 5. 默认关闭的理由

记录成员静态上只能靠 `TYPE..END RECORD` 的字段列表判断，而：

- `DEFINE r LIKE table.*` —— 字段在数据库 schema 里，静态不可见
  （这部分被"opaque 跳过"处理掉了，占 1813676/2085436 = 87%）；


按 PLAN §7.2 与风险表的决定：**默认 off**，由用户开启并观察。
配置项 `recordMemberDiagnostics`，级别同其他诊断（off/error/warning/...）。

## 6. 踩坑记录（全部写进代码注释）

| 现象 | 根因 | 修法 |
|---|---|---|
| 61 处 `l_api.api041` 误报 | 类型成员表只按符号名做 key，跨作用域覆盖 | key 加容器前缀 |
| `sr.apb` 反复误报 | `apb RECORD LIKE apb_file.*` 是子记录组，内部字段静态不可见 | 有子记录的记录整体视为 opaque |
| `rvu.rvu04` 误报 | 索引在多变量 DEFINE 中把 `sr` 的字段挂到了 `rvu` 上 | 校验 TypeText 是否以 "RECORD" 开头，不匹配就当 opaque |
| `g_bxj.clear()` 误报 | `DYNAMIC ARRAY OF RECORD` 的 TypeText 不含 "ARRAY" | 按内置集合方法名排除 |
| `REPORT r(rvu)` 参数误报 | 参数遮蔽外层同名变量 | 参数无条件覆盖，标记为不可校验 |
| opaque 记录仍误报 | 没把 key 写进作用域 → 查找回退到模块级撞上同名变量 | opaque 也写入 `nil` 占位 |

## 7. 已知限制

| 限制 | 影响 | 计划 |
|---|---|---|
| 默认关闭 | 需要用户显式开启 | 设计如此（误报风险） |
| 不校验 `rec.*` | 通配形态不逐字段校验 | 语义如此（全都要） |
| `LIKE table.*` 的记录不校验 | 占语料 85% 的引用 | 需数据库 schema 索引 |
| 索引多变量 DEFINE 错配未修 | 只能靠 TypeText 检查兜底 | 应修 `internal/index`（见下） |
| 数组元素的成员访问不校验 | `arr[i].field` 是下标表达式 | 未排期 |

## 8. 上游 index bug（已修）

评估过程中发现并修复了 `internal/index` 的一个真实 bug：

```go
// 修复前：typeText 取整条 DEFINE 语句的类型，赋给每个变量
typeText := b.typeText(node)
for i, nameNode := range names {
    sym.TypeText = typeText          // ← 所有变量拿到同一个（第一个）类型
    if i < len(types) {
        if rec := findChild(types[i], "record"); rec != nil { ... }
    }
}
```

一条 DEFINE 用未命名子节点混排多个"名字 + 类型"时（语料里很常见）：

```genero
DEFINE
    rvu  RECORD LIKE rvu_file.*,
    rvv  RECORD LIKE rvv_file.*,
    sr   RECORD
             rva04 LIKE rva_file.rva04,
             ...
         END RECORD
```

后果：
- 三个变量都拿到 `"RECORD LIKE rvu_file.*"` 作为 TypeText；
- `names` 有 3 项、`types` 也有 3 项，但 `sr` 的记录字段被 `collectRecordFields`
  挂到了**错的那个变量**上（实测 `rvu` 拿到了 `sr` 的 10 个字段）；
- 记录字段还以 `KindField` 泄漏到顶层符号表。

**修复**：
1. 只有在 `len(types) == len(names)` 时才逐项配对（结构不符时宁可不给类型，
   让上层跳过语义校验，也不给错误类型）；
2. 类型文本改为 `b.typeText(types[i])`（逐项），不再复用整条语句的。

回归测试：`TestMultiVariableDefineTypes` / `TestSingleVariableDefineType` /
`TestInlineRecordDefine`（`internal/index/index_test.go`）。

在此之前，`recordname.go` 用"TypeText 是否以 RECORD 开头"做兜底检查；
修复后该检查作为防御性断言保留（index 若再退化仍能挡住误报）。
