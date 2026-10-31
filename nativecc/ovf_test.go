package nativecc

import "testing"

func TestBuiltinOvfl(t *testing.T) {
	buildAndRun(t, `
int main(int argc, char** argv) {
    long long a = 4950, b = 100;
    long long r;
    if (__builtin_add_overflow(a, b, &r)) { return 1; }
    if (r != 5050) { return 2; }
    return 0;
}
`, 0)
}

func TestBuiltinOvflMul(t *testing.T) {
	buildAndRun(t, `
int main(int argc, char** argv) {
    long long a = 7, b = 6;
    long long r;
    if (__builtin_mul_overflow(a, b, &r)) { return 1; }
    if (r != 42) { return 2; }
    return 0;
}
`, 0)
}

func TestBuiltinOvflReal(t *testing.T) {
	buildAndRun(t, `
int main(int argc, char** argv) {
    long long a = 9223372036854775807LL;
    long long b = 1;
    long long r;
    if (__builtin_add_overflow(a, b, &r)) { return 3; }
    return 0;
}
`, 3)
}
