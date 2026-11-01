package cbackend

import (
	"fmt"
	"strings"

	"zuzo/ast"
)

// genBuiltinCall 处理内置函数。返回 (C 表达式, 是否命中, error)。
// 语句型内置（打印/推入）会直接 emit 语句，表达式位置返回 z_null()。
func (g *Gen) genBuiltinCall(name string, n *ast.CallExpression) (string, bool, error) {
	args := n.Arguments
	if len(n.NamedArgs) > 0 {
		return "", true, fmt.Errorf("%d:%d: %s() 的具名参数在 C 后端暂不支持，请改用位置参数", n.Token.Line, n.Token.Column, name)
	}

	// 求值全部参数（保持从左到右副作用顺序）
	vals := make([]string, 0, len(args))
	for _, a := range args {
		v, err := g.genExpr(a)
		if err != nil {
			return "", true, err
		}
		vals = append(vals, v)
	}
	need := func(min, max int) error {
		if len(vals) < min || (max >= 0 && len(vals) > max) {
			return fmt.Errorf("%s() 参数数量不匹配: 得到 %d 个", name, len(vals))
		}
		return nil
	}

	switch name {
	case "打印":
		g.line("z_print(%d, (ZVal[]){%s});", len(vals), strings.Join(vals, ", "))
		return "z_null()", true, nil

	case "长度":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_len(" + vals[0] + ")", true, nil

	case "推入":
		if err := need(2, 2); err != nil {
			return "", true, err
		}
		g.line("z_array_push(%s, %s);", vals[0], vals[1])
		return "z_null()", true, nil

	case "连接":
		if err := need(1, 2); err != nil {
			return "", true, err
		}
		sep := "z_str(\"\")"
		if len(vals) == 2 {
			sep = vals[1]
		}
		return fmt.Sprintf("z_join(%s, %s)", vals[0], sep), true, nil

	case "字符串":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_to_str(" + vals[0] + ")", true, nil

	case "整数":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_to_int(" + vals[0] + ")", true, nil

	case "浮点数":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_to_float(" + vals[0] + ")", true, nil

	// 错误对象内省：C 后端的错误值在 执行/遇到 里是字符串载体，
	// 错误码/消息从运行时全局取（最近一次 z_die 写入）；错误行在 C 后端不可知，返回 0。
	case "错误码":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_str(z_error_code())", true, nil

	case "错误信息":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_str(z_error_msg())", true, nil

	case "错误行":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_int(0)", true, nil

	case "类型":
		if err := need(1, 1); err != nil {
			return "", true, err
		}
		return "z_type_of(" + vals[0] + ")", true, nil

	case "包含":
		if err := need(2, 2); err != nil {
			return "", true, err
		}
		return fmt.Sprintf("z_contains(%s, %s)", vals[0], vals[1]), true, nil

	// ── 数组 / 哈希 ──
	case "弹出":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_pop(%s)", vals[0]), true, nil
	case "首项":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_first(%s)", vals[0]), true, nil
	case "末项":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_last(%s)", vals[0]), true, nil
	case "键":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_keys(%s)", vals[0]), true, nil
	case "取值":
		if err := need(2, 3); err != nil { return "", true, err }
		if len(vals) == 3 {
			return fmt.Sprintf("z_get(%s, %s, %s, 1)", vals[0], vals[1], vals[2]), true, nil
		}
		return fmt.Sprintf("z_get(%s, %s, z_null(), 0)", vals[0], vals[1]), true, nil

	// ── 字符串（全局名）──
	case "大写":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("zs_upper(%s)", vals[0]), true, nil
	case "小写":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("zs_lower(%s)", vals[0]), true, nil
	case "前缀":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("zs_starts(%s, %s)", vals[0], vals[1]), true, nil
	case "后缀":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("zs_ends(%s, %s)", vals[0], vals[1]), true, nil
	case "查找":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("zs_find(%s, %s)", vals[0], vals[1]), true, nil
	case "替换":
		if err := need(3, 3); err != nil { return "", true, err }
		return fmt.Sprintf("zs_replace(%s, %s, %s)", vals[0], vals[1], vals[2]), true, nil
	case "分割":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("zs_split(%s, %s)", vals[0], vals[1]), true, nil
	case "去空白":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("zs_trim(%s)", vals[0]), true, nil
	case "裁剪":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_trim_chars(%s, %s)", vals[0], vals[1]), true, nil
	case "重复":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("zs_repeat(%s, %s)", vals[0], vals[1]), true, nil
	case "计数":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_count_sub(%s, %s)", vals[0], vals[1]), true, nil
	case "分词":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_fields(%s)", vals[0]), true, nil
	case "取字符":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_char_at(%s, %s)", vals[0], vals[1]), true, nil

	// ── 数学 ──
	case "绝对值":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_abs(%s)", vals[0]), true, nil
	case "最大值":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_max2(%s, %s)", vals[0], vals[1]), true, nil
	case "最小值":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_min2(%s, %s)", vals[0], vals[1]), true, nil
	case "向上取整":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_ceil(%s)", vals[0]), true, nil
	case "向下取整":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_floor(%s)", vals[0]), true, nil
	case "四舍五入":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_round(%s)", vals[0]), true, nil
	case "幂":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_pow(%s, %s)", vals[0], vals[1]), true, nil
	case "平方根":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_sqrt(%s)", vals[0]), true, nil
	case "正弦":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_sin(%s)", vals[0]), true, nil
	case "余弦":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_cos(%s)", vals[0]), true, nil
	case "对数":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_log(%s)", vals[0]), true, nil
	case "随机":
		if err := need(0, 0); err != nil { return "", true, err }
		return "z_rand()", true, nil
	case "随机整数":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_rand_int(%s, %s)", vals[0], vals[1]), true, nil

	// ── JSON ──
	case "json解析":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_json_parse(%s)", vals[0]), true, nil
	case "json序列化":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_json_stringify(%s)", vals[0]), true, nil

	// ── 文件 I/O（语句型：返回值可丢弃）──
	case "读文件":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_read_file(%s)", vals[0]), true, nil
	case "写文件":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_write_file(%s, %s)", vals[0], vals[1]), true, nil
	case "追加文件":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_append_file(%s, %s)", vals[0], vals[1]), true, nil
	case "文件存在":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_file_exists(%s)", vals[0]), true, nil
	case "创建文件":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_create_file(%s)", vals[0]), true, nil
	case "删除文件":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_delete_file(%s)", vals[0]), true, nil
	case "重命名":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_rename_file(%s, %s)", vals[0], vals[1]), true, nil

	// ── 编码 ──
	case "base64编码":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_base64_encode(%s)", vals[0]), true, nil
	case "base64解码":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_base64_decode(%s)", vals[0]), true, nil
	case "hex编码":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_hex_encode(%s)", vals[0]), true, nil
	case "hex解码":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_hex_decode(%s)", vals[0]), true, nil
	case "url编码":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_url_encode(%s)", vals[0]), true, nil
	case "url解码":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_url_decode(%s)", vals[0]), true, nil

	// ── 时间 ──
	case "时间戳":
		if err := need(0, 0); err != nil { return "", true, err }
		return "z_unix_time()", true, nil
	case "毫秒时间戳":
		if err := need(0, 0); err != nil { return "", true, err }
		return "z_unix_milli()", true, nil
	case "当前时间":
		if err := need(0, 0); err != nil { return "", true, err }
		return "z_now()", true, nil
	case "睡眠":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_sleep_ms(%s)", vals[0]), true, nil

	// ── 正则（第一个参数是文本，第二个是模式）──
	case "正则验证":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_regex_test(%s, %s)", vals[0], vals[1]), true, nil
	case "正则匹配":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_regex_match(%s, %s)", vals[0], vals[1]), true, nil
	case "正则查找全部":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_regex_find_all(%s, %s)", vals[0], vals[1]), true, nil
	case "正则替换":
		if err := need(3, 3); err != nil { return "", true, err }
		return fmt.Sprintf("z_regex_replace(%s, %s, %s)", vals[0], vals[1], vals[2]), true, nil
	case "正则分割":
		if err := need(2, 2); err != nil { return "", true, err }
		return fmt.Sprintf("z_regex_split(%s, %s)", vals[0], vals[1]), true, nil
	case "正则提取":
		if err := need(2, 3); err != nil { return "", true, err }
		return fmt.Sprintf("z_regex_extract(%s, %s)", vals[0], vals[1]), true, nil

	// ── 格式化 / 目录 / 环境 ──
	case "格式化":
		if len(vals) < 1 { return "", true, fmt.Errorf("格式化() 至少需要 1 个参数") }
		return fmt.Sprintf("z_format(%d, (ZVal[]){%s})", len(vals), strings.Join(vals, ", ")), true, nil
	case "读目录":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_read_dir(%s)", vals[0]), true, nil
	case "环境变量":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_getenv(%s)", vals[0]), true, nil

	// ── 摘要 ──
	case "md5":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_md5(%s)", vals[0]), true, nil
	case "sha1":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_sha1(%s)", vals[0]), true, nil
	case "sha256":
		if err := need(1, 1); err != nil { return "", true, err }
		return fmt.Sprintf("z_sha256(%s)", vals[0]), true, nil
	}
	return "", false, nil
}

// textBuiltins 文本命名空间 → C 运行时函数映射
var textBuiltins = map[string]struct {
	Fn  string
	Min int
	Max int
}{
	"大写":    {"zs_upper", 1, 1},
	"小写":    {"zs_lower", 1, 1},
	"去空白":   {"zs_trim", 1, 1},
	"替换":    {"zs_replace", 3, 3},
	"分割":    {"zs_split", 2, 2},
	"重复":    {"zs_repeat", 2, 2},
	"查找":    {"zs_find", 2, 2},
	"包含":    {"zs_contains", 2, 2},
	"前缀":    {"zs_starts", 2, 2},
	"后缀":    {"zs_ends", 2, 2},
	"比较":    {"zs_compare", 2, 2},
	"子串":    {"zs_substr", 3, 3},
	"按行分割":  {"zs_lines", 1, 1},
	"连接":    {"zs_join_arr", 2, 2},
	"首字母大写": {"zs_capitalize", 1, 1},
	"标题化":   {"zs_title", 1, 1},
	"大小写互换": {"zs_swapcase", 1, 1},
	"去左空白":  {"zs_lstrip", 1, 1},
	"去右空白":  {"zs_rstrip", 1, 1},
	"去前缀":   {"zs_remove_prefix", 2, 2},
	"去后缀":   {"zs_remove_suffix", 2, 2},
	"替换次":   {"zs_replace_n", 4, 4},
	"分割次":   {"zs_split_n", 3, 3},
	"左对齐":   {"zs_ljust", 3, 3},
	"右对齐":   {"zs_rjust", 3, 3},
	"居中":    {"zs_center", 3, 3},
}

// genTextBuiltin 处理 `文本.成员(...)` 形式的调用
func (g *Gen) genTextBuiltin(name string, n *ast.CallExpression) (string, bool, error) {
	spec, ok := textBuiltins[name]
	if !ok {
		return "", false, nil
	}
	if len(n.Arguments) < spec.Min || len(n.Arguments) > spec.Max {
		return "", true, fmt.Errorf("文本.%s() 参数数量不匹配: 得到 %d 个", name, len(n.Arguments))
	}
	var vals []string
	for _, a := range n.Arguments {
		v, err := g.genExpr(a)
		if err != nil {
			return "", true, err
		}
		vals = append(vals, v)
	}
	return fmt.Sprintf("%s(%s)", spec.Fn, strings.Join(vals, ", ")), true, nil
}

// unsupportedBuiltins C 后端明确不支持的运行时（依赖 Go 运行时/系统调用）
var unsupportedBuiltins = map[string]string{
	"并发":    "goroutine 并发",
	"通道":    "通道",
	"发送":    "通道",
	"接收":    "通道",
	"关闭":    "通道",
	"等待全部":  "goroutine 并发",
	"尝试接收":  "通道",
	"尝试执行":  "错误捕获执行",
	"运行参数":  "进程参数",
	"http获取": "网络请求", "http提交": "网络请求", "http更新": "网络请求",
	"http补丁": "网络请求", "http删除": "网络请求", "http请求": "网络请求",
	"http服务": "HTTP 服务端", "http服务后台": "HTTP 服务端",
	"http流请求": "HTTP 服务端", "http反代": "HTTP 服务端",
	"打开浏览器": "浏览器自动化", "关闭浏览器": "浏览器自动化",
	"打开页面": "浏览器自动化", "关闭页面": "浏览器自动化",
	"导航": "浏览器自动化", "当前网址": "浏览器自动化", "页面标题": "浏览器自动化",
	"等待元素": "浏览器自动化", "等待可见": "浏览器自动化",
	"点击": "浏览器自动化", "点击按钮": "浏览器自动化",
	"填写": "浏览器自动化", "清空输入": "浏览器自动化",
	"获取文本": "浏览器自动化", "获取属性": "浏览器自动化",
	"获取HTML": "浏览器自动化", "获取全部文本": "浏览器自动化",
	"元素存在": "浏览器自动化", "元素数量": "浏览器自动化",
	"执行脚本": "浏览器自动化", "等待跳转": "浏览器自动化",
	"截图": "浏览器自动化", "设置视口": "浏览器自动化", "等待": "浏览器自动化",
	"加载WASM": "WASM FFI", "WASM调用": "WASM FFI", "WASM导出": "WASM FFI",
	"WASM函数": "WASM FFI", "WASM内存读取": "WASM FFI", "WASM内存写入": "WASM FFI",
	"WASM内存大小": "WASM FFI", "WASM关闭": "WASM FFI",
	"加载C库": "C 动态库 FFI", "C函数声明": "C 动态库 FFI", "C调用": "C 动态库 FFI",
	"计时": "高精度计时", "计时秒": "高精度计时", "时间差": "时间运算",
	"格式化时间": "时间格式化", "解析时间": "时间解析",
	"读行": "标准输入",
}

// checkUnsupported 若调用了 C 后端明确不支持的运行时，给出可读错误
func (g *Gen) checkUnsupported(name string, line, col int) error {
	if reason, ok := unsupportedBuiltins[name]; ok {
		return fmt.Errorf("%d:%d: %s() 依赖%s，C 后端不支持（请用解释器执行，或改用替代实现）", line, col, name, reason)
	}
	return nil
}
