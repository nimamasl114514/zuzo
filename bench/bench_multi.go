//go:build ignore
// +build ignore

// bench_multi.go — Go 原生基准
package main

import (
	"fmt"
	"time"
)

const N = 100000
const ROUNDS = 100000

func main() {
	start := time.Now()

	var total int64
	for r := 0; r < ROUNDS; r++ {
		var sum int64
		for i := int64(0); i < N; i++ {
			sum += i
		}
		total += sum
	}

	elapsed := time.Since(start)
	perIterNs := float64(elapsed.Nanoseconds()) / float64(ROUNDS*N)
	perRoundUs := float64(elapsed.Microseconds()) / float64(ROUNDS)

	fmt.Printf("Go:              %.2f ns/iter  |  %.2f us/round  |  total=%d  |  %v\n",
		perIterNs, perRoundUs, total, elapsed)
}
