package evaluator

import (
	"sync"

	"zuzo/ast"
	"zuzo/object"
)

// expr_cache.go — 基于指纹的死代码消除 + 常量折叠
//
// 在 Evaluator 的 evalInfixExpression 中插入一个前置检查层：
// 先计算表达式的指纹，如果匹配已知的死代码模式，直接返回简化结果。
//
// 例如：x * 0 → 直接返回 Integer{0}，跳过对 x 的求值
//       x 并且 假 → 直接返回 FALSE，跳过对 x 的求值
//       x + 0 → 直接返回对 x 的求值结果，跳过 +0 运算

// globalDeadCodePatterns 全局死代码模式库（初始化一次）
var globalDeadCodePatterns = NewDeadCodePatterns()

// globalFingerprintCache 全局指纹缓存（并发安全）。
// DeadCodeSimplify 在 evalInfixExpression 的热路径中被调用，
// 每次调用 ast.Fingerprint 都会执行两遍扫描（收集变量+排序+计算）。
// 由于 AST 节点是指针唯一的（同一指针 = 同一节点），指纹是确定性的，
// 可以缓存避免重复计算。这对紧凑循环中的 i+1、i<10 等表达式尤为关键。
// 使用 sync.Map 实现并发安全，支持未来多 Goroutine 并行求值。
var globalFingerprintCache = NewExprFingerprintCache()

// DeadCodeSimplify 尝试对中缀表达式进行死代码简化。
// 如果匹配到已知模式且二次校验通过，返回简化结果和 true。
// 否则返回 nil, false，表示需要正常求值。
//
// 关键设计：返回 false 时绝对不调用 evalInfixExpression，
// 由调用方 evalInfixExpression 自行处理，避免无限递归。
//
// 性能优化：使用 globalFingerprintCache 缓存指纹，
// 同一 AST 节点只计算一次指纹，后续直接 O(1) 查缓存。
func DeadCodeSimplify(node *ast.InfixExpression, env *object.Environment) (result object.Object, simplified bool) {
	// 使用缓存获取指纹，避免热路径中反复执行两遍扫描
	fp := globalFingerprintCache.Get(node)

	// 查找死代码模式
	if pattern, ok := globalDeadCodePatterns.Lookup(fp); ok {
		return applySimplification(pattern.Pattern, node, env)
	}

	// 查找警告模式（不简化，但可以做日志）
	if _, ok := globalDeadCodePatterns.LookupWarning(fp); ok {
		// 警告模式不简化，正常求值
		// 未来可以在这里添加诊断日志
	}

	return nil, false
}

// applySimplification 根据模式应用简化。
// 返回 (result, true) 表示成功简化；返回 (nil, false) 表示无法简化，调用方应正常求值。
//
// 关键设计：所有涉及整数字面量的模式都使用泛型名 "x OP INT"，
// 因为指纹无法区分不同的整数值（所有整数映射为同一质数）。
// 必须在简化前进行严格的二次值校验，避免 x*2 被误优化为 x。
// 校验不通过时返回 false，绝不调用 evalInfixExpression（避免无限递归）。
func applySimplification(pattern string, node *ast.InfixExpression, env *object.Environment) (object.Object, bool) {
	switch pattern {
	case "x * INT":
		return simplifyMultiply(node, env)

	case "x + INT":
		return simplifyAdd(node, env)

	case "x - INT":
		return simplifySubtract(node, env)

	case "x / INT":
		return simplifyDivide(node, env)

	case "x 并且 假":
		return FALSE, true

	case "x 或者 真":
		return TRUE, true

	default:
		return nil, false
	}
}

// simplifyMultiply 简化乘法：x * 0 → 0, x * 1 → x, 0 * x → 0, 1 * x → x
// 返回 (nil, false) 表示常量值非 0 非 1，放弃优化。
func simplifyMultiply(node *ast.InfixExpression, env *object.Environment) (object.Object, bool) {
	// 检查右侧是否为 0 或 1
	if intLit, ok := getIntegerLiteral(node.Right); ok {
		switch intLit.Value {
		case 0:
			return &object.Integer{Value: 0}, true // x * 0 → 0
		case 1:
			return Eval(node.Left, env), true // x * 1 → x
		}
	}
	// 检查左侧是否为 0 或 1（乘法满足交换律）
	if intLit, ok := getIntegerLiteral(node.Left); ok {
		switch intLit.Value {
		case 0:
			return &object.Integer{Value: 0}, true // 0 * x → 0
		case 1:
			return Eval(node.Right, env), true // 1 * x → x
		}
	}
	// 常量值非 0 非 1，放弃优化
	return nil, false
}

// simplifyAdd 简化加法：x + 0 → x, 0 + x → x
func simplifyAdd(node *ast.InfixExpression, env *object.Environment) (object.Object, bool) {
	if intLit, ok := getIntegerLiteral(node.Right); ok && intLit.Value == 0 {
		return Eval(node.Left, env), true // x + 0 → x
	}
	if intLit, ok := getIntegerLiteral(node.Left); ok && intLit.Value == 0 {
		return Eval(node.Right, env), true // 0 + x → x
	}
	return nil, false
}

// simplifySubtract 简化减法：x - 0 → x
// 注意：减法不满足交换律，0 - x ≠ x，所以只处理右侧为 0 的情况
func simplifySubtract(node *ast.InfixExpression, env *object.Environment) (object.Object, bool) {
	if intLit, ok := getIntegerLiteral(node.Right); ok && intLit.Value == 0 {
		return Eval(node.Left, env), true // x - 0 → x
	}
	return nil, false
}

// simplifyDivide 简化除法：x / 1 → x
// 注意：除法不满足交换律，1 / x ≠ x
// 注意：x / 0 是除零异常，绝对不可优化，必须正常求值触发错误
func simplifyDivide(node *ast.InfixExpression, env *object.Environment) (object.Object, bool) {
	if intLit, ok := getIntegerLiteral(node.Right); ok && intLit.Value == 1 {
		return Eval(node.Left, env), true // x / 1 → x
	}
	return nil, false
}

// getIntegerLiteral 从表达式中提取整数字面量（用于二次检查）
func getIntegerLiteral(node ast.Expression) (*ast.IntegerLiteral, bool) {
	if il, ok := node.(*ast.IntegerLiteral); ok {
		return il, true
	}
	return nil, false
}

// --- 常量折叠：编译时计算纯常量表达式 ---

// getConstantValue 直接从 AST 字面量节点提取值，绕过 Eval 的 switch-case 分发。
// 在热路径中比 Eval(node, nil) 快数倍。
func getConstantValue(node ast.Expression) (object.Object, bool) {
	switch n := node.(type) {
	case *ast.IntegerLiteral:
		return &object.Integer{Value: n.Value}, true
	case *ast.FloatLiteral:
		return &object.Float{Value: n.Value}, true
	case *ast.StringLiteral:
		return &object.String{Value: n.Value}, true
	case *ast.BooleanLiteral:
		if n.Value {
			return TRUE, true
		}
		return FALSE, true
	case *ast.NullLiteral:
		return NULL, true
	default:
		return nil, false
	}
}

// ConstantFold 尝试对纯常量表达式进行常量折叠。
// 如果表达式中只包含字面量（无标识符、无函数调用），
// 则直接计算结果，避免运行时重复求值。
//
// 例如：1 + 2 → 3,  3 * 4 → 12,  真 并且 假 → 假
//
// 性能优化：使用 getConstantValue 直接提取字面量值，绕过 Eval。
func ConstantFold(node *ast.InfixExpression) (object.Object, bool) {
	// 检查是否为纯常量表达式
	if !isConstant(node.Left) || !isConstant(node.Right) {
		return nil, false
	}

	// 直接提取常量值，绕过 Eval 的 switch-case 分发
	left, ok1 := getConstantValue(node.Left)
	right, ok2 := getConstantValue(node.Right)
	if !ok1 || !ok2 {
		return nil, false
	}

	// 根据运算符计算结果
	result := evalBinaryOp(node.Operator, left, right)
	if result != nil {
		return result, true
	}
	return nil, false
}

// isConstant 判断表达式是否为常量（只含字面量）
func isConstant(node ast.Expression) bool {
	switch n := node.(type) {
	case *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.BooleanLiteral, *ast.NullLiteral:
		return true

	case *ast.InfixExpression:
		return isConstant(n.Left) && isConstant(n.Right)

	case *ast.PrefixExpression:
		return isConstant(n.Right)

	default:
		return false
	}
}

// evalBinaryOp 执行二元运算（用于常量折叠）
func evalBinaryOp(op string, left, right object.Object) object.Object {
	if left == nil || right == nil {
		return nil
	}

	// 整数运算
	if left.Type() == object.INTEGER_OBJ && right.Type() == object.INTEGER_OBJ {
		l := left.(*object.Integer).Value
		r := right.(*object.Integer).Value
		switch op {
		case "+":
			return &object.Integer{Value: l + r}
		case "-":
			return &object.Integer{Value: l - r}
		case "*":
			return &object.Integer{Value: l * r}
		case "/":
			if r == 0 {
				return nil
			}
			return &object.Integer{Value: l / r}
		case "%":
			if r == 0 {
				return nil
			}
			return &object.Integer{Value: l % r}
		case "==":
			return boolToObj(l == r)
		case "!=":
			return boolToObj(l != r)
		case "<":
			return boolToObj(l < r)
		case ">":
			return boolToObj(l > r)
		case "<=":
			return boolToObj(l <= r)
		case ">=":
			return boolToObj(l >= r)
		}
	}

	// 布尔逻辑
	if left.Type() == object.BOOLEAN_OBJ && right.Type() == object.BOOLEAN_OBJ {
		l := left.(*object.Boolean).Value
		r := right.(*object.Boolean).Value
		switch op {
		case "并且":
			return boolToObj(l && r)
		case "或者":
			return boolToObj(l || r)
		case "==":
			return boolToObj(l == r)
		case "!=":
			return boolToObj(l != r)
		}
	}

	return nil
}

func boolToObj(b bool) object.Object {
	if b {
		return TRUE
	}
	return FALSE
}

// --- SAP CSE 支持 ---

// CSECache 公共子表达式消除缓存
type CSECache struct {
	cache map[uint64]CompileFunc
}

func NewCSECache() *CSECache {
	return &CSECache{cache: make(map[uint64]CompileFunc)}
}

func (c *CSECache) Lookup(fp uint64) (CompileFunc, bool) {
	fn, ok := c.cache[fp]
	return fn, ok
}

func (c *CSECache) Register(fp uint64, fn CompileFunc) {
	c.cache[fp] = fn
}

func (c *CSECache) LookupOrRegister(fp uint64, fn CompileFunc) CompileFunc {
	if existing, ok := c.cache[fp]; ok {
		return existing
	}
	c.cache[fp] = fn
	return fn
}

// --- 并发安全的表达式指纹缓存 ---

// ExprFingerprintCache 表达式指纹缓存（并发安全）
// 使用 sync.Map 实现，支持多 Goroutine 并行求值场景。
type ExprFingerprintCache struct {
	cache sync.Map
}

func NewExprFingerprintCache() *ExprFingerprintCache {
	return &ExprFingerprintCache{}
}

// Get 获取或计算表达式的指纹（并发安全）
// 首次调用执行 O(N) 两遍扫描计算指纹，后续调用 O(1) 查缓存。
func (c *ExprFingerprintCache) Get(node ast.Expression) uint64 {
	if fp, ok := c.cache.Load(node); ok {
		return fp.(uint64)
	}
	fp := ast.Fingerprint(node)
	c.cache.Store(node, fp)
	return fp
}
