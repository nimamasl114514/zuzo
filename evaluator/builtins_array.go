package evaluator

import (
	"slices"
	"strings"

	"zuzo/object"
)

// ==================== 数组内置函数 ====================

// 推入(数组, 元素) → 数组 — 追加元素到末尾
func builtinPush(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("推入() 期望 2 个参数，得到 %d 个", len(args))
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return newError("推入() 第一个参数必须是数组，得到 %s", args[0].Type())
	}
	arr.Elements = append(arr.Elements, args[1])
	return arr
}

// 弹出(数组) → 元素 — 弹出末尾元素
func builtinPop(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("弹出() 期望 1 个参数，得到 %d 个", len(args))
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return newError("弹出() 参数必须是数组，得到 %s", args[0].Type())
	}
	if len(arr.Elements) == 0 {
		return NULL
	}
	last := arr.Elements[len(arr.Elements)-1]
	arr.Elements = arr.Elements[:len(arr.Elements)-1]
	return last
}

// 首项(数组) → 元素 — 获取第一个元素
func builtinFirst(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("首项() 期望 1 个参数，得到 %d 个", len(args))
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return newError("首项() 参数必须是数组，得到 %s", args[0].Type())
	}
	if len(arr.Elements) == 0 {
		return NULL
	}
	return arr.Elements[0]
}

// 末项(数组) → 元素 — 获取最后一个元素
func builtinLast(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("末项() 期望 1 个参数，得到 %d 个", len(args))
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return newError("末项() 参数必须是数组，得到 %s", args[0].Type())
	}
	if len(arr.Elements) == 0 {
		return NULL
	}
	return arr.Elements[len(arr.Elements)-1]
}

// 连接(数组, 分隔符) → 字符串 — 将数组元素用分隔符连接成字符串
func builtinJoin(args ...object.Object) object.Object {
	if len(args) < 1 {
		return newError("连接() 至少需要 1 个参数")
	}
	arr, ok := args[0].(*object.Array)
	if !ok {
		return newError("连接() 第一个参数必须是数组")
	}
	sep := ""
	if len(args) >= 2 {
		if s, ok := args[1].(*object.String); ok {
			sep = s.Value
		}
	}
	var parts []string
	for _, e := range arr.Elements {
		parts = append(parts, e.Inspect())
	}
	return &object.String{Value: strings.Join(parts, sep)}
}

// 包含(集合, 元素) → 布尔 — 字符串包含或数组包含
func builtinContains(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("包含() 期望 2 个参数，得到 %d 个", len(args))
	}
	switch col := args[0].(type) {
	case *object.String:
		needle, ok := args[1].(*object.String)
		if !ok {
			return newError("包含() 第二个参数必须是字符串")
		}
		return nativeBoolToBooleanObject(strings.Contains(col.Value, needle.Value))
	case *object.Array:
		target := args[1].Inspect()
		return nativeBoolToBooleanObject(slices.ContainsFunc(col.Elements, func(e object.Object) bool {
			return e.Inspect() == target
		}))
	default:
		return newError("包含() 不支持类型: %s", col.Type())
	}
}
