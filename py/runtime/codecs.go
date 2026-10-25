package runtime

// codecs.go — 通用编解码（str.encode / bytes.decode / _codecs / codecs 的底座）
//
// 第13轮：第三方库大量使用非 UTF-8 编码（ascii / latin-1 / gbk / utf-16），
// 而首版 str.encode 直接返回 UTF-8 字节、忽略 encoding 参数 —— 在 http.client、
// urllib3 的 header/URL 编码路径上会静默产出错数据。这里统一走一张编码名表。

import (
	"fmt"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	xtunicode "golang.org/x/text/encoding/unicode"

	"zuzo/py/object"
)

// lookupEncoding 把 Python 的编码名归一化后返回 x/text 编码器（nil 表示 UTF-8 原生）。
func lookupEncoding(name string) (encoding.Encoding, bool) {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "_", "-")
	switch n {
	case "", "utf-8", "utf8", "u8", "utf":
		return nil, true // UTF-8 原生
	case "ascii", "us-ascii", "646", "ansi-x3.4-1968":
		return charmap.ISO8859_1, true // ascii 单独特判，见 codecEncode
	case "latin-1", "latin1", "iso-8859-1", "iso8859-1", "8859", "cp819":
		return charmap.ISO8859_1, true
	case "cp1252", "windows-1252":
		return charmap.Windows1252, true
	case "gbk", "cp936", "936":
		return simplifiedchinese.GBK, true
	case "gb2312", "euc-cn":
		return simplifiedchinese.HZGB2312, true
	case "gb18030":
		return simplifiedchinese.GB18030, true
	case "big5", "cp950":
		return traditionalchinese.Big5, true
	case "utf-16", "utf16":
		return xtunicode.UTF16(xtunicode.LittleEndian, xtunicode.UseBOM), true
	case "utf-16-le", "utf-16le":
		return xtunicode.UTF16(xtunicode.LittleEndian, xtunicode.IgnoreBOM), true
	case "utf-16-be", "utf-16be":
		return xtunicode.UTF16(xtunicode.BigEndian, xtunicode.IgnoreBOM), true
	}
	return nil, false
}

// codecEncode 把 Go 字符串按指定编码编成字节。errorsMode: strict / ignore / replace。
func codecEncode(s, encName, errorsMode string) ([]byte, error) {
	n := normalizeCodec(encName)
	if n == "ascii" {
		out := make([]byte, 0, len(s))
		for _, r := range s {
			if r > 127 {
				return codecErr(errorsMode, out, s, "ascii", r)
			}
			out = append(out, byte(r))
		}
		return out, nil
	}
	enc, ok := lookupEncoding(n)
	if !ok {
		return nil, fmt.Errorf("unknown encoding: %s", encName)
	}
	if enc == nil {
		return []byte(s), nil
	}
	out, err := enc.NewEncoder().Bytes([]byte(s))
	if err != nil {
		if len(errorsMode) > 0 && errorsMode != "strict" {
			// 宽松模式：退回 UTF-8 字节，避免整链中断
			return []byte(s), nil
		}
		return nil, err
	}
	return out, nil
}

// codecDecode 把字节按指定编码解成 Go 字符串。
func codecDecode(b []byte, encName, errorsMode string) (string, error) {
	n := normalizeCodec(encName)
	if n == "utf-8" {
		if utf8.Valid(b) {
			return string(b), nil
		}
		if errorsMode == "ignore" || errorsMode == "replace" {
			return strings.ToValidUTF8(string(b), "\uFFFD"), nil
		}
		return string(b), nil
	}
	if n == "ascii" {
		for _, c := range b {
			if c > 127 {
				if errorsMode == "ignore" || errorsMode == "replace" {
					return string(b), nil
				}
				return "", fmt.Errorf("'ascii' codec can't decode byte 0x%02x", c)
			}
		}
		return string(b), nil
	}
	enc, ok := lookupEncoding(n)
	if !ok {
		return "", fmt.Errorf("unknown encoding: %s", encName)
	}
	if enc == nil {
		return string(b), nil
	}
	out, err := enc.NewDecoder().Bytes(b)
	if err != nil {
		if errorsMode == "ignore" || errorsMode == "replace" {
			return strings.ToValidUTF8(string(b), "\uFFFD"), nil
		}
		return "", err
	}
	return string(out), nil
}

func codecErr(mode string, out []byte, s, encName string, r rune) ([]byte, error) {
	switch mode {
	case "ignore":
		return out, nil
	case "replace":
		return append(out, '?'), nil
	}
	return nil, fmt.Errorf("'%s' codec can't encode character %q", encName, string(r))
}

func normalizeCodec(name string) string {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "_", "-")
	if i := strings.IndexByte(n, ':'); i > 0 {
		n = n[:i]
	}
	return n
}

// ==================== 模块 ====================

func init() {
	builtinModuleInits["_codecs"] = modUnderscoreCodecs
}

func modUnderscoreCodecs(ip *Interp) *object.Module {
	m := newModule("_codecs")
	m.Dict["encode"] = bi("encode", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("encode() requires a string")
		}
		s := a[0].Str()
		enc := "utf-8"
		if len(a) > 1 {
			enc = a[1].Str()
		}
		errMode := "strict"
		if len(a) > 2 {
			errMode = a[2].Str()
		}
		out, err := codecEncode(s, enc, errMode)
		if err != nil {
			ip.raise(ip.excType("UnicodeEncodeError"), "%v", err)
		}
		return &object.Bytes{V: out}, nil
	})
	m.Dict["decode"] = bi("decode", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("decode() requires bytes")
		}
		var b []byte
		switch v := a[0].(type) {
		case *object.Bytes:
			b = v.V
		case *object.Str:
			b = []byte(v.V)
		}
		enc := "utf-8"
		if len(a) > 1 {
			enc = a[1].Str()
		}
		errMode := "strict"
		if len(a) > 2 {
			errMode = a[2].Str()
		}
		s, err := codecDecode(b, enc, errMode)
		if err != nil {
			ip.raise(ip.excType("UnicodeDecodeError"), "%v", err)
		}
		return &object.Str{V: s}, nil
	})
	m.Dict["lookup"] = bi("lookup", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("lookup() requires an encoding name")
		}
		name := normalizeCodec(a[0].Str())
		if _, ok := lookupEncoding(name); !ok {
			ip.raise(ip.excType("LookupError"), "unknown encoding: %s", name)
		}
		// CPython 的 CodecInfo 是 4 元组：(encode, decode, streamreader, streamwriter)
		// urllib3/filepost.py:11 取 [3] 当增量编码器：writer.encode(text) → bytes
		mkEnc := func() *object.Builtin {
			return bi("encode", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				src := ""
				if len(a) > 0 {
					src = a[0].Str()
				}
				errMode := "strict"
				if len(a) > 1 && a[1] != object.None {
					errMode = a[1].Str()
				}
				b, err := codecEncode(src, name, errMode)
				if err != nil {
					ip.raise(ip.excType("UnicodeEncodeError"), "%v", err)
				}
				if len(a) > 1 {
					return &object.Bytes{V: b}, nil // IncrementalEncoder 语义
				}
				return &object.Tuple{Items: []object.Object{&object.Bytes{V: b}, &object.Int{V: int64(len(src))}}}, nil
			})
		}
		mkDec := func() *object.Builtin {
			return bi("decode", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
				var data []byte
				if len(a) > 0 {
					switch v := a[0].(type) {
					case *object.Bytes:
						data = v.V
					case *object.ByteArray:
						data = v.V
					case *object.Str:
						data = []byte(v.V)
					}
				}
				errMode := "strict"
				if len(a) > 1 && a[1] != object.None {
					errMode = a[1].Str()
				}
				str, err := codecDecode(data, name, errMode)
				if err != nil {
					ip.raise(ip.excType("UnicodeDecodeError"), "%v", err)
				}
				if len(a) > 1 {
					return &object.Str{V: str}, nil
				}
				return &object.Tuple{Items: []object.Object{&object.Str{V: str}, &object.Int{V: int64(len(data))}}}, nil
			})
		}
		encType := &object.TypeObj{Name: "CodecInfo", Dict: map[string]object.Object{}}
		rt := &object.Instance{Class: encType, Dict: map[string]object.Object{}}
		rt.Dict["name"] = &object.Str{V: name}
		incEnc := &object.Instance{Class: &object.TypeObj{Name: "IncrementalEncoder", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{}}
		incEnc.Dict["encode"] = mkEnc()
		incEnc.Dict["reset"] = bi("reset", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		incDec := &object.Instance{Class: &object.TypeObj{Name: "IncrementalDecoder", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{}}
		incDec.Dict["decode"] = mkDec()
		incDec.Dict["reset"] = incEnc.Dict["reset"]
		return &object.Tuple{Items: []object.Object{mkEnc(), mkDec(), incDec, incEnc}}, nil
	})
	return m
}

var _ = utf16.Decode
