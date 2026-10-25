package runtime

// namedtuple.go — typing.NamedTuple 支持（第14轮）
//
// urllib3 两种写法都用：
//   class PoolKey(typing.NamedTuple): key_host: str; key_port: int
//   Url = typing.NamedTuple("Url", [("scheme", str), ...])
// 实现口径：字段名按类体注解顺序（ClassAnnotations 有序），实例用普通 Dict 承载字段值，
// 类上装 _fields / __init__ / __getitem__ / __iter__ / __len__ / _asdict / _replace / __repr__。
// 不继承原生 tuple（会走 constructBuiltinType 丢字段），isinstance(x, tuple) 为 False——已知偏差。

import (
	"runtime/debug"
	"strings"

	"zuzo/py/object"
)

// buildNamedTuple 给 cls 装上一套 namedtuple 协议（幂等）。
func (ip *Interp) buildNamedTuple(cls *object.TypeObj, names []string) {
	items := make([]object.Object, len(names))
	for i, n := range names {
		items[i] = &object.Str{V: n}
	}
	fields := &object.Tuple{Items: items}
	cls.Dict["_fields"] = fields
	cls.Dict["__slots__"] = fields

	// 默认值：类体里 `x: int = 1` 已经把值写进 cls.Dict，这里记一份用于缺省填充
	defaults := map[string]object.Object{}
	for _, n := range names {
		if v, ok := cls.Dict[n]; ok {
			defaults[n] = v
		}
	}

	cls.Dict["__init__"] = bi("__init__", func(a []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return object.None, nil
		}
		self, ok := a[0].(*object.Instance)
		if !ok {
			return object.None, nil
		}
		pos := a[1:]
		for i, n := range names {
			if i < len(pos) {
				self.Dict[n] = pos[i]
				continue
			}
			if kw != nil {
				if v, hit := kw[n]; hit {
					self.Dict[n] = v
					continue
				}
			}
			if d, hit := defaults[n]; hit {
				self.Dict[n] = d
				continue
			}
			ip.TypeError("__new__() missing required argument: '%s'", n)
		}
		return object.None, nil
	})

	cls.Dict["__len__"] = bi("__len__", func(a []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(len(names))}, nil
	})
	cls.Dict["__getitem__"] = bi("__getitem__", func(a []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("__getitem__ requires index")
		}
		self, _ := a[0].(*object.Instance)
		if self == nil {
			ip.TypeError("tuple indices must be integers")
		}
		// 切片：sys.version_info[:2] 这种用法必须有
		if sl, ok := a[1].(*object.Slice); ok {
			s0, e0, st0 := ip.sliceRange(sl, len(names))
			var out []object.Object
			if st0 > 0 {
				for i := s0; i < e0; i += st0 {
					out = append(out, self.Dict[names[i]])
				}
			}
			return &object.Tuple{Items: out}, nil
		}
		n, ok := object.AsInt(a[1])
		if !ok {
			ip.TypeError("tuple indices must be integers")
		}
		if n < 0 {
			n += int64(len(names))
		}
		if n < 0 || n >= int64(len(names)) {
			debug.PrintStack()
			ip.IndexErr("tuple index out of range")
		}
		return self.Dict[names[n]], nil
	})
	cls.Dict["__iter__"] = bi("__iter__", func(a []object.Object, kw map[string]object.Object) (object.Object, error) {
		out := make([]object.Object, 0, len(names))
		if len(a) > 0 {
			if self, ok := a[0].(*object.Instance); ok {
				for _, n := range names {
					out = append(out, self.Dict[n])
				}
			}
		}
		return &object.Iterator{Items: out}, nil
	})
	cls.Dict["_asdict"] = bi("_asdict", func(a []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := &object.Dict{}
		if len(a) > 0 {
			if self, ok := a[0].(*object.Instance); ok {
				for _, n := range names {
					d.Set(&object.Str{V: n}, self.Dict[n])
				}
			}
		}
		return d, nil
	})
	cls.Dict["_replace"] = bi("_replace", func(a []object.Object, kw map[string]object.Object) (object.Object, error) {
		inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
		if len(a) > 0 {
			if self, ok := a[0].(*object.Instance); ok {
				for _, n := range names {
					inst.Dict[n] = self.Dict[n]
				}
			}
		}
		if kw != nil {
			for k, v := range kw {
				if _, known := defaults[k]; !known {
					// 字段存在性检查：只允许已知字段
					okField := false
					for _, n := range names {
						if n == k {
							okField = true
							break
						}
					}
					if !okField {
						ip.TypeError("Got unexpected field names: %s", k)
					}
				}
				inst.Dict[k] = v
			}
		}
		return inst, nil
	})
	cls.Dict["__repr__"] = bi("__repr__", func(a []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Str{V: cls.Name + "()"}, nil
		}
		self, ok := a[0].(*object.Instance)
		if !ok {
			return &object.Str{V: cls.Name + "()"}, nil
		}
		var sb strings.Builder
		sb.WriteString(cls.Name)
		sb.WriteString("(")
		for i, n := range names {
			if i > 0 {
				sb.WriteString(", ")
			}
			sb.WriteString(n)
			sb.WriteString("=")
			sb.WriteString(ip.Repr(self.Dict[n]))
		}
		sb.WriteString(")")
		return &object.Str{V: sb.String()}, nil
	})
	cls.Dict["__eq__"] = bi("__eq__", func(a []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			return object.FalseObj, nil
		}
		x, ok1 := a[0].(*object.Instance)
		y, ok2 := a[1].(*object.Instance)
		if !ok1 || !ok2 || x.Class != y.Class {
			return object.FalseObj, nil
		}
		for _, n := range names {
			if !ip.Eq(x.Dict[n], y.Dict[n]) {
				return object.FalseObj, nil
			}
		}
		return object.TrueObj, nil
	})
}

// namedTupleMarker 标记 typing.NamedTuple 这个「伪基类」
const namedTupleMarker = "_namedtuple_marker_"

// asTupleLike NamedTuple 实例按字段展开成 Tuple（用于与普通 tuple 比较/排序）。
func (ip *Interp) asTupleLike(o object.Object) (object.Object, bool) {
	inst, ok := o.(*object.Instance)
	if !ok || inst.Class == nil {
		return nil, false
	}
	fv, has := inst.Class.Lookup("_fields")
	if !has {
		return nil, false
	}
	fields, ok := fv.(*object.Tuple)
	if !ok {
		return nil, false
	}
	items := make([]object.Object, 0, len(fields.Items))
	for _, f := range fields.Items {
		items = append(items, inst.Dict[f.Str()])
	}
	return &object.Tuple{Items: items}, true
}
