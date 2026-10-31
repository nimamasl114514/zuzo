package nativecc

import "fmt"

// TokKind 词法单元类别
type TokKind int

const (
	TEOF TokKind = iota
	TIdent
	TKeyword
	TInt
	TFloat
	TString
	TChar
	TPunct
)

// Token 一个词法单元（保留原始文本与字面量解码值）
type Token struct {
	Kind TokKind
	Text string // 原始文本（标识符/关键字/运算符）
	IVal int64
	FVal float64
	SVal string // 字符串字面量解码后字节串；字符常量单字符串
	Line int
	Col  int
	File string
}

func (t Token) String() string {
	switch t.Kind {
	case TEOF:
		return "<eof>"
	case TString:
		return fmt.Sprintf("%q", t.SVal)
	}
	return t.Text
}

// Is 判断是否为指定标点/关键字
func (t Token) Is(s string) bool { return t.Text == s }

// keywords C89 全部关键字 + C99 常用
var keywords = map[string]bool{
	"auto": true, "break": true, "case": true, "char": true, "const": true,
	"continue": true, "default": true, "do": true, "double": true, "else": true,
	"enum": true, "extern": true, "float": true, "for": true, "goto": true,
	"if": true, "inline": true, "int": true, "long": true, "register": true,
	"restrict": true, "return": true, "short": true, "signed": true, "sizeof": true,
	"static": true, "struct": true, "switch": true, "typedef": true, "union": true,
	"unsigned": true, "void": true, "volatile": true, "while": true,
	"_Bool": true, "_Alignof": true,
}

// IsKeyword 判断是否为 C 关键字
func IsKeyword(s string) bool { return keywords[s] }

// puncts 运算符表（必须按长度降序，保证最长匹配）
var puncts = []string{
	">>=", "<<=", "...", "->", "++", "--", "<<", ">>", "<=", ">=", "==", "!=",
	"&&", "||", "+=", "-=", "*=", "/=", "%=", "&=", "|=", "^=", "##",
	"+", "-", "*", "/", "%", "<", ">", "=", "!", "&", "|", "^", "~", "?",
	":", ";", ",", ".", "(", ")", "[", "]", "{", "}", "#",
}
