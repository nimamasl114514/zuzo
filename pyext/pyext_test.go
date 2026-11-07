package pyext

// pyext_test.go — 桥的 Go 侧单测与基准（第32轮）
//
// 三条约束：
//  1. 不依赖具体 Python 版本；只用标准库 json / sys。
//  2. 机器上没有 CPython 时 Skip，不让 CI 红。
//  3. 协议编解码是纯函数，必须无条件可测（不启子进程）。

import (
	"encoding/json"
	"strconv"
	"sync"
	"testing"
	"time"

	"zuzo/object"
)

func TestDecodePrimitives(t *testing.T) {
	cases := []struct {
		raw  string
		want string
		typ  object.ObjectType
	}{
		{`{"k":"none"}`, "空", object.NULL_OBJ},
		{`{"k":"bool","v":true}`, "真", object.BOOLEAN_OBJ},
		{`{"k":"int","v":"-123"}`, "-123", object.INTEGER_OBJ},
		{`{"k":"float","v":2.5}`, "2.5", object.FLOAT_OBJ},
		{`{"k":"str","v":"你好"}`, "你好", object.STRING_OBJ},
		{`{"k":"list","v":[{"k":"int","v":"1"},{"k":"str","v":"a"}]}`, "[1, a]", object.ARRAY_OBJ},
		{`{"k":"obj","id":7,"t":"module","r":"<module x>"}`, "<module x>", object.PY_OBJ},
		{`{"k":"call","id":8,"t":"builtin_function_or_method","r":"<built-in>"}`, "<built-in>", object.PY_OBJ},
	}
	for _, c := range cases {
		got, err := decodeVal(json.RawMessage(c.raw), "sess")
		if err != nil {
			t.Fatalf("%s: %v", c.raw, err)
		}
		if string(got.Type()) != string(c.typ) {
			t.Fatalf("%s: 类型 = %s，期望 %s", c.raw, got.Type(), c.typ)
		}
		if got.Inspect() != c.want {
			t.Fatalf("%s: Inspect = %q，期望 %q", c.raw, got.Inspect(), c.want)
		}
	}
}

func TestDecodeOverflowIntBecomesString(t *testing.T) {
	// 2**70 超出 int64：必须降级为字符串，绝不静默截断
	got, err := decodeVal(json.RawMessage(`{"k":"int","v":"1180591620717411303424"}`), "sess")
	if err != nil {
		t.Fatal(err)
	}
	if got.Type() != object.STRING_OBJ {
		t.Fatalf("期望降级为字符串，得到 %s", got.Type())
	}
	if got.Inspect() != "1180591620717411303424" {
		t.Fatalf("数值被改写: %s", got.Inspect())
	}
}

func TestEncodeRoundTrip(t *testing.T) {
	h := &object.Hash{Pairs: map[object.HashKey]object.HashPair{}}
	h.SetKey(&object.String{Value: "甲"}, &object.Integer{Value: 1})
	src := &object.Array{Elements: []object.Object{
		&object.Integer{Value: 42},
		&object.Float{Value: 1.5},
		&object.String{Value: "文本"},
		&object.Boolean{Value: true},
		&object.Null{},
		&object.PyObj{ID: 9, Typ: "module", Repr: "m"},
		h,
	}}
	enc, err := encodeObj(src)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(enc)
	var back wireVal
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatal(err)
	}
	if back.K != "list" {
		t.Fatalf("顶层应为 list，得到 %s", back.K)
	}
	// 句柄必须编成 h（引用），不能编成字符串
	if s := string(b); !contains(s, `"k":"h"`) {
		t.Fatalf("句柄未按引用编码: %s", s)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// newTestClient 起一个桥；没有 CPython 就 Skip
func newTestClient(t *testing.T) *Client {
	t.Helper()
	c := New()
	c.SetNotice(nil)
	if _, _, err := discoverPython(); err != nil {
		c.Close()
		t.Skipf("本机无 CPython，跳过: %v", err)
	}
	if err := c.Start(); err != nil {
		c.Close()
		t.Skipf("桥启动失败，跳过: %v", err)
	}
	t.Cleanup(c.Close)
	return c
}

func TestClientEvalExecGlobals(t *testing.T) {
	c := newTestClient(t)
	v, err := c.Eval("1+1")
	if err != nil {
		t.Fatal(err)
	}
	if v.Inspect() != "2" {
		t.Fatalf("eval 1+1 = %s", v.Inspect())
	}
	if err := c.Exec("x = 21"); err != nil {
		t.Fatal(err)
	}
	if err := c.SetName("y", &object.Integer{Value: 2}); err != nil {
		t.Fatal(err)
	}
	v, err = c.Eval("x * y")
	if err != nil {
		t.Fatal(err)
	}
	if v.Inspect() != "42" {
		t.Fatalf("x*y = %s，期望 42", v.Inspect())
	}
	got, ok := c.Lookup("y")
	if !ok || got.Inspect() != "2" {
		t.Fatalf("Lookup(y) = %v %v", got, ok)
	}
}

func TestClientImportCallStdlib(t *testing.T) {
	c := newTestClient(t)
	mod, err := c.Import("json")
	if err != nil {
		t.Fatal(err)
	}
	m, ok := mod.(*object.PyObj)
	if !ok || m.Typ != "module" {
		t.Fatalf("import json 未返回模块句柄: %#v", mod)
	}
	fn, err := c.GetAttr(m, "dumps")
	if err != nil {
		t.Fatal(err)
	}
	f, ok := fn.(*object.PyObj)
	if !ok || !f.Callable {
		t.Fatalf("dumps 应为可调用句柄: %#v", fn)
	}
	arr := &object.Array{Elements: []object.Object{&object.Integer{Value: 1}}}
	out, err := c.Call(f, []object.Object{arr}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Inspect() != "[1]" {
		t.Fatalf("json.dumps([1]) = %s", out.Inspect())
	}
}

func TestClientErrorCarriesTraceback(t *testing.T) {
	c := newTestClient(t)
	_, err := c.Eval("1/0")
	if err == nil {
		t.Fatal("1/0 应当报错")
	}
	msg := err.Error()
	if !contains(msg, "ZeroDivisionError") {
		t.Fatalf("错误缺异常类名: %s", msg)
	}
	if !contains(msg, "Traceback") {
		t.Fatalf("错误缺 traceback: %s", msg)
	}
	// 报错后桥仍可用
	if v, e := c.Eval("2+2"); e != nil || v.Inspect() != "4" {
		t.Fatalf("报错后桥不可用: %v %v", v, e)
	}
}

func TestClientHasModule(t *testing.T) {
	c := newTestClient(t)
	ok, err := c.HasModule("json")
	if err != nil || !ok {
		t.Fatalf("json 应存在: %v %v", ok, err)
	}
	ok, err = c.HasModule("zzz_nonexist_pkg_zz")
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("不存在的包不应报存在")
	}
}

// BenchmarkEval 单次往返开销（启动与握手不计入）
func BenchmarkEval(b *testing.B) {
	c := New()
	c.SetNotice(nil)
	if err := c.Start(); err != nil {
		b.Skipf("无 CPython: %v", err)
	}
	defer c.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Eval("1+1"); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkImportNumpy 冷启动导入 numpy 的耗时（含解释器握手）
func BenchmarkImportNumpy(b *testing.B) {
	c := New()
	c.SetNotice(nil)
	if err := c.Start(); err != nil {
		b.Skipf("无 CPython: %v", err)
	}
	defer c.Close()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Import("numpy"); err != nil {
			b.Skipf("无 numpy: %v", err)
		}
	}
}

// ==================== 第33轮：多子进程连接池（ZUZO_PY_POOL） ====================
//
// 句柄路由契约：id = workerIdx<<48 | localId
//   - 默认 pool=1 时 workerIdx 恒为 0，id 与第32轮完全一致（兼容）；
//   - pool>1 时 import 首次轮询分配，后续 get/set/call/repr/native/release 按高 16 位回到原 worker；
//   - 第34轮模块亲和：同名模块固定在同一 worker（首次轮询 + 登记），因此只有「不同模块」才
//     会散到不同 worker；同一模块重复 import 恒回原 worker（避免 numpy 被加载 N 份）；
//   - globals 类 op（eval/exec/getglobal/setglobal/has/hello）固定 worker 0。

// newPoolClient 起一个 N worker 的桥（无 CPython 时 Skip）
func newPoolClient(t *testing.T, n int) *Client {
	t.Helper()
	t.Setenv("ZUZO_PY_POOL", strconv.Itoa(n))
	c := newTestClient(t)
	if c.PoolSize() != n {
		t.Fatalf("PoolSize = %d，期望 %d", c.PoolSize(), n)
	}
	return c
}

func TestPoolDefaultSingleWorkerCompatible(t *testing.T) {
	// 默认（未设置 ZUZO_PY_POOL）= 1 个 worker，且句柄 id 高 16 位必须为 0
	t.Setenv("ZUZO_PY_POOL", "")
	c := newTestClient(t)
	if c.PoolSize() != 1 {
		t.Fatalf("默认 PoolSize = %d，期望 1", c.PoolSize())
	}
	mod, err := c.Import("json")
	if err != nil {
		t.Fatal(err)
	}
	p, ok := mod.(*object.PyObj)
	if !ok {
		t.Fatalf("import 未返回句柄: %#v", mod)
	}
	if idx := int(p.ID >> 48); idx != 0 {
		t.Fatalf("pool=1 时句柄 #%d 的 worker 位 = %d，期望 0（必须与第32轮 id 完全一致）", p.ID, idx)
	}
}

func TestPoolHandleRouting(t *testing.T) {
	c := newPoolClient(t, 4)

	// 第34轮：import 只在「新模块」上轮询；同名模块亲和，故用 4 个不同模块占满 4 个 worker。
	modNames := []string{"json", "os", "time", "math"}
	handles := make([]*object.PyObj, 4)
	for i, name := range modNames {
		mod, err := c.Import(name)
		if err != nil {
			t.Fatalf("第 %d 次 import %s: %v", i, name, err)
		}
		p, ok := mod.(*object.PyObj)
		if !ok {
			t.Fatalf("import 未返回句柄: %#v", mod)
		}
		if got := int(p.ID >> 48); got != i {
			t.Fatalf("第 %d 次 import %s 落在 worker %d（句柄 #%d），期望轮询到 %d", i, name, got, p.ID, i)
		}
		handles[i] = p
	}

	// 第二轮：同名模块必须回到原 worker（模块亲和），且不重复计数
	for i, name := range modNames {
		mod, err := c.Import(name)
		if err != nil {
			t.Fatalf("重复 import %s: %v", name, err)
		}
		if got := int(mod.(*object.PyObj).ID >> 48); got != i {
			t.Fatalf("重复 import %s 落在 worker %d，期望亲和到 %d", name, got, i)
		}
	}
	for _, st := range c.PoolStats() {
		if st.Modules != 1 {
			t.Fatalf("worker %d 模块数 = %d，期望 1（同名重复 import 不应重复计数）", st.Idx, st.Modules)
		}
		if !st.Alive {
			t.Fatalf("worker %d 应为存活（池内每个 worker 都被路由到过）", st.Idx)
		}
	}

	// 每个 worker 上的句柄都必须能正常 get（回到正确子进程）
	for i, m := range handles {
		name, err := c.GetAttr(m, "__name__")
		if err != nil {
			t.Fatalf("worker %d getattr __name__: %v", i, err)
		}
		if name.Inspect() != modNames[i] {
			t.Fatalf("worker %d 模块名 = %s，期望 %s", i, name.Inspect(), modNames[i])
		}
	}

	// call 链走 worker 0（json）：dumps 必须正常抵达子进程
	fn, err := c.GetAttr(handles[0], "dumps")
	if err != nil {
		t.Fatalf("worker 0 getattr dumps: %v", err)
	}
	f, ok := fn.(*object.PyObj)
	if !ok || !f.Callable {
		t.Fatalf("worker 0 dumps 不是可调用句柄: %#v", fn)
	}
	arr := &object.Array{Elements: []object.Object{&object.Integer{Value: 7}}}
	out, err := c.Call(f, []object.Object{arr}, nil)
	if err != nil {
		t.Fatalf("worker 0 call: %v", err)
	}
	if out.Inspect() != "[7]" {
		t.Fatalf("worker 0 json.dumps = %s，期望 [7]", out.Inspect())
	}

	// 释放后该 worker 上再取属性必须失败（句柄已从子进程句柄表摘除）
	if err := c.Release(handles[2]); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := c.GetAttr(handles[2], "__name__"); err == nil {
		t.Fatal("已释放的句柄不应再可用")
	}
	// 其他 worker 不受影响
	if _, err := c.GetAttr(handles[3], "__name__"); err != nil {
		t.Fatalf("worker 3 被误伤: %v", err)
	}
}

func TestPoolCrossWorkerHandleRejected(t *testing.T) {
	c := newPoolClient(t, 2)

	m0, err := c.Import("json") // worker 0
	if err != nil {
		t.Fatal(err)
	}
	m1, err := c.Import("os") // worker 1（第34轮：不同模块才轮询到不同 worker）
	if err != nil {
		t.Fatal(err)
	}
	if int(m0.(*object.PyObj).ID>>48) == int(m1.(*object.PyObj).ID>>48) {
		t.Fatalf("两个不同模块应落在不同 worker，实际都是 %d", int(m0.(*object.PyObj).ID>>48))
	}
	fn0, err := c.GetAttr(m0.(*object.PyObj), "dumps")
	if err != nil {
		t.Fatal(err)
	}
	// 把 worker 1 的模块句柄当参数传给 worker 0 的调用：必须明确报错，不能把大 id 塞给子进程
	_, err = c.Call(fn0.(*object.PyObj), []object.Object{m1}, nil)
	if err == nil {
		t.Fatal("跨 worker 句柄应当报错")
	}
	if !contains(err.Error(), "跨") {
		t.Fatalf("错误信息应说明跨 worker 不通用，实际: %v", err)
	}
	// 同 worker 的句柄传参不受影响（json.dumps 对 module 抛 TypeError，说明确实送到了子进程）
	_, err = c.Call(fn0.(*object.PyObj), []object.Object{m0}, nil)
	if err == nil || !contains(err.Error(), "TypeError") {
		t.Fatalf("同 worker 句柄应正常抵达子进程（TypeError），实际: %v", err)
	}
}

func TestPoolWorkerRestartInvalidatesHandle(t *testing.T) {
	c := newPoolClient(t, 2)

	m, err := c.Import("os") // worker 0
	if err != nil {
		t.Fatal(err)
	}
	// 杀掉主通道子进程（Exec 允许重启重试，第二次仍会自杀 → 报错）
	if err := c.Exec("import os; os._exit(1)"); err == nil {
		t.Fatal("杀进程应当报错")
	}
	// 重启后主通道可用
	v, err := c.Eval("1+1")
	if err != nil {
		t.Fatalf("重启后求值失败: %v", err)
	}
	if v.Inspect() != "2" {
		t.Fatalf("重启后 1+1 = %s", v.Inspect())
	}
	// 旧句柄必须失效（防 ABA）
	if _, err := c.GetAttr(m.(*object.PyObj), "name"); err == nil || !contains(err.Error(), "失效") {
		t.Fatalf("旧句柄应报失效，实际: %v", err)
	}

	// 亲和关系跨重启保留：重导入 os 仍回 worker 0，新进程里重新加载后可用
	mo, err := c.Import("os")
	if err != nil {
		t.Fatal(err)
	}
	po := mo.(*object.PyObj)
	if got := int(po.ID >> 48); got != 0 {
		t.Fatalf("重启后 import os 落在 worker %d，期望亲和到 0", got)
	}
	if _, err := c.GetAttr(po, "name"); err != nil {
		t.Fatalf("重启后重新导入的 os 不可用: %v", err)
	}

	// 另一 worker 不受影响：新模块轮询到 worker 1，句柄可正常使用
	m1, err := c.Import("sys")
	if err != nil {
		t.Fatal(err)
	}
	p1 := m1.(*object.PyObj)
	if got := int(p1.ID >> 48); got != 1 {
		t.Fatalf("重启后新模块 import 落在 worker %d，期望轮询到 1", got)
	}
	if _, err := c.GetAttr(p1, "version"); err != nil {
		t.Fatalf("worker 1 被误伤: %v", err)
	}
}

// TestPoolParallelSpeedup 验收判据 3：ZUZO_PY_POOL=4 时双 goroutine 吞吐 ≥ 1.6× 单 worker。
// 用「远端 sleep 2ms」做钟摆：Go 侧开销可忽略，ratio 直接反映两个子进程是否真并行。
func TestPoolParallelSpeedup(t *testing.T) {
	if testing.Short() {
		t.Skip("-short 跳过耗时并发基准")
	}
	c := newPoolClient(t, 4)

	const iters = 30
	const nap = 0.002

	// 第34轮模块亲和：同名模块恒在同一 worker，所以两个「睡眠入口」必须来自不同模块：
	//   worker 0 → time.sleep(秒)；worker 1 → importlib.import_module("time").sleep(秒)
	// （不用 select.select / Event.wait：Windows 上前者要先 WSAStartup 报 WinError 10093，
	//   后者受系统时钟粒度限成 ~15ms，会把并行度测成 0.3×）
	timeMod, err := c.Import("time")
	if err != nil {
		t.Fatal(err)
	}
	sleepFn, err := c.GetAttr(timeMod.(*object.PyObj), "sleep")
	if err != nil {
		t.Fatal(err)
	}
	sleep0 := sleepFn.(*object.PyObj)
	args0 := func() []object.Object { return []object.Object{&object.Float{Value: nap}} }

	ilMod, err := c.Import("importlib")
	if err != nil {
		t.Fatal(err)
	}
	importModule, err := c.GetAttr(ilMod.(*object.PyObj), "import_module")
	if err != nil {
		t.Fatal(err)
	}
	timeMod1, err := c.Call(importModule.(*object.PyObj), []object.Object{&object.String{Value: "time"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	tm1, ok := timeMod1.(*object.PyObj)
	if !ok {
		t.Fatalf("import_module(\"time\") 未返回句柄: %#v", timeMod1)
	}
	waitFn, err := c.GetAttr(tm1, "sleep")
	if err != nil {
		t.Fatal(err)
	}
	sleep1 := waitFn.(*object.PyObj)
	args1 := func() []object.Object { return []object.Object{&object.Float{Value: nap}} }
	if int(sleep0.ID>>48) != 0 || int(sleep1.ID>>48) != 1 {
		t.Fatalf("睡眠句柄未落在不同 worker: #%d / #%d", sleep0.ID, sleep1.ID)
	}

	one := func(fn *object.PyObj, args []object.Object) error {
		_, err := c.Call(fn, args, nil)
		return err
	}
	serial := func() time.Duration {
		st := time.Now()
		for i := 0; i < iters; i++ {
			if err := one(sleep0, args0()); err != nil {
				t.Fatalf("serial sleep: %v", err)
			}
		}
		return time.Since(st)
	}
	parallel := func() time.Duration {
		st := time.Now()
		var wg sync.WaitGroup
		wg.Add(2)
		run := func(fn *object.PyObj, args []object.Object) {
			defer wg.Done()
			for i := 0; i < iters/2; i++ {
				if err := one(fn, args); err != nil {
					t.Errorf("parallel sleep: %v", err)
					return
				}
			}
		}
		go run(sleep0, args0())
		go run(sleep1, args1())
		wg.Wait()
		return time.Since(st)
	}

	// 各跑两轮取最好值，抵消调度抖动
	s1, p1 := serial(), parallel()
	s2, p2 := serial(), parallel()
	if s2 < s1 {
		s1 = s2
	}
	if p2 < p1 {
		p1 = p2
	}
	ratio := float64(s1) / float64(p1)
	t.Logf("单 worker %s / 双 worker %s = %.2f×", s1, p1, ratio)
	if ratio < 1.6 {
		t.Fatalf("双 goroutine 吞吐仅 %.2f×（< 1.6×），连接池未真正并行", ratio)
	}
}

// TestPoolModuleAffinity 第34轮：ZUZO_PY_POOL=4 下连续 3 次 Import("math") 必须落在同一 worker。
func TestPoolModuleAffinity(t *testing.T) {
	c := newPoolClient(t, 4)

	first := -1
	for i := 0; i < 3; i++ {
		mod, err := c.Import("math")
		if err != nil {
			t.Fatalf("第 %d 次 import math: %v", i, err)
		}
		p, ok := mod.(*object.PyObj)
		if !ok {
			t.Fatalf("import 未返回句柄: %#v", mod)
		}
		idx := int(p.ID >> 48)
		if first < 0 {
			first = idx
		} else if idx != first {
			t.Fatalf("第 %d 次 import math 落在 worker %d，期望亲和到 %d（同名模块不得散到多个子进程）", i, idx, first)
		}
		// 句柄必须真的能用在那个子进程上
		pi, err := c.GetAttr(p, "pi")
		if err != nil {
			t.Fatalf("第 %d 次 math.pi 取不到: %v", i, err)
		}
		if f, ok := pi.(*object.Float); !ok || f.Value < 3.14 {
			t.Fatalf("math.pi 异常: %#v", pi)
		}
	}

	// PoolStats 佐证：只有亲和 worker 有模块，其余 worker 不应被偷偷加载一份
	stats := c.PoolStats()
	if len(stats) != 4 {
		t.Fatalf("PoolStats 长度 = %d，期望 4", len(stats))
	}
	for _, st := range stats {
		if st.Idx == first {
			if st.Modules != 1 {
				t.Fatalf("worker %d 模块数 = %d，期望 1", st.Idx, st.Modules)
			}
			if !st.Alive || st.Calls == 0 {
				t.Fatalf("worker %d 状态异常: %+v", st.Idx, st)
			}
			continue
		}
		if st.Modules != 0 {
			t.Fatalf("worker %d 也加载了模块（模块数 %d），亲和失效", st.Idx, st.Modules)
		}
	}
}

// TestPoolImportLoadsOnce 第34轮：并发同名导入只加载一份（PoolStats().Modules 总和 = 1）。
func TestPoolImportLoadsOnce(t *testing.T) {
	c := newPoolClient(t, 4)

	const n = 4
	var wg sync.WaitGroup
	errs := make([]error, n)
	idxs := make([]int, n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			mod, err := c.Import("math")
			if err != nil {
				errs[i] = err
				return
			}
			idxs[i] = int(mod.(*object.PyObj).ID >> 48)
		}(i)
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("第 %d 个并发 import 失败: %v", i, err)
		}
	}
	for i := 1; i < n; i++ {
		if idxs[i] != idxs[0] {
			t.Fatalf("并发 import math 散到了多个 worker: %v（并发同名导入必须合并到同一 worker）", idxs)
		}
	}

	total := 0
	for _, st := range c.PoolStats() {
		total += st.Modules
	}
	if total != 1 {
		t.Fatalf("PoolStats().Modules 总和 = %d，期望 1（同名模块只加载一份）", total)
	}
}

// BenchmarkEvalSerial 单 worker 往返吞吐（基线）
func BenchmarkEvalSerial(b *testing.B) {
	b.Setenv("ZUZO_PY_POOL", "1")
	c := New()
	c.SetNotice(nil)
	if err := c.Start(); err != nil {
		b.Skipf("无 CPython: %v", err)
	}
	defer c.Close()

	mod, err := c.Import("json")
	if err != nil {
		b.Fatal(err)
	}
	fn, err := c.GetAttr(mod.(*object.PyObj), "dumps")
	if err != nil {
		b.Fatal(err)
	}
	dumps := fn.(*object.PyObj)
	arg := &object.Array{Elements: []object.Object{
		&object.Integer{Value: 1}, &object.String{Value: "x"}, &object.Boolean{Value: true},
	}}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := c.Call(dumps, []object.Object{arg}, nil); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEvalParallel 4 worker + RunParallel（每个 goroutine 各自 import → 各自 worker）
func BenchmarkEvalParallel(b *testing.B) {
	b.Setenv("ZUZO_PY_POOL", "4")
	c := New()
	c.SetNotice(nil)
	if err := c.Start(); err != nil {
		b.Skipf("无 CPython: %v", err)
	}
	defer c.Close()

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		mod, err := c.Import("json")
		if err != nil {
			b.Errorf("import: %v", err)
			return
		}
		p, ok := mod.(*object.PyObj)
		if !ok {
			b.Errorf("import 未返回句柄: %#v", mod)
			return
		}
		fn, err := c.GetAttr(p, "dumps")
		if err != nil {
			b.Errorf("getattr: %v", err)
			return
		}
		dumps := fn.(*object.PyObj)
		arg := &object.Array{Elements: []object.Object{
			&object.Integer{Value: 1}, &object.String{Value: "x"}, &object.Boolean{Value: true},
		}}
		for pb.Next() {
			if _, err := c.Call(dumps, []object.Object{arg}, nil); err != nil {
				b.Errorf("call: %v", err)
				return
			}
		}
	})
}
