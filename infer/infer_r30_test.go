package infer

import (
	"strings"
	"testing"

	"zuzo/lexer"
	"zuzo/parser"
)

func checkR30(src string) []TypeError {
	prog := parser.New(lexer.New(src)).ParseProgram()
	return CheckProgram(prog)
}

func TestR30OptionalHashGetAndDynamicContainers(t *testing.T) {
	src := `变量 h = {"x": 1}
打印(取值(h, "x"))
打印(取值(h, "missing", 42))
打印(包含(h, "x"))
打印(包含([1, "x", 真], "x"))
打印(包含("abc", "b"))
变量 mixed = [1, "x", 真]
打印(mixed)
`
	if errs := checkR30(src); len(errs) != 0 {
		t.Fatalf("合法动态容器代码不应有类型错误，得到 %v", errs)
	}
}

func TestR30OptionalHashGetRejectsWrongArity(t *testing.T) {
	src := `变量 h = {"x": 1}
取值(h)
取值(h, "x", 1, 2)
`
	errs := checkR30(src)
	if len(errs) != 2 {
		t.Fatalf("应分别拦截参数不足与参数过多，得到 %v", errs)
	}
	for _, err := range errs {
		if err.Code != ErrCodeArgCount {
			t.Errorf("错误码应为 %s，得到 %s（%v）", ErrCodeArgCount, err.Code, err)
		}
		if !strings.Contains(err.Message, "2-3") {
			t.Errorf("可选参数错误应包含范围 2-3，得到 %q", err.Message)
		}
	}
}

func TestR30BuiltinTypeErrorsRemainVisible(t *testing.T) {
	src := `变量 h = {"x": 1}
取值("not-hash", "x")
取值(h, 1)
包含(h)
大写(1, 2)
`
	errs := checkR30(src)
	if len(errs) != 4 {
		t.Fatalf("真实类型与参数错误应全部保留，得到 %v", errs)
	}
	counts := map[string]int{}
	for _, err := range errs {
		counts[err.Code]++
	}
	if counts[ErrCodeTypeMismatch] != 2 || counts[ErrCodeArgCount] != 2 {
		t.Errorf("应有两条类型错误与两条参数数量错误，得到 %v（%v）", counts, errs)
	}
}
