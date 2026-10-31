package nativecc

import "testing"

func TestZLt(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal a = z_int(95LL);
    ZVal b = z_int(100LL);
    z_print(2, (ZVal[]){z_lt(a, b), z_lt(b, a)});
    return 0;
}
`
	exe := compileWithRT(t, "zlt", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, out)
}

func TestZWhile(t *testing.T) {
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
    return 0;
}
`
	exe := compileWithRT(t, "zwhile", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, out)
}
