package runtime

// operator_mod.go — _operator（C 模块）的 Go 原生实现（第14轮）
//
// 触发点：CPython 的 Lib/operator.py 首行 `from _operator import *`。
// urllib3 / requests 链大量使用 operator 家族（itemgetter / attrgetter / methodcaller / add）。
// 这里直接实现 CPython 的 C 层接口，operator.py 的纯 Python 包装即可正常加载。

import (
	"zuzo/py/object"
)

func init() {
	builtinModuleInits["_operator"] = modOperator
}

func modOperator(ip *Interp) *object.Module {
	m := newModule("_operator")
	// _compare_digest：hmac.py:12 `from _operator import _compare_digest`，CPython 口径的常量时间比较。
	m.Dict["_compare_digest"] = bi("_compare_digest", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			return object.FalseObj, nil
		}
		x, y := pyRawBytes(a[0]), pyRawBytes(a[1])
		if len(x) != len(y) {
			return object.FalseObj, nil
		}
		var v byte
		for i := range x {
			v |= x[i] ^ y[i]
		}
		return object.GetBool(v == 0), nil
	})

	bin := func(name, op string) {
		m.Dict[name] = bi(name, func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) < 2 {
				ip.TypeError("%s expected 2 arguments, got %d", name, len(a))
			}
			return ip.BinOp(op, a[0], a[1]), nil
		})
	}
	bin("add", "+")
	bin("sub", "-")
	bin("mul", "*")
	bin("truediv", "/")
	bin("floordiv", "//")
	bin("mod", "%")
	bin("pow", "**")
	bin("lshift", "<<")
	bin("rshift", ">>")
	bin("and_", "&")
	bin("or_", "|")
	bin("xor", "^")
	bin("matmul", "@")
	// 原地变体（iadd 等）对不可变对象等价于普通运算，这里直接复用
	for _, n := range []string{"iadd", "isub", "imul", "itruediv", "ifloordiv", "imod", "ipow", "ilshift", "irshift", "iand", "ior", "ixor"} {
		m.Dict[n] = m.Dict[n[1:]]
		if n[1:] == "and_" || n[1:] == "or_" {
			m.Dict[n] = m.Dict[n[1:]]
		}
	}

	cmpf := func(name string, fn func(c int) bool) {
		m.Dict[name] = bi(name, func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) < 2 {
				ip.TypeError("%s expected 2 arguments", name)
			}
			c, ok := ip.Cmp(a[0], a[1])
			if !ok {
				if name == "eq" {
					return object.GetBool(ip.Eq(a[0], a[1])), nil
				}
				if name == "ne" {
					return object.GetBool(!ip.Eq(a[0], a[1])), nil
				}
				ip.TypeError("'%s' not supported between instances", name)
			}
			return object.GetBool(fn(c)), nil
		})
	}
	cmpf("lt", func(c int) bool { return c < 0 })
	cmpf("le", func(c int) bool { return c <= 0 })
	cmpf("eq", func(c int) bool { return c == 0 })
	cmpf("ne", func(c int) bool { return c != 0 })
	cmpf("ge", func(c int) bool { return c >= 0 })
	cmpf("gt", func(c int) bool { return c > 0 })

	m.Dict["not_"] = bi("not_", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return object.TrueObj, nil
		}
		return object.GetBool(!object.Truthy(a[0])), nil
	})
	m.Dict["truth"] = bi("truth", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return object.FalseObj, nil
		}
		return object.GetBool(object.Truthy(a[0])), nil
	})
	m.Dict["is_"] = bi("is_", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			return object.FalseObj, nil
		}
		return object.GetBool(ip.IsIdentical(a[0], a[1])), nil
	})
	m.Dict["is_not"] = bi("is_not", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			return object.TrueObj, nil
		}
		return object.GetBool(!ip.IsIdentical(a[0], a[1])), nil
	})
	m.Dict["contains"] = bi("contains", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			return object.FalseObj, nil
		}
		return object.GetBool(ip.Contains(a[0], a[1])), nil
	})
	m.Dict["getitem"] = bi("getitem", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("getitem expected 2 arguments")
		}
		return ip.GetItem(a[0], a[1]), nil
	})
	m.Dict["setitem"] = bi("setitem", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 3 {
			ip.TypeError("setitem expected 3 arguments")
		}
		ip.SetItem(a[0], a[1], a[2])
		return object.None, nil
	})
	m.Dict["delitem"] = bi("delitem", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	m.Dict["neg"] = bi("neg", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return ip.UnaryOp("-", a[0]), nil
	})
	m.Dict["pos"] = bi("pos", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return ip.UnaryOp("+", a[0]), nil
	})
	m.Dict["abs"] = bi("abs", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return ip.UnaryOp("abs", a[0]), nil
	})
	m.Dict["invert"] = bi("invert", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return ip.UnaryOp("~", a[0]), nil
	})
	m.Dict["index"] = bi("index", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if n, ok := object.AsInt(a[0]); ok {
			return &object.Int{V: n}, nil
		}
		ip.TypeError("'%s' object cannot be interpreted as an integer", object.TypeName(a[0]))
		return object.None, nil
	})
	m.Dict["length_hint"] = bi("length_hint", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Int{V: 0}, nil
		}
		return &object.Int{V: int64(ip.Len(a[0]))}, nil
	})
	m.Dict["concat"] = bi("concat", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return ip.BinOp("+", a[0], a[1]), nil
	})
	m.Dict["countOf"] = bi("countOf", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			return &object.Int{V: 0}, nil
		}
		n := 0
		for _, item := range ip.ToSlice(a[0]) {
			if ip.Eq(item, a[1]) {
				n++
			}
		}
		return &object.Int{V: int64(n)}, nil
	})
	m.Dict["indexOf"] = bi("indexOf", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.ValueError("indexOf: value not found")
		}
		for i, item := range ip.ToSlice(a[0]) {
			if ip.Eq(item, a[1]) {
				return &object.Int{V: int64(i)}, nil
			}
		}
		ip.ValueError("indexOf: value not found")
		return object.None, nil
	})

	m.Dict["itemgetter"] = bi("itemgetter", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		keys := append([]object.Object{}, a...)
		return &object.Builtin{Name: "itemgetter", Fn: func(call []object.Object, _ map[string]object.Object) (object.Object, error) {
			if len(call) == 0 {
				ip.TypeError("itemgetter expected 1 argument")
			}
			obj := call[0]
			vals := make([]object.Object, len(keys))
			for i, kk := range keys {
				vals[i] = ip.GetItem(obj, kk)
			}
			if len(vals) == 1 {
				return vals[0], nil
			}
			return &object.Tuple{Items: vals}, nil
		}}, nil
	})
	m.Dict["attrgetter"] = bi("attrgetter", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		names := make([]string, 0, len(a))
		for _, x := range a {
			names = append(names, x.Str())
		}
		return &object.Builtin{Name: "attrgetter", Fn: func(call []object.Object, _ map[string]object.Object) (object.Object, error) {
			if len(call) == 0 {
				ip.TypeError("attrgetter expected 1 argument")
			}
			vals := make([]object.Object, len(names))
			for i, n := range names {
				vals[i] = ip.GetAttr(call[0], n)
			}
			if len(vals) == 1 {
				return vals[0], nil
			}
			return &object.Tuple{Items: vals}, nil
		}}, nil
	})
	m.Dict["methodcaller"] = bi("methodcaller", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("methodcaller expected a method name")
		}
		name := a[0].Str()
		rest := append([]object.Object{}, a[1:]...)
		return &object.Builtin{Name: "methodcaller", Fn: func(call []object.Object, _ map[string]object.Object) (object.Object, error) {
			if len(call) == 0 {
				ip.TypeError("methodcaller expected 1 argument")
			}
			fn := ip.GetAttr(call[0], name)
			return ip.Call(fn, rest, nil), nil
		}}, nil
	})
	return m
}
