// t04 ndarray 的方法与模块函数：sum / reshape / dtype / mean
// 期望（python -c 现算）：和=6 形状=[[1], [2], [3]] 类型名=int64 均值=2
变量 np = py导入("numpy")
变量 a = py方法(np, "array", [[1, 2, 3]])
打印("[t04] 和=", py方法(a, "sum"), " 形状=", py方法(py方法(a, "reshape", [3, 1]), "tolist"))
打印("[t04] 类型名=", py取(py取(a, "dtype"), "name"), " 均值=", py方法(a, "mean"))
