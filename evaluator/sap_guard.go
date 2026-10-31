package evaluator

// sap_guard.go — 原生整数路径的统一溢出守护（2026-10-25 第20轮）
//
// 背景：五层执行栈的整数边界语义曾经三套并存——
//   · Tree-Walk / Level 1-3：静默 int64 回绕
//   · for Level 5（De Bruijn）：抛 E210
//   · while Level 4/5：静默 Smi 截断
// 实测反例（第19轮）：
//
//	变量 i = 0 / 变量 总 = 0
//	当 i < 2 { 总 = 4611686018427387903; 总 = 总 + 总; i = i + 1 }
//	SAP 输出 -2，--no-sap 输出 9223372036854775806。
//
// 拍板口径（2026-10-25 pyask CHOICE 1）：统一到 E210——
//   · 结果超出 int64 可表示范围 → 抛 E210「整数溢出」
//   · 结果在 int64 内但超出 Smi 编码区间（[-(2^62), 2^62-1]）→ 撤出原生路径，
//     交回该语句的普通求值器重跑（普通路径用 *object.Integer 存 int64，不会截断）
//
// 所有整数运算入口必须走本文件的判定，禁止各路径各写一套。

import (
	"math"

	"zuzo/ast"
	"zuzo/diagnostic"
	"zuzo/object"
)

// guardVerdict 一次整数运算的处置结论
type guardVerdict int

const (
	// guardOK 结果可直接写回原生槽位
	guardOK guardVerdict = iota
	// guardEscape 结果在 int64 内但超 Smi 区间，撤出原生路径重跑
	guardEscape
	// guardOverflow 结果超出 int64，抛 E210
	guardOverflow
)

// addOverflows 判断 a+b 是否溢出 int64
func addOverflows(a, b int64) bool {
	if b > 0 {
		return a > math.MaxInt64-b
	}
	if b < 0 {
		return a < math.MinInt64-b
	}
	return false
}

// subOverflows 判断 a-b 是否溢出 int64
func subOverflows(a, b int64) bool {
	if b == math.MinInt64 {
		return a >= 0
	}
	return addOverflows(a, -b)
}

// mulOverflows 判断 a*b 是否溢出 int64
func mulOverflows(a, b int64) bool {
	if a == 0 || b == 0 {
		return false
	}
	if a == -1 && b == math.MinInt64 {
		return true
	}
	if b == -1 && a == math.MinInt64 {
		return true
	}
	res := a * b
	// 除法回验：溢出时 res/b != a
	if res/b != a {
		return true
	}
	return res == math.MinInt64 && (a != math.MinInt64 || b != 1)
}

// guardIntArith 判定一次二元整数运算（op 取 "+" / "-" / "*"）。
// 返回 (结果, 处置)。guardOverflow 时结果无意义。
func guardIntArith(op string, a, b int64) (int64, guardVerdict) {
	switch op {
	case "+":
		if addOverflows(a, b) {
			return 0, guardOverflow
		}
		return a + b, guardOK
	case "-":
		if subOverflows(a, b) {
			return 0, guardOverflow
		}
		return a - b, guardOK
	case "*":
		if mulOverflows(a, b) {
			return 0, guardOverflow
		}
		return a * b, guardOK
	}
	return 0, guardOK
}

// guardNativeOp 判定一条原生循环 op（for/while 的 L3/L4/L5 共用）。
//
// kind 为 nativeOpKind；v1 为第一操作数；v2 为第二操作数（仅 opAdd/opSub/opMul 使用）；
// hasV2 为 false 时用 constVal 作为第二操作数（对应 opAddConst 等）。
// 返回 (结果, 处置)。opAssignVar 直拷源值（源值已在 Smi 区间内），恒为 guardOK。
func guardNativeOp(kind nativeOpKind, v1, v2 int64, hasV2 bool, constVal int64) (int64, guardVerdict) {
	var res int64
	var v guardVerdict

	switch kind {
	case opAdd:
		if hasV2 {
			res, v = guardIntArith("+", v1, v2)
		} else {
			res, v = guardIntArith("+", v1, constVal)
		}
	case opSub:
		if hasV2 {
			res, v = guardIntArith("-", v1, v2)
		} else {
			res, v = guardIntArith("-", v1, constVal)
		}
	case opMul:
		if hasV2 {
			res, v = guardIntArith("*", v1, v2)
		} else {
			res, v = guardIntArith("*", v1, constVal)
		}
	case opAddConst:
		res, v = guardIntArith("+", v1, constVal)
	case opSubConst:
		res, v = guardIntArith("-", v1, constVal)
	case opMulConst:
		res, v = guardIntArith("*", v1, constVal)
	case opAssignConst:
		res, v = constVal, guardOK
	case opAssignVar:
		return v1, guardOK
	default:
		return v1, guardOK
	}

	if v == guardOverflow {
		return 0, guardOverflow
	}
	// int64 内但超 Smi 编码区间：原生 Smi 槽位装不下，撤出原生路径
	if !dbSmiFits(res) {
		return res, guardEscape
	}
	return res, guardOK
}

// smiFitsInt64 供 while/for 的计数器与初值判定复用
func smiFitsInt64(v int64) bool { return dbSmiFits(v) }

// ============================================================
// 热路径专用：三个刻意写小的检查函数（第21轮性能回收）
// ============================================================
//
// 背景：guardNativeOp 含 8 分支 switch，Go 编译器不会内联，
// 1e6 循环里每次迭代多一次函数调用，实测净耗时 3.6ms → 7.2ms。
// 下面三个函数体皆 ≤ 6 条语句，能在调用点内联，把开关折叠进分支预测。

// addChecked 计算 a+b 并给出处置
//
//go:inline-hint 保持短小（Go 编译器内联预算按节点数计）
func addChecked(a, b int64) (int64, guardVerdict) {
	if b > 0 {
		if a > math.MaxInt64-b {
			return 0, guardOverflow
		}
	} else if b < 0 {
		if a < math.MinInt64-b {
			return 0, guardOverflow
		}
	}
	r := a + b
	if r > dbSmiMax || r < dbSmiMin {
		return r, guardEscape
	}
	return r, guardOK
}

// subChecked 计算 a-b 并给出处置
func subChecked(a, b int64) (int64, guardVerdict) {
	if b == math.MinInt64 {
		if a >= 0 {
			return 0, guardOverflow
		}
		return 0, guardOverflow
	}
	return addChecked(a, -b)
}

// mulChecked 计算 a*b 并给出处置
func mulChecked(a, b int64) (int64, guardVerdict) {
	if a == 0 || b == 0 {
		return 0, guardOK
	}
	r := a * b
	if (a == -1 && b == math.MinInt64) || (b == -1 && a == math.MinInt64) || r/b != a {
		return 0, guardOverflow
	}
	if r > dbSmiMax || r < dbSmiMin {
		return r, guardEscape
	}
	return r, guardOK
}

// setIntSlot 计算二元整数运算并写入 Object 槽位（Level 3 专用）。
// Level 3 用 *object.Integer 存 int64，不存在 Smi 截断问题，只关心 int64 溢出。
// 返回非 nil 表示已产生错误对象，调用方立到 return。
func setIntSlot(n ast.Node, env *object.Environment, slot int, op string, a, b int64) object.Object {
	res, v := guardIntArith(op, a, b)
	if v == guardOverflow {
		return newErrorWithNode(n, diagnostic.ErrIntegerOverflow.Code,
			"循环体整数运算溢出：结果超出 int64 表示范围")
	}
	env.SetSlot(slot, &object.Integer{Value: res})
	return nil
}

// updateIntSlot 推进循环计数器并写回 Object 槽位（Level 3 专用）。
// 计数器是条件的唯一输入，溢出必须显式报错，否则死循环。
func updateIntSlot(n ast.Node, env *object.Environment, slot int, updateOp string, cur, step int64) (int64, object.Object) {
	switch updateOp {
	case "+":
		if addOverflows(cur, step) {
			return cur, newErrorWithNode(n, diagnostic.ErrIntegerOverflow.Code,
				"循环计数器溢出：结果超出 int64 表示范围")
		}
		cur += step
	case "-":
		if subOverflows(cur, step) {
			return cur, newErrorWithNode(n, diagnostic.ErrIntegerOverflow.Code,
				"循环计数器溢出：结果超出 int64 表示范围")
		}
		cur -= step
	}
	env.SetSlot(slot, &object.Integer{Value: cur})
	return cur, nil
}
