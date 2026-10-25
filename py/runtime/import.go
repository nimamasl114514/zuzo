package runtime

// import.go — 模块导入系统（2026-10-25 第13轮重构）
//
// 相比首版（只认 sys.path 下的 x.py / x/__init__.py），本次补齐：
//   1. 父包递归：import a.b 先加载 a，再把 b 挂到 a 的属性上；
//   2. 子模块按父包 __path__ 查找（不是全局 sys.path）；
//   3. 包/模块元信息：__name__ / __package__ / __path__ / __file__ / __spec__ / __loader__；
//   4. 相对导入 from . import x / from ..pkg import y（按 __package__ 解析）；
//   5. namespace package（PEP 420：目录无 __init__.py 也算包）；
//   6. C 扩展探测（.pyd/.so）——本轮不直接加载，抛带标记的 ImportError 交进程桥接管；
//   7. sys.modules 同步 + 惰性 site-packages 探测（paths.go）。
//
// 查找顺序（与 CPython 简化对齐）：sys.modules → 宿主注入 → 内置 → 文件/包/命名空间 → 惰性探测重试。

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zuzo/py/ast"
	"zuzo/py/object"
)

// builtinModuleInits 由 stdlib*.go 注册
var builtinModuleInits = map[string]func(*Interp) *object.Module{}

// moduleSpecType 模块规格类型（PEP 451 最小子集：name / origin / submodule_search_locations）
var moduleSpecType = &object.TypeObj{Name: "ModuleSpec", Dict: map[string]object.Object{}}

// findKind 模块查找结果类型
type findKind int

const (
	findNone      findKind = iota
	findSource             // <dir>/x.py
	findPackage            // <dir>/x/__init__.py
	findNamespace          // <dir>/x（无 __init__.py，PEP 420）
	findExtension          // <dir>/x.pyd / x*.pyd（C 扩展）
)

type findResult struct {
	kind findKind
	path string   // source / package / extension 的文件路径
	dirs []string // namespace 包的目录集合
}

// ==================== 顶层入口 ====================

// importModule 唯一模块加载入口：返回已注册或新加载的模块对象。
func (ip *Interp) importModule(name string) *object.Module {
	name = strings.TrimSpace(name)
	if name == "" {
		ip.raise(ip.excType("ImportError"), "empty module name")
	}
	if m := ip.cachedModule(name); m != nil {
		return m
	}
	// 宿主注入的模块（如跨语言桥的 zuzo）：全局作用域已有同名 Module 时直接复用
	if v, ok := ip.Globals.GetLocal(name); ok {
		if m, isMod := v.(*object.Module); isMod {
			ip.registerModule(name, m)
			return m
		}
	}
	// 内置模块（含 os.path / urllib.request 这类点号名）
	if init, ok := builtinModuleInits[name]; ok {
		m := init(ip)
		if m.Name == "" {
			m.Name = name
		}
		ip.registerModule(name, m)
		ip.afterBuiltinInit(name, m)
		// import a.b 要同时把 b 挂到 a 的属性上（CPython 行为）
		if i := strings.LastIndex(name, "."); i > 0 {
			parent := ip.importModule(name[:i])
			parent.Dict[name[i+1:]] = m
		}
		return m
	}
	// 带父包：先加载父，再按父的 __path__ 找子
	if i := strings.LastIndex(name, "."); i > 0 {
		return ip.importSubmodule(name, name[:i], name[i+1:])
	}
	// 顶层模块
	if m := ip.loadFromDirs(name, name, ip.sysPathDirs()); m != nil {
		return m
	}
	// 惰性探测 site-packages 后重试一次
	if ip.probeThirdPartyPaths() {
		if m := ip.loadFromDirs(name, name, ip.sysPathDirs()); m != nil {
			return m
		}
	}
	ip.raiseModuleNotFound(name, "No module named '%s'", name)
	return nil
}

// importSubmodule 加载 a.b（父包已存在或先递归加载）。
func (ip *Interp) importSubmodule(full, parentName, sub string) *object.Module {
	parent := ip.importModule(parentName)
	if m := ip.cachedModule(full); m != nil {
		return m
	}
	// 父包若是内置模块且已带同名子模块属性，直接复用
	if v, ok := parent.Dict[sub]; ok {
		if m, isMod := v.(*object.Module); isMod {
			ip.registerModule(full, m)
			return m
		}
	}
	dirs := moduleDirs(parent)
	if len(dirs) > 0 {
		if m := ip.loadFromDirs(full, sub, dirs); m != nil {
			parent.Dict[sub] = m
			return m
		}
	}
	// 内置父模块没有 __path__（如内置 urllib），但磁盘上有同名包目录：
	// 在顶层 sys.path 里按完整名再试一次（urllib.parse → <Lib>/urllib/parse.py）
	if len(dirs) == 0 {
		if m := ip.loadFromDirs(full, full, ip.sysPathDirs()); m != nil {
			parent.Dict[sub] = m
			return m
		}
	}
	if ip.probeThirdPartyPaths() {
		dirs = moduleDirs(parent)
		if len(dirs) > 0 {
			if m := ip.loadFromDirs(full, sub, dirs); m != nil {
				return m
			}
		}
	}
	if _, isBuiltinParent := builtinModuleInits[parentName]; isBuiltinParent {
		if m := ip.synthBuiltinChild(full, parent, sub); m != nil {
			return m
		}
	}
	ip.raiseModuleNotFound(full, "No module named '%s'; '%s' is not a package", full, parentName)
	return nil
}

// synthBuiltinChild 为内置父模块构造占位子模块（如内置 urllib 下未单独实现的子名）。
func (ip *Interp) synthBuiltinChild(full string, parent *object.Module, sub string) *object.Module {
	cur := parent
	for _, part := range strings.Split(strings.TrimPrefix(full, parent.Name+"."), ".") {
		if prev, ok := cur.Dict[part].(*object.Module); ok {
			cur = prev
			continue
		}
		// __synth__ 标记：这是为内置父模块合成的空占位子模块，不携带任何真实成员。
		// from-import 探测必须能识别它，否则「from X import 缺失名」会静默拿到一个空 module
		// （实锤：from json import JSONDecodeError → base #1 is module）。
		m := &object.Module{Name: cur.Name + "." + part, Dict: map[string]object.Object{"__synth__": object.TrueObj}}
		cur.Dict[part] = m
		cur = m
	}
	ip.registerModule(full, cur)
	return cur
}

// cachedModule 从 sys.modules 取已加载模块。
func (ip *Interp) cachedModule(name string) *object.Module {
	if m, ok := ip.Modules[name]; ok {
		return m
	}
	return nil
}

// registerModule 注册到 sys.modules（两处同步：map + dict 视图）。
func (ip *Interp) registerModule(name string, m *object.Module) {
	ip.Modules[name] = m
	if ip.SysModules != nil {
		ip.SysModules.Set(&object.Str{V: name}, m)
	}
}

// afterBuiltinInit 在内置模块构造并注册之后做「后置接续」（此时同模块名已可安全被循环 import）。
// 目前只用于 os：CPython 的 pathlib.py 把 os.path 当 _flavour 用，需要完整 ntpath 语义
// （splitroot / splitdrive / normcase / UNC 处理）——Go 手写简化版撑不住，改成直载 ntpath.py。
func (ip *Interp) afterBuiltinInit(name string, m *object.Module) {
	if name != "os" {
		return
	}
	leaf := "posixpath"
	if isWindows() {
		leaf = "ntpath"
	}
	defer func() {
		if r := recover(); r != nil {
			// ntpath 加载失败（缺依赖等）：保留 Go 版 os.path 作为降级，不阻断主链
		}
	}()
	if pm := ip.importModule(leaf); pm != nil {
		m.Dict["path"] = pm
		if ip.SysModules != nil {
			ip.SysModules.Set(&object.Str{V: "os.path"}, pm)
		}
		ip.Modules["os.path"] = pm
	}
}

// moduleDirs 取包的 __path__（无则空）。
func moduleDirs(m *object.Module) []string {
	v, ok := m.Dict["__path__"]
	if !ok {
		return nil
	}
	switch p := v.(type) {
	case *object.List:
		out := make([]string, 0, len(p.Items))
		for _, it := range p.Items {
			if s, ok := it.(*object.Str); ok {
				out = append(out, s.V)
			}
		}
		return out
	}
	return nil
}

// ==================== 查找 ====================

// loadFromDirs 在给定 search path 里查找并加载 sub（相对名）。
func (ip *Interp) loadFromDirs(full, sub string, dirs []string) *object.Module {
	fr := findModuleIn(dirs, sub)
	switch fr.kind {
	case findSource, findPackage:
		return ip.loadModuleFromFile(full, fr.path)
	case findNamespace:
		m := &object.Module{Name: full, Dict: map[string]object.Object{}}
		items := make([]object.Object, 0, len(fr.dirs))
		for _, d := range fr.dirs {
			items = append(items, &object.Str{V: d})
		}
		m.Dict["__path__"] = &object.List{Items: items}
		m.Dict["__name__"] = &object.Str{V: full}
		m.Dict["__package__"] = &object.Str{V: full}
		m.Dict["__file__"] = object.None
		m.Dict["__spec__"] = ip.newModuleSpec(full, "", fr.dirs)
		ip.registerModule(full, m)
		return m
	case findExtension:
		ip.raise(ip.excType("ImportError"),
			"zuzo: C extension module '%s' needs external CPython (found %s); set ZUZO_PY_BRIDGE=1 to enable the subprocess bridge", full, fr.path)
	}
	return nil
}

// findModuleIn 在 search path 中查找子名（先包后模块，最后命名空间）。
func findModuleIn(dirs []string, sub string) findResult {
	rel := strings.ReplaceAll(sub, ".", string(filepath.Separator))
	var nsDirs []string
	for _, dir := range dirs {
		if dir == "" {
			dir = "."
		}
		// 1) 包：<dir>/x/__init__.py
		pkgDir := filepath.Join(dir, rel)
		if dirExists(pkgDir) {
			initFile := filepath.Join(pkgDir, "__init__.py")
			if fileExists(initFile) {
				return findResult{kind: findPackage, path: initFile}
			}
			nsDirs = append(nsDirs, pkgDir)
		}
		// 2) 模块：<dir>/x.py
		modFile := filepath.Join(dir, rel+".py")
		if fileExists(modFile) {
			return findResult{kind: findSource, path: modFile}
		}
		// 3) C 扩展：<dir>/x.pyd（Windows）/ x.so（POSIX）
		for _, ext := range []string{".pyd", ".so"} {
			if p := filepath.Join(dir, rel+ext); fileExists(p) {
				return findResult{kind: findExtension, path: p}
			}
		}
	}
	if len(nsDirs) > 0 {
		return findResult{kind: findNamespace, dirs: nsDirs}
	}
	return findResult{kind: findNone}
}

// ==================== 加载执行 ====================

// loadModuleFromFile 从 .py 文件加载模块/包。
func (ip *Interp) loadModuleFromFile(name, path string) *object.Module {
	abs, err2 := filepath.Abs(path)
	if err2 == nil {
		path = abs
	}
	data, err := os.ReadFile(path)
	if err != nil {
		ip.raise(ip.excType("ImportError"), "cannot read module '%s': %v", name, err)
	}
	src := string(data)
	m, errs := ParseSourceWithFile(src, path)
	if len(errs) > 0 {
		ip.raise(ip.excType("SyntaxError"), "%s (%s)", errs[0], path)
	}
	isPkg := strings.HasSuffix(path, string(filepath.Separator)+"__init__.py")
	mod := &object.Module{Name: name, Dict: map[string]object.Object{}, Path: path}

	env := object.NewEnv(nil)
	env.Globals = env
	pkgName := ""
	if i := strings.LastIndex(name, "."); i >= 0 {
		pkgName = name[:i]
	}
	if isPkg {
		pkgName = name
	}
	env.Set("__name__", &object.Str{V: name})
	env.Set("__file__", &object.Str{V: path})
	env.Set("__package__", &object.Str{V: pkgName})
	env.Set("__doc__", &object.Str{V: extractDoc(m.Body)})
	env.Set("__loader__", object.None)
	if isPkg {
		env.Set("__path__", &object.List{Items: []object.Object{&object.Str{V: filepath.Dir(path)}}})
		env.Set("__spec__", ip.newModuleSpec(name, path, []string{filepath.Dir(path)}))
	} else {
		env.Set("__spec__", ip.newModuleSpec(name, path, nil))
	}
	// 模块内可访问内置
	env.Parent = ip.Builtins

	// 先注册再执行：包内相对导入与循环导入都能拿到半成品模块（与 CPython 一致）
	ip.registerModule(name, mod)
	if isPkg {
		mod.Dict["__path__"] = &object.List{Items: []object.Object{&object.Str{V: filepath.Dir(path)}}}
	}
	finished := false
	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*PyError); ok {
				pe.Frames = append([]string{"  File \"" + path + "\""}, pe.Frames...)
				if pe.File == "" || pe.File == ip.MainFile {
					pe.File = path
				}
			}
			panic(r)
		}
	}()
	defer func() {
		if !finished {
			// 加载失败：把半成品从 sys.modules 摘掉（CPython 行为），
			// 否则后续 from X import Y 会命中缺属性的残模块，误报 "X is not a package"
			delete(ip.Modules, name)
			if ip.SysModules != nil {
				ip.SysModules.Delete(&object.Str{V: name})
			}
		}
	}()
	// 记录当前模块文件：错误信息里的行号/文件才对得上（原生函数抛错时尤其重要）
	prevFile := ip.MainFile
	ip.MainFile = path
	defer func() { ip.MainFile = prevFile }()
	for _, st := range m.Body {
		ip.execStmt(st, env)
	}
	finished = true
	for k, v := range env.Vars {
		mod.Dict[k] = v
	}
	if isPkg {
		if _, ok := mod.Dict["__path__"]; !ok {
			mod.Dict["__path__"] = &object.List{Items: []object.Object{&object.Str{V: filepath.Dir(path)}}}
		}
	}
	mod.Dict["__name__"] = &object.Str{V: name}
	mod.Dict["__file__"] = &object.Str{V: path}
	mod.Dict["__package__"] = &object.Str{V: pkgName}
	return mod
}

// newModuleSpec 构造 ModuleSpec 实例。
func (ip *Interp) newModuleSpec(name, origin string, subdirs []string) *object.Instance {
	inst := &object.Instance{Class: moduleSpecType, Dict: map[string]object.Object{}}
	inst.Dict["name"] = &object.Str{V: name}
	if origin == "" {
		inst.Dict["origin"] = object.None
	} else {
		inst.Dict["origin"] = &object.Str{V: origin}
	}
	if len(subdirs) == 0 {
		inst.Dict["submodule_search_locations"] = object.None
	} else {
		items := make([]object.Object, 0, len(subdirs))
		for _, d := range subdirs {
			items = append(items, &object.Str{V: d})
		}
		inst.Dict["submodule_search_locations"] = &object.List{Items: items}
	}
	inst.Dict["has_location"] = &object.Bool{V: origin != ""}
	return inst
}

// ==================== import 语句 ====================

// execImport import x / import x.y / import x as y
func (ip *Interp) execImport(node *ast.Import, env *object.Env) {
	for _, al := range node.Names {
		mod := ip.importModule(al.Name)
		if al.As != "" {
			env.Set(al.As, mod)
			continue
		}
		rootName := al.Name
		if i := strings.Index(rootName, "."); i > 0 {
			rootName = rootName[:i]
		}
		env.Set(rootName, ip.importModule(rootName))
	}
}

// execImportFrom from x import y / from . import y / from ..pkg import y
func (ip *Interp) execImportFrom(node *ast.ImportFrom, env *object.Env) {
	moduleName := node.Module
	if node.Level > 0 {
		pkg := ip.currentPackage(env)
		base := resolveRelative(pkg, node.Level)
		if base == "" && pkg == "" {
			ip.raise(ip.excType("ImportError"), "attempted relative import with no known parent package")
		}
		if moduleName == "" {
			moduleName = base
		} else if base != "" {
			moduleName = base + "." + moduleName
		}
	}
	if moduleName == "" {
		ip.raise(ip.excType("ImportError"), "empty module name in from-import")
	}
	mod := ip.importModule(moduleName)

	if len(node.Names) == 1 && node.Names[0].Name == "*" {
		if v, ok := mod.Dict["__all__"]; ok {
			for _, n := range ip.ToSlice(v) {
				key := n.Str()
				if item, ok := mod.Dict[key]; ok {
					env.Set(key, item)
				}
			}
			return
		}
		for k, v := range mod.Dict {
			if strings.HasPrefix(k, "_") {
				continue
			}
			env.Set(k, v)
		}
		return
	}
	for _, al := range node.Names {
		name := al.As
		if name == "" {
			name = al.Name
		}
		if v, ok := mod.Dict[al.Name]; ok {
			env.Set(name, v)
			continue
		}
		// 尝试子模块：from a import b → a.b
		subErr := ip.protect(func() { _ = ip.importModule(moduleName + "." + al.Name) })
		if sm, ok := ip.Modules[moduleName+"."+al.Name]; ok {
			// 合成占位子模块（__synth__）不是真子模块，不能拿来顶替 from-import 的名字：
			// 否则「from json import JSONDecodeError」会拿到空 module，ImportError 被静默吞掉。
			if _, isSynth := sm.Dict["__synth__"]; isSynth {
				delete(ip.Modules, moduleName+"."+al.Name)
				if ip.SysModules != nil {
					ip.SysModules.Delete(&object.Str{V: moduleName + "." + al.Name})
				}
			} else {
				env.Set(name, sm)
				continue
			}
		}
		if subErr != nil {
			ip.raise(ip.excType("ImportError"), "cannot import name '%s' from '%s' (%v)", al.Name, moduleName, subErr)
		}
		ip.raise(ip.excType("ImportError"), "cannot import name '%s' from '%s'", al.Name, moduleName)
	}
}

// currentPackage 取当前执行环境的 __package__（函数内 import 也能沿 LEGB 找到模块级值）。
func (ip *Interp) currentPackage(env *object.Env) string {
	if env != nil {
		if v, ok := env.Get("__package__"); ok {
			if s, ok := v.(*object.Str); ok {
				return s.V
			}
		}
		if v, ok := env.Get("__name__"); ok {
			if s, ok := v.(*object.Str); ok {
				if i := strings.LastIndex(s.V, "."); i > 0 {
					return s.V[:i]
				}
			}
		}
	}
	return ""
}

// resolveRelative 把 (__package__, level) 解析为绝对包名。
// level=1 表示当前包本身，level=2 上跳一级。
func resolveRelative(pkg string, level int) string {
	if pkg == "" {
		return ""
	}
	base := pkg
	for i := 1; i < level; i++ {
		if j := strings.LastIndex(base, "."); j >= 0 {
			base = base[:j]
		} else {
			base = ""
			break
		}
	}
	return base
}

// loadModule 兼容旧调用名（旧代码/外部引用）。
func (ip *Interp) loadModule(name string) *object.Module {
	return ip.importModule(name)
}

// raiseModuleNotFound 抛 ModuleNotFoundError 并带上 CPython 的 .name 属性
// （很多库写 `except ModuleNotFoundError as e: if e.name == "ssl"`
func (ip *Interp) raiseModuleNotFound(name string, format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	exc := object.NewException(ip.excType("ModuleNotFoundError"), &object.Str{V: msg})
	exc.Dict["name"] = &object.Str{V: name}
	exc.Dict["path"] = object.None
	panic(&PyError{Exc: exc, Frames: append([]string{}, ip.Frames...), File: ip.MainFile})
}
