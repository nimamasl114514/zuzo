def g(n):
    for i in range(n):
        yield i * i

def nested(n):
    yield from g(n)
    yield "end"

print(list(g(7)))
print(list(nested(7)))
print(sum(g(7)))
s = g(7)
print(next(s), next(s))
print([x for x in g(7) if x % 2 == 0])
print(tuple(nested(7)))
