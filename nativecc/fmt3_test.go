package nativecc

import "testing"

func TestStrtod(t *testing.T) {
	code, out := buildRunOut(t, "fmt3", `
#include <stdio.h>
#include <stdlib.h>
int main(int argc, char** argv) {
    double x = 3.14;
    double b = strtod("3.14", NULL);
    printf("b=%.17g x=%.17g\n", b, x);
    if (b == x) { printf("EQ\n"); } else { printf("NEQ\n"); }
    if (b > 3.13) { printf("GT\n"); }
    return 0;
}
`)
	t.Logf("code=%d out=%q", code, out)
}
