package object

// clib_loader.go — 跨平台动态库加载 (dlopen/LoadLibrary)
//
// 使用 CGO 调用平台原生 API，在 Windows 上用 LoadLibraryW/GetProcAddress，
// 在 Unix 上用 dlopen/dlsym/dlclose。
// CGO 仅用于 FFI 子系统，不影响其他模块的纯 Go 编译。

/*
#cgo windows CFLAGS: -DUNICODE
#cgo !windows CFLAGS:
#cgo !windows LDFLAGS: -ldl

#if defined(_WIN32) || defined(_WIN64)
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
#else
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
  // Unix 上不需要 widen/wide_free，提供空实现保持符号一致
  static const wchar_t* zuzo_widen(const char* utf8) {
    (void)utf8;
    return NULL;
  }
  static void zuzo_wide_free(const wchar_t* p) {
    (void)p;
  }
#endif
*/
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

// LoadCLib 加载动态库，返回句柄
func LoadCLib(path string) (uintptr, error) {
	switch runtime.GOOS {
	case "windows":
		return loadLibraryWindows(path)
	default:
		return dlopenUnix(path)
	}
}

// LookupCSymbol 查找库中的符号地址
func LookupCSymbol(handle uintptr, name string) (uintptr, error) {
	switch runtime.GOOS {
	case "windows":
		return getProcAddressWindows(handle, name)
	default:
		return dlsymUnix(handle, name)
	}
}

// CloseCLib 关闭动态库
func CloseCLib(handle uintptr) {
	switch runtime.GOOS {
	case "windows":
		freeLibraryWindows(handle)
	default:
		dlcloseUnix(handle)
	}
}

// ==================== Windows 实现 ====================

func loadLibraryWindows(path string) (uintptr, error) {
	if runtime.GOOS != "windows" {
		return 0, fmt.Errorf("LoadLibrary 仅在 Windows 平台可用")
	}
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

// internal helper kept for reference
func getProcAddressWindows(handle uintptr, name string) (uintptr, error) {
	if runtime.GOOS != "windows" {
		return 0, fmt.Errorf("GetProcAddress 仅在 Windows 平台可用")
	}
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	h := unsafe.Pointer(handle)
	addr := C.zuzo_get_sym(h, cName)
	if addr == nil {
		return 0, fmt.Errorf("GetProcAddress 失败: 未找到符号 %s", name)
	}
	return uintptr(addr), nil
}

func freeLibraryWindows(handle uintptr) {
	if runtime.GOOS != "windows" || handle == 0 {
		return
	}
	C.zuzo_free_lib(unsafe.Pointer(handle))
}

// ==================== Unix (Linux/macOS/BSD) 实现 ====================

func dlopenUnix(path string) (uintptr, error) {
	cPath := C.CString(path)
	defer C.free(unsafe.Pointer(cPath))

	handle := C.zuzo_load_lib((*C.wchar_t)(unsafe.Pointer(cPath)))
	if handle == nil {
		errMsg := C.GoString(C.zuzo_dlerror())
		if errMsg == "" {
			errMsg = "未知错误"
		}
		return 0, fmt.Errorf("dlopen 失败: 无法加载 %s: %s", path, errMsg)
	}
	return uintptr(unsafe.Pointer(handle)), nil
}

func dlsymUnix(handle uintptr, name string) (uintptr, error) {
	cName := C.CString(name)
	defer C.free(unsafe.Pointer(cName))

	h := unsafe.Pointer(handle)
	addr := C.zuzo_get_sym(h, cName)
	if addr == nil {
		errMsg := C.GoString(C.zuzo_dlerror())
		if errMsg == "" {
			errMsg = "未找到符号"
		}
		return 0, fmt.Errorf("dlsym 失败: 未找到符号 %s: %s", name, errMsg)
	}
	return uintptr(addr), nil
}

func dlcloseUnix(handle uintptr) {
	if handle == 0 {
		return
	}
	C.zuzo_free_lib(unsafe.Pointer(handle))
}

// stringToCPtr 将 Go 字符串转换为 C 风格的 null-terminated 字符串指针
// 返回的指针需要由调用者释放 (C.free)
func stringToCPtr(s string) (unsafe.Pointer, error) {
	cStr := C.CString(s)
	return unsafe.Pointer(cStr), nil
}
