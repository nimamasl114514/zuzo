package cbackend

import (
	"fmt"
	"sort"

	"zuzo/ast"
)

// fnInfo 一个 zuzo 函数（含嵌套闭包）的编译期信息
type fnInfo struct {
	Name     string // zuzo 名（匿名为空）
	CName    string // C 函数名
	Node     *ast.FunctionLiteral
	Parent   *fnInfo
	Children []*fnInfo
	Locals   map[string]bool // 参数 + 直接声明的局部变量
	Free     map[string]bool // 引用但未在本函数声明的名字（需要上传或捕获）
	Captures []string        // 实际从父函数捕获的变量（按名字排序，稳定）
	EnvType  string          // 生成的 env 结构体名（有捕获时非空）
	IsClosure bool           // 是否作为值被使用（匿名 / 被返回 / 被赋值）
}

// analysis 全程序闭包分析结果
type analysis struct {
	Roots []*fnInfo
	All   []*fnInfo
	byNode map[*ast.FunctionLiteral]*fnInfo
	// boxed 需要在父函数里装箱的变量名（按父函数索引）
	Boxed map[*fnInfo]map[string]bool
}

// analyzeProgram 收集全部函数并做自由变量分析
func analyzeProgram(program *ast.Program) *analysis {
	a := &analysis{
		byNode: map[*ast.FunctionLiteral]*fnInfo{},
		Boxed:  map[*fnInfo]map[string]bool{},
	}
	var visitStmts func(stmts []ast.Statement, parent *fnInfo)
	var visitExpr func(e ast.Expression, parent *fnInfo)

	visitExpr = func(e ast.Expression, parent *fnInfo) {
		if e == nil {
			return
		}
		switch n := e.(type) {
		case *ast.FunctionLiteral:
			fi := a.buildFn(n, parent)
			if parent != nil {
				parent.Children = append(parent.Children, fi)
			} else {
				a.Roots = append(a.Roots, fi)
			}
			// 递归处理函数体里的嵌套函数
			visitStmts(n.Body.Statements, fi)
		case *ast.InfixExpression:
			visitExpr(n.Left, parent)
			visitExpr(n.Right, parent)
		case *ast.PrefixExpression:
			visitExpr(n.Right, parent)
		case *ast.IfExpression:
			visitExpr(n.Condition, parent)
			visitStmts(n.Consequence.Statements, parent)
			if n.Alternative != nil {
				visitStmts(n.Alternative.Statements, parent)
			}
		case *ast.CallExpression:
			visitExpr(n.Function, parent)
			for _, arg := range n.Arguments {
				visitExpr(arg, parent)
			}
			for _, na := range n.NamedArgs {
				visitExpr(na.Value, parent)
			}
		case *ast.ArrayLiteral:
			for _, el := range n.Elements {
				visitExpr(el, parent)
			}
		case *ast.HashLiteral:
			for k, v := range n.Pairs {
				visitExpr(k, parent)
				visitExpr(v, parent)
			}
		case *ast.IndexExpression:
			visitExpr(n.Left, parent)
			visitExpr(n.Index, parent)
		case *ast.AssignExpression:
			if n.IndexTarget != nil {
				visitExpr(n.IndexTarget, parent)
			}
			visitExpr(n.Value, parent)
		case *ast.FStringLiteral:
			for _, p := range n.Parts {
				visitExpr(p, parent)
			}
		}
	}

	visitStmts = func(stmts []ast.Statement, parent *fnInfo) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ExpressionStatement:
				visitExpr(s.Expression, parent)
			case *ast.LetStatement:
				visitExpr(s.Value, parent)
			case *ast.ReturnStatement:
				visitExpr(s.ReturnValue, parent)
			case *ast.WhileStatement:
				visitExpr(s.Condition, parent)
				if s.Body != nil {
					visitStmts(s.Body.Statements, parent)
				}
			case *ast.ForStatement:
				visitExpr(s.Init, parent)
				visitExpr(s.Condition, parent)
				visitExpr(s.Update, parent)
				if s.Body != nil {
					visitStmts(s.Body.Statements, parent)
				}
			case *ast.ForInStatement:
				visitExpr(s.Iterable, parent)
				visitExpr(s.EndExpr, parent)
				visitExpr(s.Guard, parent)
				if s.Body != nil {
					visitStmts(s.Body.Statements, parent)
				}
			case *ast.BlockStatement:
				visitStmts(s.Statements, parent)
			}
		}
	}

	visitStmts(program.Statements, nil)

	// 计算捕获关系：child.Free ∩ parent.Locals = child.Captures
	for _, fi := range a.All {
		if fi.Parent == nil {
			continue
		}
		for name := range fi.Free {
			if fi.Parent.Locals[name] {
				fi.Captures = append(fi.Captures, name)
				if a.Boxed[fi.Parent] == nil {
					a.Boxed[fi.Parent] = map[string]bool{}
				}
				a.Boxed[fi.Parent][name] = true
			}
		}
		sort.Strings(fi.Captures)
		if len(fi.Captures) > 0 {
			fi.EnvType = "ZEnv_" + fi.CName
			fi.IsClosure = true
		}
	}
	// 匿名函数一律按闭包处理（即使无捕获，也需要 env 位）
	for _, fi := range a.All {
		if fi.Name == "" {
			fi.IsClosure = true
			if fi.EnvType == "" {
				fi.EnvType = "ZEnv_" + fi.CName
			}
		}
	}
	return a
}

// buildFn 计算一个函数的 locals / free
func (a *analysis) buildFn(n *ast.FunctionLiteral, parent *fnInfo) *fnInfo {
	fi := &fnInfo{Name: n.Name, Node: n, Parent: parent, Locals: map[string]bool{}, Free: map[string]bool{}}
	fi.CName = cFuncName(n.Name)
	if n.Name == "" {
		fi.CName = fmt.Sprintf("zf_anon_%d", len(a.All)+1)
	}
	a.All = append(a.All, fi)
	a.byNode[n] = fi

	for _, p := range n.Parameters {
		fi.Locals[p.Value] = true
	}
	// 收集本层声明的局部变量（不进入嵌套函数体）
	var collectLocals func(stmts []ast.Statement)
	collectLocals = func(stmts []ast.Statement) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.LetStatement:
				fi.Locals[s.Name.Value] = true
			case *ast.WhileStatement:
				if s.Body != nil {
					collectLocals(s.Body.Statements)
				}
			case *ast.ForStatement:
				if s.Body != nil {
					collectLocals(s.Body.Statements)
				}
			case *ast.ForInStatement:
				fi.Locals[s.VarName] = true
				if s.IndexName != "" {
					fi.Locals[s.IndexName] = true
				}
				if s.Body != nil {
					collectLocals(s.Body.Statements)
				}
			case *ast.BlockStatement:
				collectLocals(s.Statements)
			}
		}
	}
	collectLocals(n.Body.Statements)

	// 收集引用（不进入嵌套函数体：嵌套函数的 free 由它自己算，之后再上传）
	var used map[string]bool = map[string]bool{}
	var visitExpr func(e ast.Expression)
	var visitStmts func(stmts []ast.Statement)
	visitExpr = func(e ast.Expression) {
		if e == nil {
			return
		}
		switch n2 := e.(type) {
		case *ast.Identifier:
			used[n2.Value] = true
		case *ast.FunctionLiteral:
			return // 嵌套函数不进入
		case *ast.InfixExpression:
			visitExpr(n2.Left)
			visitExpr(n2.Right)
		case *ast.PrefixExpression:
			visitExpr(n2.Right)
		case *ast.IfExpression:
			visitExpr(n2.Condition)
			visitStmts(n2.Consequence.Statements)
			if n2.Alternative != nil {
				visitStmts(n2.Alternative.Statements)
			}
		case *ast.CallExpression:
			visitExpr(n2.Function)
			for _, arg := range n2.Arguments {
				visitExpr(arg)
			}
		case *ast.ArrayLiteral:
			for _, el := range n2.Elements {
				visitExpr(el)
			}
		case *ast.HashLiteral:
			for k, v := range n2.Pairs {
				visitExpr(k)
				visitExpr(v)
			}
		case *ast.IndexExpression:
			visitExpr(n2.Left)
			visitExpr(n2.Index)
		case *ast.AssignExpression:
			if n2.Name != nil {
				used[n2.Name.Value] = true
			}
			if n2.IndexTarget != nil {
				visitExpr(n2.IndexTarget)
			}
			visitExpr(n2.Value)
		case *ast.FStringLiteral:
			for _, p := range n2.Parts {
				visitExpr(p)
			}
		}
	}
	visitStmts = func(stmts []ast.Statement) {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ExpressionStatement:
				visitExpr(s.Expression)
			case *ast.LetStatement:
				visitExpr(s.Value)
			case *ast.ReturnStatement:
				visitExpr(s.ReturnValue)
			case *ast.WhileStatement:
				visitExpr(s.Condition)
				if s.Body != nil {
					visitStmts(s.Body.Statements)
				}
			case *ast.ForStatement:
				visitExpr(s.Init)
				visitExpr(s.Condition)
				visitExpr(s.Update)
				if s.Body != nil {
					visitStmts(s.Body.Statements)
				}
			case *ast.ForInStatement:
				visitExpr(s.Iterable)
				visitExpr(s.EndExpr)
				visitExpr(s.Guard)
				if s.Body != nil {
					visitStmts(s.Body.Statements)
				}
			}
		}
	}
	visitStmts(n.Body.Statements)

	for name := range used {
		if !fi.Locals[name] {
			fi.Free[name] = true
		}
	}
	return fi
}
