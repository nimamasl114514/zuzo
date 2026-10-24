package tests

// safety_test.go — SAP 编译期安全检查差分测试
//
// 核心理念：对同一程序分别走 SAP 快路径与纯解释慢路径，
// 断言两者结果（含错误路径）完全一致 —— 语义零分叉。
// 同时验证 SafetyReport 的判定与统计正确性。

import (
	"strings"
	"testing"

	"zuzo/ast"
	"zuzo/evaluator"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/object"
	"zuzo/parser"
)

// diffResult 双路径求值结果
type diffResult struct {
	sapOut  string // SAP 路径结果（Inspect）
	sapErr  bool   // SAP 路径是否错误对象
	slowOut string // 慢路径结果（Inspect）
	slowErr bool   // 慢路径是否错误对象
}

// evalBoth 对同一输入跑 SAP 快路径 + 纯解释慢路径
func evalBoth(input string) diffResult {
	var sapRes, slowRes object.Object

	// --- 慢路径：禁用 SAP ---
	oldSAP := evaluator.GetSAPCompiler()
	evaluator.SetSAPCompiler(nil)
	slowRes = parseAndEval(input)

	// --- SAP 快路径 ---
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	inf := infer.New()
	inf.Infer(program)
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	env := evaluator.NewEnvironment()
	sapRes = evaluator.Eval(program, env)
	evaluator.SetSAPCompiler(oldSAP)

	return diffResult{
		sapOut:  sapRes.Inspect(),
		sapErr:  isErrObject(sapRes),
		slowOut: slowRes.Inspect(),
		slowErr: isErrObject(slowRes),
	}
}

// parseAndEval 解析 + 纯求值（无 SAP）
func parseAndEval(input string) object.Object {
	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	env := evaluator.NewEnvironment()
	return evaluator.Eval(program, env)
}

// isErrObject 判断是否为错误对象
func isErrObject(o object.Object) bool {
	_, ok := o.(*object.Error)
	return ok
}

// assertConsistent 断言快慢路径结果一致
func assertConsistent(t *testing.T, input string, r diffResult) {
	t.Helper()
	if r.sapErr || r.slowErr {
		if r.sapErr && r.slowErr {
			// 两路径都错：错误必须一致
			if r.sapOut != r.slowOut {
				t.Errorf("输入: %s\n  SAP 错误: %q\n  慢路径错误: %q\n  语义分叉！", input, r.sapOut, r.slowOut)
			}
			return
		}
		// 一个错一个不错：必然分叉
		t.Fatalf("输入: %s\n  SAP: %q (err=%v)\n  慢路径: %q (err=%v)\n  错误状态不一致！",
			input, r.sapOut, r.sapErr, r.slowOut, r.slowErr)
	}
	if r.sapOut != r.slowOut {
		t.Fatalf("输入: %s\n  SAP: %q\n  慢路径: %q\n  结果分叉！", input, r.sapOut, r.slowOut)
	}
}

// TestSafetyDifferential 差分测试：SAP 快路径与慢路径语义强一致
func TestSafetyDifferential(t *testing.T) {
	cases := []string{
		// 整数运算
		`5 + 7`,
		`10 - 3`,
		`4 * 8`,
		`20 / 4`,
		`17 % 5`,
		`2 + 3 * 4`,
		`(2 + 6) / 2`,

		// 除零 / 取模零（守卫路径）
		`10 / 0`,
		`10 % 0`,
		`10.0 / 0.0`,
		`100 / (5 - 5)`,
		`(8 - 8) % 3`,

		// MinInt64 溢出（Go panic 风险点）
		`(-9223372036854775807 - 1) / -1`,
		`(-9223372036854775807 - 1) % -1`,
		// 正常负边界：不触发溢出
		`(-9223372036854775807 - 1) / -2`,
		`(-9223372036854775807 - 1) % 2`,

		// 浮点运算
		`3.5 + 2.5`,
		`10.0 / 4`,
		`7.0 % 2`,
		`-1.5 <= 0.5`,

		// 字符串
		`"你" + "好" + "世界"`,
		`"abc" == "abc"`,
		`"abc" < "abd"`,
		`f"值={10 / 2}"`,

		// 逻辑与比较
		`5 > 3 并且 2 < 4`,
		`10 >= 10`,
		`3 != 4`,

		// 循环（熔合路径）
		`变量 s = 0; 对于 i = 1; i <= 10; i = i + 1 { s = s + i }`,
		`变量 s = 100; 对于 i = 0; i < 10; i = i + 1 { s = s - i * 2 }`,
		`变量 s = ""; 对于 i = 0; i < 3; i = i + 1 { s = s + "x" }`,
		// 循环内除零 → 中途产生错误
		`变量 s = 0; 对于 i = 1; i <= 11; i = i + 1 { s = s + (100 / (11 - i)) }`,
		// 循环内浮点除法
		`变量 s = 0.0; 对于 i = 1; i <= 4; i = i + 1 { s = s + (1.0 / i) }`,

		// 动态节点：函数调用（回退慢路径，语义仍一致）
		`函数 加(a, b) { 返回 a + b }; 加(6, 2)`,
		`变量 r = 0; 函数 f(x) { 返回 x * 2 }; 对于 i = 1; i <= 5; i = i + 1 { r = r + f(i) }`,

		// 数组/索引（未支持特化 → 回退）
		`变量 a = [1, 2, 3]; a[1]`,

		// 条件表达式
		`变量 x = 10; 如果 x > 5 { x = x + 1 } 否则 { x = x - 1 }`,

		// 执行/遇到：捕获结构化错误对象（SAP 不特化此节点 → 双路径一致）
		`执行 { 10 / 0 } 遇到 e { "被除零" }`,
		`执行 { 42 } 遇到 e { "不应触发" }`,
		`执行 { 10 / 0 } 遇到 e { 10 % 0 }`,
		`执行 { 10 / 0 } 遇到 e { 错误码(e) }`,
		`执行 { 10 / 0 } 遇到 e { 错误信息(e) }`,
		`执行 { 10 / 0 } 遇到 e { 错误行(e) }`,
		`执行 { 1 / 0 + "x" } 遇到 e { "左值错误" }`,
		// 否则 fallback 运算符
		`1 / 0 否则 99`,
		`0 / 0 否则 "回退"`,
		`5 否则 99`,
		`(10 / 0) 否则 "表达式回退"`,
	}

	for _, input := range cases {
		r := evalBoth(input)
		assertConsistent(t, input, r)
	}
}

// TestSafetyVerifierReport 验证安全检查报告：守卫登记 + 拒绝审计
func TestSafetyVerifierReport(t *testing.T) {
	input := `变量 x = 100; 变量 y = x / 5; 变量 z = x % 3; 函数 f(a) { 返回 a + 1 }; f(x) + z`

	l := lexer.New(input)
	p := parser.New(l)
	program := p.ParseProgram()
	inf := infer.New()
	inf.Infer(program)
	sap := evaluator.NewSAPCompiler(inf)
	evaluator.SetSAPCompiler(sap)
	sap.CompileProgram(program)
	report := sap.Safety
	if report == nil {
		t.Fatal("CompileProgram 后 Safety 报告不应为 nil")
	}

	stats := report.Stats()
	t.Logf("总节点=%d 零检查=%d 守卫=%d 拒绝=%d 守卫登记=%d 拒绝原因=%v",
		stats.Total, stats.Safe, stats.Guarded, stats.Rejected, stats.GuardCount, stats.RejectBy)

	if stats.Total == 0 {
		t.Fatal("安全检查统计为空")
	}
	if stats.Guarded < 2 {
		t.Errorf("期望至少 2 个守卫节点（x/2 与 x%%3），实际 %d", stats.Guarded)
	}
	if stats.GuardCount < 2 {
		t.Errorf("期望守卫登记 >= 2，实际 %d", stats.GuardCount)
	}

	// 拒绝记录：含函数调用的表达式必须被显式拒绝
	last := program.Statements[len(program.Statements)-1].(*ast.ExpressionStatement)
	res, ok := last.Expression.(*ast.InfixExpression)
	if !ok {
		t.Fatal("期望最后一条为 infix 表达式")
	}
	ce, ok := res.Left.(*ast.CallExpression)
	if !ok {
		t.Fatal("期望 infix 左侧为函数调用")
	}
	if !report.Rejected(ce) {
		t.Error("函数调用节点未被安全检查拒绝")
	}
	if reason := report.Reason(ce); !strings.Contains(reason, "函数调用") {
		t.Errorf("拒绝原因异常: %q", reason)
	}

	// 除法节点必须登记守卫
	if !guardNodeFound(program, report) {
		t.Error("除法节点未被登记为守卫点")
	}
}

// guardNodeFound 在程序 AST 中寻找被登记守卫的除法节点
func guardNodeFound(program *ast.Program, report *evaluator.SafetyReport) bool {
	found := false
	for _, stmt := range program.Statements {
		if ls, ok := stmt.(*ast.LetStatement); ok {
			if inf, ok := ls.Value.(*ast.InfixExpression); ok {
				if inf.Operator == "/" && report.NeedsGuard(inf) {
					found = true
				}
			}
		}
	}
	return found
}

// TestTryCatchBindsErrorObject 执行/遇到 必须绑定结构化错误对象（Code/Line 可编程处理），
// 而非降级为字符串 —— 旧实现 handlerEnv.Set(e, String(Inspect)) 丢失全部结构。
func TestTryCatchBindsErrorObject(t *testing.T) {
	// 返回 e：顶层必须得到 *object.Error（旧实现是 *object.String）
	res := parseAndEval(`执行 { 10 / 0 } 遇到 e { e }`)
	err, ok := res.(*object.Error)
	if !ok {
		t.Fatalf("期望 *object.Error，得到 %T (%q)", res, res.Inspect())
	}
	if err.Code != "E202" {
		t.Errorf("Code 期望 E202，得到 %q", err.Code)
	}
	if err.Line == 0 {
		t.Error("错误对象缺少行号定位")
	}

	// 错误码(e)：结构化内省（旧实现无法做到——参数短路把 e 直接传播掉）
	rCode := evalBoth(`执行 { 10 / 0 } 遇到 e { 错误码(e) }`)
	assertConsistent(t, `执行 { 10 / 0 } 遇到 e { 错误码(e) }`, rCode)
	if rCode.sapOut != "E202" {
		t.Errorf("错误码(e) 期望 E202，得到 %q", rCode.sapOut)
	}

	// 错误信息(e)：消息文本
	rMsg := evalBoth(`执行 { 10 / 0 } 遇到 e { 错误信息(e) }`)
	assertConsistent(t, `执行 { 10 / 0 } 遇到 e { 错误信息(e) }`, rMsg)
	if rMsg.sapOut != "除零错误" {
		t.Errorf("错误信息(e) 期望 除零错误，得到 %q", rMsg.sapOut)
	}

	// 错误行(e)：定位行号
	rLine := evalBoth(`执行 { 10 / 0 } 遇到 e { 错误行(e) }`)
	assertConsistent(t, `执行 { 10 / 0 } 遇到 e { 错误行(e) }`, rLine)
	if rLine.sapOut != "1" {
		t.Errorf("错误行(e) 期望 1，得到 %q", rLine.sapOut)
	}

	// 字符串(e) 提供人类可读文本（含错误码 + 定位）
	rStr := evalBoth(`执行 { 10 / 0 } 遇到 e { 字符串(e) }`)
	assertConsistent(t, `执行 { 10 / 0 } 遇到 e { 字符串(e) }`, rStr)
	if !strings.Contains(rStr.sapOut, "E202") {
		t.Errorf("字符串(e) 应含错误码 E202，得到 %q", rStr.sapOut)
	}

	// 未触发错误时 handler 不执行
	rOk := evalBoth(`执行 { 42 } 遇到 e { "被抓" }`)
	assertConsistent(t, `执行 { 42 } 遇到 e { "被抓" }`, rOk)
	if rOk.sapOut != "42" {
		t.Errorf("无错误时不应触发 handler，得到 %q", rOk.sapOut)
	}

	// 非错误对象传给内省内置 → 报错传播而非 panic（双路径一致）
	rBadArg := evalBoth(`错误码("x")`)
	assertConsistent(t, `错误码("x")`, rBadArg)
	if !rBadArg.sapErr && !rBadArg.slowErr {
		t.Errorf("错误码(\"x\") 应产生错误，得到 %q", rBadArg.sapOut)
	}
}
