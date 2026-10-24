#!/usr/bin/env bash
# zuzo 混合语言 (.hypy) 用例回归：golden 对拍
# 用法：./run_tests_mix.sh         比对
#       ./run_tests_mix.sh --update 重新生成 golden（人工核对后使用）
cd "$(dirname "$0")" || exit 1
mkdir -p tests_mix/_out tests_mix/_expect
update=0
[ "$1" = "--update" ] && update=1
pass=0; fail=0; failed=""
for f in tests_mix/m*.hypy tests_mix/b*.hypy; do
  b=$(basename "$f" .hypy)
  ./zuzo.exe "$f" > "tests_mix/_out/$b.raw" 2>&1
  rc=$?
  tr -d '\r' < "tests_mix/_out/$b.raw" > "tests_mix/_out/$b.txt"
  echo "rc=$rc" >> "tests_mix/_out/$b.txt"
  rm -f "tests_mix/_out/$b.raw"
  if [ $update -eq 1 ]; then
    cp "tests_mix/_out/$b.txt" "tests_mix/_expect/$b.txt"
    echo "[GEN] $b (rc=$rc)"
    continue
  fi
  if diff -q "tests_mix/_expect/$b.txt" "tests_mix/_out/$b.txt" >/dev/null 2>&1; then
    pass=$((pass+1))
  else
    echo "[DIFF] $b"
    diff "tests_mix/_expect/$b.txt" "tests_mix/_out/$b.txt" | head -12
    fail=$((fail+1)); failed="$failed $b"
  fi
done
[ $update -eq 1 ] && { echo "golden 已更新"; exit 0; }
echo "MIX PASS=$pass FAIL=$fail${failed:+ —— 失败:$failed}"
