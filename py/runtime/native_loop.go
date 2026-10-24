package runtime

// native_loop.go — Python 整数计数循环的原生执行（对齐 zuzo SAP 的 Level 3/4 思路）
//
// 背景（第6轮实测）：Python 块 1e6 累加 164.5 ms，热点是 evalExpr 分发 + map 变量查找
// + 每轮堆分配 Int 对象；而 zuzo 侧同形态 while 走 De Bruijn 原生循环只要 22.3 ms。
// 本文件把同一思路移植到 Python 前端：识别「整数计数循环」窄模式，循环内全部用
// 局部 int64 运算，退出时一次性写回 Env（含对象池化的少量分配）。
//
// 匹配模式（不匹配立即返回 false，走原解释器，语义零变化）：
//
//	i = 0
//	total = 0
//	while i < 1000000:      # 条件：Name OP 整数字面量，无 else
//	    total += i          # body：整数赋值/增强赋值
//	    i = i + 1           # 最后一条：循环变量自增/自减常量
//
// 安全边界：
//   - 循环体内只能出现整数四则（+ - *）与常量/变量赋值；出现函数调用、容器、浮点、
//     break/continue、嵌套循环、global/nonlocal 声明 → 不匹配
//   - 变量初值必须已存在且是 int；缺失或是其他类型 → 不匹配
//   - 预计迭代次数 < 1000 → 不匹配（不值得付编译成本）

import (
	"strconv"

	"zuzo/py/ast"
	"zuzo/py/object"
)

// pyNativeOpKind 原生操作类型
type pyNativeOpKind int

const (
	pyOpAdd      pyNativeOpKind = iota // d = a + b
	pyOpSub                            // d = a - b
	pyOpMul                            // d = a * b
	pyOpSetConst                       // d = const
	pyOpSetVar                         // d = a
)

// pyNativeOp 一条原生操作；a/b 为变量索引，<0 表示该侧用常量 c
type pyNativeOp struct {
	kind pyNativeOpKind
	dst  int
	a    int
	b    int
	c    int64
}

// pyNativeLoop 一个可原生执行的循环
type pyNativeLoop struct {
	names    []string
	ops      []pyNativeOp
	loopIdx  int
	condOp   string
	bound    int64
	updateOp string
	step     int64
}

// maxNativeVars 原生循环支持的变量数上限（超过就放弃，避免线性扫描成本）
const maxNativeVars = 12

// minNativeIterations 估算迭代次数低于该值时不走原生路径
const minNativeIterations = 1000

// tryNativeWhile 尝试原生执行 while 循环；不匹配返回 (nil, false)
func (ip *Interp) tryNativeWhile(node *ast.While, env *object.Env) (object.Object, bool) {
	loop, ok := matchNativeWhile(node)
	if !ok {
		return nil, false
	}

	// 读取初值（必须已存在且为 int）
	vals := make([]int64, len(loop.names))
	for i, name := range loop.names {
		v, found := env.Get(name)
		if !found {
			return nil, false
		}
		iv, ok := v.(*object.Int)
		if !ok || !iv.IsSmall() {
			return nil, false
		}
		vals[i] = iv.V
	}

	iVal := vals[loop.loopIdx]

	// 估算迭代次数，太小则放弃（编译与写回成本不划算）
	if estimate, ok := estimateIterations(loop.condOp, iVal, loop.bound, loop.updateOp, loop.step); !ok || estimate < minNativeIterations {
		return nil, false
	}

	ops := loop.ops
	for {
		switch loop.condOp {
		case "<":
			if !(iVal < loop.bound) {
				goto done
			}
		case "<=":
			if !(iVal <= loop.bound) {
				goto done
			}
		case ">":
			if !(iVal > loop.bound) {
				goto done
			}
		case ">=":
			if !(iVal >= loop.bound) {
				goto done
			}
		}

		for _, op := range ops {
			switch op.kind {
			case pyOpAdd:
				vals[op.dst] = operand(op.a, op.c, vals) + operand(op.b, op.c, vals)
			case pyOpSub:
				vals[op.dst] = operand(op.a, op.c, vals) - operand(op.b, op.c, vals)
			case pyOpMul:
				vals[op.dst] = operand(op.a, op.c, vals) * operand(op.b, op.c, vals)
			case pyOpSetConst:
				vals[op.dst] = op.c
			case pyOpSetVar:
				vals[op.dst] = vals[op.a]
			}
		}

		if loop.updateOp == "+" {
			iVal += loop.step
		} else {
			iVal -= loop.step
		}
		vals[loop.loopIdx] = iVal
	}

done:
	// 写回环境（Int 对象按需新建；小整数走 object 缓存由 NewInt 负责）
	for i, name := range loop.names {
		env.Set(name, object.NewInt(vals[i]))
	}
	return object.None, true
}

// operand 取操作数：idx>=0 用变量值，否则用常量 constVal
// （编译期已保证同一条 op 里最多一侧是常量，所以两侧共用 op.c 不会冲突）
func operand(idx int, constVal int64, vals []int64) int64 {
	if idx < 0 {
		return constVal
	}
	return vals[idx]
}

// estimateIterations 估算循环次数；无法估算时返回 false
func estimateIterations(condOp string, cur, bound int64, updateOp string, step int64) (int64, bool) {
	if step == 0 {
		return 0, false
	}
	var span int64
	switch condOp {
	case "<", "<=":
		if updateOp == "+" {
			span = bound - cur
			if condOp == "<=" {
				span++
			}
		} else {
			return 0, false
		}
	case ">", ">=":
		if updateOp == "-" {
			span = cur - bound
			if condOp == ">=" {
				span++
			}
		} else {
			return 0, false
		}
	default:
		return 0, false
	}
	if span <= 0 {
		return 0, true
	}
	return span / step, true
}

// ==================== 模式匹配 ====================

// matchNativeWhile 从 AST 提取可原生执行的循环描述
func matchNativeWhile(node *ast.While) (*pyNativeLoop, bool) {
	if node == nil || len(node.Orelse) > 0 || len(node.Body) == 0 {
		return nil, false
	}

	// 条件：Compare{Name, [op], [整数]}
	cmp, ok := node.Cond.(*ast.Compare)
	if !ok || len(cmp.Ops) != 1 || len(cmp.Comparators) != 1 {
		return nil, false
	}
	loopName, ok := cmp.Left.(*ast.Name)
	if !ok {
		return nil, false
	}
	switch cmp.Ops[0] {
	case "<", "<=", ">", ">=":
	default:
		return nil, false
	}
	boundNum, ok := cmp.Comparators[0].(*ast.Number)
	if !ok || boundNum.IsFloat {
		return nil, false
	}
	bound, err := strconv.ParseInt(boundNum.Value, 10, 64)
	if err != nil {
		return nil, false
	}

	// 最后一条语句必须是循环变量自增/自减
	update, ok := matchLoopUpdate(node.Body[len(node.Body)-1], loopName.Id)
	if !ok {
		return nil, false
	}

	loop := &pyNativeLoop{
		loopIdx:  -1,
		condOp:   cmp.Ops[0],
		bound:    bound,
		updateOp: update.op,
		step:     update.step,
	}

	index := map[string]int{}
	addVar := func(name string) (int, bool) {
		if idx, exists := index[name]; exists {
			return idx, true
		}
		if len(loop.names) >= maxNativeVars {
			return 0, false
		}
		idx := len(loop.names)
		index[name] = idx
		loop.names = append(loop.names, name)
		return idx, true
	}

	loopIdx, ok := addVar(loopName.Id)
	if !ok {
		return nil, false
	}
	loop.loopIdx = loopIdx

	// body 前 n-1 条：整数赋值
	for _, stmt := range node.Body[:len(node.Body)-1] {
		op, ok := compileNativeAssign(stmt, addVar)
		if !ok {
			return nil, false
		}
		loop.ops = append(loop.ops, op)
	}
	return loop, true
}

type loopUpdate struct {
	op   string
	step int64
}

// matchLoopUpdate 匹配 `i = i + 1` / `i += 1`
func matchLoopUpdate(stmt ast.Stmt, loopName string) (loopUpdate, bool) {
	switch s := stmt.(type) {
	case *ast.Assign:
		if len(s.Targets) != 1 {
			return loopUpdate{}, false
		}
		target, ok := s.Targets[0].(*ast.Name)
		if !ok || target.Id != loopName {
			return loopUpdate{}, false
		}
		bin, ok := s.Value.(*ast.BinOp)
		if !ok {
			return loopUpdate{}, false
		}
		left, ok := bin.Left.(*ast.Name)
		if !ok || left.Id != loopName {
			return loopUpdate{}, false
		}
		num, ok := bin.Right.(*ast.Number)
		if !ok || num.IsFloat {
			return loopUpdate{}, false
		}
		step, err := strconv.ParseInt(num.Value, 10, 64)
		if err != nil {
			return loopUpdate{}, false
		}
		switch bin.Op {
		case "+":
			return loopUpdate{op: "+", step: step}, true
		case "-":
			return loopUpdate{op: "-", step: step}, true
		}
	case *ast.AugAssign:
		target, ok := s.Target.(*ast.Name)
		if !ok || target.Id != loopName {
			return loopUpdate{}, false
		}
		num, ok := s.Value.(*ast.Number)
		if !ok || num.IsFloat {
			return loopUpdate{}, false
		}
		step, err := strconv.ParseInt(num.Value, 10, 64)
		if err != nil {
			return loopUpdate{}, false
		}
		switch s.Op {
		case "+":
			return loopUpdate{op: "+", step: step}, true
		case "-":
			return loopUpdate{op: "-", step: step}, true
		}
	}
	return loopUpdate{}, false
}

// compileNativeAssign 把一条赋值语句编译成原生操作
func compileNativeAssign(stmt ast.Stmt, addVar func(string) (int, bool)) (pyNativeOp, bool) {
	switch s := stmt.(type) {
	case *ast.AugAssign:
		name, ok := s.Target.(*ast.Name)
		if !ok {
			return pyNativeOp{}, false
		}
		dst, ok := addVar(name.Id)
		if !ok {
			return pyNativeOp{}, false
		}
		kind, ok := nativeKindOf(s.Op)
		if !ok {
			return pyNativeOp{}, false
		}
		// d op= v  →  d = d op v
		if varIdx, constVal, isConst, ok := operandOf(s.Value, addVar); ok {
			op := pyNativeOp{kind: kind, dst: dst, a: dst}
			if isConst {
				op.b = -1
				op.c = constVal
			} else {
				op.b = varIdx
			}
			return op, true
		}
		return pyNativeOp{}, false

	case *ast.Assign:
		if len(s.Targets) != 1 {
			return pyNativeOp{}, false
		}
		name, ok := s.Targets[0].(*ast.Name)
		if !ok {
			return pyNativeOp{}, false
		}
		dst, ok := addVar(name.Id)
		if !ok {
			return pyNativeOp{}, false
		}

		// 常量赋值
		if num, ok := s.Value.(*ast.Number); ok && !num.IsFloat {
			if v, err := strconv.ParseInt(num.Value, 10, 64); err == nil {
				return pyNativeOp{kind: pyOpSetConst, dst: dst, c: v}, true
			}
			return pyNativeOp{}, false
		}
		// 变量赋值
		if src, ok := s.Value.(*ast.Name); ok {
			srcIdx, ok := addVar(src.Id)
			if !ok {
				return pyNativeOp{}, false
			}
			return pyNativeOp{kind: pyOpSetVar, dst: dst, a: srcIdx}, true
		}
		// 二元运算
		if bin, ok := s.Value.(*ast.BinOp); ok {
			kind, ok := nativeKindOf(bin.Op)
			if !ok {
				return pyNativeOp{}, false
			}
			leftIdx, leftConst, leftIsConst, ok := operandOf(bin.Left, addVar)
			if !ok {
				return pyNativeOp{}, false
			}
			rightIdx, rightConst, rightIsConst, ok := operandOf(bin.Right, addVar)
			if !ok {
				return pyNativeOp{}, false
			}
			// 两侧不能同时是常量（编译期可直接折叠，但此处直接放弃）
			if leftIsConst && rightIsConst {
				return pyNativeOp{}, false
			}
			op := pyNativeOp{kind: kind, dst: dst}
			if leftIsConst {
				op.a = -1
				op.c = leftConst
			} else {
				op.a = leftIdx
			}
			if rightIsConst {
				op.b = -1
				op.c = rightConst
			} else {
				op.b = rightIdx
			}
			return op, true
		}
	}
	return pyNativeOp{}, false
}

func nativeKindOf(op string) (pyNativeOpKind, bool) {
	switch op {
	case "+":
		return pyOpAdd, true
	case "-":
		return pyOpSub, true
	case "*":
		return pyOpMul, true
	}
	return 0, false
}

// operandOf 解析操作数：变量 → (idx, 0, false, true)；整数常量 → (0, val, true, true)
func operandOf(e ast.Expr, addVar func(string) (int, bool)) (idx int, constVal int64, isConst bool, ok bool) {
	switch v := e.(type) {
	case *ast.Name:
		i, ok := addVar(v.Id)
		if !ok {
			return 0, 0, false, false
		}
		return i, 0, false, true
	case *ast.Number:
		if v.IsFloat {
			return 0, 0, false, false
		}
		n, err := strconv.ParseInt(v.Value, 10, 64)
		if err != nil {
			return 0, 0, false, false
		}
		return 0, n, true, true
	}
	return 0, 0, false, false
}
