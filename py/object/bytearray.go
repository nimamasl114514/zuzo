package object

// bytearray.go — bytearray / memoryview 两个原生类型的对象定义（第14轮）
//
// 为什么需要：CPython 的 `_collections_abc.py:69` 有一行
//   bytearray_iterator = type(iter(bytearray()))
// 也就是说**只要 import collections.abc，就必须能构造 bytearray 并迭代它**。
// urllib3 / requests / email 的绝大多数链路都会间接 import 到它，缺了就是
// `NameError: name 'bytearray' is not defined`，整条 HTTP 链断掉。
//
// 实现口径：ByteArray 直接持有 []byte（可变），方法在 py/runtime/bytearray_mod.go；
// MemoryView 是只读视图的简化版（持有同一底层切片，不做 strides/format）。

import (
	"strings"
)

// ByteArray 可变字节序列（对应 CPython bytearray）
type ByteArray struct{ V []byte }

// ByteArrayType bytearray 的类型对象
var ByteArrayType = &TypeObj{Name: "bytearray", Bases: []*TypeObj{ObjectType}, Dict: map[string]Object{}}

func (b *ByteArray) Type() *TypeObj { return ByteArrayType }

func (b *ByteArray) Repr() string {
	var sb strings.Builder
	sb.WriteString("bytearray(b'")
	for _, c := range b.V {
		switch c {
		case '\\':
			sb.WriteString("\\\\")
		case '\n':
			sb.WriteString("\\n")
		case '\r':
			sb.WriteString("\\r")
		case '\t':
			sb.WriteString("\\t")
		case '\'':
			sb.WriteString("\\'")
		default:
			if c >= 0x20 && c < 0x7f {
				sb.WriteByte(c)
			} else {
				sb.WriteString("\\x")
				const hexd = "0123456789abcdef"
				sb.WriteByte(hexd[c>>4])
				sb.WriteByte(hexd[c&0xf])
			}
		}
	}
	sb.WriteString("')")
	return sb.String()
}

func (b *ByteArray) Str() string { return b.Repr() }

// MemoryView 内存视图（简化：只做只读字节视图）
type MemoryView struct{ V []byte }

// MemoryViewType memoryview 的类型对象
var MemoryViewType = &TypeObj{Name: "memoryview", Bases: []*TypeObj{ObjectType}, Dict: map[string]Object{}}

func (m *MemoryView) Type() *TypeObj { return MemoryViewType }
func (m *MemoryView) Repr() string   { return "<memory at 0x0>" }
func (m *MemoryView) Str() string    { return m.Repr() }
