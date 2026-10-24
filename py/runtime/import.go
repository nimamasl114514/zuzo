package runtime

import (
	"os"
	"path/filepath"
	"strings"

	"zuzo/py/ast"
	"zuzo/py/object"
)

// registerBuiltinModules 由 stdlib.go 实现
var builtinModuleInits = map[string]func(*Interp) *object.Module{}

// loadModule 加载模块（内置优先，其次文件）
func (ip *Interp) loadModule(name string) *object.Module {
	if m, ok := ip.Modules[name]; ok {
		return m
	}
	// 宿主注入的模块（如跨语言桥的 zuzo）：全局作用域已有同名 Module 时直接复用
	if v, ok := ip.Globals.GetLocal(name); ok {
		if m, isMod := v.(*object.Module); isMod {
			ip.Modules[name] = m
			return m
		}
	}
	if init, ok := builtinModuleInits[name]; ok {
		m := init(ip)
		ip.Modules[name] = m
		return m
	}
	// 查找文件
	rel := strings.ReplaceAll(name, ".", string(filepath.Separator))
	for _, dir := range ip.SysPath {
		base := dir
		if base == "" {
			base = "."
		}
		candidates := []string{
			filepath.Join(base, rel+".py"),
			filepath.Join(base, rel, "__init__.py"),
		}
		for _, path := range candidates {
			if st, err := os.Stat(path); err == nil && !st.IsDir() {
				return ip.loadModuleFromFile(name, path)
			}
		}
	}
	// 顶级包名为内置模块时，允许子模块缺失
	if i := strings.Index(name, "."); i > 0 {
		if init, ok := builtinModuleInits[name[:i]]; ok {
			parent := init(ip)
			cur := parent
			for _, part := range strings.Split(name[i+1:], ".") {
				if sub, ok := cur.Dict[part].(*object.Module); ok {
					cur = sub
					continue
				}
				m := &object.Module{Name: cur.Name + "." + part, Dict: map[string]object.Object{}}
				cur.Dict[part] = m
				cur = m
			}
			ip.Modules[name] = cur
			return cur
		}
	}
	ip.raise(ip.excType("ModuleNotFoundError"), "No module named '%s'", name)
	return nil
}

// loadModuleFromFile 从 .py 文件加载模块
func (ip *Interp) loadModuleFromFile(name, path string) *object.Module {
	data, err := os.ReadFile(path)
	if err != nil {
		ip.raise(ip.excType("ImportError"), "cannot read module '%s': %v", name, err)
	}
	src := string(data)
	m, errs := ParseSourceWithFile(src, path)
	if len(errs) > 0 {
		ip.raise(ip.excType("SyntaxError"), "%s", errs[0])
	}
	mod := &object.Module{Name: name, Dict: map[string]object.Object{}, Path: path}
	env := object.NewEnv(nil)
	env.Globals = env
	env.Set("__name__", &object.Str{V: name})
	env.Set("__file__", &object.Str{V: path})
	env.Set("__doc__", &object.Str{V: extractDoc(m.Body)})
	ip.Modules[name] = mod
	// 模块内可访问内置
	env.Parent = ip.Builtins
	for _, st := range m.Body {
		ip.execStmt(st, env)
	}
	for k, v := range env.Vars {
		mod.Dict[k] = v
	}
	return mod
}

// execImport import x / import x as y
func (ip *Interp) execImport(node *ast.Import, env *object.Env) {
	for _, al := range node.Names {
		mod := ip.loadModule(al.Name)
		name := al.As
		if name == "" {
			parts := strings.Split(al.Name, ".")
			name = parts[0]
			if len(parts) > 1 {
				// import a.b 绑定 a
				root := ip.loadModule(parts[0])
				env.Set(name, root)
				continue
			}
		}
		env.Set(name, mod)
	}
}

// execImportFrom from x import y
func (ip *Interp) execImportFrom(node *ast.ImportFrom, env *object.Env) {
	var mod *object.Module
	if node.Module == "" && node.Level > 0 {
		// 相对导入：退化为当前目录
		ip.raise(ip.excType("ImportError"), "relative import not supported")
	}
	mod = ip.loadModule(node.Module)
	if len(node.Names) == 1 && node.Names[0].Name == "*" {
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
		// 尝试子模块
		sub := ip.protect(func() { _ = ip.loadModule(node.Module + "." + al.Name) })
		if sub == nil {
			if sm, ok := ip.Modules[node.Module+"."+al.Name]; ok {
				env.Set(name, sm)
				continue
			}
		}
		ip.raise(ip.excType("ImportError"), "cannot import name '%s' from '%s'", al.Name, node.Module)
	}
}
