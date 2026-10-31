package evaluator

// strlib_align.go — 对齐 / 填充族（6 个成员）
//
// 覆盖基准：Python str（ljust/rjust/center/zfill/expandtabs）
//          + Go strings（Repeat）
// 宽度口径：按 rune 个数（与 Python 一致）；中文显示宽度对齐见 文本.按宽度填充

import (
	"strings"

	"zuzo/object"
)

func init() {
	registerStringMethods(map[string]object.BuiltinFunction{
		"左对齐":   strLJust,
		"右对齐":   strRJust,
		"居中":    strCenter,
		"零填充":   strZFill,
		"重复":    strRepeat,
		"展开制表符": strExpandTabs,
	})
}

// padTo 把 s 用 fill 重复填充到 width 个 rune；s 已够宽则原样返回。
// left/right 为左右两侧各自要补的 rune 数。
func padTo(s string, left, right int, fill string) string {
	fr := []rune(fill)
	if fr == nil {
		fr = []rune{' '}
	}
	var b strings.Builder
	b.Grow(len(s) + (left+right)*len(fill))
	writePad := func(n int, start int) {
		for i := 0; i < n; i++ {
			b.WriteRune(fr[(start+i)%len(fr)])
		}
	}
	writePad(left, 0)
	b.WriteString(s)
	writePad(right, left)
	return b.String()
}

// 左对齐(s, 总宽, 填充=" ") → 字符串 — 右侧补足到总宽（Python str.ljust）
func strLJust(args ...object.Object) object.Object {
	if e := argc("左对齐", args, 2, 3); e != nil {
		return e
	}
	s, e := argStr("左对齐", args, 0, "字符串")
	if e != nil {
		return e
	}
	w, e := argInt("左对齐", args, 1, "总宽")
	if e != nil {
		return e
	}
	fill, e := optStr("左对齐", args, 2, "填充", " ")
	if e != nil {
		return e
	}
	n := len([]rune(s))
	if int64(n) >= w {
		return strObj(s)
	}
	return strObj(padTo(s, 0, int(w)-n, fill))
}

// 右对齐(s, 总宽, 填充=" ") → 字符串 — 左侧补足（Python str.rjust）
func strRJust(args ...object.Object) object.Object {
	if e := argc("右对齐", args, 2, 3); e != nil {
		return e
	}
	s, e := argStr("右对齐", args, 0, "字符串")
	if e != nil {
		return e
	}
	w, e := argInt("右对齐", args, 1, "总宽")
	if e != nil {
		return e
	}
	fill, e := optStr("右对齐", args, 2, "填充", " ")
	if e != nil {
		return e
	}
	n := len([]rune(s))
	if int64(n) >= w {
		return strObj(s)
	}
	return strObj(padTo(s, int(w)-n, 0, fill))
}

// 居中(s, 总宽, 填充=" ") → 字符串 — 两侧均分，多出的 1 个补在右侧（Python str.center）
func strCenter(args ...object.Object) object.Object {
	if e := argc("居中", args, 2, 3); e != nil {
		return e
	}
	s, e := argStr("居中", args, 0, "字符串")
	if e != nil {
		return e
	}
	w, e := argInt("居中", args, 1, "总宽")
	if e != nil {
		return e
	}
	fill, e := optStr("居中", args, 2, "填充", " ")
	if e != nil {
		return e
	}
	n := len([]rune(s))
	if int64(n) >= w {
		return strObj(s)
	}
	need := int(w) - n
	// 严格对齐 CPython：left = need//2 + (need & width & 1)
	left := need/2 + (need & int(w) & 1)
	return strObj(padTo(s, left, need-left, fill))
}

// 零填充(s, 总宽) → 字符串 — 符号位保留，其余左侧补 0（Python str.zfill）
func strZFill(args ...object.Object) object.Object {
	if e := argc("零填充", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("零填充", args, 0, "字符串")
	if e != nil {
		return e
	}
	w, e := argInt("零填充", args, 1, "总宽")
	if e != nil {
		return e
	}
	n := int64(len([]rune(s)))
	if n >= w {
		return strObj(s)
	}
	sign := ""
	body := s
	if strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-") {
		sign = s[:1]
		body = s[1:]
	}
	return strObj(sign + strings.Repeat("0", int(w-n)) + body)
}

// 重复(s, n) → 字符串 — 重复 n 次；n<0 报错（Python 语义为返回空串，这里保留原报错口径）
func strRepeat(args ...object.Object) object.Object {
	if e := argc("重复", args, 2, 2); e != nil {
		return e
	}
	s, e := argStr("重复", args, 0, "字符串")
	if e != nil {
		return e
	}
	n, e := argInt("重复", args, 1, "次数")
	if e != nil {
		return e
	}
	if n < 0 {
		return newError("重复() 次数不能为负数: %d", n)
	}
	return strObj(strings.Repeat(s, int(n)))
}

// 展开制表符(s, 制表宽=8) → 字符串 — 按列位置补空格到下一个制表位（Python str.expandtabs）
func strExpandTabs(args ...object.Object) object.Object {
	if e := argc("展开制表符", args, 1, 2); e != nil {
		return e
	}
	s, e := argStr("展开制表符", args, 0, "")
	if e != nil {
		return e
	}
	tab := int64(8)
	if len(args) > 1 {
		tab, e = argInt("展开制表符", args, 1, "制表宽")
		if e != nil {
			return e
		}
	}
	if tab <= 0 {
		return strObj(s)
	}
	var b strings.Builder
	col := int64(0)
	for _, r := range s {
		switch r {
		case '\t':
			pad := tab - col%tab
			b.WriteString(strings.Repeat(" ", int(pad)))
			col += pad
		case '\n', '\r':
			b.WriteRune(r)
			col = 0
		default:
			b.WriteRune(r)
			col++
		}
	}
	return strObj(b.String())
}
