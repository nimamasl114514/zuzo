package evaluator

// sap_loop.go — SAP Loop Fusion（SAP 循环熔合）
//
// 核心思想：将循环体编译成"原生 Go 流水线"，完全消除每次迭代的 Eval 调度开销。
//
// 四级优化：
//
//	Level 1 — Fused Loop：预编译条件/体/更新为闭包，循环内直接调用
//	           消除 Eval type-switch 调度 + map 查找
//	Level 2 — Hardened Fused Loop：闭包内部已类型硬化（由现有 SAP 保证）
//	           消除运行时类型断言
//	Level 3 — Native Int64 Loop：简单整数循环直接生成 int64 操作
//	           零函数调用，直接槽位读写 + int64 运算
//	Level 4 — Smi-Tagged Native Loop：64 位标记值，整数完全在寄存器中运算
//	           零堆分配，零接口开销，零类型断言，纯标量运算

import (
	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/object"
)

// ================================================================
// 第一部分：扩展编译器 — 支持更多 AST 节点类型
// ================================================================

// syncValue 同步变量到槽位与 Smi 值槽（声明/赋值共用，与慢路径写入语义一致）
func syncValue(env *object.Environment, slot int, val object.Object) {
	if env.HasSlots() {
		env.SetSlot(slot, val)
	}
	// Smi 同步：如果是 Integer，编码为 Smi 写入 valueSlots
	if env.HasValueSlots() {
		if smiVal, ok := object.ObjectAsSmi(val); ok {
			env.SetValueSlot(slot, smiVal)
		}
	}
}

// compileLet 编译变量声明语句为槽位闭包
// 变量 x = expr → 求值 expr → 写入 map + 槽位
func (c *SAPCompiler) compileLet(n *ast.LetStatement) CompileFunc {
	if n.Name == nil || n.Value == nil {
		return nil
	}
	slot, ok := c.slotMap[n.Name.Value]
	if !ok {
		return nil
	}
	valFn := c.Compile(n.Value)
	if valFn == nil {
		return nil
	}
	// 慢路径语义：声明值求值出错时传播错误，不写入环境
	if c.needsErrorPropagation(n.Value) {
		return func(env *object.Environment) object.Object {
			val := valFn(env)
			if val.Type() == object.ERROR_OBJ {
				return val
			}
			env.Set(n.Name.Value, val)
			syncValue(env, slot, val)
			return NULL
		}
	}
	return func(env *object.Environment) object.Object {
		val := valFn(env)
		env.Set(n.Name.Value, val)
		syncValue(env, slot, val)
		return NULL
	}
}

// compileReturn 编译返回语句
func (c *SAPCompiler) compileReturn(n *ast.ReturnStatement) CompileFunc {
	if n.ReturnValue == nil {
		return func(env *object.Environment) object.Object {
			return &object.ReturnValue{Value: NULL}
		}
	}
	valFn := c.Compile(n.ReturnValue)
	if valFn == nil {
		return nil
	}
	// 慢路径语义：返回值求值出错时直接传播错误
	if c.needsErrorPropagation(n.ReturnValue) {
		return func(env *object.Environment) object.Object {
			val := valFn(env)
			if val.Type() == object.ERROR_OBJ {
				return val
			}
			return &object.ReturnValue{Value: val}
		}
	}
	return func(env *object.Environment) object.Object {
		return &object.ReturnValue{Value: valFn(env)}
	}
}

// compileAssign 编译赋值表达式为槽位闭包
// 将 x = expr 编译为：直接求值 expr → 写入槽位 + map
func (c *SAPCompiler) compileAssign(n *ast.AssignExpression) CompileFunc {
	if n.Name == nil {
		return nil // 暂不支持索引赋值
	}
	slot, ok := c.slotMap[n.Name.Value]
	if !ok {
		return nil
	}
	valFn := c.Compile(n.Value)
	if valFn == nil {
		return nil
	}
	// 慢路径语义：赋值前错误传播，不写入环境
	if c.needsErrorPropagation(n.Value) {
		return func(env *object.Environment) object.Object {
			val := valFn(env)
			if val.Type() == object.ERROR_OBJ {
				return val
			}
			env.Assign(n.Name.Value, val)
			syncValue(env, slot, val)
			return val
		}
	}
	return func(env *object.Environment) object.Object {
		val := valFn(env)
		env.Assign(n.Name.Value, val)
		syncValue(env, slot, val)
		return val
	}
}

// compileExprStmt 编译表达式语句
func (c *SAPCompiler) compileExprStmt(n *ast.ExpressionStatement) CompileFunc {
	if n.Expression == nil {
		return nil
	}
	return c.Compile(n.Expression)
}

// compilePrefix 编译前缀表达式（-x, !x, 非x）
func (c *SAPCompiler) compilePrefix(n *ast.PrefixExpression) CompileFunc {
	rightFn := c.Compile(n.Right)
	if rightFn == nil {
		return nil
	}
	rightType := c.inf.GetNodeType(n.Right)
	// 错误传播：右操作数可能产生守卫错误（与慢路径 evalPrefixExpression 一致）
	propagate := c.needsErrorPropagation(n.Right)

	switch n.Operator {
	case "-":
		if isIntType(rightType) {
			if propagate {
				return func(env *object.Environment) object.Object {
					v := rightFn(env)
					if v.Type() == object.ERROR_OBJ {
						return v
					}
					return &object.Integer{Value: -v.(*object.Integer).Value}
				}
			}
			return func(env *object.Environment) object.Object {
				v := rightFn(env).(*object.Integer).Value
				return &object.Integer{Value: -v}
			}
		}
		if isFloatType(rightType) {
			if propagate {
				return func(env *object.Environment) object.Object {
					v := rightFn(env)
					if v.Type() == object.ERROR_OBJ {
						return v
					}
					return &object.Float{Value: -v.(*object.Float).Value}
				}
			}
			return func(env *object.Environment) object.Object {
				v := rightFn(env).(*object.Float).Value
				return &object.Float{Value: -v}
			}
		}
	case "!", "非":
		return func(env *object.Environment) object.Object {
			v := rightFn(env)
			return nativeBoolToBooleanObject(!isTruthy(v))
		}
	}
	return nil
}

// compileBlock 编译代码块为单一闭包（顺序执行所有语句）
func (c *SAPCompiler) compileBlock(n *ast.BlockStatement) CompileFunc {
	fns := make([]CompileFunc, 0, len(n.Statements))
	for _, stmt := range n.Statements {
		fn := c.compileStmtToFn(stmt)
		if fn == nil {
			return nil
		}
		fns = append(fns, fn)
	}
	return func(env *object.Environment) object.Object {
		for _, fn := range fns {
			result := fn(env)
			if result != nil {
				rt := result.Type()
				if rt == object.RETURN_VALUE || rt == object.ERROR_OBJ {
					return result
				}
			}
		}
		return NULL
	}
}

// compileStmtToFn 将语句编译为闭包（用于循环体内联）
func (c *SAPCompiler) compileStmtToFn(stmt ast.Statement) CompileFunc {
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		return c.compileExprStmt(s)

	case *ast.LetStatement:
		if s.Name == nil || s.Value == nil {
			return nil
		}
		slot, ok := c.slotMap[s.Name.Value]
		if !ok {
			return nil
		}
		valFn := c.Compile(s.Value)
		if valFn == nil {
			return nil
		}
		// 慢路径语义：声明时错误传播，不写入环境
		if c.needsErrorPropagation(s.Value) {
			return func(env *object.Environment) object.Object {
				val := valFn(env)
				if val.Type() == object.ERROR_OBJ {
					return val
				}
				env.Set(s.Name.Value, val)
				syncValue(env, slot, val)
				return NULL
			}
		}
		return func(env *object.Environment) object.Object {
			val := valFn(env)
			env.Set(s.Name.Value, val)
			syncValue(env, slot, val)
			return NULL
		}

	case *ast.ReturnStatement:
		if s.ReturnValue == nil {
			return func(env *object.Environment) object.Object {
				return &object.ReturnValue{Value: NULL}
			}
		}
		valFn := c.Compile(s.ReturnValue)
		if valFn == nil {
			return nil
		}
		// 慢路径语义：返回值求值出错时直接传播错误
		if c.needsErrorPropagation(s.ReturnValue) {
			return func(env *object.Environment) object.Object {
				val := valFn(env)
				if val.Type() == object.ERROR_OBJ {
					return val
				}
				return &object.ReturnValue{Value: val}
			}
		}
		return func(env *object.Environment) object.Object {
			return &object.ReturnValue{Value: valFn(env)}
		}
	}
	return nil
}

// ================================================================
// 第二部分：Level 1 — Fused Loop（熔合循环）
// ================================================================

// compileWhileFused 熔合 while 循环
// 将 当 cond { body } 编译为单一闭包，循环内直接调用预编译闭包
func (c *SAPCompiler) compileWhileFused(n *ast.WhileStatement) CompileFunc {
	condFn := c.Compile(n.Condition)
	if condFn == nil {
		return nil
	}

	bodyFn := c.compileBlock(n.Body)
	if bodyFn == nil {
		return nil
	}

	return func(env *object.Environment) object.Object {
		for {
			cond := condFn(env)
			if isError(cond) {
				return cond
			}
			if !isTruthy(cond) {
				break
			}
			result := bodyFn(env)
			if result != nil {
				rt := result.Type()
				if rt == object.RETURN_VALUE || rt == object.ERROR_OBJ {
					return result
				}
			}
		}
		return NULL
	}
}

// compileForFused 熔合 for 循环
// 将 对于 init; cond; update { body } 编译为单一闭包
func (c *SAPCompiler) compileForFused(n *ast.ForStatement) CompileFunc {
	var initFn CompileFunc
	if n.Init != nil {
		initFn = c.Compile(n.Init)
		if initFn == nil {
			return nil
		}
	}

	var condFn CompileFunc
	if n.Condition != nil {
		condFn = c.Compile(n.Condition)
		if condFn == nil {
			return nil
		}
	}

	var updateFn CompileFunc
	if n.Update != nil {
		updateFn = c.Compile(n.Update)
		if updateFn == nil {
			return nil
		}
	}

	bodyFn := c.compileBlock(n.Body)
	if bodyFn == nil {
		return nil
	}

	return func(env *object.Environment) object.Object {
		if initFn != nil {
			initFn(env)
		}
		for {
			if condFn != nil {
				cond := condFn(env)
				if isError(cond) {
					return cond
				}
				if !isTruthy(cond) {
					break
				}
			}
			result := bodyFn(env)
			if result != nil {
				rt := result.Type()
				if rt == object.RETURN_VALUE || rt == object.ERROR_OBJ {
					return result
				}
			}
			if updateFn != nil {
				updateFn(env)
			}
		}
		return NULL
	}
}

// ================================================================
// 第三部分：Level 3 — Native Int64 Loop（原生整数循环）
// ================================================================

// nativeOpKind 原生操作类型
type nativeOpKind int

const (
	opAdd         nativeOpKind = iota // dest = src1 + src2
	opSub                             // dest = src1 - src2
	opMul                             // dest = src1 * src2
	opAddConst                        // dest = src1 + constVal
	opSubConst                        // dest = src1 - constVal
	opMulConst                        // dest = src1 * constVal
	opAssignConst                     // dest = constVal
	opAssignVar                       // dest = src1
)

// nativeOp 描述一个原生 int64 操作
type nativeOp struct {
	kind     nativeOpKind
	destSlot int
	destName string // 用于循环后同步槽位到 map
	srcSlot1 int
	srcSlot2 int
	constVal int64
}

// tryCompileForNative 尝试编译原生 int64 循环
//
// 检测模式：i = initVal; i < condVal; i = i + stepVal { simple_int_body }
//
// 匹配时生成零函数调用的原生循环闭包，内部直接操作 int64 槽位。
// 不匹配时返回 nil，回退到 compileForFused。
func (c *SAPCompiler) tryCompileForNative(n *ast.ForStatement) CompileFunc {
	// --- 检查 Init: AssignExpression{Name: ident, Value: IntegerLiteral} ---
	init, ok := n.Init.(*ast.AssignExpression)
	if !ok || init.Name == nil {
		return nil
	}
	loopVarName := init.Name.Value
	loopSlot, ok := c.slotMap[loopVarName]
	if !ok {
		return nil
	}
	initLit, ok := init.Value.(*ast.IntegerLiteral)
	if !ok {
		return nil
	}
	initVal := initLit.Value

	// --- 检查 Condition: InfixExpression{Left: ident, Op: "<"/"<="/">"/">=", Right: IntegerLiteral} ---
	condInf, ok := n.Condition.(*ast.InfixExpression)
	if !ok {
		return nil
	}
	condLeft, ok := condInf.Left.(*ast.Identifier)
	if !ok || condLeft.Value != loopVarName {
		return nil
	}
	condRight, ok := condInf.Right.(*ast.IntegerLiteral)
	if !ok {
		return nil
	}
	condVal := condRight.Value
	condOp := condInf.Operator

	// 仅支持 < <= > >=
	switch condOp {
	case "<", "<=", ">", ">=":
	default:
		return nil
	}

	// --- 检查 Update: AssignExpression{Name: ident, Value: InfixExpression{Left: ident, Op: "+"/"-", Right: IntegerLiteral}} ---
	updateAssign, ok := n.Update.(*ast.AssignExpression)
	if !ok || updateAssign.Name == nil || updateAssign.Name.Value != loopVarName {
		return nil
	}
	updateInf, ok := updateAssign.Value.(*ast.InfixExpression)
	if !ok {
		return nil
	}
	updateLeft, ok := updateInf.Left.(*ast.Identifier)
	if !ok || updateLeft.Value != loopVarName {
		return nil
	}
	updateRight, ok := updateInf.Right.(*ast.IntegerLiteral)
	if !ok {
		return nil
	}
	stepVal := updateRight.Value
	updateOp := updateInf.Operator

	// 仅支持 + -
	switch updateOp {
	case "+", "-":
	default:
		return nil
	}

	// --- 检查 Body: 全部是简单整数运算 ---
	ops, syncVars, ok := c.compileNativeBody(n.Body)
	if !ok {
		return nil
	}

	// 循环变量也需要同步
	syncVars = append(syncVars, syncVar{name: loopVarName, slot: loopSlot})

	// --- 生成原生循环闭包 ---
	return func(env *object.Environment) object.Object {
		// Init: 设置循环变量初始值
		iVal := initVal
		env.SetSlot(loopSlot, &object.Integer{Value: iVal})

		for {
			// Condition: 直接 int64 比较，零函数调用
			shouldBreak := false
			switch condOp {
			case "<":
				shouldBreak = !(iVal < condVal)
			case "<=":
				shouldBreak = !(iVal <= condVal)
			case ">":
				shouldBreak = !(iVal > condVal)
			case ">=":
				shouldBreak = !(iVal >= condVal)
			}
			if shouldBreak {
				break
			}

			// Body: 执行原生 int64 操作
			for _, op := range ops {
				switch op.kind {
				case opAdd:
					v1obj := env.GetSlot(op.srcSlot1)
					v2obj := env.GetSlot(op.srcSlot2)
					if v1obj == nil || v2obj == nil {
						return newErrorWithNode(n, diagnostic.ErrUndefinedIdentifier.Code, "循环变量未初始化")
					}
					v1 := v1obj.(*object.Integer).Value
					v2 := v2obj.(*object.Integer).Value
					env.SetSlot(op.destSlot, &object.Integer{Value: v1 + v2})
				case opSub:
					v1obj := env.GetSlot(op.srcSlot1)
					v2obj := env.GetSlot(op.srcSlot2)
					if v1obj == nil || v2obj == nil {
						return newErrorWithNode(n, diagnostic.ErrUndefinedIdentifier.Code, "循环变量未初始化")
					}
					v1 := v1obj.(*object.Integer).Value
					v2 := v2obj.(*object.Integer).Value
					env.SetSlot(op.destSlot, &object.Integer{Value: v1 - v2})
				case opMul:
					v1obj := env.GetSlot(op.srcSlot1)
					v2obj := env.GetSlot(op.srcSlot2)
					if v1obj == nil || v2obj == nil {
						return newErrorWithNode(n, diagnostic.ErrUndefinedIdentifier.Code, "循环变量未初始化")
					}
					v1 := v1obj.(*object.Integer).Value
					v2 := v2obj.(*object.Integer).Value
					env.SetSlot(op.destSlot, &object.Integer{Value: v1 * v2})
				case opAddConst:
					v1obj := env.GetSlot(op.srcSlot1)
					if v1obj == nil {
						return newErrorWithNode(n, diagnostic.ErrUndefinedIdentifier.Code, "循环变量未初始化")
					}
					v1 := v1obj.(*object.Integer).Value
					env.SetSlot(op.destSlot, &object.Integer{Value: v1 + op.constVal})
				case opSubConst:
					v1obj := env.GetSlot(op.srcSlot1)
					if v1obj == nil {
						return newErrorWithNode(n, diagnostic.ErrUndefinedIdentifier.Code, "循环变量未初始化")
					}
					v1 := v1obj.(*object.Integer).Value
					env.SetSlot(op.destSlot, &object.Integer{Value: v1 - op.constVal})
				case opMulConst:
					v1obj := env.GetSlot(op.srcSlot1)
					if v1obj == nil {
						return newErrorWithNode(n, diagnostic.ErrUndefinedIdentifier.Code, "循环变量未初始化")
					}
					v1 := v1obj.(*object.Integer).Value
					env.SetSlot(op.destSlot, &object.Integer{Value: v1 * op.constVal})
				case opAssignConst:
					env.SetSlot(op.destSlot, &object.Integer{Value: op.constVal})
				case opAssignVar:
					srcVal := env.GetSlot(op.srcSlot1)
					if srcVal == nil {
						return newErrorWithNode(n, diagnostic.ErrUndefinedIdentifier.Code, "循环变量未初始化")
					}
					env.SetSlot(op.destSlot, srcVal)
				}
			}

			// Update: 直接 int64 运算
			switch updateOp {
			case "+":
				iVal += stepVal
			case "-":
				iVal -= stepVal
			}
			env.SetSlot(loopSlot, &object.Integer{Value: iVal})
		}

		// 循环结束后：将槽位同步回 map（保证后续 Eval 读取正确）
		for _, sv := range syncVars {
			val := env.GetSlot(sv.slot)
			if val != nil {
				env.Assign(sv.name, val)
			}
		}
		return NULL
	}
}

// ================================================================
// 第四部分：Level 4 — Smi-Tagged Native Loop（Smi 标记原生循环）
// ================================================================

// tryCompileForSmiNative 尝试编译 Smi 标记原生循环
//
// 与 Level 3 (tryCompileForNative) 相同的检测模式，但运行时使用 Smi-Tagged Value 槽位：
//   - GetValueSlot 返回 8 字节 Value（寄存器级），而非 16 字节 Object 接口
//   - 整数运算直接在 Smi 编码上操作，零堆分配
//   - 循环退出时才将 Smi 转换回 Object（使用小整数缓存）
//
// 性能优势（相比 Level 3）：
//   - 消除每次迭代的 &object.Integer{} 堆分配（1000 次循环 = 4000+ 次分配 → 0 次）
//   - 消除接口类型断言 (*object.Integer) 的开销
//   - Value 是 8 字节标量，Go 编译器可放入寄存器传递
//   - Object 接口是 16 字节（itab + data），需内存解引用
func (c *SAPCompiler) tryCompileForSmiNative(n *ast.ForStatement) CompileFunc {
	// --- 检查 Init: AssignExpression{Name: ident, Value: IntegerLiteral} ---
	init, ok := n.Init.(*ast.AssignExpression)
	if !ok || init.Name == nil {
		return nil
	}
	loopVarName := init.Name.Value
	loopSlot, ok := c.slotMap[loopVarName]
	if !ok {
		return nil
	}
	initLit, ok := init.Value.(*ast.IntegerLiteral)
	if !ok {
		return nil
	}
	initVal := initLit.Value

	// --- 检查 Condition: InfixExpression{Left: ident, Op: "<"/"<="/>"/>=", Right: IntegerLiteral} ---
	condInf, ok := n.Condition.(*ast.InfixExpression)
	if !ok {
		return nil
	}
	condLeft, ok := condInf.Left.(*ast.Identifier)
	if !ok || condLeft.Value != loopVarName {
		return nil
	}
	condRight, ok := condInf.Right.(*ast.IntegerLiteral)
	if !ok {
		return nil
	}
	condVal := condRight.Value
	condOp := condInf.Operator

	switch condOp {
	case "<", "<=", ">", ">=":
	default:
		return nil
	}

	// --- 检查 Update: AssignExpression{Name: ident, Value: InfixExpression{Left: ident, Op: "+"/"-", Right: IntegerLiteral}} ---
	updateAssign, ok := n.Update.(*ast.AssignExpression)
	if !ok || updateAssign.Name == nil || updateAssign.Name.Value != loopVarName {
		return nil
	}
	updateInf, ok := updateAssign.Value.(*ast.InfixExpression)
	if !ok {
		return nil
	}
	updateLeft, ok := updateInf.Left.(*ast.Identifier)
	if !ok || updateLeft.Value != loopVarName {
		return nil
	}
	updateRight, ok := updateInf.Right.(*ast.IntegerLiteral)
	if !ok {
		return nil
	}
	stepVal := updateRight.Value
	updateOp := updateInf.Operator

	switch updateOp {
	case "+", "-":
	default:
		return nil
	}

	// --- 检查 Body: 全部是简单整数运算 ---
	// 复用 Level 3 的 AST 分析逻辑，生成相同的 nativeOp 列表
	ops, syncVars, ok := c.compileNativeBody(n.Body)
	if !ok {
		return nil
	}

	syncVars = append(syncVars, syncVar{name: loopVarName, slot: loopSlot})

	// --- 生成 Smi 标记原生循环闭包 ---
	// 与 Level 3 的关键区别：使用 GetValueSlot/SetValueSlot（Value 槽位）替代 GetSlot/SetSlot（Object 槽位）
	return func(env *object.Environment) object.Object {
		// Init: 将循环变量编码为 Smi 写入 valueSlots
		iVal := initVal
		env.SetValueSlot(loopSlot, object.Smi(iVal))

		for {
			// Condition: 直接 int64 比较（iVal 是本地 int64 变量，在寄存器中）
			shouldBreak := false
			switch condOp {
			case "<":
				shouldBreak = !(iVal < condVal)
			case "<=":
				shouldBreak = !(iVal <= condVal)
			case ">":
				shouldBreak = !(iVal > condVal)
			case ">=":
				shouldBreak = !(iVal >= condVal)
			}
			if shouldBreak {
				break
			}

			// Body: Smi 标记运算 — 零堆分配
			// GetValueSlot 返回 Value（8字节标量），AsSmi 是一次右移（1周期）
			// SetValueSlot 接收 Smi Value，Smi() 是一次左移+OR（1周期）
			// 对比 Level 3：GetSlot → Object 接口 → 类型断言 → 提取 int64 → 计算 → 创建 Integer → SetSlot
			for _, op := range ops {
				switch op.kind {
				case opAdd:
					v1 := env.GetValueSlot(op.srcSlot1).AsSmi()
					v2 := env.GetValueSlot(op.srcSlot2).AsSmi()
					env.SetValueSlot(op.destSlot, object.Smi(v1+v2))
				case opSub:
					v1 := env.GetValueSlot(op.srcSlot1).AsSmi()
					v2 := env.GetValueSlot(op.srcSlot2).AsSmi()
					env.SetValueSlot(op.destSlot, object.Smi(v1-v2))
				case opMul:
					v1 := env.GetValueSlot(op.srcSlot1).AsSmi()
					v2 := env.GetValueSlot(op.srcSlot2).AsSmi()
					env.SetValueSlot(op.destSlot, object.Smi(v1*v2))
				case opAddConst:
					v1 := env.GetValueSlot(op.srcSlot1).AsSmi()
					env.SetValueSlot(op.destSlot, object.Smi(v1+op.constVal))
				case opSubConst:
					v1 := env.GetValueSlot(op.srcSlot1).AsSmi()
					env.SetValueSlot(op.destSlot, object.Smi(v1-op.constVal))
				case opMulConst:
					v1 := env.GetValueSlot(op.srcSlot1).AsSmi()
					env.SetValueSlot(op.destSlot, object.Smi(v1*op.constVal))
				case opAssignConst:
					env.SetValueSlot(op.destSlot, object.Smi(op.constVal))
				case opAssignVar:
					srcVal := env.GetValueSlot(op.srcSlot1)
					env.SetValueSlot(op.destSlot, srcVal) // 直接拷贝 Value，零开销
				}
			}

			// Update: 直接 int64 运算（iVal 在寄存器中）
			switch updateOp {
			case "+":
				iVal += stepVal
			case "-":
				iVal -= stepVal
			}
			env.SetValueSlot(loopSlot, object.Smi(iVal))
		}

		// 循环退出：将 Smi Value 转换回 Object 并同步到 map + Object 槽位
		// AsObject 使用小整数缓存（-512~511），常见值零分配
		for _, sv := range syncVars {
			smiVal := env.GetValueSlot(sv.slot)
			obj := smiVal.AsObject()
			env.Assign(sv.name, obj)
			if env.HasSlots() {
				env.SetSlot(sv.slot, obj)
			}
		}
		return NULL
	}
}

type syncVar struct {
	name string
	slot int
}

// compileNativeBody 将循环体编译为原生 int64 操作列表
// 所有语句必须是 ExpressionStatement<AssignExpression> 且值为简单整数运算
func (c *SAPCompiler) compileNativeBody(block *ast.BlockStatement) ([]nativeOp, []syncVar, bool) {
	if block == nil || len(block.Statements) == 0 {
		return nil, nil, false
	}
	var ops []nativeOp
	var syncVars []syncVar
	for _, stmt := range block.Statements {
		exprStmt, ok := stmt.(*ast.ExpressionStatement)
		if !ok {
			return nil, nil, false
		}
		assign, ok := exprStmt.Expression.(*ast.AssignExpression)
		if !ok || assign.Name == nil {
			return nil, nil, false
		}
		destSlot, ok := c.slotMap[assign.Name.Value]
		if !ok {
			return nil, nil, false
		}

		op, ok := c.compileNativeExpr(assign.Value, destSlot, assign.Name.Value)
		if !ok {
			return nil, nil, false
		}
		ops = append(ops, op)
		syncVars = append(syncVars, syncVar{name: assign.Name.Value, slot: destSlot})
	}
	return ops, syncVars, true
}

// compileNativeExpr 将表达式编译为单个原生 int64 操作
func (c *SAPCompiler) compileNativeExpr(expr ast.Expression, destSlot int, destName string) (nativeOp, bool) {
	switch e := expr.(type) {
	// 常量赋值: a = 42
	case *ast.IntegerLiteral:
		return nativeOp{kind: opAssignConst, destSlot: destSlot, destName: destName, constVal: e.Value}, true

	// 变量赋值: a = b
	case *ast.Identifier:
		slot, ok := c.slotMap[e.Value]
		if !ok {
			return nativeOp{}, false
		}
		return nativeOp{kind: opAssignVar, destSlot: destSlot, destName: destName, srcSlot1: slot}, true

	// 二元运算: a = b + c, a = b + 1, etc.
	case *ast.InfixExpression:
		// 类型检查：左右都必须是整数
		leftType := c.inf.GetNodeType(e.Left)
		rightType := c.inf.GetNodeType(e.Right)
		if !isIntType(leftType) || !isIntType(rightType) {
			return nativeOp{}, false
		}

		// 确定操作类型
		var kind nativeOpKind
		switch e.Operator {
		case "+":
			kind = opAdd
		case "-":
			kind = opSub
		case "*":
			kind = opMul
		default:
			return nativeOp{}, false // 不支持 / %（除零检查无法在编译期消除）
		}

		// 左操作数必须是标识符（变量）
		leftIdent, ok := e.Left.(*ast.Identifier)
		if !ok {
			return nativeOp{}, false
		}
		leftSlot, ok := c.slotMap[leftIdent.Value]
		if !ok {
			return nativeOp{}, false
		}

		// 右操作数：标识符（变量）或整数字面量（常量）
		if rightIdent, ok := e.Right.(*ast.Identifier); ok {
			rightSlot, ok := c.slotMap[rightIdent.Value]
			if !ok {
				return nativeOp{}, false
			}
			return nativeOp{kind: kind, destSlot: destSlot, destName: destName, srcSlot1: leftSlot, srcSlot2: rightSlot}, true
		}
		if rightLit, ok := e.Right.(*ast.IntegerLiteral); ok {
			// 转换为常量变体，减少一次 GetSlot + 类型断言
			switch kind {
			case opAdd:
				kind = opAddConst
			case opSub:
				kind = opSubConst
			case opMul:
				kind = opMulConst
			}
			return nativeOp{kind: kind, destSlot: destSlot, destName: destName, srcSlot1: leftSlot, constVal: rightLit.Value}, true
		}
	}
	return nativeOp{}, false
}
