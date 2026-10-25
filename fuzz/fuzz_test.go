package fuzz

// fuzz_test.go — zuzo 的 Go 原生模糊测试目标（Go 1.18+ testing.F）
//
// 目标：用随机字节/随机源码喂 词法器 / 语法器 / 运行时，断言
//
//	① 不 panic（含越界、空指针、类型断言失败）
//	② 不挂死（单用例 3 秒上限）
//	③ 不出现 token 爆炸（词法器无限循环）
//
// 用法：
//
//	go test ./fuzz -run=^$ -fuzz=FuzzPyLexer -fuzztime=30s
//	go test ./fuzz -run=^$ -fuzz=FuzzPyParser -fuzztime=30s
//	go test ./fuzz -run=^$ -fuzz=FuzzPyExec  -fuzztime=30s
//	go test ./fuzz            # 只跑种子语料（CI 快检）
//
// 失败产物落 testdata/fuzz/<目标名>/，直接作为回归语料。

import (
	"testing"
	"time"

	zeval "zuzo/evaluator"
	zlexer "zuzo/lexer"
	zparser "zuzo/parser"

	pylexer "zuzo/py/lexer"
	pyruntime "zuzo/py/runtime"
	ztoken "zuzo/token"
)

type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }

// pySeeds 覆盖 Python 前端的高风险语法面：f-string、缩进、装饰器、async、异常、推导式、中文
var pySeeds = []string{
	"x = 1\nprint(x)\n",
	"print(f'{1+1:>5}')\n",
	"print(f\"{ {'a':1}['a'] }\")\n",
	"def f(a, b=2, *args, **kw):\n    return a + b\nprint(f(1))\n",
	"class A(object):\n    def __init__(self):\n        super().__init__()\n",
	"for i in range(3):\n    print(i)\n",
	"try:\n    1/0\nexcept ZeroDivisionError as e:\n    print(e)\n",
	"print([x * x for x in range(5) if x % 2])\n",
	"async def f():\n    await g()\n",
	"print('中文' * 3)\n",
	"print({k: v for k, v in [(1, 2)]})\n",
	"with open('x') as f:\n    pass\n",
	"@dec(1)\ndef g():\n    yield 1\n",
	"lam = lambda a, b=1: a + b\nprint(lam(2))\n",
	"match v:\n    case [1, 2]:\n        pass\n",
	"x = 0\nwhile x < 10:\n    x += 1\n",
	"print(1 if True else 2)\n",
	"print(not x and y or z)\n",
	"s = '''multi\nline'''\n",
	"x: int = 1\ny: 'str' = 'a'\n",
}

// zzSeeds 覆盖 zuzo 原生语言（中文关键字）的语法面
var zzSeeds = []string{
	"变量 x = 10",
	"函数 add(a, b) { 返回 a + b }\n打印(add(1, 2))",
	"如果 x > 1 { 打印(1) } 否则 { 打印(2) }",
	"当 x < 3 { x = x + 1 }",
	"列表 = [1, 2, 3]\n打印(列表[0])",
	"字典 = {\"a\": 1}\n打印(字典[\"a\"])",
	"循环 i 从 1 到 10 { 打印(i) }",
	"尝试 { 1 / 0 } 捕获 e { 打印(e) }",
	"类 A { 函数 初始化() { 自己.x = 1 } }",
	"x = 1 + 2 * 3 - 4 / 2 % 3",
}

// ---------- Python 前端 ----------

func FuzzPyLexer(f *testing.F) {
	for _, s := range pySeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("pylexer panic: %v\nsrc=%q", r, src)
			}
		}()
		lx := pylexer.New(src)
		toks := lx.Tokens()
		if len(toks) > 2_000_000 {
			t.Fatalf("token explosion: %d tokens for src=%q", len(toks), src)
		}
	})
}

func FuzzPyParser(f *testing.F) {
	for _, s := range pySeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("pyparser panic: %v\nsrc=%q", r, src)
			}
		}()
		_, _ = pyruntime.ParseSourceWithFile(src, "<fuzz>")
	})
}

func FuzzPyExec(f *testing.F) {
	for _, s := range pySeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		type result struct{ panicVal interface{} }
		ch := make(chan result, 1)
		go func() {
			var r result
			func() {
				defer func() {
					if pv := recover(); pv != nil {
						r.panicVal = pv
					}
				}()
				pyruntime.RunSource(src, "<fuzz>", nopWriter{}, nopWriter{})
			}()
			ch <- r
		}()
		select {
		case r := <-ch:
			if r.panicVal != nil {
				t.Fatalf("pyexec panic: %v\nsrc=%q", r.panicVal, src)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("pyexec timeout (possible hang)\nsrc=%q", src)
		}
	})
}

// ---------- zuzo 原生语言前端 ----------

func FuzzZuzoLexer(f *testing.F) {
	for _, s := range zzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		defer func() {
			if r := recover(); r != nil {
				t.Fatalf("zuzo lexer panic: %v\nsrc=%q", r, src)
			}
		}()
		lx := zlexer.New(src)
		n := 0
		for {
			tok := lx.NextToken()
			if tok.Type == ztoken.EOF {
				break
			}
			n++
			if n > 2_000_000 {
				t.Fatalf("token explosion: %d tokens for src=%q", n, src)
			}
		}
	})
}

func FuzzZuzoExec(f *testing.F) {
	for _, s := range zzSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		type result struct{ panicVal interface{} }
		ch := make(chan result, 1)
		go func() {
			var r result
			func() {
				defer func() {
					if pv := recover(); pv != nil {
						r.panicVal = pv
					}
				}()
				p := zparser.New(zlexer.New(src))
				prog := p.ParseProgram()
				if prog != nil {
					env := zeval.NewEnvironment()
					_ = zeval.Eval(prog, env)
				}
			}()
			ch <- r
		}()
		select {
		case r := <-ch:
			if r.panicVal != nil {
				t.Fatalf("zuzo exec panic: %v\nsrc=%q", r.panicVal, src)
			}
		case <-time.After(3 * time.Second):
			t.Fatalf("zuzo exec timeout (possible hang)\nsrc=%q", src)
		}
	})
}
