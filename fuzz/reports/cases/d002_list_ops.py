a = [19, -5, -4, 20, 2, 17, 4]
b = a[:]; b.append(99); b.insert(0, -99); print(b)
print([x for x in a if x % 2])
print(sorted(a))
print(reversed(a) == a[::-1])
print(sum(a), min(a), max(a), len(a))
print(a + [1, 2], [0] * 3)
print(any(x > 0 for x in a), all(x > 0 for x in a))
print(list(zip(a, range(len(a)))))
