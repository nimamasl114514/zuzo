package nativecc

import "testing"

func TestPrintfOut(t *testing.T) {
	code, out := buildRunOut(t, "pf1", `
int printf(const char* fmt, ...);
int main(int argc, char** argv) { double d = 3.14; printf("[%f]\n", d); printf("[%g]\n", d); return 0; }
`)
	t.Logf("code=%d out=%q", code, out)
}

func TestStructMemberRead(t *testing.T) {
	code, out := buildRunOut(t, "pf2", `
int printf(const char* fmt, ...);
typedef struct { int kind; union { long long i; double f; } as; } V;
int main(int argc, char** argv) {
    V v;
    v.kind = 2;
    v.as.f = 3.14;
    printf("kind=%d f=%f\n", v.kind, v.as.f);
    return 0;
}
`)
	t.Logf("code=%d out=%q", code, out)
}
