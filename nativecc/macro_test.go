package nativecc

import "testing"

func TestIsinfMacro(t *testing.T) {
	buildAndRun(t, `
#include <math.h>
int main(int argc, char** argv) {
    double x = 3.14;
    if (isnan(x)) { return 1; }
    if (isinf(x)) { return 2; }
    return 0;
}
`, 0)
}

func TestIsinfMacroInf(t *testing.T) {
	buildAndRun(t, `
#include <math.h>
int main(int argc, char** argv) {
    double x = 1.0 / 0.0;
    if (isinf(x)) { return 7; }
    return 0;
}
`, 7)
}

func TestSubSelf(t *testing.T) {
	buildAndRun(t, `
int main(int argc, char** argv) {
    double x = 3.14;
    double y = x - x;
    if (y != 0) { return 1; }
    return 0;
}
`, 0)
}
