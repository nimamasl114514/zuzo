package token

// Type 词法单元类型
type Type string

const (
	ILLEGAL Type = "ILLEGAL"
	EOF     Type = "EOF"

	// 字面量
	NAME    Type = "NAME"
	NUMBER  Type = "NUMBER"
	STRING  Type = "STRING"
	FSTRING Type = "FSTRING"

	// 结构
	NEWLINE Type = "NEWLINE"
	INDENT  Type = "INDENT"
	DEDENT  Type = "DEDENT"

	// 运算符
	PLUS       Type = "+"
	MINUS      Type = "-"
	STAR       Type = "*"
	DOUBLESTAR Type = "**"
	SLASH      Type = "/"
	DOUBLESLASH Type = "//"
	PERCENT    Type = "%"
	AT         Type = "@"
	LSHIFT     Type = "<<"
	RSHIFT     Type = ">>"
	AMP        Type = "&"
	PIPE       Type = "|"
	CARET      Type = "^"
	TILDE      Type = "~"
	LT         Type = "<"
	GT         Type = ">"
	LE         Type = "<="
	GE         Type = ">="
	EQ         Type = "=="
	NE         Type = "!="

	// 赋值
	ASSIGN     Type = "="
	PLUSEQ     Type = "+="
	MINUSEQ    Type = "-="
	STAREQ     Type = "*="
	SLASHEQ    Type = "/="
	DOUBLESLASHEQ Type = "//="
	PERCENTEQ  Type = "%="
	DOUBLESTAREQ Type = "**="
	AMP_EQ     Type = "&="
	PIPE_EQ    Type = "|="
	CARET_EQ   Type = "^="
	LSHIFTEQ   Type = "<<="
	RSHIFTEQ   Type = ">>="
	ATEQ       Type = "@="
	COLONEQ    Type = ":="

	// 分隔符
	LPAREN   Type = "("
	RPAREN   Type = ")"
	LBRACKET Type = "["
	RBRACKET Type = "]"
	LBRACE   Type = "{"
	RBRACE   Type = "}"
	COMMA    Type = ","
	COLON    Type = ":"
	DOT      Type = "."
	SEMI     Type = ";"
	ARROW    Type = "->"
	ELLIPSIS Type = "..."

	// 关键字
	KW_FALSE    Type = "False"
	KW_NONE     Type = "None"
	KW_TRUE     Type = "True"
	KW_AND      Type = "and"
	KW_AS       Type = "as"
	KW_ASSERT   Type = "assert"
	KW_ASYNC    Type = "async"
	KW_AWAIT    Type = "await"
	KW_BREAK    Type = "break"
	KW_CLASS    Type = "class"
	KW_CONTINUE Type = "continue"
	KW_DEF      Type = "def"
	KW_DEL      Type = "del"
	KW_ELIF     Type = "elif"
	KW_ELSE     Type = "else"
	KW_EXCEPT   Type = "except"
	KW_FINALLY  Type = "finally"
	KW_FOR      Type = "for"
	KW_FROM     Type = "from"
	KW_GLOBAL   Type = "global"
	KW_IF       Type = "if"
	KW_IMPORT   Type = "import"
	KW_IN       Type = "in"
	KW_IS       Type = "is"
	KW_LAMBDA   Type = "lambda"
	KW_NONLOCAL Type = "nonlocal"
	KW_NOT      Type = "not"
	KW_OR       Type = "or"
	KW_PASS     Type = "pass"
	KW_RAISE    Type = "raise"
	KW_RETURN   Type = "return"
	KW_TRY      Type = "try"
	KW_WHILE    Type = "while"
	KW_WITH     Type = "with"
	KW_YIELD    Type = "yield"
)

// Token 词法单元
type Token struct {
	Type    Type
	Literal string
	Line    int
	Column  int
	Extra   string // 附加信息：字符串前缀（b / f / rf 等）
}

var keywords = map[string]Type{
	"False": KW_FALSE, "None": KW_NONE, "True": KW_TRUE,
	"and": KW_AND, "as": KW_AS, "assert": KW_ASSERT, "async": KW_ASYNC,
	"await": KW_AWAIT, "break": KW_BREAK, "class": KW_CLASS,
	"continue": KW_CONTINUE, "def": KW_DEF, "del": KW_DEL,
	"elif": KW_ELIF, "else": KW_ELSE, "except": KW_EXCEPT,
	"finally": KW_FINALLY, "for": KW_FOR, "from": KW_FROM,
	"global": KW_GLOBAL, "if": KW_IF, "import": KW_IMPORT,
	"in": KW_IN, "is": KW_IS, "lambda": KW_LAMBDA,
	"nonlocal": KW_NONLOCAL, "not": KW_NOT, "or": KW_OR,
	"pass": KW_PASS, "raise": KW_RAISE, "return": KW_RETURN,
	"try": KW_TRY, "while": KW_WHILE, "with": KW_WITH,
	"yield": KW_YIELD,
}

// LookupIdent 判断标识符是否为关键字
func LookupIdent(ident string) Type {
	if t, ok := keywords[ident]; ok {
		return t
	}
	return NAME
}
