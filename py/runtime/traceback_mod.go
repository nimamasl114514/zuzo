package runtime

import (
	"zuzo/py/object"
)

// modTraceback 提供 traceback 的最小可用面（内置实现，挡住 CPython traceback.py）。
//
// 为什么挡：CPython 3.12 的 traceback.py:745 用了 PEP 701 特性——f-string 内出现同引号嵌套
// （f'... {_safe_string(e, '__notes__', repr)}'），zuzo 的 Python 前端尚不支持，
// 而 logging/__init__.py 模块级硬 import traceback，导致 logging 直载链断在语法错误上。
// 本模块按 CPython 口径提供 logging / urllib3 / requests 实际用到的接口。
func modTraceback(ip *Interp) *object.Module {
	m := newModule("traceback")

	// excLine 按 CPython 的 format_exception_only 口径渲染「ExcName: message」。
	excLine := func(o object.Object) string {
		if e, ok := o.(*object.Exception); ok {
			if s := e.Str(); s != "" {
				return e.Class.Name + ": " + s
			}
			return e.Class.Name
		}
		if o == nil || o == object.None {
			return "NoneType: None"
		}
		return ip.StrOf(o)
	}

	// writeTo 把文本写进 file 参数（缺省 stderr）；file 支持 .write(str)。
	writeTo := func(file object.Object, text string) {
		if file == nil || file == object.None {
			ip.Stderr.Write([]byte(text))
			return
		}
		if inst, ok := file.(*object.Instance); ok {
			if w, ok2 := ip.findDunder(inst.Class, "write"); ok2 {
				ip.Call(ip.bindMethod(inst, w), []object.Object{&object.Str{V: text}}, nil)
				return
			}
		}
		ip.Stderr.Write([]byte(text))
	}

	// argFile 从位置参数 / 关键字参数里取 file（CPython 里是第 5 个位置参数）。
	argFile := func(a []object.Object, k map[string]object.Object, pos int) object.Object {
		if k != nil {
			if v, ok := k["file"]; ok {
				return v
			}
		}
		if len(a) > pos {
			return a[pos]
		}
		return object.None
	}

	m.Dict["format_exception_only"] = bi("format_exception_only", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var value object.Object = object.None
		if len(a) >= 2 {
			value = a[1]
		} else if len(a) == 1 {
			value = a[0]
		}
		return &object.List{Items: []object.Object{&object.Str{V: excLine(value) + "\n"}}}, nil
	})
	m.Dict["format_exception"] = bi("format_exception", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var value object.Object = object.None
		if len(a) >= 2 {
			value = a[1]
		}
		return &object.List{Items: []object.Object{&object.Str{V: excLine(value) + "\n"}}}, nil
	})
	m.Dict["format_exc"] = bi("format_exc", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		// zuzo 的异常用 panic(*PyError) 传播，Interp 上没有「当前异常」字段；
		// logging 的调用路径是 record.exc_info → print_exception(ei[0], ei[1], tb, None, sio) 传参，
		// 不依赖 format_exc。无参调用按 CPython 的「无活动异常」口径返回。
		return &object.Str{V: "NoneType: None\n"}, nil
	})
	m.Dict["format_tb"] = bi("format_tb", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.List{}, nil
	})
	m.Dict["format_list"] = bi("format_list", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Str{V: ""}, nil
	})
	m.Dict["print_exception"] = bi("print_exception", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var value object.Object = object.None
		if len(a) >= 2 {
			value = a[1]
		}
		writeTo(argFile(a, k, 4), excLine(value)+"\n")
		return object.None, nil
	})
	m.Dict["print_exc"] = bi("print_exc", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		writeTo(argFile(a, k, 0), "NoneType: None\n")
		return object.None, nil
	})
	m.Dict["print_tb"] = bi("print_tb", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	m.Dict["print_stack"] = bi("print_stack", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	m.Dict["extract_tb"] = bi("extract_tb", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.List{}, nil
	})
	m.Dict["walk_tb"] = bi("walk_tb", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.List{}, nil
	})
	m.Dict["clear_frames"] = bi("clear_frames", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	m.Dict["format_stack"] = bi("format_stack", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.List{}, nil
	})
	m.Dict["print_last"] = bi("print_last", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		writeTo(argFile(a, k, 1), "NoneType: None\n")
		return object.None, nil
	})

	// TracebackException：logging 的 Formatter.formatException 在 3.13 起走它；
	// 3.12 仍走 print_exception，这里给出兼容壳以防第三方库直接构造。
	tbCls := &object.TypeObj{Name: "TracebackException", Dict: map[string]object.Object{}}
	tbCls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			if inst, ok := a[0].(*object.Instance); ok {
				var exc object.Object = object.None
				if len(a) > 1 {
					exc = a[1]
				}
				inst.Dict["_exc"] = exc
				inst.Dict["stack"] = &object.List{}
				inst.Dict["exc_type"] = exc
			}
		}
		return object.None, nil
	})
	tbCls.Dict["from_exception"] = bi("from_exception", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		// 既支持 TracebackException.from_exception(exc)（类方法），也支持实例调用
		var exc object.Object = object.None
		for _, x := range a {
			if _, isType := x.(*object.TypeObj); isType {
				continue
			}
			if _, isInst := x.(*object.Instance); isInst {
				continue
			}
			exc = x
			break
		}
		inst := &object.Instance{Class: tbCls, Dict: map[string]object.Object{}}
		inst.Dict["_exc"] = exc
		inst.Dict["stack"] = &object.List{}
		inst.Dict["exc_type"] = exc
		return inst, nil
	})
	tbCls.Dict["format"] = bi("format", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var exc object.Object = object.None
		if len(a) > 0 {
			if inst, ok := a[0].(*object.Instance); ok {
				exc = inst.Dict["_exc"]
			}
		}
		return &object.List{Items: []object.Object{&object.Str{V: excLine(exc) + "\n"}}}, nil
	})
	tbCls.Dict["format_exception_only"] = tbCls.Dict["format"]
	m.Dict["TracebackException"] = tbCls
	m.Dict["StackSummary"] = &object.TypeObj{Name: "StackSummary", Dict: map[string]object.Object{}}
	m.Dict["FrameSummary"] = &object.TypeObj{Name: "FrameSummary", Dict: map[string]object.Object{}}
	return m
}
