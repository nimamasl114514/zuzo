package toolchain

// toolchain.go — zuzo 自带工具链（第32轮）
//
// 解决的问题：`--emit-exe` 以前只认 clang，机器上没装 LLVM 就编不出 exe；
// go build 侧要 cgo 时又得自己找 gcc。zig cc 一个 exe 就带齐 clang 前端 + lld + libc，
// 所以把它定为「随 zuzo 自带的兜底工具链」：
//
//	查找顺序（zig）：ZUZO_ZIG → <zuzo.exe 同级>/tools/zig/zig.exe → <同级>/zig/zig.exe
//	                → PATH → %LOCALAPPDATA%\zuzo\toolchain\zig\zig.exe
//	查找顺序（CC）：ZUZO_CC → ZUZO_CLANG → clang（PATH + 常见安装路径）→ zig cc
//
// 用法：
//	zuzo toolchain status          看当前用谁
//	zuzo toolchain install [路径]   把 zig 装到用户工具链目录（默认从同级 tools/zig 或 PATH 复制）
//	zuzo toolchain env             打印可直接 source / set 的环境变量

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Report 当前工具链状态
type Report struct {
	Zig     string // zig 可执行文件路径（空 = 没有）
	ZigVer  string // zig version 输出首行
	Clang   string // clang 路径（空 = 没有）
	CC      string // 实际会被使用的编译器（"clang" / "zig cc" / 自定义）
	CCPath  string // 编译器可执行文件
	Install string // 用户工具链目录
}

// exeSuffix Windows 下补 .exe
func exeSuffix() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}

// InstallDir 用户级工具链目录
func InstallDir() string {
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		return filepath.Join(la, "zuzo", "toolchain", "zig")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".zuzo", "toolchain", "zig")
}

// selfDirs zuzo.exe 所在目录下的候选（自带布局）
func selfDirs() []string {
	exe, err := os.Executable()
	if err != nil {
		return nil
	}
	dir := filepath.Dir(exe)
	return []string{
		filepath.Join(dir, "tools", "zig"),
		filepath.Join(dir, "zig"),
	}
}

// FindZig 返回 zig 路径
func FindZig() string {
	if v := strings.TrimSpace(os.Getenv("ZUZO_ZIG")); v != "" {
		return v
	}
	var cands []string
	for _, d := range selfDirs() {
		cands = append(cands, filepath.Join(d, "zig"+exeSuffix()), filepath.Join(d, "zig.exe"))
	}
	if p, err := exec.LookPath("zig"); err == nil {
		cands = append(cands, p)
	}
	cands = append(cands, filepath.Join(InstallDir(), "zig"+exeSuffix()), filepath.Join(InstallDir(), "zig.exe"))
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && zigComplete(c) {
			return c
		}
	}
	return ""
}

// zigComplete zig 需要同级 lib/ 才能工作
func zigComplete(zigExe string) bool {
	st, err := os.Stat(filepath.Join(filepath.Dir(zigExe), "lib"))
	return err == nil && st.IsDir()
}

// FindClang 返回 clang 路径（不含 zig）
func FindClang() string {
	if v := strings.TrimSpace(os.Getenv("ZUZO_CLANG")); v != "" {
		if st, err := os.Stat(v); err == nil && !st.IsDir() {
			return v
		}
	}
	if p, err := exec.LookPath("clang"); err == nil {
		return p
	}
	var cands []string
	if runtime.GOOS == "windows" {
		cands = []string{
			`C:\Program Files\LLVM\bin\clang.exe`,
			`C:\Program Files (x86)\LLVM\bin\clang.exe`,
			`C:\msys64\mingw64\bin\clang.exe`,
		}
	} else {
		cands = []string{"/usr/bin/clang", "/usr/local/bin/clang"}
	}
	for _, c := range cands {
		if st, err := os.Stat(c); err == nil && !st.IsDir() && zigComplete(c) {
			return c
		}
	}
	return ""
}

// Status 汇总当前工具链
func Status() Report {
	r := Report{Install: InstallDir()}
	r.Zig = FindZig()
	if r.Zig != "" {
		out, err := exec.Command(r.Zig, "version").Output()
		if err == nil {
			r.ZigVer = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
		}
	}
	r.Clang = FindClang()
	if v := strings.TrimSpace(os.Getenv("ZUZO_CC")); v != "" && fileExists(strings.Fields(v)[0]) {
		r.CC, r.CCPath = "自定义(ZUZO_CC)", v
		if strings.Contains(strings.ToLower(v), "zig") {
			r.CC = "zig cc"
		}
	} else if r.Clang != "" {
		r.CC, r.CCPath = "clang", r.Clang
	} else if r.Zig != "" {
		r.CC, r.CCPath = "zig cc", r.Zig
	} else {
		r.CC = "（无）"
	}
	return r
}

// Install 把 zig 拷进用户工具链目录。
// from 为空时依次尝试：同级 tools/zig → PATH → ZUZO_ZIG。
func Install(from string) (string, error) {
	src := from
	if src == "" {
		src = FindZig()
	}
	if src == "" {
		return "", fmt.Errorf("找不到 zig：请先下载 zig（https://ziglang.org/download/），再用 zuzo toolchain install <zig 路径>")
	}
	if st, err := os.Stat(src); err != nil {
		return "", fmt.Errorf("找不到 zig: %s", src)
	} else if st.IsDir() {
		// 传目录：整个发行版复制
		return installDirCopy(src)
	}
	if !zigComplete(src) {
		return "", fmt.Errorf("zig 不完整（缺同级 lib/）：%s —— 请传整个 zig 发行版目录", src)
	}
	dst := filepath.Join(InstallDir(), "zig"+exeSuffix())
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return "", err
	}
	if samePath(src, dst) {
		return dst, nil
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(dst, data, 0o755); err != nil {
		return "", err
	}
	return dst, nil
}

// fileExists 判断路径是否为已存在的文件
func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

// installDirCopy 复制整个 zig 目录到用户工具链目录
func installDirCopy(srcDir string) (string, error) {
	dstDir := InstallDir()
	if err := os.MkdirAll(dstDir, 0o755); err != nil {
		return "", err
	}
	entries, err := os.ReadDir(srcDir)
	if err != nil {
		return "", err
	}
	for _, e := range entries {
		src := filepath.Join(srcDir, e.Name())
		dst := filepath.Join(dstDir, e.Name())
		if e.IsDir() {
			if err := copyTree(src, dst); err != nil {
				return "", err
			}
			continue
		}
		if err := copyFile(src, dst); err != nil {
			return "", err
		}
	}
	return filepath.Join(dstDir, "zig"+exeSuffix()), nil
}

func copyTree(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(p, target)
	})
}

func copyFile(src, dst string) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o755)
}

func samePath(a, b string) bool {
	aa, e1 := filepath.Abs(a)
	bb, e2 := filepath.Abs(b)
	if e1 != nil || e2 != nil {
		return false
	}
	return strings.EqualFold(filepath.Clean(aa), filepath.Clean(bb))
}

// EnvLines 返回可直接设置的环境变量（用于 go build 的 cgo）
func EnvLines() []string {
	r := Status()
	lines := []string{}
	if r.Zig != "" {
		zigCC := r.Zig + " cc"
		if runtime.GOOS == "windows" {
			zigCC += " -target x86_64-windows-msvc"
		}
		lines = append(lines,
			`export CGO_ENABLED=1`,
			fmt.Sprintf(`export CC="%s"`, zigCC),
			fmt.Sprintf(`export ZUZO_ZIG="%s"`, r.Zig),
		)
		lines = append(lines,
			fmt.Sprintf(`set CGO_ENABLED=1`),
			fmt.Sprintf(`set CC=%s`, zigCC),
		)
	}
	if r.Install != "" {
		lines = append(lines, fmt.Sprintf(`# 用户工具链目录: %s`, r.Install))
	}
	return lines
}

// Summary 一行摘要（给 zpm doctor 用）
func Summary() string {
	r := Status()
	zig := "无"
	if r.Zig != "" {
		zig = r.Zig
		if r.ZigVer != "" {
			zig += " (" + r.ZigVer + ")"
		}
	}
	clang := r.Clang
	if clang == "" {
		clang = "无"
	}
	return fmt.Sprintf("CC=%s · zig=%s · clang=%s", r.CC, zig, clang)
}
