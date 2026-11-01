// python_libs.hy — 在 .hy 里直接调已安装的 Python 库（点号语法，跟 Python 一样写）
// 运行：./zuzo.exe examples/python_libs.hy
// 依赖：本机 CPython ≥3.8（自动探测，可用 ZUZO_PYTHON 指定）；缺库先 ./zuzo.exe pip install numpy pandas

打印("=== numpy ===")
导入 numpy 为 np
变量 矩阵 = np.array([[1, 2, 3], [4, 5, 6]])
打印("形状:", 矩阵.shape)
打印("元素和:", 矩阵.sum())
打印("按行求和:", 矩阵.sum(1).tolist())
打印("转置:", 矩阵.T.tolist())
打印("范数:", np.linalg.norm(np.array([3.0, 4.0])))
打印("原生数组:", 矩阵.tolist())
打印("遍历:", 长度(矩阵), " 行")

打印("=== pandas ===")
导入 pandas 为 pd
变量 表 = pd.DataFrame({"姓名": ["甲", "乙", "丙"], "分数": [90, 80, 70]})
打印("行数×列数:", 表.shape)
打印("分数均值:", 表["分数"].mean())
表["及格"] = [真, 真, 假]                 // 下标赋值 = setitem（新增列）
打印("加列后:", 表.shape)
打印("前两行:", 表.head(2).to_dict("records"))

打印("=== json（标准库） ===")
导入 json
变量 输出文本 = json.dumps({"甲": 1, "乙": [2, 3]}, ensure_ascii: 假)
打印("序列化:", 输出文本)
打印("反序列化:", json.loads(输出文本))
