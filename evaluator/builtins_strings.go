package evaluator

import (
	"fmt"
	"strconv"
	"strings"

	"zuzo/object"
)

// ==================== 字符串处理内置函数（封装 Go strings/strconv） ====================

// 大写(s) → 字符串 — strings.ToUpper
func builtinToUpper(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("大写() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("大写() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: strings.ToUpper(s.Value)}
}

// 小写(s) → 字符串 — strings.ToLower
func builtinToLower(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("小写() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("小写() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: strings.ToLower(s.Value)}
}

// 前缀(s, prefix) → 布尔 — strings.HasPrefix
func builtinHasPrefix(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("前缀() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("前缀() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	prefix, ok := args[1].(*object.String)
	if !ok {
		return newError("前缀() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	return nativeBoolToBooleanObject(strings.HasPrefix(s.Value, prefix.Value))
}

// 后缀(s, suffix) → 布尔 — strings.HasSuffix
func builtinHasSuffix(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("后缀() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("后缀() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	suffix, ok := args[1].(*object.String)
	if !ok {
		return newError("后缀() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	return nativeBoolToBooleanObject(strings.HasSuffix(s.Value, suffix.Value))
}

// 查找(s, substr) → 整数 — strings.Index（返回位置，-1 表示未找到）
func builtinIndex(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("查找() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("查找() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	substr, ok := args[1].(*object.String)
	if !ok {
		return newError("查找() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	byteIdx := strings.Index(s.Value, substr.Value)
	if byteIdx < 0 {
		return &object.Integer{Value: -1}
	}
	// 与 长度()/取字符() 统一为「字符（rune）下标」，避免中文前置时偏移错位
	return &object.Integer{Value: int64(len([]rune(s.Value[:byteIdx])))}
}

// 替换(s, old, new) → 字符串 — strings.ReplaceAll
func builtinReplace(args ...object.Object) object.Object {
	if len(args) != 3 {
		return newError("替换() 期望 3 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("替换() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	old, ok := args[1].(*object.String)
	if !ok {
		return newError("替换() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	newStr, ok := args[2].(*object.String)
	if !ok {
		return newError("替换() 第三个参数必须是字符串，得到 %s", args[2].Type())
	}
	return &object.String{Value: strings.ReplaceAll(s.Value, old.Value, newStr.Value)}
}

// 分割(s, sep) → 数组[字符串] — strings.Split
func builtinSplit(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("分割() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("分割() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	sep, ok := args[1].(*object.String)
	if !ok {
		return newError("分割() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	parts := strings.Split(s.Value, sep.Value)
	elems := make([]object.Object, len(parts))
	for i, p := range parts {
		elems[i] = &object.String{Value: p}
	}
	return &object.Array{Elements: elems}
}

// 去空白(s) → 字符串 — strings.TrimSpace
func builtinTrimSpace(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("去空白() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("去空白() 参数必须是字符串，得到 %s", args[0].Type())
	}
	return &object.String{Value: strings.TrimSpace(s.Value)}
}

// 裁剪(s, cutset) → 字符串 — strings.Trim
func builtinTrim(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("裁剪() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("裁剪() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	cutset, ok := args[1].(*object.String)
	if !ok {
		return newError("裁剪() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	return &object.String{Value: strings.Trim(s.Value, cutset.Value)}
}

// 重复(s, n) → 字符串 — strings.Repeat
func builtinRepeat(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("重复() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("重复() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	n, ok := args[1].(*object.Integer)
	if !ok {
		return newError("重复() 第二个参数必须是整数，得到 %s", args[1].Type())
	}
	if n.Value < 0 {
		return newError("重复() 次数不能为负数: %d", n.Value)
	}
	return &object.String{Value: strings.Repeat(s.Value, int(n.Value))}
}

// 计数(s, substr) → 整数 — strings.Count
func builtinCount(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("计数() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("计数() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	substr, ok := args[1].(*object.String)
	if !ok {
		return newError("计数() 第二个参数必须是字符串，得到 %s", args[1].Type())
	}
	return &object.Integer{Value: int64(strings.Count(s.Value, substr.Value))}
}

// 分词(s) → 数组[字符串] — strings.Fields（按空白自动分词）
func builtinFields(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("分词() 期望 1 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("分词() 参数必须是字符串，得到 %s", args[0].Type())
	}
	fields := strings.Fields(s.Value)
	elems := make([]object.Object, len(fields))
	for i, f := range fields {
		elems[i] = &object.String{Value: f}
	}
	return &object.Array{Elements: elems}
}

// 取字符(s, i) → 字符串 — 取第 i 个 rune（以字符为单位，非字节）
func builtinCharAt(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("取字符() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("取字符() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	idx, ok := args[1].(*object.Integer)
	if !ok {
		return newError("取字符() 第二个参数必须是整数，得到 %s", args[1].Type())
	}
	runes := []rune(s.Value)
	if idx.Value < 0 || idx.Value >= int64(len(runes)) {
		return newError("取字符() 索引越界: %d（字符串长度 %d）", idx.Value, len(runes))
	}
	return &object.String{Value: string(runes[idx.Value])}
}

// 格式化 — 两种模式：
//
//	格式化("模板 %d %s", 参数...)  — sprintf 风格（Go fmt.Sprintf）
//	格式化(数值, 位数)              — 浮点数格式化（向后兼容）
func builtinFormat(args ...object.Object) object.Object {
	if len(args) < 1 {
		return newError("格式化() 至少需要 1 个参数")
	}

	// 模式1：sprintf 风格（第一个参数是字符串）
	if fmtStr, ok := args[0].(*object.String); ok {
		var goArgs []any
		for _, arg := range args[1:] {
			switch a := arg.(type) {
			case *object.Integer:
				goArgs = append(goArgs, a.Value)
			case *object.Float:
				goArgs = append(goArgs, a.Value)
			case *object.String:
				goArgs = append(goArgs, a.Value)
			case *object.Boolean:
				goArgs = append(goArgs, a.Value)
			default:
				goArgs = append(goArgs, a.Inspect())
			}
		}
		return &object.String{Value: fmt.Sprintf(fmtStr.Value, goArgs...)}
	}

	// 模式2：浮点数格式化（向后兼容：格式化(数值, 位数)）
	if len(args) != 2 {
		return newError("格式化() 期望 2 个参数，得到 %d 个", len(args))
	}
	var val float64
	switch n := args[0].(type) {
	case *object.Float:
		val = n.Value
	case *object.Integer:
		val = float64(n.Value)
	default:
		return newError("格式化() 第一个参数必须是字符串（模板）或数值，得到 %s", args[0].Type())
	}
	prec, ok := args[1].(*object.Integer)
	if !ok {
		return newError("格式化() 第二个参数必须是整数（小数位数），得到 %s", args[1].Type())
	}
	return &object.String{Value: strconv.FormatFloat(val, 'f', int(prec.Value), 64)}
}
