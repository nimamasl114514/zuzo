package evaluator

import (
	"math"
	"math/rand"
	"strconv"
	"time"

	"zuzo/object"
)

// ==================== 数学内置函数（封装 Go math/rand） ====================

// 随机初始化（仅执行一次）
var randomReady bool

func ensureRandomInit() {
	if !randomReady {
		rand.Seed(time.Now().UnixNano())
		randomReady = true
	}
}

// 随机() → 浮点数 — 返回 [0.0, 1.0) 随机浮点数
func builtinRandom(args ...object.Object) object.Object {
	if len(args) != 0 {
		return newError("随机() 期望 0 个参数，得到 %d 个", len(args))
	}
	ensureRandomInit()
	return &object.Float{Value: rand.Float64()}
}

// 随机整数(最小, 最大) → 整数 — 返回 [min, max] 闭区间随机整数
func builtinRandomInt(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("随机整数() 期望 2 个参数，得到 %d 个", len(args))
	}
	min, ok := args[0].(*object.Integer)
	if !ok {
		return newError("随机整数() 第一个参数必须是整数（最小值），得到 %s", args[0].Type())
	}
	max, ok := args[1].(*object.Integer)
	if !ok {
		return newError("随机整数() 第二个参数必须是整数（最大值），得到 %s", args[1].Type())
	}
	if min.Value > max.Value {
		return newError("随机整数() 最小值 %d 大于最大值 %d", min.Value, max.Value)
	}
	ensureRandomInit()
	range_ := max.Value - min.Value + 1
	return &object.Integer{Value: min.Value + rand.Int63n(range_)}
}

// 绝对值(n) → 整数|浮点数 — 返回绝对值
func builtinAbs(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("绝对值() 期望 1 个参数，得到 %d 个", len(args))
	}
	switch n := args[0].(type) {
	case *object.Integer:
		if n.Value < 0 {
			return &object.Integer{Value: -n.Value}
		}
		return n
	case *object.Float:
		return &object.Float{Value: math.Abs(n.Value)}
	default:
		return newError("绝对值() 参数必须是数值，得到 %s", args[0].Type())
	}
}

// 最大值(...) → 整数|浮点数 — 返回多个值中的最大值
func builtinMax(args ...object.Object) object.Object {
	if len(args) < 1 {
		return newError("最大值() 至少需要 1 个参数")
	}
	// 判断类型
	if _, ok := args[0].(*object.Integer); ok {
		result := args[0].(*object.Integer).Value
		for _, arg := range args[1:] {
			n, ok := arg.(*object.Integer)
			if !ok {
				return newError("最大值() 参数类型不一致，得到 %s", arg.Type())
			}
			if n.Value > result {
				result = n.Value
			}
		}
		return &object.Integer{Value: result}
	}
	if _, ok := args[0].(*object.Float); ok {
		result := args[0].(*object.Float).Value
		for _, arg := range args[1:] {
			n, ok := arg.(*object.Float)
			if !ok {
				return newError("最大值() 参数类型不一致，得到 %s", arg.Type())
			}
			if n.Value > result {
				result = n.Value
			}
		}
		return &object.Float{Value: result}
	}
	return newError("最大值() 参数必须是数值，得到 %s", args[0].Type())
}

// 最小值(...) → 整数|浮点数 — 返回多个值中的最小值
func builtinMin(args ...object.Object) object.Object {
	if len(args) < 1 {
		return newError("最小值() 至少需要 1 个参数")
	}
	if _, ok := args[0].(*object.Integer); ok {
		result := args[0].(*object.Integer).Value
		for _, arg := range args[1:] {
			n, ok := arg.(*object.Integer)
			if !ok {
				return newError("最小值() 参数类型不一致，得到 %s", arg.Type())
			}
			if n.Value < result {
				result = n.Value
			}
		}
		return &object.Integer{Value: result}
	}
	if _, ok := args[0].(*object.Float); ok {
		result := args[0].(*object.Float).Value
		for _, arg := range args[1:] {
			n, ok := arg.(*object.Float)
			if !ok {
				return newError("最小值() 参数类型不一致，得到 %s", arg.Type())
			}
			if n.Value < result {
				result = n.Value
			}
		}
		return &object.Float{Value: result}
	}
	return newError("最小值() 参数必须是数值，得到 %s", args[0].Type())
}

// 向上取整(n) → 整数 — math.Ceil
func builtinCeil(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("向上取整() 期望 1 个参数，得到 %d 个", len(args))
	}
	switch n := args[0].(type) {
	case *object.Float:
		return &object.Integer{Value: int64(math.Ceil(n.Value))}
	case *object.Integer:
		return n
	default:
		return newError("向上取整() 参数必须是数值，得到 %s", args[0].Type())
	}
}

// 向下取整(n) → 整数 — math.Floor
func builtinFloor(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("向下取整() 期望 1 个参数，得到 %d 个", len(args))
	}
	switch n := args[0].(type) {
	case *object.Float:
		return &object.Integer{Value: int64(math.Floor(n.Value))}
	case *object.Integer:
		return n
	default:
		return newError("向下取整() 参数必须是数值，得到 %s", args[0].Type())
	}
}

// 四舍五入(n) → 整数 — math.Round
func builtinRound(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("四舍五入() 期望 1 个参数，得到 %d 个", len(args))
	}
	switch n := args[0].(type) {
	case *object.Float:
		return &object.Integer{Value: int64(math.Round(n.Value))}
	case *object.Integer:
		return n
	default:
		return newError("四舍五入() 参数必须是数值，得到 %s", args[0].Type())
	}
}

// 幂(base, exp) → 浮点数 — math.Pow
func builtinPow(args ...object.Object) object.Object {
	if len(args) != 2 {
		return newError("幂() 期望 2 个参数，得到 %d 个", len(args))
	}
	base := toFloatPtr(args[0])
	if base == nil {
		return newError("幂() 第一个参数必须是数值，得到 %s", args[0].Type())
	}
	exp := toFloatPtr(args[1])
	if exp == nil {
		return newError("幂() 第二个参数必须是数值，得到 %s", args[1].Type())
	}
	return &object.Float{Value: math.Pow(*base, *exp)}
}

// 平方根(n) → 浮点数 — math.Sqrt
func builtinSqrt(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("平方根() 期望 1 个参数，得到 %d 个", len(args))
	}
	val := toFloatPtr(args[0])
	if val == nil {
		return newError("平方根() 参数必须是数值，得到 %s", args[0].Type())
	}
	if *val < 0 {
		return newError("平方根() 参数不能为负数: %s", strconv.FormatFloat(*val, 'f', -1, 64))
	}
	return &object.Float{Value: math.Sqrt(*val)}
}

// 正弦(n) → 浮点数 — math.Sin（弧度）
func builtinSin(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("正弦() 期望 1 个参数，得到 %d 个", len(args))
	}
	val := toFloatPtr(args[0])
	if val == nil {
		return newError("正弦() 参数必须是数值，得到 %s", args[0].Type())
	}
	return &object.Float{Value: math.Sin(*val)}
}

// 余弦(n) → 浮点数 — math.Cos（弧度）
func builtinCos(args ...object.Object) object.Object {
	if len(args) != 1 {
		return newError("余弦() 期望 1 个参数，得到 %d 个", len(args))
	}
	val := toFloatPtr(args[0])
	if val == nil {
		return newError("余弦() 参数必须是数值，得到 %s", args[0].Type())
	}
	return &object.Float{Value: math.Cos(*val)}
}

// 对数(n, base) → 浮点数 — math.Log
// 对数(n) → 浮点数 — 自然对数 ln(n)
func builtinLog(args ...object.Object) object.Object {
	if len(args) < 1 || len(args) > 2 {
		return newError("对数() 期望 1 或 2 个参数，得到 %d 个", len(args))
	}
	val := toFloatPtr(args[0])
	if val == nil {
		return newError("对数() 第一个参数必须是数值，得到 %s", args[0].Type())
	}
	if *val <= 0 {
		return newError("对数() 参数必须为正数")
	}
	if len(args) == 1 {
		return &object.Float{Value: math.Log(*val)}
	}
	base := toFloatPtr(args[1])
	if base == nil {
		return newError("对数() 第二个参数必须是数值（底数），得到 %s", args[1].Type())
	}
	if *base <= 0 || *base == 1 {
		return newError("对数() 底数必须为正数且不等于 1")
	}
	return &object.Float{Value: math.Log(*val) / math.Log(*base)}
}

// toFloatPtr 辅助函数：将 zuzo 数值转为 *float64（nil 表示非数值）
func toFloatPtr(obj object.Object) *float64 {
	switch n := obj.(type) {
	case *object.Integer:
		v := float64(n.Value)
		return &v
	case *object.Float:
		v := n.Value
		return &v
	default:
		return nil
	}
}
