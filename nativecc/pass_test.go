package nativecc

import "testing"

func TestPtrParam(t *testing.T) {
	code, out := buildRunOut(t, "pp1", `
int printf(const char* fmt, ...);
void f(const char* code) { printf("[%s]\n", code); }
int main(int argc, char** argv) { f("ABCD"); return 0; }
`)
	t.Logf("pp1: code=%d out=%q", code, out)
}

func TestBigArray(t *testing.T) {
	code, out := buildRunOut(t, "pp2", `
int printf(const char* fmt, ...);
int main(int argc, char** argv) {
    char buf[1024];
    int n = 7;
    buf[0] = 'A';
    buf[1] = 0;
    printf("[%s] n=%d\n", buf, n);
    return 0;
}
`)
	t.Logf("pp2: code=%d out=%q", code, out)
}

func TestSmallBufSn(t *testing.T) {
	code, out := buildRunOut(t, "pp3", `
#include <stdio.h>
int printf(const char* fmt, ...);
void f(const char* code) {
    char buf[32];
    snprintf(buf, sizeof(buf), "X %s Y", code);
    printf("[%s]\n", buf);
}
int main(int argc, char** argv) { f("ABCD"); return 0; }
`)
	t.Logf("pp3: code=%d out=%q", code, out)
}
