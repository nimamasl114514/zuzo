package evaluator

// modern.go — 现代语言特性求值
//
// 包含：
//   - 定义/实例 数据建模
//   - 否则 fallback 运算符
//   - 执行/遇到 错误捕获
//   - 对于 x 在 ... [到 ...] [满足 ...] 现代迭代器
//   - 跳过/跳出 循环控制

import (
	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/object"
)

// ==================== 数据建模 ====================

// evalDefineStatement 求值定义语句：创建蓝图并存入环境
func evalDefineStatement(node *ast.DefineStatement, env *object.Environment) object.Object {
	fields := make([]object.DefineField, len(node.Fields))
	for i, f := range node.Fields {
		val := Eval(f.Value, env)
		if isError(val) {
			return val
		}
		fields[i] = object.DefineField{Name: f.Name, DefaultVal: val}
	}
	def := &object.Define{Name: node.Name, Fields: fields}
	env.Set(node.Name, def)
	return NULL
}

// applyDefine 调用定义蓝图创建实例
// 严格校验：未知字段会报错，防止拼写错误导致的静默 bug
func applyDefine(def *object.Define, namedArgs map[string]object.Object) object.Object {
	props := make(map[string]object.Object)

	// 建立已定义字段集合
	definedFields := make(map[string]bool, len(def.Fields))
	for _, f := range def.Fields {
		props[f.Name] = f.DefaultVal
		definedFields[f.Name] = true
	}

	// 覆盖具名参数，校验未知字段
	for name, val := range namedArgs {
		if !definedFields[name] {
			return newError("定义 %s 中不存在字段: %s", def.Name, name)
		}
		props[name] = val
	}
	return &object.Instance{DefName: def.Name, Props: props}
}

// evalInstanceIndexExpression 实例属性访问
func evalInstanceIndexExpression(node ast.Node, inst, index object.Object) object.Object {
	i := inst.(*object.Instance)
	key, ok := index.(*object.String)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrInvalidIndexType.Code, "实例属性访问必须是字符串")
	}
	val, ok := i.Props[key.Value]
	if !ok {
		return NULL
	}
	return val
}

// ==================== 错误处理 ====================

// evalTryCatchStatement 求值执行/遇到语句
func evalTryCatchStatement(node *ast.TryCatchStatement, env *object.Environment) object.Object {
	result := Eval(node.Body, env)
	if isError(result) {
		// 捕获错误对象本身（保留 Code/Line/StackTrace 结构化信息）
		// 需要人类可读文本时用 字符串(e)；需要类型判断时用 类型(e)
		handlerEnv := object.NewEnclosedEnvironment(env)
		handlerEnv.Set(node.ErrVar, result)
		return Eval(node.Handler, handlerEnv)
	}
	return result
}

// evalElseFallback 求值否则 fallback 运算符: expr 否则 fallback
// 如果左值为错误或空，返回右值；否则返回左值
// 语义说明：假、0、"" 等合法值不会触发 fallback（不同于 JS 的 || 或 Python 的 or）
func evalElseFallback(node *ast.InfixExpression, env *object.Environment) object.Object {
	left := Eval(node.Left, env)
	if isError(left) {
		return Eval(node.Right, env)
	}
	if left.Type() == object.NULL_OBJ {
		return Eval(node.Right, env)
	}
	return left
}

// ==================== 现代迭代器 ====================

// handleLoopControl 处理循环体内的控制流信号（break/continue/return/error）。
// 返回值：(是否应该跳出循环, 是否应该跳过本次循环, 需要向上冒泡的对象)
func handleLoopControl(result object.Object) (shouldBreak bool, shouldContinue bool, bubbleUp object.Object) {
	if result == nil {
		return false, false, nil
	}
	switch result.Type() {
	case object.BREAK_SIG:
		return true, false, nil
	case object.CONTINUE_SIG:
		return false, true, nil
	case object.RETURN_VALUE, object.ERROR_OBJ:
		return false, false, result
	}
	return false, false, nil
}

// evalForInStatement 求值 for-in 遍历循环
func evalForInStatement(node *ast.ForInStatement, env *object.Environment) object.Object {
	// 范围循环: 对于 i 在 start 到 end { ... }
	if node.EndExpr != nil {
		return evalForInRange(node, env)
	}

	// 集合遍历: 对于 x 在 collection { ... }
	iterable := Eval(node.Iterable, env)
	if isError(iterable) {
		return iterable
	}

	switch coll := iterable.(type) {
	case *object.Array:
		return evalForInArray(node, env, coll)
	case *object.Hash:
		return evalForInHash(node, env, coll)
	case *object.String:
		return evalForInString(node, env, coll)
	default:
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code,
			"不可迭代的类型: %s", iterable.Type())
	}
}

// evalForInRange 范围循环: 对于 i 在 start 到 end [满足 ...] { ... }
// 自动判断方向：start <= end 时递增，start > end 时递减
func evalForInRange(node *ast.ForInStatement, env *object.Environment) object.Object {
	startVal := Eval(node.Iterable, env)
	if isError(startVal) {
		return startVal
	}
	endVal := Eval(node.EndExpr, env)
	if isError(endVal) {
		return endVal
	}

	start, ok := toInt64(startVal)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code,
			"范围起始值必须是整数，得到 %s", startVal.Type())
	}
	end, ok := toInt64(endVal)
	if !ok {
		return newErrorWithNode(node, diagnostic.ErrUnsupportedOperator.Code,
			"范围结束值必须是整数，得到 %s", endVal.Type())
	}

	// 自动判断遍历方向
	if start <= end {
		for i := start; i <= end; i++ {
			if b, _, bubble := evalLoopBody(node, env, &object.Integer{Value: i}, nil); b || bubble != nil {
				return orNULL(bubble)
			}
		}
	} else {
		for i := start; i >= end; i-- {
			if b, _, bubble := evalLoopBody(node, env, &object.Integer{Value: i}, nil); b || bubble != nil {
				return orNULL(bubble)
			}
		}
	}
	return NULL
}

// evalForInArray 数组遍历: 对于 [i, ] x 在 数组 [满足 ...] { ... }
func evalForInArray(node *ast.ForInStatement, env *object.Environment, arr *object.Array) object.Object {
	for idx, elem := range arr.Elements {
		var indexObj object.Object
		if node.IndexName != "" {
			indexObj = &object.Integer{Value: int64(idx)}
		}
		if b, c, bubble := evalLoopBody(node, env, elem, indexObj); b {
			break
		} else if c {
			continue
		} else if bubble != nil {
			return bubble
		}
	}
	return NULL
}

// evalForInHash 哈希遍历: 对于 [k, ] v 在 哈希 [满足 ...] { ... }
// 按插入顺序遍历（通过 Keys 切片），保证确定性
func evalForInHash(node *ast.ForInStatement, env *object.Environment, hash *object.Hash) object.Object {
	for _, hk := range hash.Keys {
		pair := hash.Pairs[hk]
		var indexObj object.Object
		if node.IndexName != "" {
			indexObj = pair.Key
		}
		if b, c, bubble := evalLoopBody(node, env, pair.Value, indexObj); b {
			break
		} else if c {
			continue
		} else if bubble != nil {
			return bubble
		}
	}
	return NULL
}

// evalForInString 字符串遍历: 对于 [i, ] char 在 字符串 [满足 ...] { ... }
// 按 rune（Unicode 码点）拆分遍历，正确处理多字节字符
func evalForInString(node *ast.ForInStatement, env *object.Environment, str *object.String) object.Object {
	runes := []rune(str.Value)
	for idx, r := range runes {
		charObj := &object.String{Value: string(r)}
		var indexObj object.Object
		if node.IndexName != "" {
			indexObj = &object.Integer{Value: int64(idx)}
		}
		if b, c, bubble := evalLoopBody(node, env, charObj, indexObj); b {
			break
		} else if c {
			continue
		} else if bubble != nil {
			return bubble
		}
	}
	return NULL
}

// evalLoopBody 执行单次循环体的公共逻辑：
// 设置循环变量 → 条件过滤 → 执行循环体 → 处理控制流信号
// 返回值通过 handleLoopControl 语义：(shouldBreak, shouldContinue, bubbleUp)
func evalLoopBody(node *ast.ForInStatement, env *object.Environment, valObj object.Object, indexObj object.Object) (bool, bool, object.Object) {
	loopEnv := object.NewEnclosedEnvironment(env)
	loopEnv.Set(node.VarName, valObj)
	if node.IndexName != "" && indexObj != nil {
		loopEnv.Set(node.IndexName, indexObj)
	}

	// 条件过滤
	if node.Guard != nil {
		guardResult := Eval(node.Guard, loopEnv)
		if isError(guardResult) {
			return false, false, guardResult
		}
		if !isTruthy(guardResult) {
			return false, true, nil // continue
		}
	}

	result := Eval(node.Body, loopEnv)
	return handleLoopControl(result)
}

// orNULL 如果 obj 非 nil 则返回 obj，否则返回 NULL
func orNULL(obj object.Object) object.Object {
	if obj != nil {
		return obj
	}
	return NULL
}

// toInt64 将对象转换为 int64
func toInt64(obj object.Object) (int64, bool) {
	if i, ok := obj.(*object.Integer); ok {
		return i.Value, true
	}
	return 0, false
}
