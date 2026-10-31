package nativecc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func compileZuzoCase(t *testing.T, cfile string) (string, error) {
	t.Helper()
	dir := filepath.Dir(cfile)
	mainSrc, err := os.ReadFile(cfile)
	if err != nil {
		return "", err
	}
	rtSrc, err := os.ReadFile(filepath.Join(dir, "zuzo_rt.c"))
	if err != nil {
		return "", err
	}
	exe := strings.TrimSuffix(cfile, ".c") + ".exe"
	err = CompileToExe([]Source{
		{Name: filepath.Join(dir, "t01.c"), Code: string(mainSrc)},
		{Name: filepath.Join(dir, "zuzo_rt.c"), Code: string(rtSrc)},
	}, exe, Options{Verbose: true})
	return exe, err
}

func TestRealT01(t *testing.T) {
	exe, err := compileZuzoCase(t, "../tmp/nc/t01.c")
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	cmd := exec.Command(exe)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	} else if err != nil {
		t.Logf("执行错误: %v", err)
	}
	t.Logf("退出码=%d 输出=%q", code, string(out))
}
