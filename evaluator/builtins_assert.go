package evaluator

// builtins_assert.go — 语言级断言内置（第34轮 w3）
//
//	断言(条件)
//	断言(条件, 消息)
//
// 语义：
//   - 条件按 evaluator.isTruthy 统一口径判定（空/假/0/0.0/""/空数组/空哈希 视为假），
//     保证与 当/如果 的真值判断完全一致，不另立标准。
//   - 真：静默返回 NULL（evaluator 包的空值单例），不打印、不污染表达式值。
//   - 假：返回错误对象，文本「断言失败」或「断言失败: <消息>」；main.handleResult
//     见到 ERROR_OBJ 后走 stderr + os.Exit(1)，故断言失败天然是脚本级硬失败。
//   - 参数个数错误：带错误码 E900（桥/运行期错误段），文案与同类内置保持一致。
//
// 与 BDD 系列（断言相等/断言为真/…）的区别：本内置不累计测试计数、不依赖 测试套件
// 上下文，纯语言级守卫，可在任意脚本、任意作用域直接调用。

import (
	"zuzo/object"
)

// 断言(条件, 消息?) → 空
func builtinAssert(args ...object.Object) object.Object {
	if len(args) < 1 || len(args) > 2 {
		return newErrorWithNode(nil, "E900",
			"断言() 期望 1~2 个参数（条件, 消息?），得到 %d 个", len(args))
	}

	if isTruthy(args[0]) {
		return NULL
	}

	if len(args) == 2 {
		// 消息允许任意可打印值（字符串原样取 Value，其它走 Inspect）。
		msg := args[1].Inspect()
		if s, ok := args[1].(*object.String); ok {
			msg = s.Value
		}
		return newError("断言失败: %s", msg)
	}

	return newError("断言失败")
}
