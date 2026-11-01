# zuzo

中文编程语言 + 内嵌 Python 运行时 + 混合语言执行器，纯 Go 实现，单二进制交付。

> 平台：当前主要在 **Windows x64** 开发与验证（CFFI 加载路径与浏览器内置依赖 Windows API）；Linux/macOS 移植见 [开发状态](docs/dev-status.md)。

> **English summary**
> zuzo is a Chinese-syntax programming language implemented in pure Go. It ships a five-tier
> execution pipeline (tree-walking → SAP closures → native int64 loops → Smi-tagged values →
> De Bruijn flat frames), an embedded Python runtime (self-hosted parser plus a gopapy dual-track
> front end), and a `.hypy` mixed-language executor that runs zuzo and Python blocks in one file
> with by-name variable sharing.
> On a 1,000,000-iteration integer loop (same machine, median): zuzo 23.6 ms, embedded Python
> 21.1 ms, CPython 3.12 165.3 ms.

## 特性

- **中文语法**：`变量 / 函数 / 当 / 对于 / 如果 / 返回 / 打印`，支持 UTF-8 标识符
- **五层执行栈**：Tree-Walk → SAP 闭包熔合 → 原生 int64 循环 → Smi 标记值 → De Bruijn 扁平帧，逐层回退、语义恒定
- **内嵌 Python**：自研解析器 + [gopapy v2](https://github.com/tamnd/gopapy) 双轨（Python 3.14 语法），运行时不依赖 CPython
- **混合语言 `.hypy`**：单文件内 `##ZUZO` / `##PY` 分块，两个运行时按名字双向同步变量
- **内置能力**：HTTP 服务端、浏览器自动化（chromedp）、CFFI / WASM FFI（wazero）、并发原语、正则、编码、集合库
- **字符串标准库**：内建 `文本` 命名空间（110 成员：大小写 / 裁剪 / 查找替换 / 分割 / 对齐 / 判定 / 子串 / 中文友好 / UTF-8・编码・正则）+ `libs/strings` 高层封装（统计 / 表格 / 命名法 / 模板 / 千分位）
- **工具链**：`zpm` 包管理器、类型检查 `--check`、拓扑指纹分析 `--analyze`、影子模式 `--shadow`、CPU 采样 `ZUZO_CPUPROFILE`
- **LLVM 后端**：`--emit-llvm` 直接生成文本 LLVM IR、`--emit-exe` 走 clang 产出原生 exe；与解释器逐字节对拍（20/20），详见 [LLVM 后端](docs/llvm-backend.md)
- **CPython 子进程桥**：`导入 numpy` 之后**点号直调** `numpy.array([1,2,3]).sum()`（`zuzo pip install` 装包，装完即用；也支持 `导入 pandas 为 pd`）（含 numpy / pandas 这类 C 扩展），句柄引用 + 容器快照，老代码 `py求值/py执行` 命中 `.pyd` 时自动降级并粘住子进程模式；详见 [Python 子进程桥](docs/python-bridge.md)

## 快速开始

```bash
# 构建（需要 Go >= 1.26）
go build -o zuzo ./

# 交互式
./zuzo

# 执行
./zuzo examples/hello.hy              # zuzo 源码
./zuzo tests_py/t01_hello.py          # Python 源码
./zuzo examples/mixed_demo.hypy       # 混合语言
./zuzo -e '打印("你好")'               # 内联

# 装 Python 库（与 py导入 共用同一个 CPython 解释器）
./zuzo pip install numpy pandas
./zuzo pip list

# 类型检查 / 分析
./zuzo --check 文件.hy
./zuzo --analyze 文件.hy

# LLVM 后端：生成 LLVM IR / 生成 exe
./zuzo --emit-llvm 文件.hy         # 产出 文件.ll（文本 IR）
./zuzo --emit-exe 文件.hy          # 产出 文件.exe（clang 全链）
```

> 本机 gcc 不在 PATH 时用 zig 的 clang 顶替：
> `CGO_ENABLED=1 CC="/c/Dev/zig/zig cc" go build -o zuzo.exe ./`

### 语言样例

```zuzo
变量 总 = 0
变量 i = 0
当 i < 1000000 {
    总 = 总 + i
    i = i + 1
}
打印(总)
```

```python
##PY
总 = 0
i = 0
while i < 1000000:
    总 = 总 + i
    i += 1
print(总)
```

## 性能

同一台机器（i7-14650HX / Windows / Go 1.27.1），1,000,000 次整数累加，中位数：

| 场景 | 耗时 | 每次迭代 |
|---|---|---|
| zuzo SAP（原生循环） | 23.6 ms | 23.6 ns |
| zuzo 块（混合模式） | 23.7 ms | 23.7 ns |
| **内嵌 Python（原生循环）** | **21.1 ms** | **21.1 ns** |
| CPython 3.12 | 165.3 ms | 165.3 ns |
| zuzo Tree-Walk（`--no-sap`） | 254.9 ms | 254.9 ns |
| 启动（`-e` 空跑） | 18.8 ms | — |
| 跨语言调用（zuzo→py） | 1.10 µs/次 | — |

复现：`python bench/mix/bench_mix.py`；CPU 采样：`ZUZO_CPUPROFILE=/tmp/cpu.prof ./zuzo 目标.hy`。

## 目录结构

| 目录 | 说明 |
|---|---|
| `lexer/ parser/ ast/ token/` | zuzo 语言前端 |
| `infer/` | 类型推断与槽位分配 |
| `evaluator/` | 求值器 + SAP 编译（`sap*.go`）+ 内置函数 |
| `object/` | 值表示（Smi 标记、Context 扁平帧、arena） |
| `py/` | 内嵌 Python：`parser/ runtime/ object/ pyparse/`（gopapy 适配） |
| `mix/` | 混合语言执行器（切块 / 类型桥 / 调用桥 / 块级 SAP） |
| `llvmbackend/` | LLVM 后端：AST → LLVM IR → clang → exe（含 C 运行时库） |
| `zpm/ cmd/zpm/` | 包管理器 |
| `bench/` | 跨语言基准（C/Go/Java/JS/Python/Rust/zuzo） |
| `tests/ tests_py/ tests_mix/` | Go 单测 / Python 对拍用例 / 混合 golden |
| `docs/` | 语言、混合、Python、性能、CFFI、WASM 文档 |

## 测试

```bash
./run_tests.sh        # Python 语义与 CPython 逐字节对拍（33 用例）
./run_tests_mix.sh    # 混合语言 golden（15 用例）
./run_tests_llvm.sh   # LLVM 后端对拍（20 用例）
./run_tests_bridge.sh # CPython 子进程桥对拍（17 用例：numpy / pandas / requests / 错误 / 句柄）
go test ./...         # Go 单测（含原生循环、SAP、混合切块）
```

当前状态（2026-11-01 实测）：`run_tests` **33/33 通过**，且 `--shadow` 影子模式 **33/33 双轨一致**；`run_tests_mix` **15/15**；`run_tests_llvm` **20/20**·`run_tests_bridge` **17/17**；`projects/mini_scheme` 解释器与 LLVM 产物均 **31/31**；Go 单测覆盖 `ast` / `evaluator` / `fuzz` / `mix` / `tests`。

## 文档

- [语言概览](docs/language-overview.md)
- [字符串标准库](docs/string-library.md)
- [混合语言](docs/mixed-language.md)
- [Python 支持](docs/python-support.md)
- [Python 前端（gopapy 双轨）](docs/python-parser.md)
- [性能模型与观测](docs/performance.md)
- [LLVM 后端](docs/llvm-backend.md) · [运行时内存回收](docs/gc.md)
- [CFFI](docs/cffi.md) · [WASM FFI](docs/wasm-ffi.md)
- [Python 子进程桥（CPython）](docs/python-bridge.md) · [跨语言互操作](docs/interop.md)
- [开发日志](交付-累计.md)

## 许可证

[MIT](LICENSE)