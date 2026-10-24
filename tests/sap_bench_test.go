package tests

// sap_bench_test.go — SAP (Static AST Pipelining) + Loop Fusion 性能基准测试
// （从 evaluator/ 迁移）
//
// 对比四层性能：
//   1. 传统 AST 树遍历（无 SAP）
//   2. SAP 编译闭包（表达式硬化）
//   3. SAP Fused Loop（循环熔合：预编译闭包消除 Eval 调度）
//   4. SAP Native Int64 Loop（原生整数循环：零函数调用直接 int64 操作）

import (
	"testing"

	"zuzo/ast"
	"zuzo/evaluator"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/object"
	"zuzo/parser"
)

// parseAndInfer 辅助：解析代码并运行类型推断，返回 program + inferencer
func parseAndInfer(code string) (*ast.Program, *infer.Inferencer) {
	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseProgram()

	inf := infer.New()
	inf.Infer(program)
	return program, inf
}

// ==================== 正确性测试 ====================

// TestSAPLoopCorrectness 验证 SAP 循环熔合与传统路径结果一致
func TestSAPLoopCorrectness(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			sum = sum + i
		}
	`
	program, inf := parseAndInfer(code)

	// 传统路径
	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	tradEnv := evaluator.NewEnvironment()
	evaluator.Eval(program, tradEnv)
	evaluator.SetSAPCompiler(oldSAP)

	tradSum, ok := tradEnv.Get("sum")
	if !ok {
		t.Fatal("传统路径：sum 未定义")
	}
	tradVal := tradSum.(*object.Integer).Value
	// sum = 0+1+2+...+99 = 4950
	expected := int64(4950)
	if tradVal != expected {
		t.Errorf("传统路径：期望 sum=4950，得到 %d", tradVal)
	}

	// SAP 路径（含循环熔合）
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	sapEnv := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, sapEnv)

	sapSum, ok := sapEnv.Get("sum")
	if !ok {
		t.Fatal("SAP 路径：sum 未定义")
	}
	sapVal := sapSum.(*object.Integer).Value
	if sapVal != expected {
		t.Errorf("SAP 路径：期望 sum=4950，得到 %d", sapVal)
	}
	if sapVal != tradVal {
		t.Errorf("结果不一致：传统=%d，SAP=%d", tradVal, sapVal)
	}
}

// TestSAPNativeLoopCorrectness 验证原生 int64 循环正确性
func TestSAPNativeLoopCorrectness(t *testing.T) {
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

	// 检查是否生成了原生循环闭包
	forFn := sap.GetCompiled(program.Statements[2])
	if forFn == nil {
		t.Fatal("ForStatement 未被 SAP 编译")
	}

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
	// sum = 0+1+2+...+999 = 499500
	expected := int64(499500)
	if result != expected {
		t.Errorf("期望 sum=%d，得到 %d", expected, result)
	}
}

// TestSAPWhileLoopCorrectness 验证 while 循环熔合正确性
func TestSAPWhileLoopCorrectness(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		当 i < 100 {
			sum = sum + i
			i = i + 1
		}
	`
	program, inf := parseAndInfer(code)

	// 传统路径
	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	tradEnv := evaluator.NewEnvironment()
	evaluator.Eval(program, tradEnv)
	evaluator.SetSAPCompiler(oldSAP)

	tradSum, ok := tradEnv.Get("sum")
	if !ok {
		t.Fatal("传统路径：sum 未定义")
	}
	tradVal := tradSum.(*object.Integer).Value

	// SAP 路径
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	sapEnv := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, sapEnv)

	sapSum, ok := sapEnv.Get("sum")
	if !ok {
		t.Fatal("SAP 路径：sum 未定义")
	}
	sapVal := sapSum.(*object.Integer).Value

	if tradVal != 4950 {
		t.Errorf("传统路径：期望 sum=4950，得到 %d", tradVal)
	}
	if sapVal != 4950 {
		t.Errorf("SAP 路径：期望 sum=4950，得到 %d", sapVal)
	}
}

// TestSAPNativeLoopMultiply 验证乘法运算在原生循环中正确
func TestSAPNativeLoopMultiply(t *testing.T) {
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

	prodObj, ok := env.Get("product")
	if !ok {
		t.Fatal("product 未定义")
	}
	result := prodObj.(*object.Integer).Value
	// product = 1*1*2*3*4*5 = 120
	if result != 120 {
		t.Errorf("期望 product=120，得到 %d", result)
	}
}

// TestSAPForLoopDebug 调试 for 循环熔合问题
func TestSAPForLoopDebug(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			sum = sum + i
		}
	`
	program, inf := parseAndInfer(code)

	t.Logf("SlotMap: %v", inf.GetSlotMap())
	t.Logf("Statements count: %d", len(program.Statements))

	for i, stmt := range program.Statements {
		t.Logf("Statement %d: %T", i, stmt)
	}

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)

	forStmt := program.Statements[2]
	forFn := sap.GetCompiled(forStmt)
	if forFn == nil {
		t.Fatal("ForStatement not compiled")
	}
	t.Log("ForStatement compiled successfully")

	// Check individual parts
	if fs, ok := forStmt.(*ast.ForStatement); ok {
		t.Logf("Init type: %T", fs.Init)
		t.Logf("Condition type: %T", fs.Condition)
		t.Logf("Update type: %T", fs.Update)

		initFn := sap.GetCompiled(fs.Init)
		t.Logf("Init compiled: %v", initFn != nil)

		condFn := sap.GetCompiled(fs.Condition)
		t.Logf("Condition compiled: %v", condFn != nil)

		updateFn := sap.GetCompiled(fs.Update)
		t.Logf("Update compiled: %v", updateFn != nil)

		if fs.Body != nil {
			for i, stmt := range fs.Body.Statements {
				t.Logf("Body stmt %d type: %T", i, stmt)
				fn := sap.GetCompiled(stmt)
				t.Logf("  compiled: %v", fn != nil)

				if es, ok := stmt.(*ast.ExpressionStatement); ok {
					t.Logf("  expr type: %T", es.Expression)
					fn2 := sap.GetCompiled(es.Expression)
					t.Logf("  expr compiled: %v", fn2 != nil)
				}
			}
		}
	}

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	result := evaluator.Eval(program, env)
	t.Logf("Result: %v", result)

	sumObj, ok := env.Get("sum")
	t.Logf("sum in map: %v, ok: %v", sumObj, ok)

	if env.HasSlots() {
		sumSlot := inf.GetSlotMap()["sum"]
		slotVal := env.GetSlot(sumSlot)
		t.Logf("sum slot idx: %d, val: %v", sumSlot, slotVal)

		iSlot := inf.GetSlotMap()["i"]
		iVal := env.GetSlot(iSlot)
		t.Logf("i slot idx: %d, val: %v", iSlot, iVal)
	}
}

// ==================== 表达式基准测试 ====================

// BenchmarkTraditionalEval 纯传统 AST 树遍历（无 SAP）
func BenchmarkTraditionalEval(b *testing.B) {
	code := `
		变量 a = 1
		变量 b = 2
		变量 c = a + b
		变量 d = c * 3
		变量 e = d - a
		变量 f = e + b
	`
	program, _ := parseAndInfer(code)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewEnvironment()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evaluator.Eval(program, env)
	}
}

// BenchmarkSAPEval SAP 编译闭包路径
func BenchmarkSAPEval(b *testing.B) {
	code := `
		变量 a = 1
		变量 b = 2
		变量 c = a + b
		变量 d = c * 3
		变量 e = d - a
		变量 f = e + b
	`
	program, inf := parseAndInfer(code)

	oldSAP := evaluator.GetSAPCompiler()
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evaluator.Eval(program, env)
	}
}

// BenchmarkTraditionalArith 传统路径：纯算术表达式
func BenchmarkTraditionalArith(b *testing.B) {
	code := `1 + 2 * 3 - 4 + 5 * 6 - 7 + 8 * 9 - 10`
	program, _ := parseAndInfer(code)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewEnvironment()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evaluator.Eval(program, env)
	}
}

// BenchmarkSAPArith SAP 路径：纯算术表达式
func BenchmarkSAPArith(b *testing.B) {
	code := `1 + 2 * 3 - 4 + 5 * 6 - 7 + 8 * 9 - 10`
	program, inf := parseAndInfer(code)

	oldSAP := evaluator.GetSAPCompiler()
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		evaluator.Eval(program, env)
	}
}

// ==================== 循环基准测试 ====================

// BenchmarkTraditionalLoop 传统路径：for 循环内整数运算（1000 次迭代）
func BenchmarkTraditionalLoop(b *testing.B) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 1000; i = i + 1 {
			sum = sum + i
		}
	`
	program, _ := parseAndInfer(code)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	defer evaluator.SetSAPCompiler(oldSAP)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewEnvironment()
		evaluator.Eval(program, env)
	}
}

// BenchmarkSAPLoopFused SAP Fused Loop：循环熔合（Level 1-2）
func BenchmarkSAPLoopFused(b *testing.B) {
	code := `
		变量 n = 1000
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < n; i = i + 1 {
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

// BenchmarkSAPLoopNative SAP Native Int64 Loop：原生整数循环（Level 3）
func BenchmarkSAPLoopNative(b *testing.B) {
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

	// 验证确实走了原生路径
	forStmt := program.Statements[2]
	forFn := sap.GetCompiled(forStmt)
	if forFn == nil {
		b.Fatal("ForStatement 未被 SAP 编译")
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
		evaluator.Eval(program, env)
	}
}

// BenchmarkTraditionalLoop10K 传统路径：10000 次迭代
func BenchmarkTraditionalLoop10K(b *testing.B) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 10000; i = i + 1 {
			sum = sum + i
		}
	`
	program, _ := parseAndInfer(code)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	defer evaluator.SetSAPCompiler(oldSAP)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewEnvironment()
		evaluator.Eval(program, env)
	}
}

// BenchmarkSAPLoopNative10K SAP Native Int64 Loop：10000 次迭代
func BenchmarkSAPLoopNative10K(b *testing.B) {
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

// BenchmarkTraditionalWhile 传统路径：while 循环
func BenchmarkTraditionalWhile(b *testing.B) {
	code := `
		变量 sum = 0
		变量 i = 0
		当 i < 1000 {
			sum = sum + i
			i = i + 1
		}
	`
	program, _ := parseAndInfer(code)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	defer evaluator.SetSAPCompiler(oldSAP)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewEnvironment()
		evaluator.Eval(program, env)
	}
}

// BenchmarkSAPWhileFused SAP Fused While Loop
func BenchmarkSAPWhileFused(b *testing.B) {
	code := `
		变量 sum = 0
		变量 i = 0
		当 i < 1000 {
			sum = sum + i
			i = i + 1
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

// BenchmarkSAPLoopMultiStmt 多语句循环体基准测试
func BenchmarkSAPLoopMultiStmt(b *testing.B) {
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

// BenchmarkTraditionalLoopMultiStmt 多语句循环体传统路径
func BenchmarkTraditionalLoopMultiStmt(b *testing.B) {
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
	program, _ := parseAndInfer(code)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	defer evaluator.SetSAPCompiler(oldSAP)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		env := evaluator.NewEnvironment()
		evaluator.Eval(program, env)
	}
}
