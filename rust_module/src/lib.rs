// rust_module — Zuzo WASM FFI 演示模块 (Rust 源码)
//
// 编译:
//   rustc --target wasm32-unknown-unknown -O --crate-type cdylib src/lib.rs -o rust_math.wasm
//
// 导出函数:
//   add(i32, i32) -> i32        加法
//   factorial(i32) -> i64       阶乘
//   gcd(i32, i32) -> i32        最大公约数
//   fibonacci(i32) -> i64       斐波那契
//   string_len(i32) -> i32      读取内存中字符串长度 (模拟)
//   sum_array(offset, len) -> i32  对内存中的 i32 数组求和

#![no_std]

// 防止 Rust 生成浮点相关 panic 处理代码
#[panic_handler]
fn panic(_: &core::panic::PanicInfo) -> ! {
    loop {}
}

/// add(a, b) = a + b
#[no_mangle]
pub extern "C" fn add(a: i32, b: i32) -> i32 {
    a + b
}

/// factorial(n) = n!
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

/// gcd(a, b) = 最大公约数 (辗转相除)
#[no_mangle]
pub extern "C" fn gcd(mut a: i32, mut b: i32) -> i32 {
    while b != 0 {
        let t = b;
        b = a % b;
        a = t;
    }
    a.abs()
}

/// fibonacci(n) = 第 n 个斐波那契数 (i64)
#[no_mangle]
pub extern "C" fn fibonacci(n: i32) -> i64 {
    let mut a: i64 = 0;
    let mut b: i64 = 1;
    let mut i = 0;
    while i < n {
        let temp = a + b;
        a = b;
        b = temp;
        i += 1;
    }
    a
}

/// is_prime(n) -> 1 if prime, 0 if not
#[no_mangle]
pub extern "C" fn is_prime(n: i32) -> i32 {
    if n < 2 {
        return 0;
    }
    if n == 2 {
        return 1;
    }
    if n % 2 == 0 {
        return 0;
    }
    let mut i = 3;
    while i * i <= n {
        if n % i == 0 {
            return 0;
        }
        i += 2;
    }
    1
}
