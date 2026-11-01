package cbackend

import (
	_ "embed"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/infer"
	"zuzo/lexer"
	"zuzo/nativecc"
	"zuzo/parser"
)

//go:embed runtime/zuzo_rt.c
var runtimeC string

//go:embed runtime/zuzo_rt.h
var runtimeH string

// Compiler 描述一次 C 编译的调用方式（支持 gcc / clang / zig cc）
type Compiler struct {
	Args []string // 基础命令，如 ["C:\\...\\gcc.exe"] 或 ["C:\\Dev\\zig\\zig.exe", "cc"]
	Name string
}

// FindCompiler 探测本机可用的 C 编译器。
// 顺序：ZUZO_CC 环境变量 → PATH 里的 gcc/clang → 常见安装路径 → zig cc。
func FindCompiler() (*Compiler, error) {
	if v := strings.TrimSpace(os.Getenv("ZUZO_CC")); v != "" {
		return &Compiler{Args: strings.Fields(v), Name: v}, nil
	}
	for _, name := range []string{"gcc", "clang", "cc"} {
		if p, err := exec.LookPath(name); err == nil {
			return &Compiler{Args: []string{p}, Name: name}, nil
		}
	}
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{
			`C:\Users\wwww\AppData\Local\Programs\mingw64\bin\gcc.exe`,
			`C:\Users\wwww\mingw64\bin\gcc.exe`,
			`C:\msys64\mingw64\bin\gcc.exe`,
			`C:\Program Files\LLVM\bin\clang.exe`,
		}
	} else {
		candidates = []string{"/usr/bin/gcc", "/usr/bin/clang", "/usr/local/bin/gcc"}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return &Compiler{Args: []string{c}, Name: filepath.Base(c)}, nil
		}
	}
	if p, err := exec.LookPath("zig"); err == nil {
		return &Compiler{Args: []string{p, "cc"}, Name: "zig cc"}, nil
	}
	for _, c := range []string{`C:\Dev\zig\zig.exe`, `C:\zig\zig.exe`} {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return &Compiler{Args: []string{c, "cc"}, Name: "zig cc"}, nil
		}
	}
	return nil, fmt.Errorf("未找到 C 编译器：请安装 gcc/clang，或设置环境变量 ZUZO_CC 指向编译器")
}

// DiagError 携带已渲染好的诊断文本，CLI 直接输出即可（不再加「C 后端错误:」前缀）
type DiagError struct{ Text string }

func (e *DiagError) Error() string { return e.Text }

// ParseFile 读取并解析一个 .hy 源文件
func ParseFile(path string) (*ast.Program, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取文件 %s: %w", path, err)
	}
	l := lexer.New(string(src))
	p := parser.New(l)
	prog := p.ParseProgram()
	diag := p.Diagnostics()
	if diag.HasErrors() {
		diag.SetSource(string(src))
		diag.SetFile(path)
		diag.SetStage("词法/语法")
		diag.Dedupe()
		return nil, &DiagError{Text: diag.Render()}
	}
	return prog, nil
}

// 类型检查严格开关：默认只警告（与解释器一致），true 时阻止生成代码
var strictTypes bool

// SetStrictTypes 开启/关闭类型严格模式（对应 --strict）
func SetStrictTypes(v bool) { strictTypes = v }

// CheckProgram 对已经展开导入的程序做类型检查。
// 返回已渲染的诊断文本（可能为空）与是否命中。
// C 后端此前只跑语法层，导致「能生成 C 就当作能编译」——类型错误被漏报；
// 但类型推断目前仍有假阳（如内置函数带默认参数），因此默认只警告。
func CheckProgram(prog *ast.Program, path, source string) (string, bool) {
	diag := diagnostic.New()
	diag.SetSource(source)
	diag.SetFile(path)
	diag.SetStage("类型检查")
	errs := infer.CheckProgramWithDiagnostics(prog, diag)
	if len(errs) == 0 && !diag.HasErrors() {
		return "", false
	}
	diag.Dedupe()
	diag.SetSeverityAll(diagnostic.SeverityWarning)
	return diag.Render(), true
}

// expandImports 编译期展开「导入」：把被导入文件的顶层语句并入当前程序。
// 直接导入（无别名）合并语句；命名导入（导入 "x.hy" 为 m）暂不支持。
func expandImports(prog *ast.Program, baseDir string, seen map[string]bool, depth int) error {
	if depth > 32 {
		return fmt.Errorf("导入嵌套过深（可能存在循环导入）")
	}
	var out []ast.Statement
	for _, stmt := range prog.Statements {
		imp, ok := stmt.(*ast.ImportStatement)
		if !ok {
			out = append(out, stmt)
			continue
		}
		if imp.Alias != "" {
			return fmt.Errorf("命名导入暂不支持（%s 为 %s），请改用直接导入", imp.Path, imp.Alias)
		}
		p := imp.Path
		if !filepath.IsAbs(p) {
			p = filepath.Join(baseDir, p)
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return err
		}
		// 与解释器模块系统对齐：导入 "词法" 这种省略扩展名的写法补 .hy
		if filepath.Ext(abs) == "" {
			if _, statErr := os.Stat(abs + ".hy"); statErr == nil {
				abs += ".hy"
			}
		}
		if seen[abs] {
			continue
		}
		seen[abs] = true
		sub, err := ParseFile(abs)
		if err != nil {
			return fmt.Errorf("导入 %s 失败: %w", imp.Path, err)
		}
		if err := expandImports(sub, filepath.Dir(abs), seen, depth+1); err != nil {
			return err
		}
		out = append(out, sub.Statements...)
	}
	prog.Statements = out
	return nil
}

// EmitC 解析 .hy 并生成 C 源码文本
func EmitC(path string) (string, *ast.Program, error) {
	src, readErr := os.ReadFile(path)
	if readErr != nil {
		return "", nil, fmt.Errorf("无法读取文件 %s: %w", path, readErr)
	}
	prog, err := ParseFile(path)
	if err != nil {
		return "", nil, err
	}
	abs, _ := filepath.Abs(path)
	if err := expandImports(prog, filepath.Dir(abs), map[string]bool{abs: true}, 0); err != nil {
		return "", prog, err
	}
	// 编译能力闸门：语法通过不等于能编译。
	// 类型检查前置，让 --emit-c / --emit-exe 与解释器、-c 的接受集合一致；
	// 默认只警告不阻断（推断器仍有假阳，避免误伤合法程序），--strict 才阻断。
	if warnText, hit := CheckProgram(prog, path, string(src)); hit {
		if strictTypes {
			return "", prog, &DiagError{Text: warnText}
		}
		fmt.Fprint(os.Stderr, warnText)
	}
	code, err := Generate(prog, Options{SourcePath: path, Strict: strictTypes})
	if err != nil {
		return "", prog, err
	}
	return code, prog, nil
}

// WriteC 生成 C 源码并落盘到 outPath（同时把运行时头文件放到同目录）
func WriteC(srcPath, outPath string) (*ast.Program, error) {
	code, prog, err := EmitC(srcPath)
	if err != nil {
		return prog, err
	}
	dir := filepath.Dir(outPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return prog, err
	}
	if err := os.WriteFile(outPath, []byte(code), 0o644); err != nil {
		return prog, err
	}
	// 运行时文件随产物落盘，便于用户单独拿 .c 去编译
	if err := os.WriteFile(filepath.Join(dir, "zuzo_rt.h"), []byte(runtimeH), 0o644); err != nil {
		return prog, err
	}
	if err := os.WriteFile(filepath.Join(dir, "zuzo_rt.c"), []byte(runtimeC), 0o644); err != nil {
		return prog, err
	}
	return prog, nil
}

// BuildExeNative 生成 C 并用内嵌 nativecc 编译器（纯 Go 自研 C 编译器直接出 PE，零外部依赖）。
// 返回 (C 源码路径, 可执行文件路径, error)。
func BuildExeNative(srcPath, outExe string) (string, string, error) {
	work, err := os.MkdirTemp("", "zuzo-ncc-")
	if err != nil {
		return "", "", err
	}
	if os.Getenv("ZUZO_KEEP_TEMP") == "" {
		defer os.RemoveAll(work)
	} else {
		fmt.Fprintf(os.Stderr, "[cbackend] 中间目录: %s\n", work)
	}
	code, _, err := EmitC(srcPath)
	if err != nil {
		return "", "", err
	}
	cPath := filepath.Join(work, "main.c")
	rtC := filepath.Join(work, "zuzo_rt.c")
	rtH := filepath.Join(work, "zuzo_rt.h")
	for _, w := range []struct {
		path string
		data string
	}{
		{cPath, code}, {rtC, runtimeC}, {rtH, runtimeH},
	} {
		if err := os.WriteFile(w.path, []byte(w.data), 0o644); err != nil {
			return cPath, "", err
		}
	}
	if outExe == "" {
		base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
		outExe = filepath.Join(filepath.Dir(srcPath), base+exeSuffix())
	}
	absOut, err := filepath.Abs(outExe)
	if err != nil {
		return cPath, outExe, err
	}
	srcs := make([]nativecc.Source, 0, 2)
	for _, p := range []string{cPath, rtC} {
		s, err := nativecc.LoadSource(p)
		if err != nil {
			return cPath, absOut, err
		}
		srcs = append(srcs, s)
	}
	buildOut := filepath.Join(work, "a.out"+exeSuffix())
	if err := nativecc.CompileToExe(srcs, buildOut, nativecc.Options{}); err != nil {
		return cPath, absOut, fmt.Errorf("内嵌编译器失败: %w", err)
	}
	if err := copyFile(buildOut, absOut); err != nil {
		return cPath, absOut, err
	}
	return cPath, absOut, nil
}

// BuildExe 生成 C 并调用本机编译器产出可执行文件。
// 返回 (C 源码路径, 可执行文件路径, error)。
func BuildExe(srcPath, outExe string) (string, string, error) {
	cc, err := FindCompiler()
	if err != nil {
		return "", "", err
	}
	work, err := os.MkdirTemp("", "zuzo-cbuild-")
	if err != nil {
		return "", "", err
	}
	// 调试时可保留：ZUZO_KEEP_TEMP=1
	if os.Getenv("ZUZO_KEEP_TEMP") == "" {
		defer os.RemoveAll(work)
	} else {
		fmt.Fprintf(os.Stderr, "[cbackend] 中间目录: %s\n", work)
	}

	code, _, err := EmitC(srcPath)
	if err != nil {
		return "", "", err
	}
	cPath := filepath.Join(work, "main.c")
	if err := os.WriteFile(cPath, []byte(code), 0o644); err != nil {
		return "", "", err
	}
	rtC := filepath.Join(work, "zuzo_rt.c")
	rtH := filepath.Join(work, "zuzo_rt.h")
	if err := os.WriteFile(rtC, []byte(runtimeC), 0o644); err != nil {
		return "", "", err
	}
	if err := os.WriteFile(rtH, []byte(runtimeH), 0o644); err != nil {
		return "", "", err
	}

	if outExe == "" {
		base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
		outExe = filepath.Join(filepath.Dir(srcPath), base+exeSuffix())
	}
	absOut, err := filepath.Abs(outExe)
	if err != nil {
		return cPath, outExe, err
	}

	// Windows 下 gcc/ld 对非 ASCII 路径支持不稳，统一在临时目录出产物再复制
	buildOut := filepath.Join(work, "a.out"+exeSuffix())
	args := append([]string{}, cc.Args[1:]...)
	args = append(args,
		"-O2", "-std=c11",
		"-I", work,
		"-o", buildOut,
		cPath, rtC,
		"-lm",
	)
	cmd := exec.Command(cc.Args[0], args...)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		return cPath, absOut, fmt.Errorf("C 编译失败（%s）：\n%s", cc.Name, string(out))
	}
	if err := copyFile(buildOut, absOut); err != nil {
		return cPath, absOut, err
	}
	return cPath, absOut, nil
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if dir := filepath.Dir(dst); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(dst, data, 0o755)
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
