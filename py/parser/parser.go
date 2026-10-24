package parser

import (
	"fmt"
	"strconv"
	"strings"

	"zuzo/py/ast"
	"zuzo/py/lexer"
	"zuzo/py/token"
)

// Parser Python 语法分析器（递归下降 + 优先级分层）
type Parser struct {
	toks []token.Token
	pos  int
	errs []string
}

// Parse 解析源码为模块 AST
func Parse(src string) (*ast.Module, []string) {
	l := lexer.New(src)
	toks := l.Tokens()
	p := &Parser{toks: toks}
	errs := append([]string{}, l.Errors()...)
	m := &ast.Module{}
	for !p.at(token.EOF) {
		if p.at(token.NEWLINE) {
			p.next()
			continue
		}
		if p.at(token.INDENT) {
			p.errf("意外的缩进")
			p.next()
			continue
		}
		if p.at(token.DEDENT) {
			p.next()
			continue
		}
		st := p.parseStatement()
		if st != nil {
			m.Body = append(m.Body, st)
		} else {
			p.synchronize()
		}
	}
	errs = append(errs, p.errs...)
	return m, errs
}

// ParseExpr 解析单个表达式（供 REPL / eval 使用）
func ParseExpr(src string) (ast.Expr, []string) {
	l := lexer.New(src)
	toks := l.Tokens()
	p := &Parser{toks: toks}
	e := p.parseTestList()
	if p.errs == nil && !p.at(token.EOF) && !p.at(token.NEWLINE) {
		p.errf("意外的记号 %q", p.cur().Literal)
	}
	return e, append(l.Errors(), p.errs...)
}

// ---------------- 基础 ----------------

func (p *Parser) cur() token.Token {
	if p.pos >= len(p.toks) {
		return token.Token{Type: token.EOF}
	}
	return p.toks[p.pos]
}

func (p *Parser) peekN(n int) token.Token {
	i := p.pos + n
	if i >= len(p.toks) {
		return token.Token{Type: token.EOF}
	}
	return p.toks[i]
}

func (p *Parser) next() token.Token {
	t := p.cur()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *Parser) at(t token.Type) bool { return p.cur().Type == t }

func (p *Parser) accept(t token.Type) bool {
	if p.at(t) {
		p.pos++
		return true
	}
	return false
}

func (p *Parser) expect(t token.Type) token.Token {
	if p.at(t) {
		return p.next()
	}
	p.errf("期望 %q，实际 %q", string(t), p.cur().Literal)
	return p.cur()
}

func (p *Parser) errf(format string, a ...interface{}) {
	msg := fmt.Sprintf(format, a...)
	p.errs = append(p.errs, fmt.Sprintf("第 %d 行: %s", p.cur().Line, msg))
}

func (p *Parser) synchronize() {
	for !p.at(token.EOF) {
		if p.at(token.NEWLINE) {
			p.next()
			return
		}
		p.next()
	}
}

func (p *Parser) skipNewlines() {
	for p.at(token.NEWLINE) {
		p.next()
	}
}

// ---------------- 语句 ----------------

func (p *Parser) parseStatement() ast.Stmt {
	if p.isMatchStmt() {
		return p.parseMatch()
	}
	switch p.cur().Type {
	case token.KW_IF:
		return p.parseIf()
	case token.KW_WHILE:
		return p.parseWhile()
	case token.KW_FOR:
		return p.parseFor(false)
	case token.KW_TRY:
		return p.parseTry()
	case token.KW_WITH:
		return p.parseWith(false)
	case token.KW_DEF:
		return p.parseFuncDef(nil, false)
	case token.KW_CLASS:
		return p.parseClassDef(nil)
	case token.AT:
		return p.parseDecorated()
	case token.KW_ASYNC:
		return p.parseAsync()
	}
	return p.parseSimpleStatement()
}

func (p *Parser) parseAsync() ast.Stmt {
	p.next() // async
	switch p.cur().Type {
	case token.KW_DEF:
		return p.parseFuncDef(nil, true)
	case token.KW_FOR:
		return p.parseFor(true)
	case token.KW_WITH:
		return p.parseWith(true)
	}
	p.errf("async 后应为 def / for / with")
	return nil
}

func (p *Parser) parseDecorated() ast.Stmt {
	var decs []ast.Expr
	for p.at(token.AT) {
		p.next()
		decs = append(decs, p.parseTestListNoTuple())
		p.accept(token.NEWLINE)
	}
	isAsync := false
	if p.at(token.KW_ASYNC) {
		p.next()
		isAsync = true
	}
	if p.at(token.KW_DEF) {
		return p.parseFuncDef(decs, isAsync)
	}
	if p.at(token.KW_CLASS) {
		return p.parseClassDef(decs)
	}
	p.errf("装饰器后应为 def 或 class")
	return nil
}

// parseSuite 解析语句块： 冒号后换行缩进块 / 或单行简单语句
func (p *Parser) parseSuite() []ast.Stmt {
	p.expect(token.COLON)
	if p.at(token.NEWLINE) {
		p.next()
		p.skipNewlines()
		if !p.at(token.INDENT) {
			p.errf("期望缩进块")
			return nil
		}
		p.next()
		var body []ast.Stmt
		for !p.at(token.DEDENT) && !p.at(token.EOF) {
			if p.at(token.NEWLINE) {
				p.next()
				continue
			}
			st := p.parseStatement()
			if st != nil {
				body = append(body, st)
			} else {
				p.synchronize()
			}
		}
		p.accept(token.DEDENT)
		return body
	}
	// 单行形式: if x: a = 1; b = 2
	var body []ast.Stmt
	for {
		st := p.parseSimpleStatement()
		if st == nil {
			break
		}
		body = append(body, st)
		if p.accept(token.SEMI) {
			if p.at(token.NEWLINE) || p.at(token.EOF) {
				break
			}
			continue
		}
		break
	}
	p.accept(token.NEWLINE)
	return body
}

func (p *Parser) parseIf() ast.Stmt {
	line := p.cur().Line
	p.next() // if
	cond := p.parseTestListNoTuple()
	body := p.parseSuite()
	node := &ast.If{Pos: ast.Pos{L: line}, Cond: cond, Body: body}
	if p.at(token.KW_ELIF) {
		// elif 等价于 else: if ...
		p.toks[p.pos].Type = token.KW_IF
		node.Orelse = []ast.Stmt{p.parseIf()}
	} else if p.at(token.KW_ELSE) {
		p.next()
		node.Orelse = p.parseSuite()
	}
	return node
}

func (p *Parser) parseWhile() ast.Stmt {
	line := p.cur().Line
	p.next()
	cond := p.parseTestListNoTuple()
	body := p.parseSuite()
	var orelse []ast.Stmt
	if p.at(token.KW_ELSE) {
		p.next()
		orelse = p.parseSuite()
	}
	return &ast.While{Pos: ast.Pos{L: line}, Cond: cond, Body: body, Orelse: orelse}
}

func (p *Parser) parseFor(isAsync bool) ast.Stmt {
	line := p.cur().Line
	p.next() // for
	target := p.parseTargetList()
	p.expect(token.KW_IN)
	iter := p.parseTestListNoTuple()
	body := p.parseSuite()
	var orelse []ast.Stmt
	if p.at(token.KW_ELSE) {
		p.next()
		orelse = p.parseSuite()
	}
	return &ast.For{Pos: ast.Pos{L: line}, Target: target, Iter: iter, Body: body, Orelse: orelse, IsAsync: isAsync}
}

func (p *Parser) parseTry() ast.Stmt {
	line := p.cur().Line
	p.next()
	body := p.parseSuite()
	node := &ast.Try{Pos: ast.Pos{L: line}, Body: body}
	for p.at(token.KW_EXCEPT) {
		hline := p.cur().Line
		p.next()
		h := &ast.ExceptHandler{Pos: ast.Pos{L: hline}}
		if !p.at(token.COLON) {
			h.Type = p.parseTestListNoTuple()
			if p.at(token.KW_AS) {
				p.next()
				h.Name = p.expect(token.NAME).Literal
			}
		}
		h.Body = p.parseSuite()
		node.Handlers = append(node.Handlers, h)
	}
	if p.at(token.KW_ELSE) {
		p.next()
		node.Orelse = p.parseSuite()
	}
	if p.at(token.KW_FINALLY) {
		p.next()
		node.Finally = p.parseSuite()
	}
	if node.Handlers == nil && node.Finally == nil {
		p.errf("try 语句缺少 except 或 finally")
	}
	return node
}

func (p *Parser) parseWith(isAsync bool) ast.Stmt {
	line := p.cur().Line
	p.next()
	var items []ast.WithItem
	for {
		ctx := p.parseTestListNoTuple()
		var vars ast.Expr
		if p.at(token.KW_AS) {
			p.next()
			vars = p.parseTarget()
		}
		items = append(items, ast.WithItem{Context: ctx, Vars: vars})
		if !p.accept(token.COMMA) {
			break
		}
		if p.at(token.NEWLINE) {
			break
		}
	}
	body := p.parseSuite()
	return &ast.With{Pos: ast.Pos{L: line}, Items: items, Body: body, IsAsync: isAsync}
}

func (p *Parser) parseFuncDef(decs []ast.Expr, isAsync bool) ast.Stmt {
	line := p.cur().Line
	p.next() // def
	name := p.expect(token.NAME).Literal
	params := p.parseParams()
	var returns ast.Expr
	if p.at(token.ARROW) {
		p.next()
		returns = p.parseTestListNoTuple()
	}
	body := p.parseSuite()
	return &ast.FuncDef{Pos: ast.Pos{L: line}, Name: name, Params: params, Body: body, Decorators: decs, Returns: returns, IsAsync: isAsync}
}

func (p *Parser) parseClassDef(decs []ast.Expr) ast.Stmt {
	line := p.cur().Line
	p.next() // class
	name := p.expect(token.NAME).Literal
	node := &ast.ClassDef{Pos: ast.Pos{L: line}, Name: name, Decorators: decs}
	if p.accept(token.LPAREN) {
		for !p.at(token.RPAREN) && !p.at(token.EOF) {
			if p.at(token.DOUBLESTAR) {
				p.next()
				v := p.parseTestListNoTuple()
				node.Keywords = append(node.Keywords, ast.Keyword{Name: "", Value: v})
			} else {
				e := p.parseTestListNoTuple()
				if p.at(token.ASSIGN) {
					p.next()
					v := p.parseTestListNoTuple()
					kw := ast.Keyword{Value: v}
					if n, ok := e.(*ast.Name); ok {
						kw.Name = n.Id
					}
					node.Keywords = append(node.Keywords, kw)
				} else {
					node.Bases = append(node.Bases, e)
				}
			}
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.expect(token.RPAREN)
	}
	node.Body = p.parseSuite()
	return node
}

// parseParams 解析函数参数表（含默认值、*args、仅关键字、**kwargs）
func (p *Parser) parseParams() *ast.Params {
	p.expect(token.LPAREN)
	ps := &ast.Params{}
	kwonly := false
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		if p.at(token.SLASH) {
			// 位置-only 分隔符，语义上忽略（已按普通参数处理）
			p.next()
			if p.accept(token.COMMA) {
				continue
			}
			break
		}
		if p.at(token.DOUBLESTAR) {
			p.next()
			ps.Kwarg = p.expect(token.NAME).Literal
		} else if p.at(token.STAR) {
			p.next()
			kwonly = true
			if p.at(token.NAME) {
				ps.Vararg = p.next().Literal
			}
		} else {
			arg := &ast.Arg{Name: p.expect(token.NAME).Literal}
			if p.at(token.COLON) {
				p.next()
				arg.Annotation = p.parseTestListNoTuple()
			}
			if p.at(token.ASSIGN) {
				p.next()
				arg.Default = p.parseTestListNoTuple()
			}
			if kwonly {
				ps.Kwonly = append(ps.Kwonly, arg)
			} else {
				ps.Args = append(ps.Args, arg)
			}
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RPAREN)
	return ps
}

// parseTarget 解析赋值目标
func (p *Parser) parseTarget() ast.Expr {
	if p.at(token.LPAREN) || p.at(token.LBRACKET) {
		open := p.next().Type
		closer := token.RPAREN
		if open == token.LBRACKET {
			closer = token.RBRACKET
		}
		var elems []ast.Expr
		for !p.at(closer) && !p.at(token.EOF) {
			elems = append(elems, p.parseTarget())
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.expect(closer)
		return makeTupleOrSingle(elems, p.cur().Line)
	}
	return p.parseUnaryPostfix()
}

// parseTargetList 解析 for 的目标列表
func (p *Parser) parseTargetList() ast.Expr {
	var elems []ast.Expr
	for {
		elems = append(elems, p.parseTarget())
		if !p.accept(token.COMMA) {
			break
		}
		if p.at(token.KW_IN) || p.at(token.COLON) {
			break
		}
	}
	return makeTupleOrSingle(elems, p.cur().Line)
}

func makeTupleOrSingle(elems []ast.Expr, line int) ast.Expr {
	if len(elems) == 1 {
		return elems[0]
	}
	return &ast.Tuple{Pos: ast.Pos{L: line}, Elements: elems}
}

// parseSimpleStatement 解析简单语句（不含复合语句）
func (p *Parser) parseSimpleStatement() ast.Stmt {
	line := p.cur().Line
	switch p.cur().Type {
	case token.KW_PASS:
		p.next()
		p.endSimple()
		return &ast.Pass{Pos: ast.Pos{L: line}}
	case token.KW_BREAK:
		p.next()
		p.endSimple()
		return &ast.Break{Pos: ast.Pos{L: line}}
	case token.KW_CONTINUE:
		p.next()
		p.endSimple()
		return &ast.Continue{Pos: ast.Pos{L: line}}
	case token.KW_RETURN:
		p.next()
		var v ast.Expr
		if !p.at(token.NEWLINE) && !p.at(token.EOF) && !p.at(token.SEMI) {
			v = p.parseTestList()
		}
		p.endSimple()
		return &ast.Return{Pos: ast.Pos{L: line}, Value: v}
	case token.KW_RAISE:
		p.next()
		var exc, cause ast.Expr
		if !p.at(token.NEWLINE) && !p.at(token.EOF) {
			exc = p.parseTestListNoTuple()
			if p.at(token.KW_FROM) {
				p.next()
				cause = p.parseTestListNoTuple()
			}
		}
		p.endSimple()
		return &ast.Raise{Pos: ast.Pos{L: line}, Exc: exc, Cause: cause}
	case token.KW_IMPORT:
		return p.parseImport()
	case token.KW_FROM:
		return p.parseImportFrom()
	case token.KW_GLOBAL:
		p.next()
		var names []string
		for {
			names = append(names, p.expect(token.NAME).Literal)
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.endSimple()
		return &ast.Global{Pos: ast.Pos{L: line}, Names: names}
	case token.KW_NONLOCAL:
		p.next()
		var names []string
		for {
			names = append(names, p.expect(token.NAME).Literal)
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.endSimple()
		return &ast.Nonlocal{Pos: ast.Pos{L: line}, Names: names}
	case token.KW_DEL:
		p.next()
		var targets []ast.Expr
		for {
			targets = append(targets, p.parseTarget())
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.endSimple()
		return &ast.Del{Pos: ast.Pos{L: line}, Targets: targets}
	case token.KW_ASSERT:
		p.next()
		test := p.parseTestListNoTuple()
		var msg ast.Expr
		if p.accept(token.COMMA) {
			msg = p.parseTestListNoTuple()
		}
		p.endSimple()
		return &ast.Assert{Pos: ast.Pos{L: line}, Test: test, Msg: msg}
	}

	// 赋值 / 表达式语句
	first := p.parseTestList()
	switch p.cur().Type {
	case token.COLON:
		// 注解赋值
		p.next()
		ann := p.parseTestListNoTuple()
		var val ast.Expr
		if p.accept(token.ASSIGN) {
			val = p.parseTestList()
		}
		p.endSimple()
		return &ast.AnnAssign{Pos: ast.Pos{L: line}, Target: first, Annotation: ann, Value: val}
	case token.ASSIGN:
		targets := []ast.Expr{first}
		var value ast.Expr
		for {
			p.next() // =
			if p.at(token.KW_YIELD) {
				value = p.parseYield()
			} else {
				value = p.parseTestList()
			}
			if p.at(token.ASSIGN) {
				targets = append(targets, value)
				continue
			}
			break
		}
		p.endSimple()
		return &ast.Assign{Pos: ast.Pos{L: line}, Targets: targets, Value: value}
	case token.PLUSEQ, token.MINUSEQ, token.STAREQ, token.SLASHEQ, token.DOUBLESLASHEQ,
		token.PERCENTEQ, token.DOUBLESTAREQ, token.AMP_EQ, token.PIPE_EQ, token.CARET_EQ,
		token.LSHIFTEQ, token.RSHIFTEQ, token.ATEQ:
		op := strings.TrimSuffix(p.next().Literal, "=")
		value := p.parseTestList()
		p.endSimple()
		return &ast.AugAssign{Pos: ast.Pos{L: line}, Target: first, Op: op, Value: value}
	}
	p.endSimple()
	return &ast.ExprStmt{Pos: ast.Pos{L: line}, X: first}
}

func (p *Parser) endSimple() {
	if p.accept(token.SEMI) {
		return
	}
	if p.at(token.NEWLINE) {
		p.next()
		return
	}
	if p.at(token.EOF) {
		return
	}
	// 允许后面紧跟 ; 或换行；否则报错
	if p.at(token.DEDENT) {
		return
	}
	p.errf("简单语句后出现意外记号 %q", p.cur().Literal)
}

func (p *Parser) parseImport() ast.Stmt {
	line := p.cur().Line
	p.next()
	node := &ast.Import{Pos: ast.Pos{L: line}}
	for {
		name := p.expect(token.NAME).Literal
		for p.at(token.DOT) {
			p.next()
			name += "." + p.expect(token.NAME).Literal
		}
		al := ast.Alias{Name: name}
		if p.at(token.KW_AS) {
			p.next()
			al.As = p.expect(token.NAME).Literal
		}
		node.Names = append(node.Names, al)
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.endSimple()
	return node
}

func (p *Parser) parseImportFrom() ast.Stmt {
	line := p.cur().Line
	p.next() // from
	node := &ast.ImportFrom{Pos: ast.Pos{L: line}}
	for p.at(token.DOT) {
		p.next()
		node.Level++
	}
	if p.at(token.NAME) {
		node.Module = p.next().Literal
		for p.at(token.DOT) {
			p.next()
			node.Module += "." + p.expect(token.NAME).Literal
		}
	}
	p.expect(token.KW_IMPORT)
	if p.at(token.STAR) {
		p.next()
		node.Names = []ast.Alias{{Name: "*"}}
	} else {
		paren := p.accept(token.LPAREN)
		for {
			name := p.expect(token.NAME).Literal
			al := ast.Alias{Name: name}
			if p.at(token.KW_AS) {
				p.next()
				al.As = p.expect(token.NAME).Literal
			}
			node.Names = append(node.Names, al)
			if !p.accept(token.COMMA) {
				break
			}
			if paren && p.at(token.RPAREN) {
				break
			}
		}
		if paren {
			p.expect(token.RPAREN)
		}
	}
	p.endSimple()
	return node
}

var _ = strconv.Itoa
