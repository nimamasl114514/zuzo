// Package cbackend 把 zuzo AST 翻译成 C 源码（第21轮 · C 后端）。
//
// 设计口径（见 交付-累计.md 第21轮任务总计划）：
//   - 值模型统一走 ZVal（cbackend/runtime/zuzo_rt.h），语义对齐解释器；
//   - 表达式降级：if 表达式 / 短路逻辑 / 哈希字面量提升为「语句 + 临时量」；
//   - 函数签名按编译期参数个数生成（本轮不支持把函数当值传递）。
package cbackend

import (
	"bytes"
	"fmt"
	"sort"
	"strings"

	"zuzo/ast"
)

// Options 生成选项
type Options struct {
	SourcePath string // 源文件路径（仅用于注释与错误信息）
	KeepTemp   bool   // 保留中间产物（调试用）
}

// funcInfo 编译期函数表项
type funcInfo struct {
	Name  string
	Arity int
	Node  *ast.FunctionLiteral
}

// Gen C 代码生成器
type Gen struct {
	buf    bytes.Buffer
	indent int
	tmpSeq int
	funcs  map[string]*funcInfo
	order  []string
	scopes []map[string]bool
	errs   []string
	opt    Options
	// updateLabels 循环栈：非空 = 当前循环带 update 标签（for），空串 = 无 update（while/遍历）
	updateLabels []string
	labelSeq     int
}

// Generate 把 zuzo 程序翻译成一份完整可编译的 C 源码。
func Generate(program *ast.Program, opt Options) (string, error) {
	g := &Gen{funcs: map[string]*funcInfo{}, opt: opt}
	if err := g.collectFunctions(program); err != nil {
		return "", err
	}

	g.line("/* ────────────────────────────────────────────────────────────")
	g.line(" * 由 zuzo C 后端自动生成，请勿手改。")
	if opt.SourcePath != "" {
		g.line(" * 源文件: %s", filepathToSlash(opt.SourcePath))
	}
	g.line(" * 运行时: zuzo_rt.c / zuzo_rt.h")
	g.line(" * ──────────────────────────────────────────────────────────── */")
	g.line("#include \"zuzo_rt.h\"")
	g.line("")
	g.line("#include <stdint.h>")
	g.line("")

	// 前向声明（支持递归与互相调用）
	for _, name := range g.order {
		fi := g.funcs[name]
		g.line("%s;", signature(fi))
	}
	if len(g.order) > 0 {
		g.line("")
	}

	// 函数定义
	for _, name := range g.order {
		fi := g.funcs[name]
		if err := g.genFunction(fi); err != nil {
			return "", err
		}
		g.line("")
	}

	// main：顶层语句
	g.line("int main(int argc, char** argv) {")
	g.line("    (void)argc; (void)argv;")
	g.line("    z_rt_init();")
	g.pushScope()
	for _, stmt := range program.Statements {
		if fs, ok := stmt.(*ast.ExpressionStatement); ok {
			if fl, ok := fs.Expression.(*ast.FunctionLiteral); ok && fl.Name != "" {
				continue
			}
		}
		if err := g.genStmt(stmt); err != nil {
			return "", err
		}
	}
	g.popScope()
	g.line("    return 0;")
	g.line("}")

	if len(g.errs) > 0 {
		return "", fmt.Errorf("C 后端不支持以下语法：\n  - %s", strings.Join(g.errs, "\n  - "))
	}
	return g.buf.String(), nil
}

func filepathToSlash(p string) string {
	return strings.ReplaceAll(p, "\\", "/")
}

func signature(fi *funcInfo) string {
	var params []string
	for i := 0; i < fi.Arity; i++ {
		params = append(params, fmt.Sprintf("ZVal a%d", i))
	}
	if len(params) == 0 {
		params = append(params, "void")
	}
	return fmt.Sprintf("static ZVal %s(%s)", cFuncName(fi.Name), strings.Join(params, ", "))
}

// cFuncName 把 zuzo 函数名映射为 C 标识符（保留中文，前缀防冲突）
func cFuncName(name string) string {
	if name == "" {
		return "zf_anonymous"
	}
	return "zf_" + sanitizeIdent(name)
}

// cVarName 把 zuzo 变量名映射为 C 标识符
func cVarName(name string) string {
	return "zv_" + sanitizeIdent(name)
}

// sanitizeIdent 去掉 C 标识符不允许的字符
func sanitizeIdent(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r > 127: // 中文等 UTF-8 标识符直接保留（gcc extended identifiers）
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	s := b.String()
	if s == "" {
		return "_"
	}
	if s[0] >= '0' && s[0] <= '9' {
		return "_" + s
	}
	return s
}

func (g *Gen) collectFunctions(program *ast.Program) error {
	var walk func(stmts []ast.Statement) error
	walk = func(stmts []ast.Statement) error {
		for _, stmt := range stmts {
			switch s := stmt.(type) {
			case *ast.ExpressionStatement:
				if fl, ok := s.Expression.(*ast.FunctionLiteral); ok && fl.Name != "" {
					if _, dup := g.funcs[fl.Name]; !dup {
						g.funcs[fl.Name] = &funcInfo{Name: fl.Name, Arity: len(fl.Parameters), Node: fl}
						g.order = append(g.order, fl.Name)
					}
				}
			}
		}
		return nil
	}
	if err := walk(program.Statements); err != nil {
		return err
	}
	sort.SliceStable(g.order, func(i, j int) bool { return false }) // 保持声明顺序
	return nil
}

// genFunction 生成一个 zuzo 函数的 C 实现
func (g *Gen) genFunction(fi *funcInfo) error {
	g.line("%s {", signature(fi))
	g.pushScope()
	for i, p := range fi.Node.Parameters {
		g.declare(p.Value)
		g.line("    (void)a%d;", i) // 防止未使用告警（真实使用在下方别名）
		g.line("    ZVal %s = a%d;", cVarName(p.Value), i)
	}

	body := fi.Node.Body.Statements
	for i, stmt := range body {
		isLast := i == len(body)-1
		// 函数体最后一条表达式语句 = 隐式返回值
		if isLast {
			if es, ok := stmt.(*ast.ExpressionStatement); ok {
				val, err := g.genExpr(es.Expression)
				if err != nil {
					return err
				}
				g.line("    return %s;", val)
				g.popScope()
				g.line("}")
				return nil
			}
		}
		if err := g.genStmt(stmt); err != nil {
			return err
		}
	}
	g.line("    return z_null();")
	g.popScope()
	g.line("}")
	return nil
}

func (g *Gen) line(format string, args ...any) {
	for i := 0; i < g.indent; i++ {
		g.buf.WriteString("    ")
	}
	if len(args) == 0 {
		g.buf.WriteString(format)
	} else {
		fmt.Fprintf(&g.buf, format, args...)
	}
	g.buf.WriteByte('\n')
}

func (g *Gen) pushScope() {
	g.scopes = append(g.scopes, map[string]bool{})
	g.indent++
}

func (g *Gen) popScope() {
	g.scopes = g.scopes[:len(g.scopes)-1]
	g.indent--
}

func (g *Gen) declare(name string) {
	if len(g.scopes) == 0 {
		return
	}
	g.scopes[len(g.scopes)-1][name] = true
}

func (g *Gen) declared(name string) bool {
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if g.scopes[i][name] {
			return true
		}
	}
	return false
}

func (g *Gen) temp() string {
	g.tmpSeq++
	return fmt.Sprintf("zt%d", g.tmpSeq)
}

func (g *Gen) unsupported(what string) {
	g.errs = append(g.errs, what)
}
