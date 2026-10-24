package runtime

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"zuzo/py/object"
)

// registerMethods 注册内置类型方法
func registerMethods(ip *Interp) {
	// ---------------- str ----------------
	regMethod(object.StrType, "upper", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: strings.ToUpper(selfStr(args).V)}, nil
	})
	regMethod(object.StrType, "lower", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: strings.ToLower(selfStr(args).V)}, nil
	})
	regMethod(object.StrType, "casefold", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: strings.ToLower(selfStr(args).V)}, nil
	})
	regMethod(object.StrType, "strip", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		chars := " \t\n\r\v\f"
		if len(args) > 1 {
			chars = args[1].Str()
		}
		return &object.Str{V: strings.Trim(s, chars)}, nil
	})
	regMethod(object.StrType, "lstrip", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		chars := " \t\n\r\v\f"
		if len(args) > 1 {
			chars = args[1].Str()
		}
		return &object.Str{V: strings.TrimLeft(s, chars)}, nil
	})
	regMethod(object.StrType, "rstrip", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		chars := " \t\n\r\v\f"
		if len(args) > 1 {
			chars = args[1].Str()
		}
		return &object.Str{V: strings.TrimRight(s, chars)}, nil
	})
	regMethod(object.StrType, "split", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		var parts []string
		if len(args) <= 1 || args[1] == object.None {
			parts = strings.Fields(s)
		} else {
			sep := args[1].Str()
			if sep == "" {
				ip.ValueError("empty separator")
			}
			parts = strings.Split(s, sep)
			if len(args) > 2 {
				if n, ok := object.AsInt(args[2]); ok && n >= 0 && int(n) < len(parts) {
					head := parts[:n]
					parts = append(head, strings.Join(parts[n:], sep))
				}
			}
		}
		items := make([]object.Object, len(parts))
		for i, p := range parts {
			items[i] = &object.Str{V: p}
		}
		return &object.List{Items: items}, nil
	})
	regMethod(object.StrType, "rsplit", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		var parts []string
		if len(args) <= 1 || args[1] == object.None {
			parts = strings.Fields(s)
		} else {
			parts = strings.Split(s, args[1].Str())
		}
		items := make([]object.Object, len(parts))
		for i, p := range parts {
			items[i] = &object.Str{V: p}
		}
		return &object.List{Items: items}, nil
	})
	regMethod(object.StrType, "splitlines", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		s = strings.ReplaceAll(s, "\r\n", "\n")
		s = strings.ReplaceAll(s, "\r", "\n")
		parts := strings.Split(s, "\n")
		if len(parts) > 0 && parts[len(parts)-1] == "" {
			parts = parts[:len(parts)-1]
		}
		items := make([]object.Object, len(parts))
		for i, p := range parts {
			items[i] = &object.Str{V: p}
		}
		return &object.List{Items: items}, nil
	})
	regMethod(object.StrType, "join", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		sep := selfStr(args).V
		if len(args) < 2 {
			ip.TypeError("join() takes exactly one argument")
		}
		var parts []string
		for _, v := range ip.ToSlice(args[1]) {
			if s, ok := v.(*object.Str); ok {
				parts = append(parts, s.V)
			} else {
				ip.TypeError("sequence item: expected str instance, %s found", object.TypeName(v))
			}
		}
		return &object.Str{V: strings.Join(parts, sep)}, nil
	})
	regMethod(object.StrType, "replace", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		if len(args) < 3 {
			ip.TypeError("replace() takes at least 2 arguments")
		}
		old := args[1].Str()
		new := args[2].Str()
		if len(args) > 3 {
			n, _ := object.AsInt(args[3])
			return &object.Str{V: strings.Replace(s, old, new, int(n))}, nil
		}
		return &object.Str{V: strings.ReplaceAll(s, old, new)}, nil
	})
	regMethod(object.StrType, "find", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		sub := args[1].Str()
		start, end := 0, len(s)
		if len(args) > 2 {
			v, _ := object.AsInt(args[2])
			start = int(v)
		}
		if len(args) > 3 {
			v, _ := object.AsInt(args[3])
			end = int(v)
		}
		rs := []rune(s)
		if start < 0 {
			start += len(rs)
		}
		if end < 0 {
			end += len(rs)
		}
		if start < 0 {
			start = 0
		}
		if end > len(rs) {
			end = len(rs)
		}
		if start > end {
			return &object.Int{V: -1}, nil
		}
		sub2 := string(rs[start:end])
		idx := strings.Index(sub2, sub)
		if idx < 0 {
			return &object.Int{V: -1}, nil
		}
		return &object.Int{V: int64(start + len([]rune(sub2[:idx])))}, nil
	})
	regMethod(object.StrType, "rfind", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		sub := args[1].Str()
		idx := strings.LastIndex(s, sub)
		if idx < 0 {
			return &object.Int{V: -1}, nil
		}
		return &object.Int{V: int64(len([]rune(s[:idx])))}, nil
	})
	regMethod(object.StrType, "index", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		r, err := object.StrType.Dict["find"].(*object.Builtin).Fn(args, kw)
		if err != nil {
			return nil, err
		}
		if n, _ := object.AsInt(r); n < 0 {
			ip.ValueError("substring not found")
		}
		return r, nil
	})
	regMethod(object.StrType, "count", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		sub := args[1].Str()
		if sub == "" {
			return &object.Int{V: int64(len([]rune(s)) + 1)}, nil
		}
		return &object.Int{V: int64(strings.Count(s, sub))}, nil
	})
	regMethod(object.StrType, "startswith", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		if len(args) < 2 {
			ip.TypeError("startswith() takes at least 1 argument")
		}
		var prefixes []string
		if t, ok := args[1].(*object.Tuple); ok {
			for _, it := range t.Items {
				prefixes = append(prefixes, it.Str())
			}
		} else {
			prefixes = []string{args[1].Str()}
		}
		for _, p := range prefixes {
			if strings.HasPrefix(s, p) {
				return object.TrueObj, nil
			}
		}
		return object.FalseObj, nil
	})
	regMethod(object.StrType, "endswith", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		if len(args) < 2 {
			ip.TypeError("endswith() takes at least 1 argument")
		}
		var suffixes []string
		if t, ok := args[1].(*object.Tuple); ok {
			for _, it := range t.Items {
				suffixes = append(suffixes, it.Str())
			}
		} else {
			suffixes = []string{args[1].Str()}
		}
		for _, p := range suffixes {
			if strings.HasSuffix(s, p) {
				return object.TrueObj, nil
			}
		}
		return object.FalseObj, nil
	})
	regMethod(object.StrType, "isdigit", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		if s == "" {
			return object.FalseObj, nil
		}
		for _, r := range s {
			if !unicode.IsDigit(r) {
				return object.FalseObj, nil
			}
		}
		return object.TrueObj, nil
	})
	regMethod(object.StrType, "isalpha", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		if s == "" {
			return object.FalseObj, nil
		}
		for _, r := range s {
			if !unicode.IsLetter(r) {
				return object.FalseObj, nil
			}
		}
		return object.TrueObj, nil
	})
	regMethod(object.StrType, "isalnum", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		if s == "" {
			return object.FalseObj, nil
		}
		for _, r := range s {
			if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
				return object.FalseObj, nil
			}
		}
		return object.TrueObj, nil
	})
	regMethod(object.StrType, "isspace", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		if s == "" {
			return object.FalseObj, nil
		}
		for _, r := range s {
			if !unicode.IsSpace(r) {
				return object.FalseObj, nil
			}
		}
		return object.TrueObj, nil
	})
	regMethod(object.StrType, "isnumeric", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return object.StrType.Dict["isdigit"].(*object.Builtin).Fn(args, kw)
	})
	regMethod(object.StrType, "isupper", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		has := false
		for _, r := range s {
			if unicode.IsLower(r) {
				return object.FalseObj, nil
			}
			if unicode.IsUpper(r) {
				has = true
			}
		}
		return object.GetBool(has), nil
	})
	regMethod(object.StrType, "islower", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		has := false
		for _, r := range s {
			if unicode.IsUpper(r) {
				return object.FalseObj, nil
			}
			if unicode.IsLower(r) {
				has = true
			}
		}
		return object.GetBool(has), nil
	})
	regMethod(object.StrType, "capitalize", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		rs := []rune(strings.ToLower(s))
		if len(rs) > 0 {
			rs[0] = unicode.ToUpper(rs[0])
		}
		return &object.Str{V: string(rs)}, nil
	})
	regMethod(object.StrType, "title", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		var sb strings.Builder
		prevLetter := false
		for _, r := range s {
			if unicode.IsLetter(r) {
				if !prevLetter {
					sb.WriteRune(unicode.ToUpper(r))
				} else {
					sb.WriteRune(unicode.ToLower(r))
				}
				prevLetter = true
			} else {
				sb.WriteRune(r)
				prevLetter = false
			}
		}
		return &object.Str{V: sb.String()}, nil
	})
	regMethod(object.StrType, "swapcase", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		var sb strings.Builder
		for _, r := range s {
			if unicode.IsUpper(r) {
				sb.WriteRune(unicode.ToLower(r))
			} else if unicode.IsLower(r) {
				sb.WriteRune(unicode.ToUpper(r))
			} else {
				sb.WriteRune(r)
			}
		}
		return &object.Str{V: sb.String()}, nil
	})
	regMethod(object.StrType, "center", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		w, _ := object.AsInt(args[1])
		fill := " "
		if len(args) > 2 {
			fill = args[2].Str()
		}
		n := len([]rune(s))
		if int(w) <= n {
			return &object.Str{V: s}, nil
		}
		pad := int(w) - n
		l := pad / 2
		return &object.Str{V: strings.Repeat(fill, l) + s + strings.Repeat(fill, pad-l)}, nil
	})
	regMethod(object.StrType, "ljust", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		w, _ := object.AsInt(args[1])
		fill := " "
		if len(args) > 2 {
			fill = args[2].Str()
		}
		n := len([]rune(s))
		if int(w) <= n {
			return &object.Str{V: s}, nil
		}
		return &object.Str{V: s + strings.Repeat(fill, int(w)-n)}, nil
	})
	regMethod(object.StrType, "rjust", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		w, _ := object.AsInt(args[1])
		fill := " "
		if len(args) > 2 {
			fill = args[2].Str()
		}
		n := len([]rune(s))
		if int(w) <= n {
			return &object.Str{V: s}, nil
		}
		return &object.Str{V: strings.Repeat(fill, int(w)-n) + s}, nil
	})
	regMethod(object.StrType, "zfill", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		w, _ := object.AsInt(args[1])
		n := len([]rune(s))
		if int(w) <= n {
			return &object.Str{V: s}, nil
		}
		sign := ""
		if strings.HasPrefix(s, "-") || strings.HasPrefix(s, "+") {
			sign = s[:1]
			s = s[1:]
			n--
		}
		return &object.Str{V: sign + strings.Repeat("0", int(w)-n) + s}, nil
	})
	regMethod(object.StrType, "encode", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Bytes{V: []byte(selfStr(args).V)}, nil
	})
	regMethod(object.StrType, "format", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		rest := args[1:]
		return &object.Str{V: ip.strFormat(s, rest, kw)}, nil
	})
	regMethod(object.StrType, "partition", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		sep := args[1].Str()
		i := strings.Index(s, sep)
		if i < 0 {
			return &object.Tuple{Items: []object.Object{&object.Str{V: s}, &object.Str{V: ""}, &object.Str{V: ""}}}, nil
		}
		return &object.Tuple{Items: []object.Object{
			&object.Str{V: s[:i]}, &object.Str{V: sep}, &object.Str{V: s[i+len(sep):]},
		}}, nil
	})
	regMethod(object.StrType, "rpartition", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		sep := args[1].Str()
		i := strings.LastIndex(s, sep)
		if i < 0 {
			return &object.Tuple{Items: []object.Object{&object.Str{V: ""}, &object.Str{V: ""}, &object.Str{V: s}}}, nil
		}
		return &object.Tuple{Items: []object.Object{
			&object.Str{V: s[:i]}, &object.Str{V: sep}, &object.Str{V: s[i+len(sep):]},
		}}, nil
	})
	regMethod(object.StrType, "removeprefix", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		p := args[1].Str()
		return &object.Str{V: strings.TrimPrefix(s, p)}, nil
	})
	regMethod(object.StrType, "removesuffix", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		p := args[1].Str()
		return &object.Str{V: strings.TrimSuffix(s, p)}, nil
	})
	regMethod(object.StrType, "expandtabs", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfStr(args).V
		size := 8
		if len(args) > 1 {
			n, _ := object.AsInt(args[1])
			size = int(n)
		}
		var sb strings.Builder
		col := 0
		for _, r := range s {
			if r == '\t' {
				n := size - col%size
				sb.WriteString(strings.Repeat(" ", n))
				col += n
			} else if r == '\n' {
				sb.WriteRune(r)
				col = 0
			} else {
				sb.WriteRune(r)
				col++
			}
		}
		return &object.Str{V: sb.String()}, nil
	})
	regMethod(object.StrType, "__str__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: selfStr(args).V}, nil
	})
	regMethod(object.StrType, "__repr__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: object.ReprString(selfStr(args).V)}, nil
	})

	// ---------------- list ----------------
	regMethod(object.ListType, "append", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		l.Items = append(l.Items, args[1])
		return object.None, nil
	})
	regMethod(object.ListType, "extend", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		l.Items = append(l.Items, ip.ToSlice(args[1])...)
		return object.None, nil
	})
	regMethod(object.ListType, "insert", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		idx, _ := object.AsInt(args[1])
		i := int(idx)
		if i < 0 {
			i += len(l.Items)
		}
		if i < 0 {
			i = 0
		}
		if i > len(l.Items) {
			i = len(l.Items)
		}
		l.Items = append(l.Items[:i], append([]object.Object{args[2]}, l.Items[i:]...)...)
		return object.None, nil
	})
	regMethod(object.ListType, "remove", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		for i, v := range l.Items {
			if ip.Eq(v, args[1]) {
				l.Items = append(l.Items[:i], l.Items[i+1:]...)
				return object.None, nil
			}
		}
		ip.ValueError("list.remove(x): x not in list")
		return object.None, nil
	})
	regMethod(object.ListType, "pop", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		if len(l.Items) == 0 {
			ip.IndexErr("pop from empty list")
		}
		i := len(l.Items) - 1
		if len(args) > 1 {
			v, _ := object.AsInt(args[1])
			i = int(v)
			if i < 0 {
				i += len(l.Items)
			}
		}
		if i < 0 || i >= len(l.Items) {
			ip.IndexErr("pop index out of range")
		}
		v := l.Items[i]
		l.Items = append(l.Items[:i], l.Items[i+1:]...)
		return v, nil
	})
	regMethod(object.ListType, "clear", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		l.Items = nil
		return object.None, nil
	})
	regMethod(object.ListType, "index", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		for i, v := range l.Items {
			if ip.Eq(v, args[1]) {
				return &object.Int{V: int64(i)}, nil
			}
		}
		ip.ValueError("%s is not in list", ip.Repr(args[1]))
		return object.None, nil
	})
	regMethod(object.ListType, "count", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		n := 0
		for _, v := range l.Items {
			if ip.Eq(v, args[1]) {
				n++
			}
		}
		return &object.Int{V: int64(n)}, nil
	})
	regMethod(object.ListType, "sort", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		var keyFn object.Object
		reverse := false
		if len(args) > 1 {
			//
		}
		if kw != nil {
			if v, ok := kw["key"]; ok && v != object.None {
				keyFn = v
			}
			if v, ok := kw["reverse"]; ok {
				reverse = object.Truthy(v)
			}
		}
		ip.sortObjects(l.Items, keyFn, reverse)
		return object.None, nil
	})
	regMethod(object.ListType, "reverse", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		for i, j := 0, len(l.Items)-1; i < j; i, j = i+1, j-1 {
			l.Items[i], l.Items[j] = l.Items[j], l.Items[i]
		}
		return object.None, nil
	})
	regMethod(object.ListType, "copy", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := selfList(args)
		return &object.List{Items: append([]object.Object{}, l.Items...)}, nil
	})

	// ---------------- tuple ----------------
	regMethod(object.TupleType, "count", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		t := args[0].(*object.Tuple)
		n := 0
		for _, v := range t.Items {
			if ip.Eq(v, args[1]) {
				n++
			}
		}
		return &object.Int{V: int64(n)}, nil
	})
	regMethod(object.TupleType, "index", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		t := args[0].(*object.Tuple)
		for i, v := range t.Items {
			if ip.Eq(v, args[1]) {
				return &object.Int{V: int64(i)}, nil
			}
		}
		ip.ValueError("tuple.index(x): x not in tuple")
		return object.None, nil
	})

	// ---------------- dict ----------------
	regMethod(object.DictType, "get", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := selfDict(args)
		if v, ok := d.Get(args[1]); ok {
			return v, nil
		}
		if len(args) > 2 {
			return args[2], nil
		}
		return object.None, nil
	})
	regMethod(object.DictType, "keys", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := selfDict(args)
		return &object.List{Items: append([]object.Object{}, d.Keys...)}, nil
	})
	regMethod(object.DictType, "values", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := selfDict(args)
		return &object.List{Items: append([]object.Object{}, d.Vals...)}, nil
	})
	regMethod(object.DictType, "items", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := selfDict(args)
		out := make([]object.Object, len(d.Keys))
		for i := range d.Keys {
			out[i] = &object.Tuple{Items: []object.Object{d.Keys[i], d.Vals[i]}}
		}
		return &object.List{Items: out}, nil
	})
	regMethod(object.DictType, "update", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := selfDict(args)
		if len(args) > 1 {
			if src, ok := args[1].(*object.Dict); ok {
				for i, k := range src.Keys {
					d.Set(k, src.Vals[i])
				}
			} else {
				for _, pair := range ip.ToSlice(args[1]) {
					kv := ip.ToSlice(pair)
					if len(kv) == 2 {
						d.Set(kv[0], kv[1])
					}
				}
			}
		}
		for k, v := range kw {
			d.Set(&object.Str{V: k}, v)
		}
		return object.None, nil
	})
	regMethod(object.DictType, "pop", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := selfDict(args)
		if v, ok := d.Get(args[1]); ok {
			d.Delete(args[1])
			return v, nil
		}
		if len(args) > 2 {
			return args[2], nil
		}
		ip.KeyErr(args[1])
		return object.None, nil
	})
	regMethod(object.DictType, "popitem", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := selfDict(args)
		if len(d.Keys) == 0 {
			ip.KeyErr(&object.Str{V: "popitem(): dictionary is empty"})
		}
		k := d.Keys[len(d.Keys)-1]
		v := d.Vals[len(d.Vals)-1]
		d.Delete(k)
		return &object.Tuple{Items: []object.Object{k, v}}, nil
	})
	regMethod(object.DictType, "setdefault", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := selfDict(args)
		if v, ok := d.Get(args[1]); ok {
			return v, nil
		}
		var def object.Object = object.None
		if len(args) > 2 {
			def = args[2]
		}
		d.Set(args[1], def)
		return def, nil
	})
	regMethod(object.DictType, "clear", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := selfDict(args)
		d.Keys = nil
		d.Vals = nil
		return object.None, nil
	})
	regMethod(object.DictType, "copy", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return selfDict(args).Copy(), nil
	})
	regMethod(object.DictType, "fromkeys", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d := &object.Dict{}
		var def object.Object = object.None
		if len(args) > 1 {
			def = args[1]
		}
		if len(args) > 0 {
			for _, k := range ip.ToSlice(args[0]) {
				d.Set(k, def)
			}
		}
		return d, nil
	})

	// ---------------- set ----------------
	regMethod(object.SetType, "add", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		s.Add(args[1])
		return object.None, nil
	})
	regMethod(object.SetType, "remove", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		if !s.Remove(args[1]) {
			ip.KeyErr(args[1])
		}
		return object.None, nil
	})
	regMethod(object.SetType, "discard", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		s.Remove(args[1])
		return object.None, nil
	})
	regMethod(object.SetType, "pop", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		if len(s.Items) == 0 {
			ip.KeyErr(&object.Str{V: "pop from an empty set"})
		}
		v := s.Items[0]
		s.Remove(v)
		return v, nil
	})
	regMethod(object.SetType, "clear", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		s.Items = nil
		return object.None, nil
	})
	regMethod(object.SetType, "union", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		out := &object.Set{}
		for _, v := range s.Items {
			out.Add(v)
		}
		for _, a := range args[1:] {
			for _, v := range ip.ToSlice(a) {
				out.Add(v)
			}
		}
		return out, nil
	})
	regMethod(object.SetType, "intersection", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		out := &object.Set{}
		other := &object.Set{}
		for _, a := range args[1:] {
			for _, v := range ip.ToSlice(a) {
				other.Add(v)
			}
		}
		for _, v := range s.Items {
			if other.Contains(v) {
				out.Add(v)
			}
		}
		return out, nil
	})
	regMethod(object.SetType, "difference", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		other := &object.Set{}
		for _, a := range args[1:] {
			for _, v := range ip.ToSlice(a) {
				other.Add(v)
			}
		}
		out := &object.Set{}
		for _, v := range s.Items {
			if !other.Contains(v) {
				out.Add(v)
			}
		}
		return out, nil
	})
	regMethod(object.SetType, "symmetric_difference", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		other := &object.Set{}
		for _, v := range ip.ToSlice(args[1]) {
			other.Add(v)
		}
		out := &object.Set{}
		for _, v := range s.Items {
			if !other.Contains(v) {
				out.Add(v)
			}
		}
		for _, v := range other.Items {
			if !s.Contains(v) {
				out.Add(v)
			}
		}
		return out, nil
	})
	regMethod(object.SetType, "update", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		for _, a := range args[1:] {
			for _, v := range ip.ToSlice(a) {
				s.Add(v)
			}
		}
		return object.None, nil
	})
	regMethod(object.SetType, "issubset", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		other := &object.Set{}
		for _, v := range ip.ToSlice(args[1]) {
			other.Add(v)
		}
		for _, v := range s.Items {
			if !other.Contains(v) {
				return object.FalseObj, nil
			}
		}
		return object.TrueObj, nil
	})
	regMethod(object.SetType, "issuperset", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		for _, v := range ip.ToSlice(args[1]) {
			if !s.Contains(v) {
				return object.FalseObj, nil
			}
		}
		return object.TrueObj, nil
	})
	regMethod(object.SetType, "isdisjoint", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		for _, v := range ip.ToSlice(args[1]) {
			if s.Contains(v) {
				return object.FalseObj, nil
			}
		}
		return object.TrueObj, nil
	})
	regMethod(object.SetType, "copy", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := selfSet(args)
		out := &object.Set{}
		for _, v := range s.Items {
			out.Add(v)
		}
		return out, nil
	})

	// ---------------- property / generator ----------------
	regMethod(object.PropertyType, "setter", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p := args[0].(*object.Property)
		cp := *p
		cp.Fset = args[1]
		return &cp, nil
	})
	regMethod(object.PropertyType, "getter", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p := args[0].(*object.Property)
		cp := *p
		cp.Fget = args[1]
		return &cp, nil
	})
	regMethod(object.PropertyType, "deleter", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p := args[0].(*object.Property)
		cp := *p
		cp.Fdel = args[1]
		return &cp, nil
	})
	regMethod(object.GenType, "send", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		g := args[0].(*object.Generator)
		v := object.None
		if len(args) > 1 {
			v = args[1]
		}
		val, done, err := g.Send(v)
		if err != nil {
			if pe, ok := err.(*PyError); ok {
				panic(pe)
			}
			ip.RuntimeErr("%v", err)
		}
		if done {
			exc := object.NewException(ip.excType("StopIteration"))
			if g.RetVal != nil {
				exc.Args = []object.Object{g.RetVal}
			}
			ip.RaiseObj(exc)
		}
		return val, nil
	})
	regMethod(object.GenType, "close", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if g, ok := args[0].(*object.Generator); ok && g.Close != nil {
			g.Close()
		}
		return object.None, nil
	})
	regMethod(object.GenType, "__next__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		g := args[0].(*object.Generator)
		val, done, err := g.Next()
		if err != nil {
			if pe, ok := err.(*PyError); ok {
				panic(pe)
			}
			ip.RuntimeErr("%v", err)
		}
		if done {
			ip.raise(ip.excType("StopIteration"), "")
		}
		return val, nil
	})
	regMethod(object.GenType, "__iter__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return args[0], nil
	})

	// ---------------- int / float ----------------
	regMethod(object.IntType, "bit_length", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		n, _ := object.AsInt(args[0])
		if n < 0 {
			n = -n
		}
		c := 0
		for n > 0 {
			c++
			n >>= 1
		}
		return &object.Int{V: int64(c)}, nil
	})
	regMethod(object.IntType, "is_integer", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return object.TrueObj, nil
	})
	regMethod(object.FloatType, "is_integer", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		f, _ := object.AsFloat(args[0])
		return object.GetBool(f == math.Trunc(f)), nil
	})
	regMethod(object.FloatType, "hex", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		f, _ := object.AsFloat(args[0])
		return &object.Str{V: fmt.Sprintf("%x", math.Float64bits(f))}, nil
	})

	// ---------------- bytes ----------------
	regMethod(object.BytesType, "decode", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		b := args[0].(*object.Bytes)
		return &object.Str{V: string(b.V)}, nil
	})
	regMethod(object.BytesType, "hex", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		b := args[0].(*object.Bytes)
		return &object.Str{V: fmt.Sprintf("%x", b.V)}, nil
	})
	regMethod(object.BytesType, "split", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		b := args[0].(*object.Bytes)
		sep := " "
		if len(args) > 1 {
			sep = args[1].Str()
		}
		parts := strings.Split(string(b.V), sep)
		items := make([]object.Object, len(parts))
		for i, p := range parts {
			items[i] = &object.Bytes{V: []byte(p)}
		}
		return &object.List{Items: items}, nil
	})
}

func selfStr(args []object.Object) *object.Str {
	if len(args) > 0 {
		if s, ok := args[0].(*object.Str); ok {
			return s
		}
	}
	return &object.Str{}
}

func selfList(args []object.Object) *object.List {
	if len(args) > 0 {
		if l, ok := args[0].(*object.List); ok {
			return l
		}
	}
	return &object.List{}
}

func selfDict(args []object.Object) *object.Dict {
	if len(args) > 0 {
		if d, ok := args[0].(*object.Dict); ok {
			return d
		}
	}
	return &object.Dict{}
}

func selfSet(args []object.Object) *object.Set {
	if len(args) > 0 {
		if s, ok := args[0].(*object.Set); ok {
			return s
		}
	}
	return &object.Set{}
}

// strFormat 实现 str.format
func (ip *Interp) strFormat(s string, args []object.Object, kw map[string]object.Object) string {
	var sb strings.Builder
	rs := []rune(s)
	auto := 0
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c == '{' {
			if i+1 < len(rs) && rs[i+1] == '{' {
				sb.WriteRune('{')
				i++
				continue
			}
			j := i + 1
			depth := 1
			for j < len(rs) && depth > 0 {
				if rs[j] == '{' {
					depth++
				} else if rs[j] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
				j++
			}
			if j >= len(rs) {
				sb.WriteString(string(rs[i:]))
				break
			}
			inner := string(rs[i+1 : j])
			// 拆 : spec
			name := inner
			spec := ""
			if k := strings.Index(inner, ":"); k >= 0 {
				name = inner[:k]
				spec = inner[k+1:]
			}
			// 拆 .attr / [idx]
			name = strings.TrimSpace(name)
			var v object.Object
			if name == "" {
				if auto < len(args) {
					v = args[auto]
					auto++
				}
			} else if n, err := strconv.Atoi(name); err == nil {
				if n < len(args) {
					v = args[n]
				}
			} else if kw != nil {
				if val, ok := kw[name]; ok {
					v = val
				}
			}
			if v == nil && args != nil {
				// 尝试按名字取
				for _, a := range args {
					_ = a
				}
			}
			if v == nil {
				sb.WriteString("{" + inner + "}")
			} else if spec == "" {
				sb.WriteString(ip.StrOf(v))
			} else {
				sb.WriteString(ip.FormatValue(v, spec))
			}
			i = j
			continue
		}
		if c == '}' {
			if i+1 < len(rs) && rs[i+1] == '}' {
				sb.WriteRune('}')
				i++
				continue
			}
			sb.WriteRune('}')
			continue
		}
		sb.WriteRune(c)
	}
	return sb.String()
}

var _ = sort.Strings
