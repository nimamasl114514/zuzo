package tests

// debruijn_test.go — De Bruijn Index + 扁平帧栈 边界测试 + 性能基准
//
// 测试 Level 5 (De Bruijn Native Loop) 的正确性和性能：
//   1. Context 基础操作（Get/Set/PushFrame/PopFrame）
//   2. De Bruijn 索引解析正确性
//   3. Level 5 循环正确性（与 Level 4 / 传统路径对比）
//   4. Level 5 循环边界场景
//   5. 性能基准：Level 5 vs Level 4 vs 传统

import (
	"testing"

	"zuzo/evaluator"
	"zuzo/object"
)

// ================================================================
// 第一部分：Context 基础操作
// ================================================================

func TestContextBasicGetSet(t *testing.T) {
	ctx := object.NewContext(5)

	// 写入 Smi 并读回
	ctx.SetSmi(0, 0, 42)
	ctx.SetSmi(0, 1, -100)
	ctx.SetSmi(0, 2, 0)

	if ctx.GetSmi(0, 0) != 42 {
		t.Fatal("slot 0 值不正确")
	}
	if ctx.GetSmi(0, 1) != -100 {
		t.Fatal("slot 1 值不正确")
	}
	if ctx.GetSmi(0, 2) != 0 {
		t.Fatal("slot 2 值不正确")
	}
}

func TestContextUninitializedSlot(t *testing.T) {
	ctx := object.NewContext(5)
	v := ctx.Get(0, 3) // 未初始化
	if v.IsSmi() {
		t.Fatal("未初始化的槽位不应是 Smi")
	}
	if !v.IsNil() {
		t.Fatal("未初始化的槽位应是零值")
	}
}

func TestContextPushPopFrame(t *testing.T) {
	ctx := object.NewContext(3)

	// 在根帧写入
	ctx.SetSmi(0, 0, 100)

	// 压入新帧
	ctx.PushFrame(5)
	ctx.SetSmi(0, 0, 200) // 当前帧 slot 0

	// depth=1 访问根帧
	if ctx.GetSmi(1, 0) != 100 {
		t.Fatal("depth=1 应访问根帧的 slot 0 = 100")
	}
	// depth=0 访问当前帧
	if ctx.GetSmi(0, 0) != 200 {
		t.Fatal("depth=0 应访问当前帧的 slot 0 = 200")
	}

	// 弹出帧
	ctx.PopFrame()
	if ctx.GetSmi(0, 0) != 100 {
		t.Fatal("弹出后 depth=0 应访问根帧的 slot 0 = 100")
	}
}

func TestContextMultiFrameDepth(t *testing.T) {
	ctx := object.NewContext(3)
	ctx.SetSmi(0, 0, 10) // 根帧

	ctx.PushFrame(3)
	ctx.SetSmi(0, 0, 20) // 第 1 帧

	ctx.PushFrame(3)
	ctx.SetSmi(0, 0, 30) // 第 2 帧

	// depth=0 → 当前帧
	if ctx.GetSmi(0, 0) != 30 {
		t.Fatal("depth=0 应为 30")
	}
	// depth=1 → 第 1 帧
	if ctx.GetSmi(1, 0) != 20 {
		t.Fatal("depth=1 应为 20")
	}
	// depth=2 → 根帧
	if ctx.GetSmi(2, 0) != 10 {
		t.Fatal("depth=2 应为 10")
	}

	ctx.PopFrame()
	ctx.PopFrame()
	if ctx.GetSmi(0, 0) != 10 {
		t.Fatal("全部弹出后应回到根帧 10")
	}
}

func TestContextFrameCount(t *testing.T) {
	ctx := object.NewContext(3)
	if ctx.FrameCount() != 1 {
		t.Fatal("初始应有 1 帧")
	}
	ctx.PushFrame(3)
	if ctx.FrameCount() != 2 {
		t.Fatal("PushFrame 后应有 2 帧")
	}
	ctx.PushFrame(3)
	if ctx.FrameCount() != 3 {
		t.Fatal("PushFrame 后应有 3 帧")
	}
	ctx.PopFrame()
	if ctx.FrameCount() != 2 {
		t.Fatal("PopFrame 后应有 2 帧")
	}
}

func TestContextGetObject(t *testing.T) {
	ctx := object.NewContext(3)
	ctx.SetSmi(0, 0, 42)

	obj := ctx.GetObject(0, 0)
	intObj, ok := obj.(*object.Integer)
	if !ok || intObj.Value != 42 {
		t.Fatalf("GetObject 返回不正确: %v", obj)
	}
}

func TestContextSetObject(t *testing.T) {
	ctx := object.NewContext(3)
	ctx.SetObject(0, 0, &object.Integer{Value: 999})

	if ctx.GetSmi(0, 0) != 999 {
		t.Fatal("SetObject 后 GetSmi 应返回 999")
	}
}

// ================================================================
// 第二部分：De Bruijn 循环正确性
// ================================================================

// dbEval 辅助：编译并执行代码（SAP 路径，启用 Context）
func dbEval(code string) *object.Environment {
	program, inf := parseAndInfer(code)

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, env)
	return env
}

// assertDbVsTrad 对比 De Bruijn 路径和传统路径的变量值
func assertDbVsTrad(t *testing.T, code, varName string) {
	t.Helper()

	dbEnv := dbEval(code)
	tradEnv := tradEval(code)

	dbVal, ok := dbEnv.Get(varName)
	if !ok {
		t.Fatalf("De Bruijn 路径：变量 %s 未定义", varName)
	}
	tradVal, ok := tradEnv.Get(varName)
	if !ok {
		t.Fatalf("传统路径：变量 %s 未定义", varName)
	}

	dbInt, ok1 := dbVal.(*object.Integer)
	tradInt, ok2 := tradVal.(*object.Integer)
	if !ok1 || !ok2 {
		t.Fatalf("变量 %s 不是 Integer：DB=%T, 传统=%T", varName, dbVal, tradVal)
	}

	if dbInt.Value != tradInt.Value {
		t.Errorf("变量 %s 不一致：DB=%d, 传统=%d", varName, dbInt.Value, tradInt.Value)
	}
}

func TestDeBruijnLoopBasicSum(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			sum = sum + i
		}
	`
	env := dbEval(code)
	sumVal, _ := env.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != 4950 {
		t.Errorf("期望 sum=4950，得到 %d", result)
	}
	assertDbVsTrad(t, code, "sum")
}

func TestDeBruijnLoop1000(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 1000; i = i + 1 {
			sum = sum + i
		}
	`
	env := dbEval(code)
	sumVal, _ := env.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != 499500 {
		t.Errorf("期望 sum=499500，得到 %d", result)
	}
	assertDbVsTrad(t, code, "sum")
}

func TestDeBruijnLoopMultiply(t *testing.T) {
	code := `
		变量 product = 1
		变量 i = 1
		对于 i = 1; i < 6; i = i + 1 {
			product = product * i
		}
	`
	env := dbEval(code)
	prodVal, _ := env.Get("product")
	result := prodVal.(*object.Integer).Value
	if result != 120 {
		t.Errorf("期望 product=120，得到 %d", result)
	}
	assertDbVsTrad(t, code, "product")
}

func TestDeBruijnLoopMultiStmt(t *testing.T) {
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
	env := dbEval(code)
	aVal, _ := env.Get("a")
	bVal, _ := env.Get("b")
	cVal, _ := env.Get("c")

	// 对比传统路径
	tradEnv := tradEval(code)
	tradA, _ := tradEnv.Get("a")
	tradB, _ := tradEnv.Get("b")
	tradC, _ := tradEnv.Get("c")

	if aVal.(*object.Integer).Value != tradA.(*object.Integer).Value {
		t.Errorf("a 不一致：DB=%d, 传统=%d", aVal.(*object.Integer).Value, tradA.(*object.Integer).Value)
	}
	if bVal.(*object.Integer).Value != tradB.(*object.Integer).Value {
		t.Errorf("b 不一致：DB=%d, 传统=%d", bVal.(*object.Integer).Value, tradB.(*object.Integer).Value)
	}
	if cVal.(*object.Integer).Value != tradC.(*object.Integer).Value {
		t.Errorf("c 不一致：DB=%d, 传统=%d", cVal.(*object.Integer).Value, tradC.(*object.Integer).Value)
	}
}

// ================================================================
// 第三部分：De Bruijn 循环边界场景
// ================================================================

func TestDeBruijnLoopZeroIterations(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 0; i = i + 1 {
			sum = sum + i
		}
	`
	env := dbEval(code)
	sumVal, _ := env.Get("sum")
	if sumVal.(*object.Integer).Value != 0 {
		t.Error("零迭代 sum 应为 0")
	}
	assertDbVsTrad(t, code, "sum")
}

func TestDeBruijnLoopAllConditions(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		expected int64
	}{
		{"小于", `变量 s = 0; 变量 i = 0; 对于 i = 0; i < 10; i = i + 1 { s = s + i }`, 45},
		{"小于等于", `变量 s = 0; 变量 i = 0; 对于 i = 0; i <= 10; i = i + 1 { s = s + i }`, 55},
		{"大于", `变量 s = 0; 变量 i = 0; 对于 i = 10; i > 0; i = i - 1 { s = s + i }`, 55},
		{"大于等于", `变量 s = 0; 变量 i = 0; 对于 i = 10; i >= 0; i = i - 1 { s = s + i }`, 55},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := dbEval(tc.code)
			sVal, _ := env.Get("s")
			result := sVal.(*object.Integer).Value
			if result != tc.expected {
				t.Errorf("期望 s=%d，得到 %d", tc.expected, result)
			}
			assertDbVsTrad(t, tc.code, "s")
		})
	}
}

func TestDeBruijnLoopLargeStep(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		expected int64
	}{
		{"步长10", `变量 s = 0; 变量 i = 0; 对于 i = 0; i < 100; i = i + 10 { s = s + i }`, 450},
		{"步长100", `变量 s = 0; 变量 i = 0; 对于 i = 0; i < 1000; i = i + 100 { s = s + i }`, 4500},
		{"负步长10", `变量 s = 0; 变量 i = 0; 对于 i = 100; i > 0; i = i - 10 { s = s + i }`, 550},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env := dbEval(tc.code)
			sVal, _ := env.Get("s")
			result := sVal.(*object.Integer).Value
			if result != tc.expected {
				t.Errorf("期望 s=%d，得到 %d", tc.expected, result)
			}
			assertDbVsTrad(t, tc.code, "s")
		})
	}
}

func TestDeBruijnLoopNegativeInit(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = -10; i < 10; i = i + 1 {
			sum = sum + i
		}
	`
	env := dbEval(code)
	sumVal, _ := env.Get("sum")
	if sumVal.(*object.Integer).Value != -10 {
		t.Errorf("期望 sum=-10，得到 %d", sumVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "sum")
}

func TestDeBruijnLoopVariableFinalValue(t *testing.T) {
	code := `
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 { }
	`
	env := dbEval(code)
	iVal, _ := env.Get("i")
	if iVal.(*object.Integer).Value != 100 {
		t.Errorf("期望 i=100，得到 %d", iVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "i")
}

func TestDeBruijnLoopVarUsedAfter(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 50; i = i + 1 {
			sum = sum + i
		}
		变量 final = i
	`
	env := dbEval(code)
	finalVal, _ := env.Get("final")
	if finalVal.(*object.Integer).Value != 50 {
		t.Errorf("期望 final=50，得到 %d", finalVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "sum")
	assertDbVsTrad(t, code, "final")
}

func TestDeBruijnConsecutiveLoops(t *testing.T) {
	code := `
		变量 total = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			total = total + i
		}
		变量 j = 0
		对于 j = 0; j < 100; j = j + 1 {
			total = total + j
		}
	`
	env := dbEval(code)
	totalVal, _ := env.Get("total")
	if totalVal.(*object.Integer).Value != 9900 {
		t.Errorf("期望 total=9900，得到 %d", totalVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "total")
}

func TestDeBruijnLoopVariableCopy(t *testing.T) {
	code := `
		变量 a = 0
		变量 b = 0
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			b = i
			a = b
		}
	`
	env := dbEval(code)
	aVal, _ := env.Get("a")
	bVal, _ := env.Get("b")
	if aVal.(*object.Integer).Value != 9 {
		t.Errorf("期望 a=9，得到 %d", aVal.(*object.Integer).Value)
	}
	if bVal.(*object.Integer).Value != 9 {
		t.Errorf("期望 b=9，得到 %d", bVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "a")
	assertDbVsTrad(t, code, "b")
}

func TestDeBruijnLoopNegativeAccumulation(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			sum = sum - i
		}
	`
	env := dbEval(code)
	sumVal, _ := env.Get("sum")
	if sumVal.(*object.Integer).Value != -4950 {
		t.Errorf("期望 sum=-4950，得到 %d", sumVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "sum")
}

func TestDeBruijnLoopLargeAccumulation(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 100000; i = i + 1 {
			sum = sum + i
		}
	`
	env := dbEval(code)
	sumVal, _ := env.Get("sum")
	if sumVal.(*object.Integer).Value != 4999950000 {
		t.Errorf("期望 sum=4999950000，得到 %d", sumVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "sum")
}

func TestDeBruijnLoopThreeVarCrossAssign(t *testing.T) {
	code := `
		变量 a = 0
		变量 b = 1
		变量 c = 0
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			c = a
			a = b
			b = c + a
		}
	`
	env := dbEval(code)
	aVal, _ := env.Get("a")
	bVal, _ := env.Get("b")
	cVal, _ := env.Get("c")
	if aVal.(*object.Integer).Value != 55 {
		t.Errorf("期望 a=55，得到 %d", aVal.(*object.Integer).Value)
	}
	if bVal.(*object.Integer).Value != 89 {
		t.Errorf("期望 b=89，得到 %d", bVal.(*object.Integer).Value)
	}
	if cVal.(*object.Integer).Value != 34 {
		t.Errorf("期望 c=34，得到 %d", cVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "a")
	assertDbVsTrad(t, code, "b")
	assertDbVsTrad(t, code, "c")
}

func TestDeBruijnLoopProductExceedsCache(t *testing.T) {
	code := `
		变量 product = 1
		变量 i = 1
		对于 i = 1; i < 20; i = i + 1 {
			product = product * i
		}
	`
	env := dbEval(code)
	prodVal, _ := env.Get("product")
	if prodVal.(*object.Integer).Value != 121645100408832000 {
		t.Errorf("期望 product=121645100408832000，得到 %d", prodVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "product")
}

func TestDeBruijnLoopSwapVariables(t *testing.T) {
	code := `
		变量 a = 1
		变量 b = 100
		变量 i = 0
		对于 i = 0; i < 5; i = i + 1 {
			变量 tmp = a
			a = b
			b = tmp
		}
	`
	env := dbEval(code)
	aVal, _ := env.Get("a")
	bVal, _ := env.Get("b")
	if aVal.(*object.Integer).Value != 100 {
		t.Errorf("期望 a=100，得到 %d", aVal.(*object.Integer).Value)
	}
	if bVal.(*object.Integer).Value != 1 {
		t.Errorf("期望 b=1，得到 %d", bVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "a")
	assertDbVsTrad(t, code, "b")
}

func TestDeBruijnLoopMultipleAccumulators(t *testing.T) {
	code := `
		变量 evens = 0
		变量 odds = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			evens = evens + i
			odds = odds + i + 1
		}
	`
	env := dbEval(code)
	eVal, _ := env.Get("evens")
	oVal, _ := env.Get("odds")
	if eVal.(*object.Integer).Value != 4950 {
		t.Errorf("期望 evens=4950，得到 %d", eVal.(*object.Integer).Value)
	}
	if oVal.(*object.Integer).Value != 5050 {
		t.Errorf("期望 odds=5050，得到 %d", oVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "evens")
	assertDbVsTrad(t, code, "odds")
}

func TestDeBruijnLoopSubConst(t *testing.T) {
	code := `
		变量 x = 1000
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			x = x - 5
		}
	`
	env := dbEval(code)
	xVal, _ := env.Get("x")
	if xVal.(*object.Integer).Value != 500 {
		t.Errorf("期望 x=500，得到 %d", xVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "x")
}

func TestDeBruijnLoopMixedAddSub(t *testing.T) {
	code := `
		变量 x = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			x = x + i - i
		}
	`
	env := dbEval(code)
	xVal, _ := env.Get("x")
	if xVal.(*object.Integer).Value != 0 {
		t.Errorf("期望 x=0，得到 %d", xVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "x")
}

func TestDeBruijnLoopConstantAssign(t *testing.T) {
	code := `
		变量 x = 0
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			x = 42
		}
	`
	env := dbEval(code)
	xVal, _ := env.Get("x")
	if xVal.(*object.Integer).Value != 42 {
		t.Errorf("期望 x=42，得到 %d", xVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "x")
}

func TestDeBruijnLoopMulConst(t *testing.T) {
	code := `
		变量 x = 1
		变量 i = 1
		对于 i = 1; i < 8; i = i + 1 {
			x = x * 3
		}
	`
	env := dbEval(code)
	xVal, _ := env.Get("x")
	if xVal.(*object.Integer).Value != 2187 {
		t.Errorf("期望 x=2187，得到 %d", xVal.(*object.Integer).Value)
	}
	assertDbVsTrad(t, code, "x")
}

// ================================================================
// 第四部分：性能基准测试
// ================================================================

// BenchmarkDeBruijnLoop1K De Bruijn Native Loop：1000 次迭代
func BenchmarkDeBruijnLoop1K(b *testing.B) {
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

// BenchmarkDeBruijnLoop10K De Bruijn Native Loop：10000 次迭代
func BenchmarkDeBruijnLoop10K(b *testing.B) {
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

// BenchmarkDeBruijnLoopMultiStmt De Bruijn Native Loop：多语句循环体 1000 次迭代
func BenchmarkDeBruijnLoopMultiStmt(b *testing.B) {
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

// BenchmarkDeBruijnLoopMultiply De Bruijn Native Loop：乘法循环 1000 次
func BenchmarkDeBruijnLoopMultiply(b *testing.B) {
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

// BenchmarkDeBruijnLoop100K De Bruijn Native Loop：100000 次迭代（压力测试）
func BenchmarkDeBruijnLoop100K(b *testing.B) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 100000; i = i + 1 {
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
