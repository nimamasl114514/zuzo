package parser

import (
	"zuzo/py/ast"
	"zuzo/py/token"
)

// isMatchStmt 判断当前 NAME "match" 是否开启 match 语句（软关键字）
func (p *Parser) isMatchStmt() bool {
	if !p.at(token.NAME) || p.cur().Literal != "match" {
		return false
	}
	switch p.peekN(1).Type {
	case token.ASSIGN, token.COLONEQ, token.DOT,
		token.COLON, token.COMMA, token.PLUSEQ, token.MINUSEQ, token.STAREQ,
		token.SLASHEQ, token.PERCENTEQ, token.DOUBLESTAREQ, token.PIPE,
		token.AMP, token.CARET, token.LSHIFT, token.RSHIFT, token.PLUS,
		token.MINUS, token.STAR, token.SLASH, token.DOUBLESLASH, token.PERCENT,
		token.EQ, token.NE, token.LT, token.GT, token.LE, token.GE,
		token.NEWLINE, token.EOF:
		return false
	}
	return true
}

// parseMatch 解析 match 语句
func (p *Parser) parseMatch() ast.Stmt {
	line := p.cur().Line
	p.next() // match
	subject := p.parseTestList()
	node := &ast.Match{Pos: ast.Pos{L: line}, Subject: subject}
	p.expect(token.COLON)
	p.accept(token.NEWLINE)
	p.skipNewlines()
	if !p.at(token.INDENT) {
		p.errf("match 语句缺少缩进块")
		return node
	}
	p.next()
	for !p.at(token.DEDENT) && !p.at(token.EOF) {
		if p.at(token.NEWLINE) {
			p.next()
			continue
		}
		if !(p.at(token.NAME) && p.cur().Literal == "case") {
			p.errf("match 块内期望 case")
			p.synchronize()
			continue
		}
		cline := p.cur().Line
		p.next() // case
		pat := p.parsePattern()
		mc := &ast.MatchCase{Pos: ast.Pos{L: cline}, Pattern: pat}
		if p.at(token.KW_IF) {
			p.next()
			mc.Guard = p.parseTestListNoTuple()
		}
		mc.Body = p.parseSuite()
		node.Cases = append(node.Cases, mc)
	}
	p.accept(token.DEDENT)
	return node
}

// parsePattern 解析模式（含 or / as）
func (p *Parser) parsePattern() ast.Pattern {
	line := p.cur().Line
	first := p.parsePatternAtom()
	if p.at(token.PIPE) {
		alts := []ast.Pattern{first}
		for p.accept(token.PIPE) {
			alts = append(alts, p.parsePatternAtom())
		}
		first = &ast.POr{Pos: ast.Pos{L: line}, Alts: alts}
	}
	if p.at(token.KW_AS) {
		p.next()
		name := p.expect(token.NAME).Literal
		return &ast.PAs{Pos: ast.Pos{L: line}, Pat: first, Name: name}
	}
	return first
}

// parsePatternAtom 解析原子模式
func (p *Parser) parsePatternAtom() ast.Pattern {
	line := p.cur().Line
	switch p.cur().Type {
	case token.NAME:
		if p.cur().Literal == "_" {
			p.next()
			return &ast.PWildcard{Pos: ast.Pos{L: line}}
		}
		// 点号链（值模式）
		name := p.next().Literal
		var path ast.Expr = &ast.Name{Pos: ast.Pos{L: line}, Id: name}
		for p.at(token.DOT) {
			p.next()
			attr := p.expect(token.NAME).Literal
			path = &ast.Attribute{Pos: ast.Pos{L: line}, Value: path, Attr: attr}
		}
		// 类模式 Cls(...)
		if p.at(token.LPAREN) {
			p.next()
			cp := &ast.PClass{Pos: ast.Pos{L: line}, Cls: path}
			for !p.at(token.RPAREN) && !p.at(token.EOF) {
				if p.at(token.NAME) && p.peekN(1).Type == token.ASSIGN {
					kname := p.next().Literal
					p.next() // =
					cp.Keywords = append(cp.Keywords, &ast.KeywordPat{Name: kname, Pat: p.parsePattern()})
				} else {
					cp.Positional = append(cp.Positional, p.parsePattern())
				}
				if !p.accept(token.COMMA) {
					break
				}
			}
			p.expect(token.RPAREN)
			return cp
		}
		if _, isAttr := path.(*ast.Attribute); isAttr {
			return &ast.PValue{Pos: ast.Pos{L: line}, Expr: path}
		}
		return &ast.PCapture{Pos: ast.Pos{L: line}, Name: name}
	case token.LBRACKET:
		p.next()
		return p.parseSeqPattern(token.RBRACKET, line)
	case token.LPAREN:
		p.next()
		return p.parseSeqPattern(token.RPAREN, line)
	case token.LBRACE:
		p.next()
		mp := &ast.PMapping{Pos: ast.Pos{L: line}}
		for !p.at(token.RBRACE) && !p.at(token.EOF) {
			if p.at(token.DOUBLESTAR) {
				p.next()
				mp.Rest = p.expect(token.NAME).Literal
			} else {
				k := p.parseTestListNoTuple()
				p.expect(token.COLON)
				v := p.parsePattern()
				mp.Keys = append(mp.Keys, k)
				mp.Vals = append(mp.Vals, v)
			}
			if !p.accept(token.COMMA) {
				break
			}
		}
		p.expect(token.RBRACE)
		return mp
	case token.STAR:
		// 顶层 *rest（仅序列模式内有效，容错处理）
		p.next()
		name := p.expect(token.NAME).Literal
		return &ast.PSequence{Pos: ast.Pos{L: line}, Star: name}
	}
	// 字面量模式
	e := p.parseFactor()
	return &ast.PLiteral{Pos: ast.Pos{L: line}, Expr: e}
}

// parseSeqPattern 解析序列模式内容
func (p *Parser) parseSeqPattern(closer token.Type, line int) ast.Pattern {
	sp := &ast.PSequence{Pos: ast.Pos{L: line}}
	for !p.at(closer) && !p.at(token.EOF) {
		if p.at(token.STAR) {
			p.next()
			sp.Star = p.expect(token.NAME).Literal
		} else {
			sp.Items = append(sp.Items, p.parsePattern())
		}
		if !p.accept(token.COMMA) {
			break
		}
	}
	p.expect(closer)
	return sp
}
