package nativecc

import "testing"

func TestCJKVar(t *testing.T) {
	exe := compileWithRT(t, "cjk1", `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal zv_i = z_int(0LL);
    ZVal zv_总 = z_int(0LL);
    while (z_truthy(z_lt(zv_i, z_int(100LL)))) {
        zv_总 = z_add(zv_总, zv_i);
        zv_i = z_add(zv_i, z_int(1LL));
    }
    z_print(2, (ZVal[]){zv_总, zv_i});
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("cjk1: code=%d out=%q", c, o)
}

// 与 TestCJKVar 代码完全等价但变量名为 ASCII
func TestAsciiVar(t *testing.T) {
	exe := compileWithRT(t, "cjk2", `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal zi = z_int(0LL);
    ZVal zsum = z_int(0LL);
    while (z_truthy(z_lt(zi, z_int(100LL)))) {
        zsum = z_add(zsum, zi);
        zi = z_add(zi, z_int(1LL));
    }
    z_print(2, (ZVal[]){zsum, zi});
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("cjk2: code=%d out=%q", c, o)
}
