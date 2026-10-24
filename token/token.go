package token

// TokenType 是词法单元类型的字符串表示
type TokenType string

// Token 表示一个词法单元
type Token struct {
	Type    TokenType
	Literal string
	Line    int
	Column  int
}

// 词法单元类型常量
const (
	ILLEGAL = "ILLEGAL"
	EOF     = "EOF"

	// 字面量和标识符
	IDENT   = "IDENT"   // 变量名、函数名
	INT     = "INT"     // 整数
	FLOAT   = "FLOAT"   // 浮点数
	STRING  = "STRING"  // 字符串
	FSTRING = "FSTRING" // f-字符串（Python 风格插值: f"Hello {name}"）

	// 运算符
	ASSIGN   = "="
	PLUS     = "+"
	MINUS    = "-"
	BANG     = "!"
	ASTERISK = "*"
	SLASH    = "/"
	PERCENT  = "%"
	LT       = "<"
	GT       = ">"
	LTE      = "<="
	GTE      = ">="
	EQ       = "=="
	NEQ      = "!="

	// 分隔符
	COMMA     = ","
	SEMICOLON = ";"
	LPAREN    = "("
	RPAREN    = ")"
	LBRACE    = "{"
	RBRACE    = "}"
	LBRACKET  = "["
	RBRACKET  = "]"
	COLON     = ":"
	DOT       = "."
	ARROW     = "->" // 类型注解中的返回类型标记

	// 中文关键字
	LET      = "变量"
	FUNCTION = "函数"
	IF       = "如果"
	ELSE     = "否则" // 也用作 fallback 运算符
	WHILE    = "当"
	FOR      = "对于"
	RETURN   = "返回"
	TRUE     = "真"
	FALSE    = "假"
	NULL     = "空"
	AND      = "并且"
	OR       = "或者"
	NOT      = "非"
	IMPORT   = "导入"
	AS       = "为"
	DEFINE   = "定义" // 数据建模
	TRY      = "执行" // 错误处理
	CATCH    = "遇到" // 错误捕获
	IN       = "在"  // 迭代器
	TO       = "到"  // 范围
	WHEN     = "满足" // 条件过滤
	SKIP     = "跳过" // continue
	BREAK    = "跳出" // break
)

// keywords 将中文关键字映射到词法单元类型
var keywords = map[string]TokenType{
	"变量": LET,
	"函数": FUNCTION,
	"如果": IF,
	"否则": ELSE,
	"当":  WHILE,
	"对于": FOR,
	"返回": RETURN,
	"真":  TRUE,
	"假":  FALSE,
	"空":  NULL,
	"并且": AND,
	"或者": OR,
	"非":  NOT,
	"导入": IMPORT,
	"为":  AS,
	"定义": DEFINE,
	"执行": TRY,
	"遇到": CATCH,
	"在":  IN,
	"到":  TO,
	"满足": WHEN,
	"跳过": SKIP,
	"跳出": BREAK,
}

// LookupIdent 查找标识符是否是关键字
func LookupIdent(ident string) TokenType {
	if t, ok := keywords[ident]; ok {
		return t
	}
	return IDENT
}
