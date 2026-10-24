# Python 前端：gopapy 双轨解析

> 2026-10-24 第7轮接入。目标：让 zuzo 原生执行 Python 的能力向上兼容（完整 3.14 语法），
> 同时保住既有语料的正确性。

## 一、架构

```
.py 源码
  ├─ gopapy v2（github.com/tamnd/gopapy/v2/parser2，纯 Go，Python 3.14 全语法）
  │     └─ py/pyparse/adapter.go（AST 形状转换）→ zuzo 内部 py/ast
  └─ 兜底：自带 py/parser（手写递归下降，覆盖运行时实际支持子集）
```

- 入口：`py/runtime/parse.go` 的 `ParseSourceWithFile` / `ParseSourceWithFileTrace` / `ParseExprDual`
- 适配层：`py/pyparse/adapter.go`（语句/表达式）+ `ops.go`（运算符口径）
- 排障开关：`ZUZO_PY_BUILTIN_PARSER=1` 强制走自带 parser；`ZUZO_PY_PARSER_TRACE=1` 在 stderr 打出实际用的解析器

## 二、适配层的三个口径差

| 差异 | gopapy | zuzo 运行时 | 处理 |
|---|---|---|---|
| 数值/字符串常量 | 统一 `Constant{Kind, Value}` | `Number`（原文串）/`String` | 按 Kind 还原；int64→十进制串、float64→`FormatFloat('g')` |
| 运算符 | CPython 类名（`Add`/`USub`/`And`/`LtE`） | 符号（`+`/`-`/`and`/`<=`） | `ops.go` 映射表 |
| match 模式 | `MatchValue/MatchSequence/MatchAs/...` | `PValue/PSequence/PCapture/...` | 逐类型映射，`MatchAs{Pattern:nil}` 分化为 PCapture/PWildcard |

## 三、必须回退自带 parser 的四类源码（实测踩坑）

1. **字符串转义**：gopapy 只解码 `\n \t`，对 `\x \u \U \N{} \101` 原样保留（`lex.go:819-843`）——
   半解码状态无法在适配层安全补救，源码含 `\` 即整文件回退。
2. **超长整数字面量**：`strconv.ParseInt(...,64)` 溢出直接报错（`parser.go:1083-1101`），
   自带 parser 支持任意精度（运行时用 `big.Int`）。
3. **`except*`**：星号被丢弃（`stmt.go:930`），会静默改变异常组语义。
4. **推导式首子句 `async for`**：被拒绝（`parser.go:1197`）。

守卫实现：`py/runtime/parse.go` 的 `needsBuiltinParserGuard`（含 19 位以上连续数字扫描）。

## 四、实测

| 场景 | 自带 parser | gopapy | 说明 |
|---|---|---|---|
| 23 个用例（31.6 KB）纯解析 | 4.40 ms | 1.67 ms | 约 2.6× |
| 1 万行赋值脚本端到端 | 30.33 ms | 27.50 ms | -9.3%（其余是执行成本） |
| 31 个用例总耗时 | 18.93 ms | 19.21 ms | 持平（启动占主导） |
| 命中率 | — | 24/31 | 其余 7 个因守卫或 runtime 缺口回退 |

## 五、边界与后续

- gopapy 能解析但运行时**不执行**的构造（需补 runtime）：`match` 高级模式、`t-string`（PEP 750）、
  `type` 别名（PEP 695）、`except*`、推导式 async、`__cause__`（`raise from` 链）、
  f-string 自文档 `{x=}`。
- 运行时节点缺口矩阵见 `tmp/py-runtime-node-matrix.md`（71 节点：55 完整 / 16 部分 / 0 缺失）。
- 若要让 gopapy 完全替代自带 parser，需先补：转义解码表、BigInt 常量、TryStar/IsAsync 三块。