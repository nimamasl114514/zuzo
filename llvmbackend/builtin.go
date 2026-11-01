package llvmbackend

// 内置函数映射。按 cbackend/builtin.go 的语义逐条平移：
//   - 实参个数、默认值、类型转换口径与 C 后端完全一致（错误文案也保持一致结构）；
//   - ZVal 实参一律写成 "ptr <槽位>"（Win64 下 ZVal 按引用传）；
//   - 返回 ZVal 的走 callZ（sret 临时槽），返回 void 的（打印/推入）直接 callVoid 后返回空值；
//   - 语句型内置在表达式位置求值为 空（与 C 后端返回 z_null() 等价）。

import (
	"fmt"

	"zuzo/ast"
)

// builtinSpec 一条内置映射：运行时函数名 + 实参个数下限/上限（max < 0 = 不限）。
type builtinSpec struct {
	fn  string
	min int
	max int
}

// ── 实参求值 ───────────────────────────────────────────────

// evalArgs 从左到右求值全部实参，并各自落一个临时 %ZVal 槽位。
// 立即落槽保证「求值顺序 + 值快照」双重语义：后续实参的副作用不会回溯改写已求值的实参
// （例：打印(a, 弹出(a), a) 的第一个 a 必须保持弹出前的数组）。
func (g *Gen) evalArgs(args []ast.Expression) ([]*Value, error) {
	vals := make([]*Value, 0, len(args))
	for _, a := range args {
		v, err := g.genExpr(a)
		if err != nil {
			return nil, err
		}
		slot := g.tempZ()
		g.copyZ(slot, v.Slot)
		vals = append(vals, &Value{Slot: slot})
	}
	return vals, nil
}

// ptrArgs 把值槽位渲染成运行时调用的实参串。
func ptrArgs(vals []*Value) []string {
	out := make([]string, len(vals))
	for i, v := range vals {
		out[i] = "ptr " + v.Slot
	}
	return out
}

// needArgs 实参个数校验（与 C 后端同文案）。
func needArgs(name string, got, min, max int) error {
	if got < min || (max >= 0 && got > max) {
		return fmt.Errorf("%s() 参数数量不匹配: 得到 %d 个", name, got)
	}
	return nil
}

// evalChecked 求值 + 个数校验的公共前置。
func (g *Gen) evalChecked(name string, n *ast.CallExpression, min, max int) ([]*Value, error) {
	vals, err := g.evalArgs(n.Arguments)
	if err != nil {
		return nil, err
	}
	if err := needArgs(name, len(vals), min, max); err != nil {
		return nil, err
	}
	return vals, nil
}

// ── 一对一直映射的内置 ─────────────────────────────────────

var builtinTable = map[string]builtinSpec{
	// 通用
	"长度":  {"z_len", 1, 1},
	"字符串": {"z_to_str", 1, 1},
	"整数":  {"z_to_int", 1, 1},
	"浮点数": {"z_to_float", 1, 1},
	"类型":  {"z_type_of", 1, 1},
	"包含":  {"z_contains", 2, 2},

	// 数组 / 哈希
	"弹出": {"z_pop", 1, 1},
	"首项": {"z_first", 1, 1},
	"末项": {"z_last", 1, 1},
	"键":  {"z_keys", 1, 1},

	// 字符串（全局名）
	"大写":  {"zs_upper", 1, 1},
	"小写":  {"zs_lower", 1, 1},
	"前缀":  {"zs_starts", 2, 2},
	"后缀":  {"zs_ends", 2, 2},
	"查找":  {"zs_find", 2, 2},
	"替换":  {"zs_replace", 3, 3},
	"分割":  {"zs_split", 2, 2},
	"去空白": {"zs_trim", 1, 1},
	"裁剪":  {"z_trim_chars", 2, 2},
	"重复":  {"zs_repeat", 2, 2},
	"计数":  {"z_count_sub", 2, 2},
	"分词":  {"z_fields", 1, 1},
	"取字符": {"z_char_at", 2, 2},

	// 数学
	"绝对值":  {"z_abs", 1, 1},
	"最大值":  {"z_max2", 2, 2},
	"最小值":  {"z_min2", 2, 2},
	"向上取整": {"z_ceil", 1, 1},
	"向下取整": {"z_floor", 1, 1},
	"四舍五入": {"z_round", 1, 1},
	"幂":    {"z_pow", 2, 2},
	"平方根":  {"z_sqrt", 1, 1},
	"正弦":   {"z_sin", 1, 1},
	"余弦":   {"z_cos", 1, 1},
	"对数":   {"z_log", 1, 1},
	"随机":   {"z_rand", 0, 0},
	"随机整数": {"z_rand_int", 2, 2},

	// JSON
	"json解析":  {"z_json_parse", 1, 1},
	"json序列化": {"z_json_stringify", 1, 1},

	// 文件 I/O
	"读文件":  {"z_read_file", 1, 1},
	"写文件":  {"z_write_file", 2, 2},
	"追加文件": {"z_append_file", 2, 2},
	"文件存在": {"z_file_exists", 1, 1},
	"创建文件": {"z_create_file", 1, 1},
	"删除文件": {"z_delete_file", 1, 1},
	"重命名":  {"z_rename_file", 2, 2},

	// 编码
	"base64编码": {"z_base64_encode", 1, 1},
	"base64解码": {"z_base64_decode", 1, 1},
	"hex编码":    {"z_hex_encode", 1, 1},
	"hex解码":    {"z_hex_decode", 1, 1},
	"url编码":    {"z_url_encode", 1, 1},
	"url解码":    {"z_url_decode", 1, 1},

	// 时间
	"时间戳":   {"z_unix_time", 0, 0},
	"毫秒时间戳": {"z_unix_milli", 0, 0},
	"当前时间":  {"z_now", 0, 0},
	"睡眠":    {"z_sleep_ms", 1, 1},

	// 正则（第一个参数是文本，第二个是模式）
	"正则验证":   {"z_regex_test", 2, 2},
	"正则匹配":   {"z_regex_match", 2, 2},
	"正则查找全部": {"z_regex_find_all", 2, 2},
	"正则替换":   {"z_regex_replace", 3, 3},
	"正则分割":   {"z_regex_split", 2, 2},
	"正则提取":   {"z_regex_extract", 2, 3}, // 第 3 个参数与 C 后端一致：接受但忽略

	// 目录 / 环境
	"读目录":  {"z_read_dir", 1, 1},
	"环境变量": {"z_getenv", 1, 1},

	// 摘要
	"md5":    {"z_md5", 1, 1},
	"sha1":   {"z_sha1", 1, 1},
	"sha256": {"z_sha256", 1, 1},
}

// builtinKnown 判断名字是否是内置（表内直映射 + 特殊形态）。
func builtinKnown(name string) bool {
	switch name {
	case "打印", "推入", "连接", "取值", "格式化", "错误码", "错误信息", "错误行":
		return true
	}
	_, ok := builtinTable[name]
	return ok
}

// genBuiltinCall 处理内置函数。返回 (值, 是否命中, error)。
// 语句型内置（打印/推入）会直接发射调用，表达式位置返回 空 值。
func (g *Gen) genBuiltinCall(name string, n *ast.CallExpression) (*Value, bool, error) {
	if !builtinKnown(name) {
		return nil, false, nil
	}
	// 具名参数：内置一律不支持（数据建模实例化的具名参数不经过此函数）
	if len(n.NamedArgs) > 0 {
		return nil, true, fmt.Errorf("%d:%d: %s() 的具名参数在 LLVM 后端暂不支持，请改用位置参数",
			n.Token.Line, n.Token.Column, name)
	}

	switch name {
	case "打印":
		vals, err := g.evalArgs(n.Arguments)
		if err != nil {
			return nil, true, err
		}
		arr, cnt := g.zvalArray(vals)
		g.callVoid("z_print", fmt.Sprintf("i32 %d", cnt), "ptr "+arr)
		return g.constNull(), true, nil

	case "推入":
		vals, err := g.evalChecked(name, n, 2, 2)
		if err != nil {
			return nil, true, err
		}
		g.callVoid("z_array_push", "ptr "+vals[0].Slot, "ptr "+vals[1].Slot)
		return g.constNull(), true, nil

	case "连接":
		vals, err := g.evalChecked(name, n, 1, 2)
		if err != nil {
			return nil, true, err
		}
		sep := g.constStr("")
		if len(vals) == 2 {
			sep = vals[1]
		}
		return g.callZ("z_join", "ptr "+vals[0].Slot, "ptr "+sep.Slot), true, nil

	case "取值":
		vals, err := g.evalChecked(name, n, 2, 3)
		if err != nil {
			return nil, true, err
		}
		if len(vals) == 3 {
			return g.callZ("z_get", "ptr "+vals[0].Slot, "ptr "+vals[1].Slot,
				"ptr "+vals[2].Slot, "i32 1"), true, nil
		}
		def := g.constNull()
		return g.callZ("z_get", "ptr "+vals[0].Slot, "ptr "+vals[1].Slot,
			"ptr "+def.Slot, "i32 0"), true, nil

	case "格式化":
		// 至少 1 个参数（格式串），其余可变
		vals, err := g.evalChecked(name, n, 1, -1)
		if err != nil {
			return nil, true, err
		}
		arr, cnt := g.zvalArray(vals)
		return g.callZ("z_format", fmt.Sprintf("i32 %d", cnt), "ptr "+arr), true, nil

	// 错误对象内省：错误值在运行时是字符串载体，错误码/消息取自运行时全局
	// （最近一次 z_die 写入）；错误行在编译产物里不可知，固定 0（与 C 后端一致）。
	case "错误码", "错误信息", "错误行":
		if _, err := g.evalChecked(name, n, 1, 1); err != nil {
			return nil, true, err
		}
		switch name {
		case "错误码":
			s := g.callR("ptr", "z_error_code")
			return g.callZ("z_str", "ptr "+s), true, nil
		case "错误信息":
			s := g.callR("ptr", "z_error_msg")
			return g.callZ("z_str", "ptr "+s), true, nil
		default:
			return g.constInt(0), true, nil
		}
	}

	spec := builtinTable[name]
	vals, err := g.evalChecked(name, n, spec.min, spec.max)
	if err != nil {
		return nil, true, err
	}
	return g.callZ(spec.fn, ptrArgs(vals)...), true, nil
}

// textBuiltins 文本命名空间 → 运行时函数映射（成员名表与 C 后端一致，共 27 项）
var textBuiltins = map[string]builtinSpec{
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
func (g *Gen) genTextBuiltin(name string, n *ast.CallExpression) (*Value, bool, error) {
	spec, ok := textBuiltins[name]
	if !ok {
		return nil, false, nil
	}
	if len(n.Arguments) < spec.min || len(n.Arguments) > spec.max {
		return nil, true, fmt.Errorf("文本.%s() 参数数量不匹配: 得到 %d 个", name, len(n.Arguments))
	}
	vals, err := g.evalArgs(n.Arguments)
	if err != nil {
		return nil, true, err
	}
	return g.callZ(spec.fn, ptrArgs(vals)...), true, nil
}

// unsupportedBuiltins LLVM 后端明确不支持的运行时（依赖 Go 运行时/系统调用，
// 名单与 C 后端逐条一致）
var unsupportedBuiltins = map[string]string{
	"并发":     "goroutine 并发",
	"通道":     "通道",
	"发送":     "通道",
	"接收":     "通道",
	"关闭":     "通道",
	"等待全部":   "goroutine 并发",
	"尝试接收":   "通道",
	"尝试执行":   "错误捕获执行",
	"运行参数":   "进程参数",
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

// checkUnsupported 若调用了后端不支持的运行时，给出可读错误（沿用 C 后端文案与行列号口径）
func (g *Gen) checkUnsupported(name string, line, col int) error {
	if reason, ok := unsupportedBuiltins[name]; ok {
		return fmt.Errorf("%d:%d: %s() 依赖%s，LLVM 后端不支持（请用解释器执行，或改用替代实现）", line, col, name, reason)
	}
	return nil
}
