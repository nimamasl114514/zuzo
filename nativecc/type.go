package nativecc

import "fmt"

// TypeKind C 类型类别
type TypeKind int

const (
	TyVoid TypeKind = iota
	TyBool
	TyChar
	TyShort
	TyInt
	TyLong
	TyLongLong
	TyFloat
	TyDouble
	TyPtr
	TyArray
	TyFunc
	TyStruct
	TyUnion
	TyEnum
)

// Field 结构体/联合体成员
type Field struct {
	Name   string
	Ty     *CType
	Offset int64
}

// CType C 类型描述（Windows x64 布局：long=4, ptr=8）
type CType struct {
	Kind      TypeKind
	Size      int64
	Align     int64
	Unsigned  bool
	Const     bool
	Elem      *CType   // 指针/数组元素类型
	ArrayLen  int64    // 数组长度，-1 表示未知
	Ret       *CType   // 函数返回类型
	Params    []*CType // 函数参数类型
	Variadic  bool
	Fields    []*Field // struct/union 成员
	Tag       string
	Incomplete bool // 前向声明未定义
}

func (t *CType) String() string {
	if t == nil {
		return "<nil>"
	}
	switch t.Kind {
	case TyVoid:
		return "void"
	case TyBool:
		return "_Bool"
	case TyChar:
		if t.Unsigned {
			return "unsigned char"
		}
		return "char"
	case TyShort:
		if t.Unsigned {
			return "unsigned short"
		}
		return "short"
	case TyInt:
		if t.Unsigned {
			return "unsigned int"
		}
		return "int"
	case TyLong:
		if t.Unsigned {
			return "unsigned long"
		}
		return "long"
	case TyLongLong:
		if t.Unsigned {
			return "unsigned long long"
		}
		return "long long"
	case TyFloat:
		return "float"
	case TyDouble:
		return "double"
	case TyPtr:
		return t.Elem.String() + "*"
	case TyArray:
		return fmt.Sprintf("%s[%d]", t.Elem.String(), t.ArrayLen)
	case TyFunc:
		return "函数" + t.Ret.String() + "(" + fmt.Sprint(len(t.Params)) + "参)"
	case TyStruct:
		return "struct " + t.Tag
	case TyUnion:
		return "union " + t.Tag
	case TyEnum:
		return "enum " + t.Tag
	}
	return "?"
}

// 常用基础类型单例
var (
	TyVoidT   = &CType{Kind: TyVoid, Size: 0, Align: 1}
	TyBoolT   = &CType{Kind: TyBool, Size: 1, Align: 1}
	TyCharT   = &CType{Kind: TyChar, Size: 1, Align: 1}
	TyShortT  = &CType{Kind: TyShort, Size: 2, Align: 2}
	TyIntT    = &CType{Kind: TyInt, Size: 4, Align: 4}
	TyUIntT   = &CType{Kind: TyInt, Size: 4, Align: 4, Unsigned: true}
	TyLongT   = &CType{Kind: TyLong, Size: 4, Align: 4} // Windows LLP64
	TyULongT  = &CType{Kind: TyLong, Size: 4, Align: 4, Unsigned: true}
	TyLLongT  = &CType{Kind: TyLongLong, Size: 8, Align: 8}
	TyULLongT = &CType{Kind: TyLongLong, Size: 8, Align: 8, Unsigned: true}
	TyFloatT  = &CType{Kind: TyFloat, Size: 4, Align: 4}
	TyDoubleT = &CType{Kind: TyDouble, Size: 8, Align: 8}
	TyVoidPtr = &CType{Kind: TyPtr, Size: 8, Align: 8, Elem: TyVoidT}
	TyCharPtr = &CType{Kind: TyPtr, Size: 8, Align: 8, Elem: TyCharT}
)

// PtrTo 构造指针类型
func PtrTo(elem *CType) *CType {
	return &CType{Kind: TyPtr, Size: 8, Align: 8, Elem: elem}
}

// ArrayOf 构造数组类型
func ArrayOf(elem *CType, n int64) *CType {
	t := &CType{Kind: TyArray, Elem: elem, ArrayLen: n, Align: elem.Align}
	if n >= 0 {
		t.Size = elem.Size * n
	} else {
		t.Size = 0
		t.Incomplete = true
	}
	return t
}

// FuncOf 构造函数类型
func FuncOf(ret *CType, params []*CType, variadic bool) *CType {
	return &CType{Kind: TyFunc, Ret: ret, Params: params, Variadic: variadic, Size: 8, Align: 8}
}

// IsInteger 是否整数类（含 bool/char/enum）
func (t *CType) IsInteger() bool {
	switch t.Kind {
	case TyBool, TyChar, TyShort, TyInt, TyLong, TyLongLong, TyEnum:
		return true
	}
	return false
}

// IsFloat 是否浮点
func (t *CType) IsFloat() bool { return t.Kind == TyFloat || t.Kind == TyDouble }

// IsArith 是否算术类型
func (t *CType) IsArith() bool { return t.IsInteger() || t.IsFloat() }

// IsPtr 是否指针或数组（数组退化为指针）
func (t *CType) IsPtr() bool { return t.Kind == TyPtr }

// IsScalar 标量（整数/浮点/指针）
func (t *CType) IsScalar() bool { return t.IsArith() || t.IsPtr() }

// IsRecord 结构体/联合体
func (t *CType) IsRecord() bool { return t.Kind == TyStruct || t.Kind == TyUnion }

// Layout 计算 struct/union 布局（MSVC 规则：每字段对齐到自身 align）
func (t *CType) Layout() {
	if t.Kind != TyStruct && t.Kind != TyUnion {
		return
	}
	var offset int64
	var maxAlign int64 = 1
	for _, f := range t.Fields {
		a := f.Ty.Align
		if a <= 0 {
			a = 1
		}
		if t.Kind == TyStruct {
			// 向上对齐
			offset = alignUp(offset, a)
			f.Offset = offset
			offset += f.Ty.Size
		} else {
			f.Offset = 0
			if f.Ty.Size > offset {
				offset = f.Ty.Size
			}
		}
		if a > maxAlign {
			maxAlign = a
		}
	}
	t.Align = maxAlign
	if t.Kind == TyStruct {
		t.Size = alignUp(offset, maxAlign)
	} else {
		t.Size = alignUp(offset, maxAlign)
	}
}

func alignUp(v, a int64) int64 {
	if a <= 1 {
		return v
	}
	return (v + a - 1) / a * a
}

// SizeOf 类型大小（不完整类型返回 -1）
func (t *CType) SizeOf() int64 {
	if t == nil || t.Incomplete {
		return -1
	}
	return t.Size
}
