package evaluator

// strlib_search.go — 查找 / 计数 / 替换族（12 个成员）
//
// 覆盖基准：Python str（find/rfind/index/count/replace）
//          + Go strings（Index/LastIndex/IndexAny/LastIndexAny/IndexRune/Contains/ContainsAny/Replace/Replacer/Map）
// 下标口径：全部返回「字符（rune）下标」，与 长度()/取字符() 统一；-1 表示未找到。

import (
	"strings"

	"zuzo/object"
)

func init() {
	registerStringMethods(map[string]object.BuiltinFunction{
		"查找":     strIndex,
		"反向查找":   strLastIndex,
		"查找任一":   strIndexAny,
		"反向查找任一": strLastIndexAny,
		"查找字符":   strIndexRune,
		"包含":     strContains,
		"包含任一":   strContainsAny,
		"计数":     strCount,
		"替换":     strReplace,
		"替换次":    strReplaceN,
		"批量替换":   strReplaceMulti,
		"替换字符":   strTranslate,
	})
}

// runeIndex 把字节下标换算为 rune 下标；-1 原样返回。
func runeIndex(s string, byteIdx int) int64 {
	if byteIdx < 0 {
		return -1
	}
	return int64(len([]rune(s[:byteIdx])))
}

// 查找(s, 子串) → 整数 — 首次出现的字符下标，未找到 -1
func strIndex(args ...object.Object) object.Object {
	if e := argc("查找", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("查找", args, 0, "字符串")
	if e != nil {
		return e
	}
	sub, e := argStr("查找", args, 1, "子串")
	if e != nil {
		return e
	}
	return intObj(runeIndex(s, strings.Index(s, sub)))
}

// 反向查找(s, 子串) → 整数 — 最后一次出现的字符下标，未找到 -1
func strLastIndex(args ...object.Object) object.Object {
	if e := argc("反向查找", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("反向查找", args, 0, "字符串")
	if e != nil {
		return e
	}
	sub, e := argStr("反向查找", args, 1, "子串")
	if e != nil {
		return e
	}
	return intObj(runeIndex(s, strings.LastIndex(s, sub)))
}

// 查找任一(s, 字符集) → 整数 — 任一字符首次出现的下标，未找到 -1
func strIndexAny(args ...object.Object) object.Object {
	if e := argc("查找任一", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("查找任一", args, 0, "字符串")
	if e != nil {
		return e
	}
	chars, e := argStr("查找任一", args, 1, "字符集")
	if e != nil {
		return e
	}
	return intObj(runeIndex(s, strings.IndexAny(s, chars)))
}

// 反向查找任一(s, 字符集) → 整数
func strLastIndexAny(args ...object.Object) object.Object {
	if e := argc("反向查找任一", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("反向查找任一", args, 0, "字符串")
	if e != nil {
		return e
	}
	chars, e := argStr("反向查找任一", args, 1, "字符集")
	if e != nil {
		return e
	}
	return intObj(runeIndex(s, strings.LastIndexAny(s, chars)))
}

// 查找字符(s, 字符) → 整数 — 单字符首次出现的下标（要求恰好一个 rune）
func strIndexRune(args ...object.Object) object.Object {
	if e := argc("查找字符", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("查找字符", args, 0, "字符串")
	if e != nil {
		return e
	}
	ch, e := argStr("查找字符", args, 1, "字符")
	if e != nil {
		return e
	}
	r := []rune(ch)
	if len(r) != 1 {
		return newError("查找字符() 第二个参数必须是单个字符，得到 %d 个字符", len(r))
	}
	return intObj(runeIndex(s, strings.IndexRune(s, r[0])))
}

// 包含(s, 子串) → 布尔
func strContains(args ...object.Object) object.Object {
	if e := argc("包含", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("包含", args, 0, "字符串")
	if e != nil {
		return e
	}
	sub, e := argStr("包含", args, 1, "子串")
	if e != nil {
		return e
	}
	return boolObj(strings.Contains(s, sub))
}

// 包含任一(s, 字符集) → 布尔
func strContainsAny(args ...object.Object) object.Object {
	if e := argc("包含任一", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("包含任一", args, 0, "字符串")
	if e != nil {
		return e
	}
	chars, e := argStr("包含任一", args, 1, "字符集")
	if e != nil {
		return e
	}
	return boolObj(strings.ContainsAny(s, chars))
}

// 计数(s, 子串) → 整数
func strCount(args ...object.Object) object.Object {
	if e := argc("计数", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("计数", args, 0, "字符串")
	if e != nil {
		return e
	}
	sub, e := argStr("计数", args, 1, "子串")
	if e != nil {
		return e
	}
	return intObj(int64(strings.Count(s, sub)))
}

// 替换(s, 旧, 新) → 字符串 — 全部替换
func strReplace(args ...object.Object) object.Object {
	if e := argc("替换", args, 3, 3); e != nil {
		return e
	}
	s, e := argStr("替换", args, 0, "字符串")
	if e != nil {
		return e
	}
	old, e := argStr("替换", args, 1, "旧文本")
	if e != nil {
		return e
	}
	nw, e := argStr("替换", args, 2, "新文本")
	if e != nil {
		return e
	}
	return strObj(strings.ReplaceAll(s, old, nw))
}

// 替换次(s, 旧, 新, n) → 字符串 — 最多替换 n 次；n<0 表示全部
func strReplaceN(args ...object.Object) object.Object {
	if e := argc("替换次", args, 4, 4); e != nil {
		return e
	}
	s, e := argStr("替换次", args, 0, "字符串")
	if e != nil {
		return e
	}
	old, e := argStr("替换次", args, 1, "旧文本")
	if e != nil {
		return e
	}
	nw, e := argStr("替换次", args, 2, "新文本")
	if e != nil {
		return e
	}
	n, e := argInt("替换次", args, 3, "次数")
	if e != nil {
		return e
	}
	return strObj(strings.Replace(s, old, nw, int(n)))
}

// 批量替换(s, 规则哈希) → 字符串 — 一次性并行替换，不递归（Go strings.Replacer）
// 规则哈希形如 {"a": "1", "b": "2"}
func strReplaceMulti(args ...object.Object) object.Object {
	if e := argc("批量替换", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("批量替换", args, 0, "字符串")
	if e != nil {
		return e
	}
	h, e := argHash("批量替换", args, 1, "规则哈希")
	if e != nil {
		return e
	}
	pairs := make([]string, 0, len(h.Pairs)*2)
	for _, pair := range h.Pairs {
		k, ok := pair.Key.(*object.String)
		if !ok {
			return newError("批量替换() 规则哈希的键必须是字符串，得到 %s", pair.Key.Type())
		}
		v, ok := pair.Value.(*object.String)
		if !ok {
			return newError("批量替换() 规则哈希的值必须是字符串，得到 %s", pair.Value.Type())
		}
		pairs = append(pairs, k.Value, v.Value)
	}
	if len(pairs) == 0 {
		return strObj(s)
	}
	return strObj(strings.NewReplacer(pairs...).Replace(s))
}

// 替换字符(s, 映射哈希) → 字符串 — 逐字符查表映射，未命中保留原字符（Python str.translate 子集）
func strTranslate(args ...object.Object) object.Object {
	if e := argc("替换字符", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("替换字符", args, 0, "字符串")
	if e != nil {
		return e
	}
	h, e := argHash("替换字符", args, 1, "映射哈希")
	if e != nil {
		return e
	}
	m := make(map[rune]string, len(h.Pairs))
	for _, pair := range h.Pairs {
		k, ok := pair.Key.(*object.String)
		if !ok {
			return newError("替换字符() 映射哈希的键必须是字符串，得到 %s", pair.Key.Type())
		}
		v, ok := pair.Value.(*object.String)
		if !ok {
			return newError("替换字符() 映射哈希的值必须是字符串，得到 %s", pair.Value.Type())
		}
		kr := []rune(k.Value)
		if len(kr) != 1 {
			return newError("替换字符() 映射哈希的键必须是单个字符，得到 %q", k.Value)
		}
		m[kr[0]] = v.Value
	}
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if v, ok := m[r]; ok {
			b.WriteString(v)
		} else {
			b.WriteRune(r)
		}
	}
	return strObj(b.String())
}
