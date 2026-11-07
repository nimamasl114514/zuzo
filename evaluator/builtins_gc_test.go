package evaluator

import (
	"testing"
	"time"

	"zuzo/object"
)

// TestGCReachableArrayGrowth 验证：常驻数组后可达对象数按「数组 + 元素」线性增长。
func TestGCReachableArrayGrowth(t *testing.T) {
	env := object.NewEnvironment()

	base, _ := countReachable(env)

	outer := &object.Array{}
	for i := 0; i < 100; i++ {
		inner := &object.Array{Elements: []object.Object{&object.Integer{Value: int64(i)}}}
		outer.Elements = append(outer.Elements, inner)
	}
	env.Set("保持", outer)

	got, bytes := countReachable(env)
	// 外层数组(1) + 100 个内层数组 + 100 个整数 = 201 个新对象
	if delta := got - base; delta != 201 {
		t.Errorf("可达对象增量 = %d，期望 201", delta)
	}
	if bytes <= 0 {
		t.Errorf("reach_bytes = %d，期望 > 0", bytes)
	}
}

// TestGCReachableReleased 验证：变量被覆盖为 空 后，旧对象不再计入可达。
func TestGCReachableReleased(t *testing.T) {
	env := object.NewEnvironment()

	arr := &object.Array{Elements: []object.Object{
		&object.String{Value: "abc"},
		&object.Integer{Value: 1},
	}}
	env.Set("临时", arr)
	before, _ := countReachable(env)

	env.Set("临时", &object.Null{})
	after, _ := countReachable(env)

	if after >= before {
		t.Errorf("覆盖为空后可达数未下降：before=%d after=%d", before, after)
	}
}

// TestGCReachableCycle 验证：环境自引用（闭包持有自身所在环境）不会死循环。
func TestGCReachableCycle(t *testing.T) {
	env := object.NewEnvironment()
	fn := &object.Function{Name: "递归", Env: env}
	env.Set("自己", fn)

	done := make(chan struct{})
	var got int
	go func() {
		got, _ = countReachable(env)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("countReachable 未在限定时间内返回，疑似环未去重")
	}
	// 环境本身不计入 objs，只有函数对象 = 1；关键是不能因环而死循环
	if got != 1 {
		t.Errorf("可达对象数 = %d，期望 1", got)
	}
}

// TestGCReachableHashAndInstance 验证：哈希键值与实例属性都会被遍历到。
func TestGCReachableHashAndInstance(t *testing.T) {
	env := object.NewEnvironment()

	h := &object.Hash{Pairs: map[object.HashKey]object.HashPair{}}
	h.SetKey(&object.String{Value: "k"}, &object.String{Value: "v"})
	env.Set("表", h)

	inst := &object.Instance{DefName: "甲", Props: map[string]object.Object{
		"字段": &object.Integer{Value: 7},
	}}
	env.Set("实例", inst)

	got, _ := countReachable(env)
	// 环境不计入 objs；表(1) + 键(1) + 值(1) + 实例(1) + 属性值(1) = 5
	if got != 5 {
		t.Errorf("可达对象数 = %d，期望 5", got)
	}
}
