package evaluator

// safety.go — SAP 编译期安全检查器（Safety Verification）
//
// 目标：保证 SAP 快路径与慢路径语义强一致，杜绝语义分叉与运行时 panic。
//
// 原则：
//   1. 安全判定全部发生在编译期（CompileProgram 阶段），运行期零开销、零分支。
//   2. 零假阴性：无法证明安全的节点一律拒绝特化，回退慢路径。宁可少特化，
//      不可错特化。
//   3. 判定与编译器实际能力严格同步：Safe/Guarded 节点编译必然成功；
//      Rejected 节点编译必然失败（由差分测试强制同步，防止行为漂移）。
//   4. 守卫点显式登记：除法/取模节点标记为 Guarded，运行时保留单条
//      错误守卫（纳秒级），并在报告中统计。
//
// 判定等级（严重度从高到低）：
//   Rejected > Guarded > Safe
//   父节点继承子节点的最严重等级：任一子节点被拒绝 → 父节点整体拒绝。

import (
	"zuzo/ast"
	"zuzo/infer"
)

// Verdict 安全判定结果
type Verdict int8

const (
	// VerdictUnknown 未分析
	VerdictUnknown Verdict = iota
	// VerdictSafe 可安全特化：编译闭包零检查
	VerdictSafe
	// VerdictGuarded 可特化，但需保留运行期守卫（除法/取模零）
	VerdictGuarded
	// VerdictRejected 禁止特化：回退慢路径（原因见 Reason）
	VerdictRejected
)

// SafetyStats 安全检查统计（供审计与差分测试断言）
type SafetyStats struct {
	Total      int            // 分析的节点总数
	Safe       int            // 零检查特化节点数
	Guarded    int            // 带守卫特化节点数
	Rejected   int            // 拒绝特化节点数
	GuardCount int            // 守卫登记总数（除法/取模源节点）
	RejectBy   map[string]int // 拒绝原因 → 数量
}

// SafetyReport 编译期安全检查报告
type SafetyReport struct {
	verdicts map[ast.Node]Verdict
	reasons  map[ast.Node]string
	guards   map[ast.Node]bool // 守卫源节点（除法/取模）
	stats    SafetyStats
}

// VerdictOf 返回节点的安全判定
func (r *SafetyReport) VerdictOf(node ast.Node) Verdict {
	if r == nil || node == nil {
		return VerdictUnknown
	}
	if v, ok := r.verdicts[node]; ok {
		return v
	}
	return VerdictUnknown
}

// IsCompilable 节点是否允许编译特化（Safe 或 Guarded）
func (r *SafetyReport) IsCompilable(node ast.Node) bool {
	v := r.VerdictOf(node)
	return v == VerdictSafe || v == VerdictGuarded
}

// Rejected 节点是否被安全检查拒绝特化
func (r *SafetyReport) Rejected(node ast.Node) bool {
	return r.VerdictOf(node) == VerdictRejected
}

// NeedsGuard 节点是否登记了运行期守卫
func (r *SafetyReport) NeedsGuard(node ast.Node) bool {
	if r == nil || node == nil {
		return false
	}
	return r.guards[node]
}

// Reason 返回拒绝特化的原因（空字符串表示未拒绝）
func (r *SafetyReport) Reason(node ast.Node) string {
	if r == nil || node == nil {
		return ""
	}
	return r.reasons[node]
}

// Stats 返回安全检查统计
func (r *SafetyReport) Stats() SafetyStats {
	if r == nil {
		return SafetyStats{RejectBy: map[string]int{}}
	}
	return r.stats
}

// SafetyVerifier 编译期安全检查器：遍历 AST，产出判定报告
type SafetyVerifier struct {
	inf     *infer.Inferencer
	slotMap map[string]int
	report  *SafetyReport
}

// NewSafetyVerifier 创建安全检查器
func NewSafetyVerifier(inf *infer.Inferencer, slotMap map[string]int) *SafetyVerifier {
	return &SafetyVerifier{
		inf:     inf,
		slotMap: slotMap,
		report: &SafetyReport{
			verdicts: make(map[ast.Node]Verdict),
			reasons:  make(map[ast.Node]string),
			guards:   make(map[ast.Node]bool),
			stats:    SafetyStats{RejectBy: make(map[string]int)},
		},
	}
}

// Verify 遍历整个程序，产出安全检查报告
func (v *SafetyVerifier) Verify(program *ast.Program) *SafetyReport {
	for _, stmt := range program.Statements {
		v.walkStmt(stmt)
	}
	return v.report
}

// set 记录节点判定并更新统计
func (v *SafetyVerifier) set(node ast.Node, verdict Verdict, reason string) {
	if node == nil {
		return
	}
	if _, exists := v.report.verdicts[node]; exists {
		return // 同一节点只记录一次
	}
	v.report.verdicts[node] = verdict
	v.report.stats.Total++
	switch verdict {
	case VerdictSafe:
		v.report.stats.Safe++
	case VerdictGuarded:
		v.report.stats.Guarded++
	case VerdictRejected:
		v.report.stats.Rejected++
		v.report.reasons[node] = reason
		v.report.stats.RejectBy[reason]++
	}
}

// guard 登记守卫源节点（除法/取模）
func (v *SafetyVerifier) guard(node ast.Node) {
	v.report.guards[node] = true
	v.report.stats.GuardCount++
}

// combine 合并子节点判定（继承最严重等级）
func (v *SafetyVerifier) combine(children ...Verdict) Verdict {
	worst := VerdictSafe
	for _, c := range children {
		if c == VerdictRejected {
			return VerdictRejected
		}
		if c == VerdictGuarded {
			worst = VerdictGuarded
		}
	}
	return worst
}

// walkStmt 语句安全检查
func (v *SafetyVerifier) walkStmt(stmt ast.Statement) {
	if stmt == nil {
		return
	}
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		v.set(stmt, v.walkExpr(s.Expression), "")
	case *ast.LetStatement:
		v.set(stmt, v.walkExpr(s.Value), "")
	case *ast.ReturnStatement:
		v.set(stmt, v.walkExpr(s.ReturnValue), "")
	case *ast.WhileStatement:
		v.set(stmt, v.combine(v.walkExpr(s.Condition), v.walkBlock(s.Body)), "")
	case *ast.ForStatement:
		v.set(stmt, v.combine(
			v.walkExpr(s.Init), v.walkExpr(s.Condition), v.walkExpr(s.Update), v.walkBlock(s.Body),
		), "")
	case *ast.BlockStatement:
		v.set(stmt, v.walkBlock(s), "")
	case *ast.ImportStatement:
		v.set(stmt, VerdictRejected, "动态行为: 导入")
	case *ast.DefineStatement:
		v.set(stmt, VerdictRejected, "动态行为: 定义")
	case *ast.TryCatchStatement:
		v.set(stmt, VerdictRejected, "动态行为: 执行/遇到")
	case *ast.ForInStatement:
		v.set(stmt, VerdictRejected, "未支持: 遍历循环")
	case *ast.BreakStatement:
		// 循环信号机制由熔合循环闭包处理，本身无语义风险
		v.set(stmt, VerdictSafe, "")
	case *ast.ContinueStatement:
		v.set(stmt, VerdictSafe, "")
	default:
		v.set(stmt, VerdictRejected, "未识别的语句类型")
	}
}

// walkBlock 代码块安全检查
func (v *SafetyVerifier) walkBlock(block *ast.BlockStatement) Verdict {
	if block == nil {
		return VerdictSafe
	}
	children := make([]Verdict, 0, len(block.Statements))
	for _, s := range block.Statements {
		v.walkStmt(s)
		children = append(children, v.report.VerdictOf(s))
	}
	return v.combine(children...)
}

// walkExpr 表达式安全检查
func (v *SafetyVerifier) walkExpr(expr ast.Expression) Verdict {
	if expr == nil {
		return VerdictSafe
	}
	switch e := expr.(type) {
	case *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
		*ast.BooleanLiteral, *ast.NullLiteral:
		v.set(expr, VerdictSafe, "")
		return VerdictSafe

	case *ast.Identifier:
		if _, ok := v.slotMap[e.Value]; ok {
			v.set(expr, VerdictSafe, "")
			return VerdictSafe
		}
		v.set(expr, VerdictRejected, "未分配槽位")
		return VerdictRejected

	case *ast.FStringLiteral:
		children := make([]Verdict, 0, len(e.Parts))
		for _, p := range e.Parts {
			children = append(children, v.walkExpr(p))
		}
		verdict := v.combine(children...)
		v.set(expr, verdict, "")
		return verdict

	case *ast.PrefixExpression:
		verdict := v.walkExpr(e.Right)
		v.set(expr, verdict, "")
		return verdict

	case *ast.InfixExpression:
		v.set(expr, v.walkInfix(e), "")
		return v.report.VerdictOf(expr)

	case *ast.AssignExpression:
		if e.Name == nil {
			v.set(expr, VerdictRejected, "未支持: 索引赋值")
			return VerdictRejected
		}
		if _, ok := v.slotMap[e.Name.Value]; !ok {
			v.set(expr, VerdictRejected, "未分配槽位")
			return VerdictRejected
		}
		verdict := v.walkExpr(e.Value)
		v.set(expr, verdict, "")
		return verdict

	case *ast.IfExpression:
		v.set(expr, VerdictRejected, "未支持: 如果表达式")
		return VerdictRejected
	case *ast.CallExpression:
		v.set(expr, VerdictRejected, "动态行为: 函数调用")
		return VerdictRejected
	case *ast.FunctionLiteral:
		v.set(expr, VerdictRejected, "动态行为: 函数字面量")
		return VerdictRejected
	case *ast.ArrayLiteral:
		v.set(expr, VerdictRejected, "未支持: 数组字面量")
		return VerdictRejected
	case *ast.HashLiteral:
		v.set(expr, VerdictRejected, "未支持: 哈希字面量")
		return VerdictRejected
	case *ast.IndexExpression:
		v.set(expr, VerdictRejected, "未支持: 索引访问")
		return VerdictRejected
	case *ast.ErrorExpression:
		v.set(expr, VerdictRejected, "解析错误节点")
		return VerdictRejected
	default:
		v.set(expr, VerdictRejected, "未识别的表达式类型")
		return VerdictRejected
	}
}

// walkInfix 中缀表达式安全检查：类型锁定 + 守卫登记
func (v *SafetyVerifier) walkInfix(e *ast.InfixExpression) Verdict {
	leftVerdict := v.walkExpr(e.Left)
	rightVerdict := v.walkExpr(e.Right)
	if leftVerdict == VerdictRejected || rightVerdict == VerdictRejected {
		return VerdictRejected
	}

	leftType := v.inf.GetNodeType(e.Left)
	rightType := v.inf.GetNodeType(e.Right)
	intArith := isIntType(leftType) && isIntType(rightType)
	floatArith := isFloatType(leftType) || isFloatType(rightType)
	strCmp := isStrType(leftType) && isStrType(rightType)

	// 类型必须被锁定为编译器已硬化的集合
	if !intArith && !floatArith && !strCmp {
		return VerdictRejected
	}

	// 运算符必须属于硬化集合
	switch e.Operator {
	case "+", "-", "*", "<", ">", "<=", ">=", "==", "!=":
		return v.combine(leftVerdict, rightVerdict)
	case "/", "%":
		// 除零/取模零/整数溢出：自身即为守卫源 → VerdictGuarded
		//（排除子树 Rejected 的情况，由顶部检查保证）
		v.guard(e)
		return VerdictGuarded
	default:
		// 逻辑运算符（并且/或者等）未硬化
		return VerdictRejected
	}
}
