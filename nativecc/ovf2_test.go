package nativecc

import "testing"

func TestOvflWrap(t *testing.T) {
	code, out := buildRunOut(t, "ovw", `
#include <stdio.h>
static int add_overflow(long long a, long long b, long long* r) {
    int ov = __builtin_add_overflow(a, b, r);
    fprintf(stderr, "[wrap a=%lld b=%lld ov=%d]\n", a, b, ov);
    return ov;
}
int main(int argc, char** argv) {
    long long r;
    printf("call1=%d\n", add_overflow(0, 0, &r));
    printf("r=%lld\n", r);
    printf("call2=%d\n", add_overflow(1, 2, &r));
    printf("r=%lld\n", r);
    return 0;
}
`)
	t.Logf("code=%d out=%q", code, out)
}
