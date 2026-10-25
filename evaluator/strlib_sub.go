package evaluator

// strlib_sub.go — 子串 / 切片 / 反转族（5 个成员）
//
// 覆盖基准：Python 切片语义（支持负索引、越界截断）+ 取左/取右便捷函数。
// 下标口径：全部按 rune（字符），与 长度()/查找() 一致。

import (
	"zuzo/object"
)

func init() {
	registerStringMethods(map[string]object.BuiltinFunction{
		"取字符": strCharAt,
		"子串":  strSubstr,
		"取左":  strLeft,
		"取右":  strRight,
		"反转":  strReverse,
	})
}

// 取字符(s, i) → 字符串 — 第 i 个字符（rune 下标，越界报错）
func strCharAt(args ...object.Object) object.Object {
	if e := argc("取字符", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("取字符", args, 0, "字符串")
	if e != nil {
		return e
	}
	idx, e := argInt("取字符", args, 1, "下标")
	if e != nil {
		return e
	}
	runes := []rune(s)
	if idx < 0 || idx >= int64(len(runes)) {
		return newError("取字符() 索引越界: %d（字符串长度 %d）", idx, len(runes))
	}
	return strObj(string(runes[idx]))
}

// 子串(s, 起, 止?) → 字符串 — Python 切片语义：负索引从尾部计，越界截断，起>止返回空串
func strSubstr(args ...object.Object) object.Object {
	if e := argc("子串", args, 2, 3); e != nil {
		return e
	}
	s, e := argStr("子串", args, 0, "字符串")
	if e != nil {
		return e
	}
	start, e := argInt("子串", args, 1, "起始下标")
	if e != nil {
		return e
	}
	runes := []rune(s)
	n := int64(len(runes))
	end := n
	if len(args) > 2 {
		end, e = argInt("子串", args, 2, "结束下标")
		if e != nil {
			return e
		}
	}
	// Python 语义归一
	if start < 0 {
		start += n
	}
	if end < 0 {
		end += n
	}
	if start < 0 {
		start = 0
	}
	if end > n {
		end = n
	}
	if start > end || start >= n {
		return strObj("")
	}
	return strObj(string(runes[start:end]))
}

// 取左(s, n) → 字符串 — 前 n 个字符（n 超长则全取）
func strLeft(args ...object.Object) object.Object {
	if e := argc("取左", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("取左", args, 0, "字符串")
	if e != nil {
		return e
	}
	n, e := argInt("取左", args, 1, "字符数")
	if e != nil {
		return e
	}
	if n <= 0 {
		return strObj("")
	}
	runes := []rune(s)
	if int64(len(runes)) <= n {
		return strObj(s)
	}
	return strObj(string(runes[:n]))
}

// 取右(s, n) → 字符串 — 后 n 个字符（n 超长则全取）
func strRight(args ...object.Object) object.Object {
	if e := argc("取右", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("取右", args, 0, "字符串")
	if e != nil {
		return e
	}
	n, e := argInt("取右", args, 1, "字符数")
	if e != nil {
		return e
	}
	if n <= 0 {
		return strObj("")
	}
	runes := []rune(s)
	if int64(len(runes)) <= n {
		return strObj(s)
	}
	return strObj(string(runes[int64(len(runes))-n:]))
}

// 反转(s) → 字符串 — 按 rune 反转（中文安全）
func strReverse(args ...object.Object) object.Object {
	if e := argc("反转", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("反转", args, 0, "")
	if e != nil {
		return e
	}
	runes := []rune(s)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	return strObj(string(runes))
}
