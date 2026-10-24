package evaluator

// builtins_cffi.go — C 库 FFI 内置函数
//
// 用法 (推荐):
//   lib = 加载C库("libm.dll")                    // 加载动态库 (.dll/.so/.dylib)
//   fn = lib.绑定("sqrt", ["double"], "double")  // 声明函数签名
//   结果 = fn(144.0)                             // 调用，返回 12.0
//   lib.关闭()                                   // 关闭库
//
// 简便写法 (直接调用):
//   lib = 加载C库("libm.dll")
//   打印(lib.sqrt(144.0))                        // 自动推断，直接调用
//
// 低级 API:
//   C函数声明(lib, "sqrt", ["double"], "double")  — 声明函数签名
//   C调用(fn, 参数...)                            — 显式调用

import (
	"fmt"
	"strings"

	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/object"
)

// ==================== 加载C库 ====================

// builtinLoadCLib 加载 C 动态库
// 加载C库(路径或库名) → C库对象
func builtinLoadCLib(args ...object.Object) object.Object {
	if len(args) < 1 || len(args) > 2 {
		return newError("加载C库() 期望 1-2 个参数，得到 %d 个", len(args))
	}

	path, ok := args[0].(*object.String)
	if !ok {
		return newError("加载C库() 第一个参数必须是字符串（库路径），得到 %s", args[0].Type())
	}

	name := ""
	if len(args) == 2 {
		if n, ok := args[1].(*object.String); ok {
			name = n.Value
		}
	}
	if name == "" {
		name = path.Value
	}

	handle, err := object.LoadCLib(path.Value)
	if err != nil {
		return newError("加载C库() 失败: %v", err)
	}

	return &object.CLib{
		Handle: handle,
		Name:   name,
	}
}

// ==================== 声明函数签名 ====================

// builtinCFuncDecl 声明 C 函数签名
// C函数声明(库, 函数名, [参数类型...], 返回类型) → C函数对象
func builtinCFuncDecl(args ...object.Object) object.Object {
	if len(args) < 3 {
		return newError("C函数声明() 至少需要 3 个参数（库, 函数名, 参数类型数组, 返回类型），得到 %d 个", len(args))
	}

	lib, ok := args[0].(*object.CLib)
	if !ok {
		return newError("C函数声明() 第一个参数必须是 C 库对象，得到 %s", args[0].Type())
	}

	if lib.IsClosed() {
		return newError("C函数声明() 库已关闭")
	}

	funcName, ok := args[1].(*object.String)
	if !ok {
		return newError("C函数声明() 第二个参数必须是字符串（函数名），得到 %s", args[1].Type())
	}

	// 参数类型数组
	paramArr, ok := args[2].(*object.Array)
	if !ok {
		return newError("C函数声明() 第三个参数必须是数组（参数类型），得到 %s", args[2].Type())
	}

	paramTypes := make([]object.CFFIType, 0, len(paramArr.Elements))
	for i, elem := range paramArr.Elements {
		typeStr, ok := elem.(*object.String)
		if !ok {
			return newError("C函数声明() 参数类型 %d 必须是字符串，得到 %s", i+1, elem.Type())
		}
		t, ok := object.ParseCFFIType(typeStr.Value)
		if !ok {
			return newError("C函数声明() 未知参数类型 %q (可用: int,uint,long,ulong,longlong,float,double,char,uchar,short,ushort,bool,ptr,string)", typeStr.Value)
		}
		paramTypes = append(paramTypes, t)
	}

	// 返回类型
	if len(args) < 4 {
		return newError("C函数声明() 需要返回类型参数")
	}
	retTypeStr, ok := args[3].(*object.String)
	if !ok {
		return newError("C函数声明() 返回类型必须是字符串，得到 %s", args[3].Type())
	}
	retType, ok := object.ParseCFFIType(retTypeStr.Value)
	if !ok {
		return newError("C函数声明() 未知返回类型 %q (可用: void,int,uint,long,ulong,longlong,float,double,char,uchar,short,ushort,bool,ptr,string)", retTypeStr.Value)
	}

	// 查找符号
	addr, err := object.LookupCSymbol(lib.Handle, funcName.Value)
	if err != nil {
		return newError("C函数声明() %v", err)
	}

	return &object.CFunc{
		Lib:        lib,
		Name:       funcName.Value,
		Addr:       addr,
		ParamTypes: paramTypes,
		RetType:    retType,
	}
}

// ==================== 调用 C 函数 ====================

// builtinCCall 调用 C 函数
// C调用(函数对象, 参数...) → 返回值
func builtinCCall(args ...object.Object) object.Object {
	if len(args) < 1 {
		return newError("C调用() 至少需要 1 个参数（C 函数对象），得到 %d 个", len(args))
	}

	fn, ok := args[0].(*object.CFunc)
	if !ok {
		return newError("C调用() 第一个参数必须是 C 函数对象，得到 %s", args[0].Type())
	}

	return callCFunction(fn, args[1:])
}

// callCFunction 执行实际的 C 函数调用
func callCFunction(fn *object.CFunc, args []object.Object) object.Object {
	if len(args) != len(fn.ParamTypes) {
		return newError("C调用 %s() 参数数量不匹配: 期望 %d 个，得到 %d 个", fn.Name, len(fn.ParamTypes), len(args))
	}

	// 转换参数
	goArgs := make([]interface{}, len(args))
	for i, arg := range args {
		switch fn.ParamTypes[i] {
		case object.CStr:
			s, ok := arg.(*object.String)
			if !ok {
				return newError("C调用 %s() 参数 %d 期望字符串，得到 %s", fn.Name, i+1, arg.Type())
			}
			goArgs[i] = s.Value
		case object.CFloat, object.CDouble:
			switch v := arg.(type) {
			case *object.Float:
				goArgs[i] = v.Value
			case *object.Integer:
				goArgs[i] = float64(v.Value)
			default:
				return newError("C调用 %s() 参数 %d 期望浮点数，得到 %s", fn.Name, i+1, arg.Type())
			}
		case object.CPtr:
			switch v := arg.(type) {
			case *object.Integer:
				goArgs[i] = uintptr(v.Value)
			case *object.String:
				// 字符串可以作为指针传递 (用于 void* 参数)
				goArgs[i] = uintptr(0) // 简化处理，实际需要分配 C 字符串
			default:
				return newError("C调用 %s() 参数 %d 期望指针，得到 %s", fn.Name, i+1, arg.Type())
			}
		case object.CBool:
			switch v := arg.(type) {
			case *object.Boolean:
				if v.Value {
					goArgs[i] = int64(1)
				} else {
					goArgs[i] = int64(0)
				}
			case *object.Integer:
				goArgs[i] = v.Value
			default:
				return newError("C调用 %s() 参数 %d 期望布尔，得到 %s", fn.Name, i+1, arg.Type())
			}
		default:
			// 整数类型
			switch v := arg.(type) {
			case *object.Integer:
				goArgs[i] = v.Value
			case *object.Float:
				goArgs[i] = int64(v.Value)
			default:
				return newError("C调用 %s() 参数 %d 期望整数，得到 %s", fn.Name, i+1, arg.Type())
			}
		}
	}

	// 调用 C 函数
	retInt, retDbl, isFloatRet, err := object.CallCFuncWithStrings(fn.Addr, fn.ParamTypes, fn.RetType, goArgs)
	if err != nil {
		return newError("C调用 %s() 失败: %v", fn.Name, err)
	}

	// 转换返回值
	return cReturnValueToObject(fn.RetType, retInt, retDbl, isFloatRet)
}

// cReturnValueToObject 将 C 返回值转换为 Zuzo 对象
func cReturnValueToObject(retType object.CFFIType, retInt uint64, retDbl float64, isFloatRet bool) object.Object {
	switch retType {
	case object.CVoid:
		return NULL
	case object.CFloat:
		return &object.Float{Value: retDbl}
	case object.CDouble:
		return &object.Float{Value: retDbl}
	case object.CStr:
		// 从指针读取 C 字符串
		return &object.String{Value: object.ReadCString(uintptr(retInt))}
	case object.CPtr:
		return &object.Integer{Value: int64(retInt)}
	case object.CBool:
		if retInt != 0 {
			return TRUE
		}
		return FALSE
	case object.CChar:
		return &object.Integer{Value: int64(int8(retInt))}
	case object.CUChar:
		return &object.Integer{Value: int64(uint8(retInt))}
	case object.CShort:
		return &object.Integer{Value: int64(int16(retInt))}
	case object.CUShort:
		return &object.Integer{Value: int64(uint16(retInt))}
	case object.CInt:
		return &object.Integer{Value: int64(int32(retInt))}
	case object.CUInt:
		return &object.Integer{Value: int64(uint32(retInt))}
	case object.CLong:
		// 在 64 位 Unix 上 long 是 8 字节，在 Windows 上是 4 字节
		return &object.Integer{Value: int64(retInt)}
	case object.CULong:
		return &object.Integer{Value: int64(retInt)}
	case object.CLongLong:
		return &object.Integer{Value: int64(retInt)}
	default:
		return &object.Integer{Value: int64(retInt)}
	}
}

// ==================== 点号语法支持 ====================

// evalCLibIndexExpression C 库成员访问: lib.函数名 或 lib.方法名
func evalCLibIndexExpression(node ast.Node, libObj, indexObj object.Object) object.Object {
	lib, ok := libObj.(*object.CLib)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "C 库索引需要 C 库对象")
	}

	key, ok := indexObj.(*object.String)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "C 库索引必须是字符串")
	}

	// 内置方法
	switch key.Value {
	case "关闭":
		return &object.Builtin{Fn: func(args ...object.Object) object.Object {
			lib.Close()
			return NULL
		}}
	case "绑定":
		// lib.绑定("函数名", [参数类型], 返回类型)
		return &object.Builtin{Fn: func(args ...object.Object) object.Object {
			if len(args) < 3 {
				return newError("绑定() 至少需要 3 个参数")
			}
			// 构建完整参数列表: [lib, funcName, paramTypes, retType]
			fullArgs := append([]object.Object{lib}, args...)
			return builtinCFuncDecl(fullArgs...)
		}}
	case "符号":
		// lib.符号("函数名") → 查找符号地址，返回整数 (可用于调试)
		return &object.Builtin{Fn: func(args ...object.Object) object.Object {
			if len(args) != 1 {
				return newError("符号() 期望 1 个参数")
			}
			name, ok := args[0].(*object.String)
			if !ok {
				return newError("符号() 参数必须是字符串")
			}
			addr, err := object.LookupCSymbol(lib.Handle, name.Value)
			if err != nil {
				return newError("符号() %v", err)
			}
			return &object.Integer{Value: int64(addr)}
		}}
	}

	// 自动绑定：尝试查找符号并创建一个"自动推断类型"的 C 函数
	// 由于没有类型签名信息，我们创建一个"动态" C 函数
	addr, err := object.LookupCSymbol(lib.Handle, key.Value)
	if err != nil {
		return newErrorWithNode(node, diagnostic.ErrHashKeyNotFound.Code,
			"C 库中不存在符号 %q: %v", key.Value, err)
	}

	// 返回一个特殊的 Builtin，当被调用时执行动态 C 调用
	// 由于没有类型信息，默认使用整数参数/返回值
	// 用户应该使用 lib.绑定() 来获得类型安全的调用
	return &object.Builtin{Fn: func(callArgs ...object.Object) object.Object {
		// 动态模式：将所有参数作为 int64 传递，返回 int64
		paramTypes := make([]object.CFFIType, len(callArgs))
		goArgs := make([]interface{}, len(callArgs))
		for i, a := range callArgs {
			switch v := a.(type) {
			case *object.Integer:
				paramTypes[i] = object.CLongLong
				goArgs[i] = v.Value
			case *object.Float:
				paramTypes[i] = object.CDouble
				goArgs[i] = v.Value
			case *object.String:
				paramTypes[i] = object.CStr
				goArgs[i] = v.Value
			case *object.Boolean:
				paramTypes[i] = object.CBool
				if v.Value {
					goArgs[i] = int64(1)
				} else {
					goArgs[i] = int64(0)
				}
			default:
				return newError("动态 C 调用不支持参数类型: %s (请使用 lib.绑定() 声明类型签名)", a.Type())
			}
		}

		retInt, _, _, err := object.CallCFuncWithStrings(addr, paramTypes, object.CLongLong, goArgs)
		if err != nil {
			return newError("C 调用 %s() 失败: %v", key.Value, err)
		}

		// 动态模式默认返回整数
		return &object.Integer{Value: int64(retInt)}
	}}
}

// evalCFuncIndexExpression C 函数对象的点号访问 (目前不需要特殊处理)
func evalCFuncIndexExpression(node ast.Node, fnObj, indexObj object.Object) object.Object {
	fn, ok := fnObj.(*object.CFunc)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "C 函数索引需要 C 函数对象")
	}

	key, ok := indexObj.(*object.String)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "C 函数索引必须是字符串")
	}

	switch key.Value {
	case "名称":
		return &object.String{Value: fn.Name}
	case "签名":
		params := make([]string, len(fn.ParamTypes))
		for i, t := range fn.ParamTypes {
			params[i] = object.CFFITypeName(t)
		}
		return &object.String{Value: fmt.Sprintf("%s(%s) -> %s", fn.Name, strings.Join(params, ", "), object.CFFITypeName(fn.RetType))}
	}

	return newErrorWithNode(node, diagnostic.ErrHashKeyNotFound.Code, "C 函数对象不存在属性 %q", key.Value)
}
