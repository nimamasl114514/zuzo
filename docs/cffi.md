# Zuzo C 库 FFI 文档

Zuzo 支持直接加载和调用 C 动态库（`.dll` / `.so` / `.dylib`），实现无缝的 C 互操作。

## 快速开始

### 30 秒上手

```zuzo
// 加载 C 数学库
lib = 加载C库("msvcrt.dll")              // Windows
// lib = 加载C库("libm.so.6")            // Linux
// lib = 加载C库("libm.dylib")           // macOS

// 声明函数签名并调用
sqrt_fn = lib.绑定("sqrt", ["double"], "double")
打印(C调用(sqrt_fn, 144.0))              // → 12

// 或者直接动态调用（整数函数）
打印(lib.abs(-42))                       // → 42

lib.关闭()
```

---

## API 参考

### 加载库

| 语法 | 说明 |
|------|------|
| `加载C库(路径)` | 加载动态库，返回 C 库对象 |
| `加载C库(路径, 名称)` | 加载并指定名称 |

### 点号语法

| 语法 | 说明 |
|------|------|
| `lib.绑定(函数名, [参数类型...], 返回类型)` | 声明函数签名，返回 C 函数对象 |
| `lib.函数名(参数...)` | 动态调用（自动推断类型，整数返回） |
| `lib.符号(函数名)` | 查找符号地址（调试用） |
| `lib.关闭()` | 关闭库，释放资源 |

### 低级 API

| 语法 | 说明 |
|------|------|
| `C函数声明(库, 函数名, [参数类型...], 返回类型)` | 声明函数签名 |
| `C调用(函数对象, 参数...)` | 调用已声明的 C 函数 |

### C 函数对象属性

| 属性 | 说明 |
|------|------|
| `fn.名称` | 函数名 |
| `fn.签名` | 函数签名描述 |

---

## 类型系统

### 支持的 C 类型

| 类型字符串 | C 类型 | 说明 |
|-----------|--------|------|
| `"void"` | `void` | 无返回值 |
| `"int"` | `int` | 32 位有符号整数 |
| `"uint"` | `unsigned int` | 32 位无符号整数 |
| `"long"` | `long` | 长整数（64位 Unix / 32位 Windows） |
| `"ulong"` | `unsigned long` | 无符号长整数 |
| `"longlong"` | `long long` | 64 位有符号整数 |
| `"float"` | `float` | 32 位单精度浮点 |
| `"double"` | `double` | 64 位双精度浮点 |
| `"char"` | `char` | 8 位字符 |
| `"uchar"` | `unsigned char` | 8 位无符号字符 |
| `"short"` | `short` | 16 位短整数 |
| `"ushort"` | `unsigned short` | 16 位无符号短整数 |
| `"bool"` | `bool` | 布尔值 |
| `"ptr"` | `void*` | 原始指针 |
| `"string"` | `const char*` | C 字符串（自动转换） |

### 类型映射

| Zuzo 类型 | C 类型 | 说明 |
|-----------|--------|------|
| 整数 | `int` / `long` / `longlong` | 根据声明自动转换 |
| 浮点数 | `float` / `double` | 根据声明自动转换 |
| 布尔 | `bool` / `int` | `真` → `1`, `假` → `0` |
| 字符串 | `const char*` | 自动分配 C 字符串，调用后释放 |

---

## 三种调用方式

### 方式一：类型安全模式（推荐）

先声明函数签名，再调用。类型安全，支持浮点返回值。

```zuzo
lib = 加载C库("msvcrt.dll")

// 声明签名
sqrt_fn = lib.绑定("sqrt", ["double"], "double")
pow_fn = lib.绑定("pow", ["double", "double"], "double")
abs_fn = lib.绑定("abs", ["int"], "int")

// 调用
打印(C调用(sqrt_fn, 144.0))         // → 12
打印(C调用(pow_fn, 2.0, 10.0))      // → 1024
打印(C调用(abs_fn, -42))            // → 42
```

### 方式二：动态调用模式

直接点号调用，自动推断参数类型。简单但默认返回整数。

```zuzo
lib = 加载C库("msvcrt.dll")

// 整数函数直接调用
打印(lib.abs(-100))                 // → 100

// 注意：浮点返回值会被截断为整数
// 对于浮点函数，请使用类型安全模式
```

### 方式三：低级 API

```zuzo
lib = 加载C库("msvcrt.dll")
fn = C函数声明(lib, "sqrt", ["double"], "double")
打印(C调用(fn, 144.0))              // → 12
```

---

## 完整示例

### 调用数学库

```zuzo
lib = 加载C库("msvcrt.dll")

sqrt_fn = lib.绑定("sqrt", ["double"], "double")
ceil_fn = lib.绑定("ceil", ["double"], "double")
floor_fn = lib.绑定("floor", ["double"], "double")
pow_fn = lib.绑定("pow", ["double", "double"], "double")

打印("sqrt(2) =", C调用(sqrt_fn, 2.0))
打印("ceil(3.14) =", C调用(ceil_fn, 3.14))
打印("floor(3.99) =", C调用(floor_fn, 3.99))
打印("pow(2, 10) =", C调用(pow_fn, 2.0, 10.0))

lib.关闭()
```

### 调用字符串函数

```zuzo
lib = 加载C库("msvcrt.dll")

strlen_fn = lib.绑定("strlen", ["string"], "ulong")
打印(C调用(strlen_fn, "Hello, C FFI!"))    // → 13
打印(C调用(strlen_fn, "你好"))              // → 6 (UTF-8 字节数)

lib.关闭()
```

### 查询符号地址

```zuzo
lib = 加载C库("msvcrt.dll")
打印("sqrt 地址:", lib.符号("sqrt"))
打印("abs 地址:", lib.符号("abs"))
lib.关闭()
```

---

## 跨平台库路径

| 平台 | 数学库 | 标准库 |
|------|--------|--------|
| Windows | `msvcrt.dll` | `msvcrt.dll` |
| Linux | `libm.so.6` | `libc.so.6` |
| macOS | `libm.dylib` | `libSystem.dylib` |

```zuzo
// 跨平台加载
变量 libPath = 如果 运行时平台() == "windows" {
    "msvcrt.dll"
} 否则 如果 运行时平台() == "darwin" {
    "libm.dylib"
} 否则 {
    "libm.so.6"
}
lib = 加载C库(libPath)
```

---

## 技术架构

```
┌─────────────────────────────────────────┐
│              Zuzo 代码 (.hy)             │
│   lib = 加载C库("msvcrt.dll")           │
│   fn = lib.绑定("sqrt", ["double"], "double") │
│   C调用(fn, 144.0)                      │
└──────────────┬──────────────────────────┘
               │ 点号语法 / 低级 API
               ▼
┌─────────────────────────────────────────┐
│       builtins_cffi.go (FFI 层)         │
│   类型转换: Zuzo ↔ C (int/float/str/ptr) │
│   函数签名声明 + 动态调用                │
└──────────────┬──────────────────────────┘
               │ CGO 调用桥
               ▼
┌─────────────────────────────────────────┐
│         clib_call.go (CGO 调用器)        │
│   zuzo_call_c() — 函数指针调用          │
│   支持 0~10 个参数，整数+浮点混合        │
└──────────────┬──────────────────────────┘
               │ dlopen / LoadLibrary
               ▼
┌─────────────────────────────────────────┐
│       clib_loader.go (动态库加载)        │
│   Windows: LoadLibraryW + GetProcAddress │
│   Unix: dlopen + dlsym + dlclose        │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│         C 动态库 (.dll/.so/.dylib)       │
│   msvcrt.dll / libm.so / libm.dylib    │
└─────────────────────────────────────────┘
```

### 核心文件

| 文件 | 作用 |
|------|------|
| `object/clib.go` | C 库和 C 函数对象类型定义 |
| `object/clib_loader.go` | 跨平台动态库加载 (CGO) |
| `object/clib_call.go` | C 函数指针调用器 (CGO) |
| `evaluator/builtins_cffi.go` | FFI 内置函数 + 点号语法支持 |

### 设计特点

- **跨平台** — Windows 用 LoadLibrary，Unix 用 dlopen，API 统一
- **类型安全** — 声明式函数签名，编译时类型检查
- **动态调用** — 支持直接点号调用，零样板代码
- **字符串互转** — 自动处理 Go string ↔ C string 转换
- **资源管理** — 显式 `lib.关闭()` 释放资源
- **支持 0-10 参数** — 覆盖绝大多数 C 库函数
- **整数+浮点混合** — 自动处理浮点参数和返回值
