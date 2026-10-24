#!/usr/bin/env python3
# bench_multi.py — Python (CPython) 基准
import time

N = 100000
ROUNDS = 1000  # Python 太慢，只跑 1000 轮

# 预热（让 CPython 字节码缓存生效）
warmup = 0
for w in range(1000):
    s = 0
    for i in range(N):
        s += i
    warmup += s

start = time.perf_counter()

total = 0
for r in range(ROUNDS):
    s = 0
    for i in range(N):
        s += i
    total += s

elapsed = time.perf_counter() - start
per_iter_ns = elapsed / (ROUNDS * N) * 1e9
per_round_us = elapsed / ROUNDS * 1e6

print(f"Python (CPython): {per_iter_ns:.2f} ns/iter  |  {per_round_us:.2f} us/round  |  total={total}  |  {elapsed:.3f}s  |  ({ROUNDS} rounds)")
