package runtime

// parse.go — Python 解析入口（双轨）
//
// 主轨：gopapy v2（github.com/tamnd/gopapy/v2/parser2，纯 Go，Python 3.14 全语法，
//       32µs/122 行模块，比自研 parser 快一个量级）。
// 备轨：自带 py/parser（手写递归下降，覆盖 zuzo 运行时实际支持的子集）。
//
// 策略：先跑 gopapy → 成功即用其 AST（经 py/pyparse 适配成内部 py/ast）；
//       gopapy 报错或含运行时暂不支持的构造（TypeAlias / t-string / 超长整数字面量等）
//       时静默回退备轨，保证既有语料零回归。
// 排障开关：ZUZO_PY_BUILTIN_PARSER=1 强制只走自带 parser。

import (
	"fmt"
	"os"
	"strings"

	pyast "zuzo/py/ast"
	pyparser "zuzo/py/parser"
	"zuzo/py/pyparse"
)

// ParseSourceWithFile 解析 Python 源码（主轨 gopapy，失败回退内置 parser）
func ParseSourceWithFile(src, filename string) (*pyast.Module, []string) {
	m, errs, _ := ParseSourceWithFileTrace(src, filename)
	return m, errs
}

// ParseSourceWithFileTrace 同 ParseSourceWithFile，额外返回实际使用的解析器名
// （"gopapy" / "builtin"），供基准与诊断使用。
func ParseSourceWithFileTrace(src, filename string) (*pyast.Module, []string, string) {
	if os.Getenv("ZUZO_PY_BUILTIN_PARSER") == "" && !needsBuiltinParserGuard(src) {
		if mod, err := pyparse.ParseFile(filename, src); err == nil {
			traceParser("gopapy", filename)
			return mod, nil, "gopapy"
		}
	}
	m, errs := pyparser.Parse(src)
	return m, errs, "builtin"
}

// trace 解析器选择（ZUZO_PY_PARSER_TRACE=1 时打到 stderr，供压测统计命中率）
func traceParser(name, filename string) {
	if os.Getenv("ZUZO_PY_PARSER_TRACE") != "" {
		fmt.Fprintf(os.Stderr, "[py-parser] %s <- %s\n", name, filename)
	}
}

// needsBuiltinParserGuard 判断源码是否必须走自带 parser。
//
// 实测（2026-10-24）gopapy v2.0.0 有三处会静默改变语义或直接拒绝的构造：
//  1. 字符串转义：\x / \u / \U / \N{} / 八进制只原样保留不解码（lex.go:819-843），
//     而 \n \t 又已解码 —— 半解码状态无法在 adapter 侧安全补救，统一回退。
//  2. 超长整数：strconv.ParseInt(...,64) 溢出即报错（parser.go:1083-1101）。
//  3. except* 的星号被丢弃（stmt.go:930）；async for 只允许出现在推导式第 2+ 个子句（parser.go:1197）。
//
// 这些场景下自带 parser 是正确的，故按「源码特征」整文件回退，保证语义零偏差。
func needsBuiltinParserGuard(src string) bool {
	if strings.ContainsRune(src, '\\') {
		return true
	}
	if strings.Contains(src, "except*") || strings.Contains(src, "async for") {
		return true
	}
	return hasLongIntLiteral(src)
}

// hasLongIntLiteral 扫连续数字串，超过 int64 安全位数（19 位）即认为需要自带 parser
func hasLongIntLiteral(src string) bool {
	run := 0
	for i := 0; i < len(src); i++ {
		c := src[i]
		if c >= '0' && c <= '9' {
			run++
			if run >= 19 {
				return true
			}
			continue
		}
		run = 0
	}
	return false
}

// ParseExprDual 解析单个表达式（主轨 gopapy，失败回退内置）
func ParseExprDual(src string) (pyast.Expr, []string) {
	if os.Getenv("ZUZO_PY_BUILTIN_PARSER") == "" {
		if e, err := pyparse.ParseExpression(src); err == nil {
			return e, nil
		}
	}
	return pyparser.ParseExpr(src)
}
