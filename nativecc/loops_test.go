package nativecc

import "testing"

func TestLoops(t *testing.T) {
	exe := compileWithRT(t, "loops", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal i = z_int(0LL);
    ZVal s = z_int(0LL);
    while (z_truthy(z_lt(i, z_int(50LL)))) {
        s = z_add(s, i);
        printf("i=%lld s=%lld\n", i.as.i, s.as.i);
        i = z_add(i, z_int(1LL));
    }
    printf("done i=%lld s=%lld\n", i.as.i, s.as.i);
    return 0;
}
`)
	c, o := runExe(t, exe)
	if len(o) > 800 {
		o = o[len(o)-800:]
	}
	t.Logf("loops: code=%d out=%q", c, o)
}
