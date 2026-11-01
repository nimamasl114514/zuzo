package pyext

// pyext_test.go — 桥的 Go 侧单测与基准（第32轮）
//
// 三条约束：
//  1. 不依赖具体 Python 版本；只用标准库 json / sys。
//  2. 机器上没有 CPython 时 Skip，不让 CI 红。
//  3. 协议编解码是纯函数，必须无条件可测（不启子进程）。

import (
	"encoding/json"
	"testing"

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
