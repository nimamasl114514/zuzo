package runtime

// stdlib_patch.go — 标准库补丁（第13轮）
//
// 有些内置模块首版只做了最小集，第三方库却依赖完整 API。这里在
// registerBuiltinModules 之后统一「外包一层」补 API，避免改动既有实现文件。
//
// 已补：re 的 flags 常量 · urllib.parse 的 urlsplit/urljoin/parse_qs · _codecs.lookup_error

import (
	"net/url"
	"strconv"
	"strings"

	"zuzo/py/object"
)

// registerStdlibPatch 在基础模块全部注册后调用（New() 内，顺序敏感）。
func registerStdlibPatch(ip *Interp) {
	patchBuiltinModule("re", func(ip *Interp, m *object.Module) {
		// CPython re 的 flag 位值
		flags := map[string]int64{
			"A": 256, "ASCII": 256, "I": 2, "IGNORECASE": 2, "L": 4, "LOCALE": 4,
			"M": 8, "MULTILINE": 8, "S": 16, "DOTALL": 16, "X": 64, "VERBOSE": 64,
			"U": 32, "UNICODE": 32, "DEBUG": 128, "T": 1, "TEMPLATE": 1,
			"NOFLAG": 0,
		}
		for k, v := range flags {
			m.Dict[k] = &object.Int{V: v}
		}
	})
	patchBuiltinModule("urllib.parse", func(ip *Interp, m *object.Module) {
		m.Dict["urlsplit"] = bi("urlsplit", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				ip.TypeError("urlsplit() requires a string")
			}
			return ip.urlSplitResult(a[0].Str(), false), nil
		})
		m.Dict["urlparse"] = bi("urlparse", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				ip.TypeError("urlparse() requires a string")
			}
			return ip.urlSplitResult(a[0].Str(), true), nil
		})
		m.Dict["urlunsplit"] = bi("urlunsplit", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			parts := ip.ToSlice(a[0])
			get := func(i int) string {
				if i < len(parts) {
					return parts[i].Str()
				}
				return ""
			}
			return &object.Str{V: joinURL(get(0), get(1), get(2), get(3), get(4))}, nil
		})
		m.Dict["urljoin"] = bi("urljoin", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			base, ref := a[0].Str(), a[1].Str()
			bu, err := url.Parse(base)
			if err != nil {
				return &object.Str{V: ref}, nil
			}
			ru, err := url.Parse(ref)
			if err != nil {
				return &object.Str{V: ref}, nil
			}
			return &object.Str{V: bu.ResolveReference(ru).String()}, nil
		})
		m.Dict["parse_qs"] = bi("parse_qs", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			qs := a[0].Str()
			keepBlank := false
			if len(a) > 1 {
				keepBlank = object.Truthy(a[1])
			}
			out := &object.Dict{}
			for _, kv := range parseQuery(qs) {
				if kv[1] == "" && !keepBlank {
					continue
				}
				key := &object.Str{V: kv[0]}
				val := &object.Str{V: kv[1]}
				if prev, ok := out.Get(key); ok {
					if lst, ok := prev.(*object.List); ok {
						lst.Items = append(lst.Items, val)
						continue
					}
				}
				out.Set(key, &object.List{Items: []object.Object{val}})
			}
			return out, nil
		})
		m.Dict["parse_qsl"] = bi("parse_qsl", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			var items []object.Object
			for _, kv := range parseQuery(a[0].Str()) {
				items = append(items, &object.Tuple{Items: []object.Object{
					&object.Str{V: kv[0]}, &object.Str{V: kv[1]},
				}})
			}
			return &object.List{Items: items}, nil
		})
		m.Dict["unquote_plus"] = bi("unquote_plus", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			s := strings.ReplaceAll(a[0].Str(), "+", " ")
			return &object.Str{V: unquoteStr(s)}, nil
		})
	})
	patchBuiltinModule("_codecs", func(ip *Interp, m *object.Module) {
		m.Dict["lookup_error"] = ip.excType("LookupError")
		// codecs.register(search_function)：CPython 的 encodings 包会注册大量搜索函数，
		// zuzo 用内置编码表，这里接受注册但不参与查找（保证 import 链不断）。
		m.Dict["register"] = bi("register", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		m.Dict["unregister"] = m.Dict["register"]
	})
}

// patchBuiltinModule 把模块工厂包一层（原工厂 → 追加 API）。
func patchBuiltinModule(name string, fn func(*Interp, *object.Module)) {
	prev, ok := builtinModuleInits[name]
	if !ok {
		return
	}
	builtinModuleInits[name] = func(ip *Interp) *object.Module {
		m := prev(ip)
		fn(ip, m)
		return m
	}
}

// ==================== URL 分解 ====================

var splitResultType = &object.TypeObj{Name: "SplitResult", Dict: map[string]object.Object{}}

func (ip *Interp) urlSplitResult(raw string, withParams bool) *object.Instance {
	u, err := url.Parse(raw)
	if err != nil {
		// 解析失败时退化为「整体当 path」
		inst := &object.Instance{Class: splitResultType, Dict: map[string]object.Object{}}
		inst.Dict["scheme"] = &object.Str{V: ""}
		inst.Dict["netloc"] = &object.Str{V: ""}
		inst.Dict["path"] = &object.Str{V: raw}
		inst.Dict["query"] = &object.Str{V: ""}
		inst.Dict["fragment"] = &object.Str{V: ""}
		ip.fillURLDerived(inst)
		return inst
	}
	scheme := u.Scheme
	netloc := u.Host
	if u.User != nil {
		userinfo := u.User.Username()
		if pw, ok := u.User.Password(); ok {
			userinfo += ":" + pw
		}
		netloc = userinfo + "@" + netloc
	}
	path := u.Path
	if u.RawPath != "" {
		path = u.RawPath
	}
	inst := &object.Instance{Class: splitResultType, Dict: map[string]object.Object{}}
	inst.Dict["scheme"] = &object.Str{V: scheme}
	inst.Dict["netloc"] = &object.Str{V: netloc}
	if withParams {
		inst.Dict["params"] = &object.Str{V: ""}
	}
	inst.Dict["path"] = &object.Str{V: path}
	inst.Dict["query"] = &object.Str{V: u.RawQuery}
	inst.Dict["fragment"] = &object.Str{V: u.Fragment}
	ip.fillURLDerived(inst)
	return inst
}

// fillURLDerived 补 hostname/port/username/password + _replace + geturl。
func (ip *Interp) fillURLDerived(inst *object.Instance) {
	netloc := ""
	if v, ok := inst.Dict["netloc"].(*object.Str); ok {
		netloc = v.V
	}
	user, pass, host, port := splitNetloc(netloc)
	inst.Dict["username"] = strOrNone(user)
	inst.Dict["password"] = strOrNone(pass)
	inst.Dict["hostname"] = strOrNone(host)
	if port != "" {
		if n, err := strconv.ParseInt(port, 10, 64); err == nil {
			inst.Dict["port"] = &object.Int{V: n}
		} else {
			inst.Dict["port"] = object.None
		}
	} else {
		inst.Dict["port"] = object.None
	}
	inst.Dict["_replace"] = bi("_replace", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		cp := &object.Instance{Class: inst.Class, Dict: map[string]object.Object{}}
		for kk, vv := range inst.Dict {
			cp.Dict[kk] = vv
		}
		for kk, vv := range k {
			cp.Dict[kk] = vv
		}
		return cp, nil
	})
	inst.Dict["geturl"] = bi("geturl", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		get := func(name string) string {
			if v, ok := inst.Dict[name].(*object.Str); ok {
				return v.V
			}
			return ""
		}
		return &object.Str{V: joinURL(get("scheme"), get("netloc"), get("path"), get("query"), get("fragment"))}, nil
	})
	inst.Dict["__iter__"] = bi("__iter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		items := []object.Object{}
		for _, name := range []string{"scheme", "netloc", "path", "query", "fragment"} {
			items = append(items, inst.Dict[name])
		}
		return &object.List{Items: items}, nil
	})
}

func joinURL(scheme, netloc, path, query, fragment string) string {
	var sb strings.Builder
	if scheme != "" {
		sb.WriteString(scheme)
		sb.WriteString("://")
	} else if netloc != "" {
		sb.WriteString("//")
	}
	sb.WriteString(netloc)
	sb.WriteString(path)
	if query != "" {
		sb.WriteString("?")
		sb.WriteString(query)
	}
	if fragment != "" {
		sb.WriteString("#")
		sb.WriteString(fragment)
	}
	return sb.String()
}

func splitNetloc(netloc string) (user, pass, host, port string) {
	rest := netloc
	if i := strings.LastIndex(rest, "@"); i >= 0 {
		userinfo := rest[:i]
		rest = rest[i+1:]
		if j := strings.Index(userinfo, ":"); j >= 0 {
			user, pass = userinfo[:j], userinfo[j+1:]
		} else {
			user = userinfo
		}
	}
	host = rest
	if strings.HasPrefix(rest, "[") {
		// IPv6
		if i := strings.LastIndex(rest, "]"); i >= 0 {
			host = rest[:i+1]
			if i+1 < len(rest) && rest[i+1] == ':' {
				port = rest[i+2:]
			}
		}
		return
	}
	if i := strings.LastIndex(rest, ":"); i >= 0 {
		host, port = rest[:i], rest[i+1:]
	}
	return
}

func strOrNone(s string) object.Object {
	if s == "" {
		return object.None
	}
	return &object.Str{V: s}
}

func parseQuery(qs string) [][2]string {
	var out [][2]string
	for _, pair := range strings.Split(qs, "&") {
		if pair == "" {
			continue
		}
		k, v := pair, ""
		if i := strings.Index(pair, "="); i >= 0 {
			k, v = pair[:i], pair[i+1:]
		}
		out = append(out, [2]string{unquoteStr(strings.ReplaceAll(k, "+", " ")), unquoteStr(strings.ReplaceAll(v, "+", " "))})
	}
	return out
}

func unquoteStr(s string) string {
	if !strings.Contains(s, "%") {
		return s
	}
	var sb strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '%' && i+2 < len(s) {
			if hi, ok := hexVal(s[i+1]); ok {
				if lo, ok2 := hexVal(s[i+2]); ok2 {
					sb.WriteByte(hi<<4 | lo)
					i += 2
					continue
				}
			}
		}
		sb.WriteByte(s[i])
	}
	return sb.String()
}