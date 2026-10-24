package mix

import (
	"fmt"
	"io"
	"os"

	"zuzo/evaluator"
	"zuzo/lexer"
	"zuzo/object"
	"zuzo/parser"
	pyobj "zuzo/py/object"
	pyparser "zuzo/py/parser"
	pyrt "zuzo/py/runtime"
)

// RunFile 执行 .hypy 混合源文件，返回进程退出码
func RunFile(path string, out, errw io.Writer) int {
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(errw, "错误: 无法读取文件 %s: %v\n", path, err)
		return 1
	}
	return RunSource(string(data), path, out, errw)
}

// RunSource 执行混合源码：
//   - 单个 Python 解释器实例 + 单个 zuzo 全局环境，跨块共享；
//   - 每个块按原文件行号解析（前置空行填充），错误行号不偏移；
//   - 块边界双向同步全局表，实现跨语言变量可见；
//   - 任一块出错立即停止并返回非零码。
func RunSource(src, filename string, out, errw io.Writer) int {
	blocks, err := Split(src)
	if err != nil {
		fmt.Fprintf(errw, "%s: %v\n", filename, err)
		return 1
	}

	ip := pyrt.New()
	ip.Stdout = toWriter(out)
	ip.Stderr = toWriter(errw)
	ip.SetMainFile(filename)

	// 混合模式统一走纯 Tree-Walk（SAP 槽位跨块会失效）
	evaluator.SetSAPCompiler(nil)
	env := evaluator.NewEnvironment()

	bridge := NewBridge(ip, env, out, errw)

	for _, blk := range blocks {
		if blk.Lang == LangPy {
			code := runPyBlock(bridge, blk, filename, errw)
			if code != 0 {
				return code
			}
			continue
		}
		code := runZuzoBlock(bridge, blk, errw)
		if code != 0 {
			return code
		}
	}
	return 0
}

// runPyBlock 执行一个 Python 块（块前惰性拉取 zuzo 变量，块后登记定义版本）
func runPyBlock(b *Bridge, blk Block, filename string, errw io.Writer) int {
	m, errs := pyparser.Parse(blk.Src)
	if len(errs) > 0 {
		fmt.Fprintf(errw, "  File \"%s\"\n", filename)
		for _, e := range errs {
			fmt.Fprintf(errw, "SyntaxError: %s\n", e)
		}
		return 1
	}
	refs, defs := scanPy(blk.Src, m)
	b.BeforePy(refs)
	ip := b.IP
	code := 0
	func() {
		defer func() {
			if r := recover(); r != nil {
				if pe, ok := r.(*pyrt.PyError); ok {
					printPyErr(pe, errw)
					code = 1
					return
				}
				fmt.Fprintf(errw, "Python 运行时错误（块起始第 %d 行）：%v\n", blk.StartLine, r)
				code = 1
			}
		}()
		if pe := ip.RunModule(m); pe != nil {
			printPyErr(pe, errw)
			code = 1
		}
	}()
	if code == 0 {
		b.AfterPy(defs)
	}
	return code
}

// runZuzoBlock 执行一个 zuzo 块（块前惰性拉取 Python 变量，块后登记定义版本）
func runZuzoBlock(b *Bridge, blk Block, errw io.Writer) int {
	l := lexer.New(blk.Src)
	p := parser.New(l)
	program := p.ParseProgram()
	diag := p.Diagnostics()
	diag.SetSource(blk.Src)
	if diag.HasErrors() {
		fmt.Fprint(errw, diag.Render())
		return 1
	}
	refs, defs := scanZuzo(blk.Src, program)
	b.BeforeZuzo(refs)

	// 块内含循环且无函数字面量时，走块级 SAP（热路径恢复槽位/原生循环）
	if blockWantsSAP(program, blk.Src) {
		if code, handled := runZuzoBlockSAP(b, blk, program, refs, defs, errw); handled {
			return code
		}
	}

	env := b.Env

	code := 0
	func() {
		defer func() {
			if r := recover(); r != nil {
				fmt.Fprintf(errw, "zuzo 运行时错误（块起始第 %d 行）：%v\n", blk.StartLine, r)
				code = 1
			}
		}()
		result := evaluator.Eval(program, env)
		if errObjVal, ok := result.(*object.Error); ok {
			fmt.Fprintln(errw, errObjVal.Inspect())
			code = 1
		}
	}()
	if code == 0 {
		b.AfterZuzo(defs)
	}
	return code
}

// printPyErr 输出 Python 异常（与 py/runtime 内置格式保持一致）
func printPyErr(pe *pyrt.PyError, errw io.Writer) {
	fmt.Fprintf(errw, "Traceback (most recent call last):\n")
	for _, f := range pe.Frames {
		fmt.Fprintf(errw, "  %s\n", f)
	}
	if pe.Line > 0 {
		fmt.Fprintf(errw, "  第 %d 行\n", pe.Line)
	}
	if pe.Exc != nil {
		fmt.Fprintf(errw, "%s", pe.Exc.Class.Name)
		if s := pe.Exc.Str(); s != "" {
			fmt.Fprintf(errw, ": %s", s)
		}
		fmt.Fprintf(errw, "\n")
	}
}

// toWriter 把 io.Writer 适配成 Python 运行时接受的写入接口
func toWriter(w io.Writer) io.Writer {
	if w == nil {
		return os.Stdout
	}
	return w
}

var _ = pyobj.None
