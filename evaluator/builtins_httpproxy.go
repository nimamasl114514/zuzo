package evaluator

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"zuzo/object"
)

// ==================== HTTP 反向代理内置函数（多进程 worker 负载均衡） ====================
//
// 背景：zuzo 解释器带大量包级全局状态（callStack / traceDepth / debugEnvRef /
// profileData …），单进程内多 goroutine 共享求值器不安全，所以「并发」只能走
// 进程级隔离：起 N 个 worker 进程各占一个端口，再由一个极薄的 Go 层反代对外
// 提供单一入口，轮询分发。
//
// 本内置函数就是这个 Go 层反代：不经解释器（零全局状态竞争），SSE 逐块直通。
//
// 内置函数：
//   http反代(端口, 目标[, 选项])
//     目标  = "127.0.0.1:8791,127.0.0.1:8792" 或 ["127.0.0.1:8791", "127.0.0.1:8792"]
//     选项  = {
//       "重试": 1,              // 换 worker 重试次数（默认 1）
//       "超时": 600,            // 上游整体超时秒（默认 600）
//       "状态路径": "/__proxy/status"  // 状态端点（默认该值；置空禁用）
//       "状态文件": "C:/.../data.bin",  // 启用 /status 直返并算 token 剩余秒
//       "服务名": "orcaterm-gateway",
//       "调试": 假
//     }
//
// 行为：
//   - 轮询（atomic 计数）选 worker，保证并发请求分散到不同进程
//   - 请求体一次性读入内存（≤64MB），失败可换 worker 重放
//   - 响应头逐项复制（跳过 hop-by-hop），响应体 io.Copy + 每块 flush（SSE 直通）
//   - worker 连接失败 / 5xx 且响应未开始写出时，自动换下一个 worker 重试

const proxyMaxBody = 64 << 20

// hop-by-hop 头（RFC 7230 §6.1），不转发
var hopHeaders = map[string]bool{
	"Connection":          true,
	"Proxy-Connection":    true,
	"Keep-Alive":          true,
	"Proxy-Authenticate":  true,
	"Proxy-Authorization": true,
	"Te":                  true,
	"Trailer":             true,
	"Transfer-Encoding":   true,
	"Upgrade":             true,
}

type proxyTarget struct {
	url   *url.URL
	ok    atomic.Int64 // 成功计数
	fail  atomic.Int64 // 失败计数
	index int
}

type reverseProxyState struct {
	targets   []*proxyTarget
	counter   atomic.Uint64
	retries   int
	timeout   time.Duration
	client    *http.Client
	statusAPI string
	static    map[string][]byte // 路径 → 响应体（由 Go 层直返，不下发 worker）

	// /status 直返（Go 层读 data.bin 算 token 剩余，不排队等 worker）
	statusFile string
	service    string
	debug      bool
}

// 解析目标列表：字符串（逗号/分号分隔）或数组
func parseProxyTargets(o object.Object) []*url.URL {
	var items []string
	switch v := o.(type) {
	case *object.String:
		for _, part := range strings.FieldsFunc(v.Value, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\n'
		}) {
			if strings.TrimSpace(part) != "" {
				items = append(items, strings.TrimSpace(part))
			}
		}
	case *object.Array:
		for _, e := range v.Elements {
			if s, ok := e.(*object.String); ok && strings.TrimSpace(s.Value) != "" {
				items = append(items, strings.TrimSpace(s.Value))
			}
		}
	default:
		return nil
	}

	var out []*url.URL
	for _, it := range items {
		if !strings.Contains(it, "://") {
			it = "http://" + it
		}
		u, err := url.Parse(it)
		if err != nil || u.Host == "" {
			continue
		}
		out = append(out, u)
	}
	return out
}

// builtinHttpReverseProxy http反代(端口, 目标[, 选项]) —— 启动并阻塞
func builtinHttpReverseProxy(args ...object.Object) object.Object {
	if len(args) != 2 && len(args) != 3 {
		return newError("http反代() 期望 2 或 3 个参数（端口, 目标[, 选项]），得到 %d 个", len(args))
	}
	portObj, ok := args[0].(*object.Integer)
	if !ok {
		return newError("http反代() 第一个参数必须是整数端口，得到 %s", args[0].Type())
	}
	urls := parseProxyTargets(args[1])
	if len(urls) == 0 {
		return newError("http反代() 第二个参数（目标列表）为空或格式错误")
	}

	st := &reverseProxyState{
		retries:   1,
		timeout:   600 * time.Second,
		statusAPI: "/__proxy/status",
		static:    map[string][]byte{},
		service:   "zuzo-proxy",
	}
	for i, u := range urls {
		st.targets = append(st.targets, &proxyTarget{url: u, index: i})
	}
	if len(args) == 3 {
		if opt, ok := args[2].(*object.Hash); ok {
			if v, ok := hashGetAny(opt, "重试", "retries").(*object.Integer); ok && v.Value >= 0 {
				st.retries = int(v.Value)
			}
			if v, ok := hashGetAny(opt, "超时", "timeout").(*object.Integer); ok && v.Value > 0 {
				st.timeout = time.Duration(v.Value) * time.Second
			}
			if v, ok := hashGetAny(opt, "状态路径", "statusPath").(*object.String); ok {
				st.statusAPI = v.Value
			}
			// /status 直返：状态文件（OrcaTerm data.bin）→ 服务端算 tokenRemainSec
			if v, ok := hashGetAny(opt, "状态文件", "statusFile").(*object.String); ok {
				st.statusFile = v.Value
			}
			if v, ok := hashGetAny(opt, "服务名", "service").(*object.String); ok && v.Value != "" {
				st.service = v.Value
			}
			if v, ok := hashGetAny(opt, "调试", "debug").(*object.Boolean); ok {
				st.debug = v.Value
			}
			// 静态路由：{ "/v1/models": "<json>" } 或 { "/x": {"体": "..."} }
			if sv, ok := hashGetAny(opt, "静态", "static").(*object.Hash); ok {
				for _, k := range sv.Keys {
					pair := sv.Pairs[k]
					pk, okA := pair.Key.(*object.String)
					if !okA {
						continue
					}
					pv, okB := pair.Value.(*object.String)
					if !okB {
						if sub, okC := pair.Value.(*object.Hash); okC {
							pv = &object.String{Value: objToPlainString(hashGetAny(sub, "体", "body"))}
						}
					}
					if pv != nil {
						st.static[pk.Value] = []byte(pv.Value)
					}
				}
			}
		}
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext,
		MaxIdleConns:          64,
		MaxIdleConnsPerHost:   16,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		// 反代场景不压缩：SSE 若被 gzip 缓冲会破坏实时性
		DisableCompression: true,
	}
	st.client = &http.Client{
		Transport: transport,
		// 不设全局 Timeout：长流式响应由 context 控制，全局超时会掐断长回复
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	portStr := strconv.FormatInt(portObj.Value, 10)
	if st.statusFile == "" {
		st.statusFile = os.Getenv("ORCATERM_DATA") // 兜底：环境变量指定 data.bin
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if st.statusAPI != "" && r.URL.Path == st.statusAPI {
			st.writeStatus(w)
			return
		}
		// /status：Go 层直返网关自身状态 + token 剩余（worker 忙时不排队）
		if r.URL.Path == "/status" && st.statusFile != "" {
			st.writeSelfStatus(w, portStr)
			return
		}
		if r.URL.Path == "/health" {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(200)
			_, _ = io.WriteString(w, `{"ok":true,"service":"zuzo-proxy","workers":`+strconv.Itoa(len(st.targets))+`}`)
			return
		}
		// 静态路由直返（如 /v1/models）：不进 worker 队列，慢请求不影响它
		if body, ok := st.static[r.URL.Path]; ok {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(200)
			_, _ = w.Write(body)
			return
		}
		st.serve(w, r)
	})

	srv := &http.Server{
		Addr:              ":" + portStr,
		Handler:           mux,
		ReadHeaderTimeout: 15 * time.Second,
	}
	if err := srv.ListenAndServe(); err != nil {
		return newError("http反代() 启动失败: %s", err.Error())
	}
	return &object.Null{}
}

// writeStatus 反代自检端点：worker 列表与成功/失败计数
func (st *reverseProxyState) writeStatus(w http.ResponseWriter) {
	var sb strings.Builder
	sb.WriteString(`{"ok":true,"service":"zuzo-proxy","workers":[`)
	for i, t := range st.targets {
		if i > 0 {
			sb.WriteString(",")
		}
		sb.WriteString(`{"target":"`)
		sb.WriteString(t.url.String())
		sb.WriteString(`","ok":`)
		sb.WriteString(strconv.FormatInt(t.ok.Load(), 10))
		sb.WriteString(`,"fail":`)
		sb.WriteString(strconv.FormatInt(t.fail.Load(), 10))
		sb.WriteString(`}`)
	}
	sb.WriteString(`]}`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(200)
	_, _ = io.WriteString(w, sb.String())
}

// writeSelfStatus /status：网关自身状态（端口 / worker 数 / token 剩余 / 调试开关）
// 全部在 Go 层算完直返，不进入 worker 队列，避免 worker 处理长流时排障端点被拖慢。
func (st *reverseProxyState) writeSelfStatus(w http.ResponseWriter, port string) {
	remain := tokenRemainSeconds(st.statusFile)
	var sb strings.Builder
	sb.WriteString(`{"ok":true,"service":"`)
	sb.WriteString(escapeJSON(st.service))
	sb.WriteString(`","port":`)
	sb.WriteString(port)
	sb.WriteString(`,"workers":`)
	sb.WriteString(strconv.Itoa(len(st.targets)))
	sb.WriteString(`,"tokenRemainSec":`)
	sb.WriteString(strconv.FormatInt(remain, 10))
	sb.WriteString(`,"debug":`)
	sb.WriteString(strconv.FormatBool(st.debug))
	sb.WriteString(`}`)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)
	_, _ = io.WriteString(w, sb.String())
}

// tokenRemainSeconds 读 OrcaTerm data.bin，取 oauth_access_token 的 JWT exp，返回剩余秒数。
// 失败（文件缺失 / 非 JSON / 无 token / 无 exp）返回 -1，不阻断 /status。
func tokenRemainSeconds(path string) int64 {
	if path == "" {
		return -1
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return -1
	}
	var data map[string]interface{}
	if err := json.Unmarshal(raw, &data); err != nil {
		return -1
	}
	tok, _ := data["oauth_access_token"].(string)
	if tok == "" {
		return -1
	}
	parts := strings.Split(tok, ".")
	if len(parts) < 2 {
		return -1
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return -1
	}
	var claims map[string]interface{}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return -1
	}
	exp, ok := claims["exp"].(float64)
	if !ok {
		return -1
	}
	return int64(exp) - time.Now().Unix()
}

// serve 单请求处理：选 worker → 转发 → 失败换工重试
func (st *reverseProxyState) serve(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, proxyMaxBody))
	if err != nil {
		http.Error(w, `{"error":"读取请求体失败"}`, 400)
		return
	}
	_ = r.Body.Close()

	attempts := 1 + st.retries
	start := int(st.counter.Add(1)-1) % len(st.targets)

	var lastErr string
	for a := 0; a < attempts; a++ {
		t := st.targets[(start+a)%len(st.targets)]
		ok, msg := st.forward(w, r, body, t)
		if ok {
			t.ok.Add(1)
			return
		}
		t.fail.Add(1)
		lastErr = msg
		// 已经写出响应头就不能再换工重试（会破坏客户端收到的一半响应）
		if msg == "response-started" {
			return
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(502)
	_, _ = io.WriteString(w, `{"error":{"type":"proxy_error","message":"全部 worker 不可用: `+escapeJSON(lastErr)+`"}}`)
}

// forward 转发一次；返回 (是否成功, 失败原因)
func (st *reverseProxyState) forward(w http.ResponseWriter, r *http.Request, body []byte, t *proxyTarget) (bool, string) {
	outURL := *t.url
	outURL.Path = singleJoin(t.url.Path, r.URL.Path)
	outURL.RawQuery = r.URL.RawQuery

	ctx := r.Context()
	if st.timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, st.timeout)
		defer cancel()
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, outURL.String(), strings.NewReader(string(body)))
	if err != nil {
		return false, "build-request: " + err.Error()
	}
	// 复制请求头（跳过 hop-by-hop）
	for k, vs := range r.Header {
		if hopHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	req.Header.Set("X-Forwarded-For", clientIP(r))
	req.Header.Set("X-Proxy-Worker", strconv.Itoa(t.index))
	req.ContentLength = int64(len(body))
	req.Host = t.url.Host

	resp, err := st.client.Do(req)
	if err != nil {
		return false, "worker-" + strconv.Itoa(t.index) + ": " + err.Error()
	}
	defer resp.Body.Close()

	// 5xx 视为 worker 故障：未写响应前可换工重试
	if resp.StatusCode >= 500 {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return false, "worker-" + strconv.Itoa(t.index) + " status " + strconv.Itoa(resp.StatusCode) + ": " + string(snippet)
	}

	for k, vs := range resp.Header {
		if hopHeaders[http.CanonicalHeaderKey(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)

	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		flusher.Flush()
	}
	buf := make([]byte, 32*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return true, ""
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			break
		}
	}
	return true, ""
}

// singleJoin 路径拼接（保留 worker 自身前缀）
func singleJoin(prefix, path string) string {
	if prefix == "" || prefix == "/" {
		return path
	}
	return strings.TrimRight(prefix, "/") + path
}

// clientIP 取客户端地址（含端口，便于日志定位）
func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// escapeJSON 最小 JSON 字符串转义（避免引入额外依赖）
func escapeJSON(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				b.WriteString(`\u`)
				const hex = "0123456789abcdef"
				b.WriteByte('0')
				b.WriteByte('0')
				b.WriteByte(hex[(r>>4)&0xF])
				b.WriteByte(hex[r&0xF])
			} else {
				b.WriteRune(r)
			}
		}
	}
	return b.String()
}
