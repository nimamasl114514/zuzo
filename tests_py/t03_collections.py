nums = [3, 1, 4, 1, 5, 9, 2, 6]
print(nums)
print(len(nums), nums[0], nums[-1], nums[2:5], nums[::-1])
nums.append(10)
nums.insert(0, 0)
nums.remove(1)
print(nums)
print(nums.pop(), nums.pop(0))
nums.sort()
print(nums)
nums.sort(reverse=True)
print(nums)
print(sorted([3, 1, 2]), sorted(["b", "a"], reverse=True))
print(max(nums), min(nums), sum(nums))

squares = [x * x for x in range(10)]
print(squares)
evens = [x for x in range(20) if x % 2 == 0]
print(evens)
print([x + y for x in [1, 2] for y in [10, 20]])
print([[i * j for j in range(3)] for i in range(3)])

d = {"name": "zuzo", "age": 3}
d["lang"] = "chinese"
print(d)
print(d["name"], d.get("nokey"), d.get("nokey", "default"))
print(len(d), list(d.keys()), list(d.values()), list(d.items()))
d.update({"age": 4})
print(d)
print(d.pop("lang"), d)
print({k: v * 2 for k, v in d.items()})
print(dict(a=1, b=2))
print("name" in d, "x" in d)

t = (1, 2, 3)
print(t, t[0], t[-1], len(t))
print(t + (4, 5), t * 2)
a, b, c = t
print(a, b, c)
x, *rest = [1, 2, 3, 4]
print(x, rest)

s1 = {1, 2, 3}
s2 = {3, 4, 5}
print(s1 | s2, s1 & s2, s1 - s2, s1 ^ s2)
s1.add(4)
s1.discard(1)
print(sorted(s1))
print({x % 3 for x in range(10)})

r = range(1, 10, 2)
print(list(r), len(r), r[0], r[-1])
print(list(reversed([1, 2, 3])))
print(list(enumerate(["a", "b"])))
print(list(zip([1, 2, 3], ["a", "b", "c"])))
print(list(map(lambda x: x * 2, [1, 2, 3])))
print(list(filter(lambda x: x > 1, [1, 2, 3])))

nested = [[1, 2], [3, 4]]
print(nested[0][1])
copy1 = nums[:]
copy1.append(999)
print(len(nums), len(copy1))
