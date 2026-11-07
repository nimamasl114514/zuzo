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
	"archive/zip"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
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
// from 为 http(s) URL 时（第33轮 T33-l）：下载 .zip / .tar.xz → 解压 → 校验 lib/ → 落地。
func Install(from string) (string, error) {
	if isRemoteURL(from) {
		return installFromURL(strings.TrimSpace(from))
	}
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

// ---------------------------------------------------------------------------
// 第33轮 T33-l：从官方 URL 直接下载 zig
//
//	zuzo toolchain install https://ziglang.org/download/0.13.0/zig-windows-x86_64-0.13.0.zip
//
// 链路：下载到临时目录（带百分比进度）→ 按扩展名解压（.zip 用标准库，.tar.xz/.tar.gz 用系统 tar）
//      → 找 zig 可执行文件 → zigComplete() 校验同级 lib/（第32轮踩过「只拷 zig.exe 不行」）
//      → installDirCopy() 落到 %LOCALAPPDATA%\zuzo\toolchain\zig → 落地后再校验一次。
// 只依赖标准库；Windows 上解 .tar.xz 需要系统自带 tar（Win10 1803+ 的 bsdtar），没有就明确要求下 .zip。
// ---------------------------------------------------------------------------

// isRemoteURL 判断 from 是不是 http(s) 地址
func isRemoteURL(s string) bool {
	l := strings.ToLower(strings.TrimSpace(s))
	return strings.HasPrefix(l, "http://") || strings.HasPrefix(l, "https://")
}

// installFromURL 从 URL 下载并安装 zig，返回落地后的 zig 可执行文件路径
func installFromURL(url string) (string, error) {
	tmpDir, err := os.MkdirTemp("", "zuzo-zig-*")
	if err != nil {
		return "", fmt.Errorf("创建临时目录失败：%v —— 请检查 TEMP 目录权限", err)
	}
	defer os.RemoveAll(tmpDir)

	fmt.Fprintf(os.Stderr, "下载 zig: %s\n", url)
	archive, err := download(url, tmpDir)
	if err != nil {
		return "", err
	}

	extractDir := filepath.Join(tmpDir, "unpack")
	if err := os.MkdirAll(extractDir, 0o755); err != nil {
		return "", fmt.Errorf("创建解压目录失败：%v", err)
	}
	fmt.Fprintf(os.Stderr, "解压中: %s\n", filepath.Base(archive))
	name := strings.ToLower(filepath.Base(archive))
	switch {
	case strings.HasSuffix(name, ".zip"):
		err = unzipTo(archive, extractDir)
	case strings.HasSuffix(name, ".tar.xz"), strings.HasSuffix(name, ".txz"),
		strings.HasSuffix(name, ".tar.gz"), strings.HasSuffix(name, ".tgz"),
		strings.HasSuffix(name, ".tar"):
		err = untarTo(archive, extractDir)
	default:
		return "", fmt.Errorf("不支持的压缩格式：%s —— 请下载 .zip（https://ziglang.org/download/）", filepath.Base(archive))
	}
	if err != nil {
		return "", err
	}

	zigExe, root := findZigRoot(extractDir)
	if zigExe == "" {
		return "", fmt.Errorf("压缩包里没找到 zig%s —— 请确认下载的是 zig 发行版（https://ziglang.org/download/）", exeSuffix())
	}
	if !zigComplete(zigExe) {
		return "", fmt.Errorf("下载的 zig 不完整（%s 同级缺 lib/）—— 请下载完整发行版（不要只下 zig%s）",
			zigExe, exeSuffix())
	}

	fmt.Fprintf(os.Stderr, "校验通过，安装到: %s\n", InstallDir())
	dst, err := installDirCopy(root)
	if err != nil {
		return "", fmt.Errorf("复制到用户工具链目录失败：%v —— 请检查 %s 的写权限（或用 ZUZO_ZIG 指向现成 zig）", err, InstallDir())
	}
	// 落地后再校验一次：防止复制中途丢文件
	if !zigComplete(dst) {
		return "", fmt.Errorf("安装后校验失败：%s 同级缺 lib/ —— 请重试，或手动复制整个 zig 目录到 %s", dst, InstallDir())
	}
	return dst, nil
}

// download 把 url 下载到 dir，返回本地文件路径；期间向 stderr 打百分比
func download(url, dir string) (string, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("下载地址非法：%s —— %v", url, err)
	}
	req.Header.Set("User-Agent", "zuzo-toolchain/1.0")
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("下载失败：%v —— 请检查网络/代理，或先手动下载 zip 再执行 zuzo toolchain install <本地 zip 路径>", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("下载失败：HTTP %d %s —— 地址可能已失效，请到 https://ziglang.org/download/ 取最新链接", resp.StatusCode, resp.Status)
	}

	base := filepath.Base(strings.Split(url, "?")[0])
	if base == "" || base == "." || base == "/" || base == "\\" {
		base = "zig-download.zip"
	}
	path := filepath.Join(dir, base)
	f, err := os.Create(path)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败：%v", err)
	}
	pw := &progressWriter{total: resp.ContentLength, lastPct: -1}
	if _, err := io.Copy(f, io.TeeReader(resp.Body, pw)); err != nil {
		f.Close()
		fmt.Fprintln(os.Stderr)
		return "", fmt.Errorf("下载中断：%v —— 请重试", err)
	}
	if err := f.Close(); err != nil {
		return "", fmt.Errorf("写入临时文件失败：%v", err)
	}
	fmt.Fprintf(os.Stderr, "\r下载完成 %s（%s）%s\n", base, humanSize(pw.done), strings.Repeat(" ", 24))
	return path, nil
}

// progressWriter 统计已下载字节并每 5% 打一次进度
type progressWriter struct {
	total   int64
	done    int64
	lastPct int   // 上次打印的百分比，-1 表示还没打过
	lastMil int64 // 总长未知时，每 4MB 打一次
}

func (p *progressWriter) Write(b []byte) (int, error) {
	n := len(b)
	p.done += int64(n)
	if p.total > 0 {
		pct := int(p.done * 100 / p.total)
		if pct >= p.lastPct+5 || pct == 100 {
			p.lastPct = pct
			fmt.Fprintf(os.Stderr, "\r下载中 %3d%%  %s / %s", pct, humanSize(p.done), humanSize(p.total))
		}
	} else if p.done-p.lastMil >= 4<<20 {
		p.lastMil = p.done
		fmt.Fprintf(os.Stderr, "\r下载中 %s", humanSize(p.done))
	}
	return n, nil
}

func humanSize(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// safeJoin 解压目标路径，拒绝 zip slip（../ 穿越）
func safeJoin(base, name string) (string, error) {
	target := filepath.Join(base, filepath.FromSlash(name))
	rel, err := filepath.Rel(base, target)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", fmt.Errorf("压缩包内含非法路径：%s", name)
	}
	return target, nil
}

// unzipTo 用标准库解压 zip
func unzipTo(archive, dstDir string) error {
	zr, err := zip.OpenReader(archive)
	if err != nil {
		return fmt.Errorf("解压失败（不是有效 zip？）：%v", err)
	}
	defer zr.Close()
	for _, fld := range zr.File {
		target, err := safeJoin(dstDir, fld.Name)
		if err != nil {
			return err
		}
		if fld.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := fld.Open()
		if err != nil {
			return fmt.Errorf("读取压缩包条目失败（%s）：%v —— 压缩包可能已损坏，请重新下载", fld.Name, err)
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, cerr := io.Copy(out, rc)
		rc.Close()
		if err := out.Close(); err != nil && cerr == nil {
			cerr = err
		}
		if cerr != nil {
			return fmt.Errorf("解压 %s 失败：%v", fld.Name, cerr)
		}
	}
	return nil
}

// untarTo 调系统 tar 解压 .tar.xz/.tar.gz（标准库没有 xz）
func untarTo(archive, dstDir string) error {
	tarPath, err := exec.LookPath("tar")
	if err != nil {
		if runtime.GOOS == "windows" {
			return fmt.Errorf("当前系统没有 tar，无法解压 %s —— Windows 请改用 .zip 包（https://ziglang.org/download/ 选 windows-x86_64 的 .zip）",
				filepath.Base(archive))
		}
		return fmt.Errorf("找不到 tar 命令，无法解压 %s —— 请安装 tar 后重试", filepath.Base(archive))
	}
	// 注意：GNU tar 会把 `C:\...` 当成「远程主机:路径」，必须切到压缩包所在目录用相对路径调用
	workDir := filepath.Dir(archive)
	args := []string{"-xf", filepath.Base(archive)}
	if rel, rerr := filepath.Rel(workDir, dstDir); rerr == nil && rel != "" && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
		args = append(args, "-C", rel)
	} else {
		args = append(args, "-C", dstDir)
	}
	cmd := exec.Command(tarPath, args...)
	cmd.Dir = workDir
	if out, err := cmd.CombinedOutput(); err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("解压失败（%s）：%s —— 压缩包可能损坏，请重新下载", filepath.Base(archive), msg)
	}
	return nil
}

// findZigRoot 在解压目录里找 zig 可执行文件，返回 (zig 路径, 该 zig 所在目录)
// 优先返回同级带 lib/ 的那个（发行版可能平铺多层目录）。
func findZigRoot(dir string) (zigExe, root string) {
	var first string
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info == nil || info.IsDir() {
			return nil
		}
		base := strings.ToLower(info.Name())
		if base != "zig" && base != "zig.exe" {
			return nil
		}
		if first == "" {
			first = p
		}
		if zigComplete(p) {
			zigExe, root = p, filepath.Dir(p)
			return filepath.SkipAll
		}
		return nil
	})
	if zigExe == "" && first != "" {
		return first, filepath.Dir(first)
	}
	return zigExe, root
}
