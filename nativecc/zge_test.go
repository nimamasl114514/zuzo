package nativecc

import "testing"

func TestZGe(t *testing.T) {
	exe := compileWithRT(t, "zge", `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    z_print(3, (ZVal[]){z_ge(z_int(4LL), z_int(5LL)), z_ge(z_int(5LL), z_int(5LL)), z_ge(z_int(6LL), z_int(5LL))});
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("zge code=%d out=%q", c, o)
}

func TestZBreak(t *testing.T) {
	exe := compileWithRT(t, "zbrk", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal n = z_int(0LL);
    while (z_truthy(z_bool(1))) {
        n = z_add(n, z_int(1LL));
        if (z_truthy(z_ge(n, z_int(5LL)))) { break; }
    }
    printf("n=%lld\n", n.as.i);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("zbrk code=%d out=%q", c, o)
}
