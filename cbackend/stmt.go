package cbackend

import (
	"fmt"

	"zuzo/ast"
)

// genStmt 生成一条语句（自带缩进，不含外层花括号）
func (g *Gen) genStmt(stmt ast.Statement) error {
	switch s := stmt.(type) {
	case *ast.LetStatement:
		return g.genLet(s)

	case *ast.ReturnStatement:
		val, err := g.genExpr(s.ReturnValue)
		if err != nil {
			return err
		}
		g.line("return %s;", val)
		return nil

	case *ast.ExpressionStatement:
		// if 表达式作语句：直接展开成 if 语句，避免临时量
		if ie, ok := s.Expression.(*ast.IfExpression); ok {
			return g.genIfStmt(ie)
		}
		if fl, ok := s.Expression.(*ast.FunctionLiteral); ok && fl.Name != "" {
			return nil // 具名函数已单独生成
		}
		val, err := g.genExpr(s.Expression)
		if err != nil {
			return err
		}
		g.line("(void)%s;", val)
		return nil

	case *ast.BlockStatement:
		g.line("{")
		g.pushScope()
		for _, inner := range s.Statements {
			if err := g.genStmt(inner); err != nil {
				return err
			}
		}
		g.popScope()
		g.line("}")
		return nil

	case *ast.WhileStatement:
		cond, err := g.genExpr(s.Condition)
		if err != nil {
			return err
		}
		g.line("while (z_truthy(%s)) {", cond)
		g.pushScope()
		g.updateLabels = append(g.updateLabels, "")
		for _, inner := range s.Body.Statements {
			if err := g.genStmt(inner); err != nil {
				return err
			}
		}
		g.updateLabels = g.updateLabels[:len(g.updateLabels)-1]
		g.popScope()
		g.line("}")
		return nil

	case *ast.ForStatement:
		return g.genFor(s)

	case *ast.ForInStatement:
		return g.genForIn(s)

	case *ast.BreakStatement:
		g.line("break;")
		return nil

	case *ast.ContinueStatement:
		// C 风格 for 的 update 在循环体末尾，continue 会跳过它 → 跳到 update 标签
		if n := len(g.updateLabels); n > 0 && g.updateLabels[n-1] != "" {
			g.line("goto %s;", g.updateLabels[n-1])
			return nil
		}
		g.line("continue;")
		return nil

	case *ast.ImportStatement:
		g.unsupported(fmt.Sprintf("导入语句（%s）：计划第25轮", s.Path))
		return nil

	case *ast.DefineStatement:
		g.unsupported(fmt.Sprintf("定义语句（%s）：计划第22轮", s.Name))
		return nil

	case *ast.TryCatchStatement:
		return g.genTryCatch(s)
	}
	return fmt.Errorf("不支持的语句: %T", stmt)
}

// genTryCatch 「执行 { } 遇到 err { }」→ setjmp/longjmp
func (g *Gen) genTryCatch(s *ast.TryCatchStatement) error {
	id := g.temp()
	g.line("{")
	g.line("    ZTryFrame %s;", id)
	g.line("    %s.prev = z_try_top;", id)
	g.line("    z_try_top = &%s;", id)
	g.line("    if (setjmp(%s.buf) == 0) {", id)
	g.pushScope()
	if s.Body != nil {
		for _, stmt := range s.Body.Statements {
			if err := g.genStmt(stmt); err != nil {
				return err
			}
		}
	}
	g.popScope()
	g.line("        z_try_top = %s.prev;", id)
	g.line("    } else {")
	g.line("        z_try_top = %s.prev;", id)
	g.pushScope()
	if s.ErrVar != "" {
		g.setVar(s.ErrVar, varSlot{kind: vPlain})
		g.line("        ZVal %s = z_error_take();", cVarName(s.ErrVar))
	}
	if s.Handler != nil {
		for _, stmt := range s.Handler.Statements {
			if err := g.genStmt(stmt); err != nil {
				return err
			}
		}
	}
	g.popScope()
	g.line("    }")
	g.line("}")
	return nil
}

func (g *Gen) genLet(s *ast.LetStatement) error {
	val, err := g.genExpr(s.Value)
	if err != nil {
		return err
	}
	name := s.Name.Value
	if slot, ok := g.lookupVar(name); ok {
		switch slot.kind {
		case vBox:
			g.line("%s->v = %s;", cVarName(name), val)
		case vCapture:
			g.line("zenv->%s->v = %s;", envFieldName(name), val)
		default:
			g.line("%s = %s;", cVarName(name), val)
		}
		return nil
	}
	// 被内层闭包捕获的变量需要装箱（堆上共享）
	if g.boxedBy(g.cur, name) {
		g.setVar(name, varSlot{kind: vBox})
		g.line("ZBox* %s = z_box(%s);", cVarName(name), val)
		return nil
	}
	g.declare(name)
	g.line("ZVal %s = %s;", cVarName(name), val)
	return nil
}

// genIfStmt 生成 if 语句（用于 if 表达式作语句的场景）
func (g *Gen) genIfStmt(n *ast.IfExpression) error {
	cond, err := g.genExpr(n.Condition)
	if err != nil {
		return err
	}
	g.line("if (z_truthy(%s)) {", cond)
	g.pushScope()
	if err := g.genBlockStmts(n.Consequence); err != nil {
		return err
	}
	g.popScope()
	if n.Alternative != nil {
		g.line("} else {")
		g.pushScope()
		if err := g.genBlockStmts(n.Alternative); err != nil {
			return err
		}
		g.popScope()
	}
	g.line("}")
	return nil
}

func (g *Gen) genBlockStmts(b *ast.BlockStatement) error {
	if b == nil {
		return nil
	}
	for _, stmt := range b.Statements {
		if err := g.genStmt(stmt); err != nil {
			return err
		}
	}
	return nil
}

// genFor 生成 C 风格 for：转成 { init; while (cond) { body; update; } }
func (g *Gen) genFor(s *ast.ForStatement) error {
	g.line("{")
	g.pushScope()
	if s.Init != nil {
		if _, err := g.genExpr(s.Init); err != nil {
			return err
		}
	}
	cond := "z_bool(1)"
	if s.Condition != nil {
		c, err := g.genExpr(s.Condition)
		if err != nil {
			return err
		}
		cond = c
	}
	// 用 while(1) + 显式 update 标签：continue 跳标签，保证 update 不被跳过
	g.line("while (1) {")
	g.line("    if (!z_truthy(%s)) { break; }", cond)
	g.pushScope()
	g.labelSeq++
	label := fmt.Sprintf("zupd_%d", g.labelSeq)
	g.updateLabels = append(g.updateLabels, label)
	if s.Body != nil {
		for _, stmt := range s.Body.Statements {
			if err := g.genStmt(stmt); err != nil {
				return err
			}
		}
	}
	g.updateLabels = g.updateLabels[:len(g.updateLabels)-1]
	g.line("%s: ;", label)
	if s.Update != nil {
		if _, err := g.genExpr(s.Update); err != nil {
			return err
		}
	}
	g.popScope()
	g.line("}")
	g.popScope()
	g.line("}")
	return nil
}

// genForIn 生成遍历循环：范围（start..end）与数组遍历
func (g *Gen) genForIn(s *ast.ForInStatement) error {
	if s.EndExpr != nil {
		return g.genForInRange(s)
	}
	// 集合遍历：数组（含索引变量）
	g.line("{")
	g.pushScope()
	iter, err := g.genExpr(s.Iterable)
	if err != nil {
		return err
	}
	arr := g.temp()
	g.line("ZVal %s = %s;", arr, iter)
	idx := g.temp()
	g.line("for (int64_t %s = 0; %s < z_len(%s).as.i; %s++) {", idx, idx, arr, idx)
	g.pushScope()
	g.updateLabels = append(g.updateLabels, "")
	g.declare(s.VarName)
	g.line("ZVal %s = z_index_get(%s, z_int(%s));", cVarName(s.VarName), arr, idx)
	if s.IndexName != "" {
		g.declare(s.IndexName)
		g.line("ZVal %s = z_int(%s);", cVarName(s.IndexName), idx)
	}
	if s.Guard != nil {
		guard, err := g.genExpr(s.Guard)
		if err != nil {
			return err
		}
		g.line("if (!z_truthy(%s)) { continue; }", guard)
	}
	if s.Body != nil {
		for _, stmt := range s.Body.Statements {
			if err := g.genStmt(stmt); err != nil {
				return err
			}
		}
	}
	g.updateLabels = g.updateLabels[:len(g.updateLabels)-1]
	g.popScope()
	g.line("}")
	g.popScope()
	g.line("}")
	return nil
}

// genForInRange 生成范围循环：对于 i 在 start 到 end { ... }（步长 1，含 start 不含 end）
func (g *Gen) genForInRange(s *ast.ForInStatement) error {
	g.line("{")
	g.pushScope()
	start, err := g.genExpr(s.Iterable)
	if err != nil {
		return err
	}
	end, err := g.genExpr(s.EndExpr)
	if err != nil {
		return err
	}
	g.declare(s.VarName)
	ev := g.temp()
	g.line("ZVal %s = %s;", ev, end)
	// zuzo 的范围是闭区间（含 end），对齐 evalForInRange
	g.line("for (ZVal %s = %s; z_truthy(z_le(%s, %s)); %s = z_add(%s, z_int(1))) {",
		cVarName(s.VarName), start, cVarName(s.VarName), ev, cVarName(s.VarName), cVarName(s.VarName))
	g.pushScope()
	g.updateLabels = append(g.updateLabels, "")
	if s.Guard != nil {
		guard, err := g.genExpr(s.Guard)
		if err != nil {
			return err
		}
		g.line("if (!z_truthy(%s)) { continue; }", guard)
	}
	if s.Body != nil {
		for _, stmt := range s.Body.Statements {
			if err := g.genStmt(stmt); err != nil {
				return err
			}
		}
	}
	g.updateLabels = g.updateLabels[:len(g.updateLabels)-1]
	g.popScope()
	g.line("}")
	g.popScope()
	g.line("}")
	return nil
}
