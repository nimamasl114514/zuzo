package nativecc

import "testing"

func TestLoop100(t *testing.T) {
	exe := compileWithRT(t, "loop100", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal i = z_int(96LL);
    while (z_truthy(z_lt(i, z_int(100LL)))) {
        printf("i=%lld\n", i.as.i);
        i = z_add(i, z_int(1LL));
    }
    printf("final=%lld\n", i.as.i);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("loop100: code=%d out=%q", c, o)
}
