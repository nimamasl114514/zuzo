package nativecc

import "testing"

func TestZDie(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    z_die("E210", "整数溢出：+");
    return 0;
}
`
	exe := compileWithRT(t, "zdie", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, string(out))
}

func TestZAdd(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZVal a = z_int(1LL);
    ZVal b = z_int(2LL);
    z_print(1, (ZVal[]){z_add(a, b)});
    return 0;
}
`
	exe := compileWithRT(t, "zadd", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, string(out))
}
