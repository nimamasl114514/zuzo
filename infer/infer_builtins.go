package infer

// infer_builtins.go — 内置函数的类型签名
// 与 evaluator/registerBuiltins 一一对应

// builtinType 返回内置函数的类型签名，非内置函数返回 false
func (i *Inferencer) builtinType(name string) (Type, bool) {
	switch name {
	// --- 通用 ---
	case "打印":
		// 打印(任意参数...) — 接受任意数量、任意类型的参数
		return TypeFunc{Params: []Type{TAny}, Return: TNull, Variadic: true}, true
	case "长度":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TInt}, true
	case "类型":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TStr}, true
	case "字符串":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TStr}, true
	case "整数":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TInt}, true
	case "浮点数":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TFloat}, true

	// --- 数组 ---
	case "推入":
		elemT := i.newTypeVar()
		return TypeFunc{Params: []Type{TypeArray{Elem: elemT}, elemT}, Return: TypeArray{Elem: elemT}}, true
	case "弹出":
		elemT := i.newTypeVar()
		return TypeFunc{Params: []Type{TypeArray{Elem: elemT}}, Return: elemT}, true
	case "首项":
		elemT := i.newTypeVar()
		return TypeFunc{Params: []Type{TypeArray{Elem: elemT}}, Return: elemT}, true
	case "末项":
		elemT := i.newTypeVar()
		return TypeFunc{Params: []Type{TypeArray{Elem: elemT}}, Return: elemT}, true
	case "连接":
		elemT := i.newTypeVar()
		// 连接(数组, 分隔符?) — 第二个参数可选
		return TypeFunc{Params: []Type{TypeArray{Elem: elemT}, TStr}, Return: TStr, Variadic: true}, true
	case "包含":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv, tv}, Return: TBool}, true

	// --- 字符串处理 ---
	case "大写":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "小写":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "前缀":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TBool}, true
	case "后缀":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TBool}, true
	case "查找":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TInt}, true
	case "替换":
		return TypeFunc{Params: []Type{TStr, TStr, TStr}, Return: TStr}, true
	case "分割":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TypeArray{Elem: TStr}}, true
	case "去空白":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "裁剪":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TStr}, true
	case "重复":
		return TypeFunc{Params: []Type{TStr, TInt}, Return: TStr}, true
	case "计数":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TInt}, true
	case "分词":
		return TypeFunc{Params: []Type{TStr}, Return: TypeArray{Elem: TStr}}, true
	case "取字符":
		return TypeFunc{Params: []Type{TStr, TInt}, Return: TStr}, true
	case "格式化":
		// 格式化(格式串, ...任意参数) — 可变参数类型为任意
		return TypeFunc{Params: []Type{TStr, TAny}, Return: TStr, Variadic: true}, true

	// --- 并发 ---
	case "并发":
		fnT := i.newTypeVar()
		return TypeFunc{Params: []Type{fnT}, Return: TypeChannel{Elem: i.newTypeVar()}}, true
	case "通道":
		return TypeFunc{Params: []Type{i.newTypeVar()}, Return: TypeChannel{Elem: i.newTypeVar()}}, true
	case "发送":
		chT := i.newTypeVar()
		valT := i.newTypeVar()
		return TypeFunc{Params: []Type{chT, valT}, Return: TNull}, true
	case "接收":
		chT := i.newTypeVar()
		return TypeFunc{Params: []Type{chT}, Return: i.newTypeVar()}, true
	case "尝试接收":
		chT := i.newTypeVar()
		return TypeFunc{Params: []Type{chT}, Return: i.newTypeVar()}, true
	case "尝试执行":
		fnT := i.newTypeVar()
		return TypeFunc{Params: []Type{fnT}, Return: i.newTypeVar()}, true
	case "关闭":
		chT := i.newTypeVar()
		return TypeFunc{Params: []Type{chT}, Return: TNull}, true
	case "等待全部":
		chT := i.newTypeVar()
		return TypeFunc{Params: []Type{chT}, Return: TypeArray{Elem: i.newTypeVar()}, Variadic: true}, true

	// --- 文件 I/O ---
	case "读文件":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "写文件":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TNull}, true
	case "追加文件":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TNull}, true
	case "读行":
		return TypeFunc{Params: []Type{}, Return: TStr}, true
	case "文件存在":
		return TypeFunc{Params: []Type{TStr}, Return: TBool}, true
	case "创建文件":
		return TypeFunc{Params: []Type{TStr}, Return: TNull}, true
	case "删除文件":
		return TypeFunc{Params: []Type{TStr}, Return: TNull}, true
	case "重命名":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TNull}, true
	case "读目录":
		return TypeFunc{Params: []Type{TStr}, Return: TypeArray{Elem: TStr}}, true
	case "环境变量":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "运行参数":
		return TypeFunc{Params: []Type{TInt}, Return: TStr}, true

	// --- 网络 ---
	case "http获取":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "http提交":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TStr}, true
	case "http更新":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TStr}, true
	case "http补丁":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TStr}, true
	case "http删除":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "http请求":
		return TypeFunc{Params: []Type{TStr, TStr, TStr, THash}, Return: THash}, true

	// --- JSON ---
	case "json解析":
		return TypeFunc{Params: []Type{TStr}, Return: TAny}, true
	case "json序列化":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TStr}, true
	case "json格式化":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TStr}, true

	// --- 正则表达式 ---
	case "正则匹配":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TAny}, true
	case "正则查找全部":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TypeArray{Elem: TAny}}, true
	case "正则替换":
		return TypeFunc{Params: []Type{TStr, TStr, TStr}, Return: TStr}, true
	case "正则分割":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TypeArray{Elem: TStr}}, true
	case "正则验证":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TBool}, true
	case "正则提取":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TStr}, true

	// --- 数学 ---
	case "随机":
		return TypeFunc{Params: []Type{}, Return: TFloat}, true
	case "随机整数":
		return TypeFunc{Params: []Type{TInt, TInt}, Return: TInt}, true
	case "绝对值":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: tv}, true
	case "最大值":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: tv, Variadic: true}, true
	case "最小值":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: tv, Variadic: true}, true
	case "向上取整":
		return TypeFunc{Params: []Type{TFloat}, Return: TInt}, true
	case "向下取整":
		return TypeFunc{Params: []Type{TFloat}, Return: TInt}, true
	case "四舍五入":
		return TypeFunc{Params: []Type{TFloat}, Return: TInt}, true
	case "幂":
		return TypeFunc{Params: []Type{TFloat, TFloat}, Return: TFloat}, true
	case "平方根":
		return TypeFunc{Params: []Type{TFloat}, Return: TFloat}, true
	case "正弦":
		return TypeFunc{Params: []Type{TFloat}, Return: TFloat}, true
	case "余弦":
		return TypeFunc{Params: []Type{TFloat}, Return: TFloat}, true
	case "对数":
		return TypeFunc{Params: []Type{TFloat}, Return: TFloat}, true

	// --- 时间 ---
	case "时间戳":
		return TypeFunc{Params: []Type{}, Return: TInt}, true
	case "毫秒时间戳":
		return TypeFunc{Params: []Type{}, Return: TInt}, true
	case "当前时间":
		return TypeFunc{Params: []Type{}, Return: TStr}, true
	case "格式化时间":
		return TypeFunc{Params: []Type{TInt, TStr}, Return: TStr}, true
	case "解析时间":
		return TypeFunc{Params: []Type{TStr, TStr}, Return: TInt}, true
	case "睡眠":
		return TypeFunc{Params: []Type{TInt}, Return: TNull}, true
	case "计时":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TInt}, true
	case "计时秒":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TFloat}, true
	case "时间差":
		return TypeFunc{Params: []Type{TInt, TInt}, Return: TStr}, true

	// --- 浏览器自动化 ---
	case "打开浏览器":
		return TypeFunc{Params: []Type{TBool}, Return: TAny}, true
	case "关闭浏览器":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "打开页面":
		return TypeFunc{Params: []Type{TAny}, Return: TAny}, true
	case "关闭页面":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "导航":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TNull}, true
	case "当前网址":
		return TypeFunc{Params: []Type{TAny}, Return: TStr}, true
	case "页面标题":
		return TypeFunc{Params: []Type{TAny}, Return: TStr}, true
	case "等待元素":
		return TypeFunc{Params: []Type{TAny, TStr, TInt}, Return: TNull}, true
	case "等待可见":
		return TypeFunc{Params: []Type{TAny, TStr, TInt}, Return: TBool}, true
	case "点击":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TNull}, true
	case "点击按钮":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TBool}, true
	case "填写":
		return TypeFunc{Params: []Type{TAny, TStr, TStr}, Return: TNull}, true
	case "清空输入":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TNull}, true
	case "获取文本":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TStr}, true
	case "获取属性":
		return TypeFunc{Params: []Type{TAny, TStr, TStr}, Return: TStr}, true
	case "获取HTML":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TStr}, true
	case "获取全部文本":
		return TypeFunc{Params: []Type{TAny}, Return: TStr}, true
	case "元素存在":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TBool}, true
	case "元素数量":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TInt}, true
	case "执行脚本":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TStr}, true
	case "等待跳转":
		return TypeFunc{Params: []Type{TAny, TStr, TInt}, Return: TBool}, true
	case "截图":
		return TypeFunc{Params: []Type{TAny, TStr}, Return: TNull}, true
	case "设置视口":
		return TypeFunc{Params: []Type{TAny, TInt, TInt}, Return: TNull}, true
	case "等待":
		return TypeFunc{Params: []Type{TInt}, Return: TNull}, true

	// --- 编码/解码 ---
	case "base64编码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "base64解码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "base64url编码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "base64url解码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "hex编码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "hex解码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "html转义":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "html反转义":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "url编码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "url解码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "url路径编码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "url路径解码":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "md5":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "sha1":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true
	case "sha256":
		return TypeFunc{Params: []Type{TStr}, Return: TStr}, true

	// --- BDD 测试框架 ---
	case "测试套件":
		return TypeFunc{Params: []Type{TStr, TAny}, Return: TNull}, true
	case "测试用例":
		return TypeFunc{Params: []Type{TStr, TAny}, Return: TNull}, true
	case "测试前":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "测试后":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "测试跳过":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv}, Return: TNull}, true
	case "断言相等":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv, tv}, Return: TNull}, true
	case "断言不相等":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv, tv}, Return: TNull}, true
	case "断言为真":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "断言为假":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "断言为空":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "断言非空":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "断言包含":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv, tv}, Return: TNull}, true
	case "断言大于":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv, tv}, Return: TNull}, true
	case "断言小于":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv, tv}, Return: TNull}, true
	case "断言大于等于":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv, tv}, Return: TNull}, true
	case "断言小于等于":
		tv := i.newTypeVar()
		return TypeFunc{Params: []Type{tv, tv}, Return: TNull}, true
	case "断言错误":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "断言无错误":
		return TypeFunc{Params: []Type{TAny}, Return: TNull}, true
	case "运行测试":
		return TypeFunc{Params: []Type{}, Return: TBool}, true

	// --- C 库 FFI ---
	case "加载C库":
		return TypeFunc{Params: []Type{TStr}, Return: TAny}, true
	case "C函数声明":
		return TypeFunc{Params: []Type{TAny, TStr, TypeArray{Elem: TStr}, TStr}, Return: TAny}, true
	case "C调用":
		// C调用(C函数对象, ...任意参数) — 可变参数，类型由 C 函数签名决定
		return TypeFunc{Params: []Type{TAny}, Return: TAny, Variadic: true}, true
	}

	return nil, false
}
