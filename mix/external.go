package mix

// external.go — 外部 CPython 运行器接口（第32轮 · 2026-11-01）
//
// 背景：mix 包只认识进程内的纯 Go Python 运行时（pyrt.Interp）。numpy/pandas 这类
// C 扩展在进程内永远加载不了，必须交给外部真 CPython（pyext 子进程桥）。
//
// 本文件只放「接口 + 注入点 + 错误判别」，具体实现由宿主（main/interop.go）注入，
// 避免 mix → pyext 的硬依赖，也便于单测替换。

import (
	"strings"

	"zuzo/object"
)

// PyExternal 外部 CPython 运行器
type PyExternal interface {
	// Active 是否已处于粘性子进程模式
	Active() bool
	// Activate 激活子进程（惰性拉起），成功后 Active() 为真
	Activate() error
	// Exec 在子进程常驻 globals 执行代码
	Exec(code string) error
	// Eval 在子进程常驻 globals 求值表达式
	Eval(expr string) (object.Object, error)
	// Lookup 按名字取子进程 globals（找不到返回 false）
	Lookup(name string) (object.Object, bool)
	// CallName 按名字调用子进程 globals 里的可调用对象
	CallName(name string, args []object.Object) (object.Object, error)
	// SetName 设置子进程 globals 变量
	SetName(name string, v object.Object) error
	// Native 把远端句柄转成 zuzo 原生值
	Native(v object.Object) (object.Object, error)
}

// PyExt 宿主注入的外部运行器；nil = 未启用（纯进程内语义）
var PyExt PyExternal

// isCExtImportError 判定「这条 ImportError 是不是因为 C 扩展需要真 CPython」
// 判据来自 py/runtime/import.go: "zuzo: C extension module '%s' needs external CPython"
func isCExtImportError(msg string) bool {
	return strings.Contains(msg, "needs external CPython") ||
		strings.Contains(msg, "C extension module")
}

// shouldFallback 判定「进程内失败是否值得交给真 CPython 重试一次」
// 实测（第32轮）：numpy 的 __init__.py 是纯 Python，真正的 .pyd 在更深的
// numpy.core._multiarray_umath 里，报错文案是 AttributeError: module 'os' has no
// attribute 'add_dll_directory' —— 光看 "C extension" 抓不到，必须连带判。
func shouldFallback(code, msg string) bool {
	if isCExtImportError(msg) {
		return true
	}
	lc := strings.ToLower(msg)
	if strings.Contains(lc, "add_dll_directory") ||
		strings.Contains(lc, "site-packages") ||
		strings.Contains(lc, ".pyd") ||
		strings.Contains(lc, "delvewheel") {
		return true
	}
	if strings.Contains(code, "import") &&
		(strings.Contains(lc, "no module named") ||
			strings.Contains(lc, "modulenotfounderror") ||
			strings.Contains(lc, "importerror") ||
			strings.Contains(lc, "attributeerror")) {
		return true
	}
	return false
}
