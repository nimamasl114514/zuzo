package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"zuzo/analyze"
	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/encoding"
	"zuzo/evaluator"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/llvmbackend"
	"zuzo/mix"
	"zuzo/object"
	"zuzo/parser"
	pyrt "zuzo/py/runtime"
	"zuzo/pyext"
	"zuzo/toolchain"
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

// pendingCompileDiag 编译期（类型检查）警告缓存：
// 默认模式下类型错误不阻断执行，但不应立刻打印——否则运行期再报同一处时会出现两段重复报告。
// 统一在运行结束（成功）或运行期出错时合并输出。
var pendingCompileDiag *diagnostic.Report

// compileFile 当前正在编译/执行的文件名（供运行期诊断定位）
var compileFile string

// builtinNameSet 内置函数名集合（静态检查判定「已定义」用，惰性初始化）
var builtinNameSet map[string]bool

func builtinSet() map[string]bool {
	if builtinNameSet == nil {
		names := evaluator.BuiltinNames()
		builtinNameSet = make(map[string]bool, len(names))
		for _, n := range names {
			builtinNameSet[n] = true
		}
	}
	return builtinNameSet
}

// addShadowIssues 把「与内置同名」的定义加入诊断报告（警告级），返回命中数。
// checkBaseDir 返回当前被检查文件所在目录（用于导入路径解析）
func checkBaseDir() string {
	if compileFile != "" && compileFile != "<内联>" {
		if abs, err := filepath.Abs(compileFile); err == nil {
			return filepath.Dir(abs)
		}
	}
	return "."
}

func addShadowIssues(diag *diagnostic.Report, program *ast.Program) int {
	issues := analyze.ShadowedBuiltins(program, builtinSet())
	for _, is := range issues {
		diag.AddRootWithSeverity(diagnostic.SeverityWarning, is.Code, is.Line, is.Column, is.Message,
			"换一个名字，或确认两条链行为一致")
	}
	return len(issues)
}

// addUndefinedIssues 把「全文件未定义标识符」加入诊断报告，返回命中数。
// 这类问题在运行期必然报 E201，静态提前给出可以让用户少跑一次。
func addUndefinedIssues(diag *diagnostic.Report, program *ast.Program, sev diagnostic.Severity) int {
	baseDir := "."
	if compileFile != "" && compileFile != "<内联>" {
		if abs, err := filepath.Abs(compileFile); err == nil {
			baseDir = filepath.Dir(abs)
		}
	}
	issues := analyze.UndefinedWithImports(program, builtinSet(), baseDir)
	for _, is := range issues {
		diag.AddRootWithSeverity(sev, is.Code, is.Line, is.Column, is.Message,
			"检查拼写；若是自定义函数/变量，请先定义")
	}
	return len(issues)
}

// runToolchain zuzo toolchain [status|install [路径]|env]
func runToolchain(args []string) {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "status", "状态":
		r := toolchain.Status()
		fmt.Printf("CC        : %s\n", r.CC)
		if r.CCPath != "" {
			fmt.Printf("CC 路径    : %s\n", r.CCPath)
		}
		if r.Zig != "" {
			fmt.Printf("zig       : %s %s\n", r.Zig, r.ZigVer)
		} else {
			fmt.Println("zig       : 未找到（zuzo toolchain install 可安装）")
		}
		if r.Clang != "" {
			fmt.Printf("clang     : %s\n", r.Clang)
		} else {
			fmt.Println("clang     : 未找到（不影响：有 zig cc 即可）")
		}
		fmt.Printf("用户工具链 : %s\n", r.Install)
	case "install", "安装":
		from := ""
		if len(args) > 1 {
			from = args[1]
		}
		dst, err := toolchain.Install(from)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zuzo toolchain install: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("已安装 zig: %s\n", dst)
		fmt.Println("提示：把 zig 放到 <zuzo.exe 同级>/tools/zig/ 下即视为「自带」，无需再安装。")
	case "env", "环境":
		for _, line := range toolchain.EnvLines() {
			fmt.Println(line)
		}
	default:
		fmt.Fprintln(os.Stderr, "用法: zuzo toolchain [status|install [zig路径]|env]")
		os.Exit(2)
	}
}

func main() {
	startCPUProfile()
	initInterop()
	// 第32轮：宿主退出时关掉 CPython 子进程（未启动则无操作）
	defer pyext.Default().Close()
	args := os.Args[1:]

	if len(args) == 0 {
		// 启动 REPL
		startREPL(os.Stdin, os.Stdout)
		return
	}

	// 第32轮：`zuzo toolchain ...` —— 自带工具链（zig cc 兜底）的状态/安装/环境
	if args[0] == "toolchain" || args[0] == "工具链" {
		runToolchain(args[1:])
		return
	}

	// 第32轮：`zuzo pip ...` 直接转给桥用的那个 CPython 的 pip。
	// 与 py导入 共用同一个解释器 —— 装完立刻就能在 .hy 里 import，不用再折腾 PATH。
	if args[0] == "pip" || args[0] == "pip3" {
		sub := args[1:]
		// 中文子命令别名
		if len(sub) > 0 {
			switch sub[0] {
			case "安装":
				sub[0] = "install"
			case "卸载":
				sub[0] = "uninstall"
			case "列表":
				sub[0] = "list"
			case "版本":
				sub[0] = "--version"
			}
		}
		if len(sub) == 0 {
			sub = []string{"--help"}
		}
		code, err := pyext.Pip(sub, os.Stdin, os.Stdout, os.Stderr)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zuzo pip: %v\n", err)
			if strings.Contains(err.Error(), "未找到可用的 CPython") {
				fmt.Fprintln(os.Stderr, "提示：先用 ZUZO_PYTHON=<python.exe 绝对路径> 指定解释器，或安装任意 Python 3.8+")
			}
			os.Exit(2)
		}
		os.Exit(code)
	}

	// 解析全局标志
	noSAP := false
	shadow := false
	shadowLenient := false
	strict := false
	trace := false
	debug := false
	profile := false
	jsonOut := false
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
		case "--json":
			// 机器可读诊断：只对 -c/--check 有意义（stdout 输出 JSON 数组，人读文本走 stderr）
			jsonOut = true
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

	if strings.HasPrefix(args[0], "--cc=") {
		fmt.Fprintln(os.Stderr, "错误: --cc 选项已随 C 后端移除（现在固定走 LLVM：--emit-llvm / --emit-exe）")
		os.Exit(2)
	}

	if jsonOut && args[0] != "-c" && args[0] != "--check" {
		fmt.Fprintln(os.Stderr, "错误: --json 仅用于 -c/--check 的静态检查输出")
		os.Exit(2)
	}

	switch args[0] {
	case "-e", "--eval":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "错误: 请提供要执行的代码")
			os.Exit(1)
		}
		runCode(args[1], "<内联>", noSAP, shadow, shadowLenient, strict)
	case "-c", "--check":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "错误: 请提供要检查的文件")
			os.Exit(1)
		}
		checkFile(args[1], jsonOut)
	case "-a", "--analyze":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "错误: 请提供要分析的文件")
			os.Exit(1)
		}
		analyzeFile(args[1])
	case "--emit-c":
		fmt.Fprintln(os.Stderr, "错误: --emit-c（C 源码后端）已在第31轮移除，请使用 --emit-llvm 生成 LLVM IR，或 --emit-exe 生成可执行文件")
		os.Exit(2)
	case "--emit-llvm", "--emit-ir":
		llvmbackend.SetStrictTypes(strict)
		emitLLVMCommand(args[1:])
	case "--emit-exe":
		llvmbackend.SetStrictTypes(strict)
		emitExeCommand(args[1:])
	case "--py":
		pyrt.RunREPL(os.Stdin, os.Stdout, os.Stderr)
	case "--py-repl":
		pyrt.RunREPL(os.Stdin, os.Stdout, os.Stderr)
	case "test", "测试":
		runTestCommand(args[1:])
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
	runCode(encoding.NormalizeSource(source), path, noSAP, shadow, shadowLenient, strict)
}

// runCode 解析并执行源代码
func runCode(source, filename string, noSAP, shadow, shadowLenient, strict bool) {
	compileFile = filename
	pendingCompileDiag = nil
	// 运行期诊断：严格模式下越界/缺键从「警告 + 返回空」升级为错误
	evaluator.SetRuntimeStrict(strict)
	evaluator.SetRuntimeFile(filename)
	// 确保模块管理器存在（REPL 或 -e 模式下使用当前目录）
	if moduleManager == nil {
		moduleManager = evaluator.NewModuleManager(".")
		evaluator.SetModuleManager(moduleManager)
	}

	l := lexer.New(source)
	p := parser.New(l)

	program := p.ParseProgram()
	// 设置源码/文件名上下文，使错误报告显示源码行与 file:line:col 定位
	diag := p.Diagnostics()
	diag.SetSource(source)
	diag.SetFile(filename)
	diag.SetStage("词法/语法")
	if diag.HasErrors() {
		diag.Dedupe()
		fmt.Fprint(os.Stderr, diag.Render())
		os.Exit(1)
	}

	// === 类型推断 + SAP 编译 ===
	// 类型检查复用同一份诊断报告：避免「语法一块、类型一块」两次输出
	inf := infer.New()
	inf.SetDiagnostics(diag)
	inf.Infer(program)

	// === 类型检查 ===
	// 默认：有类型错误时输出警告但不阻止执行
	// --strict：有类型错误时阻止执行
	typeErrors := inf.Errors()
	undefCount := addUndefinedIssues(diag, program, diagnostic.SeverityWarning)
	shadowCount := addShadowIssues(diag, program)
	if len(typeErrors) > 0 || undefCount > 0 || shadowCount > 0 {
		diag.SetStage("静态检查")
		if strict {
			// 严格模式：类型错误视为编译错误，不执行
			diag.Dedupe()
			fmt.Fprint(os.Stderr, diag.Render())
			os.Exit(1)
		}
		// 默认：降级为警告后继续执行，但不立即输出——缓存到运行结束统一渲染
		diag.SetSeverityAll(diagnostic.SeverityWarning)
		diag.Dedupe()
		pendingCompileDiag = diag
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
			diag.SetFile(filename)
			diag.SetStage("运行期")
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
// jsonOut 为 true 时 stdout 只出 JSON 数组，人读文本一律走 stderr；退出码不变。
func checkFile(path string, jsonOut bool) {
	source, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: 无法读取文件 %s: %v\n", path, err)
		if jsonOut {
			// 读文件失败不是诊断，但 stdout 仍保持「合法 JSON 数组」契约，免得消费者解析失败
			writeJSONDiag(diagnostic.New(), path)
		}
		os.Exit(1)
	}
	checkCode(encoding.NormalizeSource(source), path, jsonOut)
}

// emitDiag 按输出模式输出诊断：人读文本总是走 stderr，JSON 只走 stdout。
// 这样 `-c` 的文本与退出码与旧版完全一致，`-c --json` 的 stdout 又可以直接喂给编辑器/CI。
func emitDiag(diag *diagnostic.Report, filename string, jsonOut bool) {
	if text := diag.Render(); text != "" {
		fmt.Fprint(os.Stderr, text)
	}
	if jsonOut {
		writeJSONDiag(diag, filename)
	}
}

// writeJSONDiag 把诊断写成 JSON 数组到 stdout（零诊断即 []），编码失败只降级为 stderr 提示。
func writeJSONDiag(diag *diagnostic.Report, filename string) {
	if err := diag.WriteJSON(os.Stdout, filename); err != nil {
		fmt.Fprintf(os.Stderr, "错误: 诊断 JSON 编码失败: %v\n", err)
	}
}

// checkCode 对源代码进行静态类型检查
func checkCode(source, filename string, jsonOut bool) {
	compileFile = filename
	l := lexer.New(source)
	p := parser.New(l)

	program := p.ParseProgram()
	diag := p.Diagnostics()
	diag.SetSource(source)
	diag.SetFile(filename)
	diag.SetStage("静态检查")

	if diag.HasErrors() {
		// 语法有错误，但继续进行类型检查以发现连锁错误
		infer.CheckProgramWithDiagnostics(program, diag)
		diag.Dedupe()
		emitDiag(diag, filename, jsonOut)
		os.Exit(1)
	}

	typeErrors := infer.CheckProgramWithDiagnostics(program, diag)
	undefCount := addUndefinedIssues(diag, program, diagnostic.SeverityError)
	addShadowIssues(diag, program)
	for _, is := range analyze.ImportErrors(program, checkBaseDir()) {
		diag.AddRoot(is.Code, is.Line, is.Column, is.Message, "检查路径拼写与文件是否存在")
		undefCount++
	}
	for _, is := range analyze.ConstantDivision(program) {
		diag.AddRoot(is.Code, is.Line, is.Column, is.Message, "改成非零除数，或在运行前判断")
		undefCount++
	}
	diag.Dedupe()
	if len(typeErrors) == 0 && undefCount == 0 && !diag.HasErrors() {
		if jsonOut {
			// 零诊断也必须是合法 JSON：输出 []，人读的 ✓ 提示改走 stderr
			writeJSONDiag(diag, filename)
			fmt.Fprintf(os.Stderr, "✓ %s 类型检查通过，未发现类型错误\n", filename)
			return
		}
		fmt.Printf("✓ %s 类型检查通过，未发现类型错误\n", filename)
		return
	}
	emitDiag(diag, filename, jsonOut)
	os.Exit(1)
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
		// 复用编译期报告：同一位置已有类型诊断时，运行期错误作为其连带影响，只输出一段报告
		diag := pendingCompileDiag
		if diag == nil {
			diag = diagnostic.New()
			diag.SetSource(source)
		}
		diag.SetFile(compileFile)
		diag.SetStage("运行期")
		code := errObj.Code
		if code == "" {
			code = "E200"
		}
		if root := diag.FindRootOnLine(errObj.Line); root != nil && root.Code != code {
			diag.AddCascade(root, code, errObj.Line, errObj.Column, errObj.Message, "")
		} else {
			diag.AddRoot(code, errObj.Line, errObj.Column, errObj.Message, "")
		}
		diag.Dedupe()
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

	// 正常结束：如果有缓存的类型警告，此时统一输出（不阻断）
	if pendingCompileDiag != nil && pendingCompileDiag.Count() > 0 {
		fmt.Fprint(os.Stderr, "⚠ 类型检查发现以下问题（警告，不阻止执行）：\n")
		fmt.Fprint(os.Stderr, pendingCompileDiag.Render())
		pendingCompileDiag = nil
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
  zuzo test [目录]  运行 *_test.hy 用例
  zuzo -a <文件>    拓扑指纹分析（查重/死代码/CSE）
  zuzo -c <文件>    静态类型检查（加 --json 输出机器可读 JSON 数组）
  zuzo --emit-llvm <文件.hy> [-o 出.ll]  生成 LLVM IR（LLVM 后端）
  zuzo --emit-exe <文件.hy> [-o 出.exe]  生成可执行文件（LLVM IR → clang）
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
