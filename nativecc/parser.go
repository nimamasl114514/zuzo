package nativecc

import "fmt"

// Symbol 符号表条目
type Symbol struct {
	Name    string
	Ty      *CType
	IsTypedef bool
	EnumVal int64
	IsEnum  bool
	IsFunc  bool
	Static  bool
	Global  bool // 文件作用域
}

// scope 作用域
type scope struct {
	parent *scope
	names  map[string]*Symbol
}

func newScope(parent *scope) *scope {
	return &scope{parent: parent, names: map[string]*Symbol{}}
}

func (s *scope) lookup(name string) *Symbol {
	for sc := s; sc != nil; sc = sc.parent {
		if sym, ok := sc.names[name]; ok {
			return sym
		}
	}
	return nil
}

// Parser C 语法分析器
type Parser struct {
	toks   []Token
	pos    int
	scope  *scope
	file   string
	tu     *TranslationUnit
	funcs  map[string]*FuncDecl
	globals map[string]*VarDecl
	funcStack []*FuncDecl
}

// NewParser 构造解析器
func NewParser(toks []Token, file string) *Parser {
	return &Parser{
		toks:    toks,
		scope:   newScope(nil),
		file:    file,
		tu:      &TranslationUnit{},
		funcs:   map[string]*FuncDecl{},
		globals: map[string]*VarDecl{},
	}
}

// ── 游标工具 ────────────────────────────────────────────

func (p *Parser) peek() Token {
	if p.pos >= len(p.toks) {
		return Token{Kind: TEOF}
	}
	return p.toks[p.pos]
}

func (p *Parser) peekN(n int) Token {
	if p.pos+n >= len(p.toks) {
		return Token{Kind: TEOF}
	}
	return p.toks[p.pos+n]
}

func (p *Parser) advance() Token {
	t := p.peek()
	if p.pos < len(p.toks) {
		p.pos++
	}
	return t
}

func (p *Parser) at(s string) bool { return p.peek().Text == s && (p.peek().Kind == TPunct || p.peek().Kind == TKeyword) }

func (p *Parser) accept(s string) bool {
	if p.at(s) {
		p.pos++
		return true
	}
	return false
}

func (p *Parser) expect(s string) error {
	if p.at(s) {
		p.pos++
		return nil
	}
	return p.errf("期望 %q，实际 %q", s, p.peek().Text)
}

func (p *Parser) atIdent() bool { return p.peek().Kind == TIdent }

func (p *Parser) errf(format string, args ...interface{}) error {
	t := p.peek()
	return fmt.Errorf("%s:%d:%d: %s", t.File, t.Line, t.Col, fmt.Sprintf(format, args...))
}

func (p *Parser) pushScope() { p.scope = newScope(p.scope) }
func (p *Parser) popScope()  { p.scope = p.scope.parent }

func (p *Parser) declare(sym *Symbol) { p.scope.names[sym.Name] = sym }

func (p *Parser) lookup(name string) *Symbol { return p.scope.lookup(name) }

// ── 入口 ────────────────────────────────────────────────

// Parse 解析整个翻译单元
func (p *Parser) Parse() (*TranslationUnit, error) {
	for p.peek().Kind != TEOF {
		if err := p.parseExternalDecl(); err != nil {
			return nil, err
		}
	}
	return p.tu, nil
}

// ── 类型说明符 ──────────────────────────────────────────

// DeclSpec 声明说明符结果
type DeclSpec struct {
	Base    *CType
	Storage string
}

type typeFlags struct {
	nVoid, nChar, nShort, nInt, nLong, nFloat, nDouble, nSigned, nUnsigned, nBool int
	explicit *CType
	isConst  bool
}

func (f *typeFlags) resolve() (*CType, error) {
	if f.explicit != nil {
		return f.explicit, nil
	}
	switch {
	case f.nVoid > 0:
		return TyVoidT, nil
	case f.nBool > 0:
		return TyBoolT, nil
	case f.nFloat > 0:
		return TyFloatT, nil
	case f.nDouble > 0:
		return TyDoubleT, nil
	case f.nChar > 0:
		if f.nUnsigned > 0 {
			return &CType{Kind: TyChar, Size: 1, Align: 1, Unsigned: true}, nil
		}
		return TyCharT, nil
	case f.nShort > 0:
		if f.nUnsigned > 0 {
			return &CType{Kind: TyShort, Size: 2, Align: 2, Unsigned: true}, nil
		}
		return TyShortT, nil
	case f.nLong >= 2:
		if f.nUnsigned > 0 {
			return TyULLongT, nil
		}
		return TyLLongT, nil
	case f.nLong == 1:
		if f.nUnsigned > 0 {
			return TyULongT, nil
		}
		return TyLongT, nil
	case f.nInt > 0:
		if f.nUnsigned > 0 {
			return TyUIntT, nil
		}
		return TyIntT, nil
	case f.nSigned > 0 || f.nUnsigned > 0:
		if f.nUnsigned > 0 {
			return TyUIntT, nil
		}
		return TyIntT, nil
	}
	// 无类型说明符：宽松按 int
	return TyIntT, nil
}

func (p *Parser) hasTypeSpecAhead() bool {
	t := p.peek()
	if t.Kind == TKeyword {
		switch t.Text {
		case "void", "char", "short", "int", "long", "float", "double", "signed",
			"unsigned", "_Bool", "struct", "union", "enum", "const", "volatile",
			"static", "extern", "typedef", "register", "auto", "inline", "restrict":
			return true
		}
		return false
	}
	if t.Kind == TIdent {
		if sym := p.lookup(t.Text); sym != nil && sym.IsTypedef {
			return true
		}
	}
	return false
}

// parseDeclSpecs 解析声明说明符
func (p *Parser) parseDeclSpecs() (*DeclSpec, error) {
	var f typeFlags
	storage := ""
	sawAny := false
	for {
		t := p.peek()
		if t.Kind == TKeyword {
			switch t.Text {
			case "typedef", "extern", "static", "register", "auto":
				storage = t.Text
				p.advance()
				sawAny = true
				continue
			case "const":
				f.isConst = true
				p.advance()
				continue
			case "volatile", "restrict", "inline":
				p.advance()
				continue
			case "void":
				f.nVoid++
				p.advance()
				sawAny = true
				continue
			case "_Bool":
				f.nBool++
				p.advance()
				sawAny = true
				continue
			case "char":
				f.nChar++
				p.advance()
				sawAny = true
				continue
			case "short":
				f.nShort++
				p.advance()
				sawAny = true
				continue
			case "int":
				f.nInt++
				p.advance()
				sawAny = true
				continue
			case "long":
				f.nLong++
				p.advance()
				sawAny = true
				continue
			case "float":
				f.nFloat++
				p.advance()
				sawAny = true
				continue
			case "double":
				f.nDouble++
				p.advance()
				sawAny = true
				continue
			case "signed":
				f.nSigned++
				p.advance()
				sawAny = true
				continue
			case "unsigned":
				f.nUnsigned++
				p.advance()
				sawAny = true
				continue
			case "struct", "union":
				ty, err := p.parseStructUnion(t.Text == "union")
				if err != nil {
					return nil, err
				}
				f.explicit = ty
				sawAny = true
				continue
			case "enum":
				ty, err := p.parseEnum()
				if err != nil {
					return nil, err
				}
				f.explicit = ty
				sawAny = true
				continue
			}
			break
		}
		if t.Kind == TIdent {
			if sym := p.lookup(t.Text); sym != nil && sym.IsTypedef && f.explicit == nil {
				f.explicit = sym.Ty
				p.advance()
				sawAny = true
				continue
			}
		}
		break
	}
	if !sawAny {
		return nil, p.errf("缺少类型说明符")
	}
	base, err := f.resolve()
	if err != nil {
		return nil, err
	}
	return &DeclSpec{Base: base, Storage: storage}, nil
}

func (p *Parser) parseStructUnion(isUnion bool) (*CType, error) {
	kw := "struct"
	if isUnion {
		kw = "union"
	}
	p.advance() // struct/union
	tag := ""
	if p.atIdent() {
		tag = p.advance().Text
	}
	kind := TyStruct
	if isUnion {
		kind = TyUnion
	}
	// 已有同名类型：复用（前向声明/定义合并）
	var ty *CType
	if tag != "" {
		if sym := p.lookup("struct " + tag); sym != nil && sym.Ty.Kind == kind {
			ty = sym.Ty
		}
	}
	if ty == nil {
		ty = &CType{Kind: kind, Tag: tag, Incomplete: true, Align: 1}
		if tag != "" {
			p.declare(&Symbol{Name: "struct " + tag, Ty: ty})
		}
	}
	if !p.at("{") {
		if tag == "" {
			return nil, p.errf("%s 缺少标签或定义体", kw)
		}
		return ty, nil
	}
	p.advance() // {
	ty.Fields = nil
	ty.Incomplete = false
	for !p.at("}") && p.peek().Kind != TEOF {
		ds, err := p.parseDeclSpecs()
		if err != nil {
			return nil, err
		}
		for {
			name, fty, _, err := p.parseDeclarator(ds.Base, false)
			if err != nil {
				return nil, err
			}
			// 位域：暂不支持，跳过 :N
			if p.at(":") {
				p.advance()
				if _, err := p.parseConstExpr(); err != nil {
					return nil, err
				}
			}
			ty.Fields = append(ty.Fields, &Field{Name: name, Ty: fty})
			if !p.accept(",") {
				break
			}
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
	}
	if err := p.expect("}"); err != nil {
		return nil, err
	}
	ty.Layout()
	return ty, nil
}

func (p *Parser) parseEnum() (*CType, error) {
	p.advance() // enum
	tag := ""
	if p.atIdent() {
		tag = p.advance().Text
	}
	if !p.at("{") {
		if tag != "" {
			if sym := p.lookup("enum " + tag); sym != nil {
				return sym.Ty, nil
			}
		}
		ty := &CType{Kind: TyEnum, Size: 4, Align: 4, Tag: tag}
		return ty, nil
	}
	p.advance() // {
	ty := &CType{Kind: TyEnum, Size: 4, Align: 4, Tag: tag}
	var next int64
	for !p.at("}") && p.peek().Kind != TEOF {
		if !p.atIdent() {
			return nil, p.errf("枚举成员缺少名称")
		}
		name := p.advance().Text
		val := next
		if p.accept("=") {
			e, err := p.parseConstExpr()
			if err != nil {
				return nil, err
			}
			iv, ok := constIntValue(e)
			if !ok {
				return nil, p.errf("枚举值必须是常量整数")
			}
			val = iv
		}
		p.declare(&Symbol{Name: name, Ty: TyIntT, IsEnum: true, EnumVal: val})
		next = val + 1
		if !p.accept(",") {
			break
		}
	}
	if err := p.expect("}"); err != nil {
		return nil, err
	}
	if tag != "" {
		p.declare(&Symbol{Name: "enum " + tag, Ty: ty})
	}
	return ty, nil
}

// ── 声明符 ──────────────────────────────────────────────

// isNestedDeclarator 判断当前 '(' 是否开始一个嵌套声明符
func (p *Parser) isNestedDeclarator() bool {
	n := p.peekN(1)
	if n.Text == "*" || n.Text == "(" {
		return true
	}
	if n.Kind == TIdent && p.peekN(2).Text == ")" {
		// (a) 形式；若 a 是 typedef 名则更可能是参数表 (ZVal)
		if sym := p.lookup(n.Text); sym == nil || !sym.IsTypedef {
			return true
		}
	}
	return false
}

// parseDeclarator 解析声明符
// 返回 名称、类型、函数参数（若为函数类型）
func (p *Parser) parseDeclarator(base *CType, abstract bool) (string, *CType, []*Param, error) {
	// 前缀指针
	var pre []func(*CType) *CType
	for p.at("*") {
		p.advance()
		p.accept("const")
		pre = append(pre, func(t *CType) *CType { return PtrTo(t) })
	}

	var name string
	var ty *CType
	var params []*Param

	if p.at("(") && p.isNestedDeclarator() {
		// 两遍法：先定位匹配的 ')'，解析外层后缀作用到 base，
		// 再回到内层用「后缀作用后的类型」解析内层声明符。
		// 这样 int (*f)(int) → ptr(func)，int (*a)[3] → ptr(array)，
		// 而 int *a[3] → array(ptr)。
		p.advance() // 消耗 '('
		start := p.pos
		depth := 1
		for p.pos < len(p.toks) && depth > 0 {
			tk := p.toks[p.pos]
			if tk.Text == "(" {
				depth++
			}
			if tk.Text == ")" {
				depth--
			}
			if depth > 0 {
				p.pos++
			}
		}
		if depth != 0 {
			return "", nil, nil, p.errf("声明符括号未闭合")
		}
		p.pos++ // 跳过 ')'
		y, ps, err := p.parseSuffixes(base, nil)
		if err != nil {
			return "", nil, nil, err
		}
		afterSuffix := p.pos
		p.pos = start
		n, t, _, err := p.parseDeclarator(y, abstract)
		if err != nil {
			return "", nil, nil, err
		}
		if p.pos != afterSuffix && p.pos >= afterSuffix-1 {
			// 内层解析已到 ')'，此后位置由外层后缀决定
		}
		p.pos = afterSuffix
		name, ty, params = n, t, ps
		// 外层前缀指针最后应用（如 int *(*f)(int) 的外层 *）
		for i := len(pre) - 1; i >= 0; i-- {
			ty = pre[i](ty)
		}
	} else {
		if p.atIdent() {
			name = p.advance().Text
		} else if !abstract {
			return "", nil, nil, p.errf("期望标识符，实际 %q", p.peek().Text)
		}
		ty = base
		// 无括号：前缀指针先应用到 base（void *f(void) → 返回 void* 的函数）
		for i := len(pre) - 1; i >= 0; i-- {
			ty = pre[i](ty)
		}
		var err error
		ty, params, err = p.parseSuffixes(ty, nil)
		if err != nil {
			return "", nil, nil, err
		}
	}
	return name, ty, params, nil
}

// parseSuffixes 处理数组与函数后缀
func (p *Parser) parseSuffixes(ty *CType, params []*Param) (*CType, []*Param, error) {
	for {
		if p.at("[") {
			p.advance()
			n := int64(-1)
			if !p.at("]") {
				e, err := p.parseConstExpr()
				if err != nil {
					return nil, nil, err
				}
				if v, ok := constIntValue(e); ok {
					n = v
				}
			}
			if err := p.expect("]"); err != nil {
				return nil, nil, err
			}
			ty = ArrayOf(ty, n)
			continue
		}
		if p.at("(") {
			p.advance()
			ps, variadic, err := p.parseParamList()
			if err != nil {
				return nil, nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, nil, err
			}
			var ptypes []*CType
			for _, q := range ps {
				ptypes = append(ptypes, q.Ty)
			}
			ty = FuncOf(ty, ptypes, variadic)
			params = ps
			continue
		}
		break
	}
	return ty, params, nil
}

// parseParamList 解析函数参数表
func (p *Parser) parseParamList() ([]*Param, bool, error) {
	var out []*Param
	variadic := false
	if p.at(")") {
		return out, false, nil
	}
	// (void)
	if p.at("void") && p.peekN(1).Text == ")" {
		p.advance()
		return out, false, nil
	}
	for {
		if p.at("...") {
			p.advance()
			variadic = true
			break
		}
		ds, err := p.parseDeclSpecs()
		if err != nil {
			return nil, false, err
		}
		name, ty, _, err := p.parseDeclarator(ds.Base, true)
		if err != nil {
			return nil, false, err
		}
		// 数组参数退化为指针
		if ty.Kind == TyArray {
			ty = PtrTo(ty.Elem)
		}
		out = append(out, &Param{Name: name, Ty: ty})
		if !p.accept(",") {
			break
		}
	}
	return out, variadic, nil
}

// parseTypeName 解析类型名（抽象声明符）
func (p *Parser) parseTypeName() (*CType, error) {
	ds, err := p.parseDeclSpecs()
	if err != nil {
		return nil, err
	}
	_, ty, _, err := p.parseDeclarator(ds.Base, true)
	if err != nil {
		return nil, err
	}
	return ty, nil
}

// ── 顶层声明 ────────────────────────────────────────────

func (p *Parser) parseExternalDecl() error {
	ds, err := p.parseDeclSpecs()
	if err != nil {
		return err
	}
	// struct/union/enum 定义体后直接 ';'
	if p.at(";") {
		p.advance()
		if ds.Base.Kind == TyStruct || ds.Base.Kind == TyUnion {
			p.tu.Decls = append(p.tu.Decls, &RecordDecl{Ty: ds.Base})
		}
		return nil
	}

	for {
		name, ty, params, err := p.parseDeclarator(ds.Base, false)
		if err != nil {
			return err
		}
		if ds.Storage == "typedef" {
			p.declare(&Symbol{Name: name, Ty: ty, IsTypedef: true})
			p.tu.Decls = append(p.tu.Decls, &TypedefDecl{Name: name, Ty: ty})
		} else if ty.Kind == TyFunc && p.at("{") {
			// 函数定义
			fd := &FuncDecl{
				Name:     name,
				Ty:       ty,
				Params:   params,
				IsStatic: ds.Storage == "static",
				Line:     p.peek().Line,
				Boxed:    map[string]bool{},
			}
			p.declare(&Symbol{Name: name, Ty: ty, IsFunc: true, Static: fd.IsStatic, Global: true})
			p.funcs[name] = fd
			// 参数作用域
			p.funcStack = append(p.funcStack, fd)
			p.pushScope()
			for _, q := range params {
				if q.Name == "" {
					continue
				}
				p.declare(&Symbol{Name: q.Name, Ty: q.Ty, Global: false})
			}
			body, err := p.parseBlock()
			p.popScope()
			p.funcStack = p.funcStack[:len(p.funcStack)-1]
			if err != nil {
				return err
			}
			fd.Body = body
			p.tu.Decls = append(p.tu.Decls, fd)
			return nil // 函数定义后本声明结束
		} else {
			// 变量声明
			vd := &VarDecl{
				Name:     name,
				Ty:       ty,
				IsStatic: ds.Storage == "static",
				IsExtern: ds.Storage == "extern",
				Line:     p.peek().Line,
			}
			if p.accept("=") {
				init, err := p.parseInitializer(ty)
				if err != nil {
					return err
				}
				vd.Init = init
			}
			p.declare(&Symbol{Name: name, Ty: ty, Global: true, IsFunc: ty.Kind == TyFunc})
			if ty.Kind == TyFunc {
				// 函数原型
				if exist, ok := p.funcs[name]; ok {
					_ = exist
				} else {
					p.funcs[name] = &FuncDecl{Name: name, Ty: ty, Params: params, IsStatic: ds.Storage == "static"}
				}
			} else {
				p.globals[name] = vd
			}
			p.tu.Decls = append(p.tu.Decls, vd)
		}
		if !p.accept(",") {
			break
		}
	}
	if p.at(";") {
		p.advance()
		return nil
	}
	// 函数体直接跟 { 的情况已在上面处理
	if p.at("{") {
		return p.errf("意外的函数体起始")
	}
	return p.errf("声明后缺少分号，实际 %q", p.peek().Text)
}

// ── 语句 ────────────────────────────────────────────────

func (p *Parser) parseBlock() (*BlockStmt, error) {
	if err := p.expect("{"); err != nil {
		return nil, err
	}
	b := &BlockStmt{}
	p.pushScope()
	defer p.popScope()
	for !p.at("}") && p.peek().Kind != TEOF {
		if p.hasTypeSpecAhead() {
			ds2, err := p.parseDeclSpecs()
			if err != nil {
				return nil, err
			}
			decls, err := p.parseLocalDecls(ds2)
			if err != nil {
				return nil, err
			}
			b.Stmts = append(b.Stmts, &DeclStmt{Decls: decls})
			continue
		}
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		if st != nil {
			b.Stmts = append(b.Stmts, st)
		}
	}
	if err := p.expect("}"); err != nil {
		return nil, err
	}
	return b, nil
}

func (p *Parser) parseLocalDecls(ds *DeclSpec) ([]*VarDecl, error) {
	var out []*VarDecl
	for {
		name, ty, _, err := p.parseDeclarator(ds.Base, false)
		if err != nil {
			return nil, err
		}
		vd := &VarDecl{Name: name, Ty: ty, IsStatic: ds.Storage == "static", Line: p.peek().Line}
		if p.accept("=") {
			init, err := p.parseInitializer(ty)
			if err != nil {
				return nil, err
			}
			vd.Init = init
		}
		p.declare(&Symbol{Name: name, Ty: ty, Static: ds.Storage == "static"})
		if fd := p.curFunc(); fd != nil {
			vd.IsStatic = ds.Storage == "static"
			fd.Locals = append(fd.Locals, vd)
		}
		out = append(out, vd)
		if !p.accept(",") {
			break
		}
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	return out, nil
}

func (p *Parser) curFunc() *FuncDecl {
	if len(p.funcStack) == 0 {
		return nil
	}
	return p.funcStack[len(p.funcStack)-1]
}

func (p *Parser) parseStmt() (Stmt, error) {
	t := p.peek()
	if t.Kind == TKeyword {
		switch t.Text {
		case "if":
			p.advance()
			if err := p.expect("("); err != nil {
				return nil, err
			}
			cond, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			then, err := p.parseStmt()
			if err != nil {
				return nil, err
			}
			var els Stmt
			if p.accept("else") {
				els, err = p.parseStmt()
				if err != nil {
					return nil, err
				}
			}
			return &IfStmt{Cond: cond, Then: then, Else: els}, nil
		case "while":
			p.advance()
			if err := p.expect("("); err != nil {
				return nil, err
			}
			cond, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			body, err := p.parseStmt()
			if err != nil {
				return nil, err
			}
			return &WhileStmt{Cond: cond, Body: body}, nil
		case "do":
			p.advance()
			body, err := p.parseStmt()
			if err != nil {
				return nil, err
			}
			if err := p.expect("while"); err != nil {
				return nil, err
			}
			if err := p.expect("("); err != nil {
				return nil, err
			}
			cond, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			if err := p.expect(";"); err != nil {
				return nil, err
			}
			return &DoWhileStmt{Body: body, Cond: cond}, nil
		case "for":
			return p.parseFor()
		case "switch":
			p.advance()
			if err := p.expect("("); err != nil {
				return nil, err
			}
			x, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(")"); err != nil {
				return nil, err
			}
			body, err := p.parseStmt()
			if err != nil {
				return nil, err
			}
			return &SwitchStmt{X: x, Body: body}, nil
		case "case":
			p.advance()
			x, err := p.parseConstExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(":"); err != nil {
				return nil, err
			}
			st, err := p.parseStmt()
			if err != nil {
				return nil, err
			}
			return &CaseStmt{X: x, Stmt: st}, nil
		case "default":
			p.advance()
			if err := p.expect(":"); err != nil {
				return nil, err
			}
			st, err := p.parseStmt()
			if err != nil {
				return nil, err
			}
			return &CaseStmt{X: nil, Stmt: st}, nil
		case "break":
			p.advance()
			if err := p.expect(";"); err != nil {
				return nil, err
			}
			return &BreakStmt{}, nil
		case "continue":
			p.advance()
			if err := p.expect(";"); err != nil {
				return nil, err
			}
			return &ContinueStmt{}, nil
		case "return":
			p.advance()
			var x Expr
			var err error
			if !p.at(";") {
				x, err = p.parseExpr()
				if err != nil {
					return nil, err
				}
			}
			if err := p.expect(";"); err != nil {
				return nil, err
			}
			return &ReturnStmt{X: x}, nil
		case "goto":
			p.advance()
			if !p.atIdent() {
				return nil, p.errf("goto 缺少标签")
			}
			label := p.advance().Text
			if err := p.expect(";"); err != nil {
				return nil, err
			}
			return &GotoStmt{Label: label}, nil
		}
	}
	if t.Is("{") {
		return p.parseBlock()
	}
	// 标签：ident ':'
	if t.Kind == TIdent && p.peekN(1).Text == ":" {
		name := p.advance().Text
		p.advance()
		st, err := p.parseStmt()
		if err != nil {
			return nil, err
		}
		return &LabelStmt{Label: name, Stmt: st}, nil
	}
	if t.Is(";") {
		p.advance()
		return &BlockStmt{}, nil
	}
	// 表达式语句
	x, err := p.parseExpr()
	if err != nil {
		return nil, err
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	return &ExprStmt{X: x}, nil
}

func (p *Parser) parseFor() (Stmt, error) {
	p.advance() // for
	if err := p.expect("("); err != nil {
		return nil, err
	}
	p.pushScope()
	defer p.popScope()
	var init Stmt
	if p.at(";") {
		p.advance()
	} else if p.hasTypeSpecAhead() {
		ds, err := p.parseDeclSpecs()
		if err != nil {
			return nil, err
		}
		decls, err := p.parseLocalDecls(ds)
		if err != nil {
			return nil, err
		}
		init = &DeclStmt{Decls: decls}
	} else {
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(";"); err != nil {
			return nil, err
		}
		init = &ExprStmt{X: e}
	}
	var cond Expr
	if !p.at(";") {
		var err error
		cond, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	if err := p.expect(";"); err != nil {
		return nil, err
	}
	var post Expr
	if !p.at(")") {
		var err error
		post, err = p.parseExpr()
		if err != nil {
			return nil, err
		}
	}
	if err := p.expect(")"); err != nil {
		return nil, err
	}
	body, err := p.parseStmt()
	if err != nil {
		return nil, err
	}
	return &ForStmt{Init: init, Cond: cond, Post: post, Body: body}, nil
}

// ── 初始化器 ────────────────────────────────────────────

func (p *Parser) parseInitializer(ty *CType) (*Init, error) {
	if p.at("{") {
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
		return init, nil
	}
	x, err := p.parseAssignExpr()
	if err != nil {
		return nil, err
	}
	return &Init{Expr: x}, nil
}
