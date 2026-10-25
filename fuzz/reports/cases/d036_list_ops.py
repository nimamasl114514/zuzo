a = [-12, -14, -12, 15, 7, -17, -20]
print(sorted(a, key=abs))
print(reversed(a) == a[::-1])
print(list(reversed(a)))
print(sum(a), min(a), max(a), len(a))
print(a + [1, 2], [0] * 3)
print(a[1:-1:2])
print(sorted(a, reverse=True))
b = a[:]; b.append(99); b.insert(0, -99); print(b)
