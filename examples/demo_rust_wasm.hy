// demo_rust_wasm.hy — Zuzo 调用 Rust 编译的 WASM 模块
//
// Rust 源码: rust_module/src/lib.rs
// 编译命令:  rustc --target wasm32-unknown-unknown -O --crate-type cdylib src/lib.rs -o rust_math.wasm
//
// 加载后直接 m.fn() 调用，零样板

m = 加载WASM("rust_math.wasm")

打印("add(100, 200)       =", m.add(100, 200))
打印("factorial(10)      =", m.factorial(10))
打印("factorial(20)      =", m.factorial(20))
打印("fibonacci(50)      =", m.fibonacci(50))
打印("fibonacci(90)      =", m.fibonacci(90))
打印("gcd(1071, 462)     =", m.gcd(1071, 462))
打印("is_prime(7919)     =", m.is_prime(7919))

// 组合调用
打印("gcd(fib(15), fib(10)) =", m.gcd(m.fibonacci(15), m.fibonacci(10)))

// 内存操作
打印("内存大小:", m.内存大小(), "字节")
m.内存写入(0, "Hello Rust!")
打印("内存读取:", m.内存读取(0, 11))

// 列出导出
打印("导出:", m.导出())

// 清理
m.关闭()
