package infer

import (
	"fmt"
	"reflect"
	"strings"

	"zuzo/ast"
	"zuzo/diagnostic"
)

// ============================================================
// 第一部分：类型体系
// ============================================================

// 类型错误码常量（与 diagnostic/errorcode.go 对应）
const (
	ErrCodeTypeMismatch   = "E101" // 类型不匹配
	ErrCodeUndefinedVar   = "E102" // 未定义变量
	ErrCodeArgCount       = "E103" // 参数数量不匹配
	ErrCodeNotCallable    = "E104" // 不可调用
	ErrCodeBranchMismatch = "E105" // 分支类型不一致
	ErrCodeArrayMixed     = "E106" // 数组元素类型不一致
)

// Type 类型系统的核心接口
type Type interface {
	String() string
	// resolved 返回最终类型（解析类型变量链）
	resolved() Type
}

// TypePrimitive 基本类型：整数、浮点数、字符串、布尔、空
type TypePrimitive struct {
	Name string
}

func (t TypePrimitive) String() string { return t.Name }
func (t TypePrimitive) resolved() Type { return t }

// 预定义基本类型，避免重复创建
var (
	TInt   = TypePrimitive{"整数"}
	TFloat = TypePrimitive{"浮点数"}
	TStr   = TypePrimitive{"字符串"}
	TBool  = TypePrimitive{"布尔"}
	TNull  = TypePrimitive{"空"}
	TAny   = TypePrimitive{"任意"} // 无法推断时的兜底类型
	THash  = TypePrimitive{"哈希"} // 哈希表类型（值类型为任意）
)

// TypeVar 类型变量：推断过程中的未知类型
type TypeVar struct {
	ID       int
	Resolved Type // 如果已被统一，指向实际类型
}

func (t *TypeVar) String() string {
	r := t.resolved()
	if r == t {
		return fmt.Sprintf("T%d", t.ID)
	}
	return r.String()
}

// 🌟 优化 1：路径压缩 (Path Compression)
// 将 O(N) 的链式查找优化为均摊 O(1)，并彻底杜绝极深递归导致的栈溢出。
func (t *TypeVar) resolved() Type {
	if t.Resolved == nil {
		return t
	}

	// 1. 找到根节点
	root := t.Resolved
	for {
		if rv, ok := root.(*TypeVar); ok && rv.Resolved != nil {
			root = rv.Resolved
		} else {
			break
		}
	}

	// 2. 路径压缩：将路径上的所有 TypeVar 直接指向根节点
	// 注意：不能用 curr != root 比较，因为 root 可能是含 slice 的不可比较类型（如 TypeFunc）
	curr := t
	for curr.Resolved != nil {
		next := curr.Resolved
		curr.Resolved = root // 直接指向根
		if rv, ok := next.(*TypeVar); ok && rv.Resolved != nil {
			curr = rv
		} else {
			break
		}
	}

	return root
}

// TypeFunc 函数类型：参数列表 -> 返回值
// Variadic=true 表示最后一个参数可以重复（可变参数函数，如 打印、格式化、C调用）
type TypeFunc struct {
	Params   []Type
	Return   Type
	Variadic bool // 是否可变参数
}

func (t TypeFunc) String() string {
	ps := make([]string, len(t.Params))
	for i, p := range t.Params {
		ps[i] = p.String()
	}
	if t.Variadic && len(ps) > 0 {
		ps[len(ps)-1] = "..." + ps[len(ps)-1]
	}
	return fmt.Sprintf("函数(%s) -> %s", strings.Join(ps, ", "), t.Return.String())
}
func (t TypeFunc) resolved() Type { return t }

// TypeArray 数组类型
type TypeArray struct {
	Elem Type
}

func (t TypeArray) String() string { return "数组[" + t.Elem.String() + "]" }
func (t TypeArray) resolved() Type { return t }

// TypeChannel 通道类型
type TypeChannel struct {
	Elem Type
}

func (t TypeChannel) String() string { return "通道[" + t.Elem.String() + "]" }
func (t TypeChannel) resolved() Type { return t }

// ============================================================
// 第二部分：约束与推断器
// ============================================================

// Constraint 类型约束：Left 和 Right 必须是同一类型
type Constraint struct {
	Left, Right Type
	Node        ast.Node // 用于错误报告
}

// TypeError 类型错误
type TypeError struct {
	Code    string // 错误码，如 "E101", "E103"
	Message string
	Line    int
	Column  int
}

// String 返回格式化的错误信息（含错误码和位置）
func (e TypeError) String() string {
	if e.Line > 0 {
		return fmt.Sprintf("[%s] 第 %d 行: %s", e.Code, e.Line, e.Message)
	}
	return fmt.Sprintf("[%s] %s", e.Code, e.Message)
}

// Inferencer 类型推断器
type Inferencer struct {
	nextVarID   int
	constraints []Constraint
	errors      []TypeError
	// 类型环境：变量名 -> 类型
	env map[string]Type
	// 记录每个 AST 节点的推断类型
	NodeTypes map[ast.Node]Type
	// 函数返回类型栈：用于嵌套函数的返回类型约束
	retTypeStack []Type
	// 活字诊断报告（可选，设置后类型错误会作为活字加入报告）
	diag *diagnostic.Report
	// SAP：变量槽位映射（变量名 → 槽位索引）
	SlotMap  map[string]int
	nextSlot int
}

// New 创建新的类型推断器
func New() *Inferencer {
	return &Inferencer{
		env:       make(map[string]Type),
		NodeTypes: make(map[ast.Node]Type),
		SlotMap:   make(map[string]int),
	}
}

// TypeOf 返回 AST 节点的推断类型（解析后的最终类型）
func (i *Inferencer) TypeOf(node ast.Node) Type {
	if t, ok := i.NodeTypes[node]; ok {
		return t.resolved()
	}
	return TAny
}

// ParseTypeAnnotation 将类型注解文本转换为 Type
// 支持: 整数, 浮点数, 字符串, 布尔, 空, 任意
func ParseTypeAnnotation(name string) (Type, bool) {
	switch name {
	case "整数":
		return TInt, true
	case "浮点数":
		return TFloat, true
	case "字符串":
		return TStr, true
	case "布尔":
		return TBool, true
	case "空":
		return TNull, true
	case "任意":
		return TAny, true
	default:
		return nil, false
	}
}

func (i *Inferencer) GetSlotMap() map[string]int { return i.SlotMap }
func (i *Inferencer) GetEnv() map[string]Type    { return i.env }
func ResolvedType(t Type) Type                   { return t.resolved() }

func (i *Inferencer) GetNodeType(node ast.Node) Type {
	if t, ok := i.NodeTypes[node]; ok {
		return t.resolved()
	}
	return TAny
}

func (i *Inferencer) SetDiagnostics(d *diagnostic.Report) { i.diag = d }

func (i *Inferencer) newTypeVar() *TypeVar {
	tv := &TypeVar{ID: i.nextVarID}
	i.nextVarID++
	return tv
}

func (i *Inferencer) addConstraint(a, b Type, node ast.Node) {
	i.constraints = append(i.constraints, Constraint{Left: a, Right: b, Node: node})
}

func (i *Inferencer) errorf(node ast.Node, format string, args ...any) {
	line, col := getNodePosition(node)
	msg := fmt.Sprintf(format, args...)
	i.errors = append(i.errors, TypeError{Code: ErrCodeTypeMismatch, Message: msg, Line: line, Column: col})
}

// errorfWithCode 生成带指定错误码的类型错误
func (i *Inferencer) errorfWithCode(node ast.Node, code string, format string, args ...any) {
	line, col := getNodePosition(node)
	msg := fmt.Sprintf(format, args...)
	i.errors = append(i.errors, TypeError{Code: code, Message: msg, Line: line, Column: col})
}

// 🌟 优化 2：反射兜底获取行号，彻底消灭 50 行的 switch 语句。
// 未来 ast 包新增任何节点类型，都能自动提取 Token.Line，免疫遗漏。
func getNodePosition(node ast.Node) (int, int) {
	if node == nil {
		return 0, 0
	}
	v := reflect.ValueOf(node)
	// 解包指针和接口
	for v.Kind() == reflect.Ptr || v.Kind() == reflect.Interface {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return 0, 0
	}

	tokenField := v.FieldByName("Token")
	if !tokenField.IsValid() {
		return 0, 0
	}

	line := tokenField.FieldByName("Line")
	col := tokenField.FieldByName("Column")
	if line.IsValid() && col.IsValid() {
		return int(line.Int()), int(col.Int())
	}
	return 0, 0
}

func (i *Inferencer) Errors() []TypeError { return i.errors }

// ============================================================
// 第三部分：约束生成（遍历 AST）
// ============================================================

func (i *Inferencer) Infer(program *ast.Program) {
	for _, stmt := range program.Statements {
		i.inferStmt(stmt)
	}
	i.solve()

	if i.diag != nil {
		for _, te := range i.errors {
			code := te.Code
			if code == "" {
				code = diagnostic.ErrTypeMismatch.Code
			}
			if rootCause := i.diag.FindRootOnLine(te.Line); rootCause != nil {
				i.diag.AddType(rootCause, code, te.Line, te.Column, te.Message, "请先修复上方的根本原因")
			} else {
				i.diag.AddRoot(code, te.Line, te.Column, te.Message, "")
			}
		}
	}
}

func (i *Inferencer) inferStmt(stmt ast.Statement) Type {
	switch s := stmt.(type) {
	case *ast.ExpressionStatement:
		if s.Expression != nil {
			return i.inferExpr(s.Expression)
		}
	case *ast.LetStatement:
		valType := i.inferExpr(s.Value)
		if s.Name != nil {
			// 如果有类型注解，添加约束
			if s.TypeAnnot != "" {
				if annotType, ok := ParseTypeAnnotation(s.TypeAnnot); ok {
					i.addConstraint(valType, annotType, s)
				} else {
					i.errorf(s, "未知类型注解: %s", s.TypeAnnot)
				}
			}
			i.env[s.Name.Value] = valType
			if _, exists := i.SlotMap[s.Name.Value]; !exists {
				i.SlotMap[s.Name.Value] = i.nextSlot
				i.nextSlot++
			}
		}
		return valType
	case *ast.ReturnStatement:
		if s.ReturnValue != nil {
			retType := i.inferExpr(s.ReturnValue)
			if len(i.retTypeStack) > 0 {
				i.addConstraint(retType, i.retTypeStack[len(i.retTypeStack)-1], s)
			}
			return retType
		}
		return TNull
	case *ast.WhileStatement:
		condType := i.inferExpr(s.Condition)
		i.addConstraint(condType, TBool, s.Condition)
		if s.Body != nil {
			i.inferBlock(s.Body)
		}
		return TNull
	case *ast.ForStatement:
		if s.Init != nil {
			i.inferExpr(s.Init)
		}
		if s.Condition != nil {
			condType := i.inferExpr(s.Condition)
			i.addConstraint(condType, TBool, s.Condition)
		}
		if s.Update != nil {
			i.inferExpr(s.Update)
		}
		if s.Body != nil {
			i.inferBlock(s.Body)
		}
		return TNull
	case *ast.ImportStatement:
		return TNull
	case *ast.BlockStatement:
		return i.inferBlock(s)

	case *ast.ForInStatement:
		// 范围循环: 对于 i 在 start 到 end { ... }
		if s.EndExpr != nil {
			startT := i.inferExpr(s.Iterable)
			endT := i.inferExpr(s.EndExpr)
			i.addConstraint(startT, TInt, s.Iterable)
			i.addConstraint(endT, TInt, s.EndExpr)
			i.env[s.VarName] = TInt
		} else {
			// 集合遍历: 对于 x 在 collection { ... }
			collT := i.inferExpr(s.Iterable)
			var elemT Type = i.newTypeVar()
			switch lt := collT.resolved().(type) {
			case TypeArray:
				elemT = lt.Elem
			case TypePrimitive:
				if lt == THash {
					elemT = TAny
				} else if lt == TStr {
					elemT = TStr // 字符串迭代产出字符
				}
			}
			i.env[s.VarName] = elemT
			if s.IndexName != "" {
				if _, exists := i.SlotMap[s.IndexName]; !exists {
					i.SlotMap[s.IndexName] = i.nextSlot
					i.nextSlot++
				}
			}
		}
		// 注册循环变量槽位
		if _, exists := i.SlotMap[s.VarName]; !exists {
			i.SlotMap[s.VarName] = i.nextSlot
			i.nextSlot++
		}
		// 条件过滤
		if s.Guard != nil {
			guardT := i.inferExpr(s.Guard)
			i.addConstraint(guardT, TBool, s.Guard)
		}
		if s.Body != nil {
			i.inferBlock(s.Body)
		}
		return TNull

	case *ast.TryCatchStatement:
		if s.Body != nil {
			i.inferBlock(s.Body)
		}
		if s.Handler != nil {
			// 错误变量类型为字符串
			if s.ErrVar != "" {
				i.env[s.ErrVar] = TStr
				if _, exists := i.SlotMap[s.ErrVar]; !exists {
					i.SlotMap[s.ErrVar] = i.nextSlot
					i.nextSlot++
				}
			}
			i.inferBlock(s.Handler)
		}
		return TNull

	case *ast.DefineStatement:
		for _, f := range s.Fields {
			i.inferExpr(f.Value)
		}
		return TNull
	}
	return TAny
}

func (i *Inferencer) inferBlock(block *ast.BlockStatement) Type {
	var lastType Type = TNull
	if block == nil {
		return lastType
	}
	for _, stmt := range block.Statements {
		lastType = i.inferStmt(stmt)
	}
	i.NodeTypes[block] = lastType
	return lastType
}

func (i *Inferencer) inferExpr(expr ast.Expression) Type {
	if expr == nil {
		return TNull
	}

	var t Type
	switch n := expr.(type) {
	case *ast.IntegerLiteral:
		t = TInt
	case *ast.FloatLiteral:
		t = TFloat
	case *ast.StringLiteral:
		t = TStr
	case *ast.FStringLiteral:
		for _, part := range n.Parts {
			if _, ok := part.(*ast.StringLiteral); !ok {
				i.inferExpr(part)
			}
		}
		t = TStr
	case *ast.BooleanLiteral:
		t = TBool
	case *ast.NullLiteral:
		t = TNull

	case *ast.Identifier:
		if knownType, ok := i.env[n.Value]; ok {
			t = knownType
		} else if bt, ok := i.builtinType(n.Value); ok {
			t = bt
		} else {
			tv := i.newTypeVar()
			i.env[n.Value] = tv
			t = tv
		}

	case *ast.PrefixExpression:
		rightType := i.inferExpr(n.Right)
		switch n.Operator {
		case "-":
			resultT := i.newTypeVar()
			i.addConstraint(rightType, resultT, n)
			t = resultT
		case "!", "非":
			i.addConstraint(rightType, TBool, n)
			t = TBool
		default:
			t = i.newTypeVar()
		}

	case *ast.InfixExpression:
		leftType := i.inferExpr(n.Left)
		rightType := i.inferExpr(n.Right)

		switch n.Operator {
		case "+", "-", "*", "/", "%":
			i.addConstraint(leftType, rightType, n)
			lt := leftType.resolved()
			rt := rightType.resolved()
			if lt == TFloat || rt == TFloat {
				t = TFloat
			} else if lt == TInt && rt == TInt {
				t = TInt
			} else if lt == TStr && rt == TStr {
				t = TStr
			} else {
				resultT := i.newTypeVar()
				i.addConstraint(leftType, resultT, n)
				t = resultT
			}
		case "<", ">", "<=", ">=", "==", "!=":
			i.addConstraint(leftType, rightType, n)
			t = TBool
		case "并且", "或者":
			i.addConstraint(leftType, TBool, n)
			i.addConstraint(rightType, TBool, n)
			t = TBool
		case "否则":
			// fallback 运算符: 左值非空则返回左值，否则返回右值
			// 结果类型是左值和右值的联合
			resultT := i.newTypeVar()
			i.addConstraint(leftType, resultT, n)
			i.addConstraint(rightType, resultT, n)
			t = resultT
		default:
			t = i.newTypeVar()
		}

	case *ast.AssignExpression:
		valType := i.inferExpr(n.Value)
		if n.Name != nil {
			if existing, ok := i.env[n.Name.Value]; ok {
				i.addConstraint(existing, valType, n)
			}
			i.env[n.Name.Value] = valType
			if _, exists := i.SlotMap[n.Name.Value]; !exists {
				i.SlotMap[n.Name.Value] = i.nextSlot
				i.nextSlot++
			}
		} else if n.IndexTarget != nil {
			// 数组/哈希元素赋值: arr[i] = val
			i.inferExpr(n.IndexTarget)
		}
		t = valType

	case *ast.IfExpression:
		condType := i.inferExpr(n.Condition)
		i.addConstraint(condType, TBool, n.Condition)
		thenType := i.inferBlock(n.Consequence)
		if n.Alternative != nil {
			i.inferBlock(n.Alternative)
			t = TAny
		} else {
			t = thenType
		}

	case *ast.FunctionLiteral:
		// 🌟 优化 3：修复致命的作用域污染 Bug (Scope Leakage)
		// 使用深拷贝快照保存外层环境，确保函数内部的推断绝对不会污染外部变量。
		envSnapshot := make(map[string]Type, len(i.env))
		for k, v := range i.env {
			envSnapshot[k] = v
		}

		paramTypes := make([]Type, len(n.Parameters))
		for idx, param := range n.Parameters {
			var pt Type
			// 如果有参数类型注解，使用注解类型
			if param.TypeAnnot != "" {
				if annotType, ok := ParseTypeAnnotation(param.TypeAnnot); ok {
					pt = annotType
				} else {
					i.errorf(param, "未知参数类型注解: %s", param.TypeAnnot)
					pt = i.newTypeVar()
				}
			} else {
				pt = i.newTypeVar()
			}
			i.env[param.Value] = pt
			paramTypes[idx] = pt
			if _, exists := i.SlotMap[param.Value]; !exists {
				i.SlotMap[param.Value] = i.nextSlot
				i.nextSlot++
			}
		}

		// 返回类型：有注解则使用注解，否则用 TypeVar
		var retTypeVar Type
		if n.RetTypeAnnot != "" {
			if annotType, ok := ParseTypeAnnotation(n.RetTypeAnnot); ok {
				retTypeVar = annotType
			} else {
				i.errorf(n, "未知返回类型注解: %s", n.RetTypeAnnot)
				retTypeVar = i.newTypeVar()
			}
		} else {
			retTypeVar = i.newTypeVar()
		}
		fnType := TypeFunc{Params: paramTypes, Return: retTypeVar}
		if n.Name != "" {
			i.env[n.Name] = fnType
		}

		i.retTypeStack = append(i.retTypeStack, retTypeVar)
		bodyType := i.inferBlock(n.Body)
		i.retTypeStack = i.retTypeStack[:len(i.retTypeStack)-1]

		i.addConstraint(bodyType, retTypeVar, n)

		// 恢复外层环境
		i.env = envSnapshot

		if n.Name != "" {
			i.env[n.Name] = fnType
		}
		t = fnType

	case *ast.CallExpression:
		fnType := i.inferExpr(n.Function)
		argTypes := make([]Type, len(n.Arguments))
		for idx, arg := range n.Arguments {
			argTypes[idx] = i.inferExpr(arg)
		}
		// 具名参数也参与推断
		for _, na := range n.NamedArgs {
			i.inferExpr(na.Value)
		}
		// 检查被调用的对象是否是函数类型
		resolvedFn := fnType.resolved()
		if resolvedFn != TAny {
			if _, isFn := resolvedFn.(TypeFunc); !isFn {
				if _, isVar := resolvedFn.(*TypeVar); !isVar {
					// 获取被调用的函数名（如果有的话）
					fnName := ""
					if ident, ok := n.Function.(*ast.Identifier); ok {
						fnName = ident.Value
					}
					if fnName != "" {
						i.errorfWithCode(n, ErrCodeNotCallable, "「%s」不是函数，无法调用（类型: %s）", fnName, resolvedFn)
					} else {
						i.errorfWithCode(n, ErrCodeNotCallable, "无法调用非函数对象（类型: %s）", resolvedFn)
					}
				}
			}
		}
		retT := i.newTypeVar()
		expectedFn := TypeFunc{Params: argTypes, Return: retT}
		i.addConstraint(fnType, expectedFn, n)
		t = retT

	case *ast.ArrayLiteral:
		elemT := i.newTypeVar()
		for _, elem := range n.Elements {
			elemType := i.inferExpr(elem)
			i.addConstraint(elemT, elemType, n)
		}
		t = TypeArray{Elem: elemT}

	case *ast.HashLiteral:
		for k, v := range n.Pairs {
			i.inferExpr(k)
			i.inferExpr(v)
		}
		t = THash

	case *ast.IndexExpression:
		leftType := i.inferExpr(n.Left)
		indexType := i.inferExpr(n.Index)
		var resultT Type = i.newTypeVar()

		switch lt := leftType.resolved().(type) {
		case TypeArray:
			i.addConstraint(indexType, TInt, n.Index)
			i.addConstraint(resultT, lt.Elem, n)
		case TypePrimitive:
			if lt == THash {
				i.addConstraint(indexType, TStr, n.Index)
				resultT = TAny
			} else if lt == TAny {
				resultT = TAny
			} else {
				arrT := TypeArray{Elem: resultT}
				i.addConstraint(leftType, arrT, n)
				i.addConstraint(indexType, TInt, n.Index)
			}
		default:
			resultT = TAny
		}
		t = resultT

	default:
		t = i.newTypeVar()
	}

	i.NodeTypes[expr] = t
	return t
}

// ============================================================
// 第四部分：联合求解（Unification）
// ============================================================

func (i *Inferencer) solve() {
	for _, c := range i.constraints {
		unify(c.Left, c.Right, &i.errors, c)
	}
}

// 🌟 优化 4：重构 unify 逻辑，修复数值提升与 TypeVar 的冲突
// 强化报错：包含错误码、参数位置、修复建议
func unify(a, b Type, errors *[]TypeError, c Constraint) bool {
	a = a.resolved()
	b = b.resolved()

	if typeEqual(a, b) {
		return true
	}

	// 在 TAny 检查之前，先将 TypeVar 绑定到具体类型
	if av, ok := a.(*TypeVar); ok {
		if occurs(av, b) {
			line, col := getNodePosition(c.Node)
			*errors = append(*errors, TypeError{Code: ErrCodeTypeMismatch, Message: fmt.Sprintf("递归类型: %s 出现在 %s 中", av, b), Line: line, Column: col})
			return false
		}
		av.Resolved = b
		return true
	}
	if bv, ok := b.(*TypeVar); ok {
		if occurs(bv, a) {
			line, col := getNodePosition(c.Node)
			*errors = append(*errors, TypeError{Code: ErrCodeTypeMismatch, Message: fmt.Sprintf("递归类型: %s 出现在 %s 中", bv, a), Line: line, Column: col})
			return false
		}
		bv.Resolved = a
		return true
	}

	if a == TAny || b == TAny {
		return true
	}

	// 处理具体类型匹配
	pa, okA := a.(TypePrimitive)
	pb, okB := b.(TypePrimitive)
	if okA && okB {
		if pa.Name == pb.Name {
			return true
		}
		// 整数和浮点数兼容（隐式类型提升）
		if isNumeric(a) && isNumeric(b) {
			return true
		}
		line, col := getNodePosition(c.Node)
		hint := typeMismatchHint(pa.Name, pb.Name)
		msg := fmt.Sprintf("类型不匹配: 期望「%s」，得到「%s」", pa.Name, pb.Name)
		if hint != "" {
			msg += "；" + hint
		}
		*errors = append(*errors, TypeError{Code: ErrCodeTypeMismatch, Message: msg, Line: line, Column: col})
		return false
	}

	if aa, ok := a.(TypeArray); ok {
		if ab, ok := b.(TypeArray); ok {
			return unify(aa.Elem, ab.Elem, errors, c)
		}
		line, col := getNodePosition(c.Node)
		*errors = append(*errors, TypeError{Code: ErrCodeTypeMismatch, Message: fmt.Sprintf("类型不匹配: 期望「数组」，得到「%s」", b), Line: line, Column: col})
		return false
	}

	if ca, ok := a.(TypeChannel); ok {
		if cb, ok := b.(TypeChannel); ok {
			return unify(ca.Elem, cb.Elem, errors, c)
		}
		line, col := getNodePosition(c.Node)
		*errors = append(*errors, TypeError{Code: ErrCodeTypeMismatch, Message: fmt.Sprintf("类型不匹配: 期望「通道」，得到「%s」", b), Line: line, Column: col})
		return false
	}

	fa, okA := a.(TypeFunc)
	fb, okB := b.(TypeFunc)
	if okA && okB {
		// 可变参数处理：如果任意一方是可变参数，放宽参数数量检查
		if fa.Variadic || fb.Variadic {
			// 计算固定参数数量（去掉可变参数占位的最后一个）
			faFixed := len(fa.Params)
			if fa.Variadic && faFixed > 0 {
				faFixed--
			}
			fbFixed := len(fb.Params)
			if fb.Variadic && fbFixed > 0 {
				fbFixed--
			}

			// 如果可变参数一方的固定参数数量 > 另一方总参数数量，则报错
			if fa.Variadic && !fb.Variadic && faFixed > len(fb.Params) {
				line, col := getNodePosition(c.Node)
				*errors = append(*errors, TypeError{
					Code:    ErrCodeArgCount,
					Message: fmt.Sprintf("参数数量不足: 至少需要 %d 个参数，但只传入了 %d 个", faFixed, len(fb.Params)),
					Line:    line,
					Column:  col,
				})
				return false
			}
			if fb.Variadic && !fa.Variadic && fbFixed > len(fa.Params) {
				line, col := getNodePosition(c.Node)
				*errors = append(*errors, TypeError{
					Code:    ErrCodeArgCount,
					Message: fmt.Sprintf("参数数量不足: 至少需要 %d 个参数，但只传入了 %d 个", fbFixed, len(fa.Params)),
					Line:    line,
					Column:  col,
				})
				return false
			}

			// 统一参数类型：固定部分逐个检查
			ok := true

			// 确定可变参数的元素类型（如果有的话）
			var variadicElemType Type
			if fa.Variadic && len(fa.Params) > 0 {
				variadicElemType = fa.Params[len(fa.Params)-1].resolved()
			} else if fb.Variadic && len(fb.Params) > 0 {
				variadicElemType = fb.Params[len(fb.Params)-1].resolved()
			}

			// 固定参数部分逐个统一
			fixedLen := faFixed
			if fbFixed < fixedLen {
				fixedLen = fbFixed
			}
			for idx := 0; idx < fixedLen; idx++ {
				if !unifyParam(fa.Params[idx], fb.Params[idx], idx, errors, c) {
					ok = false
				}
			}

			// 可变参数部分：超出固定参数的参数都与可变元素类型统一
			if variadicElemType != nil {
				// fa 的可变部分
				for idx := faFixed; idx < len(fa.Params); idx++ {
					if !unifyParam(fa.Params[idx], variadicElemType, idx, errors, c) {
						ok = false
					}
				}
				// fb 的可变部分：
				// 如果 fb 不是可变参数，那么 fb 的所有参数中超出 fa 固定参数的部分都应该与可变元素类型统一
				// 如果 fb 是可变参数，那么 fb 超出 fbFixed 的部分与可变元素类型统一
				fbVariadicStart := fbFixed
				if !fb.Variadic {
					fbVariadicStart = faFixed // 非可变参数方：从可变参数方的固定参数数量开始
				}
				for idx := fbVariadicStart; idx < len(fb.Params); idx++ {
					if !unifyParam(fb.Params[idx], variadicElemType, idx, errors, c) {
						ok = false
					}
				}
			}

			if !unify(fa.Return, fb.Return, errors, c) {
				ok = false
			}
			return ok
		}
		// 非可变参数：严格检查参数数量
		if len(fa.Params) != len(fb.Params) {
			line, col := getNodePosition(c.Node)
			expected := len(fa.Params)
			actual := len(fb.Params)
			var hint string
			if expected > actual {
				hint = fmt.Sprintf("缺少 %d 个参数", expected-actual)
			} else {
				hint = fmt.Sprintf("多出 %d 个参数", actual-expected)
			}
			*errors = append(*errors, TypeError{
				Code:    ErrCodeArgCount,
				Message: fmt.Sprintf("参数数量不匹配: 期望 %d 个参数，得到 %d 个（%s）", expected, actual, hint),
				Line:    line,
				Column:  col,
			})
			return false
		}
		ok := true
		for idx := range fa.Params {
			if !unifyParam(fa.Params[idx], fb.Params[idx], idx, errors, c) {
				ok = false
			}
		}
		if !unify(fa.Return, fb.Return, errors, c) {
			ok = false
		}
		return ok
	}

	line, col := getNodePosition(c.Node)
	*errors = append(*errors, TypeError{Code: ErrCodeTypeMismatch, Message: fmt.Sprintf("类型不兼容: 「%s」与「%s」无法匹配", a, b), Line: line, Column: col})
	return false
}

// unifyParam 统一函数参数类型，在错误信息中包含参数位置（第几个参数）
func unifyParam(a, b Type, paramIdx int, errors *[]TypeError, c Constraint) bool {
	a = a.resolved()
	b = b.resolved()

	if typeEqual(a, b) {
		return true
	}
	if a == TAny || b == TAny {
		return true
	}

	// 如果是 TypeVar，直接绑定（递归调用 unify 处理）
	if _, ok := a.(*TypeVar); ok {
		return unify(a, b, errors, c)
	}
	if _, ok := b.(*TypeVar); ok {
		return unify(a, b, errors, c)
	}

	// 具体类型不匹配：包装错误信息，加上参数位置
	result := unify(a, b, errors, c)
	if !result && len(*errors) > 0 {
		// 修改最后一条错误，加上参数位置前缀
		last := &(*errors)[len(*errors)-1]
		last.Message = fmt.Sprintf("第 %d 个参数: %s", paramIdx+1, last.Message)
	}
	return result
}

// typeMismatchHint 返回类型不匹配时的修复建议
func typeMismatchHint(expected, got string) string {
	switch expected {
	case "整数":
		if got == "浮点数" {
			return "如需整数，请使用 整数(x) 转换"
		}
		if got == "字符串" {
			return "如需从字符串转整数，请使用 整数(x)"
		}
	case "浮点数":
		if got == "整数" {
			return "整数会自动提升为浮点数，通常无需处理"
		}
		if got == "字符串" {
			return "如需从字符串转浮点数，请使用 浮点数(x)"
		}
	case "字符串":
		if got == "整数" || got == "浮点数" {
			return "如需转为字符串，请使用 字符串(x) 或 f\"{x}\""
		}
	case "布尔":
		if got == "整数" {
			return "如需转为布尔，请使用 x != 0"
		}
	}
	return ""
}

func isNumeric(t Type) bool {
	if p, ok := t.(TypePrimitive); ok {
		return p.Name == "整数" || p.Name == "浮点数"
	}
	return false
}

func typeEqual(a, b Type) bool {
	a = a.resolved()
	b = b.resolved()

	if av, ok := a.(*TypeVar); ok {
		if bv, ok := b.(*TypeVar); ok {
			return av == bv
		}
		return false
	}
	if pa, ok := a.(TypePrimitive); ok {
		if pb, ok := b.(TypePrimitive); ok {
			return pa.Name == pb.Name
		}
		return false
	}
	if pa, ok := a.(TypeArray); ok {
		if pb, ok := b.(TypeArray); ok {
			return typeEqual(pa.Elem, pb.Elem)
		}
		return false
	}
	if pa, ok := a.(TypeChannel); ok {
		if pb, ok := b.(TypeChannel); ok {
			return typeEqual(pa.Elem, pb.Elem)
		}
		return false
	}
	if fa, ok := a.(TypeFunc); ok {
		if fb, ok := b.(TypeFunc); ok {
			// 可变参数函数：只要固定参数部分匹配即可
			if fa.Variadic || fb.Variadic {
				minLen := len(fa.Params)
				if fa.Variadic && minLen > 0 {
					minLen--
				}
				if len(fb.Params) < minLen {
					minLen = len(fb.Params)
				}
				for i := 0; i < minLen; i++ {
					if !typeEqual(fa.Params[i], fb.Params[i]) {
						return false
					}
				}
				return typeEqual(fa.Return, fb.Return)
			}
			if len(fa.Params) != len(fb.Params) {
				return false
			}
			for i := range fa.Params {
				if !typeEqual(fa.Params[i], fb.Params[i]) {
					return false
				}
			}
			return typeEqual(fa.Return, fb.Return)
		}
	}
	return false
}

// 🌟 优化 5：补全 occurs 检查，防止 TypeChannel 等容器类型的递归漏洞
func occurs(tv *TypeVar, t Type) bool {
	t = t.resolved()
	if t == tv {
		return true
	}
	switch tt := t.(type) {
	case TypeFunc:
		for _, p := range tt.Params {
			if occurs(tv, p) {
				return true
			}
		}
		return occurs(tv, tt.Return)
	case TypeArray:
		return occurs(tv, tt.Elem)
	case TypeChannel:
		return occurs(tv, tt.Elem)
	}
	return false
}

// ============================================================
// 第五部分：公开 API
// ============================================================

func CheckProgram(program *ast.Program) []TypeError {
	inf := New()
	inf.Infer(program)
	return inf.Errors()
}

func CheckProgramWithDiagnostics(program *ast.Program, diag *diagnostic.Report) []TypeError {
	inf := New()
	inf.SetDiagnostics(diag)
	inf.Infer(program)
	return inf.Errors()
}

func InferType(expr ast.Expression) string {
	inf := New()
	t := inf.inferExpr(expr)
	inf.solve()
	return t.resolved().String()
}
