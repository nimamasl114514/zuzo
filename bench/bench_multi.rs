// bench_multi.rs — Rust 基准，使用不可预测的输入防止编译器消除循环
use std::time::Instant;
use std::hint::black_box;

const N: i64 = 100000;
const ROUNDS: i64 = 100000;

fn main() {
    let start = Instant::now();

    let mut total: i64 = 0;
    for r in 0..ROUNDS {
        let mut sum: i64 = 0;
        let mut i: i64 = 0;
        // 使用 while + black_box 防止编译器将循环替换为等差数列公式
        while i < black_box(N) {
            sum += i;
            i += 1;
        }
        total = total + black_box(sum);
    }

    let elapsed = start.elapsed();
    let per_iter_ns = elapsed.as_nanos() as f64 / (ROUNDS * N) as f64;
    let per_round_us = elapsed.as_nanos() as f64 / ROUNDS as f64 / 1000.0;

    println!("Rust (-O):       {:.2} ns/iter  |  {:.2} us/round  |  total={}  |  {:?}", 
             per_iter_ns, per_round_us, total, elapsed);
}
