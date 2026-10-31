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
	"zuzo/lexer"
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

// ParseFile 读取并解析一个 .hy 源文件
func ParseFile(path string) (*ast.Program, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("无法读取文件 %s: %w", path, err)
	}
	l := lexer.New(string(src))
	p := parser.New(l)
	prog := p.ParseProgram()
	if p.Diagnostics().HasErrors() {
		return nil, fmt.Errorf("语法错误：\n%s", p.Diagnostics().Render())
	}
	return prog, nil
}

// EmitC 解析 .hy 并生成 C 源码文本
func EmitC(path string) (string, *ast.Program, error) {
	prog, err := ParseFile(path)
	if err != nil {
		return "", nil, err
	}
	code, err := Generate(prog, Options{SourcePath: path})
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
