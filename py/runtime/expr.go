package runtime

import (
	"fmt"
	"math/big"
	"strconv"
	"strings"

	"zuzo/py/ast"
	"zuzo/py/object"
)

// evalExpr 求值表达式
func (ip *Interp) evalExpr(e ast.Expr, env *object.Env) object.Object {
	if e == nil {
		return object.None
	}
	switch n := e.(type) {
	case *ast.Name:
		return ip.lookupName(n.Id, env)
	case *ast.Number:
		return ip.makeNumber(n)
	case *ast.String:
		if n.IsBytes {
			return &object.Bytes{V: []byte(n.Value)}
		}
		return &object.Str{V: n.Value}
	case *ast.FString:
		return ip.evalFString(n, env)
	case *ast.Ellipsis:
		return &object.Str{V: "Ellipsis"}
	case *ast.List:
		items := ip.evalElems(n.Elements, env)
		return &object.List{Items: items}
	case *ast.Tuple:
		items := ip.evalElems(n.Elements, env)
		return &object.Tuple{Items: items}
	case *ast.Set:
		items := ip.evalElems(n.Elements, env)
		s := &object.Set{}
		for _, it := range items {
			if _, ok := object.Hash(it); !ok {
				ip.TypeError("unhashable type: '%s'", object.TypeName(it))
			}
			s.Add(it)
		}
		return s
	case *ast.Dict:
		d := &object.Dict{}
		for i, k := range n.Keys {
			if k == nil {
				v := ip.evalExpr(n.Values[i], env)
				src, ok := v.(*object.Dict)
				if !ok {
					ip.TypeError("argument after ** must be a mapping")
				}
				for j, kk := range src.Keys {
					d.Set(kk, src.Vals[j])
				}
				continue
			}
			key := ip.evalExpr(k, env)
			val := ip.evalExpr(n.Values[i], env)
			if _, ok := object.Hash(key); !ok {
				ip.TypeError("unhashable type: '%s'", object.TypeName(key))
			}
			d.Set(key, val)
		}
		return d
	case *ast.BinOp:
		left := ip.evalExpr(n.Left, env)
		right := ip.evalExpr(n.Right, env)
		return ip.BinOp(n.Op, left, right)
	case *ast.UnaryOp:
		v := ip.evalExpr(n.Operand, env)
		return ip.UnaryOp(n.Op, v)
	case *ast.BoolOp:
		var last object.Object = object.None
		for _, sub := range n.Values {
			last = ip.evalExpr(sub, env)
			t := object.Truthy(last)
			if n.Op == "and" && !t {
				return last
			}
			if n.Op == "or" && t {
				return last
			}
		}
		return last
	case *ast.Compare:
		return ip.evalCompare(n, env)
	case *ast.Call:
		return ip.evalCall(n, env)
	case *ast.Attribute:
		obj := ip.evalExpr(n.Value, env)
		return ip.GetAttr(obj, n.Attr)
	case *ast.Subscript:
		obj := ip.evalExpr(n.Value, env)
		idx := ip.evalSubscript(obj, n.Slice, env)
		return ip.GetItem(obj, idx)
	case *ast.IfExp:
		if object.Truthy(ip.evalExpr(n.Test, env)) {
			return ip.evalExpr(n.Body, env)
		}
		return ip.evalExpr(n.Orelse, env)
	case *ast.Lambda:
		fn := &object.Function{
			Name:    "<lambda>",
			Params:  n.Params,
			Globals: ip.Globals,
			Closure: env,
			Body:    nil,
			Dict:    &object.Dict{},
		}
		fn.ArgDefaults = make([]object.Object, len(n.Params.Args))
		for i, a := range n.Params.Args {
			if a.Default != nil {
				fn.ArgDefaults[i] = ip.evalExpr(a.Default, env)
			}
		}
		fn.KwDefaults = map[string]object.Object{}
		for _, a := range n.Params.Kwonly {
			if a.Default != nil {
				fn.KwDefaults[a.Name] = ip.evalExpr(a.Default, env)
			}
		}
		// lambda 体是表达式：包成 return 语句
		fn.LambdaBody = n.Body
		return fn
	case *ast.ListComp:
		return ip.evalListComp(n, env)
	case *ast.SetComp:
		return ip.evalSetComp(n, env)
	case *ast.DictComp:
		return ip.evalDictComp(n, env)
	case *ast.GenExp:
		return ip.evalGenExp(n, env)
	case *ast.NamedExpr:
		v := ip.evalExpr(n.Value, env)
		ip.assignName(n.Target.Id, v, env)
		return v
	case *ast.Yield:
		return ip.evalYield(n, env)
	case *ast.Await:
		return ip.awaitValue(ip.evalExpr(n.Value, env))
	case *ast.Starred:
		return ip.evalExpr(n.Value, env)
	}
	ip.RuntimeErr("未支持的表达式类型 %T", e)
	return object.None
}

// lookupName 名字解析
func (ip *Interp) lookupName(name string, env *object.Env) object.Object {
	if v, ok := env.Get(name); ok {
		return v
	}
	if v, ok := ip.Builtins.Vars[name]; ok {
		return v
	}
	ip.NameErr(name)
	return object.None
}

func (ip *Interp) makeNumber(n *ast.Number) object.Object {
	// 字面量解析结果缓存：同一 AST 节点只会产出同一个不可变数值对象
	if v, ok := ip.numCache.Load(n); ok {
		return v.(object.Object)
	}
	s := strings.ReplaceAll(n.Value, "_", "")
	if n.IsFloat {
		f, err := strconv.ParseFloat(s, 64)
		if err != nil {
			ip.raise(ip.excType("SyntaxError"), "非法浮点数字面量 %q", n.Value)
		}
		return &object.Float{V: f}
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "0x") {
		v, err := strconv.ParseInt(s[2:], 16, 64)
		if err != nil {
			ip.raise(ip.excType("SyntaxError"), "非法十六进制字面量 %q", n.Value)
		}
		return &object.Int{V: v}
	}
	if strings.HasPrefix(lower, "0o") {
		v, err := strconv.ParseInt(s[2:], 8, 64)
		if err != nil {
			ip.raise(ip.excType("SyntaxError"), "非法八进制字面量 %q", n.Value)
		}
		return &object.Int{V: v}
	}
	if strings.HasPrefix(lower, "0b") {
		v, err := strconv.ParseInt(s[2:], 2, 64)
		if err != nil {
			ip.raise(ip.excType("SyntaxError"), "非法二进制字面量 %q", n.Value)
		}
		return &object.Int{V: v}
	}
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		if bb, ok := new(big.Int).SetString(s, 10); ok {
			return object.NewBigInt(bb) // BigInt 不缓存：避免共享可变大整数
		}
		f, ferr := strconv.ParseFloat(s, 64)
		if ferr == nil {
			out := &object.Float{V: f}
			ip.numCache.Store(n, out)
			return out
		}
		ip.raise(ip.excType("SyntaxError"), "非法数字字面量 %q", n.Value)
	}
	out := &object.Int{V: v}
	ip.numCache.Store(n, out)
	return out
}

// evalElems 求值元素列表（处理 * 解包）
func (ip *Interp) evalElems(exprs []ast.Expr, env *object.Env) []object.Object {
	var out []object.Object
	for _, e := range exprs {
		if st, ok := e.(*ast.Starred); ok {
			v := ip.evalExpr(st.Value, env)
			items := ip.ToSlice(v)
			out = append(out, items...)
			continue
		}
		out = append(out, ip.evalExpr(e, env))
	}
	return out
}

// evalFString 求值 f-string
func (ip *Interp) evalFString(n *ast.FString, env *object.Env) object.Object {
	var sb strings.Builder
	for _, p := range n.Parts {
		if p.IsLiteral {
			sb.WriteString(p.Literal)
			continue
		}
		v := ip.evalExpr(p.Expr, env)
		if p.Conversion != 0 {
			switch p.Conversion {
			case 'r':
				v = &object.Str{V: ip.Repr(v)}
			case 's':
				v = &object.Str{V: v.Str()}
			case 'a':
				v = &object.Str{V: ip.Repr(v)}
			}
		}
		if p.Format != "" {
			sb.WriteString(ip.FormatValue(v, p.Format))
		} else {
			sb.WriteString(v.Str())
		}
	}
	return &object.Str{V: sb.String()}
}

func (ip *Interp) evalCompare(n *ast.Compare, env *object.Env) object.Object {
	left := ip.evalExpr(n.Left, env)
	for i, op := range n.Ops {
		right := ip.evalExpr(n.Comparators[i], env)
		var ok bool
		switch op {
		case "==":
			ok = ip.Eq(left, right)
		case "!=":
			ok = !ip.Eq(left, right)
		case "is":
			ok = ip.IsIdentical(left, right)
		case "is not":
			ok = !ip.IsIdentical(left, right)
		case "in":
			ok = ip.Contains(right, left)
		case "not in":
			ok = !ip.Contains(right, left)
		case "<", ">", "<=", ">=":
			c, cmpOK := ip.Cmp(left, right)
			if !cmpOK {
				ip.TypeError("'%s' not supported between instances of '%s' and '%s'",
					op, object.TypeName(left), object.TypeName(right))
			}
			switch op {
			case "<":
				ok = c < 0
			case ">":
				ok = c > 0
			case "<=":
				ok = c <= 0
			case ">=":
				ok = c >= 0
			}
		}
		if !ok {
			return object.FalseObj
		}
		left = right
	}
	return object.TrueObj
}

func (ip *Interp) evalCall(n *ast.Call, env *object.Env) object.Object {
	fnVal := ip.evalExpr(n.Func, env)
	var args []object.Object
	for _, a := range n.Args {
		if st, ok := a.(*ast.Starred); ok {
			v := ip.evalExpr(st.Value, env)
			args = append(args, ip.ToSlice(v)...)
			continue
		}
		args = append(args, ip.evalExpr(a, env))
	}
	kwargs := map[string]object.Object{}
	order := make([]string, 0, len(n.Keywords))
	for _, kw := range n.Keywords {
		if kw.Name == "" {
			v := ip.evalExpr(kw.Value, env)
			d, ok := v.(*object.Dict)
			if !ok {
				ip.TypeError("argument after ** must be a mapping, not '%s'", object.TypeName(v))
			}
			for i, k := range d.Keys {
				ks, ok := k.(*object.Str)
				if !ok {
					ip.TypeError("keywords must be strings")
				}
				kwargs[ks.V] = d.Vals[i]
				order = append(order, ks.V)
			}
			continue
		}
		kwargs[kw.Name] = ip.evalExpr(kw.Value, env)
		order = append(order, kw.Name)
	}
	if kwargs != nil && len(kwargs) == 0 {
		kwargs = nil
	}
	ip.kwOrder = order
	return ip.Call(fnVal, args, kwargs)
}

// ==================== 推导式 ====================

// compScopes 依次进入各 for 子句作用域
func (ip *Interp) runComp(gens []ast.Comp, idx int, env *object.Env, body func(env *object.Env)) {
	if idx >= len(gens) {
		body(env)
		return
	}
	g := gens[idx]
	iterObj := ip.evalExpr(g.Iter, env)
	it := ip.Iter(iterObj)
	for {
		v, ok := ip.IterNext(it)
		if !ok {
			break
		}
		// 每轮迭代新建环境（Python 3 推导式作用域）
		// 首层复用推导式自身作用域，使海象运算符 := 的结果在各轮之间可见（CPython 语义）
		inner := env
		if idx > 0 {
			inner = object.NewEnv(env)
			inner.Globals = env.Globals
		}
		ip.assign(g.Target, v, inner)
		pass := true
		for _, cond := range g.Ifs {
			if !object.Truthy(ip.evalExpr(cond, inner)) {
				pass = false
				break
			}
		}
		if !pass {
			continue
		}
		ip.runComp(gens, idx+1, inner, body)
	}
}

func (ip *Interp) evalListComp(n *ast.ListComp, env *object.Env) object.Object {
	out := &object.List{}
	scope := object.NewEnv(env)
	scope.Globals = env.Globals
	var wal *object.Env
	ip.runComp(n.Gens, 0, scope, func(e *object.Env) {
		wal = e
		out.Items = append(out.Items, ip.evalExpr(n.Elt, e))
	})
	ip.compWalrus(n.Gens, wal, env)
	return out
}

func (ip *Interp) evalSetComp(n *ast.SetComp, env *object.Env) object.Object {
	out := &object.Set{}
	scope := object.NewEnv(env)
	scope.Globals = env.Globals
	var wal *object.Env
	ip.runComp(n.Gens, 0, scope, func(e *object.Env) {
		wal = e
		v := ip.evalExpr(n.Elt, e)
		if _, ok := object.Hash(v); !ok {
			ip.TypeError("unhashable type: '%s'", object.TypeName(v))
		}
		out.Add(v)
	})
	ip.compWalrus(n.Gens, wal, env)
	return out
}

func (ip *Interp) evalDictComp(n *ast.DictComp, env *object.Env) object.Object {
	out := &object.Dict{}
	scope := object.NewEnv(env)
	scope.Globals = env.Globals
	var wal *object.Env
	ip.runComp(n.Gens, 0, scope, func(e *object.Env) {
		wal = e
		k := ip.evalExpr(n.Key, e)
		if _, ok := object.Hash(k); !ok {
			ip.TypeError("unhashable type: '%s'", object.TypeName(k))
		}
		out.Set(k, ip.evalExpr(n.Value, e))
	})
	ip.compWalrus(n.Gens, wal, env)
	return out
}

func (ip *Interp) evalGenExp(n *ast.GenExp, env *object.Env) object.Object {
	scope := object.NewEnv(env)
	scope.Globals = env.Globals
	return ip.newGenerator("<genexpr>", func(yield func(object.Object) object.Object) {
		ip.runComp(n.Gens, 0, scope, func(e *object.Env) {
			yield(ip.evalExpr(n.Elt, e))
		})
	})
}

// ==================== 函数 / 类构造 ====================

func (ip *Interp) makeFunction(node *ast.FuncDef, env *object.Env) object.Object {
	fn := &object.Function{
		Name:     node.Name,
		QualName: node.Name,
		Params:   node.Params,
		Body:     node.Body,
		Globals:  ip.Globals,
		Closure:  env,
		IsAsync:  node.IsAsync,
		Dict:     &object.Dict{},
	}
	fn.ArgDefaults = make([]object.Object, len(node.Params.Args))
	for i, a := range node.Params.Args {
		if a.Default != nil {
			fn.ArgDefaults[i] = ip.evalExpr(a.Default, env)
		}
	}
	fn.KwDefaults = map[string]object.Object{}
	for _, a := range node.Params.Kwonly {
		if a.Default != nil {
			fn.KwDefaults[a.Name] = ip.evalExpr(a.Default, env)
		}
	}
	fn.IsGenerator = containsYield(node.Body)
	fn.Doc = extractDoc(node.Body)
	var result object.Object = fn
	for i := len(node.Decorators) - 1; i >= 0; i-- {
		dec := ip.evalExpr(node.Decorators[i], env)
		result = ip.Call(dec, []object.Object{result}, nil)
	}
	return result
}

func (ip *Interp) makeClass(node *ast.ClassDef, env *object.Env) *object.TypeObj {
	cls := &object.TypeObj{Name: node.Name, Dict: map[string]object.Object{}}
	for _, b := range node.Bases {
		bv := ip.evalExpr(b, env)
		if bt, ok := bv.(*object.TypeObj); ok {
			cls.Bases = append(cls.Bases, bt)
			if bt.IsExc {
				cls.IsExc = true
			}
		} else {
			ip.TypeError("class bases must be types")
		}
	}
	if len(cls.Bases) == 0 {
		if cls.IsExc {
			cls.Bases = []*object.TypeObj{ip.excType("Exception")}
		} else {
			cls.Bases = []*object.TypeObj{object.ObjectType}
		}
	}
	// 类体在独立作用域执行
	classEnv := object.NewEnv(env)
	classEnv.Globals = env.Globals
	classEnv.IsClass = true
	classEnv.CurrentClass = cls
	ip.execBlock(node.Body, classEnv)
	for k, v := range classEnv.Vars {
		cls.Dict[k] = v
	}
	if len(classEnv.ClassAnnotations) > 0 {
		ann := &object.Dict{}
		for _, name := range classEnv.ClassAnnotations {
			ann.Set(&object.Str{V: name}, &object.Str{V: "annotation"})
		}
		cls.Dict["__annotations__"] = ann
	}
	// 元类：class X(metaclass=M) → 调用 M.__new__(M, name, bases, ns)
	var mcls *object.TypeObj
	for _, kw := range node.Keywords {
		if kw.Name == "metaclass" {
			if t, ok := ip.evalExpr(kw.Value, env).(*object.TypeObj); ok {
				mcls = t
			}
		}
	}
	if mcls != nil && mcls != object.TypeType {
		ns := &object.Dict{}
		for k, v := range cls.Dict {
			ns.Set(&object.Str{V: k}, v)
		}
		var basesItems []object.Object
		for _, b := range cls.Bases {
			basesItems = append(basesItems, b)
		}
		newFn := ip.GetAttr(mcls, "__new__")
		res := ip.Call(newFn, []object.Object{mcls, &object.Str{V: node.Name}, &object.Tuple{Items: basesItems}, ns}, nil)
		if t, ok := res.(*object.TypeObj); ok {
			cls = t
		}
	}
	// 类名绑定由调用方完成；装饰器在此应用
	var result object.Object = cls
	for i := len(node.Decorators) - 1; i >= 0; i-- {
		dec := ip.evalExpr(node.Decorators[i], env)
		result = ip.Call(dec, []object.Object{result}, nil)
	}
	if rt, ok := result.(*object.TypeObj); ok {
		return rt
	}
	// 装饰器返回非类对象：处理为包装（简化：忽略包装）
	return cls
}

// containsYield 检测函数体内是否有 yield
func containsYield(stmts []ast.Stmt) bool {
	found := false
	var walkExpr func(e ast.Expr)
	walkExpr = func(e ast.Expr) {
		if e == nil || found {
			return
		}
		switch n := e.(type) {
		case *ast.Yield:
			found = true
		case *ast.BinOp:
			walkExpr(n.Left)
			walkExpr(n.Right)
		case *ast.UnaryOp:
			walkExpr(n.Operand)
		case *ast.BoolOp:
			for _, v := range n.Values {
				walkExpr(v)
			}
		case *ast.Compare:
			walkExpr(n.Left)
			for _, c := range n.Comparators {
				walkExpr(c)
			}
		case *ast.Call:
			walkExpr(n.Func)
			for _, a := range n.Args {
				walkExpr(a)
			}
			for _, k := range n.Keywords {
				walkExpr(k.Value)
			}
		case *ast.Attribute:
			walkExpr(n.Value)
		case *ast.Subscript:
			walkExpr(n.Value)
			walkExpr(n.Slice)
		case *ast.List:
			for _, it := range n.Elements {
				walkExpr(it)
			}
		case *ast.Tuple:
			for _, it := range n.Elements {
				walkExpr(it)
			}
		case *ast.Set:
			for _, it := range n.Elements {
				walkExpr(it)
			}
		case *ast.Dict:
			for _, k := range n.Keys {
				walkExpr(k)
			}
			for _, v := range n.Values {
				walkExpr(v)
			}
		case *ast.IfExp:
			walkExpr(n.Test)
			walkExpr(n.Body)
			walkExpr(n.Orelse)
		case *ast.Starred:
			walkExpr(n.Value)
		case *ast.NamedExpr:
			walkExpr(n.Value)
		case *ast.Await:
			walkExpr(n.Value)
		case *ast.FString:
			for _, p := range n.Parts {
				if p.Expr != nil {
					walkExpr(p.Expr)
				}
			}
		case *ast.ListComp:
			walkExpr(n.Elt)
		case *ast.SetComp:
			walkExpr(n.Elt)
		case *ast.DictComp:
			walkExpr(n.Key)
			walkExpr(n.Value)
		case *ast.GenExp:
			walkExpr(n.Elt)
		}
	}
	var walkStmt func(s ast.Stmt)
	walkStmt = func(s ast.Stmt) {
		if s == nil || found {
			return
		}
		switch n := s.(type) {
		case *ast.ExprStmt:
			walkExpr(n.X)
		case *ast.Assign:
			walkExpr(n.Value)
		case *ast.AugAssign:
			walkExpr(n.Value)
		case *ast.AnnAssign:
			walkExpr(n.Value)
		case *ast.If:
			walkExpr(n.Cond)
			for _, b := range n.Body {
				walkStmt(b)
			}
			for _, b := range n.Orelse {
				walkStmt(b)
			}
		case *ast.While:
			walkExpr(n.Cond)
			for _, b := range n.Body {
				walkStmt(b)
			}
			for _, b := range n.Orelse {
				walkStmt(b)
			}
		case *ast.For:
			walkExpr(n.Iter)
			for _, b := range n.Body {
				walkStmt(b)
			}
			for _, b := range n.Orelse {
				walkStmt(b)
			}
		case *ast.Match:
			walkExpr(n.Subject)
			for _, c := range n.Cases {
				if c == nil {
					continue
				}
				walkExpr(c.Guard)
				for _, b := range c.Body {
					walkStmt(b)
				}
			}
		case *ast.Return:
			walkExpr(n.Value)
		case *ast.Try:
			for _, b := range n.Body {
				walkStmt(b)
			}
			for _, h := range n.Handlers {
				if h == nil {
					continue
				}
				walkExpr(h.Type)
				for _, b := range h.Body {
					walkStmt(b)
				}
			}
			for _, b := range n.Orelse {
				walkStmt(b)
			}
			for _, b := range n.Finally {
				walkStmt(b)
			}
		case *ast.With:
			for _, it := range n.Items {
				walkExpr(it.Context)
			}
			for _, b := range n.Body {
				walkStmt(b)
			}
		case *ast.Raise:
			walkExpr(n.Exc)
			walkExpr(n.Cause)
		case *ast.Assert:
			walkExpr(n.Test)
			walkExpr(n.Msg)
		case *ast.Del:
			for _, t := range n.Targets {
				walkExpr(t)
			}
		}
	}
	for _, s := range stmts {
		walkStmt(s)
	}
	return found
}

func extractDoc(stmts []ast.Stmt) string {
	if len(stmts) > 0 {
		if es, ok := stmts[0].(*ast.ExprStmt); ok {
			if s, ok := es.X.(*ast.String); ok {
				return s.Value
			}
		}
	}
	return ""
}

var _ = fmt.Sprintf
