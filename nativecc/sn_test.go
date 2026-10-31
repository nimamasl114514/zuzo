package nativecc

import "testing"

func TestSnprintfStr(t *testing.T) {
	code, out := buildRunOut(t, "sn1", `
#include <stdio.h>
int main(int argc, char** argv) {
    char buf[128];
    snprintf(buf, sizeof(buf), "错误 %s: %s", "E210", "整数溢出");
    printf("[%s]\n", buf);
    snprintf(buf, sizeof(buf), "%s|%s", "aa", "bb");
    printf("[%s]\n", buf);
    return 0;
}
`)
	t.Logf("code=%d out=%q", code, out)
}
