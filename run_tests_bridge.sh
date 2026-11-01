#!/usr/bin/env bash
# run_tests_bridge.sh — CPython 子进程桥端到端对拍（第32轮 · 2026-11-01）
#
# 判据：每个 tests_bridge/tNN_*.hy 的 stdout 与同名 .expected 逐字节一致，且退出码 0。
# 期望值来源：CPython 3.12 现算（见 tests_bridge/README 的 python -c 命令），
# 不是「跑一遍看看输出什么就抄什么」——改期望值必须同时给出 python 复算命令。
#
# 用法：
#   ./run_tests_bridge.sh              # 全跑
#   ./run_tests_bridge.sh t05 t09      # 只跑名字含 t05 / t09 的

set -u
cd "$(dirname "$0")" || exit 1

ZUZO=./zuzo.exe
[ -x "$ZUZO" ] || ZUZO=./zuzo

PATTERN="${1:-}"

pass=0
fail=0
failed_list=""

shopt -s nullglob
for f in tests_bridge/t*.hy; do
    name=$(basename "$f" .hy)
    if [ -n "$PATTERN" ] && [[ "$name" != *"$PATTERN"* ]]; then
        continue
    fi
    exp="tests_bridge/$name.expected"
    if [ ! -f "$exp" ]; then
        echo "[SKIP] $name（缺少 $exp）"
        continue
    fi
    out=$("$ZUZO" "$f" 2>/dev/null)
    rc=$?
    if [ "$rc" -ne 0 ]; then
        echo "[FAIL] $name（退出码 $rc）"
        echo "$out" | head -5 | sed 's/^/    /'
        fail=$((fail + 1))
        failed_list="$failed_list $name(rc=$rc)"
        continue
    fi
    if [ "$out" == "$(cat "$exp")" ]; then
        echo "[PASS] $name"
        pass=$((pass + 1))
    else
        echo "[DIFF] $name"
        diff <(printf '%s\n' "$out") "$exp" | head -8 | sed 's/^/    /'
        fail=$((fail + 1))
        failed_list="$failed_list $name(diff)"
    fi
done

echo
echo "BRIDGE PASS=$pass FAIL=$fail"
[ -n "$failed_list" ] && echo "FAILED:$failed_list"
[ "$fail" -eq 0 ]
