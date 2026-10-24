class Base:
    kind = "base"
    def __init__(self, name):
        self.name = name
        print("Base.__init__", name)
    def describe(self):
        return f"{self.kind}:{self.name}"
    @classmethod
    def create(cls, *args):
        return cls(*args)
    @staticmethod
    def helper(x):
        return x * 2
    @property
    def label(self):
        return self.name.upper()

class Mid(Base):
    kind = "mid"
    def __init__(self, name, level):
        super().__init__(name)
        self.level = level
    def describe(self):
        return super().describe() + f"/L{self.level}"

class Leaf(Mid):
    kind = "leaf"
    def describe(self):
        return "leaf(" + super().describe() + ")"
    @property
    def label(self):
        return "<" + super().label + ">"

leaf = Leaf("node", 3)
print(leaf.describe())
print(leaf.label)
print(Leaf.kind, leaf.name, leaf.level)
print(Leaf.create("made", 1).describe())
print(Leaf.helper(21), leaf.helper(2), Base.helper(3))
print([c.__name__ for c in Leaf.__mro__])
print(isinstance(leaf, Base), issubclass(Leaf, Mid))

class Counter:
    total = 0
    def __init__(self):
        Counter.total += 1
        self.id = Counter.total

c1, c2 = Counter(), Counter()
print(c1.id, c2.id, Counter.total)

print(hasattr(leaf, "describe"), getattr(leaf, "kind"), type(leaf).__name__)
