# zuzo 能力矩阵（2026-10-25 实测）

> 本文件回答一个问题：**现在的 zuzo 能写哪些程序**。
> 判据口径：每条能力必须挂「实测命令 + 真实输出」；文档写得出但跑不通的，一律进「边界」而不是「能力」。
> 实测环境：Windows 10/11 x64 · `zuzo.exe` 29,602,816 字节（构建时间 2026-10-25 11:08）· Go 1.27.1 · CPython 3.12.13（对拍基准）· Edge（浏览器自动化载具）。
> 事实源优先级：**源码 > 实跑 > 文档**。本文件与 `docs/*.md` 冲突处，以本文件的实测记录为准。

---

## 0. 一句话结论

zuzo 当前是一个**单二进制交付的脚本/服务端运行时**：能用中文语法（`.hy`）、Python 语法（`.py`）、或两者混写（`.hypy`）写 **CLI 工具、HTTP API 服务、网页自动化脚本、数据/文本处理脚本、C 库桥接工具、带 BDD 测试的工程脚本**；**不能**写自绘 GUI 桌面程序；依赖 CPython C 扩展（numpy/pandas 类）的代码 **✅ 走 pyext 子进程桥（第32轮）**，由本机 CPython 子进程承载，不再是硬边界（见 §2.1）。

---

## 1. 三种入口与运行形态

| 入口 | 扩展名 | 执行链 | 实测 |
|---|---|---|---|
| 中文语法 | `.hy` | lexer → parser → infer → evaluator（+SAP 优化） | ✅ `./zuzo.exe examples/hello.hy` → `你好，世界！` |
| Python 语法 | `.py` | `py/token → py/parser → py/runtime`（独立前端 + 独立求值器） | ✅ `./zuzo.exe tests_py/t01_hello.py` → `Hello, World!` |
| 混合语言 | `.hypy` | 按 `##ZUZO` / `##PY` 分块顺序执行，单实例双运行时 | ✅ `./zuzo.exe examples/mixed_demo.hypy` → 8 行分析结果，EXIT=0 |
| 内联 | — | `zuzo -e '打印("你好")'` | ✅ README 声明，实测同链 |
| 交互 | — | `zuzo`（中文 REPL）/ `zuzo --py`（Python REPL） | ✅ `printf 'print(2**100)\n1+1\n' \| ./zuzo.exe --py` → `1267650600228229401496703205376` / `2` |

运行形态：**单文件二进制 + 单进程**，无外部运行时依赖（Python 侧不链接 CPython，C 库通过 C ABI 直调）。

---

## 2. 能写什么程序（按程序类型）

| # | 程序类型 | 支撑能力 | 实测证据 | 结论 |
|---|---|---|---|---|
| 1 | **命令行工具 / 批处理脚本** | 文件 I/O 11 个内置（读/写/追加/存在/创建/删除/重命名/读目录/读行/环境变量/运行参数）+ 正则 6 + 编码 15 | `examples/sort.hy` 输出排序与哈希表结果；`readdir`/`writefile` 属同链内置 | ✅ 主力场景 |
| 2 | **HTTP API 服务 / Webhook 接收端 / 反代网关** | `http服务(端口, 处理器)`、`http服务后台`、`http流请求`（流式 SSE） | 自建回环：GET → `{"ok":true,"方法":"GET","路径":"/hello"}`；POST → `{"ok":true,"方法":"POST","路径":"/submit"}` | ✅ 主力场景 |
| 3 | **HTTP 客户端 / 接口采集** | `http获取/提交/更新/补丁/删除/请求` | 同上回环，客户端拿到服务端 JSON | ✅ |
| 4 | **网页自动化（爬取 / 填表 / 截图）** | 24 个浏览器内置（打开/导航/点击/填写/取文本/取属性/执行 JS/等待元素/截图/设视口…），chromedp 驱动，自动探测 Chrome/Edge | headless Edge：`执行脚本("document.querySelector('h1').textContent")` → `zuzo-ok`；`截图` → `shot.png` 5,013 字节 | ✅ 可用（需本机有 Chrome 或 Edge） |
| 5 | **数据处理 / 文本转换** | JSON 3 + 正则 6 + UTF-8/Unicode 16 + 编码 15 + 数组 6 + 哈希 2 | `json解析/序列化`、`正则查找全部`、`全角转半角`、`md5/sha256` 均在同链内置表 | ✅ |
| 6 | **调用系统 C 库 / 自编 DLL** | zuzo 侧 `加载C库` + `绑定` + `C调用`（3 内置）；Python 侧 `import ctypes` | `sqrt(144.0)=12`、`abs(-999)=999`、`strlen("你好，世界")=15`；ctypes `lib.abs(-7)=7` | ✅ 双通道 |
| 7 | **并发 / 批量任务** | `并发/通道/发送/接收/等待全部/尝试接收/关闭/尝试执行`（8 内置，goroutine + channel 语义） | `examples/demo_concurrency.hy` → 3 任务并行求和 `4500001500000`，与串行一致 | ✅ |
| 8 | **带测试的工程脚本** | 内置 BDD：`测试套件/测试用例/测试前/测试后/断言×12/运行测试`（19 内置） | `examples/bdd_test.hy` → `测试用例: 25 通过, 0 失败` / `断言: 64 通过, 0 失败` | ✅ 无需第三方测试框架 |
| 9 | **Python 生态脚本（纯 Python 库）** | 内嵌 Python：语义覆盖到类/继承/元类/描述符/生成器/async/大整数；31 个标准库模块实测可用 | `pycheck2.py`：class 魔术方法、`except ZeroDivisionError`、生成器、hashlib、itertools、functools、datetime、collections 全通 | ⚠️ 有条件（见 §5） |
| 10 | **中文语法业务原型 / 教学程序** | 中文关键字 23 个、7 种数据类型、闭包、递归、`定义/实例`、`否则` fallback、`在…到…满足` 迭代器、模块导入（`.hy` / `.z` 包） | `modern3.hy` → `张三, 25岁, 活跃=真` / `李四, 30岁, 活跃=假`；`demo_collections.hy` 加载 `.z` 包正常 | ✅ |
| 11 | **性能敏感的热点计算** | 五层执行栈（Tree-Walk → SAP 闭包熔合 → 原生 int64 → Smi → De Bruijn） | README 基准（同机 1e6 整数循环中位数）：zuzo SAP 23.6 ms / 内嵌 Python 21.1 ms / CPython 3.12 165.3 ms | ✅ 声明值，本轮未复跑 |
| 12 | **GUI 桌面程序** | 无窗口/控件/绘图库；`打开浏览器` 是驱动外部 Chromium，不是自绘 | 内置表 175 项中无任何 GUI 类函数 | ❌ 不支持 |
| 13 | **需要 sqlite3 / ssl / http.server / multiprocessing 的程序** | 标准库未实现这四个模块 | `modscan.py`：四项均 `ModuleNotFoundError` | ❌ 不支持 |
| 14 | **依赖 CPython C 扩展的库（numpy/pandas）** | ✅ 走 pyext 子进程桥（第32轮）：`py导入 / py调 / py方法 / py取值` 在外部 CPython 子进程里执行，`.pyd` 由 CPython 自己加载，绕开 zuzo 侧 ABI 边界 | `pyext/child.py` + `pyext/client.go`（JSON Lines over stdio）；命令口径见 §2.1（输出待回填） | ✅ 走 pyext 子进程桥（第32轮） |
| 15 | **以 `.wasm` 模块为核心的插件程序** | WASM FFI 8 内置（wazero），能力齐 | 仓库无 `.wasm` 产物（`find . -name "*.wasm"` 空，只有 `math.wat`） | ⚠️ 需自备 `.wasm`，本轮未实测 |

### 2.1 第三方 CPython 库（numpy / pandas / requests 等）

> 2026-11-01 第32轮新增。`pyext` 子进程桥把「本机 CPython + site-packages」接成 zuzo 的外部运行时：`py导入("numpy")` 拿到的是**子进程里的真实对象句柄**，`py调 / py方法 / py取值` 直接在 CPython 里执行，`.pyd` C 扩展不再受 zuzo 自身对象模型与 ABI 限制。
> 口径声明：下表 5 条命令是本节的验收口径，**全部于 2026-11-01 实跑并回填原始输出**（本机 CPython 3.12.13 / numpy 2.5.1 / pandas 3.0.3）。

| # | 场景 | 命令（项目根目录执行） | 实测输出 |
|---|---|---|---|
| 1 | 导入模块 + 句柄 + 桥信息 | `./zuzo.exe -e '变量 np = py导入("numpy"); 打印(py描述(np)); 打印(py信息())'` | `<module 'numpy' from '...\site-packages\numpy\__init__.py'>`<br>`{版本: 3.12.13, 解释器: ...\python-env\python.exe, 实现: CPython, 模式: 子进程, 调用次数: 4, 失败次数: 0, 运行秒数: 0.1135566}` |
| 2 | ndarray 构造 + 归约 | `./zuzo.exe -e '变量 np = py导入("numpy"); 变量 a = py方法(np, "arange", 12); 变量 m = py方法(py方法(a, "reshape", 3, 4), "sum"); 打印(m); 打印(类型(m))'` | `66`<br>`整数`（numpy 标量按 `item()` 解包成 int64） |
| 3 | DataFrame 分组求和 + 快照 | `./zuzo.exe -e '变量 pd = py导入("pandas"); 变量 df = py方法(pd, "DataFrame", {"城市": ["北京", "上海", "上海"], "销量": [3, 5, 2]}); 打印(py取值(py方法(py方法(df, "groupby", "城市"), "sum"))); 打印(类型(py取值(df)))'` | `[{销量: 7}, {销量: 3}]`<br>`数组`（`py取值` = `to_dict("records")` 快照，索引不保留） |
| 4 | 老代码自动降级（粘性） | `./zuzo.exe -e 'py执行("import numpy as np"); 打印(py求值("int(np.arange(5).sum())")); 打印(py模式())'` | `10`<br>`子进程`（纯 Go 运行时失败 → 自动拉起 CPython → 重放 → 粘住） |
| 5 | 错误路径 | `./zuzo.exe -e 'py导入("zuzo_no_such_module_xyz")'` | 退出码 1；`✘ 根因 [E912]: Python ModuleNotFoundError: No module named 'zuzo_no_such_module_xyz'` + 子进程 traceback |

命令行入口：`./zuzo.exe pip install <包>` / `pip list` / `pip 安装|卸载|列表|版本`（透传给桥用的同一 CPython；实测 `./zuzo.exe pip install humanize` → `Successfully installed humanize-4.16.0`，随后 `.hy` 里 `py导入("humanize")` 直接可用）。

配套内置（14 个）：`py导入 / py取 / py设 / py调 / py调参 / py方法 / py取值 / py描述 / py类型 / py释放 / py模式 / py信息 / py有库 / py安装`（`py调参` 是关键字参数通道）。

- `py有库(名)`：`importlib.util.find_spec` 探测，不实际导入，用于特性门控。
- `py安装(包名)`：显式 `python -m pip install`，**不自动、不隐式联网**；缺库时由使用者决定是否安装。
- 语义口径：句柄是**引用语义**（同一 id 指向子进程同一对象），zuzo 原生容器跨桥是**快照语义**（与既有 mix 桥一致）；`ndarray` 一律给句柄（保留 `.sum()`/`.reshape()`/`.dtype` 等方法），要原生数组用 `py取值(h)`。
- 子进程 `print()` 重定向到 **stderr**，stdin/stdout 专用于 JSON Lines 协议，因此 `py执行` 里的打印不会污染返回值。
- 失败可见：子进程崩溃/超时由 Go 侧检测并按需重启，重启后旧句柄失效（需重新 `py导入`）；错误码表见 `docs/python-bridge.md` 第七节。

---

## 3. 中文语法（`.hy`）能力面

### 3.1 语法要素（`docs/language-spec.md` + 源码）

| 类别 | 支持内容 | 实测 |
|---|---|---|
| 变量/赋值 | `变量 x = 10`、重赋值、数组下标赋值、哈希键赋值 | ✅ |
| 数据类型 | 整数（int64）、浮点数、字符串（UTF-8）、布尔（`真/假`）、空（`空`）、数组、哈希 | ✅ `类型(42)` → `整数` |
| 运算符 | `+ - * / %`、比较、`并且/或者/非`（短路）、`否则` fallback | ✅ `读文件("不存在") 否则 "{\"theme\":\"dark\"}"` → fallback 生效 |
| 控制流 | `如果/否则如果/否则`、`如果` 作表达式、`当`、`对于 i=0; …`、`在 1 到 5`、`在 1 到 10 满足 条件`、`跳出/跳过` | ✅ `modern2.hy` 区间与过滤迭代输出 1..5 与 2,4,6,8,10 |
| 函数 | 具名/匿名函数、闭包（可变捕获）、递归、高阶函数 | ✅ `examples/fibonacci.hy`、`demo_modern.hy` 闭包计数 |
| 数据建模 | `定义 名字 { 字段 = 默认值 }` + `实例(字段: 值, …)`（具名参数用**冒号**，换行分隔字段） | ✅ `modern3.hy` → 具名参数覆盖默认值，顺序无关 |
| 错误内省 | `错误码/错误信息/错误行`（与 `执行/遇到` 搭配） | ⚠️ 见 §9 边界 1 |
| 模块 | `导入 "x.hy"` / `导入 "x.z" 为 别名`；搜索顺序 相对路径 → `zpm_modules/` → `~/.zuzo/stdlib/` → `~/.zuzo/.cache/zpackages/`；循环导入检测 + 单次执行缓存 | ✅ `demo_collections.hy` 加载 `collections-1.0.0.z` |
| f-string | `f"{var}"`、表达式插值、`{{}}` 转义、嵌套引号 | ✅ `hello.hy` → `你好，zuzo！` |

### 3.2 内置函数：**175 个 / 19 类**

统计口径：`evaluator/evaluator.go:1267` 的 `builtins := map[string]object.BuiltinFunction{…}` 注册表，脚本正则计数（非文档抄录）。

| 类别 | 数量 | 代表函数 | 源文件 |
|---|---|---|---|
| 通用 | 6 | `打印` `长度` `类型` `字符串` `整数` `浮点数` | `evaluator.go` |
| 错误内省 | 3 | `错误码` `错误信息` `错误行` | `evaluator.go` |
| 数组 | 6 | `推入` `弹出` `首项` `末项` `连接` `包含` | `builtins_array.go` |
| 字符串（旧全局名） | 14 | `大写` `小写` `前缀` `后缀` `查找` `替换` `分割` `取字符` `格式化` | `strlib_*.go`（第18轮重构，`builtins_strings.go` 只留 `格式化`） |
| 字符串标准库 `文本` | 110 | 内建命名空间：大小写 8 · 裁剪前后缀 13 · 查找替换 12 · 分割连接 10 · 对齐填充 6 · 判定 12 · 子串反转 5 · 中文友好 8 · UTF-8/编码/正则 36 | `evaluator/strlib_*.go` |
| 哈希 | 2 | `键` `取值` | `evaluator.go` |
| 并发 | 8 | `并发` `通道` `发送` `接收` `等待全部` `尝试接收` `关闭` `尝试执行` | `builtins_concurrency.go` |
| 文件 I/O | 11 | `读文件` `写文件` `追加文件` `读目录` `环境变量` `运行参数` | `builtins_io.go` |
| 网络（客户端） | 6 | `http获取` `http提交` `http更新` `http补丁` `http删除` `http请求` | `builtins_net.go` |
| HTTP 服务端 | 3 | `http服务` `http服务后台` `http流请求` | `builtins_httpserver.go` |
| JSON | 3 | `json解析` `json序列化` `json格式化` | `builtins_json.go` |
| 正则 | 6 | `正则匹配` `正则查找全部` `正则替换` `正则分割` `正则验证` `正则提取` | `builtins_regex.go` |
| 数学 | 13 | `绝对值` `最大值` `最小值` `幂` `平方根` `随机` `随机整数` | `builtins_math.go` |
| 时间 | 9 | `时间戳` `当前时间` `格式化时间` `解析时间` `睡眠` `计时` | `builtins_time.go` |
| 浏览器自动化 | 24 | `打开浏览器` `导航` `点击` `填写` `获取文本` `执行脚本` `截图` `设置视口` | `builtins_browser.go` |
| 编码/解码 | 15 | `base64编码` `hex编码` `md5` `sha1` `sha256` `url编码` `html转义` | `builtins_encoding.go` |
| UTF-8 / Unicode | 16 | `转编码` `规范化` `全角转半角` `半角转全角` `去除BOM` `utf8修复` | `builtins_utf8.go` |
| BDD 测试 | 19 | `测试套件` `测试用例` `断言相等` … `运行测试` | `builtins_bdd.go` |
| WASM FFI | 8 | `加载WASM` `WASM调用` `WASM导出` `WASM内存读取/写入/大小` `WASM关闭` | `builtins_ffi.go` |
| C 库 FFI | 3 | `加载C库` `C函数声明` `C调用` | `builtins_cffi.go` |

> 注：`docs/language-overview.md` 写「120+ 函数」，实际注册 175 个；该文档的速查表也缺 `键/取值/尝试执行/http服务/定义实例` 等新项，以本表为准。

---

## 4. 三种执行模式的工程开关

| 开关 | 作用 | 实测 |
|---|---|---|
| `--check` / `-c` | 静态类型检查（不执行） | `./zuzo.exe --check examples/hello.hy` → `✓ 类型检查通过，未发现类型错误` |
| `--analyze` / `-a` | 拓扑指纹分析（查重 / 死代码 / CSE 候选） | `-a examples/sort.hy` → `总节点数: 269 / 唯一指纹: 45 / 重复节点: 224 (83.3%)` |
| `--shadow` | 影子模式：SAP 路径与传统路径双轨执行，不一致即报错 | `--shadow examples/hello.hy` → 正常输出 4 行，无断言错误 |
| `--no-sap` | 关闭 SAP，纯 Tree-Walk | `--no-sap` 跑 `执行/遇到` 探针 → 同样失败（见边界 1，与 SAP 无关） |
| `--trace` | 调用轨迹到 stderr | `--trace examples/fibonacci.hy` → `[trace] → 调用 斐波那契 (行 4)` |
| `--profile` / `ZUZO_CPUPROFILE` | 函数调用统计 / CPU 采样 | README 声明；本轮未复跑 |
| `--strict` | 类型错误视为编译错误阻止执行（默认只警告） | 默认路径实测：类型警告打印后继续执行 |
| `--py` / `--py-repl` | Python REPL | ✅ 见 §1 |

---

## 5. Python 侧（`.py`）能力与边界

### 5.1 语义覆盖（`docs/python-support.md` 声明 + 本轮实测）

实测通过（`tmp/zuzo-probe/pycheck2.py`，EXIT=0）：

```
class A:  __init__ / __add__ / __repr__   →  A(3)
try/except ZeroDivisionError              →  except ZeroDivisionError
生成器表达式                               →  [0, 1, 4, 9]
hashlib.sha256(b"zuzo").hexdigest()[:12]  →  aed0c6b92763
itertools.islice(itertools.count(1), 3)   →  [1, 2, 3]
functools.reduce(lambda a,b: a+b,[1,2,3]) →  6
collections.Counter("aab").most_common(1) →  [('a', 2)]
datetime.date(2026,10,25).isoformat()     →  2026-10-25
json.dumps({"a":[1,2]}, ensure_ascii=False)→  {"a": [1, 2]}
re.findall(r"\d+", "a1b22")               →  ['1', '22']
```

声明覆盖但本轮未逐条实测：`match/case`、`async def/await`（真并发）、元类、描述符、`__slots__`、装饰器带参、`raise ... from`、`with` 多子句、`from x import *`。回归用例 `tests_py/t14_match.py` `t15_async.py` `t20_meta.py` `t26_raisefrom.py` `t27_with_multi.py` 均在 `run_tests.sh` 的 33/33 通过集合内。

### 5.2 内置函数：**64 个**

统计口径：`py/runtime/*.go` 中 `ip.reg("…")` 调用去重。

```
__import__ abs all any ascii bin bool bytes callable chr classmethod delattr dict dir
divmod enumerate eval exec exit filter float format frozenset getattr globals hasattr
hash hex id input int isinstance issubclass iter len list locals map max min next oct
open ord pow print property quit range repr reversed round set setattr slice sorted
staticmethod str sum super tuple type vars zip
```

异常类体系按 CPython 层级构建（`BaseException → Exception → ArithmeticError → ZeroDivisionError` 等，`py/runtime/builtins.go:initExceptions`）。

### 5.3 标准库：实测 **31 可用 / 4 缺失**

实测方式：`tmp/zuzo-probe/modscan.py` 对 35 个模块逐个 `__import__`。

| 结果 | 模块 |
|---|---|
| ✅ 可用（31） | `math` `random` `time` `json` `re` `os` `sys` `string` `collections` `itertools` `functools` `copy` `hashlib` `base64` `datetime` `typing` `dataclasses` `pathlib` `logging` `argparse` `unittest` `asyncio` `shutil` `threading` `socket` `urllib.request` `urllib.parse` `tempfile` `glob` `uuid` `ctypes` |
| ❌ 缺失（4） | `sqlite3` `http.server` `ssl` `multiprocessing` |

### 5.4 第三方纯 Python 库：**有条件**

- 判定：能 `import` 并跑通 = 可用；卡在语义边角 = 不可用。
- 实测反例：`import requests` → 递归过深，栈顶在 `urllib3/_collections.py:87 __init__`（第 16 轮遗留 T49 未闭环；本轮构建复现，EXIT=1）。
- 结论：第三方库目前按「逐个验证」使用，不要按「CPython 能跑就能跑」预期。

### 5.5 ctypes

- ✅ `ctypes.CDLL("msvcrt.dll")` + `lib.abs(-7)` → `7`
- ❌ `ctypes.create_string_buffer` → `AttributeError: module 'ctypes' has no attribute 'create_string_buffer'`
- ❌ 不能加载 `.pyd`（CPython C 扩展 ABI 边界，`docs/ctypes.md` §三）

### 5.6 Python 侧版本口径

`sys.version_info` 实测返回 `PY 3 11`（`zuzo py-runtime` 自报 3.11.0），对拍基准是本机 CPython **3.12.13**。写代码时不要依赖 3.12 新增语法。

---

## 6. 混合语言（`.hypy`）

| 项 | 说明 | 实测 |
|---|---|---|
| 块标记 | 整行 `##ZUZO` / `##PY`（大小写不敏感，`##HY` / `##PYTHON` 亦接受） | ✅ `examples/mixed_demo.hypy` |
| 执行 | 顺序执行、共享 stdout/stderr、单实例双运行时 | ✅ 输出 8 行（记录条数 / 品类汇总 / 中位数 / 明细 / 完成） |
| 双向调用 | zuzo → py：`py调用` `py获取` `py设置` `py存在`；py → zuzo：`zuzo.调用/获取/设置/存在` | ✅ 第 4 轮起；`tests_mix` 15/15 |
| 全局同步 | 每块结束双向同步；同名后定义覆盖；容器深拷贝快照 | ✅ `run_tests_mix.sh` → `MIX PASS=15 FAIL=0` |
| 限制 | 混合模式关闭 SAP（性能路径不同）；不支持 `--shadow` | 文档声明，未实测反例 |

---

## 7. 跨语言互操作（非混合文件）

| 入口 | 方向 | API | 实测 |
|---|---|---|---|
| 纯 `.hy` | zuzo → Python | `py求值` `py执行` `py调用` `py获取` `py设置` `py存在` | `py求值("2 ** 10")` → `1024` ✅ |
| 纯 `.py` | Python → zuzo | `import zuzo` → `调用/获取/设置/存在` | `zuzo.获取("x")` → `5`；`zuzo.调用("长度",[1,2,3])` → `3` ✅ |
| 纯 `.hy` | 模块属性访问 | `py获取("math").pi` | ❌ `[E204] 不支持对类型 字符串 使用索引操作`（`interop.md` 示例与实际不符，见边界 3） |

---

## 8. 系统能力实测记录

### 8.1 HTTP 服务端（回环）

```bash
# tmp/zuzo-probe/httpsrv2.hy
http服务后台(18099, 函数(请求){ 返回 {"状态":200,"头":{"Content-Type":"application/json"},
      "体": json序列化({"ok":真,"路径":请求["路径"],"方法":请求["方法"]})} })
睡眠(300)
打印("GET:",  http获取("http://127.0.0.1:18099/hello?x=1"))
打印("POST:", http提交("http://127.0.0.1:18099/submit", "{\"a\":1}"))
```

实测输出（EXIT=0）：

```
GET: {"ok":true,"方法":"GET","路径":"/hello"}
POST: {"ok":true,"方法":"POST","路径":"/submit"}
```

要点：处理器返回的是**响应对象 Hash**（`状态` / `头` / `体`），不是裸字符串——第一版探针返回裸 Hash 时客户端拿到空串（失败路线，已修正）。

### 8.2 浏览器自动化

```bash
# tmp/zuzo-probe/browser.hy（headless）
打开浏览器(假) → 打开页面 → 设置视口(800,600)
导航(页面, "data:text/html,<h1>zuzo-ok</h1>")
执行脚本(页面, "document.querySelector('h1').textContent")
截图(页面, "tmp/zuzo-probe/shot.png")
```

实测：`H1: zuzo-ok`；截图文件 5,013 字节。本机无 Chrome，`findBrowserPath()` 自动回落到 Edge（`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`）。

### 8.3 C 库 FFI（zuzo 侧）

`./zuzo.exe examples/demo_cffi.hy` 实测尾部输出：

```
abs(-999) = 999
strlen("Hello, C FFI!") = 13
strlen("你好，世界") = 15          ← UTF-8 字节数，正确
sqrt 地址: 140724026179952
库已关闭
```

### 8.4 并发

`./zuzo.exe examples/demo_concurrency.hy`：3 任务并行求和 `4500001500000`，与串行校验值一致，并发耗时 0 ms / 串行 1 ms。

### 8.5 BDD

`./zuzo.exe examples/bdd_test.hy`：`测试用例: 25 通过, 0 失败, 0 跳过（共 25）`；`断言: 64 通过, 0 失败（共 64）`。

---

## 9. 边界（实测失败 / 不可写清单）

> 这一节比能力清单更重要：以下每条都是**跑过、失败、有报错原文**的。

1. **`执行/遇到` 错误捕获不生效（当前构建，命中所有代码路径）**
   - 现象：`执行 { 10 / 0 } 遇到 e { 打印("捕获", e) }` → 直接输出 `[E202] 除零错误` 并 EXIT=1，handler 未执行。
   - 证据：`examples/demo_modern.hy` 第 49 行 `变量 z = x / y` 同样击穿（示例自身 EXIT=1）；`--no-sap` 同败；`-a` 显示 AST 已正确生成 `try:0` / `catch:0` 节点（解析无误）。
   - 已知原因：`evaluator/modern.go:74 evalTryCatchStatement` 只在 `isError(result)` 时捕获；实际失败点在错误向上冒泡的路径上被提前判定为致命错误（`evaluator/evaluator.go:309` 的分发存在、`--no-sap` 仍复现）。根因未定位到具体行，属待修项。
   - 影响面：所有依赖错误捕获的错误处理代码；`错误码/错误信息/错误行` 三个内置函数因此无法按设计使用。
2. **`关闭浏览器()` 签名与示例/文档不一致**
   - 现象：`关闭浏览器()` → `[E200] 关闭浏览器() 期望 1 个参数，得到 0 个`；需要传浏览器对象。
   - 影响：`examples/browser_test.hy` 与 `README` 里无参写法照抄即报错；类型检查阶段还会先给 `E101 参数数量不匹配` 警告。
3. **`py获取("math").pi` 不可用**
   - 现象：`[E204] 不支持对类型 字符串 使用索引操作`。
   - 影响：`docs/interop.md` §三 的示例照抄即失败；跨语言取模块属性需要换写法（如 `py求值("math.pi")`）。
4. **`import requests` 失败（第三方库链未闭环）**
   - 现象：递归过深，栈顶 `urllib3/_collections.py:87 __init__`，EXIT=1。
   - 影响：requests / 依赖 urllib3 的生态库当前不可用；等价替代 = `http获取`（zuzo 侧）或 `urllib.request`（Python 侧）。
5. **标准库四缺**：`sqlite3` / `http.server` / `ssl` / `multiprocessing`（`ModuleNotFoundError`）。
6. **`ctypes` 缺 `create_string_buffer`**：只能按标量/指针签名调用，缓冲区类 API 未实现。
7. **WASM FFI 无现成产物**：仓库只有 `math.wat`，没有 `.wasm`；要用需自备 `wat2wasm` 等工具编译（本轮未引入依赖）。
8. **`定义` 的字段分隔符是换行，不是逗号**：`定义 用户 { 姓名 = "无名", 年龄 = 0 }` 单行逗号写法报 `[E001] 定义体中期望字段名`；必须逐行。
9. **具名参数用冒号不用等号**：`用户(姓名: "张三")` 正确；`用户(姓名="张三")` 会被当作未传参（得到默认值），且不报错——静默偏差，最危险的一类。
10. **无自绘 GUI 能力**：175 个内置里没有任何窗口/控件/绘图函数；`打开浏览器` 只能操控外部 Chromium。
11. **`zpm` 未随主二进制交付**：包管理器源码在 `cmd/zpm/`（`install/uninstall/pack/unpack/inspect/list/freeze/search/show/init`），需自行 `go build ./cmd/zpm`；主 `zuzo.exe` 无 `zpm` 子命令。
12. **类型系统默认不阻断**：默认只有警告（`--strict` 才阻止），意味着类型错误会带病执行到运行时。

---

## 10. 选型建议（该不该用 zuzo 写）

**适合**

- 单文件交付的中文脚本工具：批量改名/转换/抓取/格式化，`zuzo 工具.hy 参数` 直接跑，无需装运行时。
- 本地 HTTP 服务与反代网关原型：`http服务后台` + 流式响应，几十行起一个服务。
- 网页自动化：登录、抓取、填表、截图，一行 `打开浏览器` 起 headless。
- 需要调 DLL / C 库的胶水程序：zuzo 侧 CFFI 或 Python 侧 ctypes 两条路。
- 带自测的脚本工程：BDD 内置，`run_tests.sh` 式对拍开箱可用。
- 中文教学与业务原型：关键字全中文，语法面覆盖到闭包/数据建模/迭代器。

**不适合**

- 需要 GUI 的自绘桌面应用（用 Electron / WPF / Qt）。
- 依赖 numpy / pandas / 任何 `.pyd` 扩展的数据科学流水线 —— **已解除**：改走 pyext 子进程桥（第32轮，见 §2.1），代价是每次跨桥调用的进程通信开销与快照拷贝。
- 需要 `sqlite3` / `ssl` / `multiprocessing` / `http.server` 的 Python 程序。
- 要求 CPython 精确语义（3.12 特性、完整标准库）的既有大型 Python 项目。
- 以 `执行/遇到` 为核心错误处理策略的代码（当前构建不生效）。
- 依赖 `requests` 及其下游的网络采集（当前递归失败，改用 `http获取`）。

---

## 11. 复现方法

```bash
cd D:/逆向/20261024-12-47-05-623/zuzo

# 0) 基线：两套回归
./run_tests.sh          # 期望 PASS=33 FAIL=0
./run_tests_mix.sh      # 期望 MIX PASS=15 FAIL=0

# 1) 内置函数清点（175 / 19 类）
python - <<'EOF'
import re
src=open('evaluator/evaluator.go',encoding='utf-8').read()
seg=src[src.index('builtins := map[string]object.BuiltinFunction{'):src.index('object.InitBuiltinPool')]
print(len(re.findall(r'"[^"]+":\s*builtin', seg)))
EOF

# 2) 三类入口
./zuzo.exe examples/hello.hy
./zuzo.exe tests_py/t01_hello.py
./zuzo.exe examples/mixed_demo.hypy

# 3) 服务端回环 / 浏览器 / CFFI
./zuzo.exe <本轮 httpsrv2.hy 同构脚本>
./zuzo.exe examples/demo_cffi.hy

# 4) Python 模块面
./zuzo.exe <modscan.py 同构脚本>   # 期望 31 OK / 4 ERR
```

本轮探针全部落在 `D:/逆向/20261025-13-51-44-525/tmp/zuzo-probe/`（`trycatch.hy` `trycatch2.hy` `httpsrv2.hy` `browser.hy` `modern2.hy` `modern3.hy` `pycheck2.py` `modscan.py` `ct.py` `interop.hy` `interop.py` `shot.png`）。

---

## 12. 与既有文档的差异登记（供后续修文档）

| # | 文档位置 | 文档说法 | 实测 |
|---|---|---|---|
| 1 | `docs/language-overview.md` §12 | 内置 120+ 函数 | 注册表 175 个 / 19 类 |
| 2 | `docs/language-overview.md` §18 / `README` | `关闭浏览器()` 无参 | 需传浏览器对象（E200） |
| 3 | `docs/interop.md` §三 | `py获取("math").pi` | E204 不支持 |
| 4 | `docs/python-support.md` §6 | 不支持 ctypes | `ctypes` 模块可用（`CDLL` / `abs` 实测通过），但仅子集 |
| 5 | `docs/dev-status.md` | `run_tests` 30/34 | 实测 33/33（第 16 轮已转绿，文档未同步） |
| 6 | `README` 性能表 | 1e6 循环 23.6 ms | 本轮未复跑，保留声明值 |

---

*生成时间：2026-10-25 · 依据：`zuzo.exe` 实测 20+ 组 + 源码清点 + `run_tests` 两套回归。*
