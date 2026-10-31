package nativecc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLexer 词法基础测试
func TestLexer(t *testing.T) {
	src := `
int 主函数(int argc) {
    // 注释
    ZVal x = z_int(10LL);
    char* s = "你好\n";
    char c = 'a';
    x += 0x1F;
    if (x != 0) { return 1; }
    /* 块注释 */
    return 0;
}
`
	toks, err := NewLexer("test.c", src).Lex()
	if err != nil {
		t.Fatalf("词法失败: %v", err)
	}
	var texts []string
	for _, tk := range toks {
		if tk.Kind == TEOF {
			break
		}
		texts = append(texts, tk.Text)
	}
	joined := strings.Join(texts, " ")
	for _, want := range []string{"主函数", "10LL", "0x1F", "+=", "!=", "{"} {
		if !strings.Contains(joined, want) {
			t.Errorf("缺少记号 %q，实际: %s", want, joined)
		}
	}
}

// TestPreprocess 预处理测试
func TestPreprocess(t *testing.T) {
	src := `
#include <stdint.h>
#ifdef _WIN32
int a = 1;
#else
int a = 2;
#endif
`
	pp := NewPreprocessor(BuiltinHeaders, nil)
	if err := pp.ProcessFile("test.c", src); err != nil {
		t.Fatalf("预处理失败: %v", err)
	}
	toks := pp.Tokens()
	var texts []string
	for _, tk := range toks {
		if tk.Kind == TEOF {
			break
		}
		texts = append(texts, tk.Text)
	}
	joined := strings.Join(texts, " ")
	if !strings.Contains(joined, "int a = 1") {
		t.Errorf("条件编译结果错误: %s", joined)
	}
	if strings.Contains(joined, "#") {
		t.Errorf("残留预处理记号: %s", joined)
	}
}

// TestParseRuntime 解析真实的 zuzo_rt.c
func TestParseRuntime(t *testing.T) {
	path := "../cbackend/runtime/zuzo_rt.c"
	data, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("找不到 %s: %v", path, err)
	}
	abs, _ := filepath.Abs(path)
	src := Source{Name: abs, Code: string(data)}
	tu, err := ParseSources([]Source{src})
	if err != nil {
		t.Fatalf("解析运行时失败: %v", err)
	}
	funcs := 0
	varTypes := 0
	for _, d := range tu.Decls {
		switch d.(type) {
		case *FuncDecl:
			funcs++
		case *VarDecl:
			varTypes++
		}
	}
	t.Logf("解析成功: %d 个顶层声明（函数 %d，变量 %d）", len(tu.Decls), funcs, varTypes)
	if funcs < 100 {
		t.Errorf("函数数量异常偏少: %d", funcs)
	}
}

// TestParseGenerated 解析 zuzo 生成的 main.c
func TestParseGenerated(t *testing.T) {
	candidates := []string{"../tmp/gen_t09_closure.c", "../tmp/sample.c"}
	found := false
	for _, path := range candidates {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		found = true
		abs, _ := filepath.Abs(path)
		src := Source{Name: abs, Code: string(data)}
		tu, err := ParseSources([]Source{src})
		if err != nil {
			t.Fatalf("解析 %s 失败: %v", path, err)
		}
		t.Logf("%s 解析成功: %d 个顶层声明", path, len(tu.Decls))
	}
	if !found {
		t.Skip("没有找到生成的 C 样例")
	}
}
