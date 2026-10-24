# Python 侧原生 FFI（ctypes）

> 2026-10-25 新增。立场：**不内嵌 CPython**，Python 代码直接原生调用任意 C 动态库。

## 一、能做什么

```python
import ctypes

lib = ctypes.CDLL("msvcrt.dll")          # 或 WinDLL / LoadLibrary
lib.sqrt.argtypes = [ctypes.c_double]
lib.sqrt.restype  = ctypes.c_double
print(lib.sqrt(144.0))                   # 12.0

lib.abs.argtypes = [ctypes.c_int]
lib.abs.restype  = ctypes.c_int
print(lib.abs(-42))                       # 42

lib.strlen.argtypes = [ctypes.c_char_p]
lib.strlen.restype  = ctypes.c_int
print(lib.strlen(b"hello"))               # 5
```

自编 C 扩展（与 CPython ctypes 同一用法）：

```bash
zig cc -shared -O2 -o myext.dll myext.c   # 或 gcc/clang
```

```python
lib = ctypes.CDLL(r"D:/path/to/myext.dll")
lib.zz_add.argtypes = [ctypes.c_int, ctypes.c_int]
lib.zz_add.restype  = ctypes.c_int
print(lib.zz_add(2, 3))                   # 5
```

## 二、支持的类型

| ctypes 名 | C 类型 | 说明 |
|---|---|---|
| `c_int` / `c_uint` | int / unsigned int | 32 位 |
| `c_long` / `c_ulong` | long / unsigned long | 平台相关 |
| `c_longlong` | long long | 64 位 |
| `c_float` / `c_double` | float / double | 浮点 |
| `c_char` / `c_ubyte` | char / unsigned char | 8 位 |
| `c_short` / `c_ushort` | short / unsigned short | 16 位 |
| `c_bool` | bool | |
| `c_void_p` | void* | 原始指针（整数表示） |
| `c_char_p` | const char* | 接受 `str` 与 `bytes` |
| `c_void` | void | 仅返回类型 |

`ctypes.sizeof(t)` 返回字节宽度。

## 三、能力边界（重要）

- **能**：加载任意 C ABI 动态库（`.dll` / `.so` / `.dylib`），按签名调用，类型自动转换。
- **不能**：加载 **CPython C 扩展**（`.pyd`，如 numpy/pandas 的原生模块）。这类模块在加载时链接
  CPython 的 C API 符号（`PyModule_Create`、`PyObject_*`），没有 CPython 运行时就不存在这些符号——
  这是 ABI 边界，无法靠"更聪明的实现"绕过。
- 替代方案：① 用 C 重写所需功能并编译成普通 `.dll`，用 `ctypes` 调用（推荐）；② 纯 Python 实现。

## 四、与 zuzo 语言侧 CFFI 的关系

同一套底层（`object.CLib` / `CallCFunc*`），两种语法入口：

| zuzo 语言 | Python（ctypes） |
|---|---|
| `加载C库("x.dll")` | `ctypes.CDLL("x.dll")` |
| `lib.绑定("f", ["int"], "int")` | `lib.f.argtypes = [ctypes.c_int]` + `lib.f.restype = ctypes.c_int` |
| `C调用(fn, 1)` | `lib.f(1)` |
| `lib.关闭()` | `lib.Close()` |

## 五、测试

```bash
./zuzo.exe tests_py/t33_ctypes.py     # 与 CPython 逐字节对拍（msvcrt 的 sqrt/abs/pow/strlen/sizeof）
```
