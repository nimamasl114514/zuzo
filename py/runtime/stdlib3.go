package runtime

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"zuzo/py/object"
)

// registerStdlib3 注册 threading / socket / urllib / tempfile / glob / uuid
func registerStdlib3(ip *Interp) {
	builtinModuleInits["threading"] = modThreading
	builtinModuleInits["socket"] = modSocket
	builtinModuleInits["urllib"] = modUrllib
	builtinModuleInits["urllib.request"] = modUrllibRequest
	builtinModuleInits["tempfile"] = modTempfile
	builtinModuleInits["glob"] = modGlob
	builtinModuleInits["uuid"] = modUUID
}

var threadCounter int64

// ==================== threading ====================

// pyThreadIdent 返回当前执行流的标识（CPython threading.get_ident 口径）。
// zuzo 用 goroutine 模拟线程，这里用线程 goroutine 的运行时 id 作近似；
// 取不到时退回 1（单线程语义，logging 只拿它当记录字段）。
func pyThreadIdent() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	// 格式："goroutine 123 [running]:"
	s := string(buf[:n])
	const p = "goroutine "
	if i := strings.Index(s, p); i >= 0 {
		j := i + len(p)
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if v, err := strconv.ParseUint(s[j:k], 10, 64); err == nil {
			return v
		}
	}
	return 1
}

// sockDefaultTimeout 对应 socket.setdefaulttimeout 的全局默认值。
var sockDefaultTimeout object.Object = object.None

func modThreading(ip *Interp) *object.Module {
	m := newModule("threading")
	cls := &object.TypeObj{Name: "Thread", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	cls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		self := a[0].(*object.Instance)
		self.Dict["_target"] = object.None
		self.Dict["_args"] = &object.Tuple{}
		self.Dict["_name"] = &object.Str{V: "Thread"}
		if k != nil {
			if v, ok := k["target"]; ok {
				self.Dict["_target"] = v
			}
			if v, ok := k["args"]; ok {
				self.Dict["_args"] = v
			}
			if v, ok := k["name"]; ok {
				self.Dict["_name"] = v
			}
		}
		if len(a) > 1 {
			self.Dict["_target"] = a[1]
		}
		n := atomic.AddInt64(&threadCounter, 1)
		self.Dict["name"] = &object.Str{V: fmt.Sprintf("Thread-%d", n)}
		return object.None, nil
	})
	cls.Dict["start"] = bi("start", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		self := a[0].(*object.Instance)
		target := self.Dict["_target"]
		args := []object.Object{}
		if tp, ok := self.Dict["_args"].(*object.Tuple); ok {
			args = tp.Items
		}
		done := make(chan struct{})
		th := &object.PyThread{Done: done}
		self.Dict["_thread"] = th
		go func() {
			defer func() {
				if r := recover(); r != nil {
					if pe, ok := r.(*PyError); ok {
						th.Err = pe
					}
				}
				close(done)
			}()
			// GIL：与主执行流互斥（CPython 语义，计算串行）
			ip.gil.Lock()
			defer ip.gil.Unlock()
			ip.Call(target, args, nil)
		}()
		return object.None, nil
	})
	cls.Dict["join"] = bi("join", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		self := a[0].(*object.Instance)
		if th, ok := self.Dict["_thread"].(*object.PyThread); ok {
			// 等待期间释放 GIL，否则线程体永远拿不到锁（CPython join 同语义）
			if ip.gilHeld.CompareAndSwap(true, false) {
				ip.gil.Unlock()
				<-th.Done
				ip.gil.Lock()
				ip.gilHeld.Store(true)
			} else {
				<-th.Done
			}
			if th.Err != nil {
				if pe, ok := th.Err.(*PyError); ok {
					panic(pe)
				}
			}
			return object.None, nil
		}
		return object.None, nil
	})
	cls.Dict["is_alive"] = bi("is_alive", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		self := a[0].(*object.Instance)
		if th, ok := self.Dict["_thread"].(*object.PyThread); ok {
			select {
			case <-th.Done:
				return object.FalseObj, nil
			default:
				return object.TrueObj, nil
			}
		}
		return object.FalseObj, nil
	})
	m.Dict["Thread"] = cls
	// get_ident / get_native_id / main_thread：logging.LogRecord.__init__（logging/__init__.py:346）
	// 模块级就取 threading.get_ident()，据此填 record.thread / record.threadName。
	m.Dict["get_ident"] = bi("get_ident", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: int64(pyThreadIdent())}, nil
	})
	m.Dict["get_native_id"] = m.Dict["get_ident"]
	m.Dict["main_thread"] = bi("main_thread", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if v, ok := m.Dict["_main_thread"].(*object.Instance); ok {
			return v, nil
		}
		inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
		inst.Dict["name"] = &object.Str{V: "MainThread"}
		m.Dict["_main_thread"] = inst
		return inst, nil
	})
	m.Dict["TIMEOUT_MAX"] = &object.Float{V: 1e9}
	m.Dict["local"] = &object.TypeObj{Name: "local", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	m.Dict["Lock"] = bi("Lock", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.NewPyLock(), nil
	})
	// RLock 必须可重入（CPython 语义）：logging 的 _acquireLock / Handler.acquire 会嵌套调用，
	// 旧实现复用 Lock（channel 非重入）→ 退出阶段 logging.shutdown 永久阻塞。
	m.Dict["RLock"] = bi("RLock", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.NewPyRLock(), nil
	})
	m.Dict["current_thread"] = bi("current_thread", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
		inst.Dict["name"] = &object.Str{V: "MainThread"}
		return inst, nil
	})
	m.Dict["active_count"] = bi("active_count", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Int{V: 1}, nil
	})
	// Lock 方法
	object.LockType.Dict["acquire"] = bi("acquire", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		l := a[0].(*object.PyLock)
		l.Acquire()
		return object.TrueObj, nil
	})
	object.LockType.Dict["release"] = bi("release", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		l := a[0].(*object.PyLock)
		l.Release()
		return object.None, nil
	})
	object.LockType.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		l := a[0].(*object.PyLock)
		l.Acquire()
		return l, nil
	})
	object.LockType.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		l := a[0].(*object.PyLock)
		l.Release()
		return object.FalseObj, nil
	})
	object.ThreadType.Dict["join"] = bi("join", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if th, ok := a[0].(*object.PyThread); ok {
			<-th.Done
		}
		return object.None, nil
	})
	return m
}

// ==================== socket ====================

func modSocket(ip *Interp) *object.Module {
	m := newModule("socket")
	m.Dict["AF_INET"] = &object.Int{V: 2}
	m.Dict["AF_INET6"] = &object.Int{V: 10}
	m.Dict["SOCK_STREAM"] = &object.Int{V: 1}
	m.Dict["SOCK_DGRAM"] = &object.Int{V: 2}
	m.Dict["SOL_SOCKET"] = &object.Int{V: 1}
	m.Dict["SO_REUSEADDR"] = &object.Int{V: 2}
	sockCls := &object.TypeObj{Name: "socket", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	m.Dict["AF_UNSPEC"] = &object.Int{V: 0}
	m.Dict["has_ipv6"] = object.TrueObj
	m.Dict["AI_PASSIVE"] = &object.Int{V: 1}
	m.Dict["AI_CANONNAME"] = &object.Int{V: 2}
	m.Dict["AI_NUMERICHOST"] = &object.Int{V: 4}
	m.Dict["IPPROTO_IP"] = &object.Int{V: 0}
	m.Dict["SOCK_RAW"] = &object.Int{V: 3}
	m.Dict["SO_KEEPALIVE"] = &object.Int{V: 8}
	m.Dict["TCP_NODELAY"] = &object.Int{V: 1}
	m.Dict["error"] = ip.excType("OSError")
	// getdefaulttimeout / setdefaulttimeout / timeout：urllib3/util/timeout.py:6 模块级导入，
	// socket.timeout 在 CPython 3.10+ 是 TimeoutError 的别名。
	m.Dict["timeout"] = ip.excType("TimeoutError")
	m.Dict["gaierror"] = ip.excType("OSError")
	m.Dict["herror"] = ip.excType("OSError")
	m.Dict["getdefaulttimeout"] = bi("getdefaulttimeout", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if sockDefaultTimeout == nil {
			return object.None, nil
		}
		return sockDefaultTimeout, nil
	})
	m.Dict["setdefaulttimeout"] = bi("setdefaulttimeout", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 || a[0] == object.None {
			sockDefaultTimeout = object.None
			return object.None, nil
		}
		sockDefaultTimeout = a[0]
		return object.None, nil
	})

	m.Dict["IPPROTO_TCP"] = &object.Int{V: 6}
	m.Dict["SHUT_RD"] = &object.Int{V: 0}
	m.Dict["SHUT_WR"] = &object.Int{V: 1}
	m.Dict["SHUT_RDWR"] = &object.Int{V: 2}
	// http/client.py:887 用 socket._GLOBAL_DEFAULT_TIMEOUT 作为「未指定超时」的哨兵
	globalDefaultTimeout := &object.Instance{Class: &object.TypeObj{Name: "_GLOBAL_DEFAULT_TIMEOUT", Dict: map[string]object.Object{}}, Dict: map[string]object.Object{}}
	m.Dict["_GLOBAL_DEFAULT_TIMEOUT"] = globalDefaultTimeout
	// socket 异常族（requests/urllib3 到处 except socket.error）
	for _, n := range []string{"error", "timeout", "gaierror", "herror", "ConnectionError"} {
		m.Dict[n] = ip.excType("OSError")
	}
	m.Dict["create_connection"] = bi("create_connection", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) == 0 {
			ip.TypeError("create_connection() requires address")
		}
		addr := ip.ToSlice(a[0])
		host := "localhost"
		port := int64(0)
		if len(addr) > 0 {
			host = addr[0].Str()
		}
		if len(addr) > 1 {
			port, _ = object.AsInt(addr[1])
		}
		conn, err := net.Dial("tcp", net.JoinHostPort(host, strconv.FormatInt(port, 10)))
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		inst := &object.Instance{Class: sockCls, Dict: map[string]object.Object{}}
		inst.Dict["_s"] = &sockHandle{conn: conn}
		return inst, nil
	})
	m.Dict["getaddrinfo"] = bi("getaddrinfo", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) < 2 {
			ip.TypeError("getaddrinfo() requires host and port")
		}
		host := a[0].Str()
		port, _ := object.AsInt(a[1])
		addrs, err := net.LookupHost(host)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		out := make([]object.Object, 0, len(addrs))
		for _, ipStr := range addrs {
			out = append(out, &object.Tuple{Items: []object.Object{
				&object.Int{V: 2}, &object.Int{V: 1}, &object.Int{V: 6}, &object.Str{V: ""},
				&object.Tuple{Items: []object.Object{&object.Str{V: ipStr}, &object.Int{V: port}}},
			}})
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["gethostname"] = bi("gethostname", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		h, err := os.Hostname()
		if err != nil {
			return &object.Str{V: "localhost"}, nil
		}
		return &object.Str{V: h}, nil
	})

	sockCls.Dict["__init__"] = bi("__init__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		if len(a) > 0 {
			if inst, ok := a[0].(*object.Instance); ok {
				inst.Dict["_conn"] = object.None
				inst.Dict["_ln"] = object.None
				inst.Dict["_timeout"] = object.None
			}
		}
		return object.None, nil
	})
	sockCls.Dict["connect"] = bi("connect", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		addr := ip.ToSlice(a[1])
		if len(addr) < 2 {
			ip.ValueError("connect() requires (host, port)")
		}
		host := addr[0].Str()
		port, _ := object.AsInt(addr[1])
		conn, err := net.Dial("tcp", net.JoinHostPort(host, strconv.FormatInt(port, 10)))
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		inst.Dict["_s"] = &sockHandle{conn: conn}
		return object.None, nil
	})
	sockCls.Dict["sendall"] = bi("sendall", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		h, ok := inst.Dict["_s"].(*sockHandle)
		if !ok || h.conn == nil {
			ip.raise(ip.excType("OSError"), "socket is not connected")
		}
		var data []byte
		switch v := a[1].(type) {
		case *object.Bytes:
			data = v.V
		case *object.Str:
			data = []byte(v.V)
		}
		if _, err := h.conn.Write(data); err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return object.None, nil
	})
	sockCls.Dict["send"] = sockCls.Dict["sendall"]
	sockCls.Dict["recv"] = bi("recv", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		h, ok := inst.Dict["_s"].(*sockHandle)
		if !ok || h.conn == nil {
			ip.raise(ip.excType("OSError"), "socket is not connected")
		}
		n, _ := object.AsInt(a[1])
		buf := make([]byte, n)
		got, err := h.conn.Read(buf)
		if err != nil && err != io.EOF {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return &object.Bytes{V: buf[:got]}, nil
	})
	sockCls.Dict["close"] = bi("close", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if h, ok := inst.Dict["_s"].(*sockHandle); ok {
			if h.conn != nil {
				h.conn.Close()
			}
			if h.ln != nil {
				h.ln.Close()
			}
		}
		return object.None, nil
	})
	sockCls.Dict["bind"] = bi("bind", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		addr := ip.ToSlice(a[1])
		host := "0.0.0.0"
		port, _ := object.AsInt(addr[1])
		if len(addr) > 0 && addr[0].Str() != "" {
			host = addr[0].Str()
		}
		ln, err := net.Listen("tcp", net.JoinHostPort(host, strconv.FormatInt(port, 10)))
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		inst.Dict["_s"] = &sockHandle{ln: ln}
		inst.Dict["_bound"] = &object.Str{V: ln.Addr().String()}
		return object.None, nil
	})
	sockCls.Dict["listen"] = bi("listen", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	sockCls.Dict["accept"] = bi("accept", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		h, ok := inst.Dict["_s"].(*sockHandle)
		if !ok || h.ln == nil {
			ip.raise(ip.excType("OSError"), "socket is not listening")
		}
		conn, err := h.ln.Accept()
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		child := &object.Instance{Class: sockCls, Dict: map[string]object.Object{}}
		child.Dict["_s"] = &sockHandle{conn: conn}
		var host string
		var port int64
		if ta, ok := conn.RemoteAddr().(*net.TCPAddr); ok {
			host = ta.IP.String()
			port = int64(ta.Port)
		}
		return &object.Tuple{Items: []object.Object{child, &object.Tuple{Items: []object.Object{&object.Str{V: host}, &object.Int{V: port}}}}}, nil
	})
	sockCls.Dict["setsockopt"] = bi("setsockopt", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.None, nil
	})
	sockCls.Dict["settimeout"] = bi("settimeout", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if h, ok := inst.Dict["_s"].(*sockHandle); ok && h.conn != nil && len(a) > 1 {
			if f, ok2 := object.AsFloat(a[1]); ok2 {
				h.conn.SetDeadline(time.Now().Add(time.Duration(f * float64(time.Second))))
			}
		}
		return object.None, nil
	})
	sockCls.Dict["__enter__"] = bi("__enter__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return a[0], nil
	})
	sockCls.Dict["__exit__"] = bi("__exit__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if h, ok := inst.Dict["_s"].(*sockHandle); ok {
			if h.conn != nil {
				h.conn.Close()
			}
			if h.ln != nil {
				h.ln.Close()
			}
		}
		return object.FalseObj, nil
	})
	m.Dict["socket"] = sockCls
	m.Dict["gethostname"] = bi("gethostname", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		h, _ := os.Hostname()
		return &object.Str{V: h}, nil
	})
	return m
}

// sockHandle 套接字句柄（实现 object.Object 以便存入实例字典）
type sockHandle struct {
	conn net.Conn
	ln   net.Listener
}

var sockHandleType = &object.TypeObj{Name: "socket", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}

func (s *sockHandle) Type() *object.TypeObj { return sockHandleType }
func (s *sockHandle) Repr() string          { return "<socket>" }
func (s *sockHandle) Str() string           { return "<socket>" }

// ==================== urllib ====================

func modUrllib(ip *Interp) *object.Module {
	m := newModule("urllib")
	m.Dict["request"] = modUrllibRequest(ip)
	// 走 factory（而不是直接调 modUrllibParse）：stdlib_patch.go 会给
	// urllib.parse 包一层补 urlsplit/urljoin/parse_qs，绕过 factory 就吃不到补丁。
	if init, ok := builtinModuleInits["urllib.parse"]; ok {
		m.Dict["parse"] = init(ip)
	} else {
		m.Dict["parse"] = modUrllibParse(ip)
	}
	return m
}

// alwaysSafe 是 CPython urllib.parse 里永远不编码的字节集（RFC 3986 unreserved）。
const alwaysSafe = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789_.-~"

// pyQuoteBytes 按 CPython quote_from_bytes 口径做百分号编码。
func pyQuoteBytes(b []byte, safe string) string {
	var sb strings.Builder
	for _, c := range b {
		if strings.IndexByte(alwaysSafe, c) >= 0 || strings.IndexByte(safe, c) >= 0 {
			sb.WriteByte(c)
		} else {
			sb.WriteString(fmt.Sprintf("%%%02X", c))
		}
	}
	return sb.String()
}

// pyRawBytes 把 str/bytes/bytearray 统一解成字节切片。
func pyRawBytes(o object.Object) []byte {
	switch v := o.(type) {
	case *object.Bytes:
		return v.V
	case *object.ByteArray:
		return v.V
	}
	return []byte(o.Str())
}

func modUrllibParse(ip *Interp) *object.Module {
	m := newModule("urllib.parse")
	// quote / quote_from_bytes 走同一实现：CPython 的 always_safe = 字母数字 + "_.-~"，
	//		// safe 参数默认 "/"（旧实现把 / 硬编码进 safe 且漏了 ~，与 CPython 不一致）。
	m.Dict["quote"] = bi("quote", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		safe := "/"
		if len(a) > 1 && a[1] != object.None {
			safe = a[1].Str()
		}
		// requests.utils.requote_uri 用关键字形式 quote(uri, safe=...)；
		// 旧实现只读位置参数，safe 被丢弃 → 整条 URL 被逐字符编码成 %3A//…
		// （第16轮实测：InvalidSchema: No connection adapters were found for http%3A//…）。
		if v, ok := k["safe"]; ok && v != object.None {
			safe = v.Str()
		}
		return &object.Str{V: pyQuoteBytes([]byte(a[0].Str()), safe)}, nil
	})
	m.Dict["quote_from_bytes"] = bi("quote_from_bytes", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		safe := "/"
		if len(a) > 1 && a[1] != object.None {
			safe = a[1].Str()
		}
		if v, ok := k["safe"]; ok && v != object.None {
			safe = v.Str()
		}
		return &object.Str{V: pyQuoteBytes(pyRawBytes(a[0]), safe)}, nil
	})
	m.Dict["unquote_to_bytes"] = bi("unquote_to_bytes", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		s := pyRawBytes(a[0])
		out := make([]byte, 0, len(s))
		for i := 0; i < len(s); i++ {
			if s[i] == '%' && i+2 < len(s) {
				if v, err := hex.DecodeString(string(s[i+1 : i+3])); err == nil {
					out = append(out, v[0])
					i += 2
					continue
				}
			}
			out = append(out, s[i])
		}
		return &object.Bytes{V: out}, nil
	})
	// unquote_plus / urlunparse：requests/compat.py:69 的 from urllib.parse import (...) 清单项
	m.Dict["unquote_plus"] = bi("unquote_plus", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		s := ""
		if len(a) > 0 {
			s = a[0].Str()
		}
		s = strings.ReplaceAll(s, "+", " ")
		var sb strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '%' && i+2 < len(s) {
				if v, err := hex.DecodeString(s[i+1 : i+3]); err == nil {
					sb.WriteByte(v[0])
					i += 2
					continue
				}
			}
			sb.WriteByte(s[i])
		}
		return &object.Str{V: sb.String()}, nil
	})
	m.Dict["urlunparse"] = bi("urlunparse", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		parts := ip.ToSlice(a[0])
		get := func(i int) string {
			if i < len(parts) {
				return parts[i].Str()
			}
			return ""
		}
		// urlunparse 收 6 元组 (scheme, netloc, path, params, query, fragment)；
		// params 非空时按 CPython 口径并回 path。
		path := get(2)
		if params := get(3); params != "" {
			path = path + ";" + params
		}
		return &object.Str{V: joinURL(get(0), get(1), path, get(4), get(5))}, nil
	})
	// urldefrag：requests/compat.py:69 模块级 from urllib.parse import urldefrag
	m.Dict["urldefrag"] = bi("urldefrag", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		u := ""
		if len(a) > 0 {
			u = a[0].Str()
		}
		if i := strings.IndexByte(u, '#'); i >= 0 {
			return &object.Tuple{Items: []object.Object{&object.Str{V: u[:i]}, &object.Str{V: u[i+1:]}}}, nil
		}
		return &object.Tuple{Items: []object.Object{&object.Str{V: u}, &object.Str{V: ""}}}, nil
	})
	m.Dict["quote_plus"] = bi("quote_plus", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		s := a[0].Str()
		out := strings.ReplaceAll(s, " ", "+")
		return &object.Str{V: out}, nil
	})
	m.Dict["unquote"] = bi("unquote", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		s := a[0].Str()
		var sb strings.Builder
		for i := 0; i < len(s); i++ {
			if s[i] == '%' && i+2 < len(s) {
				if v, err := hex.DecodeString(s[i+1 : i+3]); err == nil {
					sb.WriteByte(v[0])
					i += 2
					continue
				}
			}
			sb.WriteByte(s[i])
		}
		return &object.Str{V: sb.String()}, nil
	})
	m.Dict["urlencode"] = bi("urlencode", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		enc := func(s string) string {
			var sb strings.Builder
			for _, b := range []byte(s) {
				if (b >= 'A' && b <= 'Z') || (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') ||
					b == '-' || b == '_' || b == '.' || b == '~' {
					sb.WriteByte(b)
				} else if b == ' ' {
					sb.WriteByte('+')
				} else {
					sb.WriteString(fmt.Sprintf("%%%02X", b))
				}
			}
			return sb.String()
		}
		var parts []string
		if d, ok := a[0].(*object.Dict); ok {
			for i, key := range d.Keys {
				parts = append(parts, enc(key.Str())+"="+enc(d.Vals[i].Str()))
			}
		} else {
			for _, pair := range ip.ToSlice(a[0]) {
				kv := ip.ToSlice(pair)
				if len(kv) == 2 {
					parts = append(parts, enc(kv[0].Str())+"="+enc(kv[1].Str()))
				}
			}
		}
		return &object.Str{V: strings.Join(parts, "&")}, nil
	})
	addUrllibParseExtras(ip, m) // patch 直接生效（不依赖 registerStdlibPatch 的包装顺序）
	return m
}

func modUrllibRequest(ip *Interp) *object.Module {
	m := newModule("urllib.request")
	// requests/compat.py:81 模块级 `from urllib.request import (getproxies,
	// getproxies_environment, parse_http_list, proxy_bypass, proxy_bypass_environment)`
	buildProxies := func() *object.Dict {
		d := &object.Dict{}
		for _, kv := range os.Environ() {
			i := strings.Index(kv, "=")
			if i <= 0 {
				continue
			}
			k, v := strings.ToLower(kv[:i]), kv[i+1:]
			if v == "" {
				continue
			}
			switch k {
			case "http_proxy":
				d.Set(&object.Str{V: "http"}, &object.Str{V: v})
			case "https_proxy":
				d.Set(&object.Str{V: "https"}, &object.Str{V: v})
			case "ftp_proxy":
				d.Set(&object.Str{V: "ftp"}, &object.Str{V: v})
			}
		}
		return d
	}
	m.Dict["getproxies"] = bi("getproxies", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return buildProxies(), nil
	})
	m.Dict["getproxies_environment"] = m.Dict["getproxies"]
	m.Dict["proxy_bypass"] = bi("proxy_bypass", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return object.FalseObj, nil
	})
	m.Dict["proxy_bypass_environment"] = m.Dict["proxy_bypass"]
	m.Dict["parse_http_list"] = bi("parse_http_list", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		src := ""
		if len(a) > 0 {
			src = a[0].Str()
		}
		var out []object.Object
		cur := strings.Builder{}
		inQuote := false
		for i := 0; i < len(src); i++ {
			c := src[i]
			switch {
			case inQuote:
				if c == '\\' && i+1 < len(src) {
					i++
					cur.WriteByte(src[i])
				} else if c == '"' {
					inQuote = false
				} else {
					cur.WriteByte(c)
				}
			case c == '"':
				inQuote = true
			case c == ',':
				out = append(out, &object.Str{V: strings.TrimSpace(cur.String())})
				cur.Reset()
			default:
				cur.WriteByte(c)
			}
		}
		if s := strings.TrimSpace(cur.String()); s != "" || len(out) > 0 {
			out = append(out, &object.Str{V: s})
		}
		return &object.List{Items: out}, nil
	})
	m.Dict["Request"] = &object.TypeObj{Name: "Request", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	m.Dict["urlopen"] = bi("urlopen", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		url := a[0].Str()
		method := "GET"
		var body io.Reader
		if len(a) > 1 {
			if d, ok := a[1].(*object.Bytes); ok {
				body = strings.NewReader(string(d.V))
				method = "POST"
			} else if d, ok := a[1].(*object.Str); ok {
				body = strings.NewReader(d.V)
				method = "POST"
			}
		}
		if k != nil {
			if v, ok := k["data"]; ok && v != object.None {
				if d, ok := v.(*object.Bytes); ok {
					body = strings.NewReader(string(d.V))
					method = "POST"
				}
			}
		}
		req, err := http.NewRequest(method, url, body)
		if err != nil {
			ip.raise(ip.excType("ValueError"), "%v", err)
		}
		client := &http.Client{Timeout: 30 * time.Second}
		resp, err := client.Do(req)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		data, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cls := &object.TypeObj{Name: "HTTPResponse", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
		inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
		inst.Dict["data"] = &object.Bytes{V: data}
		inst.Dict["status"] = &object.Int{V: int64(resp.StatusCode)}
		inst.Dict["headers"] = &object.Dict{}
		inst.Dict["read"] = bi("read", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			return &object.Bytes{V: data}, nil
		})
		inst.Dict["getcode"] = bi("getcode", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			return &object.Int{V: int64(resp.StatusCode)}, nil
		})
		inst.Dict["close"] = bi("close", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			return object.None, nil
		})
		inst.Dict["__enter__"] = bi("__enter__", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			return inst, nil
		})
		inst.Dict["__exit__"] = bi("__exit__", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			return object.FalseObj, nil
		})
		return inst, nil
	})
	m.Dict["urlretrieve"] = bi("urlretrieve", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		url := a[0].Str()
		dest := ""
		if len(a) > 1 {
			dest = a[1].Str()
		}
		resp, err := http.Get(url)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		defer resp.Body.Close()
		data, _ := io.ReadAll(resp.Body)
		if dest != "" {
			os.WriteFile(dest, data, 0o644)
		}
		return &object.Tuple{Items: []object.Object{&object.Str{V: dest}, &object.NoneObj{}}}, nil
	})
	return m
}

// ==================== tempfile / glob / uuid ====================

var tmpSeq int64
var tmpMu sync.Mutex

func modTempfile(ip *Interp) *object.Module {
	m := newModule("tempfile")
	m.Dict["gettempdir"] = bi("gettempdir", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Str{V: os.TempDir()}, nil
	})
	m.Dict["mkdtemp"] = bi("mkdtemp", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		prefix := "tmp"
		if len(a) > 0 {
			prefix = a[0].Str()
		}
		dir, err := os.MkdirTemp("", prefix)
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		return &object.Str{V: dir}, nil
	})
	m.Dict["mkstemp"] = bi("mkstemp", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		f, err := os.CreateTemp("", "tmp")
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		path := f.Name()
		f.Close()
		return &object.Tuple{Items: []object.Object{&object.Int{V: 0}, &object.Str{V: path}}}, nil
	})
	m.Dict["NamedTemporaryFile"] = bi("NamedTemporaryFile", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		f, err := os.CreateTemp("", "tmp")
		if err != nil {
			ip.raise(ip.excType("OSError"), "%v", err)
		}
		path := f.Name()
		f.Close()
		cls := &object.TypeObj{Name: "NamedTemporaryFile", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
		inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
		inst.Dict["name"] = &object.Str{V: path}
		inst.Dict["write"] = bi("write", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			os.WriteFile(path, []byte(x[1].Str()), 0o644)
			return &object.Int{V: int64(len(x[1].Str()))}, nil
		})
		inst.Dict["read"] = bi("read", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			data, _ := os.ReadFile(path)
			return &object.Bytes{V: data}, nil
		})
		inst.Dict["close"] = bi("close", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			os.Remove(path)
			return object.None, nil
		})
		inst.Dict["__enter__"] = bi("__enter__", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			return inst, nil
		})
		inst.Dict["__exit__"] = bi("__exit__", func(x []object.Object, kk map[string]object.Object) (object.Object, error) {
			os.Remove(path)
			return object.FalseObj, nil
		})
		return inst, nil
	})
	return m
}

func modGlob(ip *Interp) *object.Module {
	m := newModule("glob")
	m.Dict["glob"] = bi("glob", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		matches, err := filepath.Glob(a[0].Str())
		if err != nil {
			ip.raise(ip.excType("ValueError"), "%v", err)
		}
		items := make([]object.Object, len(matches))
		for i, mt := range matches {
			items[i] = &object.Str{V: filepath.ToSlash(mt)}
		}
		return &object.List{Items: items}, nil
	})
	m.Dict["escape"] = bi("escape", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return &object.Str{V: a[0].Str()}, nil
	})
	return m
}

func modUUID(ip *Interp) *object.Module {
	m := newModule("uuid")
	cls := &object.TypeObj{Name: "UUID", Bases: []*object.TypeObj{object.ObjectType}, Dict: map[string]object.Object{}}
	cls.Dict["__str__"] = bi("__str__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if v, ok := inst.Dict["_s"].(*object.Str); ok {
			return v, nil
		}
		return &object.Str{V: ""}, nil
	})
	cls.Dict["__repr__"] = bi("__repr__", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		inst := a[0].(*object.Instance)
		if v, ok := inst.Dict["_s"].(*object.Str); ok {
			return &object.Str{V: "UUID('" + v.V + "')"}, nil
		}
		return &object.Str{V: "UUID('')"}, nil
	})
	mkUUID := func() *object.Instance {
		b := make([]byte, 16)
		rand.Read(b)
		b[6] = (b[6] & 0x0f) | 0x40
		b[8] = (b[8] & 0x3f) | 0x80
		s := fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
		inst := &object.Instance{Class: cls, Dict: map[string]object.Object{}}
		inst.Dict["_s"] = &object.Str{V: s}
		inst.Dict["hex"] = &object.Str{V: strings.ReplaceAll(s, "-", "")}
		inst.Dict["int"] = &object.Int{V: 0}
		return inst
	}
	m.Dict["uuid4"] = bi("uuid4", func(a []object.Object, k map[string]object.Object) (object.Object, error) {
		return mkUUID(), nil
	})
	m.Dict["uuid1"] = m.Dict["uuid4"]
	m.Dict["uuid"] = cls
	return m
}
