package nativecc

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func compileWithRT(t *testing.T, name, src string) string {
	t.Helper()
	dir := "../tmp/nc"
	rtSrc, err := os.ReadFile(filepath.Join(dir, "zuzo_rt.c"))
	if err != nil {
		t.Fatalf("读运行时失败: %v", err)
	}
	exe := filepath.Join(dir, name+".exe")
	err = CompileToExe([]Source{
		{Name: filepath.Join(dir, name+".c"), Code: src},
		{Name: filepath.Join(dir, "zuzo_rt.c"), Code: string(rtSrc)},
	}, exe, Options{Verbose: true})
	if err != nil {
		t.Fatalf("编译失败: %v", err)
	}
	return exe
}

func runExe(t *testing.T, exe string) (int, string) {
	cmd := exec.Command(exe)
	out, err := cmd.CombinedOutput()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code = ee.ExitCode()
	}
	return code, string(out)
}

func TestMiniInit(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    return 0;
}
`
	exe := compileWithRT(t, "mini1", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, out)
	if code != 0 {
		t.Fatalf("z_rt_init 崩溃")
	}
}

func TestMiniStr(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal s = z_str("hi");
    z_print(1, (ZVal[]){s});
    return 0;
}
`
	exe := compileWithRT(t, "mini2", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, out)
}

func TestMiniBuiltin(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal s = z_str("abc");
    z_print(1, (ZVal[]){z_len(s)});
    return 0;
}
`
	exe := compileWithRT(t, "mini3", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, out)
}
