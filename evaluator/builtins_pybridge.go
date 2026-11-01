package evaluator

// builtins_pybridge.go — .hy 侧调用真实 CPython 的内置函数（第32轮 · 2026-11-01）
//
// 立场：zuzo 自带的 Python 运行时是纯 Go 实现，能跑纯 Python 语义，但**无法加载 .pyd/.so
// C 扩展**（numpy / pandas / requests 的 C 部分都属于这一类）。本文件把「真 CPython 子进程桥」
// 暴露成 .hy 的一等能力，让 .hy 像调内置函数一样调任意已安装 Python 库。
//
// 错误码：
//   E910 桥不可用 / 启动失败（找不到 CPython、握手失败）
//   E911 参数错误
//   E912 远端 Python 抛异常（消息里带 traceback）
//   E913 目标不是 Python 句柄 / 不可调用
//
// 值语义：标量与容器跨桥是**快照**（深拷贝）；只有句柄是**引用**（同 mix 桥口径）。

import (
	"fmt"
	"strings"

	"zuzo/object"
	"zuzo/pyext"
)

func pyErr(code, format string, a ...any) *object.Error {
	return &object.Error{Code: code, Message: fmt.Sprintf(format, a...)}
}

// pyBridge 取进程级单例
func pyBridge() *pyext.Client { return pyext.Default() }

func asPyObj(o object.Object) (*object.PyObj, bool) {
	p, ok := o.(*object.PyObj)
	return p, ok
}

// checkTransferable 拦截不能过桥的值（.hy 函数/内置/定义）
// 桥是单向的：CPython 不能回调 .hy 代码，静默转成字符串只会让人误解。
func checkTransferable(name string, args []object.Object) *object.Error {
	for _, a := range args {
		switch a.(type) {
		case *object.Function, *object.Builtin, *object.Define:
			return pyErr("E914", "%s() 不支持把 .hy 函数/内置传给 Python（桥是单向的，CPython 不能回调 .hy）；请改传标量/数组/字典/句柄", name)
		}
	}
	return nil
}

// notPyObjectErr 对「传了快照容器」给出可操作的提示
func notPyObjectErr(name string, o object.Object) *object.Error {
	t := o.Type()
	hint := ""
	if t == object.ARRAY_OBJ || t == object.HASH_OBJ {
		hint = "（数组/字典过桥是快照，不是远端对象；要操作真对象请 py导入 / py取 / py方法 逐步做，或把数据作为参数传给 Python 函数）"
	}
	return pyErr("E913", "%s() 第一个参数必须是 Python 对象，得到 %s%s", name, t, hint)
}

// ==================== py导入 ====================

// py导入(模块名) → Python对象 — 导入真实 CPython 模块（含 C 扩展）
func builtinPyImport(args ...object.Object) object.Object {
	if len(args) != 1 {
		return pyErr("E911", "py导入() 期望 1 个参数（模块名），得到 %d 个", len(args))
	}
	name, ok := args[0].(*object.String)
	if !ok {
		return pyErr("E911", "py导入() 参数必须是字符串，得到 %s", args[0].Type())
	}
	v, err := pyBridge().Import(name.Value)
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "No module named") || strings.Contains(msg, "ModuleNotFoundError") {
			msg += "\n提示：该库未安装 → ./zuzo.exe pip install " + name.Value
		}
		return pyErr("E912", "%s", msg)
	}
	// 首次导入 C 扩展即视为进入子进程模式，后续 py求值/py执行 走同一份命名空间
	pyBridge().Activate()
	return v
}

// ==================== py取 ====================

// py取(目标, 属性名) → 任意 — 取模块/对象的属性
func builtinPyGet(args ...object.Object) object.Object {
	if len(args) != 2 {
		return pyErr("E911", "py取() 期望 2 个参数（目标, 属性名），得到 %d 个", len(args))
	}
	target, ok := asPyObj(args[0])
	if !ok {
		return notPyObjectErr("py取", args[0])
	}
	name, ok := args[1].(*object.String)
	if !ok {
		return pyErr("E911", "py取() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	v, err := pyBridge().GetAttr(target, name.Value)
	if err != nil {
		return pyErr("E912", "%v", err)
	}
	return v
}

// py设(目标, 属性名, 值) → 空 — 设置属性
func builtinPySet(args ...object.Object) object.Object {
	if len(args) != 3 {
		return pyErr("E911", "py设() 期望 3 个参数（目标, 属性名, 值），得到 %d 个", len(args))
	}
	target, ok := asPyObj(args[0])
	if !ok {
		return pyErr("E913", "py设() 第一个参数必须是 Python 对象，得到 %s", args[0].Type())
	}
	name, ok := args[1].(*object.String)
	if !ok {
		return pyErr("E911", "py设() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	if e := checkTransferable("py设", args[2:]); e != nil {
		return e
	}
	if err := pyBridge().SetAttr(target, name.Value, args[2]); err != nil {
		return pyErr("E912", "%v", err)
	}
	return NULL
}

// ==================== py调 / py方法 / py调参 ====================

// py调(可调用对象, 参数...) → 任意 — 直接调用（位置参数）
func builtinPyCall(args ...object.Object) object.Object {
	if len(args) < 1 {
		return pyErr("E911", "py调() 至少需要 1 个参数（可调用对象）")
	}
	if e := checkTransferable("py调", args[1:]); e != nil {
		return e
	}
	target, ok := asPyObj(args[0])
	if !ok {
		return notPyObjectErr("py调", args[0])
	}
	v, err := pyBridge().Call(target, args[1:], nil)
	if err != nil {
		return pyErr("E912", "%v", err)
	}
	return v
}

// py方法(目标, "方法名", 参数...) → 任意 — 取方法并调用（等价 py调(py取(...), ...)）
func builtinPyMethod(args ...object.Object) object.Object {
	if len(args) < 2 {
		return pyErr("E911", "py方法() 至少需要 2 个参数（目标, 方法名）")
	}
	target, ok := asPyObj(args[0])
	if !ok {
		return notPyObjectErr("py方法", args[0])
	}
	name, ok := args[1].(*object.String)
	if !ok {
		return pyErr("E911", "py方法() 第二个参数必须是字符串（方法名），得到 %s", args[1].Type())
	}
	fn, err := pyBridge().GetAttr(target, name.Value)
	if err != nil {
		return pyErr("E912", "%v", err)
	}
	fno, ok := asPyObj(fn)
	if !ok {
		return pyErr("E913", "Python 侧 %s 不是可调用对象", name.Value)
	}
	v, err := pyBridge().Call(fno, args[2:], nil)
	if err != nil {
		return pyErr("E912", "%v", err)
	}
	return v
}

// py调参(可调用对象, 参数数组, 命名参数字典) → 任意 — 支持关键字参数
func builtinPyCallKw(args ...object.Object) object.Object {
	if len(args) < 1 || len(args) > 3 {
		return pyErr("E911", "py调参() 接受 1~3 个参数（可调用对象[, 参数数组, 命名参数字典]）")
	}
	target, ok := asPyObj(args[0])
	if !ok {
		return notPyObjectErr("py调参", args[0])
	}
	var pos []object.Object
	kwargs := map[string]object.Object{}
	if len(args) >= 2 {
		arr, ok := args[1].(*object.Array)
		if !ok {
			return pyErr("E911", "py调参() 第二个参数必须是数组，得到 %s", args[1].Type())
		}
		pos = arr.Elements
	}
	if len(args) == 3 {
		h, ok := args[2].(*object.Hash)
		if !ok {
			return pyErr("E911", "py调参() 第三个参数必须是哈希，得到 %s", args[2].Type())
		}
		for _, hk := range h.Keys {
			pair, ok := h.Pairs[hk]
			if !ok {
				continue
			}
			ks, ok := pair.Key.(*object.String)
			if !ok {
				continue
			}
			kwargs[ks.Value] = pair.Value
		}
	}
	v, err := pyBridge().Call(target, pos, kwargs)
	if err != nil {
		return pyErr("E912", "%v", err)
	}
	return v
}

// ==================== py取值 / py描述 / py释放 ====================

// py取值(句柄) → 任意 — 把远端对象尽力转成 zuzo 原生（ndarray→数组、DataFrame→记录数组）
func builtinPyNative(args ...object.Object) object.Object {
	if len(args) == 2 {
		// 两参形式：py取值(对象, 键) = obj[键]（DataFrame 取列 / dict 取值）
		target, ok := asPyObj(args[0])
		if !ok {
			return pyErr("E913", "py取值(目标, 键) 的第一个参数必须是 Python 对象，得到 %s", args[0].Type())
		}
		fn, err := pyBridge().GetAttr(target, "__getitem__")
		if err != nil {
			return pyErr("E912", "%v", err)
		}
		fno, ok := asPyObj(fn)
		if !ok {
			return pyErr("E913", "目标不支持下标访问")
		}
		v, err := pyBridge().Call(fno, args[1:], nil)
		if err != nil {
			return pyErr("E912", "%v", err)
		}
		return v
	}
	if len(args) != 1 {
		return pyErr("E911", "py取值() 期望 1~2 个参数（Python 对象[, 键]）")
	}
	target, ok := asPyObj(args[0])
	if !ok {
		// 已经是原生值：幂等返回
		return args[0]
	}
	v, err := pyBridge().Native(target)
	if err != nil {
		return pyErr("E912", "%v", err)
	}
	return v
}

// py描述(句柄) → 字符串 — 远端 repr
func builtinPyRepr(args ...object.Object) object.Object {
	if len(args) != 1 {
		return pyErr("E911", "py描述() 期望 1 个参数（Python 对象）")
	}
	target, ok := asPyObj(args[0])
	if !ok {
		return &object.String{Value: args[0].Inspect()}
	}
	s, err := pyBridge().Repr(target)
	if err != nil {
		return pyErr("E912", "%v", err)
	}
	return &object.String{Value: s}
}

// py类型(句柄) → 字符串 — 远端类型名（module / ndarray / DataFrame …）
func builtinPyType(args ...object.Object) object.Object {
	if len(args) != 1 {
		return pyErr("E911", "py类型() 期望 1 个参数（Python 对象）")
	}
	target, ok := asPyObj(args[0])
	if !ok {
		return &object.String{Value: string(args[0].Type())}
	}
	return &object.String{Value: target.Typ}
}

// py释放(句柄) → 空 — 释放远端引用
func builtinPyRelease(args ...object.Object) object.Object {
	if len(args) != 1 {
		return pyErr("E911", "py释放() 期望 1 个参数（Python 对象）")
	}
	target, ok := asPyObj(args[0])
	if !ok {
		return NULL
	}
	if err := pyBridge().Release(target); err != nil {
		return pyErr("E912", "%v", err)
	}
	return NULL
}

// ==================== py模式 / py信息 / py有库 / py安装 ====================

// py模式() → 字符串；py模式("子进程"/"本地") → 切换
func builtinPyMode(args ...object.Object) object.Object {
	b := pyBridge()
	if len(args) == 0 {
		return &object.String{Value: b.Mode()}
	}
	m, ok := args[0].(*object.String)
	if !ok {
		return pyErr("E911", "py模式() 参数必须是字符串（\"子进程\" / \"本地\"）")
	}
	switch m.Value {
	case "子进程", "进程", "子进程CPython":
		if err := b.Activate(); err != nil {
			return pyErr("E910", "%v", err)
		}
		return &object.String{Value: "子进程"}
	case "本地", "内置":
		b.SetSticky(false)
		return &object.String{Value: "本地"}
	default:
		return pyErr("E911", "py模式() 不认识 %q（可用：\"子进程\" / \"本地\"）", m.Value)
	}
}

// py信息() → 哈希 — 解释器与桥运行态
func builtinPyInfo(args ...object.Object) object.Object {
	info, err := pyBridge().Info()
	if err != nil {
		return pyErr("E910", "%v", err)
	}
	h := &object.Hash{Pairs: map[object.HashKey]object.HashPair{}}
	for k, v := range info {
		h.SetKey(&object.String{Value: k}, v)
	}
	stats := pyBridge().Stats()
	for k, v := range stats {
		h.SetKey(&object.String{Value: k}, v)
	}
	return h
}

// py有库(模块名) → 布尔 — 探测是否已安装（不导入）
func builtinPyHasModule(args ...object.Object) object.Object {
	if len(args) != 1 {
		return pyErr("E911", "py有库() 期望 1 个参数（模块名）")
	}
	name, ok := args[0].(*object.String)
	if !ok {
		return pyErr("E911", "py有库() 参数必须是字符串")
	}
	has, err := pyBridge().HasModule(name.Value)
	if err != nil {
		return pyErr("E912", "%v", err)
	}
	return &object.Boolean{Value: has}
}

// py安装(包名) → 哈希{成功, 输出} — 显式 pip 安装（不做隐式联网）
func builtinPyInstall(args ...object.Object) object.Object {
	if len(args) != 1 {
		return pyErr("E911", "py安装() 期望 1 个参数（包名）")
	}
	name, ok := args[0].(*object.String)
	if !ok {
		return pyErr("E911", "py安装() 参数必须是字符串")
	}
	out, err := pyBridge().Install(name.Value)
	h := &object.Hash{Pairs: map[object.HashKey]object.HashPair{}}
	h.SetKey(&object.String{Value: "成功"}, &object.Boolean{Value: err == nil})
	if err != nil {
		h.SetKey(&object.String{Value: "错误"}, &object.String{Value: err.Error()})
	}
	tail := out
	if len(tail) > 4000 {
		tail = tail[len(tail)-4000:]
	}
	h.SetKey(&object.String{Value: "输出"}, &object.String{Value: tail})
	return h
}

// PyBridgeActive 给 mix 层查「当前是否处于粘性子进程模式」
func PyBridgeActive() bool { return pyBridge().Active() }

// PyBridgeActivate 给 mix 层在命中 C 扩展时激活子进程
func PyBridgeActivate() error { return pyBridge().Activate() }
