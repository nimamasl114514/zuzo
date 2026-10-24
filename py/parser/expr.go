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
		return &ast.String{Pos: ast.Pos{L: line}, Value: val, IsBytes: isBytes}
	case token.FSTRING:
		p.next()
		return p.parseFString(t)
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
			// 找匹配的 }
			depth := 1
			j := i + 1
			var inner strings.Builder
			for j < len(src) && depth > 0 {
				ch := src[j]
				if ch == '{' {
					depth++
				} else if ch == '}' {
					depth--
					if depth == 0 {
						break
					}
				}
				inner.WriteRune(ch)
				j++
			}
			if j >= len(src) {
				lit.WriteString(string(src[i:]))
				break
			}
			flush()
			content := inner.String()
			exprSrc := content
			format := ""
			var conv byte
			// 拆分 !conv 与 :format（跳过嵌套结构）
			depthB, depthC, depthP := 0, 0, 0
			inStr := rune(0)
			cut := len(content)
			for k, r := range []rune(content) {
				if inStr != 0 {
					if r == inStr {
						inStr = 0
					}
					continue
				}
				switch r {
				case '\'', '"':
					inStr = r
				case '(', '[', '{':
					depthP++
				case ')', ']', '}':
					depthP--
				case ':':
					if depthB == 0 && depthC == 0 && depthP == 0 {
						cut = k
					}
				case '!':
					if depthB == 0 && depthC == 0 && depthP == 0 && cut == len(content) {
						depthB++
					}
				}
				_ = depthC
				if cut != len(content) {
					break
				}
			}
			// 重新精确拆分
			rs := []rune(content)
			lvl := 0
			inS := rune(0)
			splitAt := -1
			specAt := -1
			for k := 0; k < len(rs); k++ {
				r := rs[k]
				if inS != 0 {
					if r == inS {
						inS = 0
					}
					continue
				}
				switch r {
				case '\'', '"':
					inS = r
				case '(', '[', '{':
					lvl++
				case ')', ']', '}':
					lvl--
				case '!':
					if lvl == 0 && splitAt < 0 {
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
			rest := rs[exprEnd:]
			if splitAt >= 0 {
				// !r 或 !s 或 !a
				if len(rest) >= 2 {
					conv = byte(rest[1])
					rest = rest[2:]
				}
			}
			if len(rest) > 0 && rest[0] == ':' {
				format = string(rest[1:])
			}
			// 自文档 f-string：{expr=} / {expr=!r} / {expr=:spec}
			// CPython 语义：输出「源码原文=」再接值；未指定转换/格式时默认用 repr
			debugText := ""
			if strings.HasSuffix(exprSrc, "=") &&
				!strings.HasSuffix(exprSrc, "==") && !strings.HasSuffix(exprSrc, "!=") &&
				!strings.HasSuffix(exprSrc, "<=") && !strings.HasSuffix(exprSrc, ">=") &&
				!strings.HasSuffix(exprSrc, ":=") {
				debugText = exprSrc
				exprSrc = strings.TrimSpace(strings.TrimSuffix(exprSrc, "="))
				if conv == 0 && format == "" {
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
			parts = append(parts, ast.FStringPart{Expr: sub, Format: format, Conversion: conv})
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
