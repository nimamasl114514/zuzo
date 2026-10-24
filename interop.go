package main

// interop.go — 跨语言无缝调用（纯 .hy ↔ 纯 .py，不依赖 .hypy 混合文件）
//
// 立场（用户 2026-10-24 指令：「zuzo 支持无缝调用 py，py 无缝调用 zuzo」）：
// 复用 mix.Bridge 的双向通道（对象转换 + 函数代理），把它从「混合文件专用」
// 提升为「任何入口都装」：
//
//	纯 .hy：runCode 里 installZuzoToPy(env)
//	        → zuzo 侧得到 py求值 / py执行 / py导入 / py调用 / py获取 / py设置
//	        → py 侧得到 zuzo 模块（调用/获取/设置）
//	纯 .py：pyrt.InteropHook 里 installPyToZuzo(ip)
//	        → py 侧 import zuzo 即可调用 zuzo 侧的函数/变量
//
// 两侧共享同一个 zuzo Environment（crossZuzoEnv），保证 mutating 可见。

import (
	"io"
	"os"

	"zuzo/evaluator"
	"zuzo/mix"
	"zuzo/object"
	pyrt "zuzo/py/runtime"
)

var (
	// crossPyInterp 全局 Python 解释器（惰性创建，.hy 调用 py 时用）
	crossPyInterp *pyrt.Interp
	// crossZuzoEnv 全局 zuzo 环境（.py 调用 zuzo 时用）
	crossZuzoEnv *object.Environment
	// crossBridge 已安装的桥（避免重复安装）
	crossBridge *mix.Bridge
)

// ensureCrossPyInterp 返回全局 Python 解释器
func ensureCrossPyInterp() *pyrt.Interp {
	if crossPyInterp == nil {
		crossPyInterp = pyrt.New()
		crossPyInterp.Stdout = os.Stdout
		crossPyInterp.Stderr = os.Stderr
	}
	return crossPyInterp
}

// installCrossBridge 安装双向桥（幂等）
// env 为 zuzo 侧的环境；为 nil 时若已有环境则复用，否则新建一个带内置的环境
func installCrossBridge(env *object.Environment, out, errw io.Writer) *mix.Bridge {
	if crossBridge != nil {
		return crossBridge
	}
	if env == nil {
		if crossZuzoEnv != nil {
			env = crossZuzoEnv
		} else {
			env = evaluator.NewEnvironment()
		}
	}
	crossZuzoEnv = env
	ip := ensureCrossPyInterp()
	if out == nil {
		out = os.Stdout
	}
	if errw == nil {
		errw = os.Stderr
	}
	b := mix.NewBridge(ip, env, out, errw)
	b.Install()
	crossBridge = b
	return b
}

// initInterop 在启动时注册：纯 .py 入口也会装桥（通过 pyrt.InteropHook）
func initInterop() {
	pyrt.InteropHook = func(ip *pyrt.Interp) {
		crossPyInterp = ip
		installCrossBridge(nil, os.Stdout, os.Stderr)
	}
}
