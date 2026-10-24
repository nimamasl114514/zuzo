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
	Globals    *object.Env // 全局作用域
	Builtins   *object.Env // 内置作用域
	Modules    map[string]*object.Module
	SysPath    []string
	Stdout     io.Writer
	Stderr     io.Writer
	Cwd        string
	MainFile   string
	Depth      int
	MaxDepth   int
	Frames     []string
	genStack   []*genCtx
	currentEnv *object.Env
	kwOrder    []string // 最近一次调用的关键字参数顺序
	ExcTypes   map[string]*object.TypeObj
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
//
// CPython 语义：KeyError.args = (key,)，str() 时才对 key 取 repr。
// 这里必须传原始对象，否则会在 Str() 的 KeyError 特例里二次 repr（出现 "'missing'" 双层引号）。
func (ip *Interp) KeyErr(key object.Object) {
	exc := object.NewException(ip.excType("KeyError"), key)
	ip.RaiseObj(exc)
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
	if node.IsAsync {
		ip.execAsyncFor(node, env)
		return
	}
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

// execAsyncFor 执行 async for（CPython 语义）：
//
//	aiter = iterable.__aiter__()
//	while True:
//	    try: value = await aiter.__anext__()
//	    except StopAsyncIteration: break
//	    target = value; body
//	else: orelse
func (ip *Interp) execAsyncFor(node *ast.For, env *object.Env) {
	aiter := ip.getAsyncIter(ip.evalExpr(node.Iter, env))
	for {
		v, ok := ip.asyncIterNext(aiter)
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

// getAsyncIter 获取异步迭代器（等价于 CPython 的 GET_AITER）
func (ip *Interp) getAsyncIter(o object.Object) object.Object {
	switch v := o.(type) {
	case *object.Generator:
		// 本运行时的 async def 函数（含 async generator）即 Generator，自身即异步迭代器
		return v
	case *object.Instance:
		m, ok := ip.findDunder(v.Class, "__aiter__")
		if !ok {
			break
		}
		// CPython 3.7+：__aiter__ 必须直接返回异步迭代器（不再 await）
		r := ip.Call(ip.bindMethod(v, m), nil, nil)
		if ip.isAsyncIterator(r) {
			return r
		}
		ip.TypeError("__aiter__ returned non-async-iterator (type %s)", object.TypeName(r))
	}
	ip.TypeError("'async for' requires an object with __aiter__ method, got %s", object.TypeName(o))
	return nil
}

// isAsyncIterator 判断对象是否具备 __anext__ 协议
func (ip *Interp) isAsyncIterator(o object.Object) bool {
	switch v := o.(type) {
	case *object.Generator:
		return true
	case *object.Instance:
		_, ok := ip.findDunder(v.Class, "__anext__")
		return ok
	}
	return false
}

// asyncIterNext 推进异步迭代器（等价于 GET_ANEXT + SEND + 捕获 StopAsyncIteration）
func (ip *Interp) asyncIterNext(it object.Object) (object.Object, bool) {
	switch v := it.(type) {
	case *object.Generator:
		// 原生 async generator：Next 已把 await 内部驱动完
		val, done, err := v.Next()
		if err != nil {
			if pe, ok := err.(*PyError); ok {
				if ip.isStopAsyncIteration(pe.Exc) {
					return nil, false
				}
				panic(pe)
			}
			ip.RuntimeErr("%v", err)
		}
		if done {
			return nil, false
		}
		return val, true
	case *object.Instance:
		m, ok := ip.findDunder(v.Class, "__anext__")
		if !ok {
			break
		}
		var val object.Object
		pending := ip.protect(func() {
			val = ip.awaitValue(ip.Call(ip.bindMethod(v, m), nil, nil))
		})
		if pending != nil {
			if ip.isStopAsyncIteration(pending.Exc) {
				return nil, false
			}
			if pending.Exc != nil && pending.Exc.Class.Name == "StopIteration" {
				ip.RuntimeErr("coroutine raised StopIteration")
			}
			ip.RaiseObj(pending.Exc)
		}
		return val, true
	}
	ip.TypeError("'%s' object is not an async iterator", object.TypeName(it))
	return nil, false
}

// isStopAsyncIteration 判断异常是否为 StopAsyncIteration（含子类）
func (ip *Interp) isStopAsyncIteration(e *object.Exception) bool {
	if e == nil || e.Class == nil {
		return false
	}
	for c := e.Class; c != nil; {
		if c.Name == "StopAsyncIteration" {
			return true
		}
		if len(c.Bases) == 0 {
			break
		}
		c = c.Bases[0]
	}
	return false
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
			// raise X from Y：显式因果链，同时抑制隐式上下文（CPython 语义）
			exc.SetCause(ce)
			exc.SetSuppressContext(true)
		} else if cv == object.None {
			// raise X from None：显式声明「不展示隐式上下文」
			exc.SetCause(nil)
			exc.SetSuppressContext(true)
		}
	}
	ip.RaiseObj(exc)
}

func (ip *Interp) execWith(node *ast.With, env *object.Env) {
	if node.IsAsync {
		ip.execAsyncWithItems(node.Items, node.Body, env)
		return
	}
	ip.execWithItems(node.Items, node.Body, env)
}

// lookupSpecialMethod 按 CPython 语义查找特殊方法：在类型上查找（绕过实例字典），
// 绑定实例后返回；本地原生对象按类型字典查找。
func (ip *Interp) lookupSpecialMethod(o object.Object, name string) (object.Object, bool) {
	if inst, ok := o.(*object.Instance); ok {
		if m, ok := ip.findDunder(inst.Class, name); ok {
			return ip.bindMethod(inst, m), true
		}
		return nil, false
	}
	if cls := o.Type(); cls != nil {
		if m, ok := ip.findDunder(cls, name); ok {
			return ip.bindMethod(o, m), true
		}
	}
	return nil, false
}

// execWithItems 执行同步 with（多项按 CPython 语义嵌套展开：外层先 enter，
// 退出时内层先 exit；内层抑制异常则外层收到 None）。
func (ip *Interp) execWithItems(items []ast.WithItem, body []ast.Stmt, env *object.Env) {
	if len(items) == 0 {
		ip.execBlock(body, env)
		return
	}
	item := items[0]
	ctx := ip.evalExpr(item.Context, env)
	// CPython 顺序：先查 __exit__，再查 __enter__，再调用 __enter__
	exit, ok := ip.lookupSpecialMethod(ctx, "__exit__")
	if !ok {
		ip.TypeError("'%s' object does not support the context manager protocol (missed __exit__ method)", object.TypeName(ctx))
	}
	enter, ok := ip.lookupSpecialMethod(ctx, "__enter__")
	if !ok {
		ip.TypeError("'%s' object does not support the context manager protocol", object.TypeName(ctx))
	}
	val := ip.Call(enter, nil, nil)
	if item.Vars != nil {
		ip.assign(item.Vars, val, env)
	}
	pending := ip.protect(func() {
		ip.execWithItems(items[1:], body, env)
	})
	// CPython 无异常时传 (None, None, None)
	var excTypeObj, excObj object.Object = object.None, object.None
	if pending != nil {
		excTypeObj = pending.Exc.Type()
		excObj = pending.Exc
	}
	res := ip.Call(exit, []object.Object{excTypeObj, excObj, object.None}, nil)
	if pending != nil && !object.Truthy(res) {
		ip.RaiseObj(pending.Exc)
	}
}

// execAsyncWithItems 执行 async with（CPython 语义）：
//
//	aexit = type(mgr).__aexit__; aenter = type(mgr).__aenter__
//	value = await aenter(mgr)
//	try: body
//	except BaseException as e: await aexit(type(e), e, None) → 假值则继续抛
//	else: await aexit(None, None, None)
//
// 多项时同样按嵌套展开：外层先 aenter，退出时内层先 aexit。
func (ip *Interp) execAsyncWithItems(items []ast.WithItem, body []ast.Stmt, env *object.Env) {
	if len(items) == 0 {
		ip.execBlock(body, env)
		return
	}
	item := items[0]
	ctx := ip.evalExpr(item.Context, env)
	// CPython 顺序：先查 __aenter__（缺失报通用提示），再查 __aexit__（带 missed 提示）
	aenter, ok := ip.lookupSpecialMethod(ctx, "__aenter__")
	if !ok {
		ip.TypeError("'%s' object does not support the asynchronous context manager protocol", object.TypeName(ctx))
	}
	aexit, ok := ip.lookupSpecialMethod(ctx, "__aexit__")
	if !ok {
		ip.TypeError("'%s' object does not support the asynchronous context manager protocol (missed __aexit__ method)", object.TypeName(ctx))
	}
	val := ip.awaitValue(ip.Call(aenter, nil, nil))
	if item.Vars != nil {
		ip.assign(item.Vars, val, env)
	}
	pending := ip.protect(func() {
		ip.execAsyncWithItems(items[1:], body, env)
	})
	var excTypeObj, excObj object.Object = object.None, object.None
	if pending != nil {
		excTypeObj = pending.Exc.Type()
		excObj = pending.Exc
	}
	res := ip.awaitValue(ip.Call(aexit, []object.Object{excTypeObj, excObj, object.None}, nil))
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
