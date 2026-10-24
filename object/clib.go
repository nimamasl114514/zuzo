package object

import (
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// 导出辅助函数供外部使用
var _ = unsafe.Sizeof(0)

// CLib FFI 对象类型常量
const (
	CLIB_OBJ      = "C库"
	CLIB_FUNC_OBJ = "C函数"
)

// CFFIType 表示 C 函数参数/返回值的类型
type CFFIType int

const (
	CVoid    CFFIType = iota // void
	CInt                     // int (int32)
	CUInt                    // unsigned int (uint32)
	CLong                    // long (int64 on 64-bit)
	CULong                   // unsigned long
	CLongLong                // long long (int64)
	CFloat                   // float (float32)
	CDouble                  // double (float64)
	CChar                    // char (int8)
	CUChar                   // unsigned char (uint8)
	CShort                   // short (int16)
	CUShort                  // unsigned short (uint16)
	CBool                    // bool (int)
	CPtr                     // void* (raw pointer, passed as uintptr)
	CStr                     // const char* (C string, passed as Go string → C string)
)

// CFFITypeName 返回 CFFIType 的可读名称（导出版本）
func CFFITypeName(t CFFIType) string {
	return cffiTypeName(t)
}

// cffiTypeName 返回 CFFIType 的可读名称
func cffiTypeName(t CFFIType) string {
	switch t {
	case CVoid:
		return "void"
	case CInt:
		return "int"
	case CUInt:
		return "uint"
	case CLong:
		return "long"
	case CULong:
		return "ulong"
	case CLongLong:
		return "longlong"
	case CFloat:
		return "float"
	case CDouble:
		return "double"
	case CChar:
		return "char"
	case CUChar:
		return "uchar"
	case CShort:
		return "short"
	case CUShort:
		return "ushort"
	case CBool:
		return "bool"
	case CPtr:
		return "ptr"
	case CStr:
		return "string"
	default:
		return "unknown"
	}
}

// ParseCFFIType 从字符串解析 CFFIType
func ParseCFFIType(s string) (CFFIType, bool) {
	switch s {
	case "void", "空":
		return CVoid, true
	case "int", "整数":
		return CInt, true
	case "uint", "无符号整数":
		return CUInt, true
	case "long", "长整数":
		return CLong, true
	case "ulong", "无符号长整数":
		return CULong, true
	case "longlong", "超长整数":
		return CLongLong, true
	case "float", "单精度":
		return CFloat, true
	case "double", "双精度", "浮点数":
		return CDouble, true
	case "char", "字符":
		return CChar, true
	case "uchar", "无符号字符":
		return CUChar, true
	case "short", "短整数":
		return CShort, true
	case "ushort", "无符号短整数":
		return CUShort, true
	case "bool", "布尔":
		return CBool, true
	case "ptr", "指针":
		return CPtr, true
	case "string", "字符串":
		return CStr, true
	default:
		return 0, false
	}
}

// cffiTypeSize 返回 CFFIType 的字节大小
func cffiTypeSize(t CFFIType) int {
	switch t {
	case CVoid:
		return 0
	case CInt, CUInt, CFloat:
		return 4
	case CLong, CULong:
		if runtime.GOOS == "windows" || runtime.GOARCH == "386" {
			return 4
		}
		return 8
	case CLongLong, CDouble:
		return 8
	case CChar, CUChar, CBool:
		return 1
	case CShort, CUShort:
		return 2
	case CPtr, CStr:
		return int(unsafe.Sizeof(unsafe.Pointer(nil)))
	default:
		return 0
	}
}

// CLib 表示已加载的 C 动态库
type CLib struct {
	mu     sync.Mutex
	Handle uintptr // 动态库句柄 (dlopen 返回值或 LoadLibrary 返回值)
	Name   string  // 库名称/路径
	closed bool
}

func (cl *CLib) Type() ObjectType { return CLIB_OBJ }
func (cl *CLib) Inspect() string {
	return fmt.Sprintf("C库[%s]", cl.Name)
}

// Handle 返回底层库句柄 (兼容方法)
func (cl *CLib) Handle_() uintptr {
	return cl.Handle
}

// IsClosed 返回库是否已关闭
func (cl *CLib) IsClosed() bool {
	return cl.closed
}

// Close 关闭动态库
func (cl *CLib) Close() {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.closed {
		return
	}
	cl.closed = true
	CloseCLib(cl.Handle)
}

// CFunc 表示已绑定的 C 函数
type CFunc struct {
	Lib        *CLib
	Name       string
	Addr       uintptr   // 函数地址
	ParamTypes []CFFIType
	RetType    CFFIType
}

func (f *CFunc) Type() ObjectType { return CLIB_FUNC_OBJ }
func (f *CFunc) Inspect() string {
	params := ""
	for i, t := range f.ParamTypes {
		if i > 0 {
			params += ", "
		}
		params += cffiTypeName(t)
	}
	return fmt.Sprintf("C函数 %s(%s) -> %s", f.Name, params, cffiTypeName(f.RetType))
}
