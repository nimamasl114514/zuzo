// Package cbackend 把 zuzo AST 翻译成 C 源码（C 后端）。
//
// 第22轮增强：闭包捕获（自由变量分析 → 堆环境 → 函数值调用）。
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
	SourcePath string
	KeepTemp   bool
}

// varKind 变量在 C 侧的存储形态
type varKind int

const (
	vPlain   varKind = iota // ZVal
	vBox                    // ZBox*（被内层闭包捕获）
	vCapture                // 通过 env 结构体间接访问的 ZBox*
)

type varSlot struct {
	kind    varKind
	envType string // vCapture 时所属 env 结构体
}

// Gen C 代码生成器
type Gen struct {
	buf    bytes.Buffer
	indent int
	tmpSeq int
	labelSeq int
	funcs  map[string]*fnInfo
	order  []string
	scopes []map[string]varSlot
	errs   []string
	opt    Options
	ana    *analysis
	cur    *fnInfo
	updateLabels []string
	defines  map[string]*ast.DefineStatement
	defOrder []string
}

// Generate 把 zuzo 程序翻译成一份完整可编译的 C 源码。
func Generate(program *ast.Program, opt Options) (code string, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("C 后端内部错误: %v", r)
		}
	}()

	g := &Gen{funcs: map[string]*fnInfo{}, opt: opt, defines: map[string]*ast.DefineStatement{}}
	g.ana = analyzeProgram(program)
	for _, stmt := range program.Statements {
		if d, ok := stmt.(*ast.DefineStatement); ok {
			if _, dup := g.defines[d.Name]; !dup {
				g.defines[d.Name] = d
				g.defOrder = append(g.defOrder, d.Name)
			}
		}
	}
	for _, fi := range g.ana.All {
		if fi.Name != "" {
			if _, dup := g.funcs[fi.Name]; !dup {
				g.funcs[fi.Name] = fi
				g.order = append(g.order, fi.Name)
			}
		}
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

	// 1) 前向声明
	for _, name := range g.defOrder {
		g.line("static ZVal zdef_%s(int argc, ZVal* argv, void* env);", sanitizeIdent(name))
	}
	for _, fi := range g.ana.All {
		g.line("%s;", signature(fi))
	}
	if len(g.ana.All) > 0 {
		g.line("")
	}

	// 2) 闭包环境结构体
	for _, fi := range g.ana.All {
		if fi.EnvType == "" {
			continue
		}
		if len(fi.Captures) == 0 {
			g.line("typedef struct { int unused; } %s;", fi.EnvType)
			continue
		}
		var fields []string
		for _, name := range fi.Captures {
			fields = append(fields, fmt.Sprintf("ZBox* v_%s;", sanitizeIdent(name)))
		}
		g.line("typedef struct { %s } %s;", strings.Join(fields, " "), fi.EnvType)
	}
	if len(g.ana.All) > 0 {
		g.line("")
	}

	// 3) 定义（数据建模）构造函数
	for _, name := range g.defOrder {
		if err := g.genDefine(g.defines[name]); err != nil {
			return "", err
		}
		g.line("")
	}

	// 4) 函数定义
	for _, fi := range g.ana.All {
		if err := g.genFunction(fi); err != nil {
			return "", err
		}
		g.line("")
	}

	// 4) main：顶层语句
	g.line("int main(int argc, char** argv) {")
	g.line("    (void)argc; (void)argv;")
	g.line("    z_rt_init();")
	g.pushScope()
	g.cur = nil
	for _, stmt := range program.Statements {
		if fs, ok := stmt.(*ast.ExpressionStatement); ok {
			if fl, ok := fs.Expression.(*ast.FunctionLiteral); ok && fl.Name != "" {
				continue // 具名函数已单独生成
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

func filepathToSlash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// signature 统一函数签名：参数走 argv，闭包环境走 env
func signature(fi *fnInfo) string {
	return fmt.Sprintf("static ZVal %s(int argc, ZVal* argv, void* env)", fi.CName)
}

func cFuncName(name string) string {
	if name == "" {
		return "zf_anonymous"
	}
	return "zf_" + sanitizeIdent(name)
}

func cVarName(name string) string { return "zv_" + sanitizeIdent(name) }

func envFieldName(name string) string { return "v_" + sanitizeIdent(name) }

func sanitizeIdent(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_':
			b.WriteRune(r)
		case r > 127:
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

// genFunction 生成一个函数的 C 实现
func (g *Gen) genFunction(fi *fnInfo) error {
	prev := g.cur
	g.cur = fi
	defer func() { g.cur = prev }()

	g.line("%s {", signature(fi))
	g.pushScope()
	if fi.EnvType != "" {
		g.line("    %s* zenv = (%s*)env;", fi.EnvType, fi.EnvType)
		g.line("    (void)zenv;")
	}
	g.line("    (void)argc;")
	// 参数：从 argv 取
	for i, p := range fi.Node.Parameters {
		g.line("    ZVal %s = argv[%d];", cVarName(p.Value), i)
		g.setVar(p.Value, varSlot{kind: vPlain})
	}
	// 捕获变量：登记为 vCapture，引用时走 zenv->v_x->v
	for _, name := range fi.Captures {
		g.setVar(name, varSlot{kind: vCapture, envType: fi.EnvType})
	}

	body := fi.Node.Body.Statements
	for i, stmt := range body {
		isLast := i == len(body)-1
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
	g.scopes = append(g.scopes, map[string]varSlot{})
	g.indent++
}

func (g *Gen) popScope() {
	g.scopes = g.scopes[:len(g.scopes)-1]
	g.indent--
}

func (g *Gen) setVar(name string, slot varSlot) {
	if len(g.scopes) == 0 {
		return
	}
	g.scopes[len(g.scopes)-1][name] = slot
}

func (g *Gen) lookupVar(name string) (varSlot, bool) {
	for i := len(g.scopes) - 1; i >= 0; i-- {
		if s, ok := g.scopes[i][name]; ok {
			return s, true
		}
	}
	return varSlot{}, false
}

func (g *Gen) declare(name string) { g.setVar(name, varSlot{kind: vPlain}) }

func (g *Gen) temp() string {
	g.tmpSeq++
	return fmt.Sprintf("zt%d", g.tmpSeq)
}

func (g *Gen) unsupported(what string) { g.errs = append(g.errs, what) }

// boxedBy 判断某变量是否被当前函数的子闭包捕获（需要在声明处装箱）
func (g *Gen) boxedBy(fi *fnInfo, name string) bool {
	if fi == nil {
		return false
	}
	m := g.ana.Boxed[fi]
	return m != nil && m[name]
}

// sortedNames 稳定排序，保证生成结果可复现
func sortedNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// genDefine 把「定义 X { 字段 = 默认值 }」编译成构造函数：返回一个哈希实例。
func (g *Gen) genDefine(d *ast.DefineStatement) error {
	g.line("static ZVal zdef_%s(int argc, ZVal* argv, void* env) {", sanitizeIdent(d.Name))
	g.line("    (void)argc; (void)argv; (void)env;")
	g.line("    ZVal zo = z_hash();")
	g.pushScope()
	for _, f := range d.Fields {
		val, err := g.genExpr(f.Value)
		if err != nil {
			return err
		}
		g.line("    z_hash_set(zo, z_str(%s), %s);", cString(f.Name), val)
	}
	g.popScope()
	g.line("    return zo;")
	g.line("}")
	return nil
}
