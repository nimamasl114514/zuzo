package llvmbackend

// 表达式 codegen：与 cbackend/expr.go 逐语义等价迁移到文本 LLVM IR。
//
// 约定：
//   - genExpr 返回 *Value，其 Slot 是「指向 %ZVal 的 ptr」（alloca 槽位或全局符号）；
//   - 需要多步求值的构造（if / 短路 / 哈希字面量 / 闭包）按 ANF 思路先落到槽位，
//     再返回槽位，后续表达式读该槽位；
//   - 每个基本块必须有 terminator；写分支/循环时先 place(label) 再发指令。

import (
	"fmt"
	"sort"
	"strings"

	"zuzo/ast"
)

// lxCallSret 调用一个「返回 ZVal」的自生成函数（@zf_* / @zdef_*），
// 签名统一为 void (ptr sret(%ZVal), i32 argc, ptr argv, ptr env)。
func (g *Gen) lxCallSret(sym string, args ...string) *Value {
	v := g.tempZ()
	suffix := ""
	if len(args) > 0 {
		suffix = ", " + strings.Join(args, ", ")
	}
	g.emit("call void %s(ptr sret(%%ZVal) %s%s)", sym, v, suffix)
	return &Value{Slot: v}
}

// lxBool 把 i1 条件转成 zuzo 布尔 ZVal（z_bool(0|1)）。
func (g *Gen) lxBool(cond string) *Value {
	zi := g.fb.nextTemp()
	g.emit("%s = zext i1 %s to i32", zi, cond)
	return g.constBoolReg(zi)
}

// constBoolReg 用 i32 寄存器构造 z_bool。
func (g *Gen) constBoolReg(i32reg string) *Value {
	return g.callZ("z_bool", "i32 "+i32reg)
}

// genExpr 生成一个表达式的值。
func (g *Gen) genExpr(e ast.Expression) (*Value, error) {
	switch n := e.(type) {
	case *ast.IntegerLiteral:
		return g.constInt(n.Value), nil

	case *ast.FloatLiteral:
		return g.constFloat(n.Value), nil

	case *ast.StringLiteral:
		return g.constStr(n.Value), nil

	case *ast.FStringLiteral:
		return g.genFString(n)

	case *ast.BooleanLiteral:
		return g.constBool(n.Value), nil

	case *ast.NullLiteral:
		return g.constNull(), nil

	case *ast.Identifier:
		return g.genIdent(n.Value)

	case *ast.PrefixExpression:
		right, err := g.genExpr(n.Right)
		if err != nil {
			return nil, err
		}
		switch n.Operator {
		case "-":
			return g.callZ("z_neg", "ptr "+right.Slot), nil
		case "!", "非":
			return g.callZ("z_not", "ptr "+right.Slot), nil
		}
		return nil, fmt.Errorf("不支持的前缀运算符: %s", n.Operator)

	case *ast.InfixExpression:
		return g.genInfix(n)

	case *ast.IfExpression:
		return g.genIfExpr(n)

	case *ast.CallExpression:
		return g.genCall(n)

	case *ast.ArrayLiteral:
		items := make([]*Value, 0, len(n.Elements))
		for _, el := range n.Elements {
			v, err := g.genExpr(el)
			if err != nil {
				return nil, err
			}
			items = append(items, v)
		}
		if len(items) == 0 {
			return g.callZ("z_array"), nil
		}
		arr, cnt := g.zvalArray(items)
		return g.callZ("z_array_with", fmt.Sprintf("i64 %d", cnt), "ptr "+arr), nil

	case *ast.HashLiteral:
		return g.genHashLit(n)

	case *ast.IndexExpression:
		left, err := g.genExpr(n.Left)
		if err != nil {
			return nil, err
		}
		idx, err := g.genExpr(n.Index)
		if err != nil {
			return nil, err
		}
		return g.callZ("z_index_get", "ptr "+left.Slot, "ptr "+idx.Slot), nil

	case *ast.AssignExpression:
		return g.genAssignExpr(n)

	case *ast.FunctionLiteral:
		fi := g.ana.byNode[n]
		if fi == nil {
			return nil, fmt.Errorf("内部错误：未分析到的函数字面量")
		}
		return g.genClosureRef(fi)

	case *ast.ErrorExpression:
		return nil, fmt.Errorf("解析错误: %s", n.Message)
	}
	return nil, fmt.Errorf("不支持的表达式: %T", e)
}

// genFString 字符串插值：z_concat(n, ZVal[])，与 cbackend 完全同口径。
func (g *Gen) genFString(n *ast.FStringLiteral) (*Value, error) {
	parts := make([]*Value, 0, len(n.Parts))
	for _, p := range n.Parts {
		if sl, ok := p.(*ast.StringLiteral); ok {
			parts = append(parts, g.constStr(sl.Value))
			continue
		}
		v, err := g.genExpr(p)
		if err != nil {
			return nil, err
		}
		parts = append(parts, v)
	}
	if len(parts) == 0 {
		return g.constStr(""), nil
	}
	arr, cnt := g.zvalArray(parts)
	return g.callZ("z_concat", fmt.Sprintf("i32 %d", cnt), "ptr "+arr), nil
}

// genHashLit 哈希字面量：z_hash() + 逐对 z_hash_set（键按 String() 排序，保证稳定）。
func (g *Gen) genHashLit(n *ast.HashLiteral) (*Value, error) {
	tmp := g.callZ("z_hash")
	keys := make([]ast.Expression, 0, len(n.Pairs))
	for k := range n.Pairs {
		keys = append(keys, k)
	}
	sort.SliceStable(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })
	for _, k := range keys {
		ks, err := g.genExpr(k)
		if err != nil {
			return nil, err
		}
		vs, err := g.genExpr(n.Pairs[k])
		if err != nil {
			return nil, err
		}
		g.callVoid("z_hash_set", "ptr "+tmp.Slot, "ptr "+ks.Slot, "ptr "+vs.Slot)
	}
	return tmp, nil
}

// genIdent 变量/函数引用：局部槽位 → 顶层全局 → 具名函数（闭包引用）→ define 构造器。
func (g *Gen) genIdent(name string) (*Value, error) {
	if slot, ok := g.lookupVar(name); ok {
		ref, err := g.varRef(slot)
		if err != nil {
			return nil, err
		}
		return &Value{Slot: ref}, nil
	}
	if g.globals[name] {
		return &Value{Slot: g.globalSlot(name)}, nil
	}
	if fi, ok := g.funcs[name]; ok {
		return g.genClosureRef(fi)
	}
	if _, ok := g.defines[name]; ok {
		return g.callZ("z_closure",
			"ptr @zdef_"+sanitizeIdent(name),
			"ptr null",
			"ptr "+g.strConst(name)), nil
	}
	return nil, fmt.Errorf("未定义的变量: %s", name)
}

// genInfix 二元运算；短路逻辑就地展开。
func (g *Gen) genInfix(n *ast.InfixExpression) (*Value, error) {
	if n.Operator == "并且" || n.Operator == "或者" {
		return g.genShortCircuit(n)
	}
	left, err := g.genExpr(n.Left)
	if err != nil {
		return nil, err
	}
	right, err := g.genExpr(n.Right)
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("『否则』fallback 运算符暂不支持")
	default:
		return nil, fmt.Errorf("不支持的运算符: %s", n.Operator)
	}
	return g.callZ(fn, "ptr "+left.Slot, "ptr "+right.Slot), nil
}

// genShortCircuit 并且/或者：左值真值决定短路，结果统一为布尔值。
//
//	并且：左假 → false；左真 → bool(truthy(右))
//	或者：左真 → true；左假 → bool(truthy(右))
func (g *Gen) genShortCircuit(n *ast.InfixExpression) (*Value, error) {
	left, err := g.genExpr(n.Left)
	if err != nil {
		return nil, err
	}
	res := g.tempZ()
	lShort := g.newLabel()
	lRight := g.newLabel()
	lEnd := g.newLabel()

	cond := g.truthy(left)
	if n.Operator == "并且" {
		g.condBr(cond, lRight, lShort)
	} else {
		g.condBr(cond, lShort, lRight)
	}

	// 短路分支：并且→false，或者→true
	g.place(lShort)
	shortVal := g.constBool(n.Operator != "并且")
	g.copyZ(res, shortVal.Slot)
	g.br(lEnd)

	// 求值右分支
	g.place(lRight)
	right, err := g.genExpr(n.Right)
	if err != nil {
		return nil, err
	}
	rv := g.lxBool(g.truthy(right))
	g.copyZ(res, rv.Slot)
	g.br(lEnd)

	g.place(lEnd)
	return &Value{Slot: res}, nil
}

// genIfExpr 如果表达式：then/else/end 三块 + 结果槽；无 else 时结果为 null。
func (g *Gen) genIfExpr(n *ast.IfExpression) (*Value, error) {
	cond, err := g.genExpr(n.Condition)
	if err != nil {
		return nil, err
	}
	res := g.tempZ()
	lThen := g.newLabel()
	lElse := g.newLabel()
	lEnd := g.newLabel()

	c := g.truthy(cond)
	g.condBr(c, lThen, lElse)

	g.place(lThen)
	g.pushScope()
	val, err := g.genBlockValue(n.Consequence)
	if err != nil {
		g.popScope()
		return nil, err
	}
	g.popScope()
	if !g.fb.terminated {
		g.copyZ(res, val.Slot)
		g.br(lEnd)
	}

	if n.Alternative != nil {
		g.place(lElse)
		g.pushScope()
		val2, err := g.genBlockValue(n.Alternative)
		if err != nil {
			g.popScope()
			return nil, err
		}
		g.popScope()
		if !g.fb.terminated {
			g.copyZ(res, val2.Slot)
			g.br(lEnd)
		}
	} else {
		// 无 else：false 分支直接给 null
		g.place(lElse)
		nullVal := g.constNull()
		g.copyZ(res, nullVal.Slot)
		g.br(lEnd)
	}

	g.place(lEnd)
	return &Value{Slot: res}, nil
}

// genBlockValue 块的值：最后一个语句是表达式语句时取其值，否则 null。
func (g *Gen) genBlockValue(b *ast.BlockStatement) (*Value, error) {
	if b == nil || len(b.Statements) == 0 {
		return g.constNull(), nil
	}
	for i, stmt := range b.Statements {
		isLast := i == len(b.Statements)-1
		if isLast {
			if es, ok := stmt.(*ast.ExpressionStatement); ok {
				return g.genExpr(es.Expression)
			}
		}
		if err := g.genStmt(stmt); err != nil {
			return nil, err
		}
	}
	return g.constNull(), nil
}

// genCall 函数调用：文本命名空间 → 内置 → 局部变量里的函数值 →
// define 构造器 → 具名直接调 → 顶层变量里的函数值 → 任意表达式求出的函数值。
func (g *Gen) genCall(n *ast.CallExpression) (*Value, error) {
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
			return nil, err
		}
		if v, ok, err := g.genBuiltinCall(ident.Value, n); ok {
			return v, err
		}
		// 作用域内的局部变量/参数永远遮蔽同名函数：变量里存的是函数 → 走 z_call
		if _, ok := g.lookupVar(ident.Value); ok {
			f, err := g.genIdent(ident.Value)
			if err != nil {
				return nil, err
			}
			return g.genApply(f, n)
		}
		// 定义（数据建模）实例化：人()
		if _, ok := g.defines[ident.Value]; ok {
			return g.lxCallSret("@zdef_"+sanitizeIdent(ident.Value), "i32 0", "ptr null", "ptr null"), nil
		}
		// 具名函数直接调用；与顶层变量同名时按「后定义者胜」裁决（与解释器一致）
		if fi, ok := g.funcs[ident.Value]; ok {
			if g.globals[ident.Value] && g.globalLine[ident.Value] > fi.Node.Token.Line {
				f, err := g.genIdent(ident.Value)
				if err != nil {
					return nil, err
				}
				return g.genApply(f, n)
			}
			args := make([]*Value, 0, len(n.Arguments))
			for _, a := range n.Arguments {
				v, err := g.genExpr(a)
				if err != nil {
					return nil, err
				}
				args = append(args, v)
			}
			if len(args) != len(fi.Node.Parameters) {
				return nil, fmt.Errorf("%s() 参数数量不匹配: 期望 %d，得到 %d",
					ident.Value, len(fi.Node.Parameters), len(args))
			}
			// 嵌套具名函数若带捕获，需要显式构造环境
			envArg := "ptr null"
			if len(fi.Captures) > 0 {
				ref, err := g.genClosureRef(fi)
				if err != nil {
					return nil, err
				}
				ev := g.callR("ptr", "z_fn_env", "ptr "+ref.Slot)
				envArg = "ptr " + ev
			}
			arr, cnt := g.zvalArray(args)
			return g.lxCallSret(g.fnSym(fi),
				fmt.Sprintf("i32 %d", cnt), "ptr "+arr, envArg), nil
		}
		// 顶层变量里存的是函数值（含全局闭包）
		if g.globals[ident.Value] {
			f, err := g.genIdent(ident.Value)
			if err != nil {
				return nil, err
			}
			return g.genApply(f, n)
		}
		return nil, fmt.Errorf("未定义的函数: %s", ident.Value)
	}

	f, err := g.genExpr(n.Function)
	if err != nil {
		return nil, err
	}
	return g.genApply(f, n)
}

// genApply 对函数值发起调用：z_call(fn, argc, argv)。
func (g *Gen) genApply(fnRef *Value, n *ast.CallExpression) (*Value, error) {
	args := make([]*Value, 0, len(n.Arguments))
	for _, a := range n.Arguments {
		v, err := g.genExpr(a)
		if err != nil {
			return nil, err
		}
		args = append(args, v)
	}
	arr, cnt := g.zvalArray(args)
	return g.callZ("z_call",
		"ptr "+fnRef.Slot,
		fmt.Sprintf("i32 %d", cnt),
		"ptr "+arr), nil
}

// genAssignExpr 赋值：变量 / 下标。
func (g *Gen) genAssignExpr(n *ast.AssignExpression) (*Value, error) {
	if n.Name != nil {
		val, err := g.genExpr(n.Value)
		if err != nil {
			return nil, err
		}
		name := n.Name.Value
		// 已存在的变量：写回其槽位
		if slot, ok := g.lookupVar(name); ok {
			ref, err := g.varRef(slot)
			if err != nil {
				return nil, err
			}
			g.copyZ(ref, val.Slot)
			return &Value{Slot: ref}, nil
		}
		// 顶层变量：直接写全局
		if g.globals[name] {
			slot := g.globalSlot(name)
			g.copyZ(slot, val.Slot)
			return &Value{Slot: slot}, nil
		}
		// 与解释器一致：赋值即定义；被内层函数捕获的变量需要装箱
		if g.cur != nil {
			if m := g.ana.Boxed[g.cur]; m != nil && m[name] {
				box := g.boxVar(name, val)
				ref, err := g.varRef(varSlot{kind: vBox, slot: box})
				if err != nil {
					return nil, err
				}
				return &Value{Slot: ref}, nil
			}
		}
		slot := g.tempZ()
		g.copyZ(slot, val.Slot)
		g.setVar(name, varSlot{kind: vPlain, slot: slot})
		return &Value{Slot: slot}, nil
	}

	if n.IndexTarget != nil {
		left, err := g.genExpr(n.IndexTarget.Left)
		if err != nil {
			return nil, err
		}
		idx, err := g.genExpr(n.IndexTarget.Index)
		if err != nil {
			return nil, err
		}
		val, err := g.genExpr(n.Value)
		if err != nil {
			return nil, err
		}
		g.callVoid("z_index_set", "ptr "+left.Slot, "ptr "+idx.Slot, "ptr "+val.Slot)
		return g.constNull(), nil
	}
	return nil, fmt.Errorf("不支持的赋值目标")
}
