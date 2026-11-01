# tests_bridge — CPython 子进程桥端到端用例

判据：`./run_tests_bridge.sh` → `BRIDGE PASS=13 FAIL=0`。

每个 `tNN_*.hy` 的 stdout 必须与同名 `.expected` **逐字节一致**，且退出码 0。
`.expected` 不是「跑一遍抄输出」，每个数值都能用 CPython 现算复核：

```bash
python -c "
import numpy as np, pandas as pd
print(np.arange(100000).sum())        # t09 → 4999950000
print(np.arange(10).sum())            # t02 → 45
print(np.array([1,2,3]).tolist())     # t03 → [1, 2, 3]
a=np.array([1,2,3]); print(a.sum(), a.reshape(3,1).tolist(), a.dtype.name, a.mean())   # t04 → 6 [[1], [2], [3]] int64 2.0
df=pd.DataFrame({'a':[1,2,3],'b':[10,20,30]}); print(df.shape, df['a'].sum(), df.sum().sum())  # t05 → (3, 2) 6 66
print(2**70, True, None, {'甲':1,'乙':2}, '你好'*2)   # t08 → 1180591620717411303424 True None {...}
"
```

用例分工：

| 用例 | 覆盖点 |
|---|---|
| t01 | 纯本地 `py求值` 不受桥影响 |
| t02 | `py执行 import numpy` 自动降级 + 粘性 |
| t03 | `py导入 / py方法 / py取值 / py类型` |
| t04 | ndarray 方法链（sum / reshape / dtype / mean） |
| t05 | pandas DataFrame 构造 / 取列 / 聚合 / 增列 |
| t06 | requests 属性链 + `py有库` |
| t07 | 导入不存在的模块 → 错误可捕获，桥仍可用 |
| t08 | 大整数降级字符串 / 布尔 / 空 / 字典 / 中文 / 嵌套 |
| t09 | 10 万元素 numpy 求和 |
| t10 | 200 次连续 `py求值`（常驻不重启） |
| t11 | `py模式 / py设置 / py获取 / py执行` 命名空间互通 |
| t12 | 远端 ZeroDivisionError → E903 + traceback |
| t13 | 子进程模式容器/字节转换（tuple / set / bytes→base64） |
