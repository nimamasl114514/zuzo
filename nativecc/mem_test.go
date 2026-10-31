package nativecc

import "testing"

func TestMemberRead(t *testing.T) {
	exe := compileWithRT(t, "m1rt", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    ZVal i = z_int(5LL);
    printf("i=%lld k=%d\n", i.as.i, (int)i.kind);
    return 0;
}
`)
	c2, o2 := runExe(t, exe)
	t.Logf("m1rt code=%d out=%q", c2, o2)
}

func TestMemberWhileSimple(t *testing.T) {
	exe := compileWithRT(t, "m2rt", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal i = z_int(0LL);
    while (z_truthy(z_lt(i, z_int(3LL)))) {
        printf("i=%lld\n", i.as.i);
        i = z_add(i, z_int(1LL));
    }
    printf("done i=%lld\n", i.as.i);
    return 0;
}
`)
	c2, o2 := runExe(t, exe)
	t.Logf("m2rt code=%d out=%q", c2, o2)
}
