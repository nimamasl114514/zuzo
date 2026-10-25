package runtime

// native_subclass.go — 原生子类实例（enum.IntEnum / StrEnum / IntFlag）的通用解包
//
// 背景：CPython 的 IntEnum 是 int 的真子类，`HTTPStatus.OK == 200`、`int(status)`、
// `status >= 400`、`"%d" % status` 全部原生可用。zuzo 的对象模型里枚举成员是
// *object.Instance，没有原生值语义，上述运算全部 TypeError，直接卡死 urllib3/requests。
//
// 做法：成员实例在创建时打上 `_enum_member_` 标记，本文件提供 nativeValue()/unwrapNative()
// 把它们解包成底层原生值（int/str/float），在 Eq / Cmp / BinOp / toInt / 格式化入口统一调用。
// 只认显式标记，不碰普通用户类（`class A(int)` 语义保持不变）。
//
// 另一半：CPython 的 `_simple_enum` 里成员由 `int.__new__(cls, value)` / `str.__new__(cls, value)`
// 构造（见 http/__init__.py:27），因此这里还要给 int / str 类型挂上 __new__：
//   - cls 就是 int/str 本身 → 返回原生值
//   - cls 是子类（枚举类）   → 返回 Instance，并把 value 预置进 Dict

import (
	"zuzo/py/object"
)

// nativeValue 若 o 是枚举成员实例，返回其底层原生值；否则 ok=false。
func (ip *Interp) nativeValue(o object.Object) (object.Object, bool) {
	inst, ok := o.(*object.Instance)
	if !ok || inst.Class == nil {
		return nil, false
	}
	if _, flag := inst.Dict["_enum_member_"]; !flag {
		return nil, false
	}
	v, ok := inst.Dict["value"]
	if !ok {
		return nil, false
	}
	switch v.(type) {
	case *object.Int, *object.Str, *object.Float, *object.Bool:
		return v, true
	}
	return nil, false
}

// unwrapNative 解包；非枚举成员原样返回（供 Eq / Cmp / BinOp / 格式化入口调用）。
func (ip *Interp) unwrapNative(o object.Object) object.Object {
	if v, ok := ip.nativeValue(o); ok {
		return v
	}
	return o
}

// installNativeScalarNew 给 int / str 挂 __new__（幂等）。
// CPython: int.__new__(cls, x) 在 cls 是子类时返回子类实例——枚举成员就是这样造出来的。
func installNativeScalarNew() {
	if _, ok := object.IntType.Dict["__new__"]; ok {
		return
	}
	if object.IntType.Dict == nil {
		object.IntType.Dict = map[string]object.Object{}
	}
	object.IntType.Dict["__new__"] = &object.Builtin{Name: "__new__", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		var raw int64
		if len(args) > 1 {
			switch v := args[1].(type) {
			case *object.Int:
				raw = v.V
			case *object.Bool:
				if v.V {
					raw = 1
				}
			case *object.Float:
				raw = int64(v.V)
			}
		}
		val := &object.Int{V: raw}
		cls, _ := args[0].(*object.TypeObj)
		if cls == nil || cls == object.IntType || cls.Builtin == object.BT_Int {
			return val, nil
		}
		return &object.Instance{Class: cls, Dict: map[string]object.Object{"value": val}}, nil
	}}

	if object.StrType.Dict == nil {
		object.StrType.Dict = map[string]object.Object{}
	}
	object.StrType.Dict["__new__"] = &object.Builtin{Name: "__new__", Fn: func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := ""
		if len(args) > 1 {
			s = args[1].Str()
		}
		val := &object.Str{V: s}
		cls, _ := args[0].(*object.TypeObj)
		if cls == nil || cls == object.StrType || cls.Builtin == object.BT_Str {
			return val, nil
		}
		return &object.Instance{Class: cls, Dict: map[string]object.Object{"value": val}}, nil
	}}
}
