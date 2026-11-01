package llvmbackend

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// emitFor 把源码写成临时 .hy 并生成 IR 文本。
func emitFor(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "t.hy")
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatalf("写临时源码失败: %v", err)
	}
	ir, _, err := EmitLLVM(path)
	if err != nil {
		t.Fatalf("生成 IR 失败: %v", err)
	}
	return ir
}

func mustContain(t *testing.T, ir, want string) {
	t.Helper()
	if !strings.Contains(ir, want) {
		t.Fatalf("IR 缺少片段 %q\n---- IR 前 2000 字 ----\n%s", want, head(ir, 2000))
	}
}

func mustNotContain(t *testing.T, ir, bad string) {
	t.Helper()
	if strings.Contains(ir, bad) {
		t.Fatalf("IR 不应包含 %q", bad)
	}
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

// 1) 模块骨架 + 运行时 ABI 声明 + 浮点字面量。
func TestModuleABIAndFloat(t *testing.T) {
	ir := emitFor(t, "打印(1)\n打印(2.0)\n打印(\"hi\")\n")
	mustContain(t, ir, "%ZVal = type { i32, i64 }")
	mustContain(t, ir, "target triple = \""+hostTriple()+"\"")
	// ZVal 返回值走 sret、参数走 ptr（Win64 MSVC 降级口径）
	mustContain(t, ir, "declare void @z_int(ptr sret(%ZVal), i64)")
	mustContain(t, ir, "declare void @z_print(i32, ptr)")
	// 浮点必须带小数点，否则 clang 报 integer constant must have integer type
	mustContain(t, ir, "double 2.0")
	mustNotContain(t, ir, "double 2)")
	// 字符串常量池
	mustContain(t, ir, "@.str.0 = private unnamed_addr constant")
}

// 2) 执行/遇到：_setjmp 两参 + jmp_buf 16 字节对齐 + 帧链全局。
func TestTryCatchIR(t *testing.T) {
	ir := emitFor(t, "执行 {\n变量 x = 10 / 0\n打印(x)\n} 遇到 e {\n打印(错误码(e))\n}\n")
	mustContain(t, ir, "%ZTryFrame = type { [256 x i8], ptr }")
	mustContain(t, ir, "@z_try_top = external global ptr")
	// Win64 的 _setjmp 是两参 (buf, frameaddress)，只传一个会在 longjmp 时崩
	mustContain(t, ir, "declare i32 @_setjmp(ptr, ptr) returns_twice")
	mustContain(t, ir, "call ptr @llvm.frameaddress.p0(i32 0)")
	// MSVC _setjmp 用 movaps 保存 XMM，帧必须 16 字节对齐
	mustContain(t, ir, "alloca %ZTryFrame, align 16")
	mustContain(t, ir, "call void @z_error_take")
}

// 3) 闭包捕获 + GC 安全点。
func TestClosureAndSafepoint(t *testing.T) {
	ir := emitFor(t, `函数 计数器() {
    变量 n = 0
    函数 递增() {
        n = n + 1
        返回 n
    }
    返回 递增
}
变量 c = 计数器()
打印(c())
`)
	mustContain(t, ir, "call ptr @z_new_env_ex(")
	mustContain(t, ir, "call void @z_closure(")
	mustContain(t, ir, "call void @z_gc_safepoint(")
}

// 4) 目标三元组可覆盖（跨平台只影响 IR 生成）。
func TestTargetOverride(t *testing.T) {
	t.Setenv("ZUZO_TARGET", "x86_64-unknown-linux-gnu")
	ir := emitFor(t, "打印(1)\n")
	mustContain(t, ir, "target triple = \"x86_64-unknown-linux-gnu\"")
}

// 5) 端到端冒烟：IR → clang → exe → 运行输出。
func TestBuildExeSmoke(t *testing.T) {
	if _, err := FindClang(); err != nil {
		t.Skip("本机没有 clang，跳过 exe 冒烟")
	}
	if runtime.GOOS != "windows" {
		t.Skip("冒烟用例按 Windows 宿主编写")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "hello.hy")
	if err := os.WriteFile(src, []byte("打印(\"llvm-ok\")\n"), 0o644); err != nil {
		t.Fatalf("写源码失败: %v", err)
	}
	_, exe, err := BuildExe(src, filepath.Join(dir, "hello.exe"))
	if err != nil {
		t.Fatalf("BuildExe 失败: %v", err)
	}
	out, err := exec.Command(exe).CombinedOutput()
	if err != nil {
		t.Fatalf("运行产物失败: %v (%s)", err, string(out))
	}
	if got := strings.TrimSpace(string(out)); got != "llvm-ok" {
		t.Fatalf("产物输出 = %q，期望 llvm-ok", got)
	}
}
