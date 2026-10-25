package evaluator

// strlib_case.go — 大小写族（8 个成员）
//
// 覆盖基准：Python str（upper/lower/capitalize/title/swapcase/casefold）
//          + Go strings（EqualFold/Compare）

import (
	"strings"
	"unicode"

	"golang.org/x/text/cases"

	"zuzo/object"
)

// foldCaser Unicode casefold 单例（x/text 已在本项目依赖内，零新增依赖）
var foldCaser = cases.Fold()

func init() {
	registerStringMethods(map[string]object.BuiltinFunction{
		"大写":      strUpper,
		"小写":      strLower,
		"首字母大写":   strCapitalize,
		"标题化":     strTitle,
		"大小写互换":   strSwapCase,
		"大小写折叠":   strCasefold,
		"忽略大小写相等": strEqualFold,
		"比较":      strCompare,
	})
}

// 大写(s) → 字符串 — 全大写（Unicode）
func strUpper(args ...object.Object) object.Object {
	if e := argc("大写", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("大写", args, 0, "")
	if e != nil {
		return e
	}
	return strObj(strings.ToUpper(s))
}

// 小写(s) → 字符串 — 全小写（Unicode）
func strLower(args ...object.Object) object.Object {
	if e := argc("小写", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("小写", args, 0, "")
	if e != nil {
		return e
	}
	return strObj(strings.ToLower(s))
}

// 首字母大写(s) → 字符串 — 首字符大写，其余全部小写（Python str.capitalize）
func strCapitalize(args ...object.Object) object.Object {
	if e := argc("首字母大写", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("首字母大写", args, 0, "")
	if e != nil {
		return e
	}
	r := []rune(s)
	if len(r) == 0 {
		return strObj("")
	}
	return strObj(strings.ToUpper(string(r[0])) + strings.ToLower(string(r[1:])))
}

// 标题化(s) → 字符串 — 每个「词」首字母大写、其余小写；词边界 = 任意非字母字符
// （对齐 Python str.title：'they're' → 'They'Re'）
func strTitle(args ...object.Object) object.Object {
	if e := argc("标题化", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("标题化", args, 0, "")
	if e != nil {
		return e
	}
	var b strings.Builder
	b.Grow(len(s))
	prevCased := false
	for _, r := range s {
		if unicode.IsLetter(r) {
			if prevCased {
				b.WriteRune(unicode.ToLower(r))
			} else {
				b.WriteRune(unicode.ToUpper(r))
			}
			prevCased = true
		} else {
			b.WriteRune(r)
			prevCased = false
		}
	}
	return strObj(b.String())
}

// 大小写互换(s) → 字符串 — 大写转小写、小写转大写（Python str.swapcase）
func strSwapCase(args ...object.Object) object.Object {
	if e := argc("大小写互换", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("大小写互换", args, 0, "")
	if e != nil {
		return e
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		case unicode.IsUpper(r):
			b.WriteRune(unicode.ToLower(r))
		case unicode.IsLower(r):
			b.WriteRune(unicode.ToUpper(r))
		default:
			b.WriteRune(r)
		}
	}
	return strObj(b.String())
}

// 大小写折叠(s) → 字符串 — Unicode casefold（'ß' → 'ss'；Python str.casefold）
func strCasefold(args ...object.Object) object.Object {
	if e := argc("大小写折叠", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("大小写折叠", args, 0, "")
	if e != nil {
		return e
	}
	return strObj(foldCaser.String(s))
}

// 忽略大小写相等(a, b) → 布尔 — Unicode 简单折叠比较（Go strings.EqualFold）
func strEqualFold(args ...object.Object) object.Object {
	if e := argc("忽略大小写相等", args, 2, 2); e != nil {
		return e
	}
	a, e := argStr("忽略大小写相等", args, 0, "a")
	if e != nil {
		return e
	}
	b, e := argStr("忽略大小写相等", args, 1, "b")
	if e != nil {
		return e
	}
	return boolObj(strings.EqualFold(a, b))
}

// 比较(a, b) → 整数 — 字典序比较：a<b 返回 -1，相等 0，a>b 返回 1（Go strings.Compare）
func strCompare(args ...object.Object) object.Object {
	if e := argc("比较", args, 2, 2); e != nil {
		return e
	}
	a, e := argStr("比较", args, 0, "a")
	if e != nil {
		return e
	}
	b, e := argStr("比较", args, 1, "b")
	if e != nil {
		return e
	}
	return intObj(int64(strings.Compare(a, b)))
}
