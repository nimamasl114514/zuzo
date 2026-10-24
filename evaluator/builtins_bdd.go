package evaluator

// builtins_bdd.go — BDD 测试框架（封装 Go testing 风格）
//
// 提供 Given/When/Then 风格的行为驱动测试：
//
//	测试套件("数组操作", 函数() {
//	    测试前(函数() { ... })   // beforeEach
//	    测试后(函数() { ... })   // afterEach
//
//	    测试用例("推入元素", 函数() {
//	        变量 arr = [1, 2, 3]
//	        推入(arr, 4)
//	        断言相等(长度(arr), 4)
//	        断言相等(arr[3], 4)
//	    })
//	})
//
//	运行测试()

import (
	"fmt"
	"strings"
	"time"

	"zuzo/object"
)

// ================================================================
// BDD 状态模型
// ================================================================

// bddTestCase 单个测试用例
type bddTestCase struct {
	name     string
	fn       *object.Function
	failed   bool
	errors   []string
	skipped  bool
	duration time.Duration
}

// bddTestSuite 测试套件
type bddTestSuite struct {
	name       string
	tests      []*bddTestCase
	beforeEach *object.Function
	afterEach  *object.Function
}

// bddState 全局 BDD 状态（包级变量）
type bddState struct {
	suites       []*bddTestSuite
	currentSuite *bddTestSuite
	currentTest  *bddTestCase
	totalAsserts int
	totalPassed  int
	totalFailed  int
	totalSkipped int
}

var bdd = &bddState{}

// resetBDD 重置 BDD 状态（用于重复运行测试）
func resetBDD() {
	bdd = &bddState{}
}

// ================================================================
// 内置函数实现
// ================================================================

// 测试套件(名称, 函数) → 空
// 注册一个测试套件并立即执行函数体以收集测试用例
func builtinTestSuite(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("测试套件() 期望 2 个参数（名称, 函数），得到 %d 个", len(args))
	}
	name, ok := args[0].(*object.String)
	if !ok {
		return newError("测试套件() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	fn, ok := args[1].(*object.Function)
	if !ok {
		return newError("测试套件() 第二个参数必须是函数，得到 %s", args[1].Type())
	}

	suite := &bddTestSuite{name: name.Value}
	bdd.suites = append(bdd.suites, suite)

	// 设为当前套件，执行函数体收集测试用例
	prevSuite := bdd.currentSuite
	bdd.currentSuite = suite
	applyFunction(fn, nil)
	bdd.currentSuite = prevSuite

	return NULL
}

// 测试用例(名称, 函数) → 空
// 在当前套件中注册一个测试用例（不立即执行）
func builtinTestCase(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("测试用例() 期望 2 个参数（名称, 函数），得到 %d 个", len(args))
	}
	name, ok := args[0].(*object.String)
	if !ok {
		return newError("测试用例() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	fn, ok := args[1].(*object.Function)
	if !ok {
		return newError("测试用例() 第二个参数必须是函数，得到 %s", args[1].Type())
	}

	if bdd.currentSuite == nil {
		return newError("测试用例() 必须在测试套件() 内部调用")
	}

	tc := &bddTestCase{name: name.Value, fn: fn}
	bdd.currentSuite.tests = append(bdd.currentSuite.tests, tc)

	return NULL
}

// 测试前(函数) → 空
// 注册 beforeEach 钩子（每个测试用例前执行）
func builtinBeforeEach(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("测试前() 期望 1 个参数（函数），得到 %d 个", len(args))
	}
	fn, ok := args[0].(*object.Function)
	if !ok {
		return newError("测试前() 参数必须是函数，得到 %s", args[0].Type())
	}
	if bdd.currentSuite == nil {
		return newError("测试前() 必须在测试套件() 内部调用")
	}
	bdd.currentSuite.beforeEach = fn
	return NULL
}

// 测试后(函数) → 空
// 注册 afterEach 钩子（每个测试用例后执行）
func builtinAfterEach(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("测试后() 期望 1 个参数（函数），得到 %d 个", len(args))
	}
	fn, ok := args[0].(*object.Function)
	if !ok {
		return newError("测试后() 参数必须是函数，得到 %s", args[0].Type())
	}
	if bdd.currentSuite == nil {
		return newError("测试后() 必须在测试套件() 内部调用")
	}
	bdd.currentSuite.afterEach = fn
	return NULL
}

// 测试跳过(原因) → 空
// 跳过当前测试用例
func builtinTestSkip(args ...object.Object) object.Object {
	reason := ""
	if len(args) >= 1 {
		if s, ok := args[0].(*object.String); ok {
			reason = s.Value
		}
	}
	if bdd.currentTest != nil {
		bdd.currentTest.skipped = true
		if reason != "" {
			bdd.currentTest.errors = append(bdd.currentTest.errors, "跳过: "+reason)
		}
	}
	return NULL
}

// ================================================================
// 断言函数
// ================================================================

// 断言相等(期望, 实际) → 空
func builtinAssertEqual(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("断言相等() 期望 2 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	if objectsEqual(args[0], args[1]) {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望 %s, 得到 %s", args[0].Inspect(), args[1].Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言不相等(a, b) → 空
func builtinAssertNotEqual(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("断言不相等() 期望 2 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	if !objectsEqual(args[0], args[1]) {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望不相等, 但两者均为 %s", args[0].Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言为真(值) → 空
func builtinAssertTrue(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("断言为真() 期望 1 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	if b, ok := args[0].(*object.Boolean); ok && b.Value {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望为真, 得到 %s", args[0].Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言为假(值) → 空
func builtinAssertFalse(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("断言为假() 期望 1 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	if b, ok := args[0].(*object.Boolean); ok && !b.Value {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望为假, 得到 %s", args[0].Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言为空(值) → 空
func builtinAssertNull(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("断言为空() 期望 1 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	if _, ok := args[0].(*object.Null); ok {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望为空, 得到 %s", args[0].Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言非空(值) → 空
func builtinAssertNotNull(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("断言非空() 期望 1 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	if _, ok := args[0].(*object.Null); !ok {
		bdd.totalPassed++
		return NULL
	}
	msg := "断言失败: 期望非空, 得到 空"
	recordAssertionFailure(msg)
	return NULL
}

// 断言包含(容器, 元素) → 空
// 支持字符串包含子串、数组包含元素
func builtinAssertContains(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("断言包含() 期望 2 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++

	container := args[0]
	item := args[1]
	found := false

	switch c := container.(type) {
	case *object.String:
		if s, ok := item.(*object.String); ok {
			found = strings.Contains(c.Value, s.Value)
		}
	case *object.Array:
		for _, elem := range c.Elements {
			if objectsEqual(elem, item) {
				found = true
				break
			}
		}
	}

	if found {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: %s 不包含 %s", container.Inspect(), item.Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言大于(a, b) → 空  （a > b）
func builtinAssertGreater(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("断言大于() 期望 2 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	cmp, ok := compareObjects(args[0], args[1])
	if ok && cmp > 0 {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望 %s > %s", args[0].Inspect(), args[1].Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言小于(a, b) → 空  （a < b）
func builtinAssertLess(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("断言小于() 期望 2 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	cmp, ok := compareObjects(args[0], args[1])
	if ok && cmp < 0 {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望 %s < %s", args[0].Inspect(), args[1].Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言大于等于(a, b) → 空  （a >= b）
func builtinAssertGreaterEqual(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("断言大于等于() 期望 2 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	cmp, ok := compareObjects(args[0], args[1])
	if ok && cmp >= 0 {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望 %s >= %s", args[0].Inspect(), args[1].Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言小于等于(a, b) → 空  （a <= b）
func builtinAssertLessEqual(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("断言小于等于() 期望 2 个参数，得到 %d 个", len(args))
	}
	bdd.totalAsserts++
	cmp, ok := compareObjects(args[0], args[1])
	if ok && cmp <= 0 {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望 %s <= %s", args[0].Inspect(), args[1].Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言错误(函数) → 空
// 执行函数，断言其抛出错误
func builtinAssertError(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("断言错误() 期望 1 个参数，得到 %d 个", len(args))
	}
	fn, ok := args[0].(*object.Function)
	if !ok {
		return newError("断言错误() 参数必须是函数，得到 %s", args[0].Type())
	}
	bdd.totalAsserts++

	result := applyFunction(fn, nil)
	if isError(result) {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望抛出错误, 但返回 %s", result.Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// 断言无错误(函数) → 空
// 执行函数，断言其不抛出错误
func builtinAssertNoError(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("断言无错误() 期望 1 个参数，得到 %d 个", len(args))
	}
	fn, ok := args[0].(*object.Function)
	if !ok {
		return newError("断言无错误() 参数必须是函数，得到 %s", args[0].Type())
	}
	bdd.totalAsserts++

	result := applyFunction(fn, nil)
	if !isError(result) {
		bdd.totalPassed++
		return NULL
	}
	msg := fmt.Sprintf("断言失败: 期望无错误, 但得到 %s", result.Inspect())
	recordAssertionFailure(msg)
	return NULL
}

// ================================================================
// 测试运行器
// ================================================================

// 运行测试() → 布尔
// 执行所有已注册的测试套件，输出报告，返回是否全部通过
func builtinRunTests(args ...object.Object) object.Object {
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("  zuzo BDD 测试框架")
	fmt.Println(strings.Repeat("=", 60))

	for _, suite := range bdd.suites {
		fmt.Printf("\n=== 套件: %s ===\n", suite.name)
		for _, tc := range suite.tests {
			runSingleTest(suite, tc)
		}
	}

	// 汇总
	fmt.Println("\n" + strings.Repeat("-", 60))
	totalTests := 0
	for _, s := range bdd.suites {
		totalTests += len(s.tests)
	}
	passedTests := totalTests - bdd.totalFailed - bdd.totalSkipped
	fmt.Printf("测试用例: %d 通过, %d 失败, %d 跳过 (共 %d)\n",
		passedTests, bdd.totalFailed, bdd.totalSkipped, totalTests)
	fmt.Printf("断言:     %d 通过, %d 失败 (共 %d)\n",
		bdd.totalPassed, bdd.totalFailed, bdd.totalAsserts)
	fmt.Println(strings.Repeat("=", 60))

	allPassed := bdd.totalFailed == 0
	if allPassed {
		fmt.Println("✓ 全部通过")
	} else {
		fmt.Printf("✗ %d 个断言失败\n", bdd.totalFailed)
	}

	return nativeBoolToBooleanObject(allPassed)
}

// runSingleTest 执行单个测试用例
func runSingleTest(suite *bddTestSuite, tc *bddTestCase) {
	bdd.currentTest = tc

	// 执行 beforeEach
	if suite.beforeEach != nil {
		applyFunction(suite.beforeEach, nil)
	}

	// 执行测试
	start := time.Now()
	result := applyFunction(tc.fn, nil)
	tc.duration = time.Since(start)

	// 检查运行时错误
	if isError(result) {
		tc.failed = true
		tc.errors = append(tc.errors, fmt.Sprintf("运行时错误: %s", result.Inspect()))
		bdd.totalFailed++
	}

	// 执行 afterEach
	if suite.afterEach != nil {
		applyFunction(suite.afterEach, nil)
	}

	// 输出结果
	if tc.skipped {
		bdd.totalSkipped++
		fmt.Printf("  --- 跳过: %s", tc.name)
		if len(tc.errors) > 0 {
			fmt.Printf(" (%s)", tc.errors[0])
		}
		fmt.Println()
	} else if tc.failed {
		fmt.Printf("  --- 失败: %s (%.3fs)\n", tc.name, tc.duration.Seconds())
		for _, e := range tc.errors {
			fmt.Printf("      %s\n", e)
		}
	} else {
		fmt.Printf("  --- 通过: %s (%.3fs)\n", tc.name, tc.duration.Seconds())
	}

	bdd.currentTest = nil
}

// ================================================================
// 辅助函数
// ================================================================

// recordAssertionFailure 记录断言失败
func recordAssertionFailure(msg string) {
	bdd.totalFailed++
	if bdd.currentTest != nil {
		bdd.currentTest.failed = true
		bdd.currentTest.errors = append(bdd.currentTest.errors, msg)
	}
}

// objectsEqual 深度比较两个对象是否相等
func objectsEqual(a, b object.Object) bool {
	if a == nil || b == nil {
		return a == b
	}
	switch av := a.(type) {
	case *object.Integer:
		if bv, ok := b.(*object.Integer); ok {
			return av.Value == bv.Value
		}
	case *object.Float:
		if bv, ok := b.(*object.Float); ok {
			return av.Value == bv.Value
		}
	case *object.String:
		if bv, ok := b.(*object.String); ok {
			return av.Value == bv.Value
		}
	case *object.Boolean:
		if bv, ok := b.(*object.Boolean); ok {
			return av.Value == bv.Value
		}
	case *object.Null:
		_, ok := b.(*object.Null)
		return ok
	case *object.Array:
		if bv, ok := b.(*object.Array); ok {
			if len(av.Elements) != len(bv.Elements) {
				return false
			}
			for i := range av.Elements {
				if !objectsEqual(av.Elements[i], bv.Elements[i]) {
					return false
				}
			}
			return true
		}
	case *object.Hash:
		if bv, ok := b.(*object.Hash); ok {
			if len(av.Pairs) != len(bv.Pairs) {
				return false
			}
			for k, pairA := range av.Pairs {
				pairB, ok := bv.Pairs[k]
				if !ok {
					return false
				}
				if !objectsEqual(pairA.Value, pairB.Value) {
					return false
				}
			}
			return true
		}
	}
	return false
}

// compareObjects 比较两个可排序对象，返回 -1/0/1
func compareObjects(a, b object.Object) (int, bool) {
	switch av := a.(type) {
	case *object.Integer:
		if bv, ok := b.(*object.Integer); ok {
			switch {
			case av.Value < bv.Value:
				return -1, true
			case av.Value > bv.Value:
				return 1, true
			default:
				return 0, true
			}
		}
		if bv, ok := b.(*object.Float); ok {
			f := float64(av.Value)
			switch {
			case f < bv.Value:
				return -1, true
			case f > bv.Value:
				return 1, true
			default:
				return 0, true
			}
		}
	case *object.Float:
		if bv, ok := b.(*object.Float); ok {
			switch {
			case av.Value < bv.Value:
				return -1, true
			case av.Value > bv.Value:
				return 1, true
			default:
				return 0, true
			}
		}
		if bv, ok := b.(*object.Integer); ok {
			f := float64(bv.Value)
			switch {
			case av.Value < f:
				return -1, true
			case av.Value > f:
				return 1, true
			default:
				return 0, true
			}
		}
	case *object.String:
		if bv, ok := b.(*object.String); ok {
			return strings.Compare(av.Value, bv.Value), true
		}
	}
	return 0, false
}
