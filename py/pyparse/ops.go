package pyparse

// ops.go — 运算符口径转换
//
// gopapy 的 AST 与 CPython ast.dump 兼容：BinOp/UnaryOp/BoolOp/Compare/AugAssign
// 的 op 字段是「操作符类名」（Add / Sub / USub / And / LtE …），
// 而 zuzo py/runtime 的分发用的是符号（"+" / "-" / "not" / "and" / "<=" …）。
// 本文件负责这一层翻译；遇到未知操作符直接返回原值，由上层报错暴露。

var binOpSymbol = map[string]string{
	"Add":      "+",
	"Sub":      "-",
	"Mult":     "*",
	"Div":      "/",
	"FloorDiv": "//",
	"Mod":      "%",
	"Pow":      "**",
	"MatMult":  "@",
	"LShift":   "<<",
	"RShift":   ">>",
	"BitOr":    "|",
	"BitXor":   "^",
	"BitAnd":   "&",
}

var unaryOpSymbol = map[string]string{
	"UAdd":   "+",
	"USub":   "-",
	"Not":    "not",
	"Invert": "~",
}

var boolOpSymbol = map[string]string{
	"And": "and",
	"Or":  "or",
}

var cmpOpSymbol = map[string]string{
	"Eq":    "==",
	"NotEq": "!=",
	"Lt":    "<",
	"LtE":   "<=",
	"Gt":    ">",
	"GtE":   ">=",
	"Is":    "is",
	"IsNot": "is not",
	"In":    "in",
	"NotIn": "not in",
}

func binOp(name string) string {
	if s, ok := binOpSymbol[name]; ok {
		return s
	}
	return name
}

func unaryOp(name string) string {
	if s, ok := unaryOpSymbol[name]; ok {
		return s
	}
	return name
}

func boolOp(name string) string {
	if s, ok := boolOpSymbol[name]; ok {
		return s
	}
	return name
}

func cmpOps(names []string) []string {
	out := make([]string, len(names))
	for i, n := range names {
		if s, ok := cmpOpSymbol[n]; ok {
			out[i] = s
		} else {
			out[i] = n
		}
	}
	return out
}
