package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zuzo/cbackend"
)

// parseEmitArgs 解析 `--emit-c <src> [-o <out>]` / `--emit-exe <src> [-o <out>]`
func parseEmitArgs(args []string) (src, out string, err error) {
	if len(args) < 1 {
		return "", "", fmt.Errorf("缺少源文件参数")
	}
	src = args[0]
	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "-o", "--out":
			if i+1 >= len(args) {
				return "", "", fmt.Errorf("-o 缺少输出路径")
			}
			out = args[i+1]
			i++
		default:
			return "", "", fmt.Errorf("未知参数: %s", args[i])
		}
	}
	return src, out, nil
}

func emitCCommand(args []string) {
	src, out, err := parseEmitArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n用法: zuzo --emit-c <文件.hy> [-o 输出.c]\n", err)
		os.Exit(2)
	}
	if out == "" {
		base := strings.TrimSuffix(src, filepath.Ext(src))
		out = base + ".c"
	}
	absOut, _ := filepath.Abs(out)
	if _, err := cbackend.WriteC(src, absOut); err != nil {
		fmt.Fprintf(os.Stderr, "C 后端错误: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("已生成 C 源码: %s\n", absOut)
	fmt.Printf("运行时文件: %s\n", filepath.Join(filepath.Dir(absOut), "zuzo_rt.c"))
}

func emitExeCommand(args []string) {
	src, out, err := parseEmitArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n用法: zuzo --emit-exe <文件.hy> [-o 输出.exe]\n", err)
		os.Exit(2)
	}
	cPath, exePath, err := cbackend.BuildExe(src, out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "C 后端错误: %v\n", err)
		if cPath != "" {
			fmt.Fprintf(os.Stderr, "（中间 C 源码: %s）\n", cPath)
		}
		os.Exit(1)
	}
	fmt.Printf("已生成可执行文件: %s\n", exePath)
}
