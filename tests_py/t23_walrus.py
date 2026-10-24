# 海象运算符 := 的各种用法
data = [1, 3, 5, 8, 12, 15, 20]

if (n := len(data)) > 5:
    print("count", n)

queue = list(data)
total = 0
while (item := queue.pop(0) if queue else None) is not None:
    total += item
print("total", total, "left", queue)

values = [2, 4, 6]
if (cnt := len(values)) and (avg := sum(values) / cnt) > 3:
    print(cnt, avg)

print([y for x in range(6) if (y := x * x) % 4 == 0])

counter = 0
print([(counter := counter + 1) for _ in range(5)])
print(counter)

def read_until(stop):
    out = []
    stream = iter("abcdef")
    while (ch := next(stream, "")) != stop and ch:
        out.append(ch)
    return "".join(out)

print(read_until("d"))
print(read_until("z"))

def parse(s):
    return int(s) if s.isdigit() else None

for s in ["7", "x", "9"]:
    if (v := parse(s)) is not None:
        print("ok", v)
    else:
        print("bad", repr(s))

text = "a1 b22 c333"
import re

print([int(m.group(1)) for m in re.finditer(r"[a-z](\d+)", text)])

found = None
for token in ["", "  ", "hit"]:
    if (found := token.strip()) :
        print("first non-empty:", found)
        break
else:
    print("all empty")

lengths = {}
for w in ["aa", "b", "ccc"]:
    lengths[w] = (ln := len(w)) * 10
print(sorted(lengths.items()), ln)
