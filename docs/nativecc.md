# nativecc：zuzo 内嵌纯 Go C 编译器

> nativecc 是 zuzo 的自研 C 编译器：不调用 gcc/clang，直接把 zuzo C 后端生成的 C 与 `zuzo_rt.c` 编译成 Windows x64 PE 可执行文件。
>
> 目标不是实现完整 ISO C，而是覆盖 **zuzo 生成 C + zuzo 运行时使用的受控 C 子集**，让 `zuzo.exe` 单独完成「源码 → AST → C → PE」全链路。

## 一、用法

```bash
# 使用内嵌 nativecc，零外部 C 编译器依赖
./zuzo.exe --emit-exe examples/hello.hy -o tmp/hello.exe --cc=native
./tmp/hello.exe

# 使用外部 gcc/clang（保留作权威对照）
./zuzo.exe --emit-exe examples/hello.hy -o tmp/hello-gcc.exe --cc=gcc

# native 优先，失败时回退外部编译器
./zuzo.exe --emit-exe examples/hello.hy -o tmp/hello-auto.exe --cc=auto

# 只生成 C，不链接
./zuzo.exe --emit-c examples/hello.hy -o tmp/hello.c
```

端到端对拍：

```bash
./run_tests_native.sh   # nativecc 产物 vs 解释器
./run_tests_c.sh        # 外部 C 编译器产物 vs 解释器
go test ./nativecc      # 编译器单元与最小机器码回归
```

当前实测：

```text
NATIVE-CC PASS=17 FAIL=0
C-BACKEND PASS=17 FAIL=0
```

单例编译 `tests_c/t01_print.hy`（含 `main.c + zuzo_rt.c`，7 次实跑）：

```text
samples_ms=68.504,138.946,68.255,65.758,68.645,92.715,68.903
median_ms=68.645
min_ms=65.758
max_ms=138.946
```

## 二、编译流水线

```text
zuzo .hy
  → lexer/parser（解释器同源 AST）
  → cbackend.Generate
  → main.c + zuzo_rt.c/.h
  → nativecc 预处理
  → nativecc 词法/语法/类型/语义
  → x86-64 指令编码 + 控制流/函数调用
  → PE32+ 段表、导入表、启动 stub
  → Windows x64 exe
```

| 模块 | 文件 | 职责 |
|---|---|---|
| 驱动 | `nativecc/driver.go` | 多源文件编译、AST dump、文件加载 |
| 词法 | `token.go` / `lexer.go` | UTF-8 标识符、字面量、运算符、注释 |
| 预处理 | `preproc.go` | include、对象宏、条件编译、行继续 |
| 语法 | `ast.go` / `parser.go` / `parser_expr.go` | 声明、表达式、语句、复合字面量 |
| 类型 | `type.go` / `sema.go` | struct/union/enum/typedef、布局、表达式类型 |
| 指令编码 | `enc.go` | REX/ModRM/SIB/disp/imm、SSE 标量指令 |
| 代码生成 | `codegen*.go` | 函数、控制流、调用、局部/全局数据 |
| PE 输出 | `pe.go` | PE32+、导入表、固定基址、入口 stub |
| 内置 CRT | `crt.go` | 精简头文件声明、DLL 符号映射 |

## 三、内部 ABI

内部函数全部由 nativecc 编译，因此使用受控 ABI：

- 整数/指针/结构体地址：按 `RCX / RDX / R8 / R9` 分配，随后进入栈参数区。
- 浮点：按 `XMM0 / XMM1 / XMM2 / XMM3` 分配，随后进入栈参数区。
- 结构体参数：调用方保存结构体地址，按指针传递。
- 结构体返回值：隐藏首参为返回缓冲区指针，占用一个整数寄存器槽。
- 外部可变参数函数：浮点参数同时准备 GP 副本与 XMM 值；超出寄存器面的参数写栈。

调用方把第 `i` 个栈参数写到：

```text
[rsp + 32 + i*8]
```

经过 `call` 压入返回地址、被调方 `push rbp` 后，同一个参数位于：

```text
[rbp + 48 + i*8]
```

被调函数序言必须逐一镜像调用方的寄存器/栈分配规则，装载所有溢出参数。缺失这一步会导致：

- 六参函数的第五、第六参读取未初始化槽；
- 返回结构体的四参函数最后一个参数读取未初始化槽；
- `zre_match_here` 的 `slen/si`、`zs_pad` 的 `mode`、`zs_replace_n` 的 `limit` 全部失真。

对应永久回归：

- `nativecc/stack_args_test.go::TestStackArgsSixIntegers`
- `nativecc/stack_args_test.go::TestStackArgsWithStructReturn`

## 四、CRT 与浮点格式化口径

nativecc 当前导入 `msvcrt.dll`。旧 CRT 与 MinGW GCC 使用的 UCRT 在半值格式化上存在实测差异：

```c
snprintf(buf, sizeof(buf), "%.1f", 2.25);
// nativecc + msvcrt：2.3
// gcc + UCRT：2.2
```

反汇编已核对：2.25 的位模式为 `0x4002000000000000`，可变参数栈槽 `[rsp+0x20]` 正确，因此该差异不是浮点传参错误。

zuzo 的语义以解释器为准。`zuzo_rt.c` 在 `格式化` 的 `%f` 路径先执行十进制 **round-half-even（半值偶数舍入）**，再交给 CRT 输出，保证 native/GCC/解释器三侧一致。

对应永久回归：

- `nativecc/float_round_test.go::TestZFormatHalfEven`
- `tests_c/t18_format.hy`（native 与 GCC 双后端均对拍）

## 五、当前能力边界

已覆盖并通过端到端对拍：

- 打印、变量、整数/浮点/字符串/布尔/空值；
- 算术、比较、短路逻辑、条件与循环；
- 函数、递归、闭包捕获、高阶函数；
- 数组、哈希、字符串、JSON、文件 I/O；
- 编码、摘要、时间、正则、定义、导入、格式化、读目录、环境变量。

保留 `cbackend` 的能力边界：并发、网络、HTTP、浏览器、WASM、CFFI 由 `unsupportedBuiltins` 在编译期给出明确错误，不生成含糊的未定义符号。

nativecc 自身只承诺受控 C 子集，不承诺完整 ISO C、通用 Windows SDK 或任意第三方 C 项目。

堆对象回收已由 zuzo 运行时在**第27轮**落地（精确根集 mark-sweep，见 [gc](gc.md)），当前 17 例双后端对拍全绿。

### 已知缺陷（第27轮实测记录）

1. **`.rdata` 必须可写**：导入表 IAT 与字符串常量同处 `.rdata`，加载器要在 `LdrProcessInitializationComplete` 阶段写 IAT。若该段标只读，字符串常量多于约 20 个（IAT 被挤到 `0x1000` 页边界附近）时进程启动即崩。已改为 `0xC0000040`，并由 `nativecc/iat_rodata_test.go` 守门。
2. **外部 CRT 多参调用传参不可靠**：`memset(dst, c, n)` / `memcpy(dst, src, n)` 这类三参外部调用实测会写越界（第26轮修的是自家函数调用的栈参数装载，未覆盖外部函数）。当前规避：zuzo 运行时内不用 `memset`/`memcpy`，改显式循环；待修。

## 六、排障与观测

```bash
# 单测并显示机器码规模
 go test ./nativecc -v -count=1

# 只跑栈参数与浮点回归
 go test ./nativecc -run 'TestStackArgs|TestZFormatHalfEven' -v -count=1

# native 与 GCC 双后端对拍
./run_tests_native.sh && ./run_tests_c.sh

# 检查 Go 源码格式
gofmt -l nativecc cbackend
```

定位机器码问题时，先用 `nativecc.ParseSources + nativecc.Compile` 读取 `Compiled.TextLabels`，再按 `ImageBase + .text RVA + label offset` 反汇编。PE 当前不携带通用符号表，直接用 `objdump --disassemble=<函数名>` 不会命中。
