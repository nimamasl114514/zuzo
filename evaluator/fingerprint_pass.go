package evaluator

import (
	"fmt"
	"strings"

	"zuzo/ast"
	"zuzo/token"
)

// fingerprint_pass.go — 拓扑指纹分析 pass
//
// 在 Parser 之后、SAP 编译之前运行，提供：
//   1. 代码查重：找出所有结构相同的代码块（即使变量名不同）
//   2. 死代码检测：识别 x*0、x+0、x并且假 等恒等模式
//   3. 公共子表达式消除 (CSE) 候选：无副作用的重复表达式
//   4. 优化建议报告

// FingerprintReport 指纹分析报告
type FingerprintReport struct {
	TotalNodes      int               // 总节点数
	UniqueNodes     int               // 唯一指纹数
	DuplicateGroups []DuplicateGroup  // 重复代码组
	DeadCodeFound   []DeadCodeFinding // 死代码发现
	CSECandidates   []CSECandidate    // CSE 候选
	PatternWarnings []PatternWarning  // 模式警告
}

// DuplicateGroup 重复代码组
type DuplicateGroup struct {
	Fingerprint uint64
	Count       int
	Entries     []ast.FingerprintEntry
}

// DeadCodeFinding 死代码发现
type DeadCodeFinding struct {
	Pattern    string // 模式描述，如 "x * 0"
	Suggestion string // 优化建议，如 "可替换为 0"
	Location   string // 源码位置
	NodeType   string // 节点类型
}

// CSECandidate 公共子表达式消除候选
type CSECandidate struct {
	Fingerprint uint64
	Count       int
	Locations   []string
	NodeType    string
}

// PatternWarning 模式警告
type PatternWarning struct {
	Pattern    string // 模式名
	Message    string // 警告信息
	Suggestion string // 修复建议
	Location   string // 源码位置
}

// FingerprintPass 指纹分析 pass
type FingerprintPass struct {
	table    *ast.FingerprintTable
	report   *FingerprintReport
	patterns *DeadCodePatterns
}

// NewFingerprintPass 创建指纹分析 pass
func NewFingerprintPass() *FingerprintPass {
	return &FingerprintPass{
		table:    ast.NewFingerprintTable(),
		report:   &FingerprintReport{},
		patterns: NewDeadCodePatterns(),
	}
}

// Run 运行指纹分析
func (fp *FingerprintPass) Run(program *ast.Program, fileHint string) *FingerprintReport {
	// 1. 注册所有节点到指纹表
	fp.table.RegisterProgram(program, fileHint)

	// 2. 收集统计信息
	stats := fp.table.Stats()
	fp.report.TotalNodes = stats.TotalNodes
	fp.report.UniqueNodes = stats.UniqueNodes

	// 3. 检测重复代码
	fp.detectDuplicates()

	// 4. 单次遍历：检测死代码 + 模式警告（避免双重遍历）
	fp.analyzeExpressions(program, fileHint)

	// 5. 检测 CSE 候选
	fp.detectCSE()

	return fp.report
}

// Table 返回指纹表（供外部使用，如 SAP CSE）
func (fp *FingerprintPass) Table() *ast.FingerprintTable {
	return fp.table
}

// detectDuplicates 检测重复代码
func (fp *FingerprintPass) detectDuplicates() {
	dups := fp.table.FindDuplicates()
	for _, group := range dups {
		dg := DuplicateGroup{
			Fingerprint: group[0].Fingerprint,
			Count:       len(group),
			Entries:     group,
		}
		fp.report.DuplicateGroups = append(fp.report.DuplicateGroups, dg)
	}
}

// detectCSE 检测公共子表达式消除候选
func (fp *FingerprintPass) detectCSE() {
	// 无副作用的重复表达式是 CSE 候选
	cseGroups := fp.table.FindSideEffectFreeDuplicates(isPureNode)
	for _, group := range cseGroups {
		candidate := CSECandidate{
			Fingerprint: group[0].Fingerprint,
			Count:       len(group),
			NodeType:    group[0].NodeType,
		}
		for _, entry := range group {
			candidate.Locations = append(candidate.Locations, entry.Location)
		}
		fp.report.CSECandidates = append(fp.report.CSECandidates, candidate)
	}
}

// analyzeExpressions 单次遍历 AST，同时检测死代码和模式警告。
// 合并原先的 detectDeadCode 和 detectPatternWarnings，避免双重遍历。
func (fp *FingerprintPass) analyzeExpressions(program *ast.Program, fileHint string) {
	walkProgram(program, fileHint, func(node ast.Node, location string) {
		expr, ok := node.(ast.Expression)
		if !ok {
			return
		}

		fingerprint := ast.Fingerprint(expr)

		// 1. 检测死代码
		if pattern, ok := fp.patterns.Lookup(fingerprint); ok {
			fp.report.DeadCodeFound = append(fp.report.DeadCodeFound, DeadCodeFinding{
				Pattern:    pattern.Pattern,
				Suggestion: pattern.Suggestion,
				Location:   location,
				NodeType:   ast.NodeTypeName(node),
			})
		}

		// 2. 检测警告
		if warning, ok := fp.patterns.LookupWarning(fingerprint); ok {
			fp.report.PatternWarnings = append(fp.report.PatternWarnings, PatternWarning{
				Pattern:    warning.Pattern,
				Message:    warning.Message,
				Suggestion: warning.Suggestion,
				Location:   location,
			})
		}
	})
}

// Format 将报告格式化为可读文本
func (r *FingerprintReport) Format() string {
	var buf strings.Builder

	buf.WriteString("═══════════════════════════════════════════════\n")
	buf.WriteString("  拓扑指纹分析报告 (Topological Fingerprint Analysis)\n")
	buf.WriteString("═══════════════════════════════════════════════\n\n")

	// 统计概览
	buf.WriteString("📊 统计概览\n")
	buf.WriteString(fmt.Sprintf("   总节点数:     %d\n", r.TotalNodes))
	buf.WriteString(fmt.Sprintf("   唯一指纹:     %d\n", r.UniqueNodes))
	dupCount := r.TotalNodes - r.UniqueNodes
	buf.WriteString(fmt.Sprintf("   重复节点:     %d (%.1f%%)\n", dupCount, pct(dupCount, r.TotalNodes)))
	buf.WriteString("\n")

	// 重复代码
	if len(r.DuplicateGroups) > 0 {
		buf.WriteString("🔍 代码查重\n")
		for i, group := range r.DuplicateGroups {
			buf.WriteString(fmt.Sprintf("   [%d] 重复 %d 次 (指纹: %#x)\n", i+1, group.Count, group.Fingerprint))
			for _, entry := range group.Entries {
				buf.WriteString(fmt.Sprintf("       ├─ %s  [%s]\n", entry.Location, entry.NodeType))
			}
			buf.WriteString("\n")
		}
	}

	// 死代码
	if len(r.DeadCodeFound) > 0 {
		buf.WriteString("☠️  死代码检测\n")
		for i, dc := range r.DeadCodeFound {
			buf.WriteString(fmt.Sprintf("   [%d] %s  at %s\n", i+1, dc.Pattern, dc.Location))
			buf.WriteString(fmt.Sprintf("       → %s\n", dc.Suggestion))
		}
		buf.WriteString("\n")
	}

	// CSE 候选
	if len(r.CSECandidates) > 0 {
		buf.WriteString("⚡ 公共子表达式消除 (CSE) 候选\n")
		for i, cse := range r.CSECandidates {
			buf.WriteString(fmt.Sprintf("   [%d] %s × %d (指纹: %#x)\n", i+1, cse.NodeType, cse.Count, cse.Fingerprint))
			for _, loc := range cse.Locations {
				buf.WriteString(fmt.Sprintf("       ├─ %s\n", loc))
			}
		}
		buf.WriteString("\n")
	}

	// 模式警告
	if len(r.PatternWarnings) > 0 {
		buf.WriteString("⚠️  模式警告\n")
		for i, w := range r.PatternWarnings {
			buf.WriteString(fmt.Sprintf("   [%d] %s  at %s\n", i+1, w.Pattern, w.Location))
			buf.WriteString(fmt.Sprintf("       %s\n", w.Message))
			buf.WriteString(fmt.Sprintf("       → %s\n", w.Suggestion))
		}
		buf.WriteString("\n")
	}

	// 总结
	if dupCount == 0 && len(r.DeadCodeFound) == 0 && len(r.CSECandidates) == 0 && len(r.PatternWarnings) == 0 {
		buf.WriteString("✓ 未发现可优化项，代码结构良好\n")
	}

	return buf.String()
}

// --- 死代码模式库 ---

// DeadCodePattern 死代码模式
type DeadCodePattern struct {
	Pattern    string // 模式描述
	Suggestion string // 优化建议
}

// WarningPattern 警告模式
type WarningPattern struct {
	Pattern    string
	Message    string
	Suggestion string
}

// DeadCodePatterns 死代码模式库
type DeadCodePatterns struct {
	deadCodes map[uint64]DeadCodePattern
	warnings  map[uint64]WarningPattern
}

// NewDeadCodePatterns 创建死代码模式库
func NewDeadCodePatterns() *DeadCodePatterns {
	dcp := &DeadCodePatterns{
		deadCodes: make(map[uint64]DeadCodePattern),
		warnings:  make(map[uint64]WarningPattern),
	}
	dcp.initPatterns()
	return dcp
}

// initPatterns 初始化预定义模式
func (dcp *DeadCodePatterns) initPatterns() {
	// 构造模式 AST 并计算指纹
	// 使用 _ 作为占位变量名，实际匹配时变量名不影响指纹

	// --- 死代码：恒等表达式 ---
	// 注意：所有整数常量映射为同一质数，x*0 和 x*1 指纹相同。
	// 只注册泛型模式 "x * INT"，运行时通过二次检查区分具体值。
	dcp.registerDeadCode("*", makeIdentP("_"), makeIntP(0), "x * INT", "需二次校验：0→结果恒为0, 1→可简化为x")
	// x + INT → 需二次校验：0→简化为x
	dcp.registerDeadCode("+", makeIdentP("_"), makeIntP(0), "x + INT", "需二次校验：0→可简化为x")
	// x - INT → 需二次校验：0→简化为x
	dcp.registerDeadCode("-", makeIdentP("_"), makeIntP(0), "x - INT", "需二次校验：0→可简化为x")
	// x / INT → 需二次校验：1→简化为x，0→除零异常不可优化
	dcp.registerDeadCode("/", makeIdentP("_"), makeIntP(1), "x / INT", "需二次校验：1→可简化为x")

	// --- 死代码：布尔恒等式 ---
	// x 并且 假 → 假
	dcp.registerDeadCode("并且", makeIdentP("_"), makeBoolP(false), "x 并且 假", "结果恒为假")
	// x 或者 真 → 真
	dcp.registerDeadCode("或者", makeIdentP("_"), makeBoolP(true), "x 或者 真", "结果恒为真")

	// --- 警告：可疑代码模式 ---
	// x == x → 真 (总是 true)
	dcp.registerWarning("==", makeIdentP("_"), makeIdentP("_"),
		"x == x", "自比较恒为真，可能逻辑错误", "检查是否应为 x == y")
	// x != x → 假 (总是 false)
	dcp.registerWarning("!=", makeIdentP("_"), makeIdentP("_"),
		"x != x", "自比较恒为假，可能逻辑错误", "检查是否应为 x != y")
	// x - x → 0
	dcp.registerWarning("-", makeIdentP("_"), makeIdentP("_"),
		"x - x", "自减法恒为 0", "如需 0 可直接写 0")
}

// registerDeadCode 注册死代码模式
func (dcp *DeadCodePatterns) registerDeadCode(op string, left, right ast.Expression, pattern, suggestion string) {
	expr := &ast.InfixExpression{
		Token:    token.Token{Type: token.TokenType(op), Literal: op},
		Operator: op,
		Left:     left,
		Right:    right,
	}
	fp := ast.Fingerprint(expr)
	dcp.deadCodes[fp] = DeadCodePattern{Pattern: pattern, Suggestion: suggestion}
}

// registerWarning 注册警告模式
func (dcp *DeadCodePatterns) registerWarning(op string, left, right ast.Expression, pattern, message, suggestion string) {
	expr := &ast.InfixExpression{
		Token:    token.Token{Type: token.TokenType(op), Literal: op},
		Operator: op,
		Left:     left,
		Right:    right,
	}
	fp := ast.Fingerprint(expr)
	dcp.warnings[fp] = WarningPattern{Pattern: pattern, Message: message, Suggestion: suggestion}
}

// Lookup 查找死代码模式
func (dcp *DeadCodePatterns) Lookup(fp uint64) (DeadCodePattern, bool) {
	p, ok := dcp.deadCodes[fp]
	return p, ok
}

// LookupWarning 查找警告模式
func (dcp *DeadCodePatterns) LookupWarning(fp uint64) (WarningPattern, bool) {
	p, ok := dcp.warnings[fp]
	return p, ok
}

// --- 辅助函数 ---

// isPureNode 判断节点是否无副作用（纯表达式）
func isPureNode(node ast.Node) bool {
	if node == nil {
		return false
	}
	switch n := node.(type) {
	case *ast.Identifier, *ast.IntegerLiteral, *ast.FloatLiteral,
		*ast.StringLiteral, *ast.BooleanLiteral, *ast.NullLiteral:
		return true

	case *ast.InfixExpression:
		return isPureNode(n.Left) && isPureNode(n.Right)

	case *ast.PrefixExpression:
		return isPureNode(n.Right)

	case *ast.IndexExpression:
		return isPureNode(n.Left) && isPureNode(n.Index)

	case *ast.ArrayLiteral:
		for _, e := range n.Elements {
			if !isPureNode(e) {
				return false
			}
		}
		return true

	case *ast.HashLiteral:
		for k, v := range n.Pairs {
			if !isPureNode(k) || !isPureNode(v) {
				return false
			}
		}
		return true

	// 以下类型有副作用
	case *ast.CallExpression, *ast.AssignExpression:
		return false

	default:
		return false
	}
}

// extractLine 从 AST 节点中提取真实的源码行号。
// 所有 zuzo AST 节点都嵌有 Token 字段，其中 Token.Line 是词法分析器记录的物理行号。
func extractLine(node ast.Node) int {
	switch n := node.(type) {
	case *ast.ExpressionStatement:
		return n.Token.Line
	case *ast.LetStatement:
		return n.Token.Line
	case *ast.ReturnStatement:
		return n.Token.Line
	case *ast.WhileStatement:
		return n.Token.Line
	case *ast.ForStatement:
		return n.Token.Line
	case *ast.ForInStatement:
		return n.Token.Line
	case *ast.ImportStatement:
		return n.Token.Line
	case *ast.BlockStatement:
		return n.Token.Line
	case *ast.Identifier:
		return n.Token.Line
	case *ast.IntegerLiteral:
		return n.Token.Line
	case *ast.FloatLiteral:
		return n.Token.Line
	case *ast.StringLiteral:
		return n.Token.Line
	case *ast.FStringLiteral:
		return n.Token.Line
	case *ast.BooleanLiteral:
		return n.Token.Line
	case *ast.NullLiteral:
		return n.Token.Line
	case *ast.PrefixExpression:
		return n.Token.Line
	case *ast.InfixExpression:
		return n.Token.Line
	case *ast.AssignExpression:
		return n.Token.Line
	case *ast.IfExpression:
		return n.Token.Line
	case *ast.FunctionLiteral:
		return n.Token.Line
	case *ast.CallExpression:
		return n.Token.Line
	case *ast.ArrayLiteral:
		return n.Token.Line
	case *ast.HashLiteral:
		return n.Token.Line
	case *ast.IndexExpression:
		return n.Token.Line
	case *ast.ErrorExpression:
		return n.Token.Line
	case *ast.TryCatchStatement:
		return n.Token.Line
	case *ast.DefineStatement:
		return n.Token.Line
	case *ast.BreakStatement:
		return n.Token.Line
	case *ast.ContinueStatement:
		return n.Token.Line
	default:
		return 0
	}
}

// nodeLocation 根据 AST 节点的 Token.Line 生成真实的源码位置字符串。
func nodeLocation(node ast.Node, fileHint string) string {
	line := extractLine(node)
	if line > 0 {
		return fmt.Sprintf("%s:%d", fileHint, line)
	}
	return fileHint
}

// walkProgram 遍历程序中的所有节点，使用 AST 节点的真实行号。
func walkProgram(program *ast.Program, fileHint string, fn func(ast.Node, string)) {
	for _, stmt := range program.Statements {
		walkNode(stmt, fileHint, fn)
	}
}

// walkNode 遍历 AST 节点及其子节点，为每个节点生成基于真实行号的 location。
func walkNode(node ast.Node, fileHint string, fn func(ast.Node, string)) {
	if node == nil {
		return
	}

	// 使用 AST 节点的 Token.Line 生成真实位置
	location := nodeLocation(node, fileHint)
	fn(node, location)

	switch n := node.(type) {
	case *ast.ExpressionStatement:
		walkNode(n.Expression, fileHint, fn)

	case *ast.LetStatement:
		walkNode(n.Value, fileHint, fn)

	case *ast.ReturnStatement:
		walkNode(n.ReturnValue, fileHint, fn)

	case *ast.WhileStatement:
		walkNode(n.Condition, fileHint, fn)
		if n.Body != nil {
			for _, s := range n.Body.Statements {
				walkNode(s, fileHint, fn)
			}
		}

	case *ast.ForStatement:
		walkNode(n.Init, fileHint, fn)
		walkNode(n.Condition, fileHint, fn)
		walkNode(n.Update, fileHint, fn)
		if n.Body != nil {
			for _, s := range n.Body.Statements {
				walkNode(s, fileHint, fn)
			}
		}

	case *ast.ForInStatement:
		walkNode(n.Iterable, fileHint, fn)
		walkNode(n.EndExpr, fileHint, fn)
		walkNode(n.Guard, fileHint, fn)
		if n.Body != nil {
			for _, s := range n.Body.Statements {
				walkNode(s, fileHint, fn)
			}
		}

	case *ast.IfExpression:
		walkNode(n.Condition, fileHint, fn)
		if n.Consequence != nil {
			for _, s := range n.Consequence.Statements {
				walkNode(s, fileHint, fn)
			}
		}
		if n.Alternative != nil {
			for _, s := range n.Alternative.Statements {
				walkNode(s, fileHint, fn)
			}
		}

	case *ast.BlockStatement:
		for _, s := range n.Statements {
			walkNode(s, fileHint, fn)
		}

	case *ast.PrefixExpression:
		walkNode(n.Right, fileHint, fn)

	case *ast.InfixExpression:
		walkNode(n.Left, fileHint, fn)
		walkNode(n.Right, fileHint, fn)

	case *ast.AssignExpression:
		walkNode(n.Value, fileHint, fn)
		if n.IndexTarget != nil {
			walkNode(n.IndexTarget, fileHint, fn)
		}

	case *ast.CallExpression:
		walkNode(n.Function, fileHint, fn)
		for _, arg := range n.Arguments {
			walkNode(arg, fileHint, fn)
		}
		// 遍历具名参数的值表达式
		for _, na := range n.NamedArgs {
			walkNode(na.Value, fileHint, fn)
		}

	case *ast.ArrayLiteral:
		for _, e := range n.Elements {
			walkNode(e, fileHint, fn)
		}

	case *ast.HashLiteral:
		for k, v := range n.Pairs {
			walkNode(k, fileHint, fn)
			walkNode(v, fileHint, fn)
		}

	case *ast.IndexExpression:
		walkNode(n.Left, fileHint, fn)
		walkNode(n.Index, fileHint, fn)

	// 遍历 F-String 的插值部分（Parts 包含字面文本和插值表达式）
	case *ast.FStringLiteral:
		for _, part := range n.Parts {
			walkNode(part, fileHint, fn)
		}

	case *ast.FunctionLiteral:
		if n.Body != nil {
			for _, s := range n.Body.Statements {
				walkNode(s, fileHint, fn)
			}
		}

	case *ast.TryCatchStatement:
		if n.Body != nil {
			for _, s := range n.Body.Statements {
				walkNode(s, fileHint, fn)
			}
		}
		if n.Handler != nil {
			for _, s := range n.Handler.Statements {
				walkNode(s, fileHint, fn)
			}
		}

	case *ast.DefineStatement:
		for _, f := range n.Fields {
			walkNode(f.Value, fileHint, fn)
		}
	}
}

func pct(n, d int) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d) * 100
}

// 模式构造辅助函数
func makeIdentP(name string) *ast.Identifier {
	return &ast.Identifier{Token: token.Token{Type: token.IDENT, Literal: name}, Value: name}
}

func makeIntP(val int64) *ast.IntegerLiteral {
	lit := fmt.Sprintf("%d", val)
	return &ast.IntegerLiteral{Token: token.Token{Type: token.INT, Literal: lit}, Value: val}
}

func makeBoolP(val bool) *ast.BooleanLiteral {
	var t token.TokenType = token.TRUE
	var lit string
	if !val {
		t = token.FALSE
		lit = "假"
	} else {
		lit = "真"
	}
	return &ast.BooleanLiteral{Token: token.Token{Type: t, Literal: lit}, Value: val}
}
