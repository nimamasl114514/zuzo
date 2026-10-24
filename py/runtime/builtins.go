package runtime

import (
	"math"
	"math/big"
	"sort"
	"strings"

	"zuzo/py/object"
)

// reg 注册内置函数
func (ip *Interp) reg(name string, fn object.BuiltinFn) {
	ip.Builtins.Set(name, &object.Builtin{Name: name, Fn: fn})
}

// regMethod 注册类型方法
func regMethod(t *object.TypeObj, name string, fn object.BuiltinFn) {
	if t.Dict == nil {
		t.Dict = map[string]object.Object{}
	}
	t.Dict[name] = &object.Builtin{Name: name, Fn: fn}
}

// ==================== 异常类型初始化 ====================

func (ip *Interp) initExceptions() {
	var base *object.TypeObj
	mk := func(name string, parent *object.TypeObj) *object.TypeObj {
		var bases []*object.TypeObj
		if parent == nil {
			bases = nil
		} else {
			bases = []*object.TypeObj{parent}
		}
		t := &object.TypeObj{Name: name, Bases: bases, Dict: map[string]object.Object{}, IsExc: true}
		ip.ExcTypes[name] = t
		ip.Builtins.Set(name, t)
		return t
	}
	base = mk("BaseException", nil)
	base.Bases = []*object.TypeObj{object.ObjectType}
	exc := mk("Exception", base)
	mk("ArithmeticError", exc)
	mk("ZeroDivisionError", ip.ExcTypes["ArithmeticError"])
	mk("OverflowError", ip.ExcTypes["ArithmeticError"])
	mk("FloatingPointError", ip.ExcTypes["ArithmeticError"])
	mk("LookupError", exc)
	mk("IndexError", ip.ExcTypes["LookupError"])
	mk("KeyError", ip.ExcTypes["LookupError"])
	mk("AttributeError", exc)
	mk("NameError", exc)
	mk("UnboundLocalError", ip.ExcTypes["NameError"])
	mk("TypeError", exc)
	mk("ValueError", exc)
	mk("UnicodeError", ip.ExcTypes["ValueError"])
	mk("UnicodeDecodeError", ip.ExcTypes["UnicodeError"])
	mk("UnicodeEncodeError", ip.ExcTypes["UnicodeError"])
	mk("RuntimeError", exc)
	mk("RecursionError", ip.ExcTypes["RuntimeError"])
	mk("NotImplementedError", ip.ExcTypes["RuntimeError"])
	mk("StopIteration", exc)
	mk("StopAsyncIteration", exc)
	mk("AssertionError", exc)
	mk("OSError", exc)
	mk("IOError", ip.ExcTypes["OSError"])
	mk("FileNotFoundError", ip.ExcTypes["OSError"])
	mk("FileExistsError", ip.ExcTypes["OSError"])
	mk("PermissionError", ip.ExcTypes["OSError"])
	mk("IsADirectoryError", ip.ExcTypes["OSError"])
	mk("NotADirectoryError", ip.ExcTypes["OSError"])
	mk("TimeoutError", ip.ExcTypes["OSError"])
	mk("ConnectionError", ip.ExcTypes["OSError"])
	mk("ImportError", exc)
	mk("ModuleNotFoundError", ip.ExcTypes["ImportError"])
	mk("SyntaxError", exc)
	mk("IndentationError", ip.ExcTypes["SyntaxError"])
	mk("MemoryError", exc)
	mk("SystemExit", base)
	mk("KeyboardInterrupt", base)
	mk("GeneratorExit", base)
	mk("SystemError", exc)
	mk("BufferError", exc)
	mk("ReferenceError", exc)
	mk("UnicodeTranslateError", ip.ExcTypes["UnicodeError"])
	// BaseException 的 MRO 需要包含自身
	base.Dict["__init__"] = &object.Builtin{Name: "__init__", Fn: func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) > 0 {
			if e, ok := args[0].(*object.Exception); ok {
				if len(args) > 1 {
					e.Args = append([]object.Object{}, args[1:]...)
				}
			}
		}
		return object.None, nil
	}}
	base.Dict["__str__"] = &object.Builtin{Name: "__str__", Fn: func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.Str{V: ""}, nil
		}
		if e, ok := args[0].(*object.Exception); ok {
			return &object.Str{V: e.Str()}, nil
		}
		return &object.Str{V: args[0].Str()}, nil
	}}
	ObjectType := object.ObjectType
	ip.Builtins.Set("object", ObjectType)
	ip.ExcTypes["__object__"] = ObjectType
}

// ==================== 内置函数注册 ====================

func registerBuiltins(ip *Interp) {
	ip.initExceptions()

	// 常量
	ip.Builtins.Set("True", object.TrueObj)
	ip.Builtins.Set("False", object.FalseObj)
	ip.Builtins.Set("None", object.None)
	ip.Builtins.Set("NotImplemented", object.None)
	ip.Builtins.Set("Ellipsis", &object.Str{V: "Ellipsis"})
	ip.Builtins.Set("__name__", &object.Str{V: "__main__"})
	object.ReprHook = ip.Repr

	ip.reg("print", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		sep := " "
		end := "\n"
		if kwargs != nil {
			if v, ok := kwargs["sep"]; ok && v != object.None {
				sep = v.Str()
			}
			if v, ok := kwargs["end"]; ok && v != object.None {
				end = v.Str()
			}
		}
		var sb strings.Builder
		for i, a := range args {
			if i > 0 {
				sb.WriteString(sep)
			}
			sb.WriteString(ip.StrOf(a))
		}
		sb.WriteString(end)
		ip.Printf("%s", sb.String())
		return object.None, nil
	})

	ip.reg("len", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 1 {
			ip.TypeError("len() takes exactly one argument (%d given)", len(args))
		}
		return &object.Int{V: int64(ip.Len(args[0]))}, nil
	})

	ip.reg("range", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		var start, stop, step int64
		switch len(args) {
		case 1:
			v, ok := object.AsInt(args[0])
			if !ok {
				ip.TypeError("'%s' object cannot be interpreted as an integer", object.TypeName(args[0]))
			}
			start, stop, step = 0, v, 1
		case 2:
			a, ok1 := object.AsInt(args[0])
			b, ok2 := object.AsInt(args[1])
			if !ok1 || !ok2 {
				ip.TypeError("range() arguments must be integers")
			}
			start, stop, step = a, b, 1
		case 3:
			a, ok1 := object.AsInt(args[0])
			b, ok2 := object.AsInt(args[1])
			c, ok3 := object.AsInt(args[2])
			if !ok1 || !ok2 || !ok3 {
				ip.TypeError("range() arguments must be integers")
			}
			start, stop, step = a, b, c
		default:
			ip.TypeError("range expected at most 3 arguments, got %d", len(args))
		}
		if step == 0 {
			ip.ValueError("range() arg 3 must not be zero")
		}
		return &object.Range{Start: start, Stop: stop, Step: step}, nil
	})

	ip.reg("int", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.Int{V: 0}, nil
		}
		return ip.toInt(args[0], args)
	})
	ip.reg("float", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.Float{V: 0}, nil
		}
		switch v := args[0].(type) {
		case *object.Float:
			return v, nil
		case *object.Int:
			return &object.Float{V: float64(v.V)}, nil
		case *object.Bool:
			if v.V {
				return &object.Float{V: 1}, nil
			}
			return &object.Float{V: 0}, nil
		case *object.Str:
			s := strings.TrimSpace(v.V)
			s = strings.ReplaceAll(s, "_", "")
			if s == "inf" || s == "Infinity" {
				return &object.Float{V: math.Inf(1)}, nil
			}
			if s == "-inf" || s == "-Infinity" {
				return &object.Float{V: math.Inf(-1)}, nil
			}
			if s == "nan" {
				return &object.Float{V: math.NaN()}, nil
			}
			f, err := parseFloatTolerant(s)
			if err != nil {
				ip.ValueError("could not convert string to float: %s", v.Repr())
			}
			return &object.Float{V: f}, nil
		}
		ip.TypeError("float() argument must be a string or a number, not '%s'", object.TypeName(args[0]))
		return object.None, nil
	})
	ip.reg("str", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.Str{V: ""}, nil
		}
		return &object.Str{V: ip.StrOf(args[0])}, nil
	})
	ip.reg("repr", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 1 {
			ip.TypeError("repr() takes exactly one argument")
		}
		return &object.Str{V: ip.Repr(args[0])}, nil
	})
	ip.reg("bool", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return object.FalseObj, nil
		}
		return object.GetBool(object.Truthy(args[0])), nil
	})
	ip.reg("list", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.List{}, nil
		}
		return &object.List{Items: append([]object.Object{}, ip.ToSlice(args[0])...)}, nil
	})
	ip.reg("tuple", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.Tuple{}, nil
		}
		return &object.Tuple{Items: append([]object.Object{}, ip.ToSlice(args[0])...)}, nil
	})
	ip.reg("set", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		s := &object.Set{}
		if len(args) > 0 {
			for _, v := range ip.ToSlice(args[0]) {
				s.Add(v)
			}
		}
		return s, nil
	})
	ip.reg("frozenset", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		s := &object.Set{}
		if len(args) > 0 {
			for _, v := range ip.ToSlice(args[0]) {
				s.Add(v)
			}
		}
		return s, nil
	})
	ip.reg("dict", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		d := &object.Dict{}
		if len(args) > 0 {
			if src, ok := args[0].(*object.Dict); ok {
				for i, k := range src.Keys {
					d.Set(k, src.Vals[i])
				}
			} else if inst, ok := args[0].(*object.Instance); ok {
				if dv, ok2 := inst.Dict["_data"].(*object.Dict); ok2 {
					for i, k := range dv.Keys {
						d.Set(k, dv.Vals[i])
					}
				} else if m, found := ip.findDunder(inst.Class, "keys"); found {
					keys := ip.Call(ip.bindMethod(inst, m), nil, nil)
					for _, k := range ip.ToSlice(keys) {
						d.Set(k, ip.GetItem(inst, k))
					}
				}
			} else {
				for _, pair := range ip.ToSlice(args[0]) {
					kv := ip.ToSlice(pair)
					if len(kv) != 2 {
						ip.ValueError("dictionary update sequence element has length %d; 2 is required", len(kv))
					}
					d.Set(kv[0], kv[1])
				}
			}
		}
		seen := map[string]bool{}
		for _, k := range ip.kwOrder {
			if v, ok := kwargs[k]; ok {
				d.Set(&object.Str{V: k}, v)
				seen[k] = true
			}
		}
		for k, v := range kwargs {
			if !seen[k] {
				d.Set(&object.Str{V: k}, v)
			}
		}
		return d, nil
	})
	ip.reg("bytes", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.Bytes{}, nil
		}
		switch v := args[0].(type) {
		case *object.Bytes:
			return &object.Bytes{V: append([]byte{}, v.V...)}, nil
		case *object.Int:
			return &object.Bytes{V: make([]byte, v.V)}, nil
		case *object.Str:
			enc := "utf-8"
			if len(args) > 1 {
				enc = args[1].Str()
			}
			_ = enc
			return &object.Bytes{V: []byte(v.V)}, nil
		case *object.List, *object.Tuple:
			items := ip.ToSlice(v)
			out := make([]byte, len(items))
			for i, it := range items {
				n, ok := object.AsInt(it)
				if !ok || n < 0 || n > 255 {
					ip.ValueError("bytes must be in range(0, 256)")
				}
				out[i] = byte(n)
			}
			return &object.Bytes{V: out}, nil
		}
		ip.TypeError("cannot convert '%s' object to bytes", object.TypeName(args[0]))
		return object.None, nil
	})

	ip.reg("abs", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 1 {
			ip.TypeError("abs() takes exactly one argument")
		}
		if io, ok := toIntObj(args[0]); ok {
			return object.NewBigInt(new(big.Int).Abs(io.AsBig())), nil
		}
		f, ok := object.AsFloat(args[0])
		if !ok {
			ip.TypeError("bad operand type for abs(): '%s'", object.TypeName(args[0]))
		}
		return &object.Float{V: math.Abs(f)}, nil
	})
	ip.reg("round", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			ip.TypeError("round() missing argument")
		}
		nd := 0
		if len(args) > 1 {
			v, ok := object.AsInt(args[1])
			if !ok {
				ip.TypeError("round() second argument must be int")
			}
			nd = int(v)
		}
		if i, ok := object.AsInt(args[0]); ok {
			if _, isF := args[0].(*object.Float); !isF && nd >= 0 {
				return &object.Int{V: i}, nil
			}
		}
		f, ok := object.AsFloat(args[0])
		if !ok {
			ip.TypeError("type %s doesn't define __round__ method", object.TypeName(args[0]))
		}
		if nd == 0 {
			return &object.Int{V: int64(roundHalfEven(f))}, nil
		}
		p := math.Pow(10, float64(nd))
		return &object.Float{V: roundHalfEven(f*p) / p}, nil
	})
	ip.reg("sum", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			ip.TypeError("sum() missing required argument")
		}
		var acc object.Object = &object.Int{V: 0}
		if len(args) > 1 {
			acc = args[1]
		}
		for _, v := range ip.ToSlice(args[0]) {
			acc = ip.BinOp("+", acc, v)
		}
		return acc, nil
	})
	ip.reg("min", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		return ip.minMax(args, kwargs, true)
	})
	ip.reg("max", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		return ip.minMax(args, kwargs, false)
	})
	ip.reg("sorted", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			ip.TypeError("sorted() missing required argument")
		}
		items := append([]object.Object{}, ip.ToSlice(args[0])...)
		reverse := false
		var keyFn object.Object
		if kwargs != nil {
			if v, ok := kwargs["reverse"]; ok {
				reverse = object.Truthy(v)
			}
			if v, ok := kwargs["key"]; ok && v != object.None {
				keyFn = v
			}
		}
		ip.sortObjects(items, keyFn, reverse)
		return &object.List{Items: items}, nil
	})
	ip.reg("reversed", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(args[0])
		out := make([]object.Object, len(items))
		for i := range items {
			out[len(items)-1-i] = items[i]
		}
		return &object.List{Items: out}, nil
	})
	ip.reg("enumerate", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			ip.TypeError("enumerate() missing required argument")
		}
		start := int64(0)
		if len(args) > 1 {
			v, _ := object.AsInt(args[1])
			start = v
		}
		if kwargs != nil {
			if v, ok := kwargs["start"]; ok {
				s, _ := object.AsInt(v)
				start = s
			}
		}
		var out []object.Object
		for i, v := range ip.ToSlice(args[0]) {
			out = append(out, &object.Tuple{Items: []object.Object{&object.Int{V: start + int64(i)}, v}})
		}
		return &object.List{Items: out}, nil
	})
	ip.reg("zip", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.List{}, nil
		}
		lists := make([][]object.Object, len(args))
		minLen := -1
		for i, a := range args {
			lists[i] = ip.ToSlice(a)
			if minLen < 0 || len(lists[i]) < minLen {
				minLen = len(lists[i])
			}
		}
		var out []object.Object
		for i := 0; i < minLen; i++ {
			row := make([]object.Object, len(args))
			for j := range args {
				row[j] = lists[j][i]
			}
			out = append(out, &object.Tuple{Items: row})
		}
		return &object.List{Items: out}, nil
	})
	ip.reg("map", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) < 2 {
			ip.TypeError("map() requires at least two args")
		}
		fn := args[0]
		lists := make([][]object.Object, len(args)-1)
		minLen := -1
		for i, a := range args[1:] {
			lists[i] = ip.ToSlice(a)
			if minLen < 0 || len(lists[i]) < minLen {
				minLen = len(lists[i])
			}
		}
		var out []object.Object
		for i := 0; i < minLen; i++ {
			row := make([]object.Object, len(lists))
			for j := range lists {
				row[j] = lists[j][i]
			}
			out = append(out, ip.Call(fn, row, nil))
		}
		return &object.List{Items: out}, nil
	})
	ip.reg("filter", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) < 2 {
			ip.TypeError("filter() requires two args")
		}
		fn := args[0]
		var out []object.Object
		for _, v := range ip.ToSlice(args[1]) {
			var keep bool
			if fn == object.None {
				keep = object.Truthy(v)
			} else {
				keep = object.Truthy(ip.Call(fn, []object.Object{v}, nil))
			}
			if keep {
				out = append(out, v)
			}
		}
		return &object.List{Items: out}, nil
	})
	ip.reg("any", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		for _, v := range ip.ToSlice(args[0]) {
			if object.Truthy(v) {
				return object.TrueObj, nil
			}
		}
		return object.FalseObj, nil
	})
	ip.reg("all", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		for _, v := range ip.ToSlice(args[0]) {
			if !object.Truthy(v) {
				return object.FalseObj, nil
			}
		}
		return object.TrueObj, nil
	})

	ip.reg("isinstance", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 2 {
			ip.TypeError("isinstance() takes 2 arguments")
		}
		return object.GetBool(ip.isInstance(args[0], args[1])), nil
	})
	ip.reg("issubclass", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 2 {
			ip.TypeError("issubclass() takes 2 arguments")
		}
		c, ok := args[0].(*object.TypeObj)
		if !ok {
			ip.TypeError("issubclass() arg 1 must be a class")
		}
		return object.GetBool(ip.isSubclass(c, args[1])), nil
	})
	ip.reg("type", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 1 {
			return args[0].Type(), nil
		}
		if len(args) == 3 {
			name := args[0].Str()
			cls := &object.TypeObj{Name: name, Dict: map[string]object.Object{}}
			for _, b := range ip.ToSlice(args[1]) {
				if bt, ok := b.(*object.TypeObj); ok {
					cls.Bases = append(cls.Bases, bt)
				}
			}
			if d, ok := args[2].(*object.Dict); ok {
				for i, k := range d.Keys {
					cls.Dict[k.Str()] = d.Vals[i]
				}
			}
			return cls, nil
		}
		ip.TypeError("type() takes 1 or 3 arguments")
		return object.None, nil
	})
	ip.reg("hasattr", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 2 {
			ip.TypeError("hasattr() takes 2 arguments")
		}
		name := args[1].Str()
		found := true
		func() {
			defer func() {
				if r := recover(); r != nil {
					found = false
				}
			}()
			ip.GetAttr(args[0], name)
		}()
		return object.GetBool(found), nil
	})
	ip.reg("getattr", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) < 2 {
			ip.TypeError("getattr() takes at least 2 arguments")
		}
		name := args[1].Str()
		var res object.Object
		ok := true
		func() {
			defer func() {
				if r := recover(); r != nil {
					ok = false
				}
			}()
			res = ip.GetAttr(args[0], name)
		}()
		if !ok {
			if len(args) >= 3 {
				return args[2], nil
			}
			ip.AttrErr(args[0], name)
		}
		return res, nil
	})
	ip.reg("setattr", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 3 {
			ip.TypeError("setattr() takes 3 arguments")
		}
		ip.SetAttr(args[0], args[1].Str(), args[2])
		return object.None, nil
	})
	ip.reg("delattr", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 2 {
			ip.TypeError("delattr() takes 2 arguments")
		}
		if inst, ok := args[0].(*object.Instance); ok {
			delete(inst.Dict, args[1].Str())
			return object.None, nil
		}
		ip.TypeError("delattr() argument must be an object")
		return object.None, nil
	})
	ip.reg("callable", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 1 {
			ip.TypeError("callable() takes 1 argument")
		}
		switch args[0].(type) {
		case *object.Function, *object.Builtin, *object.BoundMethod, *object.TypeObj:
			return object.TrueObj, nil
		case *object.Instance:
			inst := args[0].(*object.Instance)
			_, ok := ip.findDunder(inst.Class, "__call__")
			return object.GetBool(ok), nil
		}
		return object.FalseObj, nil
	})
	ip.reg("iter", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) != 1 {
			ip.TypeError("iter() takes 1 argument")
		}
		return ip.Iter(args[0]), nil
	})
	ip.reg("next", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) < 1 {
			ip.TypeError("next() takes at least 1 argument")
		}
		val, ok := ip.IterNext(args[0])
		if !ok {
			if len(args) >= 2 {
				return args[1], nil
			}
			exc := object.NewException(ip.excType("StopIteration"))
			if g, isGen := args[0].(*object.Generator); isGen && g.RetVal != nil {
				exc.Args = []object.Object{g.RetVal}
			}
			ip.RaiseObj(exc)
		}
		return val, nil
	})
	ip.reg("format", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) < 1 {
			ip.TypeError("format() takes at least 1 argument")
		}
		spec := ""
		if len(args) > 1 {
			spec = args[1].Str()
		}
		return &object.Str{V: ip.FormatValue(args[0], spec)}, nil
	})
	ip.reg("chr", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		n, ok := object.AsInt(args[0])
		if !ok {
			ip.TypeError("an integer is required")
		}
		return &object.Str{V: string(rune(n))}, nil
	})
	ip.reg("ord", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		s, ok := args[0].(*object.Str)
		if !ok {
			ip.TypeError("ord() expected string")
		}
		rs := []rune(s.V)
		if len(rs) != 1 {
			ip.TypeError("ord() expected a character, but string of length %d found", len(rs))
		}
		return &object.Int{V: int64(rs[0])}, nil
	})
	ip.reg("hex", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		io, ok := toIntObj(args[0])
		if !ok {
			ip.TypeError("hex() argument must be int")
		}
		return &object.Str{V: "0x" + bigTextSigned(io, 16)}, nil
	})
	ip.reg("bin", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		io, ok := toIntObj(args[0])
		if !ok {
			ip.TypeError("bin() argument must be int")
		}
		return &object.Str{V: "0b" + bigTextSigned(io, 2)}, nil
	})
	ip.reg("oct", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		io, ok := toIntObj(args[0])
		if !ok {
			ip.TypeError("oct() argument must be int")
		}
		return &object.Str{V: "0o" + bigTextSigned(io, 8)}, nil
	})
	ip.reg("divmod", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		q := ip.opFloorDiv(args[0], args[1])
		r := ip.opMod(args[0], args[1])
		return &object.Tuple{Items: []object.Object{q, r}}, nil
	})
	ip.reg("pow", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) < 2 {
			ip.TypeError("pow() takes at least 2 arguments")
		}
		if len(args) == 2 {
			return ip.opPow(args[0], args[1]), nil
		}
		m, ok := object.AsInt(args[2])
		if !ok {
			ip.TypeError("pow() 3rd argument must be int")
		}
		b, _ := object.AsInt(args[0])
		e, _ := object.AsInt(args[1])
		res := int64(1)
		b %= m
		for e > 0 {
			if e&1 == 1 {
				res = (res * b) % m
			}
			b = (b * b) % m
			e >>= 1
		}
		return &object.Int{V: res}, nil
	})
	ip.reg("id", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(len(ip.Frames))*1000 + 1}, nil
	})
	ip.reg("hash", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		k, ok := object.Hash(args[0])
		if !ok {
			ip.TypeError("unhashable type: '%s'", object.TypeName(args[0]))
		}
		h := int64(0)
		for _, r := range k.S {
			h = h*31 + int64(r)
		}
		return &object.Int{V: h + k.I}, nil
	})
	ip.reg("super", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		// super() 无参：从当前帧环境推断
		if len(args) == 0 {
			env := ip.currentEnv
			if env == nil || env.CurrentClass == nil {
				ip.RuntimeErr("super(): no arguments")
			}
			cls := env.CurrentClass
			// 找 __class__ 与 self
			var self object.Object
			for _, nm := range []string{"self", "cls", "mcls"} {
				if v, ok := env.Get(nm); ok {
					self = v
					break
				}
			}
			return &object.Super{Self: self, Class: cls}, nil
		}
		if len(args) == 2 {
			cls, _ := args[0].(*object.TypeObj)
			return &object.Super{Self: args[1], Class: cls}, nil
		}
		ip.TypeError("super() takes 0 or 2 arguments")
		return object.None, nil
	})
	ip.reg("property", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		p := &object.Property{}
		if len(args) > 0 {
			p.Fget = args[0]
		}
		if len(args) > 1 {
			p.Fset = args[1]
		}
		if len(args) > 2 {
			p.Fdel = args[2]
		}
		if kwargs != nil {
			if v, ok := kwargs["fget"]; ok {
				p.Fget = v
			}
			if v, ok := kwargs["fset"]; ok {
				p.Fset = v
			}
			if v, ok := kwargs["fdel"]; ok {
				p.Fdel = v
			}
		}
		return p, nil
	})
	ip.reg("staticmethod", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		return &object.StaticMethod{Fn: args[0]}, nil
	})
	ip.reg("classmethod", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		return &object.ClassMethod{Fn: args[0]}, nil
	})
	ip.reg("slice", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		s := &object.Slice{}
		switch len(args) {
		case 1:
			s.Stop = args[0]
		case 2:
			s.Start = args[0]
			s.Stop = args[1]
		case 3:
			s.Start = args[0]
			s.Stop = args[1]
			s.Step = args[2]
		}
		return s, nil
	})
	ip.reg("open", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		return ip.builtinOpen(args, kwargs)
	})
	ip.reg("input", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) > 0 {
			ip.Printf("%s", ip.StrOf(args[0]))
		}
		return &object.Str{V: ""}, nil
	})
	ip.reg("dir", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		var names []string
		if len(args) == 0 {
			for k := range ip.Globals.Vars {
				names = append(names, k)
			}
		} else {
			o := args[0]
			switch v := o.(type) {
			case *object.Instance:
				for k := range v.Dict {
					names = append(names, k)
				}
				for _, c := range v.Class.MRO() {
					for k := range c.Dict {
						names = append(names, k)
					}
				}
			case *object.TypeObj:
				for _, c := range v.MRO() {
					for k := range c.Dict {
						names = append(names, k)
					}
				}
			case *object.Module:
				for k := range v.Dict {
					names = append(names, k)
				}
			default:
				for _, c := range o.Type().MRO() {
					for k := range c.Dict {
						names = append(names, k)
					}
				}
			}
		}
		sort.Strings(names)
		items := make([]object.Object, 0, len(names))
		seen := map[string]bool{}
		for _, n := range names {
			if seen[n] {
				continue
			}
			seen[n] = true
			items = append(items, &object.Str{V: n})
		}
		return &object.List{Items: items}, nil
	})
	ip.reg("globals", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		d := &object.Dict{}
		for k, v := range ip.Globals.Vars {
			d.Set(&object.Str{V: k}, v)
		}
		return d, nil
	})
	ip.reg("locals", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		d := &object.Dict{}
		env := ip.currentEnv
		if env == nil {
			env = ip.Globals
		}
		for k, v := range env.Vars {
			d.Set(&object.Str{V: k}, v)
		}
		return d, nil
	})
	ip.reg("vars", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			d := &object.Dict{}
			for k, v := range ip.Globals.Vars {
				d.Set(&object.Str{V: k}, v)
			}
			return d, nil
		}
		if inst, ok := args[0].(*object.Instance); ok {
			d := &object.Dict{}
			for k, v := range inst.Dict {
				d.Set(&object.Str{V: k}, v)
			}
			return d, nil
		}
		ip.TypeError("vars() argument must have __dict__ attribute")
		return object.None, nil
	})
	ip.reg("exit", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		code := 0
		if len(args) > 0 {
			if n, ok := object.AsInt(args[0]); ok {
				code = int(n)
			}
		}
		panic(&exitSignal{code: code})
	})
	ip.reg("quit", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		panic(&exitSignal{code: 0})
	})
	ip.reg("eval", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		src := args[0].Str()
		expr, errs := parseExprString(src)
		if len(errs) > 0 {
			ip.raise(ip.excType("SyntaxError"), "%s", errs[0])
		}
		return ip.evalExpr(expr, ip.Globals), nil
	})
	// type.__new__：供元类 super().__new__ 使用
	object.TypeType.Dict["__new__"] = bi("__new__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) < 5 {
			return object.None, nil
		}
		name := args[2].Str()
		cls := &object.TypeObj{Name: name, Dict: map[string]object.Object{}}
		for _, b := range ip.ToSlice(args[3]) {
			if bt, ok := b.(*object.TypeObj); ok {
				cls.Bases = append(cls.Bases, bt)
			}
		}
		if d, ok := args[4].(*object.Dict); ok {
			for i, k := range d.Keys {
				cls.Dict[k.Str()] = d.Vals[i]
			}
		}
		return cls, nil
	})

	// 类型对象化：int/float/str/... 同时作为类型对象（供 isinstance / match / 构造使用）
	ctorNames := map[string]*object.TypeObj{
		"int": object.IntType, "float": object.FloatType, "str": object.StrType,
		"bool": object.BoolType, "list": object.ListType, "tuple": object.TupleType,
		"dict": object.DictType, "set": object.SetType, "bytes": object.BytesType,
		"type": object.TypeType,
	}
	for name, t := range ctorNames {
		if b, ok := ip.Builtins.Vars[name].(*object.Builtin); ok {
			builtinCtors[t.Builtin] = b.Fn
		}
		ip.Builtins.Set(name, t)
	}

	ip.reg("exec", func(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
		src := args[0].Str()
		m, errs := parseModuleString(src)
		if len(errs) > 0 {
			ip.raise(ip.excType("SyntaxError"), "%s", errs[0])
		}
		ip.ExecModule(m, ip.Globals)
		return object.None, nil
	})
}

// exitSignal 退出信号
type exitSignal struct{ code int }

// builtinCtors 内置类型构造器（int()/str()/... 原始实现）
var builtinCtors = map[int]object.BuiltinFn{}

// ==================== 辅助 ====================

// bigTextSigned 带符号的进制串（不含前缀）
func bigTextSigned(o *object.Int, base int) string {
	if o.AsBig().Sign() < 0 {
		return "-" + new(big.Int).Abs(o.AsBig()).Text(base)
	}
	return o.AsBig().Text(base)
}

func itoaHex(n int64) string {
	if n == 0 {
		return "0"
	}
	const digits = "0123456789abcdef"
	var buf []byte
	for n > 0 {
		buf = append([]byte{digits[n%16]}, buf...)
		n /= 16
	}
	return string(buf)
}

func roundHalfEven(f float64) float64 {
	r := math.Round(f)
	if math.Abs(f-math.Trunc(f)) == 0.5 {
		// 银行家舍入
		if int64(r)%2 != 0 {
			r -= math.Copysign(1, r)
		}
	}
	return r
}

// Len 计算长度
func (ip *Interp) Len(o object.Object) int {
	switch v := o.(type) {
	case *object.Str:
		return len([]rune(v.V))
	case *object.Bytes:
		return len(v.V)
	case *object.List:
		return len(v.Items)
	case *object.Tuple:
		return len(v.Items)
	case *object.Dict:
		return len(v.Keys)
	case *object.Set:
		return len(v.Items)
	case *object.Range:
		return int(v.Len())
	case *object.Instance:
		if m, ok := ip.findDunder(v.Class, "__len__"); ok {
			r := ip.Call(ip.bindMethod(v, m), nil, nil)
			n, _ := object.AsInt(r)
			return int(n)
		}
	case *object.Exception:
		if m, ok := ip.findDunder(v.Class, "__len__"); ok {
			r := ip.Call(ip.bindMethod(v, m), nil, nil)
			n, _ := object.AsInt(r)
			return int(n)
		}
	}
	ip.TypeError("object of type '%s' has no len()", object.TypeName(o))
	return 0
}

func (ip *Interp) toInt(o object.Object, args []object.Object) (object.Object, error) {
	switch v := o.(type) {
	case *object.Int:
		return v, nil
	case *object.Bool:
		if v.V {
			return &object.Int{V: 1}, nil
		}
		return &object.Int{V: 0}, nil
	case *object.Float:
		return &object.Int{V: int64(v.V)}, nil
	case *object.Str:
		base := int64(10)
		if len(args) > 1 {
			b, ok := object.AsInt(args[1])
			if !ok {
				ip.TypeError("int() base must be int")
			}
			base = b
		}
		s := strings.TrimSpace(v.V)
		s = strings.ReplaceAll(s, "_", "")
		neg := false
		if strings.HasPrefix(s, "-") {
			neg = true
			s = s[1:]
		} else if strings.HasPrefix(s, "+") {
			s = s[1:]
		}
		lower := strings.ToLower(s)
		if base == 0 {
			switch {
			case strings.HasPrefix(lower, "0x"):
				base = 16
				s = s[2:]
			case strings.HasPrefix(lower, "0o"):
				base = 8
				s = s[2:]
			case strings.HasPrefix(lower, "0b"):
				base = 2
				s = s[2:]
			default:
				base = 10
			}
		} else if base == 16 && strings.HasPrefix(lower, "0x") {
			s = s[2:]
		} else if base == 8 && strings.HasPrefix(lower, "0o") {
			s = s[2:]
		} else if base == 2 && strings.HasPrefix(lower, "0b") {
			s = s[2:]
		}
		if s == "" {
			ip.ValueError("invalid literal for int() with base %d: %s", base, v.Repr())
		}
		var n int64
		for _, r := range s {
			var d int64
			switch {
			case r >= '0' && r <= '9':
				d = int64(r - '0')
			case r >= 'a' && r <= 'z':
				d = int64(r-'a') + 10
			case r >= 'A' && r <= 'Z':
				d = int64(r-'A') + 10
			default:
				ip.ValueError("invalid literal for int() with base %d: %s", base, v.Repr())
			}
			if d >= base {
				ip.ValueError("invalid literal for int() with base %d: %s", base, v.Repr())
			}
			if n > (1<<62)/base || n < -(1<<62)/base {
				bb, ok2 := new(big.Int).SetString(s, int(base))
				if !ok2 {
					ip.ValueError("invalid literal for int() with base %d: %s", base, v.Repr())
				}
				if neg {
					bb.Neg(bb)
				}
				return object.NewBigInt(bb), nil
			}
			n = n*base + d
		}
		if neg {
			n = -n
		}
		return &object.Int{V: n}, nil
	}
	ip.TypeError("int() argument must be a string, a bytes-like object or a number, not '%s'", object.TypeName(o))
	return object.None, nil
}

func (ip *Interp) minMax(args []object.Object, kwargs map[string]object.Object, isMin bool) (object.Object, error) {
	if len(args) == 0 {
		ip.TypeError("min()/max() expected at least 1 argument")
	}
	var items []object.Object
	if len(args) == 1 {
		items = ip.ToSlice(args[0])
	} else {
		items = args
	}
	if len(items) == 0 {
		if kwargs != nil {
			if d, ok := kwargs["default"]; ok {
				return d, nil
			}
		}
		ip.ValueError("min() arg is an empty sequence")
	}
	var keyFn object.Object
	if kwargs != nil {
		if v, ok := kwargs["key"]; ok && v != object.None {
			keyFn = v
		}
	}
	best := items[0]
	bestKey := ip.applyKey(best, keyFn)
	for _, it := range items[1:] {
		k := ip.applyKey(it, keyFn)
		c, ok := object.Compare(k, bestKey)
		if !ok {
			ip.TypeError("'%s' not supported between instances", object.TypeName(k))
		}
		if (isMin && c < 0) || (!isMin && c > 0) {
			best = it
			bestKey = k
		}
	}
	return best, nil
}

func (ip *Interp) applyKey(v object.Object, keyFn object.Object) object.Object {
	if keyFn == nil || keyFn == object.None {
		return v
	}
	return ip.Call(keyFn, []object.Object{v}, nil)
}

func (ip *Interp) sortObjects(items []object.Object, keyFn object.Object, reverse bool) {
	keys := make([]object.Object, len(items))
	for i, it := range items {
		keys[i] = ip.applyKey(it, keyFn)
	}
	idx := make([]int, len(items))
	for i := range idx {
		idx[i] = i
	}
	sort.SliceStable(idx, func(a, b int) bool {
		c, ok := object.Compare(keys[idx[a]], keys[idx[b]])
		if !ok {
			return false
		}
		if reverse {
			return c > 0
		}
		return c < 0
	})
	out := make([]object.Object, len(items))
	for i, j := range idx {
		out[i] = items[j]
	}
	copy(items, out)
}

func (ip *Interp) isInstance(o object.Object, clsObj object.Object) bool {
	switch c := clsObj.(type) {
	case *object.TypeObj:
		if c.Name == "object" {
			return true
		}
		return o.Type().IsSubclass(c)
	case *object.Tuple:
		for _, it := range c.Items {
			if ip.isInstance(o, it) {
				return true
			}
		}
		return false
	}
	ip.TypeError("isinstance() arg 2 must be a type or tuple of types")
	return false
}

func (ip *Interp) isSubclass(c *object.TypeObj, clsObj object.Object) bool {
	switch t := clsObj.(type) {
	case *object.TypeObj:
		if t.Name == "object" {
			return true
		}
		return c.IsSubclass(t)
	case *object.Tuple:
		for _, it := range t.Items {
			if ip.isSubclass(c, it) {
				return true
			}
		}
		return false
	}
	ip.TypeError("issubclass() arg 2 must be a class")
	return false
}

// constructBuiltinType 内置类型构造
func (ip *Interp) constructBuiltinType(cls *object.TypeObj, args []object.Object, kwargs map[string]object.Object) object.Object {
	if fn, ok := builtinCtors[cls.Builtin]; ok {
		res, err := fn(args, kwargs)
		if err != nil {
			if pe, ok2 := err.(*PyError); ok2 {
				panic(pe)
			}
			ip.RuntimeErr("%v", err)
		}
		if res == nil {
			return object.None
		}
		return res
	}
	switch cls.Builtin {
	case object.BT_Int:
		if len(args) == 0 {
			return &object.Int{V: 0}
		}
		v, _ := ip.toInt(args[0], args)
		return v
	case object.BT_Float:
		if len(args) == 0 {
			return &object.Float{V: 0}
		}
		if f, ok := object.AsFloat(args[0]); ok {
			return &object.Float{V: f}
		}
		return &object.Float{V: 0}
	case object.BT_Str:
		if len(args) == 0 {
			return &object.Str{V: ""}
		}
		return &object.Str{V: ip.StrOf(args[0])}
	case object.BT_Bool:
		if len(args) == 0 {
			return object.FalseObj
		}
		return object.GetBool(object.Truthy(args[0]))
	case object.BT_List:
		if len(args) == 0 {
			return &object.List{}
		}
		return &object.List{Items: append([]object.Object{}, ip.ToSlice(args[0])...)}
	case object.BT_Tuple:
		if len(args) == 0 {
			return &object.Tuple{}
		}
		return &object.Tuple{Items: append([]object.Object{}, ip.ToSlice(args[0])...)}
	case object.BT_Dict:
		d := &object.Dict{}
		if len(args) > 0 {
			if src, ok := args[0].(*object.Dict); ok {
				for i, k := range src.Keys {
					d.Set(k, src.Vals[i])
				}
			}
		}
		seen := map[string]bool{}
		for _, k := range ip.kwOrder {
			if v, ok := kwargs[k]; ok {
				d.Set(&object.Str{V: k}, v)
				seen[k] = true
			}
		}
		for k, v := range kwargs {
			if !seen[k] {
				d.Set(&object.Str{V: k}, v)
			}
		}
		return d
	case object.BT_Set:
		s := &object.Set{}
		if len(args) > 0 {
			for _, v := range ip.ToSlice(args[0]) {
				s.Add(v)
			}
		}
		return s
	case object.BT_Bytes:
		return &object.Bytes{}
	case object.BT_Slice:
		s := &object.Slice{}
		if len(args) > 0 {
			s.Stop = args[0]
		}
		if len(args) > 1 {
			s.Start = args[0]
			s.Stop = args[1]
		}
		if len(args) > 2 {
			s.Step = args[2]
		}
		return s
	}
	ip.TypeError("cannot construct '%s'", cls.Name)
	return object.None
}
