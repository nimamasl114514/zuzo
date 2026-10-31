package nativecc

// parseExpr 顶层表达式（逗号表达式）
func (p *Parser) parseExpr() (Expr, error) {
	x, err := p.parseAssignExpr()
	if err != nil {
		return nil, err
	}
	for p.at(",") {
		p.advance()
		y, err := p.parseAssignExpr()
		if err != nil {
			return nil, err
		}
		x = &BinaryExpr{Op: ",", X: x, Y: y}
	}
	return x, nil
}

var assignOps = map[string]bool{
	"=": true, "+=": true, "-=": true, "*=": true, "/=": true, "%=": true,
	"&=": true, "|=": true, "^=": true, "<<=": true, ">>=": true,
}

func (p *Parser) parseAssignExpr() (Expr, error) {
	x, err := p.parseCondExpr()
	if err != nil {
		return nil, err
	}
	t := p.peek()
	if t.Kind == TPunct && assignOps[t.Text] {
		p.advance()
		y, err := p.parseAssignExpr()
		if err != nil {
			return nil, err
		}
		return &AssignExpr{Op: t.Text, L: x, R: y}, nil
	}
	return x, nil
}

func (p *Parser) parseCondExpr() (Expr, error) {
	cond, err := p.parseBinary(0)
	if err != nil {
		return nil, err
	}
	if p.at("?") {
		p.advance()
		then, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(":"); err != nil {
			return nil, err
		}
		els, err := p.parseCondExpr()
		if err != nil {
			return nil, err
		}
		return &CondExpr{Cond: cond, Then: then, Else: els}, nil
	}
	return cond, nil
}

// 二元运算符优先级（数字越大越紧）
var binPrec = map[string]int{
	"||": 1,
	"&&": 2,
	"|":  3,
	"^":  4,
	"&":  5,
	"==": 6, "!=": 6,
	"<": 7, ">": 7, "<=": 7, ">=": 7,
	"<<": 8, ">>": 8,
	"+": 9, "-": 9,
	"*": 10, "/": 10, "%": 10,
}

func (p *Parser) parseBinary(minPrec int) (Expr, error) {
	x, err := p.parseCast()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TPunct {
			break
		}
		prec, ok := binPrec[t.Text]
		if !ok || prec < minPrec {
			break
		}
		p.advance()
		y, err := p.parseBinary(prec + 1)
		if err != nil {
			return nil, err
		}
		x = &BinaryExpr{Op: t.Text, X: x, Y: y}
	}
	return x, nil
}

func (p *Parser) parseCast() (Expr, error) {
	if p.at("(") && p.isCastAhead() {
		p.advance()
		ty, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		if p.at("{") {
			// 复合字面量 (T){...}
			p.advance()
			init := &Init{IsList: true}
			for !p.at("}") && p.peek().Kind != TEOF {
				sub, err := p.parseInitializer(nil)
				if err != nil {
					return nil, err
				}
				init.List = append(init.List, sub)
				if !p.accept(",") {
					break
				}
			}
			if err := p.expect("}"); err != nil {
				return nil, err
			}
			return &CompoundLit{Ty: ty, Init: init, exprBase: exprBase{ty: ty}}, nil
		}
		x, err := p.parseCast()
		if err != nil {
			return nil, err
		}
		return &CastExpr{To: ty, X: x, exprBase: exprBase{ty: ty}}, nil
	}
	return p.parseUnary()
}

// isCastAhead 判断 '(' 后是否为类型名
func (p *Parser) isCastAhead() bool {
	// 保存状态探测
	save := p.pos
	p.pos++ // 跳过 (
	ok := p.hasTypeSpecAhead()
	p.pos = save
	return ok
}

func (p *Parser) parseUnary() (Expr, error) {
	t := p.peek()
	if t.Kind == TPunct {
		switch t.Text {
		case "++", "--":
			p.advance()
			x, err := p.parseUnary()
			if err != nil {
				return nil, err
			}
			return &UnaryExpr{Op: t.Text, X: x}, nil
		case "&", "*", "+", "-", "!", "~":
			p.advance()
			x, err := p.parseCast()
			if err != nil {
				return nil, err
			}
			return &UnaryExpr{Op: t.Text, X: x}, nil
		}
	}
	if t.Kind == TKeyword && t.Text == "sizeof" {
		p.advance()
		if p.at("(") && p.isCastAhead() {
			p.advance()
			ty, err := p.parseTypeName()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			return &SizeofExpr{Ty: ty, IsType: true, exprBase: exprBase{ty: TyULongT}}, nil
		}
		x, err := p.parseUnary()
		if err != nil {
			return nil, err
		}
		return &SizeofExpr{X: x, exprBase: exprBase{ty: TyULongT}}, nil
	}
	if t.Kind == TKeyword && t.Text == "_Alignof" {
		p.advance()
		if err := p.expect("("); err != nil {
			return nil, err
		}
		ty, err := p.parseTypeName()
		if err != nil {
			return nil, err
		}
		if err := p.expect(")"); err != nil {
			return nil, err
		}
		return &IntLit{V: ty.Align, exprBase: exprBase{ty: TyULongT}}, nil
	}
	return p.parsePostfix()
}

func (p *Parser) parsePostfix() (Expr, error) {
	x, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		t := p.peek()
		if t.Kind != TPunct {
			break
		}
		switch t.Text {
		case "[":
			p.advance()
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect("]"); err != nil {
				return nil, err
			}
			x = &IndexExpr{X: x, Index: idx}
		case "(":
			p.advance()
			var args []Expr
			if !p.at(")") {
				for {
					a, err := p.parseAssignExpr()
					if err != nil {
						return nil, err
					}
					args = append(args, a)
					if !p.accept(",") {
						break
					}
				}
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			x = &CallExpr{Fn: x, Args: args}
		case ".":
			p.advance()
			if !p.atIdent() {
				return nil, p.errf("成员访问缺少字段名")
			}
			name := p.advance().Text
			x = &MemberExpr{X: x, Name: name, Arrow: false}
		case "->":
			p.advance()
			if !p.atIdent() {
				return nil, p.errf("成员访问缺少字段名")
			}
			name := p.advance().Text
			x = &MemberExpr{X: x, Name: name, Arrow: true}
		case "++", "--":
			p.advance()
			x = &PostfixExpr{Op: t.Text, X: x}
		default:
			return x, nil
		}
	}
	return x, nil
}

func (p *Parser) parsePrimary() (Expr, error) {
	t := p.peek()
	switch t.Kind {
	case TInt:
		p.advance()
		return &IntLit{V: t.IVal, exprBase: exprBase{ty: TyLongT}}, nil
	case TFloat:
		p.advance()
		return &FloatLit{V: t.FVal, exprBase: exprBase{ty: TyDoubleT}}, nil
	case TString:
		p.advance()
		return &StrLit{V: t.SVal, exprBase: exprBase{ty: TyCharPtr}}, nil
	case TChar:
		p.advance()
		return &CharLit{V: t.IVal, exprBase: exprBase{ty: TyIntT}}, nil
	case TIdent:
		p.advance()
		if sym := p.lookup(t.Text); sym != nil && sym.IsEnum {
			return &IntLit{V: sym.EnumVal, exprBase: exprBase{ty: TyIntT}}, nil
		}
		return &Ident{Name: t.Text, exprBase: exprBase{}}, nil
	case TPunct:
		if t.Text == "(" {
			p.advance()
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			return x, nil
		}
	}
	// 编译器内建
	if t.Kind == TIdent && len(t.Text) > 0 && t.Text[0] == '_' {
		// __builtin_xxx 已走 Ident 分支
	}
	return nil, p.errf("无法解析的表达式起始 %q", t.Text)
}

// parseConstExpr 解析常量表达式
func (p *Parser) parseConstExpr() (Expr, error) {
	return p.parseCondExpr()
}

// constIntValue 尝试对常量表达式求值
func constIntValue(e Expr) (int64, bool) {
	switch v := e.(type) {
	case *IntLit:
		return v.V, true
	case *CharLit:
		return v.V, true
	case *UnaryExpr:
		x, ok := constIntValue(v.X)
		if !ok {
			return 0, false
		}
		switch v.Op {
		case "-":
			return -x, true
		case "+":
			return x, true
		case "~":
			return ^x, true
		case "!":
			if x == 0 {
				return 1, true
			}
			return 0, true
		}
	case *BinaryExpr:
		a, ok1 := constIntValue(v.X)
		b, ok2 := constIntValue(v.Y)
		if !ok1 || !ok2 {
			return 0, false
		}
		switch v.Op {
		case "+":
			return a + b, true
		case "-":
			return a - b, true
		case "*":
			return a * b, true
		case "/":
			if b == 0 {
				return 0, false
			}
			return a / b, true
		case "%":
			if b == 0 {
				return 0, false
			}
			return a % b, true
		case "<<":
			return a << uint(b), true
		case ">>":
			return a >> uint(b), true
		case "&":
			return a & b, true
		case "|":
			return a | b, true
		case "^":
			return a ^ b, true
		case "==":
			return b2i(a == b), true
		case "!=":
			return b2i(a != b), true
		case "<":
			return b2i(a < b), true
		case ">":
			return b2i(a > b), true
		case "<=":
			return b2i(a <= b), true
		case ">=":
			return b2i(a >= b), true
		case "&&":
			return b2i(a != 0 && b != 0), true
		case "||":
			return b2i(a != 0 || b != 0), true
		}
	case *CastExpr:
		return constIntValue(v.X)
	}
	return 0, false
}
