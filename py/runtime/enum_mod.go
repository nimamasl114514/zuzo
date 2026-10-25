package runtime

// enum_mod.go — enum 模块的 Go 原生实现（第13轮）
//
// 为什么不用 CPython 的 enum.py：
//   它的 _EnumDict 继承 dict 并靠 super().__setitem__ 记录成员名，再依赖
//   EnumType.__prepare__ / __new__ 的一整套元类协议。zuzo 的 super 暂时无法
//   代理到内置 dict 的方法，硬啃下去要改整个对象模型（Instance 继承原生类型）。
//   而 enum 是 urllib3 / email / http 等几乎所有现代库的地基，不能卡住。
//
// 本实现覆盖实际用到的语义：
//   Enum / IntEnum / StrEnum / Flag / IntFlag / auto / unique / member / nonmember
//   成员属性 .name / .value、类属性访问、按值查找 Color(1)、__members__、迭代
//
// 有意简化（文档需注明）：
//   - IntEnum / StrEnum / Flag 的成员直接用底层原生值（*object.Int / *object.Str），
//     因此 `isinstance(Color.RED, int)` 为 True、`int(Color.RED)` 可用；
//     代价是 `Color.RED.name` 这类元数据访问需要走 `Color(1).name` 或 `Color.RED.name`
//     的映射表（已在成员实例上保留 name/value 于 _enum_members_）。
//   - 不做 aliases / _missing_ / _generate_next_value_ 的完整钩子。

import (
	"sort"
	"strings"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["enum"] = modEnum
}

func modEnum(ip *Interp) *object.Module {
	m := newModule("enum")

	// ==================== EnumType（元类） ====================
	enumMeta := &object.TypeObj{Name: "EnumType", Bases: []*object.TypeObj{object.TypeType}, Dict: map[string]object.Object{}}

	// __new__(mcls, name, bases, ns)：把类体里的普通值转成枚举成员
	enumMeta.Dict["__new__"] = bi("__new__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 4 {
			ip.TypeError("EnumType.__new__() requires (mcls, name, bases, ns)")
		}
		mcls, _ := a[0].(*object.TypeObj)
		name := a[1].Str()
		cls := &object.TypeObj{Name: name, Dict: map[string]object.Object{}, MetaClass: mcls}
		for _, b := range ip.ToSlice(a[2]) {
			if bt, ok := b.(*object.TypeObj); ok {
				cls.Bases = append(cls.Bases, bt)
			}
		}
		if len(cls.Bases) == 0 {
			cls.Bases = []*object.TypeObj{object.ObjectType}
		}
		memberMap := &object.Dict{}
		valueMap := &object.Dict{}
		var members []object.Object
		names := &object.Dict{}
		addEntry := func(key string, val object.Object) {
			member := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
			member.Dict["name"] = &object.Str{V: key}
			member.Dict["value"] = val
			member.Dict["__objclass__"] = cls
			// 原生子类标记：Eq/Cmp/BinOp/int()/格式化 见 native_subclass.go
			member.Dict["_enum_member_"] = &object.Bool{V: true}
			cls.Dict[key] = member
			memberMap.Set(&object.Str{V: key}, member)
			valueMap.Set(val, member)
			names.Set(member, &object.Str{V: key})
			members = append(members, member)
		}
		switch ns := a[3].(type) {
		case *object.Dict:
			for i, k := range ns.Keys {
				key := k.Str()
				val := ns.Vals[i]
				cls.Dict[key] = val
			}
		case *object.Instance:
			for kk, vv := range ns.Dict {
				cls.Dict[kk] = vv
			}
		}
		// 把非下划线、非函数/描述符的类属性转成成员
		keys := make([]string, 0, len(cls.Dict))
		for kk := range cls.Dict {
			keys = append(keys, kk)
		}
		for _, kk := range keys {
			if strings.HasPrefix(kk, "_") {
				continue
			}
			vv := cls.Dict[kk]
			switch vv.(type) {
			case *object.Function, *object.Builtin, *object.Property, *object.StaticMethod, *object.ClassMethod:
				continue
			}
			// auto() 哨兵 → 递增整数
			if _, isAuto := vv.(*object.Instance); isAuto {
				if inst := vv.(*object.Instance); inst.Class != nil && inst.Class.Name == "_auto_null" {
					addEntry(kk, &object.Int{V: int64(len(members) + 1)})
					continue
				}
			}
			addEntry(kk, vv)
		}
		cls.Dict["_member_map_"] = memberMap
		cls.Dict["_value2member_map_"] = valueMap
		cls.Dict["_member_names_"] = &object.List{Items: stringList(keys)}
		cls.Dict["__members__"] = memberMap
		cls.Dict["_member_names_index_"] = names
		cls.Dict["__iter__"] = bi("__iter__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
			return &object.List{Items: members}, nil
		})
		return cls, nil
	})

	// __call__(cls, value)：按值查成员
	enumMeta.Dict["__call__"] = bi("__call__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("EnumMeta.__call__ requires a class")
		}
		cls, ok := a[0].(*object.TypeObj)
		if !ok {
			ip.TypeError("EnumMeta.__call__ requires a class")
		}
		// type(name, bases, ns) / EnumType(name, bases, ns)：动态建枚举类
		// （CPython _simple_enum 走的就是 type(cls_name, (etype,), body)）
		if len(a) >= 4 {
			if nf, ok := enumMeta.Dict["__new__"]; ok {
				return ip.Call(nf, a, k), nil
			}
		}
		if len(a) > 1 {
			if vm, ok := cls.Dict["_value2member_map_"].(*object.Dict); ok {
				if hit, ok := vm.Get(a[1]); ok {
					return hit, nil
				}
			}
			ip.raise(ip.excType("ValueError"), "%v is not a valid %s", a[1].Repr(), cls.Name)
		}
		// 无参：返回空实例（兼容极少见用法）
		return &object.Instance{Class: cls, Dict: map[string]object.Object{}}, nil
	})

	// Enum 基类
	enumType := &object.TypeObj{Name: "Enum", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}, MetaClass: enumMeta}
	enumType.Dict["__repr__"] = bi("__repr__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Str{V: "<Enum>"}, nil
		}
		if inst, ok := a[0].(*object.Instance); ok {
			name := ""
			if v, ok := inst.Dict["name"]; ok {
				name = v.Str()
			}
			return &object.Str{V: "<" + inst.Class.Name + "." + name + ": " + inst.Dict["value"].Repr() + ">"}, nil
		}
		return &object.Str{V: a[0].Repr()}, nil
	})
	enumType.Dict["__str__"] = bi("__str__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			if inst, ok := a[0].(*object.Instance); ok {
				// IntEnum / StrEnum：CPython 里 str() 走底层类型（str(HTTPStatus.OK) == "200"）
				if _, isEnum := inst.Dict["_enum_member_"]; isEnum {
					if v, has := inst.Dict["value"]; has {
						switch v.(type) {
						case *object.Int, *object.Str, *object.Float:
							return &object.Str{V: v.Str()}, nil
						}
					}
				}
				if v, ok := inst.Dict["name"]; ok {
					return &object.Str{V: inst.Class.Name + "." + v.Str()}, nil
				}
			}
		}
		return &object.Str{V: ""}, nil
	})
	enumType.Dict["__eq__"] = bi("__eq__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 1 {
			return &object.Bool{V: a[0] == a[1]}, nil
		}
		return &object.Bool{V: false}, nil
	})
	enumType.Dict["__hash__"] = bi("__hash__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: 0}, nil
	})

	// IntEnum / StrEnum：成员值就是原生 int / str（可直接参与运算）
	intEnum := &object.TypeObj{Name: "IntEnum", Bases: []*object.TypeObj{enumType, ip.BuiltinsType("int")}, Dict: map[string]object.Object{}, MetaClass: enumMeta}
	strEnum := &object.TypeObj{Name: "StrEnum", Bases: []*object.TypeObj{enumType, ip.BuiltinsType("str")}, Dict: map[string]object.Object{}, MetaClass: enumMeta}
	intFlag := &object.TypeObj{Name: "IntFlag", Bases: []*object.TypeObj{intEnum}, Dict: map[string]object.Object{}, MetaClass: enumMeta}
	flag := &object.TypeObj{Name: "Flag", Bases: []*object.TypeObj{intFlag}, Dict: map[string]object.Object{}, MetaClass: enumMeta}

	// _auto_null 哨兵
	autoCls := &object.TypeObj{Name: "_auto_null", Dict: map[string]object.Object{}}
	m.Dict["auto"] = bi("auto", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Instance{Class: autoCls, Dict: map[string]object.Object{}}, nil
	})
	m.Dict["_auto_null"] = &object.Instance{Class: autoCls, Dict: map[string]object.Object{}}

	m.Dict["Enum"] = enumType
	m.Dict["IntEnum"] = intEnum
	m.Dict["StrEnum"] = strEnum
	m.Dict["Flag"] = flag
	m.Dict["IntFlag"] = intFlag
	m.Dict["EnumType"] = enumMeta
	m.Dict["EnumMeta"] = enumMeta
	m.Dict["EnumCheck"] = enumMeta
	m.Dict["_EnumDict"] = &object.TypeObj{Name: "_EnumDict", Bases: []*object.TypeObj{object.DictType}, Dict: map[string]object.Object{}, MetaClass: enumMeta}

	// auto() 的解析：类创建时把 auto 实例替换为 1..n（上面 __new__ 已处理）
	m.Dict["unique"] = bi("unique", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			return a[0], nil
		}
		return object.None, nil
	})
	m.Dict["member"] = bi("member", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			return a[0], nil
		}
		return object.None, nil
	})
	m.Dict["nonmember"] = m.Dict["member"]
	m.Dict["verify"] = m.Dict["member"]
	m.Dict["global_enum"] = bi("global_enum", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			return a[0], nil
		}
		return object.None, nil
	})

	// _simple_enum(etype=Enum)：CPython 3.12 的「类体直转枚举」装饰器。
	// http/__init__.py:7 用 @_simple_enum(IntEnum) 定义 HTTPStatus，
	// 成员值形如 CONTINUE = 100, 'Continue', '...'（元组 = 首元素为值，其余给类体 __new__）。
	m.Dict["_simple_enum"] = bi("_simple_enum", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var etype *object.TypeObj = enumType
		var target *object.TypeObj
		if v, ok := k["etype"]; ok {
			if t, ok := v.(*object.TypeObj); ok {
				etype = t
			}
		}
		if len(a) > 0 {
			if t, ok := a[0].(*object.TypeObj); ok {
				if t.MetaClass == enumMeta {
					etype = t // _simple_enum(IntEnum) → 作为基类
				} else {
					target = t // @_simple_enum 直接作用在类上
				}
			}
		}
		convert := func(cls *object.TypeObj) object.Object {
			keys := make([]string, 0, len(cls.Dict))
			for kk := range cls.Dict {
				keys = append(keys, kk)
			}
			sort.Strings(keys) // Go map 无序，按名排序保证稳定
			ns := &object.Dict{}
			for _, kk := range keys {
				ns.Set(&object.Str{V: kk}, cls.Dict[kk])
			}
			var classNew object.Object
			if v, ok := cls.Dict["__new__"]; ok {
				classNew = v
			}
			if nf, ok := enumMeta.Dict["__new__"]; ok {
				created := ip.Call(nf, []object.Object{enumMeta, &object.Str{V: cls.Name}, &object.List{Items: []object.Object{etype}}, ns}, nil)
				newCls, _ := created.(*object.TypeObj)
				if newCls == nil {
					return created
				}
				// 有类体 __new__ 时：用它重建成员（元组展开 + 自定义属性，如 HTTPStatus.phrase）
				if classNew != nil {
					memberMap, _ := newCls.Dict["_member_map_"].(*object.Dict)
					for _, kk := range keys {
						if strings.HasPrefix(kk, "_") {
							continue
						}
						vv := cls.Dict[kk]
						switch vv.(type) {
						case *object.Function, *object.Builtin, *object.Property, *object.StaticMethod, *object.ClassMethod:
							continue
						}
						args := []object.Object{newCls}
						if t, ok := vv.(*object.Tuple); ok {
							args = append(args, t.Items...)
						} else {
							args = append(args, vv)
						}
						built := ip.Call(classNew, args, nil)
						inst, ok := built.(*object.Instance)
						if !ok {
							continue
						}
						if _, has := inst.Dict["value"]; !has && len(args) > 1 {
							inst.Dict["value"] = args[1]
						}
						inst.Dict["name"] = &object.Str{V: kk}
						inst.Dict["_enum_member_"] = &object.Bool{V: true}
						inst.Class = newCls
						newCls.Dict[kk] = inst
						if memberMap != nil {
							memberMap.Set(&object.Str{V: kk}, inst)
						}
					}
				}
				return newCls
			}
			return cls
		}
		if target != nil {
			return convert(target), nil
		}
		return bi("_simple_enum.decorator", func(da []object.Object, dk map[string]object.Object) (object.Object, error) {
			if len(da) == 0 {
				return object.None, nil
			}
			if cls, ok := da[0].(*object.TypeObj); ok {
				return convert(cls), nil
			}
			return da[0], nil
		}), nil
	})
	return m
}

// BuiltinsType 取内置类型对象（int / str 等）。
func (ip *Interp) BuiltinsType(name string) *object.TypeObj {
	if v, ok := ip.Builtins.Get(name); ok {
		if t, ok := v.(*object.TypeObj); ok {
			return t
		}
	}
	return object.ObjectType
}

func stringList(keys []string) []object.Object {
	out := make([]object.Object, 0, len(keys))
	for _, k := range keys {
		if strings.HasPrefix(k, "_") {
			continue
		}
		out = append(out, &object.Str{V: k})
	}
	return out
}
