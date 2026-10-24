package tests

// smi_edge_test.go — Smi-Tagging 边界测试
//
// 覆盖以下边界场景：
//   1. Smi 编码边界值（±1, ±511/512, ±2^62）
//   2. 非 Integer 类型的 ObjectAsSmi（Float, String, Boolean, Null）
//   3. 循环零迭代 / 单迭代
//   4. 所有条件运算符（<, <=, >, >=）
//   5. 大步长 / 负初始值 / 负步长
//   6. 循环变量终值正确性
//   7. 跨路径一致性（传统 vs Smi 全面对比）
//   8. valueSlots 底层操作（未初始化、扩容、非 Integer syncSlot）
//   9. 连续循环 / 嵌套循环
//  10. 循环体变量赋值 a = b

import (
	"testing"

	"zuzo/evaluator"
	"zuzo/object"
)

// ================================================================
// 第一部分：Smi 编码边界值
// ================================================================

func TestSmiEncodingBoundaryValues(t *testing.T) {
	// 最小非零值
	cases := []struct {
		name string
		val  int64
	}{
		{"正1", 1},
		{"负1", -1},
		{"缓存上界511", 511},
		{"缓存下界负512", -512},
		{"缓存上界外512", 512},
		{"缓存下界外负513", -513},
		{"Smi最大值_2的62次方减1", 4611686018427387903},
		{"Smi最小值_负2的62次方", -4611686018427387904},
		{"int32最大值", 2147483647},
		{"int32最小值", -2147483648},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := object.Smi(tc.val)
			if !v.IsSmi() {
				t.Fatalf("Smi(%d) 不是 Smi", tc.val)
			}
			got := v.AsSmi()
			if got != tc.val {
				t.Fatalf("roundtrip 失败：输入 %d，得到 %d", tc.val, got)
			}
		})
	}
}

// TestSmiAsObjectCacheBoundary 测试缓存边界：511 在缓存内，512 在缓存外
func TestSmiAsObjectCacheBoundary(t *testing.T) {
	// 511 在缓存内 → 返回缓存指针
	v511 := object.Smi(511)
	obj511a := v511.AsObject()
	obj511b := v511.AsObject()
	if obj511a != obj511b {
		t.Fatal("511 在缓存范围内，AsObject 应返回相同指针")
	}

	// 512 在缓存外 → 每次返回新对象（值正确即可）
	v512 := object.Smi(512)
	obj512 := v512.AsObject()
	intObj, ok := obj512.(*object.Integer)
	if !ok || intObj.Value != 512 {
		t.Fatalf("512 AsObject 值不正确：得到 %v", obj512)
	}

	// -512 在缓存内
	v_neg512 := object.Smi(-512)
	obj_neg512a := v_neg512.AsObject()
	obj_neg512b := v_neg512.AsObject()
	if obj_neg512a != obj_neg512b {
		t.Fatal("-512 在缓存范围内，AsObject 应返回相同指针")
	}

	// -513 在缓存外
	v_neg513 := object.Smi(-513)
	obj_neg513 := v_neg513.AsObject()
	intObj2, ok := obj_neg513.(*object.Integer)
	if !ok || intObj2.Value != -513 {
		t.Fatalf("-513 AsObject 值不正确：得到 %v", obj_neg513)
	}
}

// TestSmiAsObjectNonSmi 零值 Value（非 Smi）AsObject 应返回 Null
func TestSmiAsObjectNonSmi(t *testing.T) {
	var v object.Value // 零值，非 Smi
	obj := v.AsObject()
	if _, ok := obj.(*object.Null); !ok {
		t.Fatalf("零值 Value 的 AsObject 应返回 *Null，得到 %T", obj)
	}
}

// TestObjectAsSmiNonInteger 非 Integer 类型的 ObjectAsSmi 应返回 false
func TestObjectAsSmiNonInteger(t *testing.T) {
	cases := []struct {
		name string
		obj  object.Object
	}{
		{"Float", &object.Float{Value: 3.14}},
		{"String", &object.String{Value: "hello"}},
		{"Boolean", &object.Boolean{Value: true}},
		{"Null", &object.Null{}},
		{"Array", &object.Array{Elements: []object.Object{}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, ok := object.ObjectAsSmi(tc.obj)
			if ok {
				t.Fatalf("%s 类型不应转换为 Smi", tc.name)
			}
		})
	}
}

// TestObjectAsSmiInteger Integer 类型应正确转换
func TestObjectAsSmiInteger(t *testing.T) {
	cases := []int64{0, 1, -1, 42, -42, 511, -512, 1000000, -1000000}
	for _, n := range cases {
		obj := &object.Integer{Value: n}
		v, ok := object.ObjectAsSmi(obj)
		if !ok {
			t.Fatalf("Integer(%d) 应可转换为 Smi", n)
		}
		if !v.IsSmi() {
			t.Fatalf("转换结果不是 Smi")
		}
		if v.AsSmi() != n {
			t.Fatalf("转换值不匹配：输入 %d，得到 %d", n, v.AsSmi())
		}
	}
}

// ================================================================
// 第二部分：valueSlots 底层操作
// ================================================================

func TestValueSlotUninitialized(t *testing.T) {
	env := object.NewSlotEnvironment(10, nil, map[string]int{"x": 0, "y": 1})

	// 未初始化的槽位应返回零值
	v := env.GetValueSlot(0)
	if !v.IsNil() {
		t.Fatal("未初始化的 valueSlot 应返回零值")
	}
	if v.IsSmi() {
		t.Fatal("零值不应是 Smi（LSB=0）")
	}
}

func TestValueSlotSetAndGet(t *testing.T) {
	env := object.NewSlotEnvironment(10, nil, map[string]int{"x": 0, "y": 1, "z": 2})

	// 写入 Smi 并读回
	env.SetValueSlot(0, object.Smi(42))
	env.SetValueSlot(1, object.Smi(-100))
	env.SetValueSlot(2, object.Smi(0))

	if env.GetValueSlot(0).AsSmi() != 42 {
		t.Fatal("slot 0 值不正确")
	}
	if env.GetValueSlot(1).AsSmi() != -100 {
		t.Fatal("slot 1 值不正确")
	}
	if env.GetValueSlot(2).AsSmi() != 0 {
		t.Fatal("slot 2 值不正确")
	}
}

func TestValueSlotAutoExpand(t *testing.T) {
	env := object.NewSlotEnvironment(2, nil, map[string]int{"x": 0})

	// 写入超出初始大小的索引 → 应自动扩容
	env.SetValueSlot(10, object.Smi(999))
	if env.GetValueSlot(10).AsSmi() != 999 {
		t.Fatal("扩容后槽位值不正确")
	}
}

func TestSyncSlotInteger(t *testing.T) {
	env := object.NewSlotEnvironment(5, nil, map[string]int{"x": 0, "y": 1})

	// 通过 Set 写入 Integer → 应同步到 valueSlots
	env.Set("x", &object.Integer{Value: 123})

	v := env.GetValueSlot(0)
	if !v.IsSmi() {
		t.Fatal("Set Integer 后 valueSlot 应为 Smi")
	}
	if v.AsSmi() != 123 {
		t.Fatalf("valueSlot 值不正确：期望 123，得到 %d", v.AsSmi())
	}
}

func TestSyncSlotNonInteger(t *testing.T) {
	env := object.NewSlotEnvironment(5, nil, map[string]int{"s": 0})

	// 通过 Set 写入 String → valueSlot 应保持零值（不同步非 Integer）
	env.Set("s", &object.String{Value: "hello"})

	v := env.GetValueSlot(0)
	if v.IsSmi() {
		t.Fatal("Set String 后 valueSlot 不应是 Smi")
	}
	// Object 槽位应该有值
	obj := env.GetSlot(0)
	if obj == nil {
		t.Fatal("Object 槽位应有值")
	}
	if str, ok := obj.(*object.String); !ok || str.Value != "hello" {
		t.Fatal("Object 槽位值不正确")
	}
}

// ================================================================
// 第三部分：循环边界场景
// ================================================================

// smiEval 辅助：编译并执行代码，返回环境（SAP 路径）
func smiEval(code string) (*object.Environment, error) {
	program, inf := parseAndInfer(code)

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
	evaluator.Eval(program, env)
	return env, nil
}

// tradEval 辅助：传统路径执行代码，返回环境
func tradEval(code string) *object.Environment {
	program, _ := parseAndInfer(code)

	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewEnvironment()
	evaluator.Eval(program, env)
	return env
}

// assertSmiVsTrad 对比 Smi 路径和传统路径的变量值
func assertSmiVsTrad(t *testing.T, code, varName string) {
	t.Helper()

	smiEnv, _ := smiEval(code)
	tradEnv := tradEval(code)

	smiVal, ok := smiEnv.Get(varName)
	if !ok {
		t.Fatalf("Smi 路径：变量 %s 未定义", varName)
	}
	tradVal, ok := tradEnv.Get(varName)
	if !ok {
		t.Fatalf("传统路径：变量 %s 未定义", varName)
	}

	smiInt, ok1 := smiVal.(*object.Integer)
	tradInt, ok2 := tradVal.(*object.Integer)
	if !ok1 || !ok2 {
		t.Fatalf("变量 %s 不是 Integer：Smi=%T, 传统=%T", varName, smiVal, tradVal)
	}

	if smiInt.Value != tradInt.Value {
		t.Errorf("变量 %s 不一致：Smi=%d, 传统=%d", varName, smiInt.Value, tradInt.Value)
	}
}

// --- 零迭代 ---

func TestSmiLoopZeroIterations(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 0; i = i + 1 {
			sum = sum + i
		}
	`
	smiEnv, _ := smiEval(code)
	sumVal, _ := smiEnv.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != 0 {
		t.Errorf("零迭代：期望 sum=0，得到 %d", result)
	}

	// 循环变量应保持初始值
	iVal, _ := smiEnv.Get("i")
	iResult := iVal.(*object.Integer).Value
	if iResult != 0 {
		t.Errorf("零迭代：期望 i=0（初始值），得到 %d", iResult)
	}
}

// --- 单次迭代 ---

func TestSmiLoopSingleIteration(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 1; i = i + 1 {
			sum = sum + 42
		}
	`
	smiEnv, _ := smiEval(code)
	sumVal, _ := smiEnv.Get("sum")
	if sumVal.(*object.Integer).Value != 42 {
		t.Errorf("单迭代：期望 sum=42，得到 %d", sumVal.(*object.Integer).Value)
	}
}

// --- 所有条件运算符 ---

func TestSmiLoopAllConditions(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		expected int64
	}{
		{
			"小于_Lt",
			`变量 s = 0; 变量 i = 0; 对于 i = 0; i < 10; i = i + 1 { s = s + i }`,
			45, // 0+1+...+9
		},
		{
			"小于等于_Le",
			`变量 s = 0; 变量 i = 0; 对于 i = 0; i <= 10; i = i + 1 { s = s + i }`,
			55, // 0+1+...+10
		},
		{
			"大于_Gt",
			`变量 s = 0; 变量 i = 0; 对于 i = 10; i > 0; i = i - 1 { s = s + i }`,
			55, // 10+9+...+1
		},
		{
			"大于等于_Ge",
			`变量 s = 0; 变量 i = 0; 对于 i = 10; i >= 0; i = i - 1 { s = s + i }`,
			55, // 10+9+...+0
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			smiEnv, _ := smiEval(tc.code)
			sVal, _ := smiEnv.Get("s")
			result := sVal.(*object.Integer).Value
			if result != tc.expected {
				t.Errorf("期望 s=%d，得到 %d", tc.expected, result)
			}
			// 同时对比传统路径
			assertSmiVsTrad(t, tc.code, "s")
		})
	}
}

// --- 大步长 ---

func TestSmiLoopLargeStep(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		expected int64
	}{
		{
			"步长10",
			`变量 s = 0; 变量 i = 0; 对于 i = 0; i < 100; i = i + 10 { s = s + i }`,
			450, // 0+10+20+...+90
		},
		{
			"步长100",
			`变量 s = 0; 变量 i = 0; 对于 i = 0; i < 1000; i = i + 100 { s = s + i }`,
			4500, // 0+100+200+...+900
		},
		{
			"负步长10",
			`变量 s = 0; 变量 i = 0; 对于 i = 100; i > 0; i = i - 10 { s = s + i }`,
			550, // 100+90+80+...+10
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			smiEnv, _ := smiEval(tc.code)
			sVal, _ := smiEnv.Get("s")
			result := sVal.(*object.Integer).Value
			if result != tc.expected {
				t.Errorf("期望 s=%d，得到 %d", tc.expected, result)
			}
			assertSmiVsTrad(t, tc.code, "s")
		})
	}
}

// --- 负初始值 ---

func TestSmiLoopNegativeInit(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = -10; i < 10; i = i + 1 {
			sum = sum + i
		}
	`
	// sum = -10 + -9 + ... + 0 + 1 + ... + 9 = -10
	smiEnv, _ := smiEval(code)
	sumVal, _ := smiEnv.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != -10 {
		t.Errorf("期望 sum=-10，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "sum")
}

// --- 循环变量终值 ---

func TestSmiLoopVariableFinalValue(t *testing.T) {
	cases := []struct {
		name     string
		code     string
		expected int64
	}{
		{
			"递增循环_i终值",
			`变量 i = 0; 对于 i = 0; i < 100; i = i + 1 { }`,
			100, // 循环退出时 i = 100
		},
		{
			"递减循环_i终值",
			`变量 i = 0; 对于 i = 100; i > 0; i = i - 1 { }`,
			0, // 循环退出时 i = 0
		},
		{
			"步长5_i终值",
			`变量 i = 0; 对于 i = 0; i < 100; i = i + 5 { }`,
			100, // 0,5,10,...,95,100(退出)
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			smiEnv, _ := smiEval(tc.code)
			iVal, _ := smiEnv.Get("i")
			result := iVal.(*object.Integer).Value
			if result != tc.expected {
				t.Errorf("期望 i=%d，得到 %d", tc.expected, result)
			}
			assertSmiVsTrad(t, tc.code, "i")
		})
	}
}

// --- 循环后变量使用 ---

func TestSmiLoopVariableUsedAfterLoop(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 50; i = i + 1 {
			sum = sum + i
		}
		变量 final = i
	`
	smiEnv, _ := smiEval(code)
	finalVal, _ := smiEnv.Get("final")
	if finalVal.(*object.Integer).Value != 50 {
		t.Errorf("期望 final=50，得到 %d", finalVal.(*object.Integer).Value)
	}

	sumVal, _ := smiEnv.Get("sum")
	if sumVal.(*object.Integer).Value != 1225 { // 0+1+...+49
		t.Errorf("期望 sum=1225，得到 %d", sumVal.(*object.Integer).Value)
	}
	assertSmiVsTrad(t, code, "sum")
	assertSmiVsTrad(t, code, "final")
}

// --- 连续循环 ---

func TestSmiConsecutiveLoops(t *testing.T) {
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
	// total = 2 * (0+1+...+99) = 2 * 4950 = 9900
	smiEnv, _ := smiEval(code)
	totalVal, _ := smiEnv.Get("total")
	result := totalVal.(*object.Integer).Value
	if result != 9900 {
		t.Errorf("期望 total=9900，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "total")
}

// --- 变量赋值 a = b 在循环体中 ---

func TestSmiLoopVariableCopy(t *testing.T) {
	code := `
		变量 a = 0
		变量 b = 0
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			b = i
			a = b
		}
	`
	smiEnv, _ := smiEval(code)
	aVal, _ := smiEnv.Get("a")
	bVal, _ := smiEnv.Get("b")

	// 循环结束后 a = b = 9（最后一次迭代 i=9）
	if aVal.(*object.Integer).Value != 9 {
		t.Errorf("期望 a=9，得到 %d", aVal.(*object.Integer).Value)
	}
	if bVal.(*object.Integer).Value != 9 {
		t.Errorf("期望 b=9，得到 %d", bVal.(*object.Integer).Value)
	}
	assertSmiVsTrad(t, code, "a")
	assertSmiVsTrad(t, code, "b")
}

// --- 常量赋值在循环体中 ---

func TestSmiLoopConstantAssign(t *testing.T) {
	code := `
		变量 x = 0
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			x = 42
		}
	`
	smiEnv, _ := smiEval(code)
	xVal, _ := smiEnv.Get("x")
	if xVal.(*object.Integer).Value != 42 {
		t.Errorf("期望 x=42，得到 %d", xVal.(*object.Integer).Value)
	}
	assertSmiVsTrad(t, code, "x")
}

// --- 负数累加 ---

func TestSmiLoopNegativeAccumulation(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			sum = sum - i
		}
	`
	// sum = -(0+1+...+99) = -4950
	smiEnv, _ := smiEval(code)
	sumVal, _ := smiEnv.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != -4950 {
		t.Errorf("期望 sum=-4950，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "sum")
}

// --- 乘法累积超出缓存范围 ---

func TestSmiLoopProductExceedsCache(t *testing.T) {
	code := `
		变量 product = 1
		变量 i = 1
		对于 i = 1; i < 20; i = i + 1 {
			product = product * i
		}
	`
	// 19! = 121645100408832000，远超缓存范围
	smiEnv, _ := smiEval(code)
	prodVal, _ := smiEnv.Get("product")
	result := prodVal.(*object.Integer).Value
	expected := int64(121645100408832000)
	if result != expected {
		t.Errorf("期望 product=%d，得到 %d", expected, result)
	}
	assertSmiVsTrad(t, code, "product")
}

// --- 减法运算在循环体中 ---

func TestSmiLoopSubtractionInBody(t *testing.T) {
	code := `
		变量 diff = 1000
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			diff = diff - i
		}
	`
	// diff = 1000 - (0+1+...+99) = 1000 - 4950 = -3950
	smiEnv, _ := smiEval(code)
	diffVal, _ := smiEnv.Get("diff")
	result := diffVal.(*object.Integer).Value
	if result != -3950 {
		t.Errorf("期望 diff=-3950，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "diff")
}

// --- 三变量交叉赋值 ---

func TestSmiLoopThreeVarCrossAssign(t *testing.T) {
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
	// 这是斐波那契数列：0,1,1,2,3,5,8,13,21,34,55
	// 10 次迭代后：a=55, b=89, c=34
	smiEnv, _ := smiEval(code)
	aVal, _ := smiEnv.Get("a")
	bVal, _ := smiEnv.Get("b")
	cVal, _ := smiEnv.Get("c")

	if aVal.(*object.Integer).Value != 55 {
		t.Errorf("期望 a=55，得到 %d", aVal.(*object.Integer).Value)
	}
	if bVal.(*object.Integer).Value != 89 {
		t.Errorf("期望 b=89，得到 %d", bVal.(*object.Integer).Value)
	}
	if cVal.(*object.Integer).Value != 34 {
		t.Errorf("期望 c=34，得到 %d", cVal.(*object.Integer).Value)
	}
	assertSmiVsTrad(t, code, "a")
	assertSmiVsTrad(t, code, "b")
	assertSmiVsTrad(t, code, "c")
}

// --- 循环变量在循环后用于表达式 ---

func TestSmiLoopVarInExpressionAfter(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			sum = sum + i
		}
		变量 doubled = sum * 2
		变量 count = i
	`
	smiEnv, _ := smiEval(code)
	doubledVal, _ := smiEnv.Get("doubled")
	countVal, _ := smiEnv.Get("count")

	if doubledVal.(*object.Integer).Value != 9900 { // 4950 * 2
		t.Errorf("期望 doubled=9900，得到 %d", doubledVal.(*object.Integer).Value)
	}
	if countVal.(*object.Integer).Value != 100 {
		t.Errorf("期望 count=100，得到 %d", countVal.(*object.Integer).Value)
	}
	assertSmiVsTrad(t, code, "doubled")
	assertSmiVsTrad(t, code, "count")
}

// --- 嵌套循环（外层 Smi + 内层 Smi）---

func TestSmiNestedLoops(t *testing.T) {
	code := `
		变量 total = 0
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			变量 j = 0
			对于 j = 0; j < 10; j = j + 1 {
				total = total + 1
			}
		}
	`
	// total = 10 * 10 = 100
	smiEnv, _ := smiEval(code)
	totalVal, _ := smiEnv.Get("total")
	result := totalVal.(*object.Integer).Value
	if result != 100 {
		t.Errorf("期望 total=100，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "total")
}

// --- 嵌套循环带累加 ---

func TestSmiNestedLoopsWithAccumulation(t *testing.T) {
	code := `
		变量 total = 0
		变量 i = 0
		对于 i = 0; i < 5; i = i + 1 {
			变量 inner = 0
			对于 inner = 0; inner < 5; inner = inner + 1 {
				total = total + i * inner
			}
		}
	`
	// total = Σ(i=0..4) Σ(j=0..4) i*j
	// = Σ(i=0..4) i * (0+1+2+3+4) = (0+1+2+3+4) * 10 = 10 * 10 = 100
	smiEnv, _ := smiEval(code)
	totalVal, _ := smiEnv.Get("total")
	result := totalVal.(*object.Integer).Value
	if result != 100 {
		t.Errorf("期望 total=100，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "total")
}

// --- 混合加法和乘法 ---

func TestSmiLoopMixedAddAndMul(t *testing.T) {
	code := `
		变量 result = 0
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			result = result + i * 2
		}
	`
	// result = 0*2 + 1*2 + 2*2 + ... + 9*2 = 2*(0+1+...+9) = 2*45 = 90
	smiEnv, _ := smiEval(code)
	rVal, _ := smiEnv.Get("result")
	result := rVal.(*object.Integer).Value
	if result != 90 {
		t.Errorf("期望 result=90，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "result")
}

// --- 变量乘以常量 ---

func TestSmiLoopMulConst(t *testing.T) {
	code := `
		变量 x = 1
		变量 i = 1
		对于 i = 1; i < 8; i = i + 1 {
			x = x * 3
		}
	`
	// x = 3^7 = 2187
	smiEnv, _ := smiEval(code)
	xVal, _ := smiEnv.Get("x")
	result := xVal.(*object.Integer).Value
	if result != 2187 {
		t.Errorf("期望 x=2187，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "x")
}

// --- 累加器从非零值开始 ---

func TestSmiLoopNonZeroAccumulator(t *testing.T) {
	code := `
		变量 sum = 1000
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			sum = sum + i
		}
	`
	// sum = 1000 + (0+1+...+99) = 1000 + 4950 = 5950
	smiEnv, _ := smiEval(code)
	sumVal, _ := smiEnv.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != 5950 {
		t.Errorf("期望 sum=5950，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "sum")
}

// --- i 从负到正 ---

func TestSmiLoopNegativeToPositiveRange(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = -5; i < 5; i = i + 1 {
			sum = sum + i
		}
	`
	// sum = -5 + -4 + -3 + -2 + -1 + 0 + 1 + 2 + 3 + 4 = -5
	smiEnv, _ := smiEval(code)
	sumVal, _ := smiEnv.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != -5 {
		t.Errorf("期望 sum=-5，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "sum")
}

// --- 大数累加（超过 Smi 缓存范围但仍安全）---

func TestSmiLoopLargeAccumulation(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 0; i < 100000; i = i + 1 {
			sum = sum + i
		}
	`
	// sum = 0+1+...+99999 = 99999*100000/2 = 4999950000
	smiEnv, _ := smiEval(code)
	sumVal, _ := smiEnv.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != 4999950000 {
		t.Errorf("期望 sum=4999950000，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "sum")
}

// --- 负步长大循环 ---

func TestSmiLoopLargeNegativeStep(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 10000; i > 0; i = i - 100 {
			sum = sum + i
		}
	`
	// sum = 10000 + 9900 + 9800 + ... + 100 = 100*(100+99+...+1) = 100*5050 = 505000
	smiEnv, _ := smiEval(code)
	sumVal, _ := smiEnv.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != 505000 {
		t.Errorf("期望 sum=505000，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "sum")
}

// --- 交换两个变量 ---

func TestSmiLoopSwapVariables(t *testing.T) {
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
	// 交换 5 次（奇数次）→ a=1, b=100（回到初始值）
	// 等等，5 次交换：奇数次 → a 和 b 交换
	// 初始 a=1, b=100
	// 1次: a=100, b=1
	// 2次: a=1, b=100
	// 3次: a=100, b=1
	// 4次: a=1, b=100
	// 5次: a=100, b=1
	smiEnv, _ := smiEval(code)
	aVal, _ := smiEnv.Get("a")
	bVal, _ := smiEnv.Get("b")

	if aVal.(*object.Integer).Value != 100 {
		t.Errorf("期望 a=100，得到 %d", aVal.(*object.Integer).Value)
	}
	if bVal.(*object.Integer).Value != 1 {
		t.Errorf("期望 b=1，得到 %d", bVal.(*object.Integer).Value)
	}
	assertSmiVsTrad(t, code, "a")
	assertSmiVsTrad(t, code, "b")
}

// --- 多变量同时累加 ---

func TestSmiLoopMultipleAccumulators(t *testing.T) {
	code := `
		变量 evens = 0
		变量 odds = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			evens = evens + i
			odds = odds + i + 1
		}
	`
	// evens = 0+1+...+99 = 4950
	// odds = (1+2+...+100) = 5050
	smiEnv, _ := smiEval(code)
	eVal, _ := smiEnv.Get("evens")
	oVal, _ := smiEnv.Get("odds")

	if eVal.(*object.Integer).Value != 4950 {
		t.Errorf("期望 evens=4950，得到 %d", eVal.(*object.Integer).Value)
	}
	if oVal.(*object.Integer).Value != 5050 {
		t.Errorf("期望 odds=5050，得到 %d", oVal.(*object.Integer).Value)
	}
	assertSmiVsTrad(t, code, "evens")
	assertSmiVsTrad(t, code, "odds")
}

// --- 循环体中使用循环变量参与计算 ---

func TestSmiLoopVarInBodyExpression(t *testing.T) {
	code := `
		变量 sum = 0
		变量 i = 0
		对于 i = 1; i < 100; i = i + 1 {
			sum = sum + i + i
		}
	`
	// sum = 2*(1+2+...+99) = 2*4950 = 9900
	smiEnv, _ := smiEval(code)
	sumVal, _ := smiEnv.Get("sum")
	result := sumVal.(*object.Integer).Value
	if result != 9900 {
		t.Errorf("期望 sum=9900，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "sum")
}

// --- 变量减去常量 ---

func TestSmiLoopSubConst(t *testing.T) {
	code := `
		变量 x = 1000
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			x = x - 5
		}
	`
	// x = 1000 - 5*100 = 500
	smiEnv, _ := smiEval(code)
	xVal, _ := smiEnv.Get("x")
	result := xVal.(*object.Integer).Value
	if result != 500 {
		t.Errorf("期望 x=500，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "x")
}

// --- 混合加减 ---

func TestSmiLoopMixedAddSub(t *testing.T) {
	code := `
		变量 x = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			x = x + i - i
		}
	`
	// x = 0 (每次加 i 再减 i)
	smiEnv, _ := smiEval(code)
	xVal, _ := smiEnv.Get("x")
	result := xVal.(*object.Integer).Value
	if result != 0 {
		t.Errorf("期望 x=0，得到 %d", result)
	}
	assertSmiVsTrad(t, code, "x")
}

// --- 乘法分配律 ---

func TestSmiLoopDistributiveMul(t *testing.T) {
	code := `
		变量 a = 0
		变量 b = 0
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			a = a + i * 3
			b = b + i * 3
		}
	`
	// a = b = 3*(0+1+...+9) = 3*45 = 135
	smiEnv, _ := smiEval(code)
	aVal, _ := smiEnv.Get("a")
	bVal, _ := smiEnv.Get("b")

	if aVal.(*object.Integer).Value != 135 {
		t.Errorf("期望 a=135，得到 %d", aVal.(*object.Integer).Value)
	}
	if bVal.(*object.Integer).Value != 135 {
		t.Errorf("期望 b=135，得到 %d", bVal.(*object.Integer).Value)
	}
	assertSmiVsTrad(t, code, "a")
	assertSmiVsTrad(t, code, "b")
}
