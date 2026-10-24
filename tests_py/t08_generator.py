def gen_numbers(n):
    for i in range(n):
        yield i


def gen_fib():
    a, b = 0, 1
    while True:
        yield a
        a, b = b, a + b


def gen_with_return():
    yield 1
    yield 2
    return "done"


def gen_nested():
    yield from gen_numbers(3)
    yield from [10, 20]


print(list(gen_numbers(5)))

g = gen_numbers(3)
print(next(g), next(g), next(g))
try:
    next(g)
except StopIteration:
    print("stop")

fib = gen_fib()
print([next(fib) for _ in range(10)])

print(list(gen_with_return()))
print(list(gen_nested()))

squares = (x * x for x in range(5))
print(list(squares))

total = sum(x * x for x in range(10))
print(total)

print(any(x > 3 for x in [1, 2, 3, 4]))
print(all(x > 0 for x in [1, 2, 3]))

def read_lines(lines):
    for line in lines:
        yield line.strip()

print(list(read_lines(["  a  ", " b ", "c"])))


def infinite_counter():
    i = 0
    while True:
        v = yield i
        if v == "stop":
            break
        i += 1


c = infinite_counter()
print(next(c), next(c), next(c))
try:
    c.send("stop")
except StopIteration:
    print("counter stopped")

# 生成器管道
def pipeline(nums):
    doubled = (x * 2 for x in nums)
    filtered = (x for x in doubled if x % 3 == 0)
    return list(filtered)

print(pipeline(range(10)))
