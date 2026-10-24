package encoding

// encoding.go — 源码编码预处理模块
//
// 在源码进入词法分析器之前，自动完成全部编码转换工作：
//
//  1. BOM 检测 — UTF-8 BOM (EF BB BF)、UTF-16 LE BOM (FF FE)、UTF-16 BE BOM (FE FF)
//  2. UTF-16 → UTF-8 转换（如果检测到 UTF-16 BOM）
//  3. UTF-8 有效性验证 — 如果已经是合法 UTF-8，直接返回（最快路径）
//  4. 非 UTF-8 编码自动检测 — GBK / Big5 等（基于统计启发式）
//  5. 无效 UTF-8 修复 — 末尾兜底，用 U+FFFD 替换无法识别的字节
//
// 设计目标：用户无论用什么编辑器、什么编码保存 .hy 文件，
// zuzo 都能正确读取并执行，保证语言的无缝性。

import (
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
)

// NormalizeSource 将原始字节流规范化为 UTF-8 字符串。
// 这是所有源文件读取后、词法分析前必须调用的预处理函数。
func NormalizeSource(data []byte) string {
	if len(data) == 0 {
		return ""
	}

	// === 1. BOM 检测 ===
	// UTF-8 BOM: EF BB BF
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return stripBOMAndFix(data[3:])
	}

	// UTF-16 LE BOM: FF FE
	if len(data) >= 2 && data[0] == 0xFF && data[1] == 0xFE {
		return decodeUTF16(data[2:], false) // little-endian
	}

	// UTF-16 BE BOM: FE FF
	if len(data) >= 2 && data[0] == 0xFE && data[1] == 0xFF {
		return decodeUTF16(data[2:], true) // big-endian
	}

	// === 2. 无 BOM 情况 ===
	return detectAndConvert(data)
}

// stripBOMAndFix 去除 BOM 后修复 UTF-8（BOM 已被截掉，data 是纯内容）
func stripBOMAndFix(data []byte) string {
	s := string(data)
	if utf8.ValidString(s) {
		return s
	}
	return fixUTF8Internal(s)
}

// detectAndConvert 无 BOM 时的编码检测与转换
func detectAndConvert(data []byte) string {
	// 快速路径：如果是合法 UTF-8，直接返回
	if utf8.Valid(data) {
		return string(data)
	}

	// 非 UTF-8，尝试 GBK（中国大陆最常见的老编码）
	if s, ok := tryGBK(data); ok {
		return s
	}

	// 尝试 Big5（繁体中文）
	if s, ok := tryBig5(data); ok {
		return s
	}

	// 兜底：强制按 UTF-8 解析，损坏部分用 U+FFFD 替换
	return fixUTF8Internal(string(data))
}

// tryGBK 尝试将字节流按 GBK 解码为 UTF-8
// 返回 (解码结果, 是否成功)
func tryGBK(data []byte) (string, bool) {
	decoder := simplifiedchinese.GBK.NewDecoder()
	decoded, err := decoder.Bytes(data)
	if err != nil {
		return "", false
	}

	// 验证解码结果是否为合法 UTF-8
	if !utf8.Valid(decoded) {
		return "", false
	}

	// 启发式验证：解码后不应包含过多的替换字符 (U+FFFD)
	// 如果替换字符过多，说明 GBK 假阳性
	s := string(decoded)
	replacementCount := strings.Count(s, "\uFFFD")
	if replacementCount > len(data)/10 {
		return "", false
	}

	return s, true
}

// tryBig5 尝试将字节流按 Big5 解码为 UTF-8
func tryBig5(data []byte) (string, bool) {
	decoder := traditionalchinese.Big5.NewDecoder()
	decoded, err := decoder.Bytes(data)
	if err != nil {
		return "", false
	}

	if !utf8.Valid(decoded) {
		return "", false
	}

	s := string(decoded)
	replacementCount := strings.Count(s, "\uFFFD")
	if replacementCount > len(data)/10 {
		return "", false
	}

	return s, true
}

// decodeUTF16 将 UTF-16 字节流解码为 UTF-8 字符串
func decodeUTF16(data []byte, bigEndian bool) string {
	if len(data) < 2 {
		return ""
	}

	// 确保字节数为偶数（UTF-16 每个字符 2 字节）
	if len(data)%2 != 0 {
		// 尾部多出一个字节，截断
		data = data[:len(data)-1]
	}

	var runes []uint16
	for i := 0; i < len(data); i += 2 {
		var r uint16
		if bigEndian {
			r = uint16(data[i])<<8 | uint16(data[i+1])
		} else {
			r = uint16(data[i+1])<<8 | uint16(data[i])
		}
		runes = append(runes, r)
	}

	// 处理代理对
	decoded := utf16.Decode(runes)
	return string(decoded)
}

// FixUTF8 修复无效的 UTF-8 序列，用 U+FFFD (替换字符) 替换损坏字节
// 供外部包（如 evaluator）复用
func FixUTF8(s string) string {
	return fixUTF8Internal(s)
}

// fixUTF8Internal fixUTF8 的内部实现
func fixUTF8Internal(s string) string {
	if utf8.ValidString(s) {
		return s
	}

	var b strings.Builder
	b.Grow(len(s))
	bytes := []byte(s)
	i := 0
	for i < len(bytes) {
		r, size := utf8.DecodeRune(bytes[i:])
		b.WriteRune(r)
		i += size
	}
	return b.String()
}
