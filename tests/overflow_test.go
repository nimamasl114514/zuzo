package tests

// overflow_test.go — 整数溢出语义回归（2026-10-25 第20轮）
//
// 背景：修复前 zuzo 的整数边界语义三套并存——
//   · Tree-Walk / Level 1-3：静默 int64 回绕
//   · for Level 5（De Bruijn）：抛 E210
//   · while Level 4/5：静默 Smi 截断（2^63-2 读回 -2）
//
// 拍板口径：统一到「超 int64 抛 E210；int64 内但超 Smi 区间则撤出原生路径重跑」。
// 本文件把三条反例固化为用例，同时校验 SAP 与 Tree-Walk 结果一致。

import (
	"testing"

	"zuzo/evaluator"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/object"
	"zuzo/parser"
)

// evalOverflowCase 执行代码，返回 (变量值, 是否产生错误对象)
func evalOverflowCase(t *testing.T, code, varName string, useSAP bool) (int64, *object.Error) {
	t.Helper()

	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseProgram()
	if p.Diagnostics().HasErrors() {
		t.Fatalf("解析失败：%v", p.Diagnostics().Render())
	}
	inf := infer.New()
	inf.Infer(program)
	slotMap := inf.GetSlotMap()

	old := evaluator.GetSAPCompiler()
	defer evaluator.SetSAPCompiler(old)

	var env *object.Environment
	if useSAP {
		sap := evaluator.NewSAPCompiler(inf)
		sap.CompileProgram(program)
		evaluator.SetSAPCompiler(sap)
		env = evaluator.NewSAPEnvironment(len(slotMap), slotMap)
	} else {
		evaluator.SetSAPCompiler(nil)
		env = evaluator.NewEnvironment()
	}

	res := evaluator.Eval(program, env)
	if errObj, ok := res.(*object.Error); ok {
		return 0, errObj
	}
	v, ok := env.Get(varName)
	if !ok {
		t.Fatalf("%s：变量 %s 未定义", pathName(useSAP), varName)
	}
	// 部分路径把错误对象直接写进环境而不是返回，这里统一拦下
	if e, ok := v.(*object.Error); ok {
		return 0, e
	}
	iv, ok := v.(*object.Integer)
	if !ok {
		t.Fatalf("%s：变量 %s 不是整数（%T）", pathName(useSAP), varName, v)
	}
	return iv.Value, nil
}

func pathName(useSAP bool) string {
	if useSAP {
		return "SAP"
	}
	return "Tree-Walk"
}

// TestOverflowWhileExceedsSmi 反例 1：while 体内结果超 Smi 区间（2^63-2）
// 修复前 SAP 得 -2（Smi 截断），Tree-Walk 得 9223372036854775806。
func TestOverflowWhileExceedsSmi(t *testing.T) {
	code := `
		变量 i = 0
		变量 总 = 0
		当 i < 2 {
			总 = 4611686018427387903
			总 = 总 + 总
			i = i + 1
		}
	`
	const want int64 = 9223372036854775806
	gotSAP, errSAP := evalOverflowCase(t, code, "总", true)
	if errSAP != nil {
		t.Fatalf("SAP 路径不应报错，得到 %s", errSAP.Inspect())
	}
	gotTW, errTW := evalOverflowCase(t, code, "总", false)
	if errTW != nil {
		t.Fatalf("Tree-Walk 路径不应报错，得到 %s", errTW.Inspect())
	}
	if gotSAP != want {
		t.Errorf("SAP 得到 %d，期望 %d", gotSAP, want)
	}
	if gotTW != want {
		t.Errorf("Tree-Walk 得到 %d，期望 %d", gotTW, want)
	}
	if gotSAP != gotTW {
		t.Errorf("两路不一致：SAP=%d Tree-Walk=%d", gotSAP, gotTW)
	}
}

// TestOverflowWhileInt64 反例 2：while 体内 int64 溢出，两路都必须报 E210
func TestOverflowWhileInt64(t *testing.T) {
	code := `
		变量 i = 0
		变量 总 = 0
		当 i < 2 {
			总 = 9223372036854775807
			总 = 总 + 1
			i = i + 1
		}
	`
	_, errSAP := evalOverflowCase(t, code, "总", true)
	_, errTW := evalOverflowCase(t, code, "总", false)
	if errSAP == nil {
		t.Error("SAP 路径应报 E210，实际无错误")
	} else if errSAP.Code != "E210" {
		t.Errorf("SAP 错误码 %q，期望 E210", errSAP.Code)
	}
	if errTW == nil {
		t.Error("Tree-Walk 路径应报 E210，实际无错误")
	} else if errTW.Code != "E210" {
		t.Errorf("Tree-Walk 错误码 %q，期望 E210", errTW.Code)
	}
}

// TestOverflowForExceedsSmi 对照：for 版本两路一致（修复前 for 就是对的）
func TestOverflowForExceedsSmi(t *testing.T) {
	code := `
		变量 总 = 0
		对于 i = 0; i < 2; i = i + 1 {
			总 = 4611686018427387903
			总 = 总 + 总
		}
	`
	const want int64 = 9223372036854775806
	gotSAP, errSAP := evalOverflowCase(t, code, "总", true)
	if errSAP != nil {
		t.Fatalf("SAP 不应报错：%s", errSAP.Inspect())
	}
	gotTW, errTW := evalOverflowCase(t, code, "总", false)
	if errTW != nil {
		t.Fatalf("Tree-Walk 不应报错：%s", errTW.Inspect())
	}
	if gotSAP != want || gotTW != want {
		t.Errorf("期望 %d，实际 SAP=%d Tree-Walk=%d", want, gotSAP, gotTW)
	}
}

// TestOverflowPlainExpression 纯表达式溢出（不经循环）也必须报 E210
func TestOverflowPlainExpression(t *testing.T) {
	code := `变量 r = 9223372036854775807 + 1`
	_, errSAP := evalOverflowCase(t, code, "r", true)
	_, errTW := evalOverflowCase(t, code, "r", false)
	if errSAP == nil || errSAP.Code != "E210" {
		t.Errorf("SAP 应报 E210，实际 %v", errSAP)
	}
	if errTW == nil || errTW.Code != "E210" {
		t.Errorf("Tree-Walk 应报 E210，实际 %v", errTW)
	}
}

// TestOverflowCounterEscapes 计数器从 Smi 上界 +1 时，必须正确撤离原生路径
// （不能把 2^62 截成负数导致条件永真死循环）
func TestOverflowCounterEscapes(t *testing.T) {
	code := `
		变量 i = 4611686018427387903
		变量 总 = 0
		当 i <= 4611686018427387903 {
			总 = 总 + 1
			i = i + 1
		}
	`
	gotSAP, errSAP := evalOverflowCase(t, code, "总", true)
	if errSAP != nil {
		t.Fatalf("SAP 不应报错，得到 %s", errSAP.Inspect())
	}
	gotTW, errTW := evalOverflowCase(t, code, "总", false)
	if errTW != nil {
		t.Fatalf("Tree-Walk 不应报错，得到 %s", errTW.Inspect())
	}
	if gotSAP != 1 || gotTW != 1 {
		t.Errorf("期望 总=1，实际 SAP=%d Tree-Walk=%d", gotSAP, gotTW)
	}
}

// TestOverflowCounterInt64 计数器真的撞到 int64 边界时报 E210（初值超 Smi 直接走普通路径）
func TestOverflowCounterInt64(t *testing.T) {
	code := `
		变量 i = 9223372036854775807
		当 i >= 0 {
			i = i + 1
		}
	`
	_, errSAP := evalOverflowCase(t, code, "i", true)
	_, errTW := evalOverflowCase(t, code, "i", false)
	if errSAP == nil || errSAP.Code != "E210" {
		t.Errorf("SAP 计数器溢出应报 E210，实际 %v", errSAP)
	}
	if errTW == nil || errTW.Code != "E210" {
		t.Errorf("Tree-Walk 计数器溢出应报 E210，实际 %v", errTW)
	}
}
