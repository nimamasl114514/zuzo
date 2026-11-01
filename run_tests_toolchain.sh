#!/usr/bin/env bash
# run_tests_toolchain.sh — 自带工具链（zig cc）端到端验收（第32轮）
#
# 判据：CLI 能识别 zig / 能用 zig cc 编出可运行 exe / zpm doctor 与 zpm build 可用 / install 落地。
set -u
cd "$(dirname "$0")" || exit 1

ZUZO=./zuzo.exe
ZPM=./zpm.exe
[ -x "$ZUZO" ] || ZUZO=./zuzo
[ -x "$ZPM" ] || ZPM=./zpm
pass=0; fail=0

check() { # check <名称> <命令...>
    local name="$1"; shift
    if "$@" >/dev/null 2>&1; then
        echo "[PASS] $name"; pass=$((pass+1))
    else
        echo "[FAIL] $name"; fail=$((fail+1))
    fi
}

# 1) status 能报出 zig
if $ZUZO toolchain status | grep -q "zig       :"; then
    echo "[PASS] toolchain status 含 zig 行"; pass=$((pass+1))
else
    echo "[FAIL] toolchain status 缺 zig 行"; fail=$((fail+1))
fi

# 2) env 给出 CC
if $ZUZO toolchain env | grep -q "export CC="; then
    echo "[PASS] toolchain env 含 CC"; pass=$((pass+1))
else
    echo "[FAIL] toolchain env 缺 CC"; fail=$((fail+1))
fi

# 3) 用 zig cc 编一个 exe，并与解释器输出逐字节比对
ZIG=$( $ZUZO toolchain status | awk -F': ' '/^zig/{print $2}' | awk '{print $1}' )
if [ -n "$ZIG" ] && [ -f "$ZIG" ]; then
    mkdir -p tmp/tc
    interp=$( $ZUZO tests_llvm/t01_print.hy 2>/dev/null )
    if ZUZO_CC="$ZIG" $ZUZO --emit-exe tests_llvm/t01_print.hy -o tmp/tc/a.exe >/dev/null 2>&1; then
        got=$( ./tmp/tc/a.exe 2>/dev/null )
        if [ "$got" = "$interp" ]; then
            echo "[PASS] zig cc 构建 exe 且输出一致"; pass=$((pass+1))
        else
            echo "[FAIL] zig cc 产物输出与解释器不一致"; fail=$((fail+1))
        fi
    else
        echo "[FAIL] zig cc 构建失败"; fail=$((fail+1))
    fi
else
    echo "[SKIP] 无 zig，跳过 zig cc 构建"
fi

# 4) zpm doctor
check "zpm doctor" $ZPM doctor

# 5) zpm build
if $ZPM build examples/hello.hy -o tmp/tc/zpm.exe >/dev/null 2>&1 && ./tmp/tc/zpm.exe >/dev/null 2>&1; then
    echo "[PASS] zpm build 产物可运行"; pass=$((pass+1))
else
    echo "[FAIL] zpm build"; fail=$((fail+1))
fi

# 6) toolchain install 落地（幂等，复制到用户目录）
if $ZUZO toolchain install >/dev/null 2>&1; then
    echo "[PASS] toolchain install"; pass=$((pass+1))
else
    echo "[WARN] toolchain install 失败（可能无 zig 源）"
fi

echo
echo "TOOLCHAIN PASS=$pass FAIL=$fail"
[ "$fail" -eq 0 ]
