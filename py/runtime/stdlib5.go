package runtime

import (
	"zuzo/py/object"
)

// registerStdlib5 注册第五批补充 API：contextlib
// 通过包级 init 挂载，避免改动 stdlib.go 的注册处（多分支并行开发约定）。
func init() {
	builtinModuleInits["contextlib"] = modContextlib
}

// ==================== 生成器驱动器（支持 throw 语义） ====================

// cmAction 恢复生成器时的动作：throw != nil 表示在 yield 处抛入异常
type cmAction struct {
	throw *object.Exception
	val   object.Object
}

// cmResult 生成器一步的结果
type cmResult struct {
	val  object.Object
	done bool
	err  error
	ret  object.Object
}

// cmGen 用 goroutine + channel 驱动用户生成器函数体，
// 与 call.go 的 newGenerator 语义一致，额外支持「在 yield 处抛入异常」（throw）。
type cmGen struct {
	ip      *Interp
	f       *object.Function
	args    []object.Object
	kwargs  map[string]object.Object
	in      chan cmAction
	out     chan cmResult
	started bool
	closed  bool
}

var cmGenType = &object.TypeObj{Name: "generator", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}

func (g *cmGen) Type() *object.TypeObj { return cmGenType }
func (g *cmGen) Repr() string          { return "<generator object " + g.f.Name + ">" }
func (g *cmGen) Str() string           { return g.Repr() }

// step 推进生成器一步
func (g *cmGen) step(act cmAction) cmResult {
	if g.closed {
		return cmResult{done: true}
	}
	if !g.started {
		g.started = true
		go g.run()
	} else {
		// in 带 1 缓冲：即使生成器 goroutine 已退出，也不会永久阻塞
		g.in <- act
	}
	r, ok := <-g.out
	if !ok {
		g.closed = true
		return cmResult{done: true}
	}
	if r.err != nil || r.done {
		g.closed = true
	}
	return r
}

// run 生成器主循环（在独立 goroutine 中执行函数体）
func (g *cmGen) run() {
	ip := g.ip
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *PyError:
				g.out <- cmResult{err: e}
			case *ctrlSignal:
				if e.kind == ctrlReturn {
					g.out <- cmResult{done: true, ret: e.val}
				} else {
					g.out <- cmResult{err: &PyError{Exc: object.NewException(ip.excType("RuntimeError"),
						&object.Str{V: "generator raised StopIteration"})}}
				}
			default:
				panic(r)
			}
		}
		close(g.out)
	}()

	env := ip.bindArgs(g.f, g.args, g.kwargs)
	prevEnv := ip.currentEnv
	ip.currentEnv = env
	ip.genStack = append(ip.genStack, &genCtx{yield: func(v object.Object) object.Object {
		g.out <- cmResult{val: v}
		act := <-g.in
		if act.throw != nil {
			// 在 yield 处抛入异常：panic 沿用户 try/except/finally 展开
			panic(&PyError{Exc: act.throw, Frames: append([]string{}, ip.Frames...), File: ip.MainFile})
		}
		return act.val
	}})
	defer func() {
		ip.currentEnv = prevEnv
		if n := len(ip.genStack); n > 0 {
			ip.genStack = ip.genStack[:n-1]
		}
	}()

	if g.f.LambdaBody != nil {
		ip.evalExpr(g.f.LambdaBody, env)
		return
	}
	ip.execBlock(g.f.Body, env)
}

// cmGenOf 从上下文管理器实例取出驱动器
func cmGenOf(inst *object.Instance) *cmGen {
	g, _ := inst.Dict["_gen"].(*cmGen)
	return g
}

// cmEnter 执行 _GeneratorContextManager.__enter__
func (ip *Interp) cmEnter(inst *object.Instance) object.Object {
	g := cmGenOf(inst)
	if g == nil {
		ip.RuntimeErr("context manager is not initialized")
	}
	r := g.step(cmAction{val: object.None})
	if r.err != nil {
		if pe, ok := r.err.(*PyError); ok {
			panic(pe)
		}
		ip.RuntimeErr("%v", r.err)
	}
	if r.done {
		ip.RuntimeErr("generator didn't yield")
	}
	return r.val
}

// cmExit 执行 _GeneratorContextManager.__exit__
func (ip *Interp) cmExit(inst *object.Instance, et, ev object.Object) object.Object {
	g := cmGenOf(inst)
	if g == nil {
		ip.RuntimeErr("context manager is not initialized")
	}
	// 无异常：把生成器推进到结束
	if et == nil || et == object.None {
		r := g.step(cmAction{val: object.None})
		if r.err != nil {
			if pe, ok := r.err.(*PyError); ok {
				panic(pe)
			}
			ip.RuntimeErr("%v", r.err)
		}
		if !r.done {
			ip.RuntimeErr("generator didn't stop")
		}
		return object.FalseObj
	}
	// 有异常：在 yield 处抛入
	var exc *object.Exception
	if e, ok := ev.(*object.Exception); ok {
		exc = e
	} else if t, ok := et.(*object.TypeObj); ok {
		exc = object.NewException(t)
	} else {
		exc = object.NewException(ip.excType("RuntimeError"))
	}
	r := g.step(cmAction{throw: exc})
	if r.done {
		// 生成器吞掉异常后正常结束（等价 StopIteration）→ 抑制
		return object.TrueObj
	}
	if r.err != nil {
		pe, ok := r.err.(*PyError)
		if !ok {
			ip.RuntimeErr("%v", r.err)
		}
		if pe.Exc == exc {
			return object.FalseObj
		}
		panic(pe)
	}
	ip.RuntimeErr("generator didn't stop after throw()")
	return object.FalseObj
}

// ==================== contextlib 模块 ====================

func modContextlib(ip *Interp) *object.Module {
	m := newModule("contextlib")

	// ---------------- contextmanager ----------------
	cmCls := &object.TypeObj{Name: "contextmanager", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	cmCls.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return ip.cmEnter(a[0].(*object.Instance)), nil
	})
	cmCls.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		var et, ev object.Object = object.None, object.None
		if len(a) > 1 {
			et = a[1]
		}
		if len(a) > 2 {
			ev = a[2]
		}
		return ip.cmExit(inst, et, ev), nil
	})

	m.Dict["contextmanager"] = bi("contextmanager", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 1 {
			ip.TypeError("contextmanager() missing 1 required positional argument: 'func'")
		}
		fn := a[0]
		wrapper := bi("helper", func(ca []object.Object, ck map[string]object.Object) (object.Object, error) {
			f, ok := fn.(*object.Function)
			if !ok {
				ip.TypeError("'%s' object is not a generator function", object.TypeName(fn))
			}
			inst := &object.Instance{Class: cmCls, Dict: map[string]object.Object{}}
			inst.Dict["_gen"] = &cmGen{
				ip:     ip,
				f:      f,
				args:   ca,
				kwargs: ck,
				in:     make(chan cmAction, 1),
				out:    make(chan cmResult, 1),
			}
			return inst, nil
		})
		return wrapper, nil
	})

	// ---------------- ExitStack ----------------
	exitCls := &object.TypeObj{Name: "ExitStack", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	newExitStack := func(items []object.Object) *object.Instance {
		inst := &object.Instance{Class: exitCls, Dict: map[string]object.Object{}}
		inst.Dict["_exits"] = &object.List{Items: items}
		return inst
	}
	exitCls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if _, ok := inst.Dict["_exits"]; !ok {
			inst.Dict["_exits"] = &object.List{}
		}
		return object.None, nil
	})
	exitCls.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return a[0], nil
	})
	exitCls.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		ip.exitStackClose(a[0].(*object.Instance))
		return object.FalseObj, nil
	})
	exitCls.Dict["enter_context"] = bi("enter_context", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if len(a) < 2 {
			ip.TypeError("enter_context() missing 1 required positional argument: 'cm'")
		}
		cm := a[1]
		ex, ok := ip.lookupSpecialMethod(cm, "__exit__")
		if !ok {
			ip.TypeError("'%s' object does not support the context manager protocol", object.TypeName(cm))
		}
		en, ok := ip.lookupSpecialMethod(cm, "__enter__")
		if !ok {
			ip.TypeError("'%s' object does not support the context manager protocol", object.TypeName(cm))
		}
		ip.exitStackPush(inst, ex)
		return ip.Call(en, nil, nil), nil
	})
	exitCls.Dict["push"] = bi("push", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if len(a) < 2 {
			ip.TypeError("push() missing 1 required positional argument: 'exit'")
		}
		ex := a[1]
		ip.exitStackPush(inst, ex)
		return ex, nil
	})
	exitCls.Dict["callback"] = bi("callback", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if len(a) < 2 {
			ip.TypeError("callback() missing 1 required positional argument: 'callback'")
		}
		cb := a[1]
		extra := append([]object.Object{}, a[2:]...)
		kw := k
		ip.exitStackPush(inst, bi("_callback", func(ca []object.Object, ck map[string]object.Object) (object.Object, error) {
			args := append([]object.Object{}, extra...)
			return ip.Call(cb, args, kw), nil
		}))
		return cb, nil
	})
	exitCls.Dict["pop_all"] = bi("pop_all", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		lst, _ := inst.Dict["_exits"].(*object.List)
		var items []object.Object
		if lst != nil {
			items = lst.Items
			lst.Items = nil
		}
		return newExitStack(items), nil
	})
	exitCls.Dict["close"] = bi("close", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		ip.exitStackClose(a[0].(*object.Instance))
		return object.None, nil
	})
	m.Dict["ExitStack"] = exitCls

	// ---------------- suppress ----------------
	supCls := &object.TypeObj{Name: "suppress", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	supCls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		items := []object.Object{}
		if len(a) > 1 {
			items = append(items, a[1:]...)
		}
		inst.Dict["_types"] = &object.Tuple{Items: items}
		return object.None, nil
	})
	supCls.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return a[0], nil
	})
	supCls.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		et := object.None
		ev := object.None
		if len(a) > 1 {
			et = a[1]
		}
		if len(a) > 2 {
			ev = a[2]
		}
		if et == nil || et == object.None {
			return object.FalseObj, nil
		}
		exc, ok := ev.(*object.Exception)
		if !ok {
			if t, isT := et.(*object.TypeObj); isT {
				exc = object.NewException(t)
			} else {
				return object.FalseObj, nil
			}
		}
		types, _ := inst.Dict["_types"].(*object.Tuple)
		if types != nil {
			for _, t := range types.Items {
				if ip.excTypeMatches(exc, t) {
					return object.TrueObj, nil
				}
			}
		}
		return object.FalseObj, nil
	})
	m.Dict["suppress"] = supCls

	// ---------------- closing ----------------
	closeCls := &object.TypeObj{Name: "closing", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	closeCls.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return a[0].(*object.Instance).Dict["_thing"], nil
	})
	closeCls.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		thing := inst.Dict["_thing"]
		ip.Call(ip.GetAttr(thing, "close"), nil, nil)
		return object.FalseObj, nil
	})
	m.Dict["closing"] = bi("closing", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 1 {
			ip.TypeError("closing() missing 1 required positional argument: 'thing'")
		}
		inst := &object.Instance{Class: closeCls, Dict: map[string]object.Object{}}
		inst.Dict["_thing"] = a[0]
		return inst, nil
	})

	// ---------------- nullcontext ----------------
	nullCls := &object.TypeObj{Name: "nullcontext", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	nullCls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		var ret object.Object = object.None
		if len(a) > 1 {
			ret = a[1]
		}
		inst.Dict["_ret"] = ret
		var ex object.Object = object.FalseObj
		if v, ok := k["exit_result"]; ok {
			ex = v
		}
		inst.Dict["_exit"] = ex
		return object.None, nil
	})
	nullCls.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if v, ok := inst.Dict["_ret"]; ok {
			return v, nil
		}
		return object.None, nil
	})
	nullCls.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if v, ok := inst.Dict["_exit"]; ok {
			return v, nil
		}
		return object.FalseObj, nil
	})
	m.Dict["nullcontext"] = nullCls

	return m
}

// exitStackPush 压入一个 exit 可调用对象
func (ip *Interp) exitStackPush(inst *object.Instance, ex object.Object) {
	lst, ok := inst.Dict["_exits"].(*object.List)
	if !ok {
		lst = &object.List{}
		inst.Dict["_exits"] = lst
	}
	lst.Items = append(lst.Items, ex)
}

// exitStackClose 逆序执行所有 exit，回调收到 (None, None, None)
func (ip *Interp) exitStackClose(inst *object.Instance) {
	lst, ok := inst.Dict["_exits"].(*object.List)
	if !ok || lst == nil {
		return
	}
	for len(lst.Items) > 0 {
		ex := lst.Items[len(lst.Items)-1]
		lst.Items = lst.Items[:len(lst.Items)-1]
		ip.Call(ex, []object.Object{object.None, object.None, object.None}, nil)
	}
}

// excTypeMatches 判断异常是否匹配给定类型（支持元组嵌套）
func (ip *Interp) excTypeMatches(exc *object.Exception, t object.Object) bool {
	switch v := t.(type) {
	case *object.TypeObj:
		return exc.Class.IsSubclass(v)
	case *object.Tuple:
		for _, it := range v.Items {
			if ip.excTypeMatches(exc, it) {
				return true
			}
		}
	}
	return false
}
