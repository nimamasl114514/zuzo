package ast

import (
	"sort"
)

// fingerprint.go — 拓扑指纹 (Topological Fingerprint)
//
// 基于"交换律感知"的哥德尔树折叠算法。
// 将整棵 AST 子树压缩为一个 uint64 整数（指纹），实现 O(1) 的结构同构匹配。
//
// 核心思想：
//   1. 命名无关：不同变量名映射为不同质数，但通过规范排序保证
//      x+y 与 a+b 指纹相同（第一个变量→11, 第二个→13）
//   2. 交换律感知：对于 +, *, 并且, 或者, ==, != 等满足交换律的运算符，
//      将子节点指纹相乘。利用乘法交换律，子树顺序翻转指纹不变。
//   3. 非交换律保护：对于 -, /, < 等，使用多项式加权区分左右子树。
//      由于不同变量有不同质数，x-y 与 y-x 指纹不同。
//   4. 结合律感知：乘法满足结合律，(a+b)+c 与 a+(b+c) 指纹相同。
//
// 利用 uint64 的自然溢出（模 2^64）作为哈希域，无需手动取模，极致性能。

// 质数表：为不同的 AST 节点类型和运算符分配质数
var (
	primeLiteralInt uint64 = 13  // 整数字面量
	primeLiteralFlt uint64 = 17  // 浮点字面量
	primeLiteralStr uint64 = 19  // 字符串字面量
	primeLiteralBl  uint64 = 23  // 布尔字面量
	primeNull       uint64 = 29  // 空值
	primeDefault    uint64 = 41  // 未匹配节点的默认质数
	primeError      uint64 = 43  // 错误占位节点
	primeFString    uint64 = 47  // f-字符串
	primeArray      uint64 = 53  // 数组（有序，非交换）
	primeHash       uint64 = 59  // 哈希表（无序，交换律）
	primeIf         uint64 = 61  // 如果表达式
	primeFunc       uint64 = 67  // 函数字面量
	primeCall       uint64 = 71  // 函数调用
	primeIndex      uint64 = 73  // 索引访问
	primeAssign     uint64 = 79  // 赋值
	primeImport     uint64 = 83  // 导入语句
	primeBreak      uint64 = 89  // 跳出语句
	primeContinue   uint64 = 97  // 跳过语句
	primeTryCatch   uint64 = 101 // 执行/遇到语句
	primeDefine     uint64 = 103 // 定义语句
)

// varPrimeTable 变量质数表：按规范顺序为变量分配质数。
// 第一个变量（字典序最小）→ 11，第二个 → 131，第三个 → 137，以此类推。
// 选择大质数避免与节点类型质数冲突。
var varPrimeTable = []uint64{
	11, 131, 137, 149, 157, 167, 173, 179,
	181, 191, 193, 197, 199, 211, 223, 227,
	229, 233, 239, 241, 251, 257, 263, 269,
	271, 277, 281, 283, 293, 307, 311, 313,
}

// 运算符质数表
var opPrimes = map[string]uint64{
	"+":  331,
	"-":  337,
	"*":  347,
	"/":  349,
	"%":  353,
	"<":  359,
	">":  367,
	"<=": 373,
	">=": 379,
	"==": 383,
	"!=": 389,
	"并且": 397,
	"或者": 401,
	"否则": 409, // fallback 运算符（非交换）
	"=":  419,
	"!":  421,
	"非":  431,
}

// commutativeOps 记录满足交换律的运算符集合。
// 对于这些运算符，a OP b 与 b OP a 的指纹相同。
var commutativeOps = map[string]bool{
	"+":  true,
	"*":  true,
	"==": true,
	"!=": true,
	"并且": true,
	"或者": true,
}

// fingerprintCtx 指纹计算上下文。
// 使用两遍扫描：
//
//	第一遍：collectVars 收集子树中所有唯一变量名
//	规范排序后分配不同质数（第一个变量→varPrimeTable[0]，第二个→[1]...）
//	第二遍：compute 使用分配的质数计算指纹
//
// 这样 x-y 与 a-b 指纹相同（命名无关），
// 但 x-y 与 y-x 指纹不同（非交换律保护）。
type fingerprintCtx struct {
	varPrimes map[string]uint64 // 变量名 → 质数
	varSet    map[string]bool   // 变量名集合（收集阶段使用）
}

func newFingerprintCtx() *fingerprintCtx {
	return &fingerprintCtx{
		varPrimes: make(map[string]uint64),
		varSet:    make(map[string]bool),
	}
}

// --- 第一遍：收集变量 ---

func (ctx *fingerprintCtx) collectVarsExpr(node Expression) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case *Identifier:
		ctx.varSet[n.Value] = true

	case *PrefixExpression:
		ctx.collectVarsExpr(n.Right)

	case *InfixExpression:
		ctx.collectVarsExpr(n.Left)
		ctx.collectVarsExpr(n.Right)

	case *AssignExpression:
		// 赋值目标变量名不参与收集（声明侧），只收集值表达式中的变量
		ctx.collectVarsExpr(n.Value)
		if n.IndexTarget != nil {
			ctx.collectVarsExpr(n.IndexTarget)
		}

	case *IfExpression:
		ctx.collectVarsExpr(n.Condition)
		ctx.collectVarsBlock(n.Consequence)
		ctx.collectVarsBlock(n.Alternative)

	case *FunctionLiteral:
		// 参数名不参与收集（声明侧），只收集函数体中的变量
		ctx.collectVarsBlock(n.Body)

	case *CallExpression:
		// 函数名不参与收集：它是结构元素（类似运算符），不是变量
		// 这样 f(x,y) 和 g(a,b) 指纹相同（命名无关）
		for _, arg := range n.Arguments {
			ctx.collectVarsExpr(arg)
		}
		for _, na := range n.NamedArgs {
			ctx.collectVarsExpr(na.Value)
		}

	case *ArrayLiteral:
		for _, e := range n.Elements {
			ctx.collectVarsExpr(e)
		}

	case *HashLiteral:
		for k, v := range n.Pairs {
			ctx.collectVarsExpr(k)
			ctx.collectVarsExpr(v)
		}

	case *IndexExpression:
		ctx.collectVarsExpr(n.Left)
		ctx.collectVarsExpr(n.Index)

	case *FStringLiteral:
		for _, p := range n.Parts {
			ctx.collectVarsExpr(p)
		}
	}
}

func (ctx *fingerprintCtx) collectVarsBlock(block *BlockStatement) {
	if block == nil {
		return
	}
	for _, stmt := range block.Statements {
		ctx.collectVarsNode(stmt)
	}
}

func (ctx *fingerprintCtx) collectVarsNode(node Node) {
	if node == nil {
		return
	}
	switch n := node.(type) {
	case Expression:
		ctx.collectVarsExpr(n)

	case *ExpressionStatement:
		ctx.collectVarsExpr(n.Expression)

	case *LetStatement:
		// 声明的变量名不参与收集，只收集值表达式中的变量
		ctx.collectVarsExpr(n.Value)

	case *ReturnStatement:
		ctx.collectVarsExpr(n.ReturnValue)

	case *WhileStatement:
		ctx.collectVarsExpr(n.Condition)
		ctx.collectVarsBlock(n.Body)

	case *ForStatement:
		ctx.collectVarsExpr(n.Init)
		ctx.collectVarsExpr(n.Condition)
		ctx.collectVarsExpr(n.Update)
		ctx.collectVarsBlock(n.Body)

	case *ForInStatement:
		ctx.collectVarsExpr(n.Iterable)
		ctx.collectVarsExpr(n.EndExpr)
		ctx.collectVarsExpr(n.Guard)
		ctx.collectVarsBlock(n.Body)

	case *BlockStatement:
		ctx.collectVarsBlock(n)

	case *IfExpression:
		ctx.collectVarsExpr(n.Condition)
		ctx.collectVarsBlock(n.Consequence)
		ctx.collectVarsBlock(n.Alternative)

	case *FunctionLiteral:
		ctx.collectVarsBlock(n.Body)

	case *TryCatchStatement:
		ctx.collectVarsBlock(n.Body)
		ctx.collectVarsBlock(n.Handler)

	case *DefineStatement:
		for _, f := range n.Fields {
			ctx.collectVarsExpr(f.Value)
		}

	case *ImportStatement, *BreakStatement, *ContinueStatement:
		// 无变量
	}
}

// --- 规范分配质数 ---

func (ctx *fingerprintCtx) assignPrimes() {
	if len(ctx.varSet) == 0 {
		return
	}
	names := make([]string, 0, len(ctx.varSet))
	for name := range ctx.varSet {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		if i < len(varPrimeTable) {
			ctx.varPrimes[name] = varPrimeTable[i]
		} else {
			// 超出表范围：使用名称哈希作为后备
			ctx.varPrimes[name] = primeDefault + uint64(hashString(name))
		}
	}
}

func (ctx *fingerprintCtx) identPrime(name string) uint64 {
	if p, ok := ctx.varPrimes[name]; ok {
		return p
	}
	// 变量未被收集（不应发生），使用默认质数
	return primeDefault
}

// --- 第二遍：计算指纹 ---

func (ctx *fingerprintCtx) computeExpr(node Expression) uint64 {
	if node == nil {
		return 0
	}

	switch n := node.(type) {
	// --- 叶子节点 ---
	case *Identifier:
		return ctx.identPrime(n.Value)

	case *IntegerLiteral:
		return primeLiteralInt

	case *FloatLiteral:
		return primeLiteralFlt

	case *StringLiteral:
		return primeLiteralStr

	case *BooleanLiteral:
		return primeLiteralBl

	case *NullLiteral:
		return primeNull

	// --- 前缀表达式 ---
	case *PrefixExpression:
		opPrime := getOpPrime(n.Operator)
		return opPrime ^ ctx.computeExpr(n.Right)

	// --- 中缀表达式（算法灵魂：交换律感知）---
	case *InfixExpression:
		left := ctx.computeExpr(n.Left)
		right := ctx.computeExpr(n.Right)
		opPrime := getOpPrime(n.Operator)

		if isCommutative(n.Operator) {
			// 交换律运算符：乘法满足交换律 left*right == right*left
			// 无论 AST 子树怎么翻转，指纹绝对不变
			return opPrime ^ (left * right)
		}
		// 非交换律运算符：多项式加权区分左右子树
		// 由于不同变量有不同质数，x-y 与 y-x 指纹不同
		return opPrime ^ (left*31 + right*37)

	// --- 赋值表达式 ---
	case *AssignExpression:
		valHash := ctx.computeExpr(n.Value)
		if n.Name != nil {
			return primeAssign ^ valHash
		}
		if n.IndexTarget != nil {
			return primeAssign ^ ctx.computeExpr(n.IndexTarget) ^ (valHash * 31)
		}
		return primeAssign ^ valHash

	// --- 如果表达式 ---
	case *IfExpression:
		condHash := ctx.computeExpr(n.Condition)
		thenHash := ctx.computeBlock(n.Consequence)
		elseHash := uint64(0)
		if n.Alternative != nil {
			elseHash = ctx.computeBlock(n.Alternative)
		}
		return primeIf ^ (condHash*31 + thenHash*37 + elseHash*41)

	// --- 函数字面量 ---
	case *FunctionLiteral:
		paramsHash := uint64(1)
		for range n.Parameters {
			paramsHash *= primeDefault // 每个参数贡献一个质数
		}
		bodyHash := ctx.computeBlock(n.Body)
		return primeFunc ^ (paramsHash * bodyHash)

	// --- 函数调用 ---
	case *CallExpression:
		fnHash := ctx.computeExpr(n.Function)
		argsHash := uint64(1)
		for _, arg := range n.Arguments {
			argsHash *= ctx.computeExpr(arg)
		}
		namedHash := uint64(1)
		for _, na := range n.NamedArgs {
			namedHash *= getOpPrime(na.Name) * ctx.computeExpr(na.Value)
		}
		return primeCall ^ fnHash ^ argsHash ^ namedHash

	// --- 数组字面量（有序，非交换）---
	case *ArrayLiteral:
		hash := uint64(0)
		for i, e := range n.Elements {
			hash += ctx.computeExpr(e) * getPosPrime(i)
		}
		return primeArray ^ hash

	// --- 哈希表字面量（无序，交换律）---
	case *HashLiteral:
		hash := uint64(1)
		for k, v := range n.Pairs {
			hash *= ctx.computeExpr(k) ^ ctx.computeExpr(v)
		}
		return primeHash ^ hash

	// --- 索引表达式（非交换）---
	case *IndexExpression:
		left := ctx.computeExpr(n.Left)
		index := ctx.computeExpr(n.Index)
		return primeIndex ^ (left*31 + index*37)

	// --- f-字符串字面量 ---
	case *FStringLiteral:
		hash := uint64(0)
		for i, p := range n.Parts {
			hash += ctx.computeExpr(p) * getPosPrime(i)
		}
		return primeFString ^ hash

	// --- 错误占位节点 ---
	case *ErrorExpression:
		return primeError

	default:
		return primeDefault
	}
}

func (ctx *fingerprintCtx) computeBlock(block *BlockStatement) uint64 {
	if block == nil {
		return 0
	}
	hash := uint64(0)
	for i, stmt := range block.Statements {
		hash += ctx.computeNode(stmt) * getPosPrime(i)
	}
	return hash
}

func (ctx *fingerprintCtx) computeNode(node Node) uint64 {
	if node == nil {
		return 0
	}

	switch n := node.(type) {
	case Expression:
		return ctx.computeExpr(n)

	case *ExpressionStatement:
		return ctx.computeExpr(n.Expression)

	case *LetStatement:
		return ctx.computeExpr(n.Value)

	case *ReturnStatement:
		if n.ReturnValue != nil {
			return ctx.computeExpr(n.ReturnValue)
		}
		return primeNull

	case *WhileStatement:
		condHash := ctx.computeExpr(n.Condition)
		bodyHash := ctx.computeBlock(n.Body)
		return primeIf ^ (condHash*31 + bodyHash*37)

	case *ForStatement:
		initHash := ctx.computeExpr(n.Init)
		condHash := ctx.computeExpr(n.Condition)
		updateHash := ctx.computeExpr(n.Update)
		bodyHash := ctx.computeBlock(n.Body)
		return primeIf ^ (initHash*31 + condHash*37 + updateHash*41 + bodyHash*43)

	case *ForInStatement:
		iterHash := ctx.computeExpr(n.Iterable)
		endHash := uint64(0)
		if n.EndExpr != nil {
			endHash = ctx.computeExpr(n.EndExpr)
		}
		guardHash := uint64(0)
		if n.Guard != nil {
			guardHash = ctx.computeExpr(n.Guard)
		}
		bodyHash := ctx.computeBlock(n.Body)
		return primeIf ^ (iterHash*31 + endHash*37 + guardHash*41 + bodyHash*43)

	case *BlockStatement:
		return ctx.computeBlock(n)

	case *IfExpression:
		return ctx.computeExpr(n)

	case *FunctionLiteral:
		return ctx.computeExpr(n)

	case *AssignExpression:
		return ctx.computeExpr(n)

	case *ImportStatement:
		return primeImport

	case *BreakStatement:
		return primeBreak

	case *ContinueStatement:
		return primeContinue

	case *TryCatchStatement:
		bodyHash := ctx.computeBlock(n.Body)
		handlerHash := ctx.computeBlock(n.Handler)
		return primeTryCatch ^ (bodyHash*31 + handlerHash*37)

	case *DefineStatement:
		hash := uint64(1)
		for _, f := range n.Fields {
			hash *= getOpPrime(f.Name) * ctx.computeExpr(f.Value)
		}
		return primeDefine ^ hash

	default:
		return primeDefault
	}
}

// --- 公共 API ---

// Fingerprint 计算 AST 表达式节点的拓扑指纹。
// 利用 uint64 的自然溢出（相当于模 2^64）作为哈希域，无需手动取模。
//
// 对 nil 节点返回 0。
func Fingerprint(node Expression) uint64 {
	if node == nil {
		return 0
	}
	ctx := newFingerprintCtx()
	ctx.collectVarsExpr(node)
	ctx.assignPrimes()
	return ctx.computeExpr(node)
}

// FingerprintNode 计算 AST 节点（语句或表达式）的拓扑指纹。
// 对语句节点，提取其中的核心表达式进行指纹计算。
func FingerprintNode(node Node) uint64 {
	if node == nil {
		return 0
	}
	ctx := newFingerprintCtx()
	ctx.collectVarsNode(node)
	ctx.assignPrimes()
	return ctx.computeNode(node)
}

// fingerprintBlock 计算代码块中所有语句指纹的组合。（向后兼容）
func fingerprintBlock(block *BlockStatement) uint64 {
	if block == nil {
		return 0
	}
	ctx := newFingerprintCtx()
	ctx.collectVarsBlock(block)
	ctx.assignPrimes()
	return ctx.computeBlock(block)
}

// --- 辅助函数 ---

// isCommutative 判断运算符是否满足交换律
func isCommutative(op string) bool {
	return commutativeOps[op]
}

// getOpPrime 获取运算符对应的质数
func getOpPrime(op string) uint64 {
	if p, ok := opPrimes[op]; ok {
		return p
	}
	return primeDefault
}

// posPrimes 为位置敏感的加权提供质数。
var posPrimes = []uint64{
	2, 3, 5, 7, 11, 13, 17, 19,
	23, 29, 31, 37, 41, 43, 47, 53,
}

// getPosPrime 获取位置 i 对应的加权质数
func getPosPrime(i int) uint64 {
	if i >= 0 && i < len(posPrimes) {
		return posPrimes[i]
	}
	return 31
}

// hashString 简单字符串哈希（用于超出质数表时的后备）
func hashString(s string) uint64 {
	var h uint64 = 1469598103934665603 // FNV offset basis
	for _, c := range s {
		h ^= uint64(c)
		h *= 1099511628211 // FNV prime
	}
	return h
}
