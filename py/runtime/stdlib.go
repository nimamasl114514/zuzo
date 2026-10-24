package runtime

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"zuzo/py/object"
)

func registerBuiltinModules(ip *Interp) {
	registerMethods(ip)
	builtinModuleInits["math"] = modMath
	builtinModuleInits["random"] = modRandom
	builtinModuleInits["time"] = modTime
	builtinModuleInits["json"] = modJSON
	builtinModuleInits["re"] = modRe
	builtinModuleInits["os"] = modOS
	builtinModuleInits["os.path"] = modOSPath
	builtinModuleInits["sys"] = modSys
	builtinModuleInits["string"] = modString
	builtinModuleInits["collections"] = modCollections
	builtinModuleInits["itertools"] = modItertools
	builtinModuleInits["functools"] = modFunctools
	builtinModuleInits["copy"] = modCopy
	builtinModuleInits["hashlib"] = modHashlib
	builtinModuleInits["base64"] = modBase64
	builtinModuleInits["datetime"] = modDatetime
	builtinModuleInits["typing"] = modTyping
	builtinModuleInits["dataclasses"] = modDataclasses
	builtinModuleInits["asyncio"] = modAsyncio
	registerStdlib2(ip)
	registerStdlib4(ip)
}

// newModule 创建模块
func newModule(name string) *object.Module {
	return &object.Module{Name: name, Dict: map[string]object.Object{}}
}

// bi 把 Go 函数包装为内置
func bi(name string, fn object.BuiltinFn) *object.Builtin {
	return &object.Builtin{Name: name, Fn: fn}
}

// fn 简化内置函数签名（固定参数）
func fn1(name string, f func(a object.Object) object.Object) *object.Builtin {
	return bi(name, func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) < 1 {
			return nil, &PyError{Exc: object.NewException(object.ExcType, &object.Str{V: name + "() missing argument"})}
		}
		return f(args[0]), nil
	})
}

// ==================== math ====================

func modMath(ip *Interp) *object.Module {
	m := newModule("math")
	m.Dict["pi"] = &object.Float{V: math.Pi}
	m.Dict["e"] = &object.Float{V: math.E}
	m.Dict["tau"] = &object.Float{V: math.Pi * 2}
	m.Dict["inf"] = &object.Float{V: math.Inf(1)}
	m.Dict["nan"] = &object.Float{V: math.NaN()}
	f1 := func(name string, f func(float64) float64) {
		m.Dict[name] = bi(name, func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
			v, ok := object.AsFloat(args[0])
			if !ok {
				ip.TypeError("must be real number, not %s", object.TypeName(args[0]))
			}
			r := f(v)
			if math.IsNaN(r) && !math.IsNaN(v) {
				ip.ValueError("math domain error")
			}
			return &object.Float{V: r}, nil
		})
	}
	f1("sqrt", math.Sqrt)
	f1("exp", math.Exp)
	f1("sin", math.Sin)
	f1("cos", math.Cos)
	f1("tan", math.Tan)
	f1("asin", math.Asin)
	f1("acos", math.Acos)
	f1("atan", math.Atan)
	f1("sinh", math.Sinh)
	f1("cosh", math.Cosh)
	f1("tanh", math.Tanh)
	f1("log", math.Log)
	f1("log2", math.Log2)
	f1("log10", math.Log10)
	f1("fabs", math.Abs)
	f1("radians", func(v float64) float64 { return v * math.Pi / 180 })
	f1("degrees", func(v float64) float64 { return v * 180 / math.Pi })
	m.Dict["floor"] = bi("floor", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		v, _ := object.AsFloat(args[0])
		return &object.Int{V: int64(math.Floor(v))}, nil
	})
	m.Dict["ceil"] = bi("ceil", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		v, _ := object.AsFloat(args[0])
		return &object.Int{V: int64(math.Ceil(v))}, nil
	})
	m.Dict["trunc"] = bi("trunc", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		v, _ := object.AsFloat(args[0])
		return &object.Int{V: int64(math.Trunc(v))}, nil
	})
	m.Dict["pow"] = bi("pow", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		a, _ := object.AsFloat(args[0])
		b, _ := object.AsFloat(args[1])
		return &object.Float{V: math.Pow(a, b)}, nil
	})
	m.Dict["atan2"] = bi("atan2", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		a, _ := object.AsFloat(args[0])
		b, _ := object.AsFloat(args[1])
		return &object.Float{V: math.Atan2(a, b)}, nil
	})
	m.Dict["hypot"] = bi("hypot", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		a, _ := object.AsFloat(args[0])
		b, _ := object.AsFloat(args[1])
		return &object.Float{V: math.Hypot(a, b)}, nil
	})
	m.Dict["factorial"] = bi("factorial", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		n, ok := object.AsInt(args[0])
		if !ok || n < 0 {
			ip.ValueError("factorial() not defined for negative values")
		}
		if n > 20 {
			r := big.NewInt(1)
			for i := int64(2); i <= n; i++ {
				r.Mul(r, big.NewInt(i))
			}
			return object.NewBigInt(r), nil
		}
		var r int64 = 1
		for i := int64(2); i <= n; i++ {
			r *= i
		}
		return &object.Int{V: r}, nil
	})
	m.Dict["gcd"] = bi("gcd", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		a, _ := object.AsInt(args[0])
		b, _ := object.AsInt(args[1])
		for b != 0 {
			a, b = b, a%b
		}
		if a < 0 {
			a = -a
		}
		return &object.Int{V: a}, nil
	})
	m.Dict["isnan"] = bi("isnan", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		v, _ := object.AsFloat(args[0])
		return object.GetBool(math.IsNaN(v)), nil
	})
	m.Dict["isinf"] = bi("isinf", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		v, _ := object.AsFloat(args[0])
		return object.GetBool(math.IsInf(v, 0)), nil
	})
	m.Dict["isfinite"] = bi("isfinite", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		v, _ := object.AsFloat(args[0])
		return object.GetBool(!math.IsInf(v, 0) && !math.IsNaN(v)), nil
	})
	return m
}

// ==================== random ====================

func modRandom(ip *Interp) *object.Module {
	m := newModule("random")
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	m.Dict["random"] = bi("random", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Float{V: r.Float64()}, nil
	})
	m.Dict["seed"] = bi("seed", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) > 0 {
			if n, ok := object.AsInt(args[0]); ok {
				r = rand.New(rand.NewSource(n))
			}
		} else {
			r = rand.New(rand.NewSource(time.Now().UnixNano()))
		}
		return object.None, nil
	})
	m.Dict["randint"] = bi("randint", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		a, _ := object.AsInt(args[0])
		b, _ := object.AsInt(args[1])
		if b < a {
			ip.ValueError("empty range for randint()")
		}
		return &object.Int{V: a + r.Int63n(b-a+1)}, nil
	})
	m.Dict["randrange"] = bi("randrange", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		var start, stop int64
		switch len(args) {
		case 1:
			stop, _ = object.AsInt(args[0])
		case 2:
			start, _ = object.AsInt(args[0])
			stop, _ = object.AsInt(args[1])
		case 3:
			start, _ = object.AsInt(args[0])
			stop, _ = object.AsInt(args[1])
		}
		if stop <= start {
			ip.ValueError("empty range for randrange()")
		}
		return &object.Int{V: start + r.Int63n(stop-start)}, nil
	})
	m.Dict["uniform"] = bi("uniform", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		a, _ := object.AsFloat(args[0])
		b, _ := object.AsFloat(args[1])
		return &object.Float{V: a + r.Float64()*(b-a)}, nil
	})
	m.Dict["choice"] = bi("choice", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(args[0])
		if len(items) == 0 {
			ip.IndexErr("Cannot choose from an empty sequence")
		}
		return items[r.Intn(len(items))], nil
	})
	m.Dict["shuffle"] = bi("shuffle", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l, ok := args[0].(*object.List)
		if !ok {
			ip.TypeError("shuffle() requires a list")
		}
		r.Shuffle(len(l.Items), func(i, j int) { l.Items[i], l.Items[j] = l.Items[j], l.Items[i] })
		return object.None, nil
	})
	m.Dict["sample"] = bi("sample", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(args[0])
		k, _ := object.AsInt(args[1])
		if int(k) > len(items) {
			ip.ValueError("Sample larger than population")
		}
		perm := r.Perm(len(items))
		out := make([]object.Object, k)
		for i := 0; i < int(k); i++ {
			out[i] = items[perm[i]]
		}
		return &object.List{Items: out}, nil
	})
	return m
}

// ==================== time ====================

func modTime(ip *Interp) *object.Module {
	m := newModule("time")
	start := time.Now()
	m.Dict["time"] = bi("time", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Float{V: float64(time.Now().UnixNano()) / 1e9}, nil
	})
	m.Dict["time_ns"] = bi("time_ns", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Int{V: time.Now().UnixNano()}, nil
	})
	m.Dict["perf_counter"] = bi("perf_counter", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Float{V: float64(time.Since(start).Nanoseconds()) / 1e9}, nil
	})
	m.Dict["sleep"] = bi("sleep", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		v, _ := object.AsFloat(args[0])
		time.Sleep(time.Duration(v * float64(time.Second)))
		return object.None, nil
	})
	m.Dict["strftime"] = bi("strftime", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		layout := goTimeLayout(args[0].Str())
		t := time.Now()
		if len(args) > 1 {
			if tup, ok := args[1].(*object.Tuple); ok && len(tup.Items) >= 6 {
				y, _ := object.AsInt(tup.Items[0])
				mo, _ := object.AsInt(tup.Items[1])
				d, _ := object.AsInt(tup.Items[2])
				h, _ := object.AsInt(tup.Items[3])
				mi, _ := object.AsInt(tup.Items[4])
				s, _ := object.AsInt(tup.Items[5])
				t = time.Date(int(y), time.Month(mo), int(d), int(h), int(mi), int(s), 0, time.Local)
			}
		}
		return &object.Str{V: t.Format(layout)}, nil
	})
	m.Dict["localtime"] = bi("localtime", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		t := time.Now()
		if len(args) > 0 {
			if f, ok := object.AsFloat(args[0]); ok {
				t = time.Unix(int64(f), 0)
			}
		}
		items := []object.Object{
			&object.Int{V: int64(t.Year())}, &object.Int{V: int64(t.Month())}, &object.Int{V: int64(t.Day())},
			&object.Int{V: int64(t.Hour())}, &object.Int{V: int64(t.Minute())}, &object.Int{V: int64(t.Second())},
			&object.Int{V: int64(t.Weekday())}, &object.Int{V: int64(t.YearDay())}, &object.Int{V: -1},
		}
		return &object.Tuple{Items: items}, nil
	})
	return m
}

// goTimeLayout 把 Python strftime 格式转 Go layout（常用占位）
func goTimeLayout(f string) string {
	repl := []struct{ py, go_ string }{
		{"%Y", "2006"}, {"%m", "01"}, {"%d", "02"}, {"%H", "15"},
		{"%M", "04"}, {"%S", "05"}, {"%y", "06"}, {"%j", "002"},
		{"%p", "PM"}, {"%I", "03"}, {"%a", "Mon"}, {"%A", "Monday"},
		{"%b", "Jan"}, {"%B", "January"}, {"%Z", "MST"}, {"%z", "-0700"},
		{"%f", "000000"}, {"%%", "%"},
	}
	out := f
	for _, r := range repl {
		out = strings.ReplaceAll(out, r.py, r.go_)
	}
	return out
}

// ==================== json ====================

func modJSON(ip *Interp) *object.Module {
	m := newModule("json")
	m.Dict["dumps"] = bi("dumps", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		indent := 0
		ensureAscii := true
		if kw != nil {
			if v, ok := kw["indent"]; ok && v != object.None {
				n, _ := object.AsInt(v)
				indent = int(n)
			}
			if v, ok := kw["ensure_ascii"]; ok {
				ensureAscii = object.Truthy(v)
			}
		}
		s, err := jsonEncode(ip, args[0], indent, 0, ensureAscii)
		if err != nil {
			return nil, err
		}
		return &object.Str{V: s}, nil
	})
	m.Dict["loads"] = bi("loads", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s := args[0].Str()
		v, err := jsonDecode(s)
		if err != nil {
			ip.raise(ip.excType("ValueError"), "%v", err)
		}
		return v, nil
	})
	m.Dict["dump"] = bi("dump", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		s, _ := jsonEncode(ip, args[0], 0, 0, false)
		ip.Printf("%s", s)
		return object.None, nil
	})
	return m
}

func jsonEncode(ip *Interp, o object.Object, indent, level int, ensureAscii bool) (string, error) {
	switch v := o.(type) {
	case *object.NoneObj:
		return "null", nil
	case *object.Bool:
		if v.V {
			return "true", nil
		}
		return "false", nil
	case *object.Int:
		return strconv.FormatInt(v.V, 10), nil
	case *object.Float:
		if math.IsInf(v.V, 0) || math.IsNaN(v.V) {
			return "null", nil
		}
		return strconv.FormatFloat(v.V, 'g', -1, 64), nil
	case *object.Str:
		return jsonQuote(v.V, ensureAscii), nil
	case *object.List:
		return jsonArray(ip, v.Items, indent, level, ensureAscii)
	case *object.Tuple:
		return jsonArray(ip, v.Items, indent, level, ensureAscii)
	case *object.Dict:
		return jsonObject(ip, v, indent, level, ensureAscii)
	}
	return "null", nil
}

func jsonQuote(s string, ensureAscii bool) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString("\\\"")
		case '\\':
			sb.WriteString("\\\\")
		case '\n':
			sb.WriteString("\\n")
		case '\r':
			sb.WriteString("\\r")
		case '\t':
			sb.WriteString("\\t")
		case '\b':
			sb.WriteString("\\b")
		case '\f':
			sb.WriteString("\\f")
		default:
			if r < 32 {
				sb.WriteString(fmt.Sprintf("\\u%04x", r))
			} else if ensureAscii && r > 127 {
				sb.WriteString(fmt.Sprintf("\\u%04x", r))
			} else {
				sb.WriteRune(r)
			}
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

func jsonArray(ip *Interp, items []object.Object, indent, level int, ascii bool) (string, error) {
	if len(items) == 0 {
		return "[]", nil
	}
	pad := strings.Repeat(" ", indent*(level+1))
	endPad := strings.Repeat(" ", indent*level)
	var parts []string
	for _, it := range items {
		s, err := jsonEncode(ip, it, indent, level+1, ascii)
		if err != nil {
			return "", err
		}
		parts = append(parts, s)
	}
	if indent > 0 {
		return "[\n" + pad + strings.Join(parts, ",\n"+pad) + "\n" + endPad + "]", nil
	}
	return "[" + strings.Join(parts, ", ") + "]", nil
}

func jsonObject(ip *Interp, d *object.Dict, indent, level int, ascii bool) (string, error) {
	if len(d.Keys) == 0 {
		return "{}", nil
	}
	pad := strings.Repeat(" ", indent*(level+1))
	endPad := strings.Repeat(" ", indent*level)
	var parts []string
	for i, k := range d.Keys {
		var key string
		if s, ok := k.(*object.Str); ok {
			key = jsonQuote(s.V, ascii)
		} else {
			key = jsonQuote(ip.StrOf(k), ascii)
		}
		vs, err := jsonEncode(ip, d.Vals[i], indent, level+1, ascii)
		if err != nil {
			return "", err
		}
		parts = append(parts, key+": "+vs)
	}
	if indent > 0 {
		return "{\n" + pad + strings.Join(parts, ",\n"+pad) + "\n" + endPad + "}", nil
	}
	return "{" + strings.Join(parts, ", ") + "}", nil
}

// jsonDecode 简易 JSON 解析
func jsonDecode(s string) (object.Object, error) {
	p := &jsonParser{src: []rune(s)}
	p.skipWS()
	v, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	p.skipWS()
	return v, nil
}

type jsonParser struct {
	src []rune
	pos int
}

func (p *jsonParser) skipWS() {
	for p.pos < len(p.src) {
		switch p.src[p.pos] {
		case ' ', '\t', '\n', '\r':
			p.pos++
		default:
			return
		}
	}
}

func (p *jsonParser) parseValue() (object.Object, error) {
	p.skipWS()
	if p.pos >= len(p.src) {
		return nil, fmt.Errorf("unexpected end of JSON")
	}
	c := p.src[p.pos]
	switch {
	case c == '{':
		p.pos++
		d := &object.Dict{}
		p.skipWS()
		if p.pos < len(p.src) && p.src[p.pos] == '}' {
			p.pos++
			return d, nil
		}
		for {
			p.skipWS()
			if p.pos >= len(p.src) || p.src[p.pos] != '"' {
				return nil, fmt.Errorf("expected string key")
			}
			k, err := p.parseString()
			if err != nil {
				return nil, err
			}
			p.skipWS()
			if p.pos >= len(p.src) || p.src[p.pos] != ':' {
				return nil, fmt.Errorf("expected ':'")
			}
			p.pos++
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			d.Set(&object.Str{V: k}, v)
			p.skipWS()
			if p.pos < len(p.src) && p.src[p.pos] == ',' {
				p.pos++
				continue
			}
			if p.pos < len(p.src) && p.src[p.pos] == '}' {
				p.pos++
				return d, nil
			}
			return nil, fmt.Errorf("expected ',' or '}'")
		}
	case c == '[':
		p.pos++
		var items []object.Object
		p.skipWS()
		if p.pos < len(p.src) && p.src[p.pos] == ']' {
			p.pos++
			return &object.List{Items: items}, nil
		}
		for {
			v, err := p.parseValue()
			if err != nil {
				return nil, err
			}
			items = append(items, v)
			p.skipWS()
			if p.pos < len(p.src) && p.src[p.pos] == ',' {
				p.pos++
				continue
			}
			if p.pos < len(p.src) && p.src[p.pos] == ']' {
				p.pos++
				return &object.List{Items: items}, nil
			}
			return nil, fmt.Errorf("expected ',' or ']'")
		}
	case c == '"':
		s, err := p.parseString()
		if err != nil {
			return nil, err
		}
		return &object.Str{V: s}, nil
	case c == 't':
		if strings.HasPrefix(string(p.src[p.pos:]), "true") {
			p.pos += 4
			return object.TrueObj, nil
		}
	case c == 'f':
		if strings.HasPrefix(string(p.src[p.pos:]), "false") {
			p.pos += 5
			return object.FalseObj, nil
		}
	case c == 'n':
		if strings.HasPrefix(string(p.src[p.pos:]), "null") {
			p.pos += 4
			return object.None, nil
		}
	}
	// 数字
	start := p.pos
	if p.pos < len(p.src) && (p.src[p.pos] == '-' || p.src[p.pos] == '+') {
		p.pos++
	}
	isFloat := false
	for p.pos < len(p.src) {
		ch := p.src[p.pos]
		if ch >= '0' && ch <= '9' {
			p.pos++
		} else if ch == '.' || ch == 'e' || ch == 'E' || ch == '-' || ch == '+' {
			isFloat = true
			p.pos++
		} else {
			break
		}
	}
	numStr := string(p.src[start:p.pos])
	if numStr == "" {
		return nil, fmt.Errorf("unexpected character %q", string(c))
	}
	if isFloat {
		f, err := strconv.ParseFloat(numStr, 64)
		if err != nil {
			return nil, err
		}
		return &object.Float{V: f}, nil
	}
	n, err := strconv.ParseInt(numStr, 10, 64)
	if err != nil {
		f, ferr := strconv.ParseFloat(numStr, 64)
		if ferr != nil {
			return nil, err
		}
		return &object.Float{V: f}, nil
	}
	return &object.Int{V: n}, nil
}

func (p *jsonParser) parseString() (string, error) {
	p.pos++ // "
	var sb strings.Builder
	for p.pos < len(p.src) {
		c := p.src[p.pos]
		if c == '"' {
			p.pos++
			return sb.String(), nil
		}
		if c == '\\' {
			p.pos++
			if p.pos >= len(p.src) {
				break
			}
			e := p.src[p.pos]
			switch e {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case 'r':
				sb.WriteRune('\r')
			case 'b':
				sb.WriteRune('\b')
			case 'f':
				sb.WriteRune('\f')
			case '/':
				sb.WriteRune('/')
			case '\\':
				sb.WriteRune('\\')
			case '"':
				sb.WriteRune('"')
			case 'u':
				if p.pos+4 < len(p.src) {
					h := string(p.src[p.pos+1 : p.pos+5])
					if v, err := strconv.ParseUint(h, 16, 32); err == nil {
						sb.WriteRune(rune(v))
						p.pos += 4
					}
				}
			default:
				sb.WriteRune(e)
			}
			p.pos++
			continue
		}
		sb.WriteRune(c)
		p.pos++
	}
	return "", fmt.Errorf("unterminated string")
}

// ==================== re ====================

func modRe(ip *Interp) *object.Module {
	m := newModule("re")
	m.Dict["I"] = &object.Int{V: 2}
	m.Dict["IGNORECASE"] = &object.Int{V: 2}
	m.Dict["M"] = &object.Int{V: 8}
	m.Dict["MULTILINE"] = &object.Int{V: 8}
	m.Dict["S"] = &object.Int{V: 16}
	m.Dict["DOTALL"] = &object.Int{V: 16}

	compileRe := func(pattern string, flags int64) *regexp.Regexp {
		p := pattern
		if flags&2 != 0 {
			p = "(?i)" + p
		}
		if flags&8 != 0 {
			p = "(?m)" + p
		}
		if flags&16 != 0 {
			p = "(?s)" + p
		}
		re, err := regexp.Compile(p)
		if err != nil {
			ip.raise(ip.excType("re.error"), "%v", err)
		}
		return re
	}

	m.Dict["match"] = bi("match", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		pat := args[0].Str()
		s := args[1].Str()
		re := compileRe("^(?:"+pat+")", 0)
		loc := re.FindStringSubmatchIndex(s)
		if loc == nil {
			return object.None, nil
		}
		return makeMatch(ip, re, s, loc), nil
	})
	m.Dict["search"] = bi("search", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		pat := args[0].Str()
		s := args[1].Str()
		re := compileRe(pat, 0)
		loc := re.FindStringSubmatchIndex(s)
		if loc == nil {
			return object.None, nil
		}
		return makeMatch(ip, re, s, loc), nil
	})
	m.Dict["findall"] = bi("findall", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		pat := args[0].Str()
		s := args[1].Str()
		re := compileRe(pat, 0)
		all := re.FindAllStringSubmatch(s, -1)
		var out []object.Object
		for _, g := range all {
			if len(g) == 1 {
				out = append(out, &object.Str{V: g[0]})
			} else {
				items := make([]object.Object, 0, len(g)-1)
				for _, x := range g[1:] {
					items = append(items, &object.Str{V: x})
				}
				out = append(out, &object.Tuple{Items: items})
			}
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["sub"] = bi("sub", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		pat := args[0].Str()
		repl := args[1].Str()
		s := args[2].Str()
		count := -1
		if len(args) > 3 {
			n, _ := object.AsInt(args[3])
			count = int(n)
		}
		re := compileRe(pat, 0)
		result := re.ReplaceAllStringFunc(s, func(mstr string) string {
			if count == 0 {
				return mstr
			}
			if count > 0 {
				count--
			}
			// 支持 \1 反向引用
			r := repl
			for i := 0; i <= 9; i++ {
				r = strings.ReplaceAll(r, "\\"+strconv.Itoa(i), "${"+strconv.Itoa(i)+"}")
			}
			return string(re.ExpandString([]byte{}, r, mstr, re.FindStringSubmatchIndex(mstr)))
		})
		return &object.Str{V: result}, nil
	})
	m.Dict["split"] = bi("split", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		pat := args[0].Str()
		s := args[1].Str()
		re := compileRe(pat, 0)
		parts := re.Split(s, -1)
		items := make([]object.Object, len(parts))
		for i, p := range parts {
			items[i] = &object.Str{V: p}
		}
		return &object.List{Items: items}, nil
	})
	m.Dict["escape"] = bi("escape", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: regexp.QuoteMeta(args[0].Str())}, nil
	})
	m.Dict["compile"] = bi("compile", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		pat := args[0].Str()
		flags := int64(0)
		if len(args) > 1 {
			flags, _ = object.AsInt(args[1])
		}
		re := compileRe(pat, flags)
		cls := &object.TypeObj{Name: "Pattern", Dict: map[string]object.Object{}}
		inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
		inst.Dict["pattern"] = &object.Str{V: pat}
		inst.Dict["match"] = bi("match", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			s := a[0].Str()
			loc := re.FindStringSubmatchIndex(s)
			if loc == nil || loc[0] != 0 {
				return object.None, nil
			}
			return makeMatch(ip, re, s, loc), nil
		})
		inst.Dict["search"] = bi("search", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			s := a[0].Str()
			loc := re.FindStringSubmatchIndex(s)
			if loc == nil {
				return object.None, nil
			}
			return makeMatch(ip, re, s, loc), nil
		})
		inst.Dict["findall"] = bi("findall", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			s := a[0].Str()
			all := re.FindAllStringSubmatch(s, -1)
			var out []object.Object
			for _, g := range all {
				if len(g) == 1 {
					out = append(out, &object.Str{V: g[0]})
				} else {
					items := make([]object.Object, 0, len(g)-1)
					for _, x := range g[1:] {
						items = append(items, &object.Str{V: x})
					}
					out = append(out, &object.Tuple{Items: items})
				}
			}
			return &object.List{Items: out}, nil
		})
		inst.Dict["sub"] = bi("sub", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			repl := a[0].Str()
			s := a[1].Str()
			return &object.Str{V: re.ReplaceAllString(s, repl)}, nil
		})
		return inst, nil
	})
	return m
}

// makeMatch 构造 Match 对象
func makeMatch(ip *Interp, re *regexp.Regexp, s string, loc []int) object.Object {
	cls := &object.TypeObj{Name: "Match", Dict: map[string]object.Object{}}
	inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
	names := re.SubexpNames()
	inst.Dict["group"] = bi("group", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		idx := 0
		if len(a) > 0 {
			if n, ok := object.AsInt(a[0]); ok {
				idx = int(n)
			} else if sname, ok := a[0].(*object.Str); ok {
				for i, nm := range names {
					if nm == sname.V {
						idx = i
						break
					}
				}
			}
		}
		if idx*2+1 >= len(loc) || loc[idx*2] < 0 {
			return object.None, nil
		}
		return &object.Str{V: s[loc[idx*2]:loc[idx*2+1]]}, nil
	})
	inst.Dict["groups"] = bi("groups", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var out []object.Object
		for i := 1; i*2+1 < len(loc); i++ {
			if loc[i*2] < 0 {
				out = append(out, object.None)
			} else {
				out = append(out, &object.Str{V: s[loc[i*2]:loc[i*2+1]]})
			}
		}
		return &object.Tuple{Items: out}, nil
	})
	inst.Dict["groupdict"] = bi("groupdict", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		d := &object.Dict{}
		for i, nm := range names {
			if nm == "" || i*2+1 >= len(loc) {
				continue
			}
			if loc[i*2] < 0 {
				d.Set(&object.Str{V: nm}, object.None)
			} else {
				d.Set(&object.Str{V: nm}, &object.Str{V: s[loc[i*2]:loc[i*2+1]]})
			}
		}
		return d, nil
	})
	inst.Dict["start"] = bi("start", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(loc[0])}, nil
	})
	inst.Dict["end"] = bi("end", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(loc[1])}, nil
	})
	inst.Dict["span"] = bi("span", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Tuple{Items: []object.Object{&object.Int{V: int64(loc[0])}, &object.Int{V: int64(loc[1])}}}, nil
	})
	inst.Dict["group0"] = &object.Str{V: s[loc[0]:loc[1]]}
	return inst
}

// ==================== os / os.path ====================

func modOS(ip *Interp) *object.Module {
	m := newModule("os")
	m.Dict["getcwd"] = bi("getcwd", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d, _ := os.Getwd()
		return &object.Str{V: d}, nil
	})
	m.Dict["listdir"] = bi("listdir", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		dir := "."
		if len(args) > 0 {
			dir = args[0].Str()
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		var out []object.Object
		for _, e := range entries {
			out = append(out, &object.Str{V: e.Name()})
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Str() < out[j].Str() })
		return &object.List{Items: out}, nil
	})
	m.Dict["makedirs"] = bi("makedirs", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		err := os.MkdirAll(args[0].Str(), 0o755)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return object.None, nil
	})
	m.Dict["mkdir"] = bi("mkdir", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if err := os.Mkdir(args[0].Str(), 0o755); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return object.None, nil
	})
	m.Dict["rmdir"] = bi("rmdir", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if err := os.Remove(args[0].Str()); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return object.None, nil
	})
	m.Dict["remove"] = bi("remove", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if err := os.Remove(args[0].Str()); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return object.None, nil
	})
	m.Dict["rename"] = bi("rename", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if err := os.Rename(args[0].Str(), args[1].Str()); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return object.None, nil
	})
	m.Dict["getenv"] = bi("getenv", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		v := os.Getenv(args[0].Str())
		if v == "" && len(args) > 1 {
			return args[1], nil
		}
		return &object.Str{V: v}, nil
	})
	m.Dict["environ"] = func() object.Object {
		d := &object.Dict{}
		for _, kv := range os.Environ() {
			if i := strings.Index(kv, "="); i > 0 {
				d.Set(&object.Str{V: kv[:i]}, &object.Str{V: kv[i+1:]})
			}
		}
		return d
	}()
	m.Dict["sep"] = &object.Str{V: string(filepath.Separator)}
	m.Dict["linesep"] = &object.Str{V: "\n"}
	m.Dict["path"] = modOSPath(ip)
	return m
}

func modOSPath(ip *Interp) *object.Module {
	m := newModule("os.path")
	m.Dict["join"] = bi("join", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		parts := make([]string, len(args))
		for i, a := range args {
			parts[i] = a.Str()
		}
		return &object.Str{V: filepath.Join(parts...)}, nil
	})
	m.Dict["exists"] = bi("exists", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		_, err := os.Stat(args[0].Str())
		return object.GetBool(err == nil), nil
	})
	m.Dict["isfile"] = bi("isfile", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		st, err := os.Stat(args[0].Str())
		return object.GetBool(err == nil && !st.IsDir()), nil
	})
	m.Dict["isdir"] = bi("isdir", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		st, err := os.Stat(args[0].Str())
		return object.GetBool(err == nil && st.IsDir()), nil
	})
	m.Dict["basename"] = bi("basename", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: filepath.Base(args[0].Str())}, nil
	})
	m.Dict["dirname"] = bi("dirname", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: filepath.Dir(args[0].Str())}, nil
	})
	m.Dict["abspath"] = bi("abspath", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p, _ := filepath.Abs(args[0].Str())
		return &object.Str{V: p}, nil
	})
	m.Dict["split"] = bi("split", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d, f := filepath.Split(args[0].Str())
		return &object.Tuple{Items: []object.Object{&object.Str{V: d}, &object.Str{V: f}}}, nil
	})
	m.Dict["splitext"] = bi("splitext", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p := args[0].Str()
		ext := filepath.Ext(p)
		return &object.Tuple{Items: []object.Object{&object.Str{V: strings.TrimSuffix(p, ext)}, &object.Str{V: ext}}}, nil
	})
	m.Dict["isabs"] = bi("isabs", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return object.GetBool(filepath.IsAbs(args[0].Str())), nil
	})
	m.Dict["normpath"] = bi("normpath", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: filepath.Clean(args[0].Str())}, nil
	})
	m.Dict["relpath"] = bi("relpath", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		base := "."
		if len(args) > 1 {
			base = args[1].Str()
		}
		r, err := filepath.Rel(base, args[0].Str())
		if err != nil {
			ip.raise(ip.excType("ValueError"), "%v", err)
		}
		return &object.Str{V: r}, nil
	})
	m.Dict["realpath"] = bi("realpath", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p, err := filepath.Abs(args[0].Str())
		if err != nil {
			p = args[0].Str()
		}
		return &object.Str{V: p}, nil
	})
	m.Dict["expanduser"] = bi("expanduser", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p := args[0].Str()
		if len(p) > 0 && p[0] == '~' {
			home, _ := os.UserHomeDir()
			p = filepath.Join(home, p[1:])
		}
		return &object.Str{V: p}, nil
	})
	m.Dict["getsize"] = bi("getsize", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		st, err := os.Stat(args[0].Str())
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return &object.Int{V: st.Size()}, nil
	})
	return m
}

// ==================== sys ====================

func modSys(ip *Interp) *object.Module {
	m := newModule("sys")
	m.Dict["argv"] = &object.List{Items: []object.Object{&object.Str{V: ip.MainFile}}}
	m.Dict["version"] = &object.Str{V: "3.11.0 (main, Oct 24 2026, 00:00:00) [MSC v.1938 64 bit (AMD64)]"}
	m.Dict["platform"] = &object.Str{V: "win32"}
	m.Dict["maxsize"] = &object.Int{V: math.MaxInt64}
	m.Dict["exit"] = bi("exit", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		code := 0
		if len(args) > 0 {
			if n, ok := object.AsInt(args[0]); ok {
				code = int(n)
			}
		}
		panic(&exitSignal{code: code})
	})
	m.Dict["path"] = func() object.Object {
		items := make([]object.Object, len(ip.SysPath))
		for i, p := range ip.SysPath {
			items[i] = &object.Str{V: p}
		}
		return &object.List{Items: items}
	}()
	m.Dict["getrecursionlimit"] = bi("getrecursionlimit", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(ip.MaxDepth)}, nil
	})
	m.Dict["setrecursionlimit"] = bi("setrecursionlimit", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if n, ok := object.AsInt(args[0]); ok {
			ip.MaxDepth = int(n)
		}
		return object.None, nil
	})
	m.Dict["stdout"] = &object.Str{V: ""}
	return m
}

// ==================== string ====================

func modString(ip *Interp) *object.Module {
	m := newModule("string")
	m.Dict["ascii_lowercase"] = &object.Str{V: "abcdefghijklmnopqrstuvwxyz"}
	m.Dict["ascii_uppercase"] = &object.Str{V: "ABCDEFGHIJKLMNOPQRSTUVWXYZ"}
	m.Dict["ascii_letters"] = &object.Str{V: "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"}
	m.Dict["digits"] = &object.Str{V: "0123456789"}
	m.Dict["hexdigits"] = &object.Str{V: "0123456789abcdefABCDEF"}
	m.Dict["octdigits"] = &object.Str{V: "01234567"}
	m.Dict["punctuation"] = &object.Str{V: "!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~"}
	m.Dict["whitespace"] = &object.Str{V: " \t\n\r\v\f"}
	m.Dict["printable"] = &object.Str{V: "0123456789abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ!\"#$%&'()*+,-./:;<=>?@[\\]^_`{|}~ \t\n\r\v\f"}
	return m
}

// ==================== collections ====================

func modCollections(ip *Interp) *object.Module {
	m := newModule("collections")

	// Counter 类
	counterCls := &object.TypeObj{Name: "Counter", Dict: map[string]object.Object{}}
	counterCls.Dict["__init__"] = bi("__init__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) < 1 {
			return object.None, nil
		}
		self, ok := args[0].(*object.Instance)
		if !ok {
			return object.None, nil
		}
		self.Dict["_data"] = &object.Dict{}
		if len(args) > 1 {
			d := self.Dict["_data"].(*object.Dict)
			for _, v := range ip.ToSlice(args[1]) {
				cur, _ := d.Get(v)
				n, _ := object.AsInt(cur)
				d.Set(v, &object.Int{V: n + 1})
			}
		}
		return object.None, nil
	})
	counterCls.Dict["most_common"] = bi("most_common", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		type kv struct {
			k object.Object
			n int64
		}
		var list []kv
		for i, k := range d.Keys {
			n, _ := object.AsInt(d.Vals[i])
			list = append(list, kv{k, n})
		}
		sort.SliceStable(list, func(i, j int) bool { return list[i].n > list[j].n })
		limit := len(list)
		if len(args) > 1 {
			if v, ok := object.AsInt(args[1]); ok && int(v) < limit {
				limit = int(v)
			}
		}
		out := make([]object.Object, 0, limit)
		for i := 0; i < limit; i++ {
			out = append(out, &object.Tuple{Items: []object.Object{list[i].k, &object.Int{V: list[i].n}}})
		}
		return &object.List{Items: out}, nil
	})
	counterCls.Dict["keys"] = bi("keys", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		return &object.List{Items: append([]object.Object{}, d.Keys...)}, nil
	})
	counterCls.Dict["items"] = bi("items", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		out := make([]object.Object, len(d.Keys))
		for i := range d.Keys {
			out[i] = &object.Tuple{Items: []object.Object{d.Keys[i], d.Vals[i]}}
		}
		return &object.List{Items: out}, nil
	})
	counterCls.Dict["__len__"] = bi("__len__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		return &object.Int{V: int64(len(self.Dict["_data"].(*object.Dict).Keys))}, nil
	})
	counterCls.Dict["__iter__"] = bi("__iter__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		return &object.Iterator{Items: append([]object.Object{}, d.Keys...)}, nil
	})
	counterCls.Dict["update"] = bi("update", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		if len(args) > 1 {
			for _, v := range ip.ToSlice(args[1]) {
				cur, _ := d.Get(v)
				n, _ := object.AsInt(cur)
				d.Set(v, &object.Int{V: n + 1})
			}
		}
		return object.None, nil
	})
	counterCls.Dict["elements"] = bi("elements", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		var out []object.Object
		for i, k := range d.Keys {
			n, _ := object.AsInt(d.Vals[i])
			for j := int64(0); j < n; j++ {
				out = append(out, k)
			}
		}
		return &object.List{Items: out}, nil
	})
	counterCls.Dict["__getitem__"] = bi("__getitem__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		if v, ok := d.Get(args[1]); ok {
			return v, nil
		}
		return &object.Int{V: 0}, nil
	})
	m.Dict["Counter"] = counterCls

	// defaultdict
	ddCls := &object.TypeObj{Name: "defaultdict", Dict: map[string]object.Object{}}
	ddCls.Dict["__init__"] = bi("__init__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		self.Dict["_factory"] = object.None
		self.Dict["_data"] = &object.Dict{}
		if len(args) > 1 {
			self.Dict["_factory"] = args[1]
		}
		return object.None, nil
	})
	ddCls.Dict["__getitem__"] = bi("__getitem__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		if v, ok := d.Get(args[1]); ok {
			return v, nil
		}
		f := self.Dict["_factory"]
		var nv object.Object = object.None
		if f != object.None && f != nil {
			nv = ip.Call(f, nil, nil)
		}
		d.Set(args[1], nv)
		return nv, nil
	})
	ddCls.Dict["keys"] = bi("keys", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		return &object.List{Items: append([]object.Object{}, d.Keys...)}, nil
	})
	ddCls.Dict["values"] = bi("values", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		return &object.List{Items: append([]object.Object{}, d.Vals...)}, nil
	})
	ddCls.Dict["items"] = bi("items", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		out := make([]object.Object, len(d.Keys))
		for i := range d.Keys {
			out[i] = &object.Tuple{Items: []object.Object{d.Keys[i], d.Vals[i]}}
		}
		return &object.List{Items: out}, nil
	})
	ddCls.Dict["get"] = bi("get", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		if v, ok := d.Get(args[1]); ok {
			return v, nil
		}
		if len(args) > 2 {
			return args[2], nil
		}
		return object.None, nil
	})
	ddCls.Dict["__len__"] = bi("__len__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		return &object.Int{V: int64(len(self.Dict["_data"].(*object.Dict).Keys))}, nil
	})
	ddCls.Dict["__iter__"] = bi("__iter__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		return &object.Iterator{Items: append([]object.Object{}, d.Keys...)}, nil
	})
	ddCls.Dict["__contains__"] = bi("__contains__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		d := self.Dict["_data"].(*object.Dict)
		return object.GetBool(d.Index(args[1]) >= 0), nil
	})
	ddCls.Dict["__setitem__"] = bi("__setitem__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		self.Dict["_data"].(*object.Dict).Set(args[1], args[2])
		return object.None, nil
	})
	m.Dict["defaultdict"] = ddCls

	// deque
	dequeCls := &object.TypeObj{Name: "deque", Dict: map[string]object.Object{}}
	dequeCls.Dict["__init__"] = bi("__init__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		items := []object.Object{}
		if len(args) > 1 {
			items = append(items, ip.ToSlice(args[1])...)
		}
		self.Dict["_items"] = &object.List{Items: items}
		return object.None, nil
	})
	dequeCls.Dict["append"] = bi("append", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := args[0].(*object.Instance).Dict["_items"].(*object.List)
		l.Items = append(l.Items, args[1])
		return object.None, nil
	})
	dequeCls.Dict["appendleft"] = bi("appendleft", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := args[0].(*object.Instance).Dict["_items"].(*object.List)
		l.Items = append([]object.Object{args[1]}, l.Items...)
		return object.None, nil
	})
	dequeCls.Dict["pop"] = bi("pop", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := args[0].(*object.Instance).Dict["_items"].(*object.List)
		if len(l.Items) == 0 {
			ip.IndexErr("pop from an empty deque")
		}
		v := l.Items[len(l.Items)-1]
		l.Items = l.Items[:len(l.Items)-1]
		return v, nil
	})
	dequeCls.Dict["popleft"] = bi("popleft", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := args[0].(*object.Instance).Dict["_items"].(*object.List)
		if len(l.Items) == 0 {
			ip.IndexErr("pop from an empty deque")
		}
		v := l.Items[0]
		l.Items = l.Items[1:]
		return v, nil
	})
	dequeCls.Dict["__len__"] = bi("__len__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := args[0].(*object.Instance).Dict["_items"].(*object.List)
		return &object.Int{V: int64(len(l.Items))}, nil
	})
	dequeCls.Dict["__iter__"] = bi("__iter__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		l := args[0].(*object.Instance).Dict["_items"].(*object.List)
		return &object.Iterator{Items: append([]object.Object{}, l.Items...)}, nil
	})
	m.Dict["deque"] = dequeCls

	// namedtuple（工厂）
	m.Dict["namedtuple"] = bi("namedtuple", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		name := args[0].Str()
		fields := ip.ToSlice(args[1])
		cls := &object.TypeObj{Name: name, Dict: map[string]object.Object{}}
		fieldNames := make([]string, len(fields))
		for i, f := range fields {
			fieldNames[i] = f.Str()
		}
		cls.Dict["_fields"] = &object.Tuple{Items: fields}
		cls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			self := a[0].(*object.Instance)
			for i, fn := range fieldNames {
				if i+1 < len(a) {
					self.Dict[fn] = a[i+1]
				}
			}
			return object.None, nil
		})
		m.Dict[name] = cls
		return cls, nil
	})
	m.Dict["OrderedDict"] = &object.TypeObj{Name: "OrderedDict", Bases: []*object.TypeObj{object.DictType}, Dict: map[string]object.Object{}}
	return m
}

// ==================== itertools ====================

func modItertools(ip *Interp) *object.Module {
	m := newModule("itertools")
	m.Dict["chain"] = bi("chain", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		var out []object.Object
		for _, a := range args {
			out = append(out, ip.ToSlice(a)...)
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["product"] = bi("product", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		lists := make([][]object.Object, len(args))
		for i, a := range args {
			lists[i] = ip.ToSlice(a)
		}
		var out []object.Object
		var rec func(idx int, cur []object.Object)
		rec = func(idx int, cur []object.Object) {
			if idx == len(lists) {
				out = append(out, &object.Tuple{Items: append([]object.Object{}, cur...)})
				return
			}
			for _, v := range lists[idx] {
				rec(idx+1, append(cur, v))
			}
		}
		rec(0, nil)
		return &object.List{Items: out}, nil
	})
	m.Dict["combinations"] = bi("combinations", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(args[0])
		r, _ := object.AsInt(args[1])
		var out []object.Object
		var rec func(start int, cur []object.Object)
		rec = func(start int, cur []object.Object) {
			if len(cur) == int(r) {
				out = append(out, &object.Tuple{Items: append([]object.Object{}, cur...)})
				return
			}
			for i := start; i < len(items); i++ {
				rec(i+1, append(cur, items[i]))
			}
		}
		rec(0, nil)
		return &object.List{Items: out}, nil
	})
	m.Dict["permutations"] = bi("permutations", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(args[0])
		r := int64(len(items))
		if len(args) > 1 {
			r, _ = object.AsInt(args[1])
		}
		var out []object.Object
		used := make([]bool, len(items))
		var rec func(cur []object.Object)
		rec = func(cur []object.Object) {
			if int64(len(cur)) == r {
				out = append(out, &object.Tuple{Items: append([]object.Object{}, cur...)})
				return
			}
			for i := range items {
				if used[i] {
					continue
				}
				used[i] = true
				rec(append(cur, items[i]))
				used[i] = false
			}
		}
		rec(nil)
		return &object.List{Items: out}, nil
	})
	m.Dict["repeat"] = bi("repeat", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		n := int64(-1)
		if len(args) > 1 {
			n, _ = object.AsInt(args[1])
		}
		var out []object.Object
		for i := int64(0); n < 0 || i < n; i++ {
			out = append(out, args[0])
			if n < 0 && i > 10000 {
				break
			}
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["count"] = bi("count", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		start, _ := object.AsInt(args[0])
		step := int64(1)
		if len(args) > 1 {
			step, _ = object.AsInt(args[1])
		}
		var out []object.Object
		for i := 0; i < 1000; i++ {
			out = append(out, &object.Int{V: start + int64(i)*step})
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["islice"] = bi("islice", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(args[0])
		start, stop := 0, len(items)
		if len(args) == 2 {
			v, _ := object.AsInt(args[1])
			stop = int(v)
		} else if len(args) > 2 {
			v, _ := object.AsInt(args[1])
			start = int(v)
			v2, _ := object.AsInt(args[2])
			stop = int(v2)
		}
		if start > len(items) {
			start = len(items)
		}
		if stop > len(items) {
			stop = len(items)
		}
		if start > stop {
			start = stop
		}
		return &object.List{Items: append([]object.Object{}, items[start:stop]...)}, nil
	})
	m.Dict["groupby"] = bi("groupby", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(args[0])
		var out []object.Object
		var curKey object.Object
		var curGroup []object.Object
		for _, it := range items {
			k := it
			if len(args) > 1 {
				k = ip.Call(args[1], []object.Object{it}, nil)
			}
			if curKey == nil || !ip.Eq(curKey, k) {
				if curKey != nil {
					out = append(out, &object.Tuple{Items: []object.Object{curKey, &object.List{Items: curGroup}}})
				}
				curKey = k
				curGroup = []object.Object{it}
			} else {
				curGroup = append(curGroup, it)
			}
		}
		if curKey != nil {
			out = append(out, &object.Tuple{Items: []object.Object{curKey, &object.List{Items: curGroup}}})
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["zip_longest"] = m.Dict["chain"]
	return m
}

// ==================== functools ====================

func modFunctools(ip *Interp) *object.Module {
	m := newModule("functools")
	m.Dict["reduce"] = bi("reduce", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		fn := args[0]
		items := ip.ToSlice(args[1])
		var acc object.Object
		start := 0
		if len(args) > 2 {
			acc = args[2]
		} else {
			if len(items) == 0 {
				ip.TypeError("reduce() of empty sequence with no initial value")
			}
			acc = items[0]
			start = 1
		}
		for i := start; i < len(items); i++ {
			acc = ip.Call(fn, []object.Object{acc, items[i]}, nil)
		}
		return acc, nil
	})
	m.Dict["partial"] = bi("partial", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		fn := args[0]
		bound := append([]object.Object{}, args[1:]...)
		return bi("partial", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			all := append(append([]object.Object{}, bound...), a...)
			return ip.Call(fn, all, k), nil
		}), nil
	})
	m.Dict["wraps"] = bi("wraps", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return bi("decorator", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return a[0], nil
		}), nil
	})
	m.Dict["lru_cache"] = bi("lru_cache", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		var target object.Object
		if len(args) > 0 && (isCallableObj(args[0])) {
			target = args[0]
		}
		wrap := func(fn object.Object) object.Object {
			cache := map[string]object.Object{}
			return bi("cached", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				key := ""
				for _, x := range a {
					key += ip.Repr(x) + "|"
				}
				if v, ok := cache[key]; ok {
					return v, nil
				}
				v := ip.Call(fn, a, k)
				cache[key] = v
				return v, nil
			})
		}
		if target != nil {
			return wrap(target), nil
		}
		return bi("lru_cache_decorator", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return wrap(a[0]), nil
		}), nil
	})
	m.Dict["cmp_to_key"] = bi("cmp_to_key", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return args[0], nil
	})
	return m
}

func isCallableObj(o object.Object) bool {
	switch o.(type) {
	case *object.Function, *object.Builtin, *object.TypeObj:
		return true
	}
	return false
}

// ==================== copy ====================

func modCopy(ip *Interp) *object.Module {
	m := newModule("copy")
	m.Dict["copy"] = bi("copy", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return ip.shallowCopy(args[0]), nil
	})
	m.Dict["deepcopy"] = bi("deepcopy", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return ip.deepCopy(args[0], map[object.Object]object.Object{}), nil
	})
	return m
}

func (ip *Interp) shallowCopy(o object.Object) object.Object {
	switch v := o.(type) {
	case *object.List:
		return &object.List{Items: append([]object.Object{}, v.Items...)}
	case *object.Dict:
		return v.Copy()
	case *object.Set:
		s := &object.Set{}
		s.Items = append(s.Items, v.Items...)
		return s
	case *object.Tuple:
		return v
	case *object.Instance:
		inst := &object.Instance{Class: v.Class, Dict: map[string]object.Object{}}
		for k, val := range v.Dict {
			inst.Dict[k] = val
		}
		return inst
	}
	return o
}

func (ip *Interp) deepCopy(o object.Object, seen map[object.Object]object.Object) object.Object {
	switch v := o.(type) {
	case *object.List:
		out := &object.List{Items: make([]object.Object, len(v.Items))}
		for i, it := range v.Items {
			out.Items[i] = ip.deepCopy(it, seen)
		}
		return out
	case *object.Tuple:
		out := &object.Tuple{Items: make([]object.Object, len(v.Items))}
		for i, it := range v.Items {
			out.Items[i] = ip.deepCopy(it, seen)
		}
		return out
	case *object.Dict:
		out := &object.Dict{}
		for i, k := range v.Keys {
			out.Set(ip.deepCopy(k, seen), ip.deepCopy(v.Vals[i], seen))
		}
		return out
	case *object.Set:
		out := &object.Set{}
		for _, it := range v.Items {
			out.Add(ip.deepCopy(it, seen))
		}
		return out
	case *object.Instance:
		inst := &object.Instance{Class: v.Class, Dict: map[string]object.Object{}}
		for k, val := range v.Dict {
			inst.Dict[k] = ip.deepCopy(val, seen)
		}
		return inst
	}
	return o
}

// ==================== hashlib / base64 ====================

func modHashlib(ip *Interp) *object.Module {
	m := newModule("hashlib")
	mk := func(name string, fn func([]byte) []byte) {
		cls := &object.TypeObj{Name: name, Dict: map[string]object.Object{}}
		var digest []byte
		m.Dict[name] = bi(name, func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
			var data []byte
			if len(args) > 0 {
				switch v := args[0].(type) {
				case *object.Str:
					data = []byte(v.V)
				case *object.Bytes:
					data = v.V
				}
			}
			digest = fn(data)
			inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
			inst.Dict["hexdigest"] = bi("hexdigest", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				return &object.Str{V: hex.EncodeToString(digest)}, nil
			})
			inst.Dict["digest"] = bi("digest", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				return &object.Bytes{V: digest}, nil
			})
			inst.Dict["update"] = bi("update", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				var more []byte
				switch v := a[0].(type) {
				case *object.Str:
					more = []byte(v.V)
				case *object.Bytes:
					more = v.V
				}
				digest = fn(append(append([]byte{}, digest...), more...))
				return object.None, nil
			})
			return inst, nil
		})
	}
	mk("md5", func(b []byte) []byte { h := md5.Sum(b); return h[:] })
	mk("sha1", func(b []byte) []byte { h := sha1.Sum(b); return h[:] })
	mk("sha256", func(b []byte) []byte { h := sha256.Sum256(b); return h[:] })
	return m
}

func modBase64(ip *Interp) *object.Module {
	m := newModule("base64")
	m.Dict["b64encode"] = bi("b64encode", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		var data []byte
		switch v := args[0].(type) {
		case *object.Bytes:
			data = v.V
		case *object.Str:
			data = []byte(v.V)
		}
		return &object.Bytes{V: []byte(base64.StdEncoding.EncodeToString(data))}, nil
	})
	m.Dict["b64decode"] = bi("b64decode", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		var s string
		switch v := args[0].(type) {
		case *object.Bytes:
			s = string(v.V)
		case *object.Str:
			s = v.V
		}
		data, err := base64.StdEncoding.DecodeString(s)
		if err != nil {
			ip.raise(ip.excType("ValueError"), "%v", err)
		}
		return &object.Bytes{V: data}, nil
	})
	m.Dict["b64decode_str"] = m.Dict["b64decode"]
	return m
}

// ==================== datetime ====================

func modDatetime(ip *Interp) *object.Module {
	m := newModule("datetime")
	dtCls := &object.TypeObj{Name: "datetime", Dict: map[string]object.Object{}}
	dtCls.Dict["__init__"] = bi("__init__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		now := time.Now()
		vals := []int64{int64(now.Year()), int64(now.Month()), int64(now.Day()), 0, 0, 0}
		for i := 1; i < len(args) && i <= 6; i++ {
			if n, ok := object.AsInt(args[i]); ok {
				vals[i-1] = n
			}
		}
		for i, name := range []string{"year", "month", "day", "hour", "minute", "second"} {
			self.Dict[name] = &object.Int{V: vals[i]}
		}
		return object.None, nil
	})
	dtCls.Dict["isoformat"] = bi("isoformat", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		get := func(n string) int64 {
			if v, ok := self.Dict[n]; ok {
				x, _ := object.AsInt(v)
				return x
			}
			return 0
		}
		return &object.Str{V: fmt.Sprintf("%04d-%02d-%02dT%02d:%02d:%02d",
			get("year"), get("month"), get("day"), get("hour"), get("minute"), get("second"))}, nil
	})
	dtCls.Dict["strftime"] = bi("strftime", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self := args[0].(*object.Instance)
		get := func(n string) int64 {
			if v, ok := self.Dict[n]; ok {
				x, _ := object.AsInt(v)
				return x
			}
			return 0
		}
		t := time.Date(int(get("year")), time.Month(get("month")), int(get("day")),
			int(get("hour")), int(get("minute")), int(get("second")), 0, time.Local)
		return &object.Str{V: t.Format(goTimeLayout(args[1].Str()))}, nil
	})
	dtCls.Dict["__str__"] = bi("__str__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		r, _ := dtCls.Dict["isoformat"].(*object.Builtin).Fn(args, kw)
		return r, nil
	})
	m.Dict["datetime"] = dtCls
	m.Dict["now"] = bi("now", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		inst := &object.Instance{Class: dtCls, Dict: map[string]object.Object{}}
		now := time.Now()
		inst.Dict["year"] = &object.Int{V: int64(now.Year())}
		inst.Dict["month"] = &object.Int{V: int64(now.Month())}
		inst.Dict["day"] = &object.Int{V: int64(now.Day())}
		inst.Dict["hour"] = &object.Int{V: int64(now.Hour())}
		inst.Dict["minute"] = &object.Int{V: int64(now.Minute())}
		inst.Dict["second"] = &object.Int{V: int64(now.Second())}
		return inst, nil
	})
	dateCls := &object.TypeObj{Name: "date", Dict: map[string]object.Object{}}
	dateCls.Dict["today"] = bi("today", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		inst := &object.Instance{Class: dateCls, Dict: map[string]object.Object{}}
		now := time.Now()
		inst.Dict["year"] = &object.Int{V: int64(now.Year())}
		inst.Dict["month"] = &object.Int{V: int64(now.Month())}
		inst.Dict["day"] = &object.Int{V: int64(now.Day())}
		return inst, nil
	})
	m.Dict["date"] = dateCls
	m.Dict["timedelta"] = &object.TypeObj{Name: "timedelta", Dict: map[string]object.Object{}}
	return m
}

// ==================== asyncio ====================

func modAsyncio(ip *Interp) *object.Module {
	m := newModule("asyncio")
	m.Dict["run"] = bi("run", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return object.None, nil
		}
		return ip.awaitValue(args[0]), nil
	})
	m.Dict["sleep"] = bi("sleep", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		sec := 0.0
		if len(args) > 0 {
			sec, _ = object.AsFloat(args[0])
		}
		return ip.newGenerator("<asyncio.sleep>", func(yield func(object.Object) object.Object) {
			if sec > 0 {
				time.Sleep(time.Duration(sec * float64(time.Second)))
			}
		}), nil
	})
	m.Dict["gather"] = bi("gather", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		// 真并发：先全部起 Task，再统一等待
		tasks := make([]*object.Task, 0, len(args))
		for i, a := range args {
			tasks = append(tasks, ip.spawnTask(a, fmt.Sprintf("task-%d", i)))
		}
		out := make([]object.Object, 0, len(tasks))
		for _, t := range tasks {
			<-t.Done
			if t.Err != nil {
				if pe, ok := t.Err.(*PyError); ok {
					panic(pe)
				}
				ip.RuntimeErr("%v", t.Err)
			}
			out = append(out, t.Result)
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["create_task"] = bi("create_task", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			ip.TypeError("create_task() missing coroutine")
		}
		name := "Task"
		return ip.spawnTask(args[0], name), nil
	})
	m.Dict["wait_for"] = bi("wait_for", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return object.None, nil
		}
		return ip.awaitValue(args[0]), nil
	})
	m.Dict["iscoroutine"] = bi("iscoroutine", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		switch args[0].(type) {
		case *object.Generator, *object.Task:
			return object.TrueObj, nil
		}
		return object.FalseObj, nil
	})
	return m
}

// spawnTask 在 goroutine 中驱动协程，返回 Task
func (ip *Interp) spawnTask(coro object.Object, name string) *object.Task {
	t := &object.Task{Name: name, Done: make(chan struct{})}
	if existing, ok := coro.(*object.Task); ok {
		return existing
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				if pe, ok := r.(*PyError); ok {
					t.Err = pe
				} else if _, isExit := r.(*exitSignal); isExit {
					t.Err = nil
				} else {
					t.Err = fmt.Errorf("%v", r)
				}
				t.Result = object.None
			}
			close(t.Done)
		}()
		t.Result = ip.awaitValue(coro)
	}()
	return t
}

// ==================== typing / dataclasses ====================

func modTyping(ip *Interp) *object.Module {
	m := newModule("typing")
	for _, n := range []string{"List", "Dict", "Tuple", "Set", "Optional", "Any", "Union", "Callable", "Iterable", "Iterator", "Sequence", "Mapping", "Type", "NamedTuple", "TYPE_CHECKING"} {
		m.Dict[n] = &object.TypeObj{Name: n, Dict: map[string]object.Object{}}
	}
	return m
}

func modDataclasses(ip *Interp) *object.Module {
	m := newModule("dataclasses")
	m.Dict["dataclass"] = bi("dataclass", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		var target object.Object
		if len(args) > 0 {
			if isCallableObj(args[0]) {
				target = args[0]
			}
		}
		wrap := func(cls object.Object) object.Object {
			t, ok := cls.(*object.TypeObj)
			if !ok {
				return cls
			}
			// 自动生成 __init__：优先用 __annotations__ 顺序
			fields := []string{}
			if ann, ok := t.Dict["__annotations__"].(*object.Dict); ok {
				for _, k := range ann.Keys {
					fields = append(fields, k.Str())
				}
			} else {
				for k := range t.Dict {
					if strings.HasPrefix(k, "_") {
						continue
					}
					fields = append(fields, k)
				}
				sort.Strings(fields)
			}
			t.Dict["__match_args__"] = func() object.Object {
				items := make([]object.Object, len(fields))
				for i, f := range fields {
					items[i] = &object.Str{V: f}
				}
				return &object.Tuple{Items: items}
			}()
			t.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				if len(a) == 0 {
					return object.None, nil
				}
				self, ok := a[0].(*object.Instance)
				if !ok {
					return object.None, nil
				}
				for i, f := range fields {
					if i+1 < len(a) {
						self.Dict[f] = a[i+1]
					} else if v, ok := k[f]; ok {
						self.Dict[f] = v
					} else if def, ok := t.Dict[f]; ok {
						if fs, isSpec := def.(*object.FieldSpec); isSpec {
							if fs.Factory != nil && fs.Factory != object.None {
								self.Dict[f] = ip.Call(fs.Factory, nil, nil)
							} else {
								self.Dict[f] = fs.Default
							}
						} else {
							self.Dict[f] = def
						}
					}
				}
				return object.None, nil
			})
			return t
		}
		if target != nil {
			return wrap(target), nil
		}
		return bi("dataclass_decorator", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return wrap(a[0]), nil
		}), nil
	})
	m.Dict["field"] = bi("field", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		fs := &object.FieldSpec{Default: object.None}
		if kw != nil {
			if v, ok := kw["default"]; ok {
				fs.Default = v
			}
			if v, ok := kw["default_factory"]; ok {
				fs.Factory = v
			}
		}
		return fs, nil
	})
	return m
}
