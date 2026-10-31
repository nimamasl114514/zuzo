package nativecc

import "testing"

func TestT04A(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal zv_i = z_int(0LL);
    ZVal zv_总 = z_int(0LL);
    while (z_truthy(z_lt(zv_i, z_int(100LL)))) {
        zv_总 = z_add(zv_总, zv_i);
        (void)zv_总;
        zv_i = z_add(zv_i, z_int(1LL));
        (void)zv_i;
    }
    z_print(2, (ZVal[]){zv_总, zv_i});
    return 0;
}
`
	exe := compileWithRT(t, "t04a", src)
	c, o := runExe(t, exe)
	t.Logf("A: code=%d out=%q", c, o)
}

func TestT04B(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal i = z_int(0LL);
    ZVal s = z_int(0LL);
    while (z_truthy(z_lt(i, z_int(100LL)))) {
        s = z_add(s, i);
        i = z_add(i, z_int(1LL));
    }
    z_print(2, (ZVal[]){s, i});
    ZVal n = z_int(0LL);
    while (z_truthy(z_bool(1))) {
        n = z_add(n, z_int(1LL));
        if (z_truthy(z_ge(n, z_int(5LL)))) { break; }
    }
    z_print(1, (ZVal[]){n});
    return 0;
}
`
	exe := compileWithRT(t, "t04b", src)
	c, o := runExe(t, exe)
	t.Logf("B: code=%d out=%q", c, o)
}
