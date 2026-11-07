package llvmbackend

// 第33轮 · w5：LLVM 后端对 py* 内置（真实 CPython 子进程桥）必须给出可读的编译期错误。
//
// 背景：第32轮引入的 14 个 py* 内置依赖 pyext/ 的外部 CPython 进程与握手协议，
// 既无法降级成静态 LLVM IR，也没有等价的 native 符号。若不显式拦截，会在
// 链接/符号解析阶段抛「未定义的函数: py导入」这类对用户毫无帮助的内部错误。
//
// 本文件锁死三件事：
//   1. 14 个名字一个不少地进了 unsupportedBuiltins（防止后续新增内置时漏登记）；
//   2. checkUnsupported 的文案包含定位信息与三要素（CPython 子进程桥 / LLVM 后端不支持 / 请用解释器执行）；
//   3. 走完整 EmitLLVM 链路时返回 error 而不是 panic 或产出未定义符号。

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pyBridgeBuiltins 第32轮引入的全部 14 个 CPython 子进程桥内置。
// 顺序与 evaluator/evaluator.go 的注册表一致，便于逐条核对。
var pyBridgeBuiltins = []string{
	"py导入", "py取", "py设", "py调", "py调参", "py方法", "py取值",
	"py描述", "py类型", "py释放", "py模式", "py信息", "py有库", "py安装",
}

const pyBridgeReason = "CPython 子进程桥"

// TestUnsupportedBuiltinsCoversPyBridge 表完整性：14 个名字逐个登记，且 reason 统一。
func TestUnsupportedBuiltinsCoversPyBridge(t *testing.T) {
	if len(pyBridgeBuiltins) != 14 {
		t.Fatalf("py* 内置清单应为 14 个，实际 %d 个", len(pyBridgeBuiltins))
	}
	for _, name := range pyBridgeBuiltins {
		reason, ok := unsupportedBuiltins[name]
		if !ok {
			t.Errorf("%s() 未登记进 unsupportedBuiltins，LLVM 后端会编出未定义符号", name)
			continue
		}
		if reason != pyBridgeReason {
			t.Errorf("%s() 的 reason = %q，期望 %q", name, reason, pyBridgeReason)
		}
	}
}

// TestCheckUnsupportedPyBridgeMessage 文案：行列号 + 名字 + 三要素齐全。
func TestCheckUnsupportedPyBridgeMessage(t *testing.T) {
	g := &Gen{}
	for _, name := range pyBridgeBuiltins {
		err := g.checkUnsupported(name, 7, 3)
		if err == nil {
			t.Fatalf("%s() 应被 checkUnsupported 拒绝，实际通过", name)
		}
		msg := err.Error()
		// 定位信息：行:列: 名字()
		if !strings.Contains(msg, "7:3: "+name+"()") {
			t.Errorf("%s() 错误缺少定位信息，实际: %s", name, msg)
		}
		// 三要素
		for _, want := range []string{pyBridgeReason, "LLVM 后端不支持", "请用解释器执行"} {
			if !strings.Contains(msg, want) {
				t.Errorf("%s() 错误缺少 %q，实际: %s", name, want, msg)
			}
		}
	}
}

// TestCheckUnsupportedAllowsSupportedBuiltins 反向保险：受支持的内置不能被误伤。
func TestCheckUnsupportedAllowsSupportedBuiltins(t *testing.T) {
	g := &Gen{}
	for _, name := range []string{"打印", "长度", "格式化", "读文件", "sha256"} {
		if err := g.checkUnsupported(name, 1, 1); err != nil {
			t.Errorf("%s() 是受支持的内置，不应被拒绝: %v", name, err)
		}
	}
}

// TestEmitLLVMRejectsPyBuiltin 端到端：完整生成链路返回可读错误，而不是 panic / 未定义符号。
func TestEmitLLVMRejectsPyBuiltin(t *testing.T) {
	cases := []struct {
		desc string
		src  string
		name string
	}{
		{"变量初始化位置", "变量 np = py导入(\"numpy\")\n打印(np)\n", "py导入"},
		{"表达式语句位置", "py安装(\"requests\")\n", "py安装"},
		{"条件分支内", "如果 1 > 0 {\n    变量 t = py类型(1)\n    打印(t)\n}\n", "py类型"},
	}
	for _, c := range cases {
		t.Run(c.desc, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "t.hy")
			if err := os.WriteFile(path, []byte(c.src), 0o644); err != nil {
				t.Fatalf("写临时源码失败: %v", err)
			}
			var (
				ir  string
				err error
			)
			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("EmitLLVM 对 %s() 发生 panic: %v", c.name, r)
					}
				}()
				ir, _, err = EmitLLVM(path)
			}()
			if err == nil {
				t.Fatalf("EmitLLVM 对 %s() 应返回错误，实际成功（IR 长度 %d）", c.name, len(ir))
			}
			if ir != "" {
				t.Errorf("被拒绝时不应产出 IR，实际 %d 字节", len(ir))
			}
			msg := err.Error()
			// 三要素至少命中两项
			hits := 0
			for _, want := range []string{pyBridgeReason, "LLVM 后端不支持", "请用解释器执行"} {
				if strings.Contains(msg, want) {
					hits++
				}
			}
			if hits < 2 {
				t.Fatalf("%s() 的错误信息可读性不足（命中 %d/3 要素）: %s", c.name, hits, msg)
			}
			// 必须点出具体是哪个内置，且不能是「未定义的函数」这类内部错
			if !strings.Contains(msg, c.name+"()") {
				t.Errorf("%s() 错误未点名内置，实际: %s", c.name, msg)
			}
			if strings.Contains(msg, "未定义的函数") {
				t.Errorf("%s() 仍走链接期兜底错误，应被 checkUnsupported 提前拦截: %s", c.name, msg)
			}
		})
	}
}
