package evaluator

// strlib_core.go — 字符串标准库核心：命名空间 `文本` + 统一参数校验助手
//
// 设计原则（L20 架构与抽象）：
//   1. 每个字符串方法只做三件事：校验参数 → 计算 → 装箱字符串/整数/数组。
//   2. 参数校验一律走 argc / argStr / argInt / argStrs / argHash 五个助手，
//      消除旧 builtins_strings.go 里 42 处重复样板。
//   3. 各能力组分散在 strlib_<组>.go，通过 init() 调 registerStringMethods 挂进方法表；
//      新增一个字符串能力 = 加一个 func + 注册一行，核心零改动（开闭原则）。
//   4. `文本` 模块是单例，首次 registerBuiltins 时构造一次；旧的扁平全局函数
//      （大写/小写/…）在 builtins_strings.go 里以同一 BuiltinFunction 别名保留，零破坏。
//   5. 命名空间不叫 `字符串` 的原因：`字符串` 已是全局类型转换内置（字符串(42) → "42"），
//      同名会破坏兼容；`文本` 与现有 API 零冲突。

import (
	"sync"

	"zuzo/object"
)

// ==================== 参数校验助手 ====================

// argc 校验参数个数：要求 min <= len(args) <= max（max < 0 表示不限上限）。
func argc(fn string, args []object.Object, min, max int) *object.Error {
	n := len(args)
	if n >= min && (max < 0 || n <= max) {
		return nil
	}
	switch {
	case min == max:
		return newError("%s() 期望 %d 个参数，得到 %d 个", fn, min, n)
	case max < 0:
		return newError("%s() 期望至少 %d 个参数，得到 %d 个", fn, min, n)
	default:
		return newError("%s() 期望 %d-%d 个参数，得到 %d 个", fn, min, max, n)
	}
}

// typeErr 统一生成参数类型错误；what 为空时沿用旧格式（"X() 参数必须是字符串"）。
func typeErr(fn string, args []object.Object, i int, want, what string) *object.Error {
	got := "空"
	if i < len(args) && args[i] != nil {
		got = string(args[i].Type())
	}
	if what == "" {
		return newError("%s() 参数必须是%s，得到 %s", fn, want, got)
	}
	return newError("%s() 第 %d 个参数（%s）必须是%s，得到 %s", fn, i+1, what, want, got)
}

// argStr 取第 i 个字符串参数。
func argStr(fn string, args []object.Object, i int, what string) (string, *object.Error) {
	if i >= len(args) {
		return "", newError("%s() 缺少第 %d 个参数", fn, i+1)
	}
	s, ok := args[i].(*object.String)
	if !ok {
		return "", typeErr(fn, args, i, "字符串", what)
	}
	return s.Value, nil
}

// optStr 取第 i 个可选字符串参数（缺省返回 def）。
func optStr(fn string, args []object.Object, i int, what, def string) (string, *object.Error) {
	if i >= len(args) {
		return def, nil
	}
	return argStr(fn, args, i, what)
}

// argInt 取第 i 个整数参数。
func argInt(fn string, args []object.Object, i int, what string) (int64, *object.Error) {
	if i >= len(args) {
		return 0, newError("%s() 缺少第 %d 个参数", fn, i+1)
	}
	n, ok := args[i].(*object.Integer)
	if !ok {
		return 0, typeErr(fn, args, i, "整数", what)
	}
	return n.Value, nil
}

// argStrs 取第 i 个「字符串数组」参数，逐项校验元素类型。
func argStrs(fn string, args []object.Object, i int, what string) ([]string, *object.Error) {
	if i >= len(args) {
		return nil, newError("%s() 缺少第 %d 个参数", fn, i+1)
	}
	arr, ok := args[i].(*object.Array)
	if !ok {
		return nil, typeErr(fn, args, i, "字符串数组", what)
	}
	out := make([]string, 0, len(arr.Elements))
	for j, el := range arr.Elements {
		s, ok := el.(*object.String)
		if !ok {
			return nil, newError("%s() 第 %d 个参数（%s）的第 %d 项必须是字符串，得到 %s",
				fn, i+1, what, j+1, el.Type())
		}
		out = append(out, s.Value)
	}
	return out, nil
}

// argHash 取第 i 个哈希参数。
func argHash(fn string, args []object.Object, i int, what string) (*object.Hash, *object.Error) {
	if i >= len(args) {
		return nil, newError("%s() 缺少第 %d 个参数", fn, i+1)
	}
	h, ok := args[i].(*object.Hash)
	if !ok {
		return nil, typeErr(fn, args, i, "哈希", what)
	}
	return h, nil
}

// 常用返回值构造（减少重复 noise）
func strObj(s string) *object.String { return &object.String{Value: s} }
func intObj(n int64) *object.Integer { return &object.Integer{Value: n} }
func boolObj(b bool) object.Object   { return nativeBoolToBooleanObject(b) }

// strArray 把 []string 装箱为 zuzo 数组。
func strArray(ss []string) *object.Array {
	elems := make([]object.Object, len(ss))
	for i, s := range ss {
		elems[i] = &object.String{Value: s}
	}
	return &object.Array{Elements: elems}
}

// ==================== 命名空间方法表与模块单例 ====================

var (
	stringLibMethods = map[string]object.BuiltinFunction{}
	stringLibOnce    sync.Once
	stringLibModule  *object.Module
)

// registerStringMethods 由各 strlib_<组>.go 的 init() 调用，把一组方法挂进方法表。
func registerStringMethods(m map[string]object.BuiltinFunction) {
	for name, fn := range m {
		stringLibMethods[name] = fn
	}
}

// newStringLibModule 构造 `文本` 模块（单例）。
func newStringLibModule() *object.Module {
	stringLibOnce.Do(func() {
		env := object.NewEnvironment()
		for name, fn := range stringLibMethods {
			env.Set(name, &object.Builtin{Fn: fn, Name: "文本." + name})
		}
		stringLibModule = &object.Module{Name: "文本", Env: env}
	})
	return stringLibModule
}

// registerStringLib 把 `文本` 命名空间注入环境；在 registerBuiltins 末尾调用。
func registerStringLib(env *object.Environment) {
	env.Set("文本", newStringLibModule())
}

// StringLibMethodCount 返回命名空间成员数（供测试与文档自检使用）。
func StringLibMethodCount() int {
	return len(stringLibMethods)
}
