# C 后端（zuzo → C → exe）

> 第21轮新增。目标：让 zuzo 在解释执行之外，具备 **AOT 编译**能力——
> 直接把 `.hy` 翻译成 C 源码，再交给本机 C 编译器产出原生可执行文件。
>
> 设计上借鉴 Nim 编译器的 C 后端思路（parser → 语义 → C 代码生成 → 外部 C 编译器链接），
> 但实现完全独立：不依赖、不修改 Nim 源码。

## 一、用法

```bash
# 生成 C 源码（同时把运行时 zuzo_rt.c / zuzo_rt.h 放到同目录）
zuzo --emit-c 例子.hy
zuzo --emit-c 例子.hy -o 输出目录/例子.c

# 一步出可执行文件
zuzo --emit-exe 例子.hy
zuzo --emit-exe 例子.hy -o 输出目录/例子.exe

# 指定编译器（默认自动探测 gcc/clang/zig cc）
ZUZO_CC="C:\path\to\gcc.exe" zuzo --emit-exe 例子.hy

# 保留中间产物（调试代码生成）
ZUZO_KEEP_TEMP=1 zuzo --emit-exe 例子.hy
```

编译器探测顺序：`ZUZO_CC` 环境变量 → PATH 里的 `gcc`/`clang`/`cc` → 常见安装路径
（`%LOCALAPPDATA%\Programs\mingw64\bin\gcc.exe` 等）→ `zig cc`。

## 二、架构（三层）

```
zuzo 源码 .hy
    │  lexer + parser（复用解释器前端，AST 完全一致）
    ▼
ast.Program
    │  cbackend.Generate（本包）
    ▼
C 源码（main.c）
    │  + zuzo_rt.c / zuzo_rt.h（运行时，go:embed 内嵌进 zuzo.exe）
    ▼
gcc -O2 -std=c11  →  exe
```

关键点：**前端复用**。C 后端直接吃解释器的 AST，因此语法解析、错误诊断与解释执行完全同源，
不存在"两套语法两套 bug"的问题。

## 三、值模型

zuzo 是动态类型语言，C 是静态类型。C 侧用统一的 tagged union 承载：

```c
typedef enum { ZK_NULL, ZK_INT, ZK_FLOAT, ZK_STR, ZK_BOOL,
               ZK_ARRAY, ZK_HASH, ZK_FUNC } ZKind;

struct ZVal {
    ZKind kind;
    union {
        int64_t i; double f; ZStr* s; int b;
        ZArr* a; ZHash* h; ZFunc* fn;
    } as;
};
```

语义对齐点（逐条对齐 `evaluator/evaluator.go`）：

| 语义 | 解释器 | C 运行时 |
|---|---|---|
| 真值判定 | 空/假/0/0.0/""/空数组/空哈希 = 假 | `z_truthy` 一致 |
| 整数溢出 | 超 int64 报 E210 | `__builtin_*_overflow` + `z_die("E210")` |
| 除零 | E211 | `z_die("E211")` |
| 取模零 | E212 | `z_die("E212")` |
| 混合数值 | int+float → float | `z_add` 等先做数值提升 |
| 字符串 | `+` 拼接、`==`/`<` 内容比较 | 一致 |
| 数组越界 | 返回 空 | 一致 |
| 哈希缺键 | 返回 空 | 一致 |
| 打印 | 各参数 `Inspect()` 空格连接 | `z_inspect` 同口径（含 `[1, 2]`、`{k: v}`、真/假/空） |
| 换行 | Go 输出 LF | Windows 下 `_setmode(stdout, _O_BINARY)` 对齐 LF |

## 四、表达式降级（ANF）

C 没有"表达式里的 if"，因此需要多步求值的表达式统一降级成「语句 + 临时量」：

| zuzo 表达式 | C 生成形态 |
|---|---|
| `如果 c { a } 否则 { b }` | `ZVal zt1; if (z_truthy(c)) { zt1 = a; } else { zt1 = b; }` |
| `a 并且 b` | `ZVal ztN; if (!z_truthy(a)) ztN = z_bool(0); else ztN = z_bool(z_truthy(b));` |
| `{k: v, ...}` 哈希字面量 | `ZVal ztN = z_hash(); z_hash_set(ztN, ...);` |

哈希字面量在 AST 里是 `map[Expression]Expression`（无序），生成时按 `String()` 排序，
保证**同一份源码每次生成的 C 完全一致**（可复现构建）。

## 五、循环与 continue 的坑（已修）

`对于 i = 0; i < n; i = i + 1 { ... 跳过 ... }` 里，`跳过`（continue）必须仍然执行 update。
C 的 `while` 体末尾放 update 会被 continue 跳过 → 死循环。本实现的处理：

```c
while (1) {
    if (!z_truthy(cond)) break;
    ...body...
zupd_1: ;
    i = z_add(i, z_int(1));
}
```

`跳过` 生成 `goto zupd_1;`，保证 update 一定执行。嵌套循环用独立标签计数器，
遍历类循环（while / 对于 x 在 ...）压空标签，避免 continue 误跳到外层 for 的 update。

## 六、能力矩阵（第21轮）

| 能力 | 状态 | 说明 |
|---|---|---|
| 打印 / 字面量（整数·浮点·字符串·布尔·空） | ✅ | 逐字节对齐 |
| 变量声明 / 赋值（含隐式声明） | ✅ | `变量 x = ...`、`x = ...` |
| 算术 `+ - * / %` / 比较 / 逻辑（短路） | ✅ | 含整数溢出与除零守卫 |
| `如果 / 否则 如果 / 否则`、if 表达式 | ✅ | |
| `当`（while）、`对于`（C 风格 for）、`跳出`/`跳过` | ✅ | |
| `对于 x 在 0 到 n`（闭区间） | ✅ | |
| `对于 x 在 数组`（含索引变量） | ✅ | |
| 函数定义 / 递归 / 多参数 / 隐式返回 | ✅ | |
| 数组：字面量 / 下标读写 / `推入` / `长度` / `连接` | ✅ | 越界返回 空 |
| 哈希：字面量 / 下标读写 / 缺键返回 空 | ✅ | 保插入顺序 |
| f-string 插值 | ✅ | `z_concat` |
| 内置：`打印 长度 推入 连接 字符串 整数 浮点数 类型 包含` | ✅ | |
| 匿名函数 / 闭包捕获 / 函数作为值 / 高阶函数 | ✅ 第22轮 | 自由变量分析 + 堆装箱 + env 结构体 |
| `执行 / 遇到` 错误处理 | ✅ 第22轮 | setjmp/longjmp；解释器 runCode 路径本身不捕获（见 §十） |
| `文本` 命名空间核心成员（14 个） | ✅ 第22轮 | 大写/小写/去空白/替换/分割/重复/查找/包含/前缀/后缀/比较/子串/按行分割/连接 |
| `定义` 数据建模 | ⏳ 第23轮 | |
| `文本` 命名空间其余成员（~96 个，Unicode 语义） | ⏳ 第23轮 | 需 Unicode 大小写与分类表 |
| 数组/哈希/JSON/数学/时间/IO 全量内置 | ⏳ 第23轮 | |
| 正则 / 编码 / 并发 / 网络 / HTTP | ⏳ 第24轮 | |
| `导入` 模块、函数作为值、性能特化 | ⏳ 第25轮 | |
| 浏览器自动化 / WASM FFI / CFFI | ⏳ 编译期明确报错 | 依赖 Go 运行时，不迁 C |

## 六点五、闭包（第22轮）

```zuzo
函数 计数器() {
    变量 n = 0
    函数 递增() { n = n + 1  返回 n }
    返回 递增
}
变量 c = 计数器()
打印(c())   // 1
打印(c())   // 2
```

编译期做三件事：

1. **自由变量分析**（`cbackend/capture.go`）：对每个函数算 `Free = 引用 - 本层声明`；子函数的 `Free ∩ 父函数 Locals` 即捕获集，未命中的继续上传。
2. **堆装箱**：被捕获的局部变量声明为 `ZBox*`（`ZVal` 的堆壳），读写走 `zv_n->v`，保证多个闭包共享同一份可变状态。
3. **env 结构体**：每个捕获闭包生成 `typedef struct { ZBox* v_n; } ZEnv_zf_xxx;`，创建闭包时分配并填字段；函数体内 `zenv->v_n->v` 访问。

函数值统一签名 `ZVal f(int argc, ZVal* argv, void* env)`，调用走 `z_call(f, argc, argv)`；具名函数直接调用可跳过装箱。

## 六点六、错误处理（第22轮）

`执行 { } 遇到 e { }` 编译为 setjmp/longjmp：

```c
{
    ZTryFrame zt7;
    zt7.prev = z_try_top; z_try_top = &zt7;
    if (setjmp(zt7.buf) == 0) {
        ...body...
        z_try_top = zt7.prev;
    } else {
        z_try_top = zt7.prev;
        ZVal zv_e = z_error_take();
        ...handler...
    }
}
```

运行时侧 `z_die` 检查 `z_try_top`：有帧则把错误文本包成 ZVal 并 `longjmp`，无帧则打印到 stderr 并 `exit(1)`。
**注意**：解释器的 `runCode` 路径对运行时错误直接 `exit(1)`，`执行/遇到` 只在 Go 单测的 `parseAndEval` 路径生效（见 §十）。

## 七、测试

```bash
./run_tests_c.sh
# 判据：同一份 .hy，解释执行 stdout/rc 与 --emit-exe 产物 stdout/rc 完全一致
# 当前：C-BACKEND PASS=10 FAIL=0
```

用例集 `tests_c/t01..t09 + t11`：打印 / 算术逻辑 / 条件 / while / for / 函数递归 / 数组 / 哈希 / 闭包 / 文本命名空间。
`tests_c/x01_error.hy` 不参与对拍：解释器 runCode 路径不捕获运行时错误（既有缺陷），C 端按 AST 语义实现。

### 性能基线（`python bench/cbackend/bench.py`，5 轮中位数，含进程启动）

| 用例 | 解释器（SAP） | C 产物 | 加速 |
|---|---|---|---|
| 1e6 整数累加 | 29.27 ms | 29.81 ms | 0.98x |
| fib(27) 递归 | 352.33 ms | 23.06 ms | **15.28x** |
| 200 元素冒泡排序 | 41.78 ms | 9.04 ms | **4.62x** |

结论：整数热点循环的解释器 SAP 已接近 C（差异主要在进程启动）；**函数调用与数组操作是 C 后端的优势区**。

## 八、已知限制

1. **堆对象不回收**：字符串/数组/哈希本轮只分配不释放（程序退出由 OS 回收）。长驻程序会涨内存，
   第23轮加引用计数。
2. **闭包未支持**：捕获外部变量的匿名函数会报错，第22轮实现（需要把捕获变量提升到堆环境）。
3. **函数不能当值传递**：签名按编译期参数个数生成，高阶函数第22轮。
4. **哈希查找是线性**：保插入顺序优先；数据量大时第23轮换「开放寻址 + 顺序数组」双结构。
5. **错误信息格式**：C 侧 `z_die` 输出 `错误 E210: ...` 并 exit(1)，与解释器的诊断渲染格式未做逐字节对齐。

## 九、可复现

```bash
cd D:/逆向/20261024-12-47-05-623/zuzo

# 1) 构建（本机 gcc 不在 PATH，需显式指定 CC）
CGO_ENABLED=1 CC="C:/Users/wwww/AppData/Local/Programs/mingw64/bin/gcc.exe" \
  go build -o zuzo.exe ./

# 2) 出 C
./zuzo.exe --emit-c examples/hello.hy -o tmp/c/hello.c

# 3) 出 exe 并跑
./zuzo.exe --emit-exe examples/fibonacci.hy -o tmp/c/fib.exe
./tmp/c/fib.exe

# 4) 对拍
./run_tests_c.sh
```

## 十、与解释器的行为差异（实测，2026-10-31 第22轮）

| 场景 | 解释器 | C 产物 | 判定 |
|---|---|---|---|
| `执行 { 10 / 0 } 遇到 e { ... }` | 编译期常量折叠直接报 E202 并 `exit(1)`，catch 不生效 | 运行期 setjmp 捕获（若除数为变量） | 解释器缺陷（`runCode` 路径）；`tests/safety_test.go` 的 `parseAndEval` 路径才生效 |
| `执行 { a - b } 遇到 e { ... }`（a 字符串 b 整数） | 运行时错误冒泡到顶层，`exit(1)` | setjmp 捕获，进入 handler | 同上 |
| 数组越界读 | 返回 空 | 返回 空 | 一致 |
| 哈希缺键读 | 返回 空 | 返回 空 | 一致 |

结论：`执行/遇到` 在 C 后端按 AST 设计语义实现；与解释器 `runCode` 路径的差异属解释器既有缺陷，已单列用例 `tests_c/x01_error.hy`（不参与逐字节对拍）。
