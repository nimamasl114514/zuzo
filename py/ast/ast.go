package ast

// Node 所有 AST 节点的公共接口
type Node interface {
	Line() int
}

// Stmt 语句接口
type Stmt interface {
	Node
	stmtNode()
}

// Expr 表达式接口
type Expr interface {
	Node
	exprNode()
}

// Pos 位置信息基类
type Pos struct{ L int }

// Line 返回行号
func (p Pos) Line() int { return p.L }

// Module 一个 Python 模块
type Module struct {
	Body []Stmt
}

func (m *Module) stmtNode() {}
func (m *Module) Line() int { return 1 }

// ==================== 语句 ====================

// ExprStmt 表达式语句
type ExprStmt struct {
	Pos
	X Expr
}

// Assign 赋值: a = b = expr / a, b = expr
type Assign struct {
	Pos
	Targets []Expr
	Value   Expr
}

// AugAssign 增强赋值: a += 1
type AugAssign struct {
	Pos
	Target Expr
	Op     string
	Value  Expr
}

// AnnAssign 注解赋值: a: int = 1
type AnnAssign struct {
	Pos
	Target     Expr
	Annotation Expr
	Value      Expr
}

// If 条件语句
type If struct {
	Pos
	Cond   Expr
	Body   []Stmt
	Orelse []Stmt
}

// While 循环
type While struct {
	Pos
	Cond   Expr
	Body   []Stmt
	Orelse []Stmt
}

// For 迭代循环
type For struct {
	Pos
	Target  Expr
	Iter    Expr
	Body    []Stmt
	Orelse  []Stmt
	IsAsync bool
}

// Arg 函数参数
type Arg struct {
	Name       string
	Annotation Expr
	Default    Expr // nil 表示无默认值
}

// Params 参数表
type Params struct {
	Args   []*Arg // 普通参数（位置/关键字皆可）
	Vararg string // *args 名称，空表示无
	Kwonly []*Arg // 仅关键字参数
	Kwarg  string // **kwargs 名称，空表示无
}

// FuncDef 函数定义
type FuncDef struct {
	Pos
	Name       string
	Params     *Params
	Body       []Stmt
	Decorators []Expr
	Returns    Expr
	IsAsync    bool
}

// Keyword 调用/类定义中的关键字参数
type Keyword struct {
	Name  string // 空表示 ** 解包
	Value Expr
}

// ClassDef 类定义
type ClassDef struct {
	Pos
	Name       string
	Bases      []Expr
	Keywords   []Keyword
	Body       []Stmt
	Decorators []Expr
}

// Return 返回语句
type Return struct {
	Pos
	Value Expr
}

// Break 跳出
type Break struct{ Pos }

// Continue 继续
type Continue struct{ Pos }

// Pass 空语句
type Pass struct{ Pos }

// ExceptHandler except 分支
type ExceptHandler struct {
	Pos
	Type Expr   // nil 表示裸 except
	Name string // as 名称
	Body []Stmt
}

// Try 异常处理
type Try struct {
	Pos
	Body     []Stmt
	Handlers []*ExceptHandler
	Orelse   []Stmt
	Finally  []Stmt
}

// Raise 抛出异常
type Raise struct {
	Pos
	Exc   Expr // nil 表示重新抛出
	Cause Expr
}

// WithItem with 子句项
type WithItem struct {
	Context Expr
	Vars    Expr // as 目标，可为 nil
}

// With 上下文管理
type With struct {
	Pos
	Items   []WithItem
	Body    []Stmt
	IsAsync bool
}

// Alias import 别名
type Alias struct {
	Name string
	As   string
}

// Import import 语句
type Import struct {
	Pos
	Names []Alias
}

// ImportFrom from ... import ... 语句
type ImportFrom struct {
	Pos
	Module string
	Names  []Alias
	Level  int
}

// Global global 声明
type Global struct {
	Pos
	Names []string
}

// Nonlocal nonlocal 声明
type Nonlocal struct {
	Pos
	Names []string
}

// Del del 语句
type Del struct {
	Pos
	Targets []Expr
}

// Assert assert 语句
type Assert struct {
	Pos
	Test Expr
	Msg  Expr
}

func (*ExprStmt) stmtNode()   {}
func (*Assign) stmtNode()     {}
func (*AugAssign) stmtNode()  {}
func (*AnnAssign) stmtNode()  {}
func (*If) stmtNode()         {}
func (*While) stmtNode()      {}
func (*For) stmtNode()        {}
func (*FuncDef) stmtNode()    {}
func (*ClassDef) stmtNode()   {}
func (*Return) stmtNode()     {}
func (*Break) stmtNode()      {}
func (*Continue) stmtNode()   {}
func (*Pass) stmtNode()       {}
func (*Try) stmtNode()        {}
func (*Raise) stmtNode()      {}
func (*With) stmtNode()       {}
func (*Import) stmtNode()     {}
func (*ImportFrom) stmtNode() {}
func (*Global) stmtNode()     {}
func (*Nonlocal) stmtNode()   {}
func (*Del) stmtNode()        {}
func (*Assert) stmtNode()     {}

// ==================== 表达式 ====================

// Name 标识符
type Name struct {
	Pos
	Id string
}

// Number 数字字面量（保留原文，运行时按需解析）
type Number struct {
	Pos
	Value   string
	IsFloat bool
}

// String 字符串字面量（已解码）
type String struct {
	Pos
	Value   string
	IsBytes bool
}

// FStringPart f-string 组成部分
type FStringPart struct {
	Literal    string // 字面部分
	Expr       Expr   // 插值表达式（Literal 非空时忽略）
	Format     string // 格式说明符
	Conversion byte   // 'r' / 's' / 'a'，0 表示无
	IsLiteral  bool
}

// FString f-string
type FString struct {
	Pos
	Parts []FStringPart
}

// List 列表字面量
type List struct {
	Pos
	Elements []Expr
}

// Tuple 元组字面量
type Tuple struct {
	Pos
	Elements []Expr
}

// Set 集合字面量
type Set struct {
	Pos
	Elements []Expr
}

// Dict 字典字面量；Keys 中 nil 表示 ** 解包
type Dict struct {
	Pos
	Keys   []Expr
	Values []Expr
}

// Comp 推导式生成器子句
type Comp struct {
	Target  Expr
	Iter    Expr
	Ifs     []Expr
	IsAsync bool
}

// ListComp 列表推导式
type ListComp struct {
	Pos
	Elt  Expr
	Gens []Comp
}

// SetComp 集合推导式
type SetComp struct {
	Pos
	Elt  Expr
	Gens []Comp
}

// DictComp 字典推导式
type DictComp struct {
	Pos
	Key   Expr
	Value Expr
	Gens  []Comp
}

// GenExp 生成器表达式
type GenExp struct {
	Pos
	Elt  Expr
	Gens []Comp
}

// BinOp 二元运算
type BinOp struct {
	Pos
	Left  Expr
	Op    string
	Right Expr
}

// UnaryOp 一元运算
type UnaryOp struct {
	Pos
	Op      string
	Operand Expr
}

// BoolOp 布尔运算（and / or，可多个操作数）
type BoolOp struct {
	Pos
	Op     string
	Values []Expr
}

// Compare 比较（支持链式）
type Compare struct {
	Pos
	Left        Expr
	Ops         []string
	Comparators []Expr
}

// Call 函数调用
type Call struct {
	Pos
	Func     Expr
	Args     []Expr
	Keywords []Keyword
}

// Attribute 属性访问
type Attribute struct {
	Pos
	Value Expr
	Attr  string
}

// Subscript 下标访问
type Subscript struct {
	Pos
	Value Expr
	Slice Expr
}

// SliceExpr 切片 a[lo:hi:step]
type SliceExpr struct {
	Pos
	Lower Expr
	Upper Expr
	Step  Expr
}

// IfExp 条件表达式 a if cond else b
type IfExp struct {
	Pos
	Test   Expr
	Body   Expr
	Orelse Expr
}

// Lambda 匿名函数
type Lambda struct {
	Pos
	Params *Params
	Body   Expr
}

// Starred 星号解包
type Starred struct {
	Pos
	Value Expr
}

// NamedExpr 海象运算符 :=
type NamedExpr struct {
	Pos
	Target *Name
	Value  Expr
}

// Yield yield 表达式
type Yield struct {
	Pos
	Value Expr
	From  bool
}

// Await await 表达式
type Await struct {
	Pos
	Value Expr
}

// EllipsisEllipsis 字面量 ...
type Ellipsis struct{ Pos }

func (*Name) exprNode()      {}
func (*Number) exprNode()    {}
func (*String) exprNode()    {}
func (*FString) exprNode()   {}
func (*List) exprNode()      {}
func (*Tuple) exprNode()     {}
func (*Set) exprNode()       {}
func (*Dict) exprNode()      {}
func (*ListComp) exprNode()  {}
func (*SetComp) exprNode()   {}
func (*DictComp) exprNode()  {}
func (*GenExp) exprNode()    {}
func (*BinOp) exprNode()     {}
func (*UnaryOp) exprNode()   {}
func (*BoolOp) exprNode()    {}
func (*Compare) exprNode()   {}
func (*Call) exprNode()      {}
func (*Attribute) exprNode() {}
func (*Subscript) exprNode() {}
func (*SliceExpr) exprNode() {}
func (*IfExp) exprNode()     {}
func (*Lambda) exprNode()    {}
func (*Starred) exprNode()   {}
func (*NamedExpr) exprNode() {}
func (*Yield) exprNode()     {}
func (*Await) exprNode()     {}
func (*Ellipsis) exprNode()  {}

// ==================== match 语句（结构化模式匹配） ====================

// Match match 语句
type Match struct {
	Pos
	Subject Expr
	Cases   []*MatchCase
}

// MatchCase case 分支
type MatchCase struct {
	Pos
	Pattern Pattern
	Guard   Expr
	Body    []Stmt
}

func (*Match) stmtNode() {}

// Pattern 模式接口
type Pattern interface {
	patNode()
	PatternLine() int
}

// PWildcard 通配模式 `_`
type PWildcard struct{ Pos }

// PCapture 捕获模式（绑定名字）
type PCapture struct {
	Pos
	Name string
}

// PLiteral 字面量/常量模式
type PLiteral struct {
	Pos
	Expr Expr
}

// PValue 值模式（点号属性，如 Color.RED）
type PValue struct {
	Pos
	Expr Expr
}

// POr 或模式
type POr struct {
	Pos
	Alts []Pattern
}

// PSequence 序列模式 [a, b, *rest]
type PSequence struct {
	Pos
	Items []Pattern
	Star  string // *rest 的名字，空表示无
}

// PMapping 映射模式 {"k": v, **rest}
type PMapping struct {
	Pos
	Keys []Expr
	Vals []Pattern
	Rest string
}

// KeywordPat 类模式的关键字子模式
type KeywordPat struct {
	Name string
	Pat  Pattern
}

// PClass 类模式 Cls(p1, k=v)
type PClass struct {
	Pos
	Cls        Expr
	Positional []Pattern
	Keywords   []*KeywordPat
}

// PAs as 模式
type PAs struct {
	Pos
	Pat  Pattern
	Name string
}

func (*PWildcard) patNode() {}
func (*PCapture) patNode()  {}
func (*PLiteral) patNode()  {}
func (*PValue) patNode()    {}
func (*POr) patNode()       {}
func (*PSequence) patNode() {}
func (*PMapping) patNode()  {}
func (*PClass) patNode()    {}
func (*PAs) patNode()       {}

// PatternLine 返回模式行号
func (p *PWildcard) PatternLine() int { return p.L }
func (p *PCapture) PatternLine() int  { return p.L }
func (p *PLiteral) PatternLine() int  { return p.L }
func (p *PValue) PatternLine() int    { return p.L }
func (p *POr) PatternLine() int       { return p.L }
func (p *PSequence) PatternLine() int { return p.L }
func (p *PMapping) PatternLine() int  { return p.L }
func (p *PClass) PatternLine() int    { return p.L }
func (p *PAs) PatternLine() int       { return p.L }
