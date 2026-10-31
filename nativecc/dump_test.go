package nativecc

import (
	"os"
	"testing"
)

func dumpOne(t *testing.T, name, src string) {
	out := "../tmp/" + name + ".exe"
	_ = os.MkdirAll("../tmp", 0o755)
	if err := CompileToExe([]Source{{Name: name + ".c", Code: src}}, out, Options{Verbose: true}); err != nil {
		t.Fatalf("编译失败: %v", err)
	}
}

func TestDumpPrintf(t *testing.T) {
	dumpOne(t, "nc_pf", `
int printf(const char* fmt, ...);
int main(int argc, char** argv) { double d = 3.14; printf("[%f]\n", d); return 0; }
`)
}

func TestDumpFloat(t *testing.T) {
	dumpOne(t, "nc_float", `
double add(double a, double b) { return a + b; }
int main(int argc, char** argv) { return (int)add(1.5, 2.5); }
`)
}

func TestDumpStruct(t *testing.T) {
	dumpOne(t, "nc_struct", `
typedef struct { int a; int b; } Pair;
int sum(Pair p) { return p.a + p.b; }
Pair make(int x, int y) { Pair p; p.a = x; p.b = y; return p; }
int main(int argc, char** argv) { Pair p = make(3, 4); return sum(p); }
`)
}
