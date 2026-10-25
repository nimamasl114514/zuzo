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
	"strings"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["importlib"] = modImportlib
	builtinModuleInits["importlib.util"] = modImportlibUtil
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
