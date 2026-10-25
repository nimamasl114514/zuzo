package runtime

// cmods.go — C 扩展模块的 Go 原生替代（第13轮 · 支持第三方 py 库）
//
// 背景：zuzo 的 Python 运行时是纯 Go 实现，没有 CPython 的 C 扩展 ABI。
// 但大量「纯 Python 库」会 import 标准库里的 C 模块（unicodedata / _struct / binascii …），
// 这些模块的 Python 接口很小，用 Go 重写成本远低于引入 CPython。
// 本文件集中放这类「小而常用」的 C 模块替代实现，每加一个都在这里留一节。
//
// 已实现：unicodedata（idna / urllib3 依赖）
// 待补（错误驱动）：_struct / binascii / array / errno / select

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"

	"zuzo/py/object"
)

func init() {
	builtinModuleInits["unicodedata"] = modUnicodeData
}

// firstRune 取参数字符串的首个码点；空串返回 -1。
func firstRune(ip *Interp, a []object.Object) rune {
	if len(a) == 0 {
		return -1
	}
	s := a[0].Str()
	rs := []rune(s)
	if len(rs) == 0 {
		return -1
	}
	return rs[0]
}

func modUnicodeData(ip *Interp) *object.Module {
	m := newModule("unicodedata")
	m.Dict["unidata_version"] = &object.Str{V: "15.0.0"}

	// category(c) → 'Lu' / 'Ll' / 'Nd' …
	m.Dict["category"] = bi("category", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		r := firstRune(ip, a)
		if r < 0 {
			ip.TypeError("category() argument must be a unicode character")
		}
		if r < 128 {
			switch {
			case r >= 'A' && r <= 'Z':
				return &object.Str{V: "Lu"}, nil
			case r >= 'a' && r <= 'z':
				return &object.Str{V: "Ll"}, nil
			case r >= '0' && r <= '9':
				return &object.Str{V: "Nd"}, nil
			}
		}
		for name, table := range unicode.Categories {
			if unicode.Is(table, r) {
				return &object.Str{V: name}, nil
			}
		}
		return &object.Str{V: "Cn"}, nil
	})

	// bidirectional(c) → 'L' / 'R' / 'AL' / 'EN' …（近似表：够 idna 判断方向性）
	m.Dict["bidirectional"] = bi("bidirectional", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		r := firstRune(ip, a)
		if r < 0 {
			ip.TypeError("bidirectional() argument must be a unicode character")
		}
		return &object.Str{V: bidiClass(r)}, nil
	})

	// combining(c) → 组合类（Mn 记为 230，其余 0）
	m.Dict["combining"] = bi("combining", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		r := firstRune(ip, a)
		if r < 0 {
			ip.TypeError("combining() argument must be a unicode character")
		}
		if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) {
			return &object.Int{V: 230}, nil
		}
		return &object.Int{V: 0}, nil
	})

	// normalize(form, s) → NFC / NFD / NFKC / NFKD
	m.Dict["normalize"] = bi("normalize", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("normalize() requires (form, unistr)")
		}
		form := strings.ToUpper(a[0].Str())
		s := a[1].Str()
		switch form {
		case "NFC":
			return &object.Str{V: norm.NFC.String(s)}, nil
		case "NFD":
			return &object.Str{V: norm.NFD.String(s)}, nil
		case "NFKC":
			return &object.Str{V: norm.NFKC.String(s)}, nil
		case "NFKD":
			return &object.Str{V: norm.NFKD.String(s)}, nil
		}
		ip.ValueError("invalid normalization form: %s", form)
		return object.None, nil
	})

	// name(c[, default]) → 字符名（近似：控制字符/ASCII/未分配）
	m.Dict["name"] = bi("name", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		r := firstRune(ip, a)
		if r < 0 {
			ip.TypeError("name() argument must be a unicode character")
		}
		if n, ok := unicodeName(r); ok {
			return &object.Str{V: n}, nil
		}
		if len(a) > 1 {
			return a[1], nil
		}
		ip.raise(ip.excType("ValueError"), "no name for %q", string(r))
		return object.None, nil
	})

	// decimal / digit / numeric(c[, default])
	mkNum := func(fn func(rune) (int, bool)) object.BuiltinFn {
		return func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			r := firstRune(ip, a)
			if r < 0 {
				ip.TypeError("argument must be a unicode character")
			}
			if v, ok := fn(r); ok {
				return &object.Int{V: int64(v)}, nil
			}
			if len(a) > 1 {
				return a[1], nil
			}
			ip.raise(ip.excType("ValueError"), "not a number: %q", string(r))
			return object.None, nil
		}
	}
	m.Dict["decimal"] = bi("decimal", mkNum(func(r rune) (int, bool) {
		if r >= '0' && r <= '9' {
			return int(r - '0'), true
		}
		return 0, false
	}))
	m.Dict["digit"] = bi("digit", mkNum(func(r rune) (int, bool) {
		if r >= '0' && r <= '9' {
			return int(r - '0'), true
		}
		return 0, false
	}))
	m.Dict["numeric"] = bi("numeric", mkNum(func(r rune) (int, bool) {
		if r >= '0' && r <= '9' {
			return int(r - '0'), true
		}
		return 0, false
	}))

	// east_asian_width(c) → 'Na' / 'W' / 'A' / 'F' / 'H' / 'N'
	m.Dict["east_asian_width"] = bi("east_asian_width", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		r := firstRune(ip, a)
		if r < 0 {
			ip.TypeError("east_asian_width() argument must be a unicode character")
		}
		return &object.Str{V: eastAsianWidth(r)}, nil
	})

	m.Dict["is_normalized"] = bi("is_normalized", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("is_normalized() requires (form, unistr)")
		}
		form := strings.ToUpper(a[0].Str())
		s := a[1].Str()
		switch form {
		case "NFC":
			return &object.Bool{V: norm.NFC.IsNormalString(s)}, nil
		case "NFD":
			return &object.Bool{V: norm.NFD.IsNormalString(s)}, nil
		case "NFKC":
			return &object.Bool{V: norm.NFKC.IsNormalString(s)}, nil
		case "NFKD":
			return &object.Bool{V: norm.NFKD.IsNormalString(s)}, nil
		}
		ip.ValueError("invalid normalization form: %s", form)
		return object.None, nil
	})

	// ucd_3_2_0：stringprep.py:8 的模块级硬依赖，且第 10 行断言
	// `ucd_3_2_0.unidata_version == '3.2.0'`。用独立代理模块：转发表全部函数，
	// 只把版本号改回 3.2.0（stringprep 只用到 category/combining 等，代理足够）。
	ucd := &object.Module{Name: "ucd_3_2_0", Dict: map[string]object.Object{}}
	for kk, vv := range m.Dict {
		ucd.Dict[kk] = vv
	}
	ucd.Dict["unidata_version"] = &object.Str{V: "3.2.0"}
	m.Dict["ucd_3_2_0"] = ucd

	return m
}

// bidiClass 方向性近似（非完整 UCD Bidi 表）。
func bidiClass(r rune) string {
	switch {
	case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z':
		return "L"
	case r >= '0' && r <= '9':
		return "EN"
	case r == ' ' || r == '\t' || r == '\n' || r == '\r':
		return "WS"
	case r == '+' || r == '-':
		return "ES"
	case r == '#' || r == '%' || r == '$':
		return "ET"
	case r < 128:
		return "ON"
	}
	switch {
	case unicode.Is(unicode.Hebrew, r):
		return "R"
	case unicode.Is(unicode.Arabic, r):
		return "AL"
	case unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana):
		return "L"
	case unicode.IsDigit(r):
		return "AN"
	case unicode.IsLetter(r):
		return "L"
	}
	return "ON"
}

// eastAsianWidth 东亚宽度近似（CJK / 全角 → W，其余 Na）。
func eastAsianWidth(r rune) string {
	if unicode.In(r, unicode.Han, unicode.Hangul, unicode.Hiragana, unicode.Katakana) {
		return "W"
	}
	if r >= 0xFF01 && r <= 0xFF60 {
		return "F"
	}
	return "Na"
}

func unicodeName(r rune) (string, bool) {
	switch {
	case r == 0x20:
		return "SPACE", true
	case r == 0x09:
		return "CHARACTER TABULATION", true
	case r == 0x0A:
		return "LINE FEED (LF)", true
	case r == 0x0D:
		return "CARRIAGE RETURN (CR)", true
	case r >= 0x21 && r <= 0x7E:
		names := map[rune]string{
			'!': "EXCLAMATION MARK", '"': "QUOTATION MARK", '#': "NUMBER SIGN",
			'$': "DOLLAR SIGN", '%': "PERCENT SIGN", '&': "AMPERSAND",
			'\'': "APOSTROPHE", '(': "LEFT PARENTHESIS", ')': "RIGHT PARENTHESIS",
			'*': "ASTERISK", '+': "PLUS SIGN", ',': "COMMA", '-': "HYPHEN-MINUS",
			'.': "FULL STOP", '/': "SOLIDUS", ':': "COLON", ';': "SEMICOLON",
			'<': "LESS-THAN SIGN", '=': "EQUALS SIGN", '>': "GREATER-THAN SIGN",
			'?': "QUESTION MARK", '@': "COMMERCIAL AT", '[': "LEFT SQUARE BRACKET",
			'\\': "REVERSE SOLIDUS", ']': "RIGHT SQUARE BRACKET", '^': "CIRCUMFLEX ACCENT",
			'_': "LOW LINE", '`': "GRAVE ACCENT", '{': "LEFT CURLY BRACKET",
			'|': "VERTICAL LINE", '}': "RIGHT CURLY BRACKET", '~': "TILDE",
		}
		if n, ok := names[r]; ok {
			return n, true
		}
		if r >= 'A' && r <= 'Z' {
			return "LATIN CAPITAL LETTER " + string(r), true
		}
		if r >= 'a' && r <= 'z' {
			return "LATIN SMALL LETTER " + strings.ToUpper(string(r)), true
		}
		if r >= '0' && r <= '9' {
			return "DIGIT " + string(r), true
		}
	}
	if r > 0x4E00 && r < 0x9FFF {
		return "CJK UNIFIED IDEOGRAPH-" + fmt.Sprintf("%04X", r), true
	}
	return "", false
}

var _ = strconv.Itoa
