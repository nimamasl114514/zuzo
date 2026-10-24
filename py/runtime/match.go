package runtime

import (
	"zuzo/py/ast"
	"zuzo/py/object"
)

// execMatch 执行 match 语句
func (ip *Interp) execMatch(node *ast.Match, env *object.Env) {
	subject := ip.evalExpr(node.Subject, env)
	for _, c := range node.Cases {
		bindings := map[string]object.Object{}
		if !ip.matchPattern(c.Pattern, subject, bindings, env) {
			continue
		}
		inner := object.NewEnv(env)
		inner.Globals = env.Globals
		for k, v := range bindings {
			inner.Set(k, v)
		}
		if c.Guard != nil && !object.Truthy(ip.evalExpr(c.Guard, inner)) {
			continue
		}
		ip.execBlock(c.Body, inner)
		return
	}
}

// isMatchSelfType 判断是否为「单值自匹配」内置类型
func isMatchSelfType(cls *object.TypeObj) bool {
	switch cls.Builtin {
	case object.BT_Int, object.BT_Str, object.BT_Float, object.BT_Bool,
		object.BT_List, object.BT_Dict, object.BT_Set, object.BT_Tuple,
		object.BT_Bytes, object.BT_Range, object.BT_Slice:
		return true
	}
	return false
}

// matchPattern 判断值是否匹配模式，成功时写入 bindings
func (ip *Interp) matchPattern(pat ast.Pattern, v object.Object, bindings map[string]object.Object, env *object.Env) bool {
	switch p := pat.(type) {
	case *ast.PWildcard:
		return true
	case *ast.PCapture:
		bindings[p.Name] = v
		return true
	case *ast.PLiteral:
		want := ip.evalExpr(p.Expr, env)
		return ip.Eq(want, v)
	case *ast.PValue:
		want := ip.evalExpr(p.Expr, env)
		return ip.Eq(want, v)
	case *ast.PAs:
		if !ip.matchPattern(p.Pat, v, bindings, env) {
			return false
		}
		bindings[p.Name] = v
		return true
	case *ast.POr:
		for _, alt := range p.Alts {
			local := map[string]object.Object{}
			if ip.matchPattern(alt, v, local, env) {
				for k, val := range local {
					bindings[k] = val
				}
				return true
			}
		}
		return false
	case *ast.PSequence:
		var items []object.Object
		switch seq := v.(type) {
		case *object.List:
			items = seq.Items
		case *object.Tuple:
			items = seq.Items
		default:
			return false
		}
		if p.Star == "" {
			if len(items) != len(p.Items) {
				return false
			}
			for i, sub := range p.Items {
				if !ip.matchPattern(sub, items[i], bindings, env) {
					return false
				}
			}
			return true
		}
		if len(items) < len(p.Items) {
			return false
		}
		before := len(p.Items)
		for i := 0; i < before; i++ {
			if !ip.matchPattern(p.Items[i], items[i], bindings, env) {
				return false
			}
		}
		rest := items[before:]
		bindings[p.Star] = &object.List{Items: rest}
		return true
	case *ast.PMapping:
		d, ok := v.(*object.Dict)
		if !ok {
			return false
		}
		for i, kexpr := range p.Keys {
			key := ip.evalExpr(kexpr, env)
			val, found := d.Get(key)
			if !found {
				return false
			}
			if !ip.matchPattern(p.Vals[i], val, bindings, env) {
				return false
			}
		}
		if p.Rest != "" {
			used := map[string]bool{}
			for _, kexpr := range p.Keys {
				used[ip.Repr(ip.evalExpr(kexpr, env))] = true
			}
			rest := &object.Dict{}
			for i, k := range d.Keys {
				if !used[ip.Repr(k)] {
					rest.Set(k, d.Vals[i])
				}
			}
			bindings[p.Rest] = rest
		}
		return true
	case *ast.PClass:
		clsVal := ip.evalExpr(p.Cls, env)
		cls, ok := clsVal.(*object.TypeObj)
		if !ok {
			return false
		}
		if !v.Type().IsSubclass(cls) {
			return false
		}
		// 位置模式：按 __match_args__
		if len(p.Positional) > 0 {
			margs, found := cls.Lookup("__match_args__")
			if !found {
				// MATCH_SELF 语义：int/str/list/dict 等内置类型支持「单一位置模式 = 整个值」
				if isMatchSelfType(cls) && len(p.Positional) == 1 {
					if !ip.matchPattern(p.Positional[0], v, bindings, env) {
						return false
					}
				} else {
					ip.TypeError("%s() accepts 0 positional sub-patterns (%d given)", cls.Name, len(p.Positional))
				}
				for _, kw := range p.Keywords {
					av := ip.GetAttr(v, kw.Name)
					if !ip.matchPattern(kw.Pat, av, bindings, env) {
						return false
					}
				}
				return true
			}
			names := ip.ToSlice(margs)
			if len(p.Positional) > len(names) {
				ip.TypeError("%s() accepts %d positional sub-patterns (%d given)", cls.Name, len(names), len(p.Positional))
			}
			for i, sub := range p.Positional {
				attrName := names[i].Str()
				av := ip.GetAttr(v, attrName)
				if !ip.matchPattern(sub, av, bindings, env) {
					return false
				}
			}
		}
		for _, kw := range p.Keywords {
			av := ip.GetAttr(v, kw.Name)
			if !ip.matchPattern(kw.Pat, av, bindings, env) {
				return false
			}
		}
		return true
	}
	ip.RuntimeErr("未支持的模式类型 %T", pat)
	return false
}
