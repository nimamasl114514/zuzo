package main

// profile_cpu.go — CPU 观测通道（性能优化用，零依赖）
//
// 用法：ZUZO_CPUPROFILE=/abs/path/cpu.prof ./zuzo.exe 目标文件.hy
// 采样区间覆盖「解析 + 类型推断 + SAP 编译 + 执行」全流程，
// 正常结束路径会在 handleResult 之后 StopCPUProfile 并把文件刷盘。
//
// 设计要点：
//   - 只在环境变量存在时启用，不影响正常运行路径
//   - StopCPUProfile 放在正常出口，os.Exit 的错误路径不采样（剖析无需错误路径）

import (
	"os"
	"runtime/pprof"
)

var cpuProfileFile *os.File

// startCPUProfile 按环境变量 ZUZO_CPUPROFILE 启动 CPU 采样
func startCPUProfile() {
	path := os.Getenv("ZUZO_CPUPROFILE")
	if path == "" {
		return
	}
	f, err := os.Create(path)
	if err != nil {
		return
	}
	if err := pprof.StartCPUProfile(f); err != nil {
		f.Close()
		return
	}
	cpuProfileFile = f
}

// stopCPUProfile 停止采样并刷盘；幂等，可多次调用
func stopCPUProfile() {
	if cpuProfileFile == nil {
		return
	}
	pprof.StopCPUProfile()
	cpuProfileFile.Close()
	cpuProfileFile = nil
}