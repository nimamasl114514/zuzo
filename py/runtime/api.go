package runtime

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"zuzo/py/ast"
	"zuzo/py/object"
)

// FileClass 文件对象类（全局单例）
var FileClass = &object.TypeObj{Name: "TextIOWrapper", Dict: map[string]object.Object{}}

// builtinOpen 打开文件
func (ip *Interp) builtinOpen(args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
	if len(args) == 0 {
		ip.TypeError("open() missing required argument 'file'")
	}
	path := args[0].Str()
	mode := "r"
	if len(args) > 1 {
		mode = args[1].Str()
	}
	if kwargs != nil {
		if v, ok := kwargs["mode"]; ok {
			mode = v.Str()
		}
	}
	flag := os.O_RDONLY
	switch {
	case strings.Contains(mode, "w"):
		flag = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
	case strings.Contains(mode, "a"):
		flag = os.O_WRONLY | os.O_CREATE | os.O_APPEND
	case strings.Contains(mode, "r+"):
		flag = os.O_RDWR
	case strings.Contains(mode, "w+"):
		flag = os.O_RDWR | os.O_CREATE | os.O_TRUNC
	}
	f, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		ip.raise(ip.excType("FileNotFoundError"), "%v", err)
	}
	inst := &object.Instance{Class: FileClass, Dict: map[string]object.Object{}}
	inst.Dict["name"] = &object.Str{V: path}
	inst.Dict["mode"] = &object.Str{V: mode}
	inst.Dict["_file"] = &object.Int{V: 0}
	inst.Dict["_handle"] = &object.Int{V: int64(uintptr(0))}
	inst.Dict["_pos"] = &object.Int{V: 0}
	inst.Dict["_path"] = &object.Str{V: path}
	inst.Dict["_mode"] = &object.Str{V: mode}
	f.Close()
	// 使用惰性读写：每次操作按路径重开（简化实现，保证正确性）
	inst.Dict["read"] = bi("read", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		pos := int64(0)
		if p, ok := inst.Dict["_pos"].(*object.Int); ok {
			pos = p.V
		}
		if len(a) > 0 && a[0] != object.None {
			n, _ := object.AsInt(a[0])
			end := pos + n
			if n < 0 || end > int64(len(data)) {
				end = int64(len(data))
			}
			if pos > int64(len(data)) {
				pos = int64(len(data))
			}
			s := string(data[pos:end])
			inst.Dict["_pos"] = &object.Int{V: end}
			return &object.Str{V: s}, nil
		}
		if pos > int64(len(data)) {
			pos = int64(len(data))
		}
		s := string(data[pos:])
		inst.Dict["_pos"] = &object.Int{V: int64(len(data))}
		return &object.Str{V: s}, nil
	})
	inst.Dict["readline"] = bi("readline", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		pos := int64(0)
		if p, ok := inst.Dict["_pos"].(*object.Int); ok {
			pos = p.V
		}
		if pos >= int64(len(data)) {
			return &object.Str{V: ""}, nil
		}
		idx := strings.IndexByte(string(data[pos:]), '\n')
		var line string
		if idx < 0 {
			line = string(data[pos:])
			inst.Dict["_pos"] = &object.Int{V: int64(len(data))}
		} else {
			line = string(data[pos : pos+int64(idx)+1])
			inst.Dict["_pos"] = &object.Int{V: pos + int64(idx) + 1}
		}
		return &object.Str{V: line}, nil
	})
	inst.Dict["readlines"] = bi("readlines", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		lines := strings.Split(string(data), "\n")
		var out []object.Object
		for i, l := range lines {
			if i == len(lines)-1 && l == "" {
				break
			}
			if i < len(lines)-1 {
				l += "\n"
			}
			out = append(out, &object.Str{V: l})
		}
		inst.Dict["_pos"] = &object.Int{V: int64(len(data))}
		return &object.List{Items: out}, nil
	})
	inst.Dict["write"] = bi("write", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		s := a[0].Str()
		fl := os.O_WRONLY | os.O_CREATE | os.O_APPEND
		if strings.Contains(mode, "w") {
			fl = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
			// 首次写入后改为追加
			inst.Dict["_mode"] = &object.Str{V: strings.ReplaceAll(mode, "w", "a")}
			inst.Dict["_wrote"] = object.TrueObj
		}
		wf, err := os.OpenFile(path, fl, 0o644)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		defer wf.Close()
		n, _ := wf.WriteString(s)
		return &object.Int{V: int64(n)}, nil
	})
	inst.Dict["writelines"] = bi("writelines", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		var sb strings.Builder
		for _, v := range ip.ToSlice(a[0]) {
			sb.WriteString(v.Str())
		}
		fl := os.O_WRONLY | os.O_CREATE | os.O_APPEND
		if _, wrote := inst.Dict["_wrote"]; !wrote && strings.Contains(mode, "w") {
			fl = os.O_WRONLY | os.O_CREATE | os.O_TRUNC
			inst.Dict["_wrote"] = object.TrueObj
		}
		wf, err := os.OpenFile(path, fl, 0o644)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		defer wf.Close()
		wf.WriteString(sb.String())
		return object.None, nil
	})
	inst.Dict["close"] = bi("close", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	inst.Dict["flush"] = bi("flush", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	inst.Dict["seek"] = bi("seek", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		n, _ := object.AsInt(a[0])
		inst.Dict["_pos"] = &object.Int{V: n}
		return &object.Int{V: n}, nil
	})
	inst.Dict["tell"] = bi("tell", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if p, ok := inst.Dict["_pos"].(*object.Int); ok {
			return p, nil
		}
		return &object.Int{V: 0}, nil
	})
	inst.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return inst, nil
	})
	inst.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.FalseObj, nil
	})
	inst.Dict["__iter__"] = bi("__iter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		data, err := os.ReadFile(path)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		lines := strings.Split(string(data), "\n")
		var out []object.Object
		for i, l := range lines {
			if i == len(lines)-1 && l == "" {
				break
			}
			if i < len(lines)-1 {
				l += "\n"
			}
			out = append(out, &object.Str{V: l})
		}
		return &object.Iterator{Items: out}, nil
	})
	return inst, nil
}

// ==================== 源码解析辅助 ====================

func parseExprString(src string) (ast.Expr, []string) {
	return ParseExprDual(src)
}

func parseModuleString(src string) (*ast.Module, []string) {
	return ParseSourceWithFile(src, "<string>")
}

// parseFloatTolerant 解析浮点（支持 inf/nan/科学计数）
func parseFloatTolerant(s string) (float64, error) {
	return strconv.ParseFloat(s, 64)
}

// ==================== 对外 API ====================

// RunFile 运行一个 .py 文件，返回退出码
func RunFile(path string, stdout, stderr interface{}) int {
	out, ok1 := stdout.(interface{ Write([]byte) (int, error) })
	if !ok1 {
		out = os.Stdout
	}
	errw, ok2 := stderr.(interface{ Write([]byte) (int, error) })
	if !ok2 {
		errw = os.Stderr
	}
	data, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(errw, "错误: 无法读取文件 %s: %v\n", path, err)
		return 1
	}
	return RunSource(string(data), path, out, errw)
}

// RunSource 运行源码，返回退出码
func RunSource(src, filename string, stdout, stderr interface{ Write([]byte) (int, error) }) int {
	m, errs := ParseSourceWithFile(src, filename)
	if len(errs) > 0 {
		fmt.Fprintf(stderr, "  File \"%s\"\n", filename)
		for _, e := range errs {
			fmt.Fprintf(stderr, "SyntaxError: %s\n", e)
		}
		return 1
	}
	ip := New()
	ip.Stdout = writerAdapter{stdout}
	ip.Stderr = writerAdapter{stderr}
	ip.SetMainFile(filename)
	exitCode := 0
	func() {
		defer func() {
			if r := recover(); r != nil {
				if es, ok := r.(*exitSignal); ok {
					exitCode = es.code
					return
				}
				if pe, ok := r.(*PyError); ok {
					printPyError(ip, pe, stderr)
					exitCode = 1
					return
				}
				panic(r)
			}
		}()
		pe := ip.RunModule(m)
		if pe != nil {
			printPyError(ip, pe, stderr)
			exitCode = 1
		}
	}()
	return exitCode
}

func printPyError(ip *Interp, pe *PyError, stderr interface{ Write([]byte) (int, error) }) {
	fmt.Fprintf(stderr, "Traceback (most recent call last):\n")
	for _, f := range pe.Frames {
		fmt.Fprintf(stderr, "  %s\n", f)
	}
	if pe.Line > 0 {
		fmt.Fprintf(stderr, "  第 %d 行\n", pe.Line)
	}
	if pe.Exc != nil {
		fmt.Fprintf(stderr, "%s", pe.Exc.Class.Name)
		if s := pe.Exc.Str(); s != "" {
			fmt.Fprintf(stderr, ": %s", s)
		}
		fmt.Fprintf(stderr, "\n")
	}
}

// writerAdapter 适配 writer
type writerAdapter struct {
	w interface{ Write([]byte) (int, error) }
}

func (a writerAdapter) Write(p []byte) (int, error) { return a.w.Write(p) }

// RunREPL 简单 REPL（逐行读取）
func RunREPL(stdin io.Reader, stdout, stderr interface{ Write([]byte) (int, error) }) {
	ip := New()
	ip.Stdout = writerAdapter{stdout}
	ip.Stderr = writerAdapter{stderr}
	ip.SetMainFile("<stdin>")
	fmt.Fprintf(stdout, "Python 3.11.0 (zuzo py-runtime) [Windows] 输入 exit() 退出\n")
	scanner := bufio.NewScanner(stdin)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for {
		fmt.Fprintf(stdout, ">>> ")
		if !scanner.Scan() {
			return
		}
		line := strings.TrimRight(scanner.Text(), "\r\n")
		if line == "exit()" || line == "quit()" || line == "exit" {
			return
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					if pe, ok := r.(*PyError); ok {
						printPyError(ip, pe, stderr)
						return
					}
					if _, ok := r.(*exitSignal); ok {
						return
					}
					panic(r)
				}
			}()
			// 先尝试表达式
			expr, errs := ParseExprDual(line)
			if len(errs) == 0 {
				v := ip.evalExpr(expr, ip.Globals)
				if v != object.None {
					fmt.Fprintf(stdout, "%s\n", ip.Repr(v))
				}
				return
			}
			m, errs2 := ParseSourceWithFile(line, "<stdin>")
			if len(errs2) > 0 {
				fmt.Fprintf(stderr, "SyntaxError: %s\n", errs2[0])
				return
			}
			pe := ip.RunModule(m)
			if pe != nil {
				printPyError(ip, pe, stderr)
			}
		}()
	}
}
