package nativecc

import (
	"fmt"
	"math"
)

// genCond 生成条件：结果 0/1 在 rax
func (fg *funcGen) genCond(e Expr) error {
	t := e.Type()
	if t == nil {
		if _, err := fg.c.sema.typeOf(e); err != nil {
			return err
		}
		t = e.Type()
	}
	if t != nil && t.IsFloat() {
		if err := fg.genAsFloat(e); err != nil {
			return err
		}
		fg.a.Pxor(1, 1)
		fg.a.Ucomisd(0, 1)
		fg.a.Setcc(5, RAX) // setne
		fg.a.MovzxR8(RAX, RAX)
		// 上一行等价于 movzx eax, al（用 0F B6 /r 自寻址）
		return nil
	}
	if err := fg.genAsInt(e); err != nil {
		return err
	}
	return nil
}

// genExpr 生成表达式：结果在 rax（整数/指针）或 xmm0（浮点）
func (fg *funcGen) genExpr(e Expr) error {
	if e == nil {
		return nil
	}
	if e.Type() == nil {
		if _, err := fg.c.sema.typeOf(e); err != nil {
			return err
		}
	}
	switch v := e.(type) {
	case *IntLit:
		fg.a.MovImm64(RAX, v.V)
	case *CharLit:
		fg.a.MovImm64(RAX, v.V)
	case *FloatLit:
		fg.a.MovImm64(RAX, int64(math.Float64bits(v.V)))
		fg.a.MovqXmmGpr(0, RAX)
	case *StrLit:
		lab := fg.c.internString(v.V)
		v.Sym = lab
		fg.a.Lea(RAX, &Mem{RipRel: true, Label: lab})
	case *Ident:
		return fg.genIdentValue(v)
	case *BinaryExpr:
		return fg.genBinary(v)
	case *UnaryExpr:
		return fg.genUnary(v)
	case *PostfixExpr:
		return fg.genIncDec(v.X, v.Op, false)
	case *AssignExpr:
		return fg.genAssign(v)
	case *CondExpr:
		return fg.genCondExpr(v)
	case *CallExpr:
		return fg.genCall(v)
	case *IndexExpr:
		if err := fg.genAddr(v); err != nil {
			return err
		}
		fg.a.MovRR(RDX, RAX)
		fg.loadFrom(RDX, v.Type())
	case *MemberExpr:
		if err := fg.genAddr(v); err != nil {
			return err
		}
		fg.a.MovRR(RDX, RAX)
		fg.loadFrom(RDX, v.Type())
	case *CastExpr:
		return fg.genCast(v)
	case *SizeofExpr:
		fg.a.MovImm64(RAX, fg.sizeofOf(v))
	case *CompoundLit:
		return fg.genCompoundLit(v)
	case *BuiltinCall:
		return fg.genBuiltin(v)
	default:
		return fmt.Errorf("未支持的表达式类型 %T", e)
	}
	return nil
}

// genIdentValue 标识符取值
func (fg *funcGen) genIdentValue(v *Ident) error {
	t := v.Type()
	if t != nil && t.Kind == TyFunc {
		if _, ok := fg.c.sema.Funcs[v.Name]; ok {
			fg.a.Lea(RAX, &Mem{RipRel: true, Label: fdLabel(v.Name)})
			return nil
		}
	}
	if err := fg.genAddr(v); err != nil {
		return err
	}
	if t != nil && (t.IsRecord() || t.Kind == TyArray) {
		return nil
	}
	fg.a.MovRR(RDX, RAX)
	fg.loadFrom(RDX, t)
	return nil
}

// loadFrom 从 [addr] 加载值到 rax / xmm0
func (fg *funcGen) loadFrom(addrReg int, t *CType) {
	if t == nil {
		fg.a.MovLoad(RAX, &Mem{Base: addrReg, Index: -1, Scale: 1})
		return
	}
	if t.Kind == TyArray || t.IsRecord() {
		if addrReg != RAX {
			fg.a.MovRR(RAX, addrReg)
		}
		return
	}
	m := &Mem{Base: addrReg, Index: -1, Scale: 1}
	if t.IsFloat() {
		if t.Kind == TyFloat {
			fg.a.MovsdLoad(0, m) // 占位：float 暂按 double 处理
		} else {
			fg.a.MovsdLoad(0, m)
		}
		return
	}
	switch t.Size {
	case 1:
		if t.Unsigned {
			fg.a.MovLoad8(RAX, m)
		} else {
			fg.a.MovLoad8S(RAX, m)
		}
	case 2:
		if t.Unsigned {
			fg.a.MovLoad16(RAX, m)
		} else {
			fg.a.MovLoad16S(RAX, m)
		}
	case 4:
		if t.Unsigned {
			fg.a.MovLoad32(RAX, m)
		} else {
			fg.a.MovLoad32S(RAX, m)
		}
	default:
		fg.a.MovLoad(RAX, m)
	}
}

// storeTo 把 rax / xmm0 存到 [addr]
func (fg *funcGen) storeTo(addrReg int, t *CType) {
	m := &Mem{Base: addrReg, Index: -1, Scale: 1}
	if t == nil {
		fg.a.MovStore(m, RAX)
		return
	}
	if t.IsFloat() {
		fg.a.MovsdStore(m, 0)
		return
	}
	if t.IsRecord() {
		// struct 值在 rax 中是指针 → memcpy
		fg.copyStruct(addrReg, t.Size)
		return
	}
	switch t.Size {
	case 1:
		fg.a.MovStore8(m, RAX)
	case 2:
		fg.a.MovStore16(m, RAX)
	case 4:
		fg.a.MovStore32(m, RAX)
	default:
		fg.a.MovStore(m, RAX)
	}
}

// copyStruct 把 rax 指向的来源复制到 [addrReg]
func (fg *funcGen) copyStruct(addrReg int, size int64) {
	dst := fg.alloc(8)
	fg.a.MovStore(stackMem(dst), addrReg)
	src := fg.alloc(8)
	fg.a.MovStore(stackMem(src), RAX)
	fg.a.MovLoad(RCX, stackMem(dst))
	fg.a.MovLoad(RDX, stackMem(src))
	fg.a.MovImm32(R8, int32(size))
	fg.callImport("memcpy")
	fg.free(16)
}

// genAddr 生成左值地址到 rax
func (fg *funcGen) genAddr(e Expr) error {
	if e.Type() == nil {
		if _, err := fg.c.sema.typeOf(e); err != nil {
			return err
		}
	}
	switch v := e.(type) {
	case *Ident:
		if off, ok := fg.localOff[v.Name]; ok {
			if fg.byRef[v.Name] {
				fg.a.MovLoad(RAX, stackMem(off))
			} else {
				fg.a.Lea(RAX, stackMem(off))
			}
			return nil
		}
		if gn, ok := fg.rename[v.Name]; ok {
			fg.a.Lea(RAX, &Mem{RipRel: true, Label: "Lstatic_" + gn})
			return nil
		}
		if _, ok := fg.c.sema.Globals[v.Name]; ok {
			fg.a.Lea(RAX, &Mem{RipRel: true, Label: "Lglob_" + v.Name})
			return nil
		}
		if _, ok := fg.c.sema.Funcs[v.Name]; ok {
			fg.a.Lea(RAX, &Mem{RipRel: true, Label: fdLabel(v.Name)})
			return nil
		}
		return fmt.Errorf("未定义的变量: %s", v.Name)
	case *UnaryExpr:
		if v.Op == "*" {
			return fg.genExpr(v.X)
		}
	case *IndexExpr:
		return fg.genAddrIndex(v)
	case *MemberExpr:
		return fg.genAddrMember(v)
	case *CompoundLit:
		if err := fg.genCompoundLit(v); err != nil {
			return err
		}
		return nil
	case *CallExpr:
		// 函数返回 struct → 结果就是地址
		if t := v.Type(); t != nil && t.IsRecord() {
			return fg.genCall(v)
		}
	}
	return fmt.Errorf("无法取地址的表达式 %T", e)
}

// genAddrIndex 生成 a[i] 的地址
func (fg *funcGen) genAddrIndex(v *IndexExpr) error {
	bt := v.X.Type()
	var elem *CType
	if bt != nil {
		switch bt.Kind {
		case TyPtr, TyArray:
			elem = bt.Elem
		}
	}
	if err := fg.genExpr(v.X); err != nil {
		return err
	}
	tmp := fg.alloc(8)
	fg.a.MovStore(stackMem(tmp), RAX)
	if err := fg.genAsInt(v.Index); err != nil {
		return err
	}
	if elem != nil && elem.Size > 1 {
		fg.a.ImulRRI(RAX, RAX, int32(elem.Size))
	}
	fg.a.MovLoad(RCX, stackMem(tmp))
	fg.a.AddRR(RAX, RCX)
	fg.free(8)
	return nil
}

// genAddrMember 生成 x.f / p->f 的地址
func (fg *funcGen) genAddrMember(v *MemberExpr) error {
	if v.Arrow {
		if err := fg.genExpr(v.X); err != nil {
			return err
		}
	} else {
		if err := fg.genAddr(v.X); err != nil {
			return err
		}
	}
	if v.Off != 0 {
		fg.a.Lea(RAX, &Mem{Base: RAX, Index: -1, Scale: 1, Disp: int32(v.Off)})
	}
	return nil
}

// genAsInt 生成表达式并保证整数结果在 rax
func (fg *funcGen) genAsInt(e Expr) error {
	t := e.Type()
	if err := fg.genExpr(e); err != nil {
		return err
	}
	if t != nil && t.IsFloat() {
		fg.a.Cvttsd2si(RAX, 0)
	}
	return nil
}

// genAsFloat 生成表达式并保证浮点结果在 xmm0
func (fg *funcGen) genAsFloat(e Expr) error {
	t := e.Type()
	if err := fg.genExpr(e); err != nil {
		return err
	}
	if t != nil && !t.IsFloat() {
		fg.a.Cvtsi2sd(0, RAX)
	}
	return nil
}

func isCompareOp(op string) bool {
	switch op {
	case "==", "!=", "<", ">", "<=", ">=":
		return true
	}
	return false
}

// genBinary 二元表达式
func (fg *funcGen) genBinary(v *BinaryExpr) error {
	lt, rt := v.X.Type(), v.Y.Type()
	if v.Op == "," {
		if err := fg.genExpr(v.X); err != nil {
			return err
		}
		return fg.genExpr(v.Y)
	}
	if v.Op == "&&" || v.Op == "||" {
		return fg.genLogical(v)
	}
	// 指针算术
	if (v.Op == "+" || v.Op == "-") && lt != nil && rt != nil {
		if lt.IsPtr() && rt.IsInteger() {
			sz := int64(1)
			if lt.Elem != nil && lt.Elem.Size > 0 {
				sz = lt.Elem.Size
			}
			if err := fg.genAsInt(v.X); err != nil {
				return err
			}
			tmp := fg.alloc(8)
			fg.a.MovStore(stackMem(tmp), RAX)
			if err := fg.genAsInt(v.Y); err != nil {
				return err
			}
			if sz > 1 {
				fg.a.ImulRRI(RAX, RAX, int32(sz))
			}
			fg.a.MovRR(RCX, RAX)
			fg.a.MovLoad(RAX, stackMem(tmp))
			if v.Op == "+" {
				fg.a.AddRR(RAX, RCX)
			} else {
				fg.a.SubRR(RAX, RCX)
			}
			fg.free(8)
			return nil
		}
		if rt.IsPtr() && lt.IsInteger() && v.Op == "+" {
			sz := int64(1)
			if rt.Elem != nil && rt.Elem.Size > 0 {
				sz = rt.Elem.Size
			}
			if err := fg.genAsInt(v.X); err != nil {
				return err
			}
			if sz > 1 {
				fg.a.ImulRRI(RAX, RAX, int32(sz))
			}
			tmp := fg.alloc(8)
			fg.a.MovStore(stackMem(tmp), RAX)
			if err := fg.genAsInt(v.Y); err != nil {
				return err
			}
			fg.a.MovRR(RCX, RAX)
			fg.a.MovLoad(RAX, stackMem(tmp))
			fg.a.AddRR(RAX, RCX)
			fg.free(8)
			return nil
		}
		if lt.IsPtr() && rt.IsPtr() && v.Op == "-" {
			if err := fg.genAsInt(v.X); err != nil {
				return err
			}
			tmp := fg.alloc(8)
			fg.a.MovStore(stackMem(tmp), RAX)
			if err := fg.genAsInt(v.Y); err != nil {
				return err
			}
			fg.a.MovRR(RCX, RAX)
			fg.a.MovLoad(RAX, stackMem(tmp))
			fg.a.SubRR(RAX, RCX)
			if lt.Elem != nil && lt.Elem.Size > 1 {
				fg.a.MovImm32(RCX, int32(lt.Elem.Size))
				fg.a.Cqo()
				fg.a.Idiv(RCX)
			}
			fg.free(8)
			return nil
		}
	}
	if isCompareOp(v.Op) {
		return fg.genCompare(v)
	}
	if v.Type() != nil && v.Type().IsFloat() {
		return fg.genFloatArith(v)
	}
	return fg.genIntArith(v)
}

func (fg *funcGen) genIntArith(v *BinaryExpr) error {
	if err := fg.genAsInt(v.X); err != nil {
		return err
	}
	tmp := fg.alloc(8)
	fg.a.MovStore(stackMem(tmp), RAX)
	if err := fg.genAsInt(v.Y); err != nil {
		return err
	}
	fg.a.MovRR(RCX, RAX)
	fg.a.MovLoad(RAX, stackMem(tmp))
	fg.free(8)
	switch v.Op {
	case "+":
		fg.a.AddRR(RAX, RCX)
	case "-":
		fg.a.SubRR(RAX, RCX)
	case "*":
		fg.a.ImulRR(RAX, RCX)
	case "/":
		fg.a.Cqo()
		fg.a.Idiv(RCX)
	case "%":
		fg.a.Cqo()
		fg.a.Idiv(RCX)
		fg.a.MovRR(RAX, RDX)
	case "&":
		fg.a.AndRR(RAX, RCX)
	case "|":
		fg.a.OrRR(RAX, RCX)
	case "^":
		fg.a.XorRR(RAX, RCX)
	case "<<":
		fg.a.ShiftCL(4, RAX)
	case ">>":
		if v.Type() != nil && v.Type().Unsigned {
			fg.a.ShiftCL(5, RAX)
		} else {
			fg.a.ShiftCL(7, RAX)
		}
	default:
		return fmt.Errorf("未支持的整数运算: %s", v.Op)
	}
	return nil
}

func (fg *funcGen) genFloatArith(v *BinaryExpr) error {
	if err := fg.genAsFloat(v.X); err != nil {
		return err
	}
	tmp := fg.alloc(8)
	fg.a.MovsdStore(stackMem(tmp), 0)
	if err := fg.genAsFloat(v.Y); err != nil {
		return err
	}
	fg.a.MovsdRR(1, 0)
	fg.a.MovsdLoad(0, stackMem(tmp))
	fg.free(8)
	switch v.Op {
	case "+":
		fg.a.SseArith(0, 0, 1)
	case "-":
		fg.a.SseArith(2, 0, 1)
	case "*":
		fg.a.SseArith(1, 0, 1)
	case "/":
		fg.a.SseArith(3, 0, 1)
	default:
		return fmt.Errorf("未支持的浮点运算: %s", v.Op)
	}
	return nil
}

func (fg *funcGen) genCompare(v *BinaryExpr) error {
	lt, rt := v.X.Type(), v.Y.Type()
	if (lt != nil && lt.IsFloat()) || (rt != nil && rt.IsFloat()) {
		if err := fg.genAsFloat(v.X); err != nil {
			return err
		}
		tmp := fg.alloc(8)
		fg.a.MovsdStore(stackMem(tmp), 0)
		if err := fg.genAsFloat(v.Y); err != nil {
			return err
		}
		fg.a.MovsdRR(1, 0)
		fg.a.MovsdLoad(0, stackMem(tmp))
		fg.free(8)
		fg.a.Ucomisd(0, 1)
		// C 语义要求处理 NaN：== != < <= 需结合 PF（无序）判定
		cc := 4
		mode := 0 // 0=直接；1=and(setnp)；2=or(setp)
		switch v.Op {
		case "==":
			cc, mode = 4, 1 // sete & setnp
		case "!=":
			cc, mode = 5, 2 // setne | setp
		case "<":
			cc, mode = 2, 1 // setb & setnp
		case ">":
			cc = 7 // seta（无序时已为假）
		case "<=":
			cc, mode = 6, 1 // setbe & setnp
		case ">=":
			cc = 3 // setae（无序时已为假）
		}
		fg.a.Setcc(cc, RAX)
		if mode != 0 {
			// mode=1(and) 需 setnp(PF=0)；mode=2(or) 需 setp(PF=1)
			pcc := 10
			if mode == 1 {
				pcc = 11
			}
			fg.a.Setcc(pcc, RCX)
			fg.a.MovzxR8(RAX, RAX)
			fg.a.MovzxR8(RCX, RCX)
			if mode == 1 {
				fg.a.AndRR(RAX, RCX)
			} else {
				fg.a.OrRR(RAX, RCX)
			}
			return nil
		}
		fg.a.MovzxR8(RAX, RAX)
		return nil
	}
	if err := fg.genAsInt(v.X); err != nil {
		return err
	}
	tmp := fg.alloc(8)
	fg.a.MovStore(stackMem(tmp), RAX)
	if err := fg.genAsInt(v.Y); err != nil {
		return err
	}
	fg.a.MovRR(RCX, RAX)
	fg.a.MovLoad(RAX, stackMem(tmp))
	fg.free(8)
	fg.a.CmpRR(RAX, RCX)
	uns := (lt != nil && lt.Unsigned) || (rt != nil && rt.Unsigned)
	cc := 4
	switch v.Op {
	case "==":
		cc = 4
	case "!=":
		cc = 5
	case "<":
		if uns {
			cc = 2
		} else {
			cc = 12
		}
	case ">":
		if uns {
			cc = 7
		} else {
			cc = 15
		}
	case "<=":
		if uns {
			cc = 6
		} else {
			cc = 14
		}
	case ">=":
		if uns {
			cc = 3
		} else {
			cc = 13
		}
	}
	fg.a.Setcc(cc, RAX)
	fg.a.MovzxR8(RAX, RAX)
	return nil
}

func (fg *funcGen) genLogical(v *BinaryExpr) error {
	end := fg.c.newLabel("logic")
	shortL := fg.c.newLabel("short")
	if err := fg.genCond(v.X); err != nil {
		return err
	}
	fg.a.CmpImm32(RAX, 0)
	if v.Op == "&&" {
		fg.a.Jcc(4, shortL) // je → 假
	} else {
		fg.a.Jcc(5, shortL) // jne → 真
	}
	if err := fg.genCond(v.Y); err != nil {
		return err
	}
	fg.a.CmpImm32(RAX, 0)
	if v.Op == "&&" {
		fg.a.Jcc(4, shortL)
	} else {
		fg.a.Jcc(5, shortL)
	}
	fg.a.MovImm32(RAX, 0)
	if v.Op == "&&" {
		fg.a.MovImm32(RAX, 1)
	}
	fg.a.JmpLabel(end)
	fg.a.Bind(shortL)
	if v.Op == "&&" {
		fg.a.MovImm32(RAX, 0)
	} else {
		fg.a.MovImm32(RAX, 1)
	}
	fg.a.Bind(end)
	return nil
}

// genUnary 一元表达式
func (fg *funcGen) genUnary(v *UnaryExpr) error {
	switch v.Op {
	case "&":
		return fg.genAddr(v.X)
	case "*":
		if err := fg.genExpr(v.X); err != nil {
			return err
		}
		fg.a.MovRR(RDX, RAX)
		fg.loadFrom(RDX, v.Type())
		return nil
	case "-":
		if v.Type() != nil && v.Type().IsFloat() {
			if err := fg.genAsFloat(v.X); err != nil {
				return err
			}
			fg.a.MovImm64(RAX, -9223372036854775807-1)
			fg.a.MovqXmmGpr(1, RAX)
			fg.a.SseArith(3, 1, 1) // 占位
			// 用异或翻转符号位更简单：这里用 0 - x
			fg.a.Pxor(1, 1)
			fg.a.SseArith(2, 1, 0) // 1 = 0 - x
			fg.a.MovsdRR(0, 1)
			return nil
		}
		if err := fg.genAsInt(v.X); err != nil {
			return err
		}
		fg.a.Neg(RAX)
		return nil
	case "+":
		return fg.genExpr(v.X)
	case "!":
		if err := fg.genCond(v.X); err != nil {
			return err
		}
		fg.a.CmpImm32(RAX, 0)
		fg.a.Setcc(4, RAX)
		fg.a.MovzxR8(RAX, RAX)
		return nil
	case "~":
		if err := fg.genAsInt(v.X); err != nil {
			return err
		}
		fg.a.Not(RAX)
		return nil
	case "++", "--":
		return fg.genIncDec(v.X, v.Op, true)
	}
	return fmt.Errorf("未支持的一元运算: %s", v.Op)
}

// genIncDec 自增自减（prefix=true 返回新值）
func (fg *funcGen) genIncDec(x Expr, op string, prefix bool) error {
	t := x.Type()
	if err := fg.genAddr(x); err != nil {
		return err
	}
	addr := fg.alloc(8)
	fg.a.MovStore(stackMem(addr), RAX)
	fg.a.MovLoad(RDX, stackMem(addr))
	fg.loadFrom(RDX, t)

	if t != nil && t.IsFloat() {
		// old 存起来
		old := fg.alloc(8)
		fg.a.MovsdStore(stackMem(old), 0)
		fg.a.MovImm64(RAX, int64(math.Float64bits(1.0)))
		fg.a.MovqXmmGpr(1, RAX)
		if op == "++" {
			fg.a.SseArith(0, 0, 1)
		} else {
			fg.a.SseArith(2, 0, 1)
		}
		fg.a.MovLoad(RCX, stackMem(addr))
		fg.a.MovsdStore(&Mem{Base: RCX, Index: -1, Scale: 1}, 0)
		if !prefix {
			fg.a.MovsdLoad(0, stackMem(old))
		}
		fg.free(16)
		return nil
	}

	old := fg.alloc(8)
	fg.a.MovStore(stackMem(old), RAX)
	if op == "++" {
		fg.a.MovImm32(RCX, 1)
		fg.a.AddRR(RAX, RCX)
	} else {
		fg.a.MovImm32(RCX, 1)
		fg.a.SubRR(RAX, RCX)
	}
	fg.a.MovLoad(RDX, stackMem(addr))
	fg.storeTo(RDX, t)
	if !prefix {
		fg.a.MovLoad(RAX, stackMem(old))
	}
	fg.free(16)
	return nil
}

// genAssign 赋值
func (fg *funcGen) genAssign(v *AssignExpr) error {
	lt := v.L.Type()
	if lt == nil {
		if _, err := fg.c.sema.typeOf(v.L); err != nil {
			return err
		}
		lt = v.L.Type()
	}
	if err := fg.genAddr(v.L); err != nil {
		return err
	}
	addr := fg.alloc(8)
	fg.a.MovStore(stackMem(addr), RAX)

	if v.Op == "=" {
		if lt != nil && lt.IsRecord() {
			// struct 赋值：右值表达式值即地址
			if err := fg.genExpr(v.R); err != nil {
				return err
			}
			fg.emitMemcpyReg(RAX, -1, addr, lt.Size)
			fg.a.MovLoad(RAX, stackMem(addr))
			fg.free(8)
			return nil
		}
		if err := fg.genExpr(v.R); err != nil {
			return err
		}
		fg.a.MovLoad(RCX, stackMem(addr))
		fg.storeTo(RCX, lt)
		fg.free(8)
		return nil
	}

	// 复合赋值
	op := v.Op[:len(v.Op)-1]
	// 读旧值
	fg.a.MovLoad(RDX, stackMem(addr))
	fg.a.MovStore(stackMem(addr), RDX)
	// 保存地址
	if lt != nil && lt.IsFloat() {
		fg.loadFrom(RDX, lt)
		old := fg.alloc(8)
		fg.a.MovsdStore(stackMem(old), 0)
		if err := fg.genAsFloat(v.R); err != nil {
			return err
		}
		fg.a.MovsdRR(1, 0)
		fg.a.MovsdLoad(0, stackMem(old))
		switch op {
		case "+":
			fg.a.SseArith(0, 0, 1)
		case "-":
			fg.a.SseArith(2, 0, 1)
		case "*":
			fg.a.SseArith(1, 0, 1)
		case "/":
			fg.a.SseArith(3, 0, 1)
		default:
			return fmt.Errorf("未支持的复合赋值: %s", v.Op)
		}
		fg.a.MovLoad(RCX, stackMem(addr))
		fg.a.MovsdStore(&Mem{Base: RCX, Index: -1, Scale: 1}, 0)
		fg.free(16)
		return nil
	}
	fg.loadFrom(RDX, lt)
	old := fg.alloc(8)
	fg.a.MovStore(stackMem(old), RAX)
	if err := fg.genAsInt(v.R); err != nil {
		return err
	}
	fg.a.MovRR(RCX, RAX)
	fg.a.MovLoad(RAX, stackMem(old))
	switch op {
	case "+":
		fg.a.AddRR(RAX, RCX)
	case "-":
		fg.a.SubRR(RAX, RCX)
	case "*":
		fg.a.ImulRR(RAX, RCX)
	case "/":
		fg.a.Cqo()
		fg.a.Idiv(RCX)
	case "%":
		fg.a.Cqo()
		fg.a.Idiv(RCX)
		fg.a.MovRR(RAX, RDX)
	case "&":
		fg.a.AndRR(RAX, RCX)
	case "|":
		fg.a.OrRR(RAX, RCX)
	case "^":
		fg.a.XorRR(RAX, RCX)
	case "<<":
		fg.a.ShiftCL(4, RAX)
	case ">>":
		fg.a.ShiftCL(7, RAX)
	default:
		return fmt.Errorf("未支持的复合赋值: %s", v.Op)
	}
	fg.a.MovLoad(RDX, stackMem(addr))
	fg.storeTo(RDX, lt)
	fg.free(16)
	return nil
}

// genCondExpr 三目表达式
func (fg *funcGen) genCondExpr(v *CondExpr) error {
	elseL := fg.c.newLabel("celse")
	endL := fg.c.newLabel("cend")
	if err := fg.genCond(v.Cond); err != nil {
		return err
	}
	fg.a.CmpImm32(RAX, 0)
	fg.a.Jcc(4, elseL)
	if err := fg.genExpr(v.Then); err != nil {
		return err
	}
	fg.a.JmpLabel(endL)
	fg.a.Bind(elseL)
	if err := fg.genExpr(v.Else); err != nil {
		return err
	}
	fg.a.Bind(endL)
	return nil
}

// genCast 类型转换
func (fg *funcGen) genCast(v *CastExpr) error {
	st := v.X.Type()
	if st == nil {
		if _, err := fg.c.sema.typeOf(v.X); err != nil {
			return err
		}
		st = v.X.Type()
	}
	dst := v.To
	if dst.IsFloat() {
		return fg.genAsFloat(v.X)
	}
	if dst.IsPtr() || dst.IsInteger() {
		if st != nil && st.IsFloat() {
			return fg.genAsInt(v.X)
		}
		// 内部用 64 位寄存器表示；窄类型在 load/store 阶段已按宽度处理，
		// 显式转换不需再读内存截断（旧实现误把寄存器值当地址二次解引用）。
		return fg.genExpr(v.X)
	}
	return fg.genExpr(v.X)
}

// sizeofOf 计算 sizeof 的编译期值
func (fg *funcGen) sizeofOf(v *SizeofExpr) int64 {
	if v.IsType {
		if v.Ty.SizeOf() < 0 {
			return 0
		}
		return v.Ty.Size
	}
	if v.X != nil {
		if v.X.Type() == nil {
			fg.c.sema.typeOf(v.X)
		}
		if t := v.X.Type(); t != nil && t.SizeOf() >= 0 {
			return t.Size
		}
	}
	return 8
}

// genCompoundLit 复合字面量：栈上构造
func (fg *funcGen) genCompoundLit(v *CompoundLit) error {
	// 未知长度数组 T[]：按初始化列表元素个数推导长度
	if v.Ty.Kind == TyArray && v.Ty.ArrayLen < 0 && v.Init != nil {
		n := int64(len(v.Init.List))
		v.Ty.ArrayLen = n
		v.Ty.Incomplete = false
		if v.Ty.Elem != nil {
			v.Ty.Size = v.Ty.Elem.Size * n
		}
	}
	sz := v.Ty.Size
	if sz <= 0 {
		sz = 8
	}
	off := fg.alloc(int32(sz))
	// 清零
	fg.zeroMem(off, sz)
	fg.a.Lea(RAX, stackMem(off))
	if v.Init != nil {
		if err := fg.genInit(RAX, v.Ty, v.Init); err != nil {
			return err
		}
	}
	fg.a.Lea(RAX, stackMem(off))
	return nil
}

// zeroMem 清零 [rbp+off] 处 size 字节
func (fg *funcGen) zeroMem(off int32, size int64) {
	for i := int64(0); i < size; i += 8 {
		fg.a.MovImm32(RCX, 0)
		fg.a.MovStore(stackMem(off+int32(i)), RCX)
	}
}
