package object

// arena.go — Bump Allocator（碰撞分配器）
//
// 核心思想：预分配大块内存，每次分配只需移动指针（append 到 slice），
// 批量释放只需重置长度。消除 Go new() 的锁、GC 屏障、清零开销。
//
// GC 安全性：
//   不使用 unsafe.Pointer + []byte 方案（Go GC 不扫描 []byte 中的指针，
//   会导致 string 等含指针对象被误回收）。改用类型化 Go slice 作为池：
//   - []string, []float64, [][]Value 等 are GC-visible
//   - GC 正确追踪池内对象的指针
//   - 仍获得 bump allocation + batch reset 的核心优势
//
// Value 堆句柄编码（LSB=0 路径）：
//   Smi:  | int64 (63 bits)                | 1 |   ← LSB = 1
//   Heap: | slot index (59 bits) | type (4 bits) | 0 |   ← LSB = 0
//          bits 5-63: 池内索引     bits 1-4: 类型标记  bit 0: 0=heap

import (
	"sync"
)

// ================================================================
// 堆句柄类型标记（4 bits, 值 0x0~0xF，LSB=0）
// ================================================================

const (
	heapTagShift  = 1                     // 类型标记从 bit 1 开始
	heapTagBits   = 4                     // 4 bits 类型
	heapTagMask   = Value(0x1E)           // bits 1-4
	heapIdxShift  = 5                     // 索引从 bit 5 开始
	heapNil       = Value(0)              // 零值 = Nil
	heapString    = Value(0x02)           // type=1
	heapFloat     = Value(0x04)           // type=2
	heapBool      = Value(0x06)           // type=3
	heapArray     = Value(0x08)           // type=4
	heapHash      = Value(0x0A)           // type=5
	heapBuiltin   = Value(0x0C)           // type=6
	heapFunc      = Value(0x0E)           // type=7
)

// makeHeapValue 构造堆句柄 Value
//go:inline
func makeHeapValue(tag Value, idx int) Value {
	return (Value(idx) << heapIdxShift) | tag
}

// IsHeap 判断 Value 是否为堆对象句柄（非 Smi 且非零值）
//go:inline
func (v Value) IsHeap() bool {
	return v != 0 && v&smiMask == 0
}

// HeapType 返回堆对象的类型标记
//go:inline
func (v Value) HeapType() Value {
	return v & heapTagMask
}

// HeapIndex 返回堆对象的池内索引
//go:inline
func (v Value) HeapIndex() int {
	return int(v >> heapIdxShift)
}

// HeapTypeString 返回 String 类型的堆标记（用于外部比较）
func HeapTypeString() Value { return heapString }

// HeapTypeFloat 返回 Float 类型的堆标记
func HeapTypeFloat() Value { return heapFloat }

// HeapTypeBool 返回 Bool 类型的堆标记
func HeapTypeBool() Value { return heapBool }

// HeapTypeArray 返回 Array 类型的堆标记
func HeapTypeArray() Value { return heapArray }

// ================================================================
// Arena — 类型化碰撞分配器
// ================================================================

// Arena 预分配的类型化对象池，bump allocation + batch reset
type Arena struct {
	// 类型化池——GC 可见，安全追踪指针
	strings  []string     // String 池
	floats   []float64    // Float 池
	bools    []bool       // Boolean 池
	arrays   [][]Value    // Array 池（元素为 Value）
	hashKeys []Value      // Hash 键池
	hashVals []Value      // Hash 值池

	// 快照标记（用于 scoped reset）
	marks arenaMarks
}

type arenaMarks struct {
	strMark  int
	fltMark  int
	blnMark  int
	arrMark  int
}

// NewArena 创建 Arena，preAlloc 为每种池的预分配容量
func NewArena(preAlloc int) *Arena {
	if preAlloc < 64 {
		preAlloc = 64
	}
	return &Arena{
		strings: make([]string, 0, preAlloc),
		floats:  make([]float64, 0, preAlloc),
		bools:   make([]bool, 0, preAlloc),
		arrays:  make([][]Value, 0, preAlloc/4),
	}
}

// --- Bump 分配方法（每次分配 = 1 次 append，无 new()） ---

// AllocString 分配一个字符串到池中，返回堆句柄 Value
//go:inline
func (a *Arena) AllocString(s string) Value {
	idx := len(a.strings)
	a.strings = append(a.strings, s)
	return makeHeapValue(heapString, idx)
}

// AllocFloat 分配一个浮点数到池中
//go:inline
func (a *Arena) AllocFloat(f float64) Value {
	idx := len(a.floats)
	a.floats = append(a.floats, f)
	return makeHeapValue(heapFloat, idx)
}

// AllocBool 分配一个布尔值到池中
//go:inline
func (a *Arena) AllocBool(b bool) Value {
	idx := len(a.bools)
	a.bools = append(a.bools, b)
	return makeHeapValue(heapBool, idx)
}

// AllocArray 分配一个数组到池中（elements 为 Value 切片）
//go:inline
func (a *Arena) AllocArray(elements []Value) Value {
	idx := len(a.arrays)
	a.arrays = append(a.arrays, elements)
	return makeHeapValue(heapArray, idx)
}

// --- 读取方法 ---

// GetString 按堆句柄获取字符串
//go:inline
func (a *Arena) GetString(v Value) string {
	return a.strings[v.HeapIndex()]
}

// GetFloat 按堆句柄获取浮点数
//go:inline
func (a *Arena) GetFloat(v Value) float64 {
	return a.floats[v.HeapIndex()]
}

// GetBool 按堆句柄获取布尔值
//go:inline
func (a *Arena) GetBool(v Value) bool {
	return a.bools[v.HeapIndex()]
}

// GetArray 按堆句柄获取数组元素切片
//go:inline
func (a *Arena) GetArray(v Value) []Value {
	return a.arrays[v.HeapIndex()]
}

// --- Value → Object 转换（循环退出时同步回 Environment） ---

// ValueToObject 将 Value 转换为 Object（需要 Arena 引用）
// 对于 Smi 值，使用小整数缓存
// 对于堆句柄，从 Arena 池中取出并包装为 Object
//go:inline
func (a *Arena) ValueToObject(v Value) Object {
	if v.IsSmi() {
		return v.AsObject() // Smi 路径，无需 Arena
	}
	if v == 0 {
		return &Null{}
	}
	switch v.HeapType() {
	case heapString:
		return &String{Value: a.strings[v.HeapIndex()]}
	case heapFloat:
		return &Float{Value: a.floats[v.HeapIndex()]}
	case heapBool:
		if a.bools[v.HeapIndex()] {
			return TrueVal
		}
		return FalseVal
	case heapArray:
		elems := a.arrays[v.HeapIndex()]
		objs := make([]Object, len(elems))
		for i, ev := range elems {
			objs[i] = a.ValueToObject(ev)
		}
		return &Array{Elements: objs}
	}
	return &Null{}
}

// ObjectToValue 将 Object 转换为 Value（存入 Context 槽位）
// Integer → Smi（零分配）
// String/Float/Bool/Array → Arena 堆句柄（1 次 append，无 new()）
//go:inline
func (a *Arena) ObjectToValue(obj Object) Value {
	if obj == nil {
		return 0
	}
	switch o := obj.(type) {
	case *Integer:
		return Smi(o.Value)
	case *String:
		return a.AllocString(o.Value)
	case *Float:
		return a.AllocFloat(o.Value)
	case *Boolean:
		return a.AllocBool(o.Value)
	case *Null:
		return 0
	}
	// 其他类型暂不支持 Arena 编码，存零值
	return 0
}

// --- 批量重置 ---

// Mark 记录当前分配位置（用于 scoped reset）
func (a *Arena) Mark() arenaMarks {
	return arenaMarks{
		strMark: len(a.strings),
		fltMark: len(a.floats),
		blnMark: len(a.bools),
		arrMark: len(a.arrays),
	}
}

// ResetTo 回退到标记位置——O(1) 批量释放，无 GC 介入
func (a *Arena) ResetTo(m arenaMarks) {
	a.strings = a.strings[:m.strMark]
	a.floats = a.floats[:m.fltMark]
	a.bools = a.bools[:m.blnMark]
	a.arrays = a.arrays[:m.arrMark]
}

// Reset 全量重置——O(1) 释放所有 Arena 对象
func (a *Arena) Reset() {
	a.strings = a.strings[:0]
	a.floats = a.floats[:0]
	a.bools = a.bools[:0]
	a.arrays = a.arrays[:0]
}

// Stats 返回 Arena 统计信息
func (a *Arena) Stats() (strCount, fltCount, blnCount, arrCount int) {
	return len(a.strings), len(a.floats), len(a.bools), len(a.arrays)
}

// ================================================================
// 全局预分配常量对象（消除重复创建）
// ================================================================

var (
	TrueVal  = &Boolean{Value: true}
	FalseVal = &Boolean{Value: false}
	NullVal  = &Null{}
)

// ================================================================
// Builtin 函数池——消除每次创建环境时的 150+ 次分配
// ================================================================

var (
	builtinPool     map[string]*Builtin
	builtinPoolOnce sync.Once
)

// InitBuiltinPool 初始化全局 Builtin 池（仅执行一次）
func InitBuiltinPool(builtins map[string]BuiltinFunction) {
	builtinPoolOnce.Do(func() {
		builtinPool = make(map[string]*Builtin, len(builtins))
		for name, fn := range builtins {
			builtinPool[name] = &Builtin{
				Fn:             fn,
				Name:           name,
				AllowErrorArgs: name == "错误码" || name == "错误信息" || name == "错误行",
			}
		}
	})
}

// GetBuiltin 从池中获取 Builtin（零分配，复用已有对象）
//go:inline
func GetBuiltin(name string) *Builtin {
	if b, ok := builtinPool[name]; ok {
		return b
	}
	return nil
}

// BuiltinPoolCount 返回池中 Builtin 数量
func BuiltinPoolCount() int {
	return len(builtinPool)
}
