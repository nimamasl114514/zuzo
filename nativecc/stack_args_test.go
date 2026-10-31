package nativecc

import "testing"

func TestStackArgsSixIntegers(t *testing.T) {
	buildAndRun(t, `
long sum6(long a, long b, long c, long d, long e, long f) {
    return a + b + c + d + e + f;
}
int main(int argc, char** argv) {
    return sum6(1, 2, 3, 4, 5, 6);
}`, 21)
}

func TestStackArgsWithStructReturn(t *testing.T) {
	buildAndRun(t, `
typedef struct { long x; long y; } Pair;
Pair make_pair(long a, long b, long c, long d) {
    Pair p = {a + b, c + d};
    return p;
}
int main(int argc, char** argv) {
    Pair p = make_pair(1, 2, 3, 4);
    return p.x * 10 + p.y;
}`, 37)
}
