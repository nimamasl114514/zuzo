package pyext

// client.go — CPython 子进程桥（Go 侧）
//
// 为什么是子进程而不是 cgo/Python C API：
//   - zuzo 的 Python 运行时是纯 Go 实现，没有 CPython 的 C-ABI；
//   - numpy / pandas / 任何依赖 .pyd 的库都必须在真 CPython 里跑；
//   - 子进程方案零链接依赖、崩了不拖垮宿主、可按需惰性启动。
//
// 生命周期：惰性启动（首次调用才拉起）→ 常驻 → 宿主退出时 Close。
// 并发：单管道串行（互斥锁），协议按 id 一一对应。
// 故障：读管道 EOF / 超时 → 杀进程并标记 dead，下次调用尝试重启一次。

import (
	"bufio"
	"crypto/sha1"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"zuzo/object"
)

//go:embed child.py
var childScript []byte

// DefaultTimeout 单次往返上限（大数组/重计算可调）
var DefaultTimeout = 120 * time.Second

// Client 一个 CPython 子进程桥实例
type Client struct {
	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID int64

	pyExe   string
	pyVer   string
	session string // 子进程会话标识（hello 返回）

	started bool
	dead    bool
	sticky  bool // 粘性子进程模式（命中 C 扩展后自动置位）
	closed  bool

	scriptPath string
	noticeOut  io.Writer // 模式切换提示写到 stderr
	quiet      bool
	callCount  int64
	failCount  int64
	startedAt  time.Time
}

// New 创建桥（不启动进程）
func New() *Client {
	q := os.Getenv("ZUZO_PY_BRIDGE_QUIET")
	if v := strings.TrimSpace(os.Getenv("ZUZO_PY_TIMEOUT")); v != "" {
		// 单位毫秒（与文档一致），非法值忽略
		var ms int64
		if _, err := fmt.Sscanf(v, "%d", &ms); err == nil && ms > 0 {
			DefaultTimeout = time.Duration(ms) * time.Millisecond
		}
	}
	return &Client{noticeOut: os.Stderr, quiet: q == "1" || q == "true"}
}

var (
	defOnce   sync.Once
	defClient *Client
)

// Default 进程级单例（.hy 全程复用同一个 CPython）
func Default() *Client {
	defOnce.Do(func() { defClient = New() })
	return defClient
}

// SetNotice 设置模式切换提示的输出目标（nil = 静默）
func (c *Client) SetNotice(w io.Writer) { c.noticeOut = w }

// ==================== 启动 / 关闭 ====================

// pythonCandidate 一个候选解释器
type pythonCandidate struct {
	exe  string
	args []string
}

func pythonCandidates() []pythonCandidate {
	var out []pythonCandidate
	if v := strings.TrimSpace(os.Getenv("ZUZO_PYTHON")); v != "" {
		out = append(out, pythonCandidate{exe: v})
	}
	out = append(out,
		pythonCandidate{exe: "python"},
		pythonCandidate{exe: "python3"},
	)
	if os.PathSeparator == '\\' {
		// 常见安装位置（PATH 里没有时兜底）：官方安装器 / conda / 本机工具链
		globPats := []string{
			`C:\Python3*\python.exe`,
			`C:\ProgramData\Anaconda3\python.exe`,
			`C:\ProgramData\miniconda3\python.exe`,
		}
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			globPats = append(globPats,
				filepath.Join(la, "Programs", "Python", "Python3*", "python.exe"),
				filepath.Join(la, "Programs", "Python", "Launcher", "py.exe"),
			)
		}
		if ad := os.Getenv("APPDATA"); ad != "" {
			globPats = append(globPats,
				filepath.Join(ad, "Python", "Python3*", "python.exe"),
				// 灵犀自带 python-env（宿主 PATH 常被收紧，直接按已知位置兜底）
				filepath.Join(ad, "WPS 灵犀", "python-env", "python.exe"),
			)
		}
		for _, pat := range globPats {
			matches, _ := filepath.Glob(pat)
			for _, m := range matches {
				out = append(out, pythonCandidate{exe: m})
			}
		}
		out = append(out, pythonCandidate{exe: "py", args: []string{"-3"}})
	}
	return out
}

// discoverPython 探测一个可用的真 CPython（>=3.8）
func discoverPython() (string, string, error) {
	const probe = "import sys;print(sys.version_info[0],sys.version_info[1],sep='.')"
	var lastErr error
	for _, cand := range pythonCandidates() {
		args := append(append([]string{}, cand.args...), "-c", probe)
		cmd := exec.Command(cand.exe, args...)
		out, err := cmd.Output()
		if err != nil {
			lastErr = fmt.Errorf("%s: %v", cand.exe, err)
			continue
		}
		ver := strings.TrimSpace(string(out))
		parts := strings.Split(ver, ".")
		if len(parts) != 2 {
			lastErr = fmt.Errorf("%s: 版本输出异常 %q", cand.exe, ver)
			continue
		}
		major, minor := 0, 0
		fmt.Sscanf(parts[0], "%d", &major)
		fmt.Sscanf(parts[1], "%d", &minor)
		if major != 3 || minor < 8 {
			lastErr = fmt.Errorf("%s: 需要 CPython >= 3.8，实际 %s", cand.exe, ver)
			continue
		}
		return cand.exe, ver, nil
	}
	if lastErr == nil {
		lastErr = errors.New("未找到 python/python3")
	}
	return "", "", fmt.Errorf("pyext: 未找到可用的 CPython（设置 ZUZO_PYTHON 指定解释器）: %v", lastErr)
}

// extractScript 把内嵌 child.py 落到临时文件（内容哈希命名，跨进程复用）
func extractScript() (string, error) {
	sum := sha1.Sum(childScript)
	tag := hex.EncodeToString(sum[:6])
	dir := filepath.Join(os.TempDir(), "zuzo-pyext-"+tag)
	path := filepath.Join(dir, "child.py")
	if st, err := os.Stat(path); err == nil && st.Size() == int64(len(childScript)) {
		return path, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(path, childScript, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

// Start 启动子进程（幂等）
func (c *Client) Start() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.startLocked()
}

func (c *Client) startLocked() error {
	if c.started && !c.dead {
		return nil
	}
	if c.closed {
		return errors.New("pyext: 桥已关闭")
	}
	script, err := extractScript()
	if err != nil {
		return fmt.Errorf("pyext: 释放 child.py 失败: %v", err)
	}
	pyExe, pyVer, err := c.resolvePythonLocked()
	if err != nil {
		return err
	}
	cmd := exec.Command(pyExe, "-u", script, "--stdio")
	cmd.Stderr = os.Stderr
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("pyext: 启动 %s 失败: %v", pyExe, err)
	}
	c.cmd = cmd
	c.stdin = stdin
	c.reader = bufio.NewReaderSize(stdout, 1<<20)
	c.started = true
	c.dead = false
	c.scriptPath = script
	c.pyExe = pyExe
	c.pyVer = pyVer
	c.startedAt = time.Now()

	// 握手
	raw, err := c.roundTripLocked("hello", nil, 30*time.Second)
	if err != nil {
		c.killLocked()
		return fmt.Errorf("pyext: 子进程握手失败: %v", err)
	}
	var hs struct {
		Py      string `json:"py"`
		Exe     string `json:"exe"`
		Impl    string `json:"impl"`
		Session string `json:"session"`
	}
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &hs)
	}
	c.session = hs.Session
	if hs.Py != "" {
		c.pyVer = hs.Py
	}
	if hs.Exe != "" {
		c.pyExe = hs.Exe
	}
	return nil
}

// resolvePythonLocked 探测并缓存解释器
func (c *Client) resolvePythonLocked() (string, string, error) {
	if c.pyExe != "" && c.pyVer != "" {
		return c.pyExe, c.pyVer, nil
	}
	exe, ver, err := discoverPython()
	if err != nil {
		return "", "", err
	}
	c.pyExe, c.pyVer = exe, ver
	return exe, ver, nil
}

// Close 关闭子进程（宿主退出前调用）
func (c *Client) Close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	if c.started && !c.dead && c.stdin != nil {
		req := map[string]any{"id": 0, "op": "shutdown"}
		if b, err := json.Marshal(req); err == nil {
			_, _ = c.stdin.Write(append(b, '\n'))
		}
	}
	c.killLocked()
}

func (c *Client) killLocked() {
	if c.stdin != nil {
		_ = c.stdin.Close()
	}
	if c.cmd != nil && c.cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			_ = c.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = c.cmd.Process.Kill()
			<-done
		}
	}
	c.dead = true
	c.started = false
}

// ==================== 单次往返 ====================

type wireErr struct {
	T  string `json:"t"`
	M  string `json:"m"`
	TB string `json:"tb"`
}

func (e *wireErr) Error() string {
	if e == nil {
		return ""
	}
	name := e.T
	if name == "" {
		name = "PythonError"
	}
	msg := fmt.Sprintf("Python %s: %s", name, e.M)
	if e.TB != "" {
		msg += "\n" + strings.TrimRight(e.TB, "\n")
	}
	return msg
}

type wireResp struct {
	ID int64           `json:"id"`
	OK bool            `json:"ok"`
	V  json.RawMessage `json:"v"`
	E  *wireErr        `json:"e"`
}

// roundTripLocked 发送一条请求并读回响应（调用方须持锁）
func (c *Client) roundTripLocked(op string, payload map[string]any, timeout time.Duration) (json.RawMessage, error) {
	if c.dead || !c.started || c.stdin == nil || c.reader == nil {
		return nil, errors.New("pyext: 子进程未启动")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	c.nextID++
	id := c.nextID
	req := map[string]any{"id": id, "op": op}
	for k, v := range payload {
		req[k] = v
	}
	buf, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	buf = append(buf, '\n')
	if _, err := c.stdin.Write(buf); err != nil {
		c.dead = true
		return nil, fmt.Errorf("pyext: 写管道失败: %v", err)
	}
	c.callCount++

	type readResult struct {
		resp wireResp
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			ch <- readResult{err: err}
			return
		}
		var r wireResp
		if err := json.Unmarshal(line, &r); err != nil {
			ch <- readResult{err: fmt.Errorf("协议解析失败: %v (原文前80字节: %s)", err, head(line, 80))}
			return
		}
		ch <- readResult{resp: r}
	}()

	select {
	case res := <-ch:
		if res.err != nil {
			c.dead = true
			c.failCount++
			if res.err == io.EOF {
				return nil, errors.New("pyext: 子进程已退出（管道 EOF）")
			}
			return nil, fmt.Errorf("pyext: %v", res.err)
		}
		if !res.resp.OK {
			c.failCount++
			return nil, res.resp.E
		}
		return res.resp.V, nil
	case <-time.After(timeout):
		c.dead = true
		c.failCount++
		_ = c.cmd.Process.Kill()
		return nil, fmt.Errorf("pyext: 调用超时（%s）", timeout)
	}
}

func head(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}

// call 对外单次调用（自动启动 / 自动重启一次）
func (c *Client) call(op string, payload map[string]any) (object.Object, error) {
	return c.callTimeout(op, payload, 0)
}

func (c *Client) callTimeout(op string, payload map[string]any, timeout time.Duration) (object.Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.startLocked(); err != nil {
		return nil, err
	}
	raw, err := c.roundTripLocked(op, payload, timeout)
	if err != nil {
		if c.dead && !c.closed {
			// 重启一次再试（子进程可能被外部杀掉）
			if e2 := c.startLocked(); e2 == nil {
				raw, err = c.roundTripLocked(op, payload, timeout)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	if len(raw) == 0 {
		return &object.Null{}, nil
	}
	return decodeVal(raw, c.session)
}

// ==================== 对外 API（zuzo 内置函数调用） ====================

// Active 是否处于「粘性子进程模式」
func (c *Client) Active() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sticky && c.started && !c.dead
}

// Activate 激活并粘住子进程模式（命中 C 扩展时自动调用）
func (c *Client) Activate() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return errors.New("pyext: 桥已关闭")
	}
	c.sticky = true
	return c.startLocked()
}

// SetSticky 手动设置粘性模式（py模式("本地") 关闭）
func (c *Client) SetSticky(v bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.sticky = v
}

// Import 导入一个 Python 模块
func (c *Client) Import(name string) (object.Object, error) {
	return c.call("import", map[string]any{"name": name})
}

// checkHandle 拦截「子进程重启后的旧句柄」（ABA）
func (c *Client) checkHandle(p *object.PyObj) error {
	if p == nil {
		return errors.New("pyext: 空句柄")
	}
	if p.Session != "" && c.session != "" && p.Session != c.session {
		return errors.New("句柄已失效：CPython 子进程重启过，请重新 py导入 / py取（旧句柄不再指向原对象）")
	}
	return nil
}

// GetAttr 取属性
func (c *Client) GetAttr(target *object.PyObj, name string) (object.Object, error) {
	if err := c.checkHandle(target); err != nil {
		return nil, err
	}
	return c.call("get", map[string]any{"id": target.ID, "name": name})
}

// SetAttr 设属性
func (c *Client) SetAttr(target *object.PyObj, name string, v object.Object) error {
	if err := c.checkHandle(target); err != nil {
		return err
	}
	
	enc, err := encodeObj(v)
	if err != nil {
		return err
	}
	_, err = c.call("set", map[string]any{"id": target.ID, "name": name, "v": enc})
	return err
}

// Call 调用句柄
func (c *Client) Call(target *object.PyObj, args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
	if err := c.checkHandle(target); err != nil {
		return nil, err
	}
	
	encArgs := make([]any, 0, len(args))
	for _, a := range args {
		e, err := encodeObj(a)
		if err != nil {
			return nil, err
		}
		encArgs = append(encArgs, e)
	}
	encKw := map[string]any{}
	for k, v := range kwargs {
		e, err := encodeObj(v)
		if err != nil {
			return nil, err
		}
		encKw[k] = e
	}
	return c.call("call", map[string]any{"id": target.ID, "args": encArgs, "kwargs": encKw})
}

// Repr 取远端 repr
func (c *Client) Repr(target *object.PyObj) (string, error) {
	if err := c.checkHandle(target); err != nil {
		return "", err
	}
	
	v, err := c.call("repr", map[string]any{"id": target.ID})
	if err != nil {
		return "", err
	}
	if s, ok := v.(*object.String); ok {
		return s.Value, nil
	}
	return v.Inspect(), nil
}

// Native 尽力转成 zuzo 原生值（ndarray→数组、DataFrame→记录数组…）
func (c *Client) Native(target *object.PyObj) (object.Object, error) {
	if err := c.checkHandle(target); err != nil {
		return nil, err
	}
	
	return c.call("native", map[string]any{"id": target.ID})
}

// Release 释放远端句柄
func (c *Client) Release(target *object.PyObj) error {
	if err := c.checkHandle(target); err != nil {
		return err
	}
	
	_, err := c.call("release", map[string]any{"id": target.ID})
	return err
}

// HasModule 探测模块是否可导入（不实际执行）
func (c *Client) HasModule(name string) (bool, error) {
	v, err := c.call("has", map[string]any{"name": name})
	if err != nil {
		return false, err
	}
	if b, ok := v.(*object.Boolean); ok {
		return b.Value, nil
	}
	return false, nil
}

// Exec 在子进程常驻 globals 执行代码
func (c *Client) Exec(code string) error {
	_, err := c.call("exec", map[string]any{"code": code})
	return err
}

// Eval 在子进程常驻 globals 求值表达式
func (c *Client) Eval(expr string) (object.Object, error) {
	return c.call("eval", map[string]any{"expr": expr})
}

// Lookup 按名字取子进程 globals 里的对象（找不到返回 false）
func (c *Client) Lookup(name string) (object.Object, bool) {
	v, err := c.call("getglobal", map[string]any{"name": name})
	if err != nil {
		return nil, false
	}
	return v, true
}

// SetName 设置子进程 globals 变量
func (c *Client) SetName(name string, v object.Object) error {
	enc, err := encodeObj(v)
	if err != nil {
		return err
	}
	_, err = c.call("setglobal", map[string]any{"name": name, "v": enc})
	return err
}

// CallName 按名字调用子进程 globals 里的可调用对象
func (c *Client) CallName(name string, args []object.Object) (object.Object, error) {
	v, ok := c.Lookup(name)
	if !ok {
		return nil, fmt.Errorf("Python 侧未定义：%s", name)
	}
	obj, isObj := v.(*object.PyObj)
	if !isObj {
		return nil, fmt.Errorf("Python 侧 %s 不是可调用对象", name)
	}
	return c.Call(obj, args, nil)
}

// Info 解释器信息
func (c *Client) Info() (map[string]object.Object, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := c.startLocked(); err != nil {
		return nil, err
	}
	raw, err := c.roundTripLocked("hello", nil, 30*time.Second)
	if err != nil {
		return nil, err
	}
	var hs struct {
		Py   string `json:"py"`
		Exe  string `json:"exe"`
		Impl string `json:"impl"`
	}
	_ = json.Unmarshal(raw, &hs)
	out := map[string]object.Object{
		"版本":  &object.String{Value: hs.Py},
		"解释器": &object.String{Value: hs.Exe},
		"实现":  &object.String{Value: hs.Impl},
		"模式":  &object.String{Value: c.modeLocked()},
	}
	return out, nil
}

// Mode 当前模式（"本地" / "子进程"）
func (c *Client) Mode() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.modeLocked()
}

func (c *Client) modeLocked() string {
	if c.sticky {
		return "子进程"
	}
	return "本地"
}

// Stats 观测：调用次数 / 失败次数 / 已运行时长
func (c *Client) Stats() map[string]object.Object {
	c.mu.Lock()
	defer c.mu.Unlock()
	var up float64
	if c.started && !c.dead {
		up = time.Since(c.startedAt).Seconds()
	}
	return map[string]object.Object{
		"调用次数": &object.Integer{Value: c.callCount},
		"失败次数": &object.Integer{Value: c.failCount},
		"运行秒数": &object.Float{Value: up},
	}
}

// Install 走 pip 安装（显式调用，绝不隐式联网）
func (c *Client) Install(pkg string) (string, error) {
	c.mu.Lock()
	exe, _, err := c.resolvePythonLocked()
	c.mu.Unlock()
	if err != nil {
		return "", err
	}
	cmd := exec.Command(exe, "-m", "pip", "install", pkg)
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil {
		return text, fmt.Errorf("pip install %s 失败: %v", pkg, err)
	}
	return text, nil
}

