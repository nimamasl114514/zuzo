package evaluator

// strlib_split.go — 分割 / 连接 / 位置分割族（10 个成员）
//
// 覆盖基准：Python str（split/rsplit/splitlines/join/partition/rpartition）
//          + Go strings（Split/SplitN/Fields/FieldsFunc/Join）

import (
	"strings"

	"zuzo/object"
)

func init() {
	registerStringMethods(map[string]object.BuiltinFunction{
		"分割":    strSplit,
		"分割次":   strSplitN,
		"反向分割":  strRSplit,
		"反向分割次": strRSplitN,
		"按行分割":  strSplitLines,
		"分词":    strFields,
		"分词按":   strFieldsFunc,
		"连接":    builtinJoin, // 复用 builtins_array.go 既有实现（数组, 分隔符）
		"位置分割":  strPartition,
		"反向位置分割": strRPartition,
	})
}

// 分割(s, 分隔符) → 数组[字符串]
func strSplit(args ...object.Object) object.Object {
	if e := argc("分割", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("分割", args, 0, "字符串")
	if e != nil {
		return e
	}
	sep, e := argStr("分割", args, 1, "分隔符")
	if e != nil {
		return e
	}
	return strArray(strings.Split(s, sep))
}

// 分割次(s, 分隔符, n) → 数组 — 最多切 n 段，其余并入最后一段
func strSplitN(args ...object.Object) object.Object {
	if e := argc("分割次", args, 3, 3); e != nil {
		return e
	}
	s, e := argStr("分割次", args, 0, "字符串")
	if e != nil {
		return e
	}
	sep, e := argStr("分割次", args, 1, "分隔符")
	if e != nil {
		return e
	}
	n, e := argInt("分割次", args, 2, "段数")
	if e != nil {
		return e
	}
	if n <= 0 {
		return strArray(nil)
	}
	return strArray(strings.SplitN(s, sep, int(n)))
}

// 反向分割(s, 分隔符) → 数组 — 与 分割 等价（Python rsplit 无 n 时行为一致）
func strRSplit(args ...object.Object) object.Object {
	return strSplit(args...)
}

// 反向分割次(s, 分隔符, n) → 数组 — 从右往左最多切 n 段
func strRSplitN(args ...object.Object) object.Object {
	if e := argc("反向分割次", args, 3, 3); e != nil {
		return e
	}
	s, e := argStr("反向分割次", args, 0, "字符串")
	if e != nil {
		return e
	}
	sep, e := argStr("反向分割次", args, 1, "分隔符")
	if e != nil {
		return e
	}
	n, e := argInt("反向分割次", args, 2, "段数")
	if e != nil {
		return e
	}
	if n <= 0 {
		return strArray(nil)
	}
	if n == 1 || sep == "" {
		return strArray([]string{s})
	}
	// 从右往左找 n-1 个分隔符切点
	cuts := make([]int, 0, n-1)
	from := len(s)
	for int64(len(cuts)) < n-1 {
		idx := strings.LastIndex(s[:from], sep)
		if idx < 0 {
			break
		}
		cuts = append(cuts, idx)
		from = idx
	}
	if len(cuts) == 0 {
		return strArray([]string{s})
	}
	// cuts 从右到左，倒序后按升序切
	parts := make([]string, 0, len(cuts)+1)
	start := 0
	for i := len(cuts) - 1; i >= 0; i-- {
		parts = append(parts, s[start:cuts[i]])
		start = cuts[i] + len(sep)
	}
	parts = append(parts, s[start:])
	return strArray(parts)
}

// 按行分割(s) → 数组 — 按 \n / \r\n / \r 切行，末尾换行不产生空尾项（Python str.splitlines）
func strSplitLines(args ...object.Object) object.Object {
	if e := argc("按行分割", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("按行分割", args, 0, "")
	if e != nil {
		return e
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			out = append(out, s[start:i])
			start = i + 1
		case '\r':
			out = append(out, s[start:i])
			if i+1 < len(s) && s[i+1] == '\n' {
				i++
			}
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	if out == nil {
		out = []string{}
	}
	return strArray(out)
}

// 分词(s) → 数组 — 按 Unicode 空白切词，自动合并连续空白
func strFields(args ...object.Object) object.Object {
	if e := argc("分词", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("分词", args, 0, "")
	if e != nil {
		return e
	}
	return strArray(strings.Fields(s))
}

// 分词按(s, 分隔字符集) → 数组 — 凡落在字符集内的字符都视为分隔符（Go strings.FieldsFunc）
func strFieldsFunc(args ...object.Object) object.Object {
	if e := argc("分词按", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("分词按", args, 0, "字符串")
	if e != nil {
		return e
	}
	seps, e := argStr("分词按", args, 1, "分隔字符集")
	if e != nil {
		return e
	}
	sepSet := make(map[rune]bool, len(seps))
	for _, r := range seps {
		sepSet[r] = true
	}
	if len(sepSet) == 0 {
		return strArray([]string{s})
	}
	return strArray(strings.FieldsFunc(s, func(r rune) bool { return sepSet[r] }))
}

// 位置分割(s, 分隔符) → 数组[前, 分隔符, 后] — 找不到时返回 [s, "", ""]（Python str.partition）
func strPartition(args ...object.Object) object.Object {
	if e := argc("位置分割", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("位置分割", args, 0, "字符串")
	if e != nil {
		return e
	}
	sep, e := argStr("位置分割", args, 1, "分隔符")
	if e != nil {
		return e
	}
	idx := strings.Index(s, sep)
	if idx < 0 {
		return strArray([]string{s, "", ""})
	}
	return strArray([]string{s[:idx], sep, s[idx+len(sep):]})
}

// 反向位置分割(s, 分隔符) → 数组[前, 分隔符, 后] — 找不到时返回 ["", "", s]
func strRPartition(args ...object.Object) object.Object {
	if e := argc("反向位置分割", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("反向位置分割", args, 0, "字符串")
	if e != nil {
		return e
	}
	sep, e := argStr("反向位置分割", args, 1, "分隔符")
	if e != nil {
		return e
	}
	idx := strings.LastIndex(s, sep)
	if idx < 0 {
		return strArray([]string{"", "", s})
	}
	return strArray([]string{s[:idx], sep, s[idx+len(sep):]})
}
