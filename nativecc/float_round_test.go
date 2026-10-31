package nativecc

import (
	"strings"
	"testing"
)

func TestZFormatHalfEven(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal out = z_format(2, (ZVal[]){z_str("%.1f"), z_float(2.25)});
    z_print(1, (ZVal[]){out});
    return 0;
}
`
	exe := compileWithRT(t, "float-round", src)
	code, out := runExe(t, exe)
	if code != 0 {
		t.Fatalf("退出码 = %d，输出 %q", code, out)
	}
	if strings.TrimSpace(out) != "2.2" {
		t.Fatalf("半值格式化 = %q，期望 %q", strings.TrimSpace(out), "2.2")
	}
}
