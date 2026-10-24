//go:build ignore
// +build ignore

package main

// bench_zuzo_nosap.go — Zuzo 纯解释器（无 SAP）100K 循环基准
import (
	"fmt"
	"time"

	"zuzo/evaluator"
	"zuzo/lexer"
	"zuzo/parser"
)

const N = 100000
const ROUNDS = 1000

func main() {
	code := `变量 sum = 0; 变量 i = 0; 对于 i = 0; i < 100000; i = i + 1 { sum = sum + i }`
	l := lexer.New(code)
	p := parser.New(l)
	program := p.ParseProgram()

	// 预热
	for w := 0; w < 50; w++ {
		env := evaluator.NewEnvironment()
		evaluator.Eval(program, env)
	}

	start := time.Now()

	for r := 0; r < ROUNDS; r++ {
		env := evaluator.NewEnvironment()
		evaluator.Eval(program, env)
	}

	elapsed := time.Since(start)
	perIterNs := float64(elapsed.Nanoseconds()) / float64(ROUNDS*N)
	perRoundUs := float64(elapsed.Microseconds()) / float64(ROUNDS)

	fmt.Printf("Zuzo pure-interp: %.2f ns/iter  |  %.2f us/round  |  (%d rounds)  |  %v\n",
		perIterNs, perRoundUs, ROUNDS, elapsed)
}