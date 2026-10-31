package nativecc

import "testing"

func TestT01a(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    z_print(1, (ZVal[]){z_str("你好，世界！")});
    return 0;
}
`
	exe := compileWithRT(t, "t01a", src)
	code, out := runExe(t, exe)
	t.Logf("a: code=%d out=%q", code, out)
}

func TestT01b(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    z_print(5, (ZVal[]){z_int(42LL), z_float(3.14), z_bool(1), z_bool(0), z_null()});
    return 0;
}
`
	exe := compileWithRT(t, "t01b", src)
	code, out := runExe(t, exe)
	t.Logf("b: code=%d out=%q", code, out)
}

func TestT01c(t *testing.T) {
	src := `
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    z_print(3, (ZVal[]){z_int(1LL), z_int(2LL), z_int(3LL)});
    return 0;
}
`
	exe := compileWithRT(t, "t01c", src)
	code, out := runExe(t, exe)
	t.Logf("c: code=%d out=%q", code, out)
}
