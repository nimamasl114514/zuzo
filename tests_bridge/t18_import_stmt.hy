// t18 「导入 numpy」语句 + 长度()/for-in 对 Python 对象生效（最省事的入口）
// 期望：和=10 长度=3 遍历和=6 别名形状=[2, 1]
导入 numpy
导入 pandas 为 pd
导入 numpy.linalg 为 la
变量 a = numpy.array([1, 2, 3])
打印("[t18] 和=", a.sum(), " 长度=", 长度(a))
变量 总 = 0
对于 x 在 a { 总 = 总 + x }
打印("[t18] 遍历和=", 总, " 别名形状=", pd.DataFrame({"a": [1, 2]}).shape, " 范数=", la.norm(numpy.array([3.0, 4.0])))
