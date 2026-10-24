package tests

// parse_bench_test.go — 前端（词法+语法）性能基准
//
// 用途：性能优化轮定位「解析占端到端多少」以及 lexer/parser 的内部热点。
// 典型场景：混合语言每块都要独立解析，大文件（数万行）启动也吃解析。

import (
	"strings"
	"testing"

	"zuzo/lexer"
	"zuzo/parser"
	"zuzo/token"
)

func buildAssignSource(n int) string {
	var b strings.Builder
	b.Grow(n * 16)
	for i := 0; i < n; i++ {
		b.WriteString("变量 v")
		b.WriteString(itoaFast(i))
		b.WriteString(" = ")
		b.WriteString(itoaFast(i))
		b.WriteString("\n")
	}
	return b.String()
}

// itoaFast 避免引入 strconv 追加开销（基准构建阶段用）
func itoaFast(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

// BenchmarkParse100k 解析 10 万条赋值语句
func BenchmarkParse100k(b *testing.B) {
	code := buildAssignSource(100000)
	b.SetBytes(int64(len(code)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := lexer.New(code)
		p := parser.New(l)
		p.ParseProgram()
	}
}

// BenchmarkLex100k 仅词法（隔离 lexer 成本）
func BenchmarkLex100k(b *testing.B) {
	code := buildAssignSource(100000)
	b.SetBytes(int64(len(code)))
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		l := lexer.New(code)
		for {
			tok := l.NextToken()
			if tok.Type == token.EOF {
				break
			}
		}
	}
}