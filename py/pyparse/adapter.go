package pyparse

// adapter.go — 把 gopapy v2（Python 3.14 全语法纯 Go 解析器）产出的 AST
// 转换为 zuzo 内部 py/ast 节点，供现有 py/runtime 直接执行。
//
// 设计原则：
//  1. 只做「形状转换」，不做语义改写：数值保留原始十进制串，字符串保留解码结果。
//  2. 任何 gopapy 有而 zuzo 运行时没有对应节点的构造，返回带类型名的错误，
//     由调用方（py/runtime 的解析入口）回退到内置 parser —— 双轨保证零回归。
//  3. 位置信息只保留行号（zuzo py/ast 的 Pos 只有 L 字段）。

import (
	"fmt"
	"strconv"

	"github.com/tamnd/gopapy/v2/parser2"

	pyast "zuzo/py/ast"
)

// ParseFile 用 gopapy 解析 Python 源码并转换为 zuzo 内部 AST。
func ParseFile(filename, src string) (*pyast.Module, error) {
	mod, err := parser2.ParseFile(filename, src)
	if err != nil {
		return nil, err
	}
	c := &conv{}
	return c.module(mod)
}

// ParseExpression 解析单个表达式（REPL / eval 用）。
func ParseExpression(src string) (pyast.Expr, error) {
	e, err := parser2.ParseExpression(src)
	if err != nil {
		return nil, err
	}
	c := &conv{}
	return c.expr(e), c.err
}

type conv struct {
	err error
}

func (c *conv) fail(format string, a ...any) pyast.Stmt {
	if c.err == nil {
		c.err = fmt.Errorf(format, a...)
	}
	return nil
}

func (c *conv) failExpr(format string, a ...any) pyast.Expr {
	if c.err == nil {
		c.err = fmt.Errorf(format, a...)
	}
	return nil
}

func pos(p parser2.Pos) pyast.Pos { return pyast.Pos{L: p.Line} }

// ==================== 模块与语句 ====================

func (c *conv) module(m *parser2.Module) (*pyast.Module, error) {
	out := &pyast.Module{}
	for _, s := range m.Body {
		st := c.stmt(s)
		if c.err != nil {
			return nil, c.err
		}
		if st == nil {
			continue
		}
		out.Body = append(out.Body, st)
	}
	return out, nil
}

func (c *conv) stmts(list []parser2.Stmt) []pyast.Stmt {
	out := make([]pyast.Stmt, 0, len(list))
	for _, s := range list {
		st := c.stmt(s)
		if c.err != nil {
			return out
		}
		if st == nil {
			continue
		}
		out = append(out, st)
	}
	return out
}

func (c *conv) stmt(s parser2.Stmt) pyast.Stmt {
	switch n := s.(type) {
	case *parser2.ExprStmt:
		return &pyast.ExprStmt{Pos: pos(n.P), X: c.expr(n.Value)}

	case *parser2.Assign:
		targets := make([]pyast.Expr, 0, len(n.Targets))
		for _, t := range n.Targets {
			targets = append(targets, c.expr(t))
		}
		return &pyast.Assign{Pos: pos(n.P), Targets: targets, Value: c.expr(n.Value)}

	case *parser2.AugAssign:
		return &pyast.AugAssign{Pos: pos(n.P), Target: c.expr(n.Target), Op: binOp(n.Op), Value: c.expr(n.Value)}

	case *parser2.AnnAssign:
		return &pyast.AnnAssign{Pos: pos(n.P), Target: c.expr(n.Target), Annotation: c.expr(n.Annotation), Value: c.expr(n.Value)}

	case *parser2.If:
		return &pyast.If{Pos: pos(n.P), Cond: c.expr(n.Test), Body: c.stmts(n.Body), Orelse: c.stmts(n.Orelse)}

	case *parser2.While:
		return &pyast.While{Pos: pos(n.P), Cond: c.expr(n.Test), Body: c.stmts(n.Body), Orelse: c.stmts(n.Orelse)}

	case *parser2.For:
		return &pyast.For{Pos: pos(n.P), Target: c.expr(n.Target), Iter: c.expr(n.Iter), Body: c.stmts(n.Body), Orelse: c.stmts(n.Orelse)}

	case *parser2.AsyncFor:
		return &pyast.For{Pos: pos(n.P), Target: c.expr(n.Target), Iter: c.expr(n.Iter), Body: c.stmts(n.Body), Orelse: c.stmts(n.Orelse), IsAsync: true}

	case *parser2.FunctionDef:
		return &pyast.FuncDef{Pos: pos(n.P), Name: n.Name, Params: c.params(n.Args), Body: c.stmts(n.Body), Decorators: c.exprs(n.DecoratorList), Returns: c.expr(n.Returns)}

	case *parser2.AsyncFunctionDef:
		return &pyast.FuncDef{Pos: pos(n.P), Name: n.Name, Params: c.params(n.Args), Body: c.stmts(n.Body), Decorators: c.exprs(n.DecoratorList), Returns: c.expr(n.Returns), IsAsync: true}

	case *parser2.ClassDef:
		kws := make([]pyast.Keyword, 0, len(n.Keywords))
		for _, k := range n.Keywords {
			kws = append(kws, pyast.Keyword{Name: k.Arg, Value: c.expr(k.Value)})
		}
		return &pyast.ClassDef{Pos: pos(n.P), Name: n.Name, Bases: c.exprs(n.Bases), Keywords: kws, Body: c.stmts(n.Body), Decorators: c.exprs(n.DecoratorList)}

	case *parser2.Return:
		return &pyast.Return{Pos: pos(n.P), Value: c.expr(n.Value)}

	case *parser2.Break:
		return &pyast.Break{Pos: pos(n.P)}

	case *parser2.Continue:
		return &pyast.Continue{Pos: pos(n.P)}

	case *parser2.Pass:
		return &pyast.Pass{Pos: pos(n.P)}

	case *parser2.Try:
		handlers := make([]*pyast.ExceptHandler, 0, len(n.Handlers))
		for _, h := range n.Handlers {
			handlers = append(handlers, &pyast.ExceptHandler{Pos: pos(h.P), Type: c.expr(h.Type), Name: h.Name, Body: c.stmts(h.Body)})
		}
		return &pyast.Try{Pos: pos(n.P), Body: c.stmts(n.Body), Handlers: handlers, Orelse: c.stmts(n.Orelse), Finally: c.stmts(n.Finalbody)}

	case *parser2.Raise:
		return &pyast.Raise{Pos: pos(n.P), Exc: c.expr(n.Exc), Cause: c.expr(n.Cause)}

	case *parser2.With:
		return &pyast.With{Pos: pos(n.P), Items: c.withItems(n.Items), Body: c.stmts(n.Body)}

	case *parser2.AsyncWith:
		return &pyast.With{Pos: pos(n.P), Items: c.withItems(n.Items), Body: c.stmts(n.Body), IsAsync: true}

	case *parser2.Import:
		names := make([]pyast.Alias, 0, len(n.Names))
		for _, a := range n.Names {
			names = append(names, pyast.Alias{Name: a.Name, As: a.Asname})
		}
		return &pyast.Import{Pos: pos(n.P), Names: names}

	case *parser2.ImportFrom:
		names := make([]pyast.Alias, 0, len(n.Names))
		for _, a := range n.Names {
			names = append(names, pyast.Alias{Name: a.Name, As: a.Asname})
		}
		return &pyast.ImportFrom{Pos: pos(n.P), Module: n.Module, Names: names, Level: n.Level}

	case *parser2.Global:
		return &pyast.Global{Pos: pos(n.P), Names: n.Names}

	case *parser2.Nonlocal:
		return &pyast.Nonlocal{Pos: pos(n.P), Names: n.Names}

	case *parser2.Delete:
		targets := make([]pyast.Expr, 0, len(n.Targets))
		for _, t := range n.Targets {
			targets = append(targets, c.expr(t))
		}
		return &pyast.Del{Pos: pos(n.P), Targets: targets}

	case *parser2.Assert:
		return &pyast.Assert{Pos: pos(n.P), Test: c.expr(n.Test), Msg: c.expr(n.Msg)}

	case *parser2.Match:
		cases := make([]*pyast.MatchCase, 0, len(n.Cases))
		for _, cs := range n.Cases {
			cases = append(cases, &pyast.MatchCase{Pos: pos(cs.P), Pattern: c.pattern(cs.Pattern), Guard: c.expr(cs.Guard), Body: c.stmts(cs.Body)})
		}
		return &pyast.Match{Pos: pos(n.P), Subject: c.expr(n.Subject), Cases: cases}

	case *parser2.TypeAlias:
		return c.fail("不支持的 Python 语法：type 别名（TypeAlias）")

	default:
		return c.fail("不支持的 Python 语句：%T", s)
	}
}

func (c *conv) withItems(items []*parser2.WithItem) []pyast.WithItem {
	out := make([]pyast.WithItem, 0, len(items))
	for _, it := range items {
		out = append(out, pyast.WithItem{Context: c.expr(it.ContextExpr), Vars: c.expr(it.OptionalVars)})
	}
	return out
}

func (c *conv) exprs(list []parser2.Expr) []pyast.Expr {
	if len(list) == 0 {
		return nil
	}
	out := make([]pyast.Expr, 0, len(list))
	for _, e := range list {
		out = append(out, c.expr(e))
	}
	return out
}

func (c *conv) params(a *parser2.Arguments) *pyast.Params {
	p := &pyast.Params{}
	if a == nil {
		return p
	}

	positional := make([]*parser2.Arg, 0, len(a.PosOnly)+len(a.Args))
	positional = append(positional, a.PosOnly...)
	positional = append(positional, a.Args...)

	// Defaults 对齐的是「尾部 N 个位置参数」
	defStart := len(positional) - len(a.Defaults)
	for i, arg := range positional {
		var def parser2.Expr
		if i >= defStart && len(a.Defaults) > 0 {
			def = a.Defaults[i-defStart]
		}
		p.Args = append(p.Args, &pyast.Arg{Name: arg.Name, Annotation: c.expr(arg.Annotation), Default: c.expr(def)})
	}

	if a.Vararg != nil {
		p.Vararg = a.Vararg.Name
	}
	for i, arg := range a.KwOnly {
		var def parser2.Expr
		if i < len(a.KwOnlyDef) {
			def = a.KwOnlyDef[i]
		}
		p.Kwonly = append(p.Kwonly, &pyast.Arg{Name: arg.Name, Annotation: c.expr(arg.Annotation), Default: c.expr(def)})
	}
	if a.Kwarg != nil {
		p.Kwarg = a.Kwarg.Name
	}
	return p
}

// ==================== 表达式 ====================

func (c *conv) expr(e parser2.Expr) pyast.Expr {
	if e == nil {
		return nil
	}
	switch n := e.(type) {
	case *parser2.Constant:
		return c.constant(n)

	case *parser2.Name:
		return &pyast.Name{Pos: pos(n.P), Id: n.Id}

	case *parser2.BinOp:
		return &pyast.BinOp{Pos: pos(n.P), Left: c.expr(n.Left), Op: binOp(n.Op), Right: c.expr(n.Right)}

	case *parser2.UnaryOp:
		return &pyast.UnaryOp{Pos: pos(n.P), Op: unaryOp(n.Op), Operand: c.expr(n.Operand)}

	case *parser2.BoolOp:
		return &pyast.BoolOp{Pos: pos(n.P), Op: boolOp(n.Op), Values: c.exprs(n.Values)}

	case *parser2.Compare:
		return &pyast.Compare{Pos: pos(n.P), Left: c.expr(n.Left), Ops: cmpOps(n.Ops), Comparators: c.exprs(n.Comparators)}

	case *parser2.Call:
		kws := make([]pyast.Keyword, 0, len(n.Keywords))
		for _, k := range n.Keywords {
			kws = append(kws, pyast.Keyword{Name: k.Arg, Value: c.expr(k.Value)})
		}
		return &pyast.Call{Pos: pos(n.P), Func: c.expr(n.Func), Args: c.exprs(n.Args), Keywords: kws}

	case *parser2.Attribute:
		return &pyast.Attribute{Pos: pos(n.P), Value: c.expr(n.Value), Attr: n.Attr}

	case *parser2.Subscript:
		return &pyast.Subscript{Pos: pos(n.P), Value: c.expr(n.Value), Slice: c.expr(n.Slice)}

	case *parser2.Slice:
		return &pyast.SliceExpr{Pos: pos(n.P), Lower: c.expr(n.Lower), Upper: c.expr(n.Upper), Step: c.expr(n.Step)}

	case *parser2.List:
		return &pyast.List{Pos: pos(n.P), Elements: c.exprs(n.Elts)}

	case *parser2.Tuple:
		return &pyast.Tuple{Pos: pos(n.P), Elements: c.exprs(n.Elts)}

	case *parser2.Set:
		return &pyast.Set{Pos: pos(n.P), Elements: c.exprs(n.Elts)}

	case *parser2.Dict:
		keys := make([]pyast.Expr, 0, len(n.Keys))
		vals := make([]pyast.Expr, 0, len(n.Values))
		for i := range n.Values {
			var k parser2.Expr
			if i < len(n.Keys) {
				k = n.Keys[i]
			}
			keys = append(keys, c.expr(k)) // nil = ** 解包
			vals = append(vals, c.expr(n.Values[i]))
		}
		return &pyast.Dict{Pos: pos(n.P), Keys: keys, Values: vals}

	case *parser2.ListComp:
		return &pyast.ListComp{Pos: pos(n.P), Elt: c.expr(n.Elt), Gens: c.gens(n.Gens)}

	case *parser2.SetComp:
		return &pyast.SetComp{Pos: pos(n.P), Elt: c.expr(n.Elt), Gens: c.gens(n.Gens)}

	case *parser2.DictComp:
		return &pyast.DictComp{Pos: pos(n.P), Key: c.expr(n.Key), Value: c.expr(n.Value), Gens: c.gens(n.Gens)}

	case *parser2.GeneratorExp:
		return &pyast.GenExp{Pos: pos(n.P), Elt: c.expr(n.Elt), Gens: c.gens(n.Gens)}

	case *parser2.Lambda:
		return &pyast.Lambda{Pos: pos(n.P), Params: c.params(n.Args), Body: c.expr(n.Body)}

	case *parser2.IfExp:
		return &pyast.IfExp{Pos: pos(n.P), Test: c.expr(n.Test), Body: c.expr(n.Body), Orelse: c.expr(n.OrElse)}

	case *parser2.Starred:
		return &pyast.Starred{Pos: pos(n.P), Value: c.expr(n.Value)}

	case *parser2.NamedExpr:
		target, _ := c.expr(n.Target).(*pyast.Name)
		if target == nil {
			return c.failExpr("海象运算符的目标必须是标识符")
		}
		return &pyast.NamedExpr{Pos: pos(n.P), Target: target, Value: c.expr(n.Value)}

	case *parser2.Await:
		return &pyast.Await{Pos: pos(n.P), Value: c.expr(n.Value)}

	case *parser2.Yield:
		return &pyast.Yield{Pos: pos(n.P), Value: c.expr(n.Value)}

	case *parser2.YieldFrom:
		return &pyast.Yield{Pos: pos(n.P), Value: c.expr(n.Value), From: true}

	case *parser2.JoinedStr:
		return c.joinedStr(n)

	case *parser2.FormattedValue:
		return c.formattedValue(n)

	case *parser2.TemplateStr:
		return c.failExpr("不支持的 Python 语法：t-string（PEP 750）")

	default:
		return c.failExpr("不支持的 Python 表达式：%T", e)
	}
}

func (c *conv) gens(list []*parser2.Comprehension) []pyast.Comp {
	out := make([]pyast.Comp, 0, len(list))
	for _, g := range list {
		out = append(out, pyast.Comp{Target: c.expr(g.Target), Iter: c.expr(g.Iter), Ifs: c.exprs(g.Ifs), IsAsync: g.IsAsync})
	}
	return out
}

// constant 把 gopapy 的统一 Constant 还原成 zuzo 的四类字面量节点
func (c *conv) constant(n *parser2.Constant) pyast.Expr {
	switch n.Kind {
	case "int":
		v, ok := n.Value.(int64)
		if !ok {
			return c.failExpr("整数字面量类型异常：%T", n.Value)
		}
		return &pyast.Number{Pos: pos(n.P), Value: strconv.FormatInt(v, 10)}
	case "float":
		v, ok := n.Value.(float64)
		if !ok {
			return c.failExpr("浮点字面量类型异常：%T", n.Value)
		}
		return &pyast.Number{Pos: pos(n.P), IsFloat: true, Value: strconv.FormatFloat(v, 'g', -1, 64)}
	case "str":
		v, _ := n.Value.(string)
		return &pyast.String{Pos: pos(n.P), Value: v}
	case "bytes":
		v, _ := n.Value.(string)
		return &pyast.String{Pos: pos(n.P), Value: v, IsBytes: true}
	case "None":
		return &pyast.Name{Pos: pos(n.P), Id: "None"}
	case "True":
		return &pyast.Name{Pos: pos(n.P), Id: "True"}
	case "False":
		return &pyast.Name{Pos: pos(n.P), Id: "False"}
	case "Ellipsis":
		return &pyast.Ellipsis{Pos: pos(n.P)}
	default:
		return c.failExpr("不支持的常量子类型：%s", n.Kind)
	}
}

// joinedStr 把 gopapy 的 JoinedStr/FormattedValue 转成 zuzo 的 FString 部件序列
func (c *conv) joinedStr(n *parser2.JoinedStr) pyast.Expr {
	out := &pyast.FString{Pos: pos(n.P)}
	for _, v := range n.Values {
		switch part := v.(type) {
		case *parser2.Constant:
			if s, ok := part.Value.(string); ok {
				out.Parts = append(out.Parts, pyast.FStringPart{Literal: s, IsLiteral: true})
			}
		case *parser2.FormattedValue:
			fp := pyast.FStringPart{Expr: c.expr(part.Value)}
			if part.Conversion > 0 {
				fp.Conversion = byte(part.Conversion)
			}
			if spec, ok := part.FormatSpec.(*parser2.JoinedStr); ok {
				fp.Format = c.joinFormat(spec)
			}
			out.Parts = append(out.Parts, fp)
		}
	}
	return out
}

// joinFormat 提取格式说明符文本（zuzo FStringPart.Format 是字符串）
func (c *conv) joinFormat(n *parser2.JoinedStr) string {
	s := ""
	for _, v := range n.Values {
		if p, ok := v.(*parser2.Constant); ok {
			if str, ok := p.Value.(string); ok {
				s += str
			}
		}
	}
	return s
}

func (c *conv) formattedValue(n *parser2.FormattedValue) pyast.Expr {
	out := &pyast.FString{Pos: pos(n.P)}
	fp := pyast.FStringPart{Expr: c.expr(n.Value)}
	if n.Conversion > 0 {
		fp.Conversion = byte(n.Conversion)
	}
	if spec, ok := n.FormatSpec.(*parser2.JoinedStr); ok {
		fp.Format = c.joinFormat(spec)
	}
	out.Parts = append(out.Parts, fp)
	return out
}

// ==================== match 模式 ====================

func (c *conv) pattern(p parser2.Pattern) pyast.Pattern {
	switch n := p.(type) {
	case *parser2.MatchAs:
		if n.Pattern == nil {
			if n.Name == "_" || n.Name == "" {
				return &pyast.PWildcard{Pos: pos(n.P)}
			}
			return &pyast.PCapture{Pos: pos(n.P), Name: n.Name}
		}
		return &pyast.PAs{Pos: pos(n.P), Pat: c.pattern(n.Pattern), Name: n.Name}

	case *parser2.MatchValue:
		if _, isAttr := n.Value.(*parser2.Attribute); isAttr {
			return &pyast.PValue{Pos: pos(n.P), Expr: c.expr(n.Value)}
		}
		return &pyast.PLiteral{Pos: pos(n.P), Expr: c.expr(n.Value)}

	case *parser2.MatchSingleton:
		var id string
		switch n.Value {
		case nil:
			id = "None"
		case true:
			id = "True"
		case false:
			id = "False"
		default:
			return &pyast.PWildcard{Pos: pos(n.P)}
		}
		return &pyast.PLiteral{Pos: pos(n.P), Expr: &pyast.Name{Pos: pos(n.P), Id: id}}

	case *parser2.MatchSequence:
		seq := &pyast.PSequence{Pos: pos(n.P)}
		for _, sub := range n.Patterns {
			if star, ok := sub.(*parser2.MatchStar); ok {
				seq.Star = star.Name
				continue
			}
			seq.Items = append(seq.Items, c.pattern(sub))
		}
		return seq

	case *parser2.MatchMapping:
		m := &pyast.PMapping{Pos: pos(n.P), Rest: n.Rest}
		for i, k := range n.Keys {
			m.Keys = append(m.Keys, c.expr(k))
			if i < len(n.Patterns) {
				m.Vals = append(m.Vals, c.pattern(n.Patterns[i]))
			}
		}
		return m

	case *parser2.MatchClass:
		cls := &pyast.PClass{Pos: pos(n.P), Cls: c.expr(n.Cls)}
		for _, sub := range n.Patterns {
			cls.Positional = append(cls.Positional, c.pattern(sub))
		}
		for i, attr := range n.KwdAttrs {
			if i < len(n.KwdPatterns) {
				cls.Keywords = append(cls.Keywords, &pyast.KeywordPat{Name: attr, Pat: c.pattern(n.KwdPatterns[i])})
			}
		}
		return cls

	case *parser2.MatchOr:
		or := &pyast.POr{Pos: pos(n.P)}
		for _, sub := range n.Patterns {
			or.Alts = append(or.Alts, c.pattern(sub))
		}
		return or

	case *parser2.MatchStar:
		return &pyast.PCapture{Pos: pos(n.P), Name: n.Name}

	default:
		if c.err == nil {
			c.err = fmt.Errorf("不支持的 match 模式：%T", p)
		}
		return nil
	}
}