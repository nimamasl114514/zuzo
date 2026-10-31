package nativecc

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func genC(t *testing.T, hy string, outC string) bool {
	cmd := exec.Command("../zuzo.exe", "--emit-c", hy, "-o", outC)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Logf("生成 C 失败 %s: %v %s", hy, err, string(out))
		return false
	}
	return true
}

func TestAllCases(t *testing.T) {
	files, _ := filepath.Glob("../tests_c/*.hy")
	if len(files) == 0 {
		t.Skip("没有用例")
	}
	dir := "../tmp/nc/cases"
	rtSrc, _ := os.ReadFile(filepath.Join(dir, "zuzo_rt.c"))
	pass, fail := 0, 0
	for _, hy := range files {
		name := strings.TrimSuffix(filepath.Base(hy), ".hy")
		if strings.HasPrefix(name, "lib_") {
			continue
		}
		outC := filepath.Join(dir, name+".c")
		if !genC(t, hy, outC) {
			fail++
			continue
		}
		mainSrc, err := os.ReadFile(outC)
		if err != nil {
			fail++
			continue
		}
		exe := filepath.Join(dir, name+".exe")
		err = CompileToExe([]Source{
			{Name: outC, Code: string(mainSrc)},
			{Name: filepath.Join(dir, "zuzo_rt.c"), Code: string(rtSrc)},
		}, exe, Options{})
		if err != nil {
			t.Logf("[%s] 编译失败: %v", name, err)
			fail++
			continue
		}
		// 解释器输出
		interpOut, _ := exec.Command("../zuzo.exe", hy).Output()
		// native 输出
		cmd := exec.Command(exe)
		nativeOut, _ := cmd.Output()
		if string(interpOut) == string(nativeOut) {
			pass++
			t.Logf("[%s] PASS", name)
		} else {
			fail++
			t.Logf("[%s] DIFF\n  interp=%q\n  native=%q", name, string(interpOut), string(nativeOut))
		}
	}
	t.Logf("总计 PASS=%d FAIL=%d", pass, fail)
}
