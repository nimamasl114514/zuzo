package llvmbackend

// 语句 codegen：按 cbackend/stmt.go 的语义逐条平移到 LLVM IR。
//
// 与 C 后端的关键差异（都是「文本 IR」带来的必然调整）：
//   - 没有 C 的块作用域，变量一律 entry 块 alloca + scopes 表登记（同名声遮蔽靠 scopes）；
//   - 循环/分支用显式基本块 + br 连接，每个基本块都必须有 terminator（emit 在
//     terminator 后会静默丢弃，所以先 place 再发指令）；
//   - continue 跳到循环的 step 标签（对应 C 后端 for 的 goto zupd_N，保证 update 不被跳过）；
//   - 条件表达式的前置语句天然落在条件块里，每轮重新执行，不会退化成「只算一次」的死循环；
//   - 异常仍走 setjmp/longjmp：%ZTryFrame 与运行时 z_try_top 共同维护帧链。

import (
	"fmt"

	"zuzo/ast"
)

// ── 小工具 ─────────────────────────────────────────────────

// stmLoadI64 / stmStoreI64 读写 i64 槽位（循环计数器、长度缓存）。
func (g *Gen) stmLoadI64(slot string) string {
	r := g.fb.nextTemp()
	g.emit("%s = load i64, ptr %s, align 8", r, slot)
	return r
}

func (g *Gen) stmStoreI64(slot, val string) {
	g.emit("store i64 %s, ptr %s, align 8", val, slot)
}

// stmPayloadI64 取 %ZVal 的 i64 载荷（等价 C 侧的 v.as.i，用于 z_len 结果）。
func (g *Gen) stmPayloadI64(v *Value) string {
	p := g.fb.nextTemp()
	g.emit("%s = getelementptr %%ZVal, ptr %s, i32 0, i32 1", p, v.Slot)
	r := g.fb.nextTemp()
	g.emit("%s = load i64, ptr %s, align 8", r, p)
	return r
}

// stmDeclareLocal 在当前作用域声明一个局部变量（被内层闭包捕获时装箱）。
func (g *Gen) stmDeclareLocal(name string, val *Value) error {
	if name == "" {
		return nil
	}
	if g.ana.Boxed[g.cur][name] {
		g.boxVar(name, val)
		return nil
	}
	slot := g.tempZ()
	g.copyZ(slot, val.Slot)
	g.setVar(name, varSlot{kind: vPlain, slot: slot})
	return nil
}

// stmAllocaAligned 在 entry 块分配指定对齐的槽位。
//
// 为什么要单独一个：%ZTryFrame 里第一个字段是 jmp_buf，MSVC 的 _setjmp 会用
// movaps 整块保存 XMM 寄存器，要求缓冲区 16 字节对齐；通用 alloca() 只给 align 8，
// 一旦帧落在 8 对齐（非 16 对齐）的栈位上，_setjmp 立即触发 #GP 崩溃
// （第31轮实测：align 8 → 段错误退出，align 16 → 正常）。
func (g *Gen) stmAllocaAligned(typ string, align int) string {
	name := g.fb.nextTemp()
	fmt.Fprintf(&g.fb.entry, "  %s = alloca %s, align %d\n", name, typ, align)
	return name
}

// stmPushLoop / stmPopLoop 维护 break / continue 的目标标签栈。
func (g *Gen) stmPushLoop(cont, brk string) {
	g.fb.loops = append(g.fb.loops, loopCtx{contLabel: cont, breakLbl: brk})
}

func (g *Gen) stmPopLoop() {
	if n := len(g.fb.loops); n > 0 {
		g.fb.loops = g.fb.loops[:n-1]
	}
}

// stmGenList 顺序生成一组语句；遇到 terminator 后剩余语句不可达，直接收尾。
func (g *Gen) stmGenList(list []ast.Statement) error {
	for _, stmt := range list {
		if g.fb.terminated {
			return nil
		}
		if err := g.genStmt(stmt); err != nil {
			return err
		}
		g.emitSafepoint()
	}
	return nil
}

// stmGuard 生成「满足 条件」过滤：条件为假跳到 contLbl（continue 语义）。
func (g *Gen) stmGuard(guard ast.Expression, contLbl string) error {
	if guard == nil {
		return nil
	}
	gv, err := g.genExpr(guard)
	if err != nil {
		return err
	}
	okLbl := g.newLabel()
	skipLbl := g.newLabel()
	g.condBr(g.truthy(gv), okLbl, skipLbl)
	g.place(skipLbl)
	g.br(contLbl)
	g.place(okLbl)
	return nil
}

// ── 语句分发 ───────────────────────────────────────────────

func (g *Gen) genStmt(stmt ast.Statement) error {
	// 当前块已终止 → 该语句不可达，静默丢弃（避免生成无 terminator 的孤立块）
	if g.fb.terminated {
		return nil
	}
	switch s := stmt.(type) {
	case *ast.LetStatement:
		return g.genLet(s)

	case *ast.ReturnStatement:
		return g.genReturn(s)

	case *ast.ExpressionStatement:
		// if 表达式作语句：直接展开成 if 语句，避免多余临时量
		if s.Expression == nil {
			return nil
		}
		if ie, ok := s.Expression.(*ast.IfExpression); ok {
			return g.genIfStmt(ie)
		}
		if fl, ok := s.Expression.(*ast.FunctionLiteral); ok && fl.Name != "" {
			return nil // 具名函数已在 Generate 阶段单独生成
		}
		if _, err := g.genExpr(s.Expression); err != nil {
			return err
		}
		return nil

	case *ast.BlockStatement:
		g.pushScope()
		if err := g.genBlockStmts(s); err != nil {
			g.popScope()
			return err
		}
		g.popScope()
		return nil

	case *ast.WhileStatement:
		return g.genWhile(s)

	case *ast.ForStatement:
		return g.genFor(s)

	case *ast.ForInStatement:
		return g.genForIn(s)

	case *ast.BreakStatement:
		if n := len(g.fb.loops); n > 0 {
			g.br(g.fb.loops[n-1].breakLbl)
			return nil
		}
		return fmt.Errorf("%d:%d: 跳出 语句不在循环内", s.Token.Line, s.Token.Column)

	case *ast.ContinueStatement:
		if n := len(g.fb.loops); n > 0 {
			g.br(g.fb.loops[n-1].contLabel)
			return nil
		}
		return fmt.Errorf("%d:%d: 跳过 语句不在循环内", s.Token.Line, s.Token.Column)

	case *ast.TryCatchStatement:
		return g.genTryCatch(s)

	case *ast.DefineStatement:
		return nil // 构造函数已在 Generate 阶段生成

	case *ast.ImportStatement:
		// EmitLLVM 已把导入展开进主文件，正常不会走到这里
		g.unsupportedAt(s.Token.Line, s.Token.Column,
			fmt.Sprintf("导入语句（%s）未展开，请把被导入内容的语句直接写在主文件里", s.Path))
		return nil
	}
	return fmt.Errorf("不支持的语句: %T", stmt)
}

// ── 变量声明 ───────────────────────────────────────────────

func (g *Gen) genLet(s *ast.LetStatement) error {
	if s.Name == nil {
		return fmt.Errorf("%d:%d: 变量 语句缺少名字", s.Token.Line, s.Token.Column)
	}
	val, err := g.genExpr(s.Value)
	if err != nil {
		return err
	}
	name := s.Name.Value

	// 已存在同名变量 → 就是赋值（含装箱 / 闭包捕获两种间接形态）
	if slot, ok := g.lookupVar(name); ok {
		switch slot.kind {
		case vPlain, vGlobal:
			g.copyZ(slot.slot, val.Slot)
		default:
			ref, err := g.varRef(slot)
			if err != nil {
				return err
			}
			g.copyZ(ref, val.Slot)
		}
		return nil
	}
	// 顶层变量：落到模块级全局 @zg_<name>
	if g.globals[name] {
		g.copyZ(g.globalSlot(name), val.Slot)
		return nil
	}
	return g.stmDeclareLocal(name, val)
}

// genReturn 求值 → 写 %ret → ret void（main 里对齐解释器：返回值不影响退出码）。
func (g *Gen) genReturn(s *ast.ReturnStatement) error {
	var val *Value
	if s.ReturnValue != nil {
		v, err := g.genExpr(s.ReturnValue)
		if err != nil {
			return err
		}
		val = v
	} else {
		val = g.constNull()
	}
	if g.fb.retSlot == "" {
		// main / 顶层：求值保留副作用，退出码固定 0（与 cbackend 的 return 0 一致）
		g.emit("ret i32 0")
		g.fb.terminated = true
		return nil
	}
	g.copyZ(g.fb.retSlot, val.Slot)
	g.emitRetVoid()
	return nil
}

// genIfStmt 生成 if 语句（用于 if 表达式作语句的场景）。
func (g *Gen) genIfStmt(n *ast.IfExpression) error {
	cond, err := g.genExpr(n.Condition)
	if err != nil {
		return err
	}
	thenLbl := g.newLabel()
	mergeLbl := g.newLabel()
	elseLbl := mergeLbl
	if n.Alternative != nil {
		elseLbl = g.newLabel()
	}
	g.condBr(g.truthy(cond), thenLbl, elseLbl)

	g.place(thenLbl)
	g.pushScope()
	if err := g.genBlockStmts(n.Consequence); err != nil {
		g.popScope()
		return err
	}
	g.popScope()
	thenFalls := !g.fb.terminated
	if thenFalls {
		g.br(mergeLbl)
	}

	if n.Alternative == nil {
		// 无 else：条件为假直接落到 merge，merge 一定有前驱
		g.place(mergeLbl)
		return nil
	}

	g.place(elseLbl)
	g.pushScope()
	if err := g.genBlockStmts(n.Alternative); err != nil {
		g.popScope()
		return err
	}
	g.popScope()
	elseFalls := !g.fb.terminated
	if elseFalls {
		g.br(mergeLbl)
	}
	if thenFalls || elseFalls {
		g.place(mergeLbl)
	} else {
		// 两支都返回 → 后续语句不可达，不生成 merge 块
		g.fb.terminated = true
	}
	return nil
}

func (g *Gen) genBlockStmts(b *ast.BlockStatement) error {
	if b == nil {
		return nil
	}
	return g.stmGenList(b.Statements)
}

// ── 循环 ───────────────────────────────────────────────────

// genWhile 当 条件 { 体 }：cond 块里每轮重新求值条件。
func (g *Gen) genWhile(s *ast.WhileStatement) error {
	condLbl := g.newLabel()
	bodyLbl := g.newLabel()
	endLbl := g.newLabel()

	g.br(condLbl)
	g.place(condLbl)
	cond, err := g.genExpr(s.Condition)
	if err != nil {
		return err
	}
	g.condBr(g.truthy(cond), bodyLbl, endLbl)

	g.place(bodyLbl)
	g.pushScope()
	g.stmPushLoop(condLbl, endLbl)
	if err := g.genBlockStmts(s.Body); err != nil {
		g.stmPopLoop()
		g.popScope()
		return err
	}
	g.stmPopLoop()
	g.popScope()
	if !g.fb.terminated {
		g.br(condLbl)
	}

	g.place(endLbl)
	return nil
}

// genFor C 风格 for：{ init; cond -> body -> step -> cond; }，continue 跳 step。
func (g *Gen) genFor(s *ast.ForStatement) error {
	condLbl := g.newLabel()
	bodyLbl := g.newLabel()
	stepLbl := g.newLabel()
	endLbl := g.newLabel()

	g.pushScope()
	if s.Init != nil {
		if _, err := g.genExpr(s.Init); err != nil {
			g.popScope()
			return err
		}
	}

	g.br(condLbl)
	g.place(condLbl)
	if s.Condition != nil {
		c, err := g.genExpr(s.Condition)
		if err != nil {
			g.popScope()
			return err
		}
		g.condBr(g.truthy(c), bodyLbl, endLbl)
	} else {
		g.br(bodyLbl)
	}

	g.place(bodyLbl)
	g.pushScope()
	g.stmPushLoop(stepLbl, endLbl)
	if err := g.genBlockStmts(s.Body); err != nil {
		g.stmPopLoop()
		g.popScope()
		g.popScope()
		return err
	}
	g.stmPopLoop()
	g.popScope()
	if !g.fb.terminated {
		g.br(stepLbl)
	}

	// step（update 在 body 作用域之外求值，与 C 后端一致）
	g.place(stepLbl)
	if s.Update != nil {
		if _, err := g.genExpr(s.Update); err != nil {
			g.popScope()
			return err
		}
	}
	if !g.fb.terminated {
		g.br(condLbl)
	}

	g.place(endLbl)
	g.popScope()
	return nil
}

// genForIn 遍历循环：范围（start 到 end）与集合（数组）遍历。
func (g *Gen) genForIn(s *ast.ForInStatement) error {
	if s.EndExpr != nil {
		return g.genForInRange(s)
	}
	return g.genForInArray(s)
}

// genForInArray 集合遍历：for (i = 0; i < z_len(arr).as.i; i++) { x = z_index_get(arr, z_int(i)) }
func (g *Gen) genForInArray(s *ast.ForInStatement) error {
	g.pushScope()
	iter, err := g.genExpr(s.Iterable)
	if err != nil {
		g.popScope()
		return err
	}
	arrSlot := g.tempZ()
	g.copyZ(arrSlot, iter.Slot)

	idxSlot := g.alloca("i64")
	g.stmStoreI64(idxSlot, "0")

	condLbl := g.newLabel()
	bodyLbl := g.newLabel()
	stepLbl := g.newLabel()
	endLbl := g.newLabel()

	g.br(condLbl)
	g.place(condLbl)
	lenV := g.callZ("z_len", "ptr "+arrSlot)
	lenI := g.stmPayloadI64(lenV)
	cur := g.stmLoadI64(idxSlot)
	cmp := g.fb.nextTemp()
	g.emit("%s = icmp slt i64 %s, %s", cmp, cur, lenI)
	g.condBr(cmp, bodyLbl, endLbl)

	g.place(bodyLbl)
	g.pushScope()
	g.stmPushLoop(stepLbl, endLbl)
	i := g.stmLoadI64(idxSlot)
	idxZ := g.callZ("z_int", "i64 "+i)
	elem := g.callZ("z_index_get", "ptr "+arrSlot, "ptr "+idxZ.Slot)
	if err := g.stmDeclareLocal(s.VarName, elem); err != nil {
		g.stmPopLoop()
		g.popScope()
		g.popScope()
		return err
	}
	if s.IndexName != "" {
		idxVal := g.callZ("z_int", "i64 "+i)
		if err := g.stmDeclareLocal(s.IndexName, idxVal); err != nil {
			g.stmPopLoop()
			g.popScope()
			g.popScope()
			return err
		}
	}
	if err := g.stmGuard(s.Guard, stepLbl); err != nil {
		g.stmPopLoop()
		g.popScope()
		g.popScope()
		return err
	}
	if err := g.genBlockStmts(s.Body); err != nil {
		g.stmPopLoop()
		g.popScope()
		g.popScope()
		return err
	}
	g.stmPopLoop()
	g.popScope()
	if !g.fb.terminated {
		g.br(stepLbl)
	}

	g.place(stepLbl)
	last := g.stmLoadI64(idxSlot)
	next := g.fb.nextTemp()
	g.emit("%s = add i64 %s, 1", next, last)
	g.stmStoreI64(idxSlot, next)
	g.br(condLbl)

	g.place(endLbl)
	g.popScope()
	return nil
}

// genForInRange 范围循环：对于 i 在 start 到 end（闭区间，步长 1）。
func (g *Gen) genForInRange(s *ast.ForInStatement) error {
	g.pushScope()
	startV, err := g.genExpr(s.Iterable)
	if err != nil {
		g.popScope()
		return err
	}
	endV, err := g.genExpr(s.EndExpr)
	if err != nil {
		g.popScope()
		return err
	}
	endSlot := g.tempZ()
	g.copyZ(endSlot, endV.Slot)

	// 循环变量：被内层闭包捕获时装箱，读写都经由 ZBox 内的 ZVal
	var vSlot, boxSlot string
	if g.ana.Boxed[g.cur][s.VarName] {
		boxSlot = g.boxVar(s.VarName, startV)
	} else {
		vSlot = g.tempZ()
		g.copyZ(vSlot, startV.Slot)
		g.setVar(s.VarName, varSlot{kind: vPlain, slot: vSlot})
	}
	// curPtr 在当前块里取「循环变量 %ZVal 的地址」（装箱时每次先 load 出 box）
	curPtr := func() string {
		if boxSlot != "" {
			ref, err := g.varRef(varSlot{kind: vBox, slot: boxSlot})
			if err == nil {
				return ref
			}
		}
		return vSlot
	}

	one := g.constInt(1)

	condLbl := g.newLabel()
	bodyLbl := g.newLabel()
	stepLbl := g.newLabel()
	endLbl := g.newLabel()

	g.br(condLbl)
	g.place(condLbl)
	le := g.callZ("z_le", "ptr "+curPtr(), "ptr "+endSlot)
	g.condBr(g.truthy(le), bodyLbl, endLbl)

	g.place(bodyLbl)
	g.pushScope()
	g.stmPushLoop(stepLbl, endLbl)
	if err := g.stmGuard(s.Guard, stepLbl); err != nil {
		g.stmPopLoop()
		g.popScope()
		g.popScope()
		return err
	}
	if err := g.genBlockStmts(s.Body); err != nil {
		g.stmPopLoop()
		g.popScope()
		g.popScope()
		return err
	}
	g.stmPopLoop()
	g.popScope()
	if !g.fb.terminated {
		g.br(stepLbl)
	}

	g.place(stepLbl)
	added := g.callZ("z_add", "ptr "+curPtr(), "ptr "+one.Slot)
	g.copyZ(curPtr(), added.Slot)
	g.br(condLbl)

	g.place(endLbl)
	g.popScope()
	return nil
}

// ── 异常 ───────────────────────────────────────────────────

// genTryCatch 执行 { } 遇到 err { } → setjmp/longjmp。
// 帧链：frame.prev = z_try_top; z_try_top = &frame; r = _setjmp(frame)
//
//	r == 0 → try 体（正常落穿时恢复 z_try_top）
//	r != 0 → catch 体（先恢复 z_try_top，再 z_error_take）
func (g *Gen) genTryCatch(s *ast.TryCatchStatement) error {
	frame := g.stmAllocaAligned("%ZTryFrame", 16)
	prev := g.fb.nextTemp()
	g.emit("%s = load ptr, ptr @z_try_top, align 8", prev)
	prevField := g.fb.nextTemp()
	g.emit("%s = getelementptr %%ZTryFrame, ptr %s, i32 0, i32 1", prevField, frame)
	// 注意：setjmp 之后不再复用 %prev（跨 longjmp 的寄存器值不可信），
	// 两条路径都从 frame.prev 重新 load 一次再写回 z_try_top。
	g.emit("store ptr %s, ptr %s, align 8", prev, prevField)
	g.emit("store ptr %s, ptr @z_try_top, align 8", frame)

	// Win64 的 _setjmp 是两参：(_setjmp(buf, frameaddress))，第二参由编译器用
	// llvm.frameaddress 提供；只传一个参数会让 RDX 带垃圾值，longjmp 时崩在 CRT 里。
	fp := g.fb.nextTemp()
	g.emit("%s = call ptr @llvm.frameaddress.p0(i32 0)", fp)
	r := g.callR("i32", "_setjmp", "ptr "+frame, "ptr "+fp)
	isZero := g.fb.nextTemp()
	g.emit("%s = icmp eq i32 %s, 0", isZero, r)
	tryLbl := g.newLabel()
	catchLbl := g.newLabel()
	endLbl := g.newLabel()
	g.condBr(isZero, tryLbl, catchLbl)

	// try 路径
	g.place(tryLbl)
	g.pushScope()
	if err := g.genBlockStmts(s.Body); err != nil {
		g.popScope()
		return err
	}
	g.popScope()
	tryFalls := !g.fb.terminated
	if tryFalls {
		p := g.fb.nextTemp()
		g.emit("%s = load ptr, ptr %s, align 8", p, prevField)
		g.emit("store ptr %s, ptr @z_try_top, align 8", p)
		g.br(endLbl)
	}

	// catch 路径
	g.place(catchLbl)
	p := g.fb.nextTemp()
	g.emit("%s = load ptr, ptr %s, align 8", p, prevField)
	g.emit("store ptr %s, ptr @z_try_top, align 8", p)
	g.pushScope()
	if s.ErrVar != "" {
		ev := g.callZ("z_error_take")
		if err := g.stmDeclareLocal(s.ErrVar, ev); err != nil {
			g.popScope()
			return err
		}
	}
	if err := g.genBlockStmts(s.Handler); err != nil {
		g.popScope()
		return err
	}
	g.popScope()
	catchFalls := !g.fb.terminated
	if catchFalls {
		g.br(endLbl)
	}

	if tryFalls || catchFalls {
		g.place(endLbl)
	} else {
		// 两条路径都已终止 → 不生成 merge 块
		g.fb.terminated = true
	}
	return nil
}
