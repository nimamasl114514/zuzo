# zuzo 运行时内存回收（两代分代 mark-sweep GC）

> 第 27 轮实现整堆 mark-sweep（2026-10-31），第 31 轮（2026-11-01）随后端换代迁到 LLVM 后端，
> 第 35 轮（2026-11-07）升级为**两代分代**：年轻代高频回收、老年代低频回收、存活对象按年龄晋升。
> 目标：让 AOT 产物（LLVM 后端）的堆对象不再「只分配不释放」，长驻程序内存有界，同时保证解释器与 LLVM 产物语义零漂移。
>
> 一句话：**只在安全点回收不可达对象；根集由生成代码显式给出；按代龄分两代回收，不做保守栈扫描。**

## 一、为什么不是引用计数

| 方案 | 代价 | 结论 |
|---|---|---|
| 引用计数（赋值 inc / 语句末 dec） | 要改每一条语句生成 + 每个运行时 API 的入参/返回值权属；C 里 double-free / use-after-free 风险高，逐字节对拍很难全绿 | 未采用 |
| **精确根集 mark-sweep + 两代分代（当前）** | 值语义零改动，只在语句边界埋检查点；容器/闭包天然被覆盖；分代把「短命对象」的回收成本压到只扫年轻代 | 采用 |
| 保守栈扫描 | Windows x64 需 TEB 栈界 + 指针有效性验证，误标会导致野指针读取 | 未采用（作为兜底备选） |

## 二、对象模型

所有堆对象首位嵌入 `ZObj`（32 字节）：

```c
struct ZObj {
    struct ZObj* next; /* 同代对象链：年轻代 / 老年代各自一条 */
    uint32_t     kind; /* ZOK_STR / ZOK_ARR / ZOK_HASH / ZOK_FUNC / ZOK_BOX / ZOK_ENV */
    uint32_t     mark; /* 标记 epoch：等于当前 GC epoch 即本轮可达 */
    size_t       size; /* 申请字节数 */
    uint32_t     gen;  /* 代：0 = 年轻代，1 = 老年代 */
    uint32_t     age;  /* 历经的年轻代回收次数（达到阈值即晋升） */
};
```

- 携带 `ZObj` 的对象：`ZStr` / `ZArr` / `ZHash` / `ZFunc` / `ZBox` / env。
- 分配统一走 `z_obj_new(kind, size)`：`malloc` + 清零 + 头插**年轻代**链 + 字节统计。
- 附属缓冲（`ZStr.data` / `ZArr.items` / `ZHash.entries` / `ZEnvHead.box_off`）由 sweep 随宿主一起释放。
- LLVM 侧的 `%ZObj` 类型声明必须与上表逐字段一致（`llvmbackend/gen.go`），`%ZBox` 的 GEP 依赖该布局。

## 三、分代设计

| 项 | 决策 | 理由 |
|---|---|---|
| 代数 | 2 代：年轻代（gen=0）/ 老年代（gen=1），两条独立对象链 | 一代回收成本正比于堆大小；两代即可把 90% 的短命对象关进小堆 |
| 晋升 | 历经 `Z_GC_PROMOTE_AGE = 2` 次 minor 仍存活 → 整对象移入老年代 | 两次回收还活着，基本是长寿命对象，再扫就是浪费 |
| minor | 只清扫年轻代；**老年代整体当隐式根** | 不做写屏障也能保证不漏标：老年代引用的一切都被视为可达，代价是标记阶段多扫一遍老年代 |
| major | 整堆标记 + 双链清扫；年轻代存活者直接晋升 | 回收老年代垃圾的唯一时机 |
| 标记 | **epoch 计数**：`o->mark == z_gc_epoch` 即可达，每轮起始递增 epoch | 免去每轮遍历全堆清零 mark 位 |
| 阈值 | young 初始 1 MB；old 初始 8 MB；回收后 `max(floor, 2 × 该代存活)` | 小堆不频繁回收，大堆不回收过稀 |
| 触发顺序 | 先判 young，够就 minor；再判 old，够就 major | 一次安全点内可能连续两级 |

**为什么分代而不做增量/并发**：zuzo 是 AOT 产物 + 精确根集，根只在语句边界有效。分代在不动根集契约、不改生成代码的前提下拿到「回收成本 ∝ 短命对象量」的收益；增量/并发需要读屏障与三色不变式，代价远大于收益。

## 四、根集（谁能保住对象）

GC 的根由**生成代码在安全点显式传入**，共四类：

1. 当前各层作用域里的 `ZVal` 变量地址（`&zv_xxx`，内层作用域优先）；
2. 当前作用域里的 `ZBox*` 变量（被闭包捕获的共享可变变量）；
3. 本帧 env payload（`zope` 的闭包环境，GC 用 `ZEnvHead` 回退取头，再按偏移表扫出所有 `ZBox*`）；
4. 运行时的错误传递值 `z_error_value`（`执行/遇到` 语义下跨语句存活）。

**minor 的隐式根**：老年代链表整体视为根——从老年代对象出发可达的年轻代对象一律保留。因此「老对象引用新对象」这种最常见的老带新场景无需写屏障。

env 的偏移表在 `z_new_env_ex(nbox, offs, size)` 时**运行期拷贝**一份（`ZEnvHead.box_off`），不写全局静态描述表——LLVM 后端按捕获数现场生成偏移数组，运行期拷贝一份更稳妥。env 字段全是 `ZBox*`（8 字节），偏移 = 序号 × 8。

## 五、安全点与触发

```c
void z_gc_safepoint(int nvals, ZVal** vals, int nboxes, ZBox** boxes, void* env);
```

- **埋点位置**：生成代码的每一条语句之后（`genBlockStmts`、`genFunction` 体、`main` 顶层、`for`/`for-in`/`for-in-range` 循环体）。等价于「语句边界 + 循环回边」。
- **触发条件**：`young_bytes >= young_threshold` → minor；`old_bytes >= old_threshold` → major；否则只做两次比较，开销可忽略。
- **绝不在任意分配点回收**：表达式求值中途的临时 `ZVal`（如 `(ZVal[]){...}` 复合字面量）不在根集里，只有语句结束后才安全，所以触发点必须落在语句边界。
- **可观测性**：`ZUZO_NO_SAFEPOINT=1` 可在生成期关掉全部安全点（仅用于归因实验）。

## 六、开关与观测

| 手段 | 用法 | 说明 |
|---|---|---|
| 关闭回收 | 环境变量 `ZUZO_GC=off`（或 `z_gc_set_enabled(0)`） | 行为回到「只分配不释放」，用于归因与对照实验 |
| 立即回收 | `z_gc_collect_now()` | 无根集强制清空两代，仅供测试夹具 |
| 调阈值 | `z_gc_set_threshold(bytes)` | 设年轻代预算；0 = 复位到 1 MB |
| 年轻代阈值 | 环境变量 `ZUZO_GC_YOUNG=<字节>` | 同时抬高低水位（major 自适应以它为下限） |
| 老年代阈值 | 环境变量 `ZUZO_GC_OLD=<字节>` | 同上 |
| 回收追踪 | 环境变量 `ZUZO_GC_TRACE=1` | 每次 minor/major 向 stderr 打一行：`[gc] minor #n freed= promoted= young= old= objects=` |
| 统计 | `z_gc_stats(&live, &total, &collections, &objects)` | 存活字节 / 累计分配 / 回收次数 / 对象数 |
| 分代统计 | `z_gc_gen_stats(&young, &old, &ythr, &othr, &minor, &major, &promoted)` | 两代字节、两代阈值、minor/major 次数、累计晋升字节 |

判「GC 是否生效」的标准做法：同一 exe 跑两遍，`ZUZO_GC=off` 与默认，比较工作集峰值——两者差异即回收效果。

## 七、实测（Windows x64 · 本机）

### 7.1 分代（第 35 轮，`tests_llvm/t35_gen_gc.hy`）

用例：① 400 元素长存数组；② 20000 次短命 `[i, i+1, "abc"+"def"]` 洪泛；③ 4000 次覆盖同一变量的字符串拼接（制造老年代垃圾）。

默认阈值（young 1 MB / old 8 MB）：

```
[gc] minor #1 freed=1029296 promoted=0     young=19360 old=0     objects=403
[gc] minor #2 freed=1029400 promoted=19256 young=104   old=19256 objects=403
[gc] minor #3 freed=1048600 promoted=0     young=104   old=19256 objects=403
输出：400 199990000 3000
```

- 三次 minor 各回收约 1 MB 短命对象；第二次把 19,256 B 的长存数组晋升到老年代；`old` 非零 = 晋升真实发生。

小阈值（`ZUZO_GC_YOUNG=65536 ZUZO_GC_OLD=16384`）：

```
[gc] minor #1 freed=46296 promoted=0     young=19360 old=0     objects=403
[gc] minor #2 freed=46200 promoted=19256 young=104   old=19256 objects=403
[gc] major #1 freed=0     promoted=104    young=0 old=19360 objects=403
[gc] major #2 freed=104   promoted=16656  young=0 old=70248 objects=1463
[gc] major #3 freed=144056 promoted=9416  young=0 old=28776 objects=599
[gc] major #4 freed=0     promoted=16992  young=0 old=82248 objects=1713
[gc] major #5 freed=144056 promoted=16368 young=0 old=40776 objects=849
...
输出：400 199990000 3000
```

- major 交替 `freed=0` / `freed=144056`：前半段老年代只有长存对象（全部可达，回收 0）；后半段开始回收到 ③ 段晋升后又被覆盖的旧字符串（每次约 144 KB）。
- 三种执行方式输出逐字节一致：解释器 / AOT 默认 / `ZUZO_GC=off` 均为 `400 199990000 3000`——**分代不改语义**。

### 7.2 整堆基线（第 27 轮，对照用）

场景：10 万次 `文本.重复("ab",100) + "x"`（约 200 B/次）+ 每千次一次容器写。

| 配置 | 峰值工作集 | 结束工作集 |
|---|---|---|
| native 后端 · 无 GC（基线，改前） | 62,204 KB | 54,616 KB |
| native 后端 · GC 开 | 21,628 KB | 15,636 KB |
| native 后端 · `ZUZO_GC=off` | 68,692 KB | 54,472 KB |
| 解释器（Go GC） | 17,084 KB | 8,704 KB |

N 曲线（GC 开）：1e4 → 10,036 KB；1e5 → 22,120 KB；1e6 → 22,396 KB。N 涨 100 倍、峰值只涨 2.23 倍 = 内存有界。

## 八、已知限制

1. **stop-the-world**：回收期间暂停执行，当前实现无增量/并发；百万级对象时单次回收耗时可观测。
2. **minor 不回收老年代**：老年代只在 major 时清理，阈值 8 MB 起步——只有老年代垃圾的程序会晚回收（不会漏回收）。
3. **晋升不可逆**：没有降代机制；瞬时长寿对象（如一次性大缓存）会永久占老年代预算。
4. **只追踪 zuzo 堆对象**：`ZBuf`（打印/序列化临时缓冲）、文件句柄、`malloc` 出来的裸指针不受管理。
5. **不做压缩/移动**：无碎片整理；长期抖动场景可能出现空洞。
6. **根集依赖生成代码**：若未来新增语句类型未埋安全点，该语句内产生的垃圾会晚回收（不会误回收）。
7. **env 偏移表假设字段全是 `ZBox*`**：若将来 env 出现非指针字段，需改回 `offsetof` 并同步 LLVM 后端的 env 布局常量（`llvmbackend/llvm.go` 的 `capIdx*8`）。

## 九、排障清单

| 现象 | 先查什么 |
|---|---|
| 产物崩溃在 GC 后 | 该对象是否真的可达？用 `ZUZO_GC=off` 复跑——不崩即根集漏了（查该变量是否在该作用域登记为 `vPlain`/`vBox`） |
| 语义差异只在对拍时出现 | 先跑 `run_tests_llvm.sh` 定位是「解释器语义」还是「LLVM 产物」问题；同一份程序再 `--emit-llvm` 看 IR 是否漏了安全点 |
| 峰值仍随 N 增长 | 开 `ZUZO_GC_TRACE=1` 看 minor/major 是否发生；检查循环体语句是否都埋了安全点；检查是否有容器常驻累积（活对象不算泄漏） |
| 老年代只涨不落 | 属预期（见限制 2）：老年代要等 major。要立即验证回收能力，用 `ZUZO_GC_OLD=16384` 压低阈值复跑 |
| 单次回收太慢 | 调大阈值（`ZUZO_GC_YOUNG` / `ZUZO_GC_OLD`），或减少大循环内的语句数 |
| minor 次数异常多 | 年轻代阈值被 `z_gc_set_threshold` 或环境变量压得过低；检查是否把 1 MB 的地板也一起降了 |

## 十、历史：已删除的内嵌 C 编译器（2026-10-31，2026-11-01 随 C 后端一并移除）

第 27 轮 GC 改造曾连带修复内嵌 C 编译器 nativecc 的两处缺陷（`.rdata` 段只读导致 IAT 写入崩溃、外部 CRT 多参调用传参不可靠）。
第 31 轮后端换代后 nativecc 与 C 源码后端一并删除，本段只作历史记录；现行排查路径见第九节（`run_tests_llvm.sh` + `--emit-llvm` 看 IR）。

排查方法论（可复用）：**编译产物崩 → 先用另一条工具链编同一程序判定是「代码生成」还是「编译器/运行时」问题 → 再「逐行前缀二分」缩小源程序 → 再用调试器抓崩溃地址与段表对照**。
