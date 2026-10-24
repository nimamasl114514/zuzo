package lexer

import (
	"fmt"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"zuzo/py/token"
)

// Lexer Python 词法分析器（缩进敏感）
type Lexer struct {
	src    []rune
	pos    int
	line   int
	col    int
	indent []int // 缩进栈
	depth  int   // 括号深度
	toks   []token.Token
	errs   []string
}

// New 创建词法分析器
func New(src string) *Lexer {
	src = strings.TrimPrefix(src, "\ufeff")
	src = strings.ReplaceAll(src, "\r\n", "\n")
	src = strings.ReplaceAll(src, "\r", "\n")
	return &Lexer{src: []rune(src), line: 1, col: 1, indent: []int{0}}
}

// Errors 返回词法错误
func (l *Lexer) Errors() []string { return l.errs }

func (l *Lexer) errf(format string, a ...interface{}) {
	l.errs = append(l.errs, fmt.Sprintf("第 %d 行: %s", l.line, fmt.Sprintf(format, a...)))
}

func (l *Lexer) eof() bool { return l.pos >= len(l.src) }

func (l *Lexer) ch() rune {
	if l.eof() {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) peek(n int) rune {
	if l.pos+n >= len(l.src) {
		return 0
	}
	return l.src[l.pos+n]
}

func (l *Lexer) advance() rune {
	if l.eof() {
		return 0
	}
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

func (l *Lexer) emit(t token.Type, lit string, line, col int) {
	l.toks = append(l.toks, token.Token{Type: t, Literal: lit, Line: line, Column: col})
}

// Tokens 完成词法分析，返回全部 token（含 NEWLINE/INDENT/DEDENT/EOF）
func (l *Lexer) Tokens() []token.Token {
	atLineStart := true
	for !l.eof() {
		if atLineStart && l.depth == 0 {
			if l.handleIndent() {
				atLineStart = false
			}
			if l.eof() {
				break
			}
			continue
		}
		c := l.ch()
		switch {
		case c == ' ' || c == '\t' || c == '\f':
			l.advance()
		case c == '#':
			for !l.eof() && l.ch() != '\n' {
				l.advance()
			}
		case c == '\n':
			if l.depth > 0 {
				l.advance()
				continue
			}
			l.emit(token.NEWLINE, "\n", l.line, l.col)
			l.advance()
			atLineStart = true
		case c == '\\' && l.peek(1) == '\n':
			l.advance()
			l.advance()
		default:
			l.scanToken()
		}
	}
	// 收尾：最后一行若无换行，补一个 NEWLINE
	if len(l.toks) > 0 {
		last := l.toks[len(l.toks)-1]
		if last.Type != token.NEWLINE && last.Type != token.DEDENT {
			l.emit(token.NEWLINE, "\n", l.line, l.col)
		}
	}
	for len(l.indent) > 1 {
		l.indent = l.indent[:len(l.indent)-1]
		l.emit(token.DEDENT, "", l.line, l.col)
	}
	l.emit(token.EOF, "", l.line, l.col)
	return l.toks
}

// handleIndent 处理行首缩进。返回 true 表示本行有实际内容（已处理缩进）。
// 空行 / 纯注释行不产生 INDENT/DEDENT，也不产生 NEWLINE。
func (l *Lexer) handleIndent() bool {
	start := l.pos
	width := 0
	for !l.eof() {
		c := l.ch()
		if c == ' ' {
			width++
			l.advance()
		} else if c == '\t' {
			width += 8 - (width % 8)
			l.advance()
		} else if c == '\f' {
			width = 0
			l.advance()
		} else {
			break
		}
	}
	if l.eof() {
		return false
	}
	c := l.ch()
	if c == '\n' {
		l.advance()
		return false
	}
	if c == '#' {
		for !l.eof() && l.ch() != '\n' {
			l.advance()
		}
		if !l.eof() {
			l.advance()
		}
		return false
	}
	// 有实际内容：比较缩进
	cur := l.indent[len(l.indent)-1]
	if width > cur {
		l.indent = append(l.indent, width)
		l.emit(token.INDENT, "", l.line, 1)
	} else if width < cur {
		for len(l.indent) > 1 && width < l.indent[len(l.indent)-1] {
			l.indent = l.indent[:len(l.indent)-1]
			l.emit(token.DEDENT, "", l.line, 1)
		}
		if width != l.indent[len(l.indent)-1] {
			l.errf("缩进不一致（%d 列不匹配任何外层缩进级别）", width)
			l.indent = append(l.indent, width)
		}
	}
	if l.pos == start && width > 0 {
		// 不应发生
	}
	return true
}

func isIdentStart(r rune) bool {
	return r == '_' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r)
}

func (l *Lexer) scanToken() {
	line, col := l.line, l.col
	c := l.ch()

	// 字符串前缀或标识符
	if isIdentStart(c) {
		// 先判断字符串前缀（命中时 tryString 内部已产出 token）
		if l.tryString() {
			return
		}
		var sb strings.Builder
		for !l.eof() && isIdentPart(l.ch()) {
			sb.WriteRune(l.advance())
		}
		word := sb.String()
		l.emit(token.LookupIdent(word), word, line, col)
		return
	}

	// 数字
	if c >= '0' && c <= '9' || (c == '.' && l.peek(1) >= '0' && l.peek(1) <= '9') {
		l.scanNumber()
		return
	}

	// 字符串（无前缀）
	if c == '\'' || c == '"' {
		s, isBytes, err := l.scanStringBody("")
		if err != nil {
			l.errf("%v", err)
			return
		}
		t := token.STRING
		if isBytes {
			l.emitExtra(t, s, line, col, "b")
		} else {
			l.emit(t, s, line, col)
		}
		return
	}

	// 运算符
	switch c {
	case '+':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.PLUSEQ, "+=", line, col)
		} else {
			l.emit(token.PLUS, "+", line, col)
		}
	case '-':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.MINUSEQ, "-=", line, col)
		} else if l.ch() == '>' {
			l.advance()
			l.emit(token.ARROW, "->", line, col)
		} else {
			l.emit(token.MINUS, "-", line, col)
		}
	case '*':
		l.advance()
		if l.ch() == '*' {
			l.advance()
			if l.ch() == '=' {
				l.advance()
				l.emit(token.DOUBLESTAREQ, "**=", line, col)
			} else {
				l.emit(token.DOUBLESTAR, "**", line, col)
			}
		} else if l.ch() == '=' {
			l.advance()
			l.emit(token.STAREQ, "*=", line, col)
		} else {
			l.emit(token.STAR, "*", line, col)
		}
	case '/':
		l.advance()
		if l.ch() == '/' {
			l.advance()
			if l.ch() == '=' {
				l.advance()
				l.emit(token.DOUBLESLASHEQ, "//=", line, col)
			} else {
				l.emit(token.DOUBLESLASH, "//", line, col)
			}
		} else if l.ch() == '=' {
			l.advance()
			l.emit(token.SLASHEQ, "/=", line, col)
		} else {
			l.emit(token.SLASH, "/", line, col)
		}
	case '%':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.PERCENTEQ, "%=", line, col)
		} else {
			l.emit(token.PERCENT, "%", line, col)
		}
	case '@':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.ATEQ, "@=", line, col)
		} else {
			l.emit(token.AT, "@", line, col)
		}
	case '<':
		l.advance()
		if l.ch() == '<' {
			l.advance()
			if l.ch() == '=' {
				l.advance()
				l.emit(token.LSHIFTEQ, "<<=", line, col)
			} else {
				l.emit(token.LSHIFT, "<<", line, col)
			}
		} else if l.ch() == '=' {
			l.advance()
			l.emit(token.LE, "<=", line, col)
		} else {
			l.emit(token.LT, "<", line, col)
		}
	case '>':
		l.advance()
		if l.ch() == '>' {
			l.advance()
			if l.ch() == '=' {
				l.advance()
				l.emit(token.RSHIFTEQ, ">>=", line, col)
			} else {
				l.emit(token.RSHIFT, ">>", line, col)
			}
		} else if l.ch() == '=' {
			l.advance()
			l.emit(token.GE, ">=", line, col)
		} else {
			l.emit(token.GT, ">", line, col)
		}
	case '&':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.AMP_EQ, "&=", line, col)
		} else {
			l.emit(token.AMP, "&", line, col)
		}
	case '|':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.PIPE_EQ, "|=", line, col)
		} else {
			l.emit(token.PIPE, "|", line, col)
		}
	case '^':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.CARET_EQ, "^=", line, col)
		} else {
			l.emit(token.CARET, "^", line, col)
		}
	case '~':
		l.advance()
		l.emit(token.TILDE, "~", line, col)
	case '=':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.EQ, "==", line, col)
		} else {
			l.emit(token.ASSIGN, "=", line, col)
		}
	case '!':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.NE, "!=", line, col)
		} else {
			l.errf("非法字符 '!'")
		}
	case ':':
		l.advance()
		if l.ch() == '=' {
			l.advance()
			l.emit(token.COLONEQ, ":=", line, col)
		} else {
			l.emit(token.COLON, ":", line, col)
		}
	case '(':
		l.advance()
		l.depth++
		l.emit(token.LPAREN, "(", line, col)
	case ')':
		l.advance()
		if l.depth > 0 {
			l.depth--
		}
		l.emit(token.RPAREN, ")", line, col)
	case '[':
		l.advance()
		l.depth++
		l.emit(token.LBRACKET, "[", line, col)
	case ']':
		l.advance()
		if l.depth > 0 {
			l.depth--
		}
		l.emit(token.RBRACKET, "]", line, col)
	case '{':
		l.advance()
		l.depth++
		l.emit(token.LBRACE, "{", line, col)
	case '}':
		l.advance()
		if l.depth > 0 {
			l.depth--
		}
		l.emit(token.RBRACE, "}", line, col)
	case ',':
		l.advance()
		l.emit(token.COMMA, ",", line, col)
	case ';':
		l.advance()
		l.emit(token.SEMI, ";", line, col)
	case '.':
		l.advance()
		if l.ch() == '.' && l.peek(1) == '.' {
			l.advance()
			l.advance()
			l.emit(token.ELLIPSIS, "...", line, col)
		} else {
			l.emit(token.DOT, ".", line, col)
		}
	default:
		l.errf("非法字符 %q", string(c))
		l.advance()
	}
}

func (l *Lexer) emitExtra(t token.Type, lit string, line, col int, extra string) {
	l.toks = append(l.toks, token.Token{Type: t, Literal: lit, Line: line, Column: col, Extra: extra})
}

// tryString 尝试把当前位置识别为带前缀的字符串
func (l *Lexer) tryString() bool {
	// 前缀最多 2~3 个字符（r/b/u/f 组合）
	n := 0
	prefix := ""
	raw := false
	isBytes := false
	isF := false
	for n < 3 {
		c := l.peek(n)
		lc := unicode.ToLower(c)
		if lc == 'r' {
			raw = true
		} else if lc == 'b' {
			isBytes = true
		} else if lc == 'f' {
			isF = true
		} else if lc == 'u' {
			// u 前缀无实义
		} else {
			break
		}
		prefix += string(c)
		n++
	}
	if n == 0 {
		return false
	}
	q := l.peek(n)
	if q != '\'' && q != '"' {
		return false
	}
	if isBytes && isF {
		return false // fb 非法组合，按普通标识符处理
	}
	line, col := l.line, l.col
	for i := 0; i < n; i++ {
		l.advance()
	}
	s, bs, err := l.scanStringBodyRaw(prefix)
	if err != nil {
		l.errf("%v", err)
		return true
	}
	if isF {
		// f-string：保留原始内文，由 parser 拆分插值
		l.toks = append(l.toks, token.Token{Type: token.FSTRING, Literal: s, Line: line, Column: col, Extra: prefix})
		return true
	}
	if bs || isBytes {
		l.toks = append(l.toks, token.Token{Type: token.STRING, Literal: s, Line: line, Column: col, Extra: "b"})
	} else {
		l.toks = append(l.toks, token.Token{Type: token.STRING, Literal: s, Line: line, Column: col})
	}
	_ = raw
	return true
}

// scanStringBody 扫描（无前缀的）字符串体，返回解码后内容
func (l *Lexer) scanStringBody(prefix string) (string, bool, error) {
	return l.scanStringBodyRaw(prefix)
}

// scanStringBodyRaw 扫描字符串体。返回内容、是否 bytes。
// raw 前缀时不做转义处理。
func (l *Lexer) scanStringBodyRaw(prefix string) (string, bool, error) {
	raw := strings.ContainsAny(prefix, "rR")
	isF := strings.ContainsAny(prefix, "fF")
	q := l.ch()
	triple := false
	if l.peek(1) == q && l.peek(2) == q {
		triple = true
	}
	if triple {
		l.advance()
		l.advance()
		l.advance()
	} else {
		l.advance()
	}
	var sb strings.Builder
	for {
		if l.eof() {
			return sb.String(), false, fmt.Errorf("字符串未闭合")
		}
		c := l.ch()
		if triple {
			if c == q && l.peek(1) == q && l.peek(2) == q {
				l.advance()
				l.advance()
				l.advance()
				break
			}
		} else {
			if c == q {
				l.advance()
				break
			}
			if c == '\n' {
				return sb.String(), false, fmt.Errorf("单行字符串未闭合")
			}
		}
		if c == '\\' && !raw {
			// f-string 内保留转义原文，交给拆分阶段处理
			if isF {
				sb.WriteRune(l.advance())
				if !l.eof() {
					sb.WriteRune(l.advance())
				}
				continue
			}
			l.advance()
			if l.eof() {
				break
			}
			e := l.advance()
			switch e {
			case 'n':
				sb.WriteRune('\n')
			case 't':
				sb.WriteRune('\t')
			case 'r':
				sb.WriteRune('\r')
			case '0':
				sb.WriteRune(0)
			case 'a':
				sb.WriteRune(7)
			case 'b':
				sb.WriteRune(8)
			case 'f':
				sb.WriteRune(12)
			case 'v':
				sb.WriteRune(11)
			case '\\':
				sb.WriteRune('\\')
			case '\'':
				sb.WriteRune('\'')
			case '"':
				sb.WriteRune('"')
			case '\n':
				// 行续
			case 'x':
				h := ""
				for i := 0; i < 2 && !l.eof(); i++ {
					h += string(l.advance())
				}
				if v, err := strconv.ParseUint(h, 16, 8); err == nil {
					sb.WriteRune(rune(v))
				} else {
					sb.WriteString("\\x" + h)
				}
			case 'u':
				h := ""
				for i := 0; i < 4 && !l.eof(); i++ {
					h += string(l.advance())
				}
				if v, err := strconv.ParseUint(h, 16, 32); err == nil {
					sb.WriteRune(rune(v))
				} else {
					sb.WriteString("\\u" + h)
				}
			case 'U':
				h := ""
				for i := 0; i < 8 && !l.eof(); i++ {
					h += string(l.advance())
				}
				if v, err := strconv.ParseUint(h, 16, 32); err == nil {
					sb.WriteRune(rune(v))
				} else {
					sb.WriteString("\\U" + h)
				}
			default:
				sb.WriteRune('\\')
				sb.WriteRune(e)
			}
			continue
		}
		sb.WriteRune(l.advance())
	}
	return sb.String(), false, nil
}

// scanNumber 扫描数字字面量
func (l *Lexer) scanNumber() {
	line, col := l.line, l.col
	var sb strings.Builder
	// 进制前缀
	if l.ch() == '0' && (l.peek(1) == 'x' || l.peek(1) == 'X' || l.peek(1) == 'o' || l.peek(1) == 'O' || l.peek(1) == 'b' || l.peek(1) == 'B') {
		sb.WriteRune(l.advance())
		sb.WriteRune(l.advance())
		for !l.eof() {
			c := l.ch()
			if (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F') || c == '_' {
				sb.WriteRune(l.advance())
			} else {
				break
			}
		}
		l.emit(token.NUMBER, sb.String(), line, col)
		return
	}
	isFloat := false
	for !l.eof() {
		c := l.ch()
		if c >= '0' && c <= '9' || c == '_' {
			sb.WriteRune(l.advance())
		} else if c == '.' && !isFloat {
			// 注意: a.b 形式不是浮点；只有当后面是数字或非标识符时才算
			if l.peek(1) >= '0' && l.peek(1) <= '9' || (!isIdentStart(l.peek(1)) && l.peek(1) != '.') {
				isFloat = true
				sb.WriteRune(l.advance())
			} else {
				break
			}
		} else if c == 'e' || c == 'E' {
			n1 := l.peek(1)
			n2 := l.peek(2)
			if n1 >= '0' && n1 <= '9' || ((n1 == '+' || n1 == '-') && n2 >= '0' && n2 <= '9') {
				isFloat = true
				sb.WriteRune(l.advance())
				if l.ch() == '+' || l.ch() == '-' {
					sb.WriteRune(l.advance())
				}
			} else {
				break
			}
		} else if (c == 'j' || c == 'J') && !isFloat {
			// 复数：暂不支持，吞掉
			sb.WriteRune(l.advance())
			break
		} else {
			break
		}
	}
	l.emit(token.NUMBER, sb.String(), line, col)
}

// Unescape 解码字符串转义（供 f-string 字面部分使用）
func Unescape(s string) string {
	if !strings.ContainsRune(s, '\\') {
		return s
	}
	rs := []rune(s)
	var sb strings.Builder
	for i := 0; i < len(rs); i++ {
		c := rs[i]
		if c != '\\' || i+1 >= len(rs) {
			sb.WriteRune(c)
			continue
		}
		i++
		e := rs[i]
		switch e {
		case 'n':
			sb.WriteRune('\n')
		case 't':
			sb.WriteRune('\t')
		case 'r':
			sb.WriteRune('\r')
		case '0':
			sb.WriteRune(0)
		case '\\':
			sb.WriteRune('\\')
		case '\'':
			sb.WriteRune('\'')
		case '"':
			sb.WriteRune('"')
		case 'a':
			sb.WriteRune(7)
		case 'b':
			sb.WriteRune(8)
		case 'f':
			sb.WriteRune(12)
		case 'v':
			sb.WriteRune(11)
		case '\n':
		case 'x':
			if i+2 < len(rs) {
				if v, err := strconv.ParseUint(string(rs[i+1:i+3]), 16, 8); err == nil {
					sb.WriteRune(rune(v))
					i += 2
					continue
				}
			}
			sb.WriteString("\\x")
		case 'u':
			if i+4 < len(rs) {
				if v, err := strconv.ParseUint(string(rs[i+1:i+5]), 16, 32); err == nil {
					sb.WriteRune(rune(v))
					i += 4
					continue
				}
			}
			sb.WriteString("\\u")
		default:
			sb.WriteRune('\\')
			sb.WriteRune(e)
		}
	}
	return sb.String()
}

var _ = utf8.RuneLen
