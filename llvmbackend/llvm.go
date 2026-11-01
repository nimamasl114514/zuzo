// Package llvmbackend 把 zuzo AST 直接翻译为文本 LLVM IR（LLVM 后端）。
//
// 设计口径（2026-11-01 第31轮，依据 cbackend 的语义等价迁移）：
//   - 值模型：%ZVal = { i32 kind, i64 payload }，与 C 运行时 ZVal 16 字节布局一致；
//   - 每个表达式求值结果 = 一个指向 %ZVal 槽位的 ptr（alloca 或全局），不做逐值 SSA 拆解；
//   - 运行时 ABI：Win64 MSVC 下 ZVal 参数按引用传 ptr、ZVal 返回值走 sret 隐藏首参，
//     已用 tmp/abiprobe 实测（clang 22.1.8）；
//   - 函数统一签名 void (ptr sret(%ZVal), i32 argc, ptr argv, ptr env)，与 ZFnPtr 对齐。
package llvmbackend

import (
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"zuzo/ast"
)

// Options 生成选项。
type Options struct {
	SourcePath string
	KeepTemp   bool
	Strict     bool
}

// Value 是一个 ptr 类型的 SSA 值，指向一个 %ZVal 槽位。
type Value struct{ Slot string }

// varKind 变量在 IR 侧的存储形态（与 cbackend 同名语义）。
type varKind int

const (
	vPlain   varKind = iota // 直接是 %ZVal alloca
	vGlobal                 // 顶层变量 → @zg_<name>
	vBox                    // alloca ptr，存 ZBox*
	vCapture                // 通过 env 第 capIdx 个槽位间接访问 ZBox*
)

type varSlot struct {
	kind   varKind
	slot   string // vPlain: %ZVal alloca；vBox: ptr alloca；vGlobal: @zg_x
	capIdx int
}

// fnInfo / analysis 见 capture.go（与 cbackend 同构，整体平移）。

// loopCtx 记录 break/continue 目标标签。
type loopCtx struct {
	contLabel string
	breakLbl  string
}

// fnBuilder 一个函数体的构建状态。
type fnBuilder struct {
	entry      strings.Builder // entry 块里的 alloca
	body       strings.Builder // 后续基本块
	tseq       int
	lseq       int
	retSlot    string
	envPtr     string // 当前函数 env 指针的 SSA 名（无捕获为 ""）
	terminated bool
	loops      []loopCtx
	indent     int
}

func (b *fnBuilder) nextTemp() string {
	b.tseq++
	return fmt.Sprintf("%%t%d", b.tseq)
}

func (b *fnBuilder) nextLabel() string {
	b.lseq++
	return fmt.Sprintf("L%d", b.lseq)
}

// Gen LLVM IR 生成器。
type Gen struct {
	opt        Options
	ana        *analysis
	funcs      map[string]*fnInfo
	order      []string
	defines    map[string]*ast.DefineStatement
	defOrder   []string
	globals    map[string]bool
	globalOrder []string
	globalLine map[string]int
	scopes     []map[string]varSlot
	cur        *fnInfo
	errs       []string

	// 模块拼装
	strDefs    []string
	strIdx     map[string]int
	decls      map[string]string
	declOrder  []string
	globalDefs []string
	fnDefs     []string

	fb *fnBuilder
}

// ── 基础发射 ───────────────────────────────────────────────

func (g *Gen) emit(format string, args ...any) {
	if g.fb == nil {
		panic("llvmbackend: emit 时没有当前函数")
	}
	if g.fb.terminated {
		return
	}
	g.fb.body.WriteString(strings.Repeat("  ", g.fb.indent))
	fmt.Fprintf(&g.fb.body, format, args...)
	g.fb.body.WriteString("\n")
}

func (g *Gen) alloca(typ string) string {
	name := g.fb.nextTemp()
	fmt.Fprintf(&g.fb.entry, "  %s = alloca %s, align 8\n", name, typ)
	return name
}

// tempZ 分配一个 %ZVal 槽位（entry 块 alloca）。
func (g *Gen) tempZ() string { return g.alloca("%ZVal") }

func (g *Gen) newLabel() string { return g.fb.nextLabel() }

func (g *Gen) place(l string) {
	g.fb.body.WriteString(l + ":\n")
	g.fb.terminated = false
}

func (g *Gen) br(l string) {
	g.emit("br label %%%s", l)
	g.fb.terminated = true
}

func (g *Gen) condBr(cond, t, f string) {
	g.emit("br i1 %s, label %%%s, label %%%s", cond, t, f)
	g.fb.terminated = true
}

func (g *Gen) emitRetVoid() {
	g.emit("ret void")
	g.fb.terminated = true
}

// ── 运行时声明与调用 ───────────────────────────────────────

func irType(t string) string {
	switch t {
	case "ZVal":
		return "%ZVal"
	case "int64":
		return "i64"
	case "int":
		return "i32"
	case "double":
		return "double"
	case "ptr":
		return "ptr"
	case "void":
		return "void"
	}
	return "ptr"
}

func (s rtSig) render(name string) string {
	var b strings.Builder
	if s.ret == "ZVal" {
		b.WriteString("declare void @" + name + "(ptr sret(%ZVal)")
		for _, a := range s.args {
			b.WriteString(", " + irType(a))
		}
		b.WriteString(")")
		return b.String()
	}
	b.WriteString("declare " + irType(s.ret) + " @" + name + "(")
	for i, a := range s.args {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(irType(a))
	}
	b.WriteString(")")
	return b.String()
}

// rt 确保运行时/CRT 声明已登记，返回符号名（带 @）。
func (g *Gen) rt(name string) string {
	if _, ok := g.decls[name]; !ok {
		if txt, ok := crtDecls[name]; ok {
			g.decls[name] = txt
			g.declOrder = append(g.declOrder, name)
			return "@" + name
		}
		sig, ok := rtTable[name]
		if !ok {
			panic("llvmbackend: 未登记的运行时函数 " + name)
		}
		g.decls[name] = sig.render(name)
		g.declOrder = append(g.declOrder, name)
	}
	return "@" + name
}

func (g *Gen) callZ(name string, args ...string) *Value {
	v := g.tempZ()
	suffix := ""
	if len(args) > 0 {
		suffix = ", " + strings.Join(args, ", ")
	}
	g.emit("call void %s(ptr sret(%%ZVal) %s%s)", g.rt(name), v, suffix)
	return &Value{Slot: v}
}

func (g *Gen) callR(ret, name string, args ...string) string {
	r := g.fb.nextTemp()
	g.emit("%s = call %s %s(%s)", r, ret, g.rt(name), strings.Join(args, ", "))
	return r
}

func (g *Gen) callVoid(name string, args ...string) {
	g.emit("call void %s(%s)", g.rt(name), strings.Join(args, ", "))
}

// ── 值操作 ─────────────────────────────────────────────────

func (g *Gen) loadZ(slot string) string {
	r := g.fb.nextTemp()
	g.emit("%s = load %%ZVal, ptr %s, align 8", r, slot)
	return r
}

func (g *Gen) storeZ(reg, slot string) {
	g.emit("store %%ZVal %s, ptr %s, align 8", reg, slot)
}

func (g *Gen) copyZ(dst, src string) {
	g.storeZ(g.loadZ(src), dst)
}

// truthy 把 %ZVal 槽位转成 i1。
func (g *Gen) truthy(v *Value) string {
	r := g.callR("i32", "z_truthy", "ptr "+v.Slot)
	b := g.fb.nextTemp()
	g.emit("%s = icmp ne i32 %s, 0", b, r)
	return b
}

// ── 常量 ───────────────────────────────────────────────────

func floatLit(f float64) string {
	if math.IsInf(f, 1) {
		return "0x7FF0000000000000"
	}
	if math.IsInf(f, -1) {
		return "0xFFF0000000000000"
	}
	if math.IsNaN(f) {
		return "0x7FF8000000000000"
	}
	return strconv.FormatFloat(f, 'g', -1, 64)
}

func (g *Gen) constInt(n int64) *Value {
	return g.callZ("z_int", "i64 "+strconv.FormatInt(n, 10))
}

func (g *Gen) constFloat(f float64) *Value {
	return g.callZ("z_float", "double "+floatLit(f))
}

func (g *Gen) constBool(b bool) *Value {
	n := 0
	if b {
		n = 1
	}
	return g.callZ("z_bool", "i32 "+strconv.Itoa(n))
}

func (g *Gen) constNull() *Value { return g.callZ("z_null") }

func (g *Gen) strConst(s string) string {
	if i, ok := g.strIdx[s]; ok {
		return fmt.Sprintf("@.str.%d", i)
	}
	i := len(g.strDefs)
	g.strIdx[s] = i
	data := []byte(s)
	var sb strings.Builder
	for _, c := range data {
		switch {
		case c == '"':
			sb.WriteString("\\22")
		case c == '\\':
			sb.WriteString("\\5C")
		case c >= 0x20 && c < 0x7f:
			sb.WriteByte(c)
		default:
			fmt.Fprintf(&sb, "\\%02X", c)
		}
	}
	g.strDefs = append(g.strDefs, fmt.Sprintf("@.str.%d = private unnamed_addr constant [%d x i8] c\"%s\\00\"", i, len(data)+1, sb.String()))
	return fmt.Sprintf("@.str.%d", i)
}

func (g *Gen) constStr(s string) *Value {
	return g.callZ("z_str", "ptr "+g.strConst(s))
}

// zvalArray 把一组值落到连续 %ZVal 数组（供 z_print/z_call/z_concat 等 ZVal* 参数）。
func (g *Gen) zvalArray(vals []*Value) (string, int) {
	n := len(vals)
	if n == 0 {
		return "null", 0
	}
	arr := g.alloca(fmt.Sprintf("[%d x %%ZVal]", n))
	for i, v := range vals {
		p := g.fb.nextTemp()
		g.emit("%s = getelementptr [%d x %%ZVal], ptr %s, i64 0, i64 %d", p, n, arr, i)
		g.copyZ(p, v.Slot)
	}
	return arr, n
}

// ── 作用域 ─────────────────────────────────────────────────

func (g *Gen) pushScope() { g.scopes = append(g.scopes, map[string]varSlot{}) }

func (g *Gen) popScope() {
	if len(g.scopes) > 0 {
		g.scopes = g.scopes[:len(g.scopes)-1]
	}
}

func (g *Gen) setVar(name string, s varSlot) {
	if len(g.scopes) == 0 {
		g.pushScope()
	}
	g.scopes[len(g.scopes)-1][name] = s
}

func (g *Gen) lookupVar(name string) (varSlot, bool) {
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if s, ok := g.scopes[i][name]; ok {
			return s, true
		}
	}
	return varSlot{}, false
}

// varRef 把变量槽位解析为「指向 %ZVal 的 ptr」。
func (g *Gen) varRef(s varSlot) (string, error) {
	switch s.kind {
	case vPlain, vGlobal:
		return s.slot, nil
	case vBox:
		box := g.fb.nextTemp()
		g.emit("%s = load ptr, ptr %s, align 8", box, s.slot)
		p := g.fb.nextTemp()
		g.emit("%s = getelementptr %%ZBox, ptr %s, i32 0, i32 1", p, box)
		return p, nil
	case vCapture:
		q := g.fb.nextTemp()
		g.emit("%s = getelementptr i8, ptr %s, i64 %d", q, g.fb.envPtr, s.capIdx*8)
		box := g.fb.nextTemp()
		g.emit("%s = load ptr, ptr %s, align 8", box, q)
		p := g.fb.nextTemp()
		g.emit("%s = getelementptr %%ZBox, ptr %s, i32 0, i32 1", p, box)
		return p, nil
	}
	return "", fmt.Errorf("无效的变量槽位")
}

// boxVar 新建一个装箱变量（vBox），value 为初值。
func (g *Gen) boxVar(name string, v *Value) string {
	box := g.callR("ptr", "z_box", "ptr "+v.Slot)
	slot := g.alloca("ptr")
	g.emit("store ptr %s, ptr %s, align 8", box, slot)
	g.setVar(name, varSlot{kind: vBox, slot: slot})
	return slot
}

// ── 命名 ───────────────────────────────────────────────────

func sanitizeIdent(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r > 127:
			fmt.Fprintf(&b, "_u%04X", r)
		default:
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" {
		return "_"
	}
	if s[0] >= '0' && s[0] <= '9' {
		return "_" + s
	}
	return s
}

func cFuncName(name string) string {
	if name == "" {
		return "zf_anonymous"
	}
	return "zf_" + sanitizeIdent(name)
}

func cGlobalName(name string) string { return "zg_" + sanitizeIdent(name) }

func (g *Gen) fnSym(fi *fnInfo) string { return "@" + fi.CName }

func (g *Gen) globalSlot(name string) string { return "@" + cGlobalName(name) }

// ── 闭包构造 ───────────────────────────────────────────────

// genClosureRef 生成一个函数值（ZVal），按需构造 env。
func (g *Gen) genClosureRef(fi *fnInfo) (*Value, error) {
	display := fi.Name
	if display == "" {
		display = "匿名"
	}
	if fi.EnvType == "" {
		return g.callZ("z_closure", "ptr "+g.fnSym(fi), "ptr null", "ptr "+g.strConst(display)), nil
	}
	n := len(fi.Captures)
	offs := "null"
	if n > 0 {
		arr := g.alloca(fmt.Sprintf("[%d x i64]", n))
		for i := 0; i < n; i++ {
			p := g.fb.nextTemp()
			g.emit("%s = getelementptr [%d x i64], ptr %s, i64 0, i64 %d", p, n, arr, i)
			g.emit("store i64 %d, ptr %s, align 8", i*8, p)
		}
		offs = arr
	}
	size := n * 8
	if size < 8 {
		size = 8
	}
	env := g.callR("ptr", "z_new_env_ex", fmt.Sprintf("i32 %d", n), "ptr "+offs, fmt.Sprintf("i64 %d", size))
	for i, name := range fi.Captures {
		slot, ok := g.lookupVar(name)
		if !ok {
			return nil, fmt.Errorf("闭包捕获失败：找不到外层变量 %s", name)
		}
		var box string
		switch slot.kind {
		case vBox:
			box = g.fb.nextTemp()
			g.emit("%s = load ptr, ptr %s, align 8", box, slot.slot)
		case vCapture:
			q := g.fb.nextTemp()
			g.emit("%s = getelementptr i8, ptr %s, i64 %d", q, g.fb.envPtr, slot.capIdx*8)
			box = g.fb.nextTemp()
			g.emit("%s = load ptr, ptr %s, align 8", box, q)
		case vPlain:
			box = g.callR("ptr", "z_box", "ptr "+slot.slot)
		default:
			return nil, fmt.Errorf("闭包捕获失败：变量 %s 形态不支持", name)
		}
		p := g.fb.nextTemp()
		g.emit("%s = getelementptr i8, ptr %s, i64 %d", p, env, i*8)
		g.emit("store ptr %s, ptr %s, align 8", box, p)
	}
	return g.callZ("z_closure", "ptr "+g.fnSym(fi), "ptr "+env, "ptr "+g.strConst(display)), nil
}

// ── GC 安全点 ──────────────────────────────────────────────

func (g *Gen) emitSafepoint() {
	if os.Getenv("ZUZO_NO_SAFEPOINT") != "" {
		return
	}
	if g.fb == nil || g.fb.terminated {
		return
	}
	seen := map[string]bool{}
	var vals, boxes []string
	for i := len(g.scopes) - 1; i >= 0; i-- {
		sc := g.scopes[i]
		names := make([]string, 0, len(sc))
		for n := range sc {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, n := range names {
			if seen[n] {
				continue
			}
			seen[n] = true
			s := sc[n]
			switch s.kind {
			case vPlain:
				vals = append(vals, s.slot)
			case vBox:
				boxes = append(boxes, s.slot)
			}
		}
	}
	for _, n := range g.globalOrder {
		if seen[n] {
			continue
		}
		seen[n] = true
		vals = append(vals, g.globalSlot(n))
	}
	if len(vals) == 0 && len(boxes) == 0 {
		return
	}
	env := "null"
	if g.fb.envPtr != "" {
		env = g.fb.envPtr
	}
	emitArr := func(typ string, items []string) string {
		if len(items) == 0 {
			return "null"
		}
		arr := g.alloca(fmt.Sprintf("[%d x ptr]", len(items)))
		for i, it := range items {
			p := g.fb.nextTemp()
			g.emit("%s = getelementptr [%d x ptr], ptr %s, i64 0, i64 %d", p, len(items), arr, i)
			g.emit("store ptr %s, ptr %s, align 8", it, p)
		}
		return arr
	}
	varr := emitArr("ZVal", vals)
	barr := emitArr("Box", boxes)
	g.callVoid("z_gc_safepoint", fmt.Sprintf("i32 %d", len(vals)), "ptr "+varr,
		fmt.Sprintf("i32 %d", len(boxes)), "ptr "+barr, "ptr "+env)
}

// ── 错误 ───────────────────────────────────────────────────

func (g *Gen) unsupported(what string) {
	g.errs = append(g.errs, what)
}

func (g *Gen) unsupportedAt(line, col int, what string) {
	g.errs = append(g.errs, fmt.Sprintf("%d:%d: %s", line, col, what))
}
