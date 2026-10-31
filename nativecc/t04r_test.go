package nativecc

import "testing"

func TestT04Repro(t *testing.T) {
	src := `
#include <stdio.h>
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
    (void)z_null();
    ZVal zv_n = z_int(0LL);
    while (z_truthy(z_bool(1))) {
        zv_n = z_add(zv_n, z_int(1LL));
        (void)zv_n;
        if (z_truthy(z_ge(zv_n, z_int(5LL)))) {
            break;
        }
    }
    z_print(1, (ZVal[]){zv_n});
    (void)z_null();
    return 0;
}
`
	exe := compileWithRT(t, "t04r", src)
	c, o := runExe(t, exe)
	t.Logf("code=%d out=%q", c, o)
}
