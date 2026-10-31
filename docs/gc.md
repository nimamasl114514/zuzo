# zuzo 运行时内存回收（mark-sweep GC）

> 第 27 轮实现（2026-10-31）。目标：让 AOT 产物（C 后端 / nativecc 后端）的堆对象不再「只分配不释放」，长驻程序内存有界，同时保证解释器、GCC 后端、nativecc 后端三条线语义零漂移。
>
> 一句话：**只在安全点回收不可达对象；根集由生成代码显式给出，不做保守栈扫描。**

## 一、为什么不是引用计数

| 方案 | 代价 | 结论 |
|---|---|---|
| 引用计数（赋值 inc / 语句末 dec） | 要改 cbackend 每一条语句生成 + 每个运行时 API 的入参/返回值权属；C 里 double-free / use-after-free 风险高，17 例字节对拍很难全绿 | 未采用 |
| **精确根集 mark-sweep（当前）** | 值语义零改动，只在语句边界埋检查点；容器/闭包天然被覆盖 | 采用 |
| 保守栈扫描 | Windows x64 需 TEB 栈界 + 指针有效性验证，误标会导致野指针读取 | 未采用（作为兜底备选） |

## 二、对象模型

所有堆对象首位嵌入 `ZObj`：

```c
struct ZObj {
    ZObj*    next;   /* 全局对象链，sweep 线性遍历 */
    uint32_t kind;   /* ZOK_STR / ZOK_ARR / ZOK_HASH / ZOK_FUNC / ZOK_BOX / ZOK_ENV */
    uint32_t mark;   /* 标记位 */
    size_t   size;   /* 申请字节数 */
};
```

- 携带 `ZObj` 的对象：`ZStr` / `ZArr` / `ZHash` / `ZFunc` / `ZBox` / env。
- 分配统一走 `z_obj_new(kind, size)`：`malloc` + 挂链 + 字节统计；`malloc` 后 `memset` 清零，避免未初始化字段被误扫。
- 附属缓冲（`ZStr.data` / `ZArr.items` / `ZHash.entries` / `ZEnvHead.box_off`）由 sweep 随宿主一起释放。

## 三、根集（谁能保住对象）

GC 的根由**生成代码在安全点显式传入**，共四类：

1. 当前各层作用域里的 `ZVal` 变量地址（`&zv_xxx`，内层作用域优先）；
2. 当前作用域里的 `ZBox*` 变量（被闭包捕获的共享可变变量）；
3. 本帧 env payload（`zope` 的闭包环境，GC 用 `ZEnvHead` 回退取头，再按偏移表扫出所有 `ZBox*`）；
4. 运行时的错误传递值 `z_error_value`（`执行/遇到` 语义下跨语句存活）。

env 的偏移表在 `z_new_env_ex(nbox, offs, size)` 时**运行期拷贝**一份（`ZEnvHead.box_off`），不写全局静态描述表——因为内嵌 nativecc 的全局初始化器不支持 `sizeof` / 数组地址常量这类非常量表达式。env 字段全是 `ZBox*`（8 字节），偏移 = 序号 × 8。

## 四、安全点与触发

```c
void z_gc_safepoint(int nvals, ZVal** vals, int nboxes, ZBox** boxes, void* env);
```

- **埋点位置**：生成代码的每一条语句之后（`genBlockStmts`、`genFunction` 体、`main` 顶层、`for`/`for-in`/`for-in-range` 循环体）。等价于「语句边界 + 循环回边」。
- **触发条件**：`z_gc_live_bytes >= z_gc_threshold` 才真正回收；否则只做一次比较，开销可忽略。
- **阈值策略**：初始 4 MB；每次回收后取 `max(4MB, 2 × 存活字节)`——避免小堆频繁 GC，也避免大堆回收过稀。
- **绝不在任意分配点回收**：表达式求值中途的临时 `ZVal`（如 `(ZVal[]){...}` 复合字面量）不在根集里，只有语句结束后才安全，所以触发点必须落在语句边界。

## 五、开关与观测

| 手段 | 用法 | 说明 |
|---|---|---|
| 关闭回收 | 环境变量 `ZUZO_GC=off`（或 `z_gc_set_enabled(0)`） | 行为回到「只分配不释放」，用于归因与对照实验 |
| 立即回收 | `z_gc_collect_now()` | 无根集强制回收，仅供测试夹具 |
| 调阈值 | `z_gc_set_threshold(bytes)` | 0 = 复位到 4 MB |
| 统计 | `z_gc_stats(&live, &total, &collections, &objects)` | 存活字节 / 累计分配 / 回收次数 / 对象数 |

判「GC 是否生效」的标准做法：同一 exe 跑两遍，`ZUZO_GC=off` 与默认，比较工作集峰值——两者差异即回收效果。

## 六、实测（Windows x64 · 本机）

场景：10 万次 `文本.重复("ab",100) + "x"`（约 200 B/次）+ 每千次一次容器写。

| 配置 | 峰值工作集 | 结束工作集 |
|---|---|---|
| native 后端 · 无 GC（基线，第 27 轮改前） | 62,204 KB | 54,616 KB |
| native 后端 · GC 开 | 21,628 KB | 15,636 KB |
| native 后端 · `ZUZO_GC=off` | 68,692 KB | 54,472 KB |
| 解释器（Go GC） | 17,084 KB | 8,704 KB |

N 曲线（GC 开，native 后端）：

| N | 峰值 | 结束 |
|---|---|---|
| 1e4 | 10,036 KB | 10,036 KB |
| 1e5 | 22,120 KB | 16,268 KB |
| 1e6 | 22,396 KB | 5,556 KB |

N 涨 100 倍、峰值只涨 2.23 倍且 1e5 → 1e6 几乎持平 = 内存有界。

## 七、已知限制

1. **stop-the-world**：回收期间暂停执行，当前实现无增量/并发；百万级对象时单次回收耗时可观测（1e6 场景整体 2.9s）。
2. **只追踪 zuzo 堆对象**：`ZBuf`（打印/序列化临时缓冲）、文件句柄、`malloc` 出来的裸指针不受管理。
3. **不做压缩/移动**：无碎片整理；长期抖动场景可能出现空洞。
4. **根集依赖生成代码**：若未来新增语句类型未埋安全点，该语句内产生的垃圾会晚回收（不会误回收，只会晚回收）。
5. **env 偏移表假设字段全是 `ZBox*`**：若将来 env 出现非指针字段，需改回 `offsetof` 并同步 nativecc 的常量初始化能力。

## 八、排障清单

| 现象 | 先查什么 |
|---|---|
| 产物崩溃在 GC 后 | 该对象是否真的可达？用 `ZUZO_GC=off` 复跑——不崩即根集漏了（查该变量是否在该作用域登记为 `vPlain`/`vBox`） |
| 语义差异只在对拍时出现 | 先跑 `run_tests_c.sh`（GCC 后端）与 `run_tests_native.sh`（nativecc）分开定位，前者挂说明 C 语义问题，后者挂说明 nativecc 编译问题 |
| 峰值仍随 N 增长 | 检查该循环体语句是否都埋了安全点；检查是否有容器常驻累积（活对象不算泄漏） |
| 单次回收太慢 | 调大阈值 `z_gc_set_threshold`，或减少大循环内的语句数 |

## 九、本轮连带修复的 nativecc 两处缺陷（2026-10-31）

GC 改造让运行时多了字符串常量与新函数，暴露出内嵌编译器 nativecc 的两个既有缺陷，均已修并加了守门测试：

| # | 现象 | 定位证据 | 根因 | 修法 |
|---|---|---|---|---|
| 1 | `examples/sort.hy` 的 native 产物在 `0` 输出时崩溃（rc=0xC0000005），GC 关闭也崩，同一份 C 用 gcc 编正常 | gdb：崩在 `ntdll!LdrProcessInitializationComplete`（进程入口都没执行），写地址 `RDX=0x140030FFA` 落在 `.rdata` 内偏移 `0xFFA`；PE 段表里 `.rdata` Characteristics=`0x40000040`（只读） | `.rdata` 同时承载导入表 IAT，加载器要写 IAT；IAT 被字符串常量挤到 `0x1000` 边界附近后，写只读页即访问违例。字符串常量总数 > 20 时必崩（20 个正常 / 21 个崩） | `nativecc/pe.go` 把 `.rdata` 段属性改成 `0xC0000040`（READ\|WRITE\|INITIALIZED_DATA）；新增 `nativecc/iat_rodata_test.go`（段属性守门 + 30 字符串运行） |
| 2 | `memset(o, 0, size)` 在 native 产物里写越界（越界落点正好是 `.rdata`） | 把 `memset` 换成逐字节循环后，同一程序由崩转为正常 | nativecc 对**外部 CRT 多参调用**（`memset`/`memcpy` 这类三参）的传参不可靠——第26轮修的是「自家函数调用的栈参数装载」，外部函数未覆盖 | `zuzo_rt.c` 内 `memset` / `memcpy` 改为显式循环（注释注明原因）；nativecc 侧修复列入遗留 |

排查方法论（可复用）：**编译产物崩 → 先用 gcc 编同一份 C 判定是「代码生成」还是「编译器」问题 → 再走「逐行前缀二分」缩小源程序 → 再用 gdb 抓崩溃地址与段表对照**。本次三步各一次即锁定 `.rdata` 只读。
