package llvmbackend

import (
	"fmt"
	"strings"

	"zuzo/ast"
)

// Generate 把 AST 生成文本 LLVM IR。
func Generate(program *ast.Program, opt Options) (code string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("LLVM 后端内部错误: %v", r)
		}
	}()

	g := &Gen{
		opt:        opt,
		funcs:      map[string]*fnInfo{},
		defines:    map[string]*ast.DefineStatement{},
		globals:    map[string]bool{},
		globalLine: map[string]int{},
		strIdx:     map[string]int{},
		decls:      map[string]string{},
	}
	g.ana = analyzeProgram(program)
	g.collectGlobals(program)
	for _, stmt := range program.Statements {
		if d, ok := stmt.(*ast.DefineStatement); ok {
			if _, dup := g.defines[d.Name]; !dup {
				g.defines[d.Name] = d
				g.defOrder = append(g.defOrder, d.Name)
			}
		}
	}
	for _, fi := range g.ana.All {
		if fi.Name != "" {
			if _, dup := g.funcs[fi.Name]; !dup {
				g.funcs[fi.Name] = fi
				g.order = append(g.order, fi.Name)
			}
		}
	}

	// 顶层变量 → 模块级全局
	for _, name := range g.globalOrder {
		g.globalDefs = append(g.globalDefs, fmt.Sprintf("@%s = internal global %%ZVal zeroinitializer, align 8", cGlobalName(name)))
	}

	// 数据建模构造函数
	for _, name := range g.defOrder {
		if err := g.genDefine(g.defines[name]); err != nil {
			return "", err
		}
	}
	// 函数
	for _, fi := range g.ana.All {
		if err := g.genFunction(fi); err != nil {
			return "", err
		}
	}
	// 入口
	if err := g.genMain(program); err != nil {
		return "", err
	}

	if len(g.errs) > 0 {
		return "", fmt.Errorf("LLVM 后端不支持以下语法：\n  - %s", strings.Join(g.errs, "\n  - "))
	}
	return g.moduleText(), nil
}

func (g *Gen) moduleText() string {
	var b strings.Builder
	b.WriteString("; ModuleID = 'zuzo'\n")
	b.WriteString("; 由 zuzo LLVM 后端自动生成，请勿手改。\n")
	if g.opt.SourcePath != "" {
		b.WriteString("; 源文件: " + strings.ReplaceAll(g.opt.SourcePath, "\\", "/") + "\n")
	}
	triple := g.opt.Target
	if triple == "" {
		triple = defaultTriple()
	}
	b.WriteString("target triple = \"" + triple + "\"\n\n")
	b.WriteString("%ZVal = type { i32, i64 }\n")
	b.WriteString("%ZObj = type { ptr, i32, i32, i64 }\n")
	b.WriteString("%ZBox = type { %ZObj, %ZVal }\n")
	b.WriteString("%ZTryFrame = type { [256 x i8], ptr }\n\n")
	b.WriteString("@z_try_top = external global ptr\n\n")
	for _, s := range g.strDefs {
		b.WriteString(s + "\n")
	}
	if len(g.strDefs) > 0 {
		b.WriteString("\n")
	}
	for _, s := range g.globalDefs {
		b.WriteString(s + "\n")
	}
	if len(g.globalDefs) > 0 {
		b.WriteString("\n")
	}
	for _, name := range g.declOrder {
		b.WriteString(g.decls[name] + "\n")
	}
	if len(g.declOrder) > 0 {
		b.WriteString("\n")
	}
	for _, f := range g.fnDefs {
		b.WriteString(f)
	}
	return b.String()
}

// collectGlobals 顶层语句里声明的变量 → 模块级全局（与 cbackend 同口径）。
func (g *Gen) collectGlobals(program *ast.Program) {
	var walkStmt func(ast.Statement)
	var walkExpr func(ast.Expression)
	add := func(name string, line int) {
		if name == "" {
			return
		}
		if g.globals[name] {
			if line > g.globalLine[name] {
				g.globalLine[name] = line
			}
			return
		}
		g.globals[name] = true
		g.globalLine[name] = line
		g.globalOrder = append(g.globalOrder, name)
	}
	walkStmt = func(st ast.Statement) {
		switch n := st.(type) {
		case *ast.LetStatement:
			if n.Name != nil {
				add(n.Name.Value, n.Token.Line)
			}
			walkExpr(n.Value)
		case *ast.ExpressionStatement:
			walkExpr(n.Expression)
		case *ast.ReturnStatement:
			walkExpr(n.ReturnValue)
		case *ast.BlockStatement:
			for _, s := range n.Statements {
				walkStmt(s)
			}
		case *ast.WhileStatement:
			walkExpr(n.Condition)
			if n.Body != nil {
				walkStmt(n.Body)
			}
		case *ast.ForStatement:
			walkExpr(n.Init)
			walkExpr(n.Condition)
			walkExpr(n.Update)
			if n.Body != nil {
				walkStmt(n.Body)
			}
		case *ast.ForInStatement:
			walkExpr(n.Iterable)
			walkExpr(n.EndExpr)
			walkExpr(n.Guard)
			if n.Body != nil {
				walkStmt(n.Body)
			}
		}
	}
	walkExpr = func(e ast.Expression) {
		switch n := e.(type) {
		case *ast.IfExpression:
			walkExpr(n.Condition)
			if n.Consequence != nil {
				walkStmt(n.Consequence)
			}
			if n.Alternative != nil {
				walkStmt(n.Alternative)
			}
		case *ast.InfixExpression:
			walkExpr(n.Left)
			walkExpr(n.Right)
		case *ast.PrefixExpression:
			walkExpr(n.Right)
		case *ast.AssignExpression:
			walkExpr(n.Value)
		case *ast.CallExpression:
			walkExpr(n.Function)
			for _, a := range n.Arguments {
				walkExpr(a)
			}
		case *ast.ArrayLiteral:
			for _, el := range n.Elements {
				walkExpr(el)
			}
		case *ast.IndexExpression:
			walkExpr(n.Left)
			walkExpr(n.Index)
		}
	}
	for _, st := range program.Statements {
		walkStmt(st)
	}
}

func (g *Gen) beginFunction(retSlot string, envPtr string) {
	g.fb = &fnBuilder{retSlot: retSlot, envPtr: envPtr}
}

func (g *Gen) finishFunction(header string) {
	var b strings.Builder
	b.WriteString(header)
	b.WriteString(" {\nentry:\n")
	b.WriteString(g.fb.entry.String())
	b.WriteString(g.fb.body.String())
	b.WriteString("}\n\n")
	g.fnDefs = append(g.fnDefs, b.String())
	g.fb = nil
}

func (g *Gen) genFunction(fi *fnInfo) error {
	prev := g.cur
	g.cur = fi
	defer func() { g.cur = prev }()

	envPtr := ""
	if fi.EnvType != "" {
		envPtr = "%env"
	}
	g.beginFunction("%ret", envPtr)
	g.pushScope()
	for i, p := range fi.Node.Parameters {
		slot := g.tempZ()
		pv := g.fb.nextTemp()
		g.emit("%s = getelementptr %%ZVal, ptr %%argv, i64 %d", pv, i)
		g.copyZ(slot, pv)
		g.setVar(p.Value, varSlot{kind: vPlain, slot: slot})
	}
	for i, name := range fi.Captures {
		g.setVar(name, varSlot{kind: vCapture, capIdx: i})
	}

	body := fi.Node.Body.Statements
	implicitRet := true
	for i, stmt := range body {
		if i == len(body)-1 {
			if es, ok := stmt.(*ast.ExpressionStatement); ok {
				v, err := g.genExpr(es.Expression)
				if err != nil {
					g.popScope()
					return err
				}
				g.copyZ("%ret", v.Slot)
				g.emitRetVoid()
				g.popScope()
				g.finishFunction(g.funcHeader(fi))
				return nil
			}
		}
		if err := g.genStmt(stmt); err != nil {
			g.popScope()
			return err
		}
		if !g.fb.terminated {
			g.emitSafepoint()
		}
	}
	if implicitRet {
		nv := g.callZ("z_null")
		g.copyZ("%ret", nv.Slot)
		g.emitRetVoid()
	}
	g.popScope()
	g.finishFunction(g.funcHeader(fi))
	return nil
}

func (g *Gen) funcHeader(fi *fnInfo) string {
	return fmt.Sprintf("define internal void %s(ptr sret(%%ZVal) %%ret, i32 %%argc, ptr %%argv, ptr %%env)", g.fnSym(fi))
}

func (g *Gen) genDefine(d *ast.DefineStatement) error {
	prev := g.cur
	g.cur = nil
	defer func() { g.cur = prev }()
	g.beginFunction("%ret", "")
	g.pushScope()
	zo := g.callZ("z_hash")
	for _, f := range d.Fields {
		val, err := g.genExpr(f.Value)
		if err != nil {
			g.popScope()
			return err
		}
		g.callVoid("z_hash_set", "ptr "+zo.Slot, "ptr "+g.constStr(f.Name).Slot, "ptr "+val.Slot)
	}
	g.popScope()
	g.copyZ("%ret", zo.Slot)
	g.emitRetVoid()
	g.finishFunction(fmt.Sprintf("define internal void @zdef_%s(ptr sret(%%ZVal) %%ret, i32 %%argc, ptr %%argv, ptr %%env)", sanitizeIdent(d.Name)))
	return nil
}

func (g *Gen) genMain(program *ast.Program) error {
	g.cur = nil
	g.beginFunction("", "")
	g.pushScope()
	g.callVoid("z_rt_init")
	if g.opt.Strict {
		g.callVoid("z_set_strict", "i32 1")
	}
	for _, stmt := range program.Statements {
		if fs, ok := stmt.(*ast.ExpressionStatement); ok {
			if fl, ok := fs.Expression.(*ast.FunctionLiteral); ok && fl.Name != "" {
				continue // 具名函数已单独生成
			}
		}
		if err := g.genStmt(stmt); err != nil {
			g.popScope()
			return err
		}
		if !g.fb.terminated {
			g.emitSafepoint()
		}
	}
	g.emit("ret i32 0")
	g.fb.terminated = true
	g.popScope()
	g.finishFunction("define i32 @main(i32 %argc, ptr %argv)")
	return nil
}
