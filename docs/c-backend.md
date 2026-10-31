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

# 选择编译链：内嵌 nativecc / 外部 gcc / native 优先自动回退
zuzo --emit-exe 例子.hy --cc=native
zuzo --emit-exe 例子.hy --cc=gcc
zuzo --emit-exe 例子.hy --cc=auto
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
├─ gcc -O2 -std=c11 → exe（权威对照）
└─ nativecc → x86-64 → PE exe（零外部 C 编译器依赖）
```

内嵌编译器的模块、ABI 与验收见 [nativecc](nativecc.md)。

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

## 六、能力矩阵（第23轮现状）

| 能力 | 状态 | 说明 |
|---|---|---|
| 打印 / 字面量（整数·浮点·字符串·布尔·空） | ✅ | 逐字节对齐 |
| 变量声明 / 赋值（含隐式声明） | ✅ | |
| 算术 `+ - * / %` / 比较 / 逻辑（短路） | ✅ | 含整数溢出 E210 与除零守卫 |
| `如果 / 否则 如果 / 否则`、if 表达式 | ✅ | |
| `当`、`对于`（C 风格）、`跳出`/`跳过` | ✅ | continue 走 update 标签，不跳过自增 |
| `对于 x 在 0 到 n`（闭区间）/ `对于 x 在 数组` | ✅ | |
| 函数定义 / 递归 / 多参数 / 隐式返回 | ✅ | |
| 匿名函数 / 闭包捕获 / 函数作为值 / 高阶函数 | ✅ | 自由变量分析 + ZBox 堆装箱 + env 结构体 |
| `执行 / 遇到` 错误处理 | ✅ | setjmp/longjmp（解释器 runCode 路径本身不捕获，见 §十） |
| `定义` 数据建模（构造函数 + 属性读写） | ✅ | 实例用哈希承载；方法暂不支持 |
| `导入` 模块（直接导入） | ✅ | 编译期展开；命名导入暂不支持 |
| 数组：字面量/下标/推入/弹出/首项/末项/长度/连接/包含 | ✅ | 越界返回 空 |
| 哈希：字面量/下标读写/键/取值（带默认值）/包含 | ✅ | 保插入顺序 |
| 数学：绝对值/最大/最小/取整族/幂/平方根/三角/对数/随机 | ✅ | 13 个 |
| 字符串全局：大写/小写/前缀/后缀/查找/替换/分割/去空白/裁剪/重复/计数/分词/取字符 | ✅ | 14 个 |
| `文本` 命名空间 | ✅ 27 个 | 大小写族/裁剪族/查找族/分割族/对齐族/子串/按行分割/比较 |
| JSON：解析 / 序列化 | ✅ | 序列化 key 按字典序（对齐 Go json.Marshal） |
| 文件 I/O：读/写/追加/存在/创建/删除/重命名 | ✅ | |
| 编码：base64 / hex / url 编解码 | ✅ | |
| 摘要：md5 / sha1 / sha256 | ✅ | 标准实现，输出小写 hex（`abc` 三组测试向量全对） |
| 时间：时间戳 / 毫秒时间戳 / 当前时间 / 睡眠 | ✅ | |
| 正则：验证/匹配/查找全部/替换/分割/提取 | ✅ | 回溯子集（`. * + ? [] ^ $ \d\w\s ()`），捕获组未单独提取 |
| 并发 / 网络 / HTTP / 浏览器 / WASM / CFFI | ⛔ 明确报错 | 编译期报「依赖 X，C 后端不支持」（`builtin.go:unsupportedBuiltins` 名单） |
| 堆对象回收 | ⏳ 未做 | 只分配不释放（短进程无感） |
| 「文本」Unicode 大小写/分类（~83 成员） | ⏳ 未做 | 需码表 |
| `格式化`（fmt 风格） | ✅ | `%d %s %f %.Nf %x %X %c %v %%`；浮点半值偶数舍入 |

## 七、测试

```bash
./run_tests_c.sh       # 外部 C 编译器：C-BACKEND PASS=17 FAIL=0
./run_tests_native.sh  # 内嵌 nativecc：NATIVE-CC PASS=17 FAIL=0
# 判据：同一份 .hy，解释执行 stdout/rc 与 --emit-exe 产物 stdout/rc 完全一致
# 当前：两个后端均 17/17 通过
```

用例集 `tests_c/t01..t17`：打印 / 算术逻辑 / 条件 / while / for / 函数递归 / 数组 / 哈希 / 闭包 / 文本命名空间。
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
2. **nativecc 是受控 C 子集**：只覆盖 zuzo 生成 C 与 `zuzo_rt.c`，不承诺任意 ISO C 项目；详见 [nativecc](nativecc.md)。
3. **闭包已支持**：自由变量分析 + ZBox 堆装箱 + env 结构体，函数可作为值与参数传递。
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
