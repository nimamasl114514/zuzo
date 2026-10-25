a, b = 1, 2
print(a, b)
a, b = b, a
print(a, b)
x, *rest = [1, 2, 3, 4]
print(x, rest)
*init, last = (1, 2, 3)
print(init, last)
(p, q), r = (7, 8), 9
print(p, q, r)
def f(*args, **kw):
    return len(args), sorted(kw.items())
print(f(1, 2, 3, k=1, j=2))
first, second, *_ = "abcdef"
print(first, second)
