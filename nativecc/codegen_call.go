package nativecc

import "fmt"

// argInfo 一个实参的临时存贮信息
type argInfo struct {
	off      int32
	ty       *CType
	isFloat  bool
	isRecord bool
}

// genCall 函数调用
func (fg *funcGen) genCall(v *CallExpr) error {
	ret := v.Type()
	if ret == nil {
		if _, err := fg.c.sema.typeOf(v); err != nil {
			return err
		}
		ret = v.Type()
	}
	isStructRet := ret != nil && ret.IsRecord()

	// 临时区只增不减（返回缓冲必须活到使用点之后）
	// 编译器内建
	if id, ok := v.Fn.(*Ident); ok && Builtins[id.Name] {
		return fg.genBuiltin(&BuiltinCall{Name: id.Name, Args: v.Args, exprBase: exprBase{ty: TyIntT}})
	}

	// 判定调用目标
	isDirect := false
	isImport := false
	funcName := ""
	if id, ok := v.Fn.(*Ident); ok {
		if fd, ok2 := fg.c.sema.Funcs[id.Name]; ok2 && fd.Body != nil {
			isDirect = true
			funcName = id.Name
		} else if _, ok3 := Imports[id.Name]; ok3 {
			isDirect = true
			isImport = true
			funcName = id.Name
		} else if _, ok2 := fg.c.sema.Funcs[id.Name]; ok2 {
			isDirect = true
			funcName = id.Name
		}
	}

	// 求值参数（从右到左）
	args := make([]argInfo, len(v.Args))
	for i := len(v.Args) - 1; i >= 0; i-- {
		a := v.Args[i]
		t := a.Type()
		if t == nil {
			if _, err := fg.c.sema.typeOf(a); err != nil {
				return err
			}
			t = a.Type()
		}
		ai := argInfo{ty: t}
		if t != nil && t.IsRecord() {
			ai.isRecord = true
			if err := fg.genExpr(a); err != nil {
				return err
			}
			ai.off = fg.alloc(8)
			fg.a.MovStore(stackMem(ai.off), RAX)
		} else if t != nil && t.IsFloat() {
			ai.isFloat = true
			if err := fg.genAsFloat(a); err != nil {
				return err
			}
			ai.off = fg.alloc(8)
			fg.a.MovsdStore(stackMem(ai.off), 0)
		} else {
			if err := fg.genAsInt(a); err != nil {
				return err
			}
			ai.off = fg.alloc(8)
			fg.a.MovStore(stackMem(ai.off), RAX)
		}
		args[i] = ai
	}

	// 分配寄存器
	intRegs := []int{RCX, RDX, R8, R9}
	floatRegs := []int{0, 1, 2, 3}
	ii, fi := 0, 0
	retBuf := int32(0)
	if isStructRet {
		sz := ret.Size
		if sz <= 0 {
			sz = 8
		}
		retBuf = fg.alloc(int32(sz))
		fg.zeroMem(retBuf, sz)
		fg.a.Lea(RCX, stackMem(retBuf))
		ii = 1
	}
	// 可变参数函数（printf 系列）：MS ABI 要求浮点参数在 GP 与 XMM 各放一份
	isVariadic := false
	if isDirect && isImport {
		if sym, ok := Imports[funcName]; ok && sym.NArgs < 0 {
			isVariadic = true
		}
	}

	var stackArgs []argInfo
	for _, ai := range args {
		if isVariadic {
			// 每个参数都占一个 GP 槽（浮点放副本），浮点另占一个 XMM 槽
			if ai.isFloat {
				if fi < len(floatRegs) {
					fg.a.MovsdLoad(floatRegs[fi], stackMem(ai.off))
					fi++
				}
				if ii < len(intRegs) && fi > 0 {
					fg.a.MovqGprXmm(intRegs[ii], floatRegs[fi-1])
					ii++
				} else {
					stackArgs = append(stackArgs, ai)
				}
				continue
			}
			if ii < len(intRegs) {
				fg.a.MovLoad(intRegs[ii], stackMem(ai.off))
				ii++
			} else {
				stackArgs = append(stackArgs, ai)
			}
			continue
		}
		if ai.isFloat {
			if fi < len(floatRegs) {
				fg.a.MovsdLoad(floatRegs[fi], stackMem(ai.off))
				fi++
			} else {
				stackArgs = append(stackArgs, ai)
			}
			continue
		}
		if ii < len(intRegs) {
			fg.a.MovLoad(intRegs[ii], stackMem(ai.off))
			ii++
		} else {
			stackArgs = append(stackArgs, ai)
		}
	}
	// 栈参数写到 [rsp+32+i*8]
	for i, ai := range stackArgs {
		disp := int32(32 + i*8)
		if ai.isFloat {
			fg.a.MovsdLoad(0, stackMem(ai.off))
			fg.a.MovsdStore(&Mem{Base: RSP, Index: -1, Scale: 1, Disp: disp}, 0)
		} else {
			fg.a.MovLoad(RAX, stackMem(ai.off))
			fg.a.MovStore(&Mem{Base: RSP, Index: -1, Scale: 1, Disp: disp}, RAX)
		}
	}

	// 调用
	if isDirect {
		if isImport {
			fg.callImport(funcName)
		} else {
			fg.a.CallLabel(fdLabel(funcName))
		}
	} else {
		if err := fg.genExpr(v.Fn); err != nil {
			return err
		}
		fg.a.CallReg(RAX)
	}

	// 结果
	if isStructRet {
		fg.a.Lea(RAX, stackMem(retBuf))
	}
	return nil
}

// genInit 把 init 初始化到 [addrReg] 指向的内存
func (fg *funcGen) genInit(addrReg int, t *CType, init *Init) error {
	if init == nil {
		return nil
	}
	save := fg.alloc(8)
	defer fg.free(8)
	if addrReg != RAX {
		fg.a.MovRR(RAX, addrReg)
	}
	fg.a.MovStore(stackMem(save), RAX)

	if init.IsList {
		if t != nil && t.IsRecord() {
			// {0} / {1,2} 聚合初始化：未覆盖字段默认 0
			fg.a.MovLoad(RCX, stackMem(save))
			fg.zeroMemAt(RCX, t.Size)
		}
		if t != nil && t.Kind == TyArray {
			esz := t.Elem.Size
			if esz <= 0 {
				esz = 8
			}
			for i, sub := range init.List {
				if t.ArrayLen >= 0 && int64(i) >= t.ArrayLen {
					break
				}
				fg.a.MovLoad(RCX, stackMem(save))
				if i > 0 {
					fg.a.Lea(RCX, &Mem{Base: RCX, Index: -1, Scale: 1, Disp: int32(int64(i) * esz)})
				}
				if err := fg.genInit(RCX, t.Elem, sub); err != nil {
					return err
				}
			}
			return nil
		}
		if t != nil && t.IsRecord() {
			for i, sub := range init.List {
				if i >= len(t.Fields) {
					break
				}
				f := t.Fields[i]
				fg.a.MovLoad(RCX, stackMem(save))
				if f.Offset != 0 {
					fg.a.Lea(RCX, &Mem{Base: RCX, Index: -1, Scale: 1, Disp: int32(f.Offset)})
				}
				if err := fg.genInit(RCX, f.Ty, sub); err != nil {
					return err
				}
			}
			return nil
		}
		if len(init.List) == 1 {
			fg.a.MovLoad(RCX, stackMem(save))
			return fg.genInit(RCX, t, init.List[0])
		}
		return nil
	}

	// 标量 / struct 值
	if t != nil && t.IsRecord() {
		// 右值 struct → memcpy
		if err := fg.genExpr(init.Expr); err != nil {
			return err
		}
		fg.emitMemcpyReg(RAX, -1, save, t.Size)
		return nil
	}
	if t != nil && t.IsFloat() {
		if err := fg.genAsFloat(init.Expr); err != nil {
			return err
		}
		fg.a.MovLoad(RCX, stackMem(save))
		fg.a.MovsdStore(&Mem{Base: RCX, Index: -1, Scale: 1}, 0)
		return nil
	}
	if err := fg.genAsInt(init.Expr); err != nil {
		return err
	}
	fg.a.MovLoad(RCX, stackMem(save))
	if t == nil {
		fg.a.MovStore(&Mem{Base: RCX, Index: -1, Scale: 1}, RAX)
		return nil
	}
	switch t.Size {
	case 1:
		fg.a.MovStore8(&Mem{Base: RCX, Index: -1, Scale: 1}, RAX)
	case 2:
		fg.a.MovStore16(&Mem{Base: RCX, Index: -1, Scale: 1}, RAX)
	case 4:
		fg.a.MovStore32(&Mem{Base: RCX, Index: -1, Scale: 1}, RAX)
	default:
		fg.a.MovStore(&Mem{Base: RCX, Index: -1, Scale: 1}, RAX)
	}
	return nil
}

// genBuiltin 编译器内建函数
func (fg *funcGen) genBuiltin(v *BuiltinCall) error {
	switch v.Name {
	case "__builtin_add_overflow", "__builtin_sub_overflow", "__builtin_mul_overflow":
		if len(v.Args) != 3 {
			return fmt.Errorf("%s 需要 3 个参数", v.Name)
		}
		a := fg.alloc(8)
		b := fg.alloc(8)
		if err := fg.genAsInt(v.Args[0]); err != nil {
			return err
		}
		fg.a.MovStore(stackMem(a), RAX)
		if err := fg.genAsInt(v.Args[1]); err != nil {
			return err
		}
		fg.a.MovStore(stackMem(b), RAX)
		fg.a.MovLoad(RAX, stackMem(a))
		fg.a.MovLoad(RCX, stackMem(b))
		switch v.Name {
		case "__builtin_add_overflow":
			fg.a.AddRR(RAX, RCX)
		case "__builtin_sub_overflow":
			fg.a.SubRR(RAX, RCX)
		case "__builtin_mul_overflow":
			fg.a.ImulRR(RAX, RCX)
		}
		res := fg.alloc(8)
		fg.a.MovStore(stackMem(res), RAX)
		fg.a.Setcc(0, R11) // seto
		// 存结果到第三参数指向的地址（第三参数是 &r，取表达式值即地址）
		if err := fg.genAsInt(v.Args[2]); err != nil {
			return err
		}
		fg.a.MovRR(RCX, RAX)
		fg.a.MovLoad(RAX, stackMem(res))
		fg.a.MovStore(&Mem{Base: RCX, Index: -1, Scale: 1}, RAX)
		fg.a.MovzxR8(R11, R11)
		fg.a.MovRR(RAX, R11)
		fg.free(24)
		return nil
	case "__builtin_offsetof":
		fg.a.MovImm64(RAX, 0)
		return nil
	}
	return fmt.Errorf("未支持的内建函数: %s", v.Name)
}

// zeroMemAt 通过 rep stosb 风格循环清零 [addrReg] 的 size 字节
func (fg *funcGen) zeroMemAt(addrReg int, size int64) {
	if size <= 0 {
		return
	}
	// 简单逐 8 字节清零（以对齐为准；不足 8 字节的部分用 8 字节写覆盖，由调用方确保不越界）
	dst := fg.alloc(8)
	fg.a.MovStore(stackMem(dst), addrReg)
	n := int(size / 8)
	if size%8 != 0 {
		n++
	}
	for i := 0; i < n; i++ {
		fg.a.MovLoad(RCX, stackMem(dst))
		fg.a.Lea(RCX, &Mem{Base: RCX, Index: -1, Scale: 1, Disp: int32(i * 8)})
		fg.a.MovImm32(RAX, 0)
		fg.a.MovStore(&Mem{Base: RCX, Index: -1, Scale: 1}, RAX)
	}
	fg.free(8)
}
