package parser

import (
	"strings"

	"zuzo/py/ast"
	"zuzo/py/lexer"
	"zuzo/py/token"
)

// parseTestList 解析表达式列表，逗号分隔时组成元组
func (p *Parser) parseTestList() ast.Expr {
	line := p.cur().Line
	first := p.parseTestListNoTuple()
	if !p.at(token.COMMA) {
		return first
	}
	elems := []ast.Expr{first}
	for p.accept(token.COMMA) {
		if p.at(token.NEWLINE) || p.at(token.EOF) || p.at(token.ASSIGN) ||
			p.at(token.RPAREN) || p.at(token.RBRACKET) || p.at(token.RBRACE) ||
			p.at(token.COLON) || p.at(token.SEMI) {
			break
		}
		elems = append(elems, p.parseTestListNoTuple())
	}
	return &ast.Tuple{Pos: ast.Pos{L: line}, Elements: elems}
}

// parseTestListNoTuple 解析单个表达式（可能是三元表达式 / 海象赋值）
func (p *Parser) parseTestListNoTuple() ast.Expr {
	line := p.cur().Line
	e := p.parseExpression()
	if p.at(token.COLONEQ) {
		p.next()
		v := p.parseExpression()
		if n, ok := e.(*ast.Name); ok {
			return &ast.NamedExpr{Pos: ast.Pos{L: line}, Target: n, Value: v}
		}
	}
	return e
}

// parseExpression 条件表达式: a if cond else b
func (p *Parser) parseExpression() ast.Expr {
	line := p.cur().Line
	if p.at(token.KW_LAMBDA) {
		return p.parseLambda()
	}
	e := p.parseOr()
	if p.at(token.KW_IF) {
		p.next()
		cond := p.parseOr()
		p.expect(token.KW_ELSE)
		alt := p.parseExpression()
		return &ast.IfExp{Pos: ast.Pos{L: line}, Test: cond, Body: e, Orelse: alt}
	}
	return e
}

func (p *Parser) parseOr() ast.Expr {
	line := p.cur().Line
	left := p.parseAnd()
	if !p.at(token.KW_OR) {
		return left
	}
	vals := []ast.Expr{left}
	for p.accept(token.KW_OR) {
		vals = append(vals, p.parseAnd())
	}
	return &ast.BoolOp{Pos: ast.Pos{L: line}, Op: "or", Values: vals}
}

func (p *Parser) parseAnd() ast.Expr {
	line := p.cur().Line
	left := p.parseNot()
	if !p.at(token.KW_AND) {
		return left
	}
	vals := []ast.Expr{left}
	for p.accept(token.KW_AND) {
		vals = append(vals, p.parseNot())
	}
	return &ast.BoolOp{Pos: ast.Pos{L: line}, Op: "and", Values: vals}
}

func (p *Parser) parseNot() ast.Expr {
	line := p.cur().Line
	if p.at(token.KW_NOT) {
		p.next()
		return &ast.UnaryOp{Pos: ast.Pos{L: line}, Op: "not", Operand: p.parseNot()}
	}
	return p.parseComparison()
}

var compOps = map[token.Type]string{
	token.LT: "<", token.GT: ">", token.LE: "<=", token.GE: ">=",
	token.EQ: "==", token.NE: "!=",
}

func (p *Parser) parseComparison() ast.Expr {
	line := p.cur().Line
	left := p.parseBitOr()
	var ops []string
	var comps []ast.Expr
	for {
		t := p.cur().Type
		if op, ok := compOps[t]; ok {
			p.next()
			ops = append(ops, op)
			comps = append(comps, p.parseBitOr())
			continue
		}
		if t == token.KW_IN {
			p.next()
			ops = append(ops, "in")
			comps = append(comps, p.parseBitOr())
			continue
		}
		if t == token.KW_IS {
			p.next()
			if p.at(token.KW_NOT) {
				p.next()
				ops = append(ops, "is not")
			} else {
				ops = append(ops, "is")
			}
			comps = append(comps, p.parseBitOr())
			continue
		}
		if t == token.KW_NOT && p.peekN(1).Type == token.KW_IN {
			p.next()
			p.next()
			ops = append(ops, "not in")
			comps = append(comps, p.parseBitOr())
			continue
		}
		break
	}
	if len(ops) == 0 {
		return left
	}
	return &ast.Compare{Pos: ast.Pos{L: line}, Left: left, Ops: ops, Comparators: comps}
}

func (p *Parser) parseBitOr() ast.Expr {
	return p.parseBinLevel(p.parseBitXor, token.PIPE, "|")
}

func (p *Parser) parseBitXor() ast.Expr {
	return p.parseBinLevel(p.parseBitAnd, token.CARET, "^")
}

func (p *Parser) parseBitAnd() ast.Expr {
	return p.parseBinLevel(p.parseShift, token.AMP, "&")
}

func (p *Parser) parseShift() ast.Expr {
	e := p.parseArith()
	for p.at(token.LSHIFT) || p.at(token.RSHIFT) {
		line := p.cur().Line
		op := p.next().Literal
		e = &ast.BinOp{Pos: ast.Pos{L: line}, Left: e, Op: op, Right: p.parseArith()}
	}
	return e
}

func (p *Parser) parseArith() ast.Expr {
	e := p.parseTerm()
	for p.at(token.PLUS) || p.at(token.MINUS) {
		line := p.cur().Line
		op := p.next().Literal
		e = &ast.BinOp{Pos: ast.Pos{L: line}, Left: e, Op: op, Right: p.parseTerm()}
	}
	return e
}

func (p *Parser) parseTerm() ast.Expr {
	e := p.parseFactor()
	for {
		t := p.cur().Type
		if t != token.STAR && t != token.SLASH && t != token.DOUBLESLASH && t != token.PERCENT && t != token.AT {
			break
		}
		line := p.cur().Line
		op := p.next().Literal
		e = &ast.BinOp{Pos: ast.Pos{L: line}, Left: e, Op: op, Right: p.parseFactor()}
	}
	return e
}

func (p *Parser) parseBinLevel(next func() ast.Expr, t token.Type, op string) ast.Expr {
	e := next()
	for p.at(t) {
		line := p.cur().Line
		p.next()
		e = &ast.BinOp{Pos: ast.Pos{L: line}, Left: e, Op: op, Right: next()}
	}
	return e
}

// parseFactor 一元运算（+ - ~），操作数为幂表达式
func (p *Parser) parseFactor() ast.Expr {
	line := p.cur().Line
	if p.at(token.PLUS) || p.at(token.MINUS) || p.at(token.TILDE) {
		op := p.next().Literal
		return &ast.UnaryOp{Pos: ast.Pos{L: line}, Op: op, Operand: p.parseFactor()}
	}
	return p.parsePower()
}

// parsePower 幂运算（右结合，优先级高于一元运算）
func (p *Parser) parsePower() ast.Expr {
	line := p.cur().Line
	base := p.parseAwait()
	if p.at(token.DOUBLESTAR) {
		p.next()
		return &ast.BinOp{Pos: ast.Pos{L: line}, Left: base, Op: "**", Right: p.parseFactor()}
	}
	return base
}

func (p *Parser) parseAwait() ast.Expr {
	line := p.cur().Line
	if p.at(token.KW_AWAIT) {
		p.next()
		return &ast.Await{Pos: ast.Pos{L: line}, Value: p.parseAwait()}
	}
	return p.parseUnaryPostfix()
}

// parseUnaryPostfix 原子表达式 + 后缀（调用 / 下标 / 属性）
func (p *Parser) parseUnaryPostfix() ast.Expr {
	e := p.parseAtom()
	for {
		switch p.cur().Type {
		case token.LPAREN:
			line := p.cur().Line
			args, kws := p.parseCallArgs()
			e = &ast.Call{Pos: ast.Pos{L: line}, Func: e, Args: args, Keywords: kws}
		case token.LBRACKET:
			line := p.cur().Line
			p.next()
			idx := p.parseSubscript()
			p.expect(token.RBRACKET)
			e = &ast.Subscript{Pos: ast.Pos{L: line}, Value: e, Slice: idx}
		case token.DOT:
			line := p.cur().Line
			p.next()
			name := p.expect(token.NAME).Literal
			e = &ast.Attribute{Pos: ast.Pos{L: line}, Value: e, Attr: name}
		default:
			return e
		}
	}
}

// parseCallArgs 解析调用参数
func (p *Parser) parseCallArgs() ([]ast.Expr, []ast.Keyword) {
	line0 := p.cur().Line
	p.expect(token.LPAREN)
	var args []ast.Expr
	var kws []ast.Keyword
	for !p.at(token.RPAREN) && !p.at(token.EOF) {
		if p.at(token.DOUBLESTAR) {
			line := p.cur().Line
			p.next()
			v := p.parseTestListNoTuple()
			kws = append(kws, ast.Keyword{Name: "", Value: v})
			_ = line
		} else if p.at(token.STAR) {
			line := p.cur().Line
			p.next()
			args = append(args, &ast.Starred{Pos: ast.Pos{L: line}, Value: p.parseTestListNoTuple()})
		} else {
			e := p.parseTestListNoTuple()
			if p.at(token.KW_FOR) || p.at(token.KW_ASYNC) {
				// 生成器表达式作为唯一参数
				gens := p.parseCompClauses()
				args = append(args, &ast.GenExp{Pos: ast.Pos{L: line0}, Elt: e, Gens: gens})
				if !p.accept(token.COMMA) {
					break
				}
				continue
			}
			if p.at(token.ASSIGN) {
				p.next()
				v := p.parseTestListNoTuple()
				name := ""
				if n, ok := e.(*ast.Name); ok {
					name = n.Id
				}
				kws = append(kws, ast.Keyword{Name: name, Value: v})
			} else {
				args = append(args, e)
			}
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(token.RPAREN)
	return args, kws
}

// parseSubscript 解析下标（支持切片与多维）
func (p *Parser) parseSubscript() ast.Expr {
	line := p.cur().Line
	var elems []ast.Expr
	for {
		elems = append(elems, p.parseSubscriptItem())
		if !p.accept(token.COMMA) {
			break
		}
		if p.at(token.RBRACKET) {
			break
		}
	}
	if len(elems) == 1 {
		return elems[0]
	}
	return &ast.Tuple{Pos: ast.Pos{L: line}, Elements: elems}
}

func (p *Parser) parseSubscriptItem() ast.Expr {
	line := p.cur().Line
	var lower ast.Expr
	if !p.at(token.COLON) {
		lower = p.parseTestListNoTuple()
	}
	if !p.at(token.COLON) {
		return lower
	}
	p.next() // :
	var upper, step ast.Expr
	if !p.at(token.COLON) && !p.at(token.RBRACKET) && !p.at(token.COMMA) {
		upper = p.parseTestListNoTuple()
	}
	if p.at(token.COLON) {
		p.next()
		if !p.at(token.RBRACKET) && !p.at(token.COMMA) {
			step = p.parseTestListNoTuple()
		}
	}
	return &ast.SliceExpr{Pos: ast.Pos{L: line}, Lower: lower, Upper: upper, Step: step}
}

// parseLambda 解析 lambda 表达式
func (p *Parser) parseLambda() ast.Expr {
	line := p.cur().Line
	p.next() // lambda
	ps := &ast.Params{}
	kwonly := false
	for !p.at(token.COLON) && !p.at(token.EOF) {
		if p.at(token.DOUBLESTAR) {
			p.next()
			ps.Kwarg = p.expect(token.NAME).Literal
		} else if p.at(token.STAR) {
			p.next()
			kwonly = true
			if p.at(token.NAME) {
				ps.Vararg = p.next().Literal
			}
		} else if p.at(token.SLASH) {
			p.next()
		} else {
			arg := &ast.Arg{Name: p.expect(token.NAME).Literal}
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
	p.expect(token.COLON)
	body := p.parseExpression()
	return &ast.Lambda{Pos: ast.Pos{L: line}, Params: ps, Body: body}
}

// parseYield 解析 yield 表达式
func (p *Parser) parseYield() ast.Expr {
	line := p.cur().Line
	p.next() // yield
	if p.at(token.KW_FROM) {
		p.next()
		return &ast.Yield{Pos: ast.Pos{L: line}, Value: p.parseTestList(), From: true}
	}
	if p.at(token.NEWLINE) || p.at(token.RPAREN) || p.at(token.SEMI) || p.at(token.EOF) || p.at(token.RBRACKET) {
		return &ast.Yield{Pos: ast.Pos{L: line}}
	}
	return &ast.Yield{Pos: ast.Pos{L: line}, Value: p.parseTestList()}
}

// parseAtom 解析原子表达式
func (p *Parser) parseAtom() ast.Expr {
	t := p.cur()
	line := t.Line
	switch t.Type {
	case token.NAME:
		p.next()
		return &ast.Name{Pos: ast.Pos{L: line}, Id: t.Literal}
	case token.NUMBER:
		p.next()
		isFloat := strings.ContainsAny(t.Literal, ".eE") && !strings.HasPrefix(strings.ToLower(t.Literal), "0x") &&
			!strings.HasPrefix(strings.ToLower(t.Literal), "0o") && !strings.HasPrefix(strings.ToLower(t.Literal), "0b")
		return &ast.Number{Pos: ast.Pos{L: line}, Value: t.Literal, IsFloat: isFloat}
	case token.STRING:
		p.next()
		// 相邻字符串字面量拼接
		val := t.Literal
		isBytes := t.Extra == "b"
		for p.at(token.STRING) {
			val += p.next().Literal
		}
		// 普通串后面紧跟 f-string：合并成 FString（PEP 隐式拼接）
		if p.at(token.FSTRING) {
			parts := []ast.FStringPart{{Literal: val, IsLiteral: true}}
			for p.at(token.FSTRING) || p.at(token.STRING) {
				nt := p.cur()
				p.next()
				if nt.Type == token.FSTRING {
					if sub, ok := p.parseFString(nt).(*ast.FString); ok {
						parts = append(parts, sub.Parts...)
					}
				} else {
					parts = append(parts, ast.FStringPart{Literal: nt.Literal, IsLiteral: true})
				}
			}
			return &ast.FString{Pos: ast.Pos{L: line}, Parts: parts}
		}
		return &ast.String{Pos: ast.Pos{L: line}, Value: val, IsBytes: isBytes}
	case token.FSTRING:
		p.next()
		e := p.parseFString(t)
		// 相邻 f-string / 字符串隐式拼接（f"a" f"b" → f"ab"）
		if p.at(token.FSTRING) || p.at(token.STRING) {
			fs, ok := e.(*ast.FString)
			if !ok {
				return e
			}
			parts := append([]ast.FStringPart{}, fs.Parts...)
			for p.at(token.FSTRING) || p.at(token.STRING) {
				nt := p.cur()
				p.next()
				if nt.Type == token.FSTRING {
					if sub, ok := p.parseFString(nt).(*ast.FString); ok {
						parts = append(parts, sub.Parts...)
					}
				} else {
					parts = append(parts, ast.FStringPart{Literal: nt.Literal, IsLiteral: true})
				}
			}
			return &ast.FString{Pos: fs.Pos, Parts: parts}
		}
		return e
	case token.KW_TRUE:
		p.next()
		return &ast.Name{Pos: ast.Pos{L: line}, Id: "True"}
	case token.KW_FALSE:
		p.next()
		return &ast.Name{Pos: ast.Pos{L: line}, Id: "False"}
	case token.KW_NONE:
		p.next()
		return &ast.Name{Pos: ast.Pos{L: line}, Id: "None"}
	case token.ELLIPSIS:
		p.next()
		return &ast.Ellipsis{Pos: ast.Pos{L: line}}
	case token.LPAREN:
		p.next()
		if p.at(token.RPAREN) {
			p.next()
			return &ast.Tuple{Pos: ast.Pos{L: line}}
		}
		if p.at(token.KW_YIELD) {
			e := p.parseYield()
			p.expect(token.RPAREN)
			return e
		}
		first := p.parseTestListNoTuple()
		if p.at(token.KW_FOR) || p.at(token.KW_ASYNC) {
			// 生成器表达式
			gens := p.parseCompClauses()
			p.expect(token.RPAREN)
			return &ast.GenExp{Pos: ast.Pos{L: line}, Elt: first, Gens: gens}
		}
		if p.at(token.COMMA) {
			elems := []ast.Expr{first}
			for p.accept(token.COMMA) {
				if p.at(token.RPAREN) {
					break
				}
				elems = append(elems, p.parseTestListNoTuple())
			}
			p.expect(token.RPAREN)
			return &ast.Tuple{Pos: ast.Pos{L: line}, Elements: elems}
		}
		p.expect(token.RPAREN)
		return first
	case token.LBRACKET:
		p.next()
		if p.at(token.RBRACKET) {
			p.next()
			return &ast.List{Pos: ast.Pos{L: line}}
		}
		first := p.parseTestListNoTuple()
		if p.at(token.KW_FOR) || p.at(token.KW_ASYNC) {
			gens := p.parseCompClauses()
			p.expect(token.RBRACKET)
			return &ast.ListComp{Pos: ast.Pos{L: line}, Elt: first, Gens: gens}
		}
		elems := []ast.Expr{first}
		for p.accept(token.COMMA) {
			if p.at(token.RBRACKET) {
				break
			}
			elems = append(elems, p.parseTestListNoTuple())
		}
		p.expect(token.RBRACKET)
		return &ast.List{Pos: ast.Pos{L: line}, Elements: elems}
	case token.LBRACE:
		return p.parseBrace()
	case token.KW_LAMBDA:
		return p.parseLambda()
	case token.KW_YIELD:
		return p.parseYield()
	case token.KW_NOT:
		p.next()
		return &ast.UnaryOp{Pos: ast.Pos{L: line}, Op: "not", Operand: p.parseNot()}
	case token.STAR:
		p.next()
		return &ast.Starred{Pos: ast.Pos{L: line}, Value: p.parseTestListNoTuple()}
	}
	p.errf("无法解析的表达式起始记号 %q", t.Literal)
	p.next()
	return &ast.Name{Pos: ast.Pos{L: line}, Id: "__error__"}
}

// parseBrace 解析 { } 字面量（dict / set / 推导式）
func (p *Parser) parseBrace() ast.Expr {
	line := p.cur().Line
	p.next() // {
	if p.at(token.RBRACE) {
		p.next()
		return &ast.Dict{Pos: ast.Pos{L: line}}
	}
	if p.at(token.DOUBLESTAR) {
		p.next()
		v := p.parseTestListNoTuple()
		keys := []ast.Expr{nil}
		vals := []ast.Expr{v}
		for p.accept(token.COMMA) {
			if p.at(token.RBRACE) {
				break
			}
			if p.at(token.DOUBLESTAR) {
				p.next()
				vals = append(vals, p.parseTestListNoTuple())
				keys = append(keys, nil)
				continue
			}
			k := p.parseTestListNoTuple()
			p.expect(token.COLON)
			vals = append(vals, p.parseTestListNoTuple())
			keys = append(keys, k)
		}
		p.expect(token.RBRACE)
		return &ast.Dict{Pos: ast.Pos{L: line}, Keys: keys, Values: vals}
	}
	first := p.parseTestListNoTuple()
	if p.at(token.COLON) {
		p.next()
		val := p.parseTestListNoTuple()
		if p.at(token.KW_FOR) || p.at(token.KW_ASYNC) {
			gens := p.parseCompClauses()
			p.expect(token.RBRACE)
			return &ast.DictComp{Pos: ast.Pos{L: line}, Key: first, Value: val, Gens: gens}
		}
		keys := []ast.Expr{first}
		vals := []ast.Expr{val}
		for p.accept(token.COMMA) {
			if p.at(token.RBRACE) {
				break
			}
			if p.at(token.DOUBLESTAR) {
				p.next()
				vals = append(vals, p.parseTestListNoTuple())
				keys = append(keys, nil)
				continue
			}
			k := p.parseTestListNoTuple()
			p.expect(token.COLON)
			vals = append(vals, p.parseTestListNoTuple())
			keys = append(keys, k)
		}
		p.expect(token.RBRACE)
		return &ast.Dict{Pos: ast.Pos{L: line}, Keys: keys, Values: vals}
	}
	if p.at(token.KW_FOR) || p.at(token.KW_ASYNC) {
		gens := p.parseCompClauses()
		p.expect(token.RBRACE)
		return &ast.SetComp{Pos: ast.Pos{L: line}, Elt: first, Gens: gens}
	}
	elems := []ast.Expr{first}
	for p.accept(token.COMMA) {
		if p.at(token.RBRACE) {
			break
		}
		elems = append(elems, p.parseTestListNoTuple())
	}
	p.expect(token.RBRACE)
	return &ast.Set{Pos: ast.Pos{L: line}, Elements: elems}
}

// parseCompClauses 解析推导式的 for/if 子句
func (p *Parser) parseCompClauses() []ast.Comp {
	var gens []ast.Comp
	for p.at(token.KW_FOR) || p.at(token.KW_ASYNC) {
		isAsync := false
		if p.at(token.KW_ASYNC) {
			p.next()
			isAsync = true
		}
		p.expect(token.KW_FOR)
		target := p.parseTargetList()
		p.expect(token.KW_IN)
		iter := p.parseOr()
		c := ast.Comp{Target: target, Iter: iter, IsAsync: isAsync}
		for p.at(token.KW_IF) {
			p.next()
			c.Ifs = append(c.Ifs, p.parseOr())
		}
		gens = append(gens, c)
	}
	return gens
}

// parseFString 拆分 f-string 为字面量与插值表达式
func (p *Parser) parseFString(tok token.Token) ast.Expr {
	raw := strings.ContainsAny(tok.Extra, "rR")
	src := []rune(tok.Literal)
	var parts []ast.FStringPart
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			v := lit.String()
			if !raw {
				v = lexer.Unescape(v)
			}
			parts = append(parts, ast.FStringPart{Literal: v, IsLiteral: true})
			lit.Reset()
		}
	}
	i := 0
	for i < len(src) {
		c := src[i]
		if c == '{' {
			if i+1 < len(src) && src[i+1] == '{' {
				lit.WriteRune('{')
				i += 2
				continue
			}
			// 找匹配的 }（跳过字段内嵌套的字符串与转义，PEP 701）
			j := fstringFieldEnd(src, i+1)
			if j < 0 {
				lit.WriteString(string(src[i:]))
				break
			}
			flush()
			content := string(src[i+1 : j])
			exprSrc, conv, format, hasSpec := fstringSplitField(content)
			// 自文档 f-string：{expr=} / {expr=!r} / {expr=:spec}
			// CPython 语义：输出「源码原文=」再接值；未指定转换/格式时默认用 repr
			debugText := ""
			if strings.HasSuffix(exprSrc, "=") &&
				!strings.HasSuffix(exprSrc, "==") && !strings.HasSuffix(exprSrc, "!=") &&
				!strings.HasSuffix(exprSrc, "<=") && !strings.HasSuffix(exprSrc, ">=") &&
				!strings.HasSuffix(exprSrc, ":=") {
				debugText = exprSrc
				exprSrc = strings.TrimSpace(strings.TrimSuffix(exprSrc, "="))
				if conv == 0 && !hasSpec {
					conv = 'r'
				}
			}
			sub, errs := ParseExpr(exprSrc)
			if len(errs) > 0 {
				for _, e := range errs {
					p.errs = append(p.errs, "f-string 内表达式错误: "+e)
				}
			}
			if debugText != "" {
				parts = append(parts, ast.FStringPart{Literal: debugText, IsLiteral: true})
			}
			if hasSpec && strings.ContainsRune(format, '{') {
				// PEP 701 嵌套字段说明符（f"{v:{fill}{width}}"）：说明符本身需在运行时求值，
				// 而 ast.FStringPart.Format 是静态字串，故改写为 format(v, 说明符) 调用。
				parts = append(parts, ast.FStringPart{Expr: buildDynamicFormat(sub, conv, format, tok.Line)})
			} else {
				parts = append(parts, ast.FStringPart{Expr: sub, Format: format, Conversion: conv})
			}
			i = j + 1
			continue
		}
		if c == '}' {
			if i+1 < len(src) && src[i+1] == '}' {
				lit.WriteRune('}')
				i += 2
				continue
			}
			lit.WriteRune('}')
			i++
			continue
		}
		lit.WriteRune(c)
		i++
	}
	flush()
	return &ast.FString{Pos: ast.Pos{L: tok.Line}, Parts: parts}
}

// ==================== f-string 字段扫描（PEP 701） ====================

// fstringSkipString 跳过 src[i]（引号起点）处的字符串字面量，返回其后一个下标。
// 支持三引号与反斜杠转义；用于让字段边界/分隔符识别不被字段内的字符串干扰。
func fstringSkipString(src []rune, i int) int {
	q := src[i]
	triple := i+2 < len(src) && src[i+1] == q && src[i+2] == q
	i++
	if triple {
		i += 2
	}
	for i < len(src) {
		c := src[i]
		if c == '\\' {
			i += 2
			continue
		}
		if triple {
			if c == q && i+2 < len(src) && src[i+1] == q && src[i+2] == q {
				return i + 3
			}
		} else if c == q {
			return i + 1
		}
		i++
	}
	return len(src)
}

// fstringFieldEnd 从 src[start]（'{' 之后）开始找插值字段的结束 '}'，返回其下标。
// 跳过字段内嵌套字符串、转义与嵌套大括号；找不到返回 -1。
func fstringFieldEnd(src []rune, start int) int {
	depth := 1
	for i := start; i < len(src); i++ {
		switch src[i] {
		case '\'', '"':
			i = fstringSkipString(src, i) - 1
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// fstringSplitField 把字段内容拆成「表达式原文 / 转换符 / 格式说明符」。
// 只在顶层（括号深度 0、不在字符串内）识别 !conv 与 :spec；!= 不算转换符。
func fstringSplitField(content string) (exprSrc string, conv byte, spec string, hasSpec bool) {
	rs := []rune(content)
	lvl := 0
	splitAt, specAt := -1, -1
	for k := 0; k < len(rs); k++ {
		switch r := rs[k]; r {
		case '\'', '"':
			k = fstringSkipString(rs, k) - 1
		case '(', '[', '{':
			lvl++
		case ')', ']', '}':
			lvl--
		case '!':
			if lvl == 0 && splitAt < 0 && (k+1 >= len(rs) || rs[k+1] != '=') {
				splitAt = k
			}
		case ':':
			if lvl == 0 && specAt < 0 {
				specAt = k
			}
		}
	}
	exprEnd := len(rs)
	if splitAt >= 0 {
		exprEnd = splitAt
	} else if specAt >= 0 {
		exprEnd = specAt
	}
	exprSrc = strings.TrimSpace(string(rs[:exprEnd]))
	rest := exprEnd
	if splitAt >= 0 {
		if splitAt+1 < len(rs) {
			conv = byte(rs[splitAt+1])
		}
		rest = splitAt + 2
	}
	if rest < len(rs) && rs[rest] == ':' {
		spec = string(rs[rest+1:])
		hasSpec = true
	}
	return exprSrc, conv, spec, hasSpec
}

// fstringSpecParts 把格式说明符文本拆成部件：字面段 + 嵌套插值字段。
// 嵌套字段按 CPython 语义先求值再 str() 展开（可由运行时 evalFString 直接完成）。
func fstringSpecParts(spec string, line int) []ast.FStringPart {
	src := []rune(spec)
	var parts []ast.FStringPart
	var lit strings.Builder
	flush := func() {
		if lit.Len() > 0 {
			parts = append(parts, ast.FStringPart{Literal: lit.String(), IsLiteral: true})
			lit.Reset()
		}
	}
	for i := 0; i < len(src); {
		c := src[i]
		if c == '{' {
			if i+1 < len(src) && src[i+1] == '{' {
				lit.WriteRune('{')
				i += 2
				continue
			}
			j := fstringFieldEnd(src, i+1)
			if j < 0 {
				lit.WriteString(string(src[i:]))
				break
			}
			flush()
			exprSrc, conv, sub, hasSpec := fstringSplitField(string(src[i+1 : j]))
			subExpr, _ := ParseExpr(exprSrc)
			if hasSpec && strings.ContainsRune(sub, '{') {
				parts = append(parts, ast.FStringPart{Expr: buildDynamicFormat(subExpr, conv, sub, line)})
			} else {
				parts = append(parts, ast.FStringPart{Expr: subExpr, Format: sub, Conversion: conv})
			}
			i = j + 1
			continue
		}
		if c == '}' && i+1 < len(src) && src[i+1] == '}' {
			lit.WriteRune('}')
			i += 2
			continue
		}
		lit.WriteRune(c)
		i++
	}
	flush()
	return parts
}

// buildDynamicFormat 把「值 + 动态格式说明符」组装成 format(值, 说明符) 调用。
//
// ast.FStringPart.Format 是静态字串，承载不了 PEP 701 的嵌套字段说明符（如
// f"{v:{fill}{width}}"）；转成内建 format() 调用即可复用既有运行时，无需改 runtime。
// 转换符 !r/!s/!a 先于说明符生效，故包一层只带 Conversion 的 FString。
func buildDynamicFormat(value ast.Expr, conv byte, spec string, line int) ast.Expr {
	return BuildDynamicFormatExpr(line, value, conv, fstringSpecParts(spec, line))
}

// BuildDynamicFormatExpr 供 py/pyparse（gopapy 轨）复用同一套动态说明符改写规则。
func BuildDynamicFormatExpr(line int, value ast.Expr, conv byte, specParts []ast.FStringPart) ast.Expr {
	pos := ast.Pos{L: line}
	inner := value
	if conv != 0 {
		inner = &ast.FString{Pos: pos, Parts: []ast.FStringPart{{Expr: value, Conversion: conv}}}
	}
	return &ast.Call{
		Pos:  pos,
		Func: &ast.Name{Pos: pos, Id: "format"},
		Args: []ast.Expr{inner, &ast.FString{Pos: pos, Parts: specParts}},
	}
}
