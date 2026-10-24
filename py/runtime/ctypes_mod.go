package runtime

// ctypes_mod.go — Python 侧原生 FFI（ctypes 风格，不依赖 CPython）
//
// 用法（与 CPython ctypes 对齐的子集）：
//
//	import ctypes
//	lib = ctypes.CDLL("msvcrt.dll")
//	lib.sqrt.argtypes = [ctypes.c_double]
//	lib.sqrt.restype = ctypes.c_double
//	print(lib.sqrt(144.0))          # 12
//	print(lib.abs(-42))             # 42（默认签名：int -> int）
//
//	lib2 = ctypes.WinDLL("user32.dll")
//	print(lib2.GetTickCount())      # 系统滴答
//
// 说明：加载与调用复用 zuzo 侧已有的 object.CLib / object.CallCFunc，
// 因此与 zuzo 语言里的 `加载C库/C调用` 是同一套底层，只是换成 Python 语法暴露。

import (
	"fmt"

	zobj "zuzo/object"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["ctypes"] = modCtypes
}

func modCtypes(ip *Interp) *object.Module {
	m := newModule("ctypes")

	// --- 标量类型对象 ---
	ctypes := []struct {
		name string
		kind zobj.CFFIType
	}{
		{"c_int", zobj.CInt},
		{"c_uint", zobj.CUInt},
		{"c_long", zobj.CLong},
		{"c_ulong", zobj.CULong},
		{"c_longlong", zobj.CLongLong},
		{"c_float", zobj.CFloat},
		{"c_double", zobj.CDouble},
		{"c_char", zobj.CChar},
		{"c_ubyte", zobj.CUChar},
		{"c_short", zobj.CShort},
		{"c_ushort", zobj.CUShort},
		{"c_bool", zobj.CBool},
		{"c_void_p", zobj.CPtr},
		{"c_char_p", zobj.CStr},
	}
	for _, ct := range ctypes {
		// 类型对象同时是「值构造器」与「签名元素」：
		//   c_int(5)            → CValue（由 Interp.Call 对 CTypeObj 分支处理）
		//   argtypes = [c_int]  → 列表元素即 CTypeObj，直接作为签名
		m.Dict[ct.name] = &object.CTypeObj{Name: ct.name, Kind: ct.kind}
	}

	// --- CDLL / WinDLL / LoadLibrary ---
	m.Dict["CDLL"] = &object.Builtin{
		Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
			return loadCDLL(args)
		},
	}
	m.Dict["WinDLL"] = m.Dict["CDLL"]
	m.Dict["LoadLibrary"] = m.Dict["CDLL"]
	m.Dict["cdll"] = m.Dict["CDLL"] // ctypes.cdll.LoadLibrary 的简化别名

	// --- sizeof / 常量 ---
	m.Dict["sizeof"] = &object.Builtin{
		Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
			if len(args) == 1 {
				if cv, ok := args[0].(*object.CValue); ok {
					return object.NewInt(int64(cffiSize(cv.Kind))), nil
				}
				if ct, ok := args[0].(*object.CTypeObj); ok {
					return object.NewInt(int64(cffiSize(ct.Kind))), nil
				}
			}
			return object.NewInt(0), nil
		},
	}
	return m
}

// loadCDLL 加载动态库并返回 Python 侧 CDLL 对象
func loadCDLL(args []object.Object) (object.Object, error) {
	if len(args) == 0 {
		return nil, fmt.Errorf("CDLL() 需要库路径")
	}
	path, ok := args[0].(*object.Str)
	if !ok {
		return nil, fmt.Errorf("CDLL() 参数必须是字符串路径")
	}
	handle, err := zobj.LoadCLib(path.V)
	if err != nil {
		return nil, fmt.Errorf("加载失败: %v", err)
	}
	return &object.CDLLObj{Handle: handle, Path: path.V, Funcs: map[string]*object.CFuncPtr{}}, nil
}

// ctypesGetAttr 处理 CDLL / CFuncPtr 的属性访问（由 Interp.GetAttr 调用）
func (ip *Interp) ctypesGetAttr(o object.Object, name string) (object.Object, bool) {
	switch v := o.(type) {
	case *object.CDLLObj:
		// 方法：Close
		if name == "Close" || name == "close" {
			return &object.Builtin{Fn: func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				zobj.CloseCLib(v.Handle)
				return object.None, nil
			}}, true
		}
		// 函数成员：按需解析符号
		if f, ok := v.Funcs[name]; ok {
			return f, true
		}
		addr, err := zobj.LookupCSymbol(v.Handle, name)
		if err != nil {
			return nil, false
		}
		f := &object.CFuncPtr{Name: name, Addr: addr, Lib: v}
		v.Funcs[name] = f
		return f, true

	case *object.CFuncPtr:
		switch name {
		case "argtypes":
			out := &object.List{}
			for _, t := range v.ArgTypes {
				out.Items = append(out.Items, &object.CTypeObj{Name: "arg", Kind: t})
			}
			return out, true
		case "restype":
			return &object.CTypeObj{Name: "ret", Kind: v.Restype}, true
		}
		return nil, false

	case *object.CValue:
		switch name {
		case "value":
			switch v.Kind {
			case zobj.CFloat, zobj.CDouble:
				return &object.Float{V: v.FloatV}, true
			case zobj.CStr:
				return &object.Str{V: v.StrV}, true
			case zobj.CPtr:
				return object.NewInt(int64(v.PtrV)), true
			default:
				return object.NewInt(v.IntV), true
			}
		}
		return nil, false
	}
	return nil, false
}

// ctypesSetAttr 处理 argtypes / restype 赋值（由 Interp 的赋值路径调用）
func ctypesSetAttr(o object.Object, name string, val object.Object) bool {
	f, ok := o.(*object.CFuncPtr)
	if !ok {
		return false
	}
	switch name {
	case "argtypes":
		lst, ok := val.(*object.List)
		if !ok {
			return true
		}
		f.ArgTypes = nil
		for _, it := range lst.Items {
			if ct, ok := it.(*object.CTypeObj); ok {
				f.ArgTypes = append(f.ArgTypes, ct.Kind)
			}
		}
		f.HasSig = true
		return true
	case "restype":
		if ct, ok := val.(*object.CTypeObj); ok {
			f.Restype = ct.Kind
			f.HasSig = true
		}
		return true
	}
	return false
}

// ctypesCall 调用 C 函数指针（由 Interp.Call 调用）
func (ip *Interp) ctypesCall(f *object.CFuncPtr, args []object.Object) (object.Object, bool) {
	// 构造 zuzo 侧调用参数：签名未设时全部按 int 处理
	paramTypes := f.ArgTypes
	retType := f.Restype
	if !f.HasSig {
		paramTypes = make([]zobj.CFFIType, len(args))
		for i := range paramTypes {
			paramTypes[i] = zobj.CInt
		}
		retType = zobj.CInt
	}

	goArgs := make([]interface{}, 0, len(args))
	for i, a := range args {
		want := zobj.CInt
		if i < len(paramTypes) {
			want = paramTypes[i]
		}
		goArgs = append(goArgs, toGoArg(want, a))
	}

	// 直接使用与 zuzo 侧相同的调用通道（支持字符串参数）
	rv, fv, isFloat, err := zobj.CallCFuncWithStrings(f.Addr, paramTypes, retType, goArgs)
	if err != nil {
		ip.RuntimeErr("C 调用失败: %v", err)
	}
	switch retType {
	case zobj.CVoid:
		return object.None, true
	case zobj.CFloat, zobj.CDouble:
		return &object.Float{V: fv}, true
	case zobj.CStr:
		return &object.Str{V: zobj.ReadCString(uintptr(rv))}, true
	case zobj.CPtr:
		return object.NewInt(int64(rv)), true
	default:
		if isFloat {
			return &object.Float{V: fv}, true
		}
		return object.NewInt(int64(rv)), true
	}
}

// toGoArg 把 Python 对象转成 C 调用参数
func toGoArg(kind zobj.CFFIType, o object.Object) interface{} {
	switch kind {
	case zobj.CStr:
		if s, ok := o.(*object.Str); ok {
			return s.V
		}
		// Python 的 bytes 字面量（b"..."）落在 *object.Bytes 上，ctypes 的 c_char_p 接受它
		if b, ok := o.(*object.Bytes); ok {
			return string(b.V)
		}
		return ""
	case zobj.CFloat, zobj.CDouble:
		if f, ok := o.(*object.Float); ok {
			return f.V
		}
		if iv, ok := o.(*object.Int); ok {
			return float64(iv.V)
		}
		if cv, ok := o.(*object.CValue); ok {
			return cv.FloatV
		}
		return 0.0
	default:
		if iv, ok := o.(*object.Int); ok {
			return iv.V
		}
		if f, ok := o.(*object.Float); ok {
			return int64(f.V)
		}
		if cv, ok := o.(*object.CValue); ok {
			if cv.Kind == zobj.CFloat || cv.Kind == zobj.CDouble {
				return int64(cv.FloatV)
			}
			return cv.IntV
		}
		return int64(0)
	}
}

// toCValue 把 Python 值包装成 CValue
func toCValue(kind zobj.CFFIType, o object.Object) *object.CValue {
	cv := &object.CValue{Kind: kind}
	switch v := o.(type) {
	case *object.Int:
		cv.IntV = v.V
		cv.FloatV = float64(v.V)
	case *object.Float:
		cv.FloatV = v.V
		cv.IntV = int64(v.V)
	case *object.Str:
		cv.StrV = v.V
	case *object.CValue:
		*cv = *v
		cv.Kind = kind
	}
	return cv
}

// cffiSize 返回类型字节宽度
func cffiSize(k zobj.CFFIType) int {
	switch k {
	case zobj.CChar, zobj.CUChar, zobj.CBool:
		return 1
	case zobj.CShort, zobj.CUShort:
		return 2
	case zobj.CInt, zobj.CUInt, zobj.CFloat:
		return 4
	case zobj.CLongLong, zobj.CDouble, zobj.CPtr, zobj.CStr, zobj.CLong, zobj.CULong:
		return 8
	}
	return 0
}
