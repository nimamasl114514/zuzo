//go:build ignore
// +build ignore

package main

// bench_zuzo.go — Zuzo L5 100K 循环多轮基准
import (
	"fmt"
	"time"

	"zuzo/evaluator"
	"zuzo/infer"
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
	inf := infer.New()
	inf.Infer(program)

	oldSAP := evaluator.GetSAPCompiler()
	sap := evaluator.NewSAPCompiler(inf)
	sap.CompileProgram(program)
	evaluator.SetSAPCompiler(sap)
	defer evaluator.SetSAPCompiler(oldSAP)

	// 预热
	for w := 0; w < 50; w++ {
		env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
		evaluator.Eval(program, env)
	}

	start := time.Now()

	for r := 0; r < ROUNDS; r++ {
		env := evaluator.NewSAPEnvironment(len(inf.GetSlotMap()), inf.GetSlotMap())
		evaluator.Eval(program, env)
	}

	elapsed := time.Since(start)
	perIterNs := float64(elapsed.Nanoseconds()) / float64(ROUNDS*N)
	perRoundUs := float64(elapsed.Microseconds()) / float64(ROUNDS)

	fmt.Printf("Zuzo L5:         %.2f ns/iter  |  %.2f us/round  |  (%d rounds)  |  %v\n",
		perIterNs, perRoundUs, ROUNDS, elapsed)
}
