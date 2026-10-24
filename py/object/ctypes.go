package object

// ctypes.go — Python 侧原生 FFI 对象（ctypes 风格）
//
// 设计立场（2026-10-24 用户指令「必须支持原生调用，不要内嵌 py 解释器」）：
// zuzo 不引入 CPython 运行时，而是让 Python 代码直接加载任意 C 动态库并调用——
// 与 CPython 的 ctypes 同形，但不依赖 CPython ABI。CPython 专属扩展（numpy 等
// 链接 PyModule_Create/PyObject_* 的 .pyd）不在支持范围，这是 ABI 边界。
//
// 对象模型：
//   CDLLObj   —— 已加载的动态库（包装 zuzo 侧 CLib 句柄）
//   CFuncPtr  —— 库里的函数指针（带可变签名 argtypes/restype）
//   CTypeObj  —— ctypes 标量类型（c_int / c_double / c_char_p / c_void_p）
//   CValue    —— 标量值包装（c_int(5) 的结果），可作参数也可取值

import (
	"fmt"
	"strings"

	zobj "zuzo/object"
)

// CDLLObj 已加载的 C 动态库（Python 侧视图）
type CDLLObj struct {
	Handle uintptr
	Path   string
	Funcs  map[string]*CFuncPtr // 缓存同名函数指针
}

func (d *CDLLObj) Type() *TypeObj { return ctypesType("CDLL") }
func (d *CDLLObj) Repr() string   { return fmt.Sprintf("<CDLL '%s' handle=0x%x>", d.Path, d.Handle) }
func (d *CDLLObj) Str() string    { return d.Repr() }

// CFuncPtr C 函数指针（带可配签名）
type CFuncPtr struct {
	Name     string
	Addr     uintptr
	Lib      *CDLLObj
	ArgTypes []zobj.CFFIType // 空 = 默认 zobj.CInt
	Restype  zobj.CFFIType   // 默认 zobj.CInt
	HasSig   bool
}

func (f *CFuncPtr) Type() *TypeObj { return ctypesType("CFuncPtr") }
func (f *CFuncPtr) Repr() string   { return fmt.Sprintf("<CFuncPtr '%s' addr=0x%x>", f.Name, f.Addr) }
func (f *CFuncPtr) Str() string    { return f.Repr() }

// CTypeObj ctypes 标量类型对象（c_int 等）
type CTypeObj struct {
	Name string
	Kind zobj.CFFIType
}

func (t *CTypeObj) Type() *TypeObj { return ctypesType("ctype") }
func (t *CTypeObj) Repr() string   { return "<ctype '" + t.Name + "'>" }
func (t *CTypeObj) Str() string    { return t.Repr() }

// CValue 标量值包装（c_int(5) / c_double(1.5) / c_char_p("x")）
type CValue struct {
	Kind   zobj.CFFIType
	IntV   int64
	FloatV float64
	StrV   string
	PtrV   uintptr
}

func (v *CValue) Type() *TypeObj { return ctypesType("cvalue") }
func (v *CValue) Repr() string {
	switch v.Kind {
	case zobj.CFloat, zobj.CDouble:
		return fmt.Sprintf("c_%s(%v)", ctypeLabel(v.Kind), v.FloatV)
	case zobj.CStr:
		return fmt.Sprintf("c_char_p(%q)", v.StrV)
	case zobj.CPtr:
		return fmt.Sprintf("c_void_p(0x%x)", v.PtrV)
	default:
		return fmt.Sprintf("c_%s(%d)", ctypeLabel(v.Kind), v.IntV)
	}
}
func (v *CValue) Str() string { return v.Repr() }

func ctypeLabel(k zobj.CFFIType) string {
	switch k {
	case zobj.CInt:
		return "int"
	case zobj.CUInt:
		return "uint"
	case zobj.CLong:
		return "long"
	case zobj.CULong:
		return "ulong"
	case zobj.CLongLong:
		return "longlong"
	case zobj.CFloat:
		return "float"
	case zobj.CDouble:
		return "double"
	case zobj.CChar:
		return "char"
	case zobj.CUChar:
		return "ubyte"
	case zobj.CShort:
		return "short"
	case zobj.CUShort:
		return "ushort"
	case zobj.CBool:
		return "bool"
	case zobj.CPtr:
		return "void_p"
	case zobj.CStr:
		return "char_p"
	case zobj.CVoid:
		return "void"
	}
	return "int"
}

// ctypesType 返回 ctypes 相关类型的 TypeObj（名称用于 type(...).__name__）
func ctypesType(name string) *TypeObj {
	if t, ok := ctypesTypeCache[name]; ok {
		return t
	}
	t := &TypeObj{Name: name, Dict: map[string]Object{}}
	ctypesTypeCache[name] = t
	return t
}

var ctypesTypeCache = map[string]*TypeObj{}

// CFFITypeFromName 把 Python 侧类型字符串（"int"/"double"/"char_p"…）映射到 CFFIType
func CFFITypeFromName(s string) (zobj.CFFIType, bool) {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "int", "c_int":
		return zobj.CInt, true
	case "uint", "c_uint":
		return zobj.CUInt, true
	case "long", "c_long":
		return zobj.CLong, true
	case "ulong", "c_ulong":
		return zobj.CULong, true
	case "longlong", "c_longlong":
		return zobj.CLongLong, true
	case "float", "c_float":
		return zobj.CFloat, true
	case "double", "c_double":
		return zobj.CDouble, true
	case "char", "c_char":
		return zobj.CChar, true
	case "ubyte", "c_ubyte":
		return zobj.CUChar, true
	case "short", "c_short":
		return zobj.CShort, true
	case "ushort", "c_ushort":
		return zobj.CUShort, true
	case "bool", "c_bool":
		return zobj.CBool, true
	case "void_p", "c_void_p", "ptr":
		return zobj.CPtr, true
	case "char_p", "c_char_p", "string":
		return zobj.CStr, true
	case "void", "none":
		return zobj.CVoid, true
	}
	return zobj.CInt, false
}
