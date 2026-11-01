// t02 自动降级 + 粘性：py执行 命中 C 扩展 import numpy → 起子进程重放 → 之后 py求值 走同一份 globals
// 期望：np.arange(5).sum() = 10（python -c 现算）；模式粘住为 子进程
py执行("import numpy as np")
变量 和 = py求值("int(np.arange(5).sum())")
打印("[t02] sum=", 和, " 模式=", py模式())
