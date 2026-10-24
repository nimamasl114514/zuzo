# 列表/字典/集合推导式与生成器表达式
nums = list(range(1, 11))
print(nums)

squares = [n * n for n in nums]
print(squares)

evens = [n for n in nums if n % 2 == 0]
print(evens)

mixed = [n * 2 if n % 2 else n for n in nums]
print(mixed)

matrix = [[r * 3 + c for c in range(3)] for r in range(3)]
print(matrix)

flat = [v for row in matrix for v in row]
print(flat)

flat_odd = [v for row in matrix for v in row if v % 2 == 1]
print(flat_odd)

words = ["apple", "Banana", "cherry"]
print([w.upper() for w in words])
print([w[0] for w in words if len(w) > 5])

lengths = {w: len(w) for w in words}
print(lengths)
print(dict(sorted(lengths.items())))

print(sorted({n % 5 for n in nums}))

pairs = [(a, b) for a in range(2) for b in range(2) if a != b]
print(pairs)

swapped = {v: k for k, v in {"x": 1, "y": 2}.items()}
print(sorted(swapped.items()))

gen = (n * n for n in nums)
print(type(gen).__name__)
print(sum(gen))
print(sum(n * n for n in nums))

names = ["ana", "bob", "cid"]
print(list(enumerate(names)))
print([f"{i}:{n}" for i, n in enumerate(names)])

nested = [[1, 2], [3], []]
print([len(inner) for inner in nested])
print(max(len(inner) for inner in nested))
print(any(not inner for inner in nested), all(inner for inner in nested))
