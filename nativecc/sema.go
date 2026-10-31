package nativecc

import "fmt"

// Sema 语义分析：填表达式类型、解析标识符、计算成员偏移
type Sema struct {
	Globals map[string]*VarDecl
	Funcs   map[string]*FuncDecl
	Enums   map[string]int64
	Cur     *FuncDecl
	Locals  map[string]*VarDecl
	Errs    []string
}

// Check 对整个翻译单元做语义检查
func (s *Sema) Check(tu *TranslationUnit) error {
	s.Globals = map[string]*VarDecl{}
	s.Funcs = map[string]*FuncDecl{}
	s.Enums = map[string]int64{}

	// 第一遍：收集全局符号
	for _, d := range tu.Decls {
		switch v := d.(type) {
		case *FuncDecl:
			if v.Body != nil {
				s.Funcs[v.Name] = v
			} else if _, ok := s.Funcs[v.Name]; !ok {
				s.Funcs[v.Name] = v
			}
		case *VarDecl:
			s.Globals[v.Name] = v
		}
	}

	// 第二遍：检查函数体
	for _, d := range tu.Decls {
		fd, ok := d.(*FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		if err := s.checkFunc(fd); err != nil {
			return err
		}
	}
	return nil
}

func (s *Sema) checkFunc(fd *FuncDecl) error {
	s.Cur = fd
	s.Locals = map[string]*VarDecl{}
	for _, p := range fd.Params {
		if p.Name == "" {
			continue
		}
		s.Locals[p.Name] = &VarDecl{Name: p.Name, Ty: p.Ty}
	}
	// 收集局部变量（含嵌套作用域，后声明覆盖）
	var walk func(st Stmt) error
	walk = func(st Stmt) error {
		switch v := st.(type) {
		case *BlockStmt:
			for _, q := range v.Stmts {
				if err := walk(q); err != nil {
					return err
				}
			}
		case *DeclStmt:
			for _, vd := range v.Decls {
				s.Locals[vd.Name] = vd
			}
			for _, vd := range v.Decls {
				if vd.Init != nil {
					if err := s.checkInitExpr(vd.Init); err != nil {
						return err
					}
				}
			}
		case *IfStmt:
			if _, err := s.typeOf(v.Cond); err != nil {
				return err
			}
			if err := walk(v.Then); err != nil {
				return err
			}
			if v.Else != nil {
				if err := walk(v.Else); err != nil {
					return err
				}
			}
		case *WhileStmt:
			if _, err := s.typeOf(v.Cond); err != nil {
				return err
			}
			return walk(v.Body)
		case *DoWhileStmt:
			if err := walk(v.Body); err != nil {
				return err
			}
			_, err := s.typeOf(v.Cond)
			return err
		case *ForStmt:
			if v.Init != nil {
				if err := walk(v.Init); err != nil {
					return err
				}
			}
			if v.Cond != nil {
				if _, err := s.typeOf(v.Cond); err != nil {
					return err
				}
			}
			if v.Post != nil {
				if _, err := s.typeOf(v.Post); err != nil {
					return err
				}
			}
			return walk(v.Body)
		case *SwitchStmt:
			if _, err := s.typeOf(v.X); err != nil {
				return err
			}
			return walk(v.Body)
		case *CaseStmt:
			if v.X != nil {
				if _, err := s.typeOf(v.X); err != nil {
					return err
				}
			}
			return walk(v.Stmt)
		case *LabelStmt:
			return walk(v.Stmt)
		case *ExprStmt:
			_, err := s.typeOf(v.X)
			return err
		case *ReturnStmt:
			if v.X != nil {
				_, err := s.typeOf(v.X)
				return err
			}
		}
		return nil
	}
	return walk(fd.Body)
}

// typeOf 推导表达式类型（并递归填充子节点）
func (s *Sema) typeOf(e Expr) (*CType, error) {
	if e == nil {
		return TyVoidT, nil
	}
	if e.Type() != nil {
		// 仍需遍历子节点（首次调用时 type 一定为 nil；已填则跳过）
	}
	switch v := e.(type) {
	case *Ident:
		if vd, ok := s.Locals[v.Name]; ok {
			v.SetType(vd.Ty)
			return vd.Ty, nil
		}
		if vd, ok := s.Globals[v.Name]; ok {
			v.SetType(vd.Ty)
			return vd.Ty, nil
		}
		if fd, ok := s.Funcs[v.Name]; ok {
			v.SetType(fd.Ty)
			return fd.Ty, nil
		}
		if val, ok := s.Enums[v.Name]; ok {
			v.SetType(TyIntT)
			_ = val
			return TyIntT, nil
		}
		// 编译器内建
		if Builtins[v.Name] {
			v.SetType(TyIntT)
			return TyIntT, nil
		}
		// 未知标识符：宽松当作 int（外部符号可能在导入表里）
		if _, ok := Imports[v.Name]; ok {
			v.SetType(TyIntT)
			return TyIntT, nil
		}
		return nil, fmt.Errorf("未定义的标识符: %s", v.Name)

	case *IntLit:
		t := TyIntT
		if v.V > 2147483647 || v.V < -2147483648 {
			t = TyLLongT
		}
		v.SetType(t)
		return t, nil
	case *FloatLit:
		v.SetType(TyDoubleT)
		return TyDoubleT, nil
	case *StrLit:
		v.SetType(TyCharPtr)
		return TyCharPtr, nil
	case *CharLit:
		v.SetType(TyIntT)
		return TyIntT, nil

	case *BinaryExpr:
		lt, err := s.typeOf(v.X)
		if err != nil {
			return nil, err
		}
		rt, err := s.typeOf(v.Y)
		if err != nil {
			return nil, err
		}
		var t *CType
		switch v.Op {
		case "+", "-":
			if lt.IsPtr() && rt.IsInteger() {
				t = lt
			} else if rt.IsPtr() && lt.IsInteger() {
				t = rt
			} else if lt.IsPtr() && rt.IsPtr() {
				t = TyLLongT
			} else {
				t = promote(lt, rt)
			}
		case "*", "/", "%", "&", "|", "^", "<<", ">>":
			t = promote(lt, rt)
		case "==", "!=", "<", ">", "<=", ">=", "&&", "||":
			t = TyIntT
		case ",":
			t = rt
		default:
			t = promote(lt, rt)
		}
		v.SetType(t)
		return t, nil

	case *UnaryExpr:
		xt, err := s.typeOf(v.X)
		if err != nil {
			return nil, err
		}
		var t *CType
		switch v.Op {
		case "&":
			t = PtrTo(xt)
		case "*":
			if xt.Kind == TyPtr {
				t = xt.Elem
			} else if xt.Kind == TyArray {
				t = xt.Elem
			} else {
				t = TyIntT
			}
		case "!":
			t = TyIntT
		case "+", "-", "~":
			t = xt
		case "++", "--":
			t = xt
		default:
			t = xt
		}
		v.SetType(t)
		return t, nil

	case *PostfixExpr:
		xt, err := s.typeOf(v.X)
		if err != nil {
			return nil, err
		}
		v.SetType(xt)
		return xt, nil

	case *AssignExpr:
		lt, err := s.typeOf(v.L)
		if err != nil {
			return nil, err
		}
		if _, err := s.typeOf(v.R); err != nil {
			return nil, err
		}
		v.SetType(lt)
		return lt, nil

	case *CondExpr:
		if _, err := s.typeOf(v.Cond); err != nil {
			return nil, err
		}
		tt, err := s.typeOf(v.Then)
		if err != nil {
			return nil, err
		}
		et, err := s.typeOf(v.Else)
		if err != nil {
			return nil, err
		}
		t := promote(tt, et)
		v.SetType(t)
		return t, nil

	case *CallExpr:
		ft, err := s.typeOf(v.Fn)
		if err != nil {
			return nil, err
		}
		for _, a := range v.Args {
			if _, err := s.typeOf(a); err != nil {
				return nil, err
			}
		}
		if ft.Kind == TyFunc {
			v.SetType(ft.Ret)
			return ft.Ret, nil
		}
		if ft.Kind == TyPtr && ft.Elem.Kind == TyFunc {
			v.SetType(ft.Elem.Ret)
			return ft.Elem.Ret, nil
		}
		v.SetType(TyLLongT)
		return TyLLongT, nil

	case *IndexExpr:
		xt, err := s.typeOf(v.X)
		if err != nil {
			return nil, err
		}
		if _, err := s.typeOf(v.Index); err != nil {
			return nil, err
		}
		var t *CType
		switch xt.Kind {
		case TyArray:
			t = xt.Elem
		case TyPtr:
			t = xt.Elem
		default:
			t = TyIntT
		}
		v.SetType(t)
		return t, nil

	case *MemberExpr:
		xt, err := s.typeOf(v.X)
		if err != nil {
			return nil, err
		}
		bt := xt
		if v.Arrow {
			if xt.Kind == TyPtr {
				bt = xt.Elem
			}
		} else if xt.Kind == TyPtr {
			bt = xt.Elem
		}
		if bt == nil || !bt.IsRecord() {
			return nil, fmt.Errorf("成员访问的对象不是结构体/联合体: %s", v.Name)
		}
		for _, f := range bt.Fields {
			if f.Name == v.Name {
				v.Off = f.Offset
				v.SetType(f.Ty)
				return f.Ty, nil
			}
		}
		return nil, fmt.Errorf("%s 没有成员 %s", bt.String(), v.Name)

	case *CastExpr:
		if _, err := s.typeOf(v.X); err != nil {
			return nil, err
		}
		v.SetType(v.To)
		return v.To, nil

	case *SizeofExpr:
		// 递归推导内部表达式类型（sizeof(变量) 需要其类型取大小）
		if v.X != nil && v.X.Type() == nil {
			if _, err := s.typeOf(v.X); err != nil {
				return nil, err
			}
		}
		v.SetType(TyULongT)
		return TyULongT, nil

	case *CompoundLit:
		if v.Init != nil {
			if err := s.checkInit(v.Ty, v.Init); err != nil {
				return nil, err
			}
		}
		v.SetType(v.Ty)
		return v.Ty, nil

	case *BuiltinCall:
		for _, a := range v.Args {
			if _, err := s.typeOf(a); err != nil {
				return nil, err
			}
		}
		v.SetType(TyIntT)
		return TyIntT, nil

	case *InitListExpr:
		for _, a := range v.Items {
			if _, err := s.typeOf(a); err != nil {
				return nil, err
			}
		}
		v.SetType(TyIntT)
		return TyIntT, nil
	}
	return nil, fmt.Errorf("无法推导表达式类型: %T", e)
}

// checkInitExpr 递归检查初始化器中的表达式
func (s *Sema) checkInitExpr(init *Init) error {
	if init == nil {
		return nil
	}
	if init.IsList {
		for _, sub := range init.List {
			if err := s.checkInitExpr(sub); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := s.typeOf(init.Expr)
	return err
}

func (s *Sema) checkInit(t *CType, init *Init) error {
	if init == nil {
		return nil
	}
	if init.IsList {
		for _, sub := range init.List {
			if err := s.checkInit(nil, sub); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := s.typeOf(init.Expr)
	return err
}

// promote 二元算术的类型提升
func promote(a, b *CType) *CType {
	if a == nil {
		return b
	}
	if b == nil {
		return a
	}
	if a.Kind == TyDouble || b.Kind == TyDouble {
		return TyDoubleT
	}
	if a.Kind == TyFloat || b.Kind == TyFloat {
		return TyDoubleT
	}
	if a.Kind == TyPtr {
		return a
	}
	if b.Kind == TyPtr {
		return b
	}
	if a.Kind == TyLongLong || b.Kind == TyLongLong {
		if a.Unsigned || b.Unsigned {
			return TyULLongT
		}
		return TyLLongT
	}
	if a.Kind == TyLong || b.Kind == TyLong {
		if a.Unsigned || b.Unsigned {
			return TyULongT
		}
		return TyLongT
	}
	if a.Unsigned || b.Unsigned {
		return TyUIntT
	}
	return TyIntT
}
