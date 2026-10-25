def outer(base):
    acc = [0]
    def inner(x):
        acc[0] += x
        return base + x + acc[0]
    return inner

f = outer(3)
print([f(i) for i in range(4)])

def counter():
    c = 0
    def step():
        nonlocal c
        c += 1
        return c
    return step

s = counter()
print(s(), s(), s())

def deco(tag):
    def wrap(fn):
        def inner(*a, **kw):
            return tag + str(fn(*a, **kw))
        return inner
    return wrap

@deco("[")
def add(a, b):
    return a + b

print(add(2, 3))
