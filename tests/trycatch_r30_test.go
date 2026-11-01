package tests

import (
	"strings"
	"testing"
)

// ============================================================
// 第30轮 T30.4：执行/遇到 的运行期错误捕获（解释器口径）
//
// C 后端与 nativecc 的对应覆盖在 tests_c/t21_try.hy：
// run_tests_c.sh / run_tests_native.sh 会把同一份源码的解释器输出与
// 两种编译产物逐字节对拍（当前 20/20 全过）。
// ============================================================

func TestTryCatchRuntimeError(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  string // 期望的 Inspect() 文本；以 ~ 开头表示「包含」
	}{
		{"除零错误码", `执行 { 10 / 0 } 遇到 e { 错误码(e) }`, "E202"},
		{"取模零错误码", `执行 { 10 % 0 } 遇到 e { 错误码(e) }`, "E203"},
		{"错误信息", `执行 { 10 / 0 } 遇到 e { 错误信息(e) }`, "除零错误"},
		{"错误对象类型", `执行 { 10 / 0 } 遇到 e { 类型(e) }`, "错误"},
		{"无错误不进处理分支", `执行 { 42 } 遇到 e { 99 }`, "42"},
		{"函数内错误冒泡", `函数 f() { 返回 1 / 0 } 执行 { f() } 遇到 e { 错误码(e) }`, "E202"},
		{"处理分支可继续抛错", `执行 { 执行 { 1 / 0 } 遇到 e1 { 2 / 0 } } 遇到 e2 { 错误码(e2) }`, "E202"},
		{"错误对象可字符串化", `执行 { 10 / 0 } 遇到 e { 字符串(e) }`, "~E202"},
	}

	for _, c := range cases {
		got := testEval(c.input)
		if got == nil {
			t.Fatalf("%s: 求值返回 nil", c.name)
		}
		if strings.HasPrefix(c.want, "~") {
			if !strings.Contains(got.Inspect(), strings.TrimPrefix(c.want, "~")) {
				t.Errorf("%s: 期望包含 %q，得到 %q", c.name, strings.TrimPrefix(c.want, "~"), got.Inspect())
			}
			continue
		}
		if got.Inspect() != c.want {
			t.Errorf("%s: 期望 %q，得到 %q", c.name, c.want, got.Inspect())
		}
	}
}

// TestTryCatchDoesNotLeakError 确认被捕获的错误不会继续向上冒泡
func TestTryCatchDoesNotLeakError(t *testing.T) {
	got := testEval(`变量 r = 0
执行 { r = 1 / 0 } 遇到 e { r = 7 }
r`)
	if got.Inspect() != "7" {
		t.Fatalf("期望被捕获后继续执行得到 7，得到 %q", got.Inspect())
	}
}
