package parser

import (
	"fmt"
	"strconv"
	"strings"

	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/lexer"
	"zuzo/token"
)

// 运算符优先级（从低到高）
const (
	_LOWEST      = -1
	_ASSIGN      = 0  // = 赋值
	_FALLBACK    = 1  // 否则 fallback
	_OR          = 2  // 或者
	_AND         = 3  // 并且
	_EQUALS      = 4  // == !=
	_LESSGREATER = 5  // < > <= >=
	_SUM         = 6  // + -
	_PRODUCT     = 7  // * / %
	_PREFIX      = 8  // - ! 非
	_CALL        = 9  // 函数调用 ()
	_INDEX       = 10 // 索引 []
)

// OperatorDef 运算符定义：集中描述一个 token 的优先级和解析函数
// 将原先分散在 precedences、prefixParseFns、infixParseFns 三处的信息合并为一条记录。
// 新增运算符只需在此表中添加一行，无需修改多处 map。
type OperatorDef struct {
	Precedence int            // 中缀优先级（前缀优先级由前缀函数内部硬编码）
	PrefixFn   prefixParseFn  // 前缀解析函数（可为 nil）
	InfixFn    infixParseFn   // 中缀解析函数（可为 nil）
}

type (
	prefixParseFn func() ast.Expression
	infixParseFn  func(ast.Expression) ast.Expression
)

// Parser 语法分析器（Pratt 解析法）
type Parser struct {
	l *lexer.Lexer

	curToken  token.Token
	peekToken token.Token

	operatorDefs map[token.TokenType]OperatorDef // 集中式运算符定义表

	errors []string
	diag   *diagnostic.Report // 活字诊断报告

	// 活字因果链：追踪最近产出的根因活字，用于将恢复期间的连锁错误
	// 标记为 LevelCascade（连带影响）而非 LevelRoot（根因），
	// 使 Render() 能正确排版为树形因果链而非一堆平行根因。
	lastRoot *diagnostic.Atom
}

// New 创建一个新的语法分析器
func New(l *lexer.Lexer) *Parser {
	p := &Parser{
		l:    l,
		diag: diagnostic.New(),
	}

	// 集中式运算符定义表：一行描述一个 token 的完整行为
	// Precedence 为中缀优先级；仅前缀的 token 用 _LOWEST
	p.operatorDefs = map[token.TokenType]OperatorDef{
		// --- 仅前缀（字面量、标识符） ---
		token.IDENT:     {PrefixFn: p.parseIdentifier},
		token.INT:       {PrefixFn: p.parseIntegerLiteral},
		token.FLOAT:     {PrefixFn: p.parseFloatLiteral},
		token.STRING:    {PrefixFn: p.parseStringLiteral},
		token.FSTRING:   {PrefixFn: p.parseFStringLiteral},
		token.TRUE:      {PrefixFn: p.parseBooleanLiteral},
		token.FALSE:     {PrefixFn: p.parseBooleanLiteral},
		token.NULL:      {PrefixFn: p.parseNullLiteral},
		token.LBRACKET:  {PrefixFn: p.parseArrayLiteral, InfixFn: p.parseIndexExpression, Precedence: _INDEX},
		token.LBRACE:    {PrefixFn: p.parseHashLiteral},
		token.IF:        {PrefixFn: p.parseIfExpression},
		token.FUNCTION:  {PrefixFn: p.parseFunctionLiteral},

		// --- 前缀 + 中缀（一元/二元 双角色） ---
		token.MINUS:     {Precedence: _SUM,   PrefixFn: p.parsePrefixExpression, InfixFn: p.parseInfixExpression},
		token.BANG:      {Precedence: _PREFIX, PrefixFn: p.parsePrefixExpression},
		token.NOT:       {Precedence: _PREFIX, PrefixFn: p.parsePrefixExpression},
		token.LPAREN:    {Precedence: _CALL,  PrefixFn: p.parseGroupedExpression, InfixFn: p.parseCallExpression},

		// --- 仅中缀（二元运算符） ---
		token.ASSIGN:    {Precedence: _ASSIGN,      InfixFn: p.parseAssignExpression},
		token.PLUS:      {Precedence: _SUM,         InfixFn: p.parseInfixExpression},
		token.SLASH:     {Precedence: _PRODUCT,     InfixFn: p.parseInfixExpression},
		token.ASTERISK:  {Precedence: _PRODUCT,     InfixFn: p.parseInfixExpression},
		token.PERCENT:   {Precedence: _PRODUCT,     InfixFn: p.parseInfixExpression},
		token.EQ:        {Precedence: _EQUALS,      InfixFn: p.parseInfixExpression},
		token.NEQ:       {Precedence: _EQUALS,      InfixFn: p.parseInfixExpression},
		token.LT:        {Precedence: _LESSGREATER, InfixFn: p.parseInfixExpression},
		token.GT:        {Precedence: _LESSGREATER, InfixFn: p.parseInfixExpression},
		token.LTE:       {Precedence: _LESSGREATER, InfixFn: p.parseInfixExpression},
		token.GTE:       {Precedence: _LESSGREATER, InfixFn: p.parseInfixExpression},
		token.AND:       {Precedence: _AND,         InfixFn: p.parseInfixExpression},
		token.OR:        {Precedence: _OR,          InfixFn: p.parseInfixExpression},
		token.ELSE:      {Precedence: _FALLBACK,    InfixFn: p.parseInfixExpression}, // 否则 fallback
		token.DOT:       {Precedence: _INDEX,       InfixFn: p.parseMemberExpression},
	}

	// 读取前两个词法单元
	p.nextToken()
	p.nextToken()

	return p
}

// Errors 返回解析错误列表（向后兼容）
func (p *Parser) Errors() []string {
	return p.errors
}

// Diagnostics 返回活字诊断报告
func (p *Parser) Diagnostics() *diagnostic.Report {
	return p.diag
}

// addRootError 添加一个根因活字到诊断报告
// 活字设计：每个根因都会更新 lastRoot 指针，供后续恢复时的连带检测使用。
func (p *Parser) addRootError(code string, line, col int, msg, suggestion string) *diagnostic.Atom {
	p.errors = append(p.errors, fmt.Sprintf("[行 %d] %s", line, msg))
	a := p.diag.AddRoot(code, line, col, msg, suggestion)
	p.lastRoot = a
	return a
}

// addCascadeError 添加一个连带影响活字，关联到最近的根因。
// 活字设计：当错误恢复期间产生的新错误与已有根因在相近位置（±3 行内）时，
// 将其标记为 LevelCascade 而非 LevelRoot，使 Render() 能排版为因果树。
func (p *Parser) addCascadeError(code string, line, col int, msg, suggestion string) {
	if p.lastRoot != nil && abs(line-p.lastRoot.Line) <= 3 {
		p.diag.AddCascade(p.lastRoot, code, line, col, msg, suggestion)
	} else {
		// 超出因果链范围，作为新的根因
		p.addRootError(code, line, col, msg, suggestion)
	}
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func (p *Parser) peekError(t token.TokenType) {
	msg := fmt.Sprintf("期望下一个词法单元为 %s，但得到 %s ('%s')",
		t, p.peekToken.Type, p.peekToken.Literal)
	suggestion := fmt.Sprintf("是否遗漏了 %s？", tokenName(t))
	// 活字因果链：peekError 通常因前面的根因导致 token 偏移，
	// 如果附近已有根因则标记为连带影响。
	p.addCascadeError(diagnostic.ErrExpectedToken.Code, p.peekToken.Line, p.peekToken.Column, msg, suggestion)
}

func (p *Parser) nextToken() {
	p.curToken = p.peekToken
	p.peekToken = p.l.NextToken()
}

func (p *Parser) curTokenIs(t token.TokenType) bool {
	return p.curToken.Type == t
}

func (p *Parser) peekTokenIs(t token.TokenType) bool {
	return p.peekToken.Type == t
}

func (p *Parser) expectPeek(t token.TokenType) bool {
	if !p.peekTokenIs(t) {
		p.peekError(t)
		return false
	}
	p.nextToken()
	return true
}

// peekPrecedence 返回下一个 token 的中缀优先级
// precedenceOf 静态优先级表：热路径（每个 token 两次查询）不查 map
func precedenceOf(t token.TokenType) int {
	switch t {
	case token.ASSIGN:
		return _ASSIGN
	case token.PLUS, token.MINUS:
		return _SUM
	case token.SLASH, token.ASTERISK, token.PERCENT:
		return _PRODUCT
	case token.EQ, token.NEQ:
		return _EQUALS
	case token.LT, token.GT, token.LTE, token.GTE:
		return _LESSGREATER
	case token.AND:
		return _AND
	case token.OR:
		return _OR
	case token.ELSE:
		return _FALLBACK
	case token.LBRACKET, token.DOT:
		return _INDEX
	case token.LPAREN:
		return _CALL
	case token.BANG, token.NOT:
		return _PREFIX
	}
	return _LOWEST
}

func (p *Parser) peekPrecedence() int {
	return precedenceOf(p.peekToken.Type)
}

// curPrecedence 返回当前 token 的中缀优先级
func (p *Parser) curPrecedence() int {
	return precedenceOf(p.curToken.Type)
}

// ==================== 错误恢复 ====================

// statementStartTokens 语句起始关键字集合
// zuzo 不强制分号，换行即语句分隔，所以语句起始关键字本身是天然的安全恢复点。
// 遇到这些关键字时，解析器可以安全地从新语句开始解析。
var statementStartTokens = []token.TokenType{
	token.LET,      // 变量
	token.FUNCTION, // 函数
	token.IF,       // 如果
	token.WHILE,    // 当
	token.FOR,      // 对于
	token.RETURN,   // 返回
	token.IMPORT,   // 导入
	token.DEFINE,   // 定义
	token.TRY,      // 执行
	token.SKIP,     // 跳过
	token.BREAK,    // 跳出
}

// syncToStatement 同步恢复：跳过 token 直到遇到语句起始关键字、分号、右花括号或 EOF。
// 用于语句级解析失败后，跳过错误区域，让解析器从下一个安全位置继续。
// 不消耗同步点本身，让调用方的 nextToken 自然推进。
func (p *Parser) syncToStatement() {
	for !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) || p.curTokenIs(token.RBRACE) {
			return
		}
		// 检查是否是语句起始关键字
		for _, t := range statementStartTokens {
			if p.curTokenIs(t) {
				return
			}
		}
		p.nextToken()
	}
}

// ==================== 语句解析 ====================

// ParseProgram 解析整个程序
func (p *Parser) ParseProgram() *ast.Program {
	program := &ast.Program{}
	program.Statements = []ast.Statement{}

	for !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
			continue
		}
		stmt := p.parseStatement()
		if stmt != nil && !p.isNilStatement(stmt) {
			program.Statements = append(program.Statements, stmt)
			p.nextToken() // 成功：推进到下一条语句
		} else {
			// 错误恢复：先跳过当前失败的 token，再同步到安全位置
			p.nextToken()
			p.syncToStatement()
			// syncToStatement 停在安全点（语句关键字/分号/}/EOF），
			// 不再 nextToken，让循环从该位置继续
		}
	}
	return program
}

func (p *Parser) parseStatement() ast.Statement {
	switch p.curToken.Type {
	case token.LET:
		return p.parseLetStatement()
	case token.RETURN:
		return p.parseReturnStatement()
	case token.WHILE:
		return p.parseWhileStatement()
	case token.FOR:
		return p.parseForStatement()
	case token.IMPORT:
		return p.parseImportStatement()
	case token.DEFINE:
		return p.parseDefineStatement()
	case token.TRY:
		return p.parseTryCatchStatement()
	case token.SKIP:
		return p.parseContinueStatement()
	case token.BREAK:
		return p.parseBreakStatement()
	default:
		return p.parseExpressionStatement()
	}
}

func (p *Parser) parseLetStatement() *ast.LetStatement {
	stmt := &ast.LetStatement{Token: p.curToken}

	if !p.expectPeek(token.IDENT) {
		return nil
	}
	stmt.Name = &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	// 可选类型注解: 变量 x: 整数 = ...
	if p.peekTokenIs(token.COLON) {
		p.nextToken() // 消费 ':'
		p.nextToken() // 移到类型名
		if p.curToken.Type != token.IDENT {
			p.addRootError(diagnostic.ErrUnexpectedToken.Code, p.curToken.Line, p.curToken.Column, fmt.Sprintf("类型注解必须是标识符，得到 %s", p.curToken.Type), "")
			return nil
		}
		stmt.TypeAnnot = p.curToken.Literal
	}

	if !p.expectPeek(token.ASSIGN) {
		return nil
	}
	p.nextToken()

	stmt.Value = p.parseExpression(_LOWEST)

	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

// bareReturnTokens 列出 '返回' 后面不合法的 token——
// 这些 token 说明用户写的是裸返回（返回 } / 返回 ; / 返回 EOF）。
// 活字设计：此场景产出一个 E008 根因活字，并自动补为 NullLiteral，
// 避免 parseExpression 消费 } 导致块解析偏移→级联雪崩。
var bareReturnTokens = map[token.TokenType]bool{
	token.RBRACE:    true,
	token.SEMICOLON: true,
	token.EOF:       true,
}

func (p *Parser) parseReturnStatement() *ast.ReturnStatement {
	stmt := &ast.ReturnStatement{Token: p.curToken}

	// 活字诊断：检测裸返回——返回 后面紧跟 } ; EOF
	if bareReturnTokens[p.peekToken.Type] {
		p.addRootError(diagnostic.ErrBareReturn.Code,
			p.curToken.Line, p.curToken.Column,
			"'返回' 语句缺少返回值",
			"使用 '返回 空' 表示返回空值，或在返回后提供表达式")
		// 自动补为空值，不消费 peekToken，让上层 parseBlockStatement 正常看到 }
		stmt.ReturnValue = &ast.NullLiteral{Token: p.curToken}
		if p.peekTokenIs(token.SEMICOLON) {
			p.nextToken()
		}
		return stmt
	}

	p.nextToken()
	stmt.ReturnValue = p.parseExpression(_LOWEST)

	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseWhileStatement() *ast.WhileStatement {
	stmt := &ast.WhileStatement{Token: p.curToken}
	p.nextToken()

	stmt.Condition = p.parseExpression(_LOWEST)

	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

func (p *Parser) parseForStatement() ast.Statement {
	stmt := &ast.ForStatement{Token: p.curToken}
	p.nextToken() // 消耗 对于

	// 检查是否是 for-in 语法: 对于 IDENT 在 ...
	if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.IN) {
		varName := p.curToken.Literal
		p.nextToken() // 消耗 IDENT
		p.nextToken() // 消耗 在
		return p.parseForInRest(varName, "")
	}

	// 检查是否是 for-in with index: 对于 IDENT, IDENT 在 ...
	if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.COMMA) {
		firstName := p.curToken.Literal
		p.nextToken() // 消耗 IDENT
		p.nextToken() // 消耗 ,
		if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.IN) {
			secondName := p.curToken.Literal
			p.nextToken() // 消耗 IDENT
			p.nextToken() // 消耗 在
			return p.parseForInRest(secondName, firstName)
		}
		// 不是 for-in，报错
		p.addRootError(diagnostic.ErrExpectedToken.Code, p.curToken.Line, p.curToken.Column,
			"循环语法错误：期望 '在' 关键字", "使用 对于 变量 在 集合 { ... } 语法")
		return nil
	}

	// C-style for: 对于 init; cond; update { body }
	stmt.Init = p.parseExpression(_LOWEST)
	if !p.expectPeek(token.SEMICOLON) {
		return nil
	}
	p.nextToken()

	stmt.Condition = p.parseExpression(_LOWEST)
	if !p.expectPeek(token.SEMICOLON) {
		return nil
	}
	p.nextToken()

	stmt.Update = p.parseExpression(_LOWEST)

	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStatement()
	return stmt
}

// parseImportStatement 解析导入语句: 导入 "文件路径" 或 导入 "文件路径" 为 模块名
func (p *Parser) parseImportStatement() *ast.ImportStatement {
	stmt := &ast.ImportStatement{Token: p.curToken}

	if !p.expectPeek(token.STRING) {
		p.addRootError(diagnostic.ErrInvalidImport.Code, p.curToken.Line, p.curToken.Column,
			"导入语句需要字符串路径", "请使用 导入 \"文件路径\" 语法")
		return nil
	}
	stmt.Path = p.curToken.Literal

	// 检查是否为命名导入: 导入 "文件" 为 模块名
	if p.peekTokenIs(token.AS) {
		p.nextToken() // 消耗 "为"
		if !p.expectPeek(token.IDENT) {
			p.addRootError(diagnostic.ErrInvalidImport.Code, p.curToken.Line, p.curToken.Column,
				"导入 '为' 后需要模块名", "请使用 导入 \"文件路径\" 为 模块名 语法")
			return nil
		}
		stmt.Alias = p.curToken.Literal
	}

	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

func (p *Parser) parseBlockStatement() *ast.BlockStatement {
	block := &ast.BlockStatement{Token: p.curToken}
	block.Statements = []ast.Statement{}
	p.nextToken()

	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
			continue
		}
		stmt := p.parseStatement()
		if stmt != nil {
			block.Statements = append(block.Statements, stmt)
			p.nextToken() // 成功：推进到下一条语句
		} else {
			// 错误恢复：先跳过当前失败的 token，再同步到安全位置
			p.nextToken()
			p.syncToStatement()
		}
	}
	return block
}

func (p *Parser) parseExpressionStatement() *ast.ExpressionStatement {
	stmt := &ast.ExpressionStatement{Token: p.curToken}
	stmt.Expression = p.parseExpression(_LOWEST)

	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

// ==================== 表达式解析（Pratt 解析法） ====================

// expressionTerminators 列出不应出现在表达式起始位置的 token，
// 并附带上下文感知的中文建议——每个活字都携带精准的排版建议。
var expressionTerminators = map[token.TokenType]string{
	token.RBRACE:    "右花括号 '}'——可能缺少返回值或表达式，或上一个语句未正确结束",
	token.RPAREN:    "右括号 ')'——可能函数调用的参数不完整",
	token.RBRACKET:  "右方括号 ']'——可能索引表达式不完整",
	token.SEMICOLON: "分号 ';'——此处期望表达式但遇到了分号",
	token.EOF:       "文件结尾——代码可能不完整，缺少闭合的 } 或 )",
	token.CATCH:     "'遇到' 关键字——'遇到' 只能在 执行 { ... } 之后使用",
	token.ELSE:      "'否则' 关键字——'否则' 必须跟在 如果 { ... } 之后",
	token.COLON:     "冒号 ':'——此处期望表达式",
	token.COMMA:     "逗号 ','——此处期望表达式",
}

func (p *Parser) parseExpression(precedence int) ast.Expression {
	// 活字预检：如果当前 token 明显不是合法表达式起始符，
	// 直接产出 E009 活字（附上下文建议），避免进入 noPrefixParseFnError 的通用流程。
	// 因果链设计：如果附近已有根因，则标记为连带影响。
	if hint, bad := expressionTerminators[p.curToken.Type]; bad {
		msg := fmt.Sprintf("期望表达式，但遇到 %s", hint)
		p.addRootError(diagnostic.ErrUnexpectedToken.Code,
			p.curToken.Line, p.curToken.Column, msg,
			"检查是否缺少表达式或语句不完整")
		return &ast.ErrorExpression{
			Token:   p.curToken,
			Message: msg,
		}
	}

	def, ok := p.operatorDefs[p.curToken.Type]
	if !ok || def.PrefixFn == nil {
		p.noPrefixParseFnError(p.curToken.Type)
		// 返回错误占位表达式，保持 AST 完整性
		// 不返回 nil，避免上层 isNilStatement 误判导致级联跳过
		return &ast.ErrorExpression{
			Token:   p.curToken,
			Message: fmt.Sprintf("无法解析词法单元 %s ('%s')", p.curToken.Type, p.curToken.Literal),
		}
	}
	leftExp := def.PrefixFn()
	if leftExp == nil {
		// 前缀函数内部失败（如 parseFunctionLiteral 返回 nil）
		return &ast.ErrorExpression{
			Token:   p.curToken,
			Message: fmt.Sprintf("表达式解析失败: %s ('%s')", p.curToken.Type, p.curToken.Literal),
		}
	}

	for !p.peekTokenIs(token.SEMICOLON) && precedence < p.peekPrecedence() {
		nextDef, ok := p.operatorDefs[p.peekToken.Type]
		if !ok || nextDef.InfixFn == nil {
			return leftExp
		}
		p.nextToken()
		leftExp = nextDef.InfixFn(leftExp)
	}
	return leftExp
}

func (p *Parser) noPrefixParseFnError(t token.TokenType) {
	msg := fmt.Sprintf("无法解析词法单元 %s ('%s')", t, p.curToken.Literal)
	// 活字排版：根据 token 类型给出上下文感知的精准建议
	suggestion := "检查语法是否正确，或该位置是否出现了不期望的标记"
	switch t {
	case token.RBRACE:
		suggestion = "可能缺少返回值或表达式，或上一个语句未正确结束"
	case token.RPAREN:
		suggestion = "可能函数调用的参数不完整"
	case token.RBRACKET:
		suggestion = "可能索引表达式不完整"
	case token.EOF:
		suggestion = "代码可能不完整，缺少闭合的 } 或 )"
	case token.CATCH:
		suggestion = "'遇到' 只能在 执行 { ... } 之后使用"
	case token.ELSE:
		suggestion = "'否则' 必须跟在 如果 { ... } 之后"
	}
	p.addRootError(diagnostic.ErrNoPrefixParseFn.Code, p.curToken.Line, p.curToken.Column, msg, suggestion)
}

// isNilStatement 检测 typed-nil 语句（Go 接口 nil 陷阱）
func (p *Parser) isNilStatement(stmt ast.Statement) bool {
	switch s := stmt.(type) {
	case *ast.LetStatement:
		return s == nil
	case *ast.ReturnStatement:
		return s == nil
	case *ast.WhileStatement:
		return s == nil
	case *ast.ForStatement:
		return s == nil
	case *ast.ImportStatement:
		return s == nil
	case *ast.DefineStatement:
		return s == nil
	case *ast.TryCatchStatement:
		return s == nil
	case *ast.BreakStatement:
		return s == nil
	case *ast.ContinueStatement:
		return s == nil
	case *ast.ExpressionStatement:
		return s == nil
	default:
		return false
	}
}

// --- 前缀解析函数 ---

func (p *Parser) parseIdentifier() ast.Expression {
	return &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}
}

func (p *Parser) parseIntegerLiteral() ast.Expression {
	lit := &ast.IntegerLiteral{Token: p.curToken}

	value, err := strconv.ParseInt(p.curToken.Literal, 10, 64)
	if err != nil {
		msg := fmt.Sprintf("无法将 %q 解析为整数", p.curToken.Literal)
		p.addRootError(diagnostic.ErrInvalidInteger.Code, p.curToken.Line, p.curToken.Column, msg, "检查数字格式是否正确")
		return nil
	}
	lit.Value = value
	return lit
}

func (p *Parser) parseFloatLiteral() ast.Expression {
	lit := &ast.FloatLiteral{Token: p.curToken}

	value, err := strconv.ParseFloat(p.curToken.Literal, 64)
	if err != nil {
		msg := fmt.Sprintf("无法将 %q 解析为浮点数", p.curToken.Literal)
		p.addRootError(diagnostic.ErrInvalidFloat.Code, p.curToken.Line, p.curToken.Column, msg, "检查浮点数格式是否正确")
		return nil
	}
	lit.Value = value
	return lit
}

func (p *Parser) parseStringLiteral() ast.Expression {
	return &ast.StringLiteral{Token: p.curToken, Value: p.curToken.Literal}
}

// parseFStringLiteral 解析 f-字符串: f"Hello {name}, count={count}"
// 词法层已将 f"..." 的内容（去除引号、处理转义后）存入 Literal。
// 此处扫描 {...} 段，对每段用子 lexer+parser 解析为表达式，其余作为字面文本。
// 支持 {{ → { 和 }} → }} 转义（Python 风格）。
func (p *Parser) parseFStringLiteral() ast.Expression {
	raw := p.curToken.Literal
	fsl := &ast.FStringLiteral{Token: p.curToken}

	var textBuf strings.Builder
	i := 0
	for i < len(raw) {
		// {{ → 字面 {
		if i+1 < len(raw) && raw[i] == '{' && raw[i+1] == '{' {
			textBuf.WriteByte('{')
			i += 2
			continue
		}
		// }} → 字面 }
		if i+1 < len(raw) && raw[i] == '}' && raw[i+1] == '}' {
			textBuf.WriteByte('}')
			i += 2
			continue
		}
		// { → 表达式插值开始
		if raw[i] == '{' {
			// 刷出已积累的字面文本
			if textBuf.Len() > 0 {
				fsl.Parts = append(fsl.Parts, &ast.StringLiteral{Token: p.curToken, Value: textBuf.String()})
				textBuf.Reset()
			}
			// 用花括号深度查找匹配的 }
			depth := 1
			j := i + 1
			for j < len(raw) && depth > 0 {
				if raw[j] == '{' {
					depth++
				} else if raw[j] == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
				j++
			}
			if depth != 0 {
				p.addRootError(diagnostic.ErrNoPrefixParseFn.Code, p.curToken.Line, p.curToken.Column,
					"f-字符串中花括号未闭合", "检查 f\"...{ 表达式 }...\" 中的花括号是否匹配")
				break
			}
			exprStr := raw[i+1 : j]
			// 用子 lexer+parser 解析表达式
			subLexer := lexer.New(exprStr)
			subParser := New(subLexer)
			expr := subParser.parseExpression(_LOWEST)
			if expr != nil {
				fsl.Parts = append(fsl.Parts, expr)
			}
			i = j + 1
			continue
		}
		textBuf.WriteByte(raw[i])
		i++
	}
	// 刷出剩余字面文本
	if textBuf.Len() > 0 {
		fsl.Parts = append(fsl.Parts, &ast.StringLiteral{Token: p.curToken, Value: textBuf.String()})
	}
	return fsl
}

func (p *Parser) parseBooleanLiteral() ast.Expression {
	return &ast.BooleanLiteral{Token: p.curToken, Value: p.curTokenIs(token.TRUE)}
}

func (p *Parser) parseNullLiteral() ast.Expression {
	return &ast.NullLiteral{Token: p.curToken}
}

func (p *Parser) parsePrefixExpression() ast.Expression {
	expr := &ast.PrefixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
	}
	p.nextToken()
	expr.Right = p.parseExpression(_PREFIX)
	return expr
}

func (p *Parser) parseGroupedExpression() ast.Expression {
	p.nextToken()
	exp := p.parseExpression(_LOWEST)
	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	return exp
}

func (p *Parser) parseIfExpression() ast.Expression {
	expr := &ast.IfExpression{Token: p.curToken}
	p.nextToken()

	expr.Condition = p.parseExpression(_LOWEST)

	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	expr.Consequence = p.parseBlockStatement()

	if p.peekTokenIs(token.ELSE) {
		p.nextToken()
		// 支持 否则 如果 链式条件
		if p.peekTokenIs(token.IF) {
			p.nextToken()
			nestedIf := p.parseIfExpression()
			expr.Alternative = &ast.BlockStatement{
				Token: p.curToken,
				Statements: []ast.Statement{
					&ast.ExpressionStatement{Token: p.curToken, Expression: nestedIf},
				},
			}
		} else {
			if !p.expectPeek(token.LBRACE) {
				return nil
			}
			expr.Alternative = p.parseBlockStatement()
		}
	}
	return expr
}

func (p *Parser) parseFunctionLiteral() ast.Expression {
	lit := &ast.FunctionLiteral{Token: p.curToken}

	// 可选的函数名: 函数 名称(参数) { ... }
	if p.peekTokenIs(token.IDENT) {
		p.nextToken()
		lit.Name = p.curToken.Literal
	}

	if !p.expectPeek(token.LPAREN) {
		return nil
	}
	lit.Parameters = p.parseFunctionParameters()
	if lit.Parameters == nil {
		return nil // 参数解析已报错，不再级联
	}

	// 可选的返回类型注解: 函数 名称(参数) -> 整数 { ... }
	if p.peekTokenIs(token.ARROW) {
		p.nextToken() // 消费 '->'
		p.nextToken() // 移到类型名
		if p.curToken.Type != token.IDENT {
			p.addRootError(diagnostic.ErrUnexpectedToken.Code, p.curToken.Line, p.curToken.Column, fmt.Sprintf("返回类型注解必须是标识符，得到 %s", p.curToken.Type), "")
			return nil
		}
		lit.RetTypeAnnot = p.curToken.Literal
	}

	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	lit.Body = p.parseBlockStatement()
	return lit
}

func (p *Parser) parseFunctionParameters() []*ast.Identifier {
	identifiers := []*ast.Identifier{}

	if p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		return identifiers
	}
	p.nextToken()

	ident := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

	// 可选参数类型注解: 参数名: 类型
	if p.peekTokenIs(token.COLON) {
		p.nextToken() // 消费 ':'
		p.nextToken() // 移到类型名
		if p.curToken.Type != token.IDENT {
			p.addRootError(diagnostic.ErrUnexpectedToken.Code, p.curToken.Line, p.curToken.Column, fmt.Sprintf("参数类型注解必须是标识符，得到 %s", p.curToken.Type), "")
			return nil
		}
		ident.TypeAnnot = p.curToken.Literal
	}
	identifiers = append(identifiers, ident)

	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		ident := &ast.Identifier{Token: p.curToken, Value: p.curToken.Literal}

		// 可选参数类型注解
		if p.peekTokenIs(token.COLON) {
			p.nextToken() // 消费 ':'
			p.nextToken() // 移到类型名
			if p.curToken.Type != token.IDENT {
				p.addRootError(diagnostic.ErrUnexpectedToken.Code, p.curToken.Line, p.curToken.Column, fmt.Sprintf("参数类型注解必须是标识符，得到 %s", p.curToken.Type), "")
				return nil
			}
			ident.TypeAnnot = p.curToken.Literal
		}
		identifiers = append(identifiers, ident)
	}

	if !p.expectPeek(token.RPAREN) {
		return nil
	}
	return identifiers
}

// --- 中缀解析函数 ---

func (p *Parser) parseInfixExpression(left ast.Expression) ast.Expression {
	expr := &ast.InfixExpression{
		Token:    p.curToken,
		Operator: p.curToken.Literal,
		Left:     left,
	}
	precedence := p.curPrecedence()
	p.nextToken()
	expr.Right = p.parseExpression(precedence)
	return expr
}

func (p *Parser) parseAssignExpression(left ast.Expression) ast.Expression {
	tok := p.curToken
	p.nextToken()
	val := p.parseExpression(_ASSIGN)

	switch target := left.(type) {
	case *ast.Identifier:
		return &ast.AssignExpression{Token: tok, Name: target, Value: val}
	case *ast.IndexExpression:
		return &ast.AssignExpression{Token: tok, IndexTarget: target, Value: val}
	default:
		msg := "赋值目标必须是标识符或索引"
		p.addRootError(diagnostic.ErrInvalidAssignTarget.Code, tok.Line, tok.Column, msg, "赋值左侧只能是变量名或数组/哈希索引")
		return left
	}
}

func (p *Parser) parseCallExpression(fn ast.Expression) ast.Expression {
	exp := &ast.CallExpression{Token: p.curToken, Function: fn}
	exp.Arguments, exp.NamedArgs = p.parseCallArguments()
	return exp
}

func (p *Parser) parseCallArguments() ([]ast.Expression, []ast.NamedArg) {
	args := []ast.Expression{}
	namedArgs := []ast.NamedArg{}

	if p.peekTokenIs(token.RPAREN) {
		p.nextToken()
		return args, namedArgs
	}
	p.nextToken()

	// 检查具名参数: IDENT COLON
	if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.COLON) {
		name := p.curToken.Literal
		p.nextToken() // 消耗 IDENT
		p.nextToken() // 消耗 :
		val := p.parseExpression(_LOWEST)
		namedArgs = append(namedArgs, ast.NamedArg{Name: name, Value: val})
	} else {
		args = append(args, p.parseExpression(_LOWEST))
	}

	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		if p.curTokenIs(token.IDENT) && p.peekTokenIs(token.COLON) {
			name := p.curToken.Literal
			p.nextToken() // 消耗 IDENT
			p.nextToken() // 消耗 :
			val := p.parseExpression(_LOWEST)
			namedArgs = append(namedArgs, ast.NamedArg{Name: name, Value: val})
		} else {
			args = append(args, p.parseExpression(_LOWEST))
		}
	}

	if !p.expectPeek(token.RPAREN) {
		return nil, nil
	}
	return args, namedArgs
}

func (p *Parser) parseArrayLiteral() ast.Expression {
	array := &ast.ArrayLiteral{Token: p.curToken}

	if p.peekTokenIs(token.RBRACKET) {
		p.nextToken()
		return array
	}
	p.nextToken()
	array.Elements = append(array.Elements, p.parseExpression(_LOWEST))

	for p.peekTokenIs(token.COMMA) {
		p.nextToken()
		p.nextToken()
		array.Elements = append(array.Elements, p.parseExpression(_LOWEST))
	}

	if !p.expectPeek(token.RBRACKET) {
		return nil
	}
	return array
}

func (p *Parser) parseHashLiteral() ast.Expression {
	hash := &ast.HashLiteral{Token: p.curToken}
	hash.Pairs = make(map[ast.Expression]ast.Expression)

	for !p.peekTokenIs(token.RBRACE) {
		p.nextToken()
		key := p.parseExpression(_LOWEST)

		if !p.expectPeek(token.COLON) {
			return nil
		}
		p.nextToken()

		value := p.parseExpression(_LOWEST)
		hash.Pairs[key] = value

		if !p.peekTokenIs(token.RBRACE) && !p.expectPeek(token.COMMA) {
			return nil
		}
	}

	if !p.expectPeek(token.RBRACE) {
		return nil
	}
	return hash
}

func (p *Parser) parseIndexExpression(left ast.Expression) ast.Expression {
	exp := &ast.IndexExpression{Token: p.curToken, Left: left}
	p.nextToken()
	exp.Index = p.parseExpression(_LOWEST)

	if !p.expectPeek(token.RBRACKET) {
		return nil
	}
	return exp
}

// parseMemberExpression 解析点号成员访问: expr.member
// 作为语法糖，转换为 IndexExpression{Left: expr, Index: StringLiteral{Value: "member"}}
// 使 模块.函数(参数) 等价于 模块["函数"](参数)
func (p *Parser) parseMemberExpression(left ast.Expression) ast.Expression {
	dotTok := p.curToken // .
	p.nextToken()

	if !p.curTokenIs(token.IDENT) {
		p.addRootError(diagnostic.ErrExpectedToken.Code, p.curToken.Line, p.curToken.Column,
			"点号后期望标识符（成员名），但得到 "+string(p.curToken.Type),
			"使用 模块.成员名 语法访问模块成员")
		return nil
	}

	memberName := p.curToken.Literal
	return &ast.IndexExpression{
		Token: dotTok,
		Left:  left,
		Index: &ast.StringLiteral{Token: p.curToken, Value: memberName},
	}
}

// tokenName 返回 token 类型的可读名称（用于错误提示）
func tokenName(t token.TokenType) string {
	names := map[token.TokenType]string{
		token.RPAREN:    "右括号 ')'",
		token.LPAREN:    "左括号 '('",
		token.RBRACE:    "右花括号 '}'",
		token.LBRACE:    "左花括号 '{'",
		token.RBRACKET:  "右方括号 ']'",
		token.LBRACKET:  "左方括号 '['",
		token.SEMICOLON: "分号 ';'",
		token.COMMA:     "逗号 ','",
		token.COLON:     "冒号 ':'",
		token.ASSIGN:    "等号 '='",
	}
	if name, ok := names[t]; ok {
		return name
	}
	return string(t)
}

// ==================== 现代语言特性解析 ====================

// parseDefineStatement 解析定义语句: 定义 名称 { 字段 = 默认值 ... }
func (p *Parser) parseDefineStatement() ast.Statement {
	stmt := &ast.DefineStatement{Token: p.curToken}

	if !p.expectPeek(token.IDENT) {
		return nil
	}
	stmt.Name = p.curToken.Literal

	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	p.nextToken() // 移动到第一个字段

	for !p.curTokenIs(token.RBRACE) && !p.curTokenIs(token.EOF) {
		if p.curTokenIs(token.SEMICOLON) {
			p.nextToken()
			continue
		}

		if !p.curTokenIs(token.IDENT) {
			p.addRootError(diagnostic.ErrExpectedToken.Code, p.curToken.Line, p.curToken.Column,
				"定义体中期望字段名", "使用 字段名 = 默认值 语法")
			break
		}
		fieldName := p.curToken.Literal

		if !p.expectPeek(token.ASSIGN) {
			return nil
		}
		p.nextToken() // 移动到值表达式

		val := p.parseExpression(_LOWEST)
		stmt.Fields = append(stmt.Fields, ast.DefineField{Name: fieldName, Value: val})

		if p.peekTokenIs(token.SEMICOLON) {
			p.nextToken()
		}
		p.nextToken() // 移动到下一个字段或 }
	}

	return stmt
}

// parseTryCatchStatement 解析执行/遇到语句: 执行 { ... } 遇到 err { ... }
func (p *Parser) parseTryCatchStatement() ast.Statement {
	stmt := &ast.TryCatchStatement{Token: p.curToken}

	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStatement()

	if !p.peekTokenIs(token.CATCH) {
		p.addRootError(diagnostic.ErrExpectedToken.Code, p.curToken.Line, p.curToken.Column,
			"执行块后期望 '遇到' 关键字", "使用 执行 { ... } 遇到 err { ... } 语法")
		return nil
	}
	p.nextToken() // 消耗 }，移动到 遇到
	p.nextToken() // 消耗 遇到，移动到错误变量名

	if !p.curTokenIs(token.IDENT) {
		p.addRootError(diagnostic.ErrExpectedToken.Code, p.curToken.Line, p.curToken.Column,
			"'遇到' 后期望错误变量名", "使用 遇到 err { ... } 语法")
		return nil
	}
	stmt.ErrVar = p.curToken.Literal

	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	stmt.Handler = p.parseBlockStatement()

	return stmt
}

// parseForInRest 解析 for-in 循环剩余部分: 可迭代表达式 [到 ...] [满足 ...] { body }
// 调用时 p.curToken 已经定位到可迭代表达式的第一个 token（在 已由调用方消耗）
func (p *Parser) parseForInRest(varName, indexName string) ast.Statement {
	stmt := &ast.ForInStatement{
		Token:     p.curToken,
		VarName:   varName,
		IndexName: indexName,
	}

	stmt.Iterable = p.parseExpression(_LOWEST)

	// 检查范围: 到
	if p.peekTokenIs(token.TO) {
		p.nextToken() // 消耗可迭代表达式
		p.nextToken() // 消耗 到，移动到结束值
		stmt.EndExpr = p.parseExpression(_LOWEST)
	}

	// 检查条件过滤: 满足
	if p.peekTokenIs(token.WHEN) {
		p.nextToken() // 消耗当前表达式
		p.nextToken() // 消耗 满足，移动到条件
		stmt.Guard = p.parseExpression(_LOWEST)
	}

	if !p.expectPeek(token.LBRACE) {
		return nil
	}
	stmt.Body = p.parseBlockStatement()

	return stmt
}

// parseBreakStatement 解析跳出语句
func (p *Parser) parseBreakStatement() ast.Statement {
	stmt := &ast.BreakStatement{Token: p.curToken}
	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}

// parseContinueStatement 解析跳过语句
func (p *Parser) parseContinueStatement() ast.Statement {
	stmt := &ast.ContinueStatement{Token: p.curToken}
	if p.peekTokenIs(token.SEMICOLON) {
		p.nextToken()
	}
	return stmt
}
