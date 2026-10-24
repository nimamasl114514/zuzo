// demo_wasm_ffi.hy — Zuzo WASM FFI 示例 (WAT 模块)
//
// 加载后直接 m.fn() 调用，零样板

m = 加载WASM("math.wasm")

打印("add(5, 3)          =", m.add(5, 3))
打印("sub(10, 4)         =", m.sub(10, 4))
打印("mul(6, 7)          =", m.mul(6, 7))
打印("square(9)          =", m.square(9))
打印("factorial(20)      =", m.factorial(20))
打印("fibonacci(50)      =", m.fibonacci(50))
打印("is_prime(997)      =", m.is_prime(997))

// 内存操作
打印("内存大小:", m.内存大小(), "字节")
m.内存写入(0, "Hello WASM!")
打印("内存读取:", m.内存读取(0, 11))

// 组合计算
a = 15
b = 7
打印("(a+b)*(a-b) =", m.mul(m.add(a, b), m.sub(a, b)))

m.关闭()
