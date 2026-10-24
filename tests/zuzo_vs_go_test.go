package tests

// zuzo_vs_go_test.go — Zuzo Level 5 (De Bruijn) vs 原生 Go 循环性能对比
//
// 用相同的算法（0+1+...+N-1），分别用 Zuzo 和原生 Go 实现，
// 量化从解释器到编译器五级优化后，距离 Go 原生还有多远。

import (
	"testing"

	"zuzo/evaluator"
)

// --- 原生 Go 基准 ---

// GoNativeLoop1K 纯 Go for 循环 1000 次求和
func GoNativeLoop1K() int64 {
	var sum int64
	for i := int64(0); i < 1000; i++ {
		sum += i
	}
	return sum
}

// GoNativeLoop10K 纯 Go for 循环 10000 次求和
func GoNativeLoop10K() int64 {
	var sum int64
	for i := int64(0); i < 10000; i++ {
		sum += i
	}
	return sum
}

// GoNativeLoop100K 纯 Go for 循环 100000 次求和
func GoNativeLoop100K() int64 {
	var sum int64
	for i := int64(0); i < 100000; i++ {
		sum += i
	}
	return sum
}

// GoNativeLoopMultiStmt10K 纯 Go 多语句循环体 10000 次
func GoNativeLoopMultiStmt10K() (int64, int64, int64) {
	var a, b, c int64
	for i := int64(0); i < 10000; i++ {
		a += i
		b += a
		c += b
	}
	return a, b, c
}

// GoNativeLoopMultiply1K 纯 Go 乘法循环 1000 次
func GoNativeLoopMultiply1K() int64 {
	var product int64 = 1
	for i := int64(1); i < 1000; i++ {
		product *= i
	}
	return product
}

// --- 基准测试 ---

func BenchmarkGoNativeLoop1K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GoNativeLoop1K()
	}
}

func BenchmarkGoNativeLoop10K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GoNativeLoop10K()
	}
}

func BenchmarkGoNativeLoop100K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GoNativeLoop100K()
	}
}

func BenchmarkGoNativeLoopMultiStmt10K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GoNativeLoopMultiStmt10K()
	}
}

func BenchmarkGoNativeLoopMultiply1K(b *testing.B) {
	for i := 0; i < b.N; i++ {
		GoNativeLoopMultiply1K()
	}
}

// --- Zuzo Level 5 对应基准 ---

func BenchmarkZuzoL5Loop1K(b *testing.B) {
	code := `变量 sum = 0; 变量 i = 0; 对于 i = 0; i < 1000; i = i + 1 { sum = sum + i }`
	program, inf := parseAndInfer(code)
	oldSAP := evaluator.GetSAPCompiler()
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
		evaluator.Eval(program, env)
	}
}

func BenchmarkZuzoL5Loop10K(b *testing.B) {
	code := `变量 sum = 0; 变量 i = 0; 对于 i = 0; i < 10000; i = i + 1 { sum = sum + i }`
	program, inf := parseAndInfer(code)
	oldSAP := evaluator.GetSAPCompiler()
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
		evaluator.Eval(program, env)
	}
}

func BenchmarkZuzoL5Loop100K(b *testing.B) {
	code := `变量 sum = 0; 变量 i = 0; 对于 i = 0; i < 100000; i = i + 1 { sum = sum + i }`
	program, inf := parseAndInfer(code)
	oldSAP := evaluator.GetSAPCompiler()
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
		evaluator.Eval(program, env)
	}
}

func BenchmarkZuzoL5LoopMultiStmt10K(b *testing.B) {
	code := `变量 a = 0; 变量 b = 0; 变量 c = 0; 变量 i = 0; 对于 i = 0; i < 10000; i = i + 1 { a = a + i; b = b + a; c = c + b }`
	program, inf := parseAndInfer(code)
	oldSAP := evaluator.GetSAPCompiler()
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
		evaluator.Eval(program, env)
	}
}

func BenchmarkZuzoL5LoopMultiply1K(b *testing.B) {
	code := `变量 product = 1; 变量 i = 1; 对于 i = 1; i < 1000; i = i + 1 { product = product * i }`
	program, inf := parseAndInfer(code)
	oldSAP := evaluator.GetSAPCompiler()
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
		evaluator.Eval(program, env)
	}
}
