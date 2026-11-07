package pyext

// client.go — CPython 子进程桥（Go 侧）
//
// 为什么是子进程而不是 cgo/Python C API：
//   - zuzo 的 Python 运行时是纯 Go 实现，没有 CPython 的 C-ABI；
//   - numpy / pandas / 任何依赖 .pyd 的库都必须在真 CPython 里跑；
//   - 子进程方案零链接依赖、崩了不拖垮宿主、可按需惰性启动。
//
// 生命周期：惰性启动（首次调用才拉起）→ 常驻 → 宿主退出时 Close。
//
// 并发（第33轮）：ZUZO_PY_POOL=N 多子进程连接池
//   - 未设置 / N=1（默认）：单管道串行（互斥锁），与第32轮行为完全一致；
//   - N>1：创建 N 个 worker，句柄 id 高 16 位编码 worker 序号：
//         id = workerIdx<<48 | localId          （localId 由 child.py 自增分配）
//     * 句柄类 op（get / set / call / repr / native / release）按句柄高 16 位路由到其所属 worker；
//     * 产生新句柄的 op（import）按 round-robin 分配，因此 import→取属性→调用 的整条链
//       天然落在同一个 worker 上，可并行；
//     * globals 类 op（eval / exec / getglobal / setglobal / has / hello）固定 worker 0
//       （主通道）——它们操作的是「某个子进程的 globals 命名空间」，轮询会导致
//       先 py执行("x=1") 后 py求值("x") 取不到值，故不做轮询；
//   - 每个 worker 独立超时杀进程 + 独立重启；会话戳（checkHandle 的 ABA 防护）按 worker 校验。
//
// 语义边界（重要）：跨 worker 的句柄不通用。worker A 上导入的模块/对象只能与 A 上的句柄
// 互相操作；把 A 的句柄当参数传给 B 的调用会明确报错（见 localizeHandle）。需要「一组操作
// 原子地在同一进程内完成」时，请沿同一条 py导入 句柄链走完，不要中途换句柄。
//
// 故障：读管道 EOF / 超时 → 杀该 worker 并标记 dead，下次调用尝试重启一次。

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
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"zuzo/object"
)

//go:embed child.py
var childScript []byte

// DefaultTimeout 单次往返上限（大数组/重计算可调）
var DefaultTimeout = 120 * time.Second

const (
	// maxPoolSize 工作进程数上限（防止误配把机器打爆）
	maxPoolSize = 32
	// handleShift 句柄 id 中 worker 序号的位偏移（低 48 位留给子进程自增 id）
	handleShift = 48
	// handleMask 低 48 位掩码（子进程侧 localId）
	handleMask int64 = (int64(1) << handleShift) - 1
)

// worker 一个 CPython 子进程（连接池成员）
type worker struct {
	idx int // 池内序号 → 句柄高 16 位

	mu     sync.Mutex
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	nextID int64 // 本地请求号（仅用于信封，响应不校验）

	pyExe   string
	pyVer   string
	session string // 子进程会话标识（hello 返回），重启即变

	started   bool
	dead      bool
	startedAt time.Time

	scriptPath string
}

// Client 一个 CPython 子进程桥实例（1..N 个 worker）
type Client struct {
	workers []*worker // New 时创建完毕，之后只读 → 无需加锁
	rr      uint64    // round-robin 游标（原子自增）

	discMu sync.Mutex // 解释器探测缓存
	pyExe  string
	pyVer  string

	noticeOut io.Writer // 模式切换提示写到 stderr
	quiet     bool

	stickyMu sync.Mutex
	sticky   bool // 粘性子进程模式（命中 C 扩展后自动置位）
	closed   bool

	callCount int64 // 原子计数：全部 worker 之和
	failCount int64
}

// poolSizeFromEnv 读取 ZUZO_PY_POOL（默认 1 = 保持第32轮行为）
func poolSizeFromEnv() int {
	v := strings.TrimSpace(os.Getenv("ZUZO_PY_POOL"))
	if v == "" {
		return 1
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 {
		return 1
	}
	if n > maxPoolSize {
		return maxPoolSize
	}
	return n
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
	c := &Client{noticeOut: os.Stderr, quiet: q == "1" || q == "true"}
	n := poolSizeFromEnv()
	c.workers = make([]*worker, n)
	for i := 0; i < n; i++ {
		c.workers[i] = &worker{idx: i}
	}
	return c
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

// PoolSize 工作进程数（ZUZO_PY_POOL，默认 1）
func (c *Client) PoolSize() int { return len(c.workers) }

// primary 主通道（worker 0）：globals 类 op 与 handshake 固定走它
func (c *Client) primary() *worker { return c.workers[0] }

// nextWorker round-robin 选一个 worker（产生新句柄的 op 用）
func (c *Client) nextWorker() *worker {
	if len(c.workers) == 1 {
		return c.workers[0]
	}
	n := atomic.AddUint64(&c.rr, 1) - 1
	return c.workers[int(n%uint64(len(c.workers)))]
}

// isClosed 桥是否已关闭
func (c *Client) isClosed() bool {
	c.stickyMu.Lock()
	defer c.stickyMu.Unlock()
	return c.closed
}

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

// resolvePython 探测并缓存解释器（所有 worker 共用同一份探测结果）
func (c *Client) resolvePython() (string, string, error) {
	c.discMu.Lock()
	defer c.discMu.Unlock()
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

// Start 启动主通道子进程（幂等）；其余 worker 首次被路由到时惰性启动
func (c *Client) Start() error {
	w := c.primary()
	w.mu.Lock()
	defer w.mu.Unlock()
	return c.startWorkerLocked(w)
}

func (c *Client) startWorkerLocked(w *worker) error {
	if w.started && !w.dead {
		return nil
	}
	if c.isClosed() {
		return errors.New("pyext: 桥已关闭")
	}
	script, err := extractScript()
	if err != nil {
		return fmt.Errorf("pyext: 释放 child.py 失败: %v", err)
	}
	pyExe, pyVer, err := c.resolvePython()
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
	w.cmd = cmd
	w.stdin = stdin
	w.reader = bufio.NewReaderSize(stdout, 1<<20)
	w.started = true
	w.dead = false
	w.scriptPath = script
	w.pyExe = pyExe
	w.pyVer = pyVer
	w.startedAt = time.Now()

	// 握手
	raw, err := c.roundTripLocked(w, "hello", nil, 30*time.Second)
	if err != nil {
		c.killWorkerLocked(w)
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
	w.session = hs.Session
	if hs.Py != "" {
		w.pyVer = hs.Py
	}
	if hs.Exe != "" {
		w.pyExe = hs.Exe
	}
	return nil
}

// Close 关闭所有子进程（宿主退出前调用）
func (c *Client) Close() {
	c.stickyMu.Lock()
	if c.closed {
		c.stickyMu.Unlock()
		return
	}
	c.closed = true
	c.stickyMu.Unlock()
	for _, w := range c.workers {
		w.mu.Lock()
		c.shutdownWorkerLocked(w)
		w.mu.Unlock()
	}
}

// shutdownWorkerLocked 尽力优雅关闭，再兜底 kill
func (c *Client) shutdownWorkerLocked(w *worker) {
	if w.started && !w.dead && w.stdin != nil {
		req := map[string]any{"id": 0, "op": "shutdown"}
		if b, err := json.Marshal(req); err == nil {
			_, _ = w.stdin.Write(append(b, '\n'))
		}
	}
	c.killWorkerLocked(w)
}

func (c *Client) killWorkerLocked(w *worker) {
	if w.stdin != nil {
		_ = w.stdin.Close()
	}
	if w.cmd != nil && w.cmd.Process != nil {
		done := make(chan struct{})
		go func() {
			_ = w.cmd.Wait()
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			_ = w.cmd.Process.Kill()
			<-done
		}
	}
	w.dead = true
	w.started = false
	// 会话戳随进程一起作废（下次 start 会拿到新的 session）
	w.session = ""
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

// roundTripLocked 在指定 worker 上发送一条请求并读回响应（调用方须持 w.mu）
func (c *Client) roundTripLocked(w *worker, op string, payload map[string]any, timeout time.Duration) (json.RawMessage, error) {
	if w.dead || !w.started || w.stdin == nil || w.reader == nil {
		return nil, errors.New("pyext: 子进程未启动")
	}
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	w.nextID++
	req := map[string]any{"id": w.nextID, "op": op}
	for k, v := range payload {
		req[k] = v
	}
	buf, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	buf = append(buf, '\n')
	if _, err := w.stdin.Write(buf); err != nil {
		w.dead = true
		return nil, fmt.Errorf("pyext: 写管道失败: %v", err)
	}
	atomic.AddInt64(&c.callCount, 1)

	type readResult struct {
		resp wireResp
		err  error
	}
	ch := make(chan readResult, 1)
	go func() {
		line, err := w.reader.ReadBytes('\n')
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
			w.dead = true
			atomic.AddInt64(&c.failCount, 1)
			if res.err == io.EOF {
				return nil, errors.New("pyext: 子进程已退出（管道 EOF）")
			}
			return nil, fmt.Errorf("pyext: %v", res.err)
		}
		if !res.resp.OK {
			atomic.AddInt64(&c.failCount, 1)
			return nil, res.resp.E
		}
		return res.resp.V, nil
	case <-time.After(timeout):
		w.dead = true
		atomic.AddInt64(&c.failCount, 1)
		_ = w.cmd.Process.Kill()
		return nil, fmt.Errorf("pyext: 调用超时（%s）", timeout)
	}
}

func head(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n])
}

// ==================== 句柄编解码（worker 路由） ====================

// tagHandle 给子进程返回的句柄打上 worker 序号（原地标记，值是本次解码新建的）
func tagHandle(idx int, o object.Object) object.Object {
	switch v := o.(type) {
	case *object.PyObj:
		v.ID = int64(idx)<<handleShift | (v.ID & handleMask)
		return v
	case *object.Array:
		for i, e := range v.Elements {
			v.Elements[i] = tagHandle(idx, e)
		}
		return v
	case *object.Hash:
		for _, hk := range v.Keys {
			pair, ok := v.Pairs[hk]
			if !ok {
				continue
			}
			v.Pairs[hk] = object.HashPair{
				Key:   tagHandle(idx, pair.Key),
				Value: tagHandle(idx, pair.Value),
			}
		}
		return v
	}
	return o
}

// localizeHandle 出站前把句柄 id 还原成子进程 localId，并校验归属 worker
// （proto.go 的 encodeObj 不感知连接池，故在 client.go 侧做一次深拷贝重写）
func localizeHandle(o object.Object, want int) (object.Object, error) {
	switch v := o.(type) {
	case *object.PyObj:
		idx := int(v.ID >> handleShift)
		if idx != want {
			return nil, fmt.Errorf("pyext: 句柄 #%d 属于工作进程 %d，不能跨 worker 使用（跨 worker 的句柄不通用，请在同一条 py导入 句柄链内完成这组操作）",
				v.ID&handleMask, idx)
		}
		cp := *v
		cp.ID = v.ID & handleMask
		return &cp, nil
	case *object.Array:
		out := &object.Array{Elements: make([]object.Object, len(v.Elements))}
		for i, e := range v.Elements {
			n, err := localizeHandle(e, want)
			if err != nil {
				return nil, err
			}
			out.Elements[i] = n
		}
		return out, nil
	case *object.Hash:
		h := &object.Hash{Pairs: make(map[object.HashKey]object.HashPair, len(v.Pairs))}
		for _, hk := range v.Keys {
			pair, ok := v.Pairs[hk]
			if !ok {
				continue
			}
			k, err := localizeHandle(pair.Key, want)
			if err != nil {
				return nil, err
			}
			val, err := localizeHandle(pair.Value, want)
			if err != nil {
				return nil, err
			}
			h.SetKey(k, val)
		}
		return h, nil
	}
	return o, nil
}

// workerFor 由句柄 id 反解出所属 worker 与 localId，并做会话戳校验（防 ABA）
func (c *Client) workerFor(p *object.PyObj) (*worker, int64, error) {
	if p == nil {
		return nil, 0, errors.New("pyext: 空句柄")
	}
	idx := int(p.ID >> handleShift)
	if idx < 0 || idx >= len(c.workers) {
		return nil, 0, fmt.Errorf("pyext: 句柄 #%d 无对应工作进程（当前池大小 %d）", p.ID, len(c.workers))
	}
	w := c.workers[idx]
	w.mu.Lock()
	sess := w.session
	alive := w.started && !w.dead
	w.mu.Unlock()
	if p.Session != "" && sess != "" && p.Session != sess {
		return nil, 0, errors.New("句柄已失效：CPython 子进程重启过，请重新 py导入 / py取（旧句柄不再指向原对象）")
	}
	if !alive {
		// 进程已退出：不能用旧 localId 在新进程里「碰运气」（新句柄表从 1 重分配 = ABA）
		return nil, 0, errors.New("句柄已失效：CPython 子进程已退出，请重新 py导入 / py取（旧句柄不再指向原对象）")
	}
	return w, p.ID & handleMask, nil
}

// ==================== 调用（自动启动 / 自动重启一次） ====================

// callWorker 在指定 worker 上执行一次调用（payload 中的句柄 id 必须已是 localId）
// allowRestart=true：无句柄 op（globals 类），崩了重启一次重试是安全的；
// allowRestart=false：句柄 op，重启后句柄表会重分配，重试等于 ABA，必须报错让上层重新导入。
func (c *Client) callWorker(w *worker, op string, payload map[string]any, timeout time.Duration, allowRestart bool) (object.Object, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := c.startWorkerLocked(w); err != nil {
		return nil, err
	}
	raw, err := c.roundTripLocked(w, op, payload, timeout)
	if err != nil {
		if allowRestart && w.dead && !c.isClosed() {
			// 重启一次再试（子进程可能被外部杀掉）
			if e2 := c.startWorkerLocked(w); e2 == nil {
				raw, err = c.roundTripLocked(w, op, payload, timeout)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	if len(raw) == 0 {
		return &object.Null{}, nil
	}
	v, err := decodeVal(raw, w.session)
	if err != nil {
		return nil, err
	}
	return tagHandle(w.idx, v), nil
}

// call 主通道调用（globals 类 op，允许崩后重启重试）
func (c *Client) call(op string, payload map[string]any) (object.Object, error) {
	return c.callWorker(c.primary(), op, payload, 0, true)
}

// ==================== 对外 API（zuzo 内置函数调用） ====================

// Active 是否处于「粘性子进程模式」
func (c *Client) Active() bool {
	c.stickyMu.Lock()
	sticky := c.sticky
	c.stickyMu.Unlock()
	if !sticky {
		return false
	}
	w := c.primary()
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.started && !w.dead
}

// Activate 激活并粘住子进程模式（命中 C 扩展时自动调用）
func (c *Client) Activate() error {
	c.stickyMu.Lock()
	if c.closed {
		c.stickyMu.Unlock()
		return errors.New("pyext: 桥已关闭")
	}
	c.sticky = true
	c.stickyMu.Unlock()
	return c.Start()
}

// SetSticky 手动设置粘性模式（py模式("本地") 关闭）
func (c *Client) SetSticky(v bool) {
	c.stickyMu.Lock()
	defer c.stickyMu.Unlock()
	c.sticky = v
}

// Import 导入一个 Python 模块（round-robin 分配 worker，句柄因此带上路由信息）
func (c *Client) Import(name string) (object.Object, error) {
	return c.callWorker(c.nextWorker(), "import", map[string]any{"name": name}, 0, false)
}

// GetAttr 取属性
func (c *Client) GetAttr(target *object.PyObj, name string) (object.Object, error) {
	w, id, err := c.workerFor(target)
	if err != nil {
		return nil, err
	}
	return c.callWorker(w, "get", map[string]any{"id": id, "name": name}, 0, false)
}

// SetAttr 设属性
func (c *Client) SetAttr(target *object.PyObj, name string, v object.Object) error {
	w, id, err := c.workerFor(target)
	if err != nil {
		return err
	}
	// 入参里的句柄必须属于 target 所在 worker（encodeObj 不感知连接池，先本地化再编码）
	loc, err := localizeHandle(v, w.idx)
	if err != nil {
		return err
	}
	enc, err := encodeObj(loc)
	if err != nil {
		return err
	}
	_, err = c.callWorker(w, "set", map[string]any{"id": id, "name": name, "v": enc}, 0, false)
	return err
}

// Call 调用句柄
func (c *Client) Call(target *object.PyObj, args []object.Object, kwargs map[string]object.Object) (object.Object, error) {
	w, id, err := c.workerFor(target)
	if err != nil {
		return nil, err
	}
	encArgs := make([]any, 0, len(args))
	for _, a := range args {
		loc, err := localizeHandle(a, w.idx)
		if err != nil {
			return nil, err
		}
		e, err := encodeObj(loc)
		if err != nil {
			return nil, err
		}
		encArgs = append(encArgs, e)
	}
	encKw := map[string]any{}
	for k, v := range kwargs {
		loc, err := localizeHandle(v, w.idx)
		if err != nil {
			return nil, err
		}
		e, err := encodeObj(loc)
		if err != nil {
			return nil, err
		}
		encKw[k] = e
	}
	return c.callWorker(w, "call", map[string]any{"id": id, "args": encArgs, "kwargs": encKw}, 0, false)
}

// Repr 取远端 repr
func (c *Client) Repr(target *object.PyObj) (string, error) {
	w, id, err := c.workerFor(target)
	if err != nil {
		return "", err
	}
	v, err := c.callWorker(w, "repr", map[string]any{"id": id}, 0, false)
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
	w, id, err := c.workerFor(target)
	if err != nil {
		return nil, err
	}
	return c.callWorker(w, "native", map[string]any{"id": id}, 0, false)
}

// Release 释放远端句柄
func (c *Client) Release(target *object.PyObj) error {
	w, id, err := c.workerFor(target)
	if err != nil {
		return err
	}
	_, err = c.callWorker(w, "release", map[string]any{"id": id}, 0, false)
	return err
}

// HasModule 探测模块是否可导入（不实际执行；无状态，走主通道）
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

// Exec 在子进程常驻 globals 执行代码（主通道）
func (c *Client) Exec(code string) error {
	_, err := c.call("exec", map[string]any{"code": code})
	return err
}

// Eval 在子进程常驻 globals 求值表达式（主通道）
func (c *Client) Eval(expr string) (object.Object, error) {
	return c.call("eval", map[string]any{"expr": expr})
}

// Lookup 按名字取子进程 globals 里的对象（找不到返回 false；主通道）
func (c *Client) Lookup(name string) (object.Object, bool) {
	v, err := c.call("getglobal", map[string]any{"name": name})
	if err != nil {
		return nil, false
	}
	return v, true
}

// SetName 设置子进程 globals 变量（主通道）
func (c *Client) SetName(name string, v object.Object) error {
	loc, err := localizeHandle(v, c.primary().idx)
	if err != nil {
		return err
	}
	enc, err := encodeObj(loc)
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
	w := c.primary()
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := c.startWorkerLocked(w); err != nil {
		return nil, err
	}
	raw, err := c.roundTripLocked(w, "hello", nil, 30*time.Second)
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
		"版本":    &object.String{Value: hs.Py},
		"解释器":   &object.String{Value: hs.Exe},
		"实现":    &object.String{Value: hs.Impl},
		"模式":    &object.String{Value: c.modeLocked()},
		"工作进程数": &object.Integer{Value: int64(len(c.workers))},
	}
	return out, nil
}

// Mode 当前模式（"本地" / "子进程"）
func (c *Client) Mode() string {
	c.stickyMu.Lock()
	defer c.stickyMu.Unlock()
	return c.modeLocked()
}

func (c *Client) modeLocked() string {
	if c.sticky {
		return "子进程"
	}
	return "本地"
}

// Stats 观测：调用次数 / 失败次数 / 已运行时长（跨 worker 汇总）
func (c *Client) Stats() map[string]object.Object {
	var up float64
	for _, w := range c.workers {
		w.mu.Lock()
		if w.started && !w.dead {
			if d := time.Since(w.startedAt).Seconds(); d > up {
				up = d
			}
		}
		w.mu.Unlock()
	}
	return map[string]object.Object{
		"调用次数": &object.Integer{Value: atomic.LoadInt64(&c.callCount)},
		"失败次数": &object.Integer{Value: atomic.LoadInt64(&c.failCount)},
		"运行秒数": &object.Float{Value: up},
	}
}

// PythonExe 返回桥使用的 CPython 可执行文件（供 zuzo pip 复用同一解释器）
func (c *Client) PythonExe() (string, error) {
	exe, _, err := c.resolvePython()
	return exe, err
}

// Pip 把 `zuzo pip ...` 原样转给同一个 CPython 的 pip（流式输出，退出码透传）
// 注意：与 py导入 用同一个解释器，装完立刻能被 py导入 看到（子进程侧还会 invalidate_caches 兜底）。
func Pip(args []string, in io.Reader, out, errw io.Writer) (int, error) {
	exe, err := Default().PythonExe()
	if err != nil {
		return 2, err
	}
	cmdArgs := append([]string{"-m", "pip"}, args...)
	cmd := exec.Command(exe, cmdArgs...)
	cmd.Stdin = in
	cmd.Stdout = out
	cmd.Stderr = errw
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode(), nil
		}
		return 2, err
	}
	return 0, nil
}

// Install 走 pip 安装（显式调用，绝不隐式联网）
func (c *Client) Install(pkg string) (string, error) {
	exe, _, err := c.resolvePython()
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
