package evaluator

import (
	"io"
	"net/http"
	"strings"
	"time"

	"zuzo/object"
)

// ==================== 网络内置函数（封装 Go net/http） ====================
// 注：URL 编解码已迁移至 builtins_encoding.go

// httpClient 全局复用的 HTTP 客户端（带超时）
var httpClient = &http.Client{
	Timeout: 30 * time.Second,
}

// http获取(url) → 字符串 — HTTP GET 请求，返回响应体
func builtinHttpGet(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("http获取() 期望 1 个参数，得到 %d 个", len(args))
	}
	urlStr, ok := args[0].(*object.String)
	if !ok {
		return newError("http获取() 参数必须是字符串（URL），得到 %s", args[0].Type())
	}

	resp, err := httpClient.Get(urlStr.Value)
	if err != nil {
		return newError("http获取() 请求失败: %s", err.Error())
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return newError("http获取() 读取响应失败: %s", err.Error())
	}

	if resp.StatusCode >= 400 {
		return newError("http获取() 服务器返回错误状态 %d: %s", resp.StatusCode, string(body))
	}
	return &object.String{Value: string(body)}
}

// http提交(url, body) → 字符串 — HTTP POST 请求，返回响应体
func builtinHttpPost(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("http提交() 期望 2 个参数，得到 %d 个", len(args))
	}
	urlStr, ok := args[0].(*object.String)
	if !ok {
		return newError("http提交() 第一个参数必须是字符串（URL），得到 %s", args[0].Type())
	}
	body, ok := args[1].(*object.String)
	if !ok {
		return newError("http提交() 第二个参数必须是字符串（请求体），得到 %s", args[1].Type())
	}

	resp, err := httpClient.Post(urlStr.Value, "application/json", strings.NewReader(body.Value))
	if err != nil {
		return newError("http提交() 请求失败: %s", err.Error())
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return newError("http提交() 读取响应失败: %s", err.Error())
	}

	if resp.StatusCode >= 400 {
		return newError("http提交() 服务器返回错误状态 %d: %s", resp.StatusCode, string(respBody))
	}
	return &object.String{Value: string(respBody)}
}

// http更新(url, body) → 字符串 — HTTP PUT 请求，返回响应体
func builtinHttpPut(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("http更新() 期望 2 个参数，得到 %d 个", len(args))
	}
	urlStr, ok := args[0].(*object.String)
	if !ok {
		return newError("http更新() 第一个参数必须是字符串（URL），得到 %s", args[0].Type())
	}
	body, ok := args[1].(*object.String)
	if !ok {
		return newError("http更新() 第二个参数必须是字符串（请求体），得到 %s", args[1].Type())
	}

	req, err := http.NewRequest("PUT", urlStr.Value, strings.NewReader(body.Value))
	if err != nil {
		return newError("http更新() 创建请求失败: %s", err.Error())
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return newError("http更新() 请求失败: %s", err.Error())
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return newError("http更新() 读取响应失败: %s", err.Error())
	}

	if resp.StatusCode >= 400 {
		return newError("http更新() 服务器返回错误状态 %d: %s", resp.StatusCode, string(respBody))
	}
	return &object.String{Value: string(respBody)}
}

// http补丁(url, body) → 字符串 — HTTP PATCH 请求，返回响应体
func builtinHttpPatch(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("http补丁() 期望 2 个参数，得到 %d 个", len(args))
	}
	urlStr, ok := args[0].(*object.String)
	if !ok {
		return newError("http补丁() 第一个参数必须是字符串（URL），得到 %s", args[0].Type())
	}
	body, ok := args[1].(*object.String)
	if !ok {
		return newError("http补丁() 第二个参数必须是字符串（请求体），得到 %s", args[1].Type())
	}

	req, err := http.NewRequest("PATCH", urlStr.Value, strings.NewReader(body.Value))
	if err != nil {
		return newError("http补丁() 创建请求失败: %s", err.Error())
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return newError("http补丁() 请求失败: %s", err.Error())
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return newError("http补丁() 读取响应失败: %s", err.Error())
	}

	if resp.StatusCode >= 400 {
		return newError("http补丁() 服务器返回错误状态 %d: %s", resp.StatusCode, string(respBody))
	}
	return &object.String{Value: string(respBody)}
}

// http删除(url) → 字符串 — HTTP DELETE 请求，返回响应体
func builtinHttpDelete(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("http删除() 期望 1 个参数，得到 %d 个", len(args))
	}
	urlStr, ok := args[0].(*object.String)
	if !ok {
		return newError("http删除() 参数必须是字符串（URL），得到 %s", args[0].Type())
	}

	req, err := http.NewRequest("DELETE", urlStr.Value, nil)
	if err != nil {
		return newError("http删除() 创建请求失败: %s", err.Error())
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return newError("http删除() 请求失败: %s", err.Error())
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return newError("http删除() 读取响应失败: %s", err.Error())
	}

	if resp.StatusCode >= 400 {
		return newError("http删除() 服务器返回错误状态 %d: %s", resp.StatusCode, string(respBody))
	}
	return &object.String{Value: string(respBody)}
}

// http请求(method, url, body, headers) → 哈希 — 完整 HTTP 请求
// 返回哈希: {状态码: 整数, 响应体: 字符串, 响应头: 哈希}
// headers 参数为哈希 {键: 值}，可为空哈希 {}
func builtinHttpRequest(args ...object.Object) object.Object {
	if len(args) != 4 {
		return newError("http请求() 期望 4 个参数，得到 %d 个", len(args))
	}

	method, ok := args[0].(*object.String)
	if !ok {
		return newError("http请求() 第一个参数必须是字符串（HTTP 方法），得到 %s", args[0].Type())
	}
	urlStr, ok := args[1].(*object.String)
	if !ok {
		return newError("http请求() 第二个参数必须是字符串（URL），得到 %s", args[1].Type())
	}
	bodyStr, ok := args[2].(*object.String)
	if !ok {
		return newError("http请求() 第三个参数必须是字符串（请求体），得到 %s", args[2].Type())
	}
	headersHash, ok := args[3].(*object.Hash)
	if !ok {
		return newError("http请求() 第四个参数必须是哈希（请求头），得到 %s", args[3].Type())
	}

	// 构建请求
	var bodyReader io.Reader
	if bodyStr.Value != "" {
		bodyReader = strings.NewReader(bodyStr.Value)
	}

	req, err := http.NewRequest(strings.ToUpper(method.Value), urlStr.Value, bodyReader)
	if err != nil {
		return newError("http请求() 创建请求失败: %s", err.Error())
	}

	// 设置请求头（按插入顺序遍历，保证确定性）
	for _, hk := range headersHash.Keys {
		pair := headersHash.Pairs[hk]
		key, ok := pair.Key.(*object.String)
		if !ok {
			continue
		}
		val, ok := pair.Value.(*object.String)
		if !ok {
			continue
		}
		req.Header.Set(key.Value, val.Value)
	}

	// 发送请求
	resp, err := httpClient.Do(req)
	if err != nil {
		return newError("http请求() 请求失败: %s", err.Error())
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return newError("http请求() 读取响应失败: %s", err.Error())
	}

	// 构建返回哈希: {状态码: 整数, 响应体: 字符串, 响应头: 哈希}
	result := &object.Hash{}

	// 状态码
	statusKey := &object.String{Value: "状态码"}
	result.SetKey(statusKey, &object.Integer{Value: int64(resp.StatusCode)})

	// 响应体
	bodyKey := &object.String{Value: "响应体"}
	result.SetKey(bodyKey, &object.String{Value: string(respBody)})

	// 响应头
	respHeaders := &object.Hash{}
	for k, vs := range resp.Header {
		if len(vs) > 0 {
			hKey := &object.String{Value: k}
			hVal := &object.String{Value: strings.Join(vs, ", ")}
			respHeaders.SetKey(hKey, hVal)
		}
	}
	headersKey := &object.String{Value: "响应头"}
	result.SetKey(headersKey, respHeaders)

	return result
}

