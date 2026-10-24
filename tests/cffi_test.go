package tests

import (
	"runtime"
	"testing"

	"zuzo/encoding"
	"zuzo/evaluator"
	"zuzo/lexer"
	"zuzo/object"
	"zuzo/parser"
)

// getMathLib 返回当前平台的数学库名
func getMathLib() string {
	switch runtime.GOOS {
	case "windows":
		return "msvcrt.dll"
	case "darwin":
		return "libm.dylib"
	default:
		return "libm.so.6"
	}
}

func TestCLoadLib(t *testing.T) {
	libPath := getMathLib()
	handle, err := object.LoadCLib(libPath)
	if err != nil {
		t.Skipf("无法加载 %s: %v (跳过)", libPath, err)
	}
	if handle == 0 {
		t.Fatal("LoadCLib 返回了零句柄")
	}
	defer object.CloseCLib(handle)
}

func TestCLookupSymbol(t *testing.T) {
	libPath := getMathLib()
	handle, err := object.LoadCLib(libPath)
	if err != nil {
		t.Skipf("无法加载 %s: %v", libPath, err)
	}
	defer object.CloseCLib(handle)

	addr, err := object.LookupCSymbol(handle, "abs")
	if err != nil {
		t.Fatalf("LookupCSymbol(abs) 失败: %v", err)
	}
	if addr == 0 {
		t.Fatal("LookupCSymbol 返回了零地址")
	}
}

func TestCFFITypeParsing(t *testing.T) {
	cases := []struct {
		input    string
		expected object.CFFIType
	}{
		{"int", object.CInt},
		{"uint", object.CUInt},
		{"double", object.CDouble},
		{"float", object.CFloat},
		{"void", object.CVoid},
		{"string", object.CStr},
		{"ptr", object.CPtr},
		{"bool", object.CBool},
		{"long", object.CLong},
		{"char", object.CChar},
	}

	for _, c := range cases {
		got, ok := object.ParseCFFIType(c.input)
		if !ok {
			t.Errorf("ParseCFFIType(%q) 返回 false", c.input)
			continue
		}
		if got != c.expected {
			t.Errorf("ParseCFFIType(%q) = %v, 期望 %v", c.input, got, c.expected)
		}
	}
}

func TestCFFITypeName(t *testing.T) {
	if object.CFFITypeName(object.CInt) != "int" {
		t.Errorf("CFFITypeName(CInt) = %q, 期望 %q", object.CFFITypeName(object.CInt), "int")
	}
	if object.CFFITypeName(object.CDouble) != "double" {
		t.Errorf("CFFITypeName(CDouble) = %q, 期望 %q", object.CFFITypeName(object.CDouble), "double")
	}
}

func TestCCallAbs(t *testing.T) {
	libPath := getMathLib()
	handle, err := object.LoadCLib(libPath)
	if err != nil {
		t.Skipf("无法加载 %s: %v", libPath, err)
	}
	defer object.CloseCLib(handle)

	addr, err := object.LookupCSymbol(handle, "abs")
	if err != nil {
		t.Fatalf("LookupCSymbol(abs) 失败: %v", err)
	}

	retInt, _, _, err := object.CallCFuncWithStrings(
		addr,
		[]object.CFFIType{object.CInt},
		object.CInt,
		[]interface{}{int64(-42)},
	)
	if err != nil {
		t.Fatalf("CallCFuncWithStrings 失败: %v", err)
	}

	result := int32(retInt)
	if result != 42 {
		t.Errorf("abs(-42) = %d, 期望 42", result)
	}
}

func TestCCallSqrt(t *testing.T) {
	libPath := getMathLib()
	handle, err := object.LoadCLib(libPath)
	if err != nil {
		t.Skipf("无法加载 %s: %v", libPath, err)
	}
	defer object.CloseCLib(handle)

	addr, err := object.LookupCSymbol(handle, "sqrt")
	if err != nil {
		t.Fatalf("LookupCSymbol(sqrt) 失败: %v", err)
	}

	_, retDbl, isFloatRet, err := object.CallCFuncWithStrings(
		addr,
		[]object.CFFIType{object.CDouble},
		object.CDouble,
		[]interface{}{float64(144.0)},
	)
	if err != nil {
		t.Fatalf("CallCFuncWithStrings 失败: %v", err)
	}

	if !isFloatRet {
		t.Error("期望浮点返回值")
	}

	// sqrt(144) = 12
	if retDbl < 11.99 || retDbl > 12.01 {
		t.Errorf("sqrt(144) = %f, 期望 12.0", retDbl)
	}
}

func TestCCallPow(t *testing.T) {
	libPath := getMathLib()
	handle, err := object.LoadCLib(libPath)
	if err != nil {
		t.Skipf("无法加载 %s: %v", libPath, err)
	}
	defer object.CloseCLib(handle)

	addr, err := object.LookupCSymbol(handle, "pow")
	if err != nil {
		t.Fatalf("LookupCSymbol(pow) 失败: %v", err)
	}

	_, retDbl, _, err := object.CallCFuncWithStrings(
		addr,
		[]object.CFFIType{object.CDouble, object.CDouble},
		object.CDouble,
		[]interface{}{float64(2.0), float64(10.0)},
	)
	if err != nil {
		t.Fatalf("CallCFuncWithStrings 失败: %v", err)
	}

	// pow(2, 10) = 1024
	if retDbl < 1023.99 || retDbl > 1024.01 {
		t.Errorf("pow(2, 10) = %f, 期望 1024.0", retDbl)
	}
}

func TestCCallStrlen(t *testing.T) {
	libPath := getMathLib()
	handle, err := object.LoadCLib(libPath)
	if err != nil {
		t.Skipf("无法加载 %s: %v", libPath, err)
	}
	defer object.CloseCLib(handle)

	addr, err := object.LookupCSymbol(handle, "strlen")
	if err != nil {
		t.Fatalf("LookupCSymbol(strlen) 失败: %v", err)
	}

	retInt, _, _, err := object.CallCFuncWithStrings(
		addr,
		[]object.CFFIType{object.CStr},
		object.CULong,
		[]interface{}{"Hello"},
	)
	if err != nil {
		t.Fatalf("CallCFuncWithStrings 失败: %v", err)
	}

	if retInt != 5 {
		t.Errorf("strlen(\"Hello\") = %d, 期望 5", retInt)
	}
}

// 测试 Zuzo 脚本层面的 C FFI
func TestCFFIScript(t *testing.T) {
	libPath := getMathLib()

	// 构造 Zuzo 脚本
	script := `
变量 lib = 加载C库("` + libPath + `")
变量 abs_fn = lib.绑定("abs", ["int"], "int")
变量 结果 = C调用(abs_fn, -42)
lib.关闭()
`

	env := evaluator.NewEnvironment()
	evaluator.SetSAPCompiler(nil)
	l := lexer.New(encoding.NormalizeSource([]byte(script)))
	p := parser.New(l)
	program := p.ParseProgram()
	if p.Diagnostics().HasErrors() {
		t.Fatalf("解析错误: %s", p.Errors())
	}
	result := evaluator.Eval(program, env)

	if result == nil || result.Type() == object.ERROR_OBJ {
		t.Fatalf("脚本执行失败: %s", result.Inspect())
	}
}
