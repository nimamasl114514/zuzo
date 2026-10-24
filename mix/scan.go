package mix

import (
	zast "zuzo/ast"
	"zuzo/lexer"
	ztoken "zuzo/token"

	pyast "zuzo/py/ast"
	pylexer "zuzo/py/lexer"
	pytoken "zuzo/py/token"
)

// 名字扫描（性能关键路径）
//
// 惰性同步只需要两类名字：
//   - defs：本块顶层定义的名字（登记版本，供对方预取）；
//   - refs：本块源码里出现的标识符（判断是否需要从对方拉取）。
//
// defs 走顶层 AST（只遍历语句一层，O(语句数)）；
// refs 走词法重扫（O(源码长度)，比反射遍历 AST 快一个量级，且不怕节点类型遗漏）。
// refs 略带冗余（属性名也会被收集），冗余只会导致一次多余的名字比对，语义无影响。

// scanZuzo 收集 zuzo 块的定义名与引用名
func scanZuzo(src string, prog *zast.Program) (refs, defs map[string]bool) {
	defs = map[string]bool{}
	if prog != nil {
		for _, s := range prog.Statements {
			switch x := s.(type) {
			case *zast.LetStatement:
				if x.Name != nil {
					defs[x.Name.Value] = true
				}
			case *zast.ExpressionStatement:
				if ae, ok := x.Expression.(*zast.AssignExpression); ok && ae.Name != nil {
					defs[ae.Name.Value] = true
				}
				if fl, ok := x.Expression.(*zast.FunctionLiteral); ok && fl.Name != "" {
					defs[fl.Name] = true
				}
			}
		}
	}
	return zuzoIdents(src), defs
}

// scanPy 收集 Python 块的定义名与引用名
func scanPy(src string, m *pyast.Module) (refs, defs map[string]bool) {
	defs = map[string]bool{}
	if m != nil {
		for _, s := range m.Body {
			switch x := s.(type) {
			case *pyast.Assign:
				pyTargetNames(x.Targets, defs)
			case *pyast.AugAssign:
				pyTargetNames([]pyast.Expr{x.Target}, defs)
			case *pyast.AnnAssign:
				pyTargetNames([]pyast.Expr{x.Target}, defs)
			case *pyast.FuncDef:
				defs[x.Name] = true
			case *pyast.ClassDef:
				defs[x.Name] = true
			case *pyast.For:
				pyTargetNames([]pyast.Expr{x.Target}, defs)
			}
		}
	}
	return pyIdents(src), defs
}

// zuzoIdents 词法级收集 zuzo 源码中的标识符
func zuzoIdents(src string) map[string]bool {
	ids := map[string]bool{}
	l := lexer.New(src)
	for {
		t := l.NextToken()
		if t.Type == ztoken.EOF {
			break
		}
		if t.Type == ztoken.IDENT && t.Literal != "" {
			ids[t.Literal] = true
		}
	}
	return ids
}

// pyIdents 词法级收集 Python 源码中的标识符
func pyIdents(src string) map[string]bool {
	ids := map[string]bool{}
	l := pylexer.New(src)
	for _, t := range l.Tokens() {
		if t.Type == pytoken.NAME && t.Literal != "" {
			ids[t.Literal] = true
		}
	}
	return ids
}

// pyTargetNames 从赋值目标表达式集合中提取变量名
func pyTargetNames(targets []pyast.Expr, defs map[string]bool) {
	for _, t := range targets {
		collectPyNames(t, defs)
	}
}

// collectPyNames 递归提取目标表达式中的名字（支持 a / a, (b, c) / [a, b] / *rest）
func collectPyNames(e pyast.Expr, defs map[string]bool) {
	switch x := e.(type) {
	case *pyast.Name:
		if x != nil {
			defs[x.Id] = true
		}
	case *pyast.Tuple:
		for _, it := range x.Elements {
			collectPyNames(it, defs)
		}
	case *pyast.List:
		for _, it := range x.Elements {
			collectPyNames(it, defs)
		}
	default:
		// 属性 / 下标目标（a.b = 1 / a[0] = 1）不定义新的全局名
	}
}