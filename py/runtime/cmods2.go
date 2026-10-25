package runtime

// cmods2.go — 第二批「C 模块 / 基础模块」的 Go 原生替代（第13轮 · 支持第三方 py 库）
//
// 已实现：builtins / errno / atexit / io(_io 的 BytesIO·StringIO)
// 这些是 CPython 纯 Python 标准库与三方库的地基：
//   enum/codecs/operator  → import builtins
//   urllib3               → io.BytesIO、errno.EAGAIN
//   socket/ssl 相关        → errno 常量
//   certifi               → atexit

import (
	"os"
	"strings"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["builtins"] = modBuiltins
	builtinModuleInits["errno"] = modErrno
	builtinModuleInits["atexit"] = modAtexit
	builtinModuleInits["io"] = modIO
	builtinModuleInits["_io"] = modIO
}

// ==================== builtins ====================

func modBuiltins(ip *Interp) *object.Module {
	m := newModule("builtins")
	for k, v := range ip.Builtins.Vars {
		m.Dict[k] = v
	}
	m.Dict["__name__"] = &object.Str{V: "builtins"}
	return m
}

// ==================== errno ====================

func modErrno(ip *Interp) *object.Module {
	m := newModule("errno")
	// Windows(WSA) 与 POSIX 数值不同；本项目主要 Windows 运行，按 GOOS 选择。
	codes := map[string]int64{
		"EPERM": 1, "ENOENT": 2, "ESRCH": 3, "EINTR": 4, "EIO": 5, "ENXIO": 6,
		"E2BIG": 7, "ENOEXEC": 8, "EBADF": 9, "ECHILD": 10, "EAGAIN": 11,
		"EWOULDBLOCK": 11, "ENOMEM": 12, "EACCES": 13, "EFAULT": 14, "EBUSY": 16,
		"EEXIST": 17, "EXDEV": 18, "ENODEV": 19, "ENOTDIR": 20, "EISDIR": 21,
		"EINVAL": 22, "ENFILE": 23, "EMFILE": 24, "ENOTTY": 25, "EFBIG": 27,
		"ENOSPC": 28, "ESPIPE": 29, "EROFS": 30, "EMLINK": 31, "EPIPE": 32,
		"EDOM": 33, "ERANGE": 34, "EDEADLK": 36, "ENAMETOOLONG": 38,
		"ENOLCK": 39, "ENOSYS": 40, "ENOTEMPTY": 41, "EILSEQ": 42,
		"EINPROGRESS": 115, "ECONNRESET": 104, "ECONNREFUSED": 111,
		"ETIMEDOUT": 110, "EHOSTUNREACH": 113, "ENETUNREACH": 101,
		"EADDRINUSE": 98, "EADDRNOTAVAIL": 99, "EAFNOSUPPORT": 97,
		"ENOTSOCK": 88, "ECONNABORTED": 103, "ENOTCONN": 107, "EISCONN": 106,
		"EMSGSIZE": 90, "EPROTONOSUPPORT": 93, "EOPNOTSUPP": 95,
		"ESHUTDOWN": 108, "ECANCELED": 125,
	}
	if isWindows() {
		codes["EAGAIN"] = 11
		codes["EWOULDBLOCK"] = 11
		codes["EINPROGRESS"] = 112
		codes["ECONNRESET"] = 10054
		codes["ECONNREFUSED"] = 10061
		codes["ETIMEDOUT"] = 10060
		codes["EHOSTUNREACH"] = 10065
		codes["ENETUNREACH"] = 10051
		codes["EADDRINUSE"] = 10048
		codes["EADDRNOTAVAIL"] = 10049
		codes["EAFNOSUPPORT"] = 10047
		codes["ENOTSOCK"] = 10038
		codes["ECONNABORTED"] = 10053
		codes["ENOTCONN"] = 10057
		codes["EISCONN"] = 10056
		codes["EMSGSIZE"] = 10040
		codes["EPROTONOSUPPORT"] = 10043
		codes["EOPNOTSUPP"] = 10045
		codes["ESHUTDOWN"] = 10058
		codes["ECANCELED"] = 105
	}
	errorcode := &object.Dict{}
	for k, v := range codes {
		m.Dict[k] = &object.Int{V: v}
		if _, exists := errorcode.Get(&object.Int{V: v}); !exists {
			errorcode.Set(&object.Int{V: v}, &object.Str{V: lower(k)})
		}
	}
	m.Dict["errorcode"] = errorcode
	return m
}

func lower(s string) string { return strings.ToLower(s) }

func isWindows() bool {
	return os.PathSeparator == '\\'
}

// ==================== atexit ====================

func modAtexit(ip *Interp) *object.Module {
	m := newModule("atexit")
	m.Dict["register"] = bi("register", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("register() requires a callable")
		}
		ip.ExitFuncs = append(ip.ExitFuncs, a[0])
		return a[0], nil
	})
	m.Dict["unregister"] = bi("unregister", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return object.None, nil
		}
		out := ip.ExitFuncs[:0]
		for _, f := range ip.ExitFuncs {
			if f != a[0] {
				out = append(out, f)
			}
		}
		ip.ExitFuncs = out
		return object.None, nil
	})
	m.Dict["_run_exitfuncs"] = bi("_run_exitfuncs", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		ip.runExitFuncs()
		return object.None, nil
	})
	m.Dict["_clear"] = bi("_clear", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		ip.ExitFuncs = nil
		return object.None, nil
	})
	return m
}

// runExitFuncs 逆序执行 atexit 注册的回调（CPython 语义）。
func (ip *Interp) runExitFuncs() {
	for i := len(ip.ExitFuncs) - 1; i >= 0; i-- {
		f := ip.ExitFuncs[i]
		ip.protect(func() { _ = ip.Call(f, nil, nil) })
	}
	ip.ExitFuncs = nil
}

// ==================== io / _io ====================

func modIO(ip *Interp) *object.Module {
	m := newModule("io")
	m.Dict["DEFAULT_BUFFER_SIZE"] = &object.Int{V: 8192}
	m.Dict["SEEK_SET"] = &object.Int{V: 0}
	m.Dict["SEEK_CUR"] = &object.Int{V: 1}
	m.Dict["SEEK_END"] = &object.Int{V: 2}
	m.Dict["UnsupportedOperation"] = ip.excType("OSError")
	m.Dict["BlockingIOError"] = ip.excType("BlockingIOError")

	ioBase := &object.TypeObj{Name: "IOBase", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	ioBase.Dict["close"] = bi("close", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			if inst, ok := a[0].(*object.Instance); ok {
				inst.Dict["_closed"] = &object.Bool{V: true}
			}
		}
		return object.None, nil
	})
	m.Dict["IOBase"] = ioBase
	m.Dict["RawIOBase"] = ioBase
	m.Dict["BufferedIOBase"] = ioBase
	m.Dict["TextIOBase"] = ioBase

	m.Dict["BytesIO"] = newBytesIOType(ip, "BytesIO")
	m.Dict["StringIO"] = newStringIOType(ip, "StringIO")

	return m
}

// bufState 取实例内部的缓冲区状态。
func bufState(inst *object.Instance) ([]byte, int) {
	var buf []byte
	if b, ok := inst.Dict["_buf"].(*object.Bytes); ok {
		buf = b.V
	}
	pos := 0
	if p, ok := inst.Dict["_pos"].(*object.Int); ok {
		pos = int(p.V)
	}
	return buf, pos
}

func setBuf(inst *object.Instance, buf []byte, pos int) {
	inst.Dict["_buf"] = &object.Bytes{V: buf}
	inst.Dict["_pos"] = &object.Int{V: int64(pos)}
}

func newBytesIOType(ip *Interp, name string) *object.TypeObj {
	cls := &object.TypeObj{Name: name, Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	cls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		var buf []byte
		if len(a) > 1 {
			switch v := a[1].(type) {
			case *object.Bytes:
				buf = append([]byte{}, v.V...)
			case *object.Str:
				buf = []byte(v.V)
			}
		}
		setBuf(inst, buf, 0)
		inst.Dict["_closed"] = &object.Bool{V: false}
		return object.None, nil
	})
	cls.Dict["write"] = bi("write", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, pos := bufState(inst)
		var data []byte
		switch v := a[1].(type) {
		case *object.Bytes:
			data = v.V
		case *object.Str:
			data = []byte(v.V)
		}
		if pos > len(buf) {
			buf = append(buf, make([]byte, pos-len(buf))...)
		}
		if pos+len(data) <= len(buf) {
			copy(buf[pos:], data)
		} else {
			buf = append(buf[:pos], data...)
		}
		setBuf(inst, buf, pos+len(data))
		return &object.Int{V: int64(len(data))}, nil
	})
	cls.Dict["read"] = bi("read", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, pos := bufState(inst)
		n := len(buf) - pos
		if len(a) > 1 {
			if v, ok := object.AsInt(a[1]); ok && v >= 0 && int(v) < n {
				n = int(v)
			}
		}
		if n <= 0 || pos >= len(buf) {
			return &object.Bytes{V: []byte{}}, nil
		}
		out := append([]byte{}, buf[pos:pos+n]...)
		setBuf(inst, buf, pos+n)
		return &object.Bytes{V: out}, nil
	})
	cls.Dict["read1"] = cls.Dict["read"]
	cls.Dict["readline"] = bi("readline", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, pos := bufState(inst)
		if pos >= len(buf) {
			return &object.Bytes{V: []byte{}}, nil
		}
		idx := -1
		for i := pos; i < len(buf); i++ {
			if buf[i] == '\n' {
				idx = i
				break
			}
		}
		end := len(buf)
		if idx >= 0 {
			end = idx + 1
		}
		out := append([]byte{}, buf[pos:end]...)
		setBuf(inst, buf, end)
		return &object.Bytes{V: out}, nil
	})
	cls.Dict["getvalue"] = bi("getvalue", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, _ := bufState(inst)
		return &object.Bytes{V: append([]byte{}, buf...)}, nil
	})
	cls.Dict["seek"] = bi("seek", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, pos := bufState(inst)
		off, _ := object.AsInt(a[1])
		whence := int64(0)
		if len(a) > 2 {
			whence, _ = object.AsInt(a[2])
		}
		var np int64
		switch whence {
		case 0:
			np = off
		case 1:
			np = int64(pos) + off
		case 2:
			np = int64(len(buf)) + off
		}
		if np < 0 {
			ip.ValueError("negative seek value %d", np)
		}
		setBuf(inst, buf, int(np))
		return &object.Int{V: np}, nil
	})
	cls.Dict["tell"] = bi("tell", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		_, pos := bufState(inst)
		return &object.Int{V: int64(pos)}, nil
	})
	cls.Dict["truncate"] = bi("truncate", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, pos := bufState(inst)
		size := pos
		if len(a) > 1 {
			if v, ok := object.AsInt(a[1]); ok {
				size = int(v)
			}
		}
		if size < len(buf) {
			buf = buf[:size]
		}
		setBuf(inst, buf, pos)
		return &object.Int{V: int64(size)}, nil
	})
	cls.Dict["flush"] = bi("flush", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	cls.Dict["close"] = ioBaseClose
	cls.Dict["__len__"] = bi("__len__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, _ := bufState(inst)
		return &object.Int{V: int64(len(buf))}, nil
	})
	cls.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return a[0], nil
	})
	cls.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	return cls
}

func newStringIOType(ip *Interp, name string) *object.TypeObj {
	cls := &object.TypeObj{Name: name, Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	cls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		var buf []byte
		if len(a) > 1 {
			buf = []byte(a[1].Str())
		}
		setBuf(inst, buf, 0)
		inst.Dict["_closed"] = &object.Bool{V: false}
		return object.None, nil
	})
	cls.Dict["write"] = bi("write", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, pos := bufState(inst)
		data := []byte(a[1].Str())
		if pos > len(buf) {
			buf = append(buf, make([]byte, pos-len(buf))...)
		}
		if pos+len(data) <= len(buf) {
			copy(buf[pos:], data)
		} else {
			buf = append(buf[:pos], data...)
		}
		setBuf(inst, buf, pos+len(data))
		return &object.Int{V: int64(len(data))}, nil
	})
	cls.Dict["read"] = bi("read", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, pos := bufState(inst)
		n := len(buf) - pos
		if len(a) > 1 {
			if v, ok := object.AsInt(a[1]); ok && v >= 0 && int(v) < n {
				n = int(v)
			}
		}
		if n <= 0 || pos >= len(buf) {
			return &object.Str{V: ""}, nil
		}
		out := string(buf[pos : pos+n])
		setBuf(inst, buf, pos+n)
		return &object.Str{V: out}, nil
	})
	cls.Dict["getvalue"] = bi("getvalue", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, _ := bufState(inst)
		return &object.Str{V: string(buf)}, nil
	})
	cls.Dict["seek"] = cls.Dict["seek"]
	cls.Dict["tell"] = bi("tell", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		_, pos := bufState(inst)
		return &object.Int{V: int64(pos)}, nil
	})
	cls.Dict["seek"] = bi("seek", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		buf, pos := bufState(inst)
		off, _ := object.AsInt(a[1])
		whence := int64(0)
		if len(a) > 2 {
			whence, _ = object.AsInt(a[2])
		}
		var np int64
		switch whence {
		case 0:
			np = off
		case 1:
			np = int64(pos) + off
		case 2:
			np = int64(len(buf)) + off
		}
		if np < 0 {
			ip.ValueError("negative seek value %d", np)
		}
		setBuf(inst, buf, int(np))
		return &object.Int{V: np}, nil
	})
	cls.Dict["flush"] = bi("flush", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	cls.Dict["close"] = ioBaseClose
	cls.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return a[0], nil
	})
	cls.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	return cls
}

var ioBaseClose = bi("close", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
	if len(a) > 0 {
		if inst, ok := a[0].(*object.Instance); ok {
			inst.Dict["_closed"] = &object.Bool{V: true}
		}
	}
	return object.None, nil
})
