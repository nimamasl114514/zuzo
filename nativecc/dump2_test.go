package nativecc

import "testing"

func TestDumpZD(t *testing.T) {
	dumpOne(t, "nc_zd", `
#include <stdio.h>
#include <stdlib.h>
void z_die(const char* code, const char* msg) {
    char buf[1024];
    if (code && code[0]) { snprintf(buf, sizeof(buf), "Y %s %s", code, msg); }
    else { snprintf(buf, sizeof(buf), "Z %s", msg); }
    printf("[%s]\n", buf);
    exit(1);
}
int main(int argc, char** argv) { z_die("E210", "整数溢出+"); return 0; }
`)
}
