package tests

import (
	"runtime"
	"testing"

	"zuzo/object"
)

// ================================================================
// Arena 单元测试
// ================================================================

func TestArena_StringAllocAndGet(t *testing.T) {
	arena := object.NewArena(64)

	v1 := arena.AllocString("hello")
	v2 := arena.AllocString("world")

	if !v1.IsHeap() {
		t.Fatal("AllocString should return heap Value")
	}
	if v1.HeapType() != object.HeapTypeString() {
		t.Fatalf("expected heapString type, got %v", v1.HeapType())
	}
	if arena.GetString(v1) != "hello" {
		t.Fatalf("expected 'hello', got '%s'", arena.GetString(v1))
	}
	if arena.GetString(v2) != "world" {
		t.Fatalf("expected 'world', got '%s'", arena.GetString(v2))
	}
}

func TestArena_FloatAllocAndGet(t *testing.T) {
	arena := object.NewArena(64)

	v1 := arena.AllocFloat(3.14)
	v2 := arena.AllocFloat(2.718)

	if arena.GetFloat(v1) != 3.14 {
		t.Fatalf("expected 3.14, got %f", arena.GetFloat(v1))
	}
	if arena.GetFloat(v2) != 2.718 {
		t.Fatalf("expected 2.718, got %f", arena.GetFloat(v2))
	}
}

func TestArena_BoolAllocAndGet(t *testing.T) {
	arena := object.NewArena(64)

	v1 := arena.AllocBool(true)
	v2 := arena.AllocBool(false)

	if !arena.GetBool(v1) {
		t.Fatal("expected true")
	}
	if arena.GetBool(v2) {
		t.Fatal("expected false")
	}
}

func TestArena_ArrayAllocAndGet(t *testing.T) {
	arena := object.NewArena(64)

	elems := []object.Value{
		object.Smi(10),
		object.Smi(20),
		object.Smi(30),
	}
	v := arena.AllocArray(elems)

	arr := arena.GetArray(v)
	if len(arr) != 3 {
		t.Fatalf("expected 3 elements, got %d", len(arr))
	}
	if arr[0].AsSmi() != 10 {
		t.Fatalf("expected 10, got %d", arr[0].AsSmi())
	}
	if arr[2].AsSmi() != 30 {
		t.Fatalf("expected 30, got %d", arr[2].AsSmi())
	}
}

func TestArena_Reset(t *testing.T) {
	arena := object.NewArena(64)

	arena.AllocString("a")
	arena.AllocString("b")
	arena.AllocFloat(1.0)
	arena.AllocBool(true)

	strCount, fltCount, blnCount, _ := arena.Stats()
	if strCount != 2 || fltCount != 1 || blnCount != 1 {
		t.Fatalf("before reset: expected 2/1/1, got %d/%d/%d", strCount, fltCount, blnCount)
	}

	arena.Reset()

	strCount, fltCount, blnCount, _ = arena.Stats()
	if strCount != 0 || fltCount != 0 || blnCount != 0 {
		t.Fatalf("after reset: expected 0/0/0, got %d/%d/%d", strCount, fltCount, blnCount)
	}

	v := arena.AllocString("fresh")
	if arena.GetString(v) != "fresh" {
		t.Fatalf("expected 'fresh', got '%s'", arena.GetString(v))
	}
}

func TestArena_MarkAndResetTo(t *testing.T) {
	arena := object.NewArena(64)

	arena.AllocString("keep1")
	arena.AllocString("keep2")
	mark := arena.Mark()

	arena.AllocString("temp1")
	arena.AllocString("temp2")
	arena.AllocFloat(9.9)

	strCount, fltCount, _, _ := arena.Stats()
	if strCount != 4 || fltCount != 1 {
		t.Fatalf("before ResetTo: expected 4/1, got %d/%d", strCount, fltCount)
	}

	arena.ResetTo(mark)

	strCount, fltCount, _, _ = arena.Stats()
	if strCount != 2 || fltCount != 0 {
		t.Fatalf("after ResetTo: expected 2/0, got %d/%d", strCount, fltCount)
	}
}

func TestArena_ValueToObject_Smi(t *testing.T) {
	arena := object.NewArena(64)
	v := object.Smi(42)
	obj := arena.ValueToObject(v)

	intObj, ok := obj.(*object.Integer)
	if !ok {
		t.Fatalf("expected *Integer, got %T", obj)
	}
	if intObj.Value != 42 {
		t.Fatalf("expected 42, got %d", intObj.Value)
	}
}

func TestArena_ValueToObject_String(t *testing.T) {
	arena := object.NewArena(64)
	v := arena.AllocString("test string")
	obj := arena.ValueToObject(v)

	strObj, ok := obj.(*object.String)
	if !ok {
		t.Fatalf("expected *String, got %T", obj)
	}
	if strObj.Value != "test string" {
		t.Fatalf("expected 'test string', got '%s'", strObj.Value)
	}
}

func TestArena_ValueToObject_Float(t *testing.T) {
	arena := object.NewArena(64)
	v := arena.AllocFloat(3.14159)
	obj := arena.ValueToObject(v)

	fltObj, ok := obj.(*object.Float)
	if !ok {
		t.Fatalf("expected *Float, got %T", obj)
	}
	if fltObj.Value != 3.14159 {
		t.Fatalf("expected 3.14159, got %f", fltObj.Value)
	}
}

func TestArena_ValueToObject_Bool(t *testing.T) {
	arena := object.NewArena(64)

	vTrue := arena.AllocBool(true)
	objTrue := arena.ValueToObject(vTrue)
	if objTrue != object.TrueVal {
		t.Fatalf("expected TrueVal, got %T", objTrue)
	}

	vFalse := arena.AllocBool(false)
	objFalse := arena.ValueToObject(vFalse)
	if objFalse != object.FalseVal {
		t.Fatalf("expected FalseVal, got %T", objFalse)
	}
}

func TestArena_ValueToObject_Nil(t *testing.T) {
	arena := object.NewArena(64)
	obj := arena.ValueToObject(0)

	if _, ok := obj.(*object.Null); !ok {
		t.Fatalf("expected *Null, got %T", obj)
	}
}

func TestArena_ObjectToValue_RoundTrip(t *testing.T) {
	arena := object.NewArena(64)

	tests := []struct {
		name string
		obj  object.Object
	}{
		{"Integer", &object.Integer{Value: 12345}},
		{"String", &object.String{Value: "round trip"}},
		{"Float", &object.Float{Value: 2.5}},
		{"Bool true", &object.Boolean{Value: true}},
		{"Bool false", &object.Boolean{Value: false}},
		{"Null", &object.Null{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v := arena.ObjectToValue(tt.obj)
			obj := arena.ValueToObject(v)

			switch tt.obj.(type) {
			case *object.Integer:
				if intObj, ok := obj.(*object.Integer); !ok || intObj.Value != tt.obj.(*object.Integer).Value {
					t.Fatalf("Integer round trip failed: got %v", obj)
				}
			case *object.String:
				if strObj, ok := obj.(*object.String); !ok || strObj.Value != tt.obj.(*object.String).Value {
					t.Fatalf("String round trip failed: got %v", obj)
				}
			case *object.Float:
				if fltObj, ok := obj.(*object.Float); !ok || fltObj.Value != tt.obj.(*object.Float).Value {
					t.Fatalf("Float round trip failed: got %v", obj)
				}
			case *object.Boolean:
				if boolObj, ok := obj.(*object.Boolean); !ok || boolObj.Value != tt.obj.(*object.Boolean).Value {
					t.Fatalf("Boolean round trip failed: got %v", obj)
				}
			case *object.Null:
				if _, ok := obj.(*object.Null); !ok {
					t.Fatalf("Null round trip failed: got %v", obj)
				}
			}
		})
	}
}

func TestArena_HeapValueEncoding(t *testing.T) {
	arena := object.NewArena(64)

	smiVal := object.Smi(42)
	strVal := arena.AllocString("test")
	fltVal := arena.AllocFloat(1.5)
	boolVal := arena.AllocBool(true)

	if !smiVal.IsSmi() {
		t.Fatal("Smi should be Smi")
	}
	if smiVal.IsHeap() {
		t.Fatal("Smi should not be Heap")
	}
	if !strVal.IsHeap() {
		t.Fatal("String should be Heap")
	}
	if strVal.IsSmi() {
		t.Fatal("String should not be Smi")
	}
	if !fltVal.IsHeap() {
		t.Fatal("Float should be Heap")
	}
	if !boolVal.IsHeap() {
		t.Fatal("Bool should be Heap")
	}
}

// ================================================================
// Arena vs Go new() 性能基准
// 使用 sink 变量防止编译器消除分配
// ================================================================

var sinkObj object.Object
var sinkStr string
var sinkFloat float64
var sinkBool bool
var sinkValue object.Value

// --- 单次分配对比 ---

// BenchmarkGoNew_String_Used 基准：Go new() 创建 String，使用结果防止优化
func BenchmarkGoNew_String_Used(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := &object.String{Value: "benchmark"}
		sinkStr = s.Value
		runtime.KeepAlive(s)
	}
}

// BenchmarkArena_String_Used 基准：Arena 分配 String，使用结果
func BenchmarkArena_String_Used(b *testing.B) {
	arena := object.NewArena(b.N + 64)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v := arena.AllocString("benchmark")
		sinkValue = v
	}
}

// --- 混合分配对比 ---

// BenchmarkGoNew_Mixed_Used 基准：Go new() 混合分配（防止优化消除）
func BenchmarkGoNew_Mixed_Used(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := &object.String{Value: "hello"}
		f := &object.Float{Value: 3.14}
		bl := &object.Boolean{Value: true}
		sinkStr = s.Value
		sinkFloat = f.Value
		sinkBool = bl.Value
		runtime.KeepAlive(s)
		runtime.KeepAlive(f)
		runtime.KeepAlive(bl)
	}
}

// BenchmarkArena_Mixed_Used 基准：Arena 混合分配
func BenchmarkArena_Mixed_Used(b *testing.B) {
	arena := object.NewArena(b.N*3 + 256)
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		v1 := arena.AllocString("hello")
		v2 := arena.AllocFloat(3.14)
		v3 := arena.AllocBool(true)
		sinkValue = v1 ^ v2 ^ v3
	}
}

// --- 批量重置 vs GC 核心对比 ---

// BenchmarkArena_ResetVsGC 模拟实际场景：
// 每轮分配 1000 个对象，然后释放。Arena 用 Reset，Go 用 GC。
const allocsPerRound = 1000

func BenchmarkBatchAlloc_ArenaReset(b *testing.B) {
	arena := object.NewArena(allocsPerRound*2 + 64)
	b.ReportAllocs()
	b.ResetTimer()
	for round := 0; round < b.N; round++ {
		for i := 0; i < allocsPerRound; i++ {
			v1 := arena.AllocString("bench")
			v2 := arena.AllocFloat(1.5)
			sinkValue = v1 ^ v2
		}
		arena.Reset() // O(1): 设置 4 个 slice 长度为 0
	}
}

func BenchmarkBatchAlloc_GoGC(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for round := 0; round < b.N; round++ {
		for i := 0; i < allocsPerRound; i++ {
			s := &object.String{Value: "bench"}
			f := &object.Float{Value: 1.5}
			sinkStr = s.Value
			sinkFloat = f.Value
		}
		runtime.GC() // 显式触发 GC（模拟 GC 压力）
	}
}

// BenchmarkBatchAlloc_GoNoGC 基准：Go 分配但不显式 GC（让 GC 自行决定）
func BenchmarkBatchAlloc_GoNoGC(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for round := 0; round < b.N; round++ {
		for i := 0; i < allocsPerRound; i++ {
			s := &object.String{Value: "bench"}
			f := &object.Float{Value: 1.5}
			sinkStr = s.Value
			sinkFloat = f.Value
		}
		// 不触发 GC，让 Go 运行时自行管理
	}
}

// --- 读取性能 ---

func BenchmarkArena_GetString(b *testing.B) {
	arena := object.NewArena(1024)
	v := arena.AllocString("benchmark string for read test")
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		sinkStr = arena.GetString(v)
	}
}

func BenchmarkArena_ValueToObject(b *testing.B) {
	arena := object.NewArena(1024)
	vStr := arena.AllocString("test")
	vSmi := object.Smi(42)
	b.ResetTimer()
	b.Run("Smi", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkObj = arena.ValueToObject(vSmi)
		}
	})
	b.Run("String", func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			sinkObj = arena.ValueToObject(vStr)
		}
	})
}

// --- Builtin 池化对比 ---

func BenchmarkBuiltinPool_NewEnv(b *testing.B) {
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		env := object.NewEnvironment()
		_, _ = env.Get("打印") // 使用结果防止优化
	}
}

func BenchmarkBuiltinPool_ReuseEnv(b *testing.B) {
	env := object.NewEnvironment()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = env.Get("打印")
	}
}
