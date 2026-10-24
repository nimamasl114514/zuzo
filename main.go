package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/encoding"
	"zuzo/evaluator"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/mix"
	"zuzo/object"
	"zuzo/parser"
	pyrt "zuzo/py/runtime"
)

// zuzo — 一门中文编程语言
//
// 用法:
//   zuzo              启动交互式 REPL
//   zuzo 文件.hy      执行文件
//   zuzo -e "代码"    执行内联代码
//   zuzo --check 文件  类型检查（不执行）

// moduleManager 全局模块管理器（在 runFile/runCode 中初始化）
var moduleManager *evaluator.ModuleManager

func main() {
	startCPUProfile()
	initInterop()
	args := os.Args[1:]

	if len(args) == 0 {
		// 启动 REPL
		startREPL(os.Stdin, os.Stdout)
		return
	}

	// 解析全局标志
	noSAP := false
	shadow := false
	shadowLenient := false
	strict := false
	trace := false
	debug := false
	profile := false
	var remaining []string
	for _, a := range args {
		switch a {
		case "--no-sap":
			noSAP = true
		case "--shadow":
			shadow = true
		case "--shadow-lenient":
			shadow = true
			shadowLenient = true
		case "--strict":
			strict = true
		case "--trace":
			trace = true
		case "--debug":
			debug = true
			trace = true // debug 自动启用 trace
		case "--profile":
			profile = true
		default:
			remaining = append(remaining, a)
		}
	}
	args = remaining

	if trace {
		evaluator.SetTraceEnabled(true)
	}
	if debug {
		evaluator.SetDebugMode(true)
	}
	if profile {
		evaluator.SetProfileMode(true)
	}

	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "错误: 请提供要执行的文件或命令")
		os.Exit(1)
	}

	switch args[0] {
	case "-e", "--eval":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "错误: 请提供要执行的代码")
			os.Exit(1)
		}
		runCode(args[1], noSAP, shadow, shadowLenient, strict)
	case "-c", "--check":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "错误: 请提供要检查的文件")
			os.Exit(1)
		}
		checkFile(args[1])
	case "-a", "--analyze":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "错误: 请提供要分析的文件")
			os.Exit(1)
		}
		analyzeFile(args[1])
	case "--py":
		pyrt.RunREPL(os.Stdin, os.Stdout, os.Stderr)
	case "--py-repl":
		pyrt.RunREPL(os.Stdin, os.Stdout, os.Stderr)
	case "-h", "--help":
		printHelp()
	case "version", "--version":
		fmt.Println("zuzo v1.0.0")
	default:
		// 执行文件
		runFile(args[0], noSAP, shadow, shadowLenient, strict)
	}
	stopCPUProfile()
}

// runFile 执行源文件（.hy 走中文语法，.py 走 Python 前端，.hypy 走 zuzo+Python 混合）
func runFile(path string, noSAP, shadow, shadowLenient, strict bool) {
	if strings.EqualFold(filepath.Ext(path), ".py") {
		code := pyrt.RunFile(path, os.Stdout, os.Stderr)
		stopCPUProfile()
		os.Exit(code)
	}
	if mix.IsMixFile(path) {
		code := mix.RunFile(path, os.Stdout, os.Stderr)
		stopCPUProfile()
		os.Exit(code)
	}
	source, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 无法读取文件 %s: %v\n", path, err)
		os.Exit(1)
	}
	// 设置模块管理器（支持导入语句）
	absPath, _ := filepath.Abs(path)
	moduleManager = evaluator.NewModuleManager(filepath.Dir(absPath))
	evaluator.SetModuleManager(moduleManager)
	runCode(encoding.NormalizeSource(source), noSAP, shadow, shadowLenient, strict)
}

// runCode 解析并执行源代码
func runCode(source string, noSAP, shadow, shadowLenient, strict bool) {
	// 确保模块管理器存在（REPL 或 -e 模式下使用当前目录）
	if moduleManager == nil {
		moduleManager = evaluator.NewModuleManager(".")
		evaluator.SetModuleManager(moduleManager)
	}

	l := lexer.New(source)
	p := parser.New(l)

	program := p.ParseProgram()
	// 设置源码上下文，使错误报告显示源码行
	p.Diagnostics().SetSource(source)
	if p.Diagnostics().HasErrors() {
		fmt.Fprint(os.Stderr, p.Diagnostics().Render())
		os.Exit(1)
	}

	// === 类型推断 + SAP 编译 ===
	inf := infer.New()
	inf.Infer(program)

	// === 类型检查 ===
	// 默认：有类型错误时输出警告但不阻止执行
	// --strict：有类型错误时阻止执行
	typeErrors := inf.Errors()
	if len(typeErrors) > 0 {
		diag := diagnostic.New()
		diag.SetSource(source)
		for _, te := range typeErrors {
			diag.AddRoot(diagnostic.ErrTypeMismatch.Code, te.Line, te.Column, te.Message, "")
		}
		if strict {
			// 严格模式：类型错误视为编译错误，不执行
			fmt.Fprint(os.Stderr, diag.Render())
			os.Exit(1)
		}
		// 默认：输出警告但继续执行
		fmt.Fprint(os.Stderr, "⚠ 类型检查发现以下问题（警告，不阻止执行）：\n")
		fmt.Fprint(os.Stderr, diag.Render())
		fmt.Fprintln(os.Stderr)
	}

	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	slotMap := inf.GetSlotMap()

	// === 模式选择 ===
	//
	//   --no-sap:  关闭 SAP，纯 Tree-Walk（排查 SAP 是否引入 Bug）
	//   --shadow:  双轨执行（SAP + 传统），对比结果和副作用，不一致立刻 Panic
	//   默认:      SAP 优化路径

	if shadow {
		// 影子模式：双轨执行 + 断言
		sapResult, naiveResult, err := evaluator.RunShadow(program, sap, slotMap)
		if err != nil {
			fmt.Fprintf(os.Stderr, "影子模式执行错误: %v\n", err)
			os.Exit(1)
		}
		var assertErr error
		if shadowLenient {
			assertErr = evaluator.AssertShadowLenient(sapResult, naiveResult)
		} else {
			assertErr = evaluator.AssertShadow(sapResult, naiveResult)
		}
		if assertErr != nil {
			fmt.Fprintf(os.Stderr, "\n🚨 %v\n", assertErr)
			os.Exit(1)
		}
		// 影子模式通过：输出 SAP 路径的打印结果
		fmt.Print(sapResult.Stdout)
		if sapResult.ReturnValue != nil && sapResult.ReturnValue.Type() == object.ERROR_OBJ {
			errObj := sapResult.ReturnValue.(*object.Error)
			diag := diagnostic.New()
			diag.SetSource(source)
			code := errObj.Code
			if code == "" {
				code = "E200"
			}
			diag.AddRoot(code, errObj.Line, errObj.Column, errObj.Message, "")
			fmt.Fprint(os.Stderr, diag.Render())
			os.Exit(1)
		}
		return
	}

	if noSAP {
		// 纯 Tree-Walk 路径
		evaluator.SetSAPCompiler(nil)
		env := evaluator.NewEnvironment()
		evaluator.SetDebugEnv(env)
		installCrossBridge(env, os.Stdout, os.Stderr)
		result := evaluator.Eval(program, env)
		handleResult(result, source)
		stopCPUProfile()
		if evaluator.IsProfileMode() {
			evaluator.DumpProfile(os.Stderr)
		}
		return
	}

	// 默认：SAP 优化路径
	evaluator.SetSAPCompiler(sap)
	env := evaluator.NewSAPEnvironment(len(slotMap), slotMap)
	evaluator.SetDebugEnv(env)
	installCrossBridge(env, os.Stdout, os.Stderr)
	result := evaluator.Eval(program, env)
	handleResult(result, source)
	stopCPUProfile()
	if evaluator.IsProfileMode() {
		evaluator.DumpProfile(os.Stderr)
	}
}

// analyzeFile 对源文件进行拓扑指纹分析（查重+死代码检测+CSE候选）
func analyzeFile(path string) {
	source, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 无法读取文件 %s: %v\n", path, err)
		os.Exit(1)
	}
	analyzeCode(encoding.NormalizeSource(source), path)
}

// analyzeCode 对源代码进行拓扑指纹分析
func analyzeCode(source, fileHint string) {
	l := lexer.New(source)
	p := parser.New(l)

	program := p.ParseProgram()
	diag := p.Diagnostics()
	diag.SetSource(source)

	if diag.HasErrors() {
		fmt.Fprint(os.Stderr, diag.Render())
		os.Exit(1)
	}

	// 运行指纹分析 pass
	pass := evaluator.NewFingerprintPass()
	report := pass.Run(program, fileHint)

	fmt.Print(report.Format())
}

// checkFile 对源文件进行静态类型检查（不执行）
func checkFile(path string) {
	source, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 无法读取文件 %s: %v\n", path, err)
		os.Exit(1)
	}
	checkCode(encoding.NormalizeSource(source))
}

// checkCode 对源代码进行静态类型检查
func checkCode(source string) {
	l := lexer.New(source)
	p := parser.New(l)

	program := p.ParseProgram()
	diag := p.Diagnostics()
	diag.SetSource(source)

	if diag.HasErrors() {
		// 语法有错误，但继续进行类型检查以发现连锁错误
		infer.CheckProgramWithDiagnostics(program, diag)
		fmt.Fprint(os.Stderr, diag.Render())
		os.Exit(1)
	}

	typeErrors := infer.CheckProgramWithDiagnostics(program, diag)
	if len(typeErrors) == 0 && !diag.HasErrors() {
		fmt.Println("✓ 类型检查通过，未发现类型错误")
	} else {
		fmt.Fprint(os.Stderr, diag.Render())
		os.Exit(1)
	}
}

// startREPL 启动交互式读取-求值-输出循环
func startREPL(in io.Reader, out io.Writer) {
	scanner := bufio.NewScanner(in)
	// 增大缓冲区以支持长输入
	scanner.Buffer(make([]byte, 1024*64), 1024*1024)

	env := evaluator.NewEnvironment()

	// 多行输入缓冲
	var inputBuf strings.Builder
	lineNum := 0

	printPrompt(out)

	for scanner.Scan() {
		line := scanner.Text()
		lineNum++

		// === REPL 元命令 ===
		if inputBuf.Len() == 0 && strings.HasPrefix(line, ":") {
			cmd := strings.TrimSpace(line)
			switch {
			case cmd == ":help" || cmd == ":h":
				fmt.Fprintln(out, "REPL 元命令:")
				fmt.Fprintln(out, "  :type <表达式>  — 显示表达式的推断类型")
				fmt.Fprintln(out, "  :check          — 对当前缓冲区执行类型检查")
				fmt.Fprintln(out, "  :help           — 显示此帮助")
				fmt.Fprintln(out, "  :quit           — 退出 REPL")
			case cmd == ":quit" || cmd == ":q":
				return
			case strings.HasPrefix(cmd, ":type "):
				expr := strings.TrimPrefix(cmd, ":type ")
				// 解析表达式
				l2 := lexer.New(expr)
				p2 := parser.New(l2)
				prog2 := p2.ParseProgram()
				if p2.Diagnostics().HasErrors() {
					fmt.Fprintln(out, "解析错误:", p2.Errors())
					printPrompt(out)
					continue
				}
				inf2 := infer.New()
				inf2.Infer(prog2)
				// 获取表达式的类型
				if len(prog2.Statements) > 0 {
					if exprStmt, ok := prog2.Statements[0].(*ast.ExpressionStatement); ok {
						t := inf2.TypeOf(exprStmt.Expression)
						fmt.Fprintf(out, "类型: %s\n", t.String())
					}
				}
				printPrompt(out)
				continue
			case cmd == ":check":
				if inputBuf.Len() == 0 {
					fmt.Fprintln(out, "缓冲区为空")
					printPrompt(out)
					continue
				}
				source := inputBuf.String()
				l2 := lexer.New(source)
				p2 := parser.New(l2)
				prog2 := p2.ParseProgram()
				if p2.Diagnostics().HasErrors() {
					p2.Diagnostics().SetSource(source)
					fmt.Fprint(out, p2.Diagnostics().Render())
					printPrompt(out)
					continue
				}
				inf2 := infer.New()
				inf2.Infer(prog2)
				errs := inf2.Errors()
				if len(errs) == 0 {
					fmt.Fprintln(out, "✓ 类型检查通过")
				} else {
					for _, e := range errs {
						fmt.Fprintf(out, "✗ 行 %d:%d %s\n", e.Line, e.Column, e.Message)
					}
				}
				printPrompt(out)
				continue
			}
		}

		if line == "" && inputBuf.Len() == 0 {
			printPrompt(out)
			continue
		}

		inputBuf.WriteString(line + "\n")

		// 尝试解析当前缓冲区内容
		source := inputBuf.String()
		l := lexer.New(source)
		p := parser.New(l)
		program := p.ParseProgram()

		if p.Diagnostics().HasErrors() {
			// 检查是否是不完整输入（如缺少闭合括号）
			incomplete := false
			for _, msg := range p.Errors() {
				if strings.Contains(msg, "EOF") ||
					(strings.Contains(msg, "期望") && strings.Contains(msg, "EOF")) {
					incomplete = true
					break
				}
			}

			if incomplete {
				// 继续等待更多输入
				fmt.Fprint(out, "... ")
				continue
			}

			// 输出活字诊断报告并重置
			p.Diagnostics().SetSource(source)
			fmt.Fprint(out, p.Diagnostics().Render())
			inputBuf.Reset()
			printPrompt(out)
			continue
		}

		// 解析成功，执行
		result := evaluator.Eval(program, env)

		if result != nil {
			if result.Type() == object.ERROR_OBJ {
				errObj := result.(*object.Error)
				diag := diagnostic.New()
				diag.SetSource(source)
				code := errObj.Code
				if code == "" {
					code = "E200" // 默认运行时错误码
				}
				diag.AddRoot(code, errObj.Line, errObj.Column, errObj.Message, "")
				fmt.Fprint(out, diag.Render())
			} else if result.Type() != object.NULL_OBJ {
				fmt.Fprintln(out, result.Inspect())
			}
		}

		inputBuf.Reset()
		printPrompt(out)
	}

	if err := scanner.Err(); err != nil {
		fmt.Fprintf(out, "读取错误: %v\n", err)
	}
}

func printPrompt(out io.Writer) {
	fmt.Fprint(out, "zuzo> ")
}

// handleResult 处理执行结果：如果是错误，格式化输出并退出
func handleResult(result object.Object, source string) {
	if result != nil && result.Type() == object.ERROR_OBJ {
		errObj := result.(*object.Error)
		diag := diagnostic.New()
		diag.SetSource(source)
		code := errObj.Code
		if code == "" {
			code = "E200"
		}
		diag.AddRoot(code, errObj.Line, errObj.Column, errObj.Message, "")
		fmt.Fprint(os.Stderr, diag.Render())

		// --debug 模式：输出调用栈和全局变量快照
		if evaluator.IsDebugMode() {
			fmt.Fprintln(os.Stderr, "\n=== 调试信息 ===")
			// 调用栈已在错误对象中
			if len(errObj.StackTrace) > 0 {
				fmt.Fprintln(os.Stderr, "调用栈:")
				for i, frame := range errObj.StackTrace {
					fmt.Fprintf(os.Stderr, "  %d. %s\n", i+1, frame)
				}
			}
			// 全局变量快照
			if evaluator.GetDebugEnv() != nil {
				fmt.Fprintln(os.Stderr, "\n全局变量快照:")
				evaluator.DumpEnv(evaluator.GetDebugEnv(), os.Stderr)
			}
		}
		os.Exit(1)
	}
}

func printHelp() {
	fmt.Println(`zuzo — 中文编程语言

用法:
  zuzo              启动交互式 REPL
  zuzo <文件.hy>    执行源文件
  zuzo <文件.py>    执行 Python 源文件
  zuzo <文件.hypy>  执行 zuzo + Python 混合源文件
  zuzo -e "代码"    执行内联代码
  zuzo -a <文件>    拓扑指纹分析（查重/死代码/CSE）
  zuzo -c <文件>    静态类型检查
  zuzo --help       显示帮助
  zuzo --version    显示版本

调试标志（可组合使用）:
  --no-sap          关闭 SAP 优化，纯 Tree-Walk 执行
  --shadow          影子模式：双轨执行（SAP + 传统），不一致立即报错
  --shadow-lenient  影子模式（宽松）：忽略时间戳/并发交错差异
  --strict          严格模式：类型错误视为编译错误，不执行
  --trace           执行轨迹：输出函数调用记录到 stderr
  --debug           调试模式：自动启用 --trace，错误时输出全局变量快照
  --profile         性能分析：输出函数调用次数和耗时统计

混合源文件 (.hypy):
  ##ZUZO                            切换到 zuzo 块（默认块）
  ##PY                              切换到 Python 块
  跨语言调用: py调用("函数名", 参数...) / py获取("变量名") / py设置("变量名", 值)
              Python 侧: zuzo.调用("函数名", 参数...) / zuzo.获取("变量名") / zuzo.设置("变量名", 值)
  全局表:     块边界自动双向同步，跨块与跨语言同名变量可见

示例:
  zuzo --no-sap demo.hy             纯 Tree-Walk 执行
  zuzo --shadow demo.hy             影子模式验证 SAP 正确性（严格）
  zuzo --shadow-lenient demo.hy     影子模式（宽松，适用于并发脚本）

语言特性:
  变量声明:   变量 x = 10
  赋值:       x = 20
  函数定义:   函数 加(a, b) { 返回 a + b }
  条件分支:   如果 x > 5 { ... } 否则 { ... }
  当循环:     当 x > 0 { ... }
  对于循环:   对于 i = 0; i < 10; i = i + 1 { ... }
  数组:       [1, 2, 3]
  哈希表:     {"键": "值", "数": 42}
  逻辑运算:   并且 / 或者 / 非
  注释:       // 这是注释

内置函数:
  打印(x)         打印值
  长度(x)         获取长度
  推入(数组, 值)   添加到数组末尾
  弹出(数组)      弹出数组末尾元素
  类型(x)         获取类型名
  字符串(x)       转为字符串
  整数(x)         转为整数
  浮点数(x)       转为浮点数
  连接(数组, 分隔符) 连接数组为字符串
  包含(容器, 值)   检查是否包含
  加载C库(路径)    加载C动态库
  C函数声明(库,名,类型,返回) 声明C函数
  C调用(函数,参数) 调用C函数`)
}
