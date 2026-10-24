package evaluator

import (
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"time"

	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/object"
)

// 定义全局常量对象，避免重复创建
var (
	NULL  = &object.Null{}
	TRUE  = &object.Boolean{Value: true}
	FALSE = &object.Boolean{Value: false}
)

// NewEnvironment 创建带有内置函数的全局环境
func NewEnvironment() *object.Environment {
	env := object.NewEnvironment()
	registerBuiltins(env)
	return env
}

// sapCompiler 全局 SAP 编译器（可选，nil 时走传统路径）
var sapCompiler *SAPCompiler

// SetSAPCompiler 设置 SAP 编译器（启用静态流水线优化）
func SetSAPCompiler(c *SAPCompiler) {
	sapCompiler = c
}

// GetSAPCompiler 返回当前 SAP 编译器（用于测试）
func GetSAPCompiler() *SAPCompiler {
	return sapCompiler
}

// callStack 全局调用栈（用于运行时错误追踪）
var callStack []stackFrame

// traceEnabled 全局 trace 标志（--trace 启用）
var traceEnabled bool

// debugMode 全局 debug 标志（--debug 启用，自动包含 trace）
var debugMode bool

// profileMode 全局 profile 标志（--profile 启用）
var profileMode bool

// profileData 性能数据收集
var profileData = struct {
	calls    map[string]int64
	time     map[string]int64 // 纳秒
}{
	calls: make(map[string]int64),
	time:  make(map[string]int64),
}

// debugEnvRef 保存当前全局环境引用（用于错误时输出变量快照）
var debugEnvRef *object.Environment

// traceDepth 缩进深度（用于输出对齐）
var traceDepth int

type stackFrame struct {
	name string
	line int
}

// pushFrame 压入调用栈
func pushFrame(name string, line int) {
	callStack = append(callStack, stackFrame{name: name, line: line})
}

// popFrame 弹出调用栈
func popFrame() {
	if len(callStack) > 0 {
		callStack = callStack[:len(callStack)-1]
	}
}

// snapshotStackTrace 获取当前调用栈快照（逆序：最外层在前）
func snapshotStackTrace() []string {
	if len(callStack) == 0 {
		return nil
	}
	result := make([]string, len(callStack))
	for i, frame := range callStack {
		if frame.line > 0 {
			result[i] = fmt.Sprintf("%s (行 %d)", frame.name, frame.line)
		} else {
			result[i] = frame.name
		}
	}
	return result
}

// enrichError 为错误对象附加当前调用栈
func enrichError(obj object.Object) object.Object {
	if err, ok := obj.(*object.Error); ok && len(err.StackTrace) == 0 {
		err.StackTrace = snapshotStackTrace()
	}
	return obj
}

// traceLog 输出 trace 信息到 stderr
func traceLog(format string, a ...any) {
	if !traceEnabled {
		return
	}
	indent := strings.Repeat("  ", traceDepth)
	fmt.Fprintf(os.Stderr, "[trace] %s%s\n", indent, fmt.Sprintf(format, a...))
}

// SetTraceEnabled 设置 trace 开关
func SetTraceEnabled(enabled bool) {
	traceEnabled = enabled
}

// SetDebugMode 设置 debug 模式
func SetDebugMode(enabled bool) {
	debugMode = enabled
}

// IsDebugMode 返回 debug 模式是否启用
func IsDebugMode() bool {
	return debugMode
}

// SetProfileMode 设置 profile 模式
func SetProfileMode(enabled bool) {
	profileMode = enabled
}

// IsProfileMode 返回 profile 模式是否启用
func IsProfileMode() bool {
	return profileMode
}

// DumpProfile 输出性能分析报告到 writer
func DumpProfile(w io.Writer) {
	if len(profileData.calls) == 0 {
		return
	}
	fmt.Fprintln(w, "\n=== 性能分析 ===")
	fmt.Fprintln(w, "函数调用统计（按调用次数排序）:")

	// 收集并排序
	type entry struct {
		name  string
		calls int64
		time  int64
	}
	entries := make([]entry, 0, len(profileData.calls))
	for name, calls := range profileData.calls {
		entries = append(entries, entry{name: name, calls: calls, time: profileData.time[name]})
	}
	// 按调用次数降序
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].calls > entries[j].calls
	})

	totalTime := int64(0)
	for _, e := range entries {
		totalTime += e.time
	}

	fmt.Fprintf(w, "  %-20s %8s %12s %8s\n", "函数", "调用次数", "总耗时(ms)", "占比")
	fmt.Fprintln(w, "  -----------------------------------------------------------------")
	for _, e := range entries {
		pct := 0.0
		if totalTime > 0 {
			pct = float64(e.time) / float64(totalTime) * 100
		}
		fmt.Fprintf(w, "  %-20s %8d %12.2f %7.1f%%\n", e.name, e.calls, float64(e.time)/1e6, pct)
	}
	fmt.Fprintf(w, "  -----------------------------------------------------------------\n")
	fmt.Fprintf(w, "  %-20s %8s %12.2f\n", "总计", "", float64(totalTime)/1e6)
}

// SetDebugEnv 设置调试用的全局环境引用
func SetDebugEnv(env *object.Environment) {
	debugEnvRef = env
}

// GetDebugEnv 返回调试用的全局环境引用
func GetDebugEnv() *object.Environment {
	return debugEnvRef
}

// DumpEnv 输出环境中所有变量的快照（用于 --debug 模式）
func DumpEnv(env *object.Environment, w io.Writer) {
	if env == nil {
		return
	}
	// 递归获取所有变量
	seen := make(map[string]bool)
	var dump func(e *object.Environment, depth int)
	dump = func(e *object.Environment, depth int) {
		if e == nil {
			return
		}
		prefix := strings.Repeat("  ", depth)
		all := e.GetAll()
		// 排序输出
		keys := make([]string, 0, len(all))
		for k := range all {
			if seen[k] {
				continue
			}
			seen[k] = true
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(w, "%s%s = %s\n", prefix, k, all[k].Inspect())
		}
		// 递归父环境
		parent := e.Outer()
		if parent != nil {
			dump(parent, depth)
		}
	}
	dump(env, 0)
}

// NewSAPEnvironment 创建带槽位的环境（SAP 优化路径使用）
// slotCount 为槽位数量，slotMap 为变量名→槽位索引映射，均从 inferencer.GetSlotMap() 获取
// 同时创建 Context（扁平帧栈），启用 De Bruijn Index 优化路径（Level 5）
func NewSAPEnvironment(slotCount int, slotMap map[string]int) *object.Environment {
	env := object.NewSlotEnvironment(slotCount, nil, slotMap)
	ctx := object.NewContext(slotCount)
	env.SetContext(ctx)
	registerBuiltins(env)
	return env
}

// Eval 求值入口：遍历 AST 节点并求值
func Eval(node ast.Node, env *object.Environment) object.Object {
	if node == nil {
		return NULL
	}

	// === SAP 快速路径：对可编译的节点类型查缓存 ===
	if sapCompiler != nil {
		switch node.(type) {
		case *ast.InfixExpression, *ast.IntegerLiteral, *ast.FloatLiteral, *ast.StringLiteral,
			*ast.FStringLiteral, *ast.BooleanLiteral, *ast.NullLiteral, *ast.Identifier,
			*ast.LetStatement, *ast.ReturnStatement, *ast.AssignExpression, *ast.ExpressionStatement,
			*ast.PrefixExpression, *ast.BlockStatement, *ast.WhileStatement, *ast.ForStatement:
			if fn := sapCompiler.GetCompiled(node); fn != nil {
				return fn(env)
			}
		}
	}

	switch node := node.(type) {

	// --- 程序 ---
	case *ast.Program:
		return evalProgram(node, env)

	// --- 语句 ---
	case *ast.ExpressionStatement:
		if node == nil || node.Expression == nil {
			return NULL
		}
		// 处理命名函数声明: 函数 名称(参数) { ... }
		if fnLit, ok := node.Expression.(*ast.FunctionLiteral); ok && fnLit.Name != "" {
			fn := &object.Function{Parameters: fnLit.Parameters, Body: fnLit.Body, Env: env, Name: fnLit.Name}
			env.Set(fnLit.Name, fn)
			return fn
		}
		return Eval(node.Expression, env)

	case *ast.LetStatement:
		val := Eval(node.Value, env)
		if isError(val) {
			return val
		}
		env.Set(node.Name.Value, val)
		return NULL

	case *ast.ReturnStatement:
		val := Eval(node.ReturnValue, env)
		if isError(val) {
			return val
		}
		return &object.ReturnValue{Value: val}

	case *ast.WhileStatement:
		return evalWhileStatement(node, env)

	case *ast.ForStatement:
		return evalForStatement(node, env)

	case *ast.ImportStatement:
		return evalImportStatement(node, env)

	case *ast.DefineStatement:
		return evalDefineStatement(node, env)

	case *ast.TryCatchStatement:
		return evalTryCatchStatement(node, env)

	case *ast.ForInStatement:
		return evalForInStatement(node, env)

	case *ast.BreakStatement:
		return &object.BreakSignal{}

	case *ast.ContinueStatement:
		return &object.ContinueSignal{}

	case *ast.BlockStatement:
		return evalBlockStatement(node, env)

	// --- 表达式 ---
	case *ast.IntegerLiteral:
		return &object.Integer{Value: node.Value}

	case *ast.FloatLiteral:
		return &object.Float{Value: node.Value}

	case *ast.StringLiteral:
		return &object.String{Value: node.Value}

	case *ast.FStringLiteral:
		return evalFStringLiteral(node, env)

	case *ast.BooleanLiteral:
		if node.Value {
			return TRUE
		}
		return FALSE

	case *ast.NullLiteral:
		return NULL

	case *ast.Identifier:
		return evalIdentifier(node, env)

	case *ast.PrefixExpression:
		return evalPrefixExpression(node, env)

	case *ast.InfixExpression:
		return evalInfixExpression(node, env)

	case *ast.AssignExpression:
		return evalAssignExpression(node, env)

	case *ast.IfExpression:
		return evalIfExpression(node, env)

	case *ast.FunctionLiteral:
		params := node.Parameters
		body := node.Body
		return &object.Function{Parameters: params, Body: body, Env: env, Name: node.Name}

	case *ast.CallExpression:
		return evalCallExpression(node, env)

	case *ast.ArrayLiteral:
		elems := evalExpressions(node.Elements, env)
		if len(elems) == 1 && isError(elems[0]) {
			return elems[0]
		}
		return &object.Array{Elements: elems}

	case *ast.HashLiteral:
		return evalHashLiteral(node, env)

	case *ast.IndexExpression:
		return evalIndexExpression(node, env)

	case *ast.ErrorExpression:
		// 解析错误占位节点：正常流程中不会到达此处（HasErrors 时已退出），
		// 仅为防御性处理，保证 AST 中含错误节点时不会 nil 崩溃
		return newErrorWithNode(node, diagnostic.ErrNoPrefixParseFn.Code, "解析错误: %s", node.Message)
	}

	return NULL
}

// ==================== 程序和语句求值 ====================

func evalProgram(program *ast.Program, env *object.Environment) object.Object {
	var result object.Object = NULL

	for _, stmt := range program.Statements {
		result = Eval(stmt, env)
		switch result := result.(type) {
		case *object.ReturnValue:
			return result.Value
		case *object.Error:
			return result
		}
	}
	return result
}

func evalBlockStatement(block *ast.BlockStatement, env *object.Environment) object.Object {
	var result object.Object = NULL

	for _, stmt := range block.Statements {
		result = Eval(stmt, env)
		if result != nil {
			rt := result.Type()
			if rt == object.RETURN_VALUE || rt == object.ERROR_OBJ ||
				rt == object.BREAK_SIG || rt == object.CONTINUE_SIG {
				return result
			}
		}
	}
	return result
}

func evalWhileStatement(node *ast.WhileStatement, env *object.Environment) object.Object {
	for {
		cond := Eval(node.Condition, env)
		if isError(cond) {
			return cond
		}
		if !isTruthy(cond) {
			break
		}
		result := Eval(node.Body, env)
		if result != nil {
			rt := result.Type()
			if rt == object.RETURN_VALUE || rt == object.ERROR_OBJ {
				return result
			}
			if rt == object.BREAK_SIG {
				break
			}
			if rt == object.CONTINUE_SIG {
				continue
			}
		}
	}
	return NULL
}

func evalForStatement(node *ast.ForStatement, env *object.Environment) object.Object {
	// 执行初始化
	if node.Init != nil {
		init := Eval(node.Init, env)
		if isError(init) {
			return init
		}
	}

	for {
		// 检查条件
		if node.Condition != nil {
			cond := Eval(node.Condition, env)
			if isError(cond) {
				return cond
			}
			if !isTruthy(cond) {
				break
			}
		}

		// 执行循环体
		result := Eval(node.Body, env)
		if result != nil {
			rt := result.Type()
			if rt == object.RETURN_VALUE || rt == object.ERROR_OBJ {
				return result
			}
			if rt == object.BREAK_SIG {
				break
			}
			// CONTINUE_SIG: 继续执行 update 和下一轮
		}

		// 执行更新
		if node.Update != nil {
			update := Eval(node.Update, env)
			if isError(update) {
				return update
			}
		}
	}
	return NULL
}

// ==================== 表达式求值 ====================

func evalIdentifier(node *ast.Identifier, env *object.Environment) object.Object {
	val, ok := env.Get(node.Value)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrUndefinedIdentifier.Code, "未定义的标识符: %s", node.Value)
	}
	return val
}

func evalPrefixExpression(node *ast.PrefixExpression, env *object.Environment) object.Object {
	right := Eval(node.Right, env)
	if isError(right) {
		return right
	}

	switch node.Operator {
	case "-":
		if right.Type() == object.INTEGER_OBJ {
			val := right.(*object.Integer).Value
			return &object.Integer{Value: -val}
		}
		if right.Type() == object.FLOAT_OBJ {
			val := right.(*object.Float).Value
			return &object.Float{Value: -val}
		}
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code, "运算符 '-' 不支持类型: %s", right.Type())
	case "!":
		return evalBangOperator(right)
	case "非":
		return evalBangOperator(right)
	default:
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code, "未知运算符: %s", node.Operator)
	}
}

func evalBangOperator(right object.Object) object.Object {
	switch right {
	case TRUE:
		return FALSE
	case FALSE:
		return TRUE
	case NULL:
		return TRUE
	default:
		return FALSE
	}
}

func evalInfixExpression(node *ast.InfixExpression, env *object.Environment) object.Object {
	// === 拓扑指纹：死代码消除前置检查 ===
	// 匹配 x*0→0, x+0→x, x并且假→假 等模式，跳过无用计算
	if result, ok := DeadCodeSimplify(node, env); ok {
		return result
	}

	// fallback 运算符：否则
	if node.Operator == "否则" {
		return evalElseFallback(node, env)
	}

	// 短路求值：并且
	if node.Operator == "并且" {
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		if !isTruthy(left) {
			return FALSE
		}
		right := Eval(node.Right, env)
		if isError(right) {
			return right
		}
		if isTruthy(right) {
			return TRUE
		}
		return FALSE
	}

	// 短路求值：或者
	if node.Operator == "或者" {
		left := Eval(node.Left, env)
		if isError(left) {
			return left
		}
		if isTruthy(left) {
			return TRUE
		}
		right := Eval(node.Right, env)
		if isError(right) {
			return right
		}
		if isTruthy(right) {
			return TRUE
		}
		return FALSE
	}

	left := Eval(node.Left, env)
	if isError(left) {
		return left
	}
	right := Eval(node.Right, env)
	if isError(right) {
		return right
	}

	switch {
	case left.Type() == object.INTEGER_OBJ && right.Type() == object.INTEGER_OBJ:
		return evalIntegerInfixExpressionWithNode(node, node.Operator, left, right)
	case left.Type() == object.FLOAT_OBJ || right.Type() == object.FLOAT_OBJ:
		return evalFloatInfixExpression(node, node.Operator, left, right)
	case left.Type() == object.STRING_OBJ && right.Type() == object.STRING_OBJ:
		return evalStringInfixExpression(node, node.Operator, left, right)
	case node.Operator == "==":
		return nativeBoolToBooleanObject(left == right)
	case node.Operator == "!=":
		return nativeBoolToBooleanObject(left != right)
	default:
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code, "运算符 '%s' 不支持类型: %s 和 %s", node.Operator, left.Type(), right.Type())
	}
}

func evalIntegerInfixExpressionWithNode(node ast.Node, op string, left, right object.Object) object.Object {
	leftVal := left.(*object.Integer).Value
	rightVal := right.(*object.Integer).Value

	switch op {
	case "+":
		return &object.Integer{Value: leftVal + rightVal}
	case "-":
		return &object.Integer{Value: leftVal - rightVal}
	case "*":
		return &object.Integer{Value: leftVal * rightVal}
	case "/":
		if rightVal == 0 {
			return newErrorWithNode(node, diagnostic.ErrDivisionByZero.Code, "除零错误")
		}
		// Go 整数除法对 MinInt64 / -1 会 panic，必须显式守卫
		if leftVal == math.MinInt64 && rightVal == -1 {
			return newErrorWithNode(node, diagnostic.ErrIntegerOverflow.Code, "整数溢出")
		}
		return &object.Integer{Value: leftVal / rightVal}
	case "%":
		if rightVal == 0 {
			return newErrorWithNode(node, diagnostic.ErrModByZero.Code, "取模零错误")
		}
		// Go 整数取模对 MinInt64 % -1 同样 panic
		if leftVal == math.MinInt64 && rightVal == -1 {
			return newErrorWithNode(node, diagnostic.ErrIntegerOverflow.Code, "整数溢出")
		}
		return &object.Integer{Value: leftVal % rightVal}
	case "<":
		return nativeBoolToBooleanObject(leftVal < rightVal)
	case ">":
		return nativeBoolToBooleanObject(leftVal > rightVal)
	case "<=":
		return nativeBoolToBooleanObject(leftVal <= rightVal)
	case ">=":
		return nativeBoolToBooleanObject(leftVal >= rightVal)
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code, "整数不支持运算符: %s", op)
	}
}

// old evalIntegerInfixExpression replaced by evalIntegerInfixExpressionWithNode

// evalFloatInfixExpression 处理浮点数运算（含整数/浮点数混合运算）
func evalFloatInfixExpression(node ast.Node, op string, left, right object.Object) object.Object {
	leftVal := toFloat(left)
	rightVal := toFloat(right)

	switch op {
	case "+":
		return &object.Float{Value: leftVal + rightVal}
	case "-":
		return &object.Float{Value: leftVal - rightVal}
	case "*":
		return &object.Float{Value: leftVal * rightVal}
	case "/":
		if rightVal == 0 {
			return newErrorWithNode(node, diagnostic.ErrDivisionByZero.Code, "除零错误")
		}
		return &object.Float{Value: leftVal / rightVal}
	case "%":
		if rightVal == 0 {
			return newErrorWithNode(node, diagnostic.ErrModByZero.Code, "取模零错误")
		}
		return &object.Float{Value: math.Mod(leftVal, rightVal)}
	case "<":
		return nativeBoolToBooleanObject(leftVal < rightVal)
	case ">":
		return nativeBoolToBooleanObject(leftVal > rightVal)
	case "<=":
		return nativeBoolToBooleanObject(leftVal <= rightVal)
	case ">=":
		return nativeBoolToBooleanObject(leftVal >= rightVal)
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	default:
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code, "浮点数不支持运算符: %s", op)
	}
}

// toFloat 将 Integer 或 Float 转换为 float64
func toFloat(obj object.Object) float64 {
	switch o := obj.(type) {
	case *object.Integer:
		return float64(o.Value)
	case *object.Float:
		return o.Value
	default:
		return 0
	}
}

func evalStringInfixExpression(node ast.Node, op string, left, right object.Object) object.Object {
	leftVal := left.(*object.String).Value
	rightVal := right.(*object.String).Value

	switch op {
	case "+":
		return &object.String{Value: leftVal + rightVal}
	case "==":
		return nativeBoolToBooleanObject(leftVal == rightVal)
	case "!=":
		return nativeBoolToBooleanObject(leftVal != rightVal)
	case "<":
		return nativeBoolToBooleanObject(leftVal < rightVal)
	case ">":
		return nativeBoolToBooleanObject(leftVal > rightVal)
	default:
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code, "字符串不支持运算符: %s", op)
	}
}

// evalFStringLiteral 求值 f-字符串: f"Hello {name}, count={count}"
// 各 part 依次求值后转字符串拼接（对齐 Python f-string 语义）
func evalFStringLiteral(node *ast.FStringLiteral, env *object.Environment) object.Object {
	var buf strings.Builder
	for _, part := range node.Parts {
		// StringLiteral part 直接取值（跳过求值开销）
		if sl, ok := part.(*ast.StringLiteral); ok {
			buf.WriteString(sl.Value)
			continue
		}
		// 其他表达式 part 求值后转字符串
		val := Eval(part, env)
		if isError(val) {
			return val
		}
		buf.WriteString(val.Inspect())
	}
	return &object.String{Value: buf.String()}
}

func evalAssignExpression(node *ast.AssignExpression, env *object.Environment) object.Object {
	val := Eval(node.Value, env)
	if isError(val) {
		return val
	}

	// 简单变量赋值
	if node.Name != nil {
		env.Assign(node.Name.Value, val)
		return val
	}

	// 数组/哈希元素赋值
	if node.IndexTarget != nil {
		container := Eval(node.IndexTarget.Left, env)
		if isError(container) {
			return container
		}
		index := Eval(node.IndexTarget.Index, env)
		if isError(index) {
			return index
		}

		switch c := container.(type) {
		case *object.Array:
			idx, ok := index.(*object.Integer)
			if !ok {
				return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "数组索引必须是整数")
			}
			if idx.Value < 0 || idx.Value >= int64(len(c.Elements)) {
				return newErrorWithNode(node, diagnostic.ErrIndexOutOfBounds.Code, "数组索引越界: %d", idx.Value)
			}
			c.Elements[idx.Value] = val
		case *object.Hash:
			hashKey, ok := index.(object.Hashable)
			if !ok {
				return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "不可哈希的键类型: %s", index.Type())
			}
			c.SetKey(hashKey.(object.Object), val)
		case *object.Instance:
			key, ok := index.(*object.String)
			if !ok {
				return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "实例属性必须是字符串")
			}
			c.Props[key.Value] = val
		default:
			return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code, "不支持对类型 %s 进行元素赋值", container.Type())
		}
	}
	return val
}

func evalIfExpression(node *ast.IfExpression, env *object.Environment) object.Object {
	cond := Eval(node.Condition, env)
	if isError(cond) {
		return cond
	}

	if isTruthy(cond) {
		return Eval(node.Consequence, env)
	} else if node.Alternative != nil {
		return Eval(node.Alternative, env)
	}
	return NULL
}

func evalCallExpression(node *ast.CallExpression, env *object.Environment) object.Object {
	fn := Eval(node.Function, env)
	if isError(fn) {
		return fn
	}

	// 错误内省内置（错误码/错误信息/错误行）必须能接收错误对象参数，
	// 否则 执行 { ... } 遇到 e { 错误码(e) } 拿不到任何结构化信息
	allowErrArgs := false
	if b, ok := fn.(*object.Builtin); ok {
		allowErrArgs = b.AllowErrorArgs
	}
	args := evalExpressionsSensitive(node.Arguments, env, allowErrArgs)
	if len(args) == 1 && isError(args[0]) && !allowErrArgs {
		return args[0]
	}

	// 求值具名参数
	namedArgs := make(map[string]object.Object)
	for _, na := range node.NamedArgs {
		val := Eval(na.Value, env)
		if isError(val) {
			return val
		}
		namedArgs[na.Name] = val
	}

	switch fn := fn.(type) {
	case *object.Function:
		return applyFunction(fn, args)
	case *object.Builtin:
		result := fn.Fn(args...)
		// 内置函数出错时，附加调用栈
		return enrichError(result)
	case *object.Define:
		return applyDefine(fn, namedArgs)
	default:
		return newErrorWithNode(node, diagnostic.ErrNotCallableRuntime.Code, "不可调用的对象: %s", fn.Type())
	}
}

func applyFunction(fn *object.Function, args []object.Object) object.Object {
	if len(args) != len(fn.Parameters) {
		return newErrorWithNode(fn.Body, diagnostic.ErrArgCountRuntime.Code, "参数数量不匹配: 期望 %d 个，得到 %d 个", len(fn.Parameters), len(args))
	}

	enclosedEnv := object.NewEnclosedEnvironment(fn.Env)
	for i, param := range fn.Parameters {
		enclosedEnv.Set(param.Value, args[i])
	}

	// 调用栈追踪
	funcName := fn.Name
	if funcName == "" {
		funcName = "<匿名>"
	}
	callLine := 0
	if fn.Body != nil {
		callLine = getNodeLine(fn.Body)
	}
	pushFrame(funcName, callLine)
	defer popFrame()
	traceLog("→ 调用 %s (行 %d)", funcName, callLine)
	traceDepth++
	defer func() { traceDepth-- }()

	// 性能分析：记录函数调用次数和耗时
	var profStart int64
	if profileMode {
		profStart = time.Now().UnixNano()
		profileData.calls[funcName]++
		defer func() {
			elapsed := time.Now().UnixNano() - profStart
			profileData.time[funcName] += elapsed
		}()
	}

	result := Eval(fn.Body, enclosedEnv)
	// 错误向上传播时附加调用栈
	result = enrichError(result)
	if returnValue, ok := result.(*object.ReturnValue); ok {
		return returnValue.Value
	}
	return result
}

// evalExpressions 求值表达式列表（任一参数错误立即短路传播）
func evalExpressions(exps []ast.Expression, env *object.Environment) []object.Object {
	return evalExpressionsSensitive(exps, env, false)
}

// evalExpressionsSensitive 求值表达式列表。
// allowErrors 仅对错误内省内置开启：它们必须接收错误对象才能报告 Code/Message/Line。
func evalExpressionsSensitive(exps []ast.Expression, env *object.Environment, allowErrors bool) []object.Object {
	var result []object.Object

	for _, e := range exps {
		evaluated := Eval(e, env)
		if isError(evaluated) && !allowErrors {
			return []object.Object{evaluated}
		}
		result = append(result, evaluated)
	}
	return result
}

func evalHashLiteral(node *ast.HashLiteral, env *object.Environment) object.Object {
	hash := &object.Hash{}

	for keyNode, valueNode := range node.Pairs {
		key := Eval(keyNode, env)
		if isError(key) {
			return key
		}

		hashKey, ok := key.(object.Hashable)
		if !ok {
			return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "不可哈希的键类型: %s", key.Type())
		}

		value := Eval(valueNode, env)
		if isError(value) {
			return value
		}

		hash.SetKey(hashKey.(object.Object), value)
	}
	return hash
}

func evalIndexExpression(node *ast.IndexExpression, env *object.Environment) object.Object {
	left := Eval(node.Left, env)
	if isError(left) {
		return left
	}
	index := Eval(node.Index, env)
	if isError(index) {
		return index
	}

	switch {
	case left.Type() == object.ARRAY_OBJ && index.Type() == object.INTEGER_OBJ:
		return evalArrayIndexExpression(left, index)
	case left.Type() == object.HASH_OBJ:
		return evalHashIndexExpression(node, left, index)
	case left.Type() == object.MODULE_OBJ:
		return evalModuleIndexExpression(node, left, index)
	case left.Type() == object.INSTANCE_OBJ:
		return evalInstanceIndexExpression(node, left, index)
	case left.Type() == object.WASM_OBJ:
		return evalWasmModuleIndexExpression(node, left, index)
	case left.Type() == object.CLIB_OBJ:
		return evalCLibIndexExpression(node, left, index)
	case left.Type() == object.CLIB_FUNC_OBJ:
		return evalCFuncIndexExpression(node, left, index)
	default:
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code, "不支持对类型 %s 使用索引操作", left.Type())
	}
}

func evalArrayIndexExpression(array, index object.Object) object.Object {
	arr := array.(*object.Array)
	idx := index.(*object.Integer).Value

	if idx < 0 || idx >= int64(len(arr.Elements)) {
		return NULL
	}
	return arr.Elements[idx]
}

func evalHashIndexExpression(node ast.Node, hash, index object.Object) object.Object {
	h := hash.(*object.Hash)

	hashKey, ok := index.(object.Hashable)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "不可哈希的键类型: %s", index.Type())
	}

	pair, ok := h.Pairs[hashKey.HashKey()]
	if !ok {
		return NULL
	}
	return pair.Value
}

// evalModuleIndexExpression 模块成员访问: 模块名["成员名"]
func evalModuleIndexExpression(node ast.Node, mod, index object.Object) object.Object {
	m := mod.(*object.Module)
	key, ok := index.(*object.String)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "模块索引必须是字符串")
	}
	val, ok := m.Env.Get(key.Value)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrHashKeyNotFound.Code, "模块 %s 中不存在成员 %s", m.Name, key.Value)
	}
	return val
}

// evalWasmModuleIndexExpression WASM 模块成员访问: 模块.函数名
// 支持直接点号调用导出函数，以及内置属性方法
func evalWasmModuleIndexExpression(node ast.Node, modObj, indexObj object.Object) object.Object {
	mod := modObj.(*object.WasmModule)
	key, ok := indexObj.(*object.String)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "WASM 模块索引必须是字符串")
	}

	// 内置属性方法
	switch key.Value {
	case "关闭":
		return &object.Builtin{Fn: func(args ...object.Object) object.Object {
			mod.Close()
			return NULL
		}}
	case "导出":
		return &object.Builtin{Fn: func(args ...object.Object) object.Object {
			return wasmExportList(mod)
		}}
	case "内存大小":
		return &object.Builtin{Fn: func(args ...object.Object) object.Object {
			return wasmMemSize(mod)
		}}
	case "内存读取":
		return &object.Builtin{Fn: func(args ...object.Object) object.Object {
			if len(args) != 2 {
				return newError("内存读取() 期望 2 个参数（偏移, 长度）")
			}
			return wasmMemRead(mod, args[0], args[1])
		}}
	case "内存写入":
		return &object.Builtin{Fn: func(args ...object.Object) object.Object {
			if len(args) != 2 {
				return newError("内存写入() 期望 2 个参数（偏移, 数据）")
			}
			return wasmMemWrite(mod, args[0], args[1])
		}}
	}

	// 导出函数 → 返回可直接调用的 Builtin
	fn := mod.Module.ExportedFunction(key.Value)
	if fn == nil {
		return newErrorWithNode(node, diagnostic.ErrHashKeyNotFound.Code,
			"WASM 模块中不存在导出函数 %q", key.Value)
	}

	return wasmModuleBuiltin(mod, key.Value)
}

// ==================== 辅助函数 ====================

func isTruthy(obj object.Object) bool {
	switch obj {
	case NULL:
		return false
	case TRUE:
		return true
	case FALSE:
		return false
	default:
		return true
	}
}

func nativeBoolToBooleanObject(b bool) *object.Boolean {
	if b {
		return TRUE
	}
	return FALSE
}

// getNodeLine 从 AST 节点中提取行号（0 表示未知）
func getNodeLine(node ast.Node) int {
	switch n := node.(type) {
	case *ast.Program:
		return 0
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
	case *ast.BlockStatement:
		return n.Token.Line
	case *ast.ImportStatement:
		return n.Token.Line
	case *ast.IfExpression:
		return n.Token.Line
	case *ast.InfixExpression:
		return n.Token.Line
	case *ast.PrefixExpression:
		return n.Token.Line
	case *ast.CallExpression:
		return n.Token.Line
	case *ast.IndexExpression:
		return n.Token.Line
	case *ast.AssignExpression:
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
	case *ast.ArrayLiteral:
		return n.Token.Line
	case *ast.HashLiteral:
		return n.Token.Line
	case *ast.FunctionLiteral:
		return n.Token.Line
	case *ast.ErrorExpression:
		return n.Token.Line
	case *ast.DefineStatement:
		return n.Token.Line
	case *ast.TryCatchStatement:
		return n.Token.Line
	case *ast.ForInStatement:
		return n.Token.Line
	case *ast.BreakStatement:
		return n.Token.Line
	case *ast.ContinueStatement:
		return n.Token.Line
	default:
		return 0
	}
}

// getNodeColumn 从 AST 节点中提取列号（0 表示未知）
func getNodeColumn(node ast.Node) int {
	switch n := node.(type) {
	case *ast.ExpressionStatement:
		return n.Token.Column
	case *ast.LetStatement:
		return n.Token.Column
	case *ast.ReturnStatement:
		return n.Token.Column
	case *ast.WhileStatement:
		return n.Token.Column
	case *ast.ForStatement:
		return n.Token.Column
	case *ast.BlockStatement:
		return n.Token.Column
	case *ast.ImportStatement:
		return n.Token.Column
	case *ast.IfExpression:
		return n.Token.Column
	case *ast.InfixExpression:
		return n.Token.Column
	case *ast.PrefixExpression:
		return n.Token.Column
	case *ast.CallExpression:
		return n.Token.Column
	case *ast.IndexExpression:
		return n.Token.Column
	case *ast.AssignExpression:
		return n.Token.Column
	case *ast.Identifier:
		return n.Token.Column
	case *ast.IntegerLiteral:
		return n.Token.Column
	case *ast.FloatLiteral:
		return n.Token.Column
	case *ast.StringLiteral:
		return n.Token.Column
	case *ast.FStringLiteral:
		return n.Token.Column
	case *ast.BooleanLiteral:
		return n.Token.Column
	case *ast.NullLiteral:
		return n.Token.Column
	case *ast.ArrayLiteral:
		return n.Token.Column
	case *ast.HashLiteral:
		return n.Token.Column
	case *ast.FunctionLiteral:
		return n.Token.Column
	case *ast.ErrorExpression:
		return n.Token.Column
	case *ast.DefineStatement:
		return n.Token.Column
	case *ast.TryCatchStatement:
		return n.Token.Column
	case *ast.ForInStatement:
		return n.Token.Column
	case *ast.BreakStatement:
		return n.Token.Column
	case *ast.ContinueStatement:
		return n.Token.Column
	default:
		return 0
	}
}

func newError(format string, a ...any) *object.Error {
	return &object.Error{Message: fmt.Sprintf(format, a...)}
}

// newErrorWithNode 创建带位置信息的错误对象
func newErrorWithNode(node ast.Node, code string, format string, a ...any) *object.Error {
	return &object.Error{
		Message: fmt.Sprintf(format, a...),
		Line:    getNodeLine(node),
		Column:  getNodeColumn(node),
		Code:    code,
	}
}

func isError(obj object.Object) bool {
	if obj != nil {
		return obj.Type() == object.ERROR_OBJ
	}
	return false
}

// ==================== 内置函数注册 ====================

func registerBuiltins(env *object.Environment) {
	builtins := map[string]object.BuiltinFunction{
		// --- 通用 ---
		"打印":   builtinPrint,
		"长度":   builtinLen,
		"类型":   builtinType,
		"字符串":  builtinStr,
		"整数":   builtinInt,
		"浮点数": builtinFloat,

		// --- 错误内省（与 执行/遇到 搭配使用） ---
		"错误码":   builtinErrorCode,
		"错误信息": builtinErrorMessage,
		"错误行":   builtinErrorLine,

		// --- 数组（builtins_array.go） ---
		"推入": builtinPush,
		"弹出": builtinPop,
		"首项": builtinFirst,
		"末项": builtinLast,
		"连接": builtinJoin,
		"包含": builtinContains,

		// --- 字符串处理（builtins_strings.go） ---
		"大写":   builtinToUpper,
		"小写":   builtinToLower,
		"前缀":   builtinHasPrefix,
		"后缀":   builtinHasSuffix,
		"查找":   builtinIndex,
		"替换":   builtinReplace,
		"分割":   builtinSplit,
		"去空白": builtinTrimSpace,
		"裁剪":   builtinTrim,
		"重复":   builtinRepeat,
		"计数":   builtinCount,
		"分词":   builtinFields,
		"取字符": builtinCharAt,
		"格式化": builtinFormat,

		// --- 哈希（键遍历；支持中文键） ---
		"键":   builtinKeys,
		"取值": builtinHashGet,

		// --- 并发（builtins_concurrency.go） ---
		"并发":   builtinGo,
		"通道":   builtinMakeChan,
		"发送":   builtinSend,
		"接收":   builtinRecv,
		"关闭":   builtinClose,
		"等待全部": builtinWaitAll,
		"尝试接收": builtinTryRecv,
		"尝试执行": builtinTryEval,

		// --- 文件 I/O（builtins_io.go） ---
		"读文件":   builtinReadFile,
		"写文件":   builtinWriteFile,
		"追加文件": builtinAppendFile,
		"读行":   builtinReadLine,
		"文件存在": builtinFileExists,
		"创建文件": builtinCreateFile,
		"删除文件": builtinDeleteFile,
		"重命名":   builtinRename,
		"读目录":   builtinReadDir,

		"环境变量": builtinGetEnv,
		"运行参数": builtinCmdArgs,

		// --- 网络（builtins_net.go） ---
		"http获取":  builtinHttpGet,
		"http提交":  builtinHttpPost,
		"http更新":  builtinHttpPut,
		"http补丁":  builtinHttpPatch,
		"http删除":  builtinHttpDelete,
		"http请求":  builtinHttpRequest,

		// --- HTTP 服务端（builtins_httpserver.go） ---
		"http服务":     builtinHttpServe,
		"http服务后台": builtinHttpServeBackground,
		"http流请求":   builtinHttpStreamRequest,

		// --- JSON（builtins_json.go） ---
		"json解析":   builtinJsonParse,
		"json序列化": builtinJsonStringify,
		"json格式化": builtinJsonFormat,

		// --- 正则表达式（builtins_regex.go） ---
		"正则匹配":   builtinRegexMatch,
		"正则查找全部": builtinRegexFindAll,
		"正则替换":   builtinRegexReplace,
		"正则分割":   builtinRegexSplit,
		"正则验证":   builtinRegexTest,
		"正则提取":   builtinRegexExtract,

		// --- 数学（builtins_math.go） ---
		"随机":    builtinRandom,
		"随机整数": builtinRandomInt,
		"绝对值":  builtinAbs,
		"最大值":  builtinMax,
		"最小值":  builtinMin,
		"向上取整": builtinCeil,
		"向下取整": builtinFloor,
		"四舍五入": builtinRound,
		"幂":     builtinPow,
		"平方根":  builtinSqrt,
		"正弦":   builtinSin,
		"余弦":   builtinCos,
		"对数":   builtinLog,

		// --- 时间（builtins_time.go） ---
		"时间戳":   builtinUnixTime,
		"毫秒时间戳": builtinUnixMilli,
		"当前时间": builtinNow,
		"格式化时间": builtinFormatTime,
		"解析时间": builtinParseTime,
		"睡眠":   builtinSleep,
		"计时":   builtinTimer,
		"计时秒":  builtinTimerSec,
		"时间差":  builtinTimeDiff,

		// --- 浏览器自动化（builtins_browser.go） ---
		"打开浏览器":  builtinOpenBrowser,
		"关闭浏览器":  builtinCloseBrowser,
		"打开页面":  builtinNewPage,
		"关闭页面":  builtinClosePage,
		"导航":    builtinNavigate,
		"当前网址":  builtinPageURL,
		"页面标题":  builtinPageTitle,
		"等待元素":  builtinWaitElement,
		"等待可见":  builtinWaitVisible,
		"点击":    builtinClickElement,
		"点击按钮":  builtinClickButton,
		"填写":    builtinFillInput,
		"清空输入":  builtinClearInput,
		"获取文本":  builtinGetText,
		"获取属性":  builtinGetAttribute,
		"获取HTML":   builtinGetHTML,
		"获取全部文本": builtinGetFullText,
		"元素存在":   builtinElementExists,
		"元素数量":   builtinElementCount,
		"执行脚本":  builtinEvalJS,
		"等待跳转":  builtinWaitNavigation,
		"截图":    builtinScreenshot,
		"设置视口":  builtinSetViewport,
		"等待":    builtinBrowserWait,

		// --- 编码/解码（builtins_encoding.go） ---
		"base64编码":    builtinBase64Encode,
		"base64解码":    builtinBase64Decode,
		"base64url编码": builtinBase64URLEncode,
		"base64url解码": builtinBase64URLDecode,
		"hex编码":      builtinHexEncode,
		"hex解码":      builtinHexDecode,
		"html转义":      builtinHTMLEscape,
		"html反转义":    builtinHTMLUnescape,
		"url编码":      builtinUrlEncode,
		"url解码":      builtinUrlDecode,
		"url路径编码":    builtinUrlPathEncode,
		"url路径解码":    builtinUrlPathDecode,
		"md5":         builtinMD5,
		"sha1":        builtinSHA1,
		"sha256":      builtinSHA256,

		// --- UTF-8 / Unicode / 转义（builtins_utf8.go） ---
		"转义":     builtinEscape,
		"反转义":     builtinUnescape,
		"unicode转义": builtinUnicodeEscape,
		"unicode反转义": builtinUnicodeUnescape,
		"码点转字符":   builtinCodepointToChar,
		"字符转码点":   builtinCharToCodepoint,
		"utf8验证":   builtinUTF8Valid,
		"utf8修复":   builtinUTF8Fix,
		"utf8字节数":  builtinUTF8ByteCount,
		"转编码":     builtinEncodeTo,
		"从编码转换":   builtinDecodeFrom,
		"规范化":     builtinNormalize,
		"全角转半角":   builtinFullToHalf,
		"半角转全角":   builtinHalfToFull,
		"去除不可见":   builtinRemoveInvisible,
		"去除BOM":   builtinRemoveBOM,

		// --- BDD 测试框架（builtins_bdd.go） ---
		"测试套件":   builtinTestSuite,
		"测试用例":   builtinTestCase,
		"测试前":    builtinBeforeEach,
		"测试后":    builtinAfterEach,
		"测试跳过":   builtinTestSkip,
		"断言相等":   builtinAssertEqual,
		"断言不相等":  builtinAssertNotEqual,
		"断言为真":   builtinAssertTrue,
		"断言为假":   builtinAssertFalse,
		"断言为空":   builtinAssertNull,
		"断言非空":   builtinAssertNotNull,
		"断言包含":   builtinAssertContains,
		"断言大于":   builtinAssertGreater,
		"断言小于":   builtinAssertLess,
		"断言大于等于": builtinAssertGreaterEqual,
		"断言小于等于": builtinAssertLessEqual,
		"断言错误":   builtinAssertError,
		"断言无错误":  builtinAssertNoError,
		"运行测试":   builtinRunTests,

		// --- WASM FFI（builtins_ffi.go） ---
		"加载WASM":   builtinLoadWasm,
		"WASM调用":   builtinWasmCall,
		"WASM导出":   builtinWasmExports,
		"WASM函数":   builtinWasmFunc,
		"WASM内存读取": builtinWasmMemRead,
		"WASM内存写入": builtinWasmMemWrite,
		"WASM内存大小": builtinWasmMemSize,
		"WASM关闭":   builtinWasmClose,

		// --- C 库 FFI（builtins_cffi.go） ---
		"加载C库":   builtinLoadCLib,
		"C函数声明": builtinCFuncDecl,
		"C调用":     builtinCCall,
	}

	// 首次调用时初始化全局 Builtin 池（仅执行一次）
	object.InitBuiltinPool(builtins)

	// 从池中获取已创建的 Builtin 对象，零分配复用
	for name := range builtins {
		if b := object.GetBuiltin(name); b != nil {
			env.Set(name, b)
		}
	}
}

// ==================== 通用内置函数 ====================

// 打印(...) → 空 — 打印参数（空格分隔）
func builtinPrint(args ...object.Object) object.Object {
	var parts []string
	for _, arg := range args {
		parts = append(parts, arg.Inspect())
	}
	fmt.Println(strings.Join(parts, " "))
	return NULL
}

// 长度(字符串|数组) → 整数 — 获取长度
func builtinLen(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("长度() 期望 1 个参数，得到 %d 个", len(args))
	}

	switch arg := args[0].(type) {
	case *object.String:
		return &object.Integer{Value: int64(len([]rune(arg.Value)))}
	case *object.Array:
		return &object.Integer{Value: int64(len(arg.Elements))}
	default:
		return newError("长度() 不支持类型: %s", arg.Type())
	}
}

// 类型(值) → 字符串 — 获取类型名称
func builtinType(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("类型() 期望 1 个参数，得到 %d 个", len(args))
	}
	return &object.String{Value: string(args[0].Type())}
}

// 字符串(值) → 字符串 — 转换为字符串
func builtinStr(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("字符串() 期望 1 个参数，得到 %d 个", len(args))
	}
	return &object.String{Value: args[0].Inspect()}
}

// 错误码(错误) → 字符串：错误码，如 "E202"
func builtinErrorCode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("错误码() 期望 1 个参数，得到 %d 个", len(args))
	}
	err, ok := args[0].(*object.Error)
	if !ok {
		return newError("错误码() 期望错误对象，得到 %s", args[0].Type())
	}
	return &object.String{Value: err.Code}
}

// 错误信息(错误) → 字符串：错误消息文本
func builtinErrorMessage(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("错误信息() 期望 1 个参数，得到 %d 个", len(args))
	}
	err, ok := args[0].(*object.Error)
	if !ok {
		return newError("错误信息() 期望错误对象，得到 %s", args[0].Type())
	}
	return &object.String{Value: err.Message}
}

// 错误行(错误) → 整数：错误所在行号（0 表示未知）
func builtinErrorLine(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("错误行() 期望 1 个参数，得到 %d 个", len(args))
	}
	err, ok := args[0].(*object.Error)
	if !ok {
		return newError("错误行() 期望错误对象，得到 %s", args[0].Type())
	}
	return &object.Integer{Value: int64(err.Line)}
}

// 整数(值) → 整数 — 转换为整数
func builtinInt(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("整数() 期望 1 个参数，得到 %d 个", len(args))
	}
	switch arg := args[0].(type) {
	case *object.Integer:
		return arg
	case *object.Float:
		return &object.Integer{Value: int64(arg.Value)}
	case *object.String:
		var n int64
		_, err := fmt.Sscanf(arg.Value, "%d", &n)
		if err != nil {
			return newError("无法将 %q 转换为整数", arg.Value)
		}
		return &object.Integer{Value: n}
	case *object.Boolean:
		if arg.Value {
			return &object.Integer{Value: 1}
		}
		return &object.Integer{Value: 0}
	default:
		return newError("整数() 不支持类型: %s", arg.Type())
	}
}

// 浮点数(值) → 浮点数 — 转换为浮点数
func builtinFloat(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("浮点数() 期望 1 个参数，得到 %d 个", len(args))
	}
	switch arg := args[0].(type) {
	case *object.Float:
		return arg
	case *object.Integer:
		return &object.Float{Value: float64(arg.Value)}
	case *object.String:
		var f float64
		_, err := fmt.Sscanf(arg.Value, "%f", &f)
		if err != nil {
			return newError("无法将 %q 转换为浮点数", arg.Value)
		}
		return &object.Float{Value: f}
	case *object.Boolean:
		if arg.Value {
			return &object.Float{Value: 1.0}
		}
		return &object.Float{Value: 0.0}
	default:
		return newError("浮点数() 不支持类型: %s", arg.Type())
	}
}

// builtinKeys 键(哈希) → 数组（按插入顺序返回键名）
func builtinKeys(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("键() 期望 1 个参数，得到 %d 个", len(args))
	}
	h, ok := args[0].(*object.Hash)
	if !ok {
		return newError("键() 参数必须是哈希，得到 %s", args[0].Type())
	}
	elems := make([]object.Object, 0, len(h.Keys))
	for _, hk := range h.Keys {
		if pair, ok := h.Pairs[hk]; ok {
			elems = append(elems, pair.Key)
		}
	}
	return &object.Array{Elements: elems}
}

// builtinHashGet 取值(哈希, 键名, 默认值?) → 值；避免 . 与 [] 的边界差异
func builtinHashGet(args ...object.Object) object.Object {
	if len(args) < 2 || len(args) > 3 {
		return newError("取值() 期望 2 或 3 个参数，得到 %d 个", len(args))
	}
	h, ok := args[0].(*object.Hash)
	if !ok {
		return newError("取值() 第一个参数必须是哈希，得到 %s", args[0].Type())
	}
	key, ok := args[1].(*object.String)
	if !ok {
		return newError("取值() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	hk := (&object.String{Value: key.Value}).HashKey()
	if pair, ok := h.Pairs[hk]; ok {
		return pair.Value
	}
	if len(args) == 3 {
		return args[2]
	}
	return &object.Null{}
}
