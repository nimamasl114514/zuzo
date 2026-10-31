package nativecc

import "testing"

func TestFloatStoreLoad(t *testing.T) {
	buildAndRun(t, `
int main(int argc, char** argv) {
    double d = 3.14;
    return (int)(d * 100.0);
}
`, 314)
}

func TestFloatGlobal(t *testing.T) {
	buildAndRun(t, `
double g = 2.5;
int main(int argc, char** argv) {
    return (int)(g * 2.0);
}
`, 5)
}

func TestFloatArg(t *testing.T) {
	buildAndRun(t, `
int f(double x) { return (int)(x * 10.0); }
int main(int argc, char** argv) {
    double d = 3.14;
    return f(d);
}
`, 31)
}
