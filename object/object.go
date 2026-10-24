package object

import (
	"fmt"
	"hash/fnv"
	"math"
	"strconv"
	"strings"

	"zuzo/ast"
)

// ObjectType 运行时对象类型的字符串表示
type ObjectType string

// 对象类型常量
const (
	INTEGER_OBJ  = "整数"
	FLOAT_OBJ    = "浮点数"
	STRING_OBJ   = "字符串"
	BOOLEAN_OBJ  = "布尔"
	NULL_OBJ     = "空"
	RETURN_VALUE = "返回值"
	ERROR_OBJ    = "错误"
	FUNCTION_OBJ = "函数"
	BUILTIN_OBJ  = "内置函数"
	ARRAY_OBJ    = "数组"
	HASH_OBJ     = "哈希"
	MODULE_OBJ   = "模块"
	INSTANCE_OBJ = "实例"   // 定义实例
	DEFINE_OBJ   = "定义"   // 定义蓝图
	BREAK_SIG    = "跳出信号" // break 控制流信号
	CONTINUE_SIG = "跳过信号" // continue 控制流信号
)

// Object 所有运行时对象的接口
type Object interface {
	Type() ObjectType
	Inspect() string
}

// Integer 整数
type Integer struct{ Value int64 }

func (i *Integer) Type() ObjectType { return INTEGER_OBJ }
func (i *Integer) Inspect() string  { return fmt.Sprintf("%d", i.Value) }

// HashKey 用于哈希表的键
func (i *Integer) HashKey() HashKey {
	return HashKey{Type: INTEGER_OBJ, Value: uint64(i.Value)}
}

// Float 浮点数
type Float struct{ Value float64 }

func (f *Float) Type() ObjectType { return FLOAT_OBJ }
func (f *Float) Inspect() string {
	// 去除不必要的小数位，但保留至少一位
	s := strconv.FormatFloat(f.Value, 'f', -1, 64)
	return s
}

// HashKey 用于哈希表的键
func (f *Float) HashKey() HashKey {
	return HashKey{Type: FLOAT_OBJ, Value: math.Float64bits(f.Value)}
}

// String 字符串
type String struct{ Value string }

func (s *String) Type() ObjectType { return STRING_OBJ }
func (s *String) Inspect() string  { return s.Value }

func (s *String) HashKey() HashKey {
	h := fnv.New64a()
	h.Write([]byte(s.Value))
	return HashKey{Type: STRING_OBJ, Value: h.Sum64()}
}

// Boolean 布尔值
type Boolean struct{ Value bool }

func (b *Boolean) Type() ObjectType { return BOOLEAN_OBJ }
func (b *Boolean) Inspect() string {
	if b.Value {
		return "真"
	}
	return "假"
}

func (b *Boolean) HashKey() HashKey {
	var v uint64
	if b.Value {
		v = 1
	}
	return HashKey{Type: BOOLEAN_OBJ, Value: v}
}

// Null 空值
type Null struct{}

func (n *Null) Type() ObjectType { return NULL_OBJ }
func (n *Null) Inspect() string  { return "空" }

// ReturnValue 返回值包装器
type ReturnValue struct{ Value Object }

func (rv *ReturnValue) Type() ObjectType { return RETURN_VALUE }
func (rv *ReturnValue) Inspect() string  { return rv.Value.Inspect() }

// Error 错误对象
// Line 和 Column 为 0 表示未知位置
type Error struct {
	Message    string
	Line       int
	Column     int
	Code       string   // 错误码，如 "E201"
	StackTrace []string // 调用栈（函数名 + 行号），最外层在前
}

func (e *Error) Type() ObjectType { return ERROR_OBJ }
func (e *Error) Inspect() string {
	var sb strings.Builder
	if e.Line > 0 {
		if e.Code != "" {
			sb.WriteString(fmt.Sprintf("运行错误 [%s] 行 %d: %s", e.Code, e.Line, e.Message))
		} else {
			sb.WriteString(fmt.Sprintf("运行错误 行 %d: %s", e.Line, e.Message))
		}
	} else if e.Code != "" {
		sb.WriteString(fmt.Sprintf("运行错误 [%s]: %s", e.Code, e.Message))
	} else {
		sb.WriteString("运行错误: " + e.Message)
	}
	if len(e.StackTrace) > 0 {
		sb.WriteString("\n调用栈:")
		for i, frame := range e.StackTrace {
			sb.WriteString(fmt.Sprintf("\n  %d. %s", i+1, frame))
		}
	}
	return sb.String()
}

// Function 用户定义的函数
type Function struct {
	Parameters []*ast.Identifier
	Body       *ast.BlockStatement
	Env        *Environment
	Name       string
}

func (f *Function) Type() ObjectType { return FUNCTION_OBJ }
func (f *Function) Inspect() string {
	var params []string
	for _, p := range f.Parameters {
		params = append(params, p.String())
	}
	return fmt.Sprintf("函数 %s(%s) { ... }", f.Name, fmt.Sprintf("%s", params))
}

// BuiltinFunction 内置函数类型
type BuiltinFunction func(args ...Object) Object

// Builtin 内置函数对象
type Builtin struct {
	Fn             BuiltinFunction // 实现函数
	Name           string          // 内置函数名（如 "打印"），由 Builtin 池写入
	AllowErrorArgs bool            // 允许错误对象作为参数（错误内省系列：错误码/错误信息/错误行）
}

func (b *Builtin) Type() ObjectType { return BUILTIN_OBJ }
func (b *Builtin) Inspect() string  { return "内置函数" }

// Array 数组
type Array struct{ Elements []Object }

func (a *Array) Type() ObjectType { return ARRAY_OBJ }
func (a *Array) Inspect() string {
	var elems []string
	for _, e := range a.Elements {
		elems = append(elems, e.Inspect())
	}
	return "[" + joinStrings(elems, ", ") + "]"
}

// HashKey 哈希键
type HashKey struct {
	Type  ObjectType
	Value uint64
}

// HashPair 哈希键值对
type HashPair struct {
	Key   Object
	Value Object
}

// Hash 哈希表
// Keys 追踪插入顺序，保证遍历和 Inspect 的确定性（非随机）。
type Hash struct {
	Pairs map[HashKey]HashPair
	Keys  []HashKey // 插入顺序记录
}

// SetKey 设置哈希键值对，同时维护插入顺序。
// 如果键已存在则仅更新值（不改变顺序）；新键则追加到 Keys 尾部。
func (h *Hash) SetKey(key Object, value Object) {
	if h.Pairs == nil {
		h.Pairs = make(map[HashKey]HashPair)
	}
	hk := key.(Hashable).HashKey()
	if _, exists := h.Pairs[hk]; !exists {
		h.Keys = append(h.Keys, hk)
	}
	h.Pairs[hk] = HashPair{Key: key, Value: value}
}

func (h *Hash) Type() ObjectType { return HASH_OBJ }
func (h *Hash) Inspect() string {
	var pairs []string
	for _, hk := range h.Keys {
		pair := h.Pairs[hk]
		pairs = append(pairs, fmt.Sprintf("%s: %s", pair.Key.Inspect(), pair.Value.Inspect()))
	}
	return "{" + joinStrings(pairs, ", ") + "}"
}

// Hashable 可哈希对象接口
type Hashable interface {
	HashKey() HashKey
}

// Environment 环境用于管理变量作用域
type Environment struct {
	store      map[string]Object // 兼容：原有 map 存储
	slots      []Object          // SAP：Object 槽位数组（快速存取）
	valueSlots []Value           // Smi：64 位标记值槽位（零堆分配）
	slotMap    map[string]int    // SAP：变量名 → 槽位索引映射（仅 SAP 根环境持有）
	outer      *Environment
	context    *Context // De Bruijn：扁平帧栈（Level 5 热路径使用）
}

// Module 模块对象：导入文件的命名空间包装
type Module struct {
	Name string       // 模块名
	Env  *Environment // 模块的环境（包含所有导出的变量/函数）
}

func (m *Module) Type() ObjectType { return MODULE_OBJ }
func (m *Module) Inspect() string {
	return "模块 " + m.Name
}

// DefineField 定义字段：名称 + 默认值
type DefineField struct {
	Name       string
	DefaultVal Object
}

// Define 定义蓝图：存储字段定义和默认值
type Define struct {
	Name   string
	Fields []DefineField
}

func (d *Define) Type() ObjectType { return DEFINE_OBJ }
func (d *Define) Inspect() string {
	return "定义 " + d.Name
}

// Instance 定义实例：具有命名属性的对象
type Instance struct {
	DefName string
	Props   map[string]Object
}

func (i *Instance) Type() ObjectType { return INSTANCE_OBJ }
func (i *Instance) Inspect() string {
	var pairs []string
	for k, v := range i.Props {
		pairs = append(pairs, fmt.Sprintf("%s: %s", k, v.Inspect()))
	}
	return "{" + joinStrings(pairs, ", ") + "}"
}

// BreakSignal 跳出循环信号
type BreakSignal struct{}

func (b *BreakSignal) Type() ObjectType { return BREAK_SIG }
func (b *BreakSignal) Inspect() string  { return "跳出" }

// ContinueSignal 跳过本次循环信号
type ContinueSignal struct{}

func (c *ContinueSignal) Type() ObjectType { return CONTINUE_SIG }
func (c *ContinueSignal) Inspect() string  { return "跳过" }

// NewEnvironment 创建一个新的环境
func NewEnvironment() *Environment {
	s := make(map[string]Object)
	return &Environment{store: s, outer: nil}
}

// NewEnclosedEnvironment 创建一个有外层环境的环境（用于函数闭包）
func NewEnclosedEnvironment(outer *Environment) *Environment {
	env := NewEnvironment()
	env.outer = outer
	return env
}

// NewSlotEnvironment 创建带槽位的环境（SAP 优化路径）
// slotMap 为变量名→槽位索引映射，用于 Set/Assign 时同步更新槽位
// 同时初始化 valueSlots，启用 Smi-Tagging 快速路径
func NewSlotEnvironment(slotCount int, outer *Environment, slotMap map[string]int) *Environment {
	return &Environment{
		store:      make(map[string]Object),
		slots:      make([]Object, slotCount),
		valueSlots: make([]Value, slotCount),
		slotMap:    slotMap,
		outer:      outer,
	}
}

// syncSlot 同步更新槽位（如果变量有对应的槽位索引）
// 修复 SAP slot/map 脑裂 Bug：非 SAP 编译路径的 Set/Assign 也必须同步槽位
// 同时同步 valueSlots：如果是 Integer，编码为 Smi 写入 valueSlots
func (e *Environment) syncSlot(name string, val Object) {
	if e.slotMap != nil {
		if slot, ok := e.slotMap[name]; ok {
			if slot < len(e.slots) {
				e.slots[slot] = val
			}
			// Smi 同步：如果是 Integer，编码为 Smi 写入 valueSlots
			if slot < len(e.valueSlots) {
				if smiVal, ok := ObjectAsSmi(val); ok {
					e.valueSlots[slot] = smiVal
				}
			}
		}
	}
}

// Get 获取变量值
func (e *Environment) Get(name string) (Object, bool) {
	obj, ok := e.store[name]
	if !ok && e.outer != nil {
		obj, ok = e.outer.Get(name)
	}
	return obj, ok
}

// Set 设置变量值（在当前作用域创建/覆盖）
func (e *Environment) Set(name string, val Object) Object {
	e.store[name] = val
	e.syncSlot(name, val) // 同步槽位，防止 SAP 读取到过期值
	return val
}

// Assign 赋值：在作用域链中查找已有变量并更新，找不到则在当前作用域创建
func (e *Environment) Assign(name string, val Object) Object {
	if _, ok := e.store[name]; ok {
		e.store[name] = val
		e.syncSlot(name, val) // 同步槽位，防止 SAP 读取到过期值
		return val
	}
	if e.outer != nil {
		if _, ok := e.outer.Get(name); ok {
			return e.outer.Assign(name, val)
		}
	}
	e.store[name] = val
	e.syncSlot(name, val) // 同步槽位
	return val
}

// --- SAP 槽位操作（O(1) 数组偏移，无哈希） ---

// GetSlot 按槽位索引获取值
func (e *Environment) GetSlot(idx int) Object {
	if idx < len(e.slots) {
		return e.slots[idx]
	}
	return nil
}

// SetSlot 按槽位索引设置值
func (e *Environment) SetSlot(idx int, val Object) {
	if idx >= len(e.slots) {
		// 扩容
		newSlots := make([]Object, idx+1)
		copy(newSlots, e.slots)
		e.slots = newSlots
	}
	e.slots[idx] = val
}

// HasSlots 是否启用了槽位模式
func (e *Environment) HasSlots() bool {
	return e.slots != nil
}

// --- Smi Value 槽位操作（零堆分配，直接寄存器级运算） ---

// GetValueSlot 按 Smi 槽位索引获取 Value（8 字节标量，无接口开销）
//
//go:inline
func (e *Environment) GetValueSlot(idx int) Value {
	if idx < len(e.valueSlots) {
		return e.valueSlots[idx]
	}
	return 0 // 零值 = 非 Smi（LSB=0），调用者应回退到 Object 槽位
}

// SetValueSlot 按 Smi 槽位索引设置 Value
//
//go:inline
func (e *Environment) SetValueSlot(idx int, val Value) {
	if idx >= len(e.valueSlots) {
		// 扩容
		newSlots := make([]Value, idx+1)
		copy(newSlots, e.valueSlots)
		e.valueSlots = newSlots
	}
	e.valueSlots[idx] = val
}

// HasValueSlots 是否启用了 Smi 槽位模式
//
//go:inline
func (e *Environment) HasValueSlots() bool {
	return e.valueSlots != nil
}

// GetValueSlots 返回整个 valueSlots 切片（用于 Context 同步）
func (e *Environment) GetValueSlots() []Value {
	return e.valueSlots
}

// --- De Bruijn Context 操作 ---

// GetContext 返回关联的扁平帧栈 Context
func (e *Environment) GetContext() *Context {
	return e.context
}

// SetContext 设置关联的 Context
func (e *Environment) SetContext(ctx *Context) {
	e.context = ctx
}

// HasContext 是否启用了 De Bruijn 扁平帧栈
func (e *Environment) HasContext() bool {
	return e.context != nil
}

// GetAll 返回当前作用域中的所有变量（不含外层，用于模块导出）
func (e *Environment) GetAll() map[string]Object {
	result := make(map[string]Object)
	for k, v := range e.store {
		result[k] = v
	}
	return result
}

// Outer 返回外层环境（用于调试）
func (e *Environment) Outer() *Environment {
	return e.outer
}

// joinStrings 连接字符串切片（封装 strings.Join）
func joinStrings(strs []string, sep string) string {
	return strings.Join(strs, sep)
}
