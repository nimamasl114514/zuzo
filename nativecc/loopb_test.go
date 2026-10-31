package nativecc

import "testing"

func TestLoopB(t *testing.T) {
	exe := compileWithRT(t, "loopb", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal i = z_int(0LL);
    ZVal s = z_int(0LL);
    while (z_truthy(z_lt(i, z_int(100LL)))) {
        if (z_truthy(z_ge(i, z_int(94LL)))) {
            printf("i=%lld s=%lld\n", i.as.i, s.as.i);
        }
        s = z_add(s, i);
        i = z_add(i, z_int(1LL));
    }
    printf("final=%lld %lld\n", i.as.i, s.as.i);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("loopb: code=%d out=%q", c, o)
}
