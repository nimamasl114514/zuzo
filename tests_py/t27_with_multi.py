import contextlib

class Resource:
    def __init__(self, name, fail=False):
        self.name = name
        self.fail = fail
    def __enter__(self):
        print("open", self.name)
        if self.fail:
            raise RuntimeError("cannot open " + self.name)
        return self
    def __exit__(self, et, ev, tb):
        print("close", self.name, et.__name__ if et else "no-exc")
        return False

with Resource("a") as a, Resource("b") as b:
    print("inside", a.name, b.name)
try:
    with Resource("x") as x, Resource("bad", fail=True):
        print("never", x.name)
except RuntimeError as e:
    print("caught", e)

@contextlib.contextmanager
def tag(name):
    print("<" + name + ">")
    try:
        yield name
    finally:
        print("</" + name + ">")

with tag("p") as t:
    print("text", t)

with contextlib.ExitStack() as stack:
    names = [stack.enter_context(Resource(f"r{i}")).name for i in range(3)]
    print("stacked", names)

@contextlib.contextmanager
def swallow():
    try:
        yield
    except Exception as e:
        print("suppressed", type(e).__name__)

with swallow():
    raise ValueError("x")
with contextlib.suppress(ZeroDivisionError):
    1 / 0
print("after suppress")
