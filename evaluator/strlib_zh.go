package evaluator

// strlib_zh.go — 中文友好族（8 个成员）
//
// 这是 zuzo 作为中文编程语言的差异化能力：
//   - 显示宽度：东亚宽字符（汉字/全角标点/假名）计 2 列，组合字符计 0 列
//   - 按宽度截断 / 填充：终端表格对齐的正确做法（按 rune 数会错位）
//   - 中文数字 ↔ 阿拉伯数字：'一百二十三' ↔ 123
//   - 包含中文：判断是否含 CJK 汉字
//   - 全角转半角 / 半角转全角：复用 builtins_utf8.go 既有实现

import (
	"strings"
	"unicode"

	"golang.org/x/text/width"

	"zuzo/object"
)

func init() {
	registerStringMethods(map[string]object.BuiltinFunction{
		"显示宽度":  strDisplayWidth,
		"按宽度截断": strTruncateWidth,
		"按宽度填充": strPadWidth,
		"中文数字":  strParseChineseNumber,
		"中文数字串": strChineseNumber,
		"包含中文":  strContainsHan,
		"全角转半角": builtinFullToHalf, // 复用 builtins_utf8.go
		"半角转全角": builtinHalfToFull, // 复用 builtins_utf8.go
	})
}

// runeWidth 单字符显示宽度：东亚宽/全角 = 2，组合/格式/控制 = 0，其余 = 1。
func runeWidth(r rune) int {
	if r == 0 {
		return 0
	}
	if unicode.IsControl(r) {
		return 0
	}
	if unicode.Is(unicode.Mn, r) || unicode.Is(unicode.Me, r) || unicode.Is(unicode.Cf, r) {
		return 0
	}
	switch width.LookupRune(r).Kind() {
	case width.EastAsianWide, width.EastAsianFullwidth:
		return 2
	}
	return 1
}

// displayWidth 字符串显示宽度（终端列数）。
func displayWidth(s string) int {
	w := 0
	for _, r := range s {
		w += runeWidth(r)
	}
	return w
}

// 显示宽度(s) → 整数 — 终端列数：汉字/全角标点 2 列，ASCII 1 列，组合字符 0 列
func strDisplayWidth(args ...object.Object) object.Object {
	if e := argc("显示宽度", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("显示宽度", args, 0, "")
	if e != nil {
		return e
	}
	return intObj(int64(displayWidth(s)))
}

// 按宽度截断(s, 最大宽, 省略="") → 字符串 — 超出最大显示宽度时截断并追加省略标记
func strTruncateWidth(args ...object.Object) object.Object {
	if e := argc("按宽度截断", args, 2, 3); e != nil {
		return e
	}
	s, e := argStr("按宽度截断", args, 0, "字符串")
	if e != nil {
		return e
	}
	maxW, e := argInt("按宽度截断", args, 1, "最大宽")
	if e != nil {
		return e
	}
	ellipsis, e := optStr("按宽度截断", args, 2, "省略标记", "")
	if e != nil {
		return e
	}
	if maxW <= 0 {
		return strObj("")
	}
	if int64(displayWidth(s)) <= maxW {
		return strObj(s)
	}
	limit := int(maxW) - displayWidth(ellipsis)
	if limit < 0 {
		limit = 0
	}
	var b strings.Builder
	w := 0
	for _, r := range s {
		rw := runeWidth(r)
		if w+rw > limit {
			break
		}
		b.WriteRune(r)
		w += rw
	}
	return strObj(b.String() + ellipsis)
}

// 按宽度填充(s, 总宽, 填充=" ", 方向="左") → 字符串 — 按显示宽度补足；方向：左/右/中
func strPadWidth(args ...object.Object) object.Object {
	if e := argc("按宽度填充", args, 2, 4); e != nil {
		return e
	}
	s, e := argStr("按宽度填充", args, 0, "字符串")
	if e != nil {
		return e
	}
	total, e := argInt("按宽度填充", args, 1, "总宽")
	if e != nil {
		return e
	}
	fill, e := optStr("按宽度填充", args, 2, "填充", " ")
	if e != nil {
		return e
	}
	dir, e := optStr("按宽度填充", args, 3, "方向", "左")
	if e != nil {
		return e
	}
	if fill == "" {
		fill = " "
	}
	fw := displayWidth(fill)
	if fw <= 0 {
		fw = 1
	}
	cur := displayWidth(s)
	if int64(cur) >= total {
		return strObj(s)
	}
	needCols := int(total) - cur
	// 填充单元个数（按显示宽度换算，向上取整保证补足）
	nUnits := (needCols + fw - 1) / fw
	pad := func(n int) string {
		if n <= 0 {
			return ""
		}
		return strings.Repeat(fill, n)
	}
	switch dir {
	case "右":
		return strObj(pad(nUnits) + s)
	case "中":
		left := nUnits / 2
		return strObj(pad(left) + s + pad(nUnits-left))
	default: // "左"
		return strObj(s + pad(nUnits))
	}
}

// 包含中文(s) → 布尔 — 是否含 CJK 汉字
func strContainsHan(args ...object.Object) object.Object {
	if e := argc("包含中文", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("包含中文", args, 0, "")
	if e != nil {
		return e
	}
	for _, r := range s {
		if unicode.Is(unicode.Han, r) {
			return boolObj(true)
		}
	}
	return boolObj(false)
}

// ==================== 中文数字 ↔ 阿拉伯数字 ====================

var zhDigitVal = map[rune]int64{
	'零': 0, '〇': 0, '一': 1, '壹': 1, '二': 2, '贰': 2, '两': 2,
	'三': 3, '叁': 3, '四': 4, '肆': 4, '五': 5, '伍': 5,
	'六': 6, '陆': 6, '七': 7, '柒': 7, '八': 8, '捌': 8, '九': 9, '玖': 9,
}

var zhUnitVal = map[rune]int64{
	'十': 10, '拾': 10, '百': 100, '佰': 100, '千': 1000, '仟': 1000,
	'万': 10000, '亿': 100000000,
}

// parseChineseNumber 解析中文数字（支持 零/一…九/十百千万亿/两/大写数字/负号）。
func parseChineseNumber(s string) (int64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	neg := false
	if strings.HasPrefix(s, "负") {
		neg = true
		s = strings.TrimPrefix(s, "负")
	}
	var total, section, number int64
	seen := false
	for _, r := range s {
		if d, ok := zhDigitVal[r]; ok {
			if number != 0 {
				// 连续数字（如 "一二三" → 123）按位拼接
				number = number*10 + d
			} else {
				number = d
			}
			seen = true
			continue
		}
		if u, ok := zhUnitVal[r]; ok {
			seen = true
			switch u {
			case 10, 100, 1000:
				if number == 0 {
					number = 1 // "十二" = 12
				}
				section += number * u
				number = 0
			case 10000, 100000000:
				section = (section + number) * u
				total += section
				section = 0
				number = 0
			}
			continue
		}
		return 0, false
	}
	if !seen {
		return 0, false
	}
	n := total + section + number
	if neg {
		n = -n
	}
	return n, true
}

// 中文数字(s) → 整数 — 解析中文数字；无法解析时报错
func strParseChineseNumber(args ...object.Object) object.Object {
	if e := argc("中文数字", args, 1, 1); e != nil {
		return e
	}
	s, e := argStr("中文数字", args, 0, "")
	if e != nil {
		return e
	}
	n, ok := parseChineseNumber(s)
	if !ok {
		return newError("中文数字() 无法解析为数字: %q", s)
	}
	return intObj(n)
}

var zhDigitChar = []string{"零", "一", "二", "三", "四", "五", "六", "七", "八", "九"}
var zhSectionUnit = []string{"", "十", "百", "千"}
var zhBigUnit = []string{"", "万", "亿", "万亿"}

// chineseSection 转换 0< n <10000 的一段。
func chineseSection(n int64) string {
	var b strings.Builder
	zero := false
	for i := 3; i >= 0; i-- {
		unit := int64(1)
		for j := 0; j < i; j++ {
			unit *= 10
		}
		d := (n / unit) % 10
		if d == 0 {
			if b.Len() > 0 {
				zero = true
			}
			continue
		}
		if zero {
			b.WriteString("零")
			zero = false
		}
		b.WriteString(zhDigitChar[d])
		b.WriteString(zhSectionUnit[i])
	}
	return b.String()
}

// chineseNumber 阿拉伯数字 → 中文数字（支持负数，0..万亿级）。
func chineseNumber(n int64) string {
	if n == 0 {
		return "零"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var sections []int64
	for n > 0 {
		sections = append(sections, n%10000)
		n /= 10000
	}
	var b strings.Builder
	for i := len(sections) - 1; i >= 0; i-- {
		v := sections[i]
		if v == 0 {
			continue
		}
		// 高位段之间补零（如 100000001 → 一亿零一）
		if b.Len() > 0 && v < 1000 {
			b.WriteString("零")
		}
		b.WriteString(chineseSection(v))
		if i < len(zhBigUnit) {
			b.WriteString(zhBigUnit[i])
		}
	}
	out := b.String()
	// 中文习惯："一十二" → "十二"
	if strings.HasPrefix(out, "一十") {
		out = "十" + out[len("一十"):]
	}
	if neg {
		out = "负" + out
	}
	return out
}

// 中文数字串(n) → 字符串 — 阿拉伯数字转中文数字
func strChineseNumber(args ...object.Object) object.Object {
	if e := argc("中文数字串", args, 1, 1); e != nil {
		return e
	}
	n, e := argInt("中文数字串", args, 0, "整数")
	if e != nil {
		return e
	}
	return strObj(chineseNumber(n))
}
