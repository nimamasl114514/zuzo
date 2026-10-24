package mix

import (
	"testing"

	"zuzo/lexer"
	"zuzo/parser"
	pyparser "zuzo/py/parser"
)

func TestScanPy(t *testing.T) {
	m, errs := pyparser.Parse("x = 1\ndef f(n):\n    return n * 2\nprint(x)\n")
	if len(errs) > 0 {
		t.Fatalf("parse errs: %v", errs)
	}
	refs, defs := scanPy("x = 1\ndef f(n):\n    return n * 2\nprint(x)\n", m)
	t.Logf("refs=%v defs=%v", refs, defs)
	if !defs["x"] || !defs["f"] {
		t.Fatalf("defs missing x/f: %v", defs)
	}
	if !refs["print"] {
		t.Fatalf("refs should contain print: %v", refs)
	}
}

func TestScanZuzo(t *testing.T) {
	src := "变量 a = 1\n打印(a, b)\n"
	p := parser.New(lexer.New(src))
	prog := p.ParseProgram()
	refs, defs := scanZuzo(src, prog)
	t.Logf("refs=%v defs=%v", refs, defs)
	if !defs["a"] {
		t.Fatalf("defs missing a: %v", defs)
	}
	if !refs["b"] {
		t.Fatalf("refs missing b: %v", refs)
	}
}
