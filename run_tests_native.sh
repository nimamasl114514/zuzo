#!/usr/bin/env bash
# zuzo C 后端 · 内嵌 nativecc 对拍（--cc=native 强制，不回退）
cd "$(dirname "$0")" || exit 1
mkdir -p tests_c/_nat
# 运行期警告（越界/缺键）不是程序输出：对拍时关闭，避免与 C 产物 stderr 差异误判
export ZUZO_RT_WARN=0
pass=0; fail=0; failed=""

strip_diag() {
  sed '/^⚠ 类型检查发现以下问题/,/^$/d' "$1"
}

for f in tests_c/t*.hy; do
  b=$(basename "$f" .hy)
  timeout 30 ./zuzo.exe "$f" > "tests_c/_nat/$b.raw.txt" 2>&1
  ri=$?
  strip_diag "tests_c/_nat/$b.raw.txt" > "tests_c/_nat/$b.interp.txt"
  timeout 60 ./zuzo.exe --emit-exe "$f" -o "tests_c/_nat/$b.exe" --cc=native > "tests_c/_nat/$b.build.txt" 2>&1
  rb=$?
  if [ $rb -ne 0 ]; then
    echo "[BUILD-FAIL] $b"
    head -6 "tests_c/_nat/$b.build.txt"
    fail=$((fail+1)); failed="$failed $b"; continue
  fi
  timeout 30 "tests_c/_nat/$b.exe" > "tests_c/_nat/$b.c.txt" 2>&1
  rc=$?
  if diff -q "tests_c/_nat/$b.interp.txt" "tests_c/_nat/$b.c.txt" >/dev/null 2>&1 && [ "$ri" -eq "$rc" ]; then
    pass=$((pass+1))
  else
    echo "[DIFF] $b (interp rc=$ri, native rc=$rc)"
    diff "tests_c/_nat/$b.interp.txt" "tests_c/_nat/$b.c.txt" | head -10
    fail=$((fail+1)); failed="$failed $b"
  fi
done

echo "NATIVE-CC PASS=$pass FAIL=$fail${failed:+ —— 失败:$failed}"
[ "$fail" -eq 0 ]
