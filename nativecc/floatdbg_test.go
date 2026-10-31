package nativecc

import "testing"

func TestFloatDebug(t *testing.T) {
	src := `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    double d = 3.14;
    printf("d=%f\n", d);
    if (d > 3.0) { printf("gt\n"); }
    if (d < 4.0) { printf("lt\n"); }
    ZVal v = z_float(d);
    printf("v=%f kind=%d\n", v.as.f, (int)v.kind);
    ZVal w = z_float(3.14);
    printf("w=%f\n", w.as.f);
    return 0;
}
`
	exe := compileWithRT(t, "fdbg", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, out)
}
