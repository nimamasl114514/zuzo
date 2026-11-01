package cbackend

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"zuzo/ast"
)

// genExpr 生成一个 C 表达式。需要多步求值的表达式（if / 短路 / 哈希字面量 /
// 闭包构造）会先把语句写进当前块，再返回一个临时变量名（ANF 降级）。
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
		return g.genIdent(n.Value)

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
		if len(items) == 0 {
			return "z_array()", nil
		}
		return fmt.Sprintf("z_array_with(%d, (ZVal[]){%s})", len(items), strings.Join(items, ", ")), nil

	case *ast.HashLiteral:
		tmp := g.temp()
		g.line("ZVal %s = z_hash();", tmp)
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
		fi := g.ana.byNode[n]
		if fi == nil {
			return "", fmt.Errorf("内部错误：未分析到的函数字面量")
		}
		return g.genClosureRef(fi)
	}
	return "", fmt.Errorf("不支持的表达式: %T", e)
}

// genIdent 变量/函数引用
func (g *Gen) genIdent(name string) (string, error) {
	if slot, ok := g.lookupVar(name); ok {
		switch slot.kind {
		case vPlain:
			return cVarName(name), nil
		case vBox:
			return cVarName(name) + "->v", nil
		case vCapture:
			return "zenv->" + envFieldName(name) + "->v", nil
		}
	}
	if g.globals[name] {
		return cGlobalName(name), nil
	}
	if fi, ok := g.funcs[name]; ok {
		return g.genClosureRef(fi)
	}
	if _, ok := g.defines[name]; ok {
		return fmt.Sprintf("z_closure(zdef_%s, NULL, %s)", sanitizeIdent(name), cString(name)), nil
	}
	return "", fmt.Errorf("未定义的变量: %s", name)
}

// genClosureRef 构造一个函数值（闭包）
func (g *Gen) genClosureRef(fi *fnInfo) (string, error) {
	display := fi.Name
	if display == "" {
		display = "匿名"
	}
	if fi.EnvType == "" {
		return fmt.Sprintf("z_closure(%s, NULL, %s)", fi.CName, cString(display)), nil
	}
	env := g.temp()
	g.line("%s* %s = (%s*)z_new_env_ex(%d, %s, sizeof(%s));", fi.EnvType, env, fi.EnvType, len(fi.Captures), envOffsets(len(fi.Captures)), fi.EnvType)
	for _, name := range fi.Captures {
		slot, ok := g.lookupVar(name)
		if !ok {
			return "", fmt.Errorf("闭包捕获失败：找不到外层变量 %s", name)
		}
		switch slot.kind {
		case vBox:
			g.line("%s->%s = %s;", env, envFieldName(name), cVarName(name))
		case vCapture:
			g.line("%s->%s = zenv->%s;", env, envFieldName(name), envFieldName(name))
		case vPlain:
			// 兜底：未装箱的变量临时装箱（语义上不共享，正常分析路径不会走到）
			g.line("%s->%s = z_box(%s);", env, envFieldName(name), cVarName(name))
		}
	}
	return fmt.Sprintf("z_closure(%s, %s, %s)", fi.CName, env, cString(display)), nil
}

// genInfix 二元运算；短路逻辑就地展开
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

// genCall 函数调用：内置 → 文本命名空间 → 具名直接调 → 函数值调用
func (g *Gen) genCall(n *ast.CallExpression) (string, error) {
	// 文本.成员(...)
	if idx, ok := n.Function.(*ast.IndexExpression); ok {
		if left, ok := idx.Left.(*ast.Identifier); ok && left.Value == "文本" {
			if lit, ok := idx.Index.(*ast.StringLiteral); ok {
				if v, hit, err := g.genTextBuiltin(lit.Value, n); hit {
					return v, err
				}
			}
		}
	}
	ident, isIdent := n.Function.(*ast.Identifier)
	if isIdent {
		if err := g.checkUnsupported(ident.Value, n.Token.Line, n.Token.Column); err != nil {
			return "", err
		}
		if args, ok, err := g.genBuiltinCall(ident.Value, n); ok {
			return args, err
		}
		// 作用域内的局部变量/参数永远遮蔽同名函数：变量里存的是函数 → 走 z_call
		if _, ok := g.lookupVar(ident.Value); ok {
			f, err := g.genIdent(ident.Value)
			if err != nil {
				return "", err
			}
			return g.genApply(f, n)
		}
		// 定义（数据建模）实例化：人()
		if _, ok := g.defines[ident.Value]; ok {
			return fmt.Sprintf("zdef_%s(0, NULL, NULL)", sanitizeIdent(ident.Value)), nil
		}
		// 具名函数直接调用；与顶层变量同名时按「后定义者胜」裁决（与解释器一致）
		if fi, ok := g.funcs[ident.Value]; ok {
			if g.globals[ident.Value] && g.globalLine[ident.Value] > fi.Node.Token.Line {
				f, err := g.genIdent(ident.Value)
				if err != nil {
					return "", err
				}
				return g.genApply(f, n)
			}
			var args []string
			for _, a := range n.Arguments {
				v, err := g.genExpr(a)
				if err != nil {
					return "", err
				}
				args = append(args, v)
			}
			if len(args) != len(fi.Node.Parameters) {
				return "", fmt.Errorf("%s() 参数数量不匹配: 期望 %d，得到 %d",
					ident.Value, len(fi.Node.Parameters), len(args))
			}
			envArg := "NULL"
			// 嵌套具名函数若带捕获，需要显式构造环境
			if len(fi.Captures) > 0 {
				ref, err := g.genClosureRef(fi)
				if err != nil {
					return "", err
				}
				envArg = "z_fn_env(" + ref + ")"
			}
			if len(args) == 0 {
				return fmt.Sprintf("%s(0, NULL, %s)", fi.CName, envArg), nil
			}
			return fmt.Sprintf("%s(%d, (ZVal[]){%s}, %s)", fi.CName, len(args), strings.Join(args, ", "), envArg), nil
		}
		// 顶层变量里存的是函数值（含全局闭包）
		if g.globals[ident.Value] {
			f, err := g.genIdent(ident.Value)
			if err != nil {
				return "", err
			}
			return g.genApply(f, n)
		}
		return "", fmt.Errorf("未定义的函数: %s", ident.Value)
	}

	f, err := g.genExpr(n.Function)
	if err != nil {
		return "", err
	}
	return g.genApply(f, n)
}

// genApply 对函数值发起调用
func (g *Gen) genApply(fnRef string, n *ast.CallExpression) (string, error) {
	var args []string
	for _, a := range n.Arguments {
		v, err := g.genExpr(a)
		if err != nil {
			return "", err
		}
		args = append(args, v)
	}
	if len(args) == 0 {
		return fmt.Sprintf("z_call(%s, 0, NULL)", fnRef), nil
	}
	return fmt.Sprintf("z_call(%s, %d, (ZVal[]){%s})", fnRef, len(args), strings.Join(args, ", ")), nil
}

// genAssignExpr 赋值：变量 / 下标
func (g *Gen) genAssignExpr(n *ast.AssignExpression) (string, error) {
	if n.Name != nil {
		val, err := g.genExpr(n.Value)
		if err != nil {
			return "", err
		}
		if slot, ok := g.lookupVar(n.Name.Value); ok {
			switch slot.kind {
			case vPlain:
				g.line("%s = %s;", cVarName(n.Name.Value), val)
				return cVarName(n.Name.Value), nil
			case vBox:
				g.line("%s->v = %s;", cVarName(n.Name.Value), val)
				return cVarName(n.Name.Value) + "->v", nil
			case vCapture:
				g.line("zenv->%s->v = %s;", envFieldName(n.Name.Value), val)
				return "zenv->" + envFieldName(n.Name.Value) + "->v", nil
			}
		}
		// 顶层变量：直接写静态量
		if g.globals[n.Name.Value] {
			g.line("%s = %s;", cGlobalName(n.Name.Value), val)
			return cGlobalName(n.Name.Value), nil
		}
		// 与解释器一致：赋值即定义
		if g.boxedBy(g.cur, n.Name.Value) {
			g.setVar(n.Name.Value, varSlot{kind: vBox})
			g.line("ZBox* %s = z_box(%s);", cVarName(n.Name.Value), val)
			return cVarName(n.Name.Value) + "->v", nil
		}
		g.setVar(n.Name.Value, varSlot{kind: vPlain})
		g.line("ZVal %s = %s;", cVarName(n.Name.Value), val)
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

func sortExprs(es []ast.Expression) {
	for i := 1; i < len(es); i++ {
		for j := i; j > 0 && es[j].String() < es[j-1].String(); j-- {
			es[j], es[j-1] = es[j-1], es[j]
		}
	}
}

// envOffsets 生成 ZBox* 偏移表的 C 复合字面量（env 字段全是 ZBox*，8 字节）
func envOffsets(n int) string {
	if n <= 0 {
		return "NULL"
	}
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		parts = append(parts, fmt.Sprintf("%d", i*8))
	}
	return "(size_t[]){" + strings.Join(parts, ", ") + "}"
}
