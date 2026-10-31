package nativecc

import "testing"

func TestGlobalPtrStr(t *testing.T) {
	code, out := buildRunOut(t, "gptr", `
int printf(const char* fmt, ...);
const char* G1 = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";
int main(int argc, char** argv) {
    printf("G[0]=%c G[25]=%c G[26]=%c G[27]=%c G[6]=%c\n", G1[0], G1[25], G1[26], G1[27], G1[6]);
    printf("str=%s\n", G1);
    return 0;
}
`)
	t.Logf("code=%d out=%q", code, out)
}

func TestGlobalPtrArray(t *testing.T) {
	code, out := buildRunOut(t, "gptr2", `
int printf(const char* fmt, ...);
int arr[4] = {10, 20, 30, 40};
int main(int argc, char** argv) {
    printf("%d %d %d %d\n", arr[0], arr[1], arr[2], arr[3]);
    return 0;
}
`)
	t.Logf("code=%d out=%q", code, out)
}
