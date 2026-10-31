package nativecc

import "testing"

func TestAddr(t *testing.T) {
	exe := compileWithRT(t, "addr", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    ZVal i = z_int(0LL);
    ZVal s = z_int(0LL);
    printf("i=%p s=%p diff=%d\n", (void*)&i, (void*)&s, (int)((char*)&i - (char*)&s));
    printf("i.as.i=%lld s.as.i=%lld\n", i.as.i, s.as.i);
    ZVal w = z_add(s, i);
    printf("after z_add: i=%lld s=%lld w=%lld kind=%d\n", i.as.i, s.as.i, w.as.i, (int)w.kind);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("addr: code=%d out=%q", c, o)
}
