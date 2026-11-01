// t06 requests：取版本号、嵌套属性取异常类名、py有库 探测（不实际导入）
// 期望（python -c 现算）：2.32.3 / RequestException / 真 / 假
变量 requests = py导入("requests")
变量 版本 = py取(requests, "__version__")
变量 异常类 = py取(py取(requests, "exceptions"), "RequestException")
变量 类名 = py取(异常类, "__name__")
打印("[t06] 版本=", 版本, " 异常=", 类名, " 查requests=", py有库("requests"), " 查zzz=", py有库("zzz_nonexist_pkg"))
