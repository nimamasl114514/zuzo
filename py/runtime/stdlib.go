package runtime

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"math"
	"math/big"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	registerStdlibPatch(ip)
}

// newModule 创建模块
func newModule(name string) *object.Module {
	// 每个模块必备 __doc__：CPython 的 operator.py 末尾就 `from _operator import __doc__`，
	// 缺它会让 import operator 整体失败（urllib3 → importlib.metadata → operator 链实锤）。
	return &object.Module{Name: name, Dict: map[string]object.Object{
		"__doc__":  object.None,
		"__name__": &object.Str{V: name},
	}}
}

// bi 把 Go 函数包装为内置
func bi(name string, fn object.BuiltinFn) *object.Builtin {
	return &object.Builtin{Name: name, Fn: fn}
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
	// monotonic / monotonic_ns / process_time：queue.py:7 `from time import monotonic as time`。
	m.Dict["monotonic"] = bi("monotonic", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Float{V: float64(time.Since(start).Nanoseconds()) / 1e9}, nil
	})
	m.Dict["monotonic_ns"] = bi("monotonic_ns", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Int{V: time.Since(start).Nanoseconds()}, nil
	})
	m.Dict["perf_counter_ns"] = bi("perf_counter_ns", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Int{V: time.Since(start).Nanoseconds()}, nil
	})
	m.Dict["process_time"] = m.Dict["monotonic"]
	m.Dict["thread_time"] = m.Dict["monotonic"]
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
	// JSONDecodeError：requests/compat.py:57 模块级 `from json import JSONDecodeError`，
	// requests/exceptions.py:31 直接拿它当基类。CPython 里是 ValueError 子类。
	jsonDecodeErr := &object.TypeObj{
		Name:  "JSONDecodeError",
		Bases: []*object.TypeObj{ip.excType("ValueError")},
		Dict:  map[string]object.Object{},
		IsExc: true,
	}
	m.Dict["JSONDecodeError"] = jsonDecodeErr
	m.Dict["decoder"] = &object.Module{Name: "json.decoder", Dict: map[string]object.Object{"JSONDecodeError": jsonDecodeErr}}
	m.Dict["encoder"] = &object.Module{Name: "json.encoder", Dict: map[string]object.Object{}}
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
		p := pyRegexToGo(pattern)
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
			// 语法不支持（前瞻/反向引用等）：降级为永不匹配，不中断调用链
			return neverMatch()
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
		return &object.List{Items: reFindallItems(re, s, -1)}, nil
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
			return &object.List{Items: reFindallItems(re, a[0].Str(), -1)}, nil
		})
		inst.Dict["sub"] = bi("sub", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			repl := a[0].Str()
			s := a[1].Str()
			return &object.Str{V: re.ReplaceAllString(s, repl)}, nil
		})
		// http/client.py:144 用 Pattern.fullmatch 做 token 校验
		inst.Dict["fullmatch"] = bi("fullmatch", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			s := a[0].Str()
			loc := re.FindStringSubmatchIndex(s)
			if loc == nil || loc[0] != 0 || loc[1] != len(s) {
				return object.None, nil
			}
			return makeMatch(ip, re, s, loc), nil
		})
		inst.Dict["finditer"] = bi("finditer", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return finditerList(ip, re, a[0].Str()), nil
		})
		inst.Dict["split"] = bi("split", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			s := a[0].Str()
			parts := re.Split(s, -1)
			items := make([]object.Object, len(parts))
			for i, p := range parts {
				items[i] = &object.Str{V: p}
			}
			return &object.List{Items: items}, nil
		})
		inst.Dict["subn"] = bi("subn", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			repl := a[0].Str()
			src := a[1].Str()
			out := re.ReplaceAllString(src, repl)
			n := len(re.FindAllStringIndex(src, -1))
			return &object.Tuple{Items: []object.Object{&object.Str{V: out}, &object.Int{V: int64(n)}}}, nil
		})
		inst.Dict["flags"] = &object.Int{V: flags}
		inst.Dict["groups"] = &object.Int{V: int64(re.NumSubexp())}
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
	// 常用常量（io.py / urllib3 的磁盘缓存都会用）
	m.Dict["SEEK_SET"] = &object.Int{V: 0}
	m.Dict["SEEK_CUR"] = &object.Int{V: 1}
	m.Dict["SEEK_END"] = &object.Int{V: 2}
	m.Dict["O_RDONLY"] = &object.Int{V: 0}
	m.Dict["O_WRONLY"] = &object.Int{V: 1}
	m.Dict["O_RDWR"] = &object.Int{V: 2}
	m.Dict["O_CREAT"] = &object.Int{V: 64}
	m.Dict["O_TRUNC"] = &object.Int{V: 512}
	m.Dict["O_APPEND"] = &object.Int{V: 1024}
	m.Dict["O_BINARY"] = &object.Int{V: 32768}
	m.Dict["F_OK"] = &object.Int{V: 0}
	m.Dict["R_OK"] = &object.Int{V: 4}
	m.Dict["W_OK"] = &object.Int{V: 2}
	m.Dict["X_OK"] = &object.Int{V: 1}
	// os.PathLike / fspath：pathlib 与大量库用它做路径协议判断
	pathLikeCls := &object.TypeObj{Name: "PathLike", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	m.Dict["PathLike"] = pathLikeCls
	// PathLike.register(cls)：CPython 里 os.PathLike 是 ABCMeta，register 记虚拟子类。
	// pathlib.py:798 模块级 `os.PathLike.register(PurePath)` 必须过，否则 pathlib 整体导入失败。
	pathLikeCls.Dict["register"] = bi("register", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		target := object.Object(object.None)
		for _, x := range a {
			if t, ok := x.(*object.TypeObj); ok && t != pathLikeCls {
				target = t
			}
		}
		if target != object.None {
			lst, _ := pathLikeCls.Dict["_virtual_subs"].(*object.List)
			if lst == nil {
				lst = &object.List{}
				pathLikeCls.Dict["_virtual_subs"] = lst
			}
			lst.Items = append(lst.Items, target)
		}
		return target, nil
	})
	m.Dict["fspath"] = bi("fspath", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Str{V: ""}, nil
		}
		switch v := a[0].(type) {
		case *object.Str:
			return v, nil
		case *object.Bytes:
			return v, nil
		case *object.Instance:
			if m2, ok := ip.findDunder(v.Class, "__fspath__"); ok {
				return ip.Call(ip.bindMethod(v, m2), nil, nil), nil
			}
		}
		ip.TypeError("expected str, bytes or os.PathLike object, not '%s'", object.TypeName(a[0]))
		return object.None, nil
	})
	m.Dict["fsencode"] = bi("fsencode", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Bytes{}, nil
		}
		return &object.Bytes{V: []byte(a[0].Str())}, nil
	})
	m.Dict["fsdecode"] = bi("fsdecode", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Str{V: ""}, nil
		}
		switch v := a[0].(type) {
		case *object.Bytes:
			return &object.Str{V: string(v.V)}, nil
		}
		return &object.Str{V: a[0].Str()}, nil
	})
	m.Dict["getcwd"] = bi("getcwd", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d, _ := os.Getwd()
		return &object.Str{V: d}, nil
	})
	m.Dict["listdir"] = bi("listdir", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		dir := "."
		if len(args) > 0 {
			dir = osPathArg(ip, args[0])
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
		err := os.MkdirAll(osPathArg(ip, args[0]), 0o755)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return object.None, nil
	})
	m.Dict["mkdir"] = bi("mkdir", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if err := os.Mkdir(osPathArg(ip, args[0]), 0o755); err != nil {
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
		if err := os.Remove(osPathArg(ip, args[0])); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return object.None, nil
	})
	m.Dict["rename"] = bi("rename", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if err := os.Rename(osPathArg(ip, args[0]), osPathArg(ip, args[1])); err != nil {
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
	// 平台标识与路径常量：pathlib.py:1420 / ntpath / posixpath 模块级全靠
	if isWindows() {
		m.Dict["name"] = &object.Str{V: "nt"}
		m.Dict["altsep"] = &object.Str{V: "/"}
		m.Dict["pathsep"] = &object.Str{V: ";"}
		m.Dict["devnull"] = &object.Str{V: "nul"}
	} else {
		m.Dict["name"] = &object.Str{V: "posix"}
		m.Dict["altsep"] = object.None
		m.Dict["pathsep"] = &object.Str{V: ":"}
		m.Dict["devnull"] = &object.Str{V: "/dev/null"}
	}
	m.Dict["curdir"] = &object.Str{V: "."}
	m.Dict["pardir"] = &object.Str{V: ".."}
	m.Dict["extsep"] = &object.Str{V: "."}
	m.Dict["getpid"] = bi("getpid", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(os.Getpid())}, nil
	})
	m.Dict["urandom"] = bi("urandom", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		n, _ := object.AsInt(a[0])
		if n < 0 {
			ip.TypeError("negative argument not allowed")
		}
		b := make([]byte, n)
		if _, err := rand.Read(b); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return &object.Bytes{V: b}, nil
	})
	m.Dict["access"] = bi("access", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := osPathArg(ip, a[0])
		_, err := os.Stat(p)
		return object.GetBool(err == nil), nil
	})
	m.Dict["cpu_count"] = bi("cpu_count", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(runtime.NumCPU())}, nil
	})
	// os.stat_result / os.stat / os.lstat：CPython 的 ntpath.py / posixpath.py / pathlib.py 全靠它
	// （ntpath.py:283 `hasattr(os.stat_result, 'st_reparse_tag')` 是模块导入期的硬依赖）
	srCls := ensureStatResultCls(ip)
	m.Dict["stat_result"] = srCls
	m.Dict["supports_follow_symlinks"] = &object.Set{}
	m.Dict["supports_dir_fd"] = &object.Set{}
	m.Dict["supports_fd"] = &object.Set{}
	m.Dict["stat"] = bi("stat", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return osStatPath(ip, a, true)
	})
	m.Dict["lstat"] = bi("lstat", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return osStatPath(ip, a, false)
	})
	m.Dict["path"] = modOSPath(ip)
	return m
}

// ==================== os.stat_result ====================

var statResultCls *object.TypeObj

// ensureStatResultCls 惰性构造 os.stat_result：基类为 tuple（CPython 里它可以按位置索引），
// 属性面提供 st_* 字段。故意不提供 st_reparse_tag —— 无 junction 支持，
// 让 ntpath.py:283 的 hasattr 走 else 分支（isjunction 恒 False）。
func ensureStatResultCls(ip *Interp) *object.TypeObj {
	if statResultCls != nil {
		return statResultCls
	}
	c := &object.TypeObj{Name: "stat_result", Bases: []*object.TypeObj{object.TupleType}, Dict: map[string]object.Object{}}
	// CPython 元组索引口径：0 st_mode 1 st_ino 2 st_dev 3 st_nlink 4 st_uid 5 st_gid
	// 6 st_size 7 st_atime 8 st_mtime 9 st_ctime
	c.Dict["__getitem__"] = bi("__getitem__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst, ok := a[0].(*object.Instance)
		if !ok {
			return object.None, nil
		}
		n, _ := object.AsInt(a[1])
		order := []string{"st_mode", "st_ino", "st_dev", "st_nlink", "st_uid", "st_gid", "st_size", "st_atime", "st_mtime", "st_ctime"}
		if n < 0 || int(n) >= len(order) {
			ip.raise(ip.excType("IndexError"), "tuple index out of range")
		}
		if v, ok2 := inst.Dict[order[n]]; ok2 {
			return v, nil
		}
		return &object.Int{V: 0}, nil
	})
	c.Dict["__repr__"] = bi("__repr__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst, _ := a[0].(*object.Instance)
		g := func(n string) int64 {
			if inst != nil {
				if v, ok := inst.Dict[n].(*object.Int); ok {
					return v.V
				}
			}
			return 0
		}
		return &object.Str{V: fmt.Sprintf("os.stat_result(st_mode=%d, st_size=%d)", g("st_mode"), g("st_size"))}, nil
	})
	statResultCls = c
	return c
}

// newStatResult 把 Go 的 os.FileInfo 包成 os.stat_result 实例。
func newStatResult(ip *Interp, fi os.FileInfo) *object.Instance {
	inst := &object.Instance{Class: ensureStatResultCls(ip), Dict: map[string]object.Object{}}
	mode := int64(0o100000) // S_IFREG
	if fi.IsDir() {
		mode = 0o040000 // S_IFDIR
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		mode = 0o120000 // S_IFLNK
	}
	mode |= int64(fi.Mode().Perm())
	t := fi.ModTime().Unix()
	inst.Dict["st_mode"] = &object.Int{V: mode}
	inst.Dict["st_ino"] = &object.Int{V: 0}
	inst.Dict["st_dev"] = &object.Int{V: 0}
	inst.Dict["st_nlink"] = &object.Int{V: 1}
	inst.Dict["st_uid"] = &object.Int{V: 0}
	inst.Dict["st_gid"] = &object.Int{V: 0}
	inst.Dict["st_size"] = &object.Int{V: fi.Size()}
	inst.Dict["st_atime"] = &object.Float{V: float64(t)}
	inst.Dict["st_mtime"] = &object.Float{V: float64(t)}
	inst.Dict["st_ctime"] = &object.Float{V: float64(t)}
	inst.Dict["st_atime_ns"] = &object.Int{V: fi.ModTime().UnixNano()}
	inst.Dict["st_mtime_ns"] = &object.Int{V: fi.ModTime().UnixNano()}
	inst.Dict["st_ctime_ns"] = &object.Int{V: fi.ModTime().UnixNano()}
	return inst
}

// osStatPath 实现 os.stat / os.lstat 共用的路径解析与错误映射。
func osStatPath(ip *Interp, a []object.Object, follow bool) (object.Object, error) {
	if len(a) == 0 {
		ip.TypeError("stat() takes at least 1 argument")
	}
	p := osPathArg(ip, a[0])
	var (
		fi  os.FileInfo
		err error
	)
	if follow {
		fi, err = os.Stat(p)
	} else {
		fi, err = os.Lstat(p)
	}
	if err != nil {
		if os.IsNotExist(err) {
			ip.raise(ip.excType("FileNotFoundError"), "[WinError 2] %v: %q", err, p)
		}
		if os.IsPermission(err) {
			ip.raise(ip.excType("PermissionError"), "[WinError 5] %v: %q", err, p)
		}
		ip.raise(ip.excType("OSError"), "%v: %q", err, p)
	}
	return newStatResult(ip, fi), nil
}

// osPathArg 把 str/bytes/PathLike 统一解成 Go 字符串。
func osPathArg(ip *Interp, o object.Object) string {
	switch v := o.(type) {
	case *object.Bytes:
		return string(v.V)
	case *object.Instance:
		if m2, ok := ip.findDunder(v.Class, "__fspath__"); ok {
			if r := ip.Call(ip.bindMethod(v, m2), nil, nil); r != nil {
				return r.Str()
			}
		}
	}
	return o.Str()
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
		_, err := os.Stat(osPathArg(ip, args[0]))
		return object.GetBool(err == nil), nil
	})
	m.Dict["isfile"] = bi("isfile", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		st, err := os.Stat(osPathArg(ip, args[0]))
		return object.GetBool(err == nil && !st.IsDir()), nil
	})
	m.Dict["isdir"] = bi("isdir", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		st, err := os.Stat(osPathArg(ip, args[0]))
		return object.GetBool(err == nil && st.IsDir()), nil
	})
	m.Dict["basename"] = bi("basename", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: filepath.Base(osPathArg(ip, args[0]))}, nil
	})
	m.Dict["dirname"] = bi("dirname", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: filepath.Dir(osPathArg(ip, args[0]))}, nil
	})
	m.Dict["abspath"] = bi("abspath", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p, _ := filepath.Abs(osPathArg(ip, args[0]))
		return &object.Str{V: p}, nil
	})
	m.Dict["split"] = bi("split", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		d, f := filepath.Split(osPathArg(ip, args[0]))
		return &object.Tuple{Items: []object.Object{&object.Str{V: d}, &object.Str{V: f}}}, nil
	})
	m.Dict["splitext"] = bi("splitext", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p := osPathArg(ip, args[0])
		ext := filepath.Ext(p)
		return &object.Tuple{Items: []object.Object{&object.Str{V: strings.TrimSuffix(p, ext)}, &object.Str{V: ext}}}, nil
	})
	m.Dict["isabs"] = bi("isabs", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return object.GetBool(filepath.IsAbs(osPathArg(ip, args[0]))), nil
	})
	m.Dict["normpath"] = bi("normpath", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: filepath.Clean(osPathArg(ip, args[0]))}, nil
	})
	m.Dict["relpath"] = bi("relpath", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		base := "."
		if len(args) > 1 {
			base = args[1].Str()
		}
		r, err := filepath.Rel(base, osPathArg(ip, args[0]))
		if err != nil {
			ip.raise(ip.excType("ValueError"), "%v", err)
		}
		return &object.Str{V: r}, nil
	})
	m.Dict["realpath"] = bi("realpath", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p, err := filepath.Abs(osPathArg(ip, args[0]))
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
		st, err := os.Stat(osPathArg(ip, args[0]))
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return &object.Int{V: st.Size()}, nil
	})
	// 平台常量与语义函数：pathlib.py:391 `os.path.sep` 是模块级 _flavour 的硬依赖
	if isWindows() {
		m.Dict["sep"] = &object.Str{V: "\\"}
		m.Dict["altsep"] = &object.Str{V: "/"}
		m.Dict["pathsep"] = &object.Str{V: ";"}
		m.Dict["devnull"] = &object.Str{V: "nul"}
	} else {
		m.Dict["sep"] = &object.Str{V: "/"}
		m.Dict["altsep"] = object.None
		m.Dict["pathsep"] = &object.Str{V: ":"}
		m.Dict["devnull"] = &object.Str{V: "/dev/null"}
	}
	m.Dict["extsep"] = &object.Str{V: "."}
	m.Dict["curdir"] = &object.Str{V: "."}
	m.Dict["pardir"] = &object.Str{V: ".."}
	m.Dict["defpath"] = &object.Str{V: ".;C:\\bin"}
	// splitdrive：Windows 下认 “C:” 盘符与 UNC “\\\\server\\share”
	m.Dict["splitdrive"] = bi("splitdrive", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p := args[0].Str()
		if !isWindows() {
			return &object.Tuple{Items: []object.Object{&object.Str{V: ""}, &object.Str{V: p}}}, nil
		}
		if len(p) >= 2 && p[1] == ':' && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) {
			return &object.Tuple{Items: []object.Object{&object.Str{V: p[:2]}, &object.Str{V: p[2:]}}}, nil
		}
		if len(p) >= 2 && (p[0] == '\\' || p[0] == '/') && (p[1] == '\\' || p[1] == '/') {
			// UNC：前两段 \\server\share
			var segs []int
			for i := 2; i < len(p); i++ {
				if p[i] == '\\' || p[i] == '/' {
					segs = append(segs, i)
				}
			}
			if len(segs) >= 2 {
				return &object.Tuple{Items: []object.Object{&object.Str{V: p[:segs[1]]}, &object.Str{V: p[segs[1]:]}}}, nil
			}
		}
		return &object.Tuple{Items: []object.Object{&object.Str{V: ""}, &object.Str{V: p}}}, nil
	})
	// normcase：Windows 下小写 + 正斜杠转反斜杠
	m.Dict["normcase"] = bi("normcase", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		p := args[0].Str()
		if isWindows() {
			p = strings.ToLower(strings.ReplaceAll(p, "/", "\\"))
		}
		return &object.Str{V: p}, nil
	})
	m.Dict["islink"] = bi("islink", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		st, err := os.Lstat(args[0].Str())
		return object.GetBool(err == nil && st.Mode()&os.ModeSymlink != 0), nil
	})
	m.Dict["samefile"] = bi("samefile", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		a, err1 := os.Stat(args[0].Str())
		b, err2 := os.Stat(args[1].Str())
		return object.GetBool(err1 == nil && err2 == nil && os.SameFile(a, b)), nil
	})
	mkTime := func(name string, get func(os.FileInfo) int64) {
		m.Dict[name] = bi(name, func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
			st, err := os.Stat(args[0].Str())
			if err != nil {
				ip.raise(ip.excType("OSError"), "%v", err)
			}
			return &object.Float{V: float64(get(st))}, nil
		})
	}
	mkTime("getmtime", func(st os.FileInfo) int64 { return st.ModTime().Unix() })
	mkTime("getatime", func(st os.FileInfo) int64 { return st.ModTime().Unix() })
	mkTime("getctime", func(st os.FileInfo) int64 { return st.ModTime().Unix() })
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
	if ip.SysPathList == nil {
		ip.initSysPath()
	}
	// path 与 modules 都必须是「活对象」：Python 侧 append / 赋值立即生效
	m.Dict["path"] = ip.SysPathList
	if ip.SysModules == nil {
		ip.SysModules = &object.Dict{}
	}
	m.Dict["modules"] = ip.SysModules
	// sys.version_info 是命名元组（有 .major/.minor/.micro 属性，也能下标/切片）
	viCls := &object.TypeObj{Name: "version_info", Dict: map[string]object.Object{}}
	ip.buildNamedTuple(viCls, []string{"major", "minor", "micro", "releaselevel", "serial"})
	vi := &object.Instance{Class: viCls, Dict: map[string]object.Object{
		"major":        &object.Int{V: 3},
		"minor":        &object.Int{V: 11},
		"micro":        &object.Int{V: 0},
		"releaselevel": &object.Str{V: "final"},
		"serial":       &object.Int{V: 0},
	}}
	m.Dict["version_info"] = vi
	m.Dict["version"] = &object.Str{V: "3.11.0 (zuzo, Python-compatible runtime)"}
	m.Dict["platform"] = &object.Str{V: "win32"}
	m.Dict["maxsize"] = &object.Int{V: 9223372036854775807}
	m.Dict["float_info"] = &object.Instance{Class: &object.TypeObj{Name: "float_info", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{
		"max": &object.Float{V: 1.7976931348623157e308}, "min": &object.Float{V: 2.2250738585072014e-308},
		"epsilon": &object.Float{V: 2.220446049250313e-16}, "dig": &object.Int{V: 15},
		"mant_dig": &object.Int{V: 53}, "max_exp": &object.Int{V: 1024}, "min_exp": &object.Int{V: -1021},
		"max_10_exp": &object.Int{V: 308}, "min_10_exp": &object.Int{V: -307},
		"radix": &object.Int{V: 2}, "rounds": &object.Int{V: 1},
	}}
	m.Dict["int_info"] = &object.Instance{Class: &object.TypeObj{Name: "int_info", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{
		"bits_per_digit": &object.Int{V: 30}, "sizeof_digit": &object.Int{V: 4},
		"default_max_str_digits": &object.Int{V: 4300}, "str_digits_check_threshold": &object.Int{V: 640},
	}}
	m.Dict["hash_info"] = &object.Instance{Class: &object.TypeObj{Name: "hash_info", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{
		"width": &object.Int{V: 64}, "modulus": &object.Int{V: 2305843009213693951},
		"inf": &object.Int{V: 314159}, "nan": &object.Int{V: 0},
		"imag": &object.Int{V: 1000003}, "algorithm": &object.Str{V: "siphash13"},
		"hash_bits": &object.Int{V: 64}, "seed_bits": &object.Int{V: 128},
		"cutoff": &object.Int{V: 0},
	}}
	m.Dict["hexversion"] = &object.Int{V: 0x030b00f0}
	m.Dict["byteorder"] = &object.Str{V: "little"}
	m.Dict["prefix"] = &object.Str{V: ip.Cwd}
	m.Dict["base_prefix"] = &object.Str{V: ip.Cwd}
	m.Dict["executable"] = &object.Str{V: "zuzo"}
	m.Dict["dont_write_bytecode"] = &object.Bool{V: true}
	m.Dict["warnoptions"] = &object.List{}
	m.Dict["argv"] = &object.List{Items: []object.Object{&object.Str{V: ip.MainFile}}}
	m.Dict["flags"] = &object.Instance{Class: &object.TypeObj{Name: "flags", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{}}
	impl := &object.Instance{Class: &object.TypeObj{Name: "Namespace", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{}}
	impl.Dict["name"] = &object.Str{V: "zuzo"}
	impl.Dict["cache_tag"] = &object.Str{V: "zuzo-311"}
	m.Dict["implementation"] = impl
	m.Dict["getrecursionlimit"] = bi("getrecursionlimit", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(ip.MaxDepth)}, nil
	})
	m.Dict["setrecursionlimit"] = bi("setrecursionlimit", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if n, ok := object.AsInt(args[0]); ok {
			ip.MaxDepth = int(n)
		}
		return object.None, nil
	})
	// sys.stdout / sys.stderr：logging.StreamHandler.emit（logging/__init__.py:1027）
	// 直接 self.stream.write(...)，旧占位 Str 会导致 AttributeError。
	m.Dict["stdout"] = newTextStream(ip, ip.Stdout, 1)
	m.Dict["stderr"] = newTextStream(ip, ip.Stderr, 2)
	m.Dict["__stdout__"] = m.Dict["stdout"]
	m.Dict["__stderr__"] = m.Dict["stderr"]
	m.Dict["stdin"] = newTextStream(ip, io.Discard, 0)
	// sys.intern：pathlib.py:404 `sys.intern(str(x))` 是 _parse_path 热路径，
	// Go 字符串不可变，直接原样返回即语义等价。
	m.Dict["intern"] = bi("intern", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			ip.TypeError("intern() takes exactly one argument")
		}
		if _, ok := args[0].(*object.Str); !ok {
			ip.TypeError("intern() argument must be str, not %s", object.TypeName(args[0]))
		}
		return args[0], nil
	})
	m.Dict["getdefaultencoding"] = bi("getdefaultencoding", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: "utf-8"}, nil
	})
	m.Dict["getfilesystemencoding"] = bi("getfilesystemencoding", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Str{V: "utf-8"}, nil
	})
	m.Dict["getsizeof"] = bi("getsizeof", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Int{V: 64}, nil
	})
	m.Dict["float_repr_style"] = &object.Str{V: "short"}
	m.Dict["abiflags"] = &object.Str{V: ""}
	return m
}

// newTextStream 构造最小 TextIOWrapper 兼容对象（write / flush / isatty / fileno / writable）。
// 仅供 sys.stdout / sys.stderr 使用：logging / urllib3 只会调 write 与 flush。
func newTextStream(ip *Interp, w io.Writer, fd int) *object.Instance {
	cls := &object.TypeObj{Name: "TextIOWrapper", Dict: map[string]object.Object{}}
	inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
	inst.Dict["write"] = bi("write", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			w.Write([]byte(a[0].Str()))
		}
		return &object.Int{V: 0}, nil
	})
	inst.Dict["flush"] = bi("flush", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	inst.Dict["close"] = inst.Dict["flush"]
	inst.Dict["writable"] = bi("writable", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.GetBool(true), nil
	})
	inst.Dict["readable"] = bi("readable", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.GetBool(false), nil
	})
	inst.Dict["isatty"] = bi("isatty", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.GetBool(false), nil
	})
	inst.Dict["fileno"] = bi("fileno", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(fd)}, nil
	})
	return inst
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
	m.Dict["Formatter"] = modStringFormatter(ip)
	m.Dict["Template"] = modStringTemplate(ip)
	return m
}

// modStringFormatter 实现 string.Formatter 的 parse/format 面。
// CPython 3.13+ 的 logging/__init__.py:30 模块级 `from string import Formatter`（别名 StrFormatter），
// 432 行就 `StrFormatter()` 实例化，parse() 用于拆 %(name)s 模板——缺了就整条 logging 链断。
func modStringFormatter(ip *Interp) *object.TypeObj {
	cls := &object.TypeObj{Name: "Formatter", Dict: map[string]object.Object{}}
	cls.Dict["parse"] = bi("parse", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		s := ""
		if len(a) > 1 {
			s = a[1].Str()
		} else if len(a) > 0 {
			s = a[0].Str()
		}
		return formatterParse(s), nil
	})
	cls.Dict["get_value"] = bi("get_value", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		key := ""
		args := object.Object(object.None)
		kwargs := object.Object(object.None)
		if len(a) > 1 {
			key = a[1].Str()
		}
		if len(a) > 2 {
			args = a[2]
		}
		if len(a) > 3 {
			kwargs = a[3]
		}
		if d, ok := kwargs.(*object.Dict); ok {
			if v, ok2 := d.Get(&object.Str{V: key}); ok2 {
				return v, nil
			}
		}
		if n, err := strconv.Atoi(key); err == nil {
			if lst := ip.ToSlice(args); n >= 0 && n < len(lst) {
				return lst[n], nil
			}
		}
		ip.raise(ip.excType("KeyError"), "%s", key)
		return object.None, nil
	})
	cls.Dict["vformat"] = bi("vformat", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("vformat() missing format string")
		}
		return &object.Str{V: a[1].Str()}, nil
	})
	cls.Dict["format"] = bi("format", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("format() missing format string")
		}
		return &object.Str{V: a[1].Str()}, nil
	})
	return cls
}

// formatterParse 按 CPython string.Formatter.parse 口径拆解格式串：
// 返回 [(literal_text, field_name, format_spec, conversion), ...]。
func formatterParse(s string) *object.List {
	var out []object.Object
	var lit strings.Builder
	flush := func(field, spec, conv object.Object) {
		out = append(out, &object.Tuple{Items: []object.Object{&object.Str{V: lit.String()}, field, spec, conv}})
		lit.Reset()
	}
	i := 0
	for i < len(s) {
		c := s[i]
		if c == '{' {
			if i+1 < len(s) && s[i+1] == '{' {
				lit.WriteByte('{')
				i += 2
				continue
			}
			depth := 1
			j := i + 1
			for j < len(s) && depth > 0 {
				switch s[j] {
				case '{':
					depth++
				case '}':
					depth--
				}
				j++
			}
			if depth != 0 {
				lit.WriteString(s[i:])
				break
			}
			name, spec, conv := splitFormatterField(s[i+1 : j-1])
			flush(strOrNone(name), strOrNone(spec), strOrNone(conv))
			i = j
			continue
		}
		if c == '}' && i+1 < len(s) && s[i+1] == '}' {
			lit.WriteByte('}')
			i += 2
			continue
		}
		lit.WriteByte(c)
		i++
	}
	out = append(out, &object.Tuple{Items: []object.Object{&object.Str{V: lit.String()}, object.None, object.None, object.None}})
	return &object.List{Items: out}
}

// splitFormatterField 把 `name!conv:spec` 拆成三段（spec 里允许嵌套大括号）。
func splitFormatterField(inner string) (name, spec, conv string) {
	hasSpec, hasConv := false, false
	depth := 0
	nameEnd := len(inner)
	for i := 0; i < len(inner); i++ {
		switch inner[i] {
		case '{', '[':
			depth++
		case '}', ']':
			depth--
		case '!':
			if depth == 0 && !hasSpec && !hasConv && i+1 < len(inner) && inner[i+1] != '=' {
				nameEnd = i
				hasConv = true
			}
		case ':':
			if depth == 0 && !hasSpec {
				if !hasConv {
					nameEnd = i
				}
				hasSpec = true
			}
		}
	}
	name = inner[:nameEnd]
	rest := inner[nameEnd:]
	if hasConv && strings.HasPrefix(rest, "!") {
		end := strings.IndexByte(rest, ':')
		if end < 0 {
			conv = rest[1:]
			return
		}
		conv = rest[1:end]
		rest = rest[end:]
	}
	if hasSpec && strings.HasPrefix(rest, ":") {
		spec = rest[1:]
	}
	return
}

// modStringTemplate 实现 string.Template（子类可覆盖 delimiter/idpattern/braceidpattern）。
func modStringTemplate(ip *Interp) *object.TypeObj {
	cls := &object.TypeObj{Name: "Template", Dict: map[string]object.Object{}}
	cls.Dict["delimiter"] = &object.Str{V: "$"}
	cls.Dict["idpattern"] = &object.Str{V: "(?a:[_a-z][_a-z0-9]*)"}
	cls.Dict["braceidpattern"] = object.None
	cls.Dict["pattern"] = object.None
	cls.Dict["flags"] = &object.Int{V: 0}
	cls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 1 {
			if inst, ok := a[0].(*object.Instance); ok {
				inst.Dict["template"] = a[1]
			}
		}
		return object.None, nil
	})
	cls.Dict["substitute"] = bi("substitute", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("substitute() missing mapping")
		}
		inst, _ := a[0].(*object.Instance)
		tmpl := ""
		if inst != nil {
			tmpl = inst.Dict["template"].Str()
		}
		return &object.Str{V: renderTemplate(ip, tmpl, a[1])}, nil
	})
	cls.Dict["safe_substitute"] = cls.Dict["substitute"]
	return cls
}

// renderTemplate 做 $name / ${name} / $$ 替换，未命中键保留原样（safe 语义）。
func renderTemplate(ip *Interp, tmpl string, mapping object.Object) string {
	get := func(name string) (string, bool) {
		switch m := mapping.(type) {
		case *object.Dict:
			if v, ok := m.Get(&object.Str{V: name}); ok {
				return v.Str(), true
			}
		case *object.Instance:
			if v, ok := m.Dict[name]; ok {
				return v.Str(), true
			}
		}
		return "", false
	}
	var sb strings.Builder
	for i := 0; i < len(tmpl); i++ {
		if tmpl[i] != '$' {
			sb.WriteByte(tmpl[i])
			continue
		}
		if i+1 < len(tmpl) && tmpl[i+1] == '$' {
			sb.WriteByte('$')
			i++
			continue
		}
		j := i + 1
		brace := false
		if j < len(tmpl) && tmpl[j] == '{' {
			brace = true
			j++
		}
		start := j
		for j < len(tmpl) {
			c := tmpl[j]
			if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' {
				j++
				continue
			}
			break
		}
		name := tmpl[start:j]
		if brace {
			if j < len(tmpl) && tmpl[j] == '}' {
				j++
			} else {
				sb.WriteString(tmpl[i:j])
				i = j - 1
				continue
			}
		}
		if v, ok := get(name); ok {
			sb.WriteString(v)
		} else {
			sb.WriteString(tmpl[i:j])
		}
		i = j - 1
	}
	return sb.String()
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
	// OrderedDict：requests/sessions.py:228 `self.adapters = OrderedDict()` 之后
	// 紧接着 `for k in self.adapters` 迭代键。旧占位类（仅 Base=DictType 的 TypeObj）
	// 造出的是裸 Instance，没有 __iter__，报 'OrderedDict' object is not iterable。
	// 这里直接返回真 Dict（Go map 不保序，而 object.Dict 用 Keys/Values 双切片天然保序），
	// 索引 / 迭代 / len / keys / items 全部走 Dict 既有语义。
	m.Dict["OrderedDict"] = &object.Builtin{Name: "OrderedDict", Fn: func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		d := &object.Dict{}
		if len(a) > 0 {
			switch src := a[0].(type) {
			case *object.Dict:
				for i, kk := range src.Keys {
					if i < len(src.Vals) {
						d.Set(kk, src.Vals[i])
					}
				}
			default:
				// 其它可迭代：键值对序列
				for _, it := range ip.ToSlice(a[0]) {
					pair := ip.ToSlice(it)
					if len(pair) >= 2 {
						d.Set(pair[0], pair[1])
					}
				}
			}
		}
		return d, nil
	}}
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
	// takewhile / dropwhile / filterfalse / compress / starmap / accumulate / zip_longest / tee
	// urllib3/util/retry.py:9 模块级 `from itertools import takewhile`。
	m.Dict["takewhile"] = bi("takewhile", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var out []object.Object
		for _, x := range ip.ToSlice(a[1]) {
			r := ip.Call(a[0], []object.Object{x}, nil)
			if !object.Truthy(r) {
				break
			}
			out = append(out, x)
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["dropwhile"] = bi("dropwhile", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var out []object.Object
		dropping := true
		for _, x := range ip.ToSlice(a[1]) {
			if dropping {
				r := ip.Call(a[0], []object.Object{x}, nil)
				if object.Truthy(r) {
					continue
				}
				dropping = false
			}
			out = append(out, x)
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["filterfalse"] = bi("filterfalse", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var out []object.Object
		for _, x := range ip.ToSlice(a[1]) {
			r := ip.Call(a[0], []object.Object{x}, nil)
			if !object.Truthy(r) {
				out = append(out, x)
			}
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["compress"] = bi("compress", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		data := ip.ToSlice(a[0])
		sel := ip.ToSlice(a[1])
		var out []object.Object
		for i, x := range data {
			if i < len(sel) && object.Truthy(sel[i]) {
				out = append(out, x)
			}
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["starmap"] = bi("starmap", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var out []object.Object
		for _, x := range ip.ToSlice(a[1]) {
			out = append(out, ip.Call(a[0], ip.ToSlice(x), nil))
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["accumulate"] = bi("accumulate", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(a[0])
		var out []object.Object
		var acc object.Object
		for i, x := range items {
			if i == 0 {
				acc = x
			} else if len(a) > 1 && a[1] != object.None {
				acc = ip.Call(a[1], []object.Object{acc, x}, nil)
			} else {
				acc = ip.BinOp("+", acc, x)
			}
			out = append(out, acc)
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["zip_longest"] = bi("zip_longest", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		fill := object.Object(object.None)
		var seqs []object.Object
		for _, x := range a {
			seqs = append(seqs, x)
		}
		if k != nil {
			if v, ok := k["fillvalue"]; ok {
				fill = v
			}
		}
		maxLen := 0
		slices := make([][]object.Object, len(seqs))
		for i, sq := range seqs {
			slices[i] = ip.ToSlice(sq)
			if len(slices[i]) > maxLen {
				maxLen = len(slices[i])
			}
		}
		var out []object.Object
		for i := 0; i < maxLen; i++ {
			row := make([]object.Object, len(slices))
			for j := range slices {
				if i < len(slices[j]) {
					row[j] = slices[j][i]
				} else {
					row[j] = fill
				}
			}
			out = append(out, &object.Tuple{Items: row})
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["pairwise"] = bi("pairwise", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(a[0])
		var out []object.Object
		for i := 1; i < len(items); i++ {
			out = append(out, &object.Tuple{Items: []object.Object{items[i-1], items[i]}})
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["tee"] = bi("tee", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		items := ip.ToSlice(a[0])
		n := int64(2)
		if len(a) > 1 {
			if v, ok := object.AsInt(a[1]); ok {
				n = v
			}
		}
		var out []object.Object
		for i := int64(0); i < n; i++ {
			out = append(out, &object.List{Items: append([]object.Object{}, items...)})
		}
		return &object.Tuple{Items: out}, nil
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
	// singledispatch：返回可调用包装，调用一律落到基函数（不做真分派）——
	// 库里的用法基本是「注册若干类型后调一次」，基函数语义足够
	m.Dict["singledispatch"] = bi("singledispatch", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("singledispatch requires a function")
		}
		base := a[0]
		cls := &object.TypeObj{Name: "singledispatch", Dict: map[string]object.Object{}}
		inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
		cls.Dict["__call__"] = bi("__call__", func(x []object.Object, y map[string]object.Object) (object.Object, error) {
			// x[0] 是 singledispatch 实例本身
			args := x
			if len(args) > 0 {
				args = args[1:]
			}
			return ip.Call(base, args, y), nil
		})
		inst.Dict["dispatch"] = bi("dispatch", func(x []object.Object, y map[string]object.Object) (object.Object, error) {
			return base, nil
		})
		inst.Dict["register"] = bi("register", func(x []object.Object, y map[string]object.Object) (object.Object, error) {
			// CPython 语义：
			//   @fn.register(int)          → 返回「装饰器」（再把实现函数原样返回）
			//   fn.register(int, impl)     → 直接返回 impl
			if len(x) >= 3 {
				return x[2], nil
			}
			return &object.Builtin{Name: "register.decorator", Fn: func(z []object.Object, _ map[string]object.Object) (object.Object, error) {
				if len(z) > 0 {
					return z[0], nil
				}
				return object.None, nil
			}}, nil
		})
		inst.Dict["_clear_cache"] = bi("_clear_cache", func(x []object.Object, y map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		return inst, nil
	})
	m.Dict["singledispatchmethod"] = m.Dict["singledispatch"]
	m.Dict["cached_property"] = bi("cached_property", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return object.None, nil
		}
		return &object.Property{Fget: a[0]}, nil
	})
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
	// wraps 需真实复制元信息（__name__/__qualname__/__doc__/__dict__），否则 wrapper 名字与 CPython 不一致
	m.Dict["wraps"] = object.NewWrapsBuiltin()
	m.Dict["lru_cache"] = bi("lru_cache", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		var target object.Object
		if len(args) > 0 && (isCallableObj(args[0])) {
			target = args[0]
		}
		if target != nil {
			return makeCachedCallable(ip, target), nil
		}
		return bi("lru_cache_decorator", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return makeCachedCallable(ip, a[0]), nil
		}), nil
	})
	// functools.cache：CPython 3.9+ 等价于 lru_cache(maxsize=None)，
	// pathlib.py:58 模块级就靠它包 _PosixFlavour._parse_parts
	m.Dict["cache"] = bi("cache", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("cache() requires a callable")
		}
		return makeCachedCallable(ip, a[0]), nil
	})
	m.Dict["cmp_to_key"] = bi("cmp_to_key", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return args[0], nil
	})
	// total_ordering：从单个根运算（__lt__/__le__/__gt__/__ge__）推导其余三个（CPython _convert 口径）。
	// urllib3 的 HTTPHeaderDict 等大量类用 @total_ordering。
	m.Dict["total_ordering"] = bi("total_ordering", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return ip.applyTotalOrdering(args[0])
	})
	return m
}

// totalOrderingDerive 描述「从根运算推导派生运算」的规则：
// 0 = 取反；1 = 取反且 self != other；2 = 原值或 self == other（对齐 CPython functools._convert）。
type totalOrderingRule struct {
	op   string
	kind int
}

var totalOrderingTable = map[string][]totalOrderingRule{
	"__lt__": {{"__gt__", 1}, {"__le__", 2}, {"__ge__", 0}},
	"__le__": {{"__gt__", 0}, {"__lt__", 1}, {"__ge__", 2}},
	"__gt__": {{"__lt__", 1}, {"__ge__", 2}, {"__le__", 0}},
	"__ge__": {{"__lt__", 1}, {"__gt__", 0}, {"__le__", 2}},
}

// applyTotalOrdering 实现 functools.total_ordering(cls)。
func (ip *Interp) applyTotalOrdering(o object.Object) (object.Object, error) {
	cls, ok := o.(*object.TypeObj)
	if !ok {
		ip.TypeError("total_ordering() argument must be a class")
	}
	has := func(n string) bool {
		_, ok := cls.Lookup(n)
		return ok
	}
	root := ""
	for _, n := range []string{"__lt__", "__le__", "__gt__", "__ge__"} {
		if has(n) {
			root = n
			break
		}
	}
	if root == "" {
		ip.raise(ip.excType("ValueError"), "must define at least one ordering operation: < > <= >=")
	}
	for _, r := range totalOrderingTable[root] {
		if has(r.op) {
			continue
		}
		op := r.op
		kind := r.kind
		cls.Dict[op] = bi(op, func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) < 2 {
				ip.TypeError("%s() takes two arguments", op)
			}
			self, other := a[0], a[1]
			fn, ok := cls.Lookup(root)
			if !ok {
				return object.None, nil
			}
			res := ip.Call(ip.bindMethod(self, fn), []object.Object{other}, nil)
			truth := object.Truthy(res)
			switch kind {
			case 0:
				return object.GetBool(!truth), nil
			case 1:
				return object.GetBool(!truth && !object.Equal(self, other)), nil
			default:
				return object.GetBool(truth || object.Equal(self, other)), nil
			}
		})
	}
	return cls, nil
}

// makeCachedCallable 按参数 repr 做无界缓存的调用包装（lru_cache / cache 共用）。
func makeCachedCallable(ip *Interp, fn object.Object) object.Object {
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
	// encodebytes / decodebytes：email.encoders:15 `from base64 import encodebytes` 硬依赖。
	// CPython 口径：每 76 字符断行（MIME base64）。
	m.Dict["encodebytes"] = bi("encodebytes", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.Bytes{}, nil
		}
		enc := base64.StdEncoding.EncodeToString(pyRawBytes(args[0]))
		var sb strings.Builder
		for i := 0; i < len(enc); i += 76 {
			end := i + 76
			if end > len(enc) {
				end = len(enc)
			}
			sb.WriteString(enc[i:end])
			sb.WriteString("\n")
		}
		return &object.Bytes{V: []byte(sb.String())}, nil
	})
	m.Dict["decodebytes"] = bi("decodebytes", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return &object.Bytes{}, nil
		}
		raw := string(pyRawBytes(args[0]))
		// 去掉所有空白（CPython 的 binascii.a2b_base64 宽容空白）
		clean := strings.Map(func(r rune) rune {
			if r == '\n' || r == '\r' || r == ' ' || r == '\t' {
				return -1
			}
			return r
		}, raw)
		data, err := base64.StdEncoding.DecodeString(clean)
		if err != nil {
			ip.raise(ip.excType("binascii.Error"), "%v", err)
		}
		return &object.Bytes{V: data}, nil
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
	// date(year, month, day)：calendar.py:98 用 datetime.date(2001, i+1, 1).strftime
	dateCls.Dict["__init__"] = bi("__init__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return object.None, nil
		}
		self, ok := args[0].(*object.Instance)
		if !ok {
			return object.None, nil
		}
		names := []string{"year", "month", "day"}
		for i, n := range names {
			v := int64(0)
			if len(args) > i+1 {
				v, _ = object.AsInt(args[i+1])
			}
			self.Dict[n] = &object.Int{V: v}
		}
		return object.None, nil
	})
	getDate := func(self *object.Instance, n string) int64 {
		if v, ok := self.Dict[n]; ok {
			x, _ := object.AsInt(v)
			return x
		}
		return 0
	}
	dateCls.Dict["strftime"] = bi("strftime", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self, _ := args[0].(*object.Instance)
		if self == nil || len(args) < 2 {
			return &object.Str{V: ""}, nil
		}
		t := time.Date(int(getDate(self, "year")), time.Month(getDate(self, "month")), int(getDate(self, "day")),
			0, 0, 0, 0, time.Local)
		return &object.Str{V: t.Format(goTimeLayout(args[1].Str()))}, nil
	})
	dateCls.Dict["isoformat"] = bi("isoformat", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self, _ := args[0].(*object.Instance)
		if self == nil {
			return &object.Str{V: ""}, nil
		}
		return &object.Str{V: fmt.Sprintf("%04d-%02d-%02d", getDate(self, "year"), getDate(self, "month"), getDate(self, "day"))}, nil
	})
	dateCls.Dict["__str__"] = bi("__str__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		r, _ := dateCls.Dict["isoformat"].(*object.Builtin).Fn(args, kw)
		return r, nil
	})
	dateCls.Dict["__repr__"] = bi("__repr__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		r, _ := dateCls.Dict["isoformat"].(*object.Builtin).Fn(args, kw)
		return &object.Str{V: "datetime.date(" + strings.ReplaceAll(r.Str(), "-", ", ") + ")"}, nil
	})
	dateCls.Dict["weekday"] = bi("weekday", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self, _ := args[0].(*object.Instance)
		if self == nil {
			return &object.Int{V: 0}, nil
		}
		t := time.Date(int(getDate(self, "year")), time.Month(getDate(self, "month")), int(getDate(self, "day")), 0, 0, 0, 0, time.Local)
		return &object.Int{V: int64((int(t.Weekday()) + 6) % 7)}, nil
	})
	dateCls.Dict["toordinal"] = bi("toordinal", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self, _ := args[0].(*object.Instance)
		if self == nil {
			return &object.Int{V: 0}, nil
		}
		t := time.Date(int(getDate(self, "year")), time.Month(getDate(self, "month")), int(getDate(self, "day")), 0, 0, 0, 0, time.UTC)
		return &object.Int{V: int64(t.Unix()/86400) + 719163}, nil
	})
	dateCls.Dict["today"] = bi("today", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		inst := &object.Instance{Class: dateCls, Dict: map[string]object.Object{}}
		now := time.Now()
		inst.Dict["year"] = &object.Int{V: int64(now.Year())}
		inst.Dict["month"] = &object.Int{V: int64(now.Month())}
		inst.Dict["day"] = &object.Int{V: int64(now.Day())}
		return inst, nil
	})
	m.Dict["date"] = dateCls
	tdCls := &object.TypeObj{Name: "timedelta", Dict: map[string]object.Object{}}
	tdCls.Dict["__init__"] = bi("__init__", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self, ok := args[0].(*object.Instance)
		if !ok {
			return object.None, nil
		}
		days, secs, micros := int64(0), int64(0), int64(0)
		if len(args) > 1 {
			days, _ = object.AsInt(args[1])
		}
		if len(args) > 2 {
			secs, _ = object.AsInt(args[2])
		}
		if len(args) > 3 {
			micros, _ = object.AsInt(args[3])
		}
		if kw != nil {
			if v, ok := kw["days"]; ok {
				days, _ = object.AsInt(v)
			}
			if v, ok := kw["seconds"]; ok {
				secs, _ = object.AsInt(v)
			}
			if v, ok := kw["microseconds"]; ok {
				micros, _ = object.AsInt(v)
			}
		}
		self.Dict["days"] = &object.Int{V: days}
		self.Dict["seconds"] = &object.Int{V: secs}
		self.Dict["microseconds"] = &object.Int{V: micros}
		return object.None, nil
	})
	tdCls.Dict["total_seconds"] = bi("total_seconds", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		self, _ := args[0].(*object.Instance)
		if self == nil {
			return &object.Float{V: 0}, nil
		}
		d, _ := object.AsInt(self.Dict["days"])
		sec, _ := object.AsInt(self.Dict["seconds"])
		us, _ := object.AsInt(self.Dict["microseconds"])
		return &object.Float{V: float64(d*86400+sec) + float64(us)/1e6}, nil
	})
	m.Dict["timedelta"] = tdCls
	return m
}

// ==================== asyncio ====================

// asyncPrimKey 是并发原语状态在 Instance.Dict 里的私有键
const asyncPrimKey = "__asyncio_prim__"

// asyncPrim 是 asyncio 并发原语（Queue / Lock / Event）的本地状态载体。
// 只存 Go 侧状态；Python 侧方法注册在对应 TypeObj.Dict 里，由 bindClassAttr 绑定 self。
// 阻塞语义：任务各自跑在自己的 goroutine 上，所以“挂起等待”= 当前 goroutine 阻塞在 channel 上，
// 其余任务照常推进（与既有 sleep/gather 的调度模型一致）。
type asyncPrim struct {
	cls  *object.TypeObj
	kind string

	mu sync.Mutex

	// Queue
	items []object.Object
	unfin int // 未 task_done 的条目数（join 用它判断）
	maxsz int
	getW  []chan struct{} // 等 get 的挂起点
	putW  []chan struct{} // 等 put 空位的挂起点
	joinW []chan struct{} // 等 join 的挂起点

	// Lock
	locked bool
	lockW  []chan struct{}

	// Event
	flag bool
	evW  []chan struct{}
}

func (p *asyncPrim) Type() *object.TypeObj { return p.cls }
func (p *asyncPrim) Repr() string          { return "<asyncio." + p.kind + " object>" }
func (p *asyncPrim) Str() string           { return p.Repr() }

func asyncPrimOf(a []object.Object) *asyncPrim {
	if len(a) == 0 {
		return nil
	}
	inst, ok := a[0].(*object.Instance)
	if !ok {
		return nil
	}
	if v, ok := inst.Dict[asyncPrimKey]; ok {
		if p, ok := v.(*asyncPrim); ok {
			return p
		}
	}
	return nil
}

// wakeAll 关闭并清空全部挂起 channel（调用方必须持锁）
func wakeAll(list *[]chan struct{}) {
	for _, c := range *list {
		close(c)
	}
	*list = nil
}

// wakeOne 唤醒单个挂起点（调用方必须持锁）
func wakeOne(list *[]chan struct{}) {
	if len(*list) == 0 {
		return
	}
	close((*list)[0])
	*list = (*list)[1:]
}

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
	// iscoroutinefunction：async def 编译成 Generator 工厂，靠标记位识别
	m.Dict["iscoroutinefunction"] = bi("iscoroutinefunction", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(args) == 0 {
			return object.FalseObj, nil
		}
		if fn, ok := args[0].(*object.Function); ok {
			return object.GetBool(fn.IsAsync), nil
		}
		return object.FalseObj, nil
	})
	// 事件循环对象：run() 期间存在；脚本里只用 is not None 与 id 类语义
	loopObj := &object.Instance{Class: &object.TypeObj{Name: "EventLoop", Dict: map[string]object.Object{}}}
	m.Dict["get_running_loop"] = bi("get_running_loop", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		if len(ip.genStack) == 0 {
			ip.raise(ip.excType("RuntimeError"), "no running event loop")
		}
		return loopObj, nil
	})
	m.Dict["get_event_loop"] = bi("get_event_loop", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return loopObj, nil
	})
	m.Dict["new_event_loop"] = bi("new_event_loop", func(args []object.Object, kw map[string]object.Object) (object.Object, error) {
		return &object.Instance{Class: &object.TypeObj{Name: "EventLoop", Dict: map[string]object.Object{}}}, nil
	})

	// ---------- 并发原语：Queue / Lock / Event ----------
	m.Dict["TimeoutError"] = ip.excType("TimeoutError")
	queueEmpty := &object.TypeObj{Name: "QueueEmpty", Bases: []*object.TypeObj{ip.excType("Exception")}, IsExc: true, Dict: map[string]object.Object{}}
	queueFull := &object.TypeObj{Name: "QueueFull", Bases: []*object.TypeObj{ip.excType("Exception")}, IsExc: true, Dict: map[string]object.Object{}}
	m.Dict["QueueEmpty"] = queueEmpty
	m.Dict["QueueFull"] = queueFull

	mustPrim := func(a []object.Object) *asyncPrim {
		p := asyncPrimOf(a)
		if p == nil {
			ip.RuntimeErr("asyncio primitive used before __init__")
		}
		return p
	}
	primInit := func(kind string) *object.Builtin {
		return bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) == 0 {
				return object.None, nil
			}
			inst, ok := a[0].(*object.Instance)
			if !ok {
				return object.None, nil
			}
			p := &asyncPrim{cls: inst.Class, kind: kind}
			if kind == "Queue" {
				if len(a) > 1 {
					if n, ok := object.AsInt(a[1]); ok {
						p.maxsz = int(n)
					}
				}
				if v, ok := k["maxsize"]; ok {
					if n, ok := object.AsInt(v); ok {
						p.maxsz = int(n)
					}
				}
				inst.Dict["maxsize"] = &object.Int{V: int64(p.maxsz)}
			}
			inst.Dict[asyncPrimKey] = p
			return object.None, nil
		})
	}

	// ---- Queue ----
	qCls := &object.TypeObj{Name: "Queue", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	qCls.Dict["__init__"] = primInit("Queue")
	qCls.Dict["maxsize"] = &object.Int{V: 0}
	qCls.Dict["put_nowait"] = bi("put_nowait", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		var item object.Object = object.None
		if len(a) > 1 {
			item = a[1]
		}
		p.mu.Lock()
		if p.maxsz > 0 && len(p.items) >= p.maxsz {
			p.mu.Unlock()
			ip.raise(queueFull, "")
			return object.None, nil
		}
		p.items = append(p.items, item)
		p.unfin++
		wakeAll(&p.getW)
		p.mu.Unlock()
		return object.None, nil
	})
	qCls.Dict["put"] = bi("put", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		var item object.Object = object.None
		if len(a) > 1 {
			item = a[1]
		}
		return ip.newGenerator("Queue.put", func(yield func(object.Object) object.Object) {
			for {
				p.mu.Lock()
				if p.maxsz <= 0 || len(p.items) < p.maxsz {
					p.items = append(p.items, item)
					p.unfin++
					wakeAll(&p.getW)
					p.mu.Unlock()
					break
				}
				c := make(chan struct{})
				p.putW = append(p.putW, c)
				p.mu.Unlock()
				<-c
			}
			yield(object.None)
		}), nil
	})
	qCls.Dict["get_nowait"] = bi("get_nowait", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		if len(p.items) == 0 {
			p.mu.Unlock()
			ip.raise(queueEmpty, "")
			return object.None, nil
		}
		v := p.items[0]
		p.items = p.items[1:]
		wakeAll(&p.putW)
		p.mu.Unlock()
		return v, nil
	})
	qCls.Dict["get"] = bi("get", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		return ip.newGenerator("Queue.get", func(yield func(object.Object) object.Object) {
			var v object.Object = object.None
			for {
				p.mu.Lock()
				if len(p.items) > 0 {
					v = p.items[0]
					p.items = p.items[1:]
					wakeAll(&p.putW)
					p.mu.Unlock()
					break
				}
				c := make(chan struct{})
				p.getW = append(p.getW, c)
				p.mu.Unlock()
				<-c
			}
			yield(v)
		}), nil
	})
	qCls.Dict["task_done"] = bi("task_done", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		p.unfin--
		if p.unfin < 0 {
			p.mu.Unlock()
			ip.ValueError("task_done() called too many times")
			return object.None, nil
		}
		if p.unfin == 0 {
			wakeAll(&p.joinW)
		}
		p.mu.Unlock()
		return object.None, nil
	})
	qCls.Dict["join"] = bi("join", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		return ip.newGenerator("Queue.join", func(yield func(object.Object) object.Object) {
			for {
				p.mu.Lock()
				if p.unfin <= 0 {
					p.mu.Unlock()
					break
				}
				c := make(chan struct{})
				p.joinW = append(p.joinW, c)
				p.mu.Unlock()
				<-c
			}
			yield(object.None)
		}), nil
	})
	qCls.Dict["qsize"] = bi("qsize", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		n := len(p.items)
		p.mu.Unlock()
		return &object.Int{V: int64(n)}, nil
	})
	qCls.Dict["empty"] = bi("empty", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		n := len(p.items)
		p.mu.Unlock()
		return object.GetBool(n == 0), nil
	})
	qCls.Dict["full"] = bi("full", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		n, m := len(p.items), p.maxsz
		p.mu.Unlock()
		return object.GetBool(m > 0 && n >= m), nil
	})
	m.Dict["Queue"] = qCls

	// ---- Lock ----
	lCls := &object.TypeObj{Name: "Lock", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	lCls.Dict["__init__"] = primInit("Lock")
	lCls.Dict["acquire"] = bi("acquire", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		return ip.newGenerator("Lock.acquire", func(yield func(object.Object) object.Object) {
			for {
				p.mu.Lock()
				if !p.locked {
					p.locked = true
					p.mu.Unlock()
					break
				}
				c := make(chan struct{})
				p.lockW = append(p.lockW, c)
				p.mu.Unlock()
				<-c
			}
			yield(object.TrueObj)
		}), nil
	})
	lCls.Dict["release"] = bi("release", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		if !p.locked {
			p.mu.Unlock()
			ip.RuntimeErr("release unlocked lock")
			return object.None, nil
		}
		p.locked = false
		wakeOne(&p.lockW)
		p.mu.Unlock()
		return object.None, nil
	})
	lCls.Dict["locked"] = bi("locked", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		v := p.locked
		p.mu.Unlock()
		return object.GetBool(v), nil
	})
	lCls.Dict["__aenter__"] = bi("__aenter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		return ip.newGenerator("Lock.__aenter__", func(yield func(object.Object) object.Object) {
			for {
				p.mu.Lock()
				if !p.locked {
					p.locked = true
					p.mu.Unlock()
					break
				}
				c := make(chan struct{})
				p.lockW = append(p.lockW, c)
				p.mu.Unlock()
				<-c
			}
			yield(object.None)
		}), nil
	})
	lCls.Dict["__aexit__"] = bi("__aexit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		return ip.newGenerator("Lock.__aexit__", func(yield func(object.Object) object.Object) {
			p.mu.Lock()
			if p.locked {
				p.locked = false
				wakeOne(&p.lockW)
			}
			p.mu.Unlock()
			yield(object.None)
		}), nil
	})
	m.Dict["Lock"] = lCls

	// ---- Event ----
	eCls := &object.TypeObj{Name: "Event", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	eCls.Dict["__init__"] = primInit("Event")
	eCls.Dict["set"] = bi("set", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		p.flag = true
		wakeAll(&p.evW)
		p.mu.Unlock()
		return object.None, nil
	})
	eCls.Dict["clear"] = bi("clear", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		p.flag = false
		p.mu.Unlock()
		return object.None, nil
	})
	eCls.Dict["is_set"] = bi("is_set", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		p.mu.Lock()
		v := p.flag
		p.mu.Unlock()
		return object.GetBool(v), nil
	})
	eCls.Dict["wait"] = bi("wait", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		p := mustPrim(a)
		return ip.newGenerator("Event.wait", func(yield func(object.Object) object.Object) {
			for {
				p.mu.Lock()
				if p.flag {
					p.mu.Unlock()
					break
				}
				c := make(chan struct{})
				p.evW = append(p.evW, c)
				p.mu.Unlock()
				<-c
			}
			yield(object.TrueObj)
		}), nil
	})
	m.Dict["Event"] = eCls

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

// ==================== typing 泛型别名 ====================

// typingAliasObj typing 泛型别名（typing.List[int] / typing.Optional[str] ...）。
// 不用 *object.Instance 承载：Dict.Repr 直接调 object 层 Repr()，Instance 的
// Repr 固定为 "<X object>"，别名一旦嵌进 __annotations__ 打印就丢信息。
type typingAliasObj struct {
	origin object.Object
	args   []object.Object
	text   string
}

func (a *typingAliasObj) Type() *object.TypeObj { return typingAliasType }
func (a *typingAliasObj) Repr() string          { return a.text }
func (a *typingAliasObj) Str() string           { return a.text }

// typingSpecialForm typing 特殊形式（Union / Optional / Literal）：
// CPython 里它们是 _SpecialForm，repr 为 "typing.Union" 这种裸名。
type typingSpecialForm struct {
	text string
}

func (f *typingSpecialForm) Type() *object.TypeObj { return typingSpecialFormType }
func (f *typingSpecialForm) Repr() string          { return f.text }
func (f *typingSpecialForm) Str() string           { return f.text }

var (
	typingAliasType       = &object.TypeObj{Name: "_GenericAlias", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}, Module: "typing"}
	typingSpecialFormType = &object.TypeObj{Name: "_SpecialForm", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}, Module: "typing"}
)

// typingGenericSpec 泛型名字 → 下标语义。
// origin 即 __origin__（typing.get_origin 的返回值）：List→list、Dict→dict、
// Optional/Union→typing.Union 特殊形式。
type typingGenericSpec struct {
	origin  object.Object
	display string
	kind    int
}

const (
	tgPlain    = iota // 下标原样铺开（List/Dict/Tuple/Callable ...）
	tgOptional        // X → (X, NoneType)，origin = typing.Union
	tgUnion           // (A, B) → (A, B)，origin = typing.Union
)

// typingGenericRegistry key 用对象指针做身份（TypeObj / special form 都是指针，可比）
var typingGenericRegistry = map[object.Object]*typingGenericSpec{}

// typingUnionForm typing.Union 特殊形式本体（既是 get_origin 的返回值，也是 Union[...] 的下标基座）
var typingUnionForm = &typingSpecialForm{text: "typing.Union"}

// typingArgRepr 别名 repr 里单个参数的显示名（对齐 CPython _type_repr：类用裸名）
func (ip *Interp) typingArgRepr(o object.Object) string {
	switch v := o.(type) {
	case *object.TypeObj:
		if v.Name != "" {
			return v.Name
		}
	case *typingAliasObj:
		return v.text
	case *typingSpecialForm:
		return v.text
	case *object.List:
		parts := make([]string, len(v.Items))
		for i, it := range v.Items {
			parts[i] = ip.typingArgRepr(it)
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case *object.Tuple:
		parts := make([]string, len(v.Items))
		for i, it := range v.Items {
			parts[i] = ip.typingArgRepr(it)
		}
		return "(" + strings.Join(parts, ", ") + ")"
	}
	return ip.Repr(o)
}

// makeTypingAlias 处理 typing 泛型下标：typing.List[int] → typingAliasObj。
// 返回 (别名, true) 表示已接管；否则交回 GetItem 走原生路径（list[int] 等 PEP 585）。
func (ip *Interp) makeTypingAlias(base object.Object, idx object.Object) (object.Object, bool) {
	spec := typingGenericRegistry[base]
	if spec == nil {
		return nil, false
	}
	// 空下标 `List[]` 这类畸形写法直接放行给原逻辑
	args := []object.Object{idx}
	if tup, ok := idx.(*object.Tuple); ok {
		args = tup.Items
	}
	// textArgs = repr 里显示的参数（CPython：Optional[str] 只显示 str，
	// 但 get_args 返回 (str, NoneType)）——两者必须分开算，否则
	// `typing.Optional[str, NoneType]` 这种假 repr 会污染 __annotations__ 打印。
	textArgs := args
	switch spec.kind {
	case tgOptional:
		if len(args) == 1 {
			textArgs = args
		}
		args = append(append([]object.Object{}, args...), object.NoneType)
	case tgUnion:
		if len(args) == 0 {
			args = []object.Object{idx}
			textArgs = args
		}
	}
	if len(args) == 1 && args[0] == nil {
		return nil, false
	}
	parts := make([]string, len(textArgs))
	for i, a := range textArgs {
		parts[i] = ip.typingArgRepr(a)
	}
	return &typingAliasObj{origin: spec.origin, args: args, text: spec.display + "[" + strings.Join(parts, ", ") + "]"}, true
}

// typingGetOrigin typing.get_origin 口径
func typingGetOrigin(o object.Object) object.Object {
	switch v := o.(type) {
	case *typingAliasObj:
		return v.origin
	case *object.Instance:
		// PEP 585 别名（list[int]）与 PEP 604 并集（int | str）
		if v.Class == genericAliasType || v.Class == unionTypeType {
			if o, ok := v.Dict["__origin__"]; ok {
				return o
			}
			if v.Class == unionTypeType {
				return typingUnionForm
			}
		}
	}
	return object.None
}

// typingGetArgs typing.get_args 口径（非别名返回空元组）
func typingGetArgs(o object.Object) *object.Tuple {
	items := func(v object.Object) *object.Tuple {
		if t, ok := v.(*object.Tuple); ok {
			return t
		}
		return &object.Tuple{Items: []object.Object{v}}
	}
	switch v := o.(type) {
	case *typingAliasObj:
		return &object.Tuple{Items: v.args}
	case *object.Instance:
		if v.Class == genericAliasType || v.Class == unionTypeType {
			if a, ok := v.Dict["__args__"]; ok {
				return items(a)
			}
		}
	}
	return &object.Tuple{}
}

// resolveAnnString 展开字符串注解（PEP 563 / 手写 'List[int]'）：
// 在模块全局 + typing 命名空间里求值；失败则原样返回字符串，绝不因注解炸掉整个导入。
func (ip *Interp) resolveAnnString(src string) object.Object {
	expr, errs := parseExprString(src)
	if len(errs) > 0 || expr == nil {
		return &object.Str{V: src}
	}
	var res object.Object
	pe := ip.protect(func() { res = ip.evalExpr(expr, ip.Globals) })
	if pe == nil && res != nil {
		return res
	}
	// 回退：把 typing 模块全部名字注入一层临时作用域再试（用户多半只 import typing）
	if tm, ok := ip.Modules["typing"]; ok {
		env := object.NewEnv(ip.Globals)
		env.Globals = ip.Globals
		for k, v := range tm.Dict {
			env.Vars[k] = v
		}
		res = nil
		if pe2 := ip.protect(func() { res = ip.evalExpr(expr, env) }); pe2 == nil && res != nil {
			return res
		}
	}
	return &object.Str{V: src}
}

// collectAnnHints 把注解字典展开成 get_type_hints 的返回值（键序保持）
func (ip *Interp) collectAnnHints(src *object.Dict) *object.Dict {
	out := &object.Dict{}
	if src == nil {
		return out
	}
	for i, k := range src.Keys {
		v := src.Vals[i]
		if s, ok := v.(*object.Str); ok {
			v = ip.resolveAnnString(s.V)
		}
		out.Set(k, v)
	}
	return out
}

// ==================== typing / dataclasses ====================

func modTyping(ip *Interp) *object.Module {
	m := newModule("typing")
	// TYPE_CHECKING 必须是 False（CPython 语义）：第三方库里 `if TYPE_CHECKING:`
	// 块里全是只给类型检查器的相对导入，一旦为真会触发循环导入/多余依赖。
	m.Dict["TYPE_CHECKING"] = object.FalseObj
	// typing.NamedTuple：既能当基类（class X(NamedTuple)），也能函数式调用
	ntMeta := &object.TypeObj{Name: "NamedTupleMeta", Bases: []*object.TypeObj{object.TypeType}, Dict: map[string]object.Object{}}
	ntMeta.Dict["__call__"] = bi("__call__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("NamedTuple() requires a typename")
		}
		name := a[1].Str()
		cls := &object.TypeObj{Name: name, Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
		var names []string
		if len(a) > 2 {
			for _, f := range ip.ToSlice(a[2]) {
				pair := ip.ToSlice(f)
				if len(pair) > 0 {
					names = append(names, pair[0].Str())
				}
			}
		}
		ip.buildNamedTuple(cls, names)
		return cls, nil
	})
	ntType := &object.TypeObj{Name: "NamedTuple", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}, MetaClass: ntMeta}
	ntType.Dict[namedTupleMarker] = object.TrueObj
	m.Dict["NamedTuple"] = ntType
	// 装饰器类（返回原对象）：@overload / @final / @runtime_checkable / @no_type_check
	ident := func(name string) *object.Builtin {
		return bi(name, func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			if len(a) > 0 {
				return a[0], nil
			}
			return object.None, nil
		})
	}
	m.Dict["overload"] = ident("overload")
	m.Dict["final"] = ident("final")
	m.Dict["runtime_checkable"] = ident("runtime_checkable")
	m.Dict["no_type_check"] = ident("no_type_check")
	m.Dict["no_type_check_decorator"] = ident("no_type_check_decorator")
	m.Dict["dataclass_transform"] = ident("dataclass_transform")
	m.Dict["NewType"] = ident("NewType")
	m.Dict["TypeVar"] = ident("TypeVar")
	m.Dict["ParamSpec"] = ident("ParamSpec")
	m.Dict["TypeVarTuple"] = ident("TypeVarTuple")
	m.Dict["Unpack"] = ident("Unpack")
	m.Dict["cast"] = bi("cast", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 1 {
			return a[1], nil
		}
		return object.None, nil
	})
	m.Dict["assert_type"] = bi("assert_type", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			return a[0], nil
		}
		return object.None, nil
	})
	m.Dict["reveal_type"] = m.Dict["assert_type"]
	m.Dict["assert_never"] = bi("assert_never", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		ip.TypeError("Expected code to be unreachable")
		return object.None, nil
	})
	// get_type_hints：支持函数 / 类（按 MRO 由基到子合并），字符串注解当场展开
	m.Dict["get_type_hints"] = bi("get_type_hints", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Dict{}, nil
		}
		var ann *object.Dict
		switch o := a[0].(type) {
		case *object.Function:
			if v, ok := o.GetAttr("__annotations__"); ok {
				ann, _ = v.(*object.Dict)
			}
		// object.Builtin 没有属性字典（无 GetAttr），内建函数无注解可合并：跳过。
		case *object.TypeObj:
			// CPython：从最基类到当前类依次合并，子类同名覆盖
			mro := o.MRO()
			for i := len(mro) - 1; i >= 0; i-- {
				if d, ok := mro[i].Dict["__annotations__"].(*object.Dict); ok {
					if ann == nil {
						ann = &object.Dict{}
					}
					for j, kk := range d.Keys {
						ann.Set(kk, d.Vals[j])
					}
				}
			}
		case *object.Module:
			if v, ok := o.Dict["__annotations__"]; ok {
				ann, _ = v.(*object.Dict)
			}
		}
		return ip.collectAnnHints(ann), nil
	})
	m.Dict["get_origin"] = bi("get_origin", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return object.None, nil
		}
		return typingGetOrigin(a[0]), nil
	})
	m.Dict["get_args"] = bi("get_args", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			return &object.Tuple{}, nil
		}
		return typingGetArgs(a[0]), nil
	})
	for _, n := range []string{"List", "Dict", "Tuple", "Set", "Optional", "Any", "Union", "Callable", "Iterable", "Iterator", "Sequence", "Mapping", "Type",
		"Generic", "TypedDict", "Protocol", "Literal", "Final", "ClassVar", "NoReturn", "TypeAlias", "Self",
		"AnyStr", "Text", "IO", "BinaryIO", "TextIO", "ByteString", "Container", "Collection", "Reversible",
		"FrozenSet", "AbstractSet", "MutableMapping", "MutableSequence",
		"MutableSet", "DefaultDict", "Deque", "Counter", "OrderedDict", "ChainMap", "KeysView", "ValuesView",
		"ItemsView", "AsyncIterable", "AsyncIterator", "Awaitable", "Coroutine", "AsyncGenerator",
		"AsyncContextManager", "ContextManager", "Generator", "Hashable", "Reversible", "Sized",
		"SupportsInt", "SupportsFloat", "SupportsBytes", "SupportsComplex", "SupportsIndex", "SupportsAbs",
		"SupportsRound", "Annotated", "Concatenate", "Required", "NotRequired", "LiteralString", "Never",
		"TypeGuard", "TypeAliasType", "ForwardRef", "Pattern", "Match"} {
		m.Dict[n] = &object.TypeObj{Name: n, Dict: map[string]object.Object{}}
	}

	// ---- 泛型下标注册：typing.List[int] / Dict[str, List[int]] / Callable[[int], str] ----
	// __origin__ 对齐 CPython：List→list、Dict→dict、Tuple→tuple、Set/FrozenSet→set。
	// 容器 ABC（Iterable/Sequence/...）CPython 指向 collections.abc.X，这里先指向自身，
	// 保证 get_origin/get_args 能拿到确定值；语义细节待后续对齐。
	genericOrigins := map[string]object.Object{
		"List": object.ListType, "Dict": object.DictType, "Tuple": object.TupleType,
		"Set": object.SetType, "FrozenSet": object.SetType, "DefaultDict": object.DictType,
	}
	// 容器 ABC 的 __origin__ 必须是 collections.abc 的真实类。
	// 否则 `class X(MutableMapping[str, str])` 的基类是 typing 的空占位 TypeObj，
	// MRO 变成 [X, Generic, MutableMapping, object]——没有 Mapping，
	// 实例连 .get/.keys/.update 都没有（urllib3 RecentlyUsedContainer，第16轮实测）。
	if abcMod, ok := ip.Modules["collections.abc"]; ok {
		pairs := map[string]string{
			"Mapping": "Mapping", "MutableMapping": "MutableMapping",
			"Iterable": "Iterable", "Iterator": "Iterator",
			"Sequence": "Sequence", "MutableSequence": "MutableSequence",
			"AbstractSet": "Set", "MutableSet": "MutableSet",
			"Collection": "Collection", "Container": "Container",
			"Reversible": "Reversible", "Sized": "Sized",
			"Hashable": "Hashable", "Callable": "Callable",
			"Generator": "Generator", "Awaitable": "Awaitable", "Coroutine": "Coroutine",
			"AsyncIterable": "AsyncIterable", "AsyncIterator": "AsyncIterator",
			"AsyncGenerator": "AsyncGenerator",
			"KeysView":       "KeysView", "ValuesView": "ValuesView", "ItemsView": "ItemsView",
		}
		for tn, an := range pairs {
			if v, ok := abcMod.Dict[an]; ok {
				genericOrigins[tn] = v
			}
		}
	}
	for _, n := range []string{"List", "Dict", "Tuple", "Set", "FrozenSet", "DefaultDict", "Deque", "Counter",
		"OrderedDict", "ChainMap", "Iterable", "Iterator", "Sequence", "Mapping", "MutableMapping",
		"MutableSequence", "MutableSet", "Generator", "AsyncIterable", "AsyncIterator", "AsyncGenerator",
		"Awaitable", "Coroutine", "ContextManager", "AsyncContextManager", "Type", "KeysView", "ValuesView",
		"ItemsView", "AbstractSet", "Collection", "Container", "Reversible", "Pattern", "Match", "Callable"} {
		t, ok := m.Dict[n].(*object.TypeObj)
		if !ok {
			continue
		}
		origin := genericOrigins[n]
		if origin == nil {
			origin = t
		}
		typingGenericRegistry[t] = &typingGenericSpec{origin: origin, display: "typing." + n, kind: tgPlain}
	}
	// Union / Optional / Literal：CPython 里是 _SpecialForm（repr 为裸名 typing.Union），
	// Optional 的 __origin__ 固定为 typing.Union。
	typingGenericRegistry[typingUnionForm] = &typingGenericSpec{origin: typingUnionForm, display: "typing.Union", kind: tgUnion}
	m.Dict["Union"] = typingUnionForm
	optForm := &typingSpecialForm{text: "typing.Optional"}
	typingGenericRegistry[optForm] = &typingGenericSpec{origin: typingUnionForm, display: "typing.Optional", kind: tgOptional}
	m.Dict["Optional"] = optForm
	litForm := &typingSpecialForm{text: "typing.Literal"}
	typingGenericRegistry[litForm] = &typingGenericSpec{origin: litForm, display: "typing.Literal", kind: tgPlain}
	m.Dict["Literal"] = litForm
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
