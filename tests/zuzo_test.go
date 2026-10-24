package tests

// zuzo_test.go — 主语言测试：词法、语法、求值（从根目录迁移）

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"zuzo/evaluator"
	"zuzo/lexer"
	"zuzo/object"
	"zuzo/parser"
	"zuzo/token"
)

// ==================== 词法分析器测试 ====================

func TestLexer(t *testing.T) {
	tests := []struct {
		input    string
		expected []struct {
			Type    string
			Literal string
		}
	}{
		{
			`变量 x = 10`,
			[]struct {
				Type    string
				Literal string
			}{
				{"变量", "变量"},
				{"IDENT", "x"},
				{"=", "="},
				{"INT", "10"},
				{"EOF", ""},
			},
		},
		{
			`函数 加(a, b) { 返回 a + b }`,
			[]struct {
				Type    string
				Literal string
			}{
				{"函数", "函数"},
				{"IDENT", "加"},
				{"(", "("},
				{"IDENT", "a"},
				{",", ","},
				{"IDENT", "b"},
				{")", ")"},
				{"{", "{"},
				{"返回", "返回"},
				{"IDENT", "a"},
				{"+", "+"},
				{"IDENT", "b"},
				{"}", "}"},
				{"EOF", ""},
			},
		},
		{
			`如果 真 并且 非 假 { 打印("ok") }`,
			[]struct {
				Type    string
				Literal string
			}{
				{"如果", "如果"},
				{"真", "真"},
				{"并且", "并且"},
				{"非", "非"},
				{"假", "假"},
				{"{", "{"},
				{"IDENT", "打印"},
				{"(", "("},
				{"STRING", "ok"},
				{")", ")"},
				{"}", "}"},
				{"EOF", ""},
			},
		},
	}

	for i, tt := range tests {
		l := lexer.New(tt.input)
		for j, expected := range tt.expected {
			tok := l.NextToken()
			if tok.Type != token.TokenType(expected.Type) && tok.Literal != expected.Literal {
				t.Errorf("测试 %d, token %d: 期望 type=%s lit=%s, 得到 type=%s lit=%s",
					i, j, expected.Type, expected.Literal, tok.Type, tok.Literal)
			}
		}
	}
}

func TestLexerStringEscape(t *testing.T) {
	l := lexer.New(`"hello\nworld\t!"`)
	tok := l.NextToken()
	if tok.Type != token.TokenType("STRING") {
		t.Fatalf("期望 STRING, 得到 %s", tok.Type)
	}
	if tok.Literal != "hello\nworld\t!" {
		t.Fatalf("转义错误: 得到 %q", tok.Literal)
	}
}

func TestLexerComment(t *testing.T) {
	l := lexer.New(`变量 x = 1 // 这是注释
变量 y = 2`)
	tokens := []token.Token{}
	for {
		tok := l.NextToken()
		tokens = append(tokens, tok)
		if tok.Type == token.TokenType("EOF") {
			break
		}
	}
	// 注释应该被跳过，y 应该能正常解析
	foundY := false
	for _, tok := range tokens {
		if tok.Literal == "y" {
			foundY = true
			break
		}
	}
	if !foundY {
		t.Error("注释后的 y 未被正确解析")
	}
}

// ==================== 语法分析器测试 ====================

func TestParserLetStatement(t *testing.T) {
	input := `变量 x = 5; 变量 y = 10; 变量 结果 = x + y;`
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		t.Fatalf("解析错误: %v", p.Errors())
	}
	if len(program.Statements) != 3 {
		t.Fatalf("期望 3 条语句, 得到 %d", len(program.Statements))
	}
}

func TestParserErrors(t *testing.T) {
	input := `变量 = 5;`
	l := lexer.New(input)
	p := parser.New(l)
	_ = p.ParseProgram()

	if len(p.Errors()) == 0 {
		t.Fatal("期望有解析错误, 但没有报错")
	}
}

func TestParserForStatement(t *testing.T) {
	input := `对于 i = 0; i < 10; i = i + 1 { 打印(i) }`
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		t.Fatalf("解析错误: %v", p.Errors())
	}
	if len(program.Statements) != 1 {
		t.Fatalf("期望 1 条语句, 得到 %d", len(program.Statements))
	}
}

func TestParserElseIf(t *testing.T) {
	input := `如果 x > 5 { 打印("大") } 否则 如果 x > 0 { 打印("小") } 否则 { 打印("零") }`
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if len(p.Errors()) != 0 {
		t.Fatalf("解析错误: %v", p.Errors())
	}
	if len(program.Statements) != 1 {
		t.Fatalf("期望 1 条语句, 得到 %d", len(program.Statements))
	}
}

// ==================== 求值器测试 ====================

func testEval(input string) object.Object {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	env := evaluator.NewEnvironment()
	return evaluator.Eval(program, env)
}

func TestEvalInteger(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"5", 5},
		{"10", 10},
		{"-5", -5},
		{"5 + 5", 10},
		{"10 - 3", 7},
		{"4 * 3", 12},
		{"20 / 4", 5},
		{"17 % 5", 2},
		{"2 + 3 * 4", 14},
		{"(2 + 3) * 4", 20},
		{"-10 + 5", -5},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		if !testIntegerObject(t, result, tt.expected) {
			t.Errorf("输入: %s", tt.input)
		}
	}
}

func TestEvalString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`"你好"`, "你好"},
		{`"hello" + " " + "world"`, "hello world"},
		{`"华" + "言"`, "华言"},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		str, ok := result.(*object.String)
		if !ok {
			t.Errorf("期望 String, 得到 %T (%s)", result, result.Inspect())
			continue
		}
		if str.Value != tt.expected {
			t.Errorf("期望 %q, 得到 %q", tt.expected, str.Value)
		}
	}
}

func TestEvalBoolean(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"真", true},
		{"假", false},
		{"1 < 2", true},
		{"1 > 2", false},
		{"1 == 1", true},
		{"1 != 1", false},
		{"1 <= 1", true},
		{"1 >= 2", false},
		{"真 并且 假", false},
		{"真 或者 假", true},
		{"非 真", false},
		{"非 假", true},
		{"(1 > 0) 并且 (2 > 1)", true},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		b, ok := result.(*object.Boolean)
		if !ok {
			t.Errorf("输入 %s: 期望 Boolean, 得到 %T", tt.input, result)
			continue
		}
		if b.Value != tt.expected {
			t.Errorf("输入 %s: 期望 %v, 得到 %v", tt.input, tt.expected, b.Value)
		}
	}
}

func TestEvalIfElse(t *testing.T) {
	tests := []struct {
		input    string
		expected any
	}{
		{"如果 真 { 10 } 否则 { 20 }", int64(10)},
		{"如果 假 { 10 } 否则 { 20 }", int64(20)},
		{"如果 1 > 2 { 10 } 否则 { 20 }", int64(20)},
		{"如果 2 > 1 { 10 } 否则 如果 3 > 2 { 20 } 否则 { 30 }", int64(10)},
		{"如果 1 > 2 { 10 } 否则 如果 3 > 2 { 20 } 否则 { 30 }", int64(20)},
		{"如果 1 > 2 { 10 } 否则 如果 3 > 4 { 20 } 否则 { 30 }", int64(30)},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		switch expected := tt.expected.(type) {
		case int64:
			if !testIntegerObject(t, result, expected) {
				t.Errorf("输入: %s", tt.input)
			}
		}
	}
}

func TestEvalWhileLoop(t *testing.T) {
	input := `
变量 sum = 0
变量 i = 1
当 i <= 100 {
    sum = sum + i
    i = i + 1
}
sum
`
	result := testEval(input)
	if !testIntegerObject(t, result, 5050) {
		t.Errorf("1+2+...+100 应该等于 5050")
	}
}

func TestEvalForLoop(t *testing.T) {
	input := `
变量 sum = 0
对于 i = 1; i <= 10; i = i + 1 {
    sum = sum + i
}
sum
`
	result := testEval(input)
	if !testIntegerObject(t, result, 55) {
		t.Errorf("1+2+...+10 应该等于 55")
	}
}

func TestEvalFunction(t *testing.T) {
	input := `
函数 平方(x) {
    返回 x * x
}
平方(7)
`
	result := testEval(input)
	if !testIntegerObject(t, result, 49) {
		t.Errorf("7 的平方应该等于 49")
	}
}

func TestEvalRecursiveFunction(t *testing.T) {
	input := `
函数 阶乘(n) {
    如果 n <= 1 {
        返回 1
    }
    返回 n * 阶乘(n - 1)
}
阶乘(10)
`
	result := testEval(input)
	if !testIntegerObject(t, result, 3628800) {
		t.Errorf("10! 应该等于 3628800")
	}
}

func TestEvalClosure(t *testing.T) {
	input := `
函数 计数器() {
    变量 count = 0
    函数 递增() {
        count = count + 1
        返回 count
    }
    返回 递增
}
变量 c = 计数器()
c() + c() + c()
`
	result := testEval(input)
	if !testIntegerObject(t, result, 6) {
		t.Errorf("闭包计数器三次递增应为 1+2+3=6")
	}
}

func TestEvalArray(t *testing.T) {
	input := `[1, 2, 3, 4, 5]`
	result := testEval(input)
	arr, ok := result.(*object.Array)
	if !ok {
		t.Fatalf("期望 Array, 得到 %T", result)
	}
	if len(arr.Elements) != 5 {
		t.Fatalf("期望长度 5, 得到 %d", len(arr.Elements))
	}
}

func TestEvalArrayIndex(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{"[1, 2, 3][0]", 1},
		{"[1, 2, 3][1]", 2},
		{"[1, 2, 3][2]", 3},
		{`变量 a = [10, 20, 30]; a[1]`, 20},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		if !testIntegerObject(t, result, tt.expected) {
			t.Errorf("输入: %s", tt.input)
		}
	}
}

func TestEvalArrayMutation(t *testing.T) {
	input := `
变量 a = [1, 2, 3]
a[1] = 42
a[1]
`
	result := testEval(input)
	if !testIntegerObject(t, result, 42) {
		t.Errorf("数组元素赋值后应为 42")
	}
}

func TestEvalHash(t *testing.T) {
	input := `变量 h = {"a": 1, "b": 2, "c": 3}; h["b"]`
	result := testEval(input)
	if !testIntegerObject(t, result, 2) {
		t.Errorf(`哈希表 h["b"] 应为 2`)
	}
}

func TestEvalBuiltinLen(t *testing.T) {
	tests := []struct {
		input    string
		expected int64
	}{
		{`长度("hello")`, 5},
		{`长度("你好世界")`, 4},
		{`长度([1, 2, 3, 4])`, 4},
		{`长度("")`, 0},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		if !testIntegerObject(t, result, tt.expected) {
			t.Errorf("输入: %s", tt.input)
		}
	}
}

func TestEvalBuiltinPush(t *testing.T) {
	input := `
变量 a = [1, 2]
推入(a, 3)
推入(a, 4)
长度(a)
`
	result := testEval(input)
	if !testIntegerObject(t, result, 4) {
		t.Errorf("推入两次后长度应为 4")
	}
}

func TestEvalBuiltinContains(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{`包含("hello world", "world")`, true},
		{`包含("hello", "xyz")`, false},
		{`包含([1, 2, 3], 2)`, true},
		{`包含([1, 2, 3], 5)`, false},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		b, ok := result.(*object.Boolean)
		if !ok {
			t.Errorf("输入 %s: 期望 Boolean, 得到 %T", tt.input, result)
			continue
		}
		if b.Value != tt.expected {
			t.Errorf("输入 %s: 期望 %v, 得到 %v", tt.input, tt.expected, b.Value)
		}
	}
}

func TestEvalBuiltinTypeConv(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`类型(42)`, "整数"},
		{`类型("hello")`, "字符串"},
		{`类型(真)`, "布尔"},
		{`类型(空)`, "空"},
		{`类型([1, 2])`, "数组"},
		{`字符串(42)`, "42"},
		{`整数("123")`, "123"},
		{`整数(真)`, "1"},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		str, ok := result.(*object.String)
		if !ok {
			// 可能返回整数
			if intObj, ok := result.(*object.Integer); ok {
				if fmt.Sprintf("%d", intObj.Value) != tt.expected {
					t.Errorf("输入 %s: 期望 %s, 得到 %d", tt.input, tt.expected, intObj.Value)
				}
				continue
			}
			t.Errorf("输入 %s: 期望 String, 得到 %T (%s)", tt.input, result, result.Inspect())
			continue
		}
		if str.Value != tt.expected {
			t.Errorf("输入 %s: 期望 %s, 得到 %s", tt.input, tt.expected, str.Value)
		}
	}
}

func TestEvalError(t *testing.T) {
	tests := []struct {
		input         string
		expectedError string
	}{
		{"未知变量", "未定义的标识符"},
		{"1 / 0", "除零错误"},
		{`变量 x = "hello"; x - "world"`, "不支持运算符"},
		{`长度(42)`, "不支持类型"},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		err, ok := result.(*object.Error)
		if !ok {
			t.Errorf("输入 %s: 期望 Error, 得到 %T (%s)", tt.input, result, result.Inspect())
			continue
		}
		if !strings.Contains(err.Message, tt.expectedError) {
			t.Errorf("输入 %s: 期望错误包含 %q, 得到 %q", tt.input, tt.expectedError, err.Message)
		}
	}
}

func TestEvalStringComparison(t *testing.T) {
	input := `"apple" < "banana"`
	result := testEval(input)
	b, ok := result.(*object.Boolean)
	if !ok {
		t.Fatalf("期望 Boolean, 得到 %T", result)
	}
	if !b.Value {
		t.Error(`"apple" < "banana" 应该为 真`)
	}
}

func TestEvalShortCircuit(t *testing.T) {
	// 短路求值: 如果左边为假, 右边不应被求值
	input := `
变量 called = 假
函数 sideEffect() {
    called = 真
    返回 真
}
假 并且 sideEffect()
called
`
	result := testEval(input)
	b, ok := result.(*object.Boolean)
	if !ok {
		t.Fatalf("期望 Boolean, 得到 %T", result)
	}
	if b.Value {
		t.Error("短路求值失败: 并且 左侧为假时不应调用右侧函数")
	}
}

// ==================== 辅助函数 ====================

func testIntegerObject(t *testing.T, obj object.Object, expected int64) bool {
	result, ok := obj.(*object.Integer)
	if !ok {
		t.Errorf("期望 Integer, 得到 %T (%s)", obj, obj.Inspect())
		return false
	}
	if result.Value != expected {
		t.Errorf("期望 %d, 得到 %d", expected, result.Value)
		return false
	}
	return true
}

func TestEvalJoinBuiltin(t *testing.T) {
	input := `连接([1, 2, 3], ", ")`
	result := testEval(input)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "1, 2, 3" {
		t.Errorf("期望 '1, 2, 3', 得到 %q", str.Value)
	}
}

func TestEvalNestedFunction(t *testing.T) {
	input := `
函数 add(x) {
    函数 inner(y) {
        返回 x + y
    }
    返回 inner
}
变量 add5 = add(5)
add5(3) + add5(10)
`
	result := testEval(input)
	if !testIntegerObject(t, result, 23) {
		t.Errorf("5+3 + 5+10 = 8+15 = 23")
	}
}

// ==================== 浮点数测试 ====================

func testFloatObject(t *testing.T, obj object.Object, expected float64) bool {
	result, ok := obj.(*object.Float)
	if !ok {
		t.Errorf("期望 Float, 得到 %T (%s)", obj, obj.Inspect())
		return false
	}
	// 使用容差比较，避免浮点精度问题
	diff := result.Value - expected
	if diff < 0 {
		diff = -diff
	}
	if diff > 1e-9 {
		t.Errorf("期望 %v, 得到 %v", expected, result.Value)
		return false
	}
	return true
}

func TestEvalFloatLiteral(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"3.14", 3.14},
		{"0.5", 0.5},
		{"-2.5", -2.5},
		{".0", 0.0},
		{"100.0", 100.0},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		if !testFloatObject(t, result, tt.expected) {
			t.Errorf("输入: %s", tt.input)
		}
	}
}

func TestEvalFloatArithmetic(t *testing.T) {
	tests := []struct {
		input    string
		expected float64
	}{
		{"3.14 + 1.0", 4.14},
		{"10.5 - 0.5", 10.0},
		{"2.5 * 4.0", 10.0},
		{"10.0 / 4.0", 2.5},
		{"7.5 % 2.0", 1.5},
		{"-3.14", -3.14},
		{"2.0 + 3.0 * 4.0", 14.0},
		{"(2.0 + 3.0) * 4.0", 20.0},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		if !testFloatObject(t, result, tt.expected) {
			t.Errorf("输入: %s", tt.input)
		}
	}
}

func TestEvalMixedIntFloat(t *testing.T) {
	tests := []struct {
		input       string
		expected    float64
		expectFloat bool
	}{
		{"3 + 0.14", 3.14, true},
		{"10 * 0.5", 5.0, true},
		{"10 / 4", 2, false},   // 整数除整数 = 整数
		{"10.0 / 4", 2.5, true}, // 浮点数除整数 = 浮点数
		{"4 + 2.0", 6.0, true},
		{"3.0 * 3", 9.0, true},
		{"1.5 + 1.5", 3.0, true},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		if tt.expectFloat {
			if !testFloatObject(t, result, tt.expected) {
				t.Errorf("输入: %s (期望浮点数)", tt.input)
			}
		} else {
			if !testIntegerObject(t, result, int64(tt.expected)) {
				t.Errorf("输入: %s (期望整数)", tt.input)
			}
		}
	}
}

func TestEvalFloatComparison(t *testing.T) {
	tests := []struct {
		input    string
		expected bool
	}{
		{"3.14 > 3.0", true},
		{"3.14 < 3.0", false},
		{"3.14 == 3.14", true},
		{"3.14 != 3.0", true},
		{"3.0 == 3", true},   // 浮点数 == 整数
		{"3.5 == 3", false},  // 浮点数 != 整数
		{"3.0 <= 3.0", true},
		{"3.0 >= 4.0", false},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		b, ok := result.(*object.Boolean)
		if !ok {
			t.Errorf("输入 %s: 期望 Boolean, 得到 %T", tt.input, result)
			continue
		}
		if b.Value != tt.expected {
			t.Errorf("输入 %s: 期望 %v, 得到 %v", tt.input, tt.expected, b.Value)
		}
	}
}

func TestEvalFloatTypeConv(t *testing.T) {
	// 类型()
	result := testEval(`类型(3.14)`)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "浮点数" {
		t.Errorf(`类型(3.14) 期望 "浮点数", 得到 %q`, str.Value)
	}

	// 浮点数() 转换
	result = testEval(`浮点数(42)`)
	if !testFloatObject(t, result, 42.0) {
		t.Errorf("浮点数(42) 应为 42.0")
	}

	result = testEval(`浮点数("3.14")`)
	if !testFloatObject(t, result, 3.14) {
		t.Errorf(`浮点数("3.14") 应为 3.14`)
	}

	// 整数() 从浮点数
	result = testEval(`整数(3.99)`)
	if !testIntegerObject(t, result, 3) {
		t.Errorf("整数(3.99) 应为 3")
	}

	// 字符串() 从浮点数
	result = testEval(`字符串(3.14)`)
	str, ok = result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "3.14" {
		t.Errorf(`字符串(3.14) 期望 "3.14", 得到 %q`, str.Value)
	}
}

func TestEvalFloatInFunction(t *testing.T) {
	input := `
函数 圆面积(半径) {
    变量 pi = 3.14159265
    返回 pi * 半径 * 半径
}
圆面积(2.0)
`
	result := testEval(input)
	if !testFloatObject(t, result, 12.5663706) {
		t.Errorf("圆面积(2.0) = π * 4 ≈ 12.566")
	}
}

func TestEvalFloatInArray(t *testing.T) {
	input := `[1.5, 2.5, 3.5][1]`
	result := testEval(input)
	if !testFloatObject(t, result, 2.5) {
		t.Errorf("数组索引 [1.5, 2.5, 3.5][1] 应为 2.5")
	}
}

func TestEvalFloatInHash(t *testing.T) {
	input := `变量 h = {1.5: "a", 2.5: "b"}; h[2.5]`
	result := testEval(input)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "b" {
		t.Errorf("哈希表 h[2.5] 期望 'b', 得到 %q", str.Value)
	}
}

// ==================== 字符串处理内置函数测试 ====================

func TestEvalToUpper(t *testing.T) {
	result := testEval(`大写("hello世界")`)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "HELLO世界" {
		t.Errorf("大写(\"hello世界\") 期望 \"HELLO世界\", 得到 %q", str.Value)
	}
}

func TestEvalToLower(t *testing.T) {
	result := testEval(`小写("HELLO世界")`)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "hello世界" {
		t.Errorf("小写(\"HELLO世界\") 期望 \"hello世界\", 得到 %q", str.Value)
	}
}

func TestEvalHasPrefix(t *testing.T) {
	result := testEval(`前缀("hello.go", "hello")`)
	b, ok := result.(*object.Boolean)
	if !ok {
		t.Fatalf("期望 Boolean, 得到 %T", result)
	}
	if !b.Value {
		t.Errorf("前缀(\"hello.go\", \"hello\") 期望 真")
	}

	result = testEval(`前缀("hello.go", "world")`)
	b, ok = result.(*object.Boolean)
	if !ok {
		t.Fatalf("期望 Boolean, 得到 %T", result)
	}
	if b.Value {
		t.Errorf("前缀(\"hello.go\", \"world\") 期望 假")
	}
}

func TestEvalHasSuffix(t *testing.T) {
	result := testEval(`后缀("hello.go", ".go")`)
	b, ok := result.(*object.Boolean)
	if !ok {
		t.Fatalf("期望 Boolean, 得到 %T", result)
	}
	if !b.Value {
		t.Errorf("后缀(\"hello.go\", \".go\") 期望 真")
	}
}

func TestEvalIndex(t *testing.T) {
	result := testEval(`查找("hello world", "world")`)
	if !testIntegerObject(t, result, 6) {
		t.Errorf("查找(\"hello world\", \"world\") 期望 6")
	}

	result = testEval(`查找("hello", "xyz")`)
	if !testIntegerObject(t, result, -1) {
		t.Errorf("查找(\"hello\", \"xyz\") 期望 -1")
	}
}

func TestEvalReplace(t *testing.T) {
	result := testEval(`替换("a-b-c-d", "-", "→")`)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "a→b→c→d" {
		t.Errorf("替换结果期望 \"a→b→c→d\", 得到 %q", str.Value)
	}
}

func TestEvalSplit(t *testing.T) {
	result := testEval(`分割("a,b,c", ",")`)
	arr, ok := result.(*object.Array)
	if !ok {
		t.Fatalf("期望 Array, 得到 %T", result)
	}
	if len(arr.Elements) != 3 {
		t.Fatalf("分割结果期望 3 个元素, 得到 %d", len(arr.Elements))
	}
	expected := []string{"a", "b", "c"}
	for i, e := range arr.Elements {
		str, ok := e.(*object.String)
		if !ok {
			t.Fatalf("元素 %d 期望 String, 得到 %T", i, e)
		}
		if str.Value != expected[i] {
			t.Errorf("元素 %d 期望 %q, 得到 %q", i, expected[i], str.Value)
		}
	}
}

func TestEvalTrimSpace(t *testing.T) {
	result := testEval(`去空白("  hello  ")`)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "hello" {
		t.Errorf("去空白结果期望 \"hello\", 得到 %q", str.Value)
	}
}

func TestEvalTrim(t *testing.T) {
	result := testEval(`裁剪("xxxhelloxxx", "x")`)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "hello" {
		t.Errorf("裁剪结果期望 \"hello\", 得到 %q", str.Value)
	}
}

func TestEvalRepeat(t *testing.T) {
	result := testEval(`重复("ab", 3)`)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "ababab" {
		t.Errorf("重复(\"ab\", 3) 期望 \"ababab\", 得到 %q", str.Value)
	}
}

func TestEvalCount(t *testing.T) {
	result := testEval(`计数("banana", "a")`)
	if !testIntegerObject(t, result, 3) {
		t.Errorf("计数(\"banana\", \"a\") 期望 3")
	}
}

func TestEvalFields(t *testing.T) {
	result := testEval(`分词("  hello   world  你好  ")`)
	arr, ok := result.(*object.Array)
	if !ok {
		t.Fatalf("期望 Array, 得到 %T", result)
	}
	if len(arr.Elements) != 3 {
		t.Fatalf("分词结果期望 3 个元素, 得到 %d", len(arr.Elements))
	}
	expected := []string{"hello", "world", "你好"}
	for i, e := range arr.Elements {
		str, ok := e.(*object.String)
		if !ok {
			t.Fatalf("元素 %d 期望 String, 得到 %T", i, e)
		}
		if str.Value != expected[i] {
			t.Errorf("元素 %d 期望 %q, 得到 %q", i, expected[i], str.Value)
		}
	}
}

func TestEvalCharAt(t *testing.T) {
	// 中文按 rune 计数
	result := testEval(`取字符("你好世界", 2)`)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "世" {
		t.Errorf("取字符(\"你好世界\", 2) 期望 \"世\", 得到 %q", str.Value)
	}

	// 英文
	result = testEval(`取字符("hello", 1)`)
	str, ok = result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "e" {
		t.Errorf("取字符(\"hello\", 1) 期望 \"e\", 得到 %q", str.Value)
	}
}

func TestEvalFormat(t *testing.T) {
	result := testEval(`格式化(3.14159, 2)`)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "3.14" {
		t.Errorf("格式化(3.14159, 2) 期望 \"3.14\", 得到 %q", str.Value)
	}

	// 整数也可以用格式化
	result = testEval(`格式化(42, 2)`)
	str, ok = result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "42.00" {
		t.Errorf("格式化(42, 2) 期望 \"42.00\", 得到 %q", str.Value)
	}
}

func TestEvalStringPipeline(t *testing.T) {
	// 链式调用：大写 → 分割 → 连接
	input := `连接(分割(大写("a,b,c"), ","), "→")`
	result := testEval(input)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "A→B→C" {
		t.Errorf("链式调用期望 \"A→B→C\", 得到 %q", str.Value)
	}
}

// ==================== 并发内置函数测试 ====================

func TestEvalFString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		// 基本插值
		{`变量 name = "世界"  f"你好, {name}!"`, "你好, 世界!"},
		{`变量 n = 42  f"count={n}"`, "count=42"},
		// 多变量插值
		{`变量 a = 1  变量 b = 2  f"{a}+{b}={a+b}"`, "1+2=3"},
		// 表达式插值
		{`f"结果: {3 * 7}"`, "结果: 21"},
		{`变量 x = 10  f"x*2={x*2}"`, "x*2=20"},
		// 无插值（纯文本）
		{`f"纯文本"`, "纯文本"},
		// 转义花括号
		{`f"字典{{key: val}}"`, "字典{key: val}"},
		// 浮点数插值
		{`变量 pi = 3.14  f"pi={pi}"`, "pi=3.14"},
		// 布尔插值
		{`变量 ok = 真  f"ok={ok}"`, "ok=真"},
		// 函数调用插值
		{`变量 s = 大写("hello")  f"upper={s}"`, "upper=HELLO"},
		// 前后都有文本
		{`变量 n = 5  f"前{n}后"`, "前5后"},
	}

	for _, tt := range tests {
		result := testEval(tt.input)
		str, ok := result.(*object.String)
		if !ok {
			t.Errorf("期望 String, 得到 %T (%s) — input: %s", result, result.Inspect(), tt.input)
			continue
		}
		if str.Value != tt.expected {
			t.Errorf("input: %s — 期望 %q, 得到 %q", tt.input, tt.expected, str.Value)
		}
	}
}

func TestEvalGoBasic(t *testing.T) {
	// 并发执行无参函数，接收返回值
	input := `
变量 ch = 并发(函数() {
	返回 42
})
接收(ch)
`
	result := testEval(input)
	if !testIntegerObject(t, result, 42) {
		t.Errorf("并发执行期望 42, 得到 %s", result.Inspect())
	}
}

func TestEvalChannelBasic(t *testing.T) {
	// 创建缓冲通道，发送和接收
	input := `
变量 ch = 通道(1)
发送(ch, "hello")
接收(ch)
`
	result := testEval(input)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T (%s)", result, result.Inspect())
	}
	if str.Value != "hello" {
		t.Errorf("通道传输期望 \"hello\", 得到 %q", str.Value)
	}
}

func TestEvalChannelInteger(t *testing.T) {
	// 传输整数
	input := `
变量 ch = 通道(1)
发送(ch, 100)
接收(ch)
`
	result := testEval(input)
	if !testIntegerObject(t, result, 100) {
		t.Errorf("通道传输期望 100, 得到 %s", result.Inspect())
	}
}

func TestEvalChannelClose(t *testing.T) {
	// 关闭通道后接收返回空
	input := `
变量 ch = 通道(1)
发送(ch, 42)
关闭(ch)
接收(ch)
接收(ch)
`
	result := testEval(input)
	// 第二次接收应该返回空（通道已关闭且为空）
	if result.Type() != object.NULL_OBJ {
		t.Errorf("关闭通道后再接收期望 空, 得到 %s", result.Inspect())
	}
}

func TestEvalTryRecv(t *testing.T) {
	// 尝试接收：空通道立即返回空
	input := `
变量 ch = 通道()
尝试接收(ch)
`
	result := testEval(input)
	if result.Type() != object.NULL_OBJ {
		t.Errorf("空通道尝试接收期望 空, 得到 %s", result.Inspect())
	}

	// 有数据时接收
	input2 := `
变量 ch = 通道(1)
发送(ch, "hi")
尝试接收(ch)
`
	result = testEval(input2)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T", result)
	}
	if str.Value != "hi" {
		t.Errorf("尝试接收期望 \"hi\", 得到 %q", str.Value)
	}
}

func TestEvalWaitAll(t *testing.T) {
	// 并发执行多个任务，等待全部完成
	input := `
变量 ch1 = 并发(函数() { 返回 10 })
变量 ch2 = 并发(函数() { 返回 20 })
变量 ch3 = 并发(函数() { 返回 30 })
变量 结果 = 等待全部(ch1, ch2, ch3)
结果[0] + 结果[1] + 结果[2]
`
	result := testEval(input)
	if !testIntegerObject(t, result, 60) {
		t.Errorf("等待全部期望 60, 得到 %s", result.Inspect())
	}
}

func TestEvalGoWithClosure(t *testing.T) {
	// 协程中使用闭包变量
	input := `
变量 x = 5
变量 ch = 并发(函数() { 返回 x * 2 })
接收(ch)
`
	result := testEval(input)
	if !testIntegerObject(t, result, 10) {
		t.Errorf("协程闭包期望 10, 得到 %s", result.Inspect())
	}
}

func TestEvalConcurrentSum(t *testing.T) {
	// 并发计算：多个协程分别计算，汇总结果
	input := `
函数 计算(开始, 结束) {
	变量 总和 = 0
	对于 i = 开始; i < 结束; i = i + 1 {
		总和 = 总和 + i
	}
	返回 总和
}

变量 ch1 = 并发(函数() { 返回 计算(1, 50) })
变量 ch2 = 并发(函数() { 返回 计算(50, 100) })
变量 结果 = 等待全部(ch1, ch2)
结果[0] + 结果[1]
`
	result := testEval(input)
	// 1+2+...+49 = 1225, 50+51+...+99 = 3725, 总和 = 4950
	if !testIntegerObject(t, result, 4950) {
		t.Errorf("并发求和期望 4950, 得到 %s", result.Inspect())
	}
}

// ==================== 文件 I/O 内置函数测试 ====================

func TestEvalFileWriteRead(t *testing.T) {
	// 写文件 → 读文件 → 验证内容一致
	input := `
写文件("test_io.zuzo", "hello zuzo")
读文件("test_io.zuzo")
`
	result := testEval(input)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T (%s)", result, result.Inspect())
	}
	if str.Value != "hello zuzo" {
		t.Errorf("读文件期望 \"hello zuzo\", 得到 %q", str.Value)
	}
	// 清理
	defer os.Remove("test_io.zuzo")
}

func TestEvalFileAppend(t *testing.T) {
	// 先写 → 再追加 → 读出来验证
	input := `
写文件("test_append.zuzo", "第一行")
追加文件("test_append.zuzo", "第二行")
读文件("test_append.zuzo")
`
	result := testEval(input)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T (%s)", result, result.Inspect())
	}
	if str.Value != "第一行第二行" {
		t.Errorf("追加文件期望 \"第一行第二行\", 得到 %q", str.Value)
	}
	defer os.Remove("test_append.zuzo")
}

func TestEvalFileExists(t *testing.T) {
	// 文件不存在 → 假；创建后 → 真
	input := `
文件存在("test_exists.zuzo")
`
	result := testEval(input)
	if result.Type() != object.BOOLEAN_OBJ {
		t.Fatalf("期望 布尔, 得到 %s", result.Type())
	}
	b := result.(*object.Boolean)
	if b.Value {
		t.Errorf("文件不应存在")
	}

	// 创建文件后再检查
	input2 := `
创建文件("test_exists.zuzo")
文件存在("test_exists.zuzo")
`
	result = testEval(input2)
	b = result.(*object.Boolean)
	if !b.Value {
		t.Errorf("文件应存在")
	}
	defer os.Remove("test_exists.zuzo")
}

func TestEvalFileCreateDelete(t *testing.T) {
	// 创建 → 存在 → 删除 → 不存在
	input := `
创建文件("test_cd.zuzo")
文件存在("test_cd.zuzo")
`
	result := testEval(input)
	b := result.(*object.Boolean)
	if !b.Value {
		t.Errorf("创建后文件应存在")
	}

	input2 := `
删除文件("test_cd.zuzo")
文件存在("test_cd.zuzo")
`
	result = testEval(input2)
	b = result.(*object.Boolean)
	if b.Value {
		t.Errorf("删除后文件不应存在")
	}
}

func TestEvalFileRename(t *testing.T) {
	// 写文件 → 重命名 → 旧路径不存在 → 新路径可读
	input := `
写文件("test_old.zuzo", "renamed content")
重命名("test_old.zuzo", "test_new.zuzo")
读文件("test_new.zuzo")
`
	result := testEval(input)
	str, ok := result.(*object.String)
	if !ok {
		t.Fatalf("期望 String, 得到 %T (%s)", result, result.Inspect())
	}
	if str.Value != "renamed content" {
		t.Errorf("重命名后读取期望 \"renamed content\", 得到 %q", str.Value)
	}

	// 验证旧文件不存在
	oldExists := testEval(`文件存在("test_old.zuzo")`)
	b := oldExists.(*object.Boolean)
	if b.Value {
		t.Errorf("旧文件不应存在")
	}
	defer os.Remove("test_new.zuzo")
}

func TestEvalReadDir(t *testing.T) {
	// 创建几个文件 → 读目录 → 验证数组包含这些文件
	input := `
创建文件("test_dir_a.zuzo")
创建文件("test_dir_b.zuzo")
变量 结果 = 读目录(".")
创建文件("test_dir_c.zuzo")
删除文件("test_dir_a.zuzo")
删除文件("test_dir_b.zuzo")
删除文件("test_dir_c.zuzo")
结果
`
	result := testEval(input)
	arr, ok := result.(*object.Array)
	if !ok {
		t.Fatalf("期望 Array, 得到 %T (%s)", result, result.Inspect())
	}

	// 验证数组中有元素（目录不为空）
	if len(arr.Elements) == 0 {
		t.Errorf("读目录期望非空数组")
	}

	// 验证每个元素都是字符串
	for _, elem := range arr.Elements {
		if elem.Type() != object.STRING_OBJ {
			t.Errorf("读目录元素期望字符串, 得到 %s", elem.Type())
		}
	}
}

func TestEvalFileIOWithError(t *testing.T) {
	// 读不存在的文件 → 返回错误对象
	input := `读文件("nonexistent_file_xyz.zuzo")`
	result := testEval(input)
	err, ok := result.(*object.Error)
	if !ok {
		t.Fatalf("期望 Error, 得到 %T (%s)", result, result.Inspect())
	}
	if !strings.Contains(err.Message, "读文件") {
		t.Errorf("错误信息应包含 \"读文件\", 得到 %q", err.Message)
	}
}
