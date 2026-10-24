package evaluator

// builtins_utf8.go — UTF-8 / Unicode / 转义 内置函数
//
// 专司文本编码与转义，保证中文编程语言在多编码环境下的无缝性：
//
//	转义/反转义     — 字符串特殊字符转义（\n \t \\ \" 等）
//	Unicode 转义   — \uXXXX 格式互转
//	码点转换        — 字符与 Unicode 码点互转（chr/ord）
//	UTF-8 验证/修复 — 检测并修复无效 UTF-8 字节序列
//	编码转换        — UTF-8 ↔ GBK / GB2312 / Big5 / Latin-1 等
//	Unicode 规范化  — NFC / NFD / NFKC / NFKD
//	中文处理        — 全角半角互转、去除不可见字符

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	zuzoEncoding "zuzo/encoding"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/japanese"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	"golang.org/x/text/unicode/norm"

	"zuzo/object"
)

// ==================== 转义 / 反转义 ====================

// 转义(s) → 字符串 — 将字符串中的特殊字符转义为可安全传输的形式
// 例: "hello\nworld" → "hello\\nworld"  (实际内容中的换行变成字面 \n)
func builtinEscape(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("转义() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("转义() 参数必须是字符串，得到 %s", args[0].Type())
	}

	var b strings.Builder
	b.Grow(len(s.Value))
	for _, r := range s.Value {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		case '\x00':
			b.WriteString(`\0`)
		case '\a':
			b.WriteString(`\a`)
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		case '\v':
			b.WriteString(`\v`)
		default:
			b.WriteRune(r)
		}
	}
	return &object.String{Value: b.String()}
}

// 反转义(s) → 字符串 — 将转义序列还原为实际字符
// 例: "hello\\nworld" → "hello\nworld"  (字面 \n 变成实际换行)
func builtinUnescape(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("反转义() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("反转义() 参数必须是字符串，得到 %s", args[0].Type())
	}

	var b strings.Builder
	b.Grow(len(s.Value))
	runes := []rune(s.Value)
	i := 0
	for i < len(runes) {
		if runes[i] == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			switch next {
			case '\\':
				b.WriteRune('\\')
				i += 2
			case '"':
				b.WriteRune('"')
				i += 2
			case '\'':
				b.WriteRune('\'')
				i += 2
			case 'n':
				b.WriteRune('\n')
				i += 2
			case 't':
				b.WriteRune('\t')
				i += 2
			case 'r':
				b.WriteRune('\r')
				i += 2
			case '0':
				b.WriteRune('\x00')
				i += 2
			case 'a':
				b.WriteRune('\a')
				i += 2
			case 'b':
				b.WriteRune('\b')
				i += 2
			case 'f':
				b.WriteRune('\f')
				i += 2
			case 'v':
				b.WriteRune('\v')
				i += 2
			case 'u':
				// \uXXXX — 4 位十六进制
				if i+6 <= len(runes) {
					hexStr := string(runes[i+2 : i+6])
					if v, err := strconv.ParseInt(hexStr, 16, 32); err == nil {
						b.WriteRune(rune(v))
						i += 6
						continue
					}
				}
				b.WriteRune(runes[i])
				i++
			case 'U':
				// \UXXXXXXXX — 8 位十六进制
				if i+10 <= len(runes) {
					hexStr := string(runes[i+2 : i+10])
					if v, err := strconv.ParseInt(hexStr, 16, 32); err == nil {
						b.WriteRune(rune(v))
						i += 10
						continue
					}
				}
				b.WriteRune(runes[i])
				i++
			case 'x':
				// \xXX — 2 位十六进制
				if i+4 <= len(runes) {
					hexStr := string(runes[i+2 : i+4])
					if v, err := strconv.ParseInt(hexStr, 16, 8); err == nil {
						b.WriteRune(rune(v))
						i += 4
						continue
					}
				}
				b.WriteRune(runes[i])
				i++
			default:
				b.WriteRune(runes[i])
				i++
			}
		} else {
			b.WriteRune(runes[i])
			i++
		}
	}
	return &object.String{Value: b.String()}
}

// ==================== Unicode 转义 ====================

// unicode转义(s) → 字符串 — 将非 ASCII 字符转为 \uXXXX 转义形式
// 例: "你好" → "\u4f60\u597d"
func builtinUnicodeEscape(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("unicode转义() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("unicode转义() 参数必须是字符串，得到 %s", args[0].Type())
	}

	var b strings.Builder
	b.Grow(len(s.Value) * 2)
	for _, r := range s.Value {
		if r < 0x80 {
			// ASCII 可见字符直接输出
			b.WriteRune(r)
		} else if r <= 0xFFFF {
			b.WriteString(fmt.Sprintf(`\u%04x`, r))
		} else {
			// 超出 BMP 平面，使用 \UXXXXXXXX
			b.WriteString(fmt.Sprintf(`\U%08x`, r))
		}
	}
	return &object.String{Value: b.String()}
}

// unicode反转义(s) → 字符串 — 将 \uXXXX / \UXXXXXXXX 转义序列还原为实际字符
// 例: "\u4f60\u597d" → "你好"
func builtinUnicodeUnescape(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("unicode反转义() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("unicode反转义() 参数必须是字符串，得到 %s", args[0].Type())
	}

	var b strings.Builder
	b.Grow(len(s.Value))
	runes := []rune(s.Value)
	i := 0
	for i < len(runes) {
		if runes[i] == '\\' && i+1 < len(runes) {
			next := runes[i+1]
			switch next {
			case 'u':
				if i+6 <= len(runes) {
					hexStr := string(runes[i+2 : i+6])
					if v, err := strconv.ParseInt(hexStr, 16, 32); err == nil {
						b.WriteRune(rune(v))
						i += 6
						continue
					}
				}
				b.WriteRune(runes[i])
				i++
			case 'U':
				if i+10 <= len(runes) {
					hexStr := string(runes[i+2 : i+10])
					if v, err := strconv.ParseInt(hexStr, 16, 32); err == nil {
						b.WriteRune(rune(v))
						i += 10
						continue
					}
				}
				b.WriteRune(runes[i])
				i++
			case 'x':
				if i+4 <= len(runes) {
					hexStr := string(runes[i+2 : i+4])
					if v, err := strconv.ParseInt(hexStr, 16, 8); err == nil {
						b.WriteRune(rune(v))
						i += 4
						continue
					}
				}
				b.WriteRune(runes[i])
				i++
			default:
				b.WriteRune(runes[i])
				i++
			}
		} else {
			b.WriteRune(runes[i])
			i++
		}
	}
	return &object.String{Value: b.String()}
}

// ==================== 码点转换 ====================

// 码点转字符(n) → 字符串 — 将 Unicode 码点转为对应字符（类似 chr）
// 例: 码点转字符(65) → "A"  码点转字符(0x4e2d) → "中"
func builtinCodepointToChar(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("码点转字符() 期望 1 个参数，得到 %d 个", len(args))
	}
	n, ok := args[0].(*object.Integer)
	if !ok {
		return newError("码点转字符() 参数必须是整数（码点），得到 %s", args[0].Type())
	}
	if n.Value < 0 || n.Value > 0x10FFFF {
		return newError("码点转字符() 码点超出有效范围 [0, 0x10FFFF]: %d", n.Value)
	}
	// 代理区间 [0xD800, 0xDFFF] 不可直接转为字符
	if n.Value >= 0xD800 && n.Value <= 0xDFFF {
		return newError("码点转字符() 码点 %d 位于 UTF-16 代理区间，不可表示为字符", n.Value)
	}
	return &object.String{Value: string(rune(n.Value))}
}

// 字符转码点(s) → 整数 — 将字符串的第一个字符转为 Unicode 码点（类似 ord）
// 例: 字符转码点("A") → 65  字符转码点("中") → 20013
func builtinCharToCodepoint(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("字符转码点() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("字符转码点() 参数必须是字符串，得到 %s", args[0].Type())
	}
	if len(s.Value) == 0 {
		return newError("字符转码点() 字符串不能为空")
	}
	r := []rune(s.Value)
	return &object.Integer{Value: int64(r[0])}
}

// ==================== UTF-8 验证 / 修复 ====================

// utf8验证(s) → 布尔 — 验证字符串是否为有效的 UTF-8 编码
func builtinUTF8Valid(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("utf8验证() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("utf8验证() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return nativeBoolToBooleanObject(utf8.ValidString(s.Value))
}

// utf8修复(s) → 字符串 — 修复无效的 UTF-8 序列，用替换字符 (U+FFFD) 替换损坏字节
func builtinUTF8Fix(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("utf8修复() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("utf8修复() 参数必须是字符串，得到 %s", args[0].Type())
	}

	// 复用 encoding 包的 FixUTF8（内部已含有效性检查）
	return &object.String{Value: zuzoEncoding.FixUTF8(s.Value)}
}

// utf8字节数(s) → 整数 — 返回字符串的 UTF-8 编码字节数（区别于 长度() 返回 rune 数）
func builtinUTF8ByteCount(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("utf8字节数() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("utf8字节数() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.Integer{Value: int64(len(s.Value))}
}

// ==================== 编码转换 ====================

// getEncoder 根据编码名返回对应的编码器
func getEncoder(name string) (encoding.Encoding, bool) {
	switch strings.ToLower(name) {
	case "gbk", "gb18030":
		return simplifiedchinese.GBK, true
	case "gb2312":
		// GB2312 是 GBK 的子集，用 GBK 编码器即可兼容
		return simplifiedchinese.GBK, true
	case "hz-gb2312", "hz":
		return simplifiedchinese.HZGB2312, true
	case "big5":
		return traditionalchinese.Big5, true
	case "latin1", "iso-8859-1", "iso8859-1":
		return charmap.ISO8859_1, true
	case "latin2", "iso-8859-2":
		return charmap.ISO8859_2, true
	case "windows-1252", "cp1252":
		return charmap.Windows1252, true
	case "windows-1251", "cp1251":
		return charmap.Windows1251, true
	case "shift-jis", "shiftjis", "sjis":
		return japanese.ShiftJIS, true
	default:
		return nil, false
	}
}

// 转编码(s, 编码名) → 字符串 — 将 UTF-8 字符串编码为指定编码（返回字节序列的字符串形式）
// 支持的编码: gbk, gb2312, gb18030, big5, latin1, latin2, windows-1252, windows-1251, shift-jis
func builtinEncodeTo(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("转编码() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("转编码() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	encName, ok := args[1].(*object.String)
	if !ok {
		return newError("转编码() 第二个参数必须是字符串（编码名），得到 %s", args[1].Type())
	}

	enc, found := getEncoder(encName.Value)
	if !found {
		return newError("转编码() 不支持的编码: %s（支持: gbk, gb2312, gb18030, big5, latin1, latin2, windows-1252, windows-1251, shift-jis）", encName.Value)
	}

	encoded, err := enc.NewEncoder().Bytes([]byte(s.Value))
	if err != nil {
		return newError("转编码() 编码失败: %s", err.Error())
	}
	return &object.String{Value: string(encoded)}
}

// 从编码转换(s, 编码名) → 字符串 — 将指定编码的字节序列解码为 UTF-8 字符串
func builtinDecodeFrom(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("从编码转换() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("从编码转换() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	encName, ok := args[1].(*object.String)
	if !ok {
		return newError("从编码转换() 第二个参数必须是字符串（编码名），得到 %s", args[1].Type())
	}

	enc, found := getEncoder(encName.Value)
	if !found {
		return newError("从编码转换() 不支持的编码: %s（支持: gbk, gb2312, gb18030, big5, latin1, latin2, windows-1252, windows-1251, shift-jis）", encName.Value)
	}

	decoded, err := enc.NewDecoder().Bytes([]byte(s.Value))
	if err != nil {
		return newError("从编码转换() 解码失败: %s", err.Error())
	}
	return &object.String{Value: string(decoded)}
}

// ==================== Unicode 规范化 ====================

// 规范化(s, 形式) → 字符串 — Unicode 规范化
// 形式: "NFC"（默认）、"NFD"、"NFKC"、"NFKD"
//
//	NFC  — 规范化组合（推荐，最常用）
//	NFD  — 规范化分解
//	NFKC — 兼容性组合
//	NFKD — 兼容性分解
func builtinNormalize(args ...object.Object) object.Object {
	if len(args) < 1 || len(args) > 2 {
		return newError("规范化() 期望 1-2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("规范化() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}

	form := norm.NFC // 默认 NFC
	if len(args) == 2 {
		formStr, ok := args[1].(*object.String)
		if !ok {
			return newError("规范化() 第二个参数必须是字符串（形式名），得到 %s", args[1].Type())
		}
		switch strings.ToUpper(formStr.Value) {
		case "NFC":
			form = norm.NFC
		case "NFD":
			form = norm.NFD
		case "NFKC":
			form = norm.NFKC
		case "NFKD":
			form = norm.NFKD
		default:
			return newError("规范化() 不支持的规范化形式: %s（支持: NFC, NFD, NFKC, NFKD）", formStr.Value)
		}
	}

	return &object.String{Value: form.String(s.Value)}
}

// ==================== 全角 / 半角转换 ====================

// 全角转半角(s) → 字符串 — 将全角字符转换为半角字符
// 包括: 全角字母数字、全角空格、全角标点
func builtinFullToHalf(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("全角转半角() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("全角转半角() 参数必须是字符串，得到 %s", args[0].Type())
	}

	var b strings.Builder
	b.Grow(len(s.Value))
	for _, r := range s.Value {
		b.WriteRune(fullToHalfRune(r))
	}
	return &object.String{Value: b.String()}
}

// 半角转全角(s) → 字符串 — 将半角字符转换为全角字符
func builtinHalfToFull(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("半角转全角() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("半角转全角() 参数必须是字符串，得到 %s", args[0].Type())
	}

	var b strings.Builder
	b.Grow(len(s.Value))
	for _, r := range s.Value {
		b.WriteRune(halfToFullRune(r))
	}
	return &object.String{Value: b.String()}
}

// fullToHalfRune 全角字符转半角
func fullToHalfRune(r rune) rune {
	switch {
	case r == 0x3000: // 全角空格
		return ' '
	case r >= 0xFF01 && r <= 0xFF5E: // 全角!-~ → 半角!-~
		return r - 0xFEE0
	default:
		return r
	}
}

// halfToFullRune 半角字符转全角
func halfToFullRune(r rune) rune {
	switch {
	case r == ' ':
		return 0x3000 // 全角空格
	case r >= 0x21 && r <= 0x7E: // 半角!-~ → 全角！-～
		return r + 0xFEE0
	default:
		return r
	}
}

// ==================== 不可见字符处理 ====================

// 去除不可见(s) → 字符串 — 去除控制字符和其他不可见字符
// 可选第二参数控制是否保留换行符（默认保留: \n \r \t）
func builtinRemoveInvisible(args ...object.Object) object.Object {
	if len(args) < 1 || len(args) > 2 {
		return newError("去除不可见() 期望 1-2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("去除不可见() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}

	keepWhitespace := true // 默认保留换行和制表符
	if len(args) == 2 {
		keep, ok := args[1].(*object.Boolean)
		if !ok {
			return newError("去除不可见() 第二个参数必须是布尔值，得到 %s", args[1].Type())
		}
		keepWhitespace = keep.Value
	}

	var b strings.Builder
	b.Grow(len(s.Value))
	for _, r := range s.Value {
		if unicode.IsControl(r) {
			if keepWhitespace && (r == '\n' || r == '\r' || r == '\t') {
				b.WriteRune(r)
				continue
			}
			// 跳过其他控制字符
			continue
		}

		// 跳过零宽字符和其他格式化字符
		switch r {
		case '\u200B', // 零宽空格
			'\u200C', // 零宽非连接符
			'\u200D', // 零宽连接符
			'\uFEFF', // BOM / 零宽不换行空格
			'\u00AD': // 软连字符
			continue
		}

		// 跳过 Unicode 格式化类别 (Cf)
		if unicode.Is(unicode.Cf, r) {
			continue
		}

		b.WriteRune(r)
	}
	return &object.String{Value: b.String()}
}

// 去除BOM(s) → 字符串 — 去除字符串开头的 UTF-8 BOM (U+FEFF)
func builtinRemoveBOM(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("去除BOM() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("去除BOM() 参数必须是字符串，得到 %s", args[0].Type())
	}
	result := strings.TrimPrefix(s.Value, "\uFEFF")
	return &object.String{Value: result}
}
