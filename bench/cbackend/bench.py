"""C 后端性能基线：解释器（SAP）vs C 产物。

用法：python bench/cbackend/bench.py [轮数]
输出：每个用例的中位数耗时与加速比（JSON + 表格）。
"""
import json
import os
import statistics
import subprocess
import sys
import time

ROOT = os.path.dirname(os.path.dirname(os.path.dirname(os.path.abspath(__file__))))
ZUZO = os.path.join(ROOT, "zuzo.exe")
OUT = os.path.join(ROOT, "bench", "cbackend", "_out")
CASES = ["bench_loop", "bench_fib", "bench_sort"]


def run_once(exe, src):
    t0 = time.perf_counter()
    p = subprocess.run([exe] if src is None else [exe, src],
                       cwd=ROOT, capture_output=True, timeout=300)
    p = subprocess.CompletedProcess(p.args, p.returncode,
                                    p.stdout.decode("utf-8", "replace"),
                                    p.stderr.decode("utf-8", "replace"))
    dt = (time.perf_counter() - t0) * 1000.0
    if p.returncode != 0:
        raise RuntimeError(f"{exe} {src} rc={p.returncode}: {p.stderr[:300]}")
    return dt, p.stdout.strip()


def main():
    rounds = int(sys.argv[1]) if len(sys.argv) > 1 else 5
    os.makedirs(OUT, exist_ok=True)
    results = []
    for case in CASES:
        src = os.path.join(ROOT, "bench", "cbackend", case + ".hy")
        exe = os.path.join(OUT, case + ".exe")
        build = subprocess.run([ZUZO, "--emit-exe", src, "-o", exe],
                               cwd=ROOT, capture_output=True, timeout=300)
        build = subprocess.CompletedProcess(build.args, build.returncode,
                                            build.stdout.decode("utf-8", "replace"),
                                            build.stderr.decode("utf-8", "replace"))
        if build.returncode != 0:
            print(f"[BUILD-FAIL] {case}: {build.stderr[:300]}")
            continue

        interp_times, c_times = [], []
        interp_out = c_out = None
        for _ in range(rounds):
            dt, out = run_once(ZUZO, src)
            interp_times.append(dt)
            interp_out = out
            dt2, out2 = run_once(exe, None)
            c_times.append(dt2)
            c_out = out2
        same = interp_out == c_out
        mi = statistics.median(interp_times)
        mc = statistics.median(c_times)
        results.append({
            "case": case,
            "rounds": rounds,
            "interp_ms": round(mi, 2),
            "c_ms": round(mc, 2),
            "speedup": round(mi / mc, 2) if mc > 0 else None,
            "same_output": same,
        })
        print(f"{case:12s} 解释器 {mi:9.2f} ms · C {mc:8.2f} ms · 加速 {mi/mc:6.2f}x · 输出{'一致' if same else '不一致'}")

    path = os.path.join(OUT, "bench_result.json")
    with open(path, "w", encoding="utf-8") as f:
        json.dump(results, f, ensure_ascii=False, indent=2)
    print(f"\n结果已写: {path}")


if __name__ == "__main__":
    main()
