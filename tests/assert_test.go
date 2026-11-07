package tests

// assert_test.go — 语言级 断言() 内置单测（第34轮 w3）
//
// 覆盖口径：
//   · 真值静默通过 → 返回 NULL，无错误对象
//   · 假值失败 → 错误对象文本「断言失败: <消息>」
//   · 无消息失败 → 文本恰为「断言失败」
//   · 参数个数错误 → 错误码 E900
//   · 自定义真值口径 → 0 / "" / 空 均判假（与 当/如果 的 isTruthy 完全一致）

import (
	"strings"
	"testing"

	"zuzo/evaluator"
	"zuzo/lexer"
	"zuzo/object"
	"zuzo/parser"
)

// evalAssertCase 解析并执行一段 zuzo 源码，返回求值结果。
func evalAssertCase(t *testing.T, code string) object.Object {
	t.Helper()

	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseProgram()
	if p.Diagnostics().HasErrors() {
		t.Fatalf("解析失败：%s", p.Diagnostics().Render())
	}

	env := evaluator.NewEnvironment()
	return evaluator.Eval(program, env)
}

// assertErrOf 断言结果是错误对象，并返回它。
func assertErrOf(t *testing.T, res object.Object) *object.Error {
	t.Helper()

	errObj, ok := res.(*object.Error)
	if !ok {
		t.Fatalf("期望错误对象，实际得到 %s（%s）", res.Type(), res.Inspect())
	}
	return errObj
}

// 真值通过：断言(1==1) 返回空，不产生错误。
func TestAssert_TruthyPasses(t *testing.T) {
	res := evalAssertCase(t, `断言(1==1)`)

	if res.Type() != object.NULL_OBJ {
		t.Fatalf("断言成功应返回空，实际得到 %s（%s）", res.Type(), res.Inspect())
	}
}

// 真值通过：后续语句照常执行，表达式值不污染返回值。
func TestAssert_TruthyKeepsRunning(t *testing.T) {
	res := evalAssertCase(t, "断言(1==1);\n变量 n = 41;\nn + 1")

	got, ok := res.(*object.Integer)
	if !ok {
		t.Fatalf("期望整数结果，实际得到 %s（%s）", res.Type(), res.Inspect())
	}
	if got.Value != 42 {
		t.Fatalf("期望 42，实际得到 %d", got.Value)
	}
}

// 假值失败：错误文本含自定义消息。
func TestAssert_FalsyReportsMessage(t *testing.T) {
	res := evalAssertCase(t, `断言(1==2, "加法错了")`)

	errObj := assertErrOf(t, res)
	if !strings.Contains(errObj.Message, "加法错了") {
		t.Fatalf("错误文本应包含「加法错了」，实际为 %q", errObj.Message)
	}
	if !strings.Contains(errObj.Message, "断言失败") {
		t.Fatalf("错误文本应以「断言失败」开头，实际为 %q", errObj.Message)
	}
	want := "断言失败: 加法错了"
	if errObj.Message != want {
		t.Fatalf("错误文本期望 %q，实际 %q", want, errObj.Message)
	}
}

// 假值失败且无消息：错误文本恰为「断言失败」。
func TestAssert_FalsyWithoutMessage(t *testing.T) {
	errObj := assertErrOf(t, evalAssertCase(t, `断言(1==2)`))

	if errObj.Message != "断言失败" {
		t.Fatalf("错误文本期望「断言失败」，实际 %q", errObj.Message)
	}
}

// 真值口径与 isTruthy 一致：空 / 0 / "" 均为假。
func TestAssert_FalsyValuesFollowTruthy(t *testing.T) {
	cases := []struct {
		name string
		code string
	}{
		{"空值", `断言(空)`},
		{"零", `断言(0)`},
		{"零浮点", `断言(0.0)`},
		{"空字符串", `断言("")`},
		{"空数组", `断言([])`},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			errObj := assertErrOf(t, evalAssertCase(t, c.code))
			if errObj.Message != "断言失败" {
				t.Fatalf("%s：错误文本期望「断言失败」，实际 %q", c.name, errObj.Message)
			}
		})
	}
}

// 参数个数错误：无参数 → E900。
func TestAssert_ArgCountTooFew(t *testing.T) {
	errObj := assertErrOf(t, evalAssertCase(t, `断言()`))

	if errObj.Code != "E900" {
		t.Fatalf("错误码期望 E900，实际 %q", errObj.Code)
	}
	if !strings.Contains(errObj.Message, "期望 1~2 个参数") {
		t.Fatalf("错误文本应说明参数个数，实际 %q", errObj.Message)
	}
}

// 参数个数错误：多余参数 → E900。
func TestAssert_ArgCountTooMany(t *testing.T) {
	errObj := assertErrOf(t, evalAssertCase(t, `断言(1==1, "消息", "多余")`))

	if errObj.Code != "E900" {
		t.Fatalf("错误码期望 E900，实际 %q", errObj.Code)
	}
	if !strings.Contains(errObj.Message, "得到 3 个") {
		t.Fatalf("错误文本应含实际参数个数，实际 %q", errObj.Message)
	}
}
