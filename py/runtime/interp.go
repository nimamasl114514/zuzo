package runtime

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"

	"zuzo/py/ast"
	"zuzo/py/object"
)

// Interp Python 解释器实例
type Interp struct {
	Globals  *object.Env // 全局作用域
	Builtins *object.Env // 内置作用域
	Modules  map[string]*object.Module
	SysPath  []string
	Stdout   io.Writer
	Stderr   io.Writer
	Cwd      string
	MainFile string
	Depth    int
	MaxDepth int
	Frames   []string
	genStack []*genCtx
	currentEnv *object.Env
	kwOrder    []string // 最近一次调用的关键字参数顺序
	ExcTypes map[string]*object.TypeObj
	// numCache 数字字面量解析结果缓存（key = AST 节点指针）。
	// 循环条件/常量在热路径上每轮都会求值，重复 ParseInt 是主要开销之一；
	// 用 sync.Map 因为 threading/asyncio 模块会在 goroutine 中并发求值。
	numCache sync.Map

	// gil 模拟 CPython 的全局解释器锁：threading 线程体与主执行流互斥，
	// 避免多 goroutine 并发读写同一个 map 触发 Go runtime 的
	// "concurrent map read and map write" 致命错误（t18_concurrency 实锤）。
	gil     sync.Mutex
	gilHeld atomic.Bool
}

// PyError Python 异常（用 panic 传播）
type PyError struct {
	Exc       *object.Exception
	Frames    []string
	Line      int
	File      string
	Recovered bool
}

func (e *PyError) Error() string {
	if e.Exc == nil {
		return "<nil exception>"
	}
	return e.Exc.Class.Name + ": " + e.Exc.Str()
}

// New 创建解释器
func New() *Interp {
	ip := &Interp{
		Modules:  map[string]*object.Module{},
		Stdout:   os.Stdout,
		Stderr:   os.Stderr,
		MaxDepth: 4000,
		ExcTypes: map[string]*object.TypeObj{},
	}
	ip.Builtins = object.NewGlobalEnv()
	ip.Globals = object.NewEnv(ip.Builtins)
	ip.Globals.Globals = ip.Globals
	ip.Builtins.Globals = ip.Globals
	cwd, _ := os.Getwd()
	ip.Cwd = cwd
	ip.SysPath = []string{ip.Cwd, "."}
	registerBuiltins(ip)
	registerBuiltinModules(ip)
	return ip
}

// ==================== 控制流信号 ====================

type ctrlKind int

const (
	ctrlReturn ctrlKind = iota + 1
	ctrlBreak
	ctrlContinue
)

type ctrlSignal struct {
	kind ctrlKind
	val  object.Object
}

// ==================== 异常构造 ====================

// raise 抛出异常
func (ip *Interp) raise(cls *object.TypeObj, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	exc := object.NewException(cls, &object.Str{V: msg})
	panic(&PyError{Exc: exc, Frames: append([]string{}, ip.Frames...), File: ip.MainFile})
}

// RaiseObj 抛出异常对象
func (ip *Interp) RaiseObj(exc *object.Exception) {
	panic(&PyError{Exc: exc, Frames: append([]string{}, ip.Frames...), File: ip.MainFile})
}

// TypeError 类型错误
func (ip *Interp) TypeError(format string, a ...interface{}) {
	ip.raise(ip.excType("TypeError"), format, a...)
}

// ValueError 值错误
func (ip *Interp) ValueError(format string, a ...interface{}) {
	ip.raise(ip.excType("ValueError"), format, a...)
}

// NameErr 名字错误
func (ip *Interp) NameErr(name string) {
	ip.raise(ip.excType("NameError"), "name '%s' is not defined", name)
}

// IndexErr 索引错误
func (ip *Interp) IndexErr(format string, a ...interface{}) {
	ip.raise(ip.excType("IndexError"), format, a...)
}

// KeyErr 键错误
func (ip *Interp) KeyErr(key object.Object) {
	ip.raise(ip.excType("KeyError"), "%s", key.Repr())
}

// AttrErr 属性错误
func (ip *Interp) AttrErr(o object.Object, attr string) {
	ip.raise(ip.excType("AttributeError"), "'%s' object has no attribute '%s'", object.TypeName(o), attr)
}

// RuntimeErr 运行时错误
func (ip *Interp) RuntimeErr(format string, a ...interface{}) {
	ip.raise(ip.excType("RuntimeError"), format, a...)
}

// ZeroDivErr 除零
func (ip *Interp) ZeroDivErr(format string, a ...interface{}) {
	ip.raise(ip.excType("ZeroDivisionError"), format, a...)
}

// OverflowErr 溢出
func (ip *Interp) OverflowErr(format string, a ...interface{}) {
	ip.raise(ip.excType("OverflowError"), format, a...)
}

// 取异常类型
func (ip *Interp) excType(name string) *object.TypeObj {
	if t, ok := ip.ExcTypes[name]; ok {
		return t
	}
	return ip.ExcTypes["Exception"]
}

// ==================== 保护执行 ====================

// protect 执行 fn，捕获 PyError
func (ip *Interp) protect(fn func()) (pe *PyError) {
	defer func() {
		if r := recover(); r != nil {
			if e, ok := r.(*PyError); ok {
				pe = e
				return
			}
			panic(r)
		}
	}()
	fn()
	return nil
}

// ==================== 模块执行 ====================

// ExecModule 执行模块
func (ip *Interp) ExecModule(m *ast.Module, env *object.Env) {
	for _, st := range m.Body {
		ip.execStmt(st, env)
	}
}

// RunModule 顶层运行，返回捕获的异常
func (ip *Interp) RunModule(m *ast.Module) (pe *PyError) {
	ip.gil.Lock()
	ip.gilHeld.Store(true)
	defer func() {
		ip.gilHeld.Store(false)
		ip.gil.Unlock()
	}()
	return ip.protect(func() { ip.ExecModule(m, ip.Globals) })
}

// ==================== 语句执行 ====================

func (ip *Interp) execBlock(stmts []ast.Stmt, env *object.Env) {
	for _, s := range stmts {
		ip.execStmt(s, env)
	}
}

func (ip *Interp) execStmt(s ast.Stmt, env *object.Env) {
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*PyError); ok && pe.Line == 0 && s != nil {
				pe.Line = s.Line()
			}
			panic(r)
		}
	}()
	ip.execStmtInner(s, env)
}

func (ip *Interp) execStmtInner(s ast.Stmt, env *object.Env) {
	switch node := s.(type) {
	case *ast.ExprStmt:
		e := ip.evalExpr(node.X, env)
		if node.X == nil {
			return
		}
		_ = e
	case *ast.Assign:
		val := ip.evalExpr(node.Value, env)
		for _, t := range node.Targets {
			ip.assign(t, val, env)
		}
	case *ast.AugAssign:
		ip.execAugAssign(node, env)
	case *ast.AnnAssign:
		if n, ok := node.Target.(*ast.Name); ok && env.IsClass {
			env.ClassAnnotations = append(env.ClassAnnotations, n.Id)
		}
		if node.Value != nil {
			v := ip.evalExpr(node.Value, env)
			ip.assign(node.Target, v, env)
		}
	case *ast.If:
		if object.Truthy(ip.evalExpr(node.Cond, env)) {
			ip.execBlock(node.Body, env)
		} else {
			ip.execBlock(node.Orelse, env)
		}
	case *ast.While:
		ip.execWhile(node, env)
	case *ast.For:
		ip.execFor(node, env)
	case *ast.FuncDef:
		fn := ip.makeFunction(node, env)
		ip.bindName(node.Name, fn, env)
	case *ast.ClassDef:
		cls := ip.makeClass(node, env)
		ip.bindName(node.Name, cls, env)
	case *ast.Return:
		var v object.Object = object.None
		if node.Value != nil {
			v = ip.evalExpr(node.Value, env)
		}
		panic(&ctrlSignal{kind: ctrlReturn, val: v})
	case *ast.Break:
		panic(&ctrlSignal{kind: ctrlBreak})
	case *ast.Continue:
		panic(&ctrlSignal{kind: ctrlContinue})
	case *ast.Pass:
	case *ast.Try:
		ip.execTry(node, env)
	case *ast.Raise:
		ip.execRaise(node, env)
	case *ast.With:
		ip.execWith(node, env)
	case *ast.Global:
		if env.GlobalNames == nil {
			env.GlobalNames = map[string]bool{}
		}
		for _, n := range node.Names {
			env.GlobalNames[n] = true
		}
	case *ast.Nonlocal:
		if env.NonlocalNames == nil {
			env.NonlocalNames = map[string]bool{}
		}
		for _, n := range node.Names {
			env.NonlocalNames[n] = true
		}
	case *ast.Del:
		for _, t := range node.Targets {
			ip.deleteTarget(t, env)
		}
	case *ast.Assert:
		if !object.Truthy(ip.evalExpr(node.Test, env)) {
			msg := "assertion failed"
			if node.Msg != nil {
				msg = ip.evalExpr(node.Msg, env).Str()
			}
			ip.raise(ip.excType("AssertionError"), "%s", msg)
		}
	case *ast.Import:
		ip.execImport(node, env)
	case *ast.ImportFrom:
		ip.execImportFrom(node, env)
	case *ast.Match:
		ip.execMatch(node, env)
	default:
		ip.RuntimeErr("未支持的语句类型 %T", s)
	}
}

// bindName 按作用域规则绑定名字
func (ip *Interp) bindName(name string, v object.Object, env *object.Env) {
	if env.GlobalNames[name] {
		ip.Globals.Set(name, v)
		return
	}
	if env.NonlocalNames[name] {
		for cur := env.Parent; cur != nil && cur != ip.Globals; cur = cur.Parent {
			if _, ok := cur.Vars[name]; ok {
				cur.Vars[name] = v
				return
			}
		}
	}
	env.Set(name, v)
}

func (ip *Interp) execWhile(node *ast.While, env *object.Env) {
	// 热路径：整数计数循环直接用原生 int64 执行（语义与原路径一致，不匹配则回退）
	if _, ok := ip.tryNativeWhile(node, env); ok {
		return
	}
	for object.Truthy(ip.evalExpr(node.Cond, env)) {
		brk, cont := ip.execLoopBody(node.Body, env)
		if brk {
			return
		}
		_ = cont
	}
	ip.execBlock(node.Orelse, env)
}

func (ip *Interp) execFor(node *ast.For, env *object.Env) {
	iter := ip.Iter(ip.evalExpr(node.Iter, env))
	for {
		v, ok := ip.IterNext(iter)
		if !ok {
			break
		}
		ip.assign(node.Target, v, env)
		brk, _ := ip.execLoopBody(node.Body, env)
		if brk {
			return
		}
	}
	ip.execBlock(node.Orelse, env)
}

// execLoopBody 执行循环体，返回 (是否 break)
func (ip *Interp) execLoopBody(body []ast.Stmt, env *object.Env) (brk bool, cont bool) {
	defer func() {
		if r := recover(); r != nil {
			if sig, ok := r.(*ctrlSignal); ok {
				switch sig.kind {
				case ctrlBreak:
					brk = true
					cont = false
					return
				case ctrlContinue:
					brk = false
					cont = true
					return
				}
			}
			panic(r)
		}
	}()
	ip.execBlock(body, env)
	return false, false
}

func (ip *Interp) execTry(node *ast.Try, env *object.Env) {
	var pending *PyError
	if node.Finally != nil {
		defer func() {
			// finally 若抛异常则覆盖原异常
			ip.execBlock(node.Finally, env)
		}()
	}
	pending = ip.protect(func() {
		ip.execBlock(node.Body, env)
	})
	if pending != nil {
		handled := false
		for _, h := range node.Handlers {
			if h.Type == nil || ip.excMatches(pending.Exc, h.Type, env) {
				if h.Name != "" {
					ip.assignName(h.Name, pending.Exc, env)
				}
				ip.execBlock(h.Body, env)
				handled = true
				break
			}
		}
		if !handled {
			ip.RaiseObj(pending.Exc)
		}
		return
	}
	ip.execBlock(node.Orelse, env)
}

// excMatches 判断异常是否匹配 except 子句的类型
func (ip *Interp) excMatches(exc *object.Exception, typeExpr ast.Expr, env *object.Env) bool {
	tv := ip.evalExpr(typeExpr, env)
	return ip.excMatchesObj(exc, tv)
}

func (ip *Interp) excMatchesObj(exc *object.Exception, tv object.Object) bool {
	switch t := tv.(type) {
	case *object.TypeObj:
		return exc.Class.IsSubclass(t)
	case *object.Tuple:
		for _, it := range t.Items {
			if tt, ok := it.(*object.TypeObj); ok && exc.Class.IsSubclass(tt) {
				return true
			}
		}
		return false
	}
	return false
}

func (ip *Interp) execRaise(node *ast.Raise, env *object.Env) {
	if node.Exc == nil {
		ip.RuntimeErr("No active exception to re-raise")
	}
	v := ip.evalExpr(node.Exc, env)
	var exc *object.Exception
	switch e := v.(type) {
	case *object.Exception:
		exc = e
	case *object.TypeObj:
		if e.IsExc {
			exc = object.NewException(e)
		} else {
			ip.TypeError("exceptions must derive from BaseException")
		}
	default:
		ip.TypeError("exceptions must derive from BaseException")
	}
	if node.Cause != nil {
		cv := ip.evalExpr(node.Cause, env)
		if ce, ok := cv.(*object.Exception); ok {
			exc.Cause = ce
		}
	}
	ip.RaiseObj(exc)
}

func (ip *Interp) execWith(node *ast.With, env *object.Env) {
	ip.execWithItems(node.Items, node.Body, env)
}

func (ip *Interp) execWithItems(items []ast.WithItem, body []ast.Stmt, env *object.Env) {
	if len(items) == 0 {
		ip.execBlock(body, env)
		return
	}
	item := items[0]
	ctx := ip.evalExpr(item.Context, env)
	enter := ip.GetAttr(ctx, "__enter__")
	exit := ip.GetAttr(ctx, "__exit__")
	val := ip.Call(enter, nil, nil)
	if item.Vars != nil {
		ip.assign(item.Vars, val, env)
	}
	pending := ip.protect(func() {
		ip.execWithItems(items[1:], body, env)
	})
	var excObj object.Object = object.None
	if pending != nil {
		excObj = pending.Exc
	}
	res := ip.Call(exit, []object.Object{excObj.Type(), excObj, object.None}, nil)
	if pending != nil && !object.Truthy(res) {
		ip.RaiseObj(pending.Exc)
	}
}

func (ip *Interp) execAugAssign(node *ast.AugAssign, env *object.Env) {
	// 目标为名字的快速路径
	if n, ok := node.Target.(*ast.Name); ok {
		cur, found := env.Get(n.Id)
		if !found {
			ip.NameErr(n.Id)
		}
		val := ip.evalExpr(node.Value, env)
		res := ip.BinOp(node.Op, cur, val)
		ip.bindName(n.Id, res, env)
		return
	}
	// 属性 / 下标
	switch t := node.Target.(type) {
	case *ast.Attribute:
		obj := ip.evalExpr(t.Value, env)
		cur := ip.GetAttr(obj, t.Attr)
		val := ip.evalExpr(node.Value, env)
		res := ip.BinOp(node.Op, cur, val)
		ip.SetAttr(obj, t.Attr, res)
		return
	case *ast.Subscript:
		obj := ip.evalExpr(t.Value, env)
		idx := ip.evalSubscript(obj, t.Slice, env)
		cur := ip.GetItem(obj, idx)
		val := ip.evalExpr(node.Value, env)
		res := ip.BinOp(node.Op, cur, val)
		ip.SetItem(obj, idx, res)
		return
	}
	ip.RuntimeErr("非法赋值目标")
}

func (ip *Interp) deleteTarget(t ast.Expr, env *object.Env) {
	switch n := t.(type) {
	case *ast.Name:
		if !env.Delete(n.Id) {
			for cur := env.Parent; cur != nil; cur = cur.Parent {
				if cur.Delete(n.Id) {
					return
				}
			}
			ip.NameErr(n.Id)
		}
	case *ast.Subscript:
		obj := ip.evalExpr(n.Value, env)
		idx := ip.evalSubscript(obj, n.Slice, env)
		ip.DelItem(obj, idx)
	case *ast.Attribute:
		obj := ip.evalExpr(n.Value, env)
		if inst, ok := obj.(*object.Instance); ok {
			delete(inst.Dict, n.Attr)
			return
		}
		ip.AttrErr(obj, n.Attr)
	case *ast.Tuple:
		for _, e := range n.Elements {
			ip.deleteTarget(e, env)
		}
	}
}

// assignName 赋值到名字（遵循作用域规则）
func (ip *Interp) assignName(name string, v object.Object, env *object.Env) {
	ip.bindName(name, v, env)
}

// ==================== 输出 ====================

// Printf 输出到 stdout
func (ip *Interp) Printf(format string, a ...interface{}) {
	fmt.Fprintf(ip.Stdout, format, a...)
}

// SetMainFile 设置主文件名（用于错误报告）
func (ip *Interp) SetMainFile(path string) {
	ip.MainFile = path
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		ip.SysPath = append([]string{dir}, ip.SysPath...)
	}
}

// FormatTraceback 格式化异常回溯
func (ip *Interp) FormatTraceback(pe *PyError) string {
	var sb strings.Builder
	sb.WriteString("Traceback (most recent call last):\n")
	frames := pe.Frames
	if len(frames) == 0 {
		frames = ip.Frames
	}
	for _, f := range frames {
		sb.WriteString("  " + f + "\n")
	}
	if pe.Line > 0 {
		sb.WriteString(fmt.Sprintf("  第 %d 行, in <module>\n", pe.Line))
	}
	if pe.Exc != nil {
		sb.WriteString(pe.Exc.Class.Name)
		if s := pe.Exc.Str(); s != "" {
			sb.WriteString(": " + s)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}
