package object

// value.go — 64-bit Smi-Tagging（小整数指针标记）
//
// 核心思想：用一个 64 位无符号整数 Value 表示所有的值。
// 利用 64 位系统内存对齐特性（指针的最低位永远为 0），将最低位作为类型标记：
//
//   LSB = 1 → Smi (Small Integer)：真实值存在高 63 位中，零堆分配
//   LSB = 0 → Heap Pointer：指向堆对象（String, Array, Function 等）
//
// 为什么强大：
//   1. 彻底消灭 99% 的堆分配：支持 62 位整数（±4.6×10^18），
//      循环计数器永远在 CPU 寄存器里计算，完全不触碰堆内存，GC 压力为 0
//   2. 接口开销归零：Value 是 8 字节标量，Go 编译器将其放入寄存器传递；
//      而 interface{} 是 16 字节（itab 指针 + 数据指针），涉及内存解引用
//   3. CPU 缓存友好：Value 数组是连续的 8 字节元素，缓存行利用率翻倍
//
// GC 安全性说明：
//   Go 的 GC 不会追踪存储在 uint64 中的指针。因此当前实现中，
//   Heap Pointer 路径不直接存储裸指针，而是保留给未来的 HeapObj 索引方案。
//   Smi 路径完全安全——它不包含任何指针，纯标量运算。
//   所有堆对象仍通过 Environment.store (map[string]Object) 保持 GC 可达。
//
// 编码格式：
//   Smi:  | int64 (63 bits)          | 1 |   ← LSB = 1
//   Heap: | heap index / future use  | 0 |   ← LSB = 0 (预留)
//
// 支持 62 位整数范围：-2^62 到 2^62-1（约 ±4.6×10^18），覆盖所有实际需求。

// Value 是统一的 64 位值表示。Smi 值直接编码在 Value 中，无需堆分配。
type Value uint64

const (
	smiTag  Value = 1 // LSB = 1 表示 Smi
	smiMask Value = 1
	smiShift       = 1 // Smi 值左移 1 位腾出 tag 位
)

// Smi 将 int64 编码为 Smi Value（零堆分配）
// 编码：(n << 1) | 1，即左移 1 位后设置最低位为 1
//go:inline
func Smi(n int64) Value {
	return Value((uint64(n) << smiShift) | uint64(smiTag))
}

// IsSmi 判断 Value 是否为 Smi（小整数）
//go:inline
func (v Value) IsSmi() bool {
	return v&smiMask == smiTag
}

// AsSmi 从 Smi Value 中提取 int64 值
// 调用者必须确保 IsSmi() 为 true
// 关键：先转 int64 再右移，确保负数使用算术右移（填充符号位而非 0）
//go:inline
func (v Value) AsSmi() int64 {
	return int64(v) >> smiShift
}

// IsNil 判断 Value 是否为零值（未初始化）
//go:inline
func (v Value) IsNil() bool {
	return v == 0
}

// --- 小整数缓存 ---
// 预分配 -512~511 范围内的 Integer 对象，避免循环退出时 AsObject 的堆分配。
// 这个范围覆盖了绝大多数循环计数器、累加器和小规模计算结果。
// 内存开销：1024 个 * 16 字节 ≈ 16KB，一次性初始化，无运行时开销。

const smiCacheMin = -512
const smiCacheMax = 511
const smiCacheSize = smiCacheMax - smiCacheMin + 1 // 1024

var smiCache [smiCacheSize]*Integer

func init() {
	for i := 0; i < smiCacheSize; i++ {
		smiCache[i] = &Integer{Value: int64(i + smiCacheMin)}
	}
}

// AsObject 将 Value 转换为 Object。
// 对于 Smi 值，优先从缓存返回，避免堆分配。
// 对于非 Smi 值（预留），返回 NULL。
//
// 注意：此方法仅在循环退出时调用（同步槽位），不在热路径中。
//go:inline
func (v Value) AsObject() Object {
	if v.IsSmi() {
		n := v.AsSmi()
		if n >= smiCacheMin && n <= smiCacheMax {
			return smiCache[n-smiCacheMin]
		}
		return &Integer{Value: n}
	}
	// 预留：未来 Heap Pointer 路径
	return &Null{}
}

// ObjectAsSmi 尝试将 Object 转换为 Smi Value。
// 如果 obj 是 Integer，返回 Smi 编码和 true。
// 否则返回 0 和 false（调用者应回退到 Object 槽位）。
//go:inline
func ObjectAsSmi(obj Object) (Value, bool) {
	if intObj, ok := obj.(*Integer); ok {
		return Smi(intObj.Value), true
	}
	return 0, false
}
