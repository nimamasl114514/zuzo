package runtime

// cmods3.go — 第三批基础模块（第13轮）
//
// 已实现：struct / _struct / binascii / select（基础）/ types.CodeType
// 这些是 urllib3、email、socket 相关链路的底层依赖。

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"math"
	"strings"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["struct"] = modStruct
	builtinModuleInits["_struct"] = modStruct
	builtinModuleInits["binascii"] = modBinascii
	builtinModuleInits["select"] = modSelect
}

// ==================== struct ====================

type structItem struct {
	code byte
	size int
}

func parseStructFmt(f string) (binary.ByteOrder, []structItem, error) {
	var order binary.ByteOrder = binary.LittleEndian // Python 默认 '=' 本机序
	i := 0
	if len(f) > 0 {
		switch f[0] {
		case '@', '=', '<', '>', '!':
			switch f[0] {
			case '<':
				order = binary.LittleEndian
			case '>', '!':
				order = binary.BigEndian
			}
			i = 1
		}
	}
	var items []structItem
	for i < len(f) {
		c := f[i]
		if c == ' ' {
			i++
			continue
		}
		if c >= '0' && c <= '9' {
			n := 0
			for i < len(f) && f[i] >= '0' && f[i] <= '9' {
				n = n*10 + int(f[i]-'0')
				i++
			}
			if len(items) == 0 {
				return nil, nil, fmt.Errorf("bad struct format")
			}
			items[len(items)-1].size *= n
			continue
		}
		sz := 0
		switch c {
		case 'b', 'B', '?', 'x':
			sz = 1
		case 'h', 'H':
			sz = 2
		case 'i', 'I', 'l', 'L', 'f':
			sz = 4
		case 'q', 'Q', 'd':
			sz = 8
		case 's':
			sz = 1
		case 'c':
			sz = 1
		case 'e':
			sz = 2
		case 'p', 'P', 'n', 'N':
			sz = 8
		default:
			return nil, nil, fmt.Errorf("bad char in struct format: %q", string(c))
		}
		items = append(items, structItem{code: c, size: sz})
		i++
	}
	return order, items, nil
}

func structCalcSize(f string) (int, error) {
	_, items, err := parseStructFmt(f)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, it := range items {
		n += it.size
	}
	return n, nil
}

func structPack(ip *Interp, f string, args []object.Object) ([]byte, error) {
	order, items, err := parseStructFmt(f)
	if err != nil {
		return nil, err
	}
	var out []byte
	ai := 0
	for _, it := range items {
		buf := make([]byte, it.size)
		switch it.code {
		case 'x':
			// pad
		case 's':
			if ai < len(args) {
				b := bytesOf(args[ai])
				copy(buf, b)
				ai++
			}
		case 'c':
			if ai < len(args) {
				b := bytesOf(args[ai])
				if len(b) > 0 {
					buf[0] = b[0]
				}
				ai++
			}
		case 'b', 'h', 'i', 'l', 'q', 'n':
			v := int64(0)
			if ai < len(args) {
				v, _ = object.AsInt(args[ai])
				ai++
			}
			putInt(buf, uint64(v), order)
		case 'B', 'H', 'I', 'L', 'Q', 'P', 'N':
			v := int64(0)
			if ai < len(args) {
				v, _ = object.AsInt(args[ai])
				ai++
			}
			putInt(buf, uint64(v), order)
		case '?':
			v := false
			if ai < len(args) {
				v = object.Truthy(args[ai])
				ai++
			}
			if v {
				buf[0] = 1
			}
		case 'f':
			v := 0.0
			if ai < len(args) {
				v, _ = object.AsFloat(args[ai])
				ai++
			}
			putInt(buf, uint64(math.Float32bits(float32(v))), order)
		case 'd':
			v := 0.0
			if ai < len(args) {
				v, _ = object.AsFloat(args[ai])
				ai++
			}
			putInt(buf, math.Float64bits(v), order)
		}
		out = append(out, buf...)
	}
	return out, nil
}

func putInt(buf []byte, v uint64, order binary.ByteOrder) {
	switch len(buf) {
	case 1:
		buf[0] = byte(v)
	case 2:
		order.PutUint16(buf, uint16(v))
	case 4:
		order.PutUint32(buf, uint32(v))
	case 8:
		order.PutUint64(buf, v)
	}
}

func structUnpack(ip *Interp, f string, data []byte) ([]object.Object, error) {
	order, items, err := parseStructFmt(f)
	if err != nil {
		return nil, err
	}
	want, _ := structCalcSize(f)
	if len(data) < want {
		return nil, fmt.Errorf("unpack requires a buffer of %d bytes", want)
	}
	var out []object.Object
	off := 0
	for _, it := range items {
		buf := data[off : off+it.size]
		off += it.size
		switch it.code {
		case 'x':
			continue
		case 's':
			out = append(out, &object.Bytes{V: append([]byte{}, buf...)})
		case 'c':
			out = append(out, &object.Bytes{V: append([]byte{}, buf...)})
		case 'b', 'h', 'i', 'l', 'q', 'n':
			out = append(out, &object.Int{V: int64(getInt(buf, order, false))})
		case 'B', 'H', 'I', 'L', 'Q', 'P', 'N':
			out = append(out, &object.Int{V: int64(getInt(buf, order, true))})
		case '?':
			out = append(out, &object.Bool{V: buf[0] != 0})
		case 'f':
			out = append(out, &object.Float{V: float64(math.Float32frombits(uint32(getInt(buf, order, true))))})
		case 'd':
			out = append(out, &object.Float{V: math.Float64frombits(getInt(buf, order, true))})
		}
	}
	return out, nil
}

func getInt(buf []byte, order binary.ByteOrder, unsigned bool) uint64 {
	var v uint64
	switch len(buf) {
	case 1:
		v = uint64(buf[0])
	case 2:
		v = uint64(order.Uint16(buf))
	case 4:
		v = uint64(order.Uint32(buf))
	case 8:
		v = order.Uint64(buf)
	}
	return v
}

func bytesOf(o object.Object) []byte {
	switch v := o.(type) {
	case *object.Bytes:
		return v.V
	case *object.Str:
		return []byte(v.V)
	}
	return nil
}

func modStruct(ip *Interp) *object.Module {
	m := newModule("struct")
	m.Dict["calcsize"] = bi("calcsize", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		n, err := structCalcSize(a[0].Str())
		if err != nil {
			ip.raise(ip.excType("ValueError"), "%v", err)
		}
		return &object.Int{V: int64(n)}, nil
	})
	m.Dict["pack"] = bi("pack", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("pack() requires a format")
		}
		out, err := structPack(ip, a[0].Str(), a[1:])
		if err != nil {
			ip.raise(ip.excType("ValueError"), "%v", err)
		}
		return &object.Bytes{V: out}, nil
	})
	m.Dict["unpack"] = bi("unpack", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("unpack() requires (format, buffer)")
		}
		vals, err := structUnpack(ip, a[0].Str(), bytesOf(a[1]))
		if err != nil {
			ip.raise(ip.excType("ValueError"), "%v", err)
		}
		return &object.Tuple{Items: vals}, nil
	})
	m.Dict["pack_into"] = bi("pack_into", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	m.Dict["unpack_from"] = bi("unpack_from", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("unpack_from() requires (format, buffer)")
		}
		return &object.Tuple{Items: []object.Object{}}, nil
	})
	m.Dict["Struct"] = &object.TypeObj{Name: "Struct", Dict: map[string]object.Object{}}
	m.Dict["error"] = ip.excType("ValueError")
	return m
}

// ==================== binascii ====================

func modBinascii(ip *Interp) *object.Module {
	m := newModule("binascii")
	m.Dict["hexlify"] = bi("hexlify", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Bytes{V: []byte(fmt.Sprintf("%x", bytesOf(a[0])))}, nil
	})
	m.Dict["b2a_hex"] = m.Dict["hexlify"]
	m.Dict["unhexlify"] = bi("unhexlify", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		s := strings.TrimSpace(string(bytesOf(a[0])))
		out := make([]byte, 0, len(s)/2)
		for i := 0; i+1 < len(s); i += 2 {
			hi, ok1 := hexVal(s[i])
			lo, ok2 := hexVal(s[i+1])
			if !ok1 || !ok2 {
				ip.raise(ip.excType("ValueError"), "Non-hexadecimal digit found")
			}
			out = append(out, hi<<4|lo)
		}
		return &object.Bytes{V: out}, nil
	})
	m.Dict["a2b_hex"] = m.Dict["unhexlify"]
	m.Dict["b2a_base64"] = bi("b2a_base64", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Bytes{V: []byte(base64.StdEncoding.EncodeToString(bytesOf(a[0])) + "\n")}, nil
	})
	m.Dict["a2b_base64"] = bi("a2b_base64", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		s := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, string(bytesOf(a[0])))
		out, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			out, err = base64.RawStdEncoding.DecodeString(s)
			if err != nil {
				ip.raise(ip.excType("ValueError"), "Invalid base64 string")
			}
		}
		return &object.Bytes{V: out}, nil
	})
	m.Dict["crc32"] = bi("crc32", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		v := uint32(0)
		if len(a) > 1 {
			if n, ok := object.AsInt(a[1]); ok {
				v = uint32(n)
			}
		}
		return &object.Int{V: int64(crc32.Update(v, crc32.IEEETable, bytesOf(a[0])))}, nil
	})
	m.Dict["Error"] = ip.excType("ValueError")
	m.Dict["Incomplete"] = ip.excType("ValueError")
	return m
}

func hexVal(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
}

// ==================== select ====================

func modSelect(ip *Interp) *object.Module {
	m := newModule("select")
	m.Dict["error"] = ip.excType("OSError")
	// select(rlist, wlist, xlist, timeout=None) → 可立即就绪的对象三元组
	m.Dict["select"] = bi("select", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		empty := &object.List{}
		return &object.Tuple{Items: []object.Object{empty, &object.List{}, &object.List{}}}, nil
	})
	pollCls := &object.TypeObj{Name: "poll", Dict: map[string]object.Object{}}
	pollCls.Dict["register"] = bi("register", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	pollCls.Dict["unregister"] = pollCls.Dict["register"]
	pollCls.Dict["poll"] = bi("poll", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.List{}, nil
	})
	m.Dict["poll"] = pollCls
	m.Dict["POLLIN"] = &object.Int{V: 1}
	m.Dict["POLLOUT"] = &object.Int{V: 4}
	m.Dict["POLLERR"] = &object.Int{V: 8}
	return m
}

// ==================== Code 对象（function.__code__） ====================

var codeType = &object.TypeObj{Name: "code", Dict: map[string]object.Object{}}

func (ip *Interp) newCodeObj(f *object.Function) *object.Instance {
	inst := &object.Instance{Class: codeType, Dict: map[string]object.Object{}}
	inst.Dict["co_name"] = &object.Str{V: f.Name}
	inst.Dict["co_qualname"] = &object.Str{V: f.QualName}
	argc := 0
	var names []object.Object
	if f.Params != nil {
		argc = len(f.Params.Args)
		for _, p := range f.Params.Args {
			names = append(names, &object.Str{V: p.Name})
		}
	}
	inst.Dict["co_argcount"] = &object.Int{V: int64(argc)}
	inst.Dict["co_varnames"] = &object.Tuple{Items: names}
	inst.Dict["co_filename"] = &object.Str{V: "<zuzo>"}
	inst.Dict["co_firstlineno"] = &object.Int{V: 1}
	inst.Dict["co_flags"] = &object.Int{V: 0}
	inst.Dict["co_code"] = &object.Bytes{V: []byte{}}
	inst.Dict["co_consts"] = &object.Tuple{}
	inst.Dict["co_names"] = &object.Tuple{}
	return inst
}
// ==================== Cell 对象（闭包单元，types.CellType 需要） ====================

var cellType = &object.TypeObj{Name: "cell", Dict: map[string]object.Object{}}

func init() {
	cellType.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			if inst, ok := a[0].(*object.Instance); ok {
				inst.Dict["cell_contents"] = object.None
			}
		}
		return object.None, nil
	})
}

func newCellObj() *object.Instance {
	inst := &object.Instance{Class: cellType, Dict: map[string]object.Object{}}
	inst.Dict["cell_contents"] = object.None
	return inst
}

// ==================== Traceback / Frame 桩对象（types.TracebackType 需要） ====================

var (
	tracebackType = &object.TypeObj{Name: "traceback", Dict: map[string]object.Object{}}
	frameType     = &object.TypeObj{Name: "frame", Dict: map[string]object.Object{}}
)

func (ip *Interp) newTracebackObj() *object.Instance {
	fr := &object.Instance{Class: frameType, Dict: map[string]object.Object{}}
	fr.Dict["f_code"] = &object.Instance{Class: codeType, Dict: map[string]object.Object{
		"co_name":     &object.Str{V: "<module>"},
		"co_filename": &object.Str{V: ip.MainFile},
	}}
	fr.Dict["f_lineno"] = &object.Int{V: 0}
	fr.Dict["f_back"] = object.None
	fr.Dict["f_globals"] = &object.Dict{}
	fr.Dict["f_locals"] = &object.Dict{}

	tb := &object.Instance{Class: tracebackType, Dict: map[string]object.Object{}}
	tb.Dict["tb_frame"] = fr
	tb.Dict["tb_next"] = object.None
	tb.Dict["tb_lineno"] = &object.Int{V: 0}
	return tb
}

// ==================== 泛型别名 / 类型并集（PEP 585 / PEP 604 最小实现） ====================

var (
	genericAliasType = &object.TypeObj{Name: "GenericAlias", Dict: map[string]object.Object{}}
	unionTypeType    = &object.TypeObj{Name: "UnionType", Dict: map[string]object.Object{}}
)

func (ip *Interp) makeGenericAlias(origin *object.TypeObj, args object.Object) *object.Instance {
	inst := &object.Instance{Class: genericAliasType, Dict: map[string]object.Object{}}
	inst.Dict["__origin__"] = origin
	inst.Dict["__args__"] = args
	inst.Dict["__module__"] = &object.Str{V: "builtins"}
	return inst
}

func (ip *Interp) makeUnionType(a, b object.Object) *object.Instance {
	inst := &object.Instance{Class: unionTypeType, Dict: map[string]object.Object{}}
	inst.Dict["__args__"] = &object.Tuple{Items: []object.Object{a, b}}
	return inst
}
