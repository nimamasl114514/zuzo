#!/usr/bin/env bash
# zuzo C 后端 · 解释器 vs C 产物 逐字节对拍
#
# 判据：同一份 .hy，解释执行 stdout/rc 与 --emit-exe 产物 stdout/rc 完全一致。
# 说明：解释器的「类型检查警告」块属于编译期诊断，不是程序输出，对拍前统一剥离。
# 退出码：0 = 全过；非 0 = 有用例失败
cd "$(dirname "$0")" || exit 1
mkdir -p tests_c/_out
pass=0; fail=0; failed=""

strip_diag() {
  # 从「⚠ 类型检查」行删到下一个空行
  sed '/^⚠ 类型检查发现以下问题/,/^$/d' "$1"
}

for f in tests_c/t*.hy; do
  b=$(basename "$f" .hy)
  timeout 30 ./zuzo.exe "$f" > "tests_c/_out/$b.raw.txt" 2>&1
  ri=$?
  strip_diag "tests_c/_out/$b.raw.txt" > "tests_c/_out/$b.interp.txt"
  timeout 60 ./zuzo.exe --emit-exe "$f" -o "tests_c/_out/$b.exe" > "tests_c/_out/$b.build.txt" 2>&1
  rb=$?
  if [ $rb -ne 0 ]; then
    echo "[BUILD-FAIL] $b"
    head -6 "tests_c/_out/$b.build.txt"
    fail=$((fail+1)); failed="$failed $b"; continue
  fi
  timeout 30 "tests_c/_out/$b.exe" > "tests_c/_out/$b.c.txt" 2>&1
  rc=$?
  if diff -q "tests_c/_out/$b.interp.txt" "tests_c/_out/$b.c.txt" >/dev/null 2>&1 && [ "$ri" -eq "$rc" ]; then
    pass=$((pass+1))
  else
    echo "[DIFF] $b (interp rc=$ri, c rc=$rc)"
    diff "tests_c/_out/$b.interp.txt" "tests_c/_out/$b.c.txt" | head -12
    fail=$((fail+1)); failed="$failed $b"
  fi
done

echo "C-BACKEND PASS=$pass FAIL=$fail${failed:+ —— 失败:$failed}"
[ "$fail" -eq 0 ]
