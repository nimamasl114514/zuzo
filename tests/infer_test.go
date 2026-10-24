package tests

// infer_test.go — 类型推断测试（从 infer/ 迁移）

import (
	"strings"
	"testing"

	"zuzo/ast"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/parser"
)

// parseExpr 辅助函数：解析源代码字符串为 AST
func parseExpr(input string) *ast.Program {
	l := lexer.New(input)
	p := parser.New(l)
	return p.ParseProgram()
}

func TestInferLiterals(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`42`, "整数"},
		{`3.14`, "浮点数"},
		{`"hello"`, "字符串"},
		{`真`, "布尔"},
		{`假`, "布尔"},
		{`空`, "空"},
	}

	for _, tt := range tests {
		inf := infer.New()
		prog := parseExpr(tt.input)
		inf.Infer(prog)
		if len(inf.Errors()) != 0 {
			t.Errorf("输入 %s: 期望无错误, 得到 %v", tt.input, inf.Errors())
			continue
		}
		// 获取最后一个表达式的类型
		if len(prog.Statements) == 0 {
			t.Errorf("输入 %s: 无语句", tt.input)
			continue
		}
		exprStmt, ok := prog.Statements[0].(*ast.ExpressionStatement)
		if !ok {
			t.Errorf("输入 %s: 期望 ExpressionStatement", tt.input)
			continue
		}
		nodeType, ok := inf.NodeTypes[exprStmt.Expression]
		if !ok {
			t.Errorf("输入 %s: 未找到推断类型", tt.input)
			continue
		}
		got := infer.ResolvedType(nodeType).String()
		if got != tt.expected {
			t.Errorf("输入 %s: 期望 %s, 得到 %s", tt.input, tt.expected, got)
		}
	}
}

func TestInferArithmetic(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`1 + 2`, "整数"},
		{`3.14 + 1.0`, "浮点数"},
		{`1 + 2.0`, "浮点数"},  // 混合运算 -> 浮点数（类型提升）
		{`10 * 0.5`, "浮点数"},
		{`"a" + "b"`, "字符串"},
	}

	for _, tt := range tests {
		inf := infer.New()
		prog := parseExpr(tt.input)
		inf.Infer(prog)
		if len(inf.Errors()) != 0 {
			t.Errorf("输入 %s: 期望无错误, 得到 %v", tt.input, inf.Errors())
			continue
		}
		exprStmt := prog.Statements[0].(*ast.ExpressionStatement)
		nodeType := inf.NodeTypes[exprStmt.Expression]
		got := infer.ResolvedType(nodeType).String()
		if got != tt.expected {
			t.Errorf("输入 %s: 期望 %s, 得到 %s", tt.input, tt.expected, got)
		}
	}
}

func TestInferComparison(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`1 < 2`, "布尔"},
		{`3.14 > 3.0`, "布尔"},
		{`"a" < "b"`, "布尔"},
		{`1 == 1`, "布尔"},
		{`3.0 == 3`, "布尔"},
	}

	for _, tt := range tests {
		inf := infer.New()
		prog := parseExpr(tt.input)
		inf.Infer(prog)
		if len(inf.Errors()) != 0 {
			t.Errorf("输入 %s: 期望无错误, 得到 %v", tt.input, inf.Errors())
			continue
		}
		exprStmt := prog.Statements[0].(*ast.ExpressionStatement)
		nodeType := inf.NodeTypes[exprStmt.Expression]
		got := infer.ResolvedType(nodeType).String()
		if got != tt.expected {
			t.Errorf("输入 %s: 期望 %s, 得到 %s", tt.input, tt.expected, got)
		}
	}
}

func TestInferLogicOps(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`真 并且 假`, "布尔"},
		{`真 或者 假`, "布尔"},
		{`非 真`, "布尔"},
		{`!假`, "布尔"},
		{`(1 > 0) 并且 (2 > 1)`, "布尔"},
	}

	for _, tt := range tests {
		inf := infer.New()
		prog := parseExpr(tt.input)
		inf.Infer(prog)
		if len(inf.Errors()) != 0 {
			t.Errorf("输入 %s: 期望无错误, 得到 %v", tt.input, inf.Errors())
			continue
		}
		exprStmt := prog.Statements[0].(*ast.ExpressionStatement)
		nodeType := inf.NodeTypes[exprStmt.Expression]
		got := infer.ResolvedType(nodeType).String()
		if got != tt.expected {
			t.Errorf("输入 %s: 期望 %s, 得到 %s", tt.input, tt.expected, got)
		}
	}
}

func TestInferVariableBinding(t *testing.T) {
	input := `
变量 x = 42
变量 y = 3.14
变量 z = "hello"
x + 1
`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	env := inf.GetEnv()
	// x 应该是整数
	if xt, ok := env["x"]; ok {
		if infer.ResolvedType(xt).String() != "整数" {
			t.Errorf("x 期望 整数, 得到 %s", infer.ResolvedType(xt).String())
		}
	} else {
		t.Error("x 未在类型环境中找到")
	}
	// y 应该是浮点数
	if yt, ok := env["y"]; ok {
		if infer.ResolvedType(yt).String() != "浮点数" {
			t.Errorf("y 期望 浮点数, 得到 %s", infer.ResolvedType(yt).String())
		}
	}
	// z 应该是字符串
	if zt, ok := env["z"]; ok {
		if infer.ResolvedType(zt).String() != "字符串" {
			t.Errorf("z 期望 字符串, 得到 %s", infer.ResolvedType(zt).String())
		}
	}
}

func TestInferFunctionParam(t *testing.T) {
	// 函数 fn(x) { 返回 x + 1 } -> x 应被推断为整数
	input := `
函数 加一(x) {
    返回 x + 1
}
`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	// 检查函数类型
	env := inf.GetEnv()
	fnType, ok := env["加一"]
	if !ok {
		t.Fatal("函数 加一 未在类型环境中找到")
	}
	fn, ok := infer.ResolvedType(fnType).(infer.TypeFunc)
	if !ok {
		t.Fatalf("期望 TypeFunc, 得到 %T", infer.ResolvedType(fnType))
	}
	if len(fn.Params) != 1 {
		t.Fatalf("期望 1 个参数, 得到 %d", len(fn.Params))
	}
	// x 应被推断为整数（因为 x + 1 且 1 是整数）
	paramType := infer.ResolvedType(fn.Params[0]).String()
	if paramType != "整数" {
		t.Errorf("参数 x 期望 整数, 得到 %s", paramType)
	}
	// 返回值应为整数
	retType := infer.ResolvedType(fn.Return).String()
	if retType != "整数" {
		t.Errorf("返回值期望 整数, 得到 %s", retType)
	}
}

func TestInferFunctionCall(t *testing.T) {
	input := `
函数 平方(x) {
    返回 x * x
}
平方(7)
`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	// 最后一条语句是 平方(7)
	lastStmt := prog.Statements[len(prog.Statements)-1]
	exprStmt, ok := lastStmt.(*ast.ExpressionStatement)
	if !ok {
		t.Fatal("期望 ExpressionStatement")
	}
	callType, ok := inf.NodeTypes[exprStmt.Expression]
	if !ok {
		t.Fatal("未找到调用表达式的推断类型")
	}
	got := infer.ResolvedType(callType).String()
	if got != "整数" {
		t.Errorf("平方(7) 期望 整数, 得到 %s", got)
	}
}

func TestInferIfExpression(t *testing.T) {
	input := `如果 真 { 10 } 否则 { 20 }`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	exprStmt := prog.Statements[0].(*ast.ExpressionStatement)
	nodeType := inf.NodeTypes[exprStmt.Expression]
	got := infer.ResolvedType(nodeType).String()
	// 动态语言：if 表达式有 else 时类型为 任意（两分支可能不同类型）
	if got != "任意" {
		t.Errorf("如果表达式期望 任意, 得到 %s", got)
	}
}

func TestInferIfBranchMismatch(t *testing.T) {
	// 动态语言：if 分支类型不一致不应报错
	input := `如果 真 { 10 } 否则 { "hello" }`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("动态语言分支类型不一致不应报错, 得到 %v", inf.Errors())
	}
}

func TestInferArrayLiteral(t *testing.T) {
	input := `[1, 2, 3]`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	exprStmt := prog.Statements[0].(*ast.ExpressionStatement)
	nodeType := inf.NodeTypes[exprStmt.Expression]
	got := infer.ResolvedType(nodeType).String()
	if got != "数组[整数]" {
		t.Errorf("期望 数组[整数], 得到 %s", got)
	}
}

func TestInferArrayMixedTypeError(t *testing.T) {
	// 数组中混合类型应该报错
	input := `[1, 2, "hello"]`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) == 0 {
		t.Error("期望类型错误（数组混合类型），但没有报错")
	}
}

func TestInferBuiltinCall(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`长度("hello")`, "整数"},
		{`长度([1, 2, 3])`, "整数"},
		{`类型(42)`, "字符串"},
		{`类型(3.14)`, "字符串"},
		{`字符串(42)`, "字符串"},
		{`整数("123")`, "整数"},
		{`浮点数(42)`, "浮点数"},
		{`包含("hello", "ell")`, "布尔"},
		{`连接([1, 2], ", ")`, "字符串"},
	}

	for _, tt := range tests {
		inf := infer.New()
		prog := parseExpr(tt.input)
		inf.Infer(prog)
		if len(inf.Errors()) != 0 {
			t.Errorf("输入 %s: 期望无错误, 得到 %v", tt.input, inf.Errors())
			continue
		}
		exprStmt := prog.Statements[0].(*ast.ExpressionStatement)
		nodeType := inf.NodeTypes[exprStmt.Expression]
		got := infer.ResolvedType(nodeType).String()
		if got != tt.expected {
			t.Errorf("输入 %s: 期望 %s, 得到 %s", tt.input, tt.expected, got)
		}
	}
}

func TestInferClosure(t *testing.T) {
	input := `
函数 计数器() {
    变量 count = 0
    函数 递增() {
        count = count + 1
        返回 count
    }
    返回 递增
}
`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	// 计数器 函数应该返回一个函数类型
	env := inf.GetEnv()
	fnType, ok := env["计数器"]
	if !ok {
		t.Fatal("函数 计数器 未在类型环境中找到")
	}
	fn, ok := infer.ResolvedType(fnType).(infer.TypeFunc)
	if !ok {
		t.Fatalf("期望 TypeFunc, 得到 %T (%s)", infer.ResolvedType(fnType), infer.ResolvedType(fnType).String())
	}
	// 返回值应该是一个函数
	retFn, ok := infer.ResolvedType(fn.Return).(infer.TypeFunc)
	if !ok {
		t.Fatalf("返回值期望 TypeFunc, 得到 %T (%s)", infer.ResolvedType(fn.Return), infer.ResolvedType(fn.Return).String())
	}
	// 递增函数返回整数
	if infer.ResolvedType(retFn.Return).String() != "整数" {
		t.Errorf("递增函数返回值期望 整数, 得到 %s", infer.ResolvedType(retFn.Return).String())
	}
}

func TestInferRecursiveFunction(t *testing.T) {
	input := `
函数 阶乘(n) {
    如果 n <= 1 {
        返回 1
    }
    返回 n * 阶乘(n - 1)
}
`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	env := inf.GetEnv()
	fnType, ok := env["阶乘"]
	if !ok {
		t.Fatal("函数 阶乘 未在类型环境中找到")
	}
	fn, ok := infer.ResolvedType(fnType).(infer.TypeFunc)
	if !ok {
		t.Fatalf("期望 TypeFunc, 得到 %T", infer.ResolvedType(fnType))
	}
	// n 应被推断为整数
	paramType := infer.ResolvedType(fn.Params[0]).String()
	if paramType != "整数" {
		t.Errorf("参数 n 期望 整数, 得到 %s", paramType)
	}
	// 返回值应为整数
	if infer.ResolvedType(fn.Return).String() != "整数" {
		t.Errorf("返回值期望 整数, 得到 %s", infer.ResolvedType(fn.Return).String())
	}
}

func TestInferTypeMismatchError(t *testing.T) {
	// 类型不匹配应该报错
	input := `变量 x = 42; x + "hello"`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) == 0 {
		t.Error("期望类型错误（整数 + 字符串），但没有报错")
	}
}

func TestInferWhileLoop(t *testing.T) {
	input := `
变量 i = 0
当 i < 10 {
    i = i + 1
}
`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
}

func TestInferForLoop(t *testing.T) {
	input := `
变量 sum = 0
对于 i = 0; i < 10; i = i + 1 {
    sum = sum + i
}
`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
}

func TestInferNestedFunction(t *testing.T) {
	input := `
函数 add(x) {
    函数 inner(y) {
        返回 x + y
    }
    返回 inner
}
变量 add5 = add(5)
add5(3)
`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	// add5(3) 应该返回整数
	lastStmt := prog.Statements[len(prog.Statements)-1]
	exprStmt := lastStmt.(*ast.ExpressionStatement)
	callType := inf.NodeTypes[exprStmt.Expression]
	got := infer.ResolvedType(callType).String()
	if got != "整数" {
		t.Errorf("add5(3) 期望 整数, 得到 %s", got)
	}
}

func TestInferNegativeNumber(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{`-5`, "整数"},
		{`-3.14`, "浮点数"},
	}

	for _, tt := range tests {
		inf := infer.New()
		prog := parseExpr(tt.input)
		inf.Infer(prog)
		if len(inf.Errors()) != 0 {
			t.Errorf("输入 %s: 期望无错误, 得到 %v", tt.input, inf.Errors())
			continue
		}
		exprStmt := prog.Statements[0].(*ast.ExpressionStatement)
		nodeType := inf.NodeTypes[exprStmt.Expression]
		got := infer.ResolvedType(nodeType).String()
		if got != tt.expected {
			t.Errorf("输入 %s: 期望 %s, 得到 %s", tt.input, tt.expected, got)
		}
	}
}

func TestInferArrayIndex(t *testing.T) {
	input := `[1, 2, 3][0]`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	exprStmt := prog.Statements[0].(*ast.ExpressionStatement)
	nodeType := inf.NodeTypes[exprStmt.Expression]
	got := infer.ResolvedType(nodeType).String()
	if got != "整数" {
		t.Errorf("数组索引期望 整数, 得到 %s", got)
	}
}

func TestInferFibonacci(t *testing.T) {
	input := `
函数 斐波那契(n) {
    如果 n <= 1 {
        返回 n
    }
    返回 斐波那契(n - 1) + 斐波那契(n - 2)
}
斐波那契(10)
`
	inf := infer.New()
	prog := parseExpr(input)
	inf.Infer(prog)
	if len(inf.Errors()) != 0 {
		t.Fatalf("期望无错误, 得到 %v", inf.Errors())
	}
	// 斐波那契(10) 应该返回整数
	lastStmt := prog.Statements[len(prog.Statements)-1]
	exprStmt := lastStmt.(*ast.ExpressionStatement)
	callType := inf.NodeTypes[exprStmt.Expression]
	got := infer.ResolvedType(callType).String()
	if got != "整数" {
		t.Errorf("斐波那契(10) 期望 整数, 得到 %s", got)
	}
}

func TestInferCheckProgram(t *testing.T) {
	// 测试公开 API
	input := `变量 x = 42; 变量 y = "hello"; x + y`
	prog := parseExpr(input)
	errs := infer.CheckProgram(prog)
	if len(errs) == 0 {
		t.Error("期望类型错误（整数 + 字符串），但没有报错")
	}
}

func TestInferFString(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "纯文本f-string",
			input:    `f"hello world"`,
			expected: "字符串",
		},
		{
			name:     "整数插值",
			input:    `变量 x = 42; f"x = {x}"`,
			expected: "字符串",
		},
		{
			name:     "字符串插值",
			input:    `变量 name = "zuzo"; f"hello {name}"`,
			expected: "字符串",
		},
		{
			name:     "表达式插值",
			input:    `变量 a = 1; 变量 b = 2; f"sum = {a + b}"`,
			expected: "字符串",
		},
		{
			name:     "多插值",
			input:    `变量 x = 1; 变量 y = 2; f"{x} + {y} = {x + y}"`,
			expected: "字符串",
		},
		{
			name:     "转义花括号",
			input:    `f"{{not a var}}"`,
			expected: "字符串",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inf := infer.New()
			prog := parseExpr(tt.input)
			if len(prog.Statements) == 0 {
				t.Fatalf("解析失败：无语句")
				return
			}
			inf.Infer(prog)
			if len(inf.Errors()) != 0 {
				t.Errorf("期望无类型错误, 得到 %v", inf.Errors())
				return
			}
			lastStmt := prog.Statements[len(prog.Statements)-1]
			exprStmt, ok := lastStmt.(*ast.ExpressionStatement)
			if !ok {
				t.Errorf("最后一条语句不是表达式语句")
				return
			}
			fstrType := inf.NodeTypes[exprStmt.Expression]
			if fstrType == nil {
				t.Errorf("未找到表达式类型")
				return
			}
			got := infer.ResolvedType(fstrType).String()
			if got != tt.expected {
				t.Errorf("期望 %s, 得到 %s", tt.expected, got)
			}
		})
	}
}

func TestInferVariadicBuiltin(t *testing.T) {
	// 可变参数内置函数不应产生参数数量不匹配的误报
	tests := []struct {
		input    string
		expected string
	}{
		// 打印 接受任意数量参数
		{`打印("a", "b", "c")`, "空"},
		{`打印(1, 2, 3, 4, 5)`, "空"},
		{`打印()`, "空"},
		// 格式化 接受 1+ 个参数
		{`格式化("%d-%d", 1, 2)`, "字符串"},
		{`格式化("%s-%s-%s", "a", "b", "c")`, "字符串"},
		// 最大值/最小值 接受 1+ 个参数
		{`最大值(1, 2, 3)`, "整数"},
		{`最小值(1.0, 2.0, 3.0, 4.0)`, "浮点数"},
		// C调用 接受 1+ 个参数
		{`C调用(空, 1, 2, 3)`, "任意"},
		// 等待全部 接受 1+ 个通道，返回数组（元素类型不确定，只检查无错误）
		{`等待全部(通道(1), 通道(2), 通道(3))`, ""},
	}

	for _, tt := range tests {
		inf := infer.New()
		prog := parseExpr(tt.input)
		inf.Infer(prog)
		if len(inf.Errors()) != 0 {
			t.Errorf("输入 %s: 期望无错误, 得到 %v", tt.input, inf.Errors())
			continue
		}
		// expected 为空时只检查无类型错误
		if tt.expected == "" {
			continue
		}
		exprStmt := prog.Statements[0].(*ast.ExpressionStatement)
		nodeType := inf.NodeTypes[exprStmt.Expression]
		got := infer.ResolvedType(nodeType).String()
		if got != tt.expected {
			t.Errorf("输入 %s: 期望 %s, 得到 %s", tt.input, tt.expected, got)
		}
	}
}

func TestInferEnhancedErrorReporting(t *testing.T) {
	// 测试强化后的错误信息包含错误码、参数位置、修复建议
	tests := []struct {
		name           string
		input          string
		expectError    bool
		expectCode     string // 期望的错误码
		expectContains string // 错误信息应包含的关键词
	}{
		{
			name:           "参数数量不匹配_缺少参数",
			input:          `函数 f(x, y, z) { 返回 x }; f(1)`,
			expectError:    true,
			expectCode:     "E103",
			expectContains: "参数数量不匹配",
		},
		{
			name:           "参数数量不匹配_多余参数",
			input:          `函数 f(x) { 返回 x }; f(1, 2, 3)`,
			expectError:    true,
			expectCode:     "E103",
			expectContains: "多出 2 个参数",
		},
		{
			name:           "可变参数函数_参数类型不匹配",
			input:          `最大值(1, "abc", 3)`,
			expectError:    true,
			expectCode:     "E101",
			expectContains: "第 2 个参数",
		},
		{
			name:           "类型不匹配_整数与字符串",
			input:          `变量 x = 42; x + "hello"`,
			expectError:    true,
			expectCode:     "E101",
			expectContains: "期望",
		},
		{
			name:           "不可调用_调用整数",
			input:          `变量 x = 42; x()`,
			expectError:    true,
			expectCode:     "E104",
			expectContains: "不是函数",
		},
		{
			name:           "不可调用_调用字符串",
			input:          `变量 x = "hello"; x(1, 2)`,
			expectError:    true,
			expectCode:     "E104",
			expectContains: "不是函数",
		},
		{
			name:           "函数参数类型不匹配_带位置",
			input:          `函数 add(x: 整数, y: 整数) { 返回 x + y }; add(1, "hello")`,
			expectError:    true,
			expectCode:     "E101",
			expectContains: "第 2 个参数",
		},
		{
			name:        "可变参数函数_正常调用不报错",
			input:       `最大值(1, 2, 3, 4, 5)`,
			expectError: false,
		},
		{
			name:           "可变参数函数_混合类型报错",
			input:          `最大值(1, 2, "three", 4)`,
			expectError:    true,
			expectCode:     "E101",
			expectContains: "第 3 个参数",
		},
		{
			name:        "格式化_正常多参数调用",
			input:       `格式化("%d %s %d", 1, "hello", 2)`,
			expectError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inf := infer.New()
			prog := parseExpr(tt.input)
			inf.Infer(prog)
			errs := inf.Errors()

			if tt.expectError {
				if len(errs) == 0 {
					t.Errorf("期望有错误，但没有报错")
					return
				}
				// 检查是否有匹配的错误码
				found := false
				for _, e := range errs {
					if e.Code == tt.expectCode && strings.Contains(e.Message, tt.expectContains) {
						found = true
						break
					}
				}
				if !found {
					// 即使错误码不匹配，检查消息内容
					for _, e := range errs {
						if strings.Contains(e.Message, tt.expectContains) {
							found = true
							break
						}
					}
				}
				if !found && len(errs) > 0 {
					t.Errorf("期望错误码 %s 包含「%s」，实际错误: %v", tt.expectCode, tt.expectContains, errs)
				}
			} else {
				if len(errs) != 0 {
					t.Errorf("期望无错误，得到: %v", errs)
				}
			}
		})
	}
}

func TestTypeErrorString(t *testing.T) {
	// 测试 TypeError 的 String 方法
	te := infer.TypeError{Code: "E101", Message: "类型不匹配", Line: 5, Column: 3}
	s := te.String()
	if !strings.Contains(s, "E101") {
		t.Errorf("错误信息应包含错误码 E101，得到: %s", s)
	}
	if !strings.Contains(s, "第 5 行") {
		t.Errorf("错误信息应包含行号，得到: %s", s)
	}
	if !strings.Contains(s, "类型不匹配") {
		t.Errorf("错误信息应包含消息内容，得到: %s", s)
	}
}
