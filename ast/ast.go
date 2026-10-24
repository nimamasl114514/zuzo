package ast

import (
	"bytes"
	"strings"

	"zuzo/token"
)

// Node 是所有 AST 节点的接口
type Node interface {
	TokenLiteral() string
	String() string
}

// Statement 语句节点接口
type Statement interface {
	Node
	statementNode()
}

// Expression 表达式节点接口
type Expression interface {
	Node
	expressionNode()
}

// Program 整个程序
type Program struct {
	Statements []Statement
}

func (p *Program) TokenLiteral() string {
	if len(p.Statements) > 0 {
		return p.Statements[0].TokenLiteral()
	}
	return ""
}

func (p *Program) String() string {
	var buf bytes.Buffer
	for _, s := range p.Statements {
		buf.WriteString(s.String())
	}
	return buf.String()
}

// ==================== 语句 ====================

// LetStatement 变量声明: 变量 x = 表达式
// TypeAnnot 是可选的类型注解: 变量 x: 整数 = 表达式
// 为 nil 时表示无注解
// ParsedType 是注解对应的 Type（通过 infer.ParseTypeAnnotation 解析）
type LetStatement struct {
	Token     token.Token // 变量
	Name      *Identifier
	TypeAnnot string // 类型注解文本（如 "整数"），为空表示无注解
	Value     Expression
}

func (ls *LetStatement) statementNode()       {}
func (ls *LetStatement) TokenLiteral() string { return ls.Token.Literal }
func (ls *LetStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString(ls.TokenLiteral() + " ")
	buf.WriteString(ls.Name.String())
	buf.WriteString(" = ")
	if ls.Value != nil {
		buf.WriteString(ls.Value.String())
	}
	buf.WriteString(";")
	return buf.String()
}

// ReturnStatement 返回语句: 返回 表达式
type ReturnStatement struct {
	Token       token.Token // 返回
	ReturnValue Expression
}

func (rs *ReturnStatement) statementNode()       {}
func (rs *ReturnStatement) TokenLiteral() string { return rs.Token.Literal }
func (rs *ReturnStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString(rs.TokenLiteral() + " ")
	if rs.ReturnValue != nil {
		buf.WriteString(rs.ReturnValue.String())
	}
	buf.WriteString(";")
	return buf.String()
}

// ExpressionStatement 表达式语句
type ExpressionStatement struct {
	Token      token.Token
	Expression Expression
}

func (es *ExpressionStatement) statementNode()       {}
func (es *ExpressionStatement) TokenLiteral() string { return es.Token.Literal }
func (es *ExpressionStatement) String() string {
	if es.Expression != nil {
		return es.Expression.String() + ";"
	}
	return ";"
}

// WhileStatement 当循环: 当 条件 { 循环体 }
type WhileStatement struct {
	Token     token.Token // 当
	Condition Expression
	Body      *BlockStatement
}

func (ws *WhileStatement) statementNode()       {}
func (ws *WhileStatement) TokenLiteral() string { return ws.Token.Literal }
func (ws *WhileStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString(ws.TokenLiteral() + " ")
	buf.WriteString(ws.Condition.String())
	buf.WriteString(" " + ws.Body.String())
	return buf.String()
}

// ForStatement 对于循环: 对于 init; cond; update { body }
type ForStatement struct {
	Token     token.Token // 对于
	Init      Expression
	Condition Expression
	Update    Expression
	Body      *BlockStatement
}

func (fs *ForStatement) statementNode()       {}
func (fs *ForStatement) TokenLiteral() string { return fs.Token.Literal }
func (fs *ForStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString(fs.TokenLiteral() + " ")
	if fs.Init != nil {
		buf.WriteString(fs.Init.String())
	}
	buf.WriteString("; ")
	if fs.Condition != nil {
		buf.WriteString(fs.Condition.String())
	}
	buf.WriteString("; ")
	if fs.Update != nil {
		buf.WriteString(fs.Update.String())
	}
	buf.WriteString(" " + fs.Body.String())
	return buf.String()
}

// ImportStatement 导入语句: 导入 "文件路径" 或 导入 "文件路径" 为 模块名
type ImportStatement struct {
	Token token.Token // 导入
	Path  string      // 文件路径
	Alias string      // 模块名（为空表示直接导入到当前作用域）
}

func (is *ImportStatement) statementNode()       {}
func (is *ImportStatement) TokenLiteral() string { return is.Token.Literal }
func (is *ImportStatement) String() string {
	s := "导入 \"" + is.Path + "\""
	if is.Alias != "" {
		s += " 为 " + is.Alias
	}
	return s
}

// BlockStatement 代码块: { 语句1; 语句2; ... }
type BlockStatement struct {
	Token      token.Token // {
	Statements []Statement
}

func (bs *BlockStatement) statementNode()       {}
func (bs *BlockStatement) TokenLiteral() string { return bs.Token.Literal }
func (bs *BlockStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString("{ ")
	for _, s := range bs.Statements {
		buf.WriteString(s.String() + " ")
	}
	buf.WriteString("}")
	return buf.String()
}

// ==================== 表达式 ====================

// Identifier 标识符
// 支持可选的类型注解: 参数名: 类型
type Identifier struct {
	Token     token.Token
	Value     string
	TypeAnnot string // 参数类型注解文本，为空表示无注解
}

func (i *Identifier) expressionNode()      {}
func (i *Identifier) TokenLiteral() string { return i.Token.Literal }
func (i *Identifier) String() string       { return i.Value }

// IntegerLiteral 整数字面量
type IntegerLiteral struct {
	Token token.Token
	Value int64
}

func (il *IntegerLiteral) expressionNode()      {}
func (il *IntegerLiteral) TokenLiteral() string { return il.Token.Literal }
func (il *IntegerLiteral) String() string       { return il.Token.Literal }

// FloatLiteral 浮点数字面量
type FloatLiteral struct {
	Token token.Token
	Value float64
}

func (fl *FloatLiteral) expressionNode()      {}
func (fl *FloatLiteral) TokenLiteral() string { return fl.Token.Literal }
func (fl *FloatLiteral) String() string       { return fl.Token.Literal }

// StringLiteral 字符串字面量
type StringLiteral struct {
	Token token.Token
	Value string
}

func (sl *StringLiteral) expressionNode()      {}
func (sl *StringLiteral) TokenLiteral() string { return sl.Token.Literal }
func (sl *StringLiteral) String() string       { return `"` + sl.Value + `"` }

// FStringLiteral f-字符串字面量: f"Hello {name}, count={count}"
// Parts 是交替的字面量字符串和插值表达式
// 例如 f"a{x}b" → Parts = [StringLiteral("a"), Identifier("x"), StringLiteral("b")]
type FStringLiteral struct {
	Token token.Token
	Parts []Expression // StringLiteral（字面文本）或任意 Expression（插值）
}

func (fsl *FStringLiteral) expressionNode()      {}
func (fsl *FStringLiteral) TokenLiteral() string { return fsl.Token.Literal }
func (fsl *FStringLiteral) String() string {
	var buf bytes.Buffer
	buf.WriteString("f\"")
	for _, p := range fsl.Parts {
		buf.WriteString(p.String())
	}
	buf.WriteString("\"")
	return buf.String()
}

// BooleanLiteral 布尔字面量: 真 / 假
type BooleanLiteral struct {
	Token token.Token
	Value bool
}

func (bl *BooleanLiteral) expressionNode()      {}
func (bl *BooleanLiteral) TokenLiteral() string { return bl.Token.Literal }
func (bl *BooleanLiteral) String() string       { return bl.Token.Literal }

// NullLiteral 空值: 空
type NullLiteral struct {
	Token token.Token
}

func (nl *NullLiteral) expressionNode()      {}
func (nl *NullLiteral) TokenLiteral() string { return nl.Token.Literal }
func (nl *NullLiteral) String() string       { return nl.Token.Literal }

// PrefixExpression 前缀表达式: -x, !x, 非x
type PrefixExpression struct {
	Token    token.Token // 前缀运算符
	Operator string
	Right    Expression
}

func (pe *PrefixExpression) expressionNode()      {}
func (pe *PrefixExpression) TokenLiteral() string { return pe.Token.Literal }
func (pe *PrefixExpression) String() string {
	return "(" + pe.Operator + pe.Right.String() + ")"
}

// InfixExpression 中缀表达式: a + b, a 并且 b
type InfixExpression struct {
	Token    token.Token // 运算符
	Operator string
	Left     Expression
	Right    Expression
}

func (ie *InfixExpression) expressionNode()      {}
func (ie *InfixExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *InfixExpression) String() string {
	return "(" + ie.Left.String() + " " + ie.Operator + " " + ie.Right.String() + ")"
}

// AssignExpression 赋值表达式: x = 值 或 数组[i] = 值
type AssignExpression struct {
	Token       token.Token
	Name        *Identifier      // 简单变量赋值（为 nil 时表示索引赋值）
	IndexTarget *IndexExpression // 数组/哈希元素赋值
	Value       Expression
}

func (ae *AssignExpression) expressionNode()      {}
func (ae *AssignExpression) TokenLiteral() string { return ae.Token.Literal }
func (ae *AssignExpression) String() string {
	return ae.Name.String() + " = " + ae.Value.String()
}

// IfExpression 条件表达式: 如果 cond { ... } 否则 { ... }
type IfExpression struct {
	Token       token.Token // 如果
	Condition   Expression
	Consequence *BlockStatement
	Alternative *BlockStatement
}

func (ie *IfExpression) expressionNode()      {}
func (ie *IfExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *IfExpression) String() string {
	var buf bytes.Buffer
	buf.WriteString("如果 " + ie.Condition.String() + " " + ie.Consequence.String())
	if ie.Alternative != nil {
		buf.WriteString(" 否则 " + ie.Alternative.String())
	}
	return buf.String()
}

// FunctionLiteral 函数: 函数 名称(参数) { 函数体 }
// 支持可选的返回类型注解: 函数 名称(参数) -> 整数 { 函数体 }
type FunctionLiteral struct {
	Token        token.Token // 函数
	Name         string
	Parameters   []*Identifier
	RetTypeAnnot string // 返回类型注解文本，为空表示无注解
	Body         *BlockStatement
}

func (fl *FunctionLiteral) expressionNode()      {}
func (fl *FunctionLiteral) TokenLiteral() string { return fl.Token.Literal }
func (fl *FunctionLiteral) String() string {
	var params []string
	for _, p := range fl.Parameters {
		params = append(params, p.String())
	}
	return "函数 " + fl.Name + "(" + strings.Join(params, ", ") + ") " + fl.Body.String()
}

// CallExpression 函数调用: 名称(参数)
type CallExpression struct {
	Token     token.Token // (
	Function  Expression
	Arguments []Expression
	NamedArgs []NamedArg // 具名参数: 名称: 值
}

func (ce *CallExpression) expressionNode()      {}
func (ce *CallExpression) TokenLiteral() string { return ce.Token.Literal }
func (ce *CallExpression) String() string {
	var args []string
	for _, a := range ce.Arguments {
		args = append(args, a.String())
	}
	for _, na := range ce.NamedArgs {
		args = append(args, na.Name+": "+na.Value.String())
	}
	return ce.Function.String() + "(" + strings.Join(args, ", ") + ")"
}

// ArrayLiteral 数组: [1, 2, 3]
type ArrayLiteral struct {
	Token    token.Token // [
	Elements []Expression
}

func (al *ArrayLiteral) expressionNode()      {}
func (al *ArrayLiteral) TokenLiteral() string { return al.Token.Literal }
func (al *ArrayLiteral) String() string {
	var elems []string
	for _, e := range al.Elements {
		elems = append(elems, e.String())
	}
	return "[" + strings.Join(elems, ", ") + "]"
}

// HashLiteral 哈希表: {"键": "值"}
type HashLiteral struct {
	Token token.Token // {
	Pairs map[Expression]Expression
}

func (hl *HashLiteral) expressionNode()      {}
func (hl *HashLiteral) TokenLiteral() string { return hl.Token.Literal }
func (hl *HashLiteral) String() string {
	var pairs []string
	for k, v := range hl.Pairs {
		pairs = append(pairs, k.String()+":"+v.String())
	}
	return "{" + strings.Join(pairs, ", ") + "}"
}

// IndexExpression 索引: arr[0], map["键"]
type IndexExpression struct {
	Token token.Token // [
	Left  Expression
	Index Expression
}

func (ie *IndexExpression) expressionNode()      {}
func (ie *IndexExpression) TokenLiteral() string { return ie.Token.Literal }
func (ie *IndexExpression) String() string {
	return "(" + ie.Left.String() + "[" + ie.Index.String() + "])"
}

// ErrorExpression 错误占位表达式
// 当解析失败时生成，保持 AST 完整性，避免级联 nil 崩溃。
// 解析器继续解析后续代码，让用户一次性看到所有语法错误。
type ErrorExpression struct {
	Token   token.Token
	Message string
}

func (ee *ErrorExpression) expressionNode()      {}
func (ee *ErrorExpression) TokenLiteral() string { return ee.Token.Literal }
func (ee *ErrorExpression) String() string {
	return "[解析错误: " + ee.Message + "]"
}

// ==================== 现代语言特性 ====================

// DefineStatement 定义语句: 定义 名称 { 字段 = 默认值 ... }
type DefineStatement struct {
	Token  token.Token // 定义
	Name   string
	Fields []DefineField
}

type DefineField struct {
	Name  string
	Value Expression // 默认值表达式
}

func (ds *DefineStatement) statementNode()       {}
func (ds *DefineStatement) TokenLiteral() string { return ds.Token.Literal }
func (ds *DefineStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString("定义 " + ds.Name + " {")
	for _, f := range ds.Fields {
		buf.WriteString(" " + f.Name + " = " + f.Value.String() + ";")
	}
	buf.WriteString(" }")
	return buf.String()
}

// NamedArg 命名参数: 名称: 值
type NamedArg struct {
	Name  string
	Value Expression
}

// TryCatchStatement 执行/遇到 语句: 执行 { ... } 遇到 err { ... }
type TryCatchStatement struct {
	Token   token.Token // 执行
	Body    *BlockStatement
	ErrVar  string // 错误变量名
	Handler *BlockStatement
}

func (ts *TryCatchStatement) statementNode()       {}
func (ts *TryCatchStatement) TokenLiteral() string { return ts.Token.Literal }
func (ts *TryCatchStatement) String() string {
	return "执行 " + ts.Body.String() + " 遇到 " + ts.ErrVar + " " + ts.Handler.String()
}

// ForInStatement 遍历循环: 对于 x 在 ... [到 ...] [满足 ...] { ... }
type ForInStatement struct {
	Token     token.Token // 对于
	VarName   string      // 循环变量名
	IndexName string      // 索引变量名（可为空，表示无索引）
	Iterable  Expression  // 可迭代对象（数组/哈希）或范围起始值
	EndExpr   Expression  // 范围结束值（为空表示集合遍历）
	Guard     Expression  // 条件过滤（可为空）
	Body      *BlockStatement
}

func (fs *ForInStatement) statementNode()       {}
func (fs *ForInStatement) TokenLiteral() string { return fs.Token.Literal }
func (fs *ForInStatement) String() string {
	var buf bytes.Buffer
	buf.WriteString("对于 " + fs.VarName)
	if fs.IndexName != "" {
		buf.WriteString(", " + fs.IndexName)
	}
	buf.WriteString(" 在 " + fs.Iterable.String())
	if fs.EndExpr != nil {
		buf.WriteString(" 到 " + fs.EndExpr.String())
	}
	if fs.Guard != nil {
		buf.WriteString(" 满足 " + fs.Guard.String())
	}
	buf.WriteString(" " + fs.Body.String())
	return buf.String()
}

// BreakStatement 跳出循环
type BreakStatement struct {
	Token token.Token // 跳出
}

func (bs *BreakStatement) statementNode()       {}
func (bs *BreakStatement) TokenLiteral() string { return bs.Token.Literal }
func (bs *BreakStatement) String() string       { return "跳出" }

// ContinueStatement 跳过本次循环
type ContinueStatement struct {
	Token token.Token // 跳过
}

func (cs *ContinueStatement) statementNode()       {}
func (cs *ContinueStatement) TokenLiteral() string { return cs.Token.Literal }
func (cs *ContinueStatement) String() string       { return "跳过" }
