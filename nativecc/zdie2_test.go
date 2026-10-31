package nativecc

import "testing"

func TestZDieMini(t *testing.T) {
	code, out := buildRunOut(t, "zd1", `
#include <stdio.h>
#include <stdlib.h>
void z_die(const char* code, const char* msg) {
    char buf[1024];
    if (code && code[0]) { snprintf(buf, sizeof(buf), "错误 %s: %s", code, msg); }
    else { snprintf(buf, sizeof(buf), "错误: %s", msg); }
    fputs(buf, stderr);
    fputc('\n', stderr);
    exit(1);
}
int main(int argc, char** argv) { z_die("E210", "整数溢出"); return 0; }
`)
	t.Logf("code=%d out=%q", code, out)
}

func TestZDieMini2(t *testing.T) {
	code, out := buildRunOut(t, "zd2", `
#include <stdio.h>
#include <stdlib.h>
int main(int argc, char** argv) {
    char buf[1024];
    snprintf(buf, sizeof(buf), "错误 %s: %s", "E210", "整数溢出");
    printf("[%s]\n", buf);
    return 0;
}
`)
	t.Logf("code=%d out=%q", code, out)
}
