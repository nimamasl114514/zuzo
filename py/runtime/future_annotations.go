package runtime

// future_annotations.go — PEP 563（from __future__ import annotations）
//
// 语义：模块一旦声明 `from __future__ import annotations`，该模块内所有注解
// 都不得在定义时求值，__annotations__ 存的是「源码字符串」。
//
// 为什么必须有这条：urllib3/exceptions.py 只在 `typing.TYPE_CHECKING` 分支里
// import ConnectionPool，却在运行时注解里引用它（`pool: ConnectionPool`）。
// 若在 def 执行时求值注解，直接 NameError，requests 链就在
// urllib3/exceptions.py:32 断掉（第16轮实测）。
//
// 第16轮的 t30_annotations 要求「注解真的求值成对象」，与本条并不冲突：
// 没有 __future__ 声明的模块照常求值，有声明才走字符串形态（CPython 同口径）。

import (
	"strconv"
	"strings"

	"zuzo/py/ast"
	"zuzo/py/object"
)

// moduleHasFutureAnnotations 扫描模块顶层语句，判断是否 from __future__ import annotations
func moduleHasFutureAnnotations(stmts []ast.Stmt) bool {
	for _, s := range stmts {
		imp, ok := s.(*ast.ImportFrom)
		if !ok || imp.Module != "__future__" {
			continue
		}
		for _, a := range imp.Names {
			if a.Name == "annotations" {
				return true
			}
		}
	}
	return false
}

// evalAnnotation 求值注解；PEP 563 生效时返回源码文本（不做求值）
func (ip *Interp) evalAnnotation(e ast.Expr, env *object.Env) object.Object {
	if e == nil {
		return object.None
	}
	if ip.FutureAnnotations {
		return &object.Str{V: AnnExprText(e)}
	}
	return ip.evalExpr(e, env)
}

// AnnExprText 把注解表达式尽量还原成源码文本（PEP 563 的字符串形态）
func AnnExprText(e ast.Expr) string {
	switch v := e.(type) {
	case nil:
		return ""
	case *ast.Name:
		return v.Id
	case *ast.Attribute:
		return AnnExprText(v.Value) + "." + v.Attr
	case *ast.Subscript:
		return AnnExprText(v.Value) + "[" + AnnExprText(v.Slice) + "]"
	case *ast.Tuple:
		parts := make([]string, 0, len(v.Elements))
		for _, el := range v.Elements {
			parts = append(parts, AnnExprText(el))
		}
		if len(parts) == 1 {
			parts = append(parts, "")
		}
		return "(" + strings.Join(parts, ", ") + ")"
	case *ast.List:
		parts := make([]string, 0, len(v.Elements))
		for _, el := range v.Elements {
			parts = append(parts, AnnExprText(el))
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *ast.String:
		return strconv.Quote(v.Value)
	case *ast.Number:
		return v.Value
	case *ast.BinOp:
		return AnnExprText(v.Left) + " " + v.Op + " " + AnnExprText(v.Right)
	case *ast.UnaryOp:
		return v.Op + AnnExprText(v.Operand)
	case *ast.ListComp:
		return "[...]"
	}
	return "object"
}
