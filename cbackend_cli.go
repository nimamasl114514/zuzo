package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"zuzo/cbackend"
)

// parseEmitArgs 解析 `--emit-c <src> [-o <out>]` / `--emit-exe <src> [-o <out>] [--cc=<mode>]`
// cc: "native"（内嵌自研编译器）| "gcc"（外部 gcc/clang）| "auto"（native 优先，失败回退 gcc）
func parseEmitArgs(args []string) (src, out, cc string, err error) {
	cc = "auto"
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "-o" || arg == "--out":
			if i+1 >= len(args) {
				return "", "", "", fmt.Errorf("-o 缺少输出路径")
			}
			out = args[i+1]
			i++
		case strings.HasPrefix(arg, "--cc="):
			cc = strings.TrimPrefix(arg, "--cc=")
			if cc != "native" && cc != "gcc" && cc != "auto" {
				return "", "", "", fmt.Errorf("未知编译器模式: %s（可选 native / gcc / auto）", cc)
			}
		case strings.HasPrefix(arg, "-"):
			return "", "", "", fmt.Errorf("未知参数: %s", arg)
		default:
			if src == "" {
				src = arg
			} else {
				return "", "", "", fmt.Errorf("多余的参数: %s（只接受一个源文件）", arg)
			}
		}
	}
	if src == "" {
		return "", "", "", fmt.Errorf("缺少源文件参数")
	}
	return src, out, cc, nil
}

// printBackendErr 统一输出后端错误：
// 诊断类错误（语法/类型）已自带完整排版，直接输出；其他后端错误加前缀并附中间产物。
func printBackendErr(err error, src, cPath string) {
	var de *cbackend.DiagError
	if errors.As(err, &de) {
		fmt.Fprint(os.Stderr, de.Text)
		if !strings.HasSuffix(de.Text, "\n") {
			fmt.Fprintln(os.Stderr)
		}
		return
	}
	msg := err.Error()
	// 后端错误消息以「行:列:」开头时，补上源文件名，与前端诊断的 file:line:col 口径一致
	if src != "" && len(msg) > 2 && msg[0] >= '0' && msg[0] <= '9' {
		if i := strings.Index(msg, ": "); i > 0 && strings.Count(msg[:i], ":") == 1 {
			msg = src + ":" + msg
		}
	}
	fmt.Fprintf(os.Stderr, "C 后端错误: %s\n", msg)
	if cPath != "" {
		fmt.Fprintf(os.Stderr, "（中间 C 源码: %s）\n", cPath)
	}
}

func emitCCommand(args []string) {
	src, out, _, err := parseEmitArgs(args)
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
		printBackendErr(err, src, "")
		os.Exit(1)
	}
	fmt.Printf("已生成 C 源码: %s\n", absOut)
	fmt.Printf("运行时文件: %s\n", filepath.Join(filepath.Dir(absOut), "zuzo_rt.c"))
}

func emitExeCommand(args []string) {
	src, out, cc, err := parseEmitArgs(args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "错误: %v\n用法: zuzo --emit-exe <文件.hy> [-o 输出.exe] [--cc=native|gcc|auto]\n", err)
		os.Exit(2)
	}

	build := func(useNative bool) (string, string, error) {
		if useNative {
			return cbackend.BuildExeNative(src, out)
		}
		return cbackend.BuildExe(src, out)
	}

	var cPath, exePath string
	var buildErr error
	switch cc {
	case "native":
		cPath, exePath, buildErr = build(true)
	case "gcc":
		cPath, exePath, buildErr = build(false)
	default: // auto：native 优先，失败回退 gcc
		cPath, exePath, buildErr = build(true)
		if buildErr != nil {
			fmt.Fprintf(os.Stderr, "[cbackend] 内嵌编译器失败，回退外部 gcc：%v\n", buildErr)
			cPath, exePath, buildErr = build(false)
		}
	}
	if buildErr != nil {
		printBackendErr(buildErr, src, cPath)
		os.Exit(1)
	}
	fmt.Printf("已生成可执行文件: %s\n", exePath)
}
