package tests

// regression_test.go — 回归测试
//
// 防御以下已修复的 Bug 再次出现：
//   1. SAP slot/map "脑裂" Bug：Set/Assign 只写 map 不写 slot，SAP 读取器读到旧值
//   2. 影子模式 (Shadow Execution)：确保双轨执行对比正确
//   3. Feature Toggle：--no-sap 路径正确性

import (
	"strings"
	"testing"
	"time"

	"zuzo/evaluator"
	"zuzo/object"
)

// ================================================================
// 第一部分：SAP slot/map 脑裂 Bug 回归测试
// ================================================================

// TestSlotMapSyncWhileBreak 回归测试：当循环通过条件变量退出
//
// 这是导致 demo_concurrency.hy 卡死的精确根因：
//   - WhileStatement 被 SAP 编译，条件从 slot 读取
//   - IfExpression 内的赋值走传统 Eval 路径，只写 map
//   - slot 永远是旧值 → 无限循环
//
// 修复后：Set/Assign 调用 syncSlot 同步更新槽位
func TestSlotMapSyncWhileBreak(t *testing.T) {
	code := `
		变量 running = 真
		变量 count = 0
		当 running {
			count = count + 1
			如果 count >= 5 {
				running = 假
			}
		}
	`

	program, inf := parseAndInfer(code)

	// SAP 路径
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	slotMap := inf.GetSlotMap()
	env := evaluator.NewSAPEnvironment(len(slotMap), slotMap)

	// 加超时保护：如果脑裂 Bug 回归，会在 3 秒内超时
	done := make(chan struct{})
	go func() {
		evaluator.Eval(program, env)
		close(done)
	}()

	select {
	case <-done:
		// 正常完成
	case <-time.After(3 * time.Second):
		t.Fatal("超时：SAP slot/map 脑裂 Bug 已回归！当循环未退出")
	}

	// 验证 count = 5
	countObj, ok := env.Get("count")
	if !ok {
		t.Fatal("count 未定义")
	}
	count := countObj.(*object.Integer).Value
	if count != 5 {
		t.Errorf("期望 count=5，得到 %d", count)
	}

	// 验证 running = 假
	runningObj, ok := env.Get("running")
	if !ok {
		t.Fatal("running 未定义")
	}
	running := runningObj.(*object.Boolean).Value
	if running {
		t.Error("期望 running=假，得到 running=真")
	}
}

// TestSlotMapSyncAssignOutsideSAP 回归测试：SAP 编译的循环内通过非 SAP 赋值修改变量
//
// 场景：ForStatement 被编译为原生 int64 循环，但循环体内有 IfExpression
// 分支修改了循环外定义的变量。该变量在循环后被读取。
func TestSlotMapSyncAssignOutsideSAP(t *testing.T) {
	code := `
		变量 sum = 0
		变量 flag = 假
		变量 i = 0
		对于 i = 0; i < 10; i = i + 1 {
			sum = sum + i
			如果 i == 5 {
				flag = 真
			}
		}
	`

	program, inf := parseAndInfer(code)

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	slotMap := inf.GetSlotMap()
	env := evaluator.NewSAPEnvironment(len(slotMap), slotMap)
	evaluator.Eval(program, env)

	// sum = 0+1+2+...+9 = 45
	sumObj, _ := env.Get("sum")
	sum := sumObj.(*object.Integer).Value
	if sum != 45 {
		t.Errorf("期望 sum=45，得到 %d", sum)
	}

	// flag 应该为真（i==5 时被设置）
	flagObj, _ := env.Get("flag")
	flag := flagObj.(*object.Boolean).Value
	if !flag {
		t.Error("期望 flag=真，得到 flag=假 — slot 可能未被同步")
	}
}

// TestSlotMapSyncMultipleAssigns 回归测试：多次赋值后 slot 一致性
func TestSlotMapSyncMultipleAssigns(t *testing.T) {
	code := `
		变量 x = 1
		变量 y = 0
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			x = x + 1
			y = y + x
		}
	`

	program, inf := parseAndInfer(code)

	// 传统路径
	evaluator.SetSAPCompiler(nil)
	tradEnv := evaluator.NewEnvironment()
	evaluator.Eval(program, tradEnv)

	tradX, _ := tradEnv.Get("x")
	tradY, _ := tradEnv.Get("y")

	// SAP 路径
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(nil)

	slotMap := inf.GetSlotMap()
	sapEnv := evaluator.NewSAPEnvironment(len(slotMap), slotMap)
	evaluator.Eval(program, sapEnv)

	sapX, _ := sapEnv.Get("x")
	sapY, _ := sapEnv.Get("y")

	if tradX.Inspect() != sapX.Inspect() {
		t.Errorf("x 不一致：传统=%s，SAP=%s", tradX.Inspect(), sapX.Inspect())
	}
	if tradY.Inspect() != sapY.Inspect() {
		t.Errorf("y 不一致：传统=%s，SAP=%s", tradY.Inspect(), sapY.Inspect())
	}
}

// TestSlotMapSyncDirect 直接测试 Environment 的 Set/Assign 是否同步 slot
func TestSlotMapSyncDirect(t *testing.T) {
	slotMap := map[string]int{
		"x": 0,
		"y": 1,
	}
	env := object.NewSlotEnvironment(2, nil, slotMap)

	// Set 应该同步 slot
	env.Set("x", &object.Integer{Value: 42})
	slotVal := env.GetSlot(0)
	if slotVal == nil {
		t.Fatal("Set 后 slot[0] 为 nil — syncSlot 未生效")
	}
	intVal := slotVal.(*object.Integer).Value
	if intVal != 42 {
		t.Errorf("Set 后 slot 值不正确：期望 42，得到 %d", intVal)
	}

	// map 值也应该一致
	mapVal, ok := env.Get("x")
	if !ok {
		t.Fatal("Set 后 map 中找不到 x")
	}
	if mapVal.(*object.Integer).Value != 42 {
		t.Error("Set 后 map 值不正确")
	}

	// Assign 也应该同步 slot
	env.Set("y", &object.Integer{Value: 0})
	env.Assign("y", &object.Integer{Value: 99})
	slotY := env.GetSlot(1)
	if slotY == nil {
		t.Fatal("Assign 后 slot[1] 为 nil — syncSlot 未生效")
	}
	if slotY.(*object.Integer).Value != 99 {
		t.Errorf("Assign 后 slot 值不正确：期望 99，得到 %d", slotY.(*object.Integer).Value)
	}
}

// TestSlotMapSyncNoSlotMap 无 slotMap 的环境不应 panic
func TestSlotMapSyncNoSlotMap(t *testing.T) {
	env := object.NewEnvironment() // 普通 map 环境，无 slotMap

	// 不应该 panic
	env.Set("x", &object.Integer{Value: 1})
	env.Assign("x", &object.Integer{Value: 2})

	val, ok := env.Get("x")
	if !ok {
		t.Fatal("普通环境 Get 失败")
	}
	if val.(*object.Integer).Value != 2 {
		t.Error("普通环境 Assign 后值不正确")
	}
}

// ================================================================
// 第二部分：影子模式 (Shadow Execution) 回归测试
// ================================================================

// TestShadowModeBasic 影子模式基本功能：SAP 和传统路径结果一致时应通过
func TestShadowModeBasic(t *testing.T) {
	code := `
		变量 x = 10
		变量 y = 20
		变量 sum = x + y
		打印(f"sum = {sum}")
		变量 i = 0
		变量 total = 0
		当 i < 5 {
			total = total + i
			i = i + 1
		}
		打印(f"total = {total}")
	`

	program, inf := parseAndInfer(code)
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	slotMap := inf.GetSlotMap()

	sapResult, naiveResult, err := evaluator.RunShadow(program, sap, slotMap)
	if err != nil {
		t.Fatalf("RunShadow 失败: %v", err)
	}

	// 严格断言
	if assertErr := evaluator.AssertShadow(sapResult, naiveResult); assertErr != nil {
		t.Fatalf("影子模式断言失败: %v", assertErr)
	}

	// 验证打印输出包含预期内容
	if !strings.Contains(sapResult.Stdout, "sum = 30") {
		t.Errorf("SAP 输出缺少 'sum = 30': %s", sapResult.Stdout)
	}
	if !strings.Contains(sapResult.Stdout, "total = 10") {
		t.Errorf("SAP 输出缺少 'total = 10': %s", sapResult.Stdout)
	}
}

// TestShadowModeDetectsMismatch 影子模式能检测出人为制造的不一致
//
// 通过手动篡改 SAP 结果验证断言器是否工作
func TestShadowModeDetectsMismatch(t *testing.T) {
	sapResult := &evaluator.ShadowResult{
		ReturnValue: &object.Integer{Value: 42},
		Stdout:      "hello\nworld\n",
	}
	naiveResult := &evaluator.ShadowResult{
		ReturnValue: &object.Integer{Value: 42},
		Stdout:      "hello\nWORLD\n", // 大小写不同
	}

	// 返回值一致，但打印输出不一致
	err := evaluator.AssertShadow(sapResult, naiveResult)
	if err == nil {
		t.Fatal("AssertShadow 应该检测到打印输出不一致，但返回了 nil")
	}
	if !strings.Contains(err.Error(), "打印输出不一致") {
		t.Errorf("错误信息应包含 '打印输出不一致'，得到: %v", err)
	}

	// 返回值不一致
	sapResult.ReturnValue = &object.Integer{Value: 99}
	err = evaluator.AssertShadow(sapResult, naiveResult)
	if err == nil {
		t.Fatal("AssertShadow 应该检测到返回值不一致")
	}
	if !strings.Contains(err.Error(), "返回值不一致") {
		t.Errorf("错误信息应包含 '返回值不一致'，得到: %v", err)
	}
}

// TestShadowModeLenient 宽松模式：数字差异和行序差异应被忽略
func TestShadowModeLenient(t *testing.T) {
	sapResult := &evaluator.ShadowResult{
		ReturnValue: &object.Integer{Value: 42},
		Stdout:      "耗时: 5ms\n结果: 42\n",
	}
	naiveResult := &evaluator.ShadowResult{
		ReturnValue: &object.Integer{Value: 42},
		Stdout:      "结果: 42\n耗时: 3ms\n", // 行序不同 + 数字不同
	}

	// 宽松模式应该通过（数字被替换为 <N>，行被排序）
	err := evaluator.AssertShadowLenient(sapResult, naiveResult)
	if err != nil {
		t.Fatalf("宽松模式应通过，但报错: %v", err)
	}

	// 严格模式应该失败
	err = evaluator.AssertShadow(sapResult, naiveResult)
	if err == nil {
		t.Fatal("严格模式应检测到差异")
	}
}

// TestShadowModeLenientCatchesReturnValue 宽松模式仍严格检查返回值
//
// 宽松模式只忽略打印输出的数字/行序差异，返回值始终严格对比
func TestShadowModeLenientCatchesReturnValue(t *testing.T) {
	sapResult := &evaluator.ShadowResult{
		ReturnValue: &object.Integer{Value: 42},
		Stdout:      "结果: 42\n",
	}
	naiveResult := &evaluator.ShadowResult{
		ReturnValue: &object.Integer{Value: 99}, // 返回值不同！
		Stdout:      "结果: 99\n",
	}

	err := evaluator.AssertShadowLenient(sapResult, naiveResult)
	if err == nil {
		t.Fatal("宽松模式应检测到返回值不一致")
	}
	if !strings.Contains(err.Error(), "返回值不一致") {
		t.Errorf("错误信息应包含 '返回值不一致'，得到: %v", err)
	}
}

// TestShadowModeWhileLoopWithCondition 影子模式：当循环 + 条件分支
// 这正是脑裂 Bug 的场景 — 如果 Bug 回归，影子模式会立即抓出
func TestShadowModeWhileLoopWithCondition(t *testing.T) {
	code := `
		变量 running = 真
		变量 count = 0
		当 running {
			count = count + 1
			如果 count >= 10 {
				running = 假
			}
		}
		打印(f"count = {count}")
	`

	program, inf := parseAndInfer(code)
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	slotMap := inf.GetSlotMap()

	// 加超时保护
	done := make(chan struct{})
	var sapResult, naiveResult *evaluator.ShadowResult
	var shadowErr error

	go func() {
		sapResult, naiveResult, shadowErr = evaluator.RunShadow(program, sap, slotMap)
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("超时：影子模式执行卡死 — 脑裂 Bug 可能已回归")
	}

	if shadowErr != nil {
		t.Fatalf("RunShadow 失败: %v", shadowErr)
	}

	// 严格断言（这个脚本没有并发/时间戳，应该严格通过）
	if assertErr := evaluator.AssertShadow(sapResult, naiveResult); assertErr != nil {
		t.Fatalf("影子模式断言失败（脑裂 Bug 可能已回归）: %v", assertErr)
	}

	// 验证 count = 10
	if !strings.Contains(sapResult.Stdout, "count = 10") {
		t.Errorf("输出应包含 'count = 10': %s", sapResult.Stdout)
	}
}

// ================================================================
// 第三部分：Feature Toggle 回归测试
// ================================================================

// TestFeatureToggleNoSAP 关闭 SAP 后应走纯 Tree-Walk
func TestFeatureToggleNoSAP(t *testing.T) {
	code := `
		变量 x = 1
		变量 i = 0
		对于 i = 0; i < 100; i = i + 1 {
			x = x + i
		}
	`

	program, _ := parseAndInfer(code)

	// 关闭 SAP
	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	defer evaluator.SetSAPCompiler(oldSAP)

	env := evaluator.NewEnvironment()
	evaluator.Eval(program, env)

	xObj, ok := env.Get("x")
	if !ok {
		t.Fatal("x 未定义")
	}
	x := xObj.(*object.Integer).Value
	// x = 1 + (0+1+2+...+99) = 1 + 4950 = 4951
	expected := int64(4951)
	if x != expected {
		t.Errorf("期望 x=%d，得到 %d", expected, x)
	}
}

// TestFeatureToggleSAPvsNoSAP SAP 开启和关闭的结果应一致
func TestFeatureToggleSAPvsNoSAP(t *testing.T) {
	code := `
		变量 x = 0
		变量 i = 0
		对于 i = 0; i < 50; i = i + 1 {
			如果 i % 2 == 0 {
				x = x + i
			}
		}
	`

	program, inf := parseAndInfer(code)

	// 传统路径
	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	tradEnv := evaluator.NewEnvironment()
	evaluator.Eval(program, tradEnv)
	tradX, _ := tradEnv.Get("x")

	// SAP 路径
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	slotMap := inf.GetSlotMap()
	sapEnv := evaluator.NewSAPEnvironment(len(slotMap), slotMap)
	evaluator.Eval(program, sapEnv)
	sapX, _ := sapEnv.Get("x")

	if tradX.Inspect() != sapX.Inspect() {
		t.Errorf("结果不一致：传统=%s，SAP=%s", tradX.Inspect(), sapX.Inspect())
	}

	// 验证具体值：x = 0+2+4+...+48 = 2*(0+1+2+...+24) = 2*300 = 600
	expected := int64(600)
	actual := sapX.(*object.Integer).Value
	if actual != expected {
		t.Errorf("期望 x=%d，得到 %d", expected, actual)
	}
}
