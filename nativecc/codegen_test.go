package nativecc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func buildAndRun(t *testing.T, src string, wantCode int) {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, "t.exe")
	if err := CompileToExe([]Source{{Name: "t.c", Code: src}}, exe, Options{Verbose: true}); err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	cmd := exec.Command(exe)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Fatalf("执行失败: %v (输出 %q)", err, string(out))
	}
	if code != wantCode {
		t.Fatalf("退出码 = %d，期望 %d；输出 %q", code, wantCode, string(out))
	}
}

func TestReturnConst(t *testing.T) {
	buildAndRun(t, `int main(int argc, char** argv) { return 42; }`, 42)
}

func TestArith(t *testing.T) {
	buildAndRun(t, `int main(int argc, char** argv) { int a = 3; int b = 4; return a * b + 2; }`, 14)
}

func TestIfElse(t *testing.T) {
	buildAndRun(t, `int main(int argc, char** argv) { int a = 10; if (a > 5) { return 7; } else { return 9; } }`, 7)
}

func TestLoop(t *testing.T) {
	buildAndRun(t, `int main(int argc, char** argv) { int s = 0; int i = 1; while (i <= 10) { s = s + i; i++; } return s; }`, 55)
}

func TestFuncCall(t *testing.T) {
	buildAndRun(t, `
int add(int a, int b) { return a + b; }
int main(int argc, char** argv) { return add(20, 22); }
`, 42)
}

func TestWriteString(t *testing.T) {
	src := `
int puts(const char* s);
int main(int argc, char** argv) {
    puts("你好 nativecc");
    return 3;
}
`
	dir := t.TempDir()
	exe := filepath.Join(dir, "s.exe")
	if err := CompileToExe([]Source{{Name: "s.c", Code: src}}, exe, Options{Verbose: true}); err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	cmd := exec.Command(exe)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, ok := err.(*exec.ExitError); !ok {
			t.Fatalf("执行失败: %v", err)
		}
	}
	if string(out) != "你好 nativecc\n" && string(out) != "你好 nativecc\r\n" {
		t.Fatalf("输出 = %q", string(out))
	}
	_ = os.Stdout
}

func buildRunOut(t *testing.T, name, src string) (int, string) {
	t.Helper()
	dir := t.TempDir()
	exe := filepath.Join(dir, name+".exe")
	if err := CompileToExe([]Source{{Name: name + ".c", Code: src}}, exe, Options{}); err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	return runExe(t, exe)
}
