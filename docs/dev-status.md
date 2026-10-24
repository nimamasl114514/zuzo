# 开发状态（2026-10-24）

> 本文件记录当前测试状态与已知缺口，避免「README 说能跑、实际不能跑」的落差。

## 一、测试状态

| 套件 | 结果 | 说明 |
|---|---|---|
| `run_tests.sh`（Python 对拍） | **25 / 33 通过** | 7 个失败为运行时能力缺口（见 §二），1 个 helper 跳过 |
| `run_tests_mix.sh`（混合 golden） | **15 / 15 通过** | 切块、类型桥、调用桥、作用域同步 |
| `go test ./...` | **全绿** | `ast` / `mix` / `tests` 三包（含原生循环、SAP、De Bruijn 基准） |
| `gofmt -l .` | 干净 | CI 强制 |

## 二、7 个失败用例与根因

| 用例 | 缺失能力 | 位置 |
|---|---|---|
| `t23_walrus` | `re.finditer` 未实现 | `py/runtime/stdlib*.go` |
| `t24_decorator_args` | 带参装饰器的参数绑定边界 | `py/runtime/call.go` |
| `t25_fstring_nested` | f-string 自文档 `{x=}` 与嵌套格式 | `py/runtime/expr.go` |
| `t26_raisefrom` | 异常链 `__cause__` / `__context__` 未建模 | `py/object` 异常对象 |
| `t27_with_multi` | 多上下文 `with a() as x, b() as y` 的 `__enter__` 链 | `py/runtime/interp.go` |
| `t29_async` | `For.IsAsync` / `With.IsAsync` 字段声明后从未被读取（async 退化为同步） | `py/ast/ast.go:88,183` |
| `t30_annotations` | `AnnAssign` 注解表达式不求值；类 `__annotations__` 值统一填 `"annotation"` | `py/runtime/interp.go:236`、`expr.go:495` |

## 三、已确认的结构性缺口

1. **`containsYield` walker 漏遍历**（`py/runtime/expr.go:535`）：未覆盖 `While.Orelse` / `For.Orelse` / `MatchCase.Body`，
   导致 yield 写在这些分支里的函数被误判为非生成器，运行时报 `SyntaxError: 'yield' outside function`。
2. **运行时节点覆盖**：71 个 py/ast 节点 = 55 完整 / 16 部分 / 0 缺失（矩阵见开发日志第 7 轮引用的 `tmp/py-runtime-node-matrix.md`）。
3. **int64 溢出**：zuzo 的 De Bruijn 原生循环与 Python 原生循环都直接 int64 运算，Python 语义应为大整数。
4. **gopapy 前端的四个已知坑**（详见 `docs/python-parser.md`）：转义半解码、超长整数报错、`except*`、推导式首子句 `async for`——
   已用「源码特征守卫 + 整文件回退」兜住，不影响正确性，只影响那类文件的解析速度。

## 四、路线图（按 ROI）

- [ ] **P0** 修 `containsYield` walker（三处补遍历）
- [ ] **P1** 异常链 `__cause__` / `__context__`
- [ ] **P1** `IsAsync` 落地（`async for` / `async with` / 异步推导式）
- [ ] **P2** f-string `{x=}` 与嵌套格式说明符
- [ ] **P2** `Environment` 存储收敛（store / slots / valueSlots / slotMap / context 五份状态合并为「槽位唯一 + map 视图」）
- [ ] **P3** 原生循环溢出预检
- [ ] **P3** token Literal 惰性化（解析侧 GC 占 ~30%）

## 五、如何复现状态

```bash
cd zuzo
go build -o zuzo.exe ./
./run_tests.sh            # 期望 PASS=25 FAIL=7
./run_tests_mix.sh        # 期望 MIX PASS=15 FAIL=0
go test ./...             # 期望全部 ok
```