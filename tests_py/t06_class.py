class Animal:
    kind = "animal"

    def __init__(self, name, age=1):
        self.name = name
        self.age = age

    def speak(self):
        return f"{self.name} makes a sound"

    def __str__(self):
        return f"Animal({self.name}, {self.age})"

    def __repr__(self):
        return f"Animal({self.name!r})"

    def __eq__(self, other):
        return isinstance(other, Animal) and self.name == other.name

    def __len__(self):
        return self.age


class Dog(Animal):
    kind = "dog"

    def __init__(self, name, breed):
        super().__init__(name, 3)
        self.breed = breed

    def speak(self):
        return f"{self.name} barks"

    def fetch(self):
        return f"{self.name} fetches"


class Cat(Animal):
    def speak(self):
        return f"{self.name} meows"


a = Animal("generic")
d = Dog("Rex", "Husky")
c = Cat("Tom")

print(a.speak())
print(d.speak())
print(c.speak())
print(d.fetch())
print(a.kind, d.kind, c.kind)
print(len(d))
print(str(a), repr(a))
print(a == Animal("generic"), a == d)
print(isinstance(d, Animal), isinstance(d, Dog), issubclass(Dog, Animal))

class Point:
    def __init__(self, x, y):
        self.x = x
        self.y = y

    def __add__(self, other):
        return Point(self.x + other.x, self.y + other.y)

    def __sub__(self, other):
        return Point(self.x - other.x, self.y - other.y)

    def __mul__(self, k):
        return Point(self.x * k, self.y * k)

    def __neg__(self):
        return Point(-self.x, -self.y)

    def __lt__(self, other):
        return (self.x, self.y) < (other.x, other.y)

    def __getitem__(self, i):
        return (self.x, self.y)[i]

    def __call__(self, k):
        return Point(self.x * k, self.y * k)

    def __repr__(self):
        return f"Point({self.x}, {self.y})"


p = Point(1, 2)
q = Point(3, 4)
print(p + q, p - q, p * 3, -p)
print(p < q, p[0], p[1])
print(p(10))


class Stack:
    def __init__(self):
        self.items = []

    def push(self, x):
        self.items.append(x)

    def pop(self):
        return self.items.pop()

    def __len__(self):
        return len(self.items)

    def __bool__(self):
        return len(self.items) > 0


s = Stack()
s.push(1)
s.push(2)
print(len(s), bool(s), s.pop(), len(s))


class Temp:
    def __init__(self, celsius):
        self._c = celsius

    @property
    def celsius(self):
        return self._c

    @celsius.setter
    def celsius(self, v):
        self._c = v

    @property
    def fahrenheit(self):
        return self._c * 9 / 5 + 32

    @staticmethod
    def freezing():
        return 0

    @classmethod
    def from_fahrenheit(cls, f):
        return cls((f - 32) * 5 / 9)


t = Temp(25)
print(t.celsius, t.fahrenheit)
t.celsius = 30
print(t.fahrenheit, Temp.freezing())
print(Temp.from_fahrenheit(212).celsius)


class Base:
    def hello(self):
        return "base"


class Mixin:
    def hello(self):
        return "mixin+" + super().hello()


class Combined(Mixin, Base):
    pass


print(Combined().hello())
print([c.__name__ for c in Combined.__mro__])


class Counter2:
    total = 0

    def __init__(self):
        Counter2.total += 1


Counter2()
Counter2()
print(Counter2.total)


class Iterable:
    def __init__(self, n):
        self.n = n

    def __iter__(self):
        return iter(range(self.n))

    def __contains__(self, v):
        return 0 <= v < self.n


it = Iterable(3)
print(list(it), 1 in it, 5 in it)
