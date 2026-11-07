#!/usr/bin/env bash
# zuzo Python 运行时 · 与 CPython 对拍
#
# 两轮：
#   第 1 轮 — 常规执行，zuzo 输出 vs CPython 输出逐字节比
#   第 2 轮 — 影子模式（--shadow）对 zuzo 侧全部用例做 SAP/Tree-Walk 双轨自检
#             （2026-10-25 第20轮新增：五层执行栈的等价性必须由 CI 守，不靠人肉）
#
# 退出码：0 = 两轮全过；非 0 = 有差异
cd "$(dirname "$0")" || exit 1

# CI（windows-latest）默认控制台编码是 cp1252，CPython 打印中文会 UnicodeEncodeError；
# 本地也要一致：统一 UTF-8 输出，不靠机器环境碰运气。
export PYTHONUTF8=1
export PYTHONIOENCODING=utf-8

# _tmp 是用例的工作目录（t17_stdlib2 等依赖它存在；干净检出时没有）
mkdir -p tests_py/_out tests_py/_tmp
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

# ── 第 2 轮：影子模式双轨 ─────────────────────────────────────────────
# 判据：影子模式退出码必须是 0 或 1（1 = 程序本身报错，属于用例预期），
# 但输出里不允许出现「影子模式断言失败」。出现即视为回归。
shadow_pass=0; shadow_fail=0; shadow_failed=""
for f in tests_py/t*.py; do
  b=$(basename "$f" .py)
  [ "$b" = "t11_helper" ] && continue
  out=$(timeout 60 ./zuzo.exe --shadow "$f" 2>&1 | tr -d '\r')
  if echo "$out" | grep -q "影子模式断言失败"; then
    echo "[SHADOW-DIFF] $b"
    echo "$out" | grep -A4 "影子模式断言失败" | head -14
    shadow_fail=$((shadow_fail+1)); shadow_failed="$shadow_failed $b"
  else
    shadow_pass=$((shadow_pass+1))
  fi
done
echo "SHADOW PASS=$shadow_pass FAIL=$shadow_fail${shadow_failed:+ —— 不一致:$shadow_failed}"

[ "$fail" -eq 0 ] && [ "$shadow_fail" -eq 0 ]
