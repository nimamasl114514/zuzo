//go:build !windows && cgo

package object

// clib_loader_unix.go — Unix (Linux/macOS/BSD) 动态库加载 (dlopen/dlsym/dlclose)
//
// 使用 CGO 调用 POSIX API，路径按 UTF-8 的 char* 传入。

/*
#cgo LDFLAGS: -ldl
#define _GNU_SOURCE
#include <dlfcn.h>
#include <stdlib.h>

static void* zuzo_load_lib(const char* path) {
  return dlopen(path, RTLD_LAZY | RTLD_GLOBAL);
}
static void* zuzo_get_sym(void* handle, const char* name) {
  return dlsym(handle, name);
}
static void zuzo_free_lib(void* handle) {
  dlclose(handle);
}
static const char* zuzo_dlerror(void) {
  return dlerror();
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// LoadCLib 加载动态库，返回句柄
func LoadCLib(path string) (uintptr, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	handle := C.zuzo_load_lib(cPath)
	if handle == nil {
		errMsg := C.GoString(C.zuzo_dlerror())
		if errMsg == "" {
			errMsg = "未知错误"
		}
		return 0, fmt.Errorf("dlopen 失败: 无法加载 %s: %s", path, errMsg)
	}
	return uintptr(unsafe.Pointer(handle)), nil
}

// LookupCSymbol 查找库中的符号地址
func LookupCSymbol(handle uintptr, name string) (uintptr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	addr := C.zuzo_get_sym(unsafe.Pointer(handle), cName)
	if addr == nil {
		errMsg := C.GoString(C.zuzo_dlerror())
		if errMsg == "" {
			errMsg = "未找到符号"
		}
		return 0, fmt.Errorf("dlsym 失败: 未找到符号 %s: %s", name, errMsg)
	}
	return uintptr(addr), nil
}

// CloseCLib 关闭动态库
func CloseCLib(handle uintptr) {
	if handle == 0 {
		return
	}
	C.zuzo_free_lib(unsafe.Pointer(handle))
}

// stringToCPtr 将 Go 字符串转换为 C 风格的 null-terminated 字符串指针
// 返回的指针需要由调用者释放 (C.free)
func stringToCPtr(s string) (unsafe.Pointer, error) {
	return unsafe.Pointer(C.CString(s)), nil
}
