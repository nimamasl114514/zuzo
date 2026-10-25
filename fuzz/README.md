# zuzo 模糊测试（fuzz）

两层模糊测试，互补：

| 层 | 文件 | 打什么 | 断言 | 典型时长 |
|---|---|---|---|---|
| Go 原生 fuzz | `fuzz/fuzz_test.go` | 词法器 / Python 语法器 / 原生语言语法器 / 运行时 | 不 panic、不挂死（3s）、token 不爆炸 | 30–60s / 目标 |
| 语义差分 fuzz | `fuzz/difffuzz.py` | 语法合法的确定性 Python 程序 | zuzo 的 stdout+退出码 == CPython | 40 例 ≈ 40s |

## 一、Go 原生 fuzz

```bash
cd zuzo
go test ./fuzz -run=^$ -fuzz=FuzzPyLexer  -fuzztime=30s
go test ./fuzz -run=^$ -fuzz=FuzzPyParser -fuzztime=30s
go test ./fuzz -run=^$ -fuzz=FuzzPyExec   -fuzztime=30s
go test ./fuzz -run=^$ -fuzz=FuzzZuzoLexer -fuzztime=30s
go test ./fuzz -run=^$ -fuzz=FuzzZuzoExec  -fuzztime=30s
go test ./fuzz            # CI 快检：只跑种子语料
```

- 失败用例自动落 `fuzz/testdata/fuzz/<目标名>/`，之后 `go test ./fuzz` 会永久回归它。
- `FuzzPyExec` / `FuzzZuzoExec` 在 goroutine 里 recover + 3 秒 select：panic 与死循环都算失败。
- 种子在 `pySeeds` / `zzSeeds`（f-string、缩进、async、异常、推导式、中文关键字）。

## 二、语义差分 fuzz

```bash
python fuzz/difffuzz.py --n 200 --seed 1 --jobs 4
python fuzz/difffuzz.py --n 100 --seed 7 --only fmt,str,list
python fuzz/difffuzz.py --n 1 --seed 12        # 单例复现
```

- 15 个模板：整数/大整数/浮点格式化/字符串方法/列表/字典/类继承/生成器/异常/闭包装饰器/切片/推导式/高阶函数/match/解包。
- 只生成**确定性纯计算**程序（无 random/time/IO），保证差分结果可复现。
- 产出：
  - `fuzz/reports/diff-<ts>.md` —— 汇总（按模板统计 + 每个不一致的首个差异行 + 源码）
  - `fuzz/reports/cases/dNNN_<tpl>.py` —— 失败源码，可直接 `./zuzo.exe <file>` 复现
  - `.cpy.txt` / `.zz.txt` —— 双方输出
- 退出码：0 = 无不一致；1 = 有差异（可直接接 CI）。

## 三、结果判定口径

- **panic / 挂死** = 最高优先级（语言实现正确性底线）。
- **退出码不一致**：先看是不是 zuzo 把「用户异常」当成实现崩溃（traceback 属于正常输出的一部分）。
- **输出不一致**：按首个差异行定位到具体模板 → 写最小复现 → 修运行时 → 重跑该 seed 确认。
- 已确认「zuzo 尚未实现」的能力（如 asyncio 高级原语）不计为回归，记录到报告「已知缺口」段。

## 四、与回归套件的关系

| 套件 | 覆盖面 | 命令 |
|---|---|---|
| `run_tests.sh` | 34 个手写对拍用例 | `./run_tests.sh` |
| `run_tests_mix.sh` | 15 个混合语言 golden | `./run_tests_mix.sh` |
| `go test ./...` | 包级单测 | `go test ./...` |
| **fuzz（本目录）** | 随机输入的不变量 + 语义差分 | 见上 |

fuzz 发现的问题修复后，应把最小复现**升级成 `tests_py/tNN_*.py` 固定用例**，再进 `run_tests.sh`。
