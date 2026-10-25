package runtime

// weakref_mod.go — _weakref / weakref 的 Go 实现（第13轮）
//
// CPython 的 _weakrefset.py（abc / email / importlib 全链依赖）只需要
// `from _weakref import ref` 与 `ReferenceType`。zuzo 没有 GC 弱引用语义，
// 这里用「强引用包装」实现：语义上是弱引用，行为上不会提前回收（内存换正确性）。

import (
	"zuzo/py/object"
)

func init() {
	builtinModuleInits["_weakref"] = modWeakref
	builtinModuleInits["weakref"] = modWeakrefPublic
}

var weakRefType = &object.TypeObj{Name: "ReferenceType", Dict: map[string]object.Object{}}

func newWeakRef(target object.Object) *object.Instance {
	inst := &object.Instance{Class: weakRefType, Dict: map[string]object.Object{}}
	inst.Dict["__target__"] = target
	return inst
}

func modWeakref(ip *Interp) *object.Module {
	m := newModule("_weakref")
	m.Dict["ref"] = bi("ref", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("ref() requires an object")
		}
		return newWeakRef(a[0]), nil
	})
	m.Dict["ReferenceType"] = weakRefType
	m.Dict["proxy"] = bi("proxy", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("proxy() requires an object")
		}
		return a[0], nil
	})
	m.Dict["getweakrefcount"] = bi("getweakrefcount", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: 0}, nil
	})
	m.Dict["getweakrefs"] = bi("getweakrefs", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.List{}, nil
	})
	return m
}

func modWeakrefPublic(ip *Interp) *object.Module {
	m := newModule("weakref")
	base := modWeakref(ip)
	for k, v := range base.Dict {
		m.Dict[k] = v
	}
	m.Dict["ProxyType"] = weakRefType
	m.Dict["CallableProxyType"] = weakRefType

	// WeakSet / WeakValueDictionary / WeakKeyDictionary：退化为普通集合
	weakSet := &object.TypeObj{Name: "WeakSet", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	weakSet.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			if inst, ok := a[0].(*object.Instance); ok {
				items := []object.Object{}
				if len(a) > 1 {
					items = ip.ToSlice(a[1])
				}
				inst.Dict["_items"] = &object.List{Items: items}
			}
		}
		return object.None, nil
	})
	weakSet.Dict["add"] = bi("add", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 1 {
			if inst, ok := a[0].(*object.Instance); ok {
				if lst, ok := inst.Dict["_items"].(*object.List); ok {
					lst.Items = append(lst.Items, a[1])
				}
			}
		}
		return object.None, nil
	})
	weakSet.Dict["__iter__"] = bi("__iter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if inst, ok := a[0].(*object.Instance); ok {
			if lst, ok := inst.Dict["_items"].(*object.List); ok {
				return lst, nil
			}
		}
		return &object.List{}, nil
	})
	m.Dict["WeakSet"] = weakSet
	m.Dict["WeakValueDictionary"] = ip.BuiltinsType("dict")
	m.Dict["WeakKeyDictionary"] = ip.BuiltinsType("dict")
	m.Dict["finalize"] = bi("finalize", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	return m
}
