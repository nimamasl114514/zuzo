package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zuzo/llvmbackend"
)

// parseEmitArgs 解析 `--emit-llvm <src> [-o <out>]` / `--emit-exe <src> [-o <out>]`。
func parseEmitArgs(args []string) (src, out string, err error) {
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-o" || arg == "--out":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("-o 缺少输出路径")
			}
			out = args[i+1]
			i++
		case strings.HasPrefix(arg, "-"):
			return "", "", fmt.Errorf("未知参数: %s", arg)
		default:
			if src == "" {
				src = arg
			} else {
				return "", "", fmt.Errorf("多余的参数: %s（只接受一个源文件）", arg)
			}
		}
	}
	if src == "" {
		return "", "", fmt.Errorf("缺少源文件参数")
	}
	return src, out, nil
}

// printBackendErr 统一输出后端错误：诊断类错误已自带排版，直接输出；其余加前缀并附中间产物。
func printBackendErr(err error, src, llPath string) {
	var de *llvmbackend.DiagError
	if errors.As(err, &de) {
		fmt.Fprint(os.Stderr, de.Text)
		if !strings.HasSuffix(de.Text, "\n") {
			fmt.Fprintln(os.Stderr)
		}
		return
	}
	msg := err.Error()
	if src != "" && len(msg) > 2 && msg[0] >= '0' && msg[0] <= '9' {
		if i := strings.Index(msg, ": "); i > 0 && strings.Count(msg[:i], ":") == 1 {
			msg = src + ":" + msg
		}
	}
	fmt.Fprintf(os.Stderr, "LLVM 后端错误: %s\n", msg)
	if llPath != "" {
		if _, statErr := os.Stat(llPath); statErr == nil {
			fmt.Fprintf(os.Stderr, "（中间 LLVM IR: %s）\n", llPath)
		}
	}
}

func emitLLVMCommand(args []string) {
	src, out, err := parseEmitArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n用法: zuzo --emit-llvm <文件.hy> [-o 输出.ll]\n", err)
		os.Exit(2)
	}
	if out == "" {
		base := strings.TrimSuffix(src, filepath.Ext(src))
		out = base + ".ll"
	}
	absOut, _ := filepath.Abs(out)
	if _, err := llvmbackend.WriteLLVM(src, absOut); err != nil {
		printBackendErr(err, src, "")
		os.Exit(1)
	}
	fmt.Printf("已生成 LLVM IR: %s\n", absOut)
}

func emitExeCommand(args []string) {
	src, out, err := parseEmitArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n用法: zuzo --emit-exe <文件.hy> [-o 输出.exe]\n", err)
		os.Exit(2)
	}
	llPath, exePath, err := llvmbackend.BuildExe(src, out)
	if err != nil {
		printBackendErr(err, src, llPath)
		os.Exit(1)
	}
	fmt.Printf("已生成可执行文件: %s\n", exePath)
}
