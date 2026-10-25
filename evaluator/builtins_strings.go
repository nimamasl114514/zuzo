package evaluator

// builtins_strings.go — 字符串内置（第18轮重构后）
//
// 重构说明：
//   原先本文件承载 14 个扁平字符串函数，每个都有 8-15 行「参数个数校验 + 类型断言」
//   重复样板（共 42 处），且能力面残缺（无子串/无左右裁剪/无填充对齐/无判定族）。
//
//   第18轮起，全部字符串能力收敛到 `strlib_*.go`（统一参数校验助手 + 分组实现），
//   并对外提供内建命名空间 `文本`（约 95 个成员）。本文件只保留：
//     1. `格式化()` — sprintf 风格，双模式语义特殊，不适合纳入方法表；
//     2. 旧全局名 → 新实现的别名说明（实际别名在 evaluator.go 的 builtins 表里）。
//
// 旧全局名零破坏：
//   大写 / 小写 / 前缀 / 后缀 / 查找 / 替换 / 分割 / 去空白 / 裁剪 / 重复 / 计数 / 分词 / 取字符
//   全部指向 strlib 实现，行为与错误消息保持兼容（单参数场景沿用旧文案）。

import (
	"fmt"
	"strconv"

	"zuzo/object"
)

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
