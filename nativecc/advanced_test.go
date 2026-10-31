package nativecc

import "testing"

func TestStructParamRet(t *testing.T) {
	buildAndRun(t, `
typedef struct { int a; int b; } Pair;
int sum(Pair p) { return p.a + p.b; }
Pair make(int x, int y) { Pair p; p.a = x; p.b = y; return p; }
int main(int argc, char** argv) { Pair p = make(3, 4); return sum(p); }
`, 7)
}

func TestArray(t *testing.T) {
	buildAndRun(t, `
int main(int argc, char** argv) {
    int a[5];
    int i;
    for (i = 0; i < 5; i++) { a[i] = i * i; }
    return a[4];
}
`, 16)
}

func TestPointer(t *testing.T) {
	buildAndRun(t, `
int main(int argc, char** argv) {
    int x = 5;
    int* p = &x;
    *p = 9;
    return x;
}
`, 9)
}

func TestCompoundLit(t *testing.T) {
	buildAndRun(t, `
typedef struct { int a; int b; } V;
int sum(int n, V* arr) { int s = 0; int i; for (i = 0; i < n; i++) { s = s + arr[i].a + arr[i].b; } return s; }
int main(int argc, char** argv) { return sum(2, (V[]){{1,2},{3,4}}); }
`, 10)
}

func TestFloat(t *testing.T) {
	buildAndRun(t, `
double add(double a, double b) { return a + b; }
int main(int argc, char** argv) { return (int)add(1.5, 2.5); }
`, 4)
}

func TestSwitch(t *testing.T) {
	buildAndRun(t, `
int main(int argc, char** argv) {
    int x = 2;
    switch (x) {
    case 1: return 10;
    case 2: return 20;
    default: return 30;
    }
}
`, 20)
}

func TestGlobal(t *testing.T) {
	buildAndRun(t, `
int g = 5;
int main(int argc, char** argv) { g = g + 3; return g; }
`, 8)
}

func TestStringLib(t *testing.T) {
	buildAndRun(t, `
int strlen(const char* s);
int strcmp(const char* a, const char* b);
int main(int argc, char** argv) {
    const char* s = "hello";
    if (strlen(s) != 5) { return 1; }
    if (strcmp(s, "hello") != 0) { return 2; }
    return 0;
}
`, 0)
}
