package main

// zuzo test —— 极简测试运行器
//
// 约定：用例文件以 `_test.hy` 结尾，退出码 0 = PASS，非 0 = FAIL。
// 每个用例都用「当前这个 zuzo 可执行文件」开子进程跑（os.Executable），
// 因此测的就是刚落盘的这个二进制，不会串到 PATH 上的旧版本。
//
// 用法:
//   zuzo test                    跑当前目录（递归）
//   zuzo test examples/tests     跑指定目录（递归）
//   zuzo test a_test.hy b_test.hy 跑指定用例文件
//   zuzo test <目录|文件> --verbose   每个用例打一行 PASS/FAIL
//
// 退出码: 全过 0 / 有失败 1 / 没找到用例或参数非法 2

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

const testSuffix = "_test.hy"

// testCase 一个待跑用例。
type testCase struct {
	abs string // 绝对路径
	rel string // 相对当前工作目录的展示路径（尽量短）
}

// testFailure 一条失败记录。
type testFailure struct {
	rel  string
	line string // stderr 首行（人读）
}

// runTestCommand 实现 `zuzo test [目录|文件...] [--verbose]`。
func runTestCommand(args []string) {
	verbose := false
	var targets []string
	for _, a := range args {
		switch a {
		case "--verbose", "-v":
			verbose = true
		case "-h", "--help", "help":
			printTestHelp()
			return
		default:
			if strings.HasPrefix(a, "-") {
				fmt.Fprintf(os.Stderr, "zuzo test: 未知选项 %s\n", a)
				printTestHelpTo(os.Stderr)
				os.Exit(2)
			}
			targets = append(targets, a)
		}
	}
	if len(targets) == 0 {
		targets = []string{"."}
	}

	cases, err := collectTestCases(targets)
	if err != nil {
		fmt.Fprintf(os.Stderr, "zuzo test: %v\n", err)
		os.Exit(2)
	}
	if len(cases) == 0 {
		fmt.Fprintf(os.Stderr, "zuzo test: 未找到 *%s 用例（目标: %s）\n",
			testSuffix, strings.Join(targets, " "))
		os.Exit(2)
	}

	exe, err := os.Executable()
	if err != nil {
		fmt.Fprintf(os.Stderr, "zuzo test: 无法定位当前可执行文件: %v\n", err)
		os.Exit(2)
	}

	pass := 0
	var failures []testFailure
	for _, c := range cases {
		code, stderrText := runOneTest(exe, c.abs)
		if code == 0 {
			pass++
			if verbose {
				fmt.Printf("PASS %s\n", c.rel)
			}
			continue
		}
		f := testFailure{rel: c.rel, line: firstMeaningfulLine(stderrText, code)}
		failures = append(failures, f)
		// verbose 下用例行直接用带原因的完整形式，避免与汇总的 FAIL 行重复刷屏
		if verbose {
			fmt.Printf("FAIL %s — %s\n", f.rel, f.line)
		}
	}

	if !verbose {
		for _, f := range failures {
			fmt.Printf("FAIL %s — %s\n", f.rel, f.line)
		}
	}
	fmt.Printf("PASS %d / FAIL %d\n", pass, len(failures))

	switch {
	case len(failures) > 0:
		os.Exit(1)
	default:
		os.Exit(0)
	}
}

// runOneTest 用 exe 跑单个用例，返回退出码与 stderr 文本。
// 用例的工作目录设为其所在目录，方便用例里用相对路径引用同级资源。
func runOneTest(exe, path string) (int, string) {
	cmd := exec.Command(exe, path)
	cmd.Dir = filepath.Dir(path)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
			if code < 0 { // 被信号杀掉
				code = 1
			}
		} else {
			return 1, fmt.Sprintf("启动失败: %v", err)
		}
	}
	if code != 0 && stderr.Len() == 0 {
		// 有的失败只写 stdout，兜一层
		return code, stdout.String()
	}
	return code, stderr.String()
}

// collectTestCases 把目录/文件参数展开成用例列表（去重、排序）。
func collectTestCases(targets []string) ([]testCase, error) {
	seen := make(map[string]bool)
	var out []testCase
	add := func(abs string) {
		abs = filepath.Clean(abs)
		if seen[abs] {
			return
		}
		seen[abs] = true
		out = append(out, testCase{abs: abs, rel: displayPath(abs)})
	}

	for _, t := range targets {
		info, err := os.Stat(t)
		if err != nil {
			return nil, fmt.Errorf("无法访问 %s: %v", t, err)
		}
		if !info.IsDir() {
			if !strings.HasSuffix(strings.ToLower(t), testSuffix) {
				return nil, fmt.Errorf("%s 不是用例文件（用例需以 %s 结尾）", t, testSuffix)
			}
			abs, err := filepath.Abs(t)
			if err != nil {
				return nil, err
			}
			add(abs)
			continue
		}
		root, err := filepath.Abs(t)
		if err != nil {
			return nil, err
		}
		walkErr := filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				// 跳过 .git（以及只放产物的 node_modules，避免扫进第三方库）
				name := d.Name()
				if p != root && (name == ".git" || name == "node_modules") {
					return filepath.SkipDir
				}
				return nil
			}
			if strings.HasSuffix(strings.ToLower(d.Name()), testSuffix) {
				add(p)
			}
			return nil
		})
		if walkErr != nil {
			return nil, fmt.Errorf("遍历 %s 失败: %v", t, walkErr)
		}
	}

	sort.Slice(out, func(i, j int) bool { return out[i].rel < out[j].rel })
	return out, nil
}

// displayPath 相对当前目录的展示路径，跨盘等无法求相对时退回绝对路径。
func displayPath(abs string) string {
	wd, err := os.Getwd()
	if err != nil {
		return abs
	}
	rel, err := filepath.Rel(wd, abs)
	if err != nil || strings.HasPrefix(rel, "..") {
		return abs
	}
	return filepath.ToSlash(rel)
}

// firstMeaningfulLine 取 stderr 的「首行」用于汇总：
// 跳过空行与纯装饰的分隔线（━━━ ...），这样 FAIL 行不会只剩一溜横杠。
func firstMeaningfulLine(text string, code int) string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	fallback := ""
	for _, raw := range strings.Split(text, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		// 跳过装饰线（━━━ ...）与纯分隔行：它们只有噪声，没有信息
		if isDecorLine(line) || strings.HasPrefix(line, "━━━") || strings.HasPrefix(line, "───") {
			continue
		}
		if fallback == "" {
			fallback = line
		}
		// 优先取定位到根因的那行，可读性最好
		if strings.Contains(line, "根因") || strings.Contains(line, "错误") || strings.Contains(line, "Error") {
			return truncateLine(line, 120)
		}
	}
	if fallback != "" {
		return truncateLine(fallback, 120)
	}
	return fmt.Sprintf("退出码 %d（无输出）", code)
}

// isDecorLine 判断是否纯装饰行（只有 ━ ═ ─ = 之类的字符）。
func isDecorLine(line string) bool {
	for _, r := range line {
		switch r {
		case '━', '─', '═', '—', '-', '=', ' ', '\t':
			continue
		default:
			return false
		}
	}
	return true
}

func truncateLine(s string, n int) string {
	rs := []rune(s)
	if len(rs) <= n {
		return s
	}
	return string(rs[:n]) + "…"
}

func printTestHelp() { printTestHelpTo(os.Stdout) }

func printTestHelpTo(w *os.File) {
	fmt.Fprintln(w, `用法: zuzo test [目录|文件...] [--verbose]
  无参数            跑当前目录（递归找 *_test.hy）
  目录              递归跑该目录下的 *_test.hy（跳过 .git）
  文件              跑指定用例（必须 *_test.hy 结尾）
  --verbose, -v     每个用例打印一行 PASS/FAIL
退出码: 全过 0 / 有失败 1 / 没找到用例 2`)
}
