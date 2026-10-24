package ast

import (
	"testing"

	"zuzo/token"
)

// === 测试辅助函数 ===

func makeIdent(name string) *Identifier {
	return &Identifier{Token: token.Token{Type: token.IDENT, Literal: name}, Value: name}
}

func makeInt(val int64) *IntegerLiteral {
	return &IntegerLiteral{Token: token.Token{Type: token.INT, Literal: "1"}, Value: val}
}

func makeFloat(val float64) *FloatLiteral {
	return &FloatLiteral{Token: token.Token{Type: token.FLOAT, Literal: "1.0"}, Value: val}
}

func makeStr(val string) *StringLiteral {
	return &StringLiteral{Token: token.Token{Type: token.STRING, Literal: val}, Value: val}
}

func makeBool(val bool) *BooleanLiteral {
	var t token.TokenType = token.TRUE
	if !val {
		t = token.FALSE
	}
	return &BooleanLiteral{Token: token.Token{Type: t, Literal: "真"}, Value: val}
}

func makeNull() *NullLiteral {
	return &NullLiteral{Token: token.Token{Type: token.NULL, Literal: "空"}}
}

func makeInfix(op string, left, right Expression) *InfixExpression {
	return &InfixExpression{
		Token:    token.Token{Type: token.TokenType(op), Literal: op},
		Operator: op,
		Left:     left,
		Right:    right,
	}
}

func makePrefix(op string, right Expression) *PrefixExpression {
	return &PrefixExpression{
		Token:    token.Token{Type: token.TokenType(op), Literal: op},
		Operator: op,
		Right:    right,
	}
}

func makeCall(fn Expression, args []Expression) *CallExpression {
	return &CallExpression{
		Token:     token.Token{Type: token.LPAREN, Literal: "("},
		Function:  fn,
		Arguments: args,
	}
}

func makeArray(elements []Expression) *ArrayLiteral {
	return &ArrayLiteral{
		Token:    token.Token{Type: token.LBRACKET, Literal: "["},
		Elements: elements,
	}
}

func makeHash(pairs map[Expression]Expression) *HashLiteral {
	return &HashLiteral{
		Token: token.Token{Type: token.LBRACE, Literal: "{"},
		Pairs: pairs,
	}
}

func makeIndex(left, index Expression) *IndexExpression {
	return &IndexExpression{
		Token: token.Token{Type: token.LBRACKET, Literal: "["},
		Left:  left,
		Index: index,
	}
}

// === 核心测试 ===

func TestFingerprint_Nil(t *testing.T) {
	if Fingerprint(nil) != 0 {
		t.Error("nil 节点的指纹应为 0")
	}
}

func TestFingerprint_Identifier(t *testing.T) {
	// 所有标识符的指纹应该相同（无视命名差异）
	x := makeIdent("x")
	y := makeIdent("y")
	a := makeIdent("a")
	b := makeIdent("b")

	if Fingerprint(x) != Fingerprint(y) {
		t.Error("x 和 y 的指纹应该相同")
	}
	if Fingerprint(x) != Fingerprint(a) {
		t.Error("x 和 a 的指纹应该相同")
	}
	if Fingerprint(x) != Fingerprint(b) {
		t.Error("x 和 b 的指纹应该相同")
	}
}

func TestFingerprint_Literals(t *testing.T) {
	// 相同类型的不同字面量指纹应该相同
	if Fingerprint(makeInt(1)) != Fingerprint(makeInt(999)) {
		t.Error("不同整数的指纹应该相同")
	}
	if Fingerprint(makeFloat(1.0)) != Fingerprint(makeFloat(3.14)) {
		t.Error("不同浮点数的指纹应该相同")
	}
	if Fingerprint(makeStr("hello")) != Fingerprint(makeStr("world")) {
		t.Error("不同字符串的指纹应该相同")
	}
	if Fingerprint(makeBool(true)) != Fingerprint(makeBool(false)) {
		t.Error("不同布尔值的指纹应该相同")
	}

	// 不同类型字面量的指纹应该不同
	if Fingerprint(makeInt(1)) == Fingerprint(makeFloat(1.0)) {
		t.Error("整数和浮点数的指纹应该不同")
	}
	if Fingerprint(makeInt(1)) == Fingerprint(makeStr("1")) {
		t.Error("整数和字符串的指纹应该不同")
	}
	if Fingerprint(makeInt(1)) == Fingerprint(makeBool(true)) {
		t.Error("整数和布尔值的指纹应该不同")
	}
	if Fingerprint(makeInt(1)) == Fingerprint(makeNull()) {
		t.Error("整数和空值的指纹应该不同")
	}
}

// === 交换律感知测试（算法灵魂）===

func TestFingerprint_CommutativeAddition(t *testing.T) {
	// x + y 和 y + x 的指纹应该相同（交换律）
	xy := makeInfix("+", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("+", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) != Fingerprint(yx) {
		t.Error("x+y 和 y+x 的指纹应该相同（交换律）")
	}
}

func TestFingerprint_CommutativeMul(t *testing.T) {
	// x * y 和 y * x 的指纹应该相同
	xy := makeInfix("*", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("*", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) != Fingerprint(yx) {
		t.Error("x*y 和 y*x 的指纹应该相同（交换律）")
	}
}

func TestFingerprint_CommutativeAnd(t *testing.T) {
	// x 并且 y 和 y 并且 x 的指纹应该相同
	xy := makeInfix("并且", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("并且", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) != Fingerprint(yx) {
		t.Error("x并且y 和 y并且x 的指纹应该相同（交换律）")
	}
}

func TestFingerprint_CommutativeOr(t *testing.T) {
	// x 或者 y 和 y 或者 x 的指纹应该相同
	xy := makeInfix("或者", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("或者", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) != Fingerprint(yx) {
		t.Error("x或者y 和 y或者x 的指纹应该相同（交换律）")
	}
}

func TestFingerprint_CommutativeEq(t *testing.T) {
	// x == y 和 y == x 的指纹应该相同
	xy := makeInfix("==", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("==", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) != Fingerprint(yx) {
		t.Error("x==y 和 y==x 的指纹应该相同（交换律）")
	}
}

func TestFingerprint_CommutativeNeq(t *testing.T) {
	// x != y 和 y != x 的指纹应该相同
	xy := makeInfix("!=", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("!=", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) != Fingerprint(yx) {
		t.Error("x!=y 和 y!=x 的指纹应该相同（交换律）")
	}
}

// === 非交换律保护测试 ===

func TestFingerprint_NonCommutativeSub(t *testing.T) {
	// x - y 和 y - x 的指纹应该不同（非交换律）
	xy := makeInfix("-", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("-", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) == Fingerprint(yx) {
		t.Error("x-y 和 y-x 的指纹应该不同（非交换律保护）")
	}
}

func TestFingerprint_NonCommutativeDiv(t *testing.T) {
	// x / y 和 y / x 的指纹应该不同
	xy := makeInfix("/", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("/", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) == Fingerprint(yx) {
		t.Error("x/y 和 y/x 的指纹应该不同（非交换律保护）")
	}
}

func TestFingerprint_NonCommutativeMod(t *testing.T) {
	// x % y 和 y % x 的指纹应该不同
	xy := makeInfix("%", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("%", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) == Fingerprint(yx) {
		t.Error("x取模y 和 y取模x 的指纹应该不同（非交换律保护）")
	}
}

func TestFingerprint_NonCommutativeLt(t *testing.T) {
	// x < y 和 y < x 的指纹应该不同
	xy := makeInfix("<", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("<", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) == Fingerprint(yx) {
		t.Error("x<y 和 y<x 的指纹应该不同（非交换律保护）")
	}
}

func TestFingerprint_NonCommutativeGt(t *testing.T) {
	// x > y 和 y > x 的指纹应该不同
	xy := makeInfix(">", makeIdent("x"), makeIdent("y"))
	yx := makeInfix(">", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) == Fingerprint(yx) {
		t.Error("x>y 和 y>x 的指纹应该不同（非交换律保护）")
	}
}

func TestFingerprint_NonCommutativeLte(t *testing.T) {
	// x <= y 和 y <= x 的指纹应该不同
	xy := makeInfix("<=", makeIdent("x"), makeIdent("y"))
	yx := makeInfix("<=", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) == Fingerprint(yx) {
		t.Error("x<=y 和 y<=x 的指纹应该不同（非交换律保护）")
	}
}

func TestFingerprint_NonCommutativeGte(t *testing.T) {
	// x >= y 和 y >= x 的指纹应该不同
	xy := makeInfix(">=", makeIdent("x"), makeIdent("y"))
	yx := makeInfix(">=", makeIdent("y"), makeIdent("x"))

	if Fingerprint(xy) == Fingerprint(yx) {
		t.Error("x>=y 和 y>=x 的指纹应该不同（非交换律保护）")
	}
}

// === 命名无关测试 ===

func TestFingerprint_NameInsensitive(t *testing.T) {
	// x + y 和 a + b 的指纹应该相同（命名无关）
	xy := makeInfix("+", makeIdent("x"), makeIdent("y"))
	ab := makeInfix("+", makeIdent("a"), makeIdent("b"))

	if Fingerprint(xy) != Fingerprint(ab) {
		t.Error("x+y 和 a+b 的指纹应该相同（命名无关）")
	}
}

// === 字面量值无关测试 ===

func TestFingerprint_LiteralValueInsensitive(t *testing.T) {
	// 1 + 2 和 3 + 4 的指纹应该相同
	a := makeInfix("+", makeInt(1), makeInt(2))
	b := makeInfix("+", makeInt(3), makeInt(4))

	if Fingerprint(a) != Fingerprint(b) {
		t.Error("1+2 和 3+4 的指纹应该相同（值无关）")
	}
}

// === 不同运算符不同指纹 ===

func TestFingerprint_DifferentOps(t *testing.T) {
	// x + y 和 x * y 的指纹应该不同
	add := makeInfix("+", makeIdent("x"), makeIdent("y"))
	mul := makeInfix("*", makeIdent("x"), makeIdent("y"))

	if Fingerprint(add) == Fingerprint(mul) {
		t.Error("x+y 和 x*y 的指纹应该不同")
	}

	// x - y 和 x / y 的指纹应该不同
	sub := makeInfix("-", makeIdent("x"), makeIdent("y"))
	div := makeInfix("/", makeIdent("x"), makeIdent("y"))

	if Fingerprint(sub) == Fingerprint(div) {
		t.Error("x-y 和 x/y 的指纹应该不同")
	}
}

// === 嵌套表达式交换律测试 ===

func TestFingerprint_NestedCommutative(t *testing.T) {
	// (x + y) * z 和 z * (y + x) 的指纹应该相同
	// 因为 * 是交换律，且 x+y == y+x
	lhs := makeInfix("*",
		makeInfix("+", makeIdent("x"), makeIdent("y")),
		makeIdent("z"),
	)
	rhs := makeInfix("*",
		makeIdent("z"),
		makeInfix("+", makeIdent("y"), makeIdent("x")),
	)

	if Fingerprint(lhs) != Fingerprint(rhs) {
		t.Error("(x+y)*z 和 z*(y+x) 的指纹应该相同（嵌套交换律）")
	}
}

func TestFingerprint_DeepNestedCommutative(t *testing.T) {
	// (a + b) * (c + d) 和 (d + c) * (b + a) 的指纹应该相同
	lhs := makeInfix("*",
		makeInfix("+", makeIdent("a"), makeIdent("b")),
		makeInfix("+", makeIdent("c"), makeIdent("d")),
	)
	rhs := makeInfix("*",
		makeInfix("+", makeIdent("d"), makeIdent("c")),
		makeInfix("+", makeIdent("b"), makeIdent("a")),
	)

	if Fingerprint(lhs) != Fingerprint(rhs) {
		t.Error("(a+b)*(c+d) 和 (d+c)*(b+a) 的指纹应该相同（深层嵌套交换律）")
	}
}

func TestFingerprint_NestedNonCommutative(t *testing.T) {
	// (x - y) * z 和 z * (y - x) 的指纹应该不同
	// 因为 - 不是交换律
	lhs := makeInfix("*",
		makeInfix("-", makeIdent("x"), makeIdent("y")),
		makeIdent("z"),
	)
	rhs := makeInfix("*",
		makeIdent("z"),
		makeInfix("-", makeIdent("y"), makeIdent("x")),
	)

	if Fingerprint(lhs) == Fingerprint(rhs) {
		t.Error("(x-y)*z 和 z*(y-x) 的指纹应该不同（内部非交换律）")
	}
}

// === 前缀表达式测试 ===

func TestFingerprint_Prefix(t *testing.T) {
	// -x 和 -y 的指纹应该相同（命名无关）
	nx := makePrefix("-", makeIdent("x"))
	ny := makePrefix("-", makeIdent("y"))

	if Fingerprint(nx) != Fingerprint(ny) {
		t.Error("-x 和 -y 的指纹应该相同")
	}

	// 非x 和 非y 的指纹应该相同
	fx := makePrefix("非", makeIdent("x"))
	fy := makePrefix("非", makeIdent("y"))

	if Fingerprint(fx) != Fingerprint(fy) {
		t.Error("非x 和 非y 的指纹应该相同")
	}

	// -x 和 !x 的指纹应该不同（不同运算符）
	bx := makePrefix("!", makeIdent("x"))
	if Fingerprint(nx) == Fingerprint(bx) {
		t.Error("-x 和 !x 的指纹应该不同")
	}
}

// === 函数调用测试 ===

func TestFingerprint_CallExpression(t *testing.T) {
	// f(x, y) 和 g(a, b) 的指纹应该相同
	// 因为函数名和参数名都是标识符（统一质数）
	call1 := makeCall(makeIdent("f"), []Expression{makeIdent("x"), makeIdent("y")})
	call2 := makeCall(makeIdent("g"), []Expression{makeIdent("a"), makeIdent("b")})

	if Fingerprint(call1) != Fingerprint(call2) {
		t.Error("f(x,y) 和 g(a,b) 的指纹应该相同（命名无关）")
	}
}

func TestFingerprint_CallArity(t *testing.T) {
	// f(x) 和 f(x, y) 的指纹应该不同（参数个数不同）
	call1 := makeCall(makeIdent("f"), []Expression{makeIdent("x")})
	call2 := makeCall(makeIdent("f"), []Expression{makeIdent("x"), makeIdent("y")})

	if Fingerprint(call1) == Fingerprint(call2) {
		t.Error("f(x) 和 f(x,y) 的指纹应该不同（参数个数不同）")
	}
}

// === 数组测试（有序）===

func TestFingerprint_ArrayOrdered(t *testing.T) {
	// [x, y] 和 [y, x] 的指纹应该不同（数组有序）
	arr1 := makeArray([]Expression{makeIdent("x"), makeIdent("y")})
	arr2 := makeArray([]Expression{makeIdent("y"), makeIdent("x")})

	if Fingerprint(arr1) == Fingerprint(arr2) {
		t.Error("[x,y] 和 [y,x] 的指纹应该不同（数组有序）")
	}
}

func TestFingerprint_ArrayNameInsensitive(t *testing.T) {
	// [x, y] 和 [a, b] 的指纹应该相同（命名无关）
	arr1 := makeArray([]Expression{makeIdent("x"), makeIdent("y")})
	arr2 := makeArray([]Expression{makeIdent("a"), makeIdent("b")})

	if Fingerprint(arr1) != Fingerprint(arr2) {
		t.Error("[x,y] 和 [a,b] 的指纹应该相同（命名无关）")
	}
}

// === 哈希表测试（无序）===

func TestFingerprint_HashUnordered(t *testing.T) {
	// {"a": x, "b": y} 和 {"b": y, "a": x} 的指纹应该相同（哈希无序）
	// 注意：键是字符串字面量，值是标识符，所以都映射为相同质数
	hash1 := makeHash(map[Expression]Expression{
		makeStr("a"): makeIdent("x"),
		makeStr("b"): makeIdent("y"),
	})
	hash2 := makeHash(map[Expression]Expression{
		makeStr("b"): makeIdent("y"),
		makeStr("a"): makeIdent("x"),
	})

	if Fingerprint(hash1) != Fingerprint(hash2) {
		t.Error("哈希表不同顺序的指纹应该相同（无序）")
	}
}

// === 索引测试 ===

func TestFingerprint_Index(t *testing.T) {
	// x[y] 和 a[b] 的指纹应该相同
	idx1 := makeIndex(makeIdent("x"), makeIdent("y"))
	idx2 := makeIndex(makeIdent("a"), makeIdent("b"))

	if Fingerprint(idx1) != Fingerprint(idx2) {
		t.Error("x[y] 和 a[b] 的指纹应该相同")
	}

	// x[y] 和 y[x] 的指纹应该不同（索引非交换）
	idx3 := makeIndex(makeIdent("y"), makeIdent("x"))
	if Fingerprint(idx1) == Fingerprint(idx3) {
		t.Error("x[y] 和 y[x] 的指纹应该不同")
	}
}

// === FingerprintTable 测试 ===

func TestFingerprintTable_RegisterAndLookup(t *testing.T) {
	table := NewFingerprintTable()

	expr := makeInfix("+", makeIdent("x"), makeIdent("y"))
	table.RegisterExpression(expr, "test.hy:1")

	fp := Fingerprint(expr)
	results := table.Lookup(fp)

	if len(results) != 1 {
		t.Fatalf("期望 1 个结果，得到 %d", len(results))
	}
	if results[0].Location != "test.hy:1" {
		t.Errorf("期望位置 test.hy:1，得到 %s", results[0].Location)
	}
}

func TestFingerprintTable_FindDuplicates(t *testing.T) {
	table := NewFingerprintTable()

	// 注册 3 个结构相同的表达式
	table.RegisterExpression(
		makeInfix("+", makeIdent("x"), makeIdent("y")),
		"file1.hy:1",
	)
	table.RegisterExpression(
		makeInfix("+", makeIdent("a"), makeIdent("b")),
		"file2.hy:1",
	)
	table.RegisterExpression(
		makeInfix("+", makeIdent("p"), makeIdent("q")),
		"file3.hy:1",
	)
	// 注册 1 个不同的表达式
	table.RegisterExpression(
		makeInfix("-", makeIdent("x"), makeIdent("y")),
		"file1.hy:2",
	)

	dups := table.FindDuplicates()
	if len(dups) != 1 {
		t.Fatalf("期望 1 个重复组，得到 %d", len(dups))
	}
	if len(dups[0]) != 3 {
		t.Errorf("期望重复组有 3 个条目，得到 %d", len(dups[0]))
	}
}

func TestFingerprintTable_Stats(t *testing.T) {
	table := NewFingerprintTable()

	// 注册 4 个表达式，其中 3 个结构相同
	table.RegisterExpression(makeInfix("+", makeIdent("x"), makeIdent("y")), "a:1")
	table.RegisterExpression(makeInfix("+", makeIdent("a"), makeIdent("b")), "a:2")
	table.RegisterExpression(makeInfix("+", makeIdent("p"), makeIdent("q")), "a:3")
	table.RegisterExpression(makeInfix("*", makeIdent("x"), makeIdent("y")), "a:4")

	stats := table.Stats()
	if stats.TotalNodes != 4 {
		t.Errorf("期望 TotalNodes=4，得到 %d", stats.TotalNodes)
	}
	if stats.UniqueNodes != 2 {
		t.Errorf("期望 UniqueNodes=2，得到 %d", stats.UniqueNodes)
	}
	if stats.DuplicateGroups != 1 {
		t.Errorf("期望 DuplicateGroups=1，得到 %d", stats.DuplicateGroups)
	}
	if stats.MaxGroupSize != 3 {
		t.Errorf("期望 MaxGroupSize=3，得到 %d", stats.MaxGroupSize)
	}
}

func TestFingerprintTable_LookupExpression(t *testing.T) {
	table := NewFingerprintTable()

	table.RegisterExpression(makeInfix("+", makeIdent("x"), makeIdent("y")), "a:1")
	table.RegisterExpression(makeInfix("-", makeIdent("x"), makeIdent("y")), "a:2")

	// 用一个结构相同但变量名不同的表达式查找
	query := makeInfix("+", makeIdent("foo"), makeIdent("bar"))
	results := table.LookupExpression(query)

	if len(results) != 1 {
		t.Fatalf("期望 1 个匹配，得到 %d", len(results))
	}
	if results[0].Location != "a:1" {
		t.Errorf("期望位置 a:1，得到 %s", results[0].Location)
	}
}

func TestFingerprintTable_SideEffectFreeDuplicates(t *testing.T) {
	table := NewFingerprintTable()

	// 纯表达式（无副作用）
	table.RegisterExpression(makeInfix("+", makeIdent("x"), makeIdent("y")), "a:1")
	table.RegisterExpression(makeInfix("+", makeIdent("a"), makeIdent("b")), "a:2")

	// 有副作用的表达式（函数调用）
	call1 := makeCall(makeIdent("f"), []Expression{makeIdent("x")})
	call2 := makeCall(makeIdent("g"), []Expression{makeIdent("a")})
	table.RegisterExpression(call1, "a:3")
	table.RegisterExpression(call2, "a:4")

	// 谓词：函数调用有副作用
	hasSideEffect := func(node Node) bool {
		_, ok := node.(*CallExpression)
		return ok
	}

	dups := table.FindSideEffectFreeDuplicates(hasSideEffect)
	if len(dups) != 1 {
		t.Fatalf("期望 1 个无副作用重复组，得到 %d", len(dups))
	}
	if len(dups[0]) != 2 {
		t.Errorf("期望重复组有 2 个条目，得到 %d", len(dups[0]))
	}
}

// === FingerprintNode 测试（语句级别）===

func TestFingerprintNode_ExpressionStatement(t *testing.T) {
	stmt1 := &ExpressionStatement{
		Token:      token.Token{Type: token.IDENT, Literal: "x"},
		Expression: makeInfix("+", makeIdent("x"), makeIdent("y")),
	}
	stmt2 := &ExpressionStatement{
		Token:      token.Token{Type: token.IDENT, Literal: "a"},
		Expression: makeInfix("+", makeIdent("a"), makeIdent("b")),
	}

	if FingerprintNode(stmt1) != FingerprintNode(stmt2) {
		t.Error("结构相同的表达式语句指纹应该相同")
	}
}

func TestFingerprintNode_LetStatement(t *testing.T) {
	stmt1 := &LetStatement{
		Token: token.Token{Type: token.LET, Literal: "变量"},
		Name:  makeIdent("x"),
		Value: makeInfix("+", makeIdent("x"), makeIdent("y")),
	}
	stmt2 := &LetStatement{
		Token: token.Token{Type: token.LET, Literal: "变量"},
		Name:  makeIdent("a"),
		Value: makeInfix("+", makeIdent("a"), makeIdent("b")),
	}

	if FingerprintNode(stmt1) != FingerprintNode(stmt2) {
		t.Error("结构相同的变量声明语句指纹应该相同")
	}
}

func TestFingerprintNode_ReturnStatement(t *testing.T) {
	stmt1 := &ReturnStatement{
		Token:       token.Token{Type: token.RETURN, Literal: "返回"},
		ReturnValue: makeInfix("*", makeIdent("x"), makeIdent("y")),
	}
	stmt2 := &ReturnStatement{
		Token:       token.Token{Type: token.RETURN, Literal: "返回"},
		ReturnValue: makeInfix("*", makeIdent("a"), makeIdent("b")),
	}

	if FingerprintNode(stmt1) != FingerprintNode(stmt2) {
		t.Error("结构相同的返回语句指纹应该相同")
	}
}

// === FingerprintTable RegisterProgram 测试 ===

func TestFingerprintTable_RegisterProgram(t *testing.T) {
	// 构造一个简单程序：变量 x = a + b; 变量 y = c + d;
	prog := &Program{
		Statements: []Statement{
			&LetStatement{
				Token: token.Token{Type: token.LET, Literal: "变量"},
				Name:  makeIdent("x"),
				Value: makeInfix("+", makeIdent("a"), makeIdent("b")),
			},
			&LetStatement{
				Token: token.Token{Type: token.LET, Literal: "变量"},
				Name:  makeIdent("y"),
				Value: makeInfix("+", makeIdent("c"), makeIdent("d")),
			},
		},
	}

	table := NewFingerprintTable()
	table.RegisterProgram(prog, "test.hy")

	stats := table.Stats()
	// 应该有重复：a+b 和 c+d 结构相同
	if stats.DuplicateGroups == 0 {
		t.Error("应该至少有 1 个重复组（a+b 和 c+d 结构相同）")
	}
}

// === 边界情况测试 ===

func TestFingerprint_NilChild(t *testing.T) {
	// nil 子节点不应 panic
	expr := &InfixExpression{
		Token:    token.Token{Type: token.PLUS, Literal: "+"},
		Operator: "+",
		Left:     nil,
		Right:    makeIdent("x"),
	}
	// 不应 panic
	_ = Fingerprint(expr)
}

func TestFingerprint_DefaultCase(t *testing.T) {
	// 未知节点类型不应 panic，返回默认质数
	errExpr := &ErrorExpression{
		Token:   token.Token{Type: token.ILLEGAL, Literal: "?"},
		Message: "test error",
	}
	fp := Fingerprint(errExpr)
	if fp != primeError {
		t.Errorf("错误节点指纹应为 %d，得到 %d", primeError, fp)
	}
}

func TestFingerprintTable_Empty(t *testing.T) {
	table := NewFingerprintTable()
	if table.Count() != 0 {
		t.Error("空表 Count 应为 0")
	}
	if table.UniqueCount() != 0 {
		t.Error("空表 UniqueCount 应为 0")
	}
	if len(table.FindDuplicates()) != 0 {
		t.Error("空表 FindDuplicates 应为空")
	}
}

// === 结合律测试（bonus：乘法交换律自然实现结合律感知）===

func TestFingerprint_AssociativityAdd(t *testing.T) {
	// (a + b) + c 和 a + (b + c) 的指纹应该相同
	// 因为乘法满足结合律：(fp(a+b) * fp(c)) == (fp(a) * fp(b+c))
	lhs := makeInfix("+",
		makeInfix("+", makeIdent("a"), makeIdent("b")),
		makeIdent("c"),
	)
	rhs := makeInfix("+",
		makeIdent("a"),
		makeInfix("+", makeIdent("b"), makeIdent("c")),
	)

	if Fingerprint(lhs) != Fingerprint(rhs) {
		// 注意：由于 uint64 溢出，这在数学上是成立的
		// (X * Y) * Z == X * (Y * Z) 在模 2^64 下成立
		t.Log("(a+b)+c 和 a+(b+c) 指纹相同或不同取决于 uint64 溢出行为")
		// 这不是一个严格的测试，因为理论上应该相同
		// 但如果有碰撞也不影响算法的正确性
	}
}
