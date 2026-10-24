package object

import (
	"math"

	"zuzo/py/ast"
)

// Function 用户定义函数
type Function struct {
	Name        string
	QualName    string
	Params      *ast.Params
	Body        []ast.Stmt
	LambdaBody  ast.Expr // 非 nil 表示 lambda（直接求值表达式）
	Globals     *Env     // 全局环境
	Closure     *Env     // 定义时的环境
	ArgDefaults []Object // 与 Params.Args 等长，nil 表示无默认值
	Defaults    []Object
	KwDefaults  map[string]Object
	IsGenerator bool
	IsAsync     bool
	Doc         string
	Module      string
	Dict        *Dict // 函数属性
}

func (f *Function) Type() *TypeObj { return funcType }
func (f *Function) Repr() string   { return "<function " + f.Name + ">" }
func (f *Function) Str() string    { return f.Repr() }

// BuiltinFn 内置函数实现
type BuiltinFn func(args []Object, kwargs map[string]Object) (Object, error)

// Builtin 内置函数
type Builtin struct {
	Name string
	Fn   BuiltinFn
	Self Object // 绑定的 self（用于方法）
}

func (b *Builtin) Type() *TypeObj { return builtinType }
func (b *Builtin) Repr() string   { return "<built-in function " + b.Name + ">" }
func (b *Builtin) Str() string    { return b.Repr() }

// BoundMethod 绑定方法
type BoundMethod struct {
	Self Object
	Fn   Object
}

func (m *BoundMethod) Type() *TypeObj { return methodType }
func (m *BoundMethod) Repr() string   { return "<bound method " + m.Fn.Repr() + ">" }
func (m *BoundMethod) Str() string    { return m.Repr() }

// Module 模块
type Module struct {
	Name string
	Dict map[string]Object
	Path string
}

func (m *Module) Type() *TypeObj { return moduleType }
func (m *Module) Repr() string   { return "<module '" + m.Name + "'>" }
func (m *Module) Str() string    { return m.Repr() }

// Instance 用户类实例
type Instance struct {
	Class *TypeObj
	Dict  map[string]Object
}

func (i *Instance) Type() *TypeObj { return i.Class }
func (i *Instance) Repr() string {
	// 若类定义了 __repr__，由运行时处理；此处给默认表示
	for _, c := range i.Class.MRO() {
		if _, ok := c.Dict["__repr__"]; ok {
			return "<" + i.Class.Name + " object>"
		}
	}
	return "<" + i.Class.Name + " object>"
}
func (i *Instance) Str() string { return i.Repr() }

// Exception Python 异常实例
type Exception struct {
	Class *TypeObj
	Args  []Object
	Dict  map[string]Object
	Cause *Exception
}

func (e *Exception) Type() *TypeObj { return e.Class }
func (e *Exception) Repr() string {
	if len(e.Args) == 0 {
		return e.Class.Name + "()"
	}
	if len(e.Args) == 1 {
		return e.Class.Name + "(" + e.Args[0].Repr() + ")"
	}
	return e.Class.Name + "(" + joinRepr(e.Args) + ")"
}
func (e *Exception) Str() string {
	if len(e.Args) == 0 {
		return ""
	}
	if len(e.Args) == 1 {
		// CPython：KeyError 的单参数按 repr 呈现（str(KeyError("host")) == "'host'"）
		if e.Class != nil && e.Class.Name == "KeyError" {
			return e.Args[0].Repr()
		}
		return e.Args[0].Str()
	}
	return newTuple(e.Args).Str()
}

// NewException 创建异常实例
func NewException(cls *TypeObj, args ...Object) *Exception {
	return &Exception{Class: cls, Args: args, Dict: map[string]Object{}}
}

func newTuple(items []Object) *Tuple { return &Tuple{Items: items} }

// Generator 生成器
type Generator struct {
	Class *TypeObj
	// Next 推进生成器，返回 (值, 是否结束, 错误)
	Next   func() (Object, bool, error)
	Send   func(v Object) (Object, bool, error)
	Close  func()
	Name   string
	RetVal Object // 生成器 return 的值（协程结果）
}

func (g *Generator) Type() *TypeObj {
	if g.Class != nil {
		return g.Class
	}
	return genType
}
func (g *Generator) Repr() string { return "<generator object " + g.Name + ">" }
func (g *Generator) Str() string  { return g.Repr() }

// Iterator 通用迭代器
type Iterator struct {
	Class *TypeObj
	Items []Object
	Pos   int
	Gen   *Generator
}

func (it *Iterator) Type() *TypeObj {
	if it.Class != nil {
		return it.Class
	}
	return iterType
}
func (it *Iterator) Repr() string { return "<iterator object>" }
func (it *Iterator) Str() string  { return it.Repr() }

// ==================== 描述符 ====================

// Property 属性描述符
type Property struct {
	Fget Object
	Fset Object
	Fdel Object
	Doc  string
}

var PropertyType = &TypeObj{Name: "property", Builtin: BT_Property, Dict: map[string]Object{}}

func (p *Property) Type() *TypeObj { return PropertyType }
func (p *Property) Repr() string   { return "<property object>" }
func (p *Property) Str() string    { return p.Repr() }

// StaticMethod 静态方法包装
type StaticMethod struct{ Fn Object }

var StaticMethodType = &TypeObj{Name: "staticmethod", Builtin: BT_StaticMethod, Dict: map[string]Object{}}

func (s *StaticMethod) Type() *TypeObj { return StaticMethodType }
func (s *StaticMethod) Repr() string   { return "<staticmethod object>" }
func (s *StaticMethod) Str() string    { return s.Repr() }

// ClassMethod 类方法包装
type ClassMethod struct{ Fn Object }

var ClassMethodType = &TypeObj{Name: "classmethod", Builtin: BT_ClassMethod, Dict: map[string]Object{}}

func (c *ClassMethod) Type() *TypeObj { return ClassMethodType }
func (c *ClassMethod) Repr() string   { return "<classmethod object>" }
func (c *ClassMethod) Str() string    { return c.Repr() }

// Super super() 代理
type Super struct {
	Self  Object
	Class *TypeObj
}

var SuperType = &TypeObj{Name: "super", Builtin: BT_Super, Dict: map[string]Object{}}

func (s *Super) Type() *TypeObj { return SuperType }
func (s *Super) Repr() string   { return "<super object>" }
func (s *Super) Str() string    { return s.Repr() }

// ==================== 环境 ====================

// Env 词法作用域
type Env struct {
	Vars    map[string]Object
	Parent  *Env
	Globals *Env
	IsClass bool
	// 类体注解（供 dataclass / __annotations__ 使用）
	ClassAnnotations []string
	// global / nonlocal 声明
	GlobalNames   map[string]bool
	NonlocalNames map[string]bool
	// 当前类（方法内部用于 super() / __class__ 解析）
	CurrentClass *TypeObj
}

// NewEnv 创建环境
func NewEnv(parent *Env) *Env {
	e := &Env{Vars: map[string]Object{}, Parent: parent}
	if parent != nil {
		e.Globals = parent.Globals
		if parent.IsClass {
			e.CurrentClass = parent.CurrentClass
		} else {
			e.CurrentClass = parent.CurrentClass
		}
	}
	return e
}

// NewGlobalEnv 创建全局环境
func NewGlobalEnv() *Env {
	e := &Env{Vars: map[string]Object{}}
	e.Globals = e
	return e
}

// Get 查找变量
func (e *Env) Get(name string) (Object, bool) {
	for cur := e; cur != nil; cur = cur.Parent {
		if v, ok := cur.Vars[name]; ok {
			return v, true
		}
	}
	return nil, false
}

// GetLocal 仅查当前作用域
func (e *Env) GetLocal(name string) (Object, bool) {
	v, ok := e.Vars[name]
	return v, ok
}

// Set 在当前作用域赋值
func (e *Env) Set(name string, v Object) { e.Vars[name] = v }

// SetExisting 沿作用域链赋值（用于非 local 赋值语义）
func (e *Env) SetExisting(name string, v Object) bool {
	for cur := e; cur != nil; cur = cur.Parent {
		if _, ok := cur.Vars[name]; ok {
			cur.Vars[name] = v
			return true
		}
	}
	return false
}

// Delete 删除变量
func (e *Env) Delete(name string) bool {
	if _, ok := e.Vars[name]; ok {
		delete(e.Vars, name)
		return true
	}
	return false
}

var _ = math.Abs
var _ = ast.Pos{}

// ==================== 并发原语 ====================

// Task 协程任务（asyncio.create_task 的返回物）
type Task struct {
	Name   string
	Done   chan struct{}
	Result Object
	Err    error
}

// TaskType 任务类型对象
var TaskType = &TypeObj{Name: "Task", Bases: []*TypeObj{ObjectType}, Builtin: BT_Iterator, Dict: map[string]Object{}}

func (t *Task) Type() *TypeObj { return TaskType }
func (t *Task) Repr() string   { return "<Task " + t.Name + ">" }
func (t *Task) Str() string    { return t.Repr() }

// PyThread 线程对象
type PyThread struct {
	Name   string
	Done   chan struct{}
	Target func()
	Err    error
}

// ThreadType 线程类型对象
var ThreadType = &TypeObj{Name: "Thread", Bases: []*TypeObj{ObjectType}, Builtin: BT_Iterator, Dict: map[string]Object{}}

func (t *PyThread) Type() *TypeObj { return ThreadType }
func (t *PyThread) Repr() string   { return "<Thread " + t.Name + ">" }
func (t *PyThread) Str() string    { return t.Repr() }

// PyLock 互斥锁
type PyLock struct {
	Ch chan struct{}
}

// LockType 锁类型对象
var LockType = &TypeObj{Name: "Lock", Bases: []*TypeObj{ObjectType}, Builtin: BT_Iterator, Dict: map[string]Object{}}

func (l *PyLock) Type() *TypeObj { return LockType }
func (l *PyLock) Repr() string   { return "<Lock>" }
func (l *PyLock) Str() string    { return l.Repr() }

// NewPyLock 创建锁
func NewPyLock() *PyLock {
	l := &PyLock{Ch: make(chan struct{}, 1)}
	return l
}

// Acquire 加锁
func (l *PyLock) Acquire() { l.Ch <- struct{}{} }

// Release 解锁
func (l *PyLock) Release() {
	select {
	case <-l.Ch:
	default:
	}
}

// FieldSpec dataclasses.field 的返回物
type FieldSpec struct {
	Default Object
	Factory Object
}

// FieldSpecType 字段规格类型对象
var FieldSpecType = &TypeObj{Name: "Field", Bases: []*TypeObj{ObjectType}, Builtin: BT_Instance, Dict: map[string]Object{}}

func (f *FieldSpec) Type() *TypeObj { return FieldSpecType }
func (f *FieldSpec) Repr() string   { return "<dataclasses.field>" }
func (f *FieldSpec) Str() string    { return f.Repr() }
