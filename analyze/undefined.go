package analyze

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/lexer"
	"zuzo/parser"
)

// ============================================================
// 未定义标识符静态检查
// ============================================================
//
// 背景：zuzo 是动态语言，`-c` 之前只做类型推断，完全不查标识符是否定义，
// 于是 `打印(某不存在函数(1))` 在静态检查下是「通过」的，只有真的跑起来才会炸。
//
// 口径（保守，宁缺勿错）：
//   - 一个名字只要在「文件任意位置」被定义过，就视为已知：
//     变量声明、函数声明与形参、for/for-in 变量、catch 变量、定义字段、导入别名、
//     以及任何赋值语句左侧（动态语言允许后续赋值出现）。
//   - 再加上内置函数表（由调用方传入，来自 evaluator.BuiltinNames）。
//   - 只有在全文件从未出现定义、也不是内置时才报，避免作用域误判导致的假阳。
//
// 这个口径抓不到「作用域用错」，但能稳定抓到拼写错误与漏定义的函数，
// 而这正是运行时才炸的那一类。

// Issue 一条静态检查发现
type Issue struct {
	Code    string
	Message string
	Line    int
	Column  int
}

type collector struct {
	defined map[string]bool
	used    map[string]*ast.Identifier // 名字 → 首次使用节点
	order   []string                   // 首次使用顺序
}

func newCollector() *collector {
	return &collector{
		defined: make(map[string]bool, 64),
		used:    make(map[string]*ast.Identifier, 64),
	}
}

func (c *collector) define(name string) {
	if name != "" {
		c.defined[name] = true
	}
}

func (c *collector) use(id *ast.Identifier) {
	if id == nil || id.Value == "" || id.Value == "_" {
		return
	}
	if _, ok := c.used[id.Value]; !ok {
		c.used[id.Value] = id
		c.order = append(c.order, id.Value)
	}
}

// Undefined 返回「全文件从未定义且不是内置」的标识符清单（按首次出现去重）
func Undefined(prog *ast.Program, builtins map[string]bool) []Issue {
	if prog == nil {
		return nil
	}
	c := newCollector()
	// 第一遍：收集所有定义
	for _, s := range prog.Statements {
		c.collectDefsStmt(s)
	}
	// 第二遍：收集所有使用
	for _, s := range prog.Statements {
		c.collectUsesStmt(s)
	}
	return c.report(builtins)
}

// ---------------- 定义收集 ----------------

func (c *collector) collectDefsStmt(s ast.Statement) {
	switch n := s.(type) {
	case *ast.LetStatement:
		if n.Name != nil {
			c.define(n.Name.Value)
		}
		c.collectDefsExpr(n.Value)
	case *ast.ReturnStatement:
		c.collectDefsExpr(n.ReturnValue)
	case *ast.ExpressionStatement:
		c.collectDefsExpr(n.Expression)
	case *ast.BlockStatement:
		for _, st := range n.Statements {
			c.collectDefsStmt(st)
		}
	case *ast.WhileStatement:
		c.collectDefsExpr(n.Condition)
		if n.Body != nil {
			c.collectDefsStmt(n.Body)
		}
	case *ast.ForStatement:
		c.collectDefsExpr(n.Init)
		c.collectDefsExpr(n.Condition)
		c.collectDefsExpr(n.Update)
		if n.Body != nil {
			c.collectDefsStmt(n.Body)
		}
	case *ast.ForInStatement:
		c.define(n.VarName)
		c.define(n.IndexName)
		c.collectDefsExpr(n.Iterable)
		c.collectDefsExpr(n.EndExpr)
		c.collectDefsExpr(n.Guard)
		if n.Body != nil {
			c.collectDefsStmt(n.Body)
		}
	case *ast.ImportStatement:
		if n.Alias != "" {
			c.define(n.Alias)
		} else if n.IsName {
			c.define(strings.SplitN(n.Path, ".", 2)[0])
		}
	case *ast.DefineStatement:
		c.define(n.Name)
		for _, f := range n.Fields {
			c.collectDefsExpr(f.Value)
		}
	case *ast.TryCatchStatement:
		if n.Body != nil {
			c.collectDefsStmt(n.Body)
		}
		c.define(n.ErrVar)
		if n.Handler != nil {
			c.collectDefsStmt(n.Handler)
		}
	}
}

func (c *collector) collectDefsExpr(e ast.Expression) {
	switch n := e.(type) {
	case nil:
		return
	case *ast.FunctionLiteral:
		c.define(n.Name)
		for _, p := range n.Parameters {
			c.define(p.Value)
		}
		if n.Body != nil {
			c.collectDefsStmt(n.Body)
		}
	case *ast.AssignExpression:
		if n.Name != nil {
			c.define(n.Name.Value)
		}
		if n.IndexTarget != nil {
			c.collectDefsExpr(n.IndexTarget)
		}
		c.collectDefsExpr(n.Value)
	case *ast.IfExpression:
		c.collectDefsExpr(n.Condition)
		if n.Consequence != nil {
			c.collectDefsStmt(n.Consequence)
		}
		if n.Alternative != nil {
			c.collectDefsStmt(n.Alternative)
		}
	case *ast.CallExpression:
		c.collectDefsExpr(n.Function)
		for _, a := range n.Arguments {
			c.collectDefsExpr(a)
		}
		for _, a := range n.NamedArgs {
			c.collectDefsExpr(a.Value)
		}
	case *ast.InfixExpression:
		c.collectDefsExpr(n.Left)
		c.collectDefsExpr(n.Right)
	case *ast.PrefixExpression:
		c.collectDefsExpr(n.Right)
	case *ast.IndexExpression:
		c.collectDefsExpr(n.Left)
		c.collectDefsExpr(n.Index)
	case *ast.ArrayLiteral:
		for _, el := range n.Elements {
			c.collectDefsExpr(el)
		}
	case *ast.HashLiteral:
		for k, v := range n.Pairs {
			c.collectDefsExpr(k)
			c.collectDefsExpr(v)
		}
	case *ast.FStringLiteral:
		// f-string 内部表达式是子 AST
		for _, sub := range n.Parts {
			c.collectDefsExpr(sub)
		}
	case *ast.ErrorExpression:
		// 解析错误占位节点，没有子表达式
	}
}

// ---------------- 使用收集 ----------------

func (c *collector) collectUsesStmt(s ast.Statement) {
	switch n := s.(type) {
	case *ast.LetStatement:
		c.collectUsesExpr(n.Value)
	case *ast.ReturnStatement:
		c.collectUsesExpr(n.ReturnValue)
	case *ast.ExpressionStatement:
		c.collectUsesExpr(n.Expression)
	case *ast.BlockStatement:
		for _, st := range n.Statements {
			c.collectUsesStmt(st)
		}
	case *ast.WhileStatement:
		c.collectUsesExpr(n.Condition)
		if n.Body != nil {
			c.collectUsesStmt(n.Body)
		}
	case *ast.ForStatement:
		c.collectUsesExpr(n.Init)
		c.collectUsesExpr(n.Condition)
		c.collectUsesExpr(n.Update)
		if n.Body != nil {
			c.collectUsesStmt(n.Body)
		}
	case *ast.ForInStatement:
		c.collectUsesExpr(n.Iterable)
		c.collectUsesExpr(n.EndExpr)
		c.collectUsesExpr(n.Guard)
		if n.Body != nil {
			c.collectUsesStmt(n.Body)
		}
	case *ast.DefineStatement:
		for _, f := range n.Fields {
			c.collectUsesExpr(f.Value)
		}
	case *ast.TryCatchStatement:
		if n.Body != nil {
			c.collectUsesStmt(n.Body)
		}
		if n.Handler != nil {
			c.collectUsesStmt(n.Handler)
		}
	}
}

func (c *collector) collectUsesExpr(e ast.Expression) {
	switch n := e.(type) {
	case nil:
		return
	case *ast.Identifier:
		c.use(n)
	case *ast.FunctionLiteral:
		if n.Body != nil {
			c.collectUsesStmt(n.Body)
		}
	case *ast.AssignExpression:
		// 赋值目标不算「使用」（它们属于定义侧）
		if n.IndexTarget != nil {
			c.collectUsesExpr(n.IndexTarget)
		}
		c.collectUsesExpr(n.Value)
	case *ast.IfExpression:
		c.collectUsesExpr(n.Condition)
		if n.Consequence != nil {
			c.collectUsesStmt(n.Consequence)
		}
		if n.Alternative != nil {
			c.collectUsesStmt(n.Alternative)
		}
	case *ast.CallExpression:
		c.collectUsesExpr(n.Function)
		for _, a := range n.Arguments {
			c.collectUsesExpr(a)
		}
		for _, a := range n.NamedArgs {
			c.collectUsesExpr(a.Value)
		}
	case *ast.InfixExpression:
		c.collectUsesExpr(n.Left)
		c.collectUsesExpr(n.Right)
	case *ast.PrefixExpression:
		c.collectUsesExpr(n.Right)
	case *ast.IndexExpression:
		c.collectUsesExpr(n.Left)
		c.collectUsesExpr(n.Index)
	case *ast.ArrayLiteral:
		for _, el := range n.Elements {
			c.collectUsesExpr(el)
		}
	case *ast.HashLiteral:
		for k, v := range n.Pairs {
			c.collectUsesExpr(k)
			c.collectUsesExpr(v)
		}
	case *ast.FStringLiteral:
		for _, sub := range n.Parts {
			c.collectUsesExpr(sub)
		}
	case *ast.ErrorExpression:
		// 解析错误占位节点，没有子表达式
	}
}

// UndefinedWithImports 在 Undefined 的基础上，把「被导入文件里的顶层定义」也计入已定义。
// baseDir = 当前文件所在目录（用于解析相对导入路径）。
// 只做一层到多层导入的递归加载；解析失败的文件静默跳过（语法错误另有诊断负责）。
func UndefinedWithImports(prog *ast.Program, builtins map[string]bool, baseDir string) []Issue {
	if prog == nil {
		return nil
	}
	c := newCollector()
	seen := make(map[string]bool, 8)
	collectDefsWithImports(c, prog, baseDir, seen, 0)
	for _, s := range prog.Statements {
		c.collectUsesStmt(s)
	}
	return c.report(builtins)
}

// collectDefsWithImports 递归收集本文件与被导入文件的顶层定义
func collectDefsWithImports(c *collector, prog *ast.Program, baseDir string, seen map[string]bool, depth int) {
	if prog == nil || depth > 32 {
		return
	}
	for _, s := range prog.Statements {
		c.collectDefsStmt(s)
	}
	for _, s := range prog.Statements {
		imp, ok := s.(*ast.ImportStatement)
		if !ok || imp.Path == "" {
			continue
		}
		p := imp.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		abs, err := filepath.Abs(p)
		if err != nil || seen[abs] {
			continue
		}
		seen[abs] = true
		sub := parseFileQuiet(abs)
		if sub == nil {
			continue
		}
		// 导入文件里的顶层「变量/函数」在直接导入时并入当前作用域
		collectDefsWithImports(c, sub, filepath.Dir(abs), seen, depth+1)
	}
}

// parseFileQuiet 解析一个 .hy 文件；失败返回 nil
func parseFileQuiet(path string) *ast.Program {
	ext := filepath.Ext(path)
	if ext == "" {
		// 导入 "math_utils" 这类省略扩展名的写法：补 .hy 再试
		if _, err := os.Stat(path + ".hy"); err == nil {
			path += ".hy"
			ext = ".hy"
		}
	}
	if !strings.EqualFold(ext, ".hy") {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	p := parser.New(lexer.New(string(data)))
	prog := p.ParseProgram()
	if p.Diagnostics().HasErrors() {
		return nil
	}
	return prog
}

// report 生成最终问题列表
func (c *collector) report(builtins map[string]bool) []Issue {
	var issues []Issue
	for _, name := range c.order {
		if c.defined[name] || builtins[name] {
			continue
		}
		id := c.used[name]
		issues = append(issues, Issue{
			Code:    diagnostic.ErrUndefinedVar.Code,
			Message: fmt.Sprintf("未定义的标识符: %s（全文件未找到定义，也不是内置函数）", name),
			Line:    id.Token.Line,
			Column:  id.Token.Column,
		})
	}
	sort.SliceStable(issues, func(i, j int) bool {
		if issues[i].Line != issues[j].Line {
			return issues[i].Line < issues[j].Line
		}
		return issues[i].Column < issues[j].Column
	})
	return issues
}

// ============================================================
// 与内置函数同名的定义（E015 警告）
// ============================================================

// ShadowedBuiltins 返回与内置函数同名的顶层定义。
// 解释器里用户定义优先，但 C 后端生成代码时内置优先 → 两条链行为不一致。
// 这是「最难查的一类坑」：解释器跑得好好的，编译成 exe 就报「参数必须是字符串」。
func ShadowedBuiltins(prog *ast.Program, builtins map[string]bool) []Issue {
	if prog == nil {
		return nil
	}
	var issues []Issue
	report := func(name string, line, col int) {
		if !builtins[name] {
			return
		}
		issues = append(issues, Issue{
			Code:    diagnostic.ErrShadowBuiltin.Code,
			Message: fmt.Sprintf("自定义的 %s 与内置函数同名：解释器会用你的版本，C 后端会优先用内置", name),
			Line:    line,
			Column:  col,
		})
	}
	for _, st := range prog.Statements {
		switch n := st.(type) {
		case *ast.ExpressionStatement:
			if fl, ok := n.Expression.(*ast.FunctionLiteral); ok && fl.Name != "" {
				report(fl.Name, fl.Token.Line, fl.Token.Column)
			}
		case *ast.LetStatement:
			if fl, ok := n.Value.(*ast.FunctionLiteral); ok && fl.Name != "" {
				report(fl.Name, fl.Token.Line, fl.Token.Column)
			} else if n.Name != nil {
				report(n.Name.Value, n.Name.Token.Line, n.Name.Token.Column)
			}
		}
	}
	return issues
}

// ============================================================
// 导入文件存在性（E301）与字面量除零（E202/E203）
// ============================================================

// ImportErrors 检查「导入 "x"」指向的文件是否存在（编译期，不执行程序）
func ImportErrors(prog *ast.Program, baseDir string) []Issue {
	if prog == nil {
		return nil
	}
	var issues []Issue
	for _, st := range prog.Statements {
		imp, ok := st.(*ast.ImportStatement)
		if !ok || imp.Path == "" {
			continue
		}
		p := imp.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		found := false
		for _, cand := range []string{abs, abs + ".hy", abs + ".z"} {
			if st, statErr := os.Stat(cand); statErr == nil && !st.IsDir() {
				found = true
				break
			}
			if st, statErr := os.Stat(cand); statErr == nil && st.IsDir() {
				found = true
				break
			}
		}
		if !found {
			issues = append(issues, Issue{
				Code:    diagnostic.ErrFileRead.Code,
				Message: fmt.Sprintf("导入的文件不存在: %s（已按 %s、.hy、.z 三种形式查找）", imp.Path, abs),
				Line:    imp.Token.Line,
				Column:  imp.Token.Column,
			})
		}
	}
	return issues
}

// ConstantDivision 检查字面量除零/取模零（编译期可判定的确定错误）
func ConstantDivision(prog *ast.Program) []Issue {
	if prog == nil {
		return nil
	}
	var issues []Issue
	var walkExpr func(e ast.Expression)
	report := func(n *ast.InfixExpression, code, msg string) {
		issues = append(issues, Issue{Code: code, Message: msg, Line: n.Token.Line, Column: n.Token.Column})
	}
	walkExpr = func(e ast.Expression) {
		switch n := e.(type) {
		case nil:
			return
		case *ast.InfixExpression:
			walkExpr(n.Left)
			walkExpr(n.Right)
			if lit, ok := n.Right.(*ast.IntegerLiteral); ok && lit.Value == 0 {
				if n.Operator == "/" {
					report(n, diagnostic.ErrDivisionByZero.Code, "除数是字面量 0：运行到这里必然报除零错误")
				}
				if n.Operator == "%" {
					report(n, diagnostic.ErrModByZero.Code, "取模的模数是字面量 0：运行到这里必然报错")
				}
			}
		case *ast.PrefixExpression:
			walkExpr(n.Right)
		case *ast.CallExpression:
			for _, a := range n.Arguments {
				walkExpr(a)
			}
		case *ast.IndexExpression:
			walkExpr(n.Left)
			walkExpr(n.Index)
		case *ast.ArrayLiteral:
			for _, el := range n.Elements {
				walkExpr(el)
			}
		case *ast.AssignExpression:
			walkExpr(n.Value)
		case *ast.IfExpression:
			walkExpr(n.Condition)
		}
	}
	var walkStmt func(s ast.Statement)
	walkStmt = func(s ast.Statement) {
		switch n := s.(type) {
		case *ast.ExpressionStatement:
			walkExpr(n.Expression)
		case *ast.LetStatement:
			walkExpr(n.Value)
		case *ast.ReturnStatement:
			walkExpr(n.ReturnValue)
		case *ast.BlockStatement:
			for _, st := range n.Statements {
				walkStmt(st)
			}
		case *ast.WhileStatement:
			walkExpr(n.Condition)
			if n.Body != nil {
				walkStmt(n.Body)
			}
		case *ast.ForStatement:
			walkExpr(n.Init)
			walkExpr(n.Condition)
			walkExpr(n.Update)
			if n.Body != nil {
				walkStmt(n.Body)
			}
		case *ast.ForInStatement:
			walkExpr(n.Iterable)
			walkExpr(n.EndExpr)
			walkExpr(n.Guard)
			if n.Body != nil {
				walkStmt(n.Body)
			}
		}
	}
	for _, s := range prog.Statements {
		walkStmt(s)
	}
	return issues
}
