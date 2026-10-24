package evaluator

import (
	"bufio"
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"zuzo/object"
)

// ==================== HTTP 服务端内置函数（封装 Go net/http） ====================
//
// 设计目标：让 zuzo 脚本能以中文语法起一个 HTTP 服务并处理请求，用于反代网关
// / 本地 API 服务等场景。解释器（Environment + 求值器）非线程安全，因此所有
// 请求处理经全局互斥锁串行化。
//
// 内置函数：
//   http服务(端口, 处理器)   启动并阻塞；处理器 = 函数(请求) -> 响应
//   http服务后台(端口, 处理器) 同上但立即返回（脚本可继续做别的事）
//
// 请求对象（Hash）：
//   { 方法: "POST", 路径: "/v1/chat/completions", 查询: {k:v}, 头: {k:v}, 体: "..." }
//
// 响应对象（Hash）：
//   { 状态: 200, 头: {"Content-Type":"application/json"}, 体: "..." }
//   { 状态: 200, 头: {...}, 流: 函数(写出) }   // 流式：逐块写出 + 立即 flush
//
// 流式回调签名：函数(写出)，其中 写出(文本) 为内置函数，调用即写入并 flush。

var httpServeMu sync.Mutex

// hashGetAny 依次尝试多个键名读取 Hash 字段（兼容中英文键）
func hashGetAny(h *object.Hash, keys ...string) object.Object {
	if h == nil || h.Pairs == nil {
		return nil
	}
	for _, k := range keys {
		hk := (&object.String{Value: k}).HashKey()
		if pair, ok := h.Pairs[hk]; ok {
			return pair.Value
		}
	}
	return nil
}

// objToPlainString 把对象转成可直接写出的字符串
func objToPlainString(o object.Object) string {
	switch v := o.(type) {
	case *object.String:
		return v.Value
	case nil:
		return ""
	default:
		return o.Inspect()
	}
}

// isCallable 判断对象是否可调用（用户函数 / 内置函数）
func isCallable(o object.Object) bool {
	if o == nil {
		return false
	}
	t := o.Type()
	return t == object.FUNCTION_OBJ || t == object.BUILTIN_OBJ
}

// httpRequestToHash 把 *http.Request 转成 zuzo 请求对象
func httpRequestToHash(r *http.Request) *object.Hash {
	body, _ := io.ReadAll(r.Body)

	query := &object.Hash{}
	for k, vs := range r.URL.Query() {
		query.SetKey(&object.String{Value: k}, &object.String{Value: vs[0]})
	}

	headers := &object.Hash{}
	for k, vs := range r.Header {
		headers.SetKey(&object.String{Value: k}, &object.String{Value: vs[0]})
	}

	req := &object.Hash{}
	req.SetKey(&object.String{Value: "方法"}, &object.String{Value: r.Method})
	req.SetKey(&object.String{Value: "路径"}, &object.String{Value: r.URL.Path})
	req.SetKey(&object.String{Value: "查询"}, query)
	req.SetKey(&object.String{Value: "头"}, headers)
	req.SetKey(&object.String{Value: "体"}, &object.String{Value: string(body)})
	return req
}

// writeHttpResponse 把 zuzo 响应对象写回 HTTP 连接（普通 / 流式两种）
func writeHttpResponse(w http.ResponseWriter, res object.Object) {
	resHash, ok := res.(*object.Hash)
	if !ok {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"error":"处理器必须返回响应对象（Hash）","detail":` + strconv.Quote(objToPlainString(res)) + `}`))
		return
	}

	status := 200
	if s := hashGetAny(resHash, "状态", "status"); s != nil {
		if si, ok := s.(*object.Integer); ok {
			status = int(si.Value)
		}
	}

	if hh := hashGetAny(resHash, "头", "headers"); hh != nil {
		if hhh, ok := hh.(*object.Hash); ok {
			for _, k := range hhh.Keys {
				pair := hhh.Pairs[k]
				w.Header().Set(objToPlainString(pair.Key), objToPlainString(pair.Value))
			}
		}
	}

	flusher, _ := w.(http.Flusher)

	// 流式响应：{ 流: 函数 } —— 回调收到 写出(文本) 内置函数
	if st := hashGetAny(resHash, "流", "stream"); st != nil && isCallable(st) {
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
		}
		w.Header().Set("Cache-Control", "no-cache")
		w.WriteHeader(status)
		if flusher != nil {
			flusher.Flush()
		}
		writeFn := &object.Builtin{
			Name: "写出",
			Fn: func(args ...object.Object) object.Object {
				if len(args) > 0 {
					_, _ = io.WriteString(w, objToPlainString(args[0]))
				}
				if flusher != nil {
					flusher.Flush()
				}
				return &object.Null{}
			},
		}
		out := CallValue(st, []object.Object{writeFn})
		if e, ok := out.(*object.Error); ok {
			_, _ = io.WriteString(w, "data: {\"error\":"+strconv.Quote(e.Message)+"}\n\n")
		}
		if flusher != nil {
			flusher.Flush()
		}
		return
	}

	// 普通响应
	body := ""
	if b := hashGetAny(resHash, "体", "body"); b != nil {
		body = objToPlainString(b)
	}
	if status == 204 || status == 304 {
		w.WriteHeader(status)
		return
	}
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

// newHttpServer 构造 http.Server（处理器统一包装：串行锁 + 异常兜底）
func newHttpServer(port int64, handler object.Object) *http.Server {
	return &http.Server{
		Addr: ":" + strconv.FormatInt(port, 10),
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// /health 绕过串行锁：慢请求不应拖垮健康检查
			if r.URL.Path == "/health" {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(200)
				_, _ = io.WriteString(w, `{"ok":true,"service":"zuzo-http"}`)
				return
			}
			httpServeMu.Lock()
			defer httpServeMu.Unlock()

			defer func() {
				if rec := recover(); rec != nil {
					w.Header().Set("Content-Type", "application/json; charset=utf-8")
					w.WriteHeader(500)
					_, _ = io.WriteString(w, `{"error":"处理器异常","detail":`+strconv.Quote(objToPlainString(&object.String{Value: toStringAny(rec)}))+`}`)
				}
			}()

			reqHash := httpRequestToHash(r)
			res := CallValue(handler, []object.Object{reqHash})
			if e, ok := res.(*object.Error); ok {
				w.Header().Set("Content-Type", "application/json; charset=utf-8")
				w.WriteHeader(500)
				_, _ = io.WriteString(w, `{"error":`+strconv.Quote(e.Message)+`}`)
				return
			}
			writeHttpResponse(w, res)
		}),
	}
}

// toStringAny 兜底把任意 recover 值转字符串
func toStringAny(v any) string {
	switch x := v.(type) {
	case error:
		return x.Error()
	case string:
		return x
	default:
		return "panic"
	}
}

// builtinHttpServe http服务(端口, 处理器) —— 启动 HTTP 服务并阻塞
func builtinHttpServe(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("http服务() 期望 2 个参数（端口, 处理器），得到 %d 个", len(args))
	}
	portObj, ok := args[0].(*object.Integer)
	if !ok {
		return newError("http服务() 第一个参数必须是整数端口，得到 %s", args[0].Type())
	}
	if !isCallable(args[1]) {
		return newError("http服务() 第二个参数必须是函数，得到 %s", args[1].Type())
	}

	srv := newHttpServer(portObj.Value, args[1])
	if err := srv.ListenAndServe(); err != nil {
		return newError("http服务() 启动失败: %s", err.Error())
	}
	return &object.Null{}
}

// builtinHttpServeBackground http服务后台(端口, 处理器) —— 启动后立即返回
func builtinHttpServeBackground(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("http服务后台() 期望 2 个参数（端口, 处理器），得到 %d 个", len(args))
	}
	portObj, ok := args[0].(*object.Integer)
	if !ok {
		return newError("http服务后台() 第一个参数必须是整数端口，得到 %s", args[0].Type())
	}
	if !isCallable(args[1]) {
		return newError("http服务后台() 第二个参数必须是函数，得到 %s", args[1].Type())
	}

	srv := newHttpServer(portObj.Value, args[1])
	go func() {
		_ = srv.ListenAndServe()
	}()
	return &object.Null{}
}
// ==================== 流式 HTTP 客户端内置（反代场景：逐行回调上游 SSE） ====================

// 共享 Transport：连接复用（每次新建 Client 会重新握手，反代场景累加 100~300ms）
var sharedStreamTransport = &http.Transport{
	Proxy:                 http.ProxyFromEnvironment,
	MaxIdleConns:          32,
	MaxIdleConnsPerHost:   16,
	IdleConnTimeout:       90 * time.Second,
	TLSHandshakeTimeout:   15 * time.Second,
	ExpectContinueTimeout: 1 * time.Second,
	ForceAttemptHTTP2:     true,
}

// builtinHttpStreamRequest http流请求(方法, url, 体, 头, 回调[, 超时秒]) -> {状态码, 错误}
// 回调每收到一行调用一次（行内不含换行）；回调返回字符串 "停止" 可提前终止读取。
// 超时秒默认 600（整体超时，含读取响应体）。
func builtinHttpStreamRequest(args ...object.Object) object.Object {
	if len(args) != 5 && len(args) != 6 {
		return newError("http流请求() 期望 5 或 6 个参数（方法, url, 体, 头, 回调[, 超时秒]），得到 %d 个", len(args))
	}
	method, ok1 := args[0].(*object.String)
	urlObj, ok2 := args[1].(*object.String)
	body, ok3 := args[2].(*object.String)
	headers, ok4 := args[3].(*object.Hash)
	if !ok1 || !ok2 || !ok3 || !ok4 {
		return newError("http流请求() 前四个参数类型必须是（字符串, 字符串, 字符串, 哈希）")
	}
	if !isCallable(args[4]) {
		return newError("http流请求() 第五个参数必须是回调函数")
	}

	var bodyReader io.Reader
	if body.Value != "" {
		bodyReader = strings.NewReader(body.Value)
	}
	req, err := http.NewRequest(strings.ToUpper(method.Value), urlObj.Value, bodyReader)
	if err != nil {
		return newError("http流请求() 创建请求失败: %s", err.Error())
	}
	for _, hk := range headers.Keys {
		pair := headers.Pairs[hk]
		k, okA := pair.Key.(*object.String)
		v, okB := pair.Value.(*object.String)
		if okA && okB {
			req.Header.Set(k.Value, v.Value)
		}
	}

	timeoutSec := int64(600)
	if len(args) == 6 {
		if n, ok := args[5].(*object.Integer); ok && n.Value > 0 {
			timeoutSec = n.Value
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeoutSec)*time.Second)
	defer cancel()
	req = req.WithContext(ctx)

	streamClient := &http.Client{Transport: sharedStreamTransport}
	resp, err := streamClient.Do(req)
	if err != nil {
		failed := &object.Hash{}
		failed.SetKey(&object.String{Value: "状态码"}, &object.Integer{Value: 0})
		failed.SetKey(&object.String{Value: "错误"}, &object.String{Value: err.Error()})
		return failed
	}

	defer resp.Body.Close()

	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			trimmed := strings.TrimRight(line, "\r\n")
			if trimmed != "" {
				out := CallValue(args[4], []object.Object{&object.String{Value: trimmed}})
				if e, ok := out.(*object.Error); ok {
					return e
				}
				if s, ok := out.(*object.String); ok && s.Value == "停止" {
					break
				}
			}
		}
		if err != nil {
			break
		}
	}

	result := &object.Hash{}
	result.SetKey(&object.String{Value: "状态码"}, &object.Integer{Value: int64(resp.StatusCode)})
	result.SetKey(&object.String{Value: "错误"}, &object.String{Value: ""})
	return result
}
