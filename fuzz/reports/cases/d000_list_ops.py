a = [13, -3, -12, -20, 4]
print(list(zip(a, range(len(a)))))
print(any(x > 0 for x in a), all(x > 0 for x in a))
print(list(reversed(a)))
print(list(enumerate(a[:3], start=1)))
print(a.index(max(a)), a.count(a[0]))
print([x for x in a if x % 2])
b = a[:]; print(b.pop(), b.pop(0), b)
print(reversed(a) == a[::-1])
