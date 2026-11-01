#!/usr/bin/env bash
# zuzo LLVM 后端 · 解释器 vs LLVM 产物 逐字节对拍
#
# 判据：同一份 .hy，解释执行 stdout/rc 与 --emit-exe 产物 stdout/rc 完全一致。
# 说明：解释器的「类型检查警告」块属于编译期诊断，不是程序输出，对拍前统一剥离。
# 退出码：0 = 全过；非 0 = 有用例失败
cd "$(dirname "$0")" || exit 1
mkdir -p tests_llvm/_out
# 运行期警告（越界/缺键）不是程序输出：对拍时关闭，避免与 LLVM 产物 stderr 差异误判
export ZUZO_RT_WARN=0
pass=0; fail=0; failed=""

strip_diag() {
  # 从「⚠ 类型检查」行删到下一个空行
  sed '/^⚠ 类型检查发现以下问题/,/^$/d' "$1"
}

for f in tests_llvm/t*.hy; do
  b=$(basename "$f" .hy)
  timeout 30 ./zuzo.exe "$f" > "tests_llvm/_out/$b.raw.txt" 2>&1
  ri=$?
  strip_diag "tests_llvm/_out/$b.raw.txt" > "tests_llvm/_out/$b.interp.txt"
  timeout 90 ./zuzo.exe --emit-exe "$f" -o "tests_llvm/_out/$b.exe" > "tests_llvm/_out/$b.build.txt" 2>&1
  rb=$?
  if [ $rb -ne 0 ]; then
    echo "[BUILD-FAIL] $b"
    head -8 "tests_llvm/_out/$b.build.txt"
    fail=$((fail+1)); failed="$failed $b"; continue
  fi
  timeout 30 "tests_llvm/_out/$b.exe" > "tests_llvm/_out/$b.llvm.txt" 2>&1
  rc=$?
  if diff -q "tests_llvm/_out/$b.interp.txt" "tests_llvm/_out/$b.llvm.txt" >/dev/null 2>&1 && [ "$ri" -eq "$rc" ]; then
    pass=$((pass+1))
  else
    echo "[DIFF] $b (interp rc=$ri, llvm rc=$rc)"
    diff "tests_llvm/_out/$b.interp.txt" "tests_llvm/_out/$b.llvm.txt" | head -12
    fail=$((fail+1)); failed="$failed $b"
  fi
done

echo "LLVM-BACKEND PASS=$pass FAIL=$fail"
[ -n "$failed" ] && echo "FAILED:$failed"
[ $fail -eq 0 ]
