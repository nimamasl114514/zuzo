package nativecc

import (
	"encoding/binary"
	"fmt"
)

// x86-64 寄存器编号
const (
	RAX = 0
	RCX = 1
	RDX = 2
	RBX = 3
	RSP = 4
	RBP = 5
	RSI = 6
	RDI = 7
	R8  = 8
	R9  = 9
	R10 = 10
	R11 = 11
	R12 = 12
	R13 = 13
	R14 = 14
	R15 = 15
)

// XMM0..XMM15 与整数寄存器编号一致（0..15）

// Mem 内存操作数
type Mem struct {
	Base   int    // 基址寄存器，-1 表示无
	Index  int    // 变址寄存器，-1 表示无
	Scale  int    // 1/2/4/8
	Disp   int32  // 位移
	RipRel bool   // RIP 相对
	Label  string // RIP 相对时引用的标签
	GotOff int    // 标签内额外偏移
}

// Assembler x86-64 机器码生成器
type Assembler struct {
	Buf     []byte
	labels  map[string]int
	fixups  []Fixup
	relocs  []Reloc
	line    int
}

// Fixup 待回填的相对地址（标签 → 位置）
type Fixup struct {
	Label string
	Pos   int // disp32 在原 Buf 中的位置
	Kind  int // 0 = 相对下条指令（RIP 相对/跳转）
	Add   int
}

// Reloc 绝对地址重定位（用于 PE 的 .rdata 内部指针，暂未使用）
type Reloc struct {
	Pos   int
	Label string
	Add   int
}

// NewAssembler 构造
func NewAssembler() *Assembler {
	return &Assembler{labels: map[string]int{}}
}

// Len 当前长度
func (a *Assembler) Len() int { return len(a.Buf) }

// Pos 当前位置标签（供跳转回填）
func (a *Assembler) Pos() int { return len(a.Buf) }

// Bind 绑定标签
func (a *Assembler) Bind(label string) {
	a.labels[label] = len(a.Buf)
}

// BindHere 当前偏移绑定标签
func (a *Assembler) BindPos(label string, pos int) {
	a.labels[label] = pos
}

func (a *Assembler) emit(b ...byte) { a.Buf = append(a.Buf, b...) }

func (a *Assembler) emit32(v int32) {
	var t [4]byte
	binary.LittleEndian.PutUint32(t[:], uint32(v))
	a.Buf = append(a.Buf, t[:]...)
}

func (a *Assembler) emit64(v int64) {
	var t [8]byte
	binary.LittleEndian.PutUint64(t[:], uint64(v))
	a.Buf = append(a.Buf, t[:]...)
}

// rex 生成 REX 前缀（w=1 强制；否则按需）
func (a *Assembler) rex(w bool, reg, index, rm int) {
	r := 0
	if reg >= 0 {
		r = (reg >> 3) & 1
	}
	x := 0
	if index >= 0 {
		x = (index >> 3) & 1
	}
	b := 0
	if rm >= 0 {
		b = (rm >> 3) & 1
	}
	if !w && r == 0 && x == 0 && b == 0 {
		return
	}
	var v byte = 0x40
	if w {
		v |= 8
	}
	v |= byte(r << 2)
	v |= byte(x << 1)
	v |= byte(b)
	a.emit(v)
}

func (a *Assembler) rexMem(w bool, reg int, m *Mem) {
	index := -1
	if m != nil {
		index = m.Index
	}
	rm := -1
	if m != nil {
		rm = m.Base
	}
	if m != nil && m.RipRel {
		rm = 5 // RBP 编码为 RIP 相对
		// RIP 相对不带 B 位
		r := (reg >> 3) & 1
		x := 0
		if index >= 0 {
			x = (index >> 3) & 1
		}
		if w || r != 0 || x != 0 {
			var v byte = 0x40
			if w {
				v |= 8
			}
			v |= byte(r << 2)
			v |= byte(x << 1)
			a.emit(v)
		}
		return
	}
	a.rex(w, reg, index, rm)
}

// modrmMem 生成 modrm（+sib+disp）用于 [mem]
func (a *Assembler) modrmMem(reg int, m *Mem) {
	if m == nil {
		// [reg] 直接（无位移）
		a.emit(byte(0xC0 | (reg&7) << 3 | 0))
		return
	}
	if m.RipRel {
		a.emit(byte(0x00 | (reg&7)<<3 | 5))
		pos := len(a.Buf)
		a.emit32(m.Disp)
		add := m.GotOff
		a.fixups = append(a.fixups, Fixup{Label: m.Label, Pos: pos, Kind: 0, Add: add})
		return
	}
	base := m.Base
	if base < 0 {
		// 绝对地址：[disp32]（用 SIB 无 base 形式）
		a.emit(byte(0x04 | (reg&7)<<3))
		a.emit(byte(0x25)) // SIB: no base, no index
		a.emit32(m.Disp)
		return
	}
	needSIB := m.Index >= 0 || (base&7) == RSP&7
	disp := m.Disp
	var mod byte
	if disp == 0 && (base&7) != RBP&7 {
		mod = 0
	} else if disp >= -128 && disp <= 127 {
		mod = 1
	} else {
		mod = 2
	}
	if needSIB {
		a.emit(byte(int(mod)<<6 | (reg&7)<<3 | 4))
		scale := m.Scale
		if scale == 0 {
			scale = 1
		}
		var ss byte
		switch scale {
		case 1:
			ss = 0
		case 2:
			ss = 1
		case 4:
			ss = 2
		case 8:
			ss = 3
		}
		idx := m.Index
		if idx < 0 {
			idx = 4 // no index
		}
		a.emit(byte(int(ss)<<6 | (idx&7)<<3 | (base & 7)))
	} else {
		a.emit(byte(int(mod)<<6 | (reg&7)<<3 | (base & 7)))
	}
	if mod == 1 {
		a.emit(byte(disp))
	} else if mod == 2 {
		a.emit32(disp)
	}
}

// ModRM 通用（reg-reg 形式）
func (a *Assembler) modrmReg(reg, rm int) {
	a.emit(byte(0xC0 | (reg&7)<<3 | (rm & 7)))
}

// ── 通用二操作数指令 ─────────────────────────────────────

// opRR 生成 REX.W + opcode + modrm 的 reg,reg 指令
func (a *Assembler) opRR(w bool, opcode []byte, reg, rm int) {
	a.rex(w, reg, -1, rm)
	a.emit(opcode...)
	a.modrmReg(reg, rm)
}

// opRM 生成 REX.W + opcode + modrm 的 reg,[mem]
func (a *Assembler) opRM(w bool, opcode []byte, reg int, m *Mem) {
	a.rexMem(w, reg, m)
	a.emit(opcode...)
	a.modrmMem(reg, m)
}

// opMR 生成 REX.W + opcode + modrm 的 [mem],reg
func (a *Assembler) opMR(w bool, opcode []byte, reg int, m *Mem) {
	a.rexMem(w, reg, m)
	a.emit(opcode...)
	a.modrmMem(reg, m)
}

// ── 常用指令 ────────────────────────────────────────────

// MovRR mov dst, src（64 位）
func (a *Assembler) MovRR(dst, src int) {
	a.opRR(true, []byte{0x89}, src, dst)
}

// MovRR32 mov dst, src（32 位，自动清零高 32 位）
func (a *Assembler) MovRR32(dst, src int) {
	a.opRR(false, []byte{0x89}, src, dst)
}

// MovImm64 mov dst, imm64
func (a *Assembler) MovImm64(dst int, v int64) {
	a.rex(true, 0, -1, dst)
	a.emit(byte(0xB8 + (dst & 7)))
	a.emit64(v)
}

// MovImm32 mov dst, imm32（零扩展）
func (a *Assembler) MovImm32(dst int, v int32) {
	if dst >= 8 {
		a.rex(false, 0, -1, dst)
	}
	a.emit(byte(0xB8 + (dst & 7)))
	a.emit32(v)
}

// MovLoad mov dst, [mem]
func (a *Assembler) MovLoad(dst int, m *Mem) {
	a.opRM(true, []byte{0x8B}, dst, m)
}

// MovLoad32 mov dst32, [mem]
func (a *Assembler) MovLoad32(dst int, m *Mem) {
	a.opRM(false, []byte{0x8B}, dst, m)
}

// MovStore mov [mem], src
func (a *Assembler) MovStore(m *Mem, src int) {
	a.opMR(true, []byte{0x89}, src, m)
}

// MovStore32 mov [mem], src32
func (a *Assembler) MovStore32(m *Mem, src int) {
	a.opMR(false, []byte{0x89}, src, m)
}

// MovStore8 mov [mem], src8
func (a *Assembler) MovStore8(m *Mem, src int) {
	a.rexMem(false, src, m)
	a.emit(0x88)
	a.modrmMem(src, m)
}

// Lea lea dst, [mem]
func (a *Assembler) Lea(dst int, m *Mem) {
	a.opRM(true, []byte{0x8D}, dst, m)
}

// MovLoad8 零扩展字节
func (a *Assembler) MovLoad8(dst int, m *Mem) {
	a.opRM(true, []byte{0x0F, 0xB6}, dst, m)
}

// MovLoad8S 符号扩展字节
func (a *Assembler) MovLoad8S(dst int, m *Mem) {
	a.opRM(true, []byte{0x0F, 0xBE}, dst, m)
}

// MovLoad16 零扩展 16 位
func (a *Assembler) MovLoad16(dst int, m *Mem) {
	a.opRM(true, []byte{0x0F, 0xB7}, dst, m)
}

// MovLoad16S 符号扩展 16 位
func (a *Assembler) MovLoad16S(dst int, m *Mem) {
	a.opRM(true, []byte{0x0F, 0xBF}, dst, m)
}

// MovLoad32S 符号扩展 32 位
func (a *Assembler) MovLoad32S(dst int, m *Mem) {
	a.opRM(true, []byte{0x63}, dst, m)
}

// MovStore16
func (a *Assembler) MovStore16(m *Mem, src int) {
	a.rexMem(false, src, m)
	a.emit(0x66, 0x89)
	a.modrmMem(src, m)
}

// AddRR add dst, src
func (a *Assembler) AddRR(dst, src int) { a.opRR(true, []byte{0x01}, src, dst) }

// SubRR sub dst, src
func (a *Assembler) SubRR(dst, src int) { a.opRR(true, []byte{0x29}, src, dst) }

// AndRR and dst, src
func (a *Assembler) AndRR(dst, src int) { a.opRR(true, []byte{0x21}, src, dst) }

// OrRR or dst, src
func (a *Assembler) OrRR(dst, src int) { a.opRR(true, []byte{0x09}, src, dst) }

// XorRR xor dst, src
func (a *Assembler) XorRR(dst, src int) { a.opRR(true, []byte{0x31}, src, dst) }

// AddRM add dst, [mem]
func (a *Assembler) AddRM(dst int, m *Mem) { a.opRM(true, []byte{0x03}, dst, m) }

// AddMR add [mem], src
func (a *Assembler) AddMR(m *Mem, src int) { a.opMR(true, []byte{0x01}, src, m) }

// SubRM sub dst, [mem]
func (a *Assembler) SubRM(dst int, m *Mem) { a.opRM(true, []byte{0x2B}, dst, m) }

// AndRM and dst, [mem]
func (a *Assembler) AndRM(dst int, m *Mem) { a.opRM(true, []byte{0x23}, dst, m) }

// OrRM or dst, [mem]
func (a *Assembler) OrRM(dst int, m *Mem) { a.opRM(true, []byte{0x0B}, dst, m) }

// XorRM xor dst, [mem]
func (a *Assembler) XorRM(dst int, m *Mem) { a.opRM(true, []byte{0x33}, dst, m) }

// CmpRR cmp dst, src（设置标志位）
func (a *Assembler) CmpRR(dst, src int) { a.opRR(true, []byte{0x39}, src, dst) }

// CmpRM cmp dst, [mem]
func (a *Assembler) CmpRM(dst int, m *Mem) { a.opRM(true, []byte{0x3B}, dst, m) }

// CmpMR cmp [mem], src
func (a *Assembler) CmpMR(m *Mem, src int) { a.opMR(true, []byte{0x39}, src, m) }

// CmpImm32 cmp r, imm32
func (a *Assembler) CmpImm32(reg int, v int32) {
	a.rex(true, 0, -1, reg)
	a.emit(byte(0x81))
	a.modrmReg(7, reg)
	a.emit32(v)
}

// CmpImm8 cmp r, imm8
func (a *Assembler) CmpImm8(reg int, v int8) {
	a.rex(true, 0, -1, reg)
	a.emit(byte(0x83))
	a.modrmReg(7, reg)
	a.emit(byte(v))
}

// TestRR test r, r（判断零）
func (a *Assembler) TestRR(reg, reg2 int) { a.opRR(true, []byte{0x85}, reg2, reg) }

// ImulRR imul dst, src（有符号）
func (a *Assembler) ImulRR(dst, src int) {
	a.rex(true, dst, -1, src)
	a.emit(0x0F, 0xAF)
	a.modrmReg(dst, src)
}

// ImulRRI imul dst, src, imm32
func (a *Assembler) ImulRRI(dst, src int, imm int32) {
	a.rex(true, dst, -1, src)
	a.emit(0x69)
	a.modrmReg(dst, src)
	a.emit32(imm)
}

// Idiv div（有符号除）：idiv reg → rdx:rax / reg
func (a *Assembler) Idiv(reg int) {
	a.rex(true, 0, -1, reg)
	a.emit(0xF7)
	a.modrmReg(7, reg)
}

// Div 无符号除
func (a *Assembler) Div(reg int) {
	a.rex(true, 0, -1, reg)
	a.emit(0xF7)
	a.modrmReg(6, reg)
}

// Cqo 符号扩展 rax → rdx:rax
func (a *Assembler) Cqo() { a.emit(0x48, 0x99) }

// Neg neg reg
func (a *Assembler) Neg(reg int) {
	a.rex(true, 0, -1, reg)
	a.emit(0xF7)
	a.modrmReg(3, reg)
}

// Not not reg
func (a *Assembler) Not(reg int) {
	a.rex(true, 0, -1, reg)
	a.emit(0xF7)
	a.modrmReg(2, reg)
}

// Shift 移位：kind 0=shl 1=shr 4=sar（C1 /4 /5 /7 立即数）
func (a *Assembler) ShiftImm(kind int, reg int, imm byte) {
	a.rex(true, 0, -1, reg)
	a.emit(0xC1)
	a.modrmReg(kind, reg)
	a.emit(imm)
}

// ShiftCL kind 同 ShiftImm，位移量在 cl
func (a *Assembler) ShiftCL(kind int, reg int) {
	a.rex(true, 0, -1, reg)
	a.emit(0xD3)
	a.modrmReg(kind, reg)
}

// Push 压栈（64 位）
func (a *Assembler) Push(reg int) {
	if reg >= 8 {
		a.emit(0x41)
	}
	a.emit(byte(0x50 + (reg & 7)))
}

// Pop 弹栈
func (a *Assembler) Pop(reg int) {
	if reg >= 8 {
		a.emit(0x41)
	}
	a.emit(byte(0x58 + (reg & 7)))
}

// Ret 返回
func (a *Assembler) Ret() { a.emit(0xC3) }

// CallLabel 调用标签（相对）
func (a *Assembler) CallLabel(label string) {
	a.emit(0xE8)
	pos := len(a.Buf)
	a.emit32(0)
	a.fixups = append(a.fixups, Fixup{Label: label, Pos: pos, Kind: 0})
}

// JmpLabel 无条件跳转
func (a *Assembler) JmpLabel(label string) {
	a.emit(0xE9)
	pos := len(a.Buf)
	a.emit32(0)
	a.fixups = append(a.fixups, Fixup{Label: label, Pos: pos, Kind: 0})
}

// jcc 条件码：0=O 1=NO 2=B/C 3=AE 4=E/Z 5=NE 6=BE 7=A 8=S 9=NS 10=P 11=NP 12=L 13=GE 14=LE 15=G
func (a *Assembler) Jcc(cc int, label string) {
	a.emit(0x0F, byte(0x80+cc))
	pos := len(a.Buf)
	a.emit32(0)
	a.fixups = append(a.fixups, Fixup{Label: label, Pos: pos, Kind: 0})
}

// Setcc 设置字节
func (a *Assembler) Setcc(cc int, reg int) {
	a.rex(false, 0, -1, reg)
	a.emit(0x0F, byte(0x90+cc))
	a.modrmReg(0, reg)
}

// CallReg 间接调用
func (a *Assembler) CallReg(reg int) {
	a.rex(false, 0, -1, reg)
	a.emit(0xFF)
	a.modrmReg(2, reg)
}

// JmpReg 间接跳转
func (a *Assembler) JmpReg(reg int) {
	a.rex(false, 0, -1, reg)
	a.emit(0xFF)
	a.modrmReg(4, reg)
}

// CallMem 调用 [mem]（用于导入表）
func (a *Assembler) CallMem(m *Mem) {
	a.rexMem(false, 0, m)
	a.emit(0xFF)
	a.modrmMem(2, m)
}

// Nop
func (a *Assembler) Nop() { a.emit(0x90) }

// Int3
func (a *Assembler) Int3() { a.emit(0xCC) }

// ── SSE 浮点 ────────────────────────────────────────────

// MovsdRR movsd dst, src
func (a *Assembler) MovsdRR(dst, src int) {
	a.emit(0xF2)
	a.rex(false, dst, -1, src)
	a.emit(0x0F, 0x10)
	a.emit(byte(0xC0 | (dst&7)<<3 | (src & 7)))
}

// MovsdLoad movsd dst, [mem]
func (a *Assembler) MovsdLoad(dst int, m *Mem) {
	a.emit(0xF2)
	a.rexMem(false, dst, m)
	a.emit(0x0F, 0x10)
	a.modrmMem(dst, m)
}

// MovsdStore movsd [mem], src
func (a *Assembler) MovsdStore(m *Mem, src int) {
	a.emit(0xF2)
	a.rexMem(false, src, m)
	a.emit(0x0F, 0x11)
	a.modrmMem(src, m)
}

// sseOpcode 标量双精度算术操作码：0=add 1=mul 2=sub 3=div（不连续！）
var sseOpcode = [4]byte{0x58, 0x59, 0x5C, 0x5E}

// SseArith 算术：0=add 1=mul 2=sub 3=div
func (a *Assembler) SseArith(op int, dst, src int) {
	a.emit(0xF2)
	a.rex(false, dst, -1, src)
	a.emit(0x0F, sseOpcode[op])
	a.emit(byte(0xC0 | (dst&7)<<3 | (src & 7)))
}

// SseArithLoad 算术（右操作数在内存）
func (a *Assembler) SseArithLoad(op int, dst int, m *Mem) {
	a.emit(0xF2)
	a.rexMem(false, dst, m)
	a.emit(0x0F, sseOpcode[op])
	a.modrmMem(dst, m)
}

// Cvtsi2sd xmm, r64
func (a *Assembler) Cvtsi2sd(xmm, reg int) {
	a.emit(0xF2)
	a.rex(true, xmm, -1, reg)
	a.emit(0x0F, 0x2A)
	a.modrmReg(xmm, reg)
}

// Cvttsd2si r64, xmm
func (a *Assembler) Cvttsd2si(reg, xmm int) {
	a.emit(0xF2)
	a.rex(true, reg, -1, xmm)
	a.emit(0x0F, 0x2C)
	a.modrmReg(reg, xmm)
}

// Ucomisd xmm1, xmm2（比较，设置 ZF/PF/CF）
func (a *Assembler) Ucomisd(xmm1, xmm2 int) {
	a.emit(0x66)
	a.rex(false, xmm1, -1, xmm2)
	a.emit(0x0F, 0x2E)
	a.emit(byte(0xC0 | (xmm1&7)<<3 | (xmm2 & 7)))
}

// MovqXmmGpr movq xmm, r64
func (a *Assembler) MovqXmmGpr(xmm, reg int) {
	a.emit(0x66)
	a.rex(true, xmm, -1, reg)
	a.emit(0x0F, 0x6E)
	a.modrmReg(xmm, reg)
}

// MovqGprXmm movq r64, xmm
func (a *Assembler) MovqGprXmm(reg, xmm int) {
	a.emit(0x66)
	a.rex(true, xmm, -1, reg)
	a.emit(0x0F, 0x7E)
	a.modrmReg(xmm, reg)
}

// Pxor xmm, xmm（清零）
func (a *Assembler) Pxor(xmm1, xmm2 int) {
	a.emit(0x66)
	a.rex(false, xmm1, -1, xmm2)
	a.emit(0x0F, 0xEF)
	a.emit(byte(0xC0 | (xmm1&7)<<3 | (xmm2 & 7)))
}

// MovupsLoad movups xmm, [mem]
func (a *Assembler) MovupsLoad(xmm int, m *Mem) {
	a.rexMem(false, xmm, m)
	a.emit(0x0F, 0x10)
	a.modrmMem(xmm, m)
}

// MovupsStore movups [mem], xmm
func (a *Assembler) MovupsStore(m *Mem, xmm int) {
	a.rexMem(false, xmm, m)
	a.emit(0x0F, 0x11)
	a.modrmMem(xmm, m)
}

// Cvtsd2ss 双精度转单精度
func (a *Assembler) Cvtsd2ss(dst, src int) {
	a.emit(0xF2)
	a.rex(false, dst, -1, src)
	a.emit(0x0F, 0x5A)
	a.emit(byte(0xC0 | (dst&7)<<3 | (src & 7)))
}

// Cvtss2sd 单精度转双精度
func (a *Assembler) Cvtss2sd(dst, src int) {
	a.emit(0xF3)
	a.rex(false, dst, -1, src)
	a.emit(0x0F, 0x5A)
	a.emit(byte(0xC0 | (dst&7)<<3 | (src & 7)))
}

// ── 标签与修复 ─────────────────────────────────────────

// Patch 回填所有标签引用；返回错误若存在未定义标签
func (a *Assembler) Patch() error {
	for _, f := range a.fixups {
		target, ok := a.labels[f.Label]
		if !ok {
			return fmt.Errorf("未定义的标签: %s", f.Label)
		}
		next := f.Pos + 4
		rel := int32(target + f.Add - next)
		binary.LittleEndian.PutUint32(a.Buf[f.Pos:], uint32(rel))
	}
	return nil
}

// PatchWithExternal 回填标签，外部标签由 resolve 提供（返回目标偏移）
func (a *Assembler) PatchWithExternal(resolve func(label string) (int, bool)) error {
	for _, f := range a.fixups {
		if target, ok := a.labels[f.Label]; ok {
			next := f.Pos + 4
			rel := int32(target + f.Add - next)
			binary.LittleEndian.PutUint32(a.Buf[f.Pos:], uint32(rel))
			continue
		}
		if resolve != nil {
			if target, ok := resolve(f.Label); ok {
				next := f.Pos + 4
				rel := int32(target + f.Add - next)
				binary.LittleEndian.PutUint32(a.Buf[f.Pos:], uint32(rel))
				continue
			}
		}
		return fmt.Errorf("未定义的标签: %s", f.Label)
	}
	return nil
}

// 辅助：构造 [rbp-disp] 内存操作数
func stackMem(disp int32) *Mem {
	return &Mem{Base: RBP, Index: -1, Scale: 1, Disp: disp}
}

// SubImm8 sub rsp, imm8（0x48 0x83 /5 imm8）
func (a *Assembler) SubImm8(reg int, imm int8) {
	a.rex(true, 0, -1, reg)
	a.emit(0x83)
	a.modrmReg(5, reg)
	a.emit(byte(imm))
}

// AddImm8 add reg, imm8
func (a *Assembler) AddImm8(reg int, imm int8) {
	a.rex(true, 0, -1, reg)
	a.emit(0x83)
	a.modrmReg(0, reg)
	a.emit(byte(imm))
}

// AddImm32 add reg, imm32
func (a *Assembler) AddImm32(reg int, imm int32) {
	a.rex(true, 0, -1, reg)
	a.emit(0x81)
	a.modrmReg(0, reg)
	a.emit32(imm)
}

// MovzxR8 movzx dst, src8（需要按 dst/src 的高位补 REX.R/REX.B）
func (a *Assembler) MovzxR8(dst, src int) {
	a.rex(false, dst, -1, src)
	a.emit(0x0F, 0xB6)
	a.emit(byte(0xC0 | (dst&7)<<3 | (src & 7)))
}
