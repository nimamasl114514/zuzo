// t13 JSON / 编码 / 文件 / 时间
变量 数据 = json解析("{\"name\":\"zuzo\",\"ver\":1,\"tags\":[\"a\",\"b\"],\"ok\":true}")
打印(数据["name"], 数据["ver"], 数据["ok"])
打印(数据["tags"])
打印(json序列化({"b": 1, "a": [1, 2]}))

打印(base64编码("hello"), base64解码("aGVsbG8="))
打印(hex编码("AB"), hex解码("4142"))
打印(url编码("a b&c"), url解码("a+b%26c"))

// 落在 _out（run_tests_llvm.sh 保证存在），不依赖未跟踪的 tmp/ 草稿目录
变量 p = "tests_llvm/_out/io_test.txt"
写文件(p, "line1\n")
追加文件(p, "line2\n")
打印(文件存在(p), 读文件(p))
打印(json序列化([1, "x", 真, 空]))

打印(时间戳() > 0, 毫秒时间戳() > 0)
