package evaluator

// builtins_encoding.go — 编码/解码内置函数
//
// 封装 Go 标准库的编码能力：
//
//	Base64 编码/解码（encoding/base64）
//	Hex   编码/解码（encoding/hex）
//	HTML  转义/反转义（html）
//	URL   编码/解码（net/url）— 从 builtins_net.go 迁移至此
//	哈希  MD5 / SHA1 / SHA256（crypto/md5, crypto/sha1, crypto/sha256）

import (
	"crypto/md5"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"html"
	"net/url"

	"zuzo/object"
)

// ==================== Base64 ====================

// base64编码(s) → 字符串 — standard Base64 编码
func builtinBase64Encode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("base64编码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("base64编码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: base64.StdEncoding.EncodeToString([]byte(s.Value))}
}

// base64解码(s) → 字符串 — standard Base64 解码
func builtinBase64Decode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("base64解码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("base64解码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	data, err := base64.StdEncoding.DecodeString(s.Value)
	if err != nil {
		return newError("base64解码() 失败: %s", err.Error())
	}
	return &object.String{Value: string(data)}
}

// base64url编码(s) → 字符串 — URL-safe Base64 编码
func builtinBase64URLEncode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("base64url编码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("base64url编码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: base64.URLEncoding.EncodeToString([]byte(s.Value))}
}

// base64url解码(s) → 字符串 — URL-safe Base64 解码
func builtinBase64URLDecode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("base64url解码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("base64url解码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	data, err := base64.URLEncoding.DecodeString(s.Value)
	if err != nil {
		return newError("base64url解码() 失败: %s", err.Error())
	}
	return &object.String{Value: string(data)}
}

// ==================== Hex ====================

// hex编码(s) → 字符串 — 将字符串的 UTF-8 字节编码为十六进制
func builtinHexEncode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("hex编码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("hex编码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: hex.EncodeToString([]byte(s.Value))}
}

// hex解码(s) → 字符串 — 将十六进制字符串解码为原始字符串
func builtinHexDecode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("hex解码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("hex解码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	data, err := hex.DecodeString(s.Value)
	if err != nil {
		return newError("hex解码() 失败: %s", err.Error())
	}
	return &object.String{Value: string(data)}
}

// ==================== HTML 转义 ====================

// html转义(s) → 字符串 — HTML 实体转义（< > & ' "）
func builtinHTMLEscape(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("html转义() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("html转义() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: html.EscapeString(s.Value)}
}

// html反转义(s) → 字符串 — HTML 实体反转义
func builtinHTMLUnescape(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("html反转义() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("html反转义() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: html.UnescapeString(s.Value)}
}

// ==================== URL 编解码（从 builtins_net.go 迁移） ====================

// url编码(s) → 字符串 — URL 编码（url.QueryEscape，空格→+）
func builtinUrlEncode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("url编码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("url编码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: url.QueryEscape(s.Value)}
}

// url解码(s) → 字符串 — URL 解码（url.QueryUnescape）
func builtinUrlDecode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("url解码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("url解码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	decoded, err := url.QueryUnescape(s.Value)
	if err != nil {
		return newError("url解码() 失败: %s", err.Error())
	}
	return &object.String{Value: decoded}
}

// url路径编码(s) → 字符串 — URL Path 编码（url.PathEscape，空格→%20）
func builtinUrlPathEncode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("url路径编码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("url路径编码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: url.PathEscape(s.Value)}
}

// url路径解码(s) → 字符串 — URL Path 解码（url.PathUnescape）
func builtinUrlPathDecode(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("url路径解码() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("url路径解码() 参数必须是字符串，得到 %s", args[0].Type())
	}
	decoded, err := url.PathUnescape(s.Value)
	if err != nil {
		return newError("url路径解码() 失败: %s", err.Error())
	}
	return &object.String{Value: decoded}
}

// ==================== 哈希摘要 ====================

// md5(s) → 字符串 — 计算字符串的 MD5 哈希（十六进制）
func builtinMD5(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("md5() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("md5() 参数必须是字符串，得到 %s", args[0].Type())
	}
	sum := md5.Sum([]byte(s.Value))
	return &object.String{Value: fmt.Sprintf("%x", sum)}
}

// sha1(s) → 字符串 — 计算字符串的 SHA1 哈希（十六进制）
func builtinSHA1(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("sha1() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("sha1() 参数必须是字符串，得到 %s", args[0].Type())
	}
	sum := sha1.Sum([]byte(s.Value))
	return &object.String{Value: fmt.Sprintf("%x", sum)}
}

// sha256(s) → 字符串 — 计算字符串的 SHA256 哈希（十六进制）
func builtinSHA256(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("sha256() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("sha256() 参数必须是字符串，得到 %s", args[0].Type())
	}
	sum := sha256.Sum256([]byte(s.Value))
	return &object.String{Value: fmt.Sprintf("%x", sum)}
}
