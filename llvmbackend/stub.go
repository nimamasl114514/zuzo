package llvmbackend

// 本文件是 codegen 的占位实现，子代理按 cbackend/{expr,stmt,builtin}.go 平移后替换。
// 约定（与 cbackend 同构）：
//   genExpr  求值一个表达式，返回指向 %ZVal 槽位的 Value；
//   genStmt  发射一条语句的全部 IR；
//   genBuiltinCall / genTextBuiltin 命中内置映射时返回 (值, true, nil)。

import (
	"fmt"

	"zuzo/ast"
)

func (g *Gen) genExpr(e ast.Expression) (*Value, error) {
	return nil, fmt.Errorf("LLVM 后端：表达式尚未实现")
}

func (g *Gen) genStmt(s ast.Statement) error {
	return fmt.Errorf("LLVM 后端：语句尚未实现")
}

func (g *Gen) genBuiltinCall(name string, n *ast.CallExpression) (*Value, bool, error) {
	return nil, false, nil
}

func (g *Gen) genTextBuiltin(name string, n *ast.CallExpression) (*Value, bool, error) {
	return nil, false, nil
}

func (g *Gen) checkUnsupported(name string, line, col int) error { return nil }
