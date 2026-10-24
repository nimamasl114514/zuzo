def classify(x):
    match x:
        case 0:
            return "zero"
        case 1 | 2 | 3:
            return "small"
        case int(n) if n > 100:
            return "big"
        case int():
            return "int"
        case str(s) if len(s) > 5:
            return "long string"
        case str():
            return "string"
        case [a, b]:
            return f"pair {a},{b}"
        case [first, *rest]:
            return f"list first={first} rest={rest}"
        case {"type": t}:
            return f"dict type={t}"
        case _:
            return "other"


for v in [0, 2, 500, 42, "hi", "hello world", [1, 2], [1, 2, 3], {"type": "A"}, 3.14]:
    print(classify(v))


class Point:
    __match_args__ = ("x", "y")

    def __init__(self, x, y):
        self.x = x
        self.y = y


def describe(p):
    match p:
        case Point(0, 0):
            return "origin"
        case Point(x, 0):
            return f"on x-axis at {x}"
        case Point(0, y):
            return f"on y-axis at {y}"
        case Point(x, y):
            return f"point {x},{y}"
    return "unknown"


print(describe(Point(0, 0)))
print(describe(Point(5, 0)))
print(describe(Point(0, 7)))
print(describe(Point(3, 4)))

match {"cmd": "go", "speed": 10}:
    case {"cmd": "go", "speed": s}:
        print("go with speed", s)
    case _:
        print("nope")

match (1, 2, 3):
    case (1, *mid):
        print("mid", mid)

match "abc":
    case ["a", rest]:
        print("str match", rest)

match 5:
    case int() as n if n % 2 == 1:
        print("odd", n)
