package mix

import (
	"strings"
	"testing"
)

func TestSplitBasic(t *testing.T) {
	src := "##PY\nprint(1)\n##ZUZO\n打印(1)\n"
	bs, err := Split(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bs) != 2 {
		t.Fatalf("want 2 blocks, got %d", len(bs))
	}
	if bs[0].Lang != LangPy || bs[1].Lang != LangZuzo {
		t.Fatalf("lang mismatch: %v %v", bs[0].Lang, bs[1].Lang)
	}
	// 行号对齐：块首语句所在行号必须等于原文件行号
	if !strings.HasPrefix(bs[1].Src, "\n\n\n") {
		t.Fatalf("line padding missing: %q", bs[1].Src[:min(6, len(bs[1].Src))])
	}
	if bs[1].StartLine != 4 {
		t.Fatalf("want start line 4, got %d", bs[1].StartLine)
	}
}

func TestSplitMarkerCaseInsensitive(t *testing.T) {
	src := "##py\nprint(1)\n## Zuzo\n打印(1)\n"
	bs, err := Split(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bs) != 2 || bs[0].Lang != LangPy || bs[1].Lang != LangZuzo {
		t.Fatalf("marker parsing failed: %+v", bs)
	}
}

func TestSplitNoMarkerFallback(t *testing.T) {
	src := "变量 a = 1\n打印(a)\n"
	bs, err := Split(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bs) != 1 || bs[0].Lang != LangZuzo {
		t.Fatalf("fallback should be single zuzo block, got %+v", bs)
	}
	if bs[0].Src != src {
		t.Fatalf("fallback source mutated")
	}
}

func TestSplitCodeBeforeFirstMarker(t *testing.T) {
	src := "变量 a = 1\n##PY\nprint(1)\n"
	_, err := Split(src)
	if err == nil {
		t.Fatal("want error for code before first marker")
	}
	se, ok := err.(*SplitError)
	if !ok || se.Line != 1 {
		t.Fatalf("want SplitError at line 1, got %v", err)
	}
}

func TestSplitPreambleAllowed(t *testing.T) {
	src := "// 注释\n# 注释\n\n##PY\nprint(1)\n"
	bs, err := Split(src)
	if err != nil {
		t.Fatalf("preamble should be allowed: %v", err)
	}
	if len(bs) != 1 || bs[0].Lang != LangPy {
		t.Fatalf("unexpected blocks: %+v", bs)
	}
}

func TestSplitSkipsBlankBlock(t *testing.T) {
	src := "##PY\n\n##ZUZO\n打印(1)\n"
	bs, err := Split(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bs) != 1 || bs[0].Lang != LangZuzo {
		t.Fatalf("blank block should be skipped, got %+v", bs)
	}
}

func TestSplitCRLFAndBOM(t *testing.T) {
	src := "\ufeff##PY\r\nprint(1)\r\n"
	bs, err := Split(src)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(bs) != 1 || strings.Contains(bs[0].Raw, "\r") {
		t.Fatalf("CRLF not normalized: %+v", bs)
	}
}

func TestIsMixFile(t *testing.T) {
	if !IsMixFile("a/b/c.hypy") || !IsMixFile("X.HYPY") {
		t.Fatal("IsMixFile should accept .hypy case-insensitively")
	}
	if IsMixFile("c.hy") || IsMixFile("c.py") {
		t.Fatal("IsMixFile should reject single-language extensions")
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
