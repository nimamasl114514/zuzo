package nativecc

import "testing"

func TestZWhileDbg(t *testing.T) {
	src := `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal i = z_int(0LL);
    while (z_truthy(z_lt(i, z_int(3LL)))) {
        printf("loop i=%lld kind=%d\n", i.as.i, (int)i.kind);
        i = z_add(i, z_int(1LL));
    }
    printf("final=%lld\n", i.as.i);
    return 0;
}
`
	exe := compileWithRT(t, "zdbg", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, out)
}
