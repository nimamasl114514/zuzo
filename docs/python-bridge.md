# Python 子进程桥（`.hy` 无缝调用 CPython 库）

> 第 32 轮（2026-11-01）。目标：**在 `.hy` 里像调内置函数一样调任意已安装的 Python 库**——含 `numpy` / `pandas` / `requests` 这类带 C 扩展的包。
> 事实源优先级：**源码 > 实跑 > 文档**。本文所有数字与输出均为本机实测（Windows 11 / CPython 3.12.13 / numpy 2.5.1 / pandas 3.0.3）。

---

## 一、为什么必须是子进程

`py/` 包是用 Go 重写的 Python 运行时，能跑 Python 语法与常用标准库，但**没有 CPython 的 C-ABI**：

| 层 | 能做到 | 做不到 |
|---|---|---|
| `py/runtime`（纯 Go） | 语法、对象模型、内置函数、`json/re/math/datetime` 等子集 | 加载 `.pyd`/`.so`、调 CPython C-API |
| CPython 子进程 | 全部 site-packages 与 C 扩展 | 与 zuzo 共享进程内对象 |

`numpy` 的 `ndarray` 是 C 结构体，`pandas` 依赖它，`requests` 依赖 `ssl` 的 C 实现——纯 Go 侧无法模拟，只能**让真 CPython 跑起来，zuzo 跨进程下单**：

```
zuzo.exe (Go)                              python.exe (CPython 3.x)
┌────────────────────────────┐             ┌──────────────────────────────┐
│ py导入 / py调 / py方法      │   stdin →   │ child.py                     │
│ py取 / py设 / py取值        │             │  协议循环（JSON Lines）      │
│ pyext/client.go             │   ← stdout  │  句柄表 map[int, object]     │
│ 惰性启动·探测·超时·互斥      │   stderr ←  │  真实 import / call          │
└────────────────────────────┘             └──────────────────────────────┘
```

- **stdin/stdout 专供协议**（一行一条 JSON），用户 `print()` 全部重定向到 stderr，不污染协议流；
- **句柄语义**：模块、`ndarray`、`DataFrame`、函数不序列化，zuzo 侧只持 `Python对象`（ID + 类型名 + repr + 可调用标志），操作回子进程执行；
- **惰性**：只有真的用 `py*` 才拉起进程，纯 `.hy` 脚本零额外开销。

---

## 二、`zuzo pip`：装完就能用

桥用的是「本机某个真实 CPython」，所以装包也走同一个解释器，命令与 pip 完全一致（不会出现「装到 A 解释器、桥用 B 解释器」的经典错位）：

```bash
./zuzo.exe pip install numpy pandas requests   # 装包（透传全部 pip 参数）
./zuzo.exe pip list                            # 看已装
./zuzo.exe pip 安装 humanize                    # 中文子命令别名：安装 / 卸载 / 列表 / 版本
./zuzo.exe pip --version                       # 打印是哪个 python 的 pip
```

实测（本机）：

```bash
$ ./zuzo.exe pip install humanize
Successfully installed humanize-4.16.0

$ cat 示例.hy
变量 h = py导入("humanize")
打印(py调(py取(h, "intcomma"), 1234567))

$ ./zuzo.exe 示例.hy
1,234,567
```

三条口径：

1. **同一个解释器**：`zuzo pip` 与 `py导入` 共用桥探测到的 CPython（`ZUZO_PYTHON` 可指定），装完立刻可用；常驻子进程还会在导入失败时 `invalidate_caches()` 重试一次，避免「装了但当前进程看不到」。
2. **不隐式联网**：只有你敲 `zuzo pip install` 才会联网；`.hy` 运行时绝不偷偷装包。
3. **缺库有指引**：`py导入("xxx")` 失败时错误信息末尾直接给命令：`提示：该库未安装 → ./zuzo.exe pip install xxx`。

---

## 三、点号语法（推荐写法）

### 3.1 最省事的入口：`导入`

```zuzo
导入 numpy                 // 变量 numpy 自动绑定模块句柄
导入 pandas 为 pd           // 起别名
导入 numpy.linalg 为 la     // 点分模块名也支持

打印(numpy.array([1, 2, 3]).sum())   // 6
打印(pd.DataFrame({"a": [1, 2]}).shape)   // [2, 1]
打印(la.norm(numpy.array([3.0, 4.0])))    // 5
```

- 等价写法仍有 `变量 np = py导入("numpy")`（动态拼名字时用得上）。
- `导入 "文件.hy"` 的本地模块语义不变；**只有当目标是标识符（不是字符串）时才走 CPython**。
- 缺库时错误信息带 `提示：该库未安装 → ./zuzo.exe pip install numpy`。

### 3.2 句柄上的点号 / 下标 / 遍历

`.hy` 里的 `a.b` / `a.b(...)` / `a["k"]` **对 Python 句柄直接生效**，写起来和 Python 一样，不必再 `py调(py取(h,"方法"), …)`：

```zuzo
变量 np = py导入("numpy")
变量 a = np.array([[1, 2, 3], [4, 5, 6]])

打印(a.shape)                 // [2, 3]        —— 取属性
打印(a.sum())                 // 21            —— 调方法
打印(a.sum(1).tolist())       // [6, 15]       —— 链式 + 参数
打印(a.T.tolist())            // [[1,4],[2,5],[3,6]]
打印(a.dtype.name)            // int64
打印(np.linalg.norm(np.array([3.0, 4.0])))   // 5

变量 pd = py导入("pandas")
变量 df = pd.DataFrame({"城市": ["北京", "上海", "上海"], "销量": [3, 5, 2]})
打印(df["销量"].sum())        // 10            —— 下标读取
df["新列"] = [1, 1, 1]         // [3, 3]        —— 下标赋值（新增列）
打印(df.groupby("城市").sum().reset_index().to_dict("records"))

变量 json = py导入("json")
打印(json.dumps({"甲": 1}, ensure_ascii: 假))   // 关键字参数用「名: 值」
```

四条规则：

| 写法 | 语义 | 说明 |
|---|---|---|
| `h.名` | `getattr(h, "名")`；取不到再试 `h["名"]` | 覆盖 `np.pi` 与 `dict["键"]` 两种习惯 |
| `h.名(参数…)` | 取属性后调用 | 参数是变参；关键字参数写 `名: 值` |
| `h["键"]` | `h["键"]`（非字符串键一律走 `__getitem__`） | `df["列"]` / `a[0]` |
| `h["键"] = 值` / `h.名 = 值` | 先 `__setitem__`，没有则 `setattr` | `df["新列"] = …` 直接生效 |
| `长度(h)` | `len(h)` | `长度(a)` → 第一维长度 |
| `对于 x 在 h { … }` | 先拍平成数组再遍历 | ndarray / Series / list 都能直接 for-in |

**作用域限定**：点号只对 `Python对象` 句柄生效；普通 zuzo 值（数组/哈希/模块/实例）仍走原有语义，不受影响（回归：`run_tests` 33/33、`mix` 15/15、`llvm` 20/20 全绿）。
**仍是句柄语义**：链式调用的中间结果都是句柄，要拍平就 `.tolist()` / `py取值(h)`。
**老写法仍在**：`py取 / py调 / py方法 / py调参` 一个都没删，脚本可混用。

---

## 四、开关与环境变量

| 环境变量 | 取值 | 行为 |
|---|---|---|
| `ZUZO_PY_BRIDGE` | 未设 / 其他值 | **auto**：`py执行`/`py求值` 失败时自动切子进程 |
| | `0` / `off` / `false` | **禁用**：保持纯 Go 语义（老版本行为），C 扩展照旧报错 |
| `ZUZO_PYTHON` | 解释器路径 | 覆盖自动探测（顺序：`ZUZO_PYTHON` → `python` → `python3` → 常见安装位置 → `py -3`） |
| `ZUZO_PY_TIMEOUT` | 毫秒，默认 `120000` | 单次往返上限；超时杀子进程并在下次调用重启 |
| `ZUZO_PY_BRIDGE_QUIET` | `1` | 不打印模式切换提示 |
| `ZUZO_PY_POOL` | 整数，默认 `1`，上限 `32` | **连接池工作进程数**（第33轮）：`N>1` 时创建 N 个子进程，`py调/py方法/py取值` 等 op 按 round-robin 路由；每个 worker 独立 globals，句柄带 worker 前缀（跨 worker 句柄会被拒绝）。双 worker 实测 **1.92×** 加速 |
| `ZUZO_PY_MAX_HANDLES` | 整数，默认 `10000` | **句柄表 LRU 上限**（第33轮）：超限时释放最久未用的句柄，避免长跑程序远端引用无限增长 |

解释器探测会先跑 `-c "import sys;print(major.minor)"` 验证是 **CPython ≥ 3.8**；不满足的候选逐个跳过，错误里保留每个候选的失败原因。

---

## 五、自动降级：老代码不改一行

```zuzo
py执行("import numpy as np")     // 第 1 步：纯 Go 运行时尝试 → 在 numpy/core/_multiarray_umath 处失败
                                  // 第 2 步：识别为「需要真 CPython」→ 自动拉起子进程
                                  // 第 3 步：子进程重放同一段代码 → 成功
打印(py求值("int(np.arange(10).sum())"))   // 45（此后粘性子进程模式，共用同一份 globals）
```

判定不是只看 `ImportError`：实测 numpy 的 `__init__.py` 是纯 Python，真正炸点是
`AttributeError: module 'os' has no attribute 'add_dll_directory'`（delvewheel 补丁），所以判据包含
`needs external CPython` / `C extension` / `add_dll_directory` / `site-packages` / `.pyd` / `delvewheel`，
以及「代码里有 `import` 且报 ImportError / ModuleNotFoundError / AttributeError」。

- **粘性**：降级成功后，本进程后续所有 `py执行`/`py求值`/`py导入`/`py获取`/`py设置`/`py调用` 都走子进程；
- **可查可切**：`py模式()` 返回 `"本地"` / `"子进程"`；`py模式("子进程")` 强制切换，`py模式("本地")` 切回；
- **副作用不迁移**：降级前在纯 Go 运行时里赋的变量不会搬到子进程（失败的那段代码是在子进程里**重放**的）。需要连续状态就直接 `py模式("子进程")` 起手。

---

## 六、内置函数速查

| 函数 | 签名 | 返回 | 示例 |
|---|---|---|---|
| `py导入` | `py导入(模块名)` | 模块句柄 | `py导入("numpy")` |
| `py取` | `py取(句柄, 属性名)` | 值 / 句柄 | `py取(np, "pi")` |
| `py设` | `py设(句柄, 属性名, 值)` | 空 | `py设(df, "c", [7,7,7])` |
| `py调` | `py调(可调用句柄, 参数...)` | 值 / 句柄 | `py调(py取(np,"sum"), [1,2,3])` |
| `py调参` | `py调参(可调用句柄, 参数数组, 命名参数字典)` | 值 / 句柄 | `py调参(f, [1], {"timeout": 10})` |
| `py方法` | `py方法(句柄, "方法名", 参数...)` | 值 / 句柄 | `py方法(df, "head", 5)` |
| `py取值` | `py取值(句柄)` / `py取值(句柄, 键)` / `py取值(df, "records+index")` | 原生值 / 值 / 哈希 | `py取值(a)`（ndarray→嵌套数组）· `py取值(df,"a")`（取列）· `py取值(df,"records+index")` → `{索引: [...], 记录: [...]}`（保留 DataFrame 索引） |
| `py描述` | `py描述(句柄)` | 字符串（repr） | `py描述(np)` |
| `py类型` | `py类型(句柄)` | 字符串（远端类型名） | `py类型(a)` → `ndarray` |
| `py释放` | `py释放(句柄)` | 空 | `py释放(df)` |
| `py模式` | `py模式()` / `py模式("子进程"\|"本地")` | 字符串 | `py模式("子进程")` |
| `py信息` | `py信息()` | 哈希表 | `py信息()["版本"]` |
| `py有库` | `py有库(模块名)` | 布尔（`find_spec`，不导入） | `py有库("torch")` |
| `py安装` | `py安装(包名)` | 哈希 `{成功, 输出, 错误?}` | `py安装("openpyxl")` |

要点：

- **优先用点号**（见第三节）：`np.array(…)` / `df["列"].sum()`；下表是点号不可用时的等价写法（如句柄存在哈希里、动态拼方法名）。
- **参数是变参**：`py调(f, 1, 2)` 就是 `f(1, 2)`；要传数组本身请直接写字面量 `py调(f, [1,2,3])`。
- `py方法(x, "m", ...)` = `py调(py取(x, "m"), ...)`，省一次取属性。
- `py取值(h)` 是「把远端对象拍平成原生值」：`ndarray→嵌套数组`、`DataFrame→records`、`Series→数组`、`set/tuple→数组`、`bytes→base64 文本`。
- **`py取值(df, "records+index")`**（第33轮）保留 DataFrame 索引：返回 `{索引: [索引值...], 记录: [{列: 值}, ...]}`，解决 `groupby` 后索引丢失问题。代价：列名恰好叫 `records` / `records+index` 的取列需改用 `py方法(df, "__getitem__", "records")`。
- **连接池**（第33轮）：`ZUZO_PY_POOL=4` 时四子进程轮询，适合 zuzo `并发` 同时打多个 Python 调用；同一组操作需在同一 worker 完成（跨 worker 句柄会被拒绝并返回 `E912`）。
- `py有库` 与 `py安装` 是显式动作；桥**不会**在背后偷偷联网装包。

---

## 七、值转换规则

### 5.1 zuzo → Python

| zuzo | Python |
|---|---|
| 整数 / 浮点 / 字符串 / 布尔 / 空 | `int` / `float` / `str` / `bool` / `None` |
| 数组 | `list`（递归，快照） |
| 字典 | `dict`（键按 zuzo 哈希键转换） |
| `Python对象` 句柄 | **原对象本身**（引用） |
| `.hy` 函数 / 内置 | 转成字符串，不能回调（桥是单向的） |

### 5.2 Python → zuzo

| Python 值 | zuzo 侧 | 语义 |
|---|---|---|
| `None` / `bool` / `int` / `float` / `str` | 同名标量 | 快照；`int` 超出 int64 → 字符串（不静默截断） |
| `bytes` / `bytearray` | 字符串（base64） | 快照 |
| `list` / `tuple` / `range` / `set` / `frozenset` | 数组 | 快照 |
| `dict` | 字典 | 快照 |
| numpy 标量（`shape == ()`） | 数值 | 快照（`item()`） |
| **`ndarray`** | **句柄**（`Python对象`） | 引用——保留 `.sum()` / `.reshape()` / `.dtype` 全部方法 |
| `DataFrame` / `Series` / 模块 / 函数 / 类实例 | 句柄 | 引用 |

`ndarray` 一律给句柄（第 32 轮修订）：把数组内联成 zuzo 数组虽然好看，但会丢掉全部方法，而「调库」的价值恰恰在方法。要原生数组就显式 `py取值(h)`：

```zuzo
变量 np = py导入("numpy")
变量 a = py方法(np, "arange", 6)
打印(py取值(a))                  // [0, 1, 2, 3, 4, 5]
打印(py方法(py方法(a, "reshape", [2,3]), "tolist"))   // [[0, 1, 2], [3, 4, 5]]
```

### 5.3 句柄 vs 快照

一句话：**句柄是引用，数组/字典是快照**。`py方法(df, "to_numpy")` 拿到的是句柄；`py取值(...)` 之后与远端对象再无关系。回写远端只能靠 `py设` / `py方法(..., "__setitem__", ...)`。

---

## 八、完整示例

### 6.1 numpy：数组与方法链

```zuzo
变量 np = py导入("numpy")
变量 a = np.array([[1, 2, 3], [4, 5, 6]])
打印(py类型(a))                       // ndarray
打印(a.shape)                          // [2, 3]
打印(a.sum())                          // 21
打印(a.mean())                         // 3.5
打印(a.reshape([3, 2]).tolist())       // [[1, 2], [3, 4], [5, 6]]
打印(a.dtype.name)                     // int64
```

### 6.2 pandas：构造 / 选列 / 聚合 / 增列

```zuzo
变量 pd = py导入("pandas")
变量 df = pd.DataFrame({"a": [1,2,3], "b": [10,20,30]})
打印(df.shape)                          // [3, 2]
打印(df["a"].sum())                     // 6
打印(df.sum().sum())                    // 66
df["c"] = [7, 7, 7]
打印(df.shape)                          // [3, 3]
打印(df.to_dict("records"))             // [{a:1,b:10,c:7}, ...]
```

### 6.3 requests：调已安装库（不联网也能验证属性链）

```zuzo
变量 requests = py导入("requests")
打印(py取(requests, "__version__"))                    // 2.32.3
打印(py取(py取(requests, "exceptions"), "RequestException"))  // <class 'requests.exceptions.RequestException'>
```

### 6.4 关键字参数

```zuzo
变量 json = py导入("json")
变量 文本 = py调参(py取(json, "dumps"), [{"甲": 1}], {"ensure_ascii": 假, "indent": 2})
打印(文本)
```

---

## 九、错误处理

| 错误码 | 含义 | 典型触发 | 处理 |
|---|---|---|---|
| `E910` | 桥启动失败 | 找不到 CPython / 握手失败 | 设 `ZUZO_PYTHON` |
| `E911` | 参数错误 | `py调` 第一个参数不是句柄、参数个数不对 | 检查调用 |
| `E912` | 远端异常 | Python 抛 `TypeError` / `ImportError`…（消息带 traceback） | 看 traceback 尾部 |
| `E913` | 目标不是句柄 / 不可调用 | 对数组（快照）调用 `py方法` | 先 `py导入`/`py取`/`py方法`；数组要操作请传参给 Python 函数 |
| `E914` | 值不能过桥 | 把 `.hy` 函数 / 内置当参数传给 Python | 桥是单向的，改传标量/数组/字典/句柄 |
| `E903` / `E904` | `py求值` / `py执行` 的错误（mix 桥口径） | 语法错误、远端异常 | 同上 |

```zuzo
执行 {
    py求值("1/0")
} 遇到 e {
    打印(错误码(e))                       // E903
    打印(包含(错误信息(e), "ZeroDivisionError"))   // 真
}
```

远端异常一律转成 zuzo 错误对象，**不会让宿主崩溃**；报错后桥保持可用（实测 t12 / t14 用例）。

**句柄会话戳（防 ABA）**：子进程启动时生成 `session` 随机串，`hello` 返回给 Go 侧；每个 `Python对象` 句柄都带这个戳。子进程崩溃/被杀后自动重启，**重启前拿到的旧句柄一律报「句柄已失效」**，不会因为新进程句柄表从 1 重分配而静默指向另一个对象（实测 t14：杀进程 → 重启 → 旧句柄报错 → 新句柄正常）。

---

## 十、性能（本机实测）

| 场景 | 数值 | 来源 |
|---|---|---|
| 单次往返（`py求值("1+1")`） | **134 µs/次** | `go test ./pyext -bench BenchmarkEval`（300 次均值） |
| 已导入模块再 `py导入("numpy")` | **0.45 ms** | `BenchmarkImportNumpy` |
| 冷启动（拉起 CPython + 握手） | **≈ 0.42 s** | `time ./zuzo.exe -e 'py模式("子进程")'` |
| 10 万元素 numpy 求和 | 亚秒级，一次性 | `tests_bridge/t09_bigdata.hy` |

口径：跨进程往返是**微秒级**，适合「大计算留在 Python、zuzo 只发指令」的用法；不要用它跑百万次小循环（那属于纯 Go 运行时的场景）。

---

## 十一、边界

1. **单向调用**：CPython 不能回调 `.hy` 函数（桥只传值，不传函数）。
2. **快照语义**：数组/字典过桥即拷贝，改副本不影响远端；改远端必须 `py设` / `__setitem__`。
3. **句柄不自动回收**：远端句柄表常驻到进程退出；长跑程序请对不再用的大对象 `py释放`。
4. **`tmp/` 里的 `child.py`**：内嵌脚本按内容哈希落到 `%TEMP%/zuzo-pyext-<hash>/child.py`，升级 zuzo 后哈希变化会自动换新文件。
5. **并发**：单管道串行（互斥锁），不是多线程并发桥；需要并发就在 Python 侧用线程/协程。
6. **不做的**：不做双向对象共享、不做序列化兼容保证、不做跨机器。

---

## 十二、验收命令

```bash
# 1) 桥单测（无 CPython 自动 Skip）
CGO_ENABLED=1 CC="/c/Dev/zig/zig cc" go test ./pyext -count=1

# 2) 端到端对拍（15 用例，期望值由 CPython 现算）
./run_tests_bridge.sh
# → BRIDGE PASS=15 FAIL=0

# 3) 子进程服务端自检
python pyext/child.py --selftest
# → SELFTEST PASS 25

# 4) pip 通道
./zuzo.exe pip --version

# 5) 关掉桥，验回退语义
ZUZO_PY_BRIDGE=0 ./zuzo.exe tests_bridge/t02_auto_fallback.hy   # 应报 C 扩展错误
```
