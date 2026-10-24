package evaluator

// sap_debruijn.go — Level 5: De Bruijn Index + 扁平帧栈原生循环
//
// 在 Level 4 (Smi-Tagged Native Loop) 基础上：
//   - 抛弃 Environment，使用 Context（连续帧栈数组）
//   - 变量访问从 env.GetValueSlot(slot) 改为 ctx.Get(depth, slot)
//   - 消除 syncSlot 开销：Context 是唯一存储，无 map+slot 双写
//   - CPU 缓存友好：连续 []Value 内存布局，预取器完美工作
//
// De Bruijn 索引解析：
//   编译期遍历 AST，为每个标识符分配 (Depth, Slot) 二维坐标。
//   当前作用域 Depth=0，外层 Depth=1，依此类推。
//   运行时通过 c.frames[c.sp-depth].slots[slot] 一次数组索引定位。

import (
	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/object"
)

// ================================================================
// 溢出安全边界（第 10 轮补）
// ================================================================
//
// De Bruijn 原生循环全程用 int64 裸运算 + Smi 编码，而 Smi 只有 63 位有符号
// （n<<1|1，有效区间 [-2^62, 2^62-1]）。超出这个区间时 Smi 编码会静默回绕
// （例如 2^62 → -2^62），与普通 *object.Integer 路径结果不一致。
//
// 两级处理，绝不静默回绕：
//  1. 结果超出 Smi 区间但仍在 int64 内：撤出原生路径，把整条 for 语句交回
//     普通整数对象路径（evalForStatement，*object.Integer 保存 int64）重跑；
//  2. 结果连 int64 都装不下：抛 E210 整数溢出错误（对象路也装不下，不静默回绕）。

const (
	dbSmiMin = -(int64(1) << 62)
	dbSmiMax = int64(1)<<62 - 1
)

// dbSmiFits 判断 int64 是否能无损编码为 Smi
//
//go:inline
func dbSmiFits(v int64) bool { return v >= dbSmiMin && v <= dbSmiMax }

// dbAddOverflow 返回 a+b 是否溢出 int64（无分支：0=未溢出，1=溢出）
func dbAddOverflow(a, b int64) uint64 {
	r := a + b
	return uint64((a^r)&(b^r)) >> 63
}

// dbSubOverflow 返回 a-b 是否溢出 int64（无分支：0=未溢出，1=溢出）
func dbSubOverflow(a, b int64) uint64 {
	r := a - b
	return uint64((a^b)&(a^r)) >> 63
}

// dbMulOverflow 返回 a*b 是否溢出 int64（0=未溢出，1=溢出）
func dbMulOverflow(a, b int64) uint64 {
	if a == 0 || b == 0 {
		return 0
	}
	if (a == -1 && b == -1<<63) || (b == -1 && a == -1<<63) {
		return 1
	}
	if a*b/b != a {
		return 1
	}
	return 0
}

// ================================================================
// 第一部分：De Bruijn 索引解析器
// ================================================================

// DeBruijnResolver 在编译期为每个标识符分配 (Depth, Slot) 坐标
type DeBruijnResolver struct {
	// scopeStack 作用域栈，每层是 map[变量名]槽位索引
	scopeStack []map[string]int
	// result 标识符 → De Bruijn 索引
	result map[*ast.Identifier]object.DeBruijnIndex
	// slotMap inferencer 的扁平槽位映射（复用）
	flatSlotMap map[string]int
}

// NewDeBruijnResolver 创建解析器
func NewDeBruijnResolver(flatSlotMap map[string]int) *DeBruijnResolver {
	return &DeBruijnResolver{
		scopeStack:  []map[string]int{{}}, // 根作用域
		result:      make(map[*ast.Identifier]object.DeBruijnIndex),
		flatSlotMap: flatSlotMap,
	}
}

// Resolve 解析整个程序，生成 De Bruijn 索引映射
func (r *DeBruijnResolver) Resolve(program *ast.Program) map[*ast.Identifier]object.DeBruijnIndex {
	for _, stmt := range program.Statements {
		r.resolveStmt(stmt)
	}
	return r.result
}

// pushScope / popScope 管理作用域栈
func (r *DeBruijnResolver) pushScope() {
	r.scopeStack = append(r.scopeStack, make(map[string]int))
}

func (r *DeBruijnResolver) popScope() {
	if len(r.scopeStack) > 1 {
		r.scopeStack = r.scopeStack[:len(r.scopeStack)-1]
	}
}

// currentScope 返回当前作用域
func (r *DeBruijnResolver) currentScope() map[string]int {
	return r.scopeStack[len(r.scopeStack)-1]
}

// resolveIdentifier 将标识符解析为 (Depth, Slot)
// 从当前作用域向外搜索，记录深度
func (r *DeBruijnResolver) resolveIdentifier(ident *ast.Identifier) {
	name := ident.Value
	// 从栈顶向下搜索
	for depth := len(r.scopeStack) - 1; depth >= 0; depth-- {
		if slot, ok := r.scopeStack[depth][name]; ok {
			r.result[ident] = object.DeBruijnIndex{Depth: len(r.scopeStack) - 1 - depth, Slot: slot}
			return
		}
	}
	// 未找到，使用扁平 slotMap 作为 depth=0 的后备
	if slot, ok := r.flatSlotMap[name]; ok {
		r.result[ident] = object.DeBruijnIndex{Depth: 0, Slot: slot}
	}
}

// registerVar 在当前作用域注册变量
func (r *DeBruijnResolver) registerVar(name string) {
	if slot, ok := r.flatSlotMap[name]; ok {
		r.currentScope()[name] = slot
	}
}

func (r *DeBruijnResolver) resolveStmt(stmt ast.Statement) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		if s.Expression != nil {
			r.resolveExpr(s.Expression)
		}
	case *ast.LetStatement:
		if s.Value != nil {
			r.resolveExpr(s.Value)
		}
		if s.Name != nil {
			r.registerVar(s.Name.Value)
		}
	case *ast.ReturnStatement:
		if s.ReturnValue != nil {
			r.resolveExpr(s.ReturnValue)
		}
	case *ast.WhileStatement:
		if s.Condition != nil {
			r.resolveExpr(s.Condition)
		}
		if s.Body != nil {
			// While 循环体不创建新作用域——与 Evaluator 行为一致
			for _, stmt := range s.Body.Statements {
				r.resolveStmt(stmt)
			}
		}
	case *ast.ForStatement:
		if s.Init != nil {
			r.resolveExpr(s.Init)
		}
		if s.Condition != nil {
			r.resolveExpr(s.Condition)
		}
		if s.Update != nil {
			r.resolveExpr(s.Update)
		}
		if s.Body != nil {
			// For 循环体不创建新作用域——与 Evaluator 行为一致
			for _, stmt := range s.Body.Statements {
				r.resolveStmt(stmt)
			}
		}
	case *ast.BlockStatement:
		r.resolveBlock(s)
	}
}

func (r *DeBruijnResolver) resolveBlock(block *ast.BlockStatement) {
	if block == nil {
		return
	}
	r.pushScope()
	defer r.popScope()
	for _, stmt := range block.Statements {
		r.resolveStmt(stmt)
	}
}

func (r *DeBruijnResolver) resolveExpr(expr ast.Expression) {
	if expr == nil {
		return
	}
	switch e := expr.(type) {
	case *ast.Identifier:
		r.resolveIdentifier(e)
	case *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.BooleanLiteral, *ast.NullLiteral:
		// 字面量无需解析
	case *ast.PrefixExpression:
		r.resolveExpr(e.Right)
	case *ast.InfixExpression:
		r.resolveExpr(e.Left)
		r.resolveExpr(e.Right)
	case *ast.AssignExpression:
		if e.Name != nil {
			r.registerVar(e.Name.Value)
		}
		r.resolveExpr(e.Value)
	case *ast.IfExpression:
		// IfExpression 的 Consequence/Alternative 在 Evaluator 中
		// 不创建新作用域（直接在当前 env 上 Eval），所以这里也不 push scope
		r.resolveExpr(e.Condition)
		if e.Consequence != nil {
			for _, stmt := range e.Consequence.Statements {
				r.resolveStmt(stmt)
			}
		}
		if e.Alternative != nil {
			for _, stmt := range e.Alternative.Statements {
				r.resolveStmt(stmt)
			}
		}
	case *ast.FStringLiteral:
		for _, part := range e.Parts {
			if _, ok := part.(*ast.StringLiteral); !ok {
				r.resolveExpr(part)
			}
		}
	}
}

// ================================================================
// 第二部分：De Bruijn 原生操作（扩展 nativeOp）
// ================================================================

// dbOp De Bruijn 原生操作——用 (Depth, Slot) 替代扁平 slot
type dbOp struct {
	kind     nativeOpKind
	destIdx  object.DeBruijnIndex
	destName string // 循环后同步到 Environment
	srcIdx1  object.DeBruijnIndex
	srcIdx2  object.DeBruijnIndex
	constVal int64
}

// ================================================================
// 第三部分：Level 5 编译器 — De Bruijn Native Loop
// ================================================================

// tryCompileForDeBruijnNative 尝试编译 De Bruijn 原生循环（Level 5）
//
// 与 Level 4 相同的 AST 模式匹配，但运行时使用 Context（扁平帧栈）：
//   - ctx.Get(depth, slot) 替代 env.GetValueSlot(slot)
//   - ctx.Set(depth, slot, val) 替代 env.SetValueSlot(slot, val)
//   - 无需 syncSlot：Context 是唯一存储
//   - 循环退出时一次性同步回 Environment（供后续非 SAP 路径使用）
//
// 返回闭包签名仍为 CompileFunc（接收 *Environment），
// 闭包内部通过 env.Context() 获取 Context。
func (c *SAPCompiler) tryCompileForDeBruijnNative(n *ast.ForStatement) CompileFunc {
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

	// --- 检查 Condition ---
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

	// --- 检查 Update ---
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

	// --- 检查 Body: 编译为 De Bruijn 原生操作 ---
	dbOps, dbSyncVars, ok := c.compileDeBruijnBody(n.Body, c.debruijnMap)
	if !ok {
		return nil
	}

	// 循环变量也需要同步
	dbSyncVars = append(dbSyncVars, syncVar{name: loopVarName, slot: loopSlot})

	// 同时编译 Level 4 闭包作为后备（当 Context 不可用时使用）
	fallbackFn := c.tryCompileForSmiNative(n)

	// --- 生成 De Bruijn 原生循环闭包 ---
	return func(env *object.Environment) object.Object {
		ctx := env.GetContext()
		if ctx == nil {
			// Context 未初始化，回退到 Level 4
			if fallbackFn != nil {
				return fallbackFn(env)
			}
			return NULL
		}

		// 同步 Environment → Context：将循环前已初始化的变量值拷贝到 Context
		// 这确保 Let 语句设置的初始值（如 sum=0, i=0）在 Context 中可用
		if env.HasValueSlots() {
			envSlots := env.GetValueSlots()
			ctxSlots := ctx.CurrentFrame()
			n := len(ctxSlots)
			if len(envSlots) < n {
				n = len(envSlots)
			}
			copy(ctxSlots, envSlots)
		}

		// 边界/步长常量本身超出 Smi 区间：不入原生路径，直接交回普通整数对象路径
		if !dbSmiFits(initVal) || !dbSmiFits(condVal) || !dbSmiFits(stepVal) {
			return evalForStatement(n, env)
		}

		// Init: 循环变量编码为 Smi 写入 Context
		iVal := initVal
		ctx.SetSmi(0, loopSlot, iVal)

		escape := false          // Smi 装不下，回退普通整数对象路径
		var errObj object.Object // int64 也装不下时的显式错误

	loop:
		for {
			// Condition: 直接 int64 比较（iVal 在寄存器中）
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

			// Body: De Bruijn Smi 运算 — 零堆分配，零接口开销
			// ctx.Get 返回 Value（8字节标量），AsSmi 是一次右移
			// ctx.Set 接收 Smi Value，Smi() 是一次左移+OR
			// 对比 Level 4: env.GetValueSlot → 指针解引用 → 数组索引
			// Level 5: ctx.Get → 数组索引 → 数组索引（连续内存，无指针解引用）
			for _, op := range dbOps {
				// 先算出 int64 结果并累计溢出标记，再做 Smi 区间判定：
				// 溢出 → 显式 E210 错误；超 Smi 区间 → 撤出原生路径
				var res int64
				var ovf uint64
				switch op.kind {
				case opAdd, opAddConst:
					v1 := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot).AsSmi()
					if op.kind == opAdd {
						v2 := ctx.Get(op.srcIdx2.Depth, op.srcIdx2.Slot).AsSmi()
						res = v1 + v2
						ovf = dbAddOverflow(v1, v2)
					} else {
						res = v1 + op.constVal
						ovf = dbAddOverflow(v1, op.constVal)
					}
				case opSub, opSubConst:
					v1 := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot).AsSmi()
					if op.kind == opSub {
						v2 := ctx.Get(op.srcIdx2.Depth, op.srcIdx2.Slot).AsSmi()
						res = v1 - v2
						ovf = dbSubOverflow(v1, v2)
					} else {
						res = v1 - op.constVal
						ovf = dbSubOverflow(v1, op.constVal)
					}
				case opMul, opMulConst:
					v1 := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot).AsSmi()
					if op.kind == opMul {
						v2 := ctx.Get(op.srcIdx2.Depth, op.srcIdx2.Slot).AsSmi()
						res = v1 * v2
						ovf = dbMulOverflow(v1, v2)
					} else {
						res = v1 * op.constVal
						ovf = dbMulOverflow(v1, op.constVal)
					}
				case opAssignConst:
					res = op.constVal
				case opAssignVar:
					// 直接拷贝 Value：源值本来就已落在 Smi 区间内
					srcVal := ctx.Get(op.srcIdx1.Depth, op.srcIdx1.Slot)
					ctx.Set(op.destIdx.Depth, op.destIdx.Slot, srcVal)
					continue
				}
				if ovf != 0 {
					errObj = newErrorWithNode(n, diagnostic.ErrIntegerOverflow.Code,
						"循环体整数运算溢出：结果超出 int64 表示范围")
					break loop
				}
				if !dbSmiFits(res) {
					escape = true
					break loop
				}
				ctx.SetSmi(op.destIdx.Depth, op.destIdx.Slot, res)
			}

			// Update: 直接 int64 运算（iVal 在寄存器中）
			// 计数器是条件的唯一输入，必须精确；一旦越界立即撤离原生路径，避免死循环
			switch updateOp {
			case "+":
				if dbAddOverflow(iVal, stepVal) != 0 {
					errObj = newErrorWithNode(n, diagnostic.ErrIntegerOverflow.Code,
						"循环计数器溢出：结果超出 int64 表示范围")
					break loop
				}
				iVal += stepVal
			case "-":
				if dbSubOverflow(iVal, stepVal) != 0 {
					errObj = newErrorWithNode(n, diagnostic.ErrIntegerOverflow.Code,
						"循环计数器溢出：结果超出 int64 表示范围")
					break loop
				}
				iVal -= stepVal
			}
			if !dbSmiFits(iVal) {
				escape = true
				break loop
			}
			ctx.SetSmi(0, loopSlot, iVal)
		}

		if errObj != nil {
			return errObj
		}
		if escape {
			// 回退普通整数对象路径：原生路径只写 Context，Environment 仍是循环前状态。
			// 先把 Context 帧恢复成入口快照（valueSlots 在原生路径期间未被改写），
			// 再把整条 for 语句交给 evalForStatement 用 *object.Integer 重跑。
			if envSlots := env.GetValueSlots(); len(envSlots) > 0 {
				copy(ctx.CurrentFrame(), envSlots)
			}
			return evalForStatement(n, env)
		}

		// 循环退出：将 Context 值同步回 Environment（供后续非 SAP 路径使用）
		for _, sv := range dbSyncVars {
			obj := ctx.GetObject(0, sv.slot)
			env.Assign(sv.name, obj)
			if env.HasSlots() {
				env.SetSlot(sv.slot, obj)
			}
		}
		return NULL
	}
}

// compileDeBruijnBody 将循环体编译为 De Bruijn 原生操作列表
func (c *SAPCompiler) compileDeBruijnBody(block *ast.BlockStatement, dbMap map[*ast.Identifier]object.DeBruijnIndex) ([]dbOp, []syncVar, bool) {
	if block == nil || len(block.Statements) == 0 {
		return nil, nil, false
	}
	var ops []dbOp
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
		destIdx := object.DeBruijnIndex{Depth: 0, Slot: destSlot}

		op, ok := c.compileDeBruijnExpr(assign.Value, destIdx, assign.Name.Value, dbMap)
		if !ok {
			return nil, nil, false
		}
		ops = append(ops, op)
		syncVars = append(syncVars, syncVar{name: assign.Name.Value, slot: destSlot})
	}
	return ops, syncVars, true
}

// compileDeBruijnExpr 将表达式编译为单个 De Bruijn 原生操作
func (c *SAPCompiler) compileDeBruijnExpr(expr ast.Expression, destIdx object.DeBruijnIndex, destName string, dbMap map[*ast.Identifier]object.DeBruijnIndex) (dbOp, bool) {
	switch e := expr.(type) {
	// 常量赋值: a = 42
	case *ast.IntegerLiteral:
		return dbOp{kind: opAssignConst, destIdx: destIdx, destName: destName, constVal: e.Value}, true

	// 变量赋值: a = b
	case *ast.Identifier:
		idx, ok := dbMap[e]
		if !ok {
			// 回退到扁平 slotMap
			slot, ok := c.slotMap[e.Value]
			if !ok {
				return dbOp{}, false
			}
			idx = object.DeBruijnIndex{Depth: 0, Slot: slot}
		}
		return dbOp{kind: opAssignVar, destIdx: destIdx, destName: destName, srcIdx1: idx}, true

	// 二元运算: a = b + c, a = b + 1
	case *ast.InfixExpression:
		leftType := c.inf.GetNodeType(e.Left)
		rightType := c.inf.GetNodeType(e.Right)
		if !isIntType(leftType) || !isIntType(rightType) {
			return dbOp{}, false
		}

		var kind nativeOpKind
		switch e.Operator {
		case "+":
			kind = opAdd
		case "-":
			kind = opSub
		case "*":
			kind = opMul
		default:
			return dbOp{}, false
		}

		// 左操作数必须是标识符
		leftIdent, ok := e.Left.(*ast.Identifier)
		if !ok {
			return dbOp{}, false
		}
		leftIdx, ok := dbMap[leftIdent]
		if !ok {
			slot, ok := c.slotMap[leftIdent.Value]
			if !ok {
				return dbOp{}, false
			}
			leftIdx = object.DeBruijnIndex{Depth: 0, Slot: slot}
		}

		// 右操作数：标识符或整数字面量
		if rightIdent, ok := e.Right.(*ast.Identifier); ok {
			rightIdx, ok := dbMap[rightIdent]
			if !ok {
				slot, ok := c.slotMap[rightIdent.Value]
				if !ok {
					return dbOp{}, false
				}
				rightIdx = object.DeBruijnIndex{Depth: 0, Slot: slot}
			}
			return dbOp{kind: kind, destIdx: destIdx, destName: destName, srcIdx1: leftIdx, srcIdx2: rightIdx}, true
		}
		if rightLit, ok := e.Right.(*ast.IntegerLiteral); ok {
			switch kind {
			case opAdd:
				kind = opAddConst
			case opSub:
				kind = opSubConst
			case opMul:
				kind = opMulConst
			}
			return dbOp{kind: kind, destIdx: destIdx, destName: destName, srcIdx1: leftIdx, constVal: rightLit.Value}, true
		}
	}
	return dbOp{}, false
}
