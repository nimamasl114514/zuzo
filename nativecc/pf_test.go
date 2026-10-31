package nativecc

import "testing"

func TestPrintfInt(t *testing.T) {
	buildAndRun(t, `
int printf(const char* fmt, ...);
int main(int argc, char** argv) { printf("%d\n", 42); return 0; }
`, 0)
}

func TestPrintfDouble(t *testing.T) {
	buildAndRun(t, `
int printf(const char* fmt, ...);
int main(int argc, char** argv) { double d = 3.14; printf("%f\n", d); return 0; }
`, 0)
}

func TestPrintfTwo(t *testing.T) {
	buildAndRun(t, `
int printf(const char* fmt, ...);
int main(int argc, char** argv) { printf("%d %d\n", 1, 2); return 0; }
`, 0)
}
