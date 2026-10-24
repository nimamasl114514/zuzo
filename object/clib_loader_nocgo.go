//go:build !cgo

package object

// clib_loader_nocgo.go — 关闭 CGO 时的 FFI 桩实现
//
// CGO_ENABLED=0 时（如纯静态交叉编译 GOOS=linux），FFI 子系统不可用，
// 这里提供与平台实现一致的符号，统一返回明确的“不支持”错误，
// 保证整个仓库仍可编译与运行，只是调用 C 库时会得到可读的错误。

import (
	"fmt"
	"unsafe"
)

const errNoCGO = "CGO 未启用，当前构建不支持动态库加载与 C 函数调用"

// LoadCLib 加载动态库（CGO 关闭时永远失败）
func LoadCLib(path string) (uintptr, error) {
	return 0, fmt.Errorf("%s: 无法加载 %s", errNoCGO, path)
}

// LookupCSymbol 查找符号（CGO 关闭时永远失败）
func LookupCSymbol(handle uintptr, name string) (uintptr, error) {
	return 0, fmt.Errorf("%s: 无法查找符号 %s", errNoCGO, name)
}

// CloseCLib 关闭动态库（CGO 关闭时为空操作）
func CloseCLib(handle uintptr) {}

// stringToCPtr 占位实现，保持符号一致
func stringToCPtr(s string) (unsafe.Pointer, error) {
	return nil, fmt.Errorf("%s", errNoCGO)
}
