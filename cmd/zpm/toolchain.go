package main

// toolchain.go — zpm 的工具链与构建子命令（第32轮）
//
//	zpm doctor              体检：工具链（clang / zig cc）、zuzo 可执行文件、当前目录清单
//	zpm build [入口] [-o 输出]  把入口 .hy 编译成原生 exe（走 zuzo --emit-exe；没 clang 时自动用自带的 zig cc）
//	zpm toolchain [status|install|env]  直接透传 zuzo toolchain
//
// 设计口径：zpm 自己不做编译，编译能力全部复用 zuzo；zpm 只负责「找到对的 zuzo 与对的 CC」。

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"zuzo/toolchain"
	"zuzo/zpm"
)

// findZuzo 定位 zuzo 可执行文件：ZUZO_BIN → zpm 同级 → PATH
func findZuzo() string {
	if v := strings.TrimSpace(os.Getenv("ZUZO_BIN")); v != "" {
		return v
	}
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		name := "zuzo"
		if filepath.Ext(exe) == ".exe" {
			name = "zuzo.exe"
		}
		for _, c := range []string{filepath.Join(dir, name), filepath.Join(dir, "..", name)} {
			if st, err := os.Stat(c); err == nil && !st.IsDir() {
				return c
			}
		}
	}
	if p, err := exec.LookPath("zuzo"); err == nil {
		return p
	}
	return ""
}

// cmdDoctor 体检
func cmdDoctor(args []string) {
	fmt.Println("zpm doctor")
	fmt.Println("----------")
	r := toolchain.Status()
	fmt.Printf("编译器      : %s\n", r.CC)
	if r.CCPath != "" {
		fmt.Printf("编译器路径   : %s\n", r.CCPath)
	}
	if r.Zig != "" {
		fmt.Printf("zig         : %s %s\n", r.Zig, r.ZigVer)
	} else {
		fmt.Println("zig         : 未找到（zuzo toolchain install 可安装；不影响已有 clang 的机器）")
	}
	if r.Clang != "" {
		fmt.Printf("clang       : %s\n", r.Clang)
	} else {
		fmt.Println("clang       : 未找到（有 zig cc 兜底）")
	}
	if z := findZuzo(); z != "" {
		out, _ := exec.Command(z, "--version").Output()
		fmt.Printf("zuzo        : %s %s\n", z, strings.TrimSpace(string(out)))
	} else {
		fmt.Println("zuzo        : 未找到（设 ZUZO_BIN 指向 zuzo.exe）")
	}
	if m, err := zpm.LoadManifest("zuzo.zpm"); err == nil {
		fmt.Printf("当前包      : %s %s（入口 %s，依赖 %d）\n", m.Name, m.Version, m.Entry, len(m.Dependencies))
	} else if m, err := zpm.LoadManifest("zpm.toml"); err == nil {
		fmt.Printf("当前包      : %s %s（入口 %s，依赖 %d）\n", m.Name, m.Version, m.Entry, len(m.Dependencies))
	} else {
		fmt.Println("当前包      : 未在本目录找到清单（zpm init 可初始化）")
	}
	fmt.Printf("工具链目录   : %s\n", r.Install)
}

// cmdBuild 把 .hy 编译成原生 exe
func cmdBuild(args []string) {
	entry := ""
	out := ""
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-o", "--output":
			if i+1 < len(args) {
				out = args[i+1]
				i++
			}
		default:
			if !strings.HasPrefix(args[i], "-") && entry == "" {
				entry = args[i]
			}
		}
	}
	if entry == "" {
		// 从清单取入口，否则 common 名
		for _, name := range []string{"zuzo.zpm", "zpm.toml"} {
			if m, err := zpm.LoadManifest(name); err == nil && m.Entry != "" {
				entry = m.Entry
				break
			}
		}
	}
	if entry == "" {
		for _, cand := range []string{"主程序.hy", "main.hy", "src/main.hy"} {
			if st, err := os.Stat(cand); err == nil && !st.IsDir() {
				entry = cand
				break
			}
		}
	}
	if entry == "" {
		fmt.Fprintln(os.Stderr, "zpm build: 没找到入口文件（用法：zpm build <入口.hy> [-o 输出.exe]）")
		os.Exit(2)
	}
	zuzo := findZuzo()
	if zuzo == "" {
		fmt.Fprintln(os.Stderr, "zpm build: 找不到 zuzo（设 ZUZO_BIN 指向 zuzo.exe）")
		os.Exit(2)
	}
	argv := []string{"--emit-exe", entry}
	if out != "" {
		argv = append(argv, "-o", out)
	}
	fmt.Printf("构建: %s %s\n", zuzo, strings.Join(argv, " "))
	cmd := exec.Command(zuzo, argv...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "zpm build 失败: %v\n", err)
		os.Exit(1)
	}
	if out != "" {
		fmt.Printf("产物: %s\n", out)
	}
}

// cmdToolchain 透传 zuzo toolchain
func cmdToolchain(args []string) {
	zuzo := findZuzo()
	if zuzo == "" {
		fmt.Fprintln(os.Stderr, "zpm toolchain: 找不到 zuzo（设 ZUZO_BIN 指向 zuzo.exe）")
		os.Exit(2)
	}
	argv := append([]string{"toolchain"}, args...)
	cmd := exec.Command(zuzo, argv...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			os.Exit(ee.ExitCode())
		}
		os.Exit(1)
	}
	_ = sort.Strings
}
