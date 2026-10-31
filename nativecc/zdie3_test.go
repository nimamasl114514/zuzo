package nativecc

import "testing"

func TestZDieProbe(t *testing.T) {
	code, out := buildRunOut(t, "zd3", `
#include <stdio.h>
#include <stdlib.h>
void z_die(const char* code, const char* msg) {
    printf("P1 code=[%s] msg=[%s]\n", code, msg);
    char buf[1024];
    printf("P2 bufaddr=%p\n", (void*)buf);
    snprintf(buf, sizeof(buf), "X %s", code);
    printf("P3 [%s]\n", buf);
    snprintf(buf, sizeof(buf), "Y %s %s", code, msg);
    printf("P4 [%s]\n", buf);
    exit(1);
}
int main(int argc, char** argv) { z_die("E210", "整数溢出"); return 0; }
`)
	t.Logf("code=%d out=%q", code, out)
}
