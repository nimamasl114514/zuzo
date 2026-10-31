package nativecc

import (
	"testing"
)

func TestPureIO(t *testing.T) {
	src := `
#include <stdio.h>
#include <string.h>
int main(int argc, char** argv) {
    printf("hello-printf\n");
    fwrite("hello-fwrite\n", 1, 13, stdout);
    fputc('X', stdout);
    fputc('\n', stdout);
    return 0;
}
`
	exe := compileWithRT(t, "io1", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, out)
	if out != "hello-printf\nhello-fwrite\nX\n" && out != "hello-printf\r\nhello-fwrite\r\nX\r\n" {
		t.Fatalf("IO 输出异常: %q", out)
	}
}

func TestInspect(t *testing.T) {
	src := `
#include <stdio.h>
#include "zuzo_rt.h"
int main(int argc, char** argv) {
    z_rt_init();
    ZBuf b; buf_init(&b);
    ZVal s = z_str("hi");
    z_inspect(&b, s);
    fwrite(b.data, 1, (size_t)b.len, stdout);
    fputc('\n', stdout);
    return 0;
}
`
	exe := compileWithRT(t, "io2", src)
	code, out := runExe(t, exe)
	t.Logf("code=%d out=%q", code, out)
}
