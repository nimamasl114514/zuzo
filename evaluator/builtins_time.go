package evaluator

import (
	"strconv"
	"time"

	"zuzo/object"
)

// ==================== 时间内置函数（封装 Go time） ====================

// 时间戳() → 整数 — 返回当前 Unix 时间戳（秒）
func builtinUnixTime(args ...object.Object) object.Object {
	if len(args) != 0 {
		return newError("时间戳() 期望 0 个参数，得到 %d 个", len(args))
	}
	return &object.Integer{Value: time.Now().Unix()}
}

// 毫秒时间戳() → 整数 — 返回当前 Unix 毫秒时间戳
func builtinUnixMilli(args ...object.Object) object.Object {
	if len(args) != 0 {
		return newError("毫秒时间戳() 期望 0 个参数，得到 %d 个", len(args))
	}
	return &object.Integer{Value: time.Now().UnixMilli()}
}

// 当前时间() → 字符串 — 返回 ISO 8601 格式的当前时间
func builtinNow(args ...object.Object) object.Object {
	if len(args) != 0 {
		return newError("当前时间() 期望 0 个参数，得到 %d 个", len(args))
	}
	return &object.String{Value: time.Now().Format("2006-01-02T15:04:05Z07:00")}
}

// 格式化时间(时间戳, 布局) → 字符串 — 将时间戳按 Go 布局格式化
// 布局参考 Go time 包: "2006-01-02 15:04:05"
func builtinFormatTime(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("格式化时间() 期望 2 个参数，得到 %d 个", len(args))
	}
	ts, ok := args[0].(*object.Integer)
	if !ok {
		return newError("格式化时间() 第一个参数必须是整数（时间戳），得到 %s", args[0].Type())
	}
	layout, ok := args[1].(*object.String)
	if !ok {
		return newError("格式化时间() 第二个参数必须是字符串（布局），得到 %s", args[1].Type())
	}
	t := time.Unix(ts.Value, 0)
	return &object.String{Value: t.Format(layout.Value)}
}

// 解析时间(字符串, 布局) → 整数 — 将时间字符串按布局解析为时间戳
func builtinParseTime(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("解析时间() 期望 2 个参数，得到 %d 个", len(args))
	}
	timeStr, ok := args[0].(*object.String)
	if !ok {
		return newError("解析时间() 第一个参数必须是字符串（时间），得到 %s", args[0].Type())
	}
	layout, ok := args[1].(*object.String)
	if !ok {
		return newError("解析时间() 第二个参数必须是字符串（布局），得到 %s", args[1].Type())
	}
	t, err := time.Parse(layout.Value, timeStr.Value)
	if err != nil {
		return newError("解析时间() 失败: %s", err.Error())
	}
	return &object.Integer{Value: t.Unix()}
}

// 睡眠(毫秒) → 空 — 阻塞当前协程指定毫秒数
func builtinSleep(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("睡眠() 期望 1 个参数，得到 %d 个", len(args))
	}
	ms, ok := args[0].(*object.Integer)
	if !ok {
		return newError("睡眠() 参数必须是整数（毫秒），得到 %s", args[0].Type())
	}
	if ms.Value < 0 {
		return newError("睡眠() 参数不能为负数: %d", ms.Value)
	}
	time.Sleep(time.Duration(ms.Value) * time.Millisecond)
	return NULL
}

// 计时(函数) → 整数 — 执行函数并返回耗时（毫秒）
func builtinTimer(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("计时() 期望 1 个参数，得到 %d 个", len(args))
	}
	fn, ok := args[0].(*object.Function)
	if !ok {
		return newError("计时() 参数必须是函数，得到 %s", args[0].Type())
	}
	start := time.Now()
	applyFunction(fn, nil)
	elapsed := time.Since(start).Milliseconds()
	return &object.Integer{Value: elapsed}
}

// 计时秒(函数) → 浮点数 — 执行函数并返回耗时（秒，带小数）
func builtinTimerSec(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("计时秒() 期望 1 个参数，得到 %d 个", len(args))
	}
	fn, ok := args[0].(*object.Function)
	if !ok {
		return newError("计时秒() 参数必须是函数，得到 %s", args[0].Type())
	}
	start := time.Now()
	applyFunction(fn, nil)
	elapsed := time.Since(start).Seconds()
	return &object.Float{Value: elapsed}
}

// 时间差(开始戳, 结束戳) → 字符串 — 返回人类可读的时间差
func builtinTimeDiff(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("时间差() 期望 2 个参数，得到 %d 个", len(args))
	}
	start, ok := args[0].(*object.Integer)
	if !ok {
		return newError("时间差() 第一个参数必须是整数（时间戳），得到 %s", args[0].Type())
	}
	end, ok := args[1].(*object.Integer)
	if !ok {
		return newError("时间差() 第二个参数必须是整数（时间戳），得到 %s", args[1].Type())
	}
	diff := end.Value - start.Value
	if diff < 0 {
		diff = -diff
	}

	var result string
	switch {
	case diff < 60:
		result = strconv.FormatInt(diff, 10) + " 秒"
	case diff < 3600:
		result = strconv.FormatInt(diff/60, 10) + " 分 " + strconv.FormatInt(diff%60, 10) + " 秒"
	case diff < 86400:
		result = strconv.FormatInt(diff/3600, 10) + " 时 " + strconv.FormatInt((diff%3600)/60, 10) + " 分"
	default:
		result = strconv.FormatInt(diff/86400, 10) + " 天 " + strconv.FormatInt((diff%86400)/3600, 10) + " 时"
	}
	return &object.String{Value: result}
}
