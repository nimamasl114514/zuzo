package runtime

// paths.go — 三方库 / 标准库源码搜索路径探测 + sys.path 活列表
//
// 设计要点（2026-10-25 第13轮 · 支持第三方 py 库）：
//  1. 目录 glob 便宜（< 1ms），启动时直接做；spawn CPython 昂贵（30~80ms），
//     只在「import 未命中且本轮尚未探测」时惰性发生一次，保住 19ms 启动基线。
//  2. 探测结果缓存到 <UserCacheDir>/zuzo/pypaths.txt（24h 内复用）。
//  3. sys.path 变活：Interp.SysPathList 是真实 List，Python 侧 append/insert 立即生效。
//  4. 两类目录，顺序固定：
//       site-packages（第三方库）—— 中间位置，可被后续 append 追加更多
//       CPython Lib（标准库纯 py 源码）—— 永远排在最后兜底
//     zuzo 自带的 Go 内置模块优先级最高（内建表先查），不会被 CPython 源码覆盖。
//     这让 bisect / codecs / enum / http.client / email 这类纯 Python 标准库
//     直接跑在 zuzo 运行时上，而不必逐个重写。

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"zuzo/py/object"
)

const pyPathCacheTTL = 24 * time.Hour

// pyPathSet 一次探测得到的路径集合
type pyPathSet struct {
	site   []string // site-packages（第三方库）
	stdlib string   // CPython Lib 目录（标准库纯 py 源码，兜底）
}

// initSysPath 初始化 sys.path（活列表）。启动路径：cwd + 环境变量 + 缓存/glob 结果。
func (ip *Interp) initSysPath() {
	list := &object.List{}
	seen := map[string]bool{}
	add := func(p string) {
		p = strings.TrimSpace(p)
		if p == "" {
			return
		}
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			return
		}
		seen[key] = true
		list.Items = append(list.Items, &object.Str{V: p})
	}
	add(ip.Cwd)
	add(".")
	for _, p := range envPyPaths() {
		add(p)
	}
	if v := os.Getenv("ZUZO_STDLIB"); v != "" && dirExists(v) {
		ip.stdlibDir = v
	}
	// 缓存优先（便宜），缓存失效才做目录 glob（仍便宜）；spawn 留到惰性探测。
	if ps := readPathCache(); ps != nil {
		for _, p := range ps.site {
			add(p)
		}
		if ip.stdlibDir == "" && ps.stdlib != "" && dirExists(ps.stdlib) {
			ip.stdlibDir = ps.stdlib
		}
	} else {
		for _, p := range globSitePackages() {
			add(p)
		}
	}
	if ip.stdlibDir != "" {
		add(ip.stdlibDir)
	}
	ip.SysPathList = list
	ip.syncSysPath()
}

// envPyPaths 读取 ZUZO_PY_PATH（分号/冒号分隔）。
func envPyPaths() []string {
	raw := os.Getenv("ZUZO_PY_PATH")
	if raw == "" {
		return nil
	}
	return strings.FieldsFunc(raw, func(r rune) bool { return r == ';' || r == ':' })
}

// sysPathDirs 取出当前 sys.path 的字符串列表（Python 侧改了立即反映）。
func (ip *Interp) sysPathDirs() []string {
	if ip.SysPathList == nil {
		return ip.SysPath
	}
	out := make([]string, 0, len(ip.SysPathList.Items))
	for _, it := range ip.SysPathList.Items {
		if s, ok := it.(*object.Str); ok {
			out = append(out, s.V)
		}
	}
	return out
}

// syncSysPath 把活列表镜像回 SysPath 字段（兼容既有读 SysPath 的代码）。
func (ip *Interp) syncSysPath() {
	ip.SysPath = ip.sysPathDirs()
}

// addSysPathDir 追加一个目录（去重）；标准库目录永远保持在末尾。
func (ip *Interp) addSysPathDir(p string) bool {
	if ip.SysPathList == nil || p == "" {
		return false
	}
	for _, it := range ip.SysPathList.Items {
		if s, ok := it.(*object.Str); ok && strings.EqualFold(filepath.Clean(s.V), filepath.Clean(p)) {
			return false
		}
	}
	item := &object.Str{V: p}
	if ip.stdlibDir != "" && !strings.EqualFold(filepath.Clean(p), filepath.Clean(ip.stdlibDir)) {
		items := ip.SysPathList.Items
		idx := len(items)
		for i, it := range items {
			if s, ok := it.(*object.Str); ok && strings.EqualFold(filepath.Clean(s.V), filepath.Clean(ip.stdlibDir)) {
				idx = i
				break
			}
		}
		items = append(items, nil)
		copy(items[idx+1:], items[idx:])
		items[idx] = item
		ip.SysPathList.Items = items
	} else {
		ip.SysPathList.Items = append(ip.SysPathList.Items, item)
	}
	ip.syncSysPath()
	return true
}

// probeThirdPartyPaths 惰性探测本机 CPython 的 site-packages + Lib（一次）。
// 返回 true 表示本次探测新增了路径（调用方应重试查找）。
func (ip *Interp) probeThirdPartyPaths() bool {
	ip.pathMu.Lock()
	if ip.pathProbed {
		ip.pathMu.Unlock()
		return false
	}
	ip.pathProbed = true
	ip.pathMu.Unlock()

	if os.Getenv("ZUZO_PY_AUTOPATH") == "0" {
		return false
	}
	ps := readPathCache()
	if ps == nil {
		var ok bool
		ps, ok = spawnProbePaths()
		if !ok || (len(ps.site) == 0 && ps.stdlib == "") {
			ps = &pyPathSet{site: globSitePackages()}
		}
		if len(ps.site) > 0 || ps.stdlib != "" {
			writePathCache(ps)
		}
	}
	added := false
	for _, p := range ps.site {
		if ip.addSysPathDir(p) {
			added = true
		}
	}
	if ps.stdlib != "" && ip.stdlibDir == "" && dirExists(ps.stdlib) {
		ip.stdlibDir = ps.stdlib
		if ip.addSysPathDir(ps.stdlib) {
			added = true
		}
	}
	return added
}

// spawnProbePaths 起一次 CPython 取 site-packages 与 Lib 目录（5s 超时）。
func spawnProbePaths() (*pyPathSet, bool) {
	script := `import sys, site
out = []
try:
    for p in site.getsitepackages():
        out.append("SITE\t" + p)
except Exception:
    pass
try:
    u = site.getusersitepackages()
    if isinstance(u, str):
        out.append("SITE\t" + u)
    else:
        for p in u:
            out.append("SITE\t" + p)
except Exception:
    pass
import sysconfig
try:
    out.append("STDLIB\t" + sysconfig.get_paths()["stdlib"])
except Exception:
    out.append("STDLIB\t" + sys.prefix + "\\Lib")
print("\n".join(out))
`
	cands := [][]string{}
	// 关键：Git Bash 启动的进程，PATH 里常是 MSYS 风格（/c/Users/...），
	// Windows CreateProcess 无法直接解析 —— 必须定位绝对 python.exe。
	if exe := findPythonExe(); exe != "" {
		cands = append(cands, []string{exe, "-c", script})
	}
	cands = append(cands,
		[]string{"python", "-c", script},
		[]string{"python3", "-c", script},
		[]string{"py", "-3", "-c", script},
	)
	for _, argv := range cands {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
		cmd.Env = append(os.Environ(), "PYTHONIOENCODING=utf-8")
		out, err := cmd.Output()
		cancel()
		if err != nil || len(out) == 0 {
			continue
		}
		ps := &pyPathSet{}
		for _, line := range strings.Split(string(out), "\n") {
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}
			switch {
			case strings.HasPrefix(line, "SITE\t"):
				p := strings.TrimPrefix(line, "SITE\t")
				if dirExists(p) {
					ps.site = append(ps.site, p)
				}
			case strings.HasPrefix(line, "STDLIB\t"):
				p := strings.TrimPrefix(line, "STDLIB\t")
				if dirExists(p) && fileExists(filepath.Join(p, "os.py")) {
					ps.stdlib = p
				}
			}
		}
		if len(ps.site) > 0 || ps.stdlib != "" {
			return ps, true
		}
	}
	return nil, false
}

// findPythonExe 定位本机 CPython 可执行文件（绝对路径）。
// 顺序：ZUZO_PYTHON → 自身同目录 → PATH（MSYS 路径自动转盘符）→ 常见安装目录 glob。
func findPythonExe() string {
	if p := os.Getenv("ZUZO_PYTHON"); p != "" && fileExists(p) {
		return p
	}
	if exe, err := os.Executable(); err == nil {
		d := filepath.Dir(exe)
		for _, n := range []string{"python.exe", "python3.exe"} {
			if p := filepath.Join(d, n); fileExists(p) {
				return p
			}
		}
	}
	for _, entry := range strings.Split(os.Getenv("PATH"), ";") {
		for _, e := range msysPathCandidates(entry) {
			for _, n := range []string{"python.exe", "python3.exe"} {
				if p := filepath.Join(e, n); fileExists(p) {
					return p
				}
			}
		}
	}
	pats := []string{}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		pats = append(pats, filepath.Join(la, "Programs", "Python", "Python3*", "python.exe"))
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		pats = append(pats, filepath.Join(ad, "Python", "Python3*", "python.exe"))
	}
	pats = append(pats, `C:\Python3*\python.exe`, `C:\ProgramData\miniconda3\python.exe`, `C:\ProgramData\Anaconda3\python.exe`)
	for _, p := range pats {
		if m, err := filepath.Glob(p); err == nil {
			for _, f := range m {
				if fileExists(f) {
					return f
				}
			}
		}
	}
	return ""
}

// msysPathCandidates 把一个 PATH 项拆成可能的 Windows 目录（含 /c/xxx → C:\\xxx 转换）。
func msysPathCandidates(entry string) []string {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return nil
	}
	parts := []string{entry}
	// 某些 shell 会把整段 MSYS PATH 塞进一个分号项（用冒号分隔）
	if strings.Contains(entry, ":") && strings.HasPrefix(entry, "/") {
		parts = strings.Split(entry, ":")
	}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		out = append(out, normalizeExeDir(p))
	}
	return out
}

func normalizeExeDir(p string) string {
	p = strings.TrimSpace(p)
	if len(p) >= 3 && p[0] == '/' && p[2] == '/' {
		c := p[1]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') {
			drive := c
			if c >= 'a' && c <= 'z' {
				drive = c - 32
			}
			return string(drive) + ":\\" + strings.ReplaceAll(p[3:], "/", "\\")
		}
	}
	return p
}

// globSitePackages 无 CPython 时的兜底：扫常见安装位置。
func globSitePackages() []string {
	var dirs []string
	pats := []string{}
	if la := os.Getenv("LOCALAPPDATA"); la != "" {
		pats = append(pats, filepath.Join(la, "Programs", "Python", "Python3*", "Lib", "site-packages"))
	}
	if ad := os.Getenv("APPDATA"); ad != "" {
		pats = append(pats, filepath.Join(ad, "Python", "Python3*", "site-packages"))
	}
	pats = append(pats,
		`C:\Python3*\Lib\site-packages`,
		`C:\ProgramData\Anaconda3\Lib\site-packages`,
		`C:\ProgramData\miniconda3\Lib\site-packages`,
	)
	for _, p := range pats {
		if m, err := filepath.Glob(p); err == nil {
			for _, d := range m {
				if dirExists(d) {
					dirs = append(dirs, d)
				}
			}
		}
	}
	return dirs
}

func pathCacheFile() string {
	dir, err := os.UserCacheDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "zuzo", "pypaths.txt")
}

func readPathCache() *pyPathSet {
	p := pathCacheFile()
	if p == "" {
		return nil
	}
	st, err := os.Stat(p)
	if err != nil || time.Since(st.ModTime()) > pyPathCacheTTL {
		return nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return nil
	}
	ps := &pyPathSet{}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case strings.HasPrefix(line, "SITE\t"):
			ps.site = append(ps.site, strings.TrimPrefix(line, "SITE\t"))
		case strings.HasPrefix(line, "STDLIB\t"):
			ps.stdlib = strings.TrimPrefix(line, "STDLIB\t")
		}
	}
	if len(ps.site) == 0 && ps.stdlib == "" {
		return nil
	}
	return ps
}

func writePathCache(ps *pyPathSet) {
	p := pathCacheFile()
	if p == "" || ps == nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return
	}
	var sb strings.Builder
	for _, d := range ps.site {
		sb.WriteString("SITE\t" + d + "\n")
	}
	if ps.stdlib != "" {
		sb.WriteString("STDLIB\t" + ps.stdlib + "\n")
	}
	_ = os.WriteFile(p, []byte(sb.String()), 0o644)
}

func fileExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && !st.IsDir()
}

func dirExists(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}

// addSysPathDirFront 把目录插到 sys.path 最前（主文件目录用）。
func (ip *Interp) addSysPathDirFront(p string) bool {
	if ip.SysPathList == nil || p == "" {
		return false
	}
	for _, it := range ip.SysPathList.Items {
		if s, ok := it.(*object.Str); ok && strings.EqualFold(filepath.Clean(s.V), filepath.Clean(p)) {
			return false
		}
	}
	items := append([]object.Object{&object.Str{V: p}}, ip.SysPathList.Items...)
	ip.SysPathList.Items = items
	ip.syncSysPath()
	return true
}
