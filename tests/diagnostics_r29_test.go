package tests

import (
	"strings"
	"testing"

	"zuzo/analyze"
	"zuzo/lexer"
	"zuzo/parser"
)

// helper：解析源码并返回诊断报告
func parseDiag(t *testing.T, src string) *parser.Parser {
	t.Helper()
	p := parser.New(lexer.New(src))
	p.ParseProgram()
	return p
}

// 第29轮 T29.3：未闭合字符串必须报 E007，且位置指向开引号
func TestR29UnclosedString(t *testing.T) {
	p := parseDiag(t, "变量 s = \"abc\n打印(s)\n")
	d := p.Diagnostics()
	if !d.HasErrors() {
		t.Fatal("未闭合字符串必须报错（此前静默接受）")
	}
	out := d.Render()
	if !strings.Contains(out, "E007") {
		t.Errorf("应报 E007，得到:\n%s", out)
	}
	if !strings.Contains(out, "未闭合") {
		t.Errorf("消息应说明未闭合，得到:\n%s", out)
	}
}

// 第29轮 T29.4：函数体缺右花括号必须报 E010
func TestR29UnclosedBrace(t *testing.T) {
	p := parseDiag(t, "函数 f(a) { 返回 a\n")
	out := p.Diagnostics().Render()
	if !strings.Contains(out, "E010") {
		t.Errorf("缺右花括号应报 E010，得到:\n%s", out)
	}
}

// 第29轮 T29.4：if 块缺右花括号必须报 E010（此前静默执行）
func TestR29UnclosedBraceIf(t *testing.T) {
	p := parseDiag(t, "如果 1 > 0 {\n  打印(1)\n")
	out := p.Diagnostics().Render()
	if !strings.Contains(out, "E010") {
		t.Errorf("if 块缺右花括号应报 E010，得到:\n%s", out)
	}
}

// 第29轮 T29.4：调用括号未闭合必须报 E011
func TestR29UnclosedParen(t *testing.T) {
	p := parseDiag(t, "打印(\"hello\"")
	out := p.Diagnostics().Render()
	if !strings.Contains(out, "E011") {
		t.Errorf("调用未闭合应报 E011，得到:\n%s", out)
	}
}

// 第29轮 T29.4：形参重复必须报 E013
func TestR29DuplicateParam(t *testing.T) {
	p := parseDiag(t, "函数 f(a, a) { 返回 a }\n打印(f(1,2))\n")
	out := p.Diagnostics().Render()
	if !strings.Contains(out, "E013") {
		t.Errorf("重复形参应报 E013，得到:\n%s", out)
	}
}

// 第29轮 T29.4：循环外 跳出 必须报 E014
func TestR29BreakOutsideLoop(t *testing.T) {
	p := parseDiag(t, "跳出\n")
	out := p.Diagnostics().Render()
	if !strings.Contains(out, "E014") {
		t.Errorf("循环外跳出应报 E014，得到:\n%s", out)
	}
	// 循环内不应报错
	p2 := parseDiag(t, "变量 i = 0\n当 i < 3 {\n  跳出\n}\n")
	if p2.Diagnostics().HasErrors() {
		t.Errorf("循环内跳出不应报错，得到:\n%s", p2.Diagnostics().Render())
	}
}

// 第29轮 T29.5：token 位置必须指向 token 起点（不是读完之后）
func TestR29TokenStartPosition(t *testing.T) {
	p := parseDiag(t, "跳出\n")
	atoms := p.Diagnostics().Atoms()
	if len(atoms) == 0 {
		t.Fatal("应有一条诊断")
	}
	if atoms[0].Line != 1 || atoms[0].Column != 1 {
		t.Errorf("跳出 应定位到 1:1，得到 %d:%d", atoms[0].Line, atoms[0].Column)
	}
}

// 第29轮 T29.8：全文件未定义的标识符必须能被静态发现
func TestR29UndefinedIdentifier(t *testing.T) {
	prog := parser.New(lexer.New("打印(未定义变量)\n")).ParseProgram()
	issues := analyze.Undefined(prog, map[string]bool{"打印": true})
	if len(issues) != 1 {
		t.Fatalf("应命中 1 个未定义标识符，得到 %d", len(issues))
	}
	if issues[0].Line != 1 || issues[0].Column != 4 {
		t.Errorf("位置应为 1:4，得到 %d:%d", issues[0].Line, issues[0].Column)
	}
}

// 第29轮 T29.8：已定义的标识符不应误报
func TestR29UndefinedNoFalsePositive(t *testing.T) {
	src := "变量 x = 1\n函数 f(a) { 返回 a + x }\n打印(f(2))\n"
	prog := parser.New(lexer.New(src)).ParseProgram()
	issues := analyze.Undefined(prog, map[string]bool{"打印": true})
	if len(issues) != 0 {
		t.Fatalf("不应有未定义标识符，得到 %d 条", len(issues))
	}
}
