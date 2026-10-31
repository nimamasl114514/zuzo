package nativecc

import "testing"

func TestLoopGe(t *testing.T) {
	exe := compileWithRT(t, "loopge", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal i = z_int(0LL);
    while (z_truthy(z_lt(i, z_int(100LL)))) {
        if (z_truthy(z_ge(i, z_int(94LL)))) {
            printf("i=%lld\n", i.as.i);
        }
        i = z_add(i, z_int(1LL));
    }
    printf("done=%lld\n", i.as.i);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("loopge: code=%d out=%q", c, o)
}

func TestLoopGeNoPrint(t *testing.T) {
	exe := compileWithRT(t, "loopge2", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal i = z_int(0LL);
    while (z_truthy(z_lt(i, z_int(100LL)))) {
        if (z_truthy(z_ge(i, z_int(94LL)))) { /* 空 */ }
        i = z_add(i, z_int(1LL));
    }
    printf("done=%lld\n", i.as.i);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("loopge2: code=%d out=%q", c, o)
}
