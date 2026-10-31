package evaluator

// strlib_legacy.go — 复用面挂载（37 个既有成员，零重写）
//
// 字符串标准库不该另起炉灶：UTF-8/Unicode、编码/摘要、正则本来就是字符串能力的一部分，
// 这里把 builtins_utf8.go / builtins_encoding.go / builtins_regex.go 的既有实现
// 直接挂进 `文本` 命名空间（同一 BuiltinFunction，零包装、零行为变更），
// 同时保留原全局名，形成「扁平名 + 命名空间」双入口。

import (
	"zuzo/object"
)

func init() {
	registerStringMethods(map[string]object.BuiltinFunction{
		// ---- UTF-8 / Unicode / 转义（builtins_utf8.go） ----
		"转义":         builtinEscape,
		"反转义":        builtinUnescape,
		"unicode转义":  builtinUnicodeEscape,
		"unicode反转义": builtinUnicodeUnescape,
		"码点转字符":      builtinCodepointToChar,
		"字符转码点":      builtinCharToCodepoint,
		"utf8验证":     builtinUTF8Valid,
		"utf8修复":     builtinUTF8Fix,
		"utf8字节数":    builtinUTF8ByteCount,
		"转编码":        builtinEncodeTo,
		"从编码转换":      builtinDecodeFrom,
		"规范化":        builtinNormalize,
		"去除BOM":      builtinRemoveBOM,

		// ---- 编码 / 摘要（builtins_encoding.go） ----
		"base64编码":    builtinBase64Encode,
		"base64解码":    builtinBase64Decode,
		"base64url编码": builtinBase64URLEncode,
		"base64url解码": builtinBase64URLDecode,
		"hex编码":       builtinHexEncode,
		"hex解码":       builtinHexDecode,
		"html转义":      builtinHTMLEscape,
		"html反转义":     builtinHTMLUnescape,
		"url编码":       builtinUrlEncode,
		"url解码":       builtinUrlDecode,
		"url路径编码":     builtinUrlPathEncode,
		"url路径解码":     builtinUrlPathDecode,
		"md5":         builtinMD5,
		"sha1":        builtinSHA1,
		"sha256":      builtinSHA256,

		// ---- 正则（builtins_regex.go） ----
		"正则匹配":   builtinRegexMatch,
		"正则查找全部": builtinRegexFindAll,
		"正则替换":   builtinRegexReplace,
		"正则分割":   builtinRegexSplit,
		"正则验证":   builtinRegexTest,
		"正则提取":   builtinRegexExtract,

		// ---- 通用 ----
		"转文本": builtinStr,    // 文本.转文本(42) → "42"
		"格式化": builtinFormat, // 文本.格式化("%d-%s", 42, "x")
	})
}
