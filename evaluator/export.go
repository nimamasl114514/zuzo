package evaluator

import (
	"zuzo/object"
)

// CallValue 从包外调用 zuzo 可调用对象（用户函数 / 内置函数）。
// 混合语言桥（mix 包）用它把 zuzo 函数暴露给 Python 侧调用。
func CallValue(fn object.Object, args []object.Object) object.Object {
	switch f := fn.(type) {
	case *object.Function:
		return applyFunction(f, args)
	case *object.Builtin:
		return enrichError(f.Fn(args...))
	default:
		return &object.Error{
			Code:    "E202",
			Message: "不可调用的对象: " + string(fn.Type()),
		}
	}
}

// EnvSnapshot 返回环境当前作用域内的变量快照（不含外层作用域）。
// 混合语言桥用它做块边界的全局表同步。
func EnvSnapshot(env *object.Environment) map[string]object.Object {
	return env.GetAll()
}
