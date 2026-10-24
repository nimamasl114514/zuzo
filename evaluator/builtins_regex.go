package evaluator

import (
	"regexp"
	"strings"

	"zuzo/object"
)

// ==================== 正则表达式内置函数（封装 Go regexp） ====================

// 正则匹配(s, pattern) → 数组|空 — 返回第一个匹配的全部捕获组；无匹配返回空
// 数组第一个元素是完整匹配，后续是各捕获组
func builtinRegexMatch(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("正则匹配() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("正则匹配() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	pattern, ok := args[1].(*object.String)
	if !ok {
		return newError("正则匹配() 第二个参数必须是字符串（正则），得到 %s", args[1].Type())
	}

	re, err := regexp.Compile(pattern.Value)
	if err != nil {
		return newError("正则匹配() 正则编译失败: %s", err.Error())
	}

	matches := re.FindStringSubmatch(s.Value)
	if matches == nil {
		return NULL
	}

	elems := make([]object.Object, len(matches))
	for i, m := range matches {
		elems[i] = &object.String{Value: m}
	}
	return &object.Array{Elements: elems}
}

// 正则查找全部(s, pattern) → 数组 — 返回所有匹配（每个匹配是一个数组）
func builtinRegexFindAll(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("正则查找全部() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("正则查找全部() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	pattern, ok := args[1].(*object.String)
	if !ok {
		return newError("正则查找全部() 第二个参数必须是字符串（正则），得到 %s", args[1].Type())
	}

	re, err := regexp.Compile(pattern.Value)
	if err != nil {
		return newError("正则查找全部() 正则编译失败: %s", err.Error())
	}

	allMatches := re.FindAllStringSubmatch(s.Value, -1)
	if allMatches == nil {
		return &object.Array{Elements: []object.Object{}}
	}

	elems := make([]object.Object, len(allMatches))
	for i, group := range allMatches {
		subElems := make([]object.Object, len(group))
		for j, m := range group {
			subElems[j] = &object.String{Value: m}
		}
		elems[i] = &object.Array{Elements: subElems}
	}
	return &object.Array{Elements: elems}
}

// 正则替换(s, pattern, replacement) → 字符串 — 替换所有匹配
func builtinRegexReplace(args ...object.Object) object.Object {
	if len(args) != 3 {
		return newError("正则替换() 期望 3 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("正则替换() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	pattern, ok := args[1].(*object.String)
	if !ok {
		return newError("正则替换() 第二个参数必须是字符串（正则），得到 %s", args[1].Type())
	}
	replacement, ok := args[2].(*object.String)
	if !ok {
		return newError("正则替换() 第三个参数必须是字符串（替换文本），得到 %s", args[2].Type())
	}

	re, err := regexp.Compile(pattern.Value)
	if err != nil {
		return newError("正则替换() 正则编译失败: %s", err.Error())
	}

	return &object.String{Value: re.ReplaceAllString(s.Value, replacement.Value)}
}

// 正则分割(s, pattern) → 数组 — 按正则分割字符串
func builtinRegexSplit(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("正则分割() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("正则分割() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	pattern, ok := args[1].(*object.String)
	if !ok {
		return newError("正则分割() 第二个参数必须是字符串（正则），得到 %s", args[1].Type())
	}

	re, err := regexp.Compile(pattern.Value)
	if err != nil {
		return newError("正则分割() 正则编译失败: %s", err.Error())
	}

	parts := re.Split(s.Value, -1)
	elems := make([]object.Object, len(parts))
	for i, p := range parts {
		elems[i] = &object.String{Value: p}
	}
	return &object.Array{Elements: elems}
}

// 正则验证(s, pattern) → 布尔 — 检查字符串是否匹配正则
func builtinRegexTest(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("正则验证() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("正则验证() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	pattern, ok := args[1].(*object.String)
	if !ok {
		return newError("正则验证() 第二个参数必须是字符串（正则），得到 %s", args[1].Type())
	}

	re, err := regexp.Compile(pattern.Value)
	if err != nil {
		return newError("正则验证() 正则编译失败: %s", err.Error())
	}

	return nativeBoolToBooleanObject(re.MatchString(s.Value))
}

// 正则提取(s, pattern) → 字符串 — 返回第一个匹配的字符串（无捕获组时返回完整匹配）
func builtinRegexExtract(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("正则提取() 期望 2 个参数，得到 %d 个", len(args))
	}
	s, ok := args[0].(*object.String)
	if !ok {
		return newError("正则提取() 第一个参数必须是字符串，得到 %s", args[0].Type())
	}
	pattern, ok := args[1].(*object.String)
	if !ok {
		return newError("正则提取() 第二个参数必须是字符串（正则），得到 %s", args[1].Type())
	}

	re, err := regexp.Compile(pattern.Value)
	if err != nil {
		return newError("正则提取() 正则编译失败: %s", err.Error())
	}

	match := re.FindString(s.Value)
	if match == "" && !strings.Contains(s.Value, "") {
		return NULL
	}
	if match == "" {
		return NULL
	}
	return &object.String{Value: match}
}
