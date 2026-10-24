package object

import (
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

// Object 所有 Python 值的公共接口
type Object interface {
	Type() *TypeObj
	Repr() string
	Str() string
}

// TypeObj 类型对象（Python 的 type 实例）
type TypeObj struct {
	Name    string
	Bases   []*TypeObj
	Dict    map[string]Object
	mro     []*TypeObj
	IsExc   bool // 是否异常类型
	Module  string
	Builtin int // 内置类型编号（0 = 用户类）
}

// 内置类型编号
const (
	BT_None = iota + 1
	BT_Bool
	BT_Int
	BT_Float
	BT_Str
	BT_Bytes
	BT_List
	BT_Tuple
	BT_Dict
	BT_Set
	BT_Range
	BT_Slice
	BT_Function
	BT_Builtin
	BT_Method
	BT_Module
	BT_Type
	BT_Generator
	BT_Iterator
	BT_Instance
	BT_Exception
	BT_Super
	BT_Property
	BT_StaticMethod
	BT_ClassMethod
	BT_Complex
	BT_File
	BT_Enumerate
	BT_Zip
	BT_Map
	BT_Filter
)

var (
	// ObjectType 所有类的根类
	ObjectType  = &TypeObj{Name: "object", Dict: map[string]Object{}}
	typeType    = &TypeObj{Name: "type", Bases: []*TypeObj{ObjectType}, Builtin: BT_Type, Dict: map[string]Object{}}
	noneType    = &TypeObj{Name: "NoneType", Bases: []*TypeObj{ObjectType}, Builtin: BT_None}
	boolType    = &TypeObj{Name: "bool", Bases: []*TypeObj{ObjectType}, Builtin: BT_Bool}
	intType     = &TypeObj{Name: "int", Bases: []*TypeObj{ObjectType}, Builtin: BT_Int}
	floatType   = &TypeObj{Name: "float", Bases: []*TypeObj{ObjectType}, Builtin: BT_Float}
	strType     = &TypeObj{Name: "str", Bases: []*TypeObj{ObjectType}, Builtin: BT_Str}
	bytesType   = &TypeObj{Name: "bytes", Bases: []*TypeObj{ObjectType}, Builtin: BT_Bytes}
	listType    = &TypeObj{Name: "list", Bases: []*TypeObj{ObjectType}, Builtin: BT_List}
	tupleType   = &TypeObj{Name: "tuple", Bases: []*TypeObj{ObjectType}, Builtin: BT_Tuple}
	dictType    = &TypeObj{Name: "dict", Bases: []*TypeObj{ObjectType}, Builtin: BT_Dict}
	setType     = &TypeObj{Name: "set", Bases: []*TypeObj{ObjectType}, Builtin: BT_Set}
	rangeType   = &TypeObj{Name: "range", Bases: []*TypeObj{ObjectType}, Builtin: BT_Range}
	sliceType   = &TypeObj{Name: "slice", Bases: []*TypeObj{ObjectType}, Builtin: BT_Slice}
	funcType    = &TypeObj{Name: "function", Bases: []*TypeObj{ObjectType}, Builtin: BT_Function}
	builtinType = &TypeObj{Name: "builtin_function_or_method", Bases: []*TypeObj{ObjectType}, Builtin: BT_Builtin}
	methodType  = &TypeObj{Name: "method", Bases: []*TypeObj{ObjectType}, Builtin: BT_Method}
	moduleType  = &TypeObj{Name: "module", Bases: []*TypeObj{ObjectType}, Builtin: BT_Module}
	genType     = &TypeObj{Name: "generator", Bases: []*TypeObj{ObjectType}, Builtin: BT_Generator}
	iterType    = &TypeObj{Name: "iterator", Bases: []*TypeObj{ObjectType}, Builtin: BT_Iterator}
	excType     = &TypeObj{Name: "Exception", IsExc: true, Bases: []*TypeObj{ObjectType}, Builtin: BT_Exception}
)

// Type 类型对象自身
func (t *TypeObj) Type() *TypeObj { return typeType }
func (t *TypeObj) Repr() string   { return "<class '" + t.Name + "'>" }
func (t *TypeObj) Str() string    { return t.Repr() }

// Name_ 返回类型名
func (t *TypeObj) Name_() string { return t.Name }

// MRO 返回方法解析顺序（C3 线性化，简化版）
func (t *TypeObj) MRO() []*TypeObj {
	if t.mro != nil {
		return t.mro
	}
	res := []*TypeObj{t}
	var merge func(lists [][]*TypeObj) []*TypeObj
	merge = func(lists [][]*TypeObj) []*TypeObj {
		var out []*TypeObj
		for {
			// 去掉空表
			var nonEmpty [][]*TypeObj
			for _, l := range lists {
				if len(l) > 0 {
					nonEmpty = append(nonEmpty, l)
				}
			}
			if len(nonEmpty) == 0 {
				break
			}
			var cand *TypeObj
			for _, l := range nonEmpty {
				h := l[0]
				inTail := false
				for _, l2 := range nonEmpty {
					for i, x := range l2 {
						if i == 0 {
							continue
						}
						if x == h {
							inTail = true
							break
						}
					}
					if inTail {
						break
					}
				}
				if !inTail {
					cand = h
					break
				}
			}
			if cand == nil {
				// 冲突时退化：直接取第一个
				cand = nonEmpty[0][0]
			}
			out = append(out, cand)
			var next [][]*TypeObj
			for _, l := range nonEmpty {
				if l[0] == cand {
					next = append(next, l[1:])
				} else {
					next = append(next, l)
				}
			}
			lists = next
		}
		return out
	}
	var lists [][]*TypeObj
	for _, b := range t.Bases {
		lists = append(lists, b.MRO())
	}
	lists = append(lists, append([]*TypeObj{}, t.Bases...))
	res = append(res, merge(lists)...)
	t.mro = res
	return res
}

// Lookup 沿 MRO 查找类属性
func (t *TypeObj) Lookup(name string) (Object, bool) {
	for _, c := range t.MRO() {
		if v, ok := c.Dict[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// IsSubclass 判断是否为 other 的子类
func (t *TypeObj) IsSubclass(other *TypeObj) bool {
	for _, c := range t.MRO() {
		if c == other {
			return true
		}
	}
	return false
}

// ==================== None ====================

// NoneObj None 值
type NoneObj struct{}

func (n *NoneObj) Type() *TypeObj { return noneType }
func (n *NoneObj) Repr() string   { return "None" }
func (n *NoneObj) Str() string    { return "None" }

// None 单例
var None Object = &NoneObj{}

// ==================== bool ====================

// Bool 布尔值
type Bool struct{ V bool }

func (b *Bool) Type() *TypeObj { return boolType }
func (b *Bool) Repr() string {
	if b.V {
		return "True"
	}
	return "False"
}
func (b *Bool) Str() string { return b.Repr() }

// TrueObj / FalseObj 单例
var (
	TrueObj  Object = &Bool{V: true}
	FalseObj Object = &Bool{V: false}
)

// GetBool 返回布尔单例
func GetBool(v bool) Object {
	if v {
		return TrueObj
	}
	return FalseObj
}

// ==================== int ====================

// Int 整数：V 为 int64 快路径；超出范围时用 Big（任意精度）
type Int struct {
	V   int64
	Big *big.Int
}

func (i *Int) Type() *TypeObj { return intType }
func (i *Int) Repr() string {
	if i.Big != nil {
		return i.Big.String()
	}
	return strconv.FormatInt(i.V, 10)
}
func (i *Int) Str() string { return i.Repr() }

// AsBig 返回 big.Int 表示
func (i *Int) AsBig() *big.Int {
	if i.Big != nil {
		return i.Big
	}
	return big.NewInt(i.V)
}

// IsSmall 是否为 int64 快路径
func (i *Int) IsSmall() bool { return i.Big == nil }

// NewInt 构造小整数
func NewInt(v int64) *Int { return &Int{V: v} }

// NewBigInt 由 big.Int 构造（可缩回 small）
func NewBigInt(b *big.Int) *Int {
	if b.IsInt64() {
		return &Int{V: b.Int64()}
	}
	return &Int{Big: new(big.Int).Set(b)}
}

// ==================== float ====================

// Float 浮点数
type Float struct{ V float64 }

func (f *Float) Type() *TypeObj { return floatType }
func (f *Float) Repr() string {
	if math.IsInf(f.V, 1) {
		return "inf"
	}
	if math.IsInf(f.V, -1) {
		return "-inf"
	}
	if math.IsNaN(f.V) {
		return "nan"
	}
	s := strconv.FormatFloat(f.V, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eEnN") {
		s += ".0"
	}
	return s
}
func (f *Float) Str() string { return f.Repr() }

// ==================== str ====================

// Str 字符串（UTF-8）
type Str struct{ V string }

func (s *Str) Type() *TypeObj { return strType }
func (s *Str) Repr() string   { return ReprString(s.V) }
func (s *Str) Str() string    { return s.V }

// ReprString 生成 Python 风格字符串表示
func ReprString(v string) string {
	quote := byte('\'')
	if strings.ContainsRune(v, '\'') && !strings.ContainsRune(v, '"') {
		quote = '"'
	}
	var sb strings.Builder
	sb.WriteByte(quote)
	for _, r := range v {
		switch r {
		case '\\':
			sb.WriteString("\\\\")
		case '\n':
			sb.WriteString("\\n")
		case '\r':
			sb.WriteString("\\r")
		case '\t':
			sb.WriteString("\\t")
		default:
			if r == rune(quote) {
				sb.WriteByte('\\')
				sb.WriteRune(r)
			} else if r < 32 {
				sb.WriteString(fmt.Sprintf("\\x%02x", r))
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte(quote)
	return sb.String()
}

// ==================== bytes ====================

// Bytes 字节串
type Bytes struct{ V []byte }

func (b *Bytes) Type() *TypeObj { return bytesType }
func (b *Bytes) Repr() string {
	var sb strings.Builder
	sb.WriteString("b'")
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
			if c < 32 || c >= 127 {
				sb.WriteString(fmt.Sprintf("\\x%02x", c))
			} else {
				sb.WriteByte(c)
			}
		}
	}
	sb.WriteString("'")
	return sb.String()
}
func (b *Bytes) Str() string { return b.Repr() }

// ==================== list / tuple ====================

// List 列表
type List struct{ Items []Object }

func (l *List) Type() *TypeObj { return listType }
func (l *List) Repr() string   { return "[" + joinRepr(l.Items) + "]" }
func (l *List) Str() string    { return l.Repr() }

// Tuple 元组
type Tuple struct{ Items []Object }

func (t *Tuple) Type() *TypeObj { return tupleType }
func (t *Tuple) Repr() string {
	if len(t.Items) == 1 {
		return "(" + t.Items[0].Repr() + ",)"
	}
	return "(" + joinRepr(t.Items) + ")"
}
func (t *Tuple) Str() string { return t.Repr() }

// ReprHook 由运行时装上的 repr 钩子（用于容器内元素走 __repr__）
var ReprHook func(Object) string

// ReprOf 优先走钩子
func ReprOf(o Object) string {
	if ReprHook != nil {
		return ReprHook(o)
	}
	return o.Repr()
}

func joinRepr(items []Object) string {
	var sb strings.Builder
	for i, it := range items {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(ReprOf(it))
	}
	return sb.String()
}

// ==================== dict ====================

// Dict 字典（保持插入顺序）
type Dict struct {
	Keys []Object
	Vals []Object
}

func (d *Dict) Type() *TypeObj { return dictType }
func (d *Dict) Repr() string {
	var sb strings.Builder
	sb.WriteString("{")
	for i, k := range d.Keys {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteString(k.Repr())
		sb.WriteString(": ")
		sb.WriteString(d.Vals[i].Repr())
	}
	sb.WriteString("}")
	return sb.String()
}
func (d *Dict) Str() string { return d.Repr() }

// Index 查找键位置，返回 -1 表示不存在
func (d *Dict) Index(key Object) int {
	for i, k := range d.Keys {
		if Equal(k, key) {
			return i
		}
	}
	return -1
}

// Get 取值
func (d *Dict) Get(key Object) (Object, bool) {
	if i := d.Index(key); i >= 0 {
		return d.Vals[i], true
	}
	return nil, false
}

// Set 设置值
func (d *Dict) Set(key, val Object) {
	if i := d.Index(key); i >= 0 {
		d.Vals[i] = val
		return
	}
	d.Keys = append(d.Keys, key)
	d.Vals = append(d.Vals, val)
}

// Delete 删除键
func (d *Dict) Delete(key Object) bool {
	i := d.Index(key)
	if i < 0 {
		return false
	}
	d.Keys = append(d.Keys[:i], d.Keys[i+1:]...)
	d.Vals = append(d.Vals[:i], d.Vals[i+1:]...)
	return true
}

// Copy 浅拷贝
func (d *Dict) Copy() *Dict {
	n := &Dict{Keys: make([]Object, len(d.Keys)), Vals: make([]Object, len(d.Vals))}
	copy(n.Keys, d.Keys)
	copy(n.Vals, d.Vals)
	return n
}

// ==================== set ====================

// Set 集合（保持插入顺序，便于确定性输出）
type Set struct{ Items []Object }

func (s *Set) Type() *TypeObj { return setType }
func (s *Set) Repr() string {
	if len(s.Items) == 0 {
		return "set()"
	}
	return "{" + joinRepr(s.Items) + "}"
}
func (s *Set) Str() string { return s.Repr() }

// Contains 判断元素是否存在
func (s *Set) Contains(v Object) bool {
	for _, x := range s.Items {
		if Equal(x, v) {
			return true
		}
	}
	return false
}

// Add 添加元素
func (s *Set) Add(v Object) {
	if !s.Contains(v) {
		s.Items = append(s.Items, v)
	}
}

// Remove 删除元素
func (s *Set) Remove(v Object) bool {
	for i, x := range s.Items {
		if Equal(x, v) {
			s.Items = append(s.Items[:i], s.Items[i+1:]...)
			return true
		}
	}
	return false
}

// ==================== range / slice ====================

// Range 范围对象
type Range struct{ Start, Stop, Step int64 }

func (r *Range) Type() *TypeObj { return rangeType }
func (r *Range) Repr() string {
	if r.Step == 1 {
		return fmt.Sprintf("range(%d, %d)", r.Start, r.Stop)
	}
	return fmt.Sprintf("range(%d, %d, %d)", r.Start, r.Stop, r.Step)
}
func (r *Range) Str() string { return r.Repr() }

// Len 元素个数
func (r *Range) Len() int64 {
	if r.Step > 0 {
		if r.Stop <= r.Start {
			return 0
		}
		return (r.Stop - r.Start + r.Step - 1) / r.Step
	}
	if r.Step < 0 {
		if r.Stop >= r.Start {
			return 0
		}
		return (r.Start - r.Stop + (-r.Step) - 1) / (-r.Step)
	}
	return 0
}

// At 取第 i 个元素
func (r *Range) At(i int64) int64 { return r.Start + i*r.Step }

// Slice 切片对象
type Slice struct {
	Start Object
	Stop  Object
	Step  Object
}

func (s *Slice) Type() *TypeObj { return sliceType }
func (s *Slice) Repr() string {
	f := func(o Object) string {
		if o == nil || o == None {
			return ""
		}
		return o.Repr()
	}
	if s.Step != nil && s.Step != None {
		return fmt.Sprintf("slice(%s, %s, %s)", f(s.Start), f(s.Stop), f(s.Step))
	}
	return fmt.Sprintf("slice(%s, %s, None)", f(s.Start), f(s.Stop))
}
func (s *Slice) Str() string { return s.Repr() }

// ==================== 通用工具 ====================

// StrLen 按字符计数
func StrLen(s string) int { return utf8.RuneCountInString(s) }

// StrIndex 按字符索引
func StrIndex(s string, i int) (rune, bool) {
	rs := []rune(s)
	if i < 0 || i >= len(rs) {
		return 0, false
	}
	return rs[i], true
}

// StrSlice 按字符切片
func StrSlice(s string, start, stop, step int) string {
	rs := []rune(s)
	n := len(rs)
	if step == 0 {
		return ""
	}
	if step > 0 {
		if start < 0 {
			start += n
		}
		if stop < 0 {
			stop += n
		}
		if start < 0 {
			start = 0
		}
		if stop > n {
			stop = n
		}
		var sb strings.Builder
		for i := start; i < stop; i += step {
			sb.WriteRune(rs[i])
		}
		return sb.String()
	}
	// 负步长
	if start < 0 {
		start += n
	}
	if stop < 0 {
		stop += n
	}
	if start >= n {
		start = n - 1
	}
	if stop < -1 {
		stop = -1
	}
	var sb strings.Builder
	for i := start; i > stop; i += step {
		if i >= 0 && i < n {
			sb.WriteRune(rs[i])
		}
	}
	return sb.String()
}

// SortedKeys 排序键（用于确定性输出）
func SortedKeys(keys []Object) []Object {
	out := append([]Object{}, keys...)
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].Repr() < out[j].Repr()
	})
	return out
}

// ==================== 导出类型别名（供运行时使用） ====================

var (
	TypeType    = typeType
	NoneType    = noneType
	BoolType    = boolType
	IntType     = intType
	FloatType   = floatType
	StrType     = strType
	BytesType   = bytesType
	ListType    = listType
	TupleType   = tupleType
	DictType    = dictType
	SetType     = setType
	RangeType   = rangeType
	SliceType   = sliceType
	FuncType    = funcType
	BuiltinType = builtinType
	MethodType  = methodType
	ModuleType  = moduleType
	GenType     = genType
	IterType    = iterType
	ExcType     = excType
)
