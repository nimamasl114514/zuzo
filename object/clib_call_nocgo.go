//go:build !cgo

package object

// clib_call_nocgo.go — 关闭 CGO 时 C 函数指针调用器的桩实现
//
// 与 clib_call.go 保持完全相同的函数签名，统一返回“不支持”错误。

import "fmt"

// CallCFunc 调用 C 函数指针（CGO 关闭时永远失败）
func CallCFunc(fn uintptr, paramTypes []CFFIType, retType CFFIType, args []int64) (uint64, float64, bool, error) {
	return 0, 0, false, fmt.Errorf("%s: 无法调用函数地址 0x%x", errNoCGO, fn)
}

// CallCFuncWithStrings 调用可能包含 string 参数的 C 函数（CGO 关闭时永远失败）
func CallCFuncWithStrings(fn uintptr, paramTypes []CFFIType, retType CFFIType, goArgs []interface{}) (uint64, float64, bool, error) {
	return 0, 0, false, fmt.Errorf("%s: 无法调用函数地址 0x%x", errNoCGO, fn)
}

// ReadCString 读取 C 字符串（CGO 关闭时返回空串）
func ReadCString(ptr uintptr) string {
	return ""
}
