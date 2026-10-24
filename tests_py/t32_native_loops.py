# 原生整数循环覆盖：各种 while 形态 + 不可原生场景回退
# 与 CPython 对拍，验证 py/runtime/native_loop.go 的语义一致性

# 1) 小于
s1 = 0
i = 0
while i < 10000:
    s1 += i
    i += 1
print("lt", s1)

# 2) 小于等于
s2 = 0
j = 0
while j <= 10000:
    s2 = s2 + j
    j = j + 1
print("le", s2)

# 3) 递减（大于）
s3 = 0
k = 10000
while k > 0:
    s3 += k
    k -= 1
print("gt", s3)

# 4) 大于等于
s4 = 0
k2 = 10000
while k2 >= 0:
    s4 += k2
    k2 -= 1
print("ge", s4)

# 5) 步长 2
s5 = 0
m = 0
while m < 20000:
    s5 += m
    m += 2
print("step2", s5)

# 6) 多变量联动
a = 0
b = 0
n = 0
while n < 5000:
    a += n
    b = b + a
    n += 1
print("multi", a, b)

# 7) 乘法
p = 1
q = 1
while q < 20:
    p = p * q
    q += 1
print("mul", p)

# 8) 小循环（估算 < 1000，应走原解释路径）
s8 = 0
r = 0
while r < 10:
    s8 += r
    r += 1
print("small", s8)

# 9) 浮点累加（不匹配原生，走原路径）
f = 0.0
t = 0
while t < 10000:
    f = f + 0.5
    t += 1
print("float", f)

# 10) 条件边界是变量（不匹配原生，走原路径）
lim = 3000
s10 = 0
u = 0
while u < lim:
    s10 += u
    u += 1
print("var-bound", s10)

# 11) 循环体内含函数调用（不匹配原生）
s11 = 0
w = 0
while w < 2000:
    s11 += len("abcd")
    w += 1
print("call", s11)

# 12) 循环变量初值非 0
s12 = 0
z = 5000
while z < 15000:
    s12 += z
    z += 1
print("nonzero-start", s12)