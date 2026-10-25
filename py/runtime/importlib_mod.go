package runtime

// importlib_mod.go — importlib / importlib.util（第三方库高频依赖，如 requests 链、插件式代码）
//
// 只实现运行时真正需要的部分：
//   importlib.import_module(name, package=None)  —— 支持相对名（前导点）
//   importlib.reload(mod)
//   importlib.invalidate_caches()
//   importlib.util.find_spec(name, package=None)
//   importlib.util.module_from_spec(spec)
//   importlib.util.spec_from_file_location(name, location)

import (
	"fmt"
	"path/filepath"
	"strings"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["importlib"] = modImportlib
	builtinModuleInits["importlib.util"] = modImportlibUtil
	builtinModuleInits["importlib.resources"] = modImportlibResources
}

func modImportlib(ip *Interp) *object.Module {
	m := newModule("importlib")

	// import_module(name, package=None)
	m.Dict["import_module"] = bi("import_module", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("import_module() missing required argument 'name'")
		}
		name := a[0].Str()
		pkg := ""
		if len(a) > 1 {
			pkg = a[1].Str()
		}
		if v, ok := k["package"]; ok && pkg == "" {
			pkg = v.Str()
		}
		name = ip.absImportName(name, pkg)
		return ip.importModule(name), nil
	})

	// reload(mod)
	m.Dict["reload"] = bi("reload", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("reload() missing required argument 'module'")
		}
		mod, ok := a[0].(*object.Module)
		if !ok {
			ip.TypeError("reload() argument must be a module")
		}
		if mod.Path == "" {
			return mod, nil // 内置模块无法重载
		}
		delete(ip.Modules, mod.Name)
		if ip.SysModules != nil {
			ip.SysModules.Delete(&object.Str{V: mod.Name})
		}
		return ip.importModule(mod.Name), nil
	})

	m.Dict["invalidate_caches"] = bi("invalidate_caches", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})

	util := modImportlibUtil(ip)
	m.Dict["util"] = util
	ip.registerModule("importlib.util", util)

	// importlib.resources 用原生实现（见 modImportlibResources 头部注释）
	res := modImportlibResources(ip)
	m.Dict["resources"] = res
	ip.registerModule("importlib.resources", res)
	return m
}

func modImportlibUtil(ip *Interp) *object.Module {
	m := newModule("importlib.util")

	m.Dict["find_spec"] = bi("find_spec", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("find_spec() missing required argument 'name'")
		}
		name := a[0].Str()
		if len(a) > 1 {
			name = ip.absImportName(name, a[1].Str())
		}
		if spec, ok := ip.findSpec(name); ok {
			return spec, nil
		}
		return object.None, nil
	})

	m.Dict["module_from_spec"] = bi("module_from_spec", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("module_from_spec() missing required argument 'spec'")
		}
		name := ""
		if inst, ok := a[0].(*object.Instance); ok {
			if v, ok := inst.Dict["name"]; ok {
				name = v.Str()
			}
		}
		return ip.importModule(name), nil
	})

	m.Dict["spec_from_file_location"] = bi("spec_from_file_location", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("spec_from_file_location() requires (name, location)")
		}
		name, loc := a[0].Str(), a[1].Str()
		return ip.newModuleSpec(name, loc, nil), nil
	})

	m.Dict["find_loader"] = bi("find_loader", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})

	return m
}

// absImportName 把相对名（. / .. / ...）按 package 解析成绝对名。
func (ip *Interp) absImportName(name, pkg string) string {
	if !strings.HasPrefix(name, ".") {
		return name
	}
	lvl := 0
	for lvl < len(name) && name[lvl] == '.' {
		lvl++
	}
	base := resolveRelative(pkg, lvl)
	rest := name[lvl:]
	switch {
	case rest == "":
		return base
	case base == "":
		return rest
	default:
		return base + "." + rest
	}
}

// findSpec 只查找不加载（importlib.util.find_spec 的底座）。
func (ip *Interp) findSpec(name string) (*object.Instance, bool) {
	if m, ok := ip.Modules[name]; ok {
		if s, ok := m.Dict["__spec__"].(*object.Instance); ok {
			return s, true
		}
		return ip.newModuleSpec(name, m.Path, moduleDirs(m)), true
	}
	if _, ok := builtinModuleInits[name]; ok {
		return ip.newModuleSpec(name, "built-in", nil), true
	}
	dirs := ip.sysPathDirs()
	sub := name
	if i := strings.LastIndex(name, "."); i > 0 {
		parent, ok := ip.Modules[name[:i]]
		if !ok {
			return nil, false
		}
		dirs = moduleDirs(parent)
		sub = name[i+1:]
	}
	fr := findModuleIn(dirs, sub)
	if fr.kind == findNone {
		if ip.probeThirdPartyPaths() {
			if i := strings.LastIndex(name, "."); i > 0 {
				if parent, ok := ip.Modules[name[:i]]; ok {
					dirs = moduleDirs(parent)
				}
			} else {
				dirs = ip.sysPathDirs()
			}
			fr = findModuleIn(dirs, sub)
		}
	}
	switch fr.kind {
	case findSource, findPackage:
		return ip.newModuleSpec(name, fr.path, nil), true
	case findNamespace:
		return ip.newModuleSpec(name, "", fr.dirs), true
	case findExtension:
		return ip.newModuleSpec(name, fr.path, nil), true
	}
	return nil, false
}

// ==================== importlib.resources（原生实现） ====================
//
// 为什么不用 Lib/importlib/resources/ 里的纯 py 实现：
//
//	_common.files(anchor) 走的是 `@functools.singledispatch def resolve(cand)`，
//	str 分支用**函数注解形式**注册（`@resolve.register` + `def _(cand: str)`）。
//	CPython 的 singledispatch.register 靠读函数的 __annotations__ 取类型，
//	而 zuzo 的函数对象没有 __annotations__（对象层就没有这个属性），
//	于是 str 分支永远注册不上 → resolve("certifi") 原样返回字符串 →
//	from_package(str) → str.__spec__ → AttributeError。
//	实测：functools.singledispatch 是 stdlib.go:2344 的桩，dispatch 恒返回基函数。
//
// 所以这里把 importlib.resources 做成本地实现：files() 直接落到 pathlib.Path，
// 真实文件系统语义（joinpath / read_text / iterdir / open 全部现成）。
// 覆盖范围：files / as_file / open_binary / open_text / read_binary / read_text /
//
//	is_resource / contents / path。zipimport 与 Traversable ABC 未覆盖。
func modImportlibResources(ip *Interp) *object.Module {
	m := newModule("importlib.resources")

	// files(anchor=None) -> pathlib.Path
	m.Dict["files"] = bi("files", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		anchor := object.Object(object.None)
		if len(a) > 0 {
			anchor = a[0]
		} else if v, ok := k["anchor"]; ok {
			anchor = v
		} else if v, ok := k["package"]; ok { // 3.12 改名前的旧关键字
			anchor = v
		}
		return ip.resFilesPath(anchor), nil
	})

	// as_file(traversable)：文件系统路径无需解包，直接给一个 no-op 上下文管理器
	m.Dict["as_file"] = bi("as_file", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("as_file() missing required argument 'path'")
		}
		return ip.resPathCtx(a[0]), nil
	})

	// path(package, resource)：3.11 前的旧 API，等价 with as_file(files(pkg)/res)
	m.Dict["path"] = bi("path", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("path() requires (package, resource)")
		}
		return ip.resPathCtx(ip.resChildPath(ip.resAnchorArg(a[0]), a[1].Str())), nil
	})

	m.Dict["read_binary"] = bi("read_binary", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("read_binary() requires (package, resource)")
		}
		p := ip.resChildPath(ip.resAnchorArg(a[0]), a[1].Str())
		return ip.Call(ip.GetAttr(p, "read_bytes"), []object.Object{}, nil), nil
	})

	m.Dict["read_text"] = bi("read_text", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("read_text() requires (package, resource)")
		}
		p := ip.resChildPath(ip.resAnchorArg(a[0]), a[1].Str())
		kw := map[string]object.Object{}
		if v, ok := k["encoding"]; ok {
			kw["encoding"] = v
		} else if len(a) > 2 {
			kw["encoding"] = a[2]
		}
		if v, ok := k["errors"]; ok {
			kw["errors"] = v
		}
		return ip.Call(ip.GetAttr(p, "read_text"), []object.Object{}, kw), nil
	})

	m.Dict["open_binary"] = bi("open_binary", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("open_binary() requires (package, resource)")
		}
		p := ip.resChildPath(ip.resAnchorArg(a[0]), a[1].Str())
		return ip.Call(ip.GetAttr(p, "open"), []object.Object{&object.Str{V: "rb"}}, nil), nil
	})

	m.Dict["open_text"] = bi("open_text", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("open_text() requires (package, resource)")
		}
		p := ip.resChildPath(ip.resAnchorArg(a[0]), a[1].Str())
		kw := map[string]object.Object{}
		if v, ok := k["encoding"]; ok {
			kw["encoding"] = v
		} else if len(a) > 2 {
			kw["encoding"] = a[2]
		}
		if v, ok := k["errors"]; ok {
			kw["errors"] = v
		}
		return ip.Call(ip.GetAttr(p, "open"), []object.Object{&object.Str{V: "r"}}, kw), nil
	})

	m.Dict["is_resource"] = bi("is_resource", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("is_resource() requires (package, resource)")
		}
		p := ip.resChildPath(ip.resAnchorArg(a[0]), a[1].Str())
		return ip.Call(ip.GetAttr(p, "is_file"), []object.Object{}, nil), nil
	})

	m.Dict["contents"] = bi("contents", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("contents() missing required argument 'package'")
		}
		base := ip.resFilesPath(ip.resAnchorArg(a[0]))
		it := ip.Call(ip.GetAttr(base, "iterdir"), []object.Object{}, nil)
		var out []object.Object
		for _, child := range ip.ToSlice(it) {
			out = append(out, ip.Call(ip.GetAttr(child, "name"), []object.Object{}, nil))
		}
		return &object.List{Items: out}, nil
	})

	return m
}

// resAnchorArg 旧 API 传的可能是模块，也可能是模块名字符串。
func (ip *Interp) resAnchorArg(o object.Object) object.Object { return o }

// resFilesPath 把 files(anchor) 落到 pathlib.Path（anchor 支持模块对象 / 模块名 / __path__ 列表）。
func (ip *Interp) resFilesPath(anchor object.Object) object.Object {
	dir := ip.resAnchorDir(anchor)
	pathCls := ip.importModule("pathlib").Dict["Path"]
	return ip.Call(pathCls, []object.Object{&object.Str{V: dir}}, nil)
}

// resAnchorDir 求 anchor 对应的包目录。
func (ip *Interp) resAnchorDir(anchor object.Object) string {
	var mod *object.Module
	switch v := anchor.(type) {
	case *object.Module:
		mod = v
	case *object.Str:
		if v.V == "" {
			ip.TypeError("Empty module name")
		}
		mod = ip.importModule(v.V)
	default:
		// files() 无参时 CPython 靠 inspect 回溯调用方 __name__；zuzo 没有帧对象，
		// 明确报错比猜错好（比静默返回错目录安全）。
		ip.TypeError("files() anchor must be a module or a module name, got %s", fmt.Sprintf("%T", anchor))
	}
	if dirs := moduleDirs(mod); len(dirs) > 0 {
		return dirs[0]
	}
	if f, ok := mod.Dict["__file__"].(*object.Str); ok && f.V != "" {
		return filepath.Dir(f.V)
	}
	ip.raise(ip.excType("TypeError"), "files() anchor '%s' has no __path__ or __file__", mod.Name)
	return ""
}

// resChildPath 求 <包目录>/resource（resource 可含 / 分隔的多级名）。
func (ip *Interp) resChildPath(anchor object.Object, resource string) object.Object {
	base := ip.resFilesPath(anchor)
	if resource == "" {
		return base
	}
	join := ip.GetAttr(base, "joinpath")
	parts := strings.Split(strings.ReplaceAll(resource, "\\", "/"), "/")
	args := make([]object.Object, 0, len(parts))
	for _, p := range parts {
		if p == "" {
			continue
		}
		args = append(args, &object.Str{V: p})
	}
	if len(args) == 0 {
		return base
	}
	return ip.Call(join, args, nil)
}

// resPathCtx 构造 as_file / path 的上下文管理器（真实路径无需临时解包）。
func (ip *Interp) resPathCtx(p object.Object) object.Object {
	cls := &object.TypeObj{Name: "_as_file", Dict: map[string]object.Object{}}
	inst := &object.Instance{Class: cls, Dict: map[string]object.Object{"_path": p, "_used": object.FalseObj}}
	cls.Dict["__enter__"] = bi("__enter__", func(x []object.Object, _ map[string]object.Object) (object.Object, error) {
		if len(x) > 0 {
			if inst, ok := x[0].(*object.Instance); ok {
				return inst.Dict["_path"], nil
			}
		}
		return p, nil
	})
	cls.Dict["__exit__"] = bi("__exit__", func(x []object.Object, _ map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	return inst
}
