# Zuzo WASM FFI 文档

Zuzo 通过 WebAssembly (WASM) 实现外部函数调用 (FFI)。
任何能编译为 `.wasm` 的语言都能被 Zuzo 直接调用。

## 快速开始

### 30 秒上手

```zuzo
m = 加载WASM("math.wasm")
打印(m.add(5, 3))       // → 8
打印(m.fibonacci(50))   // → 12586269025
m.关闭()
```

加载 `.wasm` 文件后，导出函数通过点号语法直接调用，无需样板代码。

---

## API 参考

### 点号语法（推荐）

加载模块后，导出函数即可通过 `模块.函数名(参数)` 调用。

| 语法 | 说明 |
|------|------|
| `m = 加载WASM(路径)` | 加载 `.wasm` 文件，返回模块对象 |
| `m.函数名(参数...)` | 调用 WASM 导出函数 |
| `m.导出()` | 列出所有导出函数 |
| `m.内存大小()` | 返回线性内存大小（字节） |
| `m.内存读取(偏移, 长度)` | 从线性内存读取数据，返回字符串 |
| `m.内存写入(偏移, 数据)` | 向线性内存写入字符串，返回写入字节数 |
| `m.关闭()` | 关闭模块，释放资源 |

**示例：**

```zuzo
m = 加载WASM("rust_math.wasm")

// 直接调用导出函数
打印(m.add(100, 200))           // → 300
打印(m.factorial(20))           // → 2432902008176640000
打印(m.fibonacci(90))           // → 2880067194370816120
打印(m.gcd(1071, 462))          // → 21
打印(m.is_prime(7919))          // → 1

// 组合调用
打印(m.gcd(m.fibonacci(15), m.fibonacci(10)))  // → 5

// 内存操作
打印(m.内存大小())               // → 1114112 (字节)
m.内存写入(0, "Hello Rust!")
打印(m.内存读取(0, 11))          // → Hello Rust!

// 列出导出
打印(m.导出())                   // → [add(2参数->1返回), factorial(1参数->1返回), ...]

// 关闭
m.关闭()
```

### 低级 API（兼容旧写法）

如果你需要动态指定函数名，可以使用低级 API：

```zuzo
m = 加载WASM("math.wasm")

// 动态函数名
函数名 = "add"
结果 = WASM调用(m, 函数名, 5, 3)

WASM导出(m)
WASM内存读取(m, 0, 10)
WASM内存写入(m, 0, "data")
WASM内存大小(m)
WASM关闭(m)
```

---

## 类型映射

Zuzo 自动处理与 WASM 之间的类型转换：

| Zuzo 类型 | WASM 类型 | 说明 |
|-----------|-----------|------|
| 整数 | `i32` / `i64` | 根据函数签名自动选择 |
| 浮点数 | `f32` / `f64` | 根据函数签名自动选择 |
| 布尔 | `i32` | `true` → `1`, `false` → `0` |

WASM 返回值转换：

| WASM 返回类型 | Zuzo 类型 | 说明 |
|---------------|-----------|------|
| `i32` / `i64` | 整数 | |
| `f32` / `f64` | 浮点数 | |
| 无返回值 | 空 | |
| 多返回值 | 数组 | 按顺序放入数组 |

---

## 如何编写 WASM 模块

### 方式一：Rust（推荐）

**安装 target：**
```bash
rustup target add wasm32-unknown-unknown
```

**源码 (`src/lib.rs`)：**
```rust
#![no_std]

#[panic_handler]
fn panic(_: &core::panic::PanicInfo) -> ! { loop {} }

#[no_mangle]
pub extern "C" fn add(a: i32, b: i32) -> i32 {
    a + b
}

#[no_mangle]
pub extern "C" fn factorial(n: i32) -> i64 {
    let mut result: i64 = 1;
    let mut i = 2;
    while i <= n {
        result *= i as i64;
        i += 1;
    }
    result
}
```

**编译：**
```bash
rustc --target wasm32-unknown-unknown -O --crate-type cdylib src/lib.rs -o math.wasm
```

**Zuzo 调用：**
```zuzo
m = 加载WASM("math.wasm")
打印(m.add(5, 3))         // → 8
打印(m.factorial(20))     // → 2432902008176640000
m.关闭()
```

### 方式二：WAT (WebAssembly Text Format)

WAT 是 WASM 的汇编文本格式，适合编写小型模块。

**源码 (`math.wat`)：**
```wat
(module
  (func $add (export "add") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add)

  (func $mul (export "mul") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.mul)

  (memory (export "memory") 1)
)
```

**编译（需要 wabt 工具）：**
```bash
# 安装
npm install -g wabt

# 编译
wat2wasm math.wat -o math.wasm
```

**Zuzo 调用：**
```zuzo
m = 加载WASM("math.wasm")
打印(m.add(5, 3))     // → 8
打印(m.mul(6, 7))     // → 42
m.关闭()
```

### 方式三：C / C++ (Emscripten)

**安装 Emscripten：**
```bash
# https://emscripten.org/docs/getting_started/downloads.html
```

**源码 (`math.c`)：**
```c
int add(int a, int b) {
    return a + b;
}

long long factorial(int n) {
    long long result = 1;
    for (int i = 2; i <= n; i++) {
        result *= i;
    }
    return result;
}
```

**编译：**
```bash
emcc math.c -o math.wasm \
  -O3 \
  -s EXPORTED_FUNCTIONS=['_add','_factorial'] \
  -s EXPORTED_RUNTIME_METHODS=[] \
  --no-entry
```

**Zuzo 调用：**
```zuzo
m = 加载WASM("math.wasm")
打印(m.add(5, 3))         // → 8
打印(m.factorial(10))     // → 3628800
m.关闭()
```

### 方式四：Go (TinyGo)

**安装 TinyGo：**
```bash
# https://tinygo.org/getting-started/install/
```

**源码 (`math.go`)：**
```go
package main

//export add
func add(a, b int32) int32 {
    return a + b
}

//export factorial
func factorial(n int32) int64 {
    var result int64 = 1
    for i := int32(2); i <= n; i++ {
        result *= int64(i)
    }
    return result
}

func main() {}
```

**编译：**
```bash
tinygo build -target wasm -o math.wasm math.go
```

**Zuzo 调用：**
```zuzo
m = 加载WASM("math.wasm")
打印(m.add(5, 3))         // → 8
打印(m.factorial(10))     // → 3628800
m.关闭()
```

### 方式五：AssemblyScript

**安装：**
```bash
npm install -g assemblyscript
```

**源码 (`math.ts`)：**
```typescript
export function add(a: i32, b: i32): i32 {
    return a + b;
}

export function factorial(n: i32): i64 {
    let result: i64 = 1;
    for (let i: i32 = 2; i <= n; i++) {
        result *= i;
    }
    return result;
}
```

**编译：**
```bash
asc math.ts --optimize -o math.wasm
```

**Zuzo 调用：**
```zuzo
m = 加载WASM("math.wasm")
打印(m.add(5, 3))         // → 8
打印(m.factorial(10))     // → 3628800
m.关闭()
```

---

## 内存操作

WASM 模块可以导出线性内存 (Linear Memory)，Zuzo 可以直接读写。

```zuzo
m = 加载WASM("math.wasm")

// 查询内存大小
打印(m.内存大小())           // → 65536 (1 页 = 64KB)

// 写入数据到偏移 0
m.内存写入(0, "Hello WASM!")

// 从偏移 0 读取 11 字节
打印(m.内存读取(0, 11))      // → Hello WASM!

m.关闭()
```

**内存操作的应用场景：**
- 传递字符串给 WASM 函数（写入内存，传偏移+长度）
- 从 WASM 函数返回字符串（读取内存指定区域）
- 交换二进制数据

---

## 完整示例

### Rust 模块 + Zuzo 业务逻辑

**Rust 源码 (`rust_module/src/lib.rs`)：**
```rust
#![no_std]

#[panic_handler]
fn panic(_: &core::panic::PanicInfo) -> ! { loop {} }

#[no_mangle]
pub extern "C" fn add(a: i32, b: i32) -> i32 { a + b }

#[no_mangle]
pub extern "C" fn factorial(n: i32) -> i64 {
    let mut result: i64 = 1;
    let mut i = 2;
    while i <= n { result *= i as i64; i += 1; }
    result
}

#[no_mangle]
pub extern "C" fn gcd(mut a: i32, mut b: i32) -> i32 {
    while b != 0 { let t = b; b = a % b; a = t; }
    a.abs()
}

#[no_mangle]
pub extern "C" fn fibonacci(n: i32) -> i64 {
    let mut a: i64 = 0; let mut b: i64 = 1;
    let mut i = 0;
    while i < n { let t = a + b; a = b; b = t; i += 1; }
    a
}

#[no_mangle]
pub extern "C" fn is_prime(n: i32) -> i32 {
    if n < 2 { return 0; }
    if n == 2 { return 1; }
    if n % 2 == 0 { return 0; }
    let mut i = 3;
    while i * i <= n { if n % i == 0 { return 0; } i += 2; }
    1
}
```

**编译：**
```bash
cd rust_module
rustc --target wasm32-unknown-unknown -O --crate-type cdylib src/lib.rs -o rust_math.wasm
```

**Zuzo 程序 (`demo_rust_wasm.hy`)：**
```zuzo
m = 加载WASM("rust_math.wasm")

打印(m.add(100, 200))                    // → 300
打印(m.factorial(20))                    // → 2432902008176640000
打印(m.fibonacci(90))                    // → 2880067194370816120
打印(m.gcd(1071, 462))                   // → 21
打印(m.is_prime(7919))                   // → 1
打印(m.gcd(m.fibonacci(15), m.fibonacci(10)))  // → 5

打印(m.内存大小())                        // → 1114112
m.内存写入(0, "Hello Rust!")
打印(m.内存读取(0, 11))                   // → Hello Rust!

打印(m.导出())                            // → [add(2参数->1返回), ...]
m.关闭()
```

---

## 技术架构

```
┌─────────────────────────────────────────┐
│              Zuzo 代码 (.hy)             │
│   m = 加载WASM("math.wasm")             │
│   m.add(5, 3)                           │
└──────────────┬──────────────────────────┘
               │ 点号语法 → evalIndexExpression
               │ 匹配 WASM_OBJ → 返回 Builtin 闭包
               ▼
┌─────────────────────────────────────────┐
│         builtins_ffi.go (FFI 层)        │
│   callWasmFunction(mod, name, args)     │
│   类型转换: Zuzo ↔ WASM (i32/i64/f32/f64)│
└──────────────┬──────────────────────────┘
               │ wazero API
               ▼
┌─────────────────────────────────────────┐
│         wazero (纯 Go WASM 运行时)       │
│   编译缓存 · 沙箱隔离 · 零 CGo 依赖      │
└──────────────┬──────────────────────────┘
               │
               ▼
┌─────────────────────────────────────────┐
│         .wasm 二进制模块                 │
│   由 Rust / C / WAT / Go / AS 编译      │
└─────────────────────────────────────────┘
```

### 核心文件

| 文件 | 作用 |
|------|------|
| `object/wasm.go` | WASM 对象类型定义 (`WasmModule`, `WasmFunc`) |
| `evaluator/builtins_ffi.go` | FFI 内置函数实现 + 核心调用逻辑 |
| `evaluator/evaluator.go` | `evalWasmModuleIndexExpression` — 点号语法支持 |

### 设计特点

- **纯 Go 运行时** — 使用 wazero，无 CGo 依赖，跨平台
- **编译缓存** — 磁盘缓存加速重复加载同一 `.wasm` 文件
- **零样板** — 加载后直接 `m.fn()` 调用，无需中间层
- **沙箱隔离** — WASM 运行在独立沙箱中，不会影响 Zuzo 主进程
- **自动类型转换** — 根据 WASM 函数签名自动处理 i32/i64/f32/f64
- **资源管理** — 显式 `m.关闭()` 或程序结束时自动释放
