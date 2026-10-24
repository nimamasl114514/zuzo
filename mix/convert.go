package mix

import (
	"fmt"
	"math/big"

	"zuzo/evaluator"
	"zuzo/object"
	pyobj "zuzo/py/object"
	pyrt "zuzo/py/runtime"
)

// ==================== zuzo -> Python ====================

// ToPy 把 zuzo 对象转换为 Python 对象。
// 标量与容器为深拷贝；函数/内置转为 Python 可调用代理（调用时跨回 zuzo 侧执行）。
func (b *Bridge) ToPy(o object.Object) pyobj.Object {
	if o == nil {
		return pyobj.None
	}
	switch v := o.(type) {
	case *object.Integer:
		return pyobj.NewBigInt(big.NewInt(v.Value))
	case *object.Float:
		return &pyobj.Float{V: v.Value}
	case *object.String:
		return &pyobj.Str{V: v.Value}
	case *object.Boolean:
		if v.Value {
			return pyobj.TrueObj
		}
		return pyobj.FalseObj
	case *object.Null:
		return pyobj.None
	case *object.Array:
		items := make([]pyobj.Object, 0, len(v.Elements))
		for _, e := range v.Elements {
			items = append(items, b.ToPy(e))
		}
		return &pyobj.List{Items: items}
	case *object.Hash:
		d := &pyobj.Dict{}
		for _, hk := range v.Keys {
			pair, ok := v.Pairs[hk]
			if !ok {
				continue
			}
			d.Keys = append(d.Keys, b.ToPy(pair.Key))
			d.Vals = append(d.Vals, b.ToPy(pair.Value))
		}
		return d
	case *object.Function:
		return b.zuzoFuncProxy(v.Name, v)
	case *object.Builtin:
		return b.zuzoFuncProxy(v.Name, v)
	case *object.Error:
		return &pyobj.Str{V: v.Message}
	default:
		return &pyobj.Str{V: o.Inspect()}
	}
}

// zuzoFuncProxy 把 zuzo 可调用对象包装成 Python Builtin
func (b *Bridge) zuzoFuncProxy(name string, fn object.Object) pyobj.Object {
	return &pyobj.Builtin{
		Name: name,
		Fn: func(args []pyobj.Object, kwargs map[string]pyobj.Object) (pyobj.Object, error) {
			zargs := make([]object.Object, 0, len(args))
			for _, a := range args {
				zargs = append(zargs, b.ToZuzo(a))
			}
			res := evaluator.CallValue(fn, zargs)
			if errObj, ok := res.(*object.Error); ok {
				return nil, fmt.Errorf("%s", errObj.Message)
			}
			return b.ToPy(res), nil
		},
	}
}

// ==================== Python -> zuzo ====================

// ToZuzo 把 Python 对象转换为 zuzo 对象。
// 标量与容器为深拷贝；函数/内置转为 zuzo 内置函数代理（调用时跨回 Python 侧执行）。
func (b *Bridge) ToZuzo(o pyobj.Object) object.Object {
	if o == nil || o == pyobj.None {
		return evaluator.NULL
	}
	switch v := o.(type) {
	case *pyobj.NoneObj:
		return evaluator.NULL
	case *pyobj.Int:
		if v.Big != nil {
			if v.Big.IsInt64() {
				return &object.Integer{Value: v.Big.Int64()}
			}
			// 超出 int64 精度：降级为字符串以免静默丢精度
			return &object.String{Value: v.Big.String()}
		}
		return &object.Integer{Value: v.V}
	case *pyobj.Float:
		return &object.Float{Value: v.V}
	case *pyobj.Str:
		return &object.String{Value: v.V}
	case *pyobj.Bool:
		return &object.Boolean{Value: v.V}
	case *pyobj.List:
		elems := make([]object.Object, 0, len(v.Items))
		for _, it := range v.Items {
			elems = append(elems, b.ToZuzo(it))
		}
		return &object.Array{Elements: elems}
	case *pyobj.Tuple:
		elems := make([]object.Object, 0, len(v.Items))
		for _, it := range v.Items {
			elems = append(elems, b.ToZuzo(it))
		}
		return &object.Array{Elements: elems}
	case *pyobj.Dict:
		h := &object.Hash{Pairs: map[object.HashKey]object.HashPair{}}
		for i, k := range v.Keys {
			if i >= len(v.Vals) {
				break
			}
			zk := b.ToZuzo(k)
			if _, ok := zk.(object.Hashable); !ok {
				continue
			}
			h.SetKey(zk, b.ToZuzo(v.Vals[i]))
		}
		return h
	case *pyobj.Function:
		return b.pyFuncProxy(v.Name, v)
	case *pyobj.Builtin:
		return b.pyFuncProxy(v.Name, v)
	case *pyobj.BoundMethod:
		return b.pyFuncProxy("method", v)
	case *pyobj.Module, *pyobj.TypeObj, *pyobj.Instance:
		// 模块 / 类 / 实例 无 zuzo 对应语义：转成只读摘要字符串，不参与运算
		return &object.String{Value: v.Repr()}
	default:
		return &object.String{Value: o.Repr()}
	}
}

// pyFuncProxy 把 Python 可调用对象包装成 zuzo 内置函数
func (b *Bridge) pyFuncProxy(name string, fn pyobj.Object) object.Object {
	return &object.Builtin{
		Name: name,
		Fn: func(args ...object.Object) object.Object {
			pyargs := make([]pyobj.Object, 0, len(args))
			for _, a := range args {
				pyargs = append(pyargs, b.ToPy(a))
			}
			res, err := b.CallPy(fn, pyargs)
			if err != nil {
				return &object.Error{Code: "E900", Message: err.Error()}
			}
			return b.ToZuzo(res)
		},
	}
}

// ==================== 跨语言调用 ====================

// CallPy 调用 Python 可调用对象，把 panic 形式的 Python 异常转换为 error
func (b *Bridge) CallPy(fn pyobj.Object, args []pyobj.Object) (res pyobj.Object, err error) {
	defer func() {
		if r := recover(); r != nil {
			switch e := r.(type) {
			case *pyrt.PyError:
				err = e
			case error:
				err = e
			default:
				err = fmt.Errorf("%v", e)
			}
		}
	}()
	return b.IP.Call(fn, args, nil), nil
}

// LookupPy 按名在 Python 全局与内置作用域中查找对象
func (b *Bridge) LookupPy(name string) (pyobj.Object, bool) {
	if v, ok := b.IP.Globals.Get(name); ok {
		return v, true
	}
	if v, ok := b.IP.Builtins.Get(name); ok {
		return v, true
	}
	return nil, false
}

// LookupZuzo 按名在 zuzo 全局环境中查找对象
func (b *Bridge) LookupZuzo(name string) (object.Object, bool) {
	return b.Env.Get(name)
}
