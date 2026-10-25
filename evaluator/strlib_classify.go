package evaluator

// strlib_classify.go — 字符判定族（12 个成员，对齐 Python str.is* 全量）
//
// 语义要点（与 CPython 一致）：
//   - 空串一律返回 假
//   - 是数字/是十进制/是数值 三档：Nd < (Nd|No) < (Nd|Nl|No)
//   - 是大写/是小写：至少一个「有大小写」字符，且全部有大小写字符同向（中文等无大小写字符不影响）

import (
	"unicode"

	"zuzo/object"
)

func init() {
	registerStringMethods(map[string]object.BuiltinFunction{
		"是数字":   strIsDigit,
		"是十进制":  strIsDecimal,
		"是数值":   strIsNumeric,
		"是字母":   strIsAlpha,
		"是字母数字": strIsAlnum,
		"是空白":   strIsSpace,
		"是大写":   strIsUpper,
		"是小写":   strIsLower,
		"是标题":   strIsTitle,
		"是可打印":  strIsPrintable,
		"是标识符":  strIsIdentifier,
		"是ASCII": strIsASCII,
	})
}

// eachRune 对非空字符串逐字符应用判定；空串返回 false。
func eachRune(s string, pred func(rune) bool) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !pred(r) {
			return false
		}
	}
	return true
}

// 是数字(s) → 布尔 — 全部为 Nd 或 No（Python str.isdigit）
func strIsDigit(args ...object.Object) object.Object {
	if e := argc("是数字", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是数字", args, 0, "")
	if e != nil {
		return e
	}
	return boolObj(eachRune(s, func(r rune) bool {
		return unicode.Is(unicode.Nd, r) || unicode.Is(unicode.No, r)
	}))
}

// 是十进制(s) → 布尔 — 全部为 Nd（Python str.isdecimal）
func strIsDecimal(args ...object.Object) object.Object {
	if e := argc("是十进制", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是十进制", args, 0, "")
	if e != nil {
		return e
	}
	return boolObj(eachRune(s, func(r rune) bool { return unicode.Is(unicode.Nd, r) }))
}

// 是数值(s) → 布尔 — 全部为 Nd/Nl/No（Python str.isnumeric）
func strIsNumeric(args ...object.Object) object.Object {
	if e := argc("是数值", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是数值", args, 0, "")
	if e != nil {
		return e
	}
	return boolObj(eachRune(s, unicode.IsNumber))
}

// 是字母(s) → 布尔 — 全部为 Unicode 字母（Python str.isalpha）
func strIsAlpha(args ...object.Object) object.Object {
	if e := argc("是字母", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是字母", args, 0, "")
	if e != nil {
		return e
	}
	return boolObj(eachRune(s, unicode.IsLetter))
}

// 是字母数字(s) → 布尔 — 全为字母或数字（Python str.isalnum）
func strIsAlnum(args ...object.Object) object.Object {
	if e := argc("是字母数字", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是字母数字", args, 0, "")
	if e != nil {
		return e
	}
	return boolObj(eachRune(s, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsNumber(r)
	}))
}

// 是空白(s) → 布尔 — 全为空白（Python str.isspace）
func strIsSpace(args ...object.Object) object.Object {
	if e := argc("是空白", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是空白", args, 0, "")
	if e != nil {
		return e
	}
	return boolObj(eachRune(s, unicode.IsSpace))
}

// isUpperStr Python 语义：至少一个 cased 字符，且所有 cased 字符都是大写。
func isUpperStr(s string) bool {
	hasCased := false
	for _, r := range s {
		if unicode.IsLower(r) {
			return false
		}
		if unicode.IsUpper(r) || unicode.IsTitle(r) {
			hasCased = true
		}
	}
	return hasCased
}

// isLowerStr 同 isUpperStr 的反向。
func isLowerStr(s string) bool {
	hasCased := false
	for _, r := range s {
		if unicode.IsUpper(r) || unicode.IsTitle(r) {
			return false
		}
		if unicode.IsLower(r) {
			hasCased = true
		}
	}
	return hasCased
}

// 是大写(s) → 布尔（Python str.isupper）
func strIsUpper(args ...object.Object) object.Object {
	if e := argc("是大写", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是大写", args, 0, "")
	if e != nil {
		return e
	}
	return boolObj(isUpperStr(s))
}

// 是小写(s) → 布尔（Python str.islower）
func strIsLower(args ...object.Object) object.Object {
	if e := argc("是小写", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是小写", args, 0, "")
	if e != nil {
		return e
	}
	return boolObj(isLowerStr(s))
}

// 是标题(s) → 布尔 — 每个词首大写、其余小写，且至少一个 cased（Python str.istitle）
func strIsTitle(args ...object.Object) object.Object {
	if e := argc("是标题", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是标题", args, 0, "")
	if e != nil {
		return e
	}
	hasCased := false
	prevCased := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			if prevCased {
				if !unicode.IsLower(r) {
					return boolObj(false)
				}
			} else {
				if !unicode.IsUpper(r) && !unicode.IsTitle(r) {
					return boolObj(false)
				}
				hasCased = true
			}
			prevCased = true
		} else {
			prevCased = false
		}
	}
	return boolObj(hasCased)
}

// isPrintableRune 可打印判定：排除控制字符、格式字符与零宽字符。
func isPrintableRune(r rune) bool {
	if r == ' ' {
		return true
	}
	if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) {
		return false
	}
	switch r {
	case '\u200b', '\u200c', '\u200d', '\ufeff':
		return false
	}
	return true
}

// 是可打印(s) → 布尔 — 全部字符可打印（Python str.isprintable）
func strIsPrintable(args ...object.Object) object.Object {
	if e := argc("是可打印", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是可打印", args, 0, "")
	if e != nil {
		return e
	}
	if s == "" {
		return boolObj(true) // Python: ''.isprintable() → True
	}
	for _, r := range s {
		if !isPrintableRune(r) {
			return boolObj(false)
		}
	}
	return boolObj(true)
}

// 是标识符(s) → 布尔 — 合法 zuzo/Python 标识符：首字符字母或下划线，其余字母/数字/下划线
func strIsIdentifier(args ...object.Object) object.Object {
	if e := argc("是标识符", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是标识符", args, 0, "")
	if e != nil {
		return e
	}
	if s == "" {
		return boolObj(false)
	}
	for i, r := range s {
		if i == 0 {
			if r != '_' && !unicode.IsLetter(r) {
				return boolObj(false)
			}
			continue
		}
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsNumber(r) {
			return boolObj(false)
		}
	}
	return boolObj(true)
}

// 是ASCII(s) → 布尔 — 全部字符码点 < 128（Python str.isascii；空串为真）
func strIsASCII(args ...object.Object) object.Object {
	if e := argc("是ASCII", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("是ASCII", args, 0, "")
	if e != nil {
		return e
	}
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return boolObj(false)
		}
	}
	return boolObj(true)
}
