# LLVM 后端：从 AST 直接生成 LLVM IR

> 第 31 轮（2026-11-01）落地：zuzo 的后端从「AST → C 源码 → gcc/clang/内嵌 nativecc」整体换代为
> **「AST → 文本 LLVM IR（.ll）→ clang → 目标文件 → 链接运行时 → exe」**。
> 旧 C 源码后端与内嵌 C 编译器（nativecc）已删除，仓库只保留一条后端：`llvmbackend/`。

## 一、快速使用

```bash
# 1) 只生成 LLVM IR（文本 .ll，可读、可 diff、可交给任何 LLVM 工具链）
./zuzo --emit-llvm 文件.hy            # 产出 文件.ll
./zuzo --emit-llvm 文件.hy -o out.ll

# 2) 生成可执行文件（内部走 clang：IR → obj → 链接运行时）
./zuzo --emit-exe 文件.hy             # 产出 文件.exe
./zuzo --emit-exe 文件.hy -o out.exe
```

两个命令都先做词法/语法/类型检查，接受集合与解释器、`--check` 一致：

- 默认：类型检查问题按**警告**输出到 stderr，不阻断生成；
- `--strict`：类型检查问题升级为错误，生成失败（与解释器 `--strict` 同口径）。

运行环境变量：

| 变量 | 作用 |
|---|---|
| `ZUZO_CLANG` | 指定 clang 路径（默认依次找 PATH、`C:\Program Files\LLVM\bin\clang.exe`） |
| `ZUZO_KEEP_TEMP` | 保留中间目录（main.ll / 运行时源码 / obj），排查用 |
| `ZUZO_NO_SAFEPOINT` | 不生成 GC 安全点（定位 GC 相关差异用） |
| `ZUZO_RT_WARN` | 运行时越界/缺键警告开关（对拍脚本置 0） |

## 二、构建链

```
.hy 源码
  → lexer/parser（前端不变）
  → infer 类型检查（默认警告，--strict 阻断）
  → llvmbackend.Generate：AST → 文本 LLVM IR（%ZVal 值模型 + 运行时调用）
  → clang -c -O2 -x ir main.ll -o main.obj
  → clang -c -O2 -std=c11 zuzo_rt.c -o zuzo_rt.obj
  → clang main.obj zuzo_rt.obj -o a.out.exe（复制到 -o 指定位置）
```

- **目标平台**：`x86_64-pc-windows-msvc`（模块头写死 triple + datalayout）。
- **运行时**：`llvmbackend/runtime/zuzo_rt.c`（值模型、容器、内置库、GC、错误捕获）。
  它是**被链接的运行时库**（等同 libc 角色），不再有「把 zuzo 翻成 C 源码」的代码生成路径。
- **为什么运行时仍是 C**：运行时是解释器语义的黄金实现，翻译成 IR 收益为零、风险极高；
  LLVM 后端只负责「语言 → IR」，C 运行时通过 clang 编成 obj 参与链接。

## 三、值模型与 ABI

### 3.1 值模型

```llvm
%ZVal = type { i32, i64 }   ; kind + payload，16 字节，与 C 的 ZVal 完全同布局
```

| kind | 含义 | payload |
|---|---|---|
| 0 | 空 | 0 |
| 1 | 整数 | int64 |
| 2 | 浮点 | double 位模式 |
| 3 | 字符串 | ZStr* |
| 4 | 布尔 | 0/1 |
| 5 | 数组 | ZArr* |
| 6 | 哈希 | ZHash* |
| 7 | 函数 | ZFunc* |

**每个表达式求值结果 = 一个 `ptr`，指向某个 `%ZVal` 槽位**（entry 块 alloca 或全局）。
不做逐值 SSA 拆解：动态语言的装箱值在 IR 层拆开再拼回去只会制造 ABI 噪音。

### 3.2 Win64 调用约定（实测锚定）

clang 22.1.8 在 `x86_64-pc-windows-msvc` 下对 `ZVal f(ZVal a, ZVal b)` 的 IR 降级是固定的：

| C 侧 | IR 侧 |
|---|---|
| `ZVal` 参数（16 字节聚合体） | `ptr`（按引用传，调用方给槽位地址） |
| `ZVal` 返回值 | `void` + 首参 `ptr sret(%ZVal)` |
| `int64_t` / `size_t` | `i64` |
| `int` | `i32` |
| `double` | `double` |
| `void* / char* / ZBox* / ZFnPtr` | `ptr` |

例：

```llvm
declare void @z_add(ptr sret(%ZVal), ptr, ptr)
declare void @z_int(ptr sret(%ZVal), i64)
declare ptr  @z_box(ptr)
```

> 这套约定不是猜的：`tmp/abiprobe/` 里用 C 编运行时 obj + 手写 IR 调用，实测 `addv/makev/printv`
> 三型（传参 / 返回 / 指针）全部正确后才开始写后端。

### 3.3 函数签名（与 ZFnPtr 对齐）

```llvm
define internal void @zf_函数名(ptr sret(%ZVal) %ret, i32 %argc, ptr %argv, ptr %env)
```

- 所有 zuzo 函数统一四参：返回值槽、实参个数、实参数组、闭包环境；
- 具名函数直接调（编译期已校验实参个数），函数值走 `z_call`；
- 匿名函数符号为 `@zf_anon_N`；数据建模 `定义` 的构造器为 `@zdef_名字`。

## 四、模块结构

| 文件 | 职责 |
|---|---|
| `llvmbackend/llvm.go` | 基建：类型定义、alloca/临时/标签、运行时调用助手、字符串常量池、作用域、闭包构造、GC 安全点 |
| `llvmbackend/runtime.go` | 运行时函数声明表（由 `zuzo_rt.h` 原型生成，142 条）+ CRT 声明（`_setjmp`） |
| `llvmbackend/capture.go` | 自由变量分析（闭包捕获 / 装箱集合），与旧后端同构平移 |
| `llvmbackend/gen.go` | 程序装配：全局变量、具名函数、define 构造器、`@main` |
| `llvmbackend/expr.go` | 表达式 codegen |
| `llvmbackend/stmt.go` | 语句 codegen（含 try/catch、循环、for-in） |
| `llvmbackend/builtin.go` | 内置函数 / `文本` 命名空间 → 运行时函数映射 |
| `llvmbackend/compile.go` | 解析入口、导入展开、`WriteLLVM`、`BuildExe` |
| `llvmbackend/runtime/zuzo_rt.{c,h}` | 运行时库（被 clang 编译链接） |

## 五、闭包与环境

与旧后端同一套分析（`capture.go`）：

1. 每个函数收集 `Locals`（参数 + 本层声明）与 `Free`（引用但未声明，不进嵌套函数体）；
2. `子.Free ∩ 父.Locals` = 子函数捕获集；父函数里被捕获的变量标记为**装箱**（`ZBox*`）；
3. 有捕获或匿名函数 → 生成 env：`z_new_env_ex(nbox, offs, size)`，payload 是连续 `ZBox*`，
   第 i 个槽位偏移 `i*8`；偏移表由运行时拷贝（GC 靠它精确追踪）；
4. 访问捕获变量：`env + i*8 → load ZBox* → GEP %ZBox[1]`（即 `ZBox.v`，偏移 24）；
5. 函数值：`z_closure(@zf_x, env, "显示名")`；具名函数无捕获时 env 传 `null`。

`%ZBox = type { %ZObj, %ZVal }`，`%ZObj = { ptr, i32, i32, i64 }` → `v` 偏移 24，与 C 侧一致（实测 `offsetof=24`）。

## 六、GC 安全点

运行时是 mark-sweep（见 `gc.md`）。后端在每个可分配语句后发射：

```llvm
%arr = alloca [N x ptr]          ; 当前作用域全部 vPlain 变量的槽位地址
call void @z_gc_safepoint(i32 N, ptr %arr, i32 M, ptr %barr, ptr %env)
```

- `vals` = 所有 `%ZVal` 槽位地址（含顶层全局 `@zg_*`），`boxes` = 所有 `ZBox*` 变量槽位；
- `env` 非空时运行时按偏移表继续追踪捕获变量；
- 关闭开关：`ZUZO_NO_SAFEPOINT=1`（只用于排查，不用于交付）。

## 七、执行 / 遇到（try/catch）

```llvm
%frame = alloca %ZTryFrame, align 16        ; { [256 x i8] jmp_buf, ptr prev }
%fp    = call ptr @llvm.frameaddress.p0(i32 0)
%r     = call i32 @_setjmp(ptr %frame, ptr %fp)
```

两个实测要点（踩过两次）：

1. **jmp_buf 必须 16 字节对齐**：MSVC `_setjmp` 用 `movaps` 整块保存 XMM，`align 8` 会直接 #GP。
2. **Win64 的 `_setjmp` 是两参**：`_setjmp(buf, frameaddress)`；只传一个参数会让 RDX 带垃圾值，
   longjmp 时崩在 CRT 内部。

帧链：进入时 `frame.prev = z_try_top; z_try_top = &frame`；正常落穿与 catch 两条路径都先恢复
`z_try_top`，catch 再 `z_error_take()` 取错误对象。可捕获范围与解释器一致：**运行期错误（E2xx）**，
编译期诊断（E0xx/E1xx）不进入 try。

## 八、内置函数映射

`builtin.go` 把内置名逐条映射到 `z_* / zs_*` 运行时函数，覆盖：

数组（长度/追加/弹出/首尾/键/取值）· 数学（绝对值/最值/取整/幂/开方/三角/对数/随机）·
字符串（文本命名空间 30+ 成员：大小写/裁剪/替换/分割/对齐/查找/子串/UTF-8/编码/正则）·
JSON（解析/序列化）· 文件 I/O（读/写/追加/存在/创建/删除/改名）· 编码（base64/hex/url）·
摘要（md5/sha1/sha256）· 时间（unix/毫秒/now/sleep）· 目录/环境变量 · 格式化。

不支持的内置在**编译期**报位置化错误（与旧后端同一份名单与文案），不会生成「运行到才炸」的 IR。

## 九、验收

```bash
./run_tests_llvm.sh     # 解释器 vs LLVM 产物逐字节对拍（20 用例）
cd projects/mini_scheme && ../../zuzo.exe 主程序.hy          # 31/31
../../zuzo.exe --emit-exe 主程序.hy -o ../../tmp/mini.exe
../../tmp/mini.exe                                          # 31/31
```

当前状态（2026-11-01 实测）：

- `run_tests_llvm.sh`：**PASS=20 FAIL=0**；
- `projects/mini_scheme`：解释器 **31/31**、LLVM 产物 **31/31**；
- Go 单测：`ast / evaluator / fuzz / mix / tests` 全绿。

## 十、已知限制

1. **跨平台**：模块 triple 按宿主平台推导（Windows x64 / Linux x64・arm64 / macOS arm64・x64），也可用 `ZUZO_TARGET` 覆盖；`--emit-llvm` 支持任意目标（实测 `x86_64-unknown-linux-gnu` 的 IR 可被 `clang --target=... -c` 编成 ELF obj），`--emit-exe` 在跨平台时明确拒绝（本机缺目标 CRT/链接器）。
2. **运行时是 C**：语言语义的增改要同步改 `zuzo_rt.c`（值模型/内置库/GC/错误捕获）。
3. **依赖 clang**：`--emit-exe` 需要本机 LLVM；只想要 IR 的话 `--emit-llvm` 不依赖 clang。
4. **优化等级**：当前固定 `-O2`（IR 与运行时同等级），未做 PGO/LTO。
5. **类型检查假阳**：推断器的已知 4 类假阳（见 `diagnostics.md`）在 `--strict` 下仍会阻断生成。
