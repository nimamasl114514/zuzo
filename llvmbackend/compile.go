package llvmbackend

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
	"zuzo/parser"
)

//go:embed runtime/zuzo_rt.c
var runtimeC string

//go:embed runtime/zuzo_rt.h
var runtimeH string

var strictTypes bool

// SetStrictTypes 设置严格类型模式（与解释器 --strict 对齐）。
func SetStrictTypes(v bool) { strictTypes = v }

// DiagError 携带已渲染好的诊断文本，CLI 直接输出即可。
type DiagError struct{ Text string }

func (e *DiagError) Error() string { return e.Text }

// ParseFile 读取并解析一个 .hy 源文件。
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

// CheckProgram 静态类型检查：返回渲染文本与是否命中问题。
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

// EmitLLVM 解析源码并生成 LLVM IR 文本。
func EmitLLVM(path string) (string, *ast.Program, error) {
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

// WriteLLVM 生成 .ll 文件。
func WriteLLVM(srcPath, outPath string) (*ast.Program, error) {
	code, prog, err := EmitLLVM(srcPath)
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
	return prog, nil
}

// FindClang 定位 clang：ZUZO_CLANG → PATH → 常见安装路径。
func FindClang() (string, error) {
	if v := strings.TrimSpace(os.Getenv("ZUZO_CLANG")); v != "" {
		return v, nil
	}
	if p, err := exec.LookPath("clang"); err == nil {
		return p, nil
	}
	var candidates []string
	if runtime.GOOS == "windows" {
		candidates = []string{
			`C:\Program Files\LLVM\bin\clang.exe`,
			`C:\Program Files (x86)\LLVM\bin\clang.exe`,
			`C:\msys64\mingw64\bin\clang.exe`,
		}
	} else {
		candidates = []string{"/usr/bin/clang", "/usr/local/bin/clang"}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c, nil
		}
	}
	return "", fmt.Errorf("未找到 clang：请安装 LLVM（或设置环境变量 ZUZO_CLANG 指向 clang）")
}

func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

// BuildExe 把源码编译为可执行文件：IR → clang -c → 链接运行时 obj。
func BuildExe(srcPath, outExe string) (llPath string, exePath string, err error) {
	clang, err := FindClang()
	if err != nil {
		return "", "", err
	}
	work, err := os.MkdirTemp("", "zuzo-llvm-")
	if err != nil {
		return "", "", err
	}
	if os.Getenv("ZUZO_KEEP_TEMP") == "" {
		defer os.RemoveAll(work)
	} else {
		fmt.Fprintf(os.Stderr, "[llvmbackend] 中间目录: %s\n", work)
	}

	code, _, err := EmitLLVM(srcPath)
	if err != nil {
		return "", "", err
	}
	llPath = filepath.Join(work, "main.ll")
	if err := os.WriteFile(llPath, []byte(code), 0o644); err != nil {
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
		return llPath, outExe, err
	}

	run := func(args ...string) ([]byte, error) {
		cmd := exec.Command(clang, args...)
		cmd.Dir = work
		return cmd.CombinedOutput()
	}

	mainObj := filepath.Join(work, "main.obj")
	if out, err := run("-c", "-O2", "-Wno-override-module", "-x", "ir", llPath, "-o", mainObj); err != nil {
		return llPath, absOut, fmt.Errorf("LLVM IR 编译失败：\n%s", string(out))
	}
	rtObj := filepath.Join(work, "zuzo_rt.obj")
	if out, err := run("-c", "-O2", "-std=c11", "-I", work, rtC, "-o", rtObj); err != nil {
		return llPath, absOut, fmt.Errorf("运行时编译失败：\n%s", string(out))
	}
	buildOut := filepath.Join(work, "a.out"+exeSuffix())
	if out, err := run(mainObj, rtObj, "-o", buildOut); err != nil {
		return llPath, absOut, fmt.Errorf("链接失败：\n%s", string(out))
	}
	if err := copyFile(buildOut, absOut); err != nil {
		return llPath, absOut, err
	}
	return llPath, absOut, nil
}
