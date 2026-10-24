x = 15
if x > 10:
    print("big")
elif x > 5:
    print("mid")
else:
    print("small")

for i in range(5):
    print(i, end=" ")
print()

i = 0
while i < 3:
    print("w", i)
    i += 1

for i in range(10):
    if i == 3:
        continue
    if i == 6:
        break
    print("loop", i)

for i in range(3):
    print("for-else", i)
else:
    print("for-else done")

i = 0
while i < 2:
    i += 1
else:
    print("while-else done")

total = 0
for i in range(1, 101):
    total += i
print("sum 1..100 =", total)

# 嵌套循环
for i in range(3):
    for j in range(3):
        if j > i:
            break
        print(f"({i},{j})", end=" ")
    print()

# 条件表达式
v = "yes" if x > 10 else "no"
print(v)

# 链式比较
print(1 < x < 20, 1 < x < 10)

# 布尔短路
print(True and "a", False or "b", not True)
print(0 or "" or [] or "last")

# 三元与推导
print(["even" if n % 2 == 0 else "odd" for n in range(4)])

# 海象运算符
data = [1, 2, 3, 4, 5]
if (n := len(data)) > 3:
    print("len is", n)

# pass
if True:
    pass
print("end")
