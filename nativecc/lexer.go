package nativecc

import (
	"fmt"
	"strconv"
	"strings"
)

// Lexer C 词法分析器。支持 UTF-8 标识符（zuzo 生成的 C 含中文符号名）。
type Lexer struct {
	src  string
	pos  int
	line int
	col  int
	file string
}

// NewLexer 构造词法器
func NewLexer(file, src string) *Lexer {
	return &Lexer{src: src, pos: 0, line: 1, col: 1, file: file}
}

func (l *Lexer) peekAt(n int) byte {
	if l.pos+n >= len(l.src) {
		return 0
	}
	return l.src[l.pos+n]
}

func (l *Lexer) peek() byte { return l.peekAt(0) }

func (l *Lexer) advance() byte {
	c := l.src[l.pos]
	l.pos++
	if c == '\n' {
		l.line++
		l.col = 1
	} else {
		l.col++
	}
	return c
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentCont(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\v' || c == '\f'
}

func (l *Lexer) skipSpaceAndComments() error {
	for l.pos < len(l.src) {
		c := l.peek()
		if isSpace(c) {
			l.advance()
			continue
		}
		if c == '/' && l.peekAt(1) == '/' {
			for l.pos < len(l.src) && l.peek() != '\n' {
				l.advance()
			}
			continue
		}
		if c == '/' && l.peekAt(1) == '*' {
			l.advance()
			l.advance()
			closed := false
			for l.pos < len(l.src) {
				if l.peek() == '*' && l.peekAt(1) == '/' {
					l.advance()
					l.advance()
					closed = true
					break
				}
				l.advance()
			}
			if !closed {
				return fmt.Errorf("%s:%d: 块注释未闭合", l.file, l.line)
			}
			continue
		}
		break
	}
	return nil
}

// Lex 全量词法分析
func (l *Lexer) Lex() ([]Token, error) {
	var out []Token
	for {
		if err := l.skipSpaceAndComments(); err != nil {
			return nil, err
		}
		if l.pos >= len(l.src) {
			out = append(out, Token{Kind: TEOF, Text: "", Line: l.line, Col: l.col, File: l.file})
			return out, nil
		}
		startLine, startCol := l.line, l.col
		c := l.peek()

		switch {
		case isIdentStart(c):
			start := l.pos
			for l.pos < len(l.src) && isIdentCont(l.peek()) {
				l.advance()
			}
			text := l.src[start:l.pos]
			kind := TIdent
			if IsKeyword(text) {
				kind = TKeyword
			}
			out = append(out, Token{Kind: kind, Text: text, Line: startLine, Col: startCol, File: l.file})

		case isDigit(c) || (c == '.' && isDigit(l.peekAt(1))):
			tok, err := l.lexNumber()
			if err != nil {
				return nil, err
			}
			tok.Line, tok.Col, tok.File = startLine, startCol, l.file
			out = append(out, tok)

		case c == '"':
			s, err := l.lexString()
			if err != nil {
				return nil, err
			}
			out = append(out, Token{Kind: TString, Text: s, SVal: s, Line: startLine, Col: startCol, File: l.file})

		case c == '\'':
			v, err := l.lexChar()
			if err != nil {
				return nil, err
			}
			out = append(out, Token{Kind: TChar, Text: string(rune(v)), IVal: int64(v), Line: startLine, Col: startCol, File: l.file})

		default:
			matched := ""
			for _, p := range puncts {
				if strings.HasPrefix(l.src[l.pos:], p) {
					matched = p
					break
				}
			}
			if matched == "" {
				return nil, fmt.Errorf("%s:%d:%d: 无法识别的字符 %q", l.file, l.line, l.col, string(c))
			}
			for i := 0; i < len(matched); i++ {
				l.advance()
			}
			out = append(out, Token{Kind: TPunct, Text: matched, Line: startLine, Col: startCol, File: l.file})
		}
	}
}

func (l *Lexer) lexNumber() (Token, error) {
	start := l.pos
	isFloat := false
	if l.peek() == '0' && (l.peekAt(1) == 'x' || l.peekAt(1) == 'X') {
		l.advance()
		l.advance()
		for l.pos < len(l.src) && isHex(l.peek()) {
			l.advance()
		}
		// 整数后缀
		for l.pos < len(l.src) && isIntSuffix(l.peek()) {
			l.advance()
		}
		text := l.src[start:l.pos]
		v, err := parseHexInt(text)
		if err != nil {
			return Token{}, fmt.Errorf("%s:%d: 非法十六进制字面量 %q", l.file, l.line, text)
		}
		return Token{Kind: TInt, Text: text, IVal: v}, nil
	}

	for l.pos < len(l.src) && isDigit(l.peek()) {
		l.advance()
	}
	if l.pos < len(l.src) && l.peek() == '.' {
		isFloat = true
		l.advance()
		for l.pos < len(l.src) && isDigit(l.peek()) {
			l.advance()
		}
	}
	if l.pos < len(l.src) && (l.peek() == 'e' || l.peek() == 'E') {
		isFloat = true
		l.advance()
		if l.pos < len(l.src) && (l.peek() == '+' || l.peek() == '-') {
			l.advance()
		}
		for l.pos < len(l.src) && isDigit(l.peek()) {
			l.advance()
		}
	}
	// 后缀
	for l.pos < len(l.src) && isFloatSuffix(l.peek()) {
		c := l.peek()
		if c == 'f' || c == 'F' {
			isFloat = true
		}
		l.advance()
	}
	text := l.src[start:l.pos]
	if isFloat {
		v, err := strconv.ParseFloat(strings.TrimRight(strings.TrimRight(text, "fF"), "lL"), 64)
		if err != nil {
			return Token{}, fmt.Errorf("%s:%d: 非法浮点字面量 %q", l.file, l.line, text)
		}
		return Token{Kind: TFloat, Text: text, FVal: v}, nil
	}
	digits := strings.TrimRight(text, "uUlL")
	v, err := strconv.ParseUint(digits, 0, 64)
	if err != nil {
		// 八进制 / 十进制回退
		base := 10
		if len(digits) > 1 && digits[0] == '0' {
			base = 8
		}
		v2, err2 := strconv.ParseUint(digits, base, 64)
		if err2 != nil {
			return Token{}, fmt.Errorf("%s:%d: 非法整数字面量 %q", l.file, l.line, text)
		}
		v = v2
	}
	return Token{Kind: TInt, Text: text, IVal: int64(v)}, nil
}

func isHex(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

func isIntSuffix(c byte) bool {
	return c == 'u' || c == 'U' || c == 'l' || c == 'L'
}

func isFloatSuffix(c byte) bool {
	return c == 'f' || c == 'F' || c == 'l' || c == 'L'
}

func parseHexInt(text string) (int64, error) {
	s := text
	if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
		s = s[2:]
	}
	s = strings.TrimRight(s, "uUlL")
	v, err := strconv.ParseUint(s, 16, 64)
	if err != nil {
		return 0, err
	}
	return int64(v), nil
}

// lexString 解码字符串字面量（含转义），返回原始字节串（UTF-8 原样保留）
func (l *Lexer) lexString() (string, error) {
	l.advance() // 开引号
	var b strings.Builder
	for {
		if l.pos >= len(l.src) {
			return "", fmt.Errorf("%s:%d: 字符串未闭合", l.file, l.line)
		}
		c := l.peek()
		if c == '"' {
			l.advance()
			break
		}
		if c == '\n' {
			return "", fmt.Errorf("%s:%d: 字符串内出现换行", l.file, l.line)
		}
		if c == '\\' {
			l.advance()
			v, err := l.lexEscape()
			if err != nil {
				return "", err
			}
			b.WriteByte(v)
			continue
		}
		b.WriteByte(l.advance())
	}
	return b.String(), nil
}

// lexChar 解码字符常量，返回码点（单字节）
func (l *Lexer) lexChar() (int64, error) {
	l.advance() // 开单引号
	if l.pos >= len(l.src) {
		return 0, fmt.Errorf("%s:%d: 字符常量未闭合", l.file, l.line)
	}
	var v int64
	if l.peek() == '\\' {
		l.advance()
		b, err := l.lexEscape()
		if err != nil {
			return 0, err
		}
		v = int64(b)
	} else {
		v = int64(l.advance())
	}
	// 多字符常量（如 'ab'）：按 GCC 规则左移拼接
	for l.pos < len(l.src) && l.peek() != '\'' {
		if l.peek() == '\\' {
			l.advance()
			b, err := l.lexEscape()
			if err != nil {
				return 0, err
			}
			v = (v << 8) | int64(b)
		} else {
			v = (v << 8) | int64(l.advance())
		}
	}
	if l.pos >= len(l.src) || l.peek() != '\'' {
		return 0, fmt.Errorf("%s:%d: 字符常量未闭合", l.file, l.line)
	}
	l.advance()
	return v, nil
}

// lexEscape 处理反斜杠后的转义序列（调用前已跳过 '\\'）
func (l *Lexer) lexEscape() (byte, error) {
	if l.pos >= len(l.src) {
		return 0, fmt.Errorf("转义序列在文件末尾截断")
	}
	c := l.advance()
	switch c {
	case 'n':
		return '\n', nil
	case 't':
		return '\t', nil
	case 'r':
		return '\r', nil
	case 'a':
		return 7, nil
	case 'b':
		return 8, nil
	case 'f':
		return 12, nil
	case 'v':
		return 11, nil
	case '0', '1', '2', '3', '4', '5', '6', '7':
		// 八进制，最多 3 位
		v := int(c - '0')
		for i := 0; i < 2 && l.pos < len(l.src) && l.peek() >= '0' && l.peek() <= '7'; i++ {
			v = v*8 + int(l.advance()-'0')
		}
		return byte(v), nil
	case 'x':
		v := 0
		n := 0
		for l.pos < len(l.src) && isHex(l.peek()) {
			v = v*16 + hexVal(l.advance())
			n++
		}
		if n == 0 {
			return 0, fmt.Errorf("%s:%d: \\x 后缺少十六进制数字", l.file, l.line)
		}
		return byte(v), nil
	case '\\':
		return '\\', nil
	case '\'':
		return '\'', nil
	case '"':
		return '"', nil
	case '?':
		return '?', nil
	default:
		return c, nil
	}
}

func hexVal(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0')
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10
	}
	return 0
}
