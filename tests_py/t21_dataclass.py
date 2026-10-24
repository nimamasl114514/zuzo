from dataclasses import dataclass, field
import json


@dataclass
class User:
    name: str
    age: int
    tags: list = field(default_factory=list)


u = User("alice", 30)
print(u.name, u.age, u.tags)
print(json.dumps({"name": u.name, "age": u.age}))

match u:
    case User(n, a):
        print("matched:", n, a)


@dataclass
class Point:
    x: int
    y: int


p = Point(1, 2)
print(p.x, p.y)
print(isinstance(p, Point), type(p).__name__)


class Plain:
    def __init__(self, v):
        self.v = v

    def __repr__(self):
        return f"Plain({self.v})"


print(Plain(7), [Plain(1), Plain(2)])
print(sorted([3, 1, 2], key=lambda x: -x))
print({k: v for k, v in [("a", 1), ("b", 2)]})
