class Positive:
    def __init__(self, name):
        self.name = name

    def __get__(self, obj, objtype=None):
        if obj is None:
            return self
        return obj.__dict__.get(self.name, 0)

    def __set__(self, obj, value):
        if value < 0:
            raise ValueError("must be positive")
        obj.__dict__[self.name] = value


class Model:
    score = Positive("score")

    def __init__(self, score):
        self.score = score


m = Model(10)
print(m.score)
m.score = 42
print(m.score)
try:
    m.score = -1
except ValueError as e:
    print("rejected:", e)


class Slotted:
    __slots__ = ("x", "y")

    def __init__(self, x, y):
        self.x = x
        self.y = y


s = Slotted(1, 2)
print(s.x, s.y)


def counter():
    i = 0
    while True:
        cmd = yield i
        if cmd == "stop":
            return "finished"
        i += 1


g = counter()
print(next(g), next(g))
try:
    g.send("stop")
except StopIteration as e:
    print("stopped:", e.value)

gen = (x for x in range(10))
gen.close()
print("closed ok")


class Meta(type):
    def __new__(mcls, name, bases, ns):
        cls = super().__new__(mcls, name, bases, ns)
        cls.tag = "meta:" + name
        return cls


class WithMeta(metaclass=Meta):
    pass


print(WithMeta.tag)
