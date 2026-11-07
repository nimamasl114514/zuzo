package lexer

import (
	"fmt"
	"unicode"

	"zuzo/token"
)

// Error 一个词法层错误（未闭合字符串等），由解析器统一并入诊断报告
type Error struct {
	Code    string // 错误码，如 E007
	Message string
	Line    int
	Column  int
}

// Lexer 词法分析器，将源代码字符串转换为词法单元序列
type Lexer struct {
	input        []rune  // 源代码（rune 切片，支持 UTF-8 中文）
	position     int     // 当前字符位置
	readPosition int     // 下一个读取位置
	ch           rune    // 当前字符
	line         int     // 当前行号
	column       int     // 当前列号
	errors       []Error // 词法错误（不中断扫描，交给解析器统一上报）
}

// Errors 返回扫描期间积累的词法错误
func (l *Lexer) Errors() []Error { return l.errors }

// AddError 记录一个词法错误（导出：供解析器/测试注入）
func (l *Lexer) AddError(code, msg string, line, col int) {
	l.errors = append(l.errors, Error{Code: code, Message: msg, Line: line, Column: col})
}

func (l *Lexer) addError(code, msg string, line, col int) {
	l.AddError(code, msg, line, col)
}

// New 创建一个新的词法分析器
func New(input string) *Lexer {
	l := &Lexer{
		input:  []rune(input),
		line:   1,
		column: 0,
	}
	// 跳过 UTF-8 BOM 头（\uFEFF），避免编辑器添加的 BOM 被误判为 ILLEGAL
	if len(l.input) > 0 && l.input[0] == '\uFEFF' {
		l.readPosition = 1
	}
	l.readChar()
	return l
}

// readChar 读取下一个字符
func (l *Lexer) readChar() {
	if l.readPosition >= len(l.input) {
		l.ch = 0
	} else {
		l.ch = l.input[l.readPosition]
	}
	l.position = l.readPosition
	l.readPosition++
	if l.ch == '\n' {
		l.line++
		l.column = 0
	} else {
		l.column++
	}
}

// peekChar 查看下一个字符但不移动位置
func (l *Lexer) peekChar() rune {
	if l.readPosition >= len(l.input) {
		return 0
	}
	return l.input[l.readPosition]
}

// skipWhitespace 跳过空白字符和注释
func (l *Lexer) skipWhitespace() {
	for {
		switch l.ch {
		case ' ', '\t', '\r', '\n':
			l.readChar()
		case '/':
			if l.peekChar() == '/' {
				// 行注释 // 注释内容
				for l.ch != 0 && l.ch != '\n' {
					l.readChar()
				}
			} else {
				return
			}
		default:
			return
		}
	}
}

// NextToken 返回下一个词法单元
func (l *Lexer) NextToken() token.Token {
	// 记录 token 起始位置：标识符/数字/字符串在读取过程中会推进 line/column，
	// 若用读取完的位置，诊断定位会漂到 token 末尾（甚至跨行）。
	startLine, startCol := l.line, l.column
	var tok token.Token

	l.skipWhitespace()

	tok.Line = l.line
	tok.Column = l.column

	switch l.ch {
	case '=':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.EQ, Literal: runeToString(ch) + runeToString(l.ch), Line: l.line, Column: l.column}
		} else {
			tok = newToken(token.ASSIGN, l.ch, l.line, l.column)
		}
	case '+':
		tok = newToken(token.PLUS, l.ch, l.line, l.column)
	case '-':
		if l.peekChar() == '>' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.ARROW, Literal: runeToString(ch) + runeToString(l.ch), Line: l.line, Column: l.column}
		} else {
			tok = newToken(token.MINUS, l.ch, l.line, l.column)
		}
	case '!':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.NEQ, Literal: runeToString(ch) + runeToString(l.ch), Line: l.line, Column: l.column}
		} else {
			tok = newToken(token.BANG, l.ch, l.line, l.column)
		}
	case '*':
		tok = newToken(token.ASTERISK, l.ch, l.line, l.column)
	case '/':
		tok = newToken(token.SLASH, l.ch, l.line, l.column)
	case '%':
		tok = newToken(token.PERCENT, l.ch, l.line, l.column)
	case '<':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.LTE, Literal: runeToString(ch) + runeToString(l.ch), Line: l.line, Column: l.column}
		} else {
			tok = newToken(token.LT, l.ch, l.line, l.column)
		}
	case '>':
		if l.peekChar() == '=' {
			ch := l.ch
			l.readChar()
			tok = token.Token{Type: token.GTE, Literal: runeToString(ch) + runeToString(l.ch), Line: l.line, Column: l.column}
		} else {
			tok = newToken(token.GT, l.ch, l.line, l.column)
		}
	case ',':
		tok = newToken(token.COMMA, l.ch, l.line, l.column)
	case ';':
		tok = newToken(token.SEMICOLON, l.ch, l.line, l.column)
	case '(':
		tok = newToken(token.LPAREN, l.ch, l.line, l.column)
	case ')':
		tok = newToken(token.RPAREN, l.ch, l.line, l.column)
	case '{':
		tok = newToken(token.LBRACE, l.ch, l.line, l.column)
	case '}':
		tok = newToken(token.RBRACE, l.ch, l.line, l.column)
	case '[':
		tok = newToken(token.LBRACKET, l.ch, l.line, l.column)
	case ']':
		tok = newToken(token.RBRACKET, l.ch, l.line, l.column)
	case ':':
		tok = newToken(token.COLON, l.ch, l.line, l.column)
	case '"':
		// 位置取「开引号」处，而不是读完之后的位置（否则所有字符串的词法位置都会漂到结尾）
		tok.Type = token.STRING
		tok.Literal = l.readString(startLine, startCol)
		tok.Line = startLine
		tok.Column = startCol
	case 0:
		tok.Type = token.EOF
		tok.Literal = ""
	default:
		if isIdentStart(l.ch) {
			ident := l.readIdent()
			// f-字符串前缀检测: f"..." 或 F"..."（Python 风格插值）
			if (ident == "f" || ident == "F") && l.ch == '"' {
				tok.Type = token.FSTRING
				tok.Literal = l.readFString(startLine, startCol)
				tok.Line = startLine
				tok.Column = startCol
				break
			}
			tokType := token.LookupIdent(ident)
			return token.Token{Type: tokType, Literal: ident, Line: startLine, Column: startCol}
		} else if isDigitFast(l.ch) {
			numStr, isFloat := l.readNumber()
			var tt token.TokenType
			if isFloat {
				tt = token.FLOAT
			} else {
				tt = token.INT
			}
			return token.Token{Type: tt, Literal: numStr, Line: startLine, Column: startCol}
		} else if l.ch == '.' && isDigitFast(l.peekChar()) {
			// 支持以小数点开头的浮点数: .5, .0
			numStr, _ := l.readNumber()
			return token.Token{Type: token.FLOAT, Literal: numStr, Line: startLine, Column: startCol}
		} else if l.ch == '.' {
			// 点号成员访问运算符: 模块.成员
			tok = newToken(token.DOT, l.ch, l.line, l.column)
		} else {
			tok = newToken(token.ILLEGAL, l.ch, l.line, l.column)
		}
	}

	l.readChar()
	return tok
}

func newToken(tokenType token.TokenType, ch rune, line, col int) token.Token {
	return token.Token{Type: tokenType, Literal: runeToString(ch), Line: line, Column: col}
}

// asciiStrings ASCII 单字符 → string 预建表（避免每个单字符 token 都分配一次 string）
var asciiStrings [128]string

func init() {
	for i := 0; i < 128; i++ {
		asciiStrings[i] = string(rune(i))
	}
}

// runeToString 单 rune 转 string：ASCII 走预建表（零分配），中文等走标准转换
func runeToString(r rune) string {
	if r >= 0 && r < 128 {
		return asciiStrings[r]
	}
	return string(r)
}

func isIdentStart(r rune) bool {
	// ASCII 快路径：标识符绝大多数是 ASCII，避免 unicode 查表
	if r < 128 {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_'
	}
	return unicode.IsLetter(r) || r == '_'
}

// isDigitFast 数字判定：ASCII 快路径 + unicode 兜底（全角/其它数字）
func isDigitFast(r rune) bool {
	if r < 128 {
		return r >= '0' && r <= '9'
	}
	return unicode.IsDigit(r)
}

func isIdentPart(r rune) bool {
	if r < 128 {
		return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_'
	}
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_'
}

func (l *Lexer) readIdent() string {
	start := l.position
	for isIdentPart(l.ch) {
		l.readChar()
	}
	return string(l.input[start:l.position])
}

func (l *Lexer) readNumber() (string, bool) {
	start := l.position
	isFloat := false

	// 支持以小数点开头的浮点数: .5, .0
	if l.ch == '.' {
		isFloat = true
		l.readChar() // 读取小数点
		for isDigitFast(l.ch) {
			l.readChar()
		}
		return string(l.input[start:l.position]), isFloat
	}

	for isDigitFast(l.ch) {
		l.readChar()
	}
	// 检查小数点
	if l.ch == '.' && isDigitFast(l.peekChar()) {
		isFloat = true
		l.readChar() // 读取小数点
		for isDigitFast(l.ch) {
			l.readChar()
		}
	}
	return string(l.input[start:l.position]), isFloat
}

func (l *Lexer) readString(startLine, startCol int) string {
	var buf []rune
	l.readChar() // 跳过开头的引号
	for l.ch != '"' && l.ch != 0 {
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				buf = append(buf, '\n')
			case 't':
				buf = append(buf, '\t')
			case 'r':
				buf = append(buf, '\r')
			case '"':
				buf = append(buf, '"')
			case '\\':
				buf = append(buf, '\\')
			default:
				buf = append(buf, '\\', l.ch)
			}
		} else {
			buf = append(buf, l.ch)
		}
		l.readChar()
	}
	if l.ch == 0 {
		l.addError("E007",
			fmt.Sprintf("字符串未闭合：起始于 %d:%d 的引号直到文件末尾都没有闭合", startLine, startCol),
			startLine, startCol)
	}
	return string(buf)
}

// readFString 读取 f-字符串内容（Python 风格插值）
// 与 readString 的关键区别：在 {} 内部时，" 不会关闭字符串
// 跟踪 {} 深度 + 嵌套字符串状态，确保 f"{大写("hello")}" 正确读取
func (l *Lexer) readFString(startLine, startCol int) string {
	var buf []rune
	l.readChar()         // 跳过开头的引号
	depth := 0           // {} 深度
	inNestedStr := false // 在 {} 内部是否处于嵌套字符串中

	for l.ch != 0 {
		// --- 在 {} 表达式内部，且不在嵌套字符串中 ---
		if depth > 0 && !inNestedStr {
			if l.ch == '"' {
				inNestedStr = true
				buf = append(buf, l.ch)
				l.readChar()
				continue
			}
			if l.ch == '{' {
				depth++
				buf = append(buf, l.ch)
				l.readChar()
				continue
			}
			if l.ch == '}' {
				depth--
				buf = append(buf, l.ch)
				l.readChar()
				continue
			}
			// 转义字符在表达式内保留原样（交给子解析器处理）
			if l.ch == '\\' {
				buf = append(buf, l.ch)
				l.readChar()
				if l.ch != 0 {
					buf = append(buf, l.ch)
					l.readChar()
				}
				continue
			}
			buf = append(buf, l.ch)
			l.readChar()
			continue
		}

		// --- 在 {} 表达式内部，且在嵌套字符串中 ---
		if depth > 0 && inNestedStr {
			if l.ch == '\\' {
				buf = append(buf, l.ch)
				l.readChar()
				if l.ch != 0 {
					buf = append(buf, l.ch)
					l.readChar()
				}
				continue
			}
			if l.ch == '"' {
				inNestedStr = false
				buf = append(buf, l.ch)
				l.readChar()
				continue
			}
			buf = append(buf, l.ch)
			l.readChar()
			continue
		}

		// --- 在文本部分（depth == 0） ---
		if l.ch == '"' {
			break // 闭合引号
		}
		if l.ch == '\\' {
			l.readChar()
			switch l.ch {
			case 'n':
				buf = append(buf, '\n')
			case 't':
				buf = append(buf, '\t')
			case 'r':
				buf = append(buf, '\r')
			case '"':
				buf = append(buf, '"')
			case '\\':
				buf = append(buf, '\\')
			default:
				buf = append(buf, '\\', l.ch)
			}
			l.readChar()
			continue
		}
		if l.ch == '{' {
			depth = 1
			buf = append(buf, '{')
			l.readChar()
			continue
		}
		// 单个 } 在文本部分直接保留（{{ 和 }} 的处理交给 parser）
		buf = append(buf, l.ch)
		l.readChar()
	}
	if l.ch == 0 {
		if depth > 0 {
			l.addError("E007",
				fmt.Sprintf("f-字符串未闭合：起始于 %d:%d，且插值表达式内的花括号未闭合", startLine, startCol),
				startLine, startCol)
		} else {
			l.addError("E007",
				fmt.Sprintf("f-字符串未闭合：起始于 %d:%d 的引号直到文件末尾都没有闭合", startLine, startCol),
				startLine, startCol)
		}
	}
	return string(buf)
}
