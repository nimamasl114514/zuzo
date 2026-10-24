class Resource:
    def __init__(self, name):
        self.name = name
        self.log = []

    def __enter__(self):
        self.log.append("enter")
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        self.log.append("exit")
        return False


with Resource("r1") as r:
    r.log.append("body")
print(r.log)

try:
    with Resource("r2") as r2:
        r2.log.append("body")
        raise ValueError("x")
except ValueError:
    print("caught", r2.log)

with Resource("a") as a, Resource("b") as b:
    a.log.append("x")
    b.log.append("y")
print(a.log, b.log)


class Suppress:
    def __enter__(self):
        return self

    def __exit__(self, exc_type, exc_val, exc_tb):
        return exc_type is ValueError


with Suppress():
    raise ValueError("suppressed")
print("after suppress")

# 装饰器
def logged(fn):
    def wrapper(*args, **kwargs):
        result = fn(*args, **kwargs)
        print(f"call {fn.__name__}{args} -> {result}")
        return result

    return wrapper


@logged
def add(a, b):
    return a + b


add(1, 2)
add(3, 4)


def repeat(n):
    def deco(fn):
        def wrapper(*args, **kwargs):
            return [fn(*args, **kwargs) for _ in range(n)]

        return wrapper

    return deco


@repeat(3)
def hello():
    return "hi"


print(hello())


def memo(fn):
    cache = {}

    def wrapper(n):
        if n not in cache:
            cache[n] = fn(n)
        return cache[n]

    return wrapper


@memo
def slow_fib(n):
    return n if n < 2 else slow_fib(n - 1) + slow_fib(n - 2)


print(slow_fib(20))

# 多重装饰器
def upper(fn):
    def w(*a):
        return fn(*a).upper()

    return w


def exclaim(fn):
    def w(*a):
        return fn(*a) + "!"

    return w


@upper
@exclaim
def greet(name):
    return "hello " + name


print(greet("world"))

# 解包
def f(a, b, c):
    return a + b + c


print(f(*[1, 2, 3]))
print(f(**{"a": 1, "b": 2, "c": 3}))
first, *mid, last = [1, 2, 3, 4, 5]
print(first, mid, last)
merged = {**{"a": 1}, **{"b": 2}}
print(merged)
print([*range(3), *range(3, 5)])
