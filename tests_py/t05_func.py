def add(a, b):
    return a + b


def greet(name, greeting="Hello"):
    return f"{greeting}, {name}!"


def varargs(*args, **kwargs):
    print("args:", args, "kwargs:", sorted(kwargs.items()))


def fact(n):
    if n <= 1:
        return 1
    return n * fact(n - 1)


def fib(n):
    a, b = 0, 1
    out = []
    for _ in range(n):
        out.append(a)
        a, b = b, a + b
    return out


def make_counter():
    count = 0

    def inc():
        nonlocal count
        count += 1
        return count

    return inc


def apply_twice(f, x):
    return f(f(x))


print(add(1, 2))
print(add(b=3, a=4))
print(greet("zuzo"))
print(greet("zuzo", "Hi"))
print(greet(name="z", greeting="Yo"))
varargs(1, 2, x=3, y=4)
print(fact(5))
print(fib(10))

counter = make_counter()
print(counter(), counter(), counter())

print(apply_twice(lambda x: x * 3, 2))
print((lambda a, b: a * b)(3, 4))

double = lambda x: x * 2
print(list(map(double, [1, 2, 3])))

def kwargs_demo(a, b=2, *args, c=3, **kw):
    return (a, b, args, c, kw)

print(kwargs_demo(1))
print(kwargs_demo(1, 5, 6, 7, c=8, d=9))

def default_mutable(x, acc=[]):
    acc.append(x)
    return acc

print(default_mutable(1))
print(default_mutable(2))

def nested_scope():
    x = 1

    def inner():
        return x + 1

    return inner()

print(nested_scope())

g = 100

def reads_global():
    return g

def writes_global():
    global g
    g = 200

print(reads_global())
writes_global()
print(g)

def multi_return(x):
    return x, x * 2, x * 3

a, b, c = multi_return(3)
print(a, b, c)
print(multi_return(4))

def positional_only(a, /, b, *, c):
    return a + b + c

print(positional_only(1, 2, c=3))

# 函数作为一等公民
funcs = [add, lambda x, y: x - y]
print([f(10, 4) for f in funcs])
