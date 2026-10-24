"""zuzo 混合语言性能基准（可复现）

维度：
  1. 启动开销（zuzo 单语言 / Python 单语言 / 混合文件）
  2. 解释执行吞吐（zuzo SAP / zuzo Tree-Walk / 混合模式 / CPython）
  3. 跨语言调用开销（zuzo -> py、py -> zuzo）
  4. 块边界全局表同步开销（变量规模 × 块数）

用法： python bench/mix/bench_mix.py [--quick]
"""
import statistics
import subprocess
import sys
import time
from pathlib import Path

Z = "./zuzo.exe"
OUT = Path("bench/mix")


def run_file(path, args=(), repeat=5):
    ts = []
    last = None
    for _ in range(repeat):
        t0 = time.perf_counter()
        last = subprocess.run([Z, *args, str(path)], capture_output=True)
        ts.append(time.perf_counter() - t0)
    return statistics.median(ts), last


def run_inline(code, repeat=5):
    ts = []
    for _ in range(repeat):
        t0 = time.perf_counter()
        subprocess.run([Z, "-e", code], capture_output=True)
        ts.append(time.perf_counter() - t0)
    return statistics.median(ts)


def run_stdin(path, repeat=5):
    ts = []
    for _ in range(repeat):
        t0 = time.perf_counter()
        subprocess.run([Z, str(path)], input=b"", capture_output=True)
        ts.append(time.perf_counter() - t0)
    return statistics.median(ts)


def write_tmp(template, n):
    src = (OUT / template).read_text(encoding="utf-8").replace("N", str(n))
    p = OUT / ("_tmp_" + template)
    p.write_text(src, encoding="utf-8")
    return p


def loop_bench(template, args, n, label, repeat=3):
    p = write_tmp(template, n)
    t, r = run_file(p, args, repeat)
    ok = r.stdout.decode(errors="replace").strip()
    return label, t, ok


def sync_bench(vars_per_block, blocks, repeat=3):
    """交替 zuzo/py 块，每块定义 vars_per_block 个变量，测块边界同步开销"""
    parts = []
    for b in range(blocks):
        if b % 2 == 0:
            body = "\n".join(f"变量 v{i}_{b} = {i}" for i in range(vars_per_block))
            parts.append("##ZUZO\n" + body)
        else:
            body = "\n".join(f"w{i}_{b} = {i}" for i in range(vars_per_block))
            parts.append("##PY\n" + body)
    src = "\n".join(parts) + "\n##PY\nprint('sync ok')\n"
    p = OUT / f"_sync_{vars_per_block}x{blocks}.hypy"
    p.write_text(src, encoding="utf-8")
    t, r = run_file(p, (), repeat)
    return t, r.stdout.decode(errors="replace").strip()


def main():
    quick = "--quick" in sys.argv
    n = 100_000 if quick else 1_000_000
    print(f"== zuzo 混合语言性能基准（循环 N={n:,}）==\n")

    # 1. 启动开销
    base = run_inline("打印(1)")
    single_zuzo = OUT / "loop_zuzo.hy"
    print("【启动开销（中位数，5 次）】")
    print(f"  zuzo -e 空跑           {base*1000:7.1f} ms")
    p = write_tmp("loop_mix_py.hypy", 1)
    print(f"  混合文件（2 块极小）    {run_file(p, (), 5)[0]*1000:7.1f} ms")

    # 2. 解释吞吐
    print(f"\n【循环 N={n:,} 累加（中位数，3 次）】")
    rows = []
    rows.append(loop_bench("loop_zuzo.hy", [], n, "zuzo SAP 路径"))
    rows.append(loop_bench("loop_zuzo.hy", ["--no-sap"], n, "zuzo Tree-Walk"))
    rows.append(loop_bench("loop_mix_zuzo.hypy", [], n, "zuzo 块（混合模式）"))
    rows.append(loop_bench("loop_mix_py.hypy", [], n, "Python 块（混合模式）"))
    for label, t, out in rows:
        print(f"  {label:22s} {t*1000:8.1f} ms   结果={out}")

    # CPython 对照
    src = (OUT / "loop_py.py").read_text(encoding="utf-8").replace("N", str(n))
    cp = OUT / "_tmp_loop_cp.py"
    cp.write_text(src, encoding="utf-8")
    ts = []
    for _ in range(3):
        t0 = time.perf_counter()
        rc = subprocess.run(["python", str(cp)], capture_output=True)
        ts.append(time.perf_counter() - t0)
    py_time = statistics.median(ts)
    print(f"  {'CPython 3.12':22s} {py_time*1000:8.1f} ms   结果={rc.stdout.decode(errors='replace').strip()}")

    # 3. 跨语言调用开销
    calls = 20_000 if quick else 100_000
    p = write_tmp("call_py.hypy", calls)
    t, r = run_file(p, (), 3)
    print(f"\n【跨语言调用（zuzo -> py，N={calls:,} 次）】")
    print(f"  总耗时 {t*1000:8.1f} ms   结果={r.stdout.decode(errors='replace').strip()}")
    per = (t - base) / calls * 1e6
    print(f"  扣除启动后单次约 {per:6.2f} µs")

    # 4. 同步开销
    print("\n【块边界同步开销（交替 20 块，每块 V 个变量）】")
    base_sync = sync_bench(0, 20)[0]
    for v in (100, 1000, 5000):
        t, out = sync_bench(v, 20)
        print(f"  V={v:5d}  {t*1000:8.1f} ms   (相对空块 +{(t-base_sync)*1000:6.1f} ms)  {out}")


if __name__ == "__main__":
    main()
