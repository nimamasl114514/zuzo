#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""difffuzz.py — zuzo 与 CPython 的语义差分模糊测试器

思路：随机组合「确定性纯计算」Python 程序模板 → 同一份源码分别交给 CPython 与 zuzo
执行 → 逐字节比较 stdout 与退出码。不一致即记录为差分样本（含源码与双方输出）。

为什么用模板而不是纯随机字节：
  纯随机字节 99% 在语法错误处终止，只测到错误路径；模板生成的是「语法合法、语义有
  明确正确答案」的程序，能真正压到运行时语义（格式化、切片、继承、生成器、异常）。

确定性要求（否则差分无意义）：
  - 不用 random / time / os / 网络 / 文件
  - 不依赖 dict 迭代顺序以外的不确定源（dict 保序是 Python 3.7+ 规范）
  - 浮点统一走 repr/format，避免平台差异

用法：
  python fuzz/difffuzz.py --n 200 --seed 1
  python fuzz/difffuzz.py --n 500 --seed 7 --jobs 4 --zuzo ./zuzo.exe
  python fuzz/difffuzz.py --n 100 --only fmt,str,list

产出：
  fuzz/reports/diff-<ts>.md          汇总报告（含不一致清单与分类）
  fuzz/reports/cases/dNNN_<tpl>.py   失败样本源码
  fuzz/reports/cases/dNNN_<tpl>.cpy.txt / .zz.txt   双方输出
"""

import argparse
import os
import random
import subprocess
import sys
import time
from concurrent.futures import ThreadPoolExecutor

# ---------------- 模板库 ----------------

INT_OPS = ["+", "-", "*", "//", "%", "&", "|", "^"]


def gen_int_arith(rng):
    def expr(depth=0):
        if depth >= 3 or rng.random() < 0.35:
            return str(rng.randint(-50, 200))
        op = rng.choice(INT_OPS)
        a, b = expr(depth + 1), expr(depth + 1)
        if op in ("//", "%"):
            # 除零可用性：右侧换成非零字面量，仍保留表达式复杂度
            return f"({a} {op} {rng.randint(1, 50)})"
        return f"({a} {op} {b})"

    lines = ["print(" + expr() + ")" for _ in range(6)]
    return "int_arith", "\n".join(lines) + "\n"


def gen_bigint(rng):
    e = rng.choice([40, 62, 64, 100, 200])
    lines = [
        f"a = 2 ** {e}",
        "print(a)",
        "print(a + 1, a - 1)",
        f"print(a // {rng.randint(3, 97)})",
        f"print(a % {rng.randint(3, 97)})",
        f"print(-a // 7, -a % 7)",
        "print(len(str(a)))",
        f"print(a >> {rng.randint(1, 30)}, a & 0xFFFF)",
    ]
    return "bigint", "\n".join(lines) + "\n"


def gen_float_fmt(rng):
    vals = ["0.1", "1.5", "-2.25", "3.14159265", "1e10", "1e-7", "1234567.891", "-0.0"]
    specs = [".2f", ".0f", "10.3f", ">8.2f", "<8.2f", "^8.2f", ".3%", ".1e"]
    rng.shuffle(vals)
    lines = []
    for v in vals[:5]:
        for s in rng.sample(specs, 3):
            lines.append(f'print(f"{{{v}:{s}}}")')
    lines.append('print(f"{1234567:,} {1234567:_}")')
    lines.append('print(f"{-1234567:,}")')
    return "float_fmt", "\n".join(lines) + "\n"


STR_METHODS = [
    's.upper()', 's.lower()', 's.title()', 's.strip()', 's.lstrip(" a")',
    's.rstrip("!")', 's.split(",")', '"-".join(s.split())', 's.replace("a", "@")',
    's.find("b")', 's.rfind("b")', 's.count("a")', 's.startswith("ab")',
    's.endswith("yz")', 's.zfill(12)', 's.center(11, "*")', 's.ljust(11, ".")',
    's.rjust(11, ".")', 's.swapcase()', 's.partition(",")', 's.capitalize()',
]


def gen_str_ops(rng):
    s = rng.choice(["  ab,cb,abyz  ", "abcabc", "a b c", "Hello,World", "xyzxyz"])
    lines = [f"s = {s!r}"]
    for m in rng.sample(STR_METHODS, 6):
        lines.append(f"print({m})")
    lines += [
        "print(s[1:5], s[-3:], s[::2], s[::-1])",
        "print(s[100:200], s[-100:2])",
        'print(len(s), s * 2)',
        'print("a" in s, "ab" in s)',
    ]
    return "str_ops", "\n".join(lines) + "\n"


def gen_list_ops(rng):
    base = [rng.randint(-20, 20) for _ in range(rng.randint(5, 9))]
    lines = [f"a = {base!r}"]
    ops = [
        "print(sorted(a))",
        "print(sorted(a, reverse=True))",
        "print(sorted(a, key=abs))",
        "b = a[:]; b.sort(); print(b)",
        "print(sum(a), min(a), max(a), len(a))",
        "print(any(x > 0 for x in a), all(x > 0 for x in a))",
        "print([x for x in a if x % 2])",
        "print(list(map(abs, a)))",
        "print(list(filter(lambda x: x < 0, a)))",
        "print(list(zip(a, range(len(a)))))",
        "print(list(enumerate(a[:3], start=1)))",
        "b = a[:]; b.append(99); b.insert(0, -99); print(b)",
        "b = a[:]; print(b.pop(), b.pop(0), b)",
        "print(a.index(max(a)), a.count(a[0]))",
        "print(a + [1, 2], [0] * 3)",
        "print(a[1:-1:2])",
        "print(reversed(a) == a[::-1])",
        "print(list(reversed(a)))",
    ]
    lines += rng.sample(ops, 8)
    return "list_ops", "\n".join(lines) + "\n"


def gen_dict_ops(rng):
    k = ["a", "b", "c", "d", "e"][: rng.randint(3, 5)]
    lines = [f"d = {{{', '.join(f'{x!r}: {i * 10}' for i, x in enumerate(k))}}}"]
    lines += [
        "print(sorted(d.items()))",
        "print(sorted(d.keys()), sorted(d.values()))",
        "print(d.get('zz'), d.get('zz', -1))",
        "d2 = dict(d); d2.update({'n': 1}); print(sorted(d2.items()))",
        "print(dict.fromkeys(['p', 'q'], 0))",
        "print({v: k2 for k2, v in d.items()})",
        "d3 = dict(d); print(d3.setdefault('z', 7), d3.setdefault('a', 9), sorted(d3.items()))",
        "d4 = dict(d); d4.pop('a'); print(sorted(d4.items()))",
        "print('a' in d, 'zz' in d)",
        "print(len(d), list(d)[0])",
    ]
    return "dict_ops", "\n".join(lines) + "\n"


def gen_class_ops(rng):
    v = rng.randint(1, 9)
    code = '''class A:
    kind = "A"

    def __init__(self, x):
        self.x = x

    def show(self):
        return f"A({self.x})"

    def __repr__(self):
        return f"A:{self.x}"

    def __eq__(self, other):
        return isinstance(other, A) and self.x == other.x

class B(A):
    kind = "B"

    def __init__(self, x, y):
        super().__init__(x)
        self.y = y

    def show(self):
        return f"B({{self.x}},{{self.y}})"

class C(B):
    def show(self):
        return "C+" + super().show()

a = A(%d)
b = B(%d, %d)
c = C(%d, %d)
for o in (a, b, c):
    print(o.show(), repr(o), o.kind, type(o).__name__)
print(isinstance(c, A), isinstance(a, B), issubclass(C, A))
print(a == A(%d), a == b)
print(A.__mro__[0].__name__, B.__mro__[1].__name__)
print([type(x).__name__ for x in (a, b, c)])
''' % (v, v, v + 1, v + 2, v + 3, v)
    return "class_ops", code


def gen_generator(rng):
    n = rng.randint(3, 7)
    code = f'''def g(n):
    for i in range(n):
        yield i * i

def nested(n):
    yield from g(n)
    yield "end"

print(list(g({n})))
print(list(nested({n})))
print(sum(g({n})))
s = g({n})
print(next(s), next(s))
print([x for x in g({n}) if x % 2 == 0])
print(tuple(nested({n})))
'''
    return "generator", code


def gen_exception(rng):
    code = '''def f(mode):
    if mode == 1:
        return 1 / 0
    if mode == 2:
        return int("not-a-number")
    if mode == 3:
        raise KeyError("missing")
    return 0

for m in (1, 2, 3, 4):
    try:
        print("v", f(m))
    except ZeroDivisionError as e:
        print("ZDE", type(e).__name__)
    except (ValueError, KeyError) as e:
        print("EXC", type(e).__name__, str(e).strip("'\\""))
    else:
        print("ok")
    finally:
        print("fin", m)
'''
    return "exception", code


def gen_closure(rng):
    n = rng.randint(2, 6)
    code = f'''def outer(base):
    acc = [0]
    def inner(x):
        acc[0] += x
        return base + x + acc[0]
    return inner

f = outer({n})
print([f(i) for i in range(4)])

def counter():
    c = 0
    def step():
        nonlocal c
        c += 1
        return c
    return step

s = counter()
print(s(), s(), s())

def deco(tag):
    def wrap(fn):
        def inner(*a, **kw):
            return tag + str(fn(*a, **kw))
        return inner
    return wrap

@deco("[")
def add(a, b):
    return a + b

print(add(2, 3))
'''
    return "closure", code


def gen_slice(rng):
    s = rng.choice(["abcdefghij", "0123456789", "中文测试字符串"])
    idx = rng.sample([0, 1, 2, 3, -1, -2, -3, 5, 8, 20, -20], 5)
    lines = [f"s = {s!r}"]
    for i in idx:
        lines.append(f"print(s[{i}] if -len(s) <= {i} < len(s) else 'OOR')")
    lines += [
        "print(s[2:6], s[:3], s[4:], s[:])",
        "print(s[::2], s[1::3], s[::-2])",
        "print(s[6:2:-1], s[-1:-5:-1])",
        "print(len(s), s[-1], s[0])",
    ]
    return "slice", "\n".join(lines) + "\n"


def gen_comprehension(rng):
    n = rng.randint(3, 6)
    code = f'''print([i * j for i in range({n}) for j in range(i)])
print({{i: i * i for i in range({n})}})
print({{i % 3 for i in range({n * 2})}})
print([[i + j for j in range(2)] for i in range({n})])
print(sum(i for i in range({n}) if i % 2))
print([x for x in range(20) if x % 2 == 0 if x % 3 == 0])
'''
    return "comprehension", code


def gen_higher_order(rng):
    n = rng.randint(4, 8)
    code = f'''vals = list(range({n}))
print(sorted(vals, key=lambda x: -x))
print(list(map(lambda x: x * x, vals)))
print(list(filter(lambda x: x % 2, vals)))
print(list(zip(vals, map(str, vals))))
print(dict(zip(map(str, vals), vals)))
pairs = [("b", 2), ("a", 1), ("c", 3)]
print(sorted(pairs))
print(sorted(pairs, key=lambda p: p[1], reverse=True))
print(max(vals), min(vals), sum(vals), sorted(vals))
print(all(v >= 0 for v in vals), any(v > 100 for v in vals))
'''
    return "higher_order", code


def gen_match(rng):
    v = rng.randint(1, 4)
    code = f'''def handle(x):
    match x:
        case 0:
            return "zero"
        case 1 | 2:
            return "small"
        case [a, b]:
            return f"list {{a}} {{b}}"
        case {{"k": val}}:
            return f"dict {{val}}"
        case _:
            return "other"

for arg in (0, {v}, [3, 4], {{"k": 9}}, "s"):
    print(handle(arg))
'''
    return "match", code


def gen_unpack(rng):
    code = '''a, b = 1, 2
print(a, b)
a, b = b, a
print(a, b)
x, *rest = [1, 2, 3, 4]
print(x, rest)
*init, last = (1, 2, 3)
print(init, last)
(p, q), r = (7, 8), 9
print(p, q, r)
def f(*args, **kw):
    return len(args), sorted(kw.items())
print(f(1, 2, 3, k=1, j=2))
first, second, *_ = "abcdef"
print(first, second)
'''
    return "unpack", code


GENERATORS = [
    gen_int_arith, gen_bigint, gen_float_fmt, gen_str_ops, gen_list_ops,
    gen_dict_ops, gen_class_ops, gen_generator, gen_exception, gen_closure,
    gen_slice, gen_comprehension, gen_higher_order, gen_match, gen_unpack,
]

TEMPLATE_NAMES = {
    "int_arith": "整数算术/位运算",
    "bigint": "大整数",
    "float_fmt": "浮点格式化",
    "str_ops": "字符串方法/切片",
    "list_ops": "列表操作",
    "dict_ops": "字典操作",
    "class_ops": "类/继承/super/dunder",
    "generator": "生成器/yield from",
    "exception": "异常族/finally",
    "closure": "闭包/nonlocal/装饰器",
    "slice": "切片/索引边界",
    "comprehension": "推导式",
    "higher_order": "高阶函数",
    "match": "match/case",
    "unpack": "解包/星号",
}


# ---------------- 执行与比较 ----------------

def run(cmd, timeout, cwd):
    try:
        p = subprocess.run(cmd, capture_output=True, timeout=timeout, cwd=cwd)
        out = p.stdout.decode("utf-8", "replace").replace("\r\n", "\n")
        err = p.stderr.decode("utf-8", "replace").replace("\r\n", "\n")
        return p.returncode, out, err
    except subprocess.TimeoutExpired:
        return -999, "", "<TIMEOUT>"
    except Exception as e:  # noqa: BLE001
        return -998, "", f"<RUNNER-ERROR {e}>"


def one_case(args, idx, rng_seed):
    rng = random.Random(rng_seed)
    gen = rng.choice(GENERATORS)
    tpl, code = gen(rng)
    workdir = os.path.join(args.out, "work")
    os.makedirs(workdir, exist_ok=True)
    src_path = os.path.abspath(os.path.join(workdir, f"d{idx:04d}_{tpl}.py"))
    with open(src_path, "w", encoding="utf-8", newline="\n") as f:
        f.write(code)

    rc_py, out_py, err_py = run([sys.executable, src_path], args.timeout, workdir)
    rc_zz, out_zz, err_zz = run([os.path.abspath(args.zuzo), src_path], args.timeout, workdir)

    same = (rc_py == rc_zz) and (out_py == out_zz)
    return {
        "idx": idx, "tpl": tpl, "code": code, "path": src_path,
        "rc_py": rc_py, "rc_zz": rc_zz, "out_py": out_py, "out_zz": out_zz,
        "err_py": err_py, "err_zz": err_zz, "same": same,
        "timeout": rc_zz == -999,
    }


def first_diff(a, b):
    la, lb = a.splitlines(), b.splitlines()
    for i in range(max(len(la), len(lb))):
        x = la[i] if i < len(la) else "<缺行>"
        y = lb[i] if i < len(lb) else "<缺行>"
        if x != y:
            return i + 1, x, y
    return 0, "", ""


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--n", type=int, default=100)
    ap.add_argument("--seed", type=int, default=1)
    ap.add_argument("--zuzo", default="./zuzo.exe")
    ap.add_argument("--timeout", type=float, default=15.0)
    ap.add_argument("--jobs", type=int, default=1)
    ap.add_argument("--out", default="fuzz/reports")
    ap.add_argument("--only", default="", help="逗号分隔模板名，只跑这些")
    args = ap.parse_args()

    os.makedirs(args.out, exist_ok=True)
    ts = time.strftime("%Y%m%d-%H%M%S")

    seeds = [args.seed * 100000 + i for i in range(args.n)]
    if args.only:
        allow = {x.strip() for x in args.only.split(",") if x.strip()}
        filtered = []
        for s in seeds:
            rng = random.Random(s)
            g = rng.choice(GENERATORS)
            name, _ = g(rng)
            if name in allow:
                filtered.append(s)
        seeds = filtered

    t0 = time.time()
    results = []
    if args.jobs > 1:
        with ThreadPoolExecutor(max_workers=args.jobs) as ex:
            futs = [ex.submit(one_case, args, i, s) for i, s in enumerate(seeds)]
            for fu in futs:
                results.append(fu.result())
    else:
        for i, s in enumerate(seeds):
            results.append(one_case(args, i, s))
            if (i + 1) % 20 == 0:
                print(f"  ... {i + 1}/{len(seeds)}", flush=True)

    dur = time.time() - t0
    bad = [r for r in results if not r["same"]]
    timeouts = [r for r in bad if r["timeout"]]

    cases_dir = os.path.join(args.out, "cases")
    if bad:
        os.makedirs(cases_dir, exist_ok=True)

    report = os.path.join(args.out, f"diff-{ts}.md")
    with open(report, "w", encoding="utf-8", newline="\n") as f:
        f.write(f"# zuzo 语义差分模糊测试报告\n\n")
        f.write(f"- 时间：{ts}\n- 种子：{args.seed}\n- 用例数：{len(results)}\n")
        f.write(f"- zuzo：`{os.path.abspath(args.zuzo)}`\n")
        f.write(f"- 用时：{dur:.1f}s（jobs={args.jobs}）\n")
        f.write(f"- **不一致：{len(bad)}（其中超时 {len(timeouts)}）**\n\n")

        by_tpl = {}
        for r in results:
            by_tpl.setdefault(r["tpl"], [0, 0])
            by_tpl[r["tpl"]][0] += 1
            if not r["same"]:
                by_tpl[r["tpl"]][1] += 1
        f.write("## 按模板统计\n\n| 模板 | 说明 | 用例 | 不一致 |\n|---|---|---|---|\n")
        for tpl in sorted(by_tpl):
            tot, nbad = by_tpl[tpl]
            f.write(f"| `{tpl}` | {TEMPLATE_NAMES.get(tpl, '')} | {tot} | {nbad} |\n")

        f.write("\n## 不一致明细\n\n")
        for k, r in enumerate(bad):
            tag = f"d{k:03d}_{r['tpl']}"
            f.write(f"### {tag}（seed 用例 idx={r['idx']}）\n\n")
            f.write(f"- 退出码：CPython={r['rc_py']} / zuzo={r['rc_zz']}\n")
            line_no, exp, got = first_diff(r["out_py"], r["out_zz"])
            if line_no:
                f.write(f"- 首个差异在第 {line_no} 行：\n")
                f.write(f"  - CPython: `{exp}`\n  - zuzo   : `{got}`\n")
            if r["timeout"]:
                f.write("- **超时（疑似挂死）**\n")
            if r["err_zz"]:
                f.write(f"- zuzo stderr（前 600 字）：\n```\n{r['err_zz'][:600]}\n```\n")
            f.write("\n<details><summary>源码</summary>\n\n```python\n")
            f.write(r["code"])
            f.write("```\n\n</details>\n\n")

            src_copy = os.path.join(cases_dir, f"{tag}.py")
            with open(src_copy, "w", encoding="utf-8", newline="\n") as cf:
                cf.write(r["code"])
            with open(os.path.join(cases_dir, f"{tag}.cpy.txt"), "w", encoding="utf-8", newline="\n") as cf:
                cf.write(r["out_py"])
            with open(os.path.join(cases_dir, f"{tag}.zz.txt"), "w", encoding="utf-8", newline="\n") as cf:
                cf.write(r["out_zz"])

    print(f"\n=== 差分 fuzz 完成：{len(results)} 例 / 不一致 {len(bad)}（超时 {len(timeouts)}）/ {dur:.1f}s")
    print(f"报告：{os.path.abspath(report)}")
    if bad:
        print("不一致模板：", end="")
        cnt = {}
        for r in bad:
            cnt[r["tpl"]] = cnt.get(r["tpl"], 0) + 1
        print(", ".join(f"{k}×{v}" for k, v in sorted(cnt.items())))
    return 0 if not bad else 1


if __name__ == "__main__":
    sys.exit(main())
