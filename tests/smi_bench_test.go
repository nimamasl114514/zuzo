package tests

// smi_bench_test.go — Smi-Tagging（64 位小整数标记）正确性测试 + 性能基准
//
// 对比四层性能：
//   1. 传统 AST 树遍历（无 SAP）
//   2. SAP Fused Loop（Level 1-2，闭包熔合）
//   3. SAP Native Int64 Loop（Level 3，原生 int64 槽位）
//   4. SAP Smi-Tagged Native Loop（Level 4，64 位标记值，零堆分配）

import (
	"testing"

	"zuzo/evaluator"
	"zuzo/object"
)

// ==================== Smi 编码正确性测试 ====================

func TestSmiEncoding(t *testing.T) {
	testCases := []struct {
		name  string
		value int64
	}{
		{"零", 0},
		{"正小数", 42},
		{"负小数", -42},
		{"最大缓存值", 511},
		{"最小缓存值", -512},
		{"超出缓存_正", 1000},
		{"超出缓存_负", -1000},
		{"大数", 4611686018427387903},   // 2^62-1，Smi 最大值
		{"大负数", -4611686018427387904}, // -2^62，Smi 最小值
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			v := object.Smi(tc.value)
			if !v.IsSmi() {
				t.Fatalf("Smi(%d) 返回的 Value 不是 Smi", tc.value)
			}
			got := v.AsSmi()
			if got != tc.value {
				t.Fatalf("编码-解码不一致：输入 %d，得到 %d", tc.value, got)
			}
		})
	}
}

func TestSmiNilValue(t *testing.T) {
	var v object.Value // 零值
	if v.IsSmi() {
		t.Fatal("零值 Value 不应是 Smi")
	}
	if !v.IsNil() {
		t.Fatal("零值 Value 应是 IsNil()")
	}
}

func TestSmiAsObjectCache(t *testing.T) {
	// 缓存范围内的值应该返回缓存对象（指针相同）
	for n := int64(-512); n <= 511; n++ {
		v := object.Smi(n)
		obj1 := v.AsObject()
		obj2 := v.AsObject()
		if obj1 != obj2 {
			t.Fatalf("缓存值 %d 的 AsObject 返回了不同指针", n)
		}
		intObj, ok := obj1.(*object.Integer)
		if !ok {
			t.Fatalf("AsObject 未返回 *Integer")
		}
		if intObj.Value != n {
			t.Fatalf("缓存值不匹配：期望 %d，得到 %d", n, intObj.Value)
		}
	}

	// 缓存范围外的值应该返回新对象（值正确即可）
	v := object.Smi(1000)
	obj := v.AsObject()
	intObj, ok := obj.(*object.Integer)
	if !ok {
		t.Fatalf("缓存范围外 AsObject 未返回 *Integer")
	}
	if intObj.Value != 1000 {
		t.Fatalf("缓存范围外值不匹配：期望 1000，得到 %d", intObj.Value)
	}
}

// ==================== Smi Native Loop 正确性测试 ====================

func TestSmiLoopCorrectness(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			sum = sum + i
		}
	`
	program, inf := parseAndInfer(code)

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, env)

	sumVal, ok := env.Get("sum")
	if !ok {
		t.Fatal("sum 未定义")
	}
	result := sumVal.(*object.Integer).Value
	expected := int64(4950) // 0+1+...+99
	if result != expected {
		t.Errorf("期望 sum=%d，得到 %d", expected, result)
	}
}

func TestSmiLoop1000(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 1000; i = i + 1 {
			sum = sum + i
		}
	`
	program, inf := parseAndInfer(code)

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, env)

	sumVal, _ := env.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != 499500 {
		t.Errorf("期望 sum=499500，得到 %d", result)
	}
}

func TestSmiLoopMultiply(t *testing.T) {
	code := `
		变量 product = 1
		变量 i = 1
		对于 i = 1; i < 6; i = i + 1 {
			product = product * i
		}
	`
	program, inf := parseAndInfer(code)

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, env)

	prodVal, _ := env.Get("product")
	result := prodVal.(*object.Integer).Value
	if result != 120 { // 1*1*2*3*4*5 = 120
		t.Errorf("期望 product=120，得到 %d", result)
	}
}

func TestSmiLoopMultiStmt(t *testing.T) {
	code := `
		变量 a = 0
		变量 b = 0
		变量 c = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			a = a + i
			b = b + a
			c = c + b
		}
	`
	program, inf := parseAndInfer(code)

	// 传统路径
	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	tradEnv := evaluator.NewEnvironment()
	evaluator.Eval(program, tradEnv)
	evaluator.SetSAPCompiler(oldSAP)

	tradA, _ := tradEnv.Get("a")
	tradB, _ := tradEnv.Get("b")
	tradC, _ := tradEnv.Get("c")

	// Smi 路径
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	smiEnv := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, smiEnv)

	smiA, _ := smiEnv.Get("a")
	smiB, _ := smiEnv.Get("b")
	smiC, _ := smiEnv.Get("c")

	// 对比结果
	if smiA.(*object.Integer).Value != tradA.(*object.Integer).Value {
		t.Errorf("a 不一致：传统=%d，Smi=%d",
			tradA.(*object.Integer).Value, smiA.(*object.Integer).Value)
	}
	if smiB.(*object.Integer).Value != tradB.(*object.Integer).Value {
		t.Errorf("b 不一致：传统=%d，Smi=%d",
			tradB.(*object.Integer).Value, smiB.(*object.Integer).Value)
	}
	if smiC.(*object.Integer).Value != tradC.(*object.Integer).Value {
		t.Errorf("c 不一致：传统=%d，Smi=%d",
			tradC.(*object.Integer).Value, smiC.(*object.Integer).Value)
	}
}

func TestSmiLoopLargeRange(t *testing.T) {
	// 测试超出小整数缓存范围的大数
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 10000; i = i + 1 {
			sum = sum + i
		}
	`
	program, inf := parseAndInfer(code)

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, env)

	sumVal, _ := env.Get("sum")
	result := sumVal.(*object.Integer).Value
	// sum = 0+1+...+9999 = 9999*10000/2 = 49995000
	if result != 49995000 {
		t.Errorf("期望 sum=49995000，得到 %d", result)
	}
}

func TestSmiLoopSubtraction(t *testing.T) {
	// 测试递减循环
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 100; i > 0; i = i - 1 {
			sum = sum + i
		}
	`
	program, inf := parseAndInfer(code)

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, env)

	sumVal, _ := env.Get("sum")
	result := sumVal.(*object.Integer).Value
	// sum = 100+99+...+1 = 5050
	if result != 5050 {
		t.Errorf("期望 sum=5050，得到 %d", result)
	}
}

// ==================== Smi 性能基准测试 ====================

// BenchmarkSmiLoopNative1K Smi Native Loop：1000 次迭代
func BenchmarkSmiLoopNative1K(b *testing.B) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 1000; i = i + 1 {
			sum = sum + i
		}
	`
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

// BenchmarkSmiLoopNative10K Smi Native Loop：10000 次迭代
func BenchmarkSmiLoopNative10K(b *testing.B) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 10000; i = i + 1 {
			sum = sum + i
		}
	`
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

// BenchmarkSmiLoopMultiStmt Smi Native Loop：多语句循环体 1000 次迭代
func BenchmarkSmiLoopMultiStmt(b *testing.B) {
	code := `
		变量 a = 0
		变量 b = 0
		变量 c = 0
		变量 i = 0
		对于 i = 0; i < 1000; i = i + 1 {
			a = a + i
			b = b + a
			c = c + b
		}
	`
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

// BenchmarkSmiLoopMultiply Smi Native Loop：乘法循环 1000 次
func BenchmarkSmiLoopMultiply(b *testing.B) {
	code := `
		变量 product = 1
		变量 i = 1
		对于 i = 1; i < 1000; i = i + 1 {
			product = product * i
		}
	`
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
