package runtime

import (
	"fmt"
	"os"
	"strings"

	"zuzo/py/ast"
	"zuzo/py/object"
)

// findDunder 沿 MRO 查找方法
func (ip *Interp) findDunder(cls *object.TypeObj, name string) (object.Object, bool) {
	if cls == nil {
		return nil, false
	}
	return cls.Lookup(name)
}

// bindMethod 绑定 self
func (ip *Interp) bindMethod(self object.Object, fn object.Object) object.Object {
	switch f := fn.(type) {
	case *object.Function:
		return &object.BoundMethod{Self: self, Fn: f}
	case *object.Builtin:
		cp := *f
		cp.Self = self
		return &cp
	}
	return fn
}

// GetAttr 属性访问
func (ip *Interp) GetAttr(o object.Object, name string) object.Object {
	// ctypes 对象（CDLL / CFuncPtr / CValue）优先走原生 FFI 属性通道
	if v, ok := ip.ctypesGetAttr(o, name); ok {
		return v
	}
	// __doc__ 兜底：CPython 里任何对象都有 __doc__（无则 None）。
	// email/_policybase.py:103 会对 bool 等原生值取 __doc__，缺了整条 email 链断。
	if name == "__doc__" {
		if inst, ok := o.(*object.Instance); ok {
			if dv, has := inst.Dict["__doc__"]; has {
				return dv
			}
			return object.None
		}
		if t, ok := o.(*object.TypeObj); ok {
			if dv, has := t.Dict["__doc__"]; has {
				return dv
			}
			return object.None
		}
		return object.None
	}
	// 原生容器类型的 dunder 兜底：_collections_abc 的 _check_methods 用 hasattr(C, '__xxx__')
	// 判断 ABC 兼容性，缺这些会让 register/isinstance 判定失败。
	if name == "__contains__" || name == "__len__" || name == "__iter__" || name == "__getitem__" {
		isNativeContainer := false
		switch o.(type) {
		case *object.Set, *object.List, *object.Dict, *object.Str, *object.Tuple,
			*object.Bytes, *object.ByteArray, *object.MemoryView, *object.Range:
			isNativeContainer = true
		}
		if isNativeContainer {
			self := o
			n := name
			return &object.Builtin{Name: n, Self: self, Fn: func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				args := a
				if len(args) > 0 && args[0] == self {
					args = args[1:]
				}
				switch n {
				case "__contains__":
					if len(args) < 1 {
						return object.FalseObj, nil
					}
					return object.GetBool(ip.Contains(self, args[0])), nil
				case "__len__":
					return &object.Int{V: int64(ip.Len(self))}, nil
				case "__iter__":
					return ip.Iter(self), nil
				case "__getitem__":
					if len(args) < 1 {
						ip.TypeError("__getitem__ requires index")
					}
					return ip.GetItem(self, args[0]), nil
				}
				return object.None, nil
			}}
		}
	}
	// int.to_bytes（CPython 常用：协议序列化 / 长度前缀）
	if iv, ok := o.(*object.Int); ok && name == "to_bytes" {
		self := iv
		return &object.Builtin{Name: "to_bytes", Self: self, Fn: func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) < 2 {
				ip.TypeError("to_bytes() requires length")
			}
			n, _ := object.AsInt(a[1])
			order := "big"
			if len(a) > 2 && a[2] != object.None {
				order = a[2].Str()
			}
			signed := false
			if len(a) > 3 {
				signed = object.Truthy(a[3])
			}
			v := self.V
			if v < 0 && !signed {
				ip.raise(ip.excType("OverflowError"), "can't convert negative int to unsigned")
			}
			out := make([]byte, n)
			uv := uint64(v)
			for i := int64(0); i < n; i++ {
				var b byte
				if order == "little" {
					b = byte(uv >> (8 * i))
				} else {
					b = byte(uv >> (8 * (n - 1 - i)))
				}
				out[i] = b
			}
			return &object.Bytes{V: out}, nil
		}}
	}
	// bytearray / memoryview 原生方法
	if v, ok := ip.byteArrayGetAttr(o, name); ok {
		return v
	}
	if v, ok := ip.memoryViewGetAttr(o, name); ok {
		return v
	}
	switch v := o.(type) {
	case *object.Instance:
		// 1. 实例字典
		if val, ok := v.Dict[name]; ok {
			return val
		}
		// 2. 类 MRO（含自定义描述符 __get__）
		if m, ok := ip.findDunder(v.Class, name); ok {
			if desc, isInst := m.(*object.Instance); isInst {
				if getter, has := ip.findDunder(desc.Class, "__get__"); has {
					return ip.Call(ip.bindMethod(desc, getter), []object.Object{v, v.Class}, nil)
				}
			}
			return ip.bindClassAttr(v.Class, v, m)
		}
		// 3. __getattr__
		if m, ok := ip.findDunder(v.Class, "__getattr__"); ok {
			return ip.Call(ip.bindMethod(v, m), []object.Object{&object.Str{V: name}}, nil)
		}
		if name == "__dict__" {
			return &object.DictProxy{M: v.Dict}
		}
		if name == "__class__" {
			return v.Class
		}
		ip.AttrErr(o, name)
	case *object.TypeObj:
		// int.from_bytes（类方法）
		if v == object.IntType && name == "from_bytes" {
			return &object.Builtin{Name: "from_bytes", Fn: func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				if len(a) == 0 {
					ip.TypeError("from_bytes() requires bytes")
				}
				var data []byte
				switch b := a[0].(type) {
				case *object.Bytes:
					data = b.V
				case *object.ByteArray:
					data = b.V
				case *object.Str:
					data = []byte(b.V)
				}
				order := "big"
				if len(a) > 1 && a[1] != object.None {
					order = a[1].Str()
				}
				var out uint64
				if order == "little" {
					for i := len(data) - 1; i >= 0; i-- {
						out = out<<8 | uint64(data[i])
					}
				} else {
					for _, b := range data {
						out = out<<8 | uint64(b)
					}
				}
				return &object.Int{V: int64(out)}, nil
			}}
		}
		// 类属性
		if m, ok := v.Lookup(name); ok {
			return ip.bindClassAttr(v, nil, m)
		}
		// 元类属性（type(Cls).attr）：CPython 里类属性找不到会落到元类上，
		// 如 ABCMeta.register / EnumType.__iter__ / 自定义 metaclass 的方法。
		if v.MetaClass != nil && v.MetaClass != object.TypeType {
			if m, ok := ip.findDunder(v.MetaClass, name); ok {
				return ip.bindClassAttr(v.MetaClass, v, m)
			}
		}
		switch name {
		case "__name__":
			return &object.Str{V: v.Name}
		case "__qualname__":
			return &object.Str{V: v.Name}
		case "__module__":
			if mv, ok := v.Dict["__module__"]; ok {
				return mv
			}
			return &object.Str{V: "builtins"}
		case "__doc__":
			// CPython: 没有 docstring 的类 __doc__ 为 None
			if dv, ok := v.Dict["__doc__"]; ok {
				return dv
			}
			return object.None
		case "__class__":
			return v.Type()
		case "__bases__":
			items := make([]object.Object, len(v.Bases))
			for i, b := range v.Bases {
				items[i] = b
			}
			return &object.Tuple{Items: items}
		case "__mro__":
			mro := v.MRO()
			items := make([]object.Object, len(mro))
			for i, c := range mro {
				items[i] = c
			}
			return &object.Tuple{Items: items}
		case "__dict__":
			d := &object.Dict{}
			for k2, val := range v.Dict {
				d.Set(&object.Str{V: k2}, val)
			}
			return d
		}
		ip.AttrErr(o, name)
	case *object.DictProxy:
		switch name {
		case "get":
			return &object.Builtin{Name: "get", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				if len(args) < 2 {
					return object.None, nil
				}
				if val, ok := v.M[args[1].Str()]; ok {
					return val, nil
				}
				if len(args) > 2 {
					return args[2], nil
				}
				return object.None, nil
			}}
		case "keys":
			return &object.Builtin{Name: "keys", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				var out []object.Object
				for k := range v.M {
					out = append(out, &object.Str{V: k})
				}
				return &object.List{Items: out}, nil
			}}
		case "values":
			return &object.Builtin{Name: "values", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				var out []object.Object
				for _, val := range v.M {
					out = append(out, val)
				}
				return &object.List{Items: out}, nil
			}}
		case "items":
			return &object.Builtin{Name: "items", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				var out []object.Object
				for k, val := range v.M {
					out = append(out, &object.Tuple{Items: []object.Object{&object.Str{V: k}, val}})
				}
				return &object.List{Items: out}, nil
			}}
		case "__contains__":
			return &object.Builtin{Name: "__contains__", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				if len(args) < 2 {
					return object.FalseObj, nil
				}
				_, ok := v.M[args[1].Str()]
				return object.GetBool(ok), nil
			}}
		// __dict__ 视图补齐 dict 常用方法：requests 的 PreparedRequest 在
		// `self.__dict__.update(...)` 上失败（'dict' object has no attribute 'update'，第16轮实测）
		case "update":
			return &object.Builtin{Name: "update", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				if len(args) > 1 {
					switch src := args[1].(type) {
					case *object.Dict:
						for i, kk := range src.Keys {
							if i < len(src.Vals) {
								v.M[kk.Str()] = src.Vals[i]
							}
						}
					case *object.DictProxy:
						for kk, vv := range src.M {
							v.M[kk] = vv
						}
					default:
						for _, pair := range ip.ToSlice(args[1]) {
							kv := ip.ToSlice(pair)
							if len(kv) == 2 {
								v.M[kv[0].Str()] = kv[1]
							}
						}
					}
				}
				for kk, vv := range kw {
					v.M[kk] = vv
				}
				return object.None, nil
			}}
		case "setdefault":
			return &object.Builtin{Name: "setdefault", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				if len(args) < 2 {
					return object.None, nil
				}
				key := args[1].Str()
				if val, ok := v.M[key]; ok {
					return val, nil
				}
				var def object.Object = object.None
				if len(args) > 2 {
					def = args[2]
				}
				v.M[key] = def
				return def, nil
			}}
		case "pop":
			return &object.Builtin{Name: "pop", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				if len(args) < 2 {
					return object.None, nil
				}
				key := args[1].Str()
				if val, ok := v.M[key]; ok {
					delete(v.M, key)
					return val, nil
				}
				if len(args) > 2 {
					return args[2], nil
				}
				ip.KeyErr(args[1])
				return object.None, nil
			}}
		case "clear":
			return &object.Builtin{Name: "clear", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				for kk := range v.M {
					delete(v.M, kk)
				}
				return object.None, nil
			}}
		case "copy":
			return &object.Builtin{Name: "copy", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				d := &object.Dict{}
				for kk, vv := range v.M {
					d.Set(&object.Str{V: kk}, vv)
				}
				return d, nil
			}}
		case "__len__":
			return &object.Builtin{Name: "__len__", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				return &object.Int{V: int64(len(v.M))}, nil
			}}
		case "__iter__":
			return &object.Builtin{Name: "__iter__", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				var out []object.Object
				for kk := range v.M {
					out = append(out, &object.Str{V: kk})
				}
				return &object.Iterator{Items: out}, nil
			}}
		case "__getitem__":
			return &object.Builtin{Name: "__getitem__", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				if len(args) < 2 {
					return object.None, nil
				}
				if val, ok := v.M[args[1].Str()]; ok {
					return val, nil
				}
				ip.KeyErr(args[1])
				return object.None, nil
			}}
		case "__setitem__":
			return &object.Builtin{Name: "__setitem__", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
				if len(args) >= 3 {
					v.M[args[1].Str()] = args[2]
				}
				return object.None, nil
			}}
		}
		ip.AttrErr(o, name)
	case *object.Function:
		// 自定义属性优先：functools.wraps 写入的 __name__/__doc__/__wrapped__ 等会覆盖内置只读视图
		if val, ok := v.GetAttr(name); ok {
			return val
		}
		switch name {
		case "__name__":
			return &object.Str{V: v.Name}
		case "__qualname__":
			q := v.QualName
			if q == "" {
				q = v.Name
			}
			return &object.Str{V: q}
		case "__doc__":
			if v.Doc == "" {
				return object.None
			}
			return &object.Str{V: v.Doc}
		case "__class__":
			return object.FuncType
		case "__code__":
			return ip.newCodeObj(v)
		case "__closure__":
			// CPython 的 types.py 用 `(lambda x: lambda: x)(0).__closure__[0]` 取 cell 类型；
			// 返回 None 会让它 `[0]` 直接 TypeError（enum / email / urllib3 全链断在这里）。
			return &object.Tuple{Items: []object.Object{newCellObj()}}
		case "__globals__":
			return &object.Dict{}
		case "__module__":
			return &object.Str{V: v.Module}
		case "__dict__":
			// 返回函数自身的属性字典（活引用，支持 f.__dict__['x'] = 1）
			return v.DictRef()
		case "__defaults__":
			if len(v.ArgDefaults) == 0 {
				return object.None
			}
			return &object.Tuple{Items: v.ArgDefaults}
		}
		ip.AttrErr(o, name)
	case *object.Builtin:
		switch name {
		case "__name__":
			return &object.Str{V: v.Name}
		case "__class__":
			return object.BuiltinType
		case "__self__", "__doc__":
			return object.None
		}
		ip.AttrErr(o, name)
	case *object.Range:
		switch name {
		case "start":
			return &object.Int{V: v.Start}
		case "stop":
			return &object.Int{V: v.Stop}
		case "step":
			return &object.Int{V: v.Step}
		}
	case *object.Generator:
		switch name {
		case "__name__":
			return &object.Str{V: v.Name}
		case "__class__":
			return object.GenType
		}
	case *object.Module:
		if val, ok := v.Dict[name]; ok {
			return val
		}
		if name == "__name__" {
			return &object.Str{V: v.Name}
		}
		if name == "__dict__" {
			return &object.DictProxy{M: v.Dict}
		}
		ip.raise(ip.excType("AttributeError"), "module '%s' has no attribute '%s'", v.Name, name)
	case *object.Exception:
		if val, ok := v.Dict[name]; ok {
			return val
		}
		if m, ok := ip.findDunder(v.Class, name); ok {
			return ip.bindClassAttr(v.Class, v, m)
		}
		switch name {
		case "__traceback__":
			// CPython types.py 用 `raise TypeError` + `exc.__traceback__.tb_frame` 取 FrameType；
			// 返回 None 会让它 .tb_frame 直接挂，整个 types.py 起不来。
			return ip.newTracebackObj()
		case "__cause__", "__context__", "__suppress_context__":
			return object.None
		case "args":
			return &object.Tuple{Items: v.Args}
		case "__class__":
			return v.Class
		case "message":
			return &object.Str{V: v.Str()}
		case "value":
			if len(v.Args) > 0 {
				return v.Args[0]
			}
			return object.None
		}
		ip.AttrErr(o, name)
	case *object.Super:
		// super().method：基于 self 实际类型的 MRO，从 v.Class 之后开始找
		if v.Self == nil && v.Class != nil {
			v.Self = v.Class
		}
		var mro []*object.TypeObj
		if v.Self != nil {
			if _, isType := v.Self.(*object.TypeObj); isType {
				// 元类里的 super()（如 ABCMeta.__new__ 中的 super().__new__）：
				// 查找链必须是元类自身的 MRO（ABCMeta → type → object），
				// 而不是「类对象的类型」MRO（type → object，里面没有 ABCMeta，
				// start 停在 0 又命中自己 → 无限递归，第16轮实测）。
				if v.Class != nil {
					mro = v.Class.MRO()
				}
			} else if t := v.Self.Type(); t != nil {
				mro = t.MRO()
			}
		}
		if mro == nil && v.Class != nil {
			mro = v.Class.MRO()
		}
		start := 0
		for i, c := range mro {
			if c == v.Class {
				start = i + 1
				break
			}
		}
		if os.Getenv("ZUZO_DEBUG_SUPER") != "" {
			fmt.Fprintf(os.Stderr, "[super] name=%s self=%T class=", name, v.Self)
			if v.Class != nil {
				fmt.Fprintf(os.Stderr, "%s", v.Class.Name)
			}
			fmt.Fprintf(os.Stderr, " start=%d mro=", start)
			for _, c := range mro {
				fmt.Fprintf(os.Stderr, "%s ", c.Name)
			}
			fmt.Fprintln(os.Stderr)
		}
		for i := start; i < len(mro); i++ {
			if m, ok := mro[i].Dict[name]; ok {
				if os.Getenv("ZUZO_DEBUG_SUPER") != "" {
					fmt.Fprintf(os.Stderr, "[super-hit] %s -> %T on %s (i=%d)\n", name, m, mro[i].Name, i)
				}
				return ip.bindClassAttr(mro[i], v.Self, m)
			}
		}
		ip.AttrErr(o, name)
	}

	// 内置类型：查类型字典
	t := o.Type()
	if m, ok := t.Lookup(name); ok {
		return ip.bindClassAttr(t, o, m)
	}
	// 用户类的实例属性（Exception 已处理）
	if inst, ok := o.(*object.Instance); ok {
		if m, ok := ip.findDunder(inst.Class, "__getattr__"); ok {
			return ip.Call(ip.bindMethod(inst, m), []object.Object{&object.Str{V: name}}, nil)
		}
	}
	ip.AttrErr(o, name)
	return object.None
}

// bindClassAttr 将类属性绑定到实例/类
func (ip *Interp) bindClassAttr(definedOn *object.TypeObj, self object.Object, attr object.Object) object.Object {
	switch a := attr.(type) {
	case *object.Property:
		if self == nil {
			return a
		}
		if a.Fget == nil {
			ip.raise(ip.excType("AttributeError"), "unreadable attribute")
		}
		return ip.Call(a.Fget, []object.Object{self}, nil)
	case *object.StaticMethod:
		return a.Fn
	case *object.ClassMethod:
		cls := definedOn
		if inst, ok := self.(*object.Instance); ok {
			cls = inst.Class
		}
		if self != nil {
			if t, ok := self.(*object.TypeObj); ok {
				cls = t
			}
		}
		return ip.bindMethod(cls, a.Fn)
	case *object.Function:
		if self == nil {
			return a
		}
		return &object.BoundMethod{Self: self, Fn: a}
	case *object.Builtin:
		if self == nil {
			return a
		}
		cp := *a
		cp.Self = self
		return &cp
	}
	return attr
}

// SetAttr 属性赋值
func (ip *Interp) SetAttr(o object.Object, name string, v object.Object) {
	// ctypes 函数指针的 argtypes / restype 设置
	if ctypesSetAttr(o, name, v) {
		return
	}
	switch obj := o.(type) {
	case *object.Instance:
		// data descriptor 优先
		if m, ok := ip.findDunder(obj.Class, name); ok {
			if p, ok := m.(*object.Property); ok {
				if p.Fset == nil {
					ip.raise(ip.excType("AttributeError"), "can't set attribute")
				}
				ip.Call(p.Fset, []object.Object{obj, v}, nil)
				return
			}
			if desc, isInst := m.(*object.Instance); isInst {
				if setter, has := ip.findDunder(desc.Class, "__set__"); has {
					ip.Call(ip.bindMethod(desc, setter), []object.Object{obj, v}, nil)
					return
				}
			}
		}
		if m, ok := ip.findDunder(obj.Class, "__setattr__"); ok {
			ip.Call(ip.bindMethod(obj, m), []object.Object{&object.Str{V: name}, v}, nil)
			return
		}
		obj.Dict[name] = v
		return
	case *object.TypeObj:
		obj.Dict[name] = v
		return
	case *object.Module:
		obj.Dict[name] = v
		return
	case *object.Exception:
		obj.Dict[name] = v
		return
	case *object.Function:
		// 函数对象支持任意属性（f.memo = {}、装饰器给 wrapper 挂属性）
		obj.SetAttr(name, v)
		return
	}
	if inst, ok := o.(*object.Instance); ok {
		inst.Dict[name] = v
		return
	}
	ip.raise(ip.excType("AttributeError"), "'%s' object has no attribute '%s' [go:%T]", object.TypeName(o), name, o)
}

// ==================== 下标 ====================

// evalSubscript 计算下标表达式
func (ip *Interp) evalSubscript(obj object.Object, slice ast.Expr, env *object.Env) object.Object {
	switch s := slice.(type) {
	case *ast.SliceExpr:
		sl := &object.Slice{}
		if s.Lower != nil {
			sl.Start = ip.evalExpr(s.Lower, env)
		}
		if s.Upper != nil {
			sl.Stop = ip.evalExpr(s.Upper, env)
		}
		if s.Step != nil {
			sl.Step = ip.evalExpr(s.Step, env)
		}
		return sl
	case *ast.Tuple:
		items := make([]object.Object, len(s.Elements))
		for i, e := range s.Elements {
			items[i] = ip.evalSubscript(obj, e, env)
		}
		return &object.Tuple{Items: items}
	}
	return ip.evalExpr(slice, env)
}

// normalizeIndex 处理负索引
func normalizeIndex(i int64, n int) (int, bool) {
	if i < 0 {
		i += int64(n)
	}
	if i < 0 || i >= int64(n) {
		return 0, false
	}
	return int(i), true
}

// sliceRange 计算切片范围
func (ip *Interp) sliceRange(sl *object.Slice, n int) (start, stop, step int) {
	step = 1
	if sl.Step != nil && sl.Step != object.None {
		v, ok := object.AsInt(sl.Step)
		if !ok {
			ip.TypeError("slice indices must be integers or None")
		}
		if v == 0 {
			ip.ValueError("slice step cannot be zero")
		}
		step = int(v)
	}
	if step > 0 {
		start = 0
		stop = n
		if sl.Start != nil && sl.Start != object.None {
			v, ok := object.AsInt(sl.Start)
			if !ok {
				ip.TypeError("slice indices must be integers or None")
			}
			start = int(v)
			if start < 0 {
				start += n
			}
			if start < 0 {
				start = 0
			}
			if start > n {
				start = n
			}
		}
		if sl.Stop != nil && sl.Stop != object.None {
			v, ok := object.AsInt(sl.Stop)
			if !ok {
				ip.TypeError("slice indices must be integers or None")
			}
			stop = int(v)
			if stop < 0 {
				stop += n
			}
			if stop < 0 {
				stop = 0
			}
			if stop > n {
				stop = n
			}
		}
		return
	}
	// 负步长
	start = n - 1
	stop = -1
	if sl.Start != nil && sl.Start != object.None {
		v, ok := object.AsInt(sl.Start)
		if !ok {
			ip.TypeError("slice indices must be integers or None")
		}
		start = int(v)
		if start < 0 {
			start += n
		}
		if start >= n {
			start = n - 1
		}
		if start < -1 {
			start = -1
		}
	}
	if sl.Stop != nil && sl.Stop != object.None {
		v, ok := object.AsInt(sl.Stop)
		if !ok {
			ip.TypeError("slice indices must be integers or None")
		}
		stop = int(v)
		if stop < 0 {
			stop += n
		}
		if stop >= n {
			stop = n - 1
		}
	}
	return
}

// GetItem 下标取值
func (ip *Interp) GetItem(o object.Object, idx object.Object) object.Object {
	// PEP 585：类型对象下标 → 泛型别名（list[int] / dict[str, int]）
	// CPython types.py 首行就有 `GenericAlias = type(list[int])`，不支持则整个 types.py 起不来。
	if t, ok := o.(*object.TypeObj); ok {
		return ip.makeGenericAlias(t, idx)
	}
	// 用户类：优先 __getitem__；其次 dict 子类（底层 _data + __missing__）
	if inst, ok := o.(*object.Instance); ok && inst.Class != nil {
		if m, found := ip.findDunder(inst.Class, "__getitem__"); found {
			return ip.Call(ip.bindMethod(inst, m), []object.Object{idx}, nil)
		}
		if dv, ok2 := inst.Dict["_data"].(*object.Dict); ok2 {
			if hit, found := dv.Get(idx); found {
				return hit
			}
			if m, found := ip.findDunder(inst.Class, "__missing__"); found {
				return ip.Call(ip.bindMethod(inst, m), []object.Object{idx}, nil)
			}
			ip.KeyErr(idx)
		}
	}
	switch v := o.(type) {
	case *object.Str:
		rs := []rune(v.V)
		if sl, ok := idx.(*object.Slice); ok {
			s, e, st := ip.sliceRange(sl, len(rs))
			var sb strings.Builder
			if st > 0 {
				for i := s; i < e; i += st {
					if i >= 0 && i < len(rs) {
						sb.WriteRune(rs[i])
					}
				}
			} else if st < 0 {
				for i := s; i > e; i += st {
					if i >= 0 && i < len(rs) {
						sb.WriteRune(rs[i])
					}
				}
			}
			return &object.Str{V: sb.String()}
		}
		i, ok := object.AsInt(idx)
		if !ok {
			ip.TypeError("string indices must be integers")
		}
		p, ok := normalizeIndex(i, len(rs))
		if !ok {
			ip.IndexErr("string index out of range")
		}
		return &object.Str{V: string(rs[p])}
	case *object.ByteArray:
		if sl, ok := idx.(*object.Slice); ok {
			s, e, st := ip.sliceRange(sl, len(v.V))
			var out []byte
			if st > 0 {
				for i := s; i < e; i += st {
					out = append(out, v.V[i])
				}
			} else if st < 0 {
				for i := s; i > e; i += st {
					if i >= 0 && i < len(v.V) {
						out = append(out, v.V[i])
					}
				}
			}
			return &object.ByteArray{V: out}
		}
		i, ok := object.AsInt(idx)
		if !ok {
			ip.TypeError("bytearray indices must be integers")
		}
		p, ok := normalizeIndex(i, len(v.V))
		if !ok {
			ip.IndexErr("bytearray index out of range")
		}
		return &object.Int{V: int64(v.V[p])}
	case *object.MemoryView:
		if sl, ok := idx.(*object.Slice); ok {
			s, e, st := ip.sliceRange(sl, len(v.V))
			var out []byte
			if st > 0 {
				for i := s; i < e; i += st {
					out = append(out, v.V[i])
				}
			}
			return &object.MemoryView{V: out}
		}
		i, ok := object.AsInt(idx)
		if !ok {
			ip.TypeError("memoryview indices must be integers")
		}
		p, ok := normalizeIndex(i, len(v.V))
		if !ok {
			ip.IndexErr("memoryview index out of range")
		}
		return &object.Int{V: int64(v.V[p])}
	case *object.Bytes:
		if sl, ok := idx.(*object.Slice); ok {
			s, e, st := ip.sliceRange(sl, len(v.V))
			var out []byte
			if st > 0 {
				for i := s; i < e; i += st {
					out = append(out, v.V[i])
				}
			} else {
				for i := s; i > e; i += st {
					if i >= 0 && i < len(v.V) {
						out = append(out, v.V[i])
					}
				}
			}
			return &object.Bytes{V: out}
		}
		i, ok := object.AsInt(idx)
		if !ok {
			ip.TypeError("byte indices must be integers")
		}
		p, ok := normalizeIndex(i, len(v.V))
		if !ok {
			ip.IndexErr("index out of range")
		}
		return &object.Int{V: int64(v.V[p])}
	case *object.List:
		if sl, ok := idx.(*object.Slice); ok {
			s, e, st := ip.sliceRange(sl, len(v.Items))
			var out []object.Object
			if st > 0 {
				for i := s; i < e; i += st {
					out = append(out, v.Items[i])
				}
			} else {
				for i := s; i > e; i += st {
					if i >= 0 && i < len(v.Items) {
						out = append(out, v.Items[i])
					}
				}
			}
			return &object.List{Items: out}
		}
		i, ok := object.AsInt(idx)
		if !ok {
			ip.TypeError("list indices must be integers or slices, not %s", object.TypeName(idx))
		}
		p, ok := normalizeIndex(i, len(v.Items))
		if !ok {
			ip.IndexErr("list index out of range")
		}
		return v.Items[p]
	case *object.Tuple:
		if sl, ok := idx.(*object.Slice); ok {
			s, e, st := ip.sliceRange(sl, len(v.Items))
			var out []object.Object
			if st > 0 {
				for i := s; i < e; i += st {
					out = append(out, v.Items[i])
				}
			} else {
				for i := s; i > e; i += st {
					if i >= 0 && i < len(v.Items) {
						out = append(out, v.Items[i])
					}
				}
			}
			return &object.Tuple{Items: out}
		}
		i, ok := object.AsInt(idx)
		if !ok {
			ip.TypeError("tuple indices must be integers or slices")
		}
		p, ok := normalizeIndex(i, len(v.Items))
		if !ok {
			ip.IndexErr("tuple index out of range")
		}
		return v.Items[p]
	case *object.DictProxy:
		if val, ok := v.M[idx.Str()]; ok {
			return val
		}
		ip.KeyErr(idx)
	case *object.Dict:
		if val, ok := v.Get(idx); ok {
			return val
		}
		if m, ok := ip.findDunder(object.DictType, "__missing__"); ok {
			_ = m
		}
		ip.KeyErr(idx)
	case *object.Set:
		ip.TypeError("'set' object is not subscriptable")
	case *object.Range:
		n := int(v.Len())
		if sl, ok := idx.(*object.Slice); ok {
			s, e, st := ip.sliceRange(sl, n)
			items := []object.Object{}
			if st > 0 {
				for i := s; i < e; i += st {
					items = append(items, &object.Int{V: v.At(int64(i))})
				}
			} else {
				for i := s; i > e; i += st {
					if i >= 0 && i < n {
						items = append(items, &object.Int{V: v.At(int64(i))})
					}
				}
			}
			return &object.List{Items: items}
		}
		i, ok := object.AsInt(idx)
		if !ok {
			ip.TypeError("range indices must be integers")
		}
		p, ok := normalizeIndex(i, n)
		if !ok {
			ip.IndexErr("range object index out of range")
		}
		return &object.Int{V: v.At(int64(p))}
	case *object.Instance:
		if m, ok := ip.findDunder(v.Class, "__getitem__"); ok {
			return ip.Call(ip.bindMethod(v, m), []object.Object{idx}, nil)
		}
	case *object.Exception:
		if m, ok := ip.findDunder(v.Class, "__getitem__"); ok {
			return ip.Call(ip.bindMethod(v, m), []object.Object{idx}, nil)
		}
	}
	ip.TypeError("'%s' object is not subscriptable", object.TypeName(o))
	return object.None
}

// SetItem 下标赋值
func (ip *Interp) SetItem(o object.Object, idx, val object.Object) {
	// 用户类：优先 __setitem__；其次 dict 子类（底层 _data）
	if inst, ok := o.(*object.Instance); ok && inst.Class != nil {
		if m, found := ip.findDunder(inst.Class, "__setitem__"); found {
			ip.Call(ip.bindMethod(inst, m), []object.Object{idx, val}, nil)
			return
		}
		if dv, ok2 := inst.Dict["_data"].(*object.Dict); ok2 {
			dv.Set(idx, val)
			return
		}
	}
	switch v := o.(type) {
	case *object.List:
		if sl, ok := idx.(*object.Slice); ok {
			s, e, st := ip.sliceRange(sl, len(v.Items))
			var repl []object.Object
			if lst, ok := val.(*object.List); ok {
				repl = lst.Items
			} else if tp, ok := val.(*object.Tuple); ok {
				repl = tp.Items
			} else {
				ip.TypeError("can only assign an iterable")
			}
			if st == 1 {
				out := append([]object.Object{}, v.Items[:s]...)
				out = append(out, repl...)
				out = append(out, v.Items[e:]...)
				v.Items = out
				return
			}
			// 扩展切片赋值长度必须一致
			var positions []int
			if st > 0 {
				for i := s; i < e; i += st {
					positions = append(positions, i)
				}
			} else {
				for i := s; i > e; i += st {
					positions = append(positions, i)
				}
			}
			if len(positions) != len(repl) {
				ip.ValueError("attempt to assign sequence of size %d to extended slice of size %d", len(repl), len(positions))
			}
			for i, p := range positions {
				v.Items[p] = repl[i]
			}
			return
		}
		i, ok := object.AsInt(idx)
		if !ok {
			ip.TypeError("list indices must be integers or slices")
		}
		p, ok := normalizeIndex(i, len(v.Items))
		if !ok {
			ip.IndexErr("list assignment index out of range")
		}
		v.Items[p] = val
		return
	case *object.DictProxy:
		v.M[idx.Str()] = val
		return
	case *object.Dict:
		if _, ok := object.Hash(idx); !ok {
			ip.TypeError("unhashable type: '%s'", object.TypeName(idx))
		}
		v.Set(idx, val)
		return
	case *object.Instance:
		if m, ok := ip.findDunder(v.Class, "__setitem__"); ok {
			ip.Call(ip.bindMethod(v, m), []object.Object{idx, val}, nil)
			return
		}
	}
	ip.TypeError("'%s' object does not support item assignment", object.TypeName(o))
}

// DelItem 下标删除
func (ip *Interp) DelItem(o object.Object, idx object.Object) {
	switch v := o.(type) {
	case *object.List:
		if sl, ok := idx.(*object.Slice); ok {
			s, e, st := ip.sliceRange(sl, len(v.Items))
			var out []object.Object
			if st == 1 {
				out = append(out, v.Items[:s]...)
				out = append(out, v.Items[e:]...)
				v.Items = out
				return
			}
			skip := map[int]bool{}
			if st > 0 {
				for i := s; i < e; i += st {
					skip[i] = true
				}
			} else {
				for i := s; i > e; i += st {
					if i >= 0 && i < len(v.Items) {
						skip[i] = true
					}
				}
			}
			for i, it := range v.Items {
				if !skip[i] {
					out = append(out, it)
				}
			}
			v.Items = out
			return
		}
		i, ok := object.AsInt(idx)
		if !ok {
			ip.TypeError("list indices must be integers")
		}
		p, ok := normalizeIndex(i, len(v.Items))
		if !ok {
			ip.IndexErr("list assignment index out of range")
		}
		v.Items = append(v.Items[:p], v.Items[p+1:]...)
		return
	case *object.Dict:
		if !v.Delete(idx) {
			ip.KeyErr(idx)
		}
		return
	case *object.Instance:
		if m, ok := ip.findDunder(v.Class, "__delitem__"); ok {
			ip.Call(ip.bindMethod(v, m), []object.Object{idx}, nil)
			return
		}
	}
	ip.TypeError("'%s' object doesn't support item deletion", object.TypeName(o))
}

// ==================== 迭代 ====================

// Iter 获取迭代器
func (ip *Interp) Iter(o object.Object) object.Object {
	switch v := o.(type) {
	case *object.List:
		return &object.Iterator{Items: v.Items}
	case *object.Tuple:
		return &object.Iterator{Items: v.Items}
	case *object.Str:
		rs := []rune(v.V)
		items := make([]object.Object, len(rs))
		for i, r := range rs {
			items[i] = &object.Str{V: string(r)}
		}
		return &object.Iterator{Items: items}
	case *object.Bytes:
		items := make([]object.Object, len(v.V))
		for i, b := range v.V {
			items[i] = &object.Int{V: int64(b)}
		}
		return &object.Iterator{Items: items}
	case *object.ByteArray:
		items := make([]object.Object, len(v.V))
		for i, b := range v.V {
			items[i] = &object.Int{V: int64(b)}
		}
		return &object.Iterator{Items: items}
	case *object.MemoryView:
		items := make([]object.Object, len(v.V))
		for i, b := range v.V {
			items[i] = &object.Int{V: int64(b)}
		}
		return &object.Iterator{Items: items}
	case *object.Set:
		return &object.Iterator{Items: v.Items}
	case *object.Dict:
		return &object.Iterator{Items: v.Keys}
	case *object.Range:
		n := v.Len()
		items := make([]object.Object, n)
		for i := int64(0); i < n; i++ {
			items[i] = &object.Int{V: v.At(i)}
		}
		return &object.Iterator{Items: items}
	case *object.Iterator:
		return v
	case *object.Generator:
		return v
	case *object.TypeObj:
		// 枚举类：list(Color) 走类上的 __iter__（EnumType.__new__ 装配）
		if m, ok := ip.findDunder(v, "__iter__"); ok {
			r := ip.Call(ip.bindMethod(v, m), nil, nil)
			switch rv := r.(type) {
			case *object.List:
				return &object.Iterator{Items: rv.Items}
			case *object.Tuple:
				return &object.Iterator{Items: rv.Items}
			}
			return r
		}
	case *object.Instance:
		if m, ok := ip.findDunder(v.Class, "__iter__"); ok {
			r := ip.Call(ip.bindMethod(v, m), nil, nil)
			return r
		}
		if m, ok := ip.findDunder(v.Class, "__getitem__"); ok {
			_ = m
			// 序列协议：按索引迭代
			res := &object.List{}
			for i := int64(0); ; i++ {
				pending := ip.protect(func() {
					item := ip.Call(ip.bindMethod(v, m), []object.Object{&object.Int{V: i}}, nil)
					res.Items = append(res.Items, item)
				})
				if pending != nil {
					break
				}
				if i > 100000 {
					break
				}
			}
			return &object.Iterator{Items: res.Items}
		}
	}
	if m, ok := ip.findDunder(o.Type(), "__iter__"); ok {
		return ip.Call(ip.bindMethod(o, m), nil, nil)
	}
	ip.TypeError("'%s' object is not iterable", object.TypeName(o))
	return nil
}

// IterNext 推进迭代器
func (ip *Interp) IterNext(it object.Object) (object.Object, bool) {
	switch v := it.(type) {
	case *object.Iterator:
		if v.Gen != nil {
			val, done, err := v.Gen.Next()
			if err != nil {
				ip.raiseFromErr(err)
			}
			if done {
				return nil, false
			}
			return val, true
		}
		if v.Pos >= len(v.Items) {
			return nil, false
		}
		val := v.Items[v.Pos]
		v.Pos++
		return val, true
	case *object.Generator:
		val, done, err := v.Next()
		if err != nil {
			ip.raiseFromErr(err)
		}
		if done {
			return nil, false
		}
		return val, true
	case *object.Instance:
		if m, ok := ip.findDunder(v.Class, "__next__"); ok {
			var val object.Object
			pending := ip.protect(func() {
				val = ip.Call(ip.bindMethod(v, m), nil, nil)
			})
			if pending != nil {
				if pending.Exc.Class.Name == "StopIteration" {
					return nil, false
				}
				ip.RaiseObj(pending.Exc)
			}
			return val, true
		}
	case *object.List:
		return ip.IterNext(ip.Iter(v))
	}
	ip.TypeError("'%s' object is not an iterator", object.TypeName(it))
	return nil, false
}

func (ip *Interp) raiseFromErr(err error) {
	if pe, ok := err.(*PyError); ok {
		panic(pe)
	}
	ip.RuntimeErr("%v", err)
}

// ToSlice 把可迭代对象转为切片
func (ip *Interp) ToSlice(o object.Object) []object.Object {
	switch v := o.(type) {
	case *object.List:
		return v.Items
	case *object.Tuple:
		return v.Items
	}
	it := ip.Iter(o)
	// 防死循环：Iter 可能返回 List/Tuple（如枚举 __iter__ 直接给列表），
	// 这类值的 IterNext 每次都会从 Pos=0 重建迭代器 → 必须先摊平再迭代。
	if lst, ok := it.(*object.List); ok {
		return lst.Items
	}
	if tup, ok := it.(*object.Tuple); ok {
		return tup.Items
	}
	var out []object.Object
	for {
		val, ok := ip.IterNext(it)
		if !ok {
			break
		}
		out = append(out, val)
	}
	return out
}

// ==================== 赋值 ====================

// assign 赋值到目标
func (ip *Interp) assign(target ast.Expr, val object.Object, env *object.Env) {
	switch t := target.(type) {
	case *ast.Name:
		ip.bindName(t.Id, val, env)
	case *ast.Tuple:
		ip.assignUnpack(t.Elements, val, env)
	case *ast.List:
		ip.assignUnpack(t.Elements, val, env)
	case *ast.Starred:
		ip.assign(t.Value, val, env)
	case *ast.Attribute:
		obj := ip.evalExpr(t.Value, env)
		ip.SetAttr(obj, t.Attr, val)
	case *ast.Subscript:
		obj := ip.evalExpr(t.Value, env)
		idx := ip.evalSubscript(obj, t.Slice, env)
		ip.SetItem(obj, idx, val)
	default:
		ip.RuntimeErr("非法赋值目标 %T", target)
	}
}

// assignUnpack 序列解包赋值
func (ip *Interp) assignUnpack(targets []ast.Expr, val object.Object, env *object.Env) {
	items := ip.ToSlice(val)
	starIdx := -1
	for i, t := range targets {
		if _, ok := t.(*ast.Starred); ok {
			starIdx = i
			break
		}
	}
	if starIdx < 0 {
		if len(items) != len(targets) {
			n := len(targets)
			if len(items) < n {
				ip.ValueError("not enough values to unpack (expected %d, got %d)", n, len(items))
			}
			ip.ValueError("too many values to unpack (expected %d)", n)
		}
		for i, t := range targets {
			ip.assign(t, items[i], env)
		}
		return
	}
	before := targets[:starIdx]
	after := targets[starIdx+1:]
	if len(items) < len(before)+len(after) {
		ip.ValueError("not enough values to unpack (expected at least %d, got %d)", len(before)+len(after), len(items))
	}
	for i, t := range before {
		ip.assign(t, items[i], env)
	}
	mid := items[len(before) : len(items)-len(after)]
	ip.assign(targets[starIdx].(*ast.Starred).Value, &object.List{Items: mid}, env)
	for i, t := range after {
		ip.assign(t, items[len(items)-len(after)+i], env)
	}
}

var _ = strings.TrimSpace
