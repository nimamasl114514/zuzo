package nativecc

// ── 顶层声明 ────────────────────────────────────────────

// Decl 顶层声明接口
type Decl interface{ declNode() }

// TranslationUnit 一个编译单元
type TranslationUnit struct {
	Decls []Decl
}

// VarDecl 变量声明/定义（文件作用域）
type VarDecl struct {
	Name     string
	Ty       *CType
	Init     *Init
	IsStatic bool
	IsExtern bool
	Line     int
}

func (*VarDecl) declNode() {}

// FuncDecl 函数声明/定义
type FuncDecl struct {
	Name     string
	Ty       *CType // TyFunc
	Params   []*Param
	Body     *BlockStmt // nil = 仅声明
	IsStatic bool
	Locals   []*VarDecl // 函数内局部变量（codegen 分配栈帧用）
	Boxed    map[string]bool
	Line     int
}

func (*FuncDecl) declNode() {}

// Param 函数参数
type Param struct {
	Name string
	Ty   *CType
}

// TypedefDecl typedef 声明
type TypedefDecl struct {
	Name string
	Ty   *CType
}

func (*TypedefDecl) declNode() {}

// RecordDecl struct/union 定义（类型本体已在 CType 内）
type RecordDecl struct {
	Ty *CType
}

func (*RecordDecl) declNode() {}

// ── 初始化器 ────────────────────────────────────────────

// Init 初始化器：表达式或聚合列表
type Init struct {
	Expr  Expr
	List  []*Init
	IsList bool
}

// ── 语句 ────────────────────────────────────────────────

// Stmt 语句接口
type Stmt interface{ stmtNode() }

// BlockStmt 复合语句
type BlockStmt struct {
	Stmts []Stmt
}

func (*BlockStmt) stmtNode() {}

// ExprStmt 表达式语句
type ExprStmt struct{ X Expr }

func (*ExprStmt) stmtNode() {}

// DeclStmt 局部声明语句
type DeclStmt struct {
	Decls []*VarDecl
}

func (*DeclStmt) stmtNode() {}

// IfStmt if/else
type IfStmt struct {
	Cond Expr
	Then Stmt
	Else Stmt
}

func (*IfStmt) stmtNode() {}

// WhileStmt while
type WhileStmt struct {
	Cond Expr
	Body Stmt
}

func (*WhileStmt) stmtNode() {}

// DoWhileStmt do-while
type DoWhileStmt struct {
	Body Stmt
	Cond Expr
}

func (*DoWhileStmt) stmtNode() {}

// ForStmt for
type ForStmt struct {
	Init Stmt
	Cond Expr
	Post Expr
	Body Stmt
}

func (*ForStmt) stmtNode() {}

// SwitchStmt switch
type SwitchStmt struct {
	X    Expr
	Body Stmt
}

func (*SwitchStmt) stmtNode() {}

// CaseStmt case/default（X 为 nil 表示 default）
type CaseStmt struct {
	X    Expr
	Stmt Stmt
}

func (*CaseStmt) stmtNode() {}

// BreakStmt break
type BreakStmt struct{}

func (*BreakStmt) stmtNode() {}

// ContinueStmt continue
type ContinueStmt struct{}

func (*ContinueStmt) stmtNode() {}

// ReturnStmt return
type ReturnStmt struct{ X Expr }

func (*ReturnStmt) stmtNode() {}

// GotoStmt goto / LabelStmt 标签
type GotoStmt struct{ Label string }

func (*GotoStmt) stmtNode() {}

// LabelStmt 标签语句
type LabelStmt struct {
	Label string
	Stmt  Stmt
}

func (*LabelStmt) stmtNode() {}

// ── 表达式 ──────────────────────────────────────────────

// Expr 表达式接口
type Expr interface {
	exprNode()
	Type() *CType
	SetType(*CType)
}

// exprBase 公共类型字段
type exprBase struct{ ty *CType }

func (e *exprBase) Type() *CType     { return e.ty }
func (e *exprBase) SetType(t *CType) { e.ty = t }

// Ident 标识符
type Ident struct {
	exprBase
	Name string
}

func (*Ident) exprNode() {}

// IntLit 整数字面量
type IntLit struct {
	exprBase
	V int64
}

func (*IntLit) exprNode() {}

// FloatLit 浮点字面量
type FloatLit struct {
	exprBase
	V float64
}

func (*FloatLit) exprNode() {}

// StrLit 字符串字面量
type StrLit struct {
	exprBase
	V    string
	Sym  string // 后端分配 .rdata 标签
}

func (*StrLit) exprNode() {}

// CharLit 字符常量（等价整数）
type CharLit struct {
	exprBase
	V int64
}

func (*CharLit) exprNode() {}

// BinaryExpr 二元运算
type BinaryExpr struct {
	exprBase
	Op string
	X  Expr
	Y  Expr
}

func (*BinaryExpr) exprNode() {}

// UnaryExpr 一元运算（前置 - + ! ~ & * ++ --）
type UnaryExpr struct {
	exprBase
	Op string
	X  Expr
}

func (*UnaryExpr) exprNode() {}

// PostfixExpr 后置 ++/--
type PostfixExpr struct {
	exprBase
	Op string
	X  Expr
}

func (*PostfixExpr) exprNode() {}

// AssignExpr 赋值（= += -= ...）
type AssignExpr struct {
	exprBase
	Op string
	L  Expr
	R  Expr
}

func (*AssignExpr) exprNode() {}

// CondExpr 三目
type CondExpr struct {
	exprBase
	Cond Expr
	Then Expr
	Else Expr
}

func (*CondExpr) exprNode() {}

// CallExpr 函数调用
type CallExpr struct {
	exprBase
	Fn   Expr
	Args []Expr
}

func (*CallExpr) exprNode() {}

// IndexExpr 下标
type IndexExpr struct {
	exprBase
	X     Expr
	Index Expr
}

func (*IndexExpr) exprNode() {}

// MemberExpr 成员访问（. 或 ->）
type MemberExpr struct {
	exprBase
	X     Expr
	Name  string
	Arrow bool
	Off   int64
}

func (*MemberExpr) exprNode() {}

// CastExpr 类型转换
type CastExpr struct {
	exprBase
	To *CType
	X  Expr
}

func (*CastExpr) exprNode() {}

// SizeofExpr sizeof
type SizeofExpr struct {
	exprBase
	Ty     *CType
	X      Expr
	IsType bool
}

func (*SizeofExpr) exprNode() {}

// CompoundLit 复合字面量 (T){...}
type CompoundLit struct {
	exprBase
	Ty   *CType
	Init *Init
	Sym  string // 静态分配标签（全局区）
}

func (*CompoundLit) exprNode() {}

// BuiltinCall 编译器内建（__builtin_add_overflow 等）
type BuiltinCall struct {
	exprBase
	Name string
	Args []Expr
}

func (*BuiltinCall) exprNode() {}

// InitListExpr 初始化列表（作为表达式出现时等价复合字面量）
type InitListExpr struct {
	exprBase
	Items []Expr
}

func (*InitListExpr) exprNode() {}
