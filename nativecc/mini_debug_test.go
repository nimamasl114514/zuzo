package nativecc

import "testing"

func TestMiniDecl(t *testing.T) {
	srcs := []string{
		"typedef struct { int unused; } ZEnvX;",
		"typedef struct { int a; } A;\ntypedef struct { int b; } B;\nstatic int f(int x);",
		"struct S { int x; };\ntypedef struct S S2;\nstatic int g(void);",
	}
	for i, s := range srcs {
		_, err := ParseSources([]Source{{Name: "mini.c", Code: s}})
		if err != nil {
			t.Errorf("case %d 失败: %v", i, err)
		}
	}
}
