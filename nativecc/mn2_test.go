package nativecc

import "testing"

func TestMn2(t *testing.T) {
	exe := compileWithRT(t, "mn2", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    ZVal i = z_int(0LL);
    ZVal s = z_int(0LL);
    printf("i.as.i=%lld s.as.i=%lld\n", i.as.i, s.as.i);
    ZVal w = z_add(s, i);
    printf("after: i=%lld s=%lld w=%lld k=%d\n", i.as.i, s.as.i, w.as.i, (int)w.kind);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("mn2: code=%d out=%q", c, o)
}

func TestMn3(t *testing.T) {
	exe := compileWithRT(t, "mn3", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    ZVal i = z_int(0LL);
    ZVal s = z_int(0LL);
    printf("i=%p s=%p\n", (void*)&i, (void*)&s);
    printf("diff=%d\n", (int)((char*)&i - (char*)&s));
    printf("i=%lld s=%lld\n", i.as.i, s.as.i);
    ZVal w = z_add(s, i);
    printf("after: w=%lld k=%d\n", w.as.i, (int)w.kind);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("mn3: code=%d out=%q", c, o)
}
