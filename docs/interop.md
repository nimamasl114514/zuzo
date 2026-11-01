# 跨语言互操作（zuzo ↔ Python）

> 2026-10-25 新增。目标：**任何入口都能双向调用**，不限于 `.hypy` 混合文件。

## 一、三个入口的能力

| 入口 | zuzo 调 Python | Python 调 zuzo |
|---|---|---|
| `.hypy` 混合文件 | ✅ `py调用` 等（第 4 轮起） | ✅ `zuzo.调用` 等（第 4 轮起） |
| 纯 `.hy` | ✅ `py求值` / `py执行` / `py调用` / `py获取` / `py设置` / `py存在` | —（同进程无 py 主程序） |
| 纯 `.py` | —（同进程无 zuzo 主程序） | ✅ `import zuzo` → `调用` / `获取` / `设置` / `存在` |

实现：`interop.go`（main 包）把 `mix.Bridge` 的双向通道从「混合文件专用」提升为「入口即装」；
两侧共享同一个 zuzo `Environment`，变量互见。

## 二、对象转换规则（mix/convert.go）

| zuzo | Python |
|---|---|
| 整数 / 浮点 / 字符串 / 布尔 / 空 | int / float / str / bool / None |
| 数组 | list |
| 字典 | dict |
| 函数 / 内置 | 可调用代理（调用时跨回原侧执行） |

容器为深拷贝；跨语言共享可变对象需显式 `py设置` / `zuzo.设置` 写回。

## 三、示例

```zuzo
// 纯 .hy
变量 r = py求值("2 ** 10")        // 1024
打印(py求值("'-'.join(['a','b'])")) // a-b
py执行("import math")
变量 pi = py获取("math").pi
```

```python
# 纯 .py
import zuzo
zuzo.设置("x", 5)
print(zuzo.获取("x"))            # 5
print(zuzo.调用("长度", [1,2,3])) # 3
```

## 四、边界

- 同一进程只有一侧是「主程序」；另一侧的运行时按需惰性创建。
- 异常不跨语言传播（Python 异常转成 zuzo Error 对象，反之转 RuntimeError）。
- 不做引用共享：容器跨语言传递是快照语义。
- 以上四条只描述**内嵌 Python 运行时**（纯 Go 实现）。要跑 CPython 的 C 扩展库，走第五节的外部子进程桥。

## 五、CPython 子进程桥（第32轮）

> 2026-11-01 新增。解决的问题：内嵌 Python 是纯 Go 实现的对象模型 + ABI，**加载不了 `.pyd`**，所以 `import numpy` / `import pandas` / `import requests` 一类依赖 C 扩展的库在内嵌运行时里必然失败；而 `ctypes` 只能 dlopen 普通 C ABI 动态库，补不上这个缺口。
>
> 做法：起一个**本机 CPython 子进程**当服务端（`pyext/child.py`），zuzo 侧用 Go 客户端（`pyext/client.go`）通过 **JSON Lines over stdio** 通信。于是 `.hy` 里拿到的是「真 CPython 里的对象句柄」，库怎么装、`.pyd` 怎么加载全交给 CPython 自己。

> 写起来最省事的形态是**点号直调**（第32轮）：`np.array([1,2,3]).sum()`、`df["列"].mean()`、`df["新列"] = [1,2,3]`、关键字参数 `json.dumps(x, ensure_ascii: 假)`。下面 5.1 的内置是点号不可用时的等价通道（动态方法名、句柄存在容器里等）。

### 5.1 新增内置（14 个）

| 内置 | 作用 | 典型写法 |
|---|---|---|
| `py导入(名)` | 导入模块，返回模块句柄 | `变量 np = py导入("numpy")` |
| `py取(对象, 名)` | 取属性（不执行） | `py取(np, "pi")` |
| `py设(对象, 名, 值)` | 设属性 | `py设(配置, "超时", 30)` |
| `py调(可调用, 参数…)` | 调函数/类/可调用对象 | `py方法(np, "arange", 12)` |
| `py调参(可调用, 参数数组, 命名参数字典)` | 调函数并传关键字参数 | `py调参(py取(json,"dumps"), [1], {"ensure_ascii": 假})` |
| `py方法(对象, "方法名", 参数…)` | 调对象的方法（自带 self） | `py方法(a, "reshape", 3, 4)` |
| `py取值(对象[, 键])` | 尽力转成 zuzo 原生值（快照）；两参形式 `obj[键]` | `py取值(df)` → 数组/哈希 · `py取值(df, "a")` 取列 |
| `py描述(对象)` | 取 repr（前 200 字符） | `py描述(np)` |
| `py类型(对象)` | 取远端类型名 | `py类型(a)` → `ndarray` |
| `py释放(句柄)` | 显式释放子进程侧引用 | `py释放(df)` |
| `py模式()` | 当前 Python 执行模式（内嵌 / 子进程桥） | `打印(py模式())` |
| `py信息()` | 子进程解释器 + 桥运行态 | `{版本: 3.12.13, 实现: CPython, 模式: 子进程, 调用次数: …}` |
| `py有库(名)` | 探测是否装了这个库（不导入） | `如果 py有库("pandas") { … }` |
| `py安装(包名)` | 显式 `python -m pip install` | `py安装("pandas")`（**不自动联网**） |

### 5.2 自动降级（老代码不用改）

`py求值 / py执行` 仍先交给内嵌运行时。一旦内部命中「C 扩展导入失败」（`ImportError` / `ModuleNotFoundError` 指向 `.pyd` 扩展），桥会**自动激活子进程并重放同一段代码**，此后该进程进入**粘性子进程模式**：后续 `py求值 / py执行 / py获取 / py设置 / py存在` 全部改走桥，语义不变、代码零改动。

```zuzo
// 第31轮及以前：这里会以 AttributeError/ImportError 失败；第32轮直接穿过
py执行("import numpy as np")
打印(py求值("int(np.arange(5).sum())"))   // 10
打印(py模式())                              // 桥模式
```

### 5.3 值转换：句柄是引用，容器是快照

| 方向 | 规则 |
|---|---|
| Python → zuzo（`py取值` / 自动回传） | `None/bool/int/float/str` 原样；`list/tuple/set/range` → 数组；`dict` → 哈希；`numpy` 标量 → 数值；`ndarray` → **一律句柄**（保留全部方法），要原生数组显式 `py取值(h)`；`DataFrame` → records；其余对象返回**句柄** |
| zuzo → Python | 整数 → int、浮点 → float、字符串 → str、布尔 → bool、空 → None、数组 → list、哈希 → dict；`.hy` 函数/内置**不允许过桥**（单向桥，报 `E914`） |
| 大对象 / 有状态对象 | 不自动展开，保留句柄，用 `py方法` 逐步操作（避免整表拷贝） |

句柄有**引用语义**：同一个句柄多次 `py方法` 作用在同一对象上；但 zuzo 侧拿到的容器永远是**当时的快照**，改它不会回写 Python（要回写就 `py设` 或再调一次方法）。

### 5.4 边界与开销

- **必须本机有 CPython**（`python` 在 PATH，或 `ZUZO_PYTHON` 指定解释器）；没有 Python 的机器上只能跑内嵌运行时。
- **跨桥不是免费的**：每次调用是一次 stdio 往返 + JSON 编解码，量级在微秒到毫秒；热点循环要批量下推给 Python 做（一次 `py方法` 里算完），不要在 `.hy` 里逐元素过桥。
- **子进程 `print()` 走 stderr**：stdout 专用于协议，因此 Python 侧的打印会出现在错误流里，不污染返回值。
- **异常是文本不是对象**：子进程回传 `{t, m, tb}`，zuzo 侧转成错误信息（含 traceback 摘要），不能 `执行/遇到` 拿到原始异常对象。
- **句柄会失效**：子进程崩溃/超时由 Go 侧按需重启，重启后旧句柄全部作废，需重新 `py导入`；进程退出时统一 `shutdown`。
- **不加第三方 Go 依赖**：客户端只用 `os/exec` + `encoding/json`。

> 协议细节、ops 表、值编码表、`--selftest` 用法见 [Python 子进程桥](python-bridge.md)。
