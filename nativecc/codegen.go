package nativecc

import (
	"fmt"
	"math"
)

// Compiled 编译产物
type Compiled struct {
	Text    []byte
	RData   []byte
	Data    []byte
	Entry   string
	Imports map[string]ImportSym
	// 符号表：标签 → 段内偏移（供 PE 写出）
	TextLabels  map[string]int
	RDataLabels map[string]int
	DataLabels  map[string]int
	// .rdata 中的标签重定位（lea rip+label 需要知道目标段）
	GlobalCount int
	Asm         *Assembler
	PtrRelocs   []globalPtrReloc
}

// Compiler 代码生成器
type Compiler struct {
	tu   *TranslationUnit
	sema *Sema

	text   *Assembler
	rodata []byte
	data   []byte

	rdataLabels map[string]int
	dataLabels  map[string]int
	textLabels  map[string]int

	imports map[string]bool
	strs    map[string]string // 字符串内容 → 标签（复用）
	strCnt  int
	lblCnt  int

	dataSize int64
	// .data 中需要指向 .rdata 的指针槽（PE 阶段重定位）
	globalPtrRelocs []globalPtrReloc
}

// Compile 生成代码
func Compile(tu *TranslationUnit) (*Compiled, error) {
	c := &Compiler{
		tu:          tu,
		text:        NewAssembler(),
		rdataLabels: map[string]int{},
		dataLabels:  map[string]int{},
		textLabels:  map[string]int{},
		imports:     map[string]bool{},
		strs:        map[string]string{},
	}
	c.sema = &Sema{}
	if err := c.sema.Check(tu); err != nil {
		return nil, err
	}

	// 0. 启动 stub（PE 入口）
	c.genStartupStub()

	// 0.1 setjmp/longjmp 自带实现（不依赖 msvcrt 的 _setjmpex 与 PE 展开信息）
	c.genJumpHelpers()

	// 1. 全局变量
	if err := c.genGlobals(); err != nil {
		return nil, err
	}

	// 2. 函数
	for _, d := range tu.Decls {
		fd, ok := d.(*FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if err := c.genFunc(fd); err != nil {
			return nil, fmt.Errorf("函数 %s 生成失败: %w", fd.Name, err)
		}
	}

	// 合并汇编器内部标签
	for k, v := range c.text.labels {
		if _, ok := c.textLabels[k]; !ok {
			c.textLabels[k] = v
		}
	}

	out := &Compiled{
		Text:        c.text.Buf,
		RData:       c.rodata,
		Data:        c.data,
		Entry:       "main",
		Asm:         c.text,
		Imports:     map[string]ImportSym{},
		TextLabels:  c.textLabels,
		RDataLabels: c.rdataLabels,
		DataLabels:  c.dataLabels,
		PtrRelocs:   c.globalPtrRelocs,
	}
	for name := range c.imports {
		if sym, ok := Imports[name]; ok {
			out.Imports[name] = sym
		} else if name == "__iob_func" {
			out.Imports[name] = ImportSym{"msvcrt.dll", "", 0}
		}
	}
	return out, nil
}

// genStartupStub 生成 PE 入口 stub：ExitProcess(main(0, NULL))
func (c *Compiler) genStartupStub() {
	c.text.Bind("L_entry")
	c.text.XorRR(RCX, RCX)
	c.text.XorRR(RDX, RDX)
	c.text.SubImm8(RSP, 0x28)
	c.text.CallLabel(fdLabel("main"))
	c.text.MovRR32(RCX, RAX)
	c.imports["ExitProcess"] = true
	c.text.CallMem(importMem("ExitProcess"))
	c.text.Int3()
	c.textLabels["L_entry"] = 0
}

// newLabel 生成唯一标签
func (c *Compiler) newLabel(prefix string) string {
	c.lblCnt++
	return fmt.Sprintf(".%s%d", prefix, c.lblCnt)
}

// internString 字符串常量入 .rdata，返回标签
func (c *Compiler) internString(s string) string {
	if lab, ok := c.strs[s]; ok {
		return lab
	}
	c.strCnt++
	lab := fmt.Sprintf("Lstr%d", c.strCnt)
	c.rdataLabels[lab] = len(c.rodata)
	c.rodata = append(c.rodata, []byte(s)...)
	c.rodata = append(c.rodata, 0)
	c.strs[s] = lab
	return lab
}

// genGlobals 分配全局变量
func (c *Compiler) genGlobals() error {
	for _, d := range c.tu.Decls {
		vd, ok := d.(*VarDecl)
		if !ok {
			continue
		}
		if vd.IsExtern {
			continue
		}
		if _, ok := Imports[vd.Name]; ok {
			continue
		}
		sz := vd.Ty.Size
		if vd.Ty.Kind == TyArray && vd.Ty.ArrayLen < 0 {
			// 未定长数组：从初始化器推导元素数（char s[] = ".." → len+1；{a,b} → n）
			esz := int64(1)
			if vd.Ty.Elem != nil && vd.Ty.Elem.Size > 0 {
				esz = vd.Ty.Elem.Size
			}
			n := int64(-1)
			if vd.Init != nil {
				if vd.Init.IsList {
					n = int64(len(vd.Init.List))
				} else if v, err := c.constEval(vd.Init.Expr); err == nil && v.Kind == 2 {
					n = int64(len(v.S)) + 1
				}
			}
			if n > 0 {
				vd.Ty.ArrayLen = n
				vd.Ty.Size = n * esz
				sz = vd.Ty.Size
			} else {
				sz = 0
			}
		}
		if sz <= 0 {
			sz = 1
		}
		// 对齐
		for int64(len(c.data))%max64(vd.Ty.Align, 1) != 0 {
			c.data = append(c.data, 0)
		}
		c.dataLabels["Lglob_"+vd.Name] = len(c.data)
		start := len(c.data)
		for i := int64(0); i < sz; i++ {
			c.data = append(c.data, 0)
		}
		// 初始化（仅支持常量初始化）
		if vd.Init != nil {
			if err := c.initGlobal(vd.Ty, vd.Init, start); err != nil {
				return fmt.Errorf("全局变量 %s 初始化失败: %w", vd.Name, err)
			}
		}
	}
	return nil
}

// allocStatic 分配静态局部变量（放到 .data）
func (c *Compiler) allocStatic(label string, vd *VarDecl) {
	if _, ok := c.dataLabels[label]; ok {
		return
	}
	sz := vd.Ty.Size
	if sz <= 0 {
		sz = 8
	}
	start := len(c.data)
	for i := int64(0); i < sz; i++ {
		c.data = append(c.data, 0)
	}
	c.dataLabels[label] = start
	if vd.Init != nil {
		c.initGlobal(vd.Ty, vd.Init, start)
	}
}

func max64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

// initGlobal 常量初始化（写入 data 缓冲区）
func (c *Compiler) initGlobal(t *CType, init *Init, base int) error {
	if init == nil {
		return nil
	}
	if init.IsList {
		if t.Kind == TyArray {
			esz := t.Elem.Size
			for i, sub := range init.List {
				if i >= int(t.ArrayLen) && t.ArrayLen >= 0 {
					break
				}
				if err := c.initGlobal(t.Elem, sub, base+int(int64(i)*esz)); err != nil {
					return err
				}
			}
			return nil
		}
		if t.IsRecord() {
			for i, sub := range init.List {
				if i >= len(t.Fields) {
					break
				}
				f := t.Fields[i]
				if err := c.initGlobal(f.Ty, sub, base+int(f.Offset)); err != nil {
					return err
				}
			}
			return nil
		}
		if len(init.List) == 1 {
			return c.initGlobal(t, init.List[0], base)
		}
		return nil
	}
	// 标量常量
	v, err := c.constEval(init.Expr)
	if err != nil {
		return err
	}
	inRange := func(i int) bool { return i >= 0 && i < len(c.data) }
	switch v.Kind {
	case 0: // 整数
		sz := t.Size
		if sz > 8 {
			sz = 8
		}
		if sz <= 0 {
			sz = 8
		}
		for i := int64(0); i < sz; i++ {
			if inRange(base + int(i)) {
				c.data[base+int(i)] = byte(v.I >> (8 * uint(i)))
			}
		}
	case 1: // 浮点
		bits := floatBits(v.F)
		for i := 0; i < 8 && inRange(base+i); i++ {
			c.data[base+i] = byte(bits >> (8 * uint(i)))
		}
	case 2: // 字符串
		if t.Kind == TyArray && t.Elem != nil && t.Elem.Size == 1 {
			// char s[] = "..." → 逐字节复制
			for i := 0; i < len(v.S) && inRange(base+i); i++ {
				c.data[base+i] = v.S[i]
			}
			if inRange(base + len(v.S)) {
				c.data[base+len(v.S)] = 0
			}
			return nil
		}
		lab := c.internString(v.S)
		off := c.rdataLabels[lab]
		for i := 0; i < 8 && inRange(base+i); i++ {
			c.data[base+i] = byte(int64(off) >> (8 * uint(i)))
		}
		c.globalPtrRelocs = append(c.globalPtrRelocs, globalPtrReloc{DataOff: base, RDataOff: off})
	}
	return nil
}

type globalPtrReloc struct {
	DataOff  int
	RDataOff int
}

func floatBits(f float64) uint64 {
	return math.Float64bits(f)
}

// constVal 常量求值结果
type constVal struct {
	Kind int // 0=整数 1=浮点 2=字符串
	I    int64
	F    float64
	S    string
}

func (c *Compiler) constEval(e Expr) (constVal, error) {
	switch v := e.(type) {
	case *IntLit:
		return constVal{Kind: 0, I: v.V}, nil
	case *CharLit:
		return constVal{Kind: 0, I: v.V}, nil
	case *FloatLit:
		return constVal{Kind: 1, F: v.V}, nil
	case *StrLit:
		if v.Sym == "" {
			v.Sym = c.internString(v.V)
		}
		return constVal{Kind: 2, S: v.V}, nil
	case *UnaryExpr:
		if v.Op == "-" {
			sub, err := c.constEval(v.X)
			if err != nil {
				return constVal{}, err
			}
			if sub.Kind == 1 {
				return constVal{Kind: 1, F: -sub.F}, nil
			}
			return constVal{Kind: 0, I: -sub.I}, nil
		}
	case *CastExpr:
		return c.constEval(v.X)
	}
	if iv, ok := constIntValue(e); ok {
		return constVal{Kind: 0, I: iv}, nil
	}
	return constVal{}, fmt.Errorf("非常量初始化表达式")
}

// globalPtrRelocs 记录 .data 中指向 .rdata 的指针
var _ = 0

// funcGen 单个函数的代码生成上下文
type funcGen struct {
	c         *Compiler
	fd        *FuncDecl
	a         *Assembler
	localOff  map[string]int32
	byRef     map[string]bool // struct 参数按指针存储
	frame     int32
	tmpTop    int32
	tmpMax    int32
	outBase   int32 // 传出参数区起始（负偏移）
	retSlot   int32 // struct 返回值指针存放处（0 = 无）
	epilogue  string
	rename    map[string]string
	breaks    []string
	continues []string
	switches  []switchCtx
}

type switchCtx struct {
	endLabel string
	cases    []caseInfo
	defLabel string
}

type caseInfo struct {
	val   int64
	label string
}

// genFunc 生成函数代码
func (c *Compiler) genFunc(fd *FuncDecl) error {
	fg := &funcGen{
		c:        c,
		fd:       fd,
		a:        c.text,
		localOff: map[string]int32{},
		byRef:    map[string]bool{},
		rename:   map[string]string{},
	}
	fg.epilogue = c.newLabel("epi")

	// 静态局部变量 → 提升为全局数据
	for _, vd := range fd.Locals {
		if !vd.IsStatic {
			continue
		}
		gn := fd.Name + "$" + vd.Name
		fg.rename[vd.Name] = gn
		c.allocStatic("Lstatic_"+gn, vd)
	}

	// 预分配：返回值指针槽
	if fd.Ty.Ret != nil && fd.Ty.Ret.IsRecord() {
		fg.tmpTop -= 8
		fg.retSlot = fg.tmpTop
	}

	// 参数槽
	for _, p := range fd.Params {
		if p.Name == "" {
			continue
		}
		sz := p.Ty.Size
		if p.Ty.Kind == TyArray {
			sz = 8
		}
		if p.Ty.IsRecord() {
			// struct 参数：存指针
			fg.tmpTop -= 8
			fg.localOff[p.Name] = fg.tmpTop
			fg.byRef[p.Name] = true
			continue
		}
		if sz < 8 {
			sz = 8
		}
		fg.tmpTop -= int32(alignUp(sz, 8))
		fg.localOff[p.Name] = fg.tmpTop
	}

	// 局部变量槽
	for _, vd := range fd.Locals {
		if vd.IsStatic {
			continue
		}
		if _, ok := fg.localOff[vd.Name]; ok {
			continue
		}
		sz := vd.Ty.Size
		if vd.Ty.Kind == TyArray {
			if vd.Ty.ArrayLen < 0 {
				sz = 8
			}
		}
		if sz <= 0 {
			sz = 8
		}
		fg.tmpTop -= int32(alignUp(sz, 8))
		// 槽位按 16 字节对齐：x64 运行时里 jmp_buf 等结构要求 movaps 目标 16 对齐，
		// 只按 8 对齐会让 执行/遇到 的 setjmp 在成员缓冲上崩（第30轮实测 mod16=8 → access violation）
		fg.tmpTop &^= 15
		fg.localOff[vd.Name] = fg.tmpTop
	}

	fg.tmpMax = fg.tmpTop

	// 正文生成前先占位（临时区在正文中扩展）
	bodyStart := fg.a.Len()
	fg.a.Bind(fdLabel(fd.Name))

	// 序言
	fg.a.Push(RBP)
	fg.a.MovRR(RBP, RSP)
	prologueSubPos := fg.a.Len()
	fg.a.emit(0x48, 0x81, 0xEC) // sub rsp, imm32
	fg.a.emit32(0)              // 占位，最后回填

	// 参数落地。调用方在 [rsp+32+i*8] 写溢出参数；
	// 经过 call 与 push rbp 后，被调方对应地址是 [rbp+48+i*8]。
	intRegs := []int{RCX, RDX, R8, R9}
	floatRegs := []int{0, 1, 2, 3}
	ii, fi := 0, 0
	stackArgs := int32(0)
	if fd.Ty.Ret != nil && fd.Ty.Ret.IsRecord() {
		// 隐藏返回指针
		fg.a.MovStore(stackMem(fg.retSlot), RCX)
		ii = 1
	}
	for _, p := range fd.Params {
		if p.Name == "" {
			continue
		}
		off := fg.localOff[p.Name]
		stackArgMem := func() *Mem {
			return &Mem{Base: RBP, Index: -1, Scale: 1, Disp: 48 + stackArgs*8}
		}
		if p.Ty.IsRecord() {
			if ii < len(intRegs) {
				fg.a.MovStore(stackMem(off), intRegs[ii])
				ii++
			} else {
				fg.a.MovLoad(RAX, stackArgMem())
				fg.a.MovStore(stackMem(off), RAX)
				stackArgs++
			}
			continue
		}
		if p.Ty.IsFloat() {
			if fi < len(floatRegs) {
				fg.a.MovsdStore(stackMem(off), floatRegs[fi])
				fi++
			} else {
				fg.a.MovsdLoad(0, stackArgMem())
				fg.a.MovsdStore(stackMem(off), 0)
				stackArgs++
			}
			continue
		}
		if ii < len(intRegs) {
			fg.a.MovStore(stackMem(off), intRegs[ii])
			ii++
		} else {
			fg.a.MovLoad(RAX, stackArgMem())
			fg.a.MovStore(stackMem(off), RAX)
			stackArgs++
		}
	}

	// 函数体
	if err := fg.genStmt(fd.Body); err != nil {
		return err
	}

	// 尾声
	fg.a.Bind(fg.epilogue)
	fg.a.MovRR(RSP, RBP)
	fg.a.Pop(RBP)
	fg.a.Ret()

	// 计算并回填 frame 大小
	frame := -fg.tmpMax
	if frame < 0 {
		frame = 0
	}
	frame = int32(alignUp(int64(frame)+512, 16))
	fg.frame = frame
	// 回填 sub rsp, imm32
	enc := uint32(frame)
	fg.a.Buf[prologueSubPos+3] = byte(enc)
	fg.a.Buf[prologueSubPos+4] = byte(enc >> 8)
	fg.a.Buf[prologueSubPos+5] = byte(enc >> 16)
	fg.a.Buf[prologueSubPos+6] = byte(enc >> 24)

	c.textLabels[fdLabel(fd.Name)] = bodyStart
	return nil
}

func fdLabel(name string) string { return "Lfunc_" + name }

// alloc 在临时区分配空间（返回负偏移）
func (fg *funcGen) alloc(n int32) int32 {
	fg.tmpTop -= int32(alignUp(int64(n), 8))
	if fg.tmpTop < fg.tmpMax {
		fg.tmpMax = fg.tmpTop
	}
	return fg.tmpTop
}

// free 临时区只增不减：调用返回的 struct 缓冲地址会持续被使用，
// 若回收空间会被后续分配覆盖（实测 t07/t08 崩溃根因）。
func (fg *funcGen) free(n int32) {}

// genStmt 生成语句
func (fg *funcGen) genStmt(s Stmt) error {
	switch v := s.(type) {
	case *BlockStmt:
		for _, q := range v.Stmts {
			if err := fg.genStmt(q); err != nil {
				return err
			}
		}
	case *ExprStmt:
		if v.X == nil {
			return nil
		}
		return fg.genExpr(v.X)
	case *DeclStmt:
		for _, vd := range v.Decls {
			if err := fg.genLocalDecl(vd); err != nil {
				return err
			}
		}
	case *IfStmt:
		elseL := fg.c.newLabel("else")
		endL := fg.c.newLabel("endif")
		if err := fg.genCond(v.Cond); err != nil {
			return err
		}
		fg.a.CmpImm32(RAX, 0)
		fg.a.Jcc(4, elseL) // je
		if err := fg.genStmt(v.Then); err != nil {
			return err
		}
		fg.a.JmpLabel(endL)
		fg.a.Bind(elseL)
		if v.Else != nil {
			if err := fg.genStmt(v.Else); err != nil {
				return err
			}
		}
		fg.a.Bind(endL)
	case *WhileStmt:
		top := fg.c.newLabel("while")
		end := fg.c.newLabel("wend")
		fg.breaks = append(fg.breaks, end)
		fg.continues = append(fg.continues, top)
		fg.a.Bind(top)
		if err := fg.genCond(v.Cond); err != nil {
			return err
		}
		fg.a.CmpImm32(RAX, 0)
		fg.a.Jcc(4, end)
		if err := fg.genStmt(v.Body); err != nil {
			return err
		}
		fg.a.JmpLabel(top)
		fg.a.Bind(end)
		fg.breaks = fg.breaks[:len(fg.breaks)-1]
		fg.continues = fg.continues[:len(fg.continues)-1]
	case *DoWhileStmt:
		top := fg.c.newLabel("do")
		cont := fg.c.newLabel("docond")
		end := fg.c.newLabel("doend")
		fg.breaks = append(fg.breaks, end)
		fg.continues = append(fg.continues, cont)
		fg.a.Bind(top)
		if err := fg.genStmt(v.Body); err != nil {
			return err
		}
		fg.a.Bind(cont)
		if err := fg.genCond(v.Cond); err != nil {
			return err
		}
		fg.a.CmpImm32(RAX, 0)
		fg.a.Jcc(5, top) // jne
		fg.a.Bind(end)
		fg.breaks = fg.breaks[:len(fg.breaks)-1]
		fg.continues = fg.continues[:len(fg.continues)-1]
	case *ForStmt:
		if v.Init != nil {
			if err := fg.genStmt(v.Init); err != nil {
				return err
			}
		}
		top := fg.c.newLabel("for")
		cont := fg.c.newLabel("forpost")
		end := fg.c.newLabel("forend")
		fg.breaks = append(fg.breaks, end)
		fg.continues = append(fg.continues, cont)
		fg.a.Bind(top)
		if v.Cond != nil {
			if err := fg.genCond(v.Cond); err != nil {
				return err
			}
			fg.a.CmpImm32(RAX, 0)
			fg.a.Jcc(4, end)
		}
		if err := fg.genStmt(v.Body); err != nil {
			return err
		}
		fg.a.Bind(cont)
		if v.Post != nil {
			if err := fg.genExpr(v.Post); err != nil {
				return err
			}
		}
		fg.a.JmpLabel(top)
		fg.a.Bind(end)
		fg.breaks = fg.breaks[:len(fg.breaks)-1]
		fg.continues = fg.continues[:len(fg.continues)-1]
	case *SwitchStmt:
		return fg.genSwitch(v)
	case *CaseStmt:
		// 由 genSwitch 处理；独立出现时按普通语句
		if v.Stmt != nil {
			return fg.genStmt(v.Stmt)
		}
	case *BreakStmt:
		if len(fg.breaks) == 0 {
			return fmt.Errorf("break 不在循环/switch 内")
		}
		fg.a.JmpLabel(fg.breaks[len(fg.breaks)-1])
	case *ContinueStmt:
		if len(fg.continues) == 0 {
			return fmt.Errorf("continue 不在循环内")
		}
		fg.a.JmpLabel(fg.continues[len(fg.continues)-1])
	case *ReturnStmt:
		return fg.genReturn(v)
	case *LabelStmt:
		fg.a.Bind("L_" + v.Label)
		return fg.genStmt(v.Stmt)
	case *GotoStmt:
		fg.a.JmpLabel("L_" + v.Label)
	}
	return nil
}

func (fg *funcGen) genSwitch(v *SwitchStmt) error {
	// 收集 case 标签
	end := fg.c.newLabel("swend")
	ctx := switchCtx{endLabel: end}
	// 先扫描 body 收集 case
	var collect func(st Stmt)
	collect = func(st Stmt) {
		switch q := st.(type) {
		case *BlockStmt:
			for _, s2 := range q.Stmts {
				collect(s2)
			}
		case *CaseStmt:
			lab := fg.c.newLabel("case")
			if q.X == nil {
				ctx.defLabel = lab
			} else {
				if iv, ok := constIntValue(q.X); ok {
					ctx.cases = append(ctx.cases, caseInfo{val: iv, label: lab})
				}
			}
			if q.Stmt != nil {
				collect(q.Stmt)
			}
		}
	}
	collect(v.Body)

	if err := fg.genExpr(v.X); err != nil {
		return err
	}
	// 保存到临时
	tmp := fg.alloc(8)
	fg.a.MovStore(stackMem(tmp), RAX)
	for _, ci := range ctx.cases {
		fg.a.MovLoad(RAX, stackMem(tmp))
		fg.a.CmpImm32(RAX, int32(ci.val))
		fg.a.Jcc(4, ci.label)
	}
	if ctx.defLabel != "" {
		fg.a.JmpLabel(ctx.defLabel)
	} else {
		fg.a.JmpLabel(end)
	}
	fg.free(8)

	fg.breaks = append(fg.breaks, end)
	// 生成 body（case 标签在生成时绑定）
	if err := fg.genStmtWithCases(v.Body, &ctx); err != nil {
		return err
	}
	fg.breaks = fg.breaks[:len(fg.breaks)-1]
	fg.a.Bind(end)
	return nil
}

// genStmtWithCases 生成 switch body，绑定 case 标签
func (fg *funcGen) genStmtWithCases(s Stmt, ctx *switchCtx) error {
	switch v := s.(type) {
	case *BlockStmt:
		for _, q := range v.Stmts {
			if err := fg.genStmtWithCases(q, ctx); err != nil {
				return err
			}
		}
		return nil
	case *CaseStmt:
		// 找到对应的标签
		lab := ""
		if v.X == nil {
			lab = ctx.defLabel
		} else if iv, ok := constIntValue(v.X); ok {
			for _, ci := range ctx.cases {
				if ci.val == iv && ci.label != "" {
					lab = ci.label
					// 只绑定一次
					break
				}
			}
		}
		if lab != "" {
			fg.a.Bind(lab)
		}
		if v.Stmt != nil {
			return fg.genStmtWithCases(v.Stmt, ctx)
		}
		return nil
	}
	return fg.genStmt(s)
}

func (fg *funcGen) genReturn(v *ReturnStmt) error {
	ret := fg.fd.Ty.Ret
	if ret != nil && ret.IsRecord() {
		if v.X != nil {
			// struct 表达式的值（地址）→ rax
			if err := fg.genExpr(v.X); err != nil {
				return err
			}
			fg.emitMemcpyReg(RAX, -1, fg.retSlot, ret.Size)
			fg.a.MovLoad(RAX, stackMem(fg.retSlot))
		}
		fg.a.JmpLabel(fg.epilogue)
		return nil
	}
	if v.X != nil {
		if err := fg.genExpr(v.X); err != nil {
			return err
		}
	}
	fg.a.JmpLabel(fg.epilogue)
	return nil
}

// emitMemcpyReg 调用 memcpy；dst 来自寄存器或栈槽，src 在 reg
// dstReg >= 0 时用寄存器，否则用 dstSlot 栈槽
func (fg *funcGen) emitMemcpyReg(srcReg int, dstReg int, dstSlot int32, size int64) {
	s := fg.alloc(8)
	fg.a.MovStore(stackMem(s), srcReg)
	if dstReg >= 0 {
		d := fg.alloc(8)
		fg.a.MovStore(stackMem(d), dstReg)
		fg.a.MovLoad(RCX, stackMem(d))
		fg.free(8)
	} else {
		fg.a.MovLoad(RCX, stackMem(dstSlot))
	}
	fg.a.MovLoad(RDX, stackMem(s))
	fg.a.MovImm32(R8, int32(size))
	fg.callImport("memcpy")
	fg.free(8)
}

// callImport 调用外部导入函数（参数已按 ABI 放好）
func (fg *funcGen) callImport(name string) {
	// setjmp/longjmp 走自实现（见 jmp.go），不导入 msvcrt：
	// msvcrt 的 setjmp 在 x64 上是 _setjmpex，需要调用方 .pdata，nativecc 不生成。
	if name == "setjmp" || name == "longjmp" {
		fg.a.CallLabel("__zuzo_" + name)
		return
	}
	fg.c.imports[name] = true
	fg.a.CallMem(importMem(name))
}

func importMem(name string) *Mem {
	return &Mem{Base: -1, Index: -1, Scale: 1, RipRel: true, Label: "__imp_" + name}
}

func (fg *funcGen) genLocalDecl(vd *VarDecl) error {
	if vd.IsStatic {
		return nil
	}
	off, ok := fg.localOff[vd.Name]
	if !ok {
		return nil
	}
	if vd.Init == nil {
		return nil
	}
	fg.a.Lea(RAX, stackMem(off))
	if err := fg.genInit(RAX, vd.Ty, vd.Init); err != nil {
		return err
	}
	return nil
}
