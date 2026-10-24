// bench_multi.mjs — Node.js (V8 JIT) 基准
// 用普通 Number (double) 而非 BigInt，反映 V8 JIT 真实性能
const N = 100000;
const ROUNDS = 100000;

// 预热 V8 JIT
let warmup = 0;
for (let w = 0; w < 10000; w++) {
    let s = 0;
    for (let i = 0; i < N; i++) s += i;
    warmup += s;
}

const start = process.hrtime.bigint();

let total = 0;
for (let r = 0; r < ROUNDS; r++) {
    let sum = 0;
    for (let i = 0; i < N; i++) {
        sum += i;
    }
    total += sum;
}

const elapsedNs = Number(process.hrtime.bigint() - start);
const perIterNs = elapsedNs / (ROUNDS * N);
const perRoundUs = elapsedNs / ROUNDS / 1000;
const elapsedS = elapsedNs / 1e9;

console.log(`Node.js (V8):    ${perIterNs.toFixed(2)} ns/iter  |  ${perRoundUs.toFixed(2)} us/round  |  total=${total}  |  ${elapsedS.toFixed(3)}s`);
