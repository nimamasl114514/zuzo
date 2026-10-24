package main

// Go 原生性能对比基准
// 对应 zuzo 的三个基准测试

import (
	"fmt"
	"time"
)

func fib(n int) int {
	if n < 2 {
		return n
	}
	return fib(n-1) + fib(n-2)
}

func primeSieve(limit int) int {
	isPrime := make([]int, limit)
	for i := range isPrime {
		isPrime[i] = 1
	}
	isPrime[0] = 0
	isPrime[1] = 0

	for j := 2; j*j < limit; j++ {
		if isPrime[j] == 1 {
			for k := j * j; k < limit; k += j {
				isPrime[k] = 0
			}
		}
	}

	count := 0
	for m := 2; m < limit; m++ {
		if isPrime[m] == 1 {
			count++
		}
	}
	return count
}

func calc(start, end int) int {
	sum := 0
	for i := start; i < end; i++ {
		sum += i
	}
	return sum
}

func main() {
	// --- 1. 斐波那契递归 ---
	start := time.Now()
	r1 := fib(30)
	t1 := time.Since(start)
	fmt.Printf("Go fib(30) = %d  耗时: %v\n", r1, t1)

	// --- 2. 素数筛 ---
	start = time.Now()
	r2 := primeSieve(100000)
	t2 := time.Since(start)
	fmt.Printf("Go primes(100000) = %d  耗时: %v\n", r2, t2)

	// --- 3a. 并发求和 ---
	start = time.Now()
	var ch1 = make(chan int, 1)
	var ch2 = make(chan int, 1)
	var ch3 = make(chan int, 1)
	var ch4 = make(chan int, 1)
	go func() { ch1 <- calc(0, 250000) }()
	go func() { ch2 <- calc(250000, 500000) }()
	go func() { ch3 <- calc(500000, 750000) }()
	go func() { ch4 <- calc(750000, 1000000) }()
	concurrentResult := <-ch1 + <-ch2 + <-ch3 + <-ch4
	t3a := time.Since(start)
	fmt.Printf("Go concurrent_sum(0..1M) = %d  耗时: %v\n", concurrentResult, t3a)

	// --- 3b. 串行求和 ---
	start = time.Now()
	serialResult := calc(0, 1000000)
	t3b := time.Since(start)
	fmt.Printf("Go serial_sum(0..1M) = %d  耗时: %v\n", serialResult, t3b)
}
