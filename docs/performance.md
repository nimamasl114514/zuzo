# zuzo 性能模型与观测通道

> 2026-10-24 第6轮性能优化后更新。所有数字为本机实测（i7-14650HX / Windows / Go 1.27.1）。

## 一、执行层的五个档位

zuzo 的 zuzo 语言侧有四级加速，逐级在「匹配失败时」回退：

| 档 | 名称 | 触发条件 | 每轮迭代成本（1e6 累加实测） |
|---|---|---|---|
| L0 | Tree-Walk | `--no-sap` 或 SAP 编译失败 | 231 ms |
| L1-2 | Fused Loop（闭包熔合） | 循环可编译，但 body 不满足原生条件 | ~117 ms（优化前 while 的水平） |
| L3 | Native Int64 Loop | body 全是 `a = b ± c` 类整数运算 | — |
| L4 | Smi-Tagged Native | 同上，值走 64 位标记槽位 | — |
| L5 | De Bruijn Flat Frame | 同上，值走扁平帧数组 | **22 ms** |

**for 与 while 现在都具备 L4/L5**（第6轮补齐 while）：

```zuzo
变量 总 = 0
变量 i = 0
当 i < 1000000 {      // 条件：i 与整数字面量比较
    总 = 总 + i        // body：简单整数赋值
    i = i + 1          // 最后一条：i = i ± 常量
}
```

匹配规则（不满足即回退，语义恒定）：
- 条件必须是 `标识符 < <= > >= 整数字面量`
- body 最后一条必须是 `i = i ± 常量`
- body 其余语句必须能编译成整数 op（`a = b`、`a = 常量`、`a = b ± c`、`a = b * 常量`）
- body 中出现 `跳出`/`继续`/函数调用/字符串/浮点/索引赋值 → 不匹配

## 二、混合语言（.hypy）的块级 SAP

混合执行器默认共享一个 Tree-Walk Environment（跨块按名同步）。第6轮起，单个 zuzo 块在满足条件时启用**块级 SAP**：

- 条件：块内存在 `for`/`while`，且**没有函数字面量**（函数会捕获 Environment 指针，临时槽位环境会改变闭包可见性）
- 做法：每块独立 `infer + SAPCompiler + NewSAPEnvironment`；块前按 `refs` 把需要的名字从全局环境拉进槽位，块后按 `defs` 把顶层定义写回全局
- 效果：混合 zuzo 块 1e6 累加 253 ms → 23.6 ms

不满足条件的块仍走 Tree-Walk，行为不变。

## 三、Python 运行时

- 数字字面量按 AST 节点缓存（`sync.Map`），循环条件不再每轮 `ParseInt`
- `opAdd/opSub/opMul` 增加 int+int 快路径
- threading 用 GIL 互斥模拟 CPython 语义（主执行流与线程体串行，`join` 期间释放锁）

实测 1e6 累加：164 ms（CPython 3.12 同机 142 ms）。


## 三·五、Python 侧原生循环（第7轮）

Python 的整数计数循环现在与 zuzo 走同一套思路（`py/runtime/native_loop.go`）：

```python
i = 0
total = 0
while i < 1000000:   # 条件：Name OP 整数字面量，无 else
    total += i       # body：整数赋值/增强赋值
    i = i + 1        # 最后一条：循环变量自增/自减常量
```

匹配成功后循环内全部用局部 int64 运算，退出时一次性写回 Env；不匹配（浮点、函数调用、
容器、break/continue、嵌套循环、global/nonlocal、变量边界、估算迭代 <1000）立即回退原解释器。

实测 1e6 累加：**164.5 ms → 21.1 ms（7.8×）**，与 zuzo SAP 的 23.6 ms 同量级，
约为同机 CPython 3.12（165.3 ms）的 1/7.8。

语义覆盖见 `tests_py/t32_native_loops.py`（12 种循环形态，与 CPython 逐字节对拍）。

## 四、观测通道

```bash
# CPU profile（覆盖解析 + 推断 + SAP 编译 + 执行）
ZUZO_CPUPROFILE=/abs/path/cpu.prof ./zuzo.exe 目标文件.hy
go tool pprof -top  /abs/path/cpu.prof
go tool pprof -list '函数名' /abs/path/cpu.prof
```

注意：采样率 100 Hz，目标程序需运行 ≥200 ms 才有足够样本（小脚本可用循环放大规模）。

## 五、基准

```bash
python bench/mix/bench_mix.py            # 四维端到端（启动/吞吐/跨语言/同步）
python bench/mix/bench_mix.py --quick    # 10 万级快速档

CGO_ENABLED=1 CC="/c/Dev/zig/zig cc" go test ./tests -run '^$' \
  -bench 'BenchmarkParse100k|BenchmarkLex100k|BenchmarkDeBruijnLoop10K' -benchtime=2s
```

回归：

```bash
./run_tests.sh        # Python 语义对拍（21 用例，CPython 逐字节 diff）
./run_tests_mix.sh    # 混合语言 golden（15 用例）
go test ./...         # Go 单测（含 while 原生循环 10 个双路对拍用例）
```

## 六、已知边界

- 启动 ~19 ms，其中 ~14.5 ms 是 Go 进程本身（同机空程序基线），包初始化约 5 ms。
- 解析侧约 30% 采样在 GC（token 字符串分配）；进一步优化需 token 复用/arena。
- Python 变量查找仍走 `map[string]Object`；改槽位需重构 py/object 层。
- 混合模式下含函数定义的 zuzo 块不走块级 SAP。