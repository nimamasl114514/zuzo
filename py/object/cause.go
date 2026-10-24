package object

// cause.go — Python 异常链（exception chaining）对象层支持
//
// 覆盖 CPython 的三个链属性：
//   __cause__           raise X from Y 的显式原因（对应 Exception.Cause 字段）
//   __context__         except 块内隐式接续的上一异常
//   __suppress_context__  raise X from None 时抑制隐式上下文
//
// 设计说明：
//   Exception 结构体定义在 function.go，本任务不允许改动该文件，因此
//   __context__ / __suppress_context__ 的存储落在 Exception.Dict 上
//   （运行时 SetAttr 对 *Exception 本来就是 obj.Dict[name] = v，语义一致）。
//   __cause__ 直接读写 Exception.Cause 字段，与 runtime.execRaise 的赋值路径对齐。
//
// 属性暴露方式：运行时的 GetAttr 对 *Exception 先查 Dict、再沿 MRO 走
// findDunder + bindClassAttr；而所有异常类（运行时 initExceptions 动态创建）
// 的 MRO 都以 object.ObjectType 结尾，所以把三个 property 描述符挂到
// ObjectType.Dict 上即可让全体异常实例可读，无需改 py/runtime/*.go。

const (
	// KeyCause 显式原因属性名
	KeyCause = "__cause__"
	// KeyContext 隐式上下文属性名
	KeyContext = "__context__"
	// KeySuppressContext 上下文抑制标志属性名
	KeySuppressContext = "__suppress_context__"
)

// Cause 返回显式原因，未设置时返回 None
func (e *Exception) CauseAttr() Object {
	if e == nil || e.Cause == nil {
		return None
	}
	return e.Cause
}

// SetCause 设置显式原因（raise X from Y）
func (e *Exception) SetCause(c *Exception) {
	if e == nil {
		return
	}
	e.Cause = c
}

// Context 返回隐式上下文，未设置时返回 None
func (e *Exception) Context() Object {
	if e == nil || e.Dict == nil {
		return None
	}
	if v, ok := e.Dict[KeyContext]; ok && v != nil {
		return v
	}
	return None
}

// SetContext 记录隐式上下文（except 块内抛出新的异常时由运行时调用）
func (e *Exception) SetContext(ctx *Exception) {
	if e == nil {
		return
	}
	if e.Dict == nil {
		e.Dict = map[string]Object{}
	}
	if ctx == nil {
		delete(e.Dict, KeyContext)
		return
	}
	e.Dict[KeyContext] = ctx
}

// SuppressContext 返回 __suppress_context__ 标志
//
// CPython 语义：raise X from Y 在设置 __cause__ 的同时把
// __suppress_context__ 置为 True（因此 traceback 只打 "direct cause"）；
// raise X from None 则 __cause__ 为 None、__suppress_context__ 为 True。
// 前者可由 Cause 字段推导，后者需运行时分发（execRaise）写入。
func (e *Exception) SuppressContext() bool {
	if e == nil {
		return false
	}
	if e.Dict != nil {
		if v, ok := e.Dict[KeySuppressContext]; ok && v != nil {
			return Truthy(v)
		}
	}
	return e.Cause != nil
}

// SetSuppressContext 设置 __suppress_context__（raise X from None）
func (e *Exception) SetSuppressContext(v bool) {
	if e == nil {
		return
	}
	if e.Dict == nil {
		e.Dict = map[string]Object{}
	}
	e.Dict[KeySuppressContext] = GetBool(v)
}

// Chain 返回从当前异常沿 __context__ 向下的链（不含自身）
func (e *Exception) Chain() []*Exception {
	var out []*Exception
	seen := map[*Exception]bool{e: true}
	cur := e
	for {
		next, ok := cur.Context().(*Exception)
		if !ok || next == nil || seen[next] {
			return out
		}
		seen[next] = true
		out = append(out, next)
		cur = next
	}
}

// ChainString 生成 CPython 风格的链提示行；无链时返回空串
func (e *Exception) ChainString() string {
	if e == nil {
		return ""
	}
	if e.Cause != nil {
		return "The above exception was the direct cause of the following exception:"
	}
	if e.Cause == nil && !e.SuppressContext() && e.Context() != None {
		return "During handling of the above exception, another exception occurred:"
	}
	return ""
}

// exceptionProp 构造只对 *Exception 实例可用的只读 property 描述符
func exceptionProp(name string, get func(*Exception) Object) *Property {
	p := &Property{Doc: "exception chain attribute " + name}
	p.Fget = &Builtin{Name: name, Fn: func(args []Object, kwargs map[string]Object) (Object, error) {
		if len(args) == 0 {
			return None, nil
		}
		if e, ok := args[0].(*Exception); ok {
			return get(e), nil
		}
		return None, nil
	}}
	return p
}

func installChainAttrs() {
	if ObjectType.Dict == nil {
		ObjectType.Dict = map[string]Object{}
	}
	if _, exists := ObjectType.Dict[KeyCause]; !exists {
		ObjectType.Dict[KeyCause] = exceptionProp(KeyCause, func(e *Exception) Object {
			return e.CauseAttr()
		})
	}
	if _, exists := ObjectType.Dict[KeyContext]; !exists {
		ObjectType.Dict[KeyContext] = exceptionProp(KeyContext, func(e *Exception) Object {
			return e.Context()
		})
	}
	if _, exists := ObjectType.Dict[KeySuppressContext]; !exists {
		ObjectType.Dict[KeySuppressContext] = exceptionProp(KeySuppressContext, func(e *Exception) Object {
			return GetBool(e.SuppressContext())
		})
	}
}

func init() {
	installChainAttrs()
}
