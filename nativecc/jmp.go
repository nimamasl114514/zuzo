package nativecc

// ─────────────────────────────────────────────────────────────
// 自带 setjmp / longjmp
//
// 为什么不直接导入 msvcrt：Windows x64 的 setjmp 在 CRT 里走 _setjmpex，
// 后者用 RtlUnwind(Ex) 回溯到调用方帧，需要调用方的 PE 展开信息（.pdata）。
// nativecc 目前不生成 .pdata，实测（第30轮）
//   执行 { 10/0 } 遇到 e { … }
// 在 --cc=native 产物里 longjmp 阶段抛 0xc00000ff（RtlRaiseStatus）。
//
// 这里给出一份自足的实现：只保存/恢复 Win64 ABI 的 callee-saved 整数寄存器
// 与返回地址、栈指针，不碰 SEH，也不需要 .pdata。
//
// 缓冲区布局（与 msvcrt 的 jmp_buf 无关，256 字节内）：
//   0   返回地址(rip)
//   8   调用方 rsp（setjmp 返回后应处于的栈指针）
//   16  rbp
//   24  rbx
//   32  rsi
//   40  rdi
//   48  r12
//   56  r13
//   64  r14
//   72  r15
// 未保存 XMM6-XMM15：生成的 zuzo 代码只在单个表达式内使用浮点寄存器，
// 不跨 setjmp 持有；如将来出现跨 try 的浮点活跃值，需要在这里补 movups。
// ─────────────────────────────────────────────────────────────

const (
	jmpOffRip = 0
	jmpOffRsp = 8
	jmpOffRbp = 16
	jmpOffRbx = 24
	jmpOffRsi = 32
	jmpOffRdi = 40
	jmpOffR12 = 48
	jmpOffR13 = 56
	jmpOffR14 = 64
	jmpOffR15 = 72
)

func memDisp(base int, disp int32) *Mem {
	return &Mem{Base: base, Index: -1, Scale: 1, Disp: disp}
}

// genJumpHelpers 在代码段开头生成 __zuzo_setjmp / __zuzo_longjmp。
// 调用点在 callImport 里被重定向到这两个标签。
func (c *Compiler) genJumpHelpers() {
	a := c.text

	// int __zuzo_setjmp(jmp_buf buf)   // rcx = buf
	a.Bind("__zuzo_setjmp")
	a.MovLoad(RAX, memDisp(RSP, 0)) // [rsp] = 返回地址
	a.MovStore(memDisp(RCX, jmpOffRip), RAX)
	a.Lea(RAX, memDisp(RSP, 8)) // 返回后的 rsp
	a.MovStore(memDisp(RCX, jmpOffRsp), RAX)
	a.MovStore(memDisp(RCX, jmpOffRbp), RBP)
	a.MovStore(memDisp(RCX, jmpOffRbx), RBX)
	a.MovStore(memDisp(RCX, jmpOffRsi), RSI)
	a.MovStore(memDisp(RCX, jmpOffRdi), RDI)
	a.MovStore(memDisp(RCX, jmpOffR12), R12)
	a.MovStore(memDisp(RCX, jmpOffR13), R13)
	a.MovStore(memDisp(RCX, jmpOffR14), R14)
	a.MovStore(memDisp(RCX, jmpOffR15), R15)
	a.XorRR(RAX, RAX) // 首次返回 0
	a.Ret()

	// void __zuzo_longjmp(jmp_buf buf, int val)  // rcx = buf, rdx = val
	a.Bind("__zuzo_longjmp")
	a.MovRR(RAX, RDX)
	a.TestRR(RAX, RAX)
	a.Jcc(5, "__zuzo_lj_nonzero") // jne：val != 0 保持不变
	a.MovImm32(RAX, 1)            // val == 0 → 按 C 语义返回 1
	a.Bind("__zuzo_lj_nonzero")
	a.MovLoad(RBP, memDisp(RCX, jmpOffRbp))
	a.MovLoad(RBX, memDisp(RCX, jmpOffRbx))
	a.MovLoad(RSI, memDisp(RCX, jmpOffRsi))
	a.MovLoad(RDI, memDisp(RCX, jmpOffRdi))
	a.MovLoad(R12, memDisp(RCX, jmpOffR12))
	a.MovLoad(R13, memDisp(RCX, jmpOffR13))
	a.MovLoad(R14, memDisp(RCX, jmpOffR14))
	a.MovLoad(R15, memDisp(RCX, jmpOffR15))
	a.MovLoad(R10, memDisp(RCX, jmpOffRip)) // 目标地址
	a.MovLoad(RSP, memDisp(RCX, jmpOffRsp)) // 栈指针最后恢复
	a.JmpReg(R10)
}
