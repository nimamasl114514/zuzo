# 开发状态（2026-11-07）

> 本文件记录当前测试状态与已知缺口，避免「README 说能跑、实际不能跑」的落差。
> 上一次全面刷新：**2026-11-07 第34轮**（以下数字均为当轮实跑；命令可直接复制复现）。

## 一、测试矩阵（2026-11-07 实测）

| 套件 | 命令 | 结果 | 说明 |
|---|---|---|---|
| Python 语义对拍 | `./run_tests.sh` | **33 / 33** | 与 CPython 3.12 逐字节 diff |
| 影子模式（双轨） | `./run_tests.sh`（内含 `--shadow`） | **SHADOW 33 / 33** | SAP + Tree-Walk 双跑一致 |
| 混合语言 golden | `./run_tests_mix.sh` | **15 / 15** | 切块、类型桥、调用桥、作用域同步 |
| LLVM 后端对拍 | `./run_tests_llvm.sh` | **20 / 20** | 无 clang 时自动用自带 `zig cc` |
| CPython 子进程桥 | `./run_tests_bridge.sh` | **19 / 19** | numpy / pandas / requests / 错误 / 句柄 / 索引 / 点号 |
| 自带工具链 | `./run_tests_toolchain.sh` | **6 / 6** | status / env / zig 构建对拍 / zpm doctor / zpm build / URL 直下 |
| Go 单测 | `CGO_ENABLED=1 CC="/c/Dev/zig/zig cc" go test ./...` | **9 包全绿** | `ast` `diagnostic` `evaluator` `fuzz` `infer` `llvmbackend` `mix` `pyext` `tests`；另有 19 包无测试文件 |
| 语言用例运行器 | `./zuzo.exe test examples/tests` | **PASS 1 / FAIL 1**（退出码 1） | 样例目录故意含一个失败用例；单跑 `ok_test.hy` 为 PASS 1 / FAIL 0 |

> 本机没有 gcc，所有 cgo 构建统一用 `CGO_ENABLED=1 CC="/c/Dev/zig/zig cc"`。
> `run_tests.sh` 的用例数会随 `tests_py/` 增长，以脚本末行 `PASS=n FAIL=m` 为准。

## 二、相对 2026-10-24 快照已闭环的项

1. **`t25_fstring_nested` / `t29_async` / `t30_annotations` 全部通过** —— 实跑 `./zuzo.exe tests_py/t25_fstring_nested.py` 等三条，退出码均为 0（旧文档称「3 个失败用例」，已作废）。
2. **`containsYield` walker 已覆盖全部分支** —— `py/runtime/expr.go` 的 walker 已处理 `Try` / `For` / `While` / `With`（639-652 行）与 `While` / `For` / `Match`（848-864 行），yield 写在这些分支里不再被误判。
3. **内嵌 Python 标准库四项已可导入** —— `sqlite3`（实测 `sqlite_version = 3.53.3`）、`ssl`、`http.server`、`multiprocessing` 均 `import` 成功（旧文档称四项 `ModuleNotFoundError`，已作废）。
4. **`ctypes.create_string_buffer` 可用** —— 实测 `len(create_string_buffer(8)) == 8`。
5. **异常链 / `IsAsync` / f-string 嵌套格式** —— 已随第 16~32 轮修复，当前 `run_tests` 33/33 覆盖。

## 三、仍存在的结构性缺口

1. **int64 溢出**：zuzo 的 De Bruijn 原生循环与 Python 原生循环都直接做 int64 运算，Python 语义应为任意精度大整数；超出 2^63-1 的行为与 CPython 不一致。
2. **gopapy 前端四个已知坑**（详见 `docs/python-parser.md`）：转义半解码、超长整数报错、`except*`、推导式首子句 `async for` —— 已用「源码特征守卫 + 整文件回退」兜住，不影响正确性，只影响那类文件的解析速度。
3. **GUI 自绘缺失**：无窗口 / 控件 / 绘图库；`打开浏览器()` 是驱动外部 Chromium（chromedp），不是 GUI 框架。
4. **Linux / macOS 移植未完成**：CFFI 加载路径与浏览器内置依赖 Windows API，CI 目前跑 `windows-latest`。
5. **`GC统计().live` 是近似值**：口径为「根环境链上的变量数」，不是真正的堆对象可达集合；用于观察长跑程序是否线性增长足够，但不能当作精确 GC 统计。

## 四、路线图（按 ROI）

- [x] **P0** 修 `containsYield` walker（三处补遍历）
- [x] **P1** 异常链 `__cause__` / `__context__`
- [x] **P1** `IsAsync` 落地（`async for` / `async with` / 异步推导式）
- [x] **P2** f-string `{x=}` 与嵌套格式说明符
- [x] **P2** CPython 子进程桥：连接池 / 句柄 LRU / DataFrame 索引 / 模块亲和
- [x] **P2** 工具链：自带 zig cc / URL 直下 / 发布档位（`--release=O0..Oz`）
- [ ] **P1** 大整数（消除 int64 溢出，向 CPython 语义靠拢）
- [ ] **P2** `Environment` 存储收敛（store / slots / valueSlots / slotMap / context 五份状态合并为「槽位唯一 + map 视图」）
- [ ] **P2** Linux / macOS 移植
- [ ] **P3** GUI 内置（窗口 / 控件 / 绘图）

---

*最后更新：2026-11-07 第34轮 · 依据：当轮六套脚本 + `go test ./...` + 逐条功能实测。*
