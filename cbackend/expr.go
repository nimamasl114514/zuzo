package cbackend

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"zuzo/ast"
)

// genExpr 生成一个 C 表达式。返回的字符串可直接嵌入 C 语句。
// 对于需要多步求值的表达式（if / 短路 / 哈希字面量），会先把语句写进当前块，
// 再返回一个临时变量名 —— 即 ANF（A-normal form）降级。
func (g *Gen) genExpr(e ast.Expression) (string, error) {
	switch n := e.(type) {
	case *ast.IntegerLiteral:
		if n.Value == math.MinInt64 {
			return "z_int(INT64_MIN)", nil
		}
		return fmt.Sprintf("z_int(%dLL)", n.Value), nil

	case *ast.FloatLiteral:
		return "z_float(" + cFloat(n.Value) + ")", nil

	case *ast.StringLiteral:
		return "z_str(" + cString(n.Value) + ")", nil

	case *ast.FStringLiteral:
		var parts []string
		for _, p := range n.Parts {
			// 字面文本段直接进 C 字符串，插值段先求值
			if sl, ok := p.(*ast.StringLiteral); ok {
				parts = append(parts, "z_str("+cString(sl.Value)+")")
				continue
			}
			val, err := g.genExpr(p)
			if err != nil {
				return "", err
			}
			parts = append(parts, val)
		}
		if len(parts) == 0 {
			return "z_str(\"\")", nil
		}
		return fmt.Sprintf("z_concat(%d, (ZVal[]){%s})", len(parts), strings.Join(parts, ", ")), nil

	case *ast.BooleanLiteral:
		if n.Value {
			return "z_bool(1)", nil
		}
		return "z_bool(0)", nil

	case *ast.NullLiteral:
		return "z_null()", nil

	case *ast.Identifier:
		if g.declared(n.Value) {
			return cVarName(n.Value), nil
		}
		return "", fmt.Errorf("未定义的变量: %s", n.Value)

	case *ast.PrefixExpression:
		right, err := g.genExpr(n.Right)
		if err != nil {
			return "", err
		}
		switch n.Operator {
		case "-":
			return "z_neg(" + right + ")", nil
		case "!", "非":
			return "z_not(" + right + ")", nil
		}
		return "", fmt.Errorf("不支持的前缀运算符: %s", n.Operator)

	case *ast.InfixExpression:
		return g.genInfix(n)

	case *ast.IfExpression:
		return g.genIfExpr(n)

	case *ast.CallExpression:
		return g.genCall(n)

	case *ast.ArrayLiteral:
		var items []string
		for _, el := range n.Elements {
			v, err := g.genExpr(el)
			if err != nil {
				return "", err
			}
			items = append(items, v)
		}
		return fmt.Sprintf("z_array_with(%d, (ZVal[]){%s})", len(items), strings.Join(items, ", ")), nil

	case *ast.HashLiteral:
		tmp := g.temp()
		g.line("ZVal %s = z_hash();", tmp)
		// map 遍历无序：按 String() 排序保证生成结果稳定（可复现构建）
		keys := make([]ast.Expression, 0, len(n.Pairs))
		for k := range n.Pairs {
			keys = append(keys, k)
		}
		sortExprs(keys)
		for _, k := range keys {
			ks, err := g.genExpr(k)
			if err != nil {
				return "", err
			}
			vs, err := g.genExpr(n.Pairs[k])
			if err != nil {
				return "", err
			}
			g.line("z_hash_set(%s, %s, %s);", tmp, ks, vs)
		}
		return tmp, nil

	case *ast.IndexExpression:
		left, err := g.genExpr(n.Left)
		if err != nil {
			return "", err
		}
		idx, err := g.genExpr(n.Index)
		if err != nil {
			return "", err
		}
		return fmt.Sprintf("z_index_get(%s, %s)", left, idx), nil

	case *ast.AssignExpression:
		return g.genAssignExpr(n)

	case *ast.FunctionLiteral:
		return "", fmt.Errorf("匿名函数暂不支持（计划第22轮）：%s", n.String())
	}
	return "", fmt.Errorf("不支持的表达式: %T", e)
}

// genInfix 生成二元运算；短路逻辑就地展开成 if 语句
func (g *Gen) genInfix(n *ast.InfixExpression) (string, error) {
	if n.Operator == "并且" || n.Operator == "或者" {
		return g.genShortCircuit(n)
	}
	left, err := g.genExpr(n.Left)
	if err != nil {
		return "", err
	}
	right, err := g.genExpr(n.Right)
	if err != nil {
		return "", err
	}
	var fn string
	switch n.Operator {
	case "+":
		fn = "z_add"
	case "-":
		fn = "z_sub"
	case "*":
		fn = "z_mul"
	case "/":
		fn = "z_div"
	case "%":
		fn = "z_mod"
	case "<":
		fn = "z_lt"
	case "<=":
		fn = "z_le"
	case ">":
		fn = "z_gt"
	case ">=":
		fn = "z_ge"
	case "==":
		fn = "z_eq"
	case "!=":
		fn = "z_ne"
	case "否则":
		return "", fmt.Errorf("『否则』fallback 运算符暂不支持")
	default:
		return "", fmt.Errorf("不支持的运算符: %s", n.Operator)
	}
	return fmt.Sprintf("%s(%s, %s)", fn, left, right), nil
}

// genShortCircuit 把「并且 / 或者」展开为语句序列（保持短路语义）
func (g *Gen) genShortCircuit(n *ast.InfixExpression) (string, error) {
	left, err := g.genExpr(n.Left)
	if err != nil {
		return "", err
	}
	tmp := g.temp()
	g.line("ZVal %s;", tmp)
	if n.Operator == "并且" {
		g.line("if (!z_truthy(%s)) { %s = z_bool(0); } else {", left, tmp)
	} else {
		g.line("if (z_truthy(%s)) { %s = z_bool(1); } else {", left, tmp)
	}
	g.indent++
	right, err := g.genExpr(n.Right)
	if err != nil {
		return "", err
	}
	g.line("%s = z_bool(z_truthy(%s));", tmp, right)
	g.indent--
	g.line("}")
	return tmp, nil
}

// genIfExpr 把 if 表达式降级为「临时变量 + if 语句」
func (g *Gen) genIfExpr(n *ast.IfExpression) (string, error) {
	cond, err := g.genExpr(n.Condition)
	if err != nil {
		return "", err
	}
	tmp := g.temp()
	g.line("ZVal %s;", tmp)
	g.line("if (z_truthy(%s)) {", cond)
	g.indent++
	g.pushScope()
	val, err := g.genBlockValue(n.Consequence)
	if err != nil {
		return "", err
	}
	g.popScope()
	g.line("%s = %s;", tmp, val)
	g.indent--
	if n.Alternative != nil {
		g.line("} else {")
		g.indent++
		g.pushScope()
		val2, err := g.genBlockValue(n.Alternative)
		if err != nil {
			return "", err
		}
		g.popScope()
		g.line("%s = %s;", tmp, val2)
		g.indent--
	}
	g.line("}")
	return tmp, nil
}

// genBlockValue 生成块并返回块的值（最后一条表达式语句）
func (g *Gen) genBlockValue(b *ast.BlockStatement) (string, error) {
	if b == nil || len(b.Statements) == 0 {
		return "z_null()", nil
	}
	for i, stmt := range b.Statements {
		isLast := i == len(b.Statements)-1
		if isLast {
			if es, ok := stmt.(*ast.ExpressionStatement); ok {
				return g.genExpr(es.Expression)
			}
		}
		if err := g.genStmt(stmt); err != nil {
			return "", err
		}
	}
	return "z_null()", nil
}

// genCall 函数调用：先查内置表，再查用户函数表
func (g *Gen) genCall(n *ast.CallExpression) (string, error) {
	ident, ok := n.Function.(*ast.Identifier)
	if !ok {
		return "", fmt.Errorf("仅支持按名字调用函数: %s", n.Function.String())
	}
	// 内置函数
	if args, ok, err := g.genBuiltinCall(ident.Value, n); ok {
		return args, err
	}
	// 用户函数
	fi, exists := g.funcs[ident.Value]
	if !exists {
		return "", fmt.Errorf("未定义的函数: %s", ident.Value)
	}
	var args []string
	for _, a := range n.Arguments {
		v, err := g.genExpr(a)
		if err != nil {
			return "", err
		}
		args = append(args, v)
	}
	if len(args) != fi.Arity {
		return "", fmt.Errorf("%s() 参数数量不匹配: 期望 %d，得到 %d", ident.Value, fi.Arity, len(args))
	}
	return fmt.Sprintf("%s(%s)", cFuncName(ident.Value), strings.Join(args, ", ")), nil
}

// genAssignExpr 赋值：变量赋值 / 下标赋值
func (g *Gen) genAssignExpr(n *ast.AssignExpression) (string, error) {
	if n.Name != nil {
		val, err := g.genExpr(n.Value)
		if err != nil {
			return "", err
		}
		if !g.declared(n.Name.Value) {
			// 与解释器一致：赋值即定义（env.Set 语义）
			g.declare(n.Name.Value)
			g.line("ZVal %s = %s;", cVarName(n.Name.Value), val)
			return cVarName(n.Name.Value), nil
		}
		g.line("%s = %s;", cVarName(n.Name.Value), val)
		return cVarName(n.Name.Value), nil
	}
	if n.IndexTarget != nil {
		left, err := g.genExpr(n.IndexTarget.Left)
		if err != nil {
			return "", err
		}
		idx, err := g.genExpr(n.IndexTarget.Index)
		if err != nil {
			return "", err
		}
		val, err := g.genExpr(n.Value)
		if err != nil {
			return "", err
		}
		g.line("z_index_set(%s, %s, %s);", left, idx, val)
		return "z_null()", nil
	}
	return "", fmt.Errorf("不支持的赋值目标")
}

// ── C 字面量工具 ─────────────────────────────────────────

func cString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString("\\\"")
		case '\\':
			b.WriteString("\\\\")
		case '\n':
			b.WriteString("\\n")
		case '\r':
			b.WriteString("\\r")
		case '\t':
			b.WriteString("\\t")
		default:
			if r < 0x20 {
				b.WriteString(fmt.Sprintf("\\x%02x", r))
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func cFloat(f float64) string {
	if math.IsInf(f, 1) {
		return "(1.0/0.0)"
	}
	if math.IsInf(f, -1) {
		return "(-1.0/0.0)"
	}
	if math.IsNaN(f) {
		return "(0.0/0.0)"
	}
	s := strconv.FormatFloat(f, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

type exprList []ast.Expression

func (l exprList) Len() int           { return len(l) }
func (l exprList) Less(i, j int) bool { return l[i].String() < l[j].String() }
func (l exprList) Swap(i, j int)      { l[i], l[j] = l[j], l[i] }

func sortExprs(es []ast.Expression) {
	// 简单插入排序，避免额外依赖
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && es[j].String() < es[j-1].String(); j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}
