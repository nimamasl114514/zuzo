class A:
    kind = "A"

    def __init__(self, x):
        self.x = x

    def show(self):
        return f"A({self.x})"

    def __repr__(self):
        return f"A:{self.x}"

    def __eq__(self, other):
        return isinstance(other, A) and self.x == other.x

class B(A):
    kind = "B"

    def __init__(self, x, y):
        super().__init__(x)
        self.y = y

    def show(self):
        return f"B({{self.x}},{{self.y}})"

class C(B):
    def show(self):
        return "C+" + super().show()

a = A(2)
b = B(2, 3)
c = C(4, 5)
for o in (a, b, c):
    print(o.show(), repr(o), o.kind, type(o).__name__)
print(isinstance(c, A), isinstance(a, B), issubclass(C, A))
print(a == A(2), a == b)
print(A.__mro__[0].__name__, B.__mro__[1].__name__)
print([type(x).__name__ for x in (a, b, c)])
