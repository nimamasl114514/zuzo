package tests

// diagnostic_test.go — 活字印刷式诊断系统测试（从根目录迁移）

import (
	"strings"
	"testing"

	"zuzo/evaluator"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/parser"
)

// ==================== 活字印刷式诊断系统测试 ====================

// testDiagParse 解析代码并返回诊断报告
func testDiagParse(input string) *parser.Parser {
	l := lexer.New(input)
	p := parser.New(l)
	p.ParseProgram()
	return p
}

func TestDiagBasicParseError(t *testing.T) {
	// 缺少右括号
	input := `打印("hello"`
	p := testDiagParse(input)

	if !p.Diagnostics().HasErrors() {
		t.Fatal("期望有诊断错误，但报告为空")
	}

	diag := p.Diagnostics()
	if diag.Count() == 0 {
		t.Fatal("期望至少 1 个活字")
	}

	rendered := diag.Render()
	if !strings.Contains(rendered, "根因") {
		t.Errorf("渲染结果应包含 '根因', 得到:\n%s", rendered)
	}
	if !strings.Contains(rendered, "E001") {
		t.Errorf("渲染结果应包含错误码 'E001', 得到:\n%s", rendered)
	}
	if !strings.Contains(rendered, "右括号") {
		t.Errorf("渲染结果应包含 '右括号' 建议, 得到:\n%s", rendered)
	}
}

func TestDiagMultipleParseErrors(t *testing.T) {
	// 多个语法错误
	input := `变量 x = ;
变量 y = ;`
	p := testDiagParse(input)

	diag := p.Diagnostics()
	if diag.Count() < 2 {
		t.Errorf("期望至少 2 个活字, 得到 %d", diag.Count())
	}

	rendered := diag.Render()
	// 应该有多个根因
	rootCount := strings.Count(rendered, "根因")
	if rootCount < 2 {
		t.Errorf("期望至少 2 个根因, 得到 %d", rootCount)
	}
}

func TestDiagTypeErrorOnly(t *testing.T) {
	// 纯类型错误（无语法错误）
	input := `变量 x = 42; 变量 y = "hello"; x + y`
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if p.Diagnostics().HasErrors() {
		t.Fatalf("不期望有语法错误: %s", p.Diagnostics().Render())
	}

	typeErrors := infer.CheckProgramWithDiagnostics(program, p.Diagnostics())
	if len(typeErrors) == 0 {
		t.Fatal("期望有类型错误")
	}

	diag := p.Diagnostics()
	if !diag.HasErrors() {
		t.Fatal("诊断报告应该包含类型错误")
	}

	rendered := diag.Render()
	if !strings.Contains(rendered, "类型不匹配") {
		t.Errorf("渲染结果应包含 '类型不匹配', 得到:\n%s", rendered)
	}
}

func TestDiagCausalChain(t *testing.T) {
	// 语法错误 + 同行类型错误 → 应形成因果链
	input := `变量 x = "hello"; x + 42`
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	// 无语法错误，但类型错误
	typeErrors := infer.CheckProgramWithDiagnostics(program, p.Diagnostics())

	if len(typeErrors) == 0 {
		t.Fatal("期望有类型错误")
	}

	diag := p.Diagnostics()
	rendered := diag.Render()

	// 应该有根因
	if !strings.Contains(rendered, "根因") {
		t.Errorf("渲染结果应包含 '根因', 得到:\n%s", rendered)
	}

	// 应该有类型不匹配
	if !strings.Contains(rendered, "类型不匹配") {
		t.Errorf("渲染结果应包含 '类型不匹配', 得到:\n%s", rendered)
	}
}

func TestDiagRenderFormat(t *testing.T) {
	// 验证 v2 渲染格式包含标题边框和结构
	input := `打印("hello"`
	p := testDiagParse(input)
	rendered := p.Diagnostics().Render()

	// v2 格式：使用 ━━━ 作为标题边框
	if !strings.Contains(rendered, "━━━") {
		t.Error("渲染结果应包含标题边框 '━━━'")
	}
	if !strings.Contains(rendered, "发现") {
		t.Error("渲染结果应包含 '发现' 标题")
	}
	if !strings.Contains(rendered, "问题") {
		t.Error("渲染结果应包含 '问题' 字样")
	}
}

func TestDiagSuggestion(t *testing.T) {
	// 验证修复建议
	input := `打印("hello"`
	p := testDiagParse(input)
	rendered := p.Diagnostics().Render()

	if !strings.Contains(rendered, "建议:") {
		t.Error("渲染结果应包含 '建议:' 字段")
	}
}

func TestDiagNoErrors(t *testing.T) {
	// 正确代码不应有诊断错误
	input := `变量 x = 42; 打印(x)`
	p := testDiagParse(input)

	if p.Diagnostics().HasErrors() {
		t.Errorf("正确代码不应有诊断错误: %s", p.Diagnostics().Render())
	}

	rendered := p.Diagnostics().Render()
	if rendered != "" {
		t.Errorf("无错误时渲染结果应为空字符串, 得到: %s", rendered)
	}
}

func TestDiagCausalChainRender(t *testing.T) {
	// 手动构建因果链并验证渲染
	// 这个测试直接使用 diagnostic 包
	// （在 diagnostic_pkg_test.go 中有更详细的测试）
	input := `变量 x = "hello"; x + 42`
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	infer.CheckProgramWithDiagnostics(program, p.Diagnostics())

	rendered := p.Diagnostics().Render()

	// 至少应该有渲染输出
	if len(rendered) == 0 {
		t.Error("渲染结果不应为空")
	}

	// 确保有类型不匹配错误
	if !strings.Contains(rendered, "类型不匹配") || !strings.Contains(rendered, "整数") || !strings.Contains(rendered, "字符串") {
		t.Errorf("渲染结果应包含类型不匹配信息, 得到:\n%s", rendered)
	}
}

// 确保原有功能不受影响
func TestDiagBackwardCompat(t *testing.T) {
	// 原有的 Errors() 方法仍然可用
	input := `打印("hello"`
	p := testDiagParse(input)

	if len(p.Errors()) == 0 {
		t.Error("Errors() 应返回非空错误列表")
	}
}

// 确保 evaluator 仍能正常工作
func TestDiagEvalWorks(t *testing.T) {
	input := `变量 x = 42; 打印(x)`
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()

	if p.Diagnostics().HasErrors() {
		t.Fatalf("不应有解析错误: %s", p.Diagnostics().Render())
	}

	env := evaluator.NewEnvironment()
	result := evaluator.Eval(program, env)

	if result == nil || result.Type() != "空" {
		t.Errorf("期望 空返回值, 得到 %v", result)
	}
}
