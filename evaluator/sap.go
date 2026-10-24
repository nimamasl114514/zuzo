package evaluator

// sap.go — Static AST Pipelining (SAP) 静态 AST 流水线化
//
// 核心思想：利用 infer 包已知的类型信息，将 AST 节点编译成
// "类型硬化闭包"（Type-Hardened Closure），运行时零类型判断。
//
// 例：infer 知道 a + b 是整数加法，就生成：
//   func(env) Object { return &Integer{a_val + b_val} }
// 完全跳过 Eval 的 type switch 和 env.Get 的 map 查找。

import (
	"math"

	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/infer"
	"zuzo/object"
)

// CompileFunc 编译后的闭包类型：接受环境，直接返回结果
type CompileFunc func(*object.Environment) object.Object

// SAPCache 编译缓存：AST 节点 → 编译闭包
type SAPCache map[ast.Node]CompileFunc

// SAPCompiler 静态 AST 流水线编译器
type SAPCompiler struct {
	cache       SAPCache
	inf         *infer.Inferencer
	slotMap     map[string]int
	debruijnMap map[*ast.Identifier]object.DeBruijnIndex // De Bruijn 索引映射
	Safety      *SafetyReport                            // 编译期安全检查报告（CompileProgram 阶段生成）
}

// NewSAPCompiler 创建 SAP 编译器
func NewSAPCompiler(inf *infer.Inferencer) *SAPCompiler {
	return &SAPCompiler{
		cache:       make(SAPCache),
		inf:         inf,
		slotMap:     inf.GetSlotMap(),
		debruijnMap: make(map[*ast.Identifier]object.DeBruijnIndex),
	}
}

// Compile 编译节点为闭包，返回 nil 表示无法编译（回退到传统 Eval）
func (c *SAPCompiler) Compile(node ast.Node) CompileFunc {
	// 检查缓存
	if fn, ok := c.cache[node]; ok {
		return fn
	}

	// === 编译期安全检查：被判 Rejected 的节点一票否决，禁止特化 ===
	if c.Safety != nil && c.Safety.Rejected(node) {
		return nil
	}

	// 按节点类型编译
	var fn CompileFunc
	switch n := node.(type) {
	case *ast.IntegerLiteral:
		fn = c.compileInteger(n)
	case *ast.FloatLiteral:
		fn = c.compileFloat(n)
	case *ast.StringLiteral:
		fn = c.compileString(n)
	case *ast.FStringLiteral:
		fn = c.compileFString(n)
	case *ast.NullLiteral:
		fn = c.compileNull(n)
	case *ast.Identifier:
		fn = c.compileIdentifier(n)
	case *ast.InfixExpression:
		fn = c.compileInfix(n)
	case *ast.BooleanLiteral:
		fn = c.compileBoolean(n)
	// === 循环熔合扩展 ===
	case *ast.LetStatement:
		fn = c.compileLet(n)
	case *ast.ReturnStatement:
		fn = c.compileReturn(n)
	case *ast.AssignExpression:
		fn = c.compileAssign(n)
	case *ast.ExpressionStatement:
		fn = c.compileExprStmt(n)
	case *ast.PrefixExpression:
		fn = c.compilePrefix(n)
	case *ast.BlockStatement:
		fn = c.compileBlock(n)
	case *ast.WhileStatement:
		// 优先 De Bruijn 原生（Level 5）→ Smi 槽位（Level 4）→ 熔化循环（Level 1-2）
		fn = c.tryCompileWhileDeBruijnNative(n)
		if fn == nil {
			fn = c.tryCompileWhileSmiNative(n)
		}
		if fn == nil {
			fn = c.compileWhileFused(n)
		}
	case *ast.ForStatement:
		// 优先尝试 De Bruijn 原生循环（Level 5），再尝试 Smi 标记原生循环（Level 4），
		// 再尝试 int64 原生循环（Level 3），最后回退到熔合循环（Level 1-2）
		fn = c.tryCompileForDeBruijnNative(n)
		if fn == nil {
			fn = c.tryCompileForSmiNative(n)
		}
		if fn == nil {
			fn = c.tryCompileForNative(n)
		}
		if fn == nil {
			fn = c.compileForFused(n)
		}
	}

	// 缓存
	if fn != nil {
		c.cache[node] = fn
	}
	return fn
}

// --- 字面量：直接返回常量对象，零开销 ---

func (c *SAPCompiler) compileInteger(n *ast.IntegerLiteral) CompileFunc {
	val := &object.Integer{Value: n.Value}
	return func(env *object.Environment) object.Object {
		return val
	}
}

func (c *SAPCompiler) compileBoolean(n *ast.BooleanLiteral) CompileFunc {
	var val object.Object
	if n.Value {
		val = TRUE
	} else {
		val = FALSE
	}
	return func(env *object.Environment) object.Object {
		return val
	}
}

func (c *SAPCompiler) compileFloat(n *ast.FloatLiteral) CompileFunc {
	val := &object.Float{Value: n.Value}
	return func(env *object.Environment) object.Object {
		return val
	}
}

func (c *SAPCompiler) compileString(n *ast.StringLiteral) CompileFunc {
	val := &object.String{Value: n.Value}
	return func(env *object.Environment) object.Object {
		return val
	}
}

func (c *SAPCompiler) compileNull(n *ast.NullLiteral) CompileFunc {
	return func(env *object.Environment) object.Object {
		return NULL
	}
}

// compileFString 编译 f-字符串：预编译各 part 为闭包，运行时求值拼接
func (c *SAPCompiler) compileFString(n *ast.FStringLiteral) CompileFunc {
	type partFn struct {
		isText bool
		text   string
		fn     CompileFunc
	}
	var parts []partFn
	for _, part := range n.Parts {
		if sl, ok := part.(*ast.StringLiteral); ok {
			parts = append(parts, partFn{isText: true, text: sl.Value})
		} else {
			fn := c.Compile(part)
			if fn == nil {
				return nil // 子表达式无法编译，回退
			}
			parts = append(parts, partFn{fn: fn})
		}
	}
	return func(env *object.Environment) object.Object {
		var buf []byte
		for _, p := range parts {
			if p.isText {
				buf = append(buf, p.text...)
			} else {
				val := p.fn(env)
				if isError(val) {
					return val
				}
				buf = append(buf, val.Inspect()...)
			}
		}
		return &object.String{Value: string(buf)}
	}
}

// --- 标识符：slot 数组查找，O(1) 无哈希 ---

func (c *SAPCompiler) compileIdentifier(n *ast.Identifier) CompileFunc {
	slot, ok := c.slotMap[n.Value]
	if !ok {
		return nil // 未分配槽位，回退
	}
	return func(env *object.Environment) object.Object {
		// 尝试 slot 快速路径
		if env.HasSlots() {
			val := env.GetSlot(slot)
			if val != nil {
				return val
			}
		}
		// 回退到 map 查找（外层环境）
		val, _ := env.Get(n.Value)
		return val
	}
}

// --- 中缀表达式：类型硬化算术闭包 ---

func (c *SAPCompiler) compileInfix(n *ast.InfixExpression) CompileFunc {
	// 递归编译左右子节点
	leftFn := c.Compile(n.Left)
	rightFn := c.Compile(n.Right)

	// 获取推断类型
	leftType := c.inf.GetNodeType(n.Left)
	rightType := c.inf.GetNodeType(n.Right)

	// 错误传播：仅当子树含守卫源（除法/取模，可能产生运行时错误）时携带检查
	propagate := c.needsErrorPropagation(n)

	// === 整数运算硬化 ===
	if isIntType(leftType) && isIntType(rightType) {
		return c.compileIntInfix(n, n.Operator, leftFn, rightFn, propagate)
	}

	// === 浮点数运算硬化 ===
	if isFloatType(leftType) || isFloatType(rightType) {
		return c.compileFloatInfix(n, n.Operator, leftFn, rightFn, propagate)
	}

	// === 字符串运算硬化 ===
	if isStrType(leftType) && isStrType(rightType) {
		return c.compileStrInfix(n, n.Operator, leftFn, rightFn, propagate)
	}

	// 其他类型暂不编译，回退到传统 Eval
	return nil
}

// needsErrorPropagation 编译期判定：节点（或其子树）是否会触发运行时错误守卫。
// 只有含"除零/取模零/整数溢出"守卫的节点才可能产生错误对象，
// 此时闭包必须携带错误传播检查；纯算术子树零检查（语义等价性由
// "纯子树不可能产生错误"的编译期证明保证，运行期零分支）。
func (c *SAPCompiler) needsErrorPropagation(node ast.Node) bool {
	return c.Safety != nil && c.Safety.VerdictOf(node) == VerdictGuarded
}

// evalGuardedPair 带错误传播的子表达式求值（左先右后，与慢路径求值顺序一致）
func evalGuardedPair(env *object.Environment, leftFn, rightFn CompileFunc) (object.Object, object.Object, object.Object) {
	lv := leftFn(env)
	if lv.Type() == object.ERROR_OBJ {
		return nil, nil, lv
	}
	rv := rightFn(env)
	if rv.Type() == object.ERROR_OBJ {
		return nil, nil, rv
	}
	return lv, rv, nil
}

// compileIntInfix 整数中缀运算：propagate=false 时完全跳过类型断言与错误检查
func (c *SAPCompiler) compileIntInfix(node ast.Node, op string, leftFn, rightFn CompileFunc, propagate bool) CompileFunc {
	if leftFn == nil || rightFn == nil {
		return nil
	}
	if !propagate {
		return c.compileIntInfixPure(node, op, leftFn, rightFn)
	}
	return c.compileIntInfixGuarded(node, op, leftFn, rightFn)
}

// compileIntInfixPure 零检查快闭包：运行期零分支、零错误检查
func (c *SAPCompiler) compileIntInfixPure(node ast.Node, op string, leftFn, rightFn CompileFunc) CompileFunc {
	switch op {
	case "+":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.Integer).Value
			r := rightFn(env).(*object.Integer).Value
			return &object.Integer{Value: l + r}
		}
	case "-":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.Integer).Value
			r := rightFn(env).(*object.Integer).Value
			return &object.Integer{Value: l - r}
		}
	case "*":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.Integer).Value
			r := rightFn(env).(*object.Integer).Value
			return &object.Integer{Value: l * r}
		}
	case "<":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.Integer).Value
			r := rightFn(env).(*object.Integer).Value
			return nativeBoolToBooleanObject(l < r)
		}
	case ">":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.Integer).Value
			r := rightFn(env).(*object.Integer).Value
			return nativeBoolToBooleanObject(l > r)
		}
	case "<=":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.Integer).Value
			r := rightFn(env).(*object.Integer).Value
			return nativeBoolToBooleanObject(l <= r)
		}
	case ">=":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.Integer).Value
			r := rightFn(env).(*object.Integer).Value
			return nativeBoolToBooleanObject(l >= r)
		}
	case "==":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.Integer).Value
			r := rightFn(env).(*object.Integer).Value
			return nativeBoolToBooleanObject(l == r)
		}
	case "!=":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.Integer).Value
			r := rightFn(env).(*object.Integer).Value
			return nativeBoolToBooleanObject(l != r)
		}
	}
	return nil
}

// compileIntInfixGuarded 带运行期守卫与错误传播的整数运算（除零/取模零/溢出）
func (c *SAPCompiler) compileIntInfixGuarded(node ast.Node, op string, leftFn, rightFn CompileFunc) CompileFunc {
	switch op {
	case "+":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return &object.Integer{Value: lv.(*object.Integer).Value + rv.(*object.Integer).Value}
		}
	case "-":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return &object.Integer{Value: lv.(*object.Integer).Value - rv.(*object.Integer).Value}
		}
	case "*":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return &object.Integer{Value: lv.(*object.Integer).Value * rv.(*object.Integer).Value}
		}
	case "/":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			l := lv.(*object.Integer).Value
			r := rv.(*object.Integer).Value
			if r == 0 {
				return newErrorWithNode(node, diagnostic.ErrDivisionByZero.Code, "除零错误")
			}
			// Go 整数除法对 MinInt64 / -1 会 panic，必须显式守卫
			if l == math.MinInt64 && r == -1 {
				return newErrorWithNode(node, diagnostic.ErrIntegerOverflow.Code, "整数溢出")
			}
			return &object.Integer{Value: l / r}
		}
	case "%":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			l := lv.(*object.Integer).Value
			r := rv.(*object.Integer).Value
			if r == 0 {
				return newErrorWithNode(node, diagnostic.ErrModByZero.Code, "取模零错误")
			}
			// Go 整数取模对 MinInt64 % -1 同样 panic
			if l == math.MinInt64 && r == -1 {
				return newErrorWithNode(node, diagnostic.ErrIntegerOverflow.Code, "整数溢出")
			}
			return &object.Integer{Value: l % r}
		}
	case "<":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.Integer).Value < rv.(*object.Integer).Value)
		}
	case ">":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.Integer).Value > rv.(*object.Integer).Value)
		}
	case "<=":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.Integer).Value <= rv.(*object.Integer).Value)
		}
	case ">=":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.Integer).Value >= rv.(*object.Integer).Value)
		}
	case "==":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.Integer).Value == rv.(*object.Integer).Value)
		}
	case "!=":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.Integer).Value != rv.(*object.Integer).Value)
		}
	}
	return nil
}

// compileFloatInfix 浮点数中缀运算：propagate=false 走零检查 Pure 版
func (c *SAPCompiler) compileFloatInfix(node ast.Node, op string, leftFn, rightFn CompileFunc, propagate bool) CompileFunc {
	if leftFn == nil || rightFn == nil {
		return nil
	}
	if !propagate {
		return c.compileFloatInfixPure(node, op, leftFn, rightFn)
	}
	return c.compileFloatInfixGuarded(node, op, leftFn, rightFn)
}

// compileFloatInfixPure 零检查快闭包
func (c *SAPCompiler) compileFloatInfixPure(node ast.Node, op string, leftFn, rightFn CompileFunc) CompileFunc {
	if leftFn == nil || rightFn == nil {
		return nil
	}

	switch op {
	case "+":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			return &object.Float{Value: l + r}
		}
	case "-":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			return &object.Float{Value: l - r}
		}
	case "*":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			return &object.Float{Value: l * r}
		}
	case "/":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			if r == 0 {
				return newErrorWithNode(node, diagnostic.ErrDivisionByZero.Code, "除零错误")
			}
			return &object.Float{Value: l / r}
		}
	case "<":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			return nativeBoolToBooleanObject(l < r)
		}
	case ">":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			return nativeBoolToBooleanObject(l > r)
		}
	case "<=":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			return nativeBoolToBooleanObject(l <= r)
		}
	case ">=":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			return nativeBoolToBooleanObject(l >= r)
		}
	case "==":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			return nativeBoolToBooleanObject(l == r)
		}
	case "!=":
		return func(env *object.Environment) object.Object {
			l := toFloat(leftFn(env))
			r := toFloat(rightFn(env))
			return nativeBoolToBooleanObject(l != r)
		}
	}
	return nil
}

// compileFloatInfixGuarded 带守卫与错误传播的浮点运算
func (c *SAPCompiler) compileFloatInfixGuarded(node ast.Node, op string, leftFn, rightFn CompileFunc) CompileFunc {
	if leftFn == nil || rightFn == nil {
		return nil
	}
	switch op {
	case "+":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return &object.Float{Value: toFloat(lv) + toFloat(rv)}
		}
	case "-":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return &object.Float{Value: toFloat(lv) - toFloat(rv)}
		}
	case "*":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return &object.Float{Value: toFloat(lv) * toFloat(rv)}
		}
	case "/":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			r := toFloat(rv)
			if r == 0 {
				return newErrorWithNode(node, diagnostic.ErrDivisionByZero.Code, "除零错误")
			}
			return &object.Float{Value: toFloat(lv) / r}
		}
	case "<":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(toFloat(lv) < toFloat(rv))
		}
	case ">":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(toFloat(lv) > toFloat(rv))
		}
	case "<=":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(toFloat(lv) <= toFloat(rv))
		}
	case ">=":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(toFloat(lv) >= toFloat(rv))
		}
	case "==":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(toFloat(lv) == toFloat(rv))
		}
	case "!=":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(toFloat(lv) != toFloat(rv))
		}
	}
	return nil
}

// compileStrInfix 字符串中缀运算：字符串子树不会产生守卫错误，恒走 Pure
func (c *SAPCompiler) compileStrInfix(node ast.Node, op string, leftFn, rightFn CompileFunc, propagate bool) CompileFunc {
	if leftFn == nil || rightFn == nil {
		return nil
	}
	if !propagate {
		return c.compileStrInfixPure(node, op, leftFn, rightFn)
	}
	return c.compileStrInfixGuarded(node, op, leftFn, rightFn)
}

// compileStrInfixPure 零检查字符串闭包
func (c *SAPCompiler) compileStrInfixPure(node ast.Node, op string, leftFn, rightFn CompileFunc) CompileFunc {
	if leftFn == nil || rightFn == nil {
		return nil
	}

	switch op {
	case "+":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.String).Value
			r := rightFn(env).(*object.String).Value
			return &object.String{Value: l + r}
		}
	case "==":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.String).Value
			r := rightFn(env).(*object.String).Value
			return nativeBoolToBooleanObject(l == r)
		}
	case "!=":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.String).Value
			r := rightFn(env).(*object.String).Value
			return nativeBoolToBooleanObject(l != r)
		}
	case "<":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.String).Value
			r := rightFn(env).(*object.String).Value
			return nativeBoolToBooleanObject(l < r)
		}
	case ">":
		return func(env *object.Environment) object.Object {
			l := leftFn(env).(*object.String).Value
			r := rightFn(env).(*object.String).Value
			return nativeBoolToBooleanObject(l > r)
		}
	}
	return nil
}

// compileStrInfixGuarded 带错误传播的字符串闭包（防御性分支）
func (c *SAPCompiler) compileStrInfixGuarded(node ast.Node, op string, leftFn, rightFn CompileFunc) CompileFunc {
	if leftFn == nil || rightFn == nil {
		return nil
	}
	switch op {
	case "+":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return &object.String{Value: lv.(*object.String).Value + rv.(*object.String).Value}
		}
	case "==":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.String).Value == rv.(*object.String).Value)
		}
	case "!=":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.String).Value != rv.(*object.String).Value)
		}
	case "<":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.String).Value < rv.(*object.String).Value)
		}
	case ">":
		return func(env *object.Environment) object.Object {
			lv, rv, err := evalGuardedPair(env, leftFn, rightFn)
			if err != nil {
				return err
			}
			return nativeBoolToBooleanObject(lv.(*object.String).Value > rv.(*object.String).Value)
		}
	}
	return nil
}

// --- 辅助函数 ---

func isIntType(t infer.Type) bool {
	if pt, ok := t.(infer.TypePrimitive); ok {
		return pt == infer.TInt
	}
	return false
}

func isFloatType(t infer.Type) bool {
	if pt, ok := t.(infer.TypePrimitive); ok {
		return pt == infer.TFloat
	}
	return false
}

func isStrType(t infer.Type) bool {
	if pt, ok := t.(infer.TypePrimitive); ok {
		return pt == infer.TStr
	}
	return false
}

// CompileProgram 编译整个程序：遍历 AST，预编译所有可优化的节点
func (c *SAPCompiler) CompileProgram(program *ast.Program) {
	// === 编译期安全检查：先对整个程序做一次安全分析 ===
	// 所有判定均在编译期完成，运行期零开销。
	c.Safety = NewSafetyVerifier(c.inf, c.slotMap).Verify(program)

	// 先运行 De Bruijn 解析器，为每个标识符分配 (Depth, Slot) 坐标
	resolver := NewDeBruijnResolver(c.slotMap)
	c.debruijnMap = resolver.Resolve(program)

	for _, stmt := range program.Statements {
		c.compileStatement(stmt)
	}
}

func (c *SAPCompiler) compileStatement(stmt ast.Statement) {
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		if s.Expression != nil {
			c.Compile(s.Expression)
		}
	case *ast.LetStatement:
		if s.Value != nil {
			c.Compile(s.Value)
		}
		// 编译为 SAP 闭包（供 SAP 快速路径使用，含槽位写入）
		c.Compile(s)
	case *ast.ReturnStatement:
		if s.ReturnValue != nil {
			c.Compile(s.ReturnValue)
		}
		// 编译为 SAP 闭包
		c.Compile(s)
	case *ast.WhileStatement:
		// 先预编译内部表达式（供传统 Eval 路径使用）
		if s.Condition != nil {
			c.Compile(s.Condition)
		}
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				c.compileStatement(bs)
			}
		}
		// 再尝试编译熔合循环闭包（供 SAP 快速路径使用）
		c.Compile(s)
	case *ast.ForStatement:
		// 先预编译内部表达式（供传统 Eval 路径使用）
		if s.Init != nil {
			c.Compile(s.Init)
		}
		if s.Condition != nil {
			c.Compile(s.Condition)
		}
		if s.Update != nil {
			c.Compile(s.Update)
		}
		if s.Body != nil {
			for _, bs := range s.Body.Statements {
				c.compileStatement(bs)
			}
		}
		// 再尝试编译熔合/原生循环闭包（供 SAP 快速路径使用）
		c.Compile(s)
	case *ast.BlockStatement:
		for _, bs := range s.Statements {
			c.compileStatement(bs)
		}
	}
}

// GetCompiled 获取节点的编译闭包（供 Eval 调用）
func (c *SAPCompiler) GetCompiled(node ast.Node) CompileFunc {
	if fn, ok := c.cache[node]; ok {
		return fn
	}
	return nil
}
