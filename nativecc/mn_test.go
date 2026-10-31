package nativecc

import "testing"

// W = z_add 结果直接打印（无 w 变量）
func TestMnA(t *testing.T) {
	exe := compileWithRT(t, "mnA", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    ZVal i = z_int(0LL);
    ZVal s = z_int(0LL);
    printf("w=%lld\n", z_add(s, i).as.i);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("mnA: code=%d out=%q", c, o)
}

// W = z_add 赋给变量
func TestMnB(t *testing.T) {
	exe := compileWithRT(t, "mnB", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    ZVal i = z_int(0LL);
    ZVal s = z_int(0LL);
    ZVal w = z_add(s, i);
    printf("w=%lld\n", w.as.i);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("mnB: code=%d out=%q", c, o)
}

// 加 3 变量
func TestMnC(t *testing.T) {
	exe := compileWithRT(t, "mnC", `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    ZVal i = z_int(0LL);
    ZVal s = z_int(0LL);
    ZVal w;
    w = z_add(s, i);
    printf("w=%lld\n", w.as.i);
    return 0;
}
`)
	c, o := runExe(t, exe)
	t.Logf("mnC: code=%d out=%q", c, o)
}
