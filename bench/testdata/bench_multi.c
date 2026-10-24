// bench_multi.c — 横向对比基准：100K 求和，重复 ROUNDS 轮
// 使用 volatile 防止编译器消除循环
#include <stdio.h>
#include <stdint.h>
#include <time.h>

#define N 100000
#define ROUNDS 100000

int main() {
    struct timespec start, end;
    volatile int64_t prevent_opt = N; // 防止编译时计算
    
    clock_gettime(CLOCK_MONOTONIC, &start);
    
    volatile int64_t total = 0;
    int64_t n = prevent_opt; // 通过 volatile 阻止常量传播
    for (int r = 0; r < ROUNDS; r++) {
        int64_t sum = 0;
        for (int64_t i = 0; i < n; i++) {
            sum += i;
        }
        total += sum;
    }
    
    clock_gettime(CLOCK_MONOTONIC, &end);
    
    double elapsed = (end.tv_sec - start.tv_sec) + (end.tv_nsec - start.tv_nsec) / 1e9;
    double per_round_us = elapsed / ROUNDS * 1e6;
    double per_iter_ns = elapsed / (ROUNDS * (double)N) * 1e9;
    
    printf("C (GCC -O2):     %.2f ns/iter  |  %.2f us/round  |  total=%lld  |  %.3fs\n",
           per_iter_ns, per_round_us, (long long)total, elapsed);
    return 0;
}
