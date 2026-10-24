package tests

// encoding_test.go — 编码预处理测试（从 encoding/ 迁移）

import (
	"strings"
	"testing"
	"unicode/utf8"

	"zuzo/encoding"
)

func TestNormalizeSourceUTF8(t *testing.T) {
	// 纯 UTF-8（无 BOM）
	src := []byte("变量 x = 10\n打印(\"你好\")")
	result := encoding.NormalizeSource(src)
	if result != string(src) {
		t.Errorf("UTF-8 无 BOM 应原样返回, got %q", result)
	}
}

func TestNormalizeSourceUTF8BOM(t *testing.T) {
	// UTF-8 with BOM
	bom := []byte{0xEF, 0xBB, 0xBF}
	content := []byte("变量 x = 10")
	src := append(bom, content...)
	result := encoding.NormalizeSource(src)
	if result != string(content) {
		t.Errorf("应去除 UTF-8 BOM, 期望 %q, 得到 %q", string(content), result)
	}
	// 结果不应包含 BOM
	if strings.HasPrefix(result, "\uFEFF") {
		t.Error("结果仍包含 BOM 字符")
	}
}

func TestNormalizeSourceUTF16LE(t *testing.T) {
	// UTF-16 LE with BOM: "hi"
	// BOM: FF FE
	// 'h' = 0068 → LE: 68 00
	// 'i' = 0069 → LE: 69 00
	src := []byte{0xFF, 0xFE, 0x68, 0x00, 0x69, 0x00}
	result := encoding.NormalizeSource(src)
	if result != "hi" {
		t.Errorf("UTF-16 LE 解码失败, 期望 \"hi\", 得到 %q", result)
	}
}

func TestNormalizeSourceUTF16BE(t *testing.T) {
	// UTF-16 BE with BOM: "hi"
	// BOM: FE FF
	// 'h' = 0068 → BE: 00 68
	// 'i' = 0069 → BE: 00 69
	src := []byte{0xFE, 0xFF, 0x00, 0x68, 0x00, 0x69}
	result := encoding.NormalizeSource(src)
	if result != "hi" {
		t.Errorf("UTF-16 BE 解码失败, 期望 \"hi\", 得到 %q", result)
	}
}

func TestNormalizeSourceUTF16LEChinese(t *testing.T) {
	// UTF-16 LE: "中" = U+4E2D → LE: 2D 4E
	src := []byte{0xFF, 0xFE, 0x2D, 0x4E}
	result := encoding.NormalizeSource(src)
	if result != "中" {
		t.Errorf("UTF-16 LE 中文解码失败, 期望 \"中\", 得到 %q", result)
	}
}

func TestNormalizeSourceGBK(t *testing.T) {
	// GBK 编码的 "你好"
	// 你 = 0xC4E3, 好 = 0xBAC3
	src := []byte{0xC4, 0xE3, 0xBA, 0xC3}
	result := encoding.NormalizeSource(src)
	if result != "你好" {
		t.Errorf("GBK 自动检测失败, 期望 \"你好\", 得到 %q", result)
	}
}

func TestNormalizeSourceGBKWithCode(t *testing.T) {
	// GBK 编码的 "打印(\"hello\")"
	// 打 = 0xB4F2, 印 = 0xD3A1
	src := []byte{0xB4, 0xF2, 0xD3, 0xA1, '(', '"', 'h', 'e', 'l', 'l', 'o', '"', ')'}
	result := encoding.NormalizeSource(src)
	expected := "打印(\"hello\")"
	if result != expected {
		t.Errorf("GBK 代码解码失败, 期望 %q, 得到 %q", expected, result)
	}
}

func TestNormalizeSourceEmpty(t *testing.T) {
	result := encoding.NormalizeSource([]byte{})
	if result != "" {
		t.Errorf("空输入应返回空字符串, 得到 %q", result)
	}
}

func TestNormalizeSourceInvalidBytes(t *testing.T) {
	// 无效字节序列（非 UTF-8、非 GBK、非 Big5）
	// 0xFF 0xFE 0x01 不是任何已知编码的有效开头（也非 UTF-16 BOM 后跟奇数字节）
	src := []byte{0xC0, 0xC1, 0xFE}
	result := encoding.NormalizeSource(src)
	// 应该返回包含替换字符的字符串，不崩溃
	if !utf8.ValidString(result) {
		t.Errorf("结果应为合法 UTF-8, 得到 %q", result)
	}
}

func TestFixUTF8(t *testing.T) {
	// 包含无效字节的字符串
	s := string([]byte{'h', 'i', 0xFF, 0xFE})
	result := encoding.FixUTF8(s)
	// 应包含替换字符
	if !utf8.ValidString(result) {
		t.Errorf("修复后应为合法 UTF-8, 得到 %q", result)
	}
	// 有效部分应保留
	if result[:2] != "hi" {
		t.Errorf("有效部分应保留, 得到 %q", result)
	}
}
