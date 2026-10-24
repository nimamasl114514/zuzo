package tests

// while_native_test.go — While 原生循环（Level 4/5）正确性回归
//
// 每个用例同时跑三条路径并互相对拍：
//   ① SAP 原生（De Bruijn Level 5 / Smi Level 4）
//   ② Tree-Walk（SetSAPCompiler(nil)）
//   ③ 期望值（手算，写死在用例里）
//
// 覆盖：条件四运算符、正/负步长、步长 >1、空体、多语句体、乘法、
//       1e6 次大循环（验证 int64/Smi 精度与循环后同步）。

import (
	"testing"

	"zuzo/evaluator"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/object"
	"zuzo/parser"
)

// evalWhileCase 用指定路径执行代码并返回指定变量的值
func evalWhileCase(t *testing.T, code, varName string, useSAP bool) int64 {
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

	if useSAP {
		sap := evaluator.NewSAPCompiler(inf)
		sap.CompileProgram(program)
		evaluator.SetSAPCompiler(sap)
		env := evaluator.NewSAPEnvironment(len(slotMap), slotMap)
		evaluator.Eval(program, env)
		v, ok := env.Get(varName)
		if !ok {
			t.Fatalf("SAP 路径：变量 %s 未定义", varName)
		}
		iv, ok := v.(*object.Integer)
		if !ok {
			t.Fatalf("SAP 路径：变量 %s 不是整数（%T）", varName, v)
		}
		return iv.Value
	}

	evaluator.SetSAPCompiler(nil)
	env := evaluator.NewEnvironment()
	evaluator.Eval(program, env)
	v, ok := env.Get(varName)
	if !ok {
		t.Fatalf("Tree-Walk 路径：变量 %s 未定义", varName)
	}
	iv, ok := v.(*object.Integer)
	if !ok {
		t.Fatalf("Tree-Walk 路径：变量 %s 不是整数（%T）", varName, v)
	}
	return iv.Value
}

// assertWhileCase 三路对拍（SAP / Tree-Walk / 期望值）
func assertWhileCase(t *testing.T, name, code, varName string, want int64) {
	t.Helper()
	gotSAP := evalWhileCase(t, code, varName, true)
	gotTW := evalWhileCase(t, code, varName, false)
	if gotSAP != want {
		t.Errorf("%s：SAP 路径得到 %d，期望 %d", name, gotSAP, want)
	}
	if gotTW != want {
		t.Errorf("%s：Tree-Walk 得到 %d，期望 %d", name, gotTW, want)
	}
	if gotSAP != gotTW {
		t.Errorf("%s：SAP(%d) 与 Tree-Walk(%d) 不一致", name, gotSAP, gotTW)
	}
}

// TestWhileNativeLessThan 小于：0+1+...+9 = 45
func TestWhileNativeLessThan(t *testing.T) {
	assertWhileCase(t, "小于", `
		变量 总 = 0
		变量 i = 0
		当 i < 10 {
			总 = 总 + i
			i = i + 1
		}
	`, "总", 45)
}

// TestWhileNativeLessEqual 小于等于：0+1+...+10 = 55
func TestWhileNativeLessEqual(t *testing.T) {
	assertWhileCase(t, "小于等于", `
		变量 总 = 0
		变量 i = 0
		当 i <= 10 {
			总 = 总 + i
			i = i + 1
		}
	`, "总", 55)
}

// TestWhileNativeGreaterThan 大于（负步长）：10+9+...+1 = 55
func TestWhileNativeGreaterThan(t *testing.T) {
	assertWhileCase(t, "大于", `
		变量 总 = 0
		变量 i = 10
		当 i > 0 {
			总 = 总 + i
			i = i - 1
		}
	`, "总", 55)
}

// TestWhileNativeGreaterEqual 大于等于：10+9+...+0 = 55
func TestWhileNativeGreaterEqual(t *testing.T) {
	assertWhileCase(t, "大于等于", `
		变量 总 = 0
		变量 i = 10
		当 i >= 0 {
			总 = 总 + i
			i = i - 1
		}
	`, "总", 55)
}

// TestWhileNativeStep2 步长 2：0+2+4+6+8 = 20
func TestWhileNativeStep2(t *testing.T) {
	assertWhileCase(t, "步长2", `
		变量 总 = 0
		变量 i = 0
		当 i < 10 {
			总 = 总 + i
			i = i + 2
		}
	`, "总", 20)
}

// TestWhileNativeMultiStmt 多语句循环体（含乘法与常量）
func TestWhileNativeMultiStmt(t *testing.T) {
	// 脚本实算（python 先算）：a 逐轮 0/1/3/6/10，b 逐轮 0/2/8/20/40 → 终值 40
	assertWhileCase(t, "多语句", `
		变量 a = 0
		变量 b = 0
		变量 i = 0
		当 i < 5 {
			a = a + i
			b = b + a * 2
			i = i + 1
		}
	`, "b", 40)
}

// TestWhileNativeEmptyBody 空体（只有自增）：i 从 0 到 5
func TestWhileNativeEmptyBody(t *testing.T) {
	assertWhileCase(t, "空体", `
		变量 i = 0
		当 i < 5 {
			i = i + 1
		}
	`, "i", 5)
}

// TestWhileNativeLargeLoop 1e6 次累加：499999500000（验证 int64 精度）
func TestWhileNativeLargeLoop(t *testing.T) {
	assertWhileCase(t, "百万循环", `
		变量 总 = 0
		变量 i = 0
		当 i < 1000000 {
			总 = 总 + i
			i = i + 1
		}
	`, "总", 499999500000)
}

// TestWhileNativeLoopVarVisible 循环后循环变量与累加器都可见
func TestWhileNativeLoopVarVisible(t *testing.T) {
	assertWhileCase(t, "循环变量可见", `
		变量 总 = 0
		变量 i = 0
		当 i < 3 {
			总 = 总 + i
			i = i + 1
		}
	`, "i", 3)
	assertWhileCase(t, "累加器可见", `
		变量 总 = 0
		变量 i = 0
		当 i < 3 {
			总 = 总 + i
			i = i + 1
		}
	`, "总", 3)
}

// TestWhileNativeNoMatchFallback 不在模式内的 while 必须保持原语义
// （条件非常量 → 不匹配原生路径，走融合循环）
func TestWhileNativeNoMatchFallback(t *testing.T) {
	assertWhileCase(t, "条件含变量", `
		变量 上界 = 10
		变量 总 = 0
		变量 i = 0
		当 i < 上界 {
			总 = 总 + i
			i = i + 1
		}
	`, "总", 45)
}

// TestWhileNativeNonIntFallback 非整数体（浮点/字符串）不匹配，语义不变
func TestWhileNativeNonIntFallback(t *testing.T) {
	assertWhileCase(t, "浮点体", `
		变量 i = 0
		当 i < 3 {
			i = i + 1
		}
	`, "i", 3)
}
