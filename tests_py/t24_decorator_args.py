import functools

def logged(tag="call"):
    def deco(fn):
        @functools.wraps(fn)
        def wrapper(*args, **kwargs):
            print(f"[{tag}] before {fn.__name__} args={args} kwargs={sorted(kwargs)}")
            result = fn(*args, **kwargs)
            print(f"[{tag}] after {fn.__name__} -> {result}")
            return result
        return wrapper
    return deco

@logged()
def add(a, b):
    return a + b

@logged(tag="math")
def mul(a, b=2):
    return a * b

print(add(1, 2))
print(mul(3, b=4))

def repeat(times):
    def deco(fn):
        @functools.wraps(fn)
        def wrapper(*a, **kw):
            return [fn(*a, **kw) for _ in range(times)]
        return wrapper
    return deco

@repeat(3)
def hello(name="x"):
    return f"hi {name}"

print(hello("bob"))
print(add.__name__, hello.__name__)

def cache(fn):
    store = {}
    @functools.wraps(fn)
    def wrapper(n):
        if n not in store:
            store[n] = fn(n)
        return store[n]
    wrapper.memo = store
    return wrapper

@cache
def fib(n):
    return n if n < 2 else fib(n - 1) + fib(n - 2)

print(fib(10))
print(sorted(fib.memo.items()))
