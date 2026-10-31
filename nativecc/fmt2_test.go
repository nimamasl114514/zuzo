package nativecc

import "testing"

func TestSnprintfLoop(t *testing.T) {
	code, out := buildRunOut(t, "fmt2", `
#include <stdio.h>
#include <stdlib.h>
int main(int argc, char** argv) {
    double x = 3.14;
    char out[64];
    for (int prec = 1; prec <= 17; prec++) {
        snprintf(out, sizeof(out), "%.*g", prec, x);
        printf("p%d=[%s]\n", prec, out);
        double back = strtod(out, NULL);
        if (back == x) { printf("MATCH at %d\n", prec); return 0; }
    }
    printf("no match, last=[%s]\n", out);
    return 0;
}
`)
	t.Logf("code=%d out=%q", code, out)
}
