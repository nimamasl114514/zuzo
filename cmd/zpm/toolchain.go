package main

// toolchain.go — zpm 的工具链与构建子命令（第32轮，第33轮加 --release 与 Python 桥）
//
//	zpm doctor              体检：工具链（clang / zig cc）、zuzo 可执行文件、Python 桥（解释器/版本/模式）、当前目录清单
//	zpm build [入口] [-o 输出] [--release[=O0|O1|O2|O3|Os|Oz]]  把入口 .hy 编译成原生 exe（走 zuzo --emit-exe；没 clang 时自动用自带的 zig cc）
//	zpm toolchain [status|install|env]  直接透传 zuzo toolchain
//
// 设计口径：zpm 自己不做编译，编译能力全部复用 zuzo；zpm 只负责「找到对的 zuzo 与对的 CC」。
// --release 的实现口径：把子进程环境里的 ZUZO_CC 改写成「真实编译器 + 发布参数」，
// 并用 ZUZO_OPT 把优化档位（默认 O3）交给 llvmbackend（它据此替换内置的 -O2），
// strip / gc-sections 仍由 ZUZO_CC 前缀里的 -s 等参数负责（详见 releaseCC 与 releaseFlags）。

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
		doctorPythonBridge(z)
	} else {
		fmt.Println("zuzo        : 未找到（设 ZUZO_BIN 指向 zuzo.exe）")
		fmt.Println("Python 桥: 未知（需要 zuzo 才能探测；设 ZUZO_BIN 指向 zuzo.exe）")
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

// doctorPythonBridge 打印 Python 桥三件（解释器 / 版本 / 模式），口径与 zuzo 的 py信息() 完全一致。
// 只在 doctor 里报，不阻断体检：桥不可用时给出可操作提示。
func doctorPythonBridge(zuzo string) {
	// 三行分开打印，避免依赖哈希的打印格式（键顺序不稳定）。
	const script = "信息 = py信息()\n" +
		"打印(信息[\"解释器\"])\n" +
		"打印(信息[\"版本\"])\n" +
		"打印(信息[\"模式\"])"

	outBytes, err := exec.Command(zuzo, "-e", script).Output()
	if err != nil {
		fmt.Printf("Python 桥: 不可用（%v；可先跑 zuzo -e '打印(py信息())' 看详情）\n", err)
		return
	}
	out := strings.TrimSpace(strings.ReplaceAll(string(outBytes), "\r\n", "\n"))
	lines := strings.Split(out, "\n")
	if len(lines) < 3 {
		fmt.Printf("Python 桥: 输出异常（%q；可先跑 zuzo -e '打印(py信息())' 看详情）\n", out)
		return
	}
	fmt.Println("Python 桥: 可用")
	fmt.Printf("  解释器      : %s\n", strings.TrimSpace(lines[0]))
	fmt.Printf("  版本        : %s\n", strings.TrimSpace(lines[1]))
	fmt.Printf("  模式        : %s\n", strings.TrimSpace(lines[2]))
}

// releaseTail --release 注入编译器的非优化类参数：
//   - -s                               strip 符号表（体积主要收益，COFF 符号表占大头）
//   - -ffunction-sections -fdata-sections -Wl,--gc-sections   链接期裁掉未引用的运行时函数
const releaseTail = "-s -ffunction-sections -fdata-sections -Wl,--gc-sections"

// defaultReleaseLevel 是 --release 不带级别时的优化档位。
const defaultReleaseLevel = "O3"

// parseReleaseLevel 把用户输入的档位规范成 O0/O1/O2/O3/Os/Oz（大小写不敏感，容忍前导 '-'）。
// 白名单以外的值返回 false，由调用方报错退出，绝不静默降级。
func parseReleaseLevel(s string) (string, bool) {
	v := strings.TrimSpace(s)
	v = strings.TrimPrefix(v, "-")
	if len(v) < 2 || (v[0] != 'O' && v[0] != 'o') {
		return "", false
	}
	switch strings.ToLower(v[1:]) {
	case "0", "1", "2", "3":
		return "O" + v[1:], true
	case "s":
		return "Os", true
	case "z":
		return "Oz", true
	}
	return "", false
}

// releaseFlags 返回 --release 的完整参数串：优化档位在前，strip/gc 在后。
func releaseFlags(level string) string {
	return "-" + level + " " + releaseTail
}

// releaseCC 组装 --release 用的 ZUZO_CC 值：真实编译器路径 + 必要前缀 + 发布参数。
// zuzo 侧 llvmbackend.FindCompiler 先按空格切分 ZUZO_CC（首段是可执行文件，其余当前缀），
// 所以这里的路径不能含空格；zig 需要补 "cc -target x86_64-windows-msvc" 才能当 CC 用。
func releaseCC(level string) (string, error) {
	flags := releaseFlags(level)
	// 用户已显式指定：保留原样，只追加发布参数
	if v := strings.TrimSpace(os.Getenv("ZUZO_CC")); v != "" {
		exe := strings.Fields(v)[0]
		if st, err := os.Stat(exe); err != nil || st.IsDir() {
			return "", fmt.Errorf("ZUZO_CC 首段不是可执行文件：%s（路径含空格时请先用 zuzo toolchain install 换到无空格目录）", exe)
		}
		return v + " " + flags, nil
	}

	r := toolchain.Status()
	exe, prefix := "", ""
	switch {
	case r.CCPath != "" && strings.Contains(strings.ToLower(r.CCPath), "zig"):
		exe, prefix = r.CCPath, "cc -target x86_64-windows-msvc"
	case r.Clang != "":
		exe = r.Clang
	case r.Zig != "":
		exe, prefix = r.Zig, "cc -target x86_64-windows-msvc"
	default:
		return "", fmt.Errorf("未找到 C 编译器（先跑 zpm doctor 或 zuzo toolchain install）")
	}
	exe = filepath.ToSlash(strings.TrimSpace(exe))
	if strings.ContainsAny(exe, " \t") {
		return "", fmt.Errorf("编译器路径含空格，无法通过 ZUZO_CC 注入发布参数：%s（可设 ZUZO_CC 显式指定）", exe)
	}
	if prefix != "" {
		return exe + " " + prefix + " " + flags, nil
	}
	return exe + " " + flags, nil
}

// cmdBuild 把 .hy 编译成原生 exe
func cmdBuild(args []string) {
	entry := ""
	out := ""
	release := false
	level := defaultReleaseLevel
	for i := 0; i < len(args); i++ {
		a := args[i]
		// --release / --发布 支持两种写法：裸开关（默认 O3）与 =级别（O0/O1/O2/O3/Os/Oz）。
		switch {
		case a == "--release" || a == "--发布":
			release, level = true, defaultReleaseLevel
		case strings.HasPrefix(a, "--release=") || strings.HasPrefix(a, "--发布="):
			raw := a[strings.IndexByte(a, '=')+1:]
			lv, ok := parseReleaseLevel(raw)
			if !ok {
				fmt.Fprintf(os.Stderr, "zpm build: 非法的发布级别 %q（可选 O0/O1/O2/O3/Os/Oz）\n", raw)
				os.Exit(2)
			}
			release, level = true, lv
		case a == "-o" || a == "--output":
			if i+1 < len(args) {
				out = args[i+1]
				i++
			}
		default:
			if !strings.HasPrefix(a, "-") && entry == "" {
				entry = a
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
		fmt.Fprintln(os.Stderr, "zpm build: 没找到入口文件（用法：zpm build <入口.hy> [-o 输出.exe] [--release[=O0|O1|O2|O3|Os|Oz]]）")
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
	if release {
		ccEnv, err := releaseCC(level)
		if err != nil {
			fmt.Fprintf(os.Stderr, "zpm build --release: %v\n", err)
			os.Exit(2)
		}
		fmt.Printf("发布模式: -%s（ZUZO_OPT=-%s）\n", level, level)
		fmt.Printf("          ZUZO_CC=%s\n", ccEnv)
		// 子进程环境里改写 ZUZO_CC 与 ZUZO_OPT（os/exec 会去重，保留最后一个值）：
		// ZUZO_OPT 让 llvmbackend 用它替换内置的 -O2，档位才真正生效。
		cmd.Env = append(os.Environ(), "ZUZO_CC="+ccEnv, "ZUZO_OPT=-"+level)
	}
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "zpm build 失败: %v\n", err)
		os.Exit(1)
	}
	if out != "" {
		if release {
			if st, err := os.Stat(out); err == nil {
				fmt.Printf("产物: %s（%d 字节，已 strip）\n", out, st.Size())
				return
			}
		}
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
