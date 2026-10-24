# Zuzo Python 运行时（`zuzo xxx.py`）

> zuzo v1.1 起内置 Python 语法前端：**同一个可执行文件既能跑 `.hy`（中文语法），也能直接跑 `.py`（Python 语法）**。

---

## 1. 快速开始

```bash
# 直接执行 Python 文件（自动按扩展名分流）
zuzo hello.py

# 原有中文语法文件不受影响
zuzo hello.hy
```

`hello.py`：

```python
def greet(name, greeting="Hello"):
    return f"{greeting}, {name}!"

print(greet("zuzo"))
```

输出：

```
Hello, zuzo!
```

---

## 2. 架构

```
zuzo.exe
 ├── .hy  →  lexer/parser/ast/infer/evaluator（原有中文语法链路，零改动）
 └── .py  →  py/token → py/lexer → py/parser → py/ast
                          ↓
                    py/runtime（Python 语义求值器）
                          ├── py/object（Python 对象模型）
                          ├── builtins / methods / stdlib
                          └── import（内置模块 + 本地 .py）
```

**为什么是独立前端而非复用 zuzo AST**：zuzo 的 AST 面向中文语法的树遍历求值，没有成员访问（`a.b`）、切片（`a[1:2]`）、推导式、类定义等节点；Python 语义（LEGB 作用域、魔术方法、异常类继承、生成器）无法映射。故 Python 走独立前端 + 独立求值器，仅共享进程入口与输出通道。

---

## 3. 语法覆盖

| 类别 | 支持情况 |
|---|---|
| 字面量 | int（十进制/0x/0o/0b/下划线分隔）、float（含科学计数、inf/nan）、str（单/双/三引号、r/b/f 前缀、相邻拼接）、bytes、True/False/None、Ellipsis |
| 运算符 | `+ - * / // % ** @ << >> & \| ^ ~`、比较链、`and/or/not`、`in/not in`、`is/is not`、海象 `:=`、三元表达式 |
| 控制流 | `if/elif/else`、`while/else`、`for/else`、`break/continue/pass` |
| 模式匹配 | `match/case`（字面量、捕获、`_`、或模式 `\|`、序列 `[a, *rest]`、映射 `{"k": v}`、类模式 `Cls(x, y=)`、`as` 绑定、guard；内置类型单值自匹配） |
| 异步 | `async def` / `await`（**真并发**：Task 由 goroutine 调度，`gather` 并行启动；`asyncio.run/gather/sleep/create_task/wait_for`） |
| 函数 | 位置/关键字/默认值/`*args`/`**kwargs`/仅关键字参数/`/` 位置限定、lambda、闭包、递归、装饰器（含带参装饰器）、global/nonlocal |
| 类 | 继承、多继承 + C3 MRO、`super()`（无参/双参）、类属性/实例属性、`@property`（getter/setter/deleter）、`@staticmethod`/`@classmethod`、**自定义描述符 `__get__`/`__set__`**、**元类 `metaclass=`（`__new__` 链）**、`__slots__`（接受）、魔术方法（`__init__ __str__ __repr__ __eq__ __lt__ __add__ __call__ __getitem__ __setitem__ __len__ __iter__ __contains__ __enter__ __exit__ __matmul__` …）、自定义类装饰器 |
| 异常 | `try/except/else/finally`、`raise ... from`、异常类继承体系、`assert` |
| 迭代 | `for ... in`、生成器 `yield` / `yield from` / `send`、生成器表达式、迭代器协议 |
| 集合 | list / tuple / dict（保序）/ set、切片（含负步长）、解包（`*`、`**`）、列表/字典/集合推导式 |
| 字符串 | f-string（含格式说明符 `:{spec}`、`!r/!s/!a`）、`%` 格式化、`str.format()`、全量常用方法 |
| 上下文 | `with`（多子句） |
| 导入 | `import x` / `import x as y` / `from x import y, z` / `from x import *`（内置模块 + 同目录 `.py`） |

---

## 4. 内置函数

`print len range int float str repr bool list tuple dict set frozenset bytes abs round sum min max sorted reversed enumerate zip map filter any all isinstance issubclass type hasattr getattr setattr delattr callable iter next format chr ord hex bin oct divmod pow id hash open input dir globals locals vars eval exec super property staticmethod classmethod slice exit quit`

异常类：`BaseException Exception ArithmeticError ZeroDivisionError OverflowError LookupError IndexError KeyError AttributeError NameError TypeError ValueError UnicodeError RuntimeError RecursionError StopIteration AssertionError OSError FileNotFoundError PermissionError ImportError ModuleNotFoundError SyntaxError MemoryError SystemExit KeyboardInterrupt GeneratorExit` 等。

---

## 5. 标准库子集

| 模块 | 覆盖 |
|---|---|
| `math` | pi/e/tau/inf/nan、sqrt/exp/log/log2/log10/sin/cos/tan/asin/acos/atan/atan2/sinh/cosh/tanh、floor/ceil/trunc/fabs/pow/hypot、factorial/gcd、isnan/isinf/isfinite、radians/degrees |
| `random` | random/seed/randint/randrange/uniform/choice/shuffle/sample |
| `time` | time/time_ns/perf_counter/sleep/strftime/localtime |
| `json` | dumps（indent / ensure_ascii）、loads、dump |
| `re` | match/search/findall/sub/split/escape/compile（`I/M/S` 标志、分组、命名组） |
| `os` | getcwd/listdir/makedirs/mkdir/remove/rename/getenv/environ/sep/linesep、`os.path`（join/exists/isfile/isdir/basename/dirname/abspath/split/splitext/getsize） |
| `sys` | argv/version/platform/maxsize/path/exit/getrecursionlimit/setrecursionlimit |
| `string` | ascii_letters/ascii_lowercase/ascii_uppercase/digits/hexdigits/octdigits/punctuation/whitespace/printable |
| `collections` | Counter（most_common/elements/update）、defaultdict、deque、namedtuple、OrderedDict |
| `itertools` | chain/product/combinations/permutations/repeat/count/islice/groupby |
| `functools` | reduce/partial/wraps/lru_cache/cmp_to_key |
| `copy` | copy/deepcopy |
| `hashlib` | md5/sha1/sha256（hexdigest/digest/update） |
| `base64` | b64encode/b64decode |
| `datetime` | datetime（now/isoformat/strftime）、date.today、timedelta |
| `typing` / `dataclasses` | 类型占位、`@dataclass` 自动 `__init__`、`field` |
| `pathlib` | Path 构造、`/` 拼接、name/stem/suffix/parent/parts、exists/is_file/is_dir、read_text/write_text、mkdir/iterdir/glob/resolve |
| `logging` | basicConfig、getLogger、debug/info/warning/error/critical、setLevel |
| `argparse` | ArgumentParser、add_argument（位置/可选/type/default/action=store_true）、parse_args |
| `unittest` | TestCase + assertEqual/assertTrue/assertFalse/assertIn/assertIsNone/assertAlmostEqual |
| `asyncio` | run / sleep / gather / create_task / wait_for / iscoroutine |
| `shutil` | rmtree / copy / copyfile / move |
| `threading` | Thread（start/join/is_alive）、Lock（acquire/release/with）、current_thread |
| `socket` | AF_INET/SOCK_STREAM、connect/sendall/recv/bind/listen/accept/close/with |
| `urllib.request` / `urllib.parse` | urlopen/urlretrieve；quote/unquote/quote_plus/urlencode |
| `tempfile` | gettempdir/mkdtemp/mkstemp/NamedTemporaryFile |
| `glob` | glob / escape |
| `uuid` | uuid4 / uuid1 / UUID |

---

## 6. 已知边界

| 项 | 现状 |
|---|---|
| 整数精度 | **已支持任意精度**：int64 快路径 + 溢出/超范围自动回退 `math/big`（字面量、算术、比较、格式化、进制转换全链） |
| `async/await` | 协程 + Task 由 goroutine 实现，`gather` 为真并发；无标准事件循环的定时器/回调语义 |
| 标准库 | 仍为子集：`multiprocessing`/`sqlite3`/`tkinter`/`http.server`/`ssl`/`logging.handlers` 等未提供 |
| C 扩展 | 不支持 `.pyd` / `ctypes` |
| `sys.path` 修改 | 返回副本，运行期修改不生效（启动目录与主文件目录已入路径） |
| 生成器 | goroutine 实现，未耗尽时依赖 GC 回收 |

---

## 7. 验证

```bash
cd D:/逆向/20261024-12-47-05-623/zuzo
for f in tests_py/t*.py; do
  b=$(basename "$f" .py)
  python "$f" 2>&1 | tr -d '\r' > tests_py/_out/$b.cpython.txt
  ./zuzo.exe "$f" 2>&1 | tr -d '\r' > tests_py/_out/$b.zuzo.txt
  diff -q tests_py/_out/$b.cpython.txt tests_py/_out/$b.zuzo.txt && echo "MATCH $b"
done
```

当前结果：**21/21 MATCH**（对拍基准 CPython 3.12.13）。一键脚本：`./run_tests.sh`。

用例覆盖：基础语法、字符串、集合类型、控制流、函数、类与继承、异常、生成器、标准库、with 与装饰器、跨模块导入、算法、数据结构、`match` 模式匹配、`async` 真并发、大整数、路径/日志/命令行/单测、网络工具、元编程（描述符/元类/`__slots__`）。

补充：`zuzo --py` 进入 Python REPL；未捕获异常输出含函数名与行号（`tests_py/manual/` 为手工验证用例）。
