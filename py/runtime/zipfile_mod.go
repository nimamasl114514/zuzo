package runtime

// zipfile_mod.go — zipfile 的 Go 简版实现（第14轮）
//
// 触发点：requests/utils.py:19 `import zipfile`（unzip_file 工具函数）。
// CPython 的 zipfile.py 依赖链很重（io/bz2/lzma/struct/…），在 zuzo 里加载不完整；
// 这里用 Go 的 archive/zip 直接提供 requests 实际用到的那几个入口。

import (
	"archive/zip"
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["zipfile"] = modZipfile
}

// zipHolder 让 *zip.Reader 能塞进 object.Dict（Dict 值必须是 object.Object）
type zipHolder struct{ r *zip.Reader }

func (z *zipHolder) Type() *object.TypeObj { return object.ObjectType }
func (z *zipHolder) Repr() string          { return "<zipfile>" }
func (z *zipHolder) Str() string           { return "<zipfile>" }

func modZipfile(ip *Interp) *object.Module {
	m := newModule("zipfile")
	m.Dict["ZIP_STORED"] = &object.Int{V: 0}
	m.Dict["ZIP_DEFLATED"] = &object.Int{V: 8}
	m.Dict["ZIP_BZIP2"] = &object.Int{V: 12}
	m.Dict["ZIP_LZMA"] = &object.Int{V: 14}

	// 读文件对象/路径统一取字节
	readAll := func(o object.Object) []byte {
		switch v := o.(type) {
		case *object.Bytes:
			return v.V
		case *object.ByteArray:
			return v.V
		case *object.Str:
			b, err := os.ReadFile(v.V)
			if err == nil {
				return b
			}
		case *object.Instance:
			// 文件对象：调用它的 read()（BytesIO / open() 都实现）
			var out []byte
			ip.protect(func() {
				ip.GetAttr(v, "seek")
				fn := ip.GetAttr(v, "read")
				r := ip.Call(fn, nil, nil)
				switch rv := r.(type) {
				case *object.Bytes:
					out = rv.V
				case *object.Str:
					out = []byte(rv.V)
				}
			})
			return out
		}
		return nil
	}

	m.Dict["is_zipfile"] = bi("is_zipfile", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return object.FalseObj, nil
		}
		data := readAll(a[0])
		if len(data) == 0 {
			return object.FalseObj, nil
		}
		_, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		return object.GetBool(err == nil), nil
	})

	zipCls := &object.TypeObj{Name: "ZipFile", Dict: map[string]object.Object{}}
	zipCls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("ZipFile() requires a file")
		}
		self, ok := a[0].(*object.Instance)
		if !ok {
			return object.None, nil
		}
		data := readAll(a[1])
		zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			ip.raise(ip.excType("BadZipFile"), "%v", err)
		}
		self.Dict["_zr"] = &zipHolder{r: zr}
		self.Dict["_data"] = &object.Bytes{V: data}
		return object.None, nil
	})
	zipCls.Dict["namelist"] = bi("namelist", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		self, _ := a[0].(*object.Instance)
		var out []object.Object
		if self != nil {
			if h, ok := self.Dict["_zr"].(*zipHolder); ok {
				for _, f := range h.r.File {
					out = append(out, &object.Str{V: f.Name})
				}
			}
		}
		return &object.List{Items: out}, nil
	})
	zipCls.Dict["read"] = bi("read", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("read() requires a name")
		}
		self, _ := a[0].(*object.Instance)
		name := a[1].Str()
		if self == nil {
			ip.raise(ip.excType("BadZipFile"), "bad zip")
		}
		h, ok := self.Dict["_zr"].(*zipHolder)
		if !ok {
			ip.raise(ip.excType("BadZipFile"), "bad zip")
		}
		for _, f := range h.r.File {
			if f.Name == name {
				rc, err := f.Open()
				if err != nil {
					ip.raise(ip.excType("BadZipFile"), "%v", err)
				}
				defer rc.Close()
				b, _ := io.ReadAll(rc)
				return &object.Bytes{V: b}, nil
			}
		}
		ip.raise(ip.excType("KeyError"), "There is no item named %q in the archive", name)
		return object.None, nil
	})
	zipCls.Dict["extractall"] = bi("extractall", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		self, _ := a[0].(*object.Instance)
		dst := "."
		if len(a) > 1 && a[1] != object.None {
			dst = a[1].Str()
		}
		if self != nil {
			if h, ok := self.Dict["_zr"].(*zipHolder); ok {
				for _, f := range h.r.File {
					path := filepath.Join(dst, f.Name)
					if f.FileInfo().IsDir() {
						os.MkdirAll(path, 0o755)
						continue
					}
					os.MkdirAll(filepath.Dir(path), 0o755)
					rc, err := f.Open()
					if err != nil {
						continue
					}
					out, err := os.Create(path)
					if err == nil {
						io.Copy(out, rc)
						out.Close()
					}
					rc.Close()
				}
			}
		}
		return object.None, nil
	})
	zipCls.Dict["close"] = bi("close", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	zipCls.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			return a[0], nil
		}
		return object.None, nil
	})
	zipCls.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.FalseObj, nil
	})
	m.Dict["ZipFile"] = zipCls
	m.Dict["BadZipFile"] = ip.excType("Exception")
	m.Dict["LargeZipFile"] = ip.excType("Exception")
	return m
}

// ==================== _ast（最小桩） ====================

func init() {
	builtinModuleInits["_ast"] = modAstStub
	// ast 也走内置桩：CPython 的 ast.py 依赖 _ast 的完整节点表，
	// zuzo 没有「用户态解析 Python AST」需求，加载它只会拖出一串缺失符号。
	builtinModuleInits["ast"] = func(ip *Interp) *object.Module {
		m := modAstStub(ip)
		m.Name = "ast"
		m.Dict["dump"] = bi("dump", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Str{V: "<AST>"}, nil
		})
		m.Dict["walk"] = bi("walk", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Iterator{}, nil
		})
		m.Dict["iter_fields"] = bi("iter_fields", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Iterator{}, nil
		})
		m.Dict["iter_child_nodes"] = bi("iter_child_nodes", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Iterator{}, nil
		})
		m.Dict["unparse"] = bi("unparse", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Str{V: ""}, nil
		})
		m.Dict["fix_missing_locations"] = bi("fix_missing_locations", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) > 0 {
				return a[0], nil
			}
			return object.None, nil
		})
		m.Dict["copy_location"] = m.Dict["fix_missing_locations"]
		return m
	}
}

// modAstStub CPython 的 ast.py 顶层 `from _ast import *`。
// zuzo 不解析 Python AST 给用户（自身 parser 独立），这里只需模块与节点基类存在。
func modAstStub(ip *Interp) *object.Module {
	m := newModule("_ast")
	astCls := &object.TypeObj{Name: "AST", Dict: map[string]object.Object{}}
	m.Dict["AST"] = astCls
	m.Dict["PyCF_ONLY_AST"] = &object.Int{V: 1024}
	m.Dict["PyCF_ALLOW_TOP_LEVEL_AWAIT"] = &object.Int{V: 8192}
	m.Dict["PyCF_TYPE_COMMENTS"] = &object.Int{V: 4096}
	// 常见节点类（空壳，供 isinstance / 继承用）
	for _, n := range []string{"mod", "stmt", "expr", "excepthandler", "pattern", "type_ignore", "slice",
		"boolop", "operator", "unaryop", "cmpop", "expr_context", "comprehension",
		"Module", "Expression", "FunctionDef", "AsyncFunctionDef", "ClassDef", "Name",
		"Constant", "Attribute", "Subscript", "Call", "BinOp", "UnaryOp", "BoolOp", "Compare", "List", "Tuple",
		"Dict", "Set", "Lambda", "If", "For", "While", "Return", "Assign", "AnnAssign", "Import", "ImportFrom",
		"Load", "Store", "Del", "Add", "Sub", "Mult", "Div", "Mod", "Pow", "USub", "UAdd", "Not", "And", "Or",
		"Eq", "NotEq", "Lt", "LtE", "Gt", "GtE", "Is", "IsNot", "In", "NotIn", "arg", "arguments", "keyword",
		"Pass", "Expr", "JoinedStr", "FormattedValue", "JoinedStr", "Starred", "Yield", "YieldFrom", "Await",
		"With", "AsyncWith", "Try", "ExceptHandler", "Raise", "Assert", "Delete", "Global", "Nonlocal",
		"comprehension", "IfExp", "Slice", "Index", "ExtSlice", "Ellipsis", "Num", "Str", "Bytes", "NameConstant",
		"NamedExpr", "Match", "match_case", "MatchValue", "MatchSingleton", "MatchSequence", "MatchMapping",
		"MatchClass", "MatchStar", "MatchAs", "MatchOr"} {
		if _, exists := m.Dict[n]; !exists {
			m.Dict[n] = &object.TypeObj{Name: n, Bases: []*object.TypeObj{astCls}, Dict: map[string]object.Object{}}
		}
	}
	m.Dict["parse"] = bi("parse", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		ip.raise(ip.excType("SyntaxError"), "_ast.parse is not supported in zuzo")
		return object.None, nil
	})
	m.Dict["literal_eval"] = bi("literal_eval", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		ip.raise(ip.excType("ValueError"), "malformed node")
		return object.None, nil
	})
	m.Dict["get_docstring"] = bi("get_docstring", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	return m
}

// ==================== dis（最小桩） ====================

func init() {
	// dis 只在调试/反汇编路径用（pdb / traceback 的增强输出），给个空壳避免拖入 opcode 链
	builtinModuleInits["dis"] = func(ip *Interp) *object.Module {
		m := newModule("dis")
		m.Dict["opname"] = &object.List{Items: []object.Object{}}
		m.Dict["opmap"] = &object.Dict{}
		m.Dict["cmp_op"] = &object.Tuple{}
		m.Dict["hasconst"] = &object.List{Items: []object.Object{}}
		m.Dict["hasname"] = &object.List{Items: []object.Object{}}
		m.Dict["hasjrel"] = &object.List{Items: []object.Object{}}
		m.Dict["hasjabs"] = &object.List{Items: []object.Object{}}
		m.Dict["dis"] = bi("dis", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		m.Dict["disassemble"] = m.Dict["dis"]
		m.Dict["get_instructions"] = bi("get_instructions", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Iterator{}, nil
		})
		m.Dict["COMPILER_FLAG_NAMES"] = &object.Dict{}
		m.Dict["COMPILER_FLAG_NAMES"] = &object.Dict{}
		m.Dict["findlinestarts"] = bi("findlinestarts", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Iterator{}, nil
		})
		m.Dict["stack_effect"] = bi("stack_effect", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Int{V: 0}, nil
		})
		m.Dict["Bytecode"] = &object.TypeObj{Name: "Bytecode", Dict: map[string]object.Object{}}
		m.Dict["Instruction"] = &object.TypeObj{Name: "Instruction", Dict: map[string]object.Object{}}
		return m
	}
}

// ==================== _imp（最小桩） ====================

func init() {
	builtinModuleInits["_imp"] = func(ip *Interp) *object.Module {
		m := newModule("_imp")
		m.Dict["acquire_lock"] = bi("acquire_lock", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		m.Dict["release_lock"] = m.Dict["acquire_lock"]
		m.Dict["is_builtin"] = bi("is_builtin", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.FalseObj, nil
		})
		m.Dict["is_frozen"] = m.Dict["is_builtin"]
		m.Dict["is_frozen_package"] = m.Dict["is_builtin"]
		m.Dict["extension_suffixes"] = bi("extension_suffixes", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.List{Items: []object.Object{&object.Str{V: ".pyd"}, &object.Str{V: ".so"}}}, nil
		})
		m.Dict["get_suffixes"] = m.Dict["extension_suffixes"]
		m.Dict["get_magic_number"] = bi("get_magic_number", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Bytes{V: []byte{0x55, 0x0d, 0x0d, 0x0a}}, nil
		})
		m.Dict["create_dynamic"] = bi("create_dynamic", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		m.Dict["create_builtin"] = m.Dict["create_dynamic"]
		m.Dict["exec_dynamic"] = m.Dict["create_dynamic"]
		m.Dict["exec_builtin"] = m.Dict["create_dynamic"]
		m.Dict["get_frozen_object"] = m.Dict["create_dynamic"]
		m.Dict["_fix_co_filename"] = m.Dict["create_dynamic"]
		m.Dict["PY_SOURCE"] = &object.Int{V: 1}
		m.Dict["PY_COMPILED"] = &object.Int{V: 2}
		m.Dict["C_EXTENSION"] = &object.Int{V: 3}
		m.Dict["PY_RESOURCE"] = &object.Int{V: 4}
		m.Dict["PKG_DIRECTORY"] = &object.Int{V: 5}
		m.Dict["C_BUILTIN"] = &object.Int{V: 6}
		m.Dict["PY_FROZEN"] = &object.Int{V: 7}
		m.Dict["PY_CODERESOURCE"] = &object.Int{V: 8}
		m.Dict["IMP_HOOK"] = &object.Int{V: 9}
		return m
	}
}

// ==================== _warnings / _thread / _stat 等 C 模块桩 ====================

func init() {
	builtinModuleInits["_warnings"] = func(ip *Interp) *object.Module {
		m := newModule("_warnings")
		noop := bi("noop", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		m.Dict["warn"] = noop
		m.Dict["warn_explicit"] = noop
		m.Dict["_filters_mutated"] = noop
		m.Dict["filters"] = &object.List{Items: []object.Object{}}
		m.Dict["once_registry"] = &object.Dict{}
		return m
	}
	builtinModuleInits["_thread"] = func(ip *Interp) *object.Module {
		m := newModule("_thread")
		m.Dict["allocate_lock"] = bi("allocate_lock", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			cls := &object.TypeObj{Name: "lock", Dict: map[string]object.Object{}}
			inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
			for _, n := range []string{"acquire", "release", "locked"} {
				n := n
				inst.Dict[n] = bi(n, func(a []object.Object, k map[string]object.Object) (object.Object, error) {
					if n == "locked" {
						return object.FalseObj, nil
					}
					if n == "acquire" {
						return object.TrueObj, nil
					}
					return object.None, nil
				})
			}
			return inst, nil
		})
		m.Dict["get_ident"] = bi("get_ident", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Int{V: 1}, nil
		})
		m.Dict["start_new_thread"] = bi("start_new_thread", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) > 0 {
				ip.Call(a[0], nil, nil)
			}
			return &object.Int{V: 1}, nil
		})
		m.Dict["stack_size"] = bi("stack_size", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Int{V: 0}, nil
		})
		m.Dict["RLock"] = m.Dict["allocate_lock"]
		m.Dict["TIMEOUT_MAX"] = &object.Float{V: 1e9}
		m.Dict["error"] = ip.excType("Exception")
		return m
	}
	builtinModuleInits["_stat"] = func(ip *Interp) *object.Module {
		m := newModule("_stat")
		for _, kv := range [][2]interface{}{
			{"S_IFMT", 0o170000}, {"S_IFDIR", 0o040000}, {"S_IFREG", 0o100000},
			{"S_IFLNK", 0o120000}, {"S_ISUID", 0o4000}, {"S_ISGID", 0o2000},
			{"S_ISVTX", 0o1000}, {"S_IRUSR", 0o400}, {"S_IWUSR", 0o200}, {"S_IXUSR", 0o100},
		} {
			m.Dict[kv[0].(string)] = &object.Int{V: int64(kv[1].(int))}
		}
		return m
	}
	builtinModuleInits["_locale"] = func(ip *Interp) *object.Module {
		m := newModule("_locale")
		m.Dict["LC_CTYPE"] = &object.Int{V: 0}
		m.Dict["LC_NUMERIC"] = &object.Int{V: 1}
		m.Dict["LC_TIME"] = &object.Int{V: 2}
		m.Dict["LC_COLLATE"] = &object.Int{V: 3}
		m.Dict["LC_MONETARY"] = &object.Int{V: 4}
		m.Dict["LC_MESSAGES"] = &object.Int{V: 5}
		m.Dict["LC_ALL"] = &object.Int{V: 6}
		m.Dict["CODESET"] = &object.Int{V: 14}
		m.Dict["setlocale"] = bi("setlocale", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Str{V: "C"}, nil
		})
		m.Dict["localeconv"] = bi("localeconv", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			d := &object.Dict{}
			for _, key := range []string{"decimal_point", "thousands_sep", "grouping", "int_curr_symbol",
				"currency_symbol", "mon_decimal_point", "mon_thousands_sep", "mon_grouping",
				"positive_sign", "negative_sign", "frac_digits", "p_cs_precedes", "n_cs_precedes",
				"p_sep_by_space", "n_sep_by_space", "p_sign_posn", "n_sign_posn",
				"int_frac_digits", "int_p_cs_precedes", "int_n_cs_precedes", "int_p_sep_by_space",
				"int_n_sep_by_space", "int_p_sign_posn", "int_n_sign_posn", "codeset"} {
				switch key {
				case "decimal_point":
					d.Set(&object.Str{V: key}, &object.Str{V: "."})
				case "thousands_sep", "mon_decimal_point", "mon_thousands_sep", "currency_symbol",
					"int_curr_symbol", "positive_sign", "negative_sign":
					d.Set(&object.Str{V: key}, &object.Str{V: ""})
				case "codeset":
					d.Set(&object.Str{V: key}, &object.Str{V: "UTF-8"})
				default:
					d.Set(&object.Str{V: key}, &object.Int{V: 0})
				}
			}
			return d, nil
		})
		return m
	}
}

// ==================== marshal（最小桩） ====================

func init() {
	builtinModuleInits["marshal"] = func(ip *Interp) *object.Module {
		m := newModule("marshal")
		m.Dict["version"] = &object.Int{V: 4}
		m.Dict["dumps"] = bi("dumps", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			ip.raise(ip.excType("ValueError"), "marshal.dumps is not supported in zuzo")
			return object.None, nil
		})
		m.Dict["dump"] = m.Dict["dumps"]
		m.Dict["loads"] = bi("loads", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			ip.raise(ip.excType("ValueError"), "bad marshal data")
			return object.None, nil
		})
		m.Dict["load"] = m.Dict["loads"]
		return m
	}
}

// ==================== nt / posix（平台模块别名） ====================

func init() {
	// Windows 上 CPython 的 os.py 会 `import nt`（POSIX 平台是 posix）。
	// zuzo 的 os 实现本身就是平台无关的 Go 版本，这里直接复用同一份工厂。
	builtinModuleInits["nt"] = func(ip *Interp) *object.Module {
		m := modOS(ip)
		m.Name = "nt"
		return m
	}
	builtinModuleInits["posix"] = func(ip *Interp) *object.Module {
		m := modOS(ip)
		m.Name = "posix"
		return m
	}
}

// ==================== winreg（最小桩） ====================

func init() {
	builtinModuleInits["winreg"] = func(ip *Interp) *object.Module {
		m := newModule("winreg")
		m.Dict["HKEY_CLASSES_ROOT"] = &object.Int{V: 0x80000000}
		m.Dict["HKEY_CURRENT_USER"] = &object.Int{V: 0x80000001}
		m.Dict["HKEY_LOCAL_MACHINE"] = &object.Int{V: 0x80000002}
		m.Dict["HKEY_USERS"] = &object.Int{V: 0x80000003}
		m.Dict["HKEY_CURRENT_CONFIG"] = &object.Int{V: 0x80000005}
		m.Dict["KEY_READ"] = &object.Int{V: 0x20019}
		m.Dict["KEY_WRITE"] = &object.Int{V: 0x20006}
		m.Dict["KEY_ALL_ACCESS"] = &object.Int{V: 0xF003F}
		m.Dict["REG_SZ"] = &object.Int{V: 1}
		m.Dict["REG_EXPAND_SZ"] = &object.Int{V: 2}
		m.Dict["REG_BINARY"] = &object.Int{V: 3}
		m.Dict["REG_DWORD"] = &object.Int{V: 4}
		m.Dict["OpenKey"] = bi("OpenKey", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			ip.raise(ip.excType("OSError"), "winreg is not supported in zuzo")
			return object.None, nil
		})
		m.Dict["OpenKeyEx"] = m.Dict["OpenKey"]
		m.Dict["QueryValueEx"] = m.Dict["OpenKey"]
		m.Dict["QueryValue"] = m.Dict["OpenKey"]
		m.Dict["CloseKey"] = bi("CloseKey", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		m.Dict["EnumKey"] = m.Dict["OpenKey"]
		m.Dict["EnumValue"] = m.Dict["OpenKey"]
		m.Dict["error"] = ip.excType("OSError")
		return m
	}
}

// ==================== _tokenize（最小桩） ====================

func init() {
	builtinModuleInits["_tokenize"] = func(ip *Interp) *object.Module {
		m := newModule("_tokenize")
		m.Dict["TokenInfo"] = &object.TypeObj{Name: "TokenInfo", Dict: map[string]object.Object{}}
		m.Dict["tokenize"] = bi("tokenize", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Iterator{}, nil
		})
		m.Dict["detect_encoding"] = bi("detect_encoding", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.Tuple{Items: []object.Object{&object.Str{V: "utf-8"}, &object.List{Items: []object.Object{}}}}, nil
		})
		return m
	}
}

// ==================== inspect（最小桩） ====================

func init() {
	builtinModuleInits["inspect"] = func(ip *Interp) *object.Module {
		m := newModule("inspect")
		yes := func(a []object.Object, k map[string]object.Object) (object.Object, error) { return object.TrueObj, nil }
		no := func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.FalseObj, nil
		}
		m.Dict["ismodule"] = bi("ismodule", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.GetBool(len(a) > 0 && object.TypeName(a[0]) == "module"), nil
		})
		m.Dict["isclass"] = bi("isclass", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			_, ok := a[0].(*object.TypeObj)
			return object.GetBool(ok), nil
		})
		m.Dict["isfunction"] = bi("isfunction", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			_, ok := a[0].(*object.Function)
			return object.GetBool(ok), nil
		})
		m.Dict["ismethod"] = bi("ismethod", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			_, ok := a[0].(*object.BoundMethod)
			return object.GetBool(ok), nil
		})
		m.Dict["isbuiltin"] = bi("isbuiltin", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			_, ok := a[0].(*object.Builtin)
			return object.GetBool(ok), nil
		})
		m.Dict["isroutine"] = bi("isroutine", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			switch a[0].(type) {
			case *object.Function, *object.Builtin, *object.BoundMethod:
				return object.TrueObj, nil
			}
			return object.FalseObj, nil
		})
		for _, n := range []string{"iscoroutine", "isawaitable", "isgenerator", "isgeneratorfunction",
			"isasyncgen", "isdatadescriptor", "ismethoddescriptor"} {
			m.Dict[n] = bi(n, no)
		}
		m.Dict["getdoc"] = bi("getdoc", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				return object.None, nil
			}
			r := ip.protect(func() {
				ip.GetAttr(a[0], "__doc__")
			})
			_ = r
			return object.None, nil
		})
		m.Dict["getmembers"] = bi("getmembers", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.List{Items: []object.Object{}}, nil
		})
		m.Dict["signature"] = bi("signature", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			cls := &object.TypeObj{Name: "Signature", Dict: map[string]object.Object{}}
			return &object.Instance{Class: cls, Dict: map[string]object.Object{}}, nil
		})
		m.Dict["getfullargspec"] = bi("getfullargspec", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			cls := &object.TypeObj{Name: "FullArgSpec", Dict: map[string]object.Object{}}
			inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
			inst.Dict["args"] = &object.List{Items: []object.Object{}}
			inst.Dict["varargs"] = object.None
			inst.Dict["varkw"] = object.None
			inst.Dict["defaults"] = object.None
			inst.Dict["kwonlyargs"] = &object.List{Items: []object.Object{}}
			inst.Dict["kwonlydefaults"] = object.None
			inst.Dict["annotations"] = &object.Dict{}
			return inst, nil
		})
		m.Dict["currentframe"] = bi("currentframe", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		m.Dict["stack"] = bi("stack", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return &object.List{Items: []object.Object{}}, nil
		})
		m.Dict["getsource"] = bi("getsource", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			ip.raise(ip.excType("OSError"), "source not available")
			return object.None, nil
		})
		m.Dict["getfile"] = m.Dict["getsource"]
		m.Dict["cleandoc"] = bi("cleandoc", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				return &object.Str{V: ""}, nil
			}
			lines := strings.Split(a[0].Str(), "\n")
			for i, ln := range lines {
				lines[i] = strings.TrimSpace(ln)
			}
			return &object.Str{V: strings.Join(lines, "\n")}, nil
		})
		m.Dict["Parameter"] = &object.TypeObj{Name: "Parameter", Dict: map[string]object.Object{}}
		m.Dict["Signature"] = &object.TypeObj{Name: "Signature", Dict: map[string]object.Object{}}
		m.Dict["BoundArguments"] = &object.TypeObj{Name: "BoundArguments", Dict: map[string]object.Object{}}
		m.Dict["_empty"] = &object.Instance{Class: &object.TypeObj{Name: "_empty", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{}}
		m.Dict["CO_VARARGS"] = &object.Int{V: 4}
		m.Dict["CO_VARKEYWORDS"] = &object.Int{V: 8}
		m.Dict["CO_GENERATOR"] = &object.Int{V: 32}
		_ = yes
		return m
	}
}
