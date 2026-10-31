package nativecc

import "testing"

func TestSnprintfVariants(t *testing.T) {
	code, out := buildRunOut(t, "fmt1", `
#include <stdio.h>
int main(int argc, char** argv) {
    char buf[64];
    snprintf(buf, sizeof(buf), "%.*g", 3, 3.14);
    printf("A=[%s]\n", buf);
    snprintf(buf, sizeof(buf), "%.17g", 3.14);
    printf("B=[%s]\n", buf);
    snprintf(buf, sizeof(buf), "%f", 3.14);
    printf("C=[%s]\n", buf);
    snprintf(buf, sizeof(buf), "%d", 42);
    printf("D=[%s]\n", buf);
    return 0;
}
`)
	t.Logf("code=%d out=%q", code, out)
}
