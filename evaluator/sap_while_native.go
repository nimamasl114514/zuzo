package evaluator

// sap_while_native.go — While 原生循环（补齐 for 已有的 Level 4/5 两级）
//
// 背景（第6轮实测）：for 循环已有 De Bruijn（Level 5）与 Smi 槽位（Level 4）
// 两级原生路径，而 while 只有 compileWhileFused（Level 1-2）——
// 每轮迭代仍要 Environment.Assign（map 写入）+ 堆分配 Integer。
// 真实进程 pprof（1e7 次 while 累加）显示：Environment.Assign 44%、
// mallocgc 14%、mapaccess 28%，循环体本身几乎没有算术开销。
//
// 本文件为 while 补齐等价原生路径，只匹配下述窄模式（不匹配一律 nil 回退，
// 保证语义与 Tree-Walk 完全一致）：
//
//	变量 i = 0
//	变量 总 = 0
//	当 i < 1000000 {      // 条件：i 与整数字面量比较
//	    总 = 总 + i        // 其余语句：可编译为原生 int64 op 的整数赋值
//	    i = i + 1          // 最后一条：i = i ± 常量（步长）
//	}
//
// 安全边界：body 含 跳出/继续/函数调用/字符串/浮点/索引赋值 → 不匹配；
// 循环变量自增语句不在最后一条 → 不匹配（避免语义错位）。

import (
	"zuzo/ast"
	"zuzo/object"
)

// whileNativePattern 描述一个可原生化的 while 循环
type whileNativePattern struct {
	loopVarName string
	loopSlot    int
	condOp      string
	condVal     int64
	updateOp    string
	stepVal     int64
	rest        *ast.BlockStatement // 剥离 update 后的循环体（可能为 nil = 空体）
}

// matchWhileNative 匹配 while 原生循环模式
func (c *SAPCompiler) matchWhileNative(n *ast.WhileStatement) (*whileNativePattern, bool) {
	if n == nil || n.Condition == nil || n.Body == nil {
		return nil, false
	}
	if len(n.Body.Statements) == 0 {
		return nil, false
	}

	// --- 条件：标识符 OP 整数字面量 ---
	condInf, ok := n.Condition.(*ast.InfixExpression)
	if !ok {
		return nil, false
	}
	condLeft, ok := condInf.Left.(*ast.Identifier)
	if !ok {
		return nil, false
	}
	condRight, ok := condInf.Right.(*ast.IntegerLiteral)
	if !ok {
		return nil, false
	}
	switch condInf.Operator {
	case "<", "<=", ">", ">=":
	default:
		return nil, false
	}

	loopVarName := condLeft.Value
	loopSlot, ok := c.slotMap[loopVarName]
	if !ok {
		return nil, false
	}

	// --- 最后一条语句：i = i ± 常量 ---
	last := n.Body.Statements[len(n.Body.Statements)-1]
	lastExpr, ok := last.(*ast.ExpressionStatement)
	if !ok || lastExpr.Expression == nil {
		return nil, false
	}
	updateAssign, ok := lastExpr.Expression.(*ast.AssignExpression)
	if !ok || updateAssign.Name == nil || updateAssign.Name.Value != loopVarName {
		return nil, false
	}
	updateInf, ok := updateAssign.Value.(*ast.InfixExpression)
	if !ok {
		return nil, false
	}
	updateLeft, ok := updateInf.Left.(*ast.Identifier)
	if !ok || updateLeft.Value != loopVarName {
		return nil, false
	}
	updateRight, ok := updateInf.Right.(*ast.IntegerLiteral)
	if !ok {
		return nil, false
	}
	switch updateInf.Operator {
	case "+", "-":
	default:
		return nil, false
	}

	// --- 剥离 update，剩余语句交给原生 body 编译器 ---
	var rest *ast.BlockStatement
	if len(n.Body.Statements) > 1 {
		rest = &ast.BlockStatement{Statements: n.Body.Statements[:len(n.Body.Statements)-1]}
	}

	return &whileNativePattern{
		loopVarName: loopVarName,
		loopSlot:    loopSlot,
		condOp:      condInf.Operator,
		condVal:     condRight.Value,
		updateOp:    updateInf.Operator,
		stepVal:     updateRight.Value,
		rest:        rest,
	}, true
}

// whileCondBreaks 判断当前 iVal 是否应退出循环（与运算符语义一致）
//
//go:inline
func whileCondBreaks(op string, iVal, bound int64) bool {
	switch op {
	case "<":
		return !(iVal < bound)
	case "<=":
		return !(iVal <= bound)
	case ">":
		return !(iVal > bound)
	case ">=":
		return !(iVal >= bound)
	}
	return true
}

// compileWhileNativeBody 编译剥离 update 后的循环体（空体视为合法）
func (c *SAPCompiler) compileWhileNativeBody(rest *ast.BlockStatement) ([]nativeOp, []syncVar, bool) {
	if rest == nil || len(rest.Statements) == 0 {
		return nil, nil, true
	}
	return c.compileNativeBody(rest)
}

// compileWhileDeBruijnBody 编译剥离 update 后的循环体（De Bruijn 版，空体合法）
func (c *SAPCompiler) compileWhileDeBruijnBody(rest *ast.BlockStatement) ([]dbOp, []syncVar, bool) {
	if rest == nil || len(rest.Statements) == 0 {
		return nil, nil, true
	}
	return c.compileDeBruijnBody(rest, c.debruijnMap)
}

// tryCompileWhileSmiNative 编译 while 为 Smi 槽位原生循环（Level 4）
func (c *SAPCompiler) tryCompileWhileSmiNative(n *ast.WhileStatement) CompileFunc {
	p, ok := c.matchWhileNative(n)
	if !ok {
		return nil
	}
	ops, syncVars, ok := c.compileWhileNativeBody(p.rest)
	if !ok {
		return nil
	}
	syncVars = append(syncVars, syncVar{name: p.loopVarName, slot: p.loopSlot})

	return func(env *object.Environment) object.Object {
		if !env.HasValueSlots() {
			return compileWhileFusedFallback(env, n, c)
		}

		// 初值：取循环变量当前值（由前面的 let 写入 Object 槽位）
		var iVal int64
		if obj := env.GetSlot(p.loopSlot); obj != nil {
			if iv, ok := obj.(*object.Integer); ok {
				iVal = iv.Value
			}
		}
		env.SetValueSlot(p.loopSlot, object.Smi(iVal))

		for {
			if whileCondBreaks(p.condOp, iVal, p.condVal) {
				break
			}
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
					env.SetValueSlot(op.destSlot, env.GetValueSlot(op.srcSlot1))
				}
			}
			if p.updateOp == "+" {
				iVal += p.stepVal
			} else {
				iVal -= p.stepVal
			}
			env.SetValueSlot(p.loopSlot, object.Smi(iVal))
		}

		// 循环退出：Smi → Object 同步回 map + Object 槽位
		for _, sv := range syncVars {
			obj := env.GetValueSlot(sv.slot).AsObject()
			env.Assign(sv.name, obj)
			if env.HasSlots() {
				env.SetSlot(sv.slot, obj)
			}
		}
		return NULL
	}
}

// tryCompileWhileDeBruijnNative 编译 while 为 De Bruijn 原生循环（Level 5）
// Context 不可用时回退 Smi 版；Smi 版也编译不出来时整体返回 nil（走融合循环）。
func (c *SAPCompiler) tryCompileWhileDeBruijnNative(n *ast.WhileStatement) CompileFunc {
	p, ok := c.matchWhileNative(n)
	if !ok {
		return nil
	}
	dbOps, syncVars, ok := c.compileWhileDeBruijnBody(p.rest)
	if !ok {
		return nil
	}
	// 只接受根作用域（Depth=0）的访问：Depth>0 说明循环位于嵌套作用域，
	// 本文件未建对应帧，直接放弃原生路径交由融合循环处理。
	for _, op := range dbOps {
		if op.destIdx.Depth != 0 || op.srcIdx1.Depth != 0 || op.srcIdx2.Depth != 0 {
			return nil
		}
	}
	syncVars = append(syncVars, syncVar{name: p.loopVarName, slot: p.loopSlot})

	fallbackFn := c.tryCompileWhileSmiNative(n)
	if fallbackFn == nil {
		return nil
	}

	return func(env *object.Environment) object.Object {
		ctx := env.GetContext()
		if ctx == nil {
			return fallbackFn(env)
		}

		// Environment → Context 同步：把 let 已写入的初值搬进扁平帧
		if env.HasValueSlots() {
			envSlots := env.GetValueSlots()
			ctxSlots := ctx.CurrentFrame()
			n := len(ctxSlots)
			if len(envSlots) < n {
				n = len(envSlots)
			}
			copy(ctxSlots, envSlots)
		}

		iVal := ctx.GetSmi(0, p.loopSlot)
		ctx.SetSmi(0, p.loopSlot, iVal)

		for {
			if whileCondBreaks(p.condOp, iVal, p.condVal) {
				break
			}
			for _, op := range dbOps {
				switch op.kind {
				case opAdd:
					v1 := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot).AsSmi()
					v2 := ctx.Get(op.srcIdx2.Depth, op.srcIdx2.Slot).AsSmi()
					ctx.SetSmi(op.destIdx.Depth, op.destIdx.Slot, v1+v2)
				case opSub:
					v1 := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot).AsSmi()
					v2 := ctx.Get(op.srcIdx2.Depth, op.srcIdx2.Slot).AsSmi()
					ctx.SetSmi(op.destIdx.Depth, op.destIdx.Slot, v1-v2)
				case opMul:
					v1 := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot).AsSmi()
					v2 := ctx.Get(op.srcIdx2.Depth, op.srcIdx2.Slot).AsSmi()
					ctx.SetSmi(op.destIdx.Depth, op.destIdx.Slot, v1*v2)
				case opAddConst:
					v1 := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot).AsSmi()
					ctx.SetSmi(op.destIdx.Depth, op.destIdx.Slot, v1+op.constVal)
				case opSubConst:
					v1 := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot).AsSmi()
					ctx.SetSmi(op.destIdx.Depth, op.destIdx.Slot, v1-op.constVal)
				case opMulConst:
					v1 := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot).AsSmi()
					ctx.SetSmi(op.destIdx.Depth, op.destIdx.Slot, v1*op.constVal)
				case opAssignConst:
					ctx.SetSmi(op.destIdx.Depth, op.destIdx.Slot, op.constVal)
				case opAssignVar:
					ctx.Set(op.destIdx.Depth, op.destIdx.Slot, ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot))
				}
			}
			if p.updateOp == "+" {
				iVal += p.stepVal
			} else {
				iVal -= p.stepVal
			}
			ctx.SetSmi(0, p.loopSlot, iVal)
		}

		// 循环退出：Context → Environment 同步（供后续非原生路径使用）
		for _, sv := range syncVars {
			obj := ctx.GetObject(0, sv.slot)
			env.Assign(sv.name, obj)
			if env.HasSlots() {
				env.SetSlot(sv.slot, obj)
			}
		}
		return NULL
	}
}

// compileWhileFusedFallback 运行时兜底：环境没有 Smi 槽位时回到融合循环
// （只在异常构造的 Environment 上触发；正常 SAP 路径必有 valueSlots）
func compileWhileFusedFallback(env *object.Environment, n *ast.WhileStatement, c *SAPCompiler) object.Object {
	fn := c.compileWhileFused(n)
	if fn == nil {
		return NULL
	}
	return fn(env)
}
