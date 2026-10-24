;; math.wat — Zuzo WASM FFI 测试模块（源码）
;;
;; 用 wat2wasm 编译:
;;   wat2wasm math.wat -o math.wasm
;;
;; 导出函数:
;;   add(i32, i32) -> i32        加法
;;   sub(i32, i32) -> i32        减法
;;   mul(i32, i32) -> i32        乘法
;;   square(i32) -> i32          平方
;;   factorial(i32) -> i64       阶乘
;;   fibonacci(i32) -> i64       斐波那契
;;   is_prime(i32) -> i32        素数判断 (1=是, 0=否)
;;   memory                      线性内存 (1 page = 64KB)

(module
  ;; ========== 函数 ==========

  ;; add(a, b) = a + b
  (func $add (export "add") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.add)

  ;; sub(a, b) = a - b
  (func $sub (export "sub") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.sub)

  ;; mul(a, b) = a * b
  (func $mul (export "mul") (param i32 i32) (result i32)
    local.get 0
    local.get 1
    i32.mul)

  ;; square(a) = a * a
  (func $square (export "square") (param i32) (result i32)
    local.get 0
    local.get 0
    i32.mul)

  ;; factorial(n) = n!  (i64 结果避免溢出)
  (func $factorial (export "factorial") (param $n i32) (result i64)
    (local $result i64)
    (local $i i32)

    ;; result = 1
    i64.const 1
    local.set $result

    ;; i = 2
    i32.const 2
    local.set $i

    ;; for i = 2; i <= n; i++ { result *= i }
    (block $break
      (loop $continue
        ;; if i > n, break
        local.get $i
        local.get $n
        i32.gt_s
        br_if $break

        ;; result *= i
        local.get $result
        local.get $i
        i64.extend_i32_u
        i64.mul
        local.set $result

        ;; i++
        local.get $i
        i32.const 1
        i32.add
        local.set $i

        br $continue))

    local.get $result)

  ;; fibonacci(n) = fib(n)  (i64 结果)
  (func $fibonacci (export "fibonacci") (param $n i32) (result i64)
    (local $a i64)
    (local $b i64)
    (local $temp i64)
    (local $i i32)

    ;; a = 0, b = 1
    i64.const 0
    local.set $a
    i64.const 1
    local.set $b

    ;; i = 0
    i32.const 0
    local.set $i

    (block $break
      (loop $continue
        ;; if i >= n, break
        local.get $i
        local.get $n
        i32.ge_s
        br_if $break

        ;; temp = a + b
        local.get $a
        local.get $b
        i64.add
        local.set $temp

        ;; a = b
        local.get $b
        local.set $a

        ;; b = temp
        local.get $temp
        local.set $b

        ;; i++
        local.get $i
        i32.const 1
        i32.add
        local.set $i

        br $continue))

    local.get $a)

  ;; is_prime(n) -> 1 if prime, 0 if not
  (func $is_prime (export "is_prime") (param $n i32) (result i32)
    (local $i i32)

    ;; if n < 2, return 0
    local.get $n
    i32.const 2
    i32.lt_s
    if
      i32.const 0
      return
    end

    ;; if n == 2, return 1
    local.get $n
    i32.const 2
    i32.eq
    if
      i32.const 1
      return
    end

    ;; if n % 2 == 0, return 0
    local.get $n
    i32.const 2
    i32.rem_s
    i32.const 0
    i32.eq
    if
      i32.const 0
      return
    end

    ;; i = 3
    i32.const 3
    local.set $i

    ;; for i = 3; i*i <= n; i += 2
    (block $break
      (loop $continue
        ;; if i*i > n, break
        local.get $i
        local.get $i
        i32.mul
        local.get $n
        i32.gt_s
        br_if $break

        ;; if n % i == 0, return 0
        local.get $n
        local.get $i
        i32.rem_s
        i32.const 0
        i32.eq
        if
          i32.const 0
          return
        end

        ;; i += 2
        local.get $i
        i32.const 2
        i32.add
        local.set $i

        br $continue))

    ;; return 1
    i32.const 1)

  ;; ========== 内存 ==========
  (memory (export "memory") 1)
)
