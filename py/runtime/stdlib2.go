package runtime

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"zuzo/py/object"
)

// registerStdlib2 注册 pathlib / logging / argparse / unittest
func registerStdlib2(ip *Interp) {
	builtinModuleInits["pathlib"] = modPathlib
	builtinModuleInits["logging"] = modLogging
	builtinModuleInits["argparse"] = modArgparse
	builtinModuleInits["unittest"] = modUnittest
	builtinModuleInits["shutil"] = modShutil
	registerStdlib3(ip)
}

// ==================== shutil ====================

func modShutil(ip *Interp) *object.Module {
	m := newModule("shutil")
	m.Dict["rmtree"] = bi("rmtree", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := a[0].Str()
		if err := os.RemoveAll(p); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return object.None, nil
	})
	m.Dict["copy"] = bi("copy", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		data, err := os.ReadFile(a[0].Str())
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		if err := os.WriteFile(a[1].Str(), data, 0o644); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return &object.Str{V: a[1].Str()}, nil
	})
	m.Dict["copyfile"] = m.Dict["copy"]
	m.Dict["move"] = bi("move", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if err := os.Rename(a[0].Str(), a[1].Str()); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return &object.Str{V: a[1].Str()}, nil
	})
	return m
}

// ==================== pathlib ====================

var pathClass *object.TypeObj

func pathOf(o object.Object) string {
	if inst, ok := o.(*object.Instance); ok {
		if v, ok2 := inst.Dict["_p"].(*object.Str); ok2 {
			return v.V
		}
	}
	return ""
}

func modPathlib(ip *Interp) *object.Module {
	m := newModule("pathlib")
	if pathClass == nil {
		pathClass = &object.TypeObj{Name: "Path", Dict: map[string]object.Object{}}
		pathClass.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) < 1 {
				return object.None, nil
			}
			self, ok := a[0].(*object.Instance)
			if !ok {
				return object.None, nil
			}
			parts := ""
			if len(a) > 1 {
				var segs []string
				for _, x := range a[1:] {
					segs = append(segs, x.Str())
				}
				parts = filepath.Join(segs...)
			}
			self.Dict["_p"] = &object.Str{V: parts}
			return object.None, nil
		})
		pathClass.Dict["__str__"] = bi("__str__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Str{V: pathOf(a[0])}, nil
		})
		pathClass.Dict["__fspath__"] = pathClass.Dict["__str__"]
		pathClass.Dict["__truediv__"] = bi("__truediv__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			p := filepath.Join(pathOf(a[0]), a[1].Str())
			inst := &object.Instance{Class: pathClass, Dict: map[string]object.Object{}}
			inst.Dict["_p"] = &object.Str{V: p}
			return inst, nil
		})
		mkProp := func(name string, f func(string) object.Object) {
			pathClass.Dict[name] = &object.Property{Fget: bi(name, func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				return f(pathOf(a[0])), nil
			})}
		}
		mkProp("name", func(p string) object.Object { return &object.Str{V: filepath.Base(p)} })
		mkProp("stem", func(p string) object.Object {
			b := filepath.Base(p)
			return &object.Str{V: strings.TrimSuffix(b, filepath.Ext(b))}
		})
		mkProp("suffix", func(p string) object.Object { return &object.Str{V: filepath.Ext(p)} })
		mkProp("parent", func(p string) object.Object {
			inst := &object.Instance{Class: pathClass, Dict: map[string]object.Object{}}
			inst.Dict["_p"] = &object.Str{V: filepath.Dir(p)}
			return inst
		})
		mkProp("parts", func(p string) object.Object {
			var items []object.Object
			for _, seg := range strings.FieldsFunc(p, func(r rune) bool { return r == '/' || r == '\\' }) {
				items = append(items, &object.Str{V: seg})
			}
			return &object.Tuple{Items: items}
		})
		pathClass.Dict["exists"] = bi("exists", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			_, err := os.Stat(pathOf(a[0]))
			return object.GetBool(err == nil), nil
		})
		pathClass.Dict["is_file"] = bi("is_file", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			st, err := os.Stat(pathOf(a[0]))
			return object.GetBool(err == nil && !st.IsDir()), nil
		})
		pathClass.Dict["is_dir"] = bi("is_dir", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			st, err := os.Stat(pathOf(a[0]))
			return object.GetBool(err == nil && st.IsDir()), nil
		})
		pathClass.Dict["read_text"] = bi("read_text", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			data, err := os.ReadFile(pathOf(a[0]))
			if err != nil {
				ip.raise(ip.excType("FileNotFoundError"), "%v", err)
			}
			return &object.Str{V: string(data)}, nil
		})
		pathClass.Dict["write_text"] = bi("write_text", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if err := os.WriteFile(pathOf(a[0]), []byte(a[1].Str()), 0o644); err != nil {
				ip.raise(ip.excType("OSError"), "%v", err)
			}
			return &object.Int{V: int64(len(a[1].Str()))}, nil
		})
		pathClass.Dict["mkdir"] = bi("mkdir", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if err := os.MkdirAll(pathOf(a[0]), 0o755); err != nil {
				ip.raise(ip.excType("OSError"), "%v", err)
			}
			return object.None, nil
		})
		pathClass.Dict["iterdir"] = bi("iterdir", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			entries, err := os.ReadDir(pathOf(a[0]))
			if err != nil {
				ip.raise(ip.excType("OSError"), "%v", err)
			}
			var out []object.Object
			for _, e := range entries {
				inst := &object.Instance{Class: pathClass, Dict: map[string]object.Object{}}
				inst.Dict["_p"] = &object.Str{V: filepath.Join(pathOf(a[0]), e.Name())}
				out = append(out, inst)
			}
			return &object.List{Items: out}, nil
		})
		pathClass.Dict["glob"] = bi("glob", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			pattern := filepath.Join(pathOf(a[0]), a[1].Str())
			matches, err := filepath.Glob(pattern)
			if err != nil {
				ip.raise(ip.excType("ValueError"), "%v", err)
			}
			var out []object.Object
			for _, mt := range matches {
				inst := &object.Instance{Class: pathClass, Dict: map[string]object.Object{}}
				inst.Dict["_p"] = &object.Str{V: mt}
				out = append(out, inst)
			}
			return &object.List{Items: out}, nil
		})
		pathClass.Dict["resolve"] = bi("resolve", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			p, _ := filepath.Abs(pathOf(a[0]))
			inst := &object.Instance{Class: pathClass, Dict: map[string]object.Object{}}
			inst.Dict["_p"] = &object.Str{V: p}
			return inst, nil
		})
		pathClass.Dict["joinpath"] = pathClass.Dict["__truediv__"]
		pathClass.Dict["__repr__"] = bi("__repr__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Str{V: "PosixPath('" + pathOf(a[0]) + "')"}, nil
		})
	}
	m.Dict["Path"] = pathClass
	m.Dict["PurePath"] = pathClass
	return m
}

// ==================== logging ====================

func modLogging(ip *Interp) *object.Module {
	m := newModule("logging")
	level := 2 // WARNING
	m.Dict["DEBUG"] = &object.Int{V: 0}
	m.Dict["INFO"] = &object.Int{V: 1}
	m.Dict["WARNING"] = &object.Int{V: 2}
	m.Dict["ERROR"] = &object.Int{V: 3}
	m.Dict["CRITICAL"] = &object.Int{V: 4}
	m.Dict["basicConfig"] = bi("basicConfig", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if k != nil {
			if v, ok := k["level"]; ok {
				if n, ok2 := object.AsInt(v); ok2 {
					level = int(n)
				}
			}
		}
		return object.None, nil
	})
	loggerCls := &object.TypeObj{Name: "Logger", Dict: map[string]object.Object{}}
	emit := func(lv int, tag string, name string, msg string) {
		if lv < level {
			return
		}
		if name != "" {
			fmt.Fprintf(ip.Stderr, "%s:%s:%s\n", tag, name, msg)
		} else {
			fmt.Fprintf(ip.Stderr, "%s:%s\n", tag, msg)
		}
	}
	mk := func(tag string, lv int) object.BuiltinFn {
		return func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			name := ""
			start := 0
			if len(a) > 0 {
				if inst, ok := a[0].(*object.Instance); ok {
					if nm, ok2 := inst.Dict["name"].(*object.Str); ok2 {
						name = nm.V
					}
				}
				start = 1
			}
			var parts []string
			for _, x := range a[start:] {
				parts = append(parts, ip.StrOf(x))
			}
			emit(lv, tag, name, strings.Join(parts, " "))
			return object.None, nil
		}
	}
	loggerCls.Dict["debug"] = bi("debug", mk("DEBUG", 0))
	loggerCls.Dict["info"] = bi("info", mk("INFO", 1))
	loggerCls.Dict["warning"] = bi("warning", mk("WARNING", 2))
	loggerCls.Dict["warn"] = loggerCls.Dict["warning"]
	loggerCls.Dict["error"] = bi("error", mk("ERROR", 3))
	loggerCls.Dict["critical"] = bi("critical", mk("CRITICAL", 4))
	loggerCls.Dict["setLevel"] = bi("setLevel", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 1 {
			if n, ok := object.AsInt(a[1]); ok {
				level = int(n)
			}
		}
		return object.None, nil
	})
	m.Dict["getLogger"] = bi("getLogger", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		name := ""
		if len(a) > 0 {
			name = a[0].Str()
		}
		inst := &object.Instance{Class: loggerCls, Dict: map[string]object.Object{}}
		inst.Dict["name"] = &object.Str{V: name}
		return inst, nil
	})
	m.Dict["debug"] = bi("debug", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var parts []string
		for _, x := range a {
			parts = append(parts, ip.StrOf(x))
		}
		emit(0, "DEBUG", "", strings.Join(parts, " "))
		return object.None, nil
	})
	m.Dict["info"] = bi("info", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var parts []string
		for _, x := range a {
			parts = append(parts, ip.StrOf(x))
		}
		emit(1, "INFO", "", strings.Join(parts, " "))
		return object.None, nil
	})
	m.Dict["warning"] = bi("warning", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var parts []string
		for _, x := range a {
			parts = append(parts, ip.StrOf(x))
		}
		emit(2, "WARNING", "", strings.Join(parts, " "))
		return object.None, nil
	})
	m.Dict["error"] = bi("error", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var parts []string
		for _, x := range a {
			parts = append(parts, ip.StrOf(x))
		}
		emit(3, "ERROR", "", strings.Join(parts, " "))
		return object.None, nil
	})
	return m
}

// ==================== argparse ====================

func modArgparse(ip *Interp) *object.Module {
	m := newModule("argparse")
	parserCls := &object.TypeObj{Name: "ArgumentParser", Dict: map[string]object.Object{}}
	parserCls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		self := a[0].(*object.Instance)
		self.Dict["_args"] = &object.List{}
		return object.None, nil
	})
	parserCls.Dict["add_argument"] = bi("add_argument", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		self := a[0].(*object.Instance)
		spec := &object.Dict{}
		if len(a) > 1 {
			spec.Set(&object.Str{V: "name"}, &object.Str{V: a[1].Str()})
		}
		for kk, vv := range k {
			spec.Set(&object.Str{V: kk}, vv)
		}
		lst := self.Dict["_args"].(*object.List)
		lst.Items = append(lst.Items, spec)
		return object.None, nil
	})
	parserCls.Dict["parse_args"] = bi("parse_args", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		self := a[0].(*object.Instance)
		var argv []string
		if len(a) > 1 {
			for _, v := range ip.ToSlice(a[1]) {
				argv = append(argv, v.Str())
			}
		} else {
			argv = os.Args[2:]
		}
		nsCls := &object.TypeObj{Name: "Namespace", Dict: map[string]object.Object{}}
		ns := &object.Instance{Class: nsCls, Dict: map[string]object.Object{}}
		specs := self.Dict["_args"].(*object.List)
		pos := 0
		for _, sp := range specs.Items {
			d := sp.(*object.Dict)
			nameV, _ := d.Get(&object.Str{V: "name"})
			name := nameV.Str()
			action := ""
			if v, ok := d.Get(&object.Str{V: "action"}); ok {
				action = v.Str()
			}
			dest := strings.TrimLeft(name, "-")
			if v, ok := d.Get(&object.Str{V: "dest"}); ok {
				dest = v.Str()
			}
			if d.Index(&object.Str{V: "dest"}) >= 0 {
				_ = dest
			}
			var val object.Object
			if strings.HasPrefix(name, "-") {
				if action == "store_true" {
					val = object.FalseObj
					for _, arg := range argv {
						if arg == name {
							val = object.TrueObj
						}
					}
				} else {
					val = object.None
					for i, arg := range argv {
						if arg == name && i+1 < len(argv) {
							val = &object.Str{V: argv[i+1]}
						}
					}
				}
			} else {
				if pos < len(argv) {
					val = &object.Str{V: argv[pos]}
					pos++
				} else {
					val = object.None
				}
			}
			if val == object.None {
				if dv, ok := d.Get(&object.Str{V: "default"}); ok {
					val = dv
				}
			}
			if tv, ok := d.Get(&object.Str{V: "type"}); ok && val != object.None {
				val = ip.Call(tv, []object.Object{val}, nil)
			}
			ns.Dict[dest] = val
		}
		return ns, nil
	})
	parserCls.Dict["parse_known_args"] = parserCls.Dict["parse_args"]
	m.Dict["ArgumentParser"] = parserCls
	return m
}

// ==================== unittest（简版） ====================

func modUnittest(ip *Interp) *object.Module {
	m := newModule("unittest")
	tcCls := &object.TypeObj{Name: "TestCase", Dict: map[string]object.Object{}}
	check := func(name string, cond bool, msg string) {
		if !cond {
			ip.raise(ip.excType("AssertionError"), "%s: %s", name, msg)
		}
	}
	tcCls.Dict["assertEqual"] = bi("assertEqual", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		check("assertEqual", ip.Eq(a[1], a[2]), ip.Repr(a[1])+" != "+ip.Repr(a[2]))
		return object.None, nil
	})
	tcCls.Dict["assertNotEqual"] = bi("assertNotEqual", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		check("assertNotEqual", !ip.Eq(a[1], a[2]), ip.Repr(a[1])+" == "+ip.Repr(a[2]))
		return object.None, nil
	})
	tcCls.Dict["assertTrue"] = bi("assertTrue", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		check("assertTrue", object.Truthy(a[1]), ip.Repr(a[1]))
		return object.None, nil
	})
	tcCls.Dict["assertFalse"] = bi("assertFalse", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		check("assertFalse", !object.Truthy(a[1]), ip.Repr(a[1]))
		return object.None, nil
	})
	tcCls.Dict["assertIn"] = bi("assertIn", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		check("assertIn", ip.Contains(a[2], a[1]), ip.Repr(a[1]))
		return object.None, nil
	})
	tcCls.Dict["assertIsNone"] = bi("assertIsNone", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		_, isNone := a[1].(*object.NoneObj)
		check("assertIsNone", isNone, ip.Repr(a[1]))
		return object.None, nil
	})
	tcCls.Dict["assertAlmostEqual"] = bi("assertAlmostEqual", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		x, _ := object.AsFloat(a[1])
		y, _ := object.AsFloat(a[2])
		places := 7.0
		if len(a) > 3 {
			if v, ok := object.AsInt(a[3]); ok {
				places = float64(v)
			}
		}
		tol := math.Pow(10, -places)
		check("assertAlmostEqual", math.Abs(x-y) < tol, ip.Repr(a[1])+" != "+ip.Repr(a[2]))
		return object.None, nil
	})
	m.Dict["TestCase"] = tcCls
	m.Dict["main"] = bi("main", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	return m
}

var _ = strconv.Itoa
