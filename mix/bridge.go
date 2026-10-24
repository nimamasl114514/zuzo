package mix

import (
	"fmt"
	"io"

	"zuzo/evaluator"
	"zuzo/object"
	pyobj "zuzo/py/object"
	pyrt "zuzo/py/runtime"
)

// Bridge 混合语言双向桥：
//   - zuzo 侧注入 py调用 / py获取 / py设置 / py存在
//   - Python 侧注入 zuzo 模块（zuzo.调用 / 获取 / 设置 / 存在）
//   - 块边界做全局表双向同步，实现跨块、跨语言可见
type Bridge struct {
	IP   *pyrt.Interp
	Env  *object.Environment
	Out  io.Writer
	ErrW io.Writer

	injected map[string]bool // zuzo 侧桥注入的名字（同步时跳过）

	// 惰性同步版本表（见 sync.go）
	pyVer       map[string]int // Python 侧各名字的写入版本
	zzVer       map[string]int // zuzo 侧各名字的写入版本
	zzSeenPyVer map[string]int // zuzo 已拉取的 Python 版本
	pySeenZzVer map[string]int // Python 已拉取的 zuzo 版本
}

// NewBridge 创建桥并完成双向注入
func NewBridge(ip *pyrt.Interp, env *object.Environment, out, errw io.Writer) *Bridge {
	b := &Bridge{
		IP: ip, Env: env, Out: out, ErrW: errw,
		injected:    map[string]bool{},
		pyVer:       map[string]int{},
		zzVer:       map[string]int{},
		zzSeenPyVer: map[string]int{},
		pySeenZzVer: map[string]int{},
	}
	b.install()
	return b
}

// install 注入两侧入口
func (b *Bridge) install() {
	// ---------- zuzo 侧 ----------
	b.defineZuzo("py调用", func(args ...object.Object) object.Object {
		if len(args) < 1 {
			return errObj("E900", "py调用 需要至少 1 个参数（函数名）")
		}
		name, ok := args[0].(*object.String)
		if !ok {
			return errObj("E900", "py调用 的第一个参数必须是字符串（函数名）")
		}
		fn, ok := b.LookupPy(name.Value)
		if !ok {
			return errObj("E901", "Python 侧未定义："+name.Value)
		}
		pyargs := make([]pyobj.Object, 0, len(args)-1)
		for _, a := range args[1:] {
			pyargs = append(pyargs, b.ToPy(a))
		}
		res, err := b.CallPy(fn, pyargs)
		if err != nil {
			return errObj("E902", err.Error())
		}
		return b.ToZuzo(res)
	})

	b.defineZuzo("py获取", func(args ...object.Object) object.Object {
		if len(args) < 1 {
			return errObj("E900", "py获取 需要 1 个参数（变量名）")
		}
		name, ok := args[0].(*object.String)
		if !ok {
			return errObj("E900", "py获取 的参数必须是字符串（变量名）")
		}
		v, ok := b.LookupPy(name.Value)
		if !ok {
			return errObj("E901", "Python 侧未定义："+name.Value)
		}
		return b.ToZuzo(v)
	})

	b.defineZuzo("py设置", func(args ...object.Object) object.Object {
		if len(args) < 2 {
			return errObj("E900", "py设置 需要 2 个参数（变量名, 值）")
		}
		name, ok := args[0].(*object.String)
		if !ok {
			return errObj("E900", "py设置 的第一个参数必须是字符串（变量名）")
		}
		b.IP.Globals.Set(name.Value, b.ToPy(args[1]))
		return evaluator.NULL
	})

	b.defineZuzo("py存在", func(args ...object.Object) object.Object {
		if len(args) < 1 {
			return errObj("E900", "py存在 需要 1 个参数（变量名）")
		}
		name, ok := args[0].(*object.String)
		if !ok {
			return errObj("E900", "py存在 的参数必须是字符串（变量名）")
		}
		_, ok = b.LookupPy(name.Value)
		return &object.Boolean{Value: ok}
	})

	// ---------- Python 侧 ----------
	mod := &pyobj.Module{
		Name: "zuzo",
		Dict: map[string]pyobj.Object{
			"调用": &pyobj.Builtin{Name: "zuzo.调用", Fn: func(args []pyobj.Object, kwargs map[string]pyobj.Object) (pyobj.Object, error) {
				if len(args) < 1 {
					return nil, fmt.Errorf("zuzo.调用 需要至少 1 个参数（函数名）")
				}
				name, ok := args[0].(*pyobj.Str)
				if !ok {
					return nil, fmt.Errorf("zuzo.调用 的第一个参数必须是字符串（函数名）")
				}
				fn, ok := b.LookupZuzo(name.V)
				if !ok {
					return nil, fmt.Errorf("zuzo 侧未定义：%s", name.V)
				}
				zargs := make([]object.Object, 0, len(args)-1)
				for _, a := range args[1:] {
					zargs = append(zargs, b.ToZuzo(a))
				}
				res := evaluator.CallValue(fn, zargs)
				if errObjVal, isErr := res.(*object.Error); isErr {
					return nil, fmt.Errorf("%s", errObjVal.Message)
				}
				return b.ToPy(res), nil
			}},
			"获取": &pyobj.Builtin{Name: "zuzo.获取", Fn: func(args []pyobj.Object, kwargs map[string]pyobj.Object) (pyobj.Object, error) {
				if len(args) < 1 {
					return nil, fmt.Errorf("zuzo.获取 需要 1 个参数（变量名）")
				}
				name, ok := args[0].(*pyobj.Str)
				if !ok {
					return nil, fmt.Errorf("zuzo.获取 的参数必须是字符串（变量名）")
				}
				v, ok := b.LookupZuzo(name.V)
				if !ok {
					return nil, fmt.Errorf("zuzo 侧未定义：%s", name.V)
				}
				return b.ToPy(v), nil
			}},
			"设置": &pyobj.Builtin{Name: "zuzo.设置", Fn: func(args []pyobj.Object, kwargs map[string]pyobj.Object) (pyobj.Object, error) {
				if len(args) < 2 {
					return nil, fmt.Errorf("zuzo.设置 需要 2 个参数（变量名, 值）")
				}
				name, ok := args[0].(*pyobj.Str)
				if !ok {
					return nil, fmt.Errorf("zuzo.设置 的第一个参数必须是字符串（变量名）")
				}
				b.Env.Set(name.V, b.ToZuzo(args[1]))
				return pyobj.None, nil
			}},
			"存在": &pyobj.Builtin{Name: "zuzo.存在", Fn: func(args []pyobj.Object, kwargs map[string]pyobj.Object) (pyobj.Object, error) {
				if len(args) < 1 {
					return nil, fmt.Errorf("zuzo.存在 需要 1 个参数（变量名）")
				}
				name, ok := args[0].(*pyobj.Str)
				if !ok {
					return nil, fmt.Errorf("zuzo.存在 的参数必须是字符串（变量名）")
				}
				_, ok = b.LookupZuzo(name.V)
				if ok {
					return pyobj.TrueObj, nil
				}
				return pyobj.FalseObj, nil
			}},
		},
	}
	b.IP.Globals.Set("zuzo", mod)
}

// defineZuzo 在 zuzo 全局环境登记桥内置函数
func (b *Bridge) defineZuzo(name string, fn object.BuiltinFunction) {
	b.Env.Set(name, &object.Builtin{Name: name, Fn: fn})
	b.injected[name] = true
}

// errObj 构造 zuzo 错误对象
func errObj(code, msg string) *object.Error {
	return &object.Error{Code: code, Message: msg}
}
