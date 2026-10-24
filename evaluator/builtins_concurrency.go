package evaluator

import (
	"sync"

	"zuzo/object"
)

// ==================== 并发内置函数（封装 Go goroutine + channel） ====================

// 并发(函数) → 通道 — 启动 goroutine 执行函数，返回通道接收结果
func builtinGo(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("并发() 期望 1 个参数（函数），得到 %d 个", len(args))
	}
	fn, ok := args[0].(*object.Function)
	if !ok {
		return newError("并发() 参数必须是函数，得到 %s", args[0].Type())
	}

	ch := make(chan object.Object, 1)
	go func() {
		// 捕获 goroutine 内部的 panic，确保通道一定会收到值
		// 否则 panic 会导致 ch <- result 永不执行，主线程永久死锁
		defer func() {
			if r := recover(); r != nil {
				ch <- newError("并发任务内部崩溃: %v", r)
			}
		}()

		result := applyFunction(fn, nil)
		ch <- result
	}()
	return &object.Channel{Value: ch}
}

// 尝试执行(函数) → 值 | 错误 — 执行函数，捕获运行时错误，不抛出
// 如果函数执行成功，返回结果值；如果出错，返回错误对象
func builtinTryEval(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("尝试执行() 期望 1 个参数（函数），得到 %d 个", len(args))
	}
	fn, ok := args[0].(*object.Function)
	if !ok {
		return newError("尝试执行() 参数必须是函数，得到 %s", args[0].Type())
	}

	// 捕获 panic，确保不会崩溃
	defer func() {
		// panic 已在 applyFunction 内部处理，这里作为最后防线
		_ = recover()
	}()

	result := applyFunction(fn, nil)
	return result
}

// 通道() → 通道 — 创建无缓冲通道
// 通道(容量) → 通道 — 创建有缓冲通道
func builtinMakeChan(args ...object.Object) object.Object {
	bufSize := 0
	if len(args) == 1 {
		n, ok := args[0].(*object.Integer)
		if !ok {
			return newError("通道() 参数必须是整数（缓冲容量），得到 %s", args[0].Type())
		}
		bufSize = int(n.Value)
		if bufSize < 0 {
			return newError("通道() 缓冲容量不能为负数: %d", n.Value)
		}
	} else if len(args) > 1 {
		return newError("通道() 期望 0 或 1 个参数，得到 %d 个", len(args))
	}

	ch := make(chan object.Object, bufSize)
	return &object.Channel{Value: ch}
}

// 发送(通道, 值) → 空 — 向通道发送值（缓冲满时阻塞）
func builtinSend(args ...object.Object) (result object.Object) {
	if len(args) != 2 {
		return newError("发送() 期望 2 个参数，得到 %d 个", len(args))
	}
	ch, ok := args[0].(*object.Channel)
	if !ok {
		return newError("发送() 第一个参数必须是通道，得到 %s", args[0].Type())
	}

	defer func() {
		if r := recover(); r != nil {
			result = newError("发送() 失败: 通道可能已关闭")
		}
	}()

	ch.Value <- args[1]
	return NULL
}

// 接收(通道) → 值 — 从通道接收值（通道为空时阻塞）
func builtinRecv(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("接收() 期望 1 个参数，得到 %d 个", len(args))
	}
	ch, ok := args[0].(*object.Channel)
	if !ok {
		return newError("接收() 参数必须是通道，得到 %s", args[0].Type())
	}

	val, ok := <-ch.Value
	if !ok {
		return NULL
	}
	return val
}

// 尝试接收(通道) → 值 — 非阻塞接收，通道为空返回空
func builtinTryRecv(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("尝试接收() 期望 1 个参数，得到 %d 个", len(args))
	}
	ch, ok := args[0].(*object.Channel)
	if !ok {
		return newError("尝试接收() 参数必须是通道，得到 %s", args[0].Type())
	}

	select {
	case val, ok := <-ch.Value:
		if !ok {
			return NULL
		}
		return val
	default:
		return NULL
	}
}

// 关闭(通道) → 空 — 关闭通道
func builtinClose(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("关闭() 期望 1 个参数，得到 %d 个", len(args))
	}
	ch, ok := args[0].(*object.Channel)
	if !ok {
		return newError("关闭() 参数必须是通道，得到 %s", args[0].Type())
	}

	defer func() {
		if r := recover(); r != nil {
			// 重复关闭通道会 panic，忽略
		}
	}()

	close(ch.Value)
	return NULL
}

// 等待全部(通道...) → 数组 — 等待多个通道全部完成，按顺序返回结果
func builtinWaitAll(args ...object.Object) object.Object {
	if len(args) == 0 {
		return newError("等待全部() 至少需要 1 个参数")
	}

	channels := make([]*object.Channel, 0, len(args))
	for i, arg := range args {
		ch, ok := arg.(*object.Channel)
		if !ok {
			return newError("等待全部() 第 %d 个参数必须是通道，得到 %s", i+1, arg.Type())
		}
		channels = append(channels, ch)
	}

	results := make([]object.Object, len(channels))
	var wg sync.WaitGroup

	for i, ch := range channels {
		wg.Add(1)
		go func(idx int, c *object.Channel) {
			defer wg.Done()
			val, ok := <-c.Value
			if ok {
				results[idx] = val
			} else {
				results[idx] = NULL
			}
		}(i, ch)
	}

	wg.Wait()
	return &object.Array{Elements: results}
}
