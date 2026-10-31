package nativecc

import "testing"

// 裸探针：static const 数组 + puts（正常退出时 msvcrt 自动 flush）
func TestStaticArrPuts(t *testing.T) {
	code, out := buildRunOut(t, "sc4", `
int puts(const char* s);
static const char T1[] = "ABCD0123";
int main(int argc, char** argv) {
    puts(T1);
    puts("done");
    return 0;
}
`)
	t.Logf("code=%d(%#x) out=%q", code, uint32(code), out)
}

// 对照：无初始化的 static 数组（运行时赋值）
func TestStaticNoInit(t *testing.T) {
	code, out := buildRunOut(t, "sc5", `
int puts(const char* s);
static char T1[9];
int main(int argc, char** argv) {
    T1[0] = 65; T1[1] = 66; T1[2] = 67; T1[3] = 0;
    puts(T1);
    puts("done");
    return 0;
}
`)
	t.Logf("code=%d(%#x) out=%q", code, uint32(code), out)
}

// 对照：全局（非 static）初始化数组
func TestGlobalArrInit(t *testing.T) {
	code, out := buildRunOut(t, "sc6", `
int puts(const char* s);
char T1[] = "GLOB";
int main(int argc, char** argv) {
    puts(T1);
    puts("done");
    return 0;
}
`)
	t.Logf("code=%d(%#x) out=%q", code, uint32(code), out)
}
