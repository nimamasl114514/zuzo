package evaluator

// strlib_trim.go — 裁剪 / 前后缀族（12 个成员）
//
// 覆盖基准：Python str（strip/lstrip/rstrip/removeprefix/removesuffix/startswith/endswith）
//          + Go strings（Trim/TrimLeft/TrimRight/TrimPrefix/TrimSuffix）

import (
	"strings"
	"unicode"

	"zuzo/object"
)

func init() {
	registerStringMethods(map[string]object.BuiltinFunction{
		"去空白":   strTrimSpace,
		"裁剪":    strTrim,
		"左裁剪":   strTrimLeft,
		"右裁剪":   strTrimRight,
		"去左空白":  strTrimLeftSpace,
		"去右空白":  strTrimRightSpace,
		"去前缀":   strTrimPrefix,
		"去后缀":   strTrimSuffix,
		"前缀":    strHasPrefix,
		"后缀":    strHasSuffix,
		"前缀任一":  strHasPrefixAny,
		"后缀任一":  strHasSuffixAny,
		"去除不可见": builtinRemoveInvisible, // 复用 builtins_utf8.go 既有实现
	})
}

// 去空白(s) → 字符串 — 去掉首尾 Unicode 空白
func strTrimSpace(args ...object.Object) object.Object {
	if e := argc("去空白", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("去空白", args, 0, "")
	if e != nil {
		return e
	}
	return strObj(strings.TrimSpace(s))
}

// 裁剪(s, 字符集) → 字符串 — 去掉首尾出现的「字符集」内任意字符
func strTrim(args ...object.Object) object.Object {
	if e := argc("裁剪", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("裁剪", args, 0, "字符串")
	if e != nil {
		return e
	}
	cut, e := argStr("裁剪", args, 1, "字符集")
	if e != nil {
		return e
	}
	return strObj(strings.Trim(s, cut))
}

// 左裁剪(s, 字符集) → 字符串 — 只去左侧
func strTrimLeft(args ...object.Object) object.Object {
	if e := argc("左裁剪", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("左裁剪", args, 0, "字符串")
	if e != nil {
		return e
	}
	cut, e := argStr("左裁剪", args, 1, "字符集")
	if e != nil {
		return e
	}
	return strObj(strings.TrimLeft(s, cut))
}

// 右裁剪(s, 字符集) → 字符串 — 只去右侧
func strTrimRight(args ...object.Object) object.Object {
	if e := argc("右裁剪", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("右裁剪", args, 0, "字符串")
	if e != nil {
		return e
	}
	cut, e := argStr("右裁剪", args, 1, "字符集")
	if e != nil {
		return e
	}
	return strObj(strings.TrimRight(s, cut))
}

// 去左空白(s) → 字符串
func strTrimLeftSpace(args ...object.Object) object.Object {
	if e := argc("去左空白", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("去左空白", args, 0, "")
	if e != nil {
		return e
	}
	return strObj(strings.TrimLeftFunc(s, unicode.IsSpace))
}

// 去右空白(s) → 字符串
func strTrimRightSpace(args ...object.Object) object.Object {
	if e := argc("去右空白", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("去右空白", args, 0, "")
	if e != nil {
		return e
	}
	return strObj(strings.TrimRightFunc(s, unicode.IsSpace))
}

// 去前缀(s, 前缀) → 字符串 — 有前缀则剥掉一次，无则原样返回（Python removeprefix）
func strTrimPrefix(args ...object.Object) object.Object {
	if e := argc("去前缀", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("去前缀", args, 0, "字符串")
	if e != nil {
		return e
	}
	p, e := argStr("去前缀", args, 1, "前缀")
	if e != nil {
		return e
	}
	return strObj(strings.TrimPrefix(s, p))
}

// 去后缀(s, 后缀) → 字符串（Python removesuffix）
func strTrimSuffix(args ...object.Object) object.Object {
	if e := argc("去后缀", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("去后缀", args, 0, "字符串")
	if e != nil {
		return e
	}
	p, e := argStr("去后缀", args, 1, "后缀")
	if e != nil {
		return e
	}
	return strObj(strings.TrimSuffix(s, p))
}

// 前缀(s, 前缀) → 布尔
func strHasPrefix(args ...object.Object) object.Object {
	if e := argc("前缀", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("前缀", args, 0, "字符串")
	if e != nil {
		return e
	}
	p, e := argStr("前缀", args, 1, "前缀")
	if e != nil {
		return e
	}
	return boolObj(strings.HasPrefix(s, p))
}

// 后缀(s, 后缀) → 布尔
func strHasSuffix(args ...object.Object) object.Object {
	if e := argc("后缀", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("后缀", args, 0, "字符串")
	if e != nil {
		return e
	}
	p, e := argStr("后缀", args, 1, "后缀")
	if e != nil {
		return e
	}
	return boolObj(strings.HasSuffix(s, p))
}

// 前缀任一(s, 前缀数组) → 布尔 — 命中任一即真
func strHasPrefixAny(args ...object.Object) object.Object {
	if e := argc("前缀任一", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("前缀任一", args, 0, "字符串")
	if e != nil {
		return e
	}
	ps, e := argStrs("前缀任一", args, 1, "前缀数组")
	if e != nil {
		return e
	}
	for _, p := range ps {
		if strings.HasPrefix(s, p) {
			return boolObj(true)
		}
	}
	return boolObj(false)
}

// 后缀任一(s, 后缀数组) → 布尔
func strHasSuffixAny(args ...object.Object) object.Object {
	if e := argc("后缀任一", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("后缀任一", args, 0, "字符串")
	if e != nil {
		return e
	}
	ps, e := argStrs("后缀任一", args, 1, "后缀数组")
	if e != nil {
		return e
	}
	for _, p := range ps {
		if strings.HasSuffix(s, p) {
			return boolObj(true)
		}
	}
	return boolObj(false)
}
