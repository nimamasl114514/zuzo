#!/usr/bin/env bash
# zuzo Python 运行时 · 与 CPython 对拍
cd "$(dirname "$0")" || exit 1
mkdir -p tests_py/_out
pass=0; fail=0; failed=""
for f in tests_py/t*.py; do
  b=$(basename "$f" .py)
  [ "$b" = "t11_helper" ] && continue
  python "$f" 2>&1 | tr -d '\r' > "tests_py/_out/$b.cpython.txt"
  timeout 30 ./zuzo.exe "$f" 2>&1 | tr -d '\r' > "tests_py/_out/$b.zuzo.txt"
  if diff -q "tests_py/_out/$b.cpython.txt" "tests_py/_out/$b.zuzo.txt" >/dev/null 2>&1; then
    pass=$((pass+1))
  else
    echo "[DIFF] $b"
    diff "tests_py/_out/$b.cpython.txt" "tests_py/_out/$b.zuzo.txt" | head -12
    fail=$((fail+1)); failed="$failed $b"
  fi
done
echo "PASS=$pass FAIL=$fail${failed:+ —— 失败:$failed}"
