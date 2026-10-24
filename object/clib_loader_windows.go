//go:build windows && cgo

package object

// clib_loader_windows.go — Windows 动态库加载 (LoadLibraryW/GetProcAddress)
//
// 使用 CGO 调用 Win32 API，路径统一转换为 UTF-16 宽字符，保持原有 wchar_t 行为。

/*
#cgo windows CFLAGS: -DUNICODE
#include <windows.h>
#include <stdlib.h>

// Windows: LoadLibraryW + GetProcAddress + FreeLibrary
static void* zuzo_load_lib(const wchar_t* path) {
  return (void*)LoadLibraryW(path);
}
static void* zuzo_get_sym(void* handle, const char* name) {
  return (void*)GetProcAddress((HMODULE)handle, name);
}
static void zuzo_free_lib(void* handle) {
  FreeLibrary((HMODULE)handle);
}
static const wchar_t* zuzo_widen(const char* utf8) {
  if (!utf8) return NULL;
  int len = MultiByteToWideChar(CP_UTF8, 0, utf8, -1, NULL, 0);
  if (len <= 0) return NULL;
  wchar_t* wbuf = (wchar_t*)malloc(len * sizeof(wchar_t));
  if (!wbuf) return NULL;
  MultiByteToWideChar(CP_UTF8, 0, utf8, -1, wbuf, len);
  return wbuf;  // 调用者负责 free
}
static void zuzo_wide_free(const wchar_t* p) {
  free((void*)p);
}
// Windows 上不需要 dlerror，提供空实现保持符号一致
static const char* zuzo_dlerror(void) {
  return "unknown Windows error";
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

	wPath := C.zuzo_widen(cPath)
	if wPath == nil {
		return 0, fmt.Errorf("无法将路径转换为宽字符: %s", path)
	}
	defer C.zuzo_wide_free(wPath)

	handle := C.zuzo_load_lib(wPath)
	if handle == nil {
		return 0, fmt.Errorf("LoadLibraryW 失败: 无法加载 %s", path)
	}
	return uintptr(unsafe.Pointer(handle)), nil
}

// LookupCSymbol 查找库中的符号地址
func LookupCSymbol(handle uintptr, name string) (uintptr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	addr := C.zuzo_get_sym(unsafe.Pointer(handle), cName)
	if addr == nil {
		return 0, fmt.Errorf("GetProcAddress 失败: 未找到符号 %s", name)
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
