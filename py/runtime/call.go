package runtime

import (
	"zuzo/py/ast"
	"zuzo/py/object"
)

// genResult 生成器产出
type genResult struct {
	val  object.Object
	done bool
	err  error
	ret  object.Object
}

// genCtx 生成器上下文
type genCtx struct {
	yield func(object.Object) object.Object
}

// Call 调用任意可调用对象
func (ip *Interp) Call(fn object.Object, args []object.Object, kwargs map[string]object.Object) object.Object {
	switch f := fn.(type) {
	case *object.Function:
		return ip.callFunction(f, args, kwargs)
	case *object.Builtin:
		callArgs := args
		if f.Self != nil {
			callArgs = append([]object.Object{f.Self}, args...)
		}
		res, err := f.Fn(callArgs, kwargs)
		if err != nil {
			if pe, ok := err.(*PyError); ok {
				panic(pe)
			}
			ip.RuntimeErr("%v", err)
		}
		if res == nil {
			return object.None
		}
		return res
	case *object.BoundMethod:
		return ip.Call(f.Fn, append([]object.Object{f.Self}, args...), kwargs)
	case *object.TypeObj:
		return ip.instantiate(f, args, kwargs)
	case *object.Instance:
		if m, ok := ip.findDunder(f.Class, "__call__"); ok {
			return ip.Call(ip.bindMethod(f, m), args, kwargs)
		}
		ip.TypeError("'%s' object is not callable", f.Class.Name)
	case *object.Exception:
		if m, ok := ip.findDunder(f.Class, "__call__"); ok {
			return ip.Call(ip.bindMethod(f, m), args, kwargs)
		}
		ip.TypeError("'%s' object is not callable", f.Class.Name)
	case *object.Property, *object.StaticMethod, *object.ClassMethod:
		ip.TypeError("'%s' object is not callable", object.TypeName(fn))
	}
	ip.TypeError("'%s' object is not callable", object.TypeName(fn))
	return object.None
}

// instantiate 实例化类型
func (ip *Interp) instantiate(cls *object.TypeObj, args []object.Object, kwargs map[string]object.Object) object.Object {
	if cls.IsExc {
		exc := object.NewException(cls, args...)
		// 执行用户自定义 __init__（基类 Exception 没有）
		if init, ok := cls.Dict["__init__"]; ok {
			if fn, isFn := init.(*object.Function); isFn {
				ip.Call(ip.bindMethod(exc, fn), args, kwargs)
			}
		}
		return exc
	}
	if cls.Builtin != 0 {
		return ip.constructBuiltinType(cls, args, kwargs)
	}
	// 用户类
	inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
	if init, ok := cls.Lookup("__init__"); ok {
		ip.Call(ip.bindMethod(inst, init), args, kwargs)
	} else if len(args) > 0 || len(kwargs) > 0 {
		ip.TypeError("%s() takes no arguments", cls.Name)
	}
	return inst
}

// callFunction 调用用户函数
func (ip *Interp) callFunction(f *object.Function, args []object.Object, kwargs map[string]object.Object) object.Object {
	if f.IsGenerator || f.IsAsync {
		return ip.newGenerator(f.Name, func(yield func(object.Object) object.Object) {
			env := ip.bindArgs(f, args, kwargs)
			ip.genStack = append(ip.genStack, &genCtx{yield: yield})
			defer func() {
				if len(ip.genStack) > 0 {
					ip.genStack = ip.genStack[:len(ip.genStack)-1]
				}
			}()
			if f.LambdaBody != nil {
				yield(ip.evalExpr(f.LambdaBody, env))
				return
			}
			ip.execBlock(f.Body, env)
		})
	}
	env := ip.bindArgs(f, args, kwargs)
	prevEnv := ip.currentEnv
	ip.currentEnv = env
	defer func() { ip.currentEnv = prevEnv }()
	if f.LambdaBody != nil {
		return ip.evalExpr(f.LambdaBody, env)
	}
	ip.Depth++
	if ip.Depth > ip.MaxDepth {
		ip.Depth--
		ip.raise(ip.excType("RecursionError"), "maximum recursion depth exceeded")
	}
	ip.Frames = append(ip.Frames, "  File \""+ip.MainFile+"\", in "+f.Name)
	defer func() {
		ip.Depth--
		if len(ip.Frames) > 0 {
			ip.Frames = ip.Frames[:len(ip.Frames)-1]
		}
	}()
	var result object.Object = object.None
	func() {
		defer func() {
			if r := recover(); r != nil {
				if sig, ok := r.(*ctrlSignal); ok {
					if sig.kind == ctrlReturn {
						result = sig.val
						return
					}
					// break/continue 泄漏出函数：忽略
					result = object.None
					return
				}
				panic(r)
			}
		}()
		ip.execBlock(f.Body, env)
	}()
	return result
}

// bindArgs 绑定参数到新环境
func (ip *Interp) bindArgs(f *object.Function, args []object.Object, kwargs map[string]object.Object) *object.Env {
	env := object.NewEnv(f.Closure)
	if f.Globals != nil {
		env.Globals = f.Globals
	}
	params := f.Params
	if params == nil {
		return env
	}
	nPos := len(params.Args)
	if len(args) > nPos && params.Vararg == "" {
		ip.TypeError("%s() takes %d positional argument%s but %d %s given",
			f.Name, nPos, plural(nPos), len(args), wasWere(len(args)))
	}
	used := map[string]bool{}
	for i, a := range params.Args {
		if i < len(args) {
			env.Set(a.Name, args[i])
			used[a.Name] = true
			continue
		}
		if v, ok := kwargs[a.Name]; ok {
			env.Set(a.Name, v)
			used[a.Name] = true
			continue
		}
		if f.ArgDefaults != nil && i < len(f.ArgDefaults) && f.ArgDefaults[i] != nil {
			env.Set(a.Name, f.ArgDefaults[i])
			continue
		}
		if f.Defaults != nil && i >= nPos-len(f.Defaults) {
			env.Set(a.Name, f.Defaults[i-(nPos-len(f.Defaults))])
			continue
		}
		ip.TypeError("%s() missing required positional argument: '%s'", f.Name, a.Name)
	}
	if params.Vararg != "" {
		rest := []object.Object{}
		if len(args) > nPos {
			rest = append(rest, args[nPos:]...)
		}
		env.Set(params.Vararg, &object.Tuple{Items: rest})
	}
	// 仅关键字参数
	for _, a := range params.Kwonly {
		if v, ok := kwargs[a.Name]; ok {
			env.Set(a.Name, v)
			used[a.Name] = true
			continue
		}
		if f.KwDefaults != nil {
			if v, ok := f.KwDefaults[a.Name]; ok {
				env.Set(a.Name, v)
				continue
			}
		}
		ip.TypeError("%s() missing required keyword-only argument: '%s'", f.Name, a.Name)
	}
	// 多余关键字
	extra := map[string]object.Object{}
	for k, v := range kwargs {
		if used[k] {
			continue
		}
		known := false
		for _, a := range params.Args {
			if a.Name == k {
				known = true
				break
			}
		}
		for _, a := range params.Kwonly {
			if a.Name == k {
				known = true
				break
			}
		}
		if !known {
			extra[k] = v
		}
	}
	if params.Kwarg != "" {
		d := &object.Dict{}
		for k, v := range extra {
			d.Set(&object.Str{V: k}, v)
		}
		env.Set(params.Kwarg, d)
	} else if len(extra) > 0 {
		for k := range extra {
			ip.TypeError("%s() got an unexpected keyword argument '%s'", f.Name, k)
		}
	}
	return env
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func wasWere(n int) string {
	if n == 1 {
		return "was"
	}
	return "were"
}

// ==================== 生成器 ====================

// newGenerator 创建生成器（goroutine + channel 实现暂停/恢复）
func (ip *Interp) newGenerator(name string, run func(yield func(object.Object) object.Object)) *object.Generator {
	in := make(chan object.Object)
	out := make(chan genResult, 1)
	g := &object.Generator{Name: name}
	started := false
	closed := false
	g.Next = func() (object.Object, bool, error) {
		if closed {
			return nil, true, nil
		}
		if !started {
			started = true
			go func() {
				defer func() {
					if r := recover(); r != nil {
						if pe, ok := r.(*PyError); ok {
							select {
							case out <- genResult{err: pe}:
							default:
							}
						} else if sig, ok := r.(*ctrlSignal); ok {
							if sig.kind == ctrlReturn {
								// return 结束生成器，携带返回值
								select {
								case out <- genResult{ret: sig.val, done: true}:
								default:
								}
							} else {
								select {
								case out <- genResult{err: &PyError{Exc: object.NewException(ip.excType("RuntimeError"),
									&object.Str{V: "generator raised StopIteration"})}}:
								default:
								}
							}
						} else {
							panic(r)
						}
					}
					close(out)
				}()
				run(func(v object.Object) object.Object {
					out <- genResult{val: v}
					return <-in
				})
			}()
		} else {
			in <- nil
		}
		r, ok := <-out
		if !ok {
			closed = true
			return nil, true, nil
		}
		if r.err != nil {
			closed = true
			return nil, true, r.err
		}
		if r.done {
			closed = true
			g.RetVal = r.ret
			return nil, true, nil
		}
		return r.val, false, nil
	}
	g.Send = func(v object.Object) (object.Object, bool, error) {
		if closed {
			return nil, true, nil
		}
		if !started {
			started = true
			go func() {
				defer func() {
					if r := recover(); r != nil {
						if pe, ok := r.(*PyError); ok {
							out <- genResult{err: pe}
						}
					}
					close(out)
				}()
				run(func(vv object.Object) object.Object {
					out <- genResult{val: vv}
					return <-in
				})
			}()
			r, ok := <-out
			if !ok {
				closed = true
				return nil, true, nil
			}
			if r.err != nil {
				closed = true
				return nil, true, r.err
			}
			return r.val, false, nil
		}
		in <- v
		r, ok := <-out
		if !ok {
			closed = true
			return nil, true, nil
		}
		if r.err != nil {
			closed = true
			return nil, true, r.err
		}
		if r.done {
			closed = true
			g.RetVal = r.ret
			return nil, true, nil
		}
		return r.val, false, nil
	}
	g.Close = func() { closed = true }
	return g
}

// awaitValue 驱动协程/生成器到完成并返回其返回值
func (ip *Interp) awaitValue(v object.Object) object.Object {
	switch g := v.(type) {
	case *object.Task:
		<-g.Done
		if g.Err != nil {
			if pe, ok := g.Err.(*PyError); ok {
				panic(pe)
			}
			ip.RuntimeErr("%v", g.Err)
		}
		return g.Result
	case *object.PyThread:
		<-g.Done
		return object.None
	case *object.Generator:
		var last object.Object = object.None
		for {
			val, done, err := g.Next()
			if err != nil {
				if pe, ok := err.(*PyError); ok {
					panic(pe)
				}
				ip.RuntimeErr("%v", err)
			}
			if done {
				if g.RetVal != nil {
					return g.RetVal
				}
				return last
			}
			last = val
		}
	case *object.Instance:
		// 自定义 awaitable：__await__ 返回迭代器
		if m, ok := ip.findDunder(g.Class, "__await__"); ok {
			it := ip.Call(ip.bindMethod(g, m), nil, nil)
			return ip.awaitValue(it)
		}
		return v
	}
	return v
}

// evalYield 求值 yield 表达式
func (ip *Interp) evalYield(n *ast.Yield, env *object.Env) object.Object {
	if len(ip.genStack) == 0 {
		ip.raise(ip.excType("SyntaxError"), "'yield' outside function")
	}
	g := ip.genStack[len(ip.genStack)-1]
	if n.From {
		src := ip.evalExpr(n.Value, env)
		it := ip.Iter(src)
		var last object.Object = object.None
		for {
			v, ok := ip.IterNext(it)
			if !ok {
				break
			}
			last = g.yield(v)
		}
		return last
	}
	var v object.Object = object.None
	if n.Value != nil {
		v = ip.evalExpr(n.Value, env)
	}
	return g.yield(v)
}
