package runtime

// bytearray_mod.go — bytearray / memoryview 的内置构造与方法（第14轮）
//
// 触发点：`_collections_abc.py:69` 的 `type(iter(bytearray()))`。
// 实现范围按 requests 链实际需要裁剪：构造 + 序列协议 + decode/hex/append/extend
// + bytes()/copy() + 常用查找与切分。memoryview 只做只读视图（tobytes/len/索引/切片）。

import (
	"bytes"
	"strings"

	"zuzo/py/object"
)

// registerByteArrayBuiltins 注册 bytearray / memoryview 两个内建类型构造器。
func registerByteArrayBuiltins(ip *Interp) {
	// 内置名绑定为「类型对象」：既能 isinstance(x, bytearray)，又能 bytearray(...) 构造
	// （构造走 instantiate 的特判，见 call.go）
	ip.Builtins.Set("bytearray", object.ByteArrayType)
	ip.Builtins.Set("memoryview", object.MemoryViewType)
}

// makeByteArray 处理 bytearray(...) 的全部构造形态：
//
//	bytearray()                → 空
//	bytearray(5)               → 5 个 0
//	bytearray(b"abc")          → 复制
//	bytearray(bytearray(...))  → 复制
//	bytearray("abc", "utf-8")  → 编码
//	bytearray([1,2,3])         → 逐项（0..255）
func (ip *Interp) makeByteArray(args []object.Object) object.Object {
	if len(args) == 0 {
		return &object.ByteArray{V: []byte{}}
	}
	switch v := args[0].(type) {
	case *object.Bytes:
		return &object.ByteArray{V: append([]byte{}, v.V...)}
	case *object.ByteArray:
		return &object.ByteArray{V: append([]byte{}, v.V...)}
	case *object.MemoryView:
		return &object.ByteArray{V: append([]byte{}, v.V...)}
	case *object.Int:
		n := v.V
		if n < 0 {
			ip.ValueError("negative count")
		}
		return &object.ByteArray{V: make([]byte, n)}
	case *object.Str:
		enc, errs := "utf-8", "strict"
		if len(args) > 1 && args[1] != object.None {
			enc = args[1].Str()
		}
		if len(args) > 2 && args[2] != object.None {
			errs = args[2].Str()
		}
		b, err := codecEncode(v.V, enc, errs)
		if err != nil {
			ip.ValueError("%v", err)
		}
		return &object.ByteArray{V: b}
	}
	// 可迭代（list / tuple / 生成器 …）
	out := []byte{}
	for _, item := range ip.ToSlice(args[0]) {
		n, ok := object.AsInt(item)
		if !ok || n < 0 || n > 255 {
			ip.ValueError("byte must be in range(0, 256)")
		}
		out = append(out, byte(n))
	}
	return &object.ByteArray{V: out}
}

// byteArrayGetAttr bytearray 的方法面（在 GetAttr 里优先于通用分派调用）。
func (ip *Interp) byteArrayGetAttr(o object.Object, name string) (object.Object, bool) {
	ba, ok := o.(*object.ByteArray)
	if !ok {
		return nil, false
	}
	self := ba
	// 注意：Builtin 带 Self 时，ip.Call 会把 self 作为 args[0] 传进来，
	// 这里统一剥掉，方法体内 a[0] 才是真正的第一个实参。
	mk := func(fn object.BuiltinFn) object.Object {
		return &object.Builtin{Name: name, Self: self, Fn: func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) > 0 {
				if _, isSelf := a[0].(*object.ByteArray); isSelf {
					a = a[1:]
				}
			}
			return fn(a, k)
		}}
	}
	switch name {
	case "append":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				ip.TypeError("append() takes exactly one argument")
			}
			n, ok := object.AsInt(a[0])
			if !ok || n < 0 || n > 255 {
				ip.ValueError("byte must be in range(0, 256)")
			}
			self.V = append(self.V, byte(n))
			return object.None, nil
		}), true
	case "extend":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				return object.None, nil
			}
			switch v := a[0].(type) {
			case *object.Bytes:
				self.V = append(self.V, v.V...)
			case *object.ByteArray:
				self.V = append(self.V, v.V...)
			case *object.MemoryView:
				self.V = append(self.V, v.V...)
			default:
				for _, item := range ip.ToSlice(a[0]) {
					n, ok := object.AsInt(item)
					if !ok || n < 0 || n > 255 {
						ip.ValueError("byte must be in range(0, 256)")
					}
					self.V = append(self.V, byte(n))
				}
			}
			return object.None, nil
		}), true
	case "pop":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(self.V) == 0 {
				ip.IndexErr("pop from empty bytearray")
			}
			idx := int64(len(self.V) - 1)
			if len(a) > 0 {
				if n, ok := object.AsInt(a[0]); ok {
					idx = n
				}
			}
			if idx < 0 {
				idx += int64(len(self.V))
			}
			if idx < 0 || idx >= int64(len(self.V)) {
				ip.IndexErr("pop index out of range")
			}
			val := self.V[idx]
			self.V = append(self.V[:idx], self.V[idx+1:]...)
			return &object.Int{V: int64(val)}, nil
		}), true
	case "insert":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) < 2 {
				ip.TypeError("insert() takes 2 arguments")
			}
			i, _ := object.AsInt(a[0])
			n, ok := object.AsInt(a[1])
			if !ok || n < 0 || n > 255 {
				ip.ValueError("byte must be in range(0, 256)")
			}
			if i < 0 {
				i = 0
			}
			if i > int64(len(self.V)) {
				i = int64(len(self.V))
			}
			self.V = append(self.V, 0)
			copy(self.V[i+1:], self.V[i:])
			self.V[i] = byte(n)
			return object.None, nil
		}), true
	case "remove":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				ip.TypeError("remove() takes exactly one argument")
			}
			n, _ := object.AsInt(a[0])
			for i, b := range self.V {
				if int64(b) == n {
					self.V = append(self.V[:i], self.V[i+1:]...)
					return object.None, nil
				}
			}
			ip.ValueError("value not found in bytearray")
			return object.None, nil
		}), true
	case "reverse":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			for i, j := 0, len(self.V)-1; i < j; i, j = i+1, j-1 {
				self.V[i], self.V[j] = self.V[j], self.V[i]
			}
			return object.None, nil
		}), true
	case "clear":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			self.V = self.V[:0]
			return object.None, nil
		}), true
	case "copy":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.ByteArray{V: append([]byte{}, self.V...)}, nil
		}), true
	case "decode":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			enc, errs := "utf-8", "strict"
			if len(a) > 0 && a[0] != object.None {
				enc = a[0].Str()
			}
			if len(a) > 1 && a[1] != object.None {
				errs = a[1].Str()
			}
			s, err := codecDecode(self.V, enc, errs)
			if err != nil {
				ip.ValueError("%v", err)
			}
			return &object.Str{V: s}, nil
		}), true
	case "hex":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			const hexd = "0123456789abcdef"
			var sb strings.Builder
			for _, b := range self.V {
				sb.WriteByte(hexd[b>>4])
				sb.WriteByte(hexd[b&0xf])
			}
			return &object.Str{V: sb.String()}, nil
		}), true
	case "bytes":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Bytes{V: append([]byte{}, self.V...)}, nil
		}), true
	case "find":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				return &object.Int{V: -1}, nil
			}
			sub := ip.bytesOf(a[0])
			start, end := 0, len(self.V)
			if len(a) > 1 {
				if n, ok := object.AsInt(a[1]); ok {
					start = clampIndex(int(n), len(self.V))
				}
			}
			if len(a) > 2 {
				if n, ok := object.AsInt(a[2]); ok {
					end = clampIndex(int(n), len(self.V))
				}
			}
			if start > end {
				return &object.Int{V: -1}, nil
			}
			pos := strings.Index(string(self.V[start:end]), string(sub))
			if pos < 0 {
				return &object.Int{V: -1}, nil
			}
			return &object.Int{V: int64(start + pos)}, nil
		}), true
	case "count":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				return &object.Int{V: 0}, nil
			}
			sub := ip.bytesOf(a[0])
			if len(sub) == 0 {
				return &object.Int{V: int64(len(self.V) + 1)}, nil
			}
			return &object.Int{V: int64(bytes.Count(self.V, sub))}, nil
		}), true
	case "startswith", "endswith":
		isStart := name == "startswith"
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				return object.FalseObj, nil
			}
			sub := ip.bytesOf(a[0])
			if isStart {
				return object.GetBool(bytes.HasPrefix(self.V, sub)), nil
			}
			return object.GetBool(bytes.HasSuffix(self.V, sub)), nil
		}), true
	case "replace":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) < 2 {
				ip.TypeError("replace() takes at least 2 arguments")
			}
			oldB := ip.bytesOf(a[0])
			newB := ip.bytesOf(a[1])
			n := -1
			if len(a) > 2 {
				if x, ok := object.AsInt(a[2]); ok {
					n = int(x)
				}
			}
			return &object.ByteArray{V: bytes.Replace(self.V, oldB, newB, n)}, nil
		}), true
	case "split":
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			var parts [][]byte
			if len(a) == 0 || a[0] == object.None {
				parts = bytes.Fields(self.V)
			} else {
				parts = bytes.Split(self.V, ip.bytesOf(a[0]))
			}
			items := make([]object.Object, len(parts))
			for i, p := range parts {
				items[i] = &object.ByteArray{V: append([]byte{}, p...)}
			}
			return &object.List{Items: items}, nil
		}), true
	case "strip", "lstrip", "rstrip":
		mode := name
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			cut := " \t\n\r\v\f"
			if len(a) > 0 && a[0] != object.None {
				cut = string(ip.bytesOf(a[0]))
			}
			s := string(self.V)
			switch mode {
			case "strip":
				s = strings.Trim(s, cut)
			case "lstrip":
				s = strings.TrimLeft(s, cut)
			default:
				s = strings.TrimRight(s, cut)
			}
			return &object.ByteArray{V: []byte(s)}, nil
		}), true
	case "upper", "lower":
		up := name == "upper"
		return mk(func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			out := append([]byte{}, self.V...)
			for i, b := range out {
				if up && b >= 'a' && b <= 'z' {
					out[i] = b - 32
				} else if !up && b >= 'A' && b <= 'Z' {
					out[i] = b + 32
				}
			}
			return &object.ByteArray{V: out}, nil
		}), true
	}
	return nil, false
}

// memoryViewGetAttr memoryview 的方法面（只读视图）。
func (ip *Interp) memoryViewGetAttr(o object.Object, name string) (object.Object, bool) {
	mv, ok := o.(*object.MemoryView)
	if !ok {
		return nil, false
	}
	self := mv
	switch name {
	case "tobytes":
		return &object.Builtin{Name: name, Self: self, Fn: func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Bytes{V: append([]byte{}, self.V...)}, nil
		}}, true
	case "tolist":
		return &object.Builtin{Name: name, Self: self, Fn: func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			items := make([]object.Object, len(self.V))
			for i, b := range self.V {
				items[i] = &object.Int{V: int64(b)}
			}
			return &object.List{Items: items}, nil
		}}, true
	case "release":
		return &object.Builtin{Name: name, Self: self, Fn: func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			self.V = nil
			return object.None, nil
		}}, true
	}
	return nil, false
}

// bytesOf 把 bytes 类入参统一转成 []byte（bytes / bytearray / memoryview / str）。
func (ip *Interp) bytesOf(o object.Object) []byte {
	switch v := o.(type) {
	case *object.Bytes:
		return v.V
	case *object.ByteArray:
		return v.V
	case *object.MemoryView:
		return v.V
	case *object.Str:
		return []byte(v.V)
	case *object.Int:
		return []byte{byte(v.V)}
	}
	return []byte(o.Str())
}

func clampIndex(i, n int) int {
	if i < 0 {
		i += n
	}
	if i < 0 {
		return 0
	}
	if i > n {
		return n
	}
	return i
}
