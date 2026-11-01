// python_libs.hy — 在 .hy 里直接调已安装的 Python 库（numpy / pandas / json）
// 运行：./zuzo.exe examples/python_libs.hy
// 依赖：本机 CPython ≥3.8（自动探测，可用 ZUZO_PYTHON 指定），numpy / pandas 已安装

打印("=== numpy ===")
变量 np = py导入("numpy")
变量 矩阵 = py方法(np, "array", [[1, 2, 3], [4, 5, 6]])
打印("形状:", py取(矩阵, "shape"))
打印("元素和:", py方法(矩阵, "sum"))
打印("按行求和:", py方法(py方法(矩阵, "sum", 1), "tolist"))
打印("转置:", py方法(py取(矩阵, "T"), "tolist"))
打印("原生数组:", py取值(矩阵))

打印("=== pandas ===")
变量 pd = py导入("pandas")
变量 表 = py调(py取(pd, "DataFrame"), {"姓名": ["甲", "乙", "丙"], "分数": [90, 80, 70]})
打印("行数×列数:", py取(表, "shape"))
打印("分数均值:", py方法(py取值(表, "分数"), "mean"))
py方法(表, "__setitem__", "及格", [真, 真, 假])
打印("加列后:", py取(表, "shape"))
打印("前两行:", py取值(py方法(表, "head", 2)))

打印("=== json（标准库） ===")
变量 json = py导入("json")
变量 输出文本 = py调参(py取(json, "dumps"), [{"甲": 1, "乙": [2, 3]}], {"ensure_ascii": 假})
打印("序列化:", 输出文本)
打印("反序列化:", py调(py取(json, "loads"), 输出文本))
