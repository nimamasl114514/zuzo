package nativecc

import "testing"

func TestFloatEqSimple(t *testing.T) {
	buildAndRun(t, `int main(int argc, char** argv) { double a = 1.5; double b = 1.5; if (a == b) { return 1; } return 0; }`, 1)
}

func TestFloatNeSimple(t *testing.T) {
	buildAndRun(t, `int main(int argc, char** argv) { double a = 1.5; double b = 1.5; if (a != b) { return 1; } return 0; }`, 0)
}

func TestFloatLtSimple(t *testing.T) {
	buildAndRun(t, `int main(int argc, char** argv) { double a = 1.5; double b = 2.5; if (a < b) { return 1; } return 0; }`, 1)
}
